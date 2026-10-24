package mcp

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	pathpkg "path"
	"sort"
	"strings"
	"time"

	kai "github.com/norenis/kai"
	"github.com/norenis/kai/internal/brain"
	"github.com/norenis/kai/internal/config"
	"github.com/norenis/kai/internal/events"
	"github.com/norenis/kai/internal/dashboard"
	"github.com/norenis/kai/internal/module"
	"github.com/norenis/kai/internal/prompt"
	"github.com/norenis/kai/internal/provider"
	"github.com/norenis/kai/internal/router"
	"github.com/norenis/kai/internal/session"
	bsync "github.com/norenis/kai/internal/sync"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// Source labels for brain file provenance.
const (
	SourceMCP    = "mcp"
	SourceDirect = "direct"
)

// vaultCategory is a reserved brain category that is invisible to brain_list,
// brain_search, and ask — content there is only reachable via brain_read /
// brain_write when the caller supplies the correct pin, checked fresh on
// every call. This mirrors an out-of-band vault (auth required every read),
// unlike ordinary brain content which is fully readable to anyone holding
// the MCP bearer token.
const vaultCategory = "vault"

// isVaultPath reports whether path falls under the reserved vault category.
// The path is cleaned first, so "knowledge/../vault/x.md" counts as vault.
func isVaultPath(path string) bool {
	return brain.IsVaultPath(path)
}

// vaultPinOK does a constant-time comparison against the configured vault
// pin. An empty configured pin fails closed — the vault category is simply
// unusable until a pin is set.
func (s *Server) vaultPinOK(pin string) bool {
	expected := s.cfg.Brain.VaultPin
	if expected == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(pin), []byte(expected)) == 1
}

// DefaultConfidence is used when no confidence level is specified.
const DefaultConfidence = "medium"

const reindexDebounce = 2 * time.Second

type Server struct {
	mcp      *mcpserver.MCPServer
	router   *router.Router
	brain    *brain.FileBrain
	sessions *session.Manager
	provider provider.Provider
	builder  *prompt.Builder
	cfg      *config.Config
	reindexCh chan struct{} // signals background full-index rebuild
}

func NewServer(
	r *router.Router,
	b *brain.FileBrain,
	sm *session.Manager,
	p provider.Provider,
	cfg *config.Config,
) *Server {
	s := &Server{
		router:    r,
		brain:     b,
		sessions:  sm,
		provider:  p,
		builder:   prompt.NewBuilder(cfg.AgentName(), cfg.UserName()),
		cfg:       cfg,
		reindexCh: make(chan struct{}, 1),
	}

	s.mcp = mcpserver.NewMCPServer(
		cfg.AgentName(),
		kai.Version,
		mcpserver.WithToolCapabilities(false),
	)

	s.registerTools()
	go s.reindexWorker()
	return s
}

// reindexWorker runs in the background. It waits for a signal, debounces
// rapid writes, then rebuilds the full BM25+vector index.
func (s *Server) reindexWorker() {
	for range s.reindexCh {
		// Drain any extra signals queued during the debounce window.
		timer := time.NewTimer(reindexDebounce)
	drain:
		for {
			select {
			case <-s.reindexCh:
			case <-timer.C:
				break drain
			}
		}
		timer.Stop()

		slog.Info("rebuilding full index in background")
		if err := s.brain.RebuildIndex(); err != nil {
			slog.Warn("background full index rebuild failed", "err", err)
		} else {
			slog.Info("background full index rebuild complete")
		}
	}
}

// triggerReindex sends a non-blocking signal to the background reindex worker.
func (s *Server) triggerReindex() {
	select {
	case s.reindexCh <- struct{}{}:
	default: // already queued, skip
	}
}

// TriggerReindex is the public form, called from startup.
func (s *Server) TriggerReindex() { s.triggerReindex() }

func (s *Server) Serve() error {
	return mcpserver.ServeStdio(s.mcp)
}

