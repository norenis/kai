package retriever

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/norenis/kai/internal/brain"
	"github.com/norenis/kai/internal/events"
)

var stopWords = map[string]bool{
	"the": true, "is": true, "a": true, "an": true, "and": true,
	"or": true, "but": true, "in": true, "on": true, "at": true,
	"to": true, "for": true, "of": true, "with": true, "by": true,
	"it": true, "this": true, "that": true, "are": true, "was": true,
	"be": true, "have": true, "has": true, "had": true, "do": true,
	"does": true, "did": true, "will": true, "would": true, "could": true,
	"should": true, "may": true, "might": true, "can": true, "i": true,
	"me": true, "my": true, "we": true, "you": true, "your": true,
	"what": true, "how": true, "why": true, "when": true, "where": true,
	"which": true, "who": true, "about": true, "think": true,
}

// MaxChunksPerFile prevents one file from dominating the results.
const MaxChunksPerFile = 2

// FileRetriever retrieves brain files by keyword/tag matching.
type FileRetriever struct {
	brain           *brain.FileBrain
	maxContextFiles int
}

// New creates a FileRetriever.
func New(b *brain.FileBrain, maxContextFiles int) *FileRetriever {
	return &FileRetriever{brain: b, maxContextFiles: maxContextFiles}
}

// Retrieve finds brain chunks relevant to the query using BM25 + PRF.
// Returns synthetic BrainFile entries where Content contains only the
// matched chunk, significantly reducing token count in the context.
func (r *FileRetriever) Retrieve(ctx context.Context, query string, limit int) ([]*brain.BrainFile, error) {
	files, _, err := r.RetrieveTraced(ctx, query, limit)
	return files, err
}

// RetrieveTraced is Retrieve with timings for the ask trace: "embed" (the
// query vector) and "recall" (search, fusion and loading the chunks; its
// count is the number of context entries returned).
func (r *FileRetriever) RetrieveTraced(ctx context.Context, query string, limit int) ([]*brain.BrainFile, *events.Trace, error) {
	start := time.Now()
	if limit > r.maxContextFiles {
		limit = r.maxContextFiles
	}

	// Always include profile as first result.
	var results []*brain.BrainFile
	profile, err := r.brain.Load("identity/profile.md")
	if err == nil {
		results = append(results, profile)
	}

	seen := map[string]bool{"identity/profile.md": true}

	// Try chunk-based BM25+PRF search first.
	type scoredChunk struct {
		bf    *brain.BrainFile
		score float64
	}
	var candidates []scoredChunk

	chunkResults, search := r.brain.QueryHybridTraced(query)
	if len(chunkResults) > 0 {
		// Track chunks per file to cap at MaxChunksPerFile.
		fileChunkCount := make(map[string]int)

		for _, cr := range chunkResults {
			// The vault is PIN-gated and never reaches an LLM prompt.
			if seen[cr.DocPath] || brain.IsVaultPath(cr.DocPath) {
				continue
			}
			if fileChunkCount[cr.DocPath] >= MaxChunksPerFile {
				continue
			}

			// Load the parent file and extract only the chunk text.
			parent, err := r.brain.Load(cr.DocPath)
			if err != nil {
				continue
			}

			content := strings.TrimSpace(parent.Content)
			chunkText := extractChunkText(content, cr.Offset, cr.Length)

			displayPath := cr.DocPath
			if cr.ChunkID > 0 {
				displayPath = fmt.Sprintf("%s#%d", cr.DocPath, cr.ChunkID)
			}

			chunk := &brain.BrainFile{
				Path:       displayPath,
				Tags:       parent.Tags,
				Confidence: parent.Confidence,
				Source:     parent.Source,
				Updated:    parent.Updated,
				Content:    chunkText,
			}

			finalScore := brain.RelevanceScore(parent, cr.Score)
			candidates = append(candidates, scoredChunk{bf: chunk, score: finalScore})
			fileChunkCount[cr.DocPath]++
		}
	} else {
		// Fall back to tag-based search.
		keywords := extractKeywords(query)
		tagResults := r.brain.QueryIndex(keywords)
		for _, tr := range tagResults {
			if seen[tr.Path] || brain.IsVaultPath(tr.Path) {
				continue
			}
			bf, err := r.brain.Load(tr.Path)
			if err != nil {
				continue
			}
			finalScore := brain.RelevanceScore(bf, float64(tr.Score))
			candidates = append(candidates, scoredChunk{bf: bf, score: finalScore})
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	for _, c := range candidates {
		if len(results) >= limit {
			break
		}
		// Deduplicate by doc path (strip chunk suffix).
		docPath := c.bf.Path
		if idx := strings.Index(docPath, "#"); idx > 0 {
			docPath = docPath[:idx]
		}
		// Allow multiple chunks from the same file (already capped above).
		results = append(results, c.bf)
	}

	// Touch parent files to track access.
	touched := make(map[string]bool)
	for _, bf := range results {
		docPath := bf.Path
		if idx := strings.Index(docPath, "#"); idx > 0 {
			docPath = docPath[:idx]
		}
		if !touched[docPath] {
			_ = r.brain.Touch(docPath)
			touched[docPath] = true
		}
	}

	embedMS, embedDims := 0.0, 0
	if search != nil && len(search.Stages) > 0 && search.Stages[0].Name == "embed" {
		embedMS, embedDims = search.Stages[0].MS, search.Stages[0].Count
	}
	total := float64(time.Since(start).Microseconds()) / 1000
	trace := &events.Trace{
		Stages: []events.Stage{
			{Name: "embed", MS: embedMS, Count: embedDims},
			{Name: "recall", MS: max(0, total-embedMS), Count: len(results)},
		},
		TotalMS: total,
	}
	return results, trace, nil
}

// extractChunkText safely extracts a substring from content by offset and length.
func extractChunkText(content string, offset, length int) string {
	if offset >= len(content) {
		return content
	}
	end := offset + length
	if end > len(content) {
		end = len(content)
	}
	return strings.TrimSpace(content[offset:end])
}

func extractKeywords(query string) []string {
	words := strings.Fields(strings.ToLower(query))
	var keywords []string
	for _, w := range words {
		w = strings.Trim(w, "?!.,;:'\"")
		if w == "" || stopWords[w] {
			continue
		}
		keywords = append(keywords, w)
	}
	return keywords
}
