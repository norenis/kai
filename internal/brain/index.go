package brain

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// VecIndexBuildTimeout is the maximum time allowed for the vector index build.
// If Ollama is slow, we save partial vectors and release the lock rather than
// blocking brain operations indefinitely.
const VecIndexBuildTimeout = 5 * time.Minute

// Index file names.
const (
	IndexFileName     = "index.json"
	FullIndexFileName = "index_full.json.gz"
)

// BM25 parameters.
const (
	BM25K1 = 1.5 // Term frequency saturation. Higher = more weight to repeated terms.
	BM25B  = 0.5 // Length normalization. Lower than standard 0.75 — brain files vary
	             // widely in length and short concise files should rank fairly against
	             // long consolidated ones.
)

// Field boost weights applied on top of BM25 scores.
const (
	FieldBoostTag     = 3.0
	FieldBoostPath    = 2.0
	FieldBoostContent = 1.0
)

// PRF (Pseudo-Relevance Feedback) parameters.
const (
	PRFTopK        = 3 // Number of top chunks used for feedback.
	PRFExpandTerms = 5 // Number of expansion terms to add.
	PRFMinScore    = 1.0 // Minimum top-chunk score to trigger PRF.
)

// RebuildIndex scans all brain files and rebuilds both the tag index and the
// full BM25+vector index. Use this at startup or when a full reindex is needed.
func (b *FileBrain) RebuildIndex() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.rebuildIndex()
}

// RebuildIndexIncremental performs a file-level differential rebuild.
// Only files whose content hash has changed since the last index are re-processed.
// Falls back to a full rebuild when the manifest is absent or more than half the
// files have changed.
func (b *FileBrain) RebuildIndexIncremental() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.rebuildIndexIncremental()
}

// RebuildTagIndex rebuilds only the lightweight tag index (index.json).
// Use this after writes — it is fast and does not call Ollama.
func (b *FileBrain) RebuildTagIndex() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.rebuildTagIndex()
}

// rebuildIndex is the internal unlocked implementation of RebuildIndex.
func (b *FileBrain) rebuildIndex() error {
	// Prime a shared file cache so each brain file is read from disk only once
	// across the tag, BM25, and vector index phases.
	b.fileCache = make(map[string]*BrainFile)
	defer func() { b.fileCache = nil }()

	if err := b.rebuildTagIndex(); err != nil {
		return err
	}
	if err := b.rebuildFullIndex(); err != nil {
		return err
	}

	// Save manifest so the next run can use incremental mode.
	files, err := b.listAll()
	if err == nil && b.indexCache != nil {
		chunkCounts := make(map[string]int, len(b.indexCache.Documents))
		for path, doc := range b.indexCache.Documents {
			chunkCounts[path] = doc.ChunkCount
		}
		if err := b.buildAndSaveManifest(files, chunkCounts); err != nil {
			slog.Debug("failed to save incremental manifest after full rebuild", "err", err)
		}
	}
	return nil
}

// rebuildTagIndex rebuilds only index.json (tag-based lookup). Fast, no Ollama.
func (b *FileBrain) rebuildTagIndex() error {
	idx := &Index{Tags: make(map[string][]string)}

	files, err := b.listAll()
	if err != nil {
		return fmt.Errorf("list brain files: %w", err)
	}
	slog.Debug("rebuilding tag index", "files", len(files))

	for _, f := range files {
		bf, err := b.loadCached(f)
		if err != nil {
			slog.Debug("tag index: skipping unreadable file", "path", f, "err", err)
			continue
		}
		for _, tag := range bf.Tags {
			idx.Tags[tag] = append(idx.Tags[tag], f)
		}
	}

	slog.Debug("tag index rebuilt", "files", len(files), "unique_tags", len(idx.Tags))
	return b.saveIndex(idx)
}

// LoadIndex reads the index.json file.
func (b *FileBrain) LoadIndex() (*Index, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.loadIndex()
}

// loadIndex is the internal unlocked implementation of LoadIndex.
func (b *FileBrain) loadIndex() (*Index, error) {
	path := filepath.Join(b.root, IndexFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read index: %w", err)
	}

	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("parse index: %w", err)
	}
	return &idx, nil
}