// ServeHTTP starts the MCP server over Streamable HTTP with bearer token auth.
// Also mounts the dashboard at /dashboard (no auth required).
func (s *Server) ServeHTTP(addr, token string) error {
	httpServer := mcpserver.NewStreamableHTTPServer(s.mcp)
	expected := []byte("Bearer " + token)

	mux := http.NewServeMux()
	mux.Handle("/mcp", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := []byte(r.Header.Get("Authorization"))
		if subtle.ConstantTimeCompare(got, expected) != 1 {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		httpServer.ServeHTTP(w, r)
	}))

	// One event bus serves MCP writes and the dashboard's live stream.
	bus := events.NewBus()
	s.brain.SetEvents(bus, events.SourceMCP)
	if err := s.brain.WatchDisk(context.Background()); err != nil {
		slog.Warn("brain file watcher disabled", "err", err)
	} else {
		go reindexOnExternalWrites(context.Background(), bus, externalReindexDelay, s.triggerReindex)
	}

	// Mount dashboard at /dashboard.
	dash := dashboard.New(s.brain, s.cfg.Brain.Path, s.cfg)
	dash.SetEvents(bus)
	// Set router for dashboard chat.
	if s.router != nil {
		dash.SetRouter(s.router)
		slog.Info("chat available in dashboard (LLM connected)")
	}
	// Build syncer for dashboard sync controls.
	if syncer := s.buildSyncer(); syncer != nil {
		dash.SetSyncer(syncer)
		slog.Info("sync backends available for dashboard")
	} else {
		slog.Debug("no sync backends configured", "git_enabled", s.cfg.Brain.Sync.Git.Enabled)
	}
	dash.RegisterRoutes(mux)

	slog.Info("MCP server listening", "addr", addr+"/mcp")
	slog.Info("dashboard available", "addr", addr+"/")


	return http.ListenAndServe(addr, mux)
}

// buildSyncer creates a Syncer from the current config.
func (s *Server) buildSyncer() *bsync.Syncer {
	brainPath := s.cfg.Brain.Path
	if brainPath == "" {
		brainPath = "."
	}

	var gitBackend bsync.Backend
	if s.cfg.Brain.Sync.Git.Enabled {
		gitBackend = bsync.NewGitBackend(brainPath, s.cfg.Brain.Sync.Git.Remote, s.cfg.Brain.Sync.Git.Branch)
	}

	if gitBackend == nil {
		return nil
	}
	return bsync.NewSyncer(brainPath, gitBackend, nil)
}

