package brain

import (
	"fmt"
	"sort"
)

// Hybrid search parameters.
const (
	// RRF constant (Reciprocal Rank Fusion). Higher = more even blending.
	RRFK = 60
)

// HybridResult holds a chunk reference with combined BM25 + vector score.
type HybridResult struct {
	DocPath string
	ChunkID int
	Score   float64
	Offset  int
	Length  int
}

// QueryHybrid runs BM25+PRF and vector search, fusing results via RRF.
// Falls back to BM25-only if vector index is unavailable.
func (b *FileBrain) QueryHybrid(query string) []ChunkResult {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.queryHybrid(query)
}

func (b *FileBrain) queryHybrid(query string) []ChunkResult {
	// BM25+PRF results.
	bm25Results := b.queryWithPRF(query)

	// Vector results.
	vi, err := b.loadVecIndex()
	if err != nil || vi == nil {
		return bm25Results // Fall back to BM25-only.
	}

	var vecResults []VecResult
	if e := b.queryEmbedder(vi.Provider); e != nil {
		vecResults = vi.QueryVecWithEmbedder(query, e)
	} else {
		vecResults = vi.QueryVec(query)
	}
	if len(vecResults) == 0 {
		return bm25Results
	}

	// Load full index for chunk metadata lookup.
	idx, err := b.loadFullIndex()
	if err != nil {
		return bm25Results
	}

	// Reciprocal Rank Fusion: score = 1/(k+rank_bm25) + 1/(k+rank_vec)
	rrfScores := make(map[string]float64)

	for rank, cr := range bm25Results {
		key := fmt.Sprintf("%s#%d", cr.DocPath, cr.ChunkID)
		rrfScores[key] += 1.0 / float64(RRFK+rank+1)
	}

	for rank, vr := range vecResults {
		rrfScores[vr.ChunkKey] += 1.0 / float64(RRFK+rank+1)
	}

	// Build results with chunk metadata.
	results := make([]ChunkResult, 0, len(rrfScores))
	for ck, score := range rrfScores {
		ce, ok := idx.Chunks[ck]
		if !ok {
			continue
		}
		results = append(results, ChunkResult{
			DocPath: ce.DocPath,
			ChunkID: ce.ChunkID,
			Score:   score,
			Offset:  ce.Offset,
			Length:  ce.Length,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	return results
}