func (b *FileBrain) saveIndex(idx *Index) error {
	path := filepath.Join(b.root, IndexFileName)
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal index: %w", err)
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("write index tmp: %w", err)
	}
	return os.Rename(tmpPath, path)
}

// IndexResult holds a brain file path and its match score.
type IndexResult struct {
	Path  string
	Score int
}

// QueryIndex returns brain file paths ranked by number of matching tags.
func (b *FileBrain) QueryIndex(keywords []string) []IndexResult {
	b.mu.RLock()
	defer b.mu.RUnlock()

	idx, err := b.loadIndex()
	if err != nil {
		return nil
	}

	scores := make(map[string]int)
	for _, kw := range keywords {
		if paths, ok := idx.Tags[kw]; ok {
			for _, p := range paths {
				scores[p]++
			}
		}
	}

	results := make([]IndexResult, 0, len(scores))
	for path, score := range scores {
		results = append(results, IndexResult{Path: path, Score: score})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	return results
}

// RebuildFullIndex builds the full-text inverted index (public, write-locked).
func (b *FileBrain) RebuildFullIndex() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.rebuildFullIndex()
}

// rebuildFullIndex is the internal unlocked implementation.
func (b *FileBrain) rebuildFullIndex() error {
	files, err := b.listAll()
	if err != nil {
		return fmt.Errorf("list brain files: %w", err)
	}
	slog.Debug("rebuilding full BM25 index", "files", len(files))

	idx := &FullIndex{
		Documents:     make(map[string]DocEntry),
		Chunks:        make(map[string]ChunkEntry),
		InvertedIndex: make(map[string][]Posting),
	}

	for _, f := range files {
		bf, err := b.loadCached(f)
		if err != nil {
			continue
		}
		indexFileIntoBM25(idx, f, bf)
	}

	recomputeAverages(idx)

	slog.Debug("full BM25 index built", "docs", idx.TotalDocs, "chunks", idx.TotalChunks)
	if err := b.saveFullIndex(idx); err != nil {
		return err
	}
	// Warm the full-index cache so the next read avoids disk I/O.
	b.indexCache = idx
	b.indexCacheAt = time.Now()

	// Snapshot chunk texts while the file cache is warm and lock is held.
	// This lets us release the write lock before the slow Ollama API calls.
	chunkTexts := b.snapshotChunkTexts(idx, nil)

	// Release write lock so brain reads (dashboard, queries) are not blocked
	// during the potentially long vector embedding step.
	slog.Debug("building vector index", "provider", b.embed.Provider)
	b.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), VecIndexBuildTimeout)
	vi := b.buildVecIndexUnlocked(ctx, idx, chunkTexts)
	cancel()
	b.mu.Lock() // Re-acquire before modifying cached state

	if vi != nil {
		slog.Debug("saving vector index", "chunks", len(vi.ChunkVecs))
		if err := b.saveVecIndex(vi); err != nil {
			return fmt.Errorf("save vec index: %w", err)
		}
		b.vecCache = vi
		b.vecCacheAt = time.Now()
	} else {
		slog.Debug("vector index skipped (no embedder available)")
	}

	return nil
}

// snapshotChunkTexts pre-extracts chunk text strings while the file cache is
// populated and the lock is held, so that no file I/O is needed after the
// write lock is released. If paths is non-nil, only chunks whose DocPath is
// in paths are included; nil means all chunks.
func (b *FileBrain) snapshotChunkTexts(idx *FullIndex, paths []string) map[string]string {
	var pathSet map[string]bool
	if paths != nil {
		pathSet = make(map[string]bool, len(paths))
		for _, p := range paths {
			pathSet[p] = true
		}
	}

	texts := make(map[string]string, len(idx.Chunks))
	for chunkKey, ce := range idx.Chunks {
		if pathSet != nil && !pathSet[ce.DocPath] {
			continue
		}
		bf, err := b.loadCached(ce.DocPath)
		if err != nil {
			continue
		}
		content := strings.TrimSpace(bf.Content)
		chunkText := safeSubstring(content, ce.Offset, ce.Length)
		if chunkText == "" {
			continue
		}
		doc := idx.Documents[ce.DocPath]
		prefix := ""
		if len(doc.Tags) > 0 {
			prefix = "Tags: " + strings.Join(doc.Tags, ", ") + ". "
		}
		texts[chunkKey] = prefix + chunkText
	}
	return texts
}