func (s *Server) registerTools() {
	s.mcp.AddTool(
		mcplib.NewTool("brain_list",
			mcplib.WithDescription("List all brain knowledge files with tags and confidence levels"),
		),
		s.handleBrainList,
	)

	s.mcp.AddTool(
		mcplib.NewTool("brain_read",
			mcplib.WithDescription("Read a specific brain file's full content and metadata"),
			mcplib.WithString("path",
				mcplib.Required(),
				mcplib.Description("Relative path within brain directory (e.g. 'opinions/testing.md')"),
			),
			mcplib.WithString("pin",
				mcplib.Description("Required when path starts with 'vault/' — the configured vault pin"),
			),
		),
		s.handleBrainRead,
	)

	s.mcp.AddTool(
		mcplib.NewTool("brain_write",
			mcplib.WithDescription("Write or update a brain file directly. No LLM needed. Creates the file if it doesn't exist."),
			mcplib.WithString("path",
				mcplib.Required(),
				mcplib.Description("Relative path within brain directory. Must start with an allowed category: "+strings.Join(brain.DefaultCategories, ", ")+" — or 'vault' (requires 'pin'). Example: 'opinions/testing.md'"),
			),
			mcplib.WithString("content",
				mcplib.Required(),
				mcplib.Description("The content to write"),
			),
			mcplib.WithString("tags",
				mcplib.Required(),
				mcplib.Description("Comma-separated tags (e.g. 'go,testing,tdd')"),
			),
			mcplib.WithString("confidence",
				mcplib.Description("Confidence level: high, medium, or low (default: medium)"),
			),
			mcplib.WithString("pin",
				mcplib.Description("Required when path starts with 'vault/' — the configured vault pin"),
			),
		),
		s.handleBrainWrite,
	)

	s.mcp.AddTool(
		mcplib.NewTool("brain_search",
			mcplib.WithDescription("Search brain knowledge by keywords. Returns scored results. No LLM call."),
			mcplib.WithString("query",
				mcplib.Required(),
				mcplib.Description("Search keywords (e.g. 'testing go patterns')"),
			),
		),
		s.handleBrainSearch,
	)

	s.mcp.AddTool(
		mcplib.NewTool("ask",
			mcplib.WithDescription("Ask kai a question. Two modes: (1) With LLM: full pipeline — retrieval, prompt, LLM call, learning extraction. Returns answer. (2) Without LLM (e.g. via Claude Code): retrieves brain context and builds system prompt. Returns the context so the MCP client can answer as the user's digital twin."),
			mcplib.WithString("question",
				mcplib.Required(),
				mcplib.Description("The question to ask"),
			),
		),
		s.handleAsk,
	)

	s.mcp.AddTool(
		mcplib.NewTool("teach",
			mcplib.WithDescription("Teach kai something new. Two modes: (1) With LLM configured: pass 'input' and kai extracts learnings automatically. (2) Without LLM (e.g. via Claude Code): pass 'category', 'topic', 'content', 'tags' directly — you do the extraction, kai saves it."),
			mcplib.WithString("input",
				mcplib.Description("Free-form teaching input (requires LLM provider). e.g. 'I prefer TDD for business logic'"),
			),
			mcplib.WithString("category",
				mcplib.Description("Brain category — must be one of: "+strings.Join(brain.DefaultCategories, ", ")+" (direct mode, no LLM needed)"),
			),
			mcplib.WithString("topic",
				mcplib.Description("Topic slug for filename, e.g. 'testing-philosophy' (direct mode)"),
			),
			mcplib.WithString("content",
				mcplib.Description("The knowledge content in markdown (direct mode)"),
			),
			mcplib.WithString("tags",
				mcplib.Description("Comma-separated tags, e.g. 'testing,tdd,go' (direct mode)"),
			),
			mcplib.WithString("confidence",
				mcplib.Description("Confidence level: high, medium, or low (default: medium)"),
			),
			mcplib.WithString("action",
				mcplib.Description("Action: create, append, or replace (default: create)"),
			),
		),
		s.handleTeach,
	)

	s.mcp.AddTool(
		mcplib.NewTool("brain_reorganize",
			mcplib.WithDescription("Analyze brain files for duplicates, miscategorizations, and consolidation opportunities. Returns a reorganization plan. Pass apply=true to execute it."),
			mcplib.WithString("mode",
				mcplib.Description("Mode: all, dedup, recategorize, or consolidate (default: all)"),
			),
			mcplib.WithNumber("similarity_threshold",
				mcplib.Description("Similarity threshold for duplicate detection 0.0-1.0 (default: 0.7)"),
			),
			mcplib.WithBoolean("apply",
				mcplib.Description("Execute the plan immediately (default: false, dry-run)"),
			),
		),
		s.handleBrainReorganize,
	)

	s.mcp.AddTool(
		mcplib.NewTool("module_list",
			mcplib.WithDescription("List all available modules (plugins) and their enabled status."),
		),
		s.handleModuleList,
	)

	s.mcp.AddTool(
		mcplib.NewTool("module_run",
			mcplib.WithDescription("Run a module (plugin) to fetch external data into the brain. Pass a module name, or omit to run all enabled modules."),
			mcplib.WithString("name",
				mcplib.Description("Module name to run (e.g. 'github_prs'). Omit to run all enabled modules."),
			),
		),
		s.handleModuleRun,
	)

	s.mcp.AddTool(
		mcplib.NewTool("setup_commands",
			mcplib.WithDescription("Get kai slash command files for Claude Code. Returns file names and contents as JSON. Write each file to ~/.claude/commands/kai/<name>.md to install the /kai:* slash commands."),
		),
		s.handleSetupCommands,
	)

	s.mcp.AddTool(
		mcplib.NewTool("tasks",
			mcplib.WithDescription("Manage today's task list. Actions: list (show tasks), add (create task), done/undone (toggle by index), rm (remove by index)."),
			mcplib.WithString("action",
				mcplib.Required(),
				mcplib.Description("Action: list, add, done, undone, or rm"),
			),
			mcplib.WithString("text",
				mcplib.Description("Task text — required for 'add'"),
			),
			mcplib.WithNumber("index",
				mcplib.Description("Zero-based task index — required for done, undone, rm"),
			),
			mcplib.WithString("priority",
				mcplib.Description("Priority: high, medium, or low — optional for 'add'"),
			),
		),
		s.handleTasksTool,
	)
}

