package brain

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// VecIndexFileName is the on-disk file for the vector index.
const VecIndexFileName = "vectors.bin.gz"

// Concurrent embedding worker count.
const EmbedWorkers = 12

// embedProgressInterval controls how often progress is logged (every N chunks).
const embedProgressInterval = 50

// VecIndex stores chunk embeddings and metadata for query embedding.
type VecIndex struct {
	Dims      int                  `json:"dims"`
	Provider  string               `json:"provider"`         // "ollama" or "lsi"
	TermVecs  map[string][]float32 `json:"tv,omitempty"`     // stem → embedding (LSI only)
	IDF       map[string]float64   `json:"idf,omitempty"`    // stem → IDF weight (LSI only)
	ChunkVecs map[string][]float32 `json:"cv"`               // chunkKey → embedding
	Hashes    map[string]string    `json:"h,omitempty"`      // chunkKey → content hash (for incremental)
}

// VecResult holds a chunk key and its cosine similarity score.
type VecResult struct {
	ChunkKey string
	Score    float64
}

// contentHash returns a short SHA-256 hex hash of the text.
func contentHash(text string) string {
	h := sha256.Sum256([]byte(text))
	return fmt.Sprintf("%x", h[:12]) // 24 hex chars, collision-safe for 5K chunks
}

// buildVecIndexLSI builds the vector index using LSI embeddings.
func buildVecIndexLSI(idx *FullIndex) *VecIndex {
	model := BuildLSI(idx)
	if model == nil {
		return nil
	}

	chunkVecs := make(map[string][]float32, idx.TotalChunks)

	// Build chunk term frequencies from the inverted index.
	chunkTerms := make(map[string]map[string]int)
	for term, postings := range idx.InvertedIndex {
		for _, p := range postings {
			if p.Field != "content" || p.ChunkKey == "" {
				continue
			}
			if _, ok := chunkTerms[p.ChunkKey]; !ok {
				chunkTerms[p.ChunkKey] = make(map[string]int)
			}
			chunkTerms[p.ChunkKey][term] = p.Frequency
		}
	}

	for ck, terms := range chunkTerms {
		vec := make([]float64, model.Dims)
		var totalWeight float64

		for term, freq := range terms {
			tv, ok := model.TermVecs[term]
			if !ok {
				continue
			}
			w := math.Log(1.0+float64(freq)) * model.IDF[term]
			for i, v := range tv {
				vec[i] += float64(v) * w
			}
			totalWeight += w
		}

		if totalWeight == 0 {
			continue
		}

		embedding := make([]float32, model.Dims)
		for i := range embedding {
			embedding[i] = float32(vec[i] / totalWeight)
		}
		normalizeF32(embedding)
		chunkVecs[ck] = embedding
	}

	return &VecIndex{
		Dims:      model.Dims,
		Provider:  "lsi",
		TermVecs:  model.TermVecs,
		IDF:       model.IDF,
		ChunkVecs: chunkVecs,
	}
}

// embedJob is a unit of work for the concurrent embedding pool.
type embedJob struct {
	chunkKey string
	text     string
}

// embedResult is the output of a single embedding job.
type embedResult struct {
	chunkKey string
	vec      []float32
	hash     string
}

// buildVecIndexOllama builds the vector index using Ollama embeddings.
// chunkTexts is a pre-loaded map of chunkKey → text (avoids file I/O without lock).
// Supports incremental mode: reuses vectors for unchanged chunks from existing index.
// Respects ctx cancellation/timeout — saves partial results if time runs out.
func buildVecIndexNeural(ctx context.Context, provider string, embedder Embedder, existing *VecIndex, chunkTexts map[string]string) *VecIndex {
	var toEmbed []embedJob
	reused := make(map[string][]float32)
	hashes := make(map[string]string)
	var dims int

	existingHashes := make(map[string]string)
	existingVecs := make(map[string][]float32)
	if existing != nil && existing.Provider == provider && existing.Hashes != nil {
		existingHashes = existing.Hashes
		existingVecs = existing.ChunkVecs
		dims = existing.Dims
	}

	for chunkKey, fullText := range chunkTexts {
		if fullText == "" {
			continue
		}
		hash := contentHash(fullText)
		hashes[chunkKey] = hash

		if oldHash, ok := existingHashes[chunkKey]; ok && oldHash == hash {
			if vec, ok := existingVecs[chunkKey]; ok {
				reused[chunkKey] = vec
				continue
			}
		}
		toEmbed = append(toEmbed, embedJob{chunkKey: chunkKey, text: fullText})
	}

	var newVecs []embedResult
	if len(toEmbed) > 0 {
		newVecs = embedConcurrent(ctx, embedder, toEmbed, hashes)
	}

	chunkVecs := make(map[string][]float32, len(reused)+len(newVecs))
	maps.Copy(chunkVecs, reused)
	for _, r := range newVecs {
		chunkVecs[r.chunkKey] = r.vec
		if dims == 0 {
			dims = len(r.vec)
		}
	}

	if len(chunkVecs) == 0 {
		return nil
	}

	return &VecIndex{
		Dims:      dims,
		Provider:  provider,
		ChunkVecs: chunkVecs,
		Hashes:    hashes,
	}
}