// buildVecIndexUnlocked builds the vector index without holding the write lock.
// chunkTexts is a pre-loaded snapshot so no brain file I/O is needed.
func (b *FileBrain) buildVecIndexUnlocked(ctx context.Context, idx *FullIndex, chunkTexts map[string]string) *VecIndex {
	switch b.embed.Provider {
	case EmbedOllama:
		return b.tryOllamaEmbedUnlocked(ctx, chunkTexts)
	case EmbedGemini:
		return b.tryGeminiEmbedUnlocked(ctx, chunkTexts)
	case EmbedLSI:
		return buildVecIndexLSI(idx)
	default: // Auto: try Ollama, fall back to LSI.
		if vi := b.tryOllamaEmbedUnlocked(ctx, chunkTexts); vi != nil {
			return vi
		}
		return buildVecIndexLSI(idx)
	}
}

// tryOllamaEmbedUnlocked attempts Ollama embedding without holding the write lock.
func (b *FileBrain) tryOllamaEmbedUnlocked(ctx context.Context, chunkTexts map[string]string) *VecIndex {
	embedder := NewOllamaEmbedder(OllamaEmbedConfig{
		URL:   b.embed.OllamaURL,
		Model: b.embed.OllamaModel,
	})
	slog.Debug("checking ollama embedder availability", "url", b.embed.OllamaURL, "model", b.embed.OllamaModel)
	if !embedder.Available() {
		slog.Debug("ollama embedder not available, skipping vector index")
		return nil
	}

	// Load existing index for incremental rebuild (brief read lock).
	b.mu.RLock()
	existing, _ := b.loadVecIndex()
	b.mu.RUnlock()

	slog.Debug("ollama embedder available, embedding chunks", "chunks", len(chunkTexts))
	return buildVecIndexNeural(ctx, "ollama", embedder, existing, chunkTexts)
}


// tryGeminiEmbedUnlocked attempts Gemini embedding without holding the write lock.
func (b *FileBrain) tryGeminiEmbedUnlocked(ctx context.Context, chunkTexts map[string]string) *VecIndex {
	embedder := NewGeminiEmbedder(GeminiEmbedConfig{
		BaseURL: b.embed.GeminiBaseURL,
		Model:   b.embed.GeminiModel,
		APIKey:  b.embed.GeminiAPIKey,
	})
	slog.Debug("checking gemini embedder availability", "model", b.embed.GeminiModel)
	if !embedder.Available() {
		slog.Debug("gemini embedder not available (missing key or unreachable), skipping vector index")
		return nil
	}

	b.mu.RLock()
	existing, _ := b.loadVecIndex()
	b.mu.RUnlock()

	slog.Debug("gemini embedder available, embedding chunks", "chunks", len(chunkTexts))
	return buildVecIndexNeural(ctx, "gemini", embedder, existing, chunkTexts)
}

// queryEmbedder returns the neural embedder matching a vector index provider,
// configured from the brain's embed options. Returns nil for LSI (no embedder).
func (b *FileBrain) queryEmbedder(provider string) Embedder {
	switch provider {
	case "ollama":
		return NewOllamaEmbedder(OllamaEmbedConfig{URL: b.embed.OllamaURL, Model: b.embed.OllamaModel})
	case "gemini":
		return NewGeminiEmbedder(GeminiEmbedConfig{BaseURL: b.embed.GeminiBaseURL, Model: b.embed.GeminiModel, APIKey: b.embed.GeminiAPIKey})
	default:
		return nil
	}
}