func (s *Server) handleBrainList(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	slog.Debug("brain_list: scanning files")
	files, err := s.brain.ListAll()
	if err != nil {
		return mcplib.NewToolResultError(fmt.Sprintf("list brain files: %v", err)), nil
	}
	slog.Debug("brain_list: found files", "count", len(files))

	type brainEntry struct {
		Path       string   `json:"path"`
		Tags       []string `json:"tags"`
		Confidence string   `json:"confidence"`
	}

	var entries []brainEntry
	for _, f := range files {
		if isVaultPath(f) {
			continue
		}
		bf, err := s.brain.Load(f)
		if err != nil {
			slog.Debug("brain_list: failed to load file", "path", f, "err", err)
			continue
		}
		entries = append(entries, brainEntry{
			Path:       f,
			Tags:       bf.Tags,
			Confidence: bf.Confidence,
		})
	}
	slog.Debug("brain_list: returning entries", "count", len(entries))

	if entries == nil {
		entries = []brainEntry{}
	}

	data, err := json.Marshal(entries)
	if err != nil {
		return mcplib.NewToolResultError(fmt.Sprintf("marshal response: %v", err)), nil
	}

	return mcplib.NewToolResultText(string(data)), nil
}

func (s *Server) handleBrainSearch(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	query, err := req.RequireString("query")
	if err != nil {
		return mcplib.NewToolResultError("missing required parameter: query"), nil
	}

	type searchResult struct {
		Path    string  `json:"path"`
		Score   float64 `json:"score"`
		Preview string  `json:"preview"`
	}

	// Full-text TF-IDF search with relevance scoring
	type candidate struct {
		path  string
		score float64
	}
	var candidates []candidate

	slog.Debug("brain_search: querying", "query", query)
	fullResults := s.brain.QueryFullIndex(query)
	if len(fullResults) > 0 {
		slog.Debug("brain_search: BM25 results", "count", len(fullResults))
		for _, fr := range fullResults {
			candidates = append(candidates, candidate{path: fr.Path, score: fr.Score})
		}
	} else {
		// Fall back to tag-based search
		keywords := strings.Fields(strings.ToLower(query))
		slog.Debug("brain_search: BM25 empty, falling back to tag search", "keywords", keywords)
		tagResults := s.brain.QueryIndex(keywords)
		slog.Debug("brain_search: tag results", "count", len(tagResults))
		for _, tr := range tagResults {
			candidates = append(candidates, candidate{path: tr.Path, score: float64(tr.Score)})
		}
	}

	var entries []searchResult
	for _, c := range candidates {
		if isVaultPath(c.path) {
			continue
		}
		bf, err := s.brain.Load(c.path)
		if err != nil {
			continue
		}
		finalScore := brain.RelevanceScore(bf, c.score)
		preview := ""
		content := strings.TrimSpace(bf.Content)
		lines := strings.SplitN(content, "\n", 2)
		if len(lines) > 0 {
			preview = lines[0]
		}
		entries = append(entries, searchResult{
			Path:    c.path,
			Score:   finalScore,
			Preview: preview,
		})
	}

	// Sort by score descending
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Score > entries[j].Score
	})

	if entries == nil {
		entries = []searchResult{}
	}

	data, err := json.Marshal(entries)
	if err != nil {
		return mcplib.NewToolResultError(fmt.Sprintf("marshal response: %v", err)), nil
	}

	return mcplib.NewToolResultText(string(data)), nil
}

