package brain

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
	return b.queryHybridTraced(query, nil)
}