// rebuildIndexIncremental is the internal implementation of RebuildIndexIncremental.
func (b *FileBrain) rebuildIndexIncremental() error {
	b.fileCache = make(map[string]*BrainFile)
	defer func() { b.fileCache = nil }()

	files, err := b.listAll()
	if err != nil {
		return fmt.Errorf("list brain files: %w", err)
	}

	manifest := b.loadManifest()

	// Diff: classify each file as unchanged, dirty (changed/new), or deleted.
	currentSet := make(map[string]bool, len(files))
	newManifest := &IndexManifest{Files: make(map[string]FileState, len(files))}
	var dirty []string

	for _, f := range files {
		currentSet[f] = true
		absPath := filepath.Join(b.root, f)
		hash, err := fileContentHash(absPath)
		if err != nil {
			continue
		}
		if prev, ok := manifest.Files[f]; ok && prev.Hash == hash {
			newManifest.Files[f] = prev // unchanged — carry forward
			continue
		}
		dirty = append(dirty, f)
		newManifest.Files[f] = FileState{Hash: hash} // chunks updated during rebuild
	}

	var deleted []string
	for f := range manifest.Files {
		if !currentSet[f] {
			deleted = append(deleted, f)
		}
	}

	totalChanged := len(dirty) + len(deleted)

	// Fall back to full rebuild when manifest is empty (first run) or more than
	// half the corpus changed (full rebuild is cheaper than delta bookkeeping).
	if len(manifest.Files) == 0 || totalChanged > len(files)/2 {
		slog.Info("incremental index: falling back to full rebuild",
			"changed", totalChanged, "total", len(files))
		return b.rebuildIndex()
	}

	if totalChanged == 0 {
		slog.Info("incremental index: nothing changed")
		return nil
	}

	slog.Info("incremental index: processing changed files",
		"dirty", len(dirty), "deleted", len(deleted), "unchanged", len(files)-len(dirty))

	// Tag index is cheap — always rebuild it.
	if err := b.rebuildTagIndex(); err != nil {
		return err
	}

	// Incremental BM25 update.
	if err := b.updateFullIndexIncremental(dirty, deleted, newManifest); err != nil {
		return err
	}

	// Incremental vector update — embed only dirty chunks, merge with existing.
	idx := b.indexCache // warm from updateFullIndexIncremental
	if idx == nil {
		idx, err = b.loadFullIndex()
		if err != nil {
			return err
		}
	}
	dirtyChunkTexts := b.snapshotChunkTexts(idx, dirty)

	b.mu.RLock()
	existingVI, _ := b.loadVecIndex()
	b.mu.RUnlock()

	removePaths := make(map[string]bool, len(dirty)+len(deleted))
	for _, p := range dirty {
		removePaths[p] = true
	}
	for _, p := range deleted {
		removePaths[p] = true
	}

	b.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), VecIndexBuildTimeout)
	deltaVI := b.buildVecIndexUnlocked(ctx, idx, dirtyChunkTexts)
	cancel()
	b.mu.Lock()

	var vi *VecIndex
	if deltaVI != nil && existingVI != nil {
		vi = mergeVecIndex(existingVI, deltaVI, removePaths)
	} else if deltaVI != nil {
		vi = deltaVI
	} else if existingVI != nil {
		// No new embeddings (e.g. provider unavailable) — clean up deleted/dirty vectors.
		vi = mergeVecIndex(existingVI, &VecIndex{
			Dims:      existingVI.Dims,
			Provider:  existingVI.Provider,
			ChunkVecs: make(map[string][]float32),
			Hashes:    make(map[string]string),
		}, removePaths)
	}

	if vi != nil {
		if err := b.saveVecIndex(vi); err != nil {
			return fmt.Errorf("save vec index: %w", err)
		}
		b.vecCache = vi
		b.vecCacheAt = time.Now()
	}

	return b.saveManifest(newManifest)
}