func (s *Server) handleAsk(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	question, err := req.RequireString("question")
	if err != nil {
		return mcplib.NewToolResultError("missing required parameter: question"), nil
	}

	// With LLM: read-only ask (no learning extraction, no session)
	if s.router != nil {
		result, err := s.router.Ask(ctx, question)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("ask error: %v", err)), nil
		}

		type askResponse struct {
			Response       string   `json:"response"`
			Confidence     string   `json:"confidence"`
			BrainFilesUsed []string `json:"brain_files_used"`
		}

		data, err := json.Marshal(askResponse{
			Response:       result.Response,
			Confidence:     result.Confidence,
			BrainFilesUsed: result.BrainFilesUsed,
		})
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("marshal response: %v", err)), nil
		}

		return mcplib.NewToolResultText(string(data)), nil
	}

	// Without LLM: search brain directly and return the top matched chunks as plain text.
	const askMaxChunks = 3
	chunks := s.brain.QueryHybrid(question)

	var sb strings.Builder
	seen := make(map[string]bool)
	count := 0

	for _, cr := range chunks {
		if count >= askMaxChunks {
			break
		}
		if isVaultPath(cr.DocPath) {
			continue
		}
		if seen[cr.DocPath] {
			continue
		}
		seen[cr.DocPath] = true

		bf, loadErr := s.brain.Load(cr.DocPath)
		if loadErr != nil {
			continue
		}
		content := strings.TrimSpace(bf.Content)
		if cr.Offset < len(content) {
			end := cr.Offset + cr.Length
			if end > len(content) {
				end = len(content)
			}
			content = strings.TrimSpace(content[cr.Offset:end])
		}

		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString("## ")
		sb.WriteString(cr.DocPath)
		sb.WriteString("\n")
		sb.WriteString(content)
		count++
	}

	if sb.Len() == 0 {
		return mcplib.NewToolResultText("No relevant brain files found for: " + question), nil
	}

	return mcplib.NewToolResultText(sb.String()), nil
}

func (s *Server) handleBrainRead(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	path, err := req.RequireString("path")
	if err != nil {
		return mcplib.NewToolResultError("missing required parameter: path"), nil
	}
	if isVaultPath(path) {
		pin, _ := req.RequireString("pin")
		if !s.vaultPinOK(pin) {
			return mcplib.NewToolResultError("vault: invalid or missing pin"), nil
		}
	}
	slog.Debug("brain_read", "path", path)

	bf, err := s.brain.Load(path)
	if err != nil {
		return mcplib.NewToolResultError(fmt.Sprintf("load brain file: %v", err)), nil
	}

	type brainFileResponse struct {
		Path       string   `json:"path"`
		Tags       []string `json:"tags"`
		Confidence string   `json:"confidence"`
		Content    string   `json:"content"`
	}

	data, err := json.Marshal(brainFileResponse{
		Path:       bf.Path,
		Tags:       bf.Tags,
		Confidence: bf.Confidence,
		Content:    strings.TrimSpace(bf.Content),
	})
	if err != nil {
		return mcplib.NewToolResultError(fmt.Sprintf("marshal response: %v", err)), nil
	}

	return mcplib.NewToolResultText(string(data)), nil
}

func (s *Server) handleBrainWrite(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	path, err := req.RequireString("path")
	if err != nil {
		return mcplib.NewToolResultError("missing required parameter: path"), nil
	}
	if !strings.HasSuffix(path, ".md") {
		path = path + ".md"
	}
	// Validate that the path starts with an allowed root category, or the
	// reserved vault category (gated separately by pin below).
	// Judge the cleaned path: "knowledge/../vault/x.md" is a vault write.
	rootDir := strings.SplitN(strings.TrimPrefix(pathpkg.Clean("/"+path), "/"), "/", 2)[0]
	if rootDir == vaultCategory {
		pin, _ := req.RequireString("pin")
		if !s.vaultPinOK(pin) {
			return mcplib.NewToolResultError("vault: invalid or missing pin"), nil
		}
	} else if !brain.ValidCategory(rootDir) {
		return mcplib.NewToolResultError(fmt.Sprintf(
			"invalid category %q — must be one of: %s",
			rootDir, strings.Join(brain.DefaultCategories, ", "),
		)), nil
	}

	content, err := req.RequireString("content")
	if err != nil {
		return mcplib.NewToolResultError("missing required parameter: content"), nil
	}

	tagsStr, err := req.RequireString("tags")
	if err != nil {
		return mcplib.NewToolResultError("missing required parameter: tags"), nil
	}

	confidence, _ := req.RequireString("confidence")
	if confidence == "" {
		confidence = DefaultConfidence
	}

	var tags []string
	for _, t := range strings.Split(tagsStr, ",") {
		t = strings.TrimSpace(t)
		if t != "" {
			tags = append(tags, t)
		}
	}

	slog.Debug("brain_write", "path", path, "tags", tags, "confidence", confidence)
	bf := &brain.BrainFile{
		Path:       path,
		Tags:       tags,
		Confidence: confidence,
		Source:     SourceMCP,
		Updated:    brain.DateOnly{Time: time.Now()},
		Content:    "\n" + content + "\n",
	}

	bf.WrittenBy = events.SourceMCP
	if err := s.brain.Save(bf); err != nil {
		return mcplib.NewToolResultError(fmt.Sprintf("save brain file: %v", err)), nil
	}
	slog.Debug("brain_write: file saved, rebuilding tag index", "path", path)
	if err := s.brain.RebuildTagIndex(); err != nil {
		slog.Warn("rebuild tag index failed", "err", err)
	}
	s.triggerReindex()

	return mcplib.NewToolResultText(fmt.Sprintf("Saved %s", path)), nil
}

