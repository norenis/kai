package router

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/norenis/kai/internal/brain"
	"github.com/norenis/kai/internal/events"
	"github.com/norenis/kai/internal/prompt"
	"github.com/norenis/kai/internal/provider"
	"github.com/norenis/kai/internal/retriever"
	"github.com/norenis/kai/internal/session"
)

// Confidence thresholds based on number of matching brain files.
const (
	HighConfidenceThreshold   = 3
	MediumConfidenceThreshold = 1
	DefaultRetrievalLimit     = 10
	MinMessagesForLearning    = 2
)

const reindexDebounce = 2 * time.Second


// ChatResult holds the result of a chat interaction.
type ChatResult struct {
	Response       string
	BrainFilesUsed []string
	Confidence     string
}

// Router orchestrates the conversation pipeline.
type Router struct {
	brain     *brain.FileBrain
	retriever *retriever.FileRetriever
	builder   *prompt.Builder
	provider  provider.Provider
	sessions  *session.Manager
	reindexCh chan struct{}
}

// New creates a Router.
func New(
	b *brain.FileBrain,
	r *retriever.FileRetriever,
	pb *prompt.Builder,
	p provider.Provider,
	sm *session.Manager,
) *Router {
	rt := &Router{
		brain:     b,
		retriever: r,
		builder:   pb,
		provider:  p,
		sessions:  sm,
		reindexCh: make(chan struct{}, 1),
	}
	go rt.reindexWorker()
	return rt
}

// reindexWorker runs in the background, debouncing rapid writes before rebuilding the index.
func (r *Router) reindexWorker() {
	for range r.reindexCh {
		timer := time.NewTimer(reindexDebounce)
	drain:
		for {
			select {
			case <-r.reindexCh:
			case <-timer.C:
				break drain
			}
		}
		timer.Stop()

		slog.Info("rebuilding full index in background")
		if err := r.brain.RebuildIndex(); err != nil {
			slog.Warn("background full index rebuild failed", "err", err)
		} else {
			slog.Info("background full index rebuild complete")
		}
	}
}

// triggerReindex sends a non-blocking signal to the background reindex worker.
func (r *Router) triggerReindex() {
	select {
	case r.reindexCh <- struct{}{}:
	default:
	}
}

// AskRetrievalLimit is the max brain files for read-only ask (smaller than chat to keep prompts lean).
const AskRetrievalLimit = 3

// Ask processes a read-only question — no learning extraction, no session history.
// Uses a compact prompt to keep token count low for small/local models.
func (r *Router) Ask(ctx context.Context, question string) (*ChatResult, error) {
	brainFiles, err := r.retriever.Retrieve(ctx, question, AskRetrievalLimit)
	if err != nil {
		return nil, fmt.Errorf("retrieve: %w", err)
	}

	var brainPaths []string
	for _, bf := range brainFiles {
		brainPaths = append(brainPaths, bf.Path)
	}

	// Compact system prompt — just the brain context, no full personality.
	systemPrompt := "Answer using the context below. Be concise.\n\n"
	for _, bf := range brainFiles {
		systemPrompt += "### " + bf.Path + "\n" + strings.TrimSpace(bf.Content) + "\n\n"
	}
	messages := []provider.Message{{Role: "user", Content: question}}

	resp, err := r.provider.Chat(ctx, systemPrompt, messages)
	if err != nil {
		return nil, fmt.Errorf("llm chat: %w", err)
	}

	confidence := brain.ConfidenceLow
	if len(brainFiles) > HighConfidenceThreshold {
		confidence = brain.ConfidenceHigh
	} else if len(brainFiles) > MediumConfidenceThreshold {
		confidence = brain.ConfidenceMedium
	}

	return &ChatResult{
		Response:       resp.Content,
		BrainFilesUsed: brainPaths,
		Confidence:     confidence,
	}, nil
}

// StreamAsk streams a brain-augmented answer. Returns channel of chunks and brain files used.
func (r *Router) StreamAsk(ctx context.Context, question string) (<-chan provider.Chunk, []string, error) {
	brainFiles, err := r.retriever.Retrieve(ctx, question, AskRetrievalLimit)
	if err != nil {
		return nil, nil, fmt.Errorf("retrieve: %w", err)
	}

	var brainPaths []string
	systemPrompt := "Answer using the context below. Be concise.\n\n"
	for _, bf := range brainFiles {
		brainPaths = append(brainPaths, bf.Path)
		systemPrompt += "### " + bf.Path + "\n" + strings.TrimSpace(bf.Content) + "\n\n"
	}

	ch, err := r.provider.Stream(ctx, systemPrompt, []provider.Message{{Role: "user", Content: question}})
	if err != nil {
		return nil, nil, fmt.Errorf("llm stream: %w", err)
	}
	return ch, brainPaths, nil
}

// AskSource is one numbered piece of context given to the LLM for an ask.
type AskSource struct {
	Numeral string `json:"numeral"` // lowercase roman numeral the answer cites, e.g. "ii"
	Path    string `json:"path"`    // note path
	Chunk   string `json:"chunk"`   // chunk id "path#n"
	Title   string `json:"title"`
}

var lowerRoman = []string{"i", "ii", "iii", "iv", "v", "vi", "vii", "viii", "ix", "x", "xi", "xii"}

// askHistoryLimit caps the earlier turns sent with a follow-up question.
const askHistoryLimit = 6