// updateFullIndexIncremental applies a delta update to the existing BM25 full index.
// It removes all postings/entries for dirty and deleted paths, then re-indexes dirty files.
func (b *FileBrain) updateFullIndexIncremental(dirty, deleted []string, newManifest *IndexManifest) error {
	existing, err := b.loadFullIndex()
	if err != nil {
		// No existing index — rebuild from scratch.
		slog.Debug("incremental BM25: no existing index, running full rebuild")
		return b.rebuildFullIndex()
	}

	toRemove := make(map[string]bool, len(dirty)+len(deleted))
	for _, p := range dirty {
		toRemove[p] = true
	}
	for _, p := range deleted {
		toRemove[p] = true
	}

	// Remove postings for dirty/deleted paths.
	for term, postings := range existing.InvertedIndex {
		filtered := postings[:0]
		for _, p := range postings {
			if !toRemove[p.Path] {
				filtered = append(filtered, p)
			}
		}
		if len(filtered) == 0 {
			delete(existing.InvertedIndex, term)
		} else {
			existing.InvertedIndex[term] = filtered
		}
	}

	// Remove document and chunk entries.
	for path := range toRemove {
		doc, ok := existing.Documents[path]
		if !ok {
			continue
		}
		for ci := 0; ci < doc.ChunkCount; ci++ {
			delete(existing.Chunks, fmt.Sprintf("%s#%d", path, ci))
		}
		existing.TotalChunks -= doc.ChunkCount
		existing.TotalDocs--
		delete(existing.Documents, path)
	}

	// Re-index each dirty file.
	for _, f := range dirty {
		bf, err := b.loadCached(f)
		if err != nil {
			continue
		}
		chunks := indexFileIntoBM25(existing, f, bf)

		// Update manifest chunk count.
		if fs, ok := newManifest.Files[f]; ok {
			fs.Chunks = len(chunks)
			newManifest.Files[f] = fs
		}
	}

	recomputeAverages(existing)

	slog.Debug("incremental BM25 update complete",
		"docs", existing.TotalDocs, "chunks", existing.TotalChunks)

	if err := b.saveFullIndex(existing); err != nil {
		return err
	}
	b.indexCache = existing
	b.indexCacheAt = time.Now()
	return nil
}

// indexFileIntoBM25 tokenizes, chunks, and indexes a single brain file into idx,
// adding its document entry, chunk entries, and content/tag/path postings.
// Used by both full and incremental rebuilds. Returns the chunks produced.
func indexFileIntoBM25(idx *FullIndex, path string, bf *BrainFile) []Chunk {
	content := strings.TrimSpace(bf.Content)
	contentTokens := tokenizeAndStem(content)
	docWordCount := len(contentTokens)

	chunks := chunkContent(content, ChunkTargetWords)
	if len(chunks) == 0 {
		chunks = []Chunk{{Text: content, Offset: 0, Length: len(content), WordCount: docWordCount}}
	}

	idx.Documents[path] = DocEntry{
		Path:        path,
		WordCount:   docWordCount,
		Tags:        bf.Tags,
		Updated:     bf.Updated.Time.Format(DateFormat),
		AccessCount: bf.AccessCount,
		ChunkCount:  len(chunks),
		Confidence:  bf.Confidence,
	}
	idx.TotalDocs++

	for ci, ch := range chunks {
		chunkKey := fmt.Sprintf("%s#%d", path, ci)
		chunkTokens := tokenizeAndStem(ch.Text)

		idx.Chunks[chunkKey] = ChunkEntry{
			DocPath:   path,
			ChunkID:   ci,
			WordCount: len(chunkTokens),
			Offset:    ch.Offset,
			Length:    ch.Length,
		}
		idx.TotalChunks++

		freq := make(map[string]int)
		for _, t := range chunkTokens {
			freq[t]++
		}
		for term, count := range freq {
			idx.InvertedIndex[term] = append(idx.InvertedIndex[term], Posting{
				Path:      path,
				Frequency: count,
				Field:     "content",
				ChunkKey:  chunkKey,
			})
		}
	}

	indexFieldTerms(idx, path, bf.Tags, "tag")
	pathSegments := strings.Split(strings.TrimSuffix(path, ".md"), string(filepath.Separator))
	indexFieldTerms(idx, path, pathSegments, "path")

	return chunks
}

// indexFieldTerms tokenizes each item, aggregates term frequency, and adds one
// document-level posting per term (no ChunkKey) for the given field.
func indexFieldTerms(idx *FullIndex, path string, items []string, field string) {
	freq := make(map[string]int)
	for _, item := range items {
		for _, t := range tokenizeAndStem(item) {
			freq[t]++
		}
	}
	for term, count := range freq {
		idx.InvertedIndex[term] = append(idx.InvertedIndex[term], Posting{
			Path:      path,
			Frequency: count,
			Field:     field,
		})
	}
}