func (s *Server) handleBrainReorganize(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	mode, _ := req.RequireString("mode")
	if mode == "" {
		mode = brain.ModeAll
	}

	apply := false
	if v, err := req.RequireBool("apply"); err == nil {
		apply = v
	}

	simThreshold := 0.7
	if v, err := req.RequireFloat("similarity_threshold"); err == nil && v > 0 {
		simThreshold = v
	}

	opts := brain.ReorgOptions{
		Mode:                mode,
		SimilarityThreshold: simThreshold,
		SmallFileThreshold:  50,
		DryRun:              !apply,
	}

	// Reorganize holds a single lock for the entire find+apply sequence,
	// preventing TOCTOU races between analysis and execution.
	plan, err := s.brain.Reorganize(opts)
	if err != nil {
		return mcplib.NewToolResultError(fmt.Sprintf("reorganize: %v", err)), nil
	}

	type planResponse struct {
		Actions []brain.ReorgAction `json:"actions"`
		Applied bool                `json:"applied"`
		Count   int                 `json:"count"`
	}

	actions := plan.Actions
	if actions == nil {
		actions = []brain.ReorgAction{}
	}

	data, err := json.Marshal(planResponse{
		Actions: actions,
		Applied: apply,
		Count:   len(actions),
	})
	if err != nil {
		return mcplib.NewToolResultError(fmt.Sprintf("marshal response: %v", err)), nil
	}

	// Trigger reindex after reorganization applies changes (debounced, non-blocking).
	if apply && len(actions) > 0 {
		s.triggerReindex()
	}

	return mcplib.NewToolResultText(string(data)), nil
}

func (s *Server) handleTeach(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	// Direct mode: category + topic + content provided (no LLM needed)
	category, _ := req.RequireString("category")
	topic, _ := req.RequireString("topic")
	content, _ := req.RequireString("content")

	if category != "" && topic != "" && content != "" {
		return s.handleTeachDirect(category, topic, content, req)
	}

	// LLM mode: free-form input, extract learnings via provider
	input, _ := req.RequireString("input")
	if input == "" {
		return mcplib.NewToolResultError("provide either 'input' (requires LLM) or 'category' + 'topic' + 'content' (direct mode, no LLM)"), nil
	}

	if s.provider == nil {
		return mcplib.NewToolResultError("free-form 'input' requires an LLM provider. Either configure a provider in config.yaml, or use direct mode with 'category', 'topic', and 'content' parameters"), nil
	}

	messages := []provider.Message{
		{Role: "user", Content: "I want to teach you about myself: " + input},
	}

	teachPrompt := s.builder.BuildLearningPrompt(nil, messages)

	resp, err := s.provider.Chat(ctx, "", []provider.Message{
		{Role: "user", Content: teachPrompt},
	})
	if err != nil {
		return mcplib.NewToolResultError(fmt.Sprintf("LLM error: %v", err)), nil
	}

	learnings, err := brain.ParseLearnings(resp.Content)
	if err != nil || len(learnings) == 0 {
		return mcplib.NewToolResultText("I couldn't extract any specific learnings from that. Try being more concrete."), nil
	}

	for i := range learnings {
		learnings[i].Source = SourceDirect
	}

	if err := s.brain.Learn(learnings); err != nil {
		return mcplib.NewToolResultError(fmt.Sprintf("save learnings: %v", err)), nil
	}
	if err := s.brain.RebuildTagIndex(); err != nil {
		slog.Warn("rebuild tag index failed", "err", err)
	}
	s.triggerReindex()

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Learned %d new things:\n", len(learnings)))
	for _, l := range learnings {
		sb.WriteString(fmt.Sprintf("  - %s/%s.md\n", l.Category, l.Topic))
	}

	return mcplib.NewToolResultText(sb.String()), nil
}