// StreamAskTraced is StreamAsk for the dashboard sheet: context entries are
// numbered with roman numerals the model is asked to cite ([i], [ii]),
// earlier turns of the same conversation are sent along, and the retrieval
// stages (embed, recall) are timed.
func (r *Router) StreamAskTraced(ctx context.Context, question string, history []provider.Message) (<-chan provider.Chunk, []AskSource, *events.Trace, error) {
	brainFiles, trace, err := r.retriever.RetrieveTraced(ctx, question, AskRetrievalLimit)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("retrieve: %w", err)
	}
	idx, _ := r.brain.LoadFullIndexCached()

	var sb strings.Builder
	sb.WriteString("Answer using the context below. Be concise. Cite the context you rely on with its numeral in square brackets, for example [i] or [ii][iii]. Cite nothing else.\n\n")
	sources := make([]AskSource, 0, len(brainFiles))
	for i, bf := range brainFiles {
		if i >= len(lowerRoman) {
			break
		}
		doc, chunk := bf.Path, bf.Path+"#0"
		if j := strings.LastIndex(bf.Path, "#"); j >= 0 {
			doc, chunk = bf.Path[:j], bf.Path
		}
		title := brain.FilenameTitle(doc)
		if idx != nil {
			if d, ok := idx.Documents[doc]; ok {
				title = d.DisplayTitle()
			}
		}
		src := AskSource{Numeral: lowerRoman[i], Path: doc, Chunk: chunk, Title: title}
		sources = append(sources, src)
		fmt.Fprintf(&sb, "### [%s] %s (%s)\n%s\n\n", src.Numeral, src.Title, doc, strings.TrimSpace(bf.Content))
	}

	if len(history) > askHistoryLimit {
		history = history[len(history)-askHistoryLimit:]
	}
	messages := append(append([]provider.Message{}, history...), provider.Message{Role: "user", Content: question})
	ch, err := r.provider.Stream(ctx, sb.String(), messages)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("llm stream: %w", err)
	}
	return ch, sources, trace, nil
}

// StreamFreeChat streams a direct LLM response without brain context.
func (r *Router) StreamFreeChat(ctx context.Context, message string) (<-chan provider.Chunk, error) {
	return r.provider.Stream(ctx, "", []provider.Message{{Role: "user", Content: message}})
}

// FreeChat sends a message directly to the LLM without brain context.
func (r *Router) FreeChat(ctx context.Context, message string) (*ChatResult, error) {
	messages := []provider.Message{{Role: "user", Content: message}}
	resp, err := r.provider.Chat(ctx, "", messages)
	if err != nil {
		return nil, fmt.Errorf("llm chat: %w", err)
	}
	return &ChatResult{Response: resp.Content}, nil
}

// Chat processes a user message through the full pipeline.
func (r *Router) Chat(ctx context.Context, sess *session.Session, userInput string) (*ChatResult, error) {
	// 1. Retrieve relevant brain context
	brainFiles, err := r.retriever.Retrieve(ctx, userInput, DefaultRetrievalLimit)
	if err != nil {
		return nil, fmt.Errorf("retrieve: %w", err)
	}

	var brainPaths []string
	for _, bf := range brainFiles {
		brainPaths = append(brainPaths, bf.Path)
	}

	// 2. Build system prompt
	systemPrompt := r.builder.BuildSystemPrompt(brainFiles)

	// 3. Build messages with history
	var history []provider.Message
	for _, m := range sess.Messages {
		history = append(history, m.Message)
	}
	messages := r.builder.BuildMessages(history, userInput)

	// 4. Call LLM
	resp, err := r.provider.Chat(ctx, systemPrompt, messages)
	if err != nil {
		return nil, fmt.Errorf("llm chat: %w", err)
	}

	// Determine confidence based on brain files count
	confidence := brain.ConfidenceLow
	if len(brainFiles) > HighConfidenceThreshold {
		confidence = brain.ConfidenceHigh
	} else if len(brainFiles) > MediumConfidenceThreshold {
		confidence = brain.ConfidenceMedium
	}

	// 5. Add messages to session
	now := time.Now().UTC()
	sess.AddMessage(session.SessionMessage{
		Message:   provider.Message{Role: "user", Content: userInput},
		Timestamp: now,
	})
	sess.AddMessage(session.SessionMessage{
		Message:        provider.Message{Role: "assistant", Content: resp.Content},
		Timestamp:      now,
		BrainFilesUsed: brainPaths,
		Confidence:     confidence,
	})

	// 6. Extract learnings (non-fatal on error)
	r.extractLearnings(ctx, brainFiles, sess)

	return &ChatResult{
		Response:       resp.Content,
		BrainFilesUsed: brainPaths,
		Confidence:     confidence,
	}, nil
}

func (r *Router) extractLearnings(ctx context.Context, brainFiles []*brain.BrainFile, sess *session.Session) {
	if len(sess.Messages) < MinMessagesForLearning {
		return
	}

	// Get last exchange
	lastTwo := sess.Messages[len(sess.Messages)-2:]
	exchange := []provider.Message{
		lastTwo[0].Message,
		lastTwo[1].Message,
	}

	learningPrompt := r.builder.BuildLearningPrompt(brainFiles, exchange)
	resp, err := r.provider.Chat(ctx, "", []provider.Message{
		{Role: "user", Content: learningPrompt},
	})
	if err != nil {
		slog.Warn("learning extraction failed", "err", err)
		return
	}

	learnings, err := brain.ParseLearnings(resp.Content)
	if err != nil {
		slog.Warn("parse learnings failed", "err", err)
		return
	}

	if len(learnings) == 0 {
		return
	}

	if err := r.brain.Learn(learnings); err != nil {
		slog.Error("brain learn failed", "err", err)
		return
	}

	// Fast sync rebuild of tag index so tag queries reflect new learnings immediately.
	if err := r.brain.RebuildTagIndex(); err != nil {
		slog.Warn("tag index rebuild failed", "err", err)
	}
	// Full BM25+vector rebuild happens asynchronously to avoid blocking the response.
	r.triggerReindex()

	sess.LearningsExtracted += len(learnings)
}