// batchJob is a batch of embedding jobs sent as a single Ollama request.
type batchJob struct {
	jobs []embedJob
}

// embedConcurrent runs embedding jobs in batches across multiple workers.
// Batching reduces HTTP round-trips from O(chunks) to O(chunks/EmbedBatchSize).
// Workers respect ctx — if cancelled/timed out, partial results are returned and
// the incremental rebuild will fill the rest on the next run.
func embedConcurrent(ctx context.Context, embedder Embedder, jobs []embedJob, hashes map[string]string) []embedResult {
	total := len(jobs)

	// Split jobs into batches.
	var batches []batchJob
	for i := 0; i < len(jobs); i += EmbedBatchSize {
		end := min(i+EmbedBatchSize, len(jobs))
		batches = append(batches, batchJob{jobs: jobs[i:end]})
	}

	workers := min(EmbedWorkers, len(batches))

	batchCh := make(chan batchJob, len(batches))
	resultCh := make(chan embedResult, total)

	var done atomic.Int64
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for batch := range batchCh {
				select {
				case <-ctx.Done():
					return
				default:
				}
				texts := make([]string, len(batch.jobs))
				for i, j := range batch.jobs {
					texts[i] = j.text
				}
				vecs, err := embedder.EmbedBatch(texts)
				if err != nil {
					done.Add(int64(len(batch.jobs)))
					continue
				}
				for i, job := range batch.jobs {
					vec := vecs[i]
					normalizeF32(vec)
					n := done.Add(1)
					if n == 1 || n%int64(embedProgressInterval) == 0 || int(n) == total {
						slog.Info("embedding chunks", "done", n, "total", total)
					}
					resultCh <- embedResult{
						chunkKey: job.chunkKey,
						vec:      vec,
						hash:     hashes[job.chunkKey],
					}
				}
			}
		})
	}

dispatch:
	for _, batch := range batches {
		select {
		case <-ctx.Done():
			break dispatch
		case batchCh <- batch:
		}
	}
	close(batchCh)

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	var results []embedResult
	for r := range resultCh {
		results = append(results, r)
	}
	if ctx.Err() != nil {
		slog.Warn("vector index build timed out — partial index saved; remaining chunks embedded on next reindex",
			"embedded", len(results), "total", total)
	}
	return results
}

// chunkKeyPath returns the file path portion of a chunk key ("path#N" → "path").
func chunkKeyPath(ck string) string {
	if i := strings.LastIndex(ck, "#"); i >= 0 {
		return ck[:i]
	}
	return ck
}

// mergeVecIndex builds a merged VecIndex from a base and a delta.
// Vectors for paths in removePaths are excluded from base before merging.
// Delta vectors are always included (they represent newly embedded chunks).
func mergeVecIndex(base *VecIndex, delta *VecIndex, removePaths map[string]bool) *VecIndex {
	dims := delta.Dims
	provider := delta.Provider
	if dims == 0 && base != nil {
		dims = base.Dims
	}
	if provider == "" && base != nil {
		provider = base.Provider
	}

	capacity := len(delta.ChunkVecs)
	if base != nil {
		capacity += len(base.ChunkVecs)
	}
	merged := &VecIndex{
		Dims:      dims,
		Provider:  provider,
		ChunkVecs: make(map[string][]float32, capacity),
		Hashes:    make(map[string]string, capacity),
	}

	// Copy base vectors, skipping removed paths.
	if base != nil {
		for ck, vec := range base.ChunkVecs {
			if !removePaths[chunkKeyPath(ck)] {
				merged.ChunkVecs[ck] = vec
			}
		}
		for ck, h := range base.Hashes {
			if !removePaths[chunkKeyPath(ck)] {
				merged.Hashes[ck] = h
			}
		}
		// Carry forward LSI model if delta doesn't have one.
		if base.Provider == "lsi" && len(base.TermVecs) > 0 && len(delta.TermVecs) == 0 {
			merged.TermVecs = base.TermVecs
			merged.IDF = base.IDF
		}
	}

	// Overlay delta vectors (overwrite any base entries with same key).
	maps.Copy(merged.ChunkVecs, delta.ChunkVecs)
	maps.Copy(merged.Hashes, delta.Hashes)
	if len(delta.TermVecs) > 0 {
		merged.TermVecs = delta.TermVecs
		merged.IDF = delta.IDF
	}

	return merged
}

// safeSubstring extracts a substring safely by offset and length.
func safeSubstring(s string, offset, length int) string {
	if offset >= len(s) {
		return s
	}
	end := min(offset+length, len(s))
	return strings.TrimSpace(s[offset:end])
}