// recomputeAverages recalculates AvgDocLength and AvgChunkLength from the
// current Documents and Chunks maps.
func recomputeAverages(idx *FullIndex) {
	var totalDocWords, totalChunkWords int
	for _, doc := range idx.Documents {
		totalDocWords += doc.WordCount
	}
	for _, chunk := range idx.Chunks {
		totalChunkWords += chunk.WordCount
	}
	if idx.TotalDocs > 0 {
		idx.AvgDocLength = float64(totalDocWords) / float64(idx.TotalDocs)
	}
	if idx.TotalChunks > 0 {
		idx.AvgChunkLength = float64(totalChunkWords) / float64(idx.TotalChunks)
	}
}

// LoadFullIndex reads the index_full.json file (public, read-locked).
func (b *FileBrain) LoadFullIndex() (*FullIndex, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.loadFullIndex()
}

// loadFullIndex is the internal unlocked implementation.
func (b *FileBrain) loadFullIndex() (*FullIndex, error) {
	return b.loadFullIndexCompact()
}

func (b *FileBrain) saveFullIndex(idx *FullIndex) error {
	return b.saveFullIndexCompact(idx)
}

// FullIndexResult holds a brain file path and its BM25 score.
type FullIndexResult struct {
	Path  string
	Score float64
}

// QueryFullIndex searches using BM25 and returns document-level results.
// Internally aggregates chunk scores (max per doc). Backward-compatible.
func (b *FileBrain) QueryFullIndex(query string) []FullIndexResult {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.queryFullIndex(query)
}