func (s *Server) handleTeachDirect(category, topic, content string, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	tagsStr, _ := req.RequireString("tags")
	confidence, _ := req.RequireString("confidence")
	action, _ := req.RequireString("action")

	if confidence == "" {
		confidence = DefaultConfidence
	}
	if action == "" {
		action = "create"
	}

	var tags []string
	if tagsStr != "" {
		for _, t := range strings.Split(tagsStr, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				tags = append(tags, t)
			}
		}
	}

	learnings := []brain.Learning{{
		Category:   category,
		Topic:      topic,
		Tags:       tags,
		Content:    content,
		Confidence: confidence,
		Action:     action,
		Source:     SourceDirect,
	}}

	if err := s.brain.Learn(learnings); err != nil {
		return mcplib.NewToolResultError(fmt.Sprintf("save learning: %v", err)), nil
	}
	if err := s.brain.RebuildTagIndex(); err != nil {
		slog.Warn("rebuild tag index failed", "err", err)
	}
	s.triggerReindex()

	return mcplib.NewToolResultText(fmt.Sprintf("Saved to brain/%s/%s.md", category, topic)), nil
}

func (s *Server) handleModuleList(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	reg := module.Default()
	available := reg.Available()
	sort.Strings(available)

	enabled := make(map[string]bool, len(s.cfg.Modules))
	for _, mc := range s.cfg.Modules {
		if mc.Enabled {
			enabled[mc.Name] = true
		}
	}

	type moduleEntry struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Enabled     bool   `json:"enabled"`
	}
	entries := make([]moduleEntry, 0, len(available))
	for _, name := range available {
		entries = append(entries, moduleEntry{
			Name:        name,
			Description: reg.Description(name),
			Enabled:     enabled[name],
		})
	}

	data, err := json.Marshal(entries)
	if err != nil {
		return mcplib.NewToolResultError(fmt.Sprintf("marshal: %v", err)), nil
	}
	return mcplib.NewToolResultText(string(data)), nil
}

func (s *Server) handleModuleRun(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	name, _ := req.RequireString("name")

	reg := module.Default()

	// Helper to run a single module.
	runOne := func(modName string) (int, error) {
		var modCfg map[string]interface{}
		for _, mc := range s.cfg.Modules {
			if mc.Name == modName {
				modCfg = mc.Config
				break
			}
		}

		mod, err := reg.Build(modName, modCfg)
		if err != nil {
			return 0, err
		}

		learnings, err := mod.Fetch(ctx)
		if err != nil {
			return 0, fmt.Errorf("fetch: %w", err)
		}
		if len(learnings) == 0 {
			return 0, nil
		}

		if err := s.brain.Learn(learnings); err != nil {
			return 0, fmt.Errorf("save: %w", err)
		}
		if err := s.brain.RebuildTagIndex(); err != nil {
			slog.Warn("rebuild tag index failed", "err", err)
		}
		s.triggerReindex()
		return len(learnings), nil
	}

	// Run a specific module.
	if name != "" {
		count, err := runOne(name)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("%s: %v", name, err)), nil
		}
		if count == 0 {
			return mcplib.NewToolResultText(fmt.Sprintf("%s: nothing fetched", name)), nil
		}
		return mcplib.NewToolResultText(fmt.Sprintf("%s → %d item(s) imported", name, count)), nil
	}

	// Run all enabled modules.
	var sb strings.Builder
	ran := 0
	for _, mc := range s.cfg.Modules {
		if !mc.Enabled {
			continue
		}
		count, err := runOne(mc.Name)
		if err != nil {
			sb.WriteString(fmt.Sprintf("%s: error: %v\n", mc.Name, err))
		} else if count == 0 {
			sb.WriteString(fmt.Sprintf("%s: nothing fetched\n", mc.Name))
		} else {
			sb.WriteString(fmt.Sprintf("%s → %d item(s) imported\n", mc.Name, count))
		}
		ran++
	}
	if ran == 0 {
		return mcplib.NewToolResultText("no modules enabled — set 'enabled: true' in config.yaml"), nil
	}
	return mcplib.NewToolResultText(strings.TrimSpace(sb.String())), nil
}