// QueryVec embeds a query and returns chunks ranked by cosine similarity.
// For LSI provider, uses stored term vectors. For Ollama, needs an embedder.
func (vi *VecIndex) QueryVec(query string) []VecResult {
	if vi == nil || len(vi.ChunkVecs) == 0 {
		return nil
	}

	var qVec []float32

	switch vi.Provider {
	case "ollama":
		embedder := NewOllamaEmbedder(OllamaEmbedConfig{})
		vec, err := embedder.Embed(query)
		if err != nil {
			return nil
		}
		normalizeF32(vec)
		qVec = vec

	default: // "lsi"
		qVec = vi.embedQueryLSI(query)
	}

	if qVec == nil {
		return nil
	}

	return searchBruteForce(vi.ChunkVecs, qVec)
}

// QueryVecWithEmbedder queries using a specific Ollama embedder instance.
func (vi *VecIndex) QueryVecWithEmbedder(query string, embedder Embedder) []VecResult {
	if vi == nil || len(vi.ChunkVecs) == 0 || embedder == nil {
		return nil
	}

	vec, err := embedder.Embed(query)
	if err != nil {
		return nil
	}
	normalizeF32(vec)

	return searchBruteForce(vi.ChunkVecs, vec)
}

// embedQuery returns the unit query vector: from the given embedder for
// neural indexes, from the stored term vectors for LSI. Nil when the query
// cannot be embedded (no embedder, API error, no known terms).
func (vi *VecIndex) embedQuery(query string, embedder Embedder) []float32 {
	if vi.Provider == "lsi" || vi.Provider == "" {
		return vi.embedQueryLSI(query)
	}
	if embedder == nil {
		if vi.Provider != "ollama" {
			return nil
		}
		embedder = NewOllamaEmbedder(OllamaEmbedConfig{})
	}
	vec, err := embedder.Embed(query)
	if err != nil {
		return nil
	}
	normalizeF32(vec)
	return vec
}

// searchBruteForce scans all vectors and returns sorted results.
func searchBruteForce(chunkVecs map[string][]float32, qVec []float32) []VecResult {
	results := make([]VecResult, 0, len(chunkVecs))
	for ck, cv := range chunkVecs {
		if len(cv) != len(qVec) {
			continue
		}
		score := cosineF32(qVec, cv)
		if score > 0 {
			results = append(results, VecResult{ChunkKey: ck, Score: score})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].ChunkKey < results[j].ChunkKey
	})

	return results
}

// embedQueryLSI embeds a query using stored LSI term vectors.
func (vi *VecIndex) embedQueryLSI(query string) []float32 {
	terms := tokenizeAndStem(query)
	if len(terms) == 0 {
		return nil
	}

	qVec := make([]float64, vi.Dims)
	var totalWeight float64

	termFreq := make(map[string]int)
	for _, t := range terms {
		termFreq[t]++
	}

	for term, freq := range termFreq {
		tv, ok := vi.TermVecs[term]
		if !ok {
			continue
		}
		w := math.Log(1.0+float64(freq)) * vi.IDF[term]
		for i, v := range tv {
			qVec[i] += float64(v) * w
		}
		totalWeight += w
	}

	if totalWeight == 0 {
		return nil
	}

	result := make([]float32, vi.Dims)
	for i := range result {
		result[i] = float32(qVec[i] / totalWeight)
	}
	normalizeF32(result)
	return result
}

// saveVecIndex writes the vector index to disk as gzip-compressed JSON.
func (b *FileBrain) saveVecIndex(vi *VecIndex) error {
	if vi == nil {
		return nil
	}

	path := filepath.Join(b.root, VecIndexFileName)
	data, err := json.Marshal(vi)
	if err != nil {
		return fmt.Errorf("marshal vec index: %w", err)
	}

	tmpPath := path + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("create vec index tmp: %w", err)
	}
	defer f.Close()

	gz, err := gzip.NewWriterLevel(f, gzip.DefaultCompression)
	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("create gzip writer: %w", err)
	}

	if _, err := gz.Write(data); err != nil {
		gz.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write vec index: %w", err)
	}
	if err := gz.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close gzip writer: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close vec index file: %w", err)
	}

	return os.Rename(tmpPath, path)
}

// loadVecIndex reads the vector index from disk.
func (b *FileBrain) loadVecIndex() (*VecIndex, error) {
	path := filepath.Join(b.root, VecIndexFileName)
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open vec index: %w", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("create gzip reader: %w", err)
	}
	defer gz.Close()

	var vi VecIndex
	if err := json.NewDecoder(gz).Decode(&vi); err != nil {
		return nil, fmt.Errorf("decode vec index: %w", err)
	}

	return &vi, nil
}

// LoadVecIndex reads the vector index (public, read-locked).
func (b *FileBrain) LoadVecIndex() (*VecIndex, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.loadVecIndex()
}

// normalizeF32 normalizes a float32 vector to unit length in-place.
func normalizeF32(v []float32) {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	norm := math.Sqrt(sum)
	if norm < 1e-10 {
		return
	}
	for i := range v {
		v[i] = float32(float64(v[i]) / norm)
	}
}

// cosineF32 computes cosine similarity between two unit vectors.
func cosineF32(a, b []float32) float64 {
	var sum float64
	for i := range a {
		sum += float64(a[i]) * float64(b[i])
	}
	return sum
}