func (b *FileBrain) queryFullIndex(query string) []FullIndexResult {
	chunks := b.queryFullIndexChunks(query)
	if len(chunks) == 0 {
		return nil
	}

	// Aggregate: keep max chunk score per document.
	docMax := make(map[string]float64)
	for _, c := range chunks {
		if c.Score > docMax[c.DocPath] {
			docMax[c.DocPath] = c.Score
		}
	}

	results := make([]FullIndexResult, 0, len(docMax))
	for path, score := range docMax {
		results = append(results, FullIndexResult{Path: path, Score: score})
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	return results
}

// QueryFullIndexChunks searches using BM25 and returns chunk-level results.
func (b *FileBrain) QueryFullIndexChunks(query string) []ChunkResult {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.queryFullIndexChunks(query)
}

func (b *FileBrain) queryFullIndexChunks(query string) []ChunkResult {
	idx, err := b.loadFullIndex()
	if err != nil {
		return nil
	}
	return queryChunksBM25(idx, tokenizeAndStem(query))
}

// queryChunksBM25 scores chunks against the given stemmed query terms.
func queryChunksBM25(idx *FullIndex, terms []string) []ChunkResult {
	if len(terms) == 0 || idx.TotalChunks == 0 {
		return nil
	}

	avgdl := idx.AvgChunkLength
	if avgdl == 0 {
		avgdl = idx.AvgDocLength
	}
	if avgdl == 0 {
		avgdl = 1
	}
	N := float64(idx.TotalDocs)

	scores := make(map[string]float64) // chunk key → score

	for _, term := range terms {
		postings, ok := idx.InvertedIndex[term]
		if !ok {
			continue
		}

		// Count unique documents for IDF.
		docSet := make(map[string]bool)
		for _, p := range postings {
			docSet[p.Path] = true
		}
		n := float64(len(docSet))
		idf := math.Log((N-n+0.5)/(n+0.5) + 1.0)

		for _, p := range postings {
			tf := float64(p.Frequency)
			boost := fieldBoost(p.Field)

			if p.ChunkKey != "" {
				// Content posting — score at chunk level.
				dl := float64(idx.Chunks[p.ChunkKey].WordCount)
				num := tf * (BM25K1 + 1)
				denom := tf + BM25K1*(1-BM25B+BM25B*dl/avgdl)
				scores[p.ChunkKey] += idf * (num / denom) * boost
			} else {
				// Tag/path posting — distribute to all chunks of this document,
				// divided by chunk count to avoid inflating multi-chunk files.
				doc := idx.Documents[p.Path]
				chunkCount := doc.ChunkCount
				if chunkCount == 0 {
					chunkCount = 1
				}
				dl := float64(doc.WordCount)
				num := tf * (BM25K1 + 1)
				denom := tf + BM25K1*(1-BM25B+BM25B*dl/avgdl)
				perChunk := idf * (num / denom) * boost / float64(chunkCount)
				for ci := 0; ci < doc.ChunkCount; ci++ {
					ck := fmt.Sprintf("%s#%d", p.Path, ci)
					scores[ck] += perChunk
				}
			}
		}
	}

	results := make([]ChunkResult, 0, len(scores))
	for ck, score := range scores {
		ce, ok := idx.Chunks[ck]
		if !ok {
			continue
		}
		results = append(results, ChunkResult{
			DocPath: ce.DocPath,
			ChunkID: ce.ChunkID,
			Score:   score,
			Offset:  ce.Offset,
			Length:   ce.Length,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	return results
}

// QueryWithPRF runs a two-pass search: BM25 → extract top-chunk terms → re-query.
func (b *FileBrain) QueryWithPRF(query string) []ChunkResult {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.queryWithPRF(query)
}

func (b *FileBrain) queryWithPRF(query string) []ChunkResult {
	idx, err := b.loadFullIndex()
	if err != nil {
		return nil
	}

	originalTerms := tokenizeAndStem(query)
	if len(originalTerms) == 0 {
		return nil
	}

	// First pass.
	firstPass := queryChunksBM25(idx, originalTerms)
	if len(firstPass) == 0 {
		return nil
	}

	// Skip PRF if top result is too weak.
	if firstPass[0].Score < PRFMinScore {
		return firstPass
	}

	topK := min(PRFTopK, len(firstPass))

	// Build a set of original terms for dedup.
	origSet := make(map[string]bool, len(originalTerms))
	for _, t := range originalTerms {
		origSet[t] = true
	}

	// Collect candidate expansion terms: terms that appear in the top-K chunks
	// with their IDF scores. We iterate the inverted index and check which terms
	// have postings in our top chunk keys.
	topChunkKeys := make(map[string]bool, topK)
	for _, cr := range firstPass[:topK] {
		topChunkKeys[fmt.Sprintf("%s#%d", cr.DocPath, cr.ChunkID)] = true
	}

	type termScore struct {
		term string
		idf  float64
	}
	var candidates []termScore
	N := float64(idx.TotalDocs)

	for term, postings := range idx.InvertedIndex {
		if origSet[term] {
			continue
		}
		// Check if this term appears in any top chunk.
		inTopChunk := false
		for _, p := range postings {
			if p.ChunkKey != "" && topChunkKeys[p.ChunkKey] {
				inTopChunk = true
				break
			}
		}
		if !inTopChunk {
			continue
		}
		// Compute IDF.
		docSet := make(map[string]bool)
		for _, p := range postings {
			docSet[p.Path] = true
		}
		n := float64(len(docSet))
		idf := math.Log((N-n+0.5)/(n+0.5) + 1.0)
		candidates = append(candidates, termScore{term: term, idf: idf})
	}

	// Pick the highest-IDF expansion terms.
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].idf > candidates[j].idf
	})
	expandCount := min(PRFExpandTerms, len(candidates))

	expandedTerms := make([]string, len(originalTerms), len(originalTerms)+expandCount)
	copy(expandedTerms, originalTerms)
	for _, c := range candidates[:expandCount] {
		expandedTerms = append(expandedTerms, c.term)
	}

	// Second pass with expanded query.
	return queryChunksBM25(idx, expandedTerms)
}

// fieldBoost returns the weight multiplier for the given field.
func fieldBoost(field string) float64 {
	switch field {
	case "tag":
		return FieldBoostTag
	case "path":
		return FieldBoostPath
	default:
		return FieldBoostContent
	}
}