func (s *Server) handleSetupCommands(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	agentName := s.cfg.AgentName()

	type cmdEntry struct {
		Name       string `json:"name"`
		Content    string `json:"content"`
		InstallDir string `json:"install_dir"`
	}

	cmds := kai.ClaudeCommands(agentName)
	installDir := "~/.claude/commands/" + agentName

	entries := make([]cmdEntry, 0, len(cmds))
	for name, content := range cmds {
		entries = append(entries, cmdEntry{
			Name:       name,
			Content:    content,
			InstallDir: installDir,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })

	data, err := json.Marshal(entries)
	if err != nil {
		return mcplib.NewToolResultError(fmt.Sprintf("marshal: %v", err)), nil
	}
	return mcplib.NewToolResultText(string(data)), nil
}

func (s *Server) handleTasksTool(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	action, err := req.RequireString("action")
	if err != nil || action == "" {
		return mcplib.NewToolResultError("action is required: list, add, done, undone, or rm"), nil
	}

	todayPath := brain.TaskFilePath(time.Now())

	switch action {
	case "list":
		bf, err := s.brain.Load(todayPath)
		if err != nil {
			return mcplib.NewToolResultText("No tasks for today."), nil
		}
		items := brain.ParseTasks(bf.Content)
		if len(items) == 0 {
			return mcplib.NewToolResultText("No tasks for today."), nil
		}
		var sb strings.Builder
		done := 0
		for _, t := range items {
			if t.Done {
				done++
			}
		}
		fmt.Fprintf(&sb, "Tasks (%d/%d done):\n", done, len(items))
		for i, t := range items {
			mark := "○"
			if t.Done {
				mark = "✓"
			}
			fmt.Fprintf(&sb, "[%d] %s %s\n", i, mark, t.Text)
		}
		return mcplib.NewToolResultText(strings.TrimSpace(sb.String())), nil

	case "add":
		text, err := req.RequireString("text")
		if err != nil || text == "" {
			return mcplib.NewToolResultError("text is required for 'add'"), nil
		}
		priority, _ := req.RequireString("priority")
		bf, loadErr := s.brain.Load(todayPath)
		if loadErr != nil {
			bf = brain.NewTaskFile(time.Now())
		}
		bf.Content = brain.AppendTask(bf.Content, text, priority)
		bf.Updated = brain.DateOnly{Time: time.Now()}
		bf.WrittenBy = events.SourceMCP
		if err := s.brain.Save(bf); err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("save tasks: %v", err)), nil
		}
		s.triggerReindex()
		return mcplib.NewToolResultText(fmt.Sprintf("Added: %s", text)), nil

	case "done", "undone":
		idxF, err := req.RequireFloat("index")
		if err != nil {
			return mcplib.NewToolResultError("index is required for 'done'/'undone'"), nil
		}
		idx := int(idxF)
		bf, err := s.brain.Load(todayPath)
		if err != nil {
			return mcplib.NewToolResultError("no tasks for today"), nil
		}
		newContent, err := brain.ToggleTask(bf.Content, idx, action == "done")
		if err != nil {
			return mcplib.NewToolResultError(err.Error()), nil
		}
		bf.Content = newContent
		bf.Updated = brain.DateOnly{Time: time.Now()}
		bf.WrittenBy = events.SourceMCP
		if err := s.brain.Save(bf); err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("save tasks: %v", err)), nil
		}
		verb := "Done"
		if action == "undone" {
			verb = "Undone"
		}
		return mcplib.NewToolResultText(fmt.Sprintf("%s: task [%d]", verb, idx)), nil

	case "rm":
		idxF, err := req.RequireFloat("index")
		if err != nil {
			return mcplib.NewToolResultError("index is required for 'rm'"), nil
		}
		idx := int(idxF)
		bf, err := s.brain.Load(todayPath)
		if err != nil {
			return mcplib.NewToolResultError("no tasks for today"), nil
		}
		newContent, err := brain.RemoveTask(bf.Content, idx)
		if err != nil {
			return mcplib.NewToolResultError(err.Error()), nil
		}
		bf.Content = newContent
		bf.Updated = brain.DateOnly{Time: time.Now()}
		bf.WrittenBy = events.SourceMCP
		if err := s.brain.Save(bf); err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("save tasks: %v", err)), nil
		}
		s.triggerReindex()
		return mcplib.NewToolResultText(fmt.Sprintf("Removed task [%d]", idx)), nil

	default:
		return mcplib.NewToolResultError(fmt.Sprintf("unknown action %q — use: list, add, done, undone, rm", action)), nil
	}
}
