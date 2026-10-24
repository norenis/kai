package brain

import (
	"fmt"
	"sort"
	"time"

	"github.com/norenis/kai/internal/events"
)

// tracer collects stage timings; a nil tracer records nothing.
type tracer struct {
	start  time.Time
	stages []events.Stage
}

func newTracer() *tracer { return &tracer{start: time.Now()} }

func (t *tracer) stage(name string, since time.Time, count int) {
	if t == nil {
		return
	}
	ms := float64(time.Since(since).Microseconds()) / 1000
	t.stages = append(t.stages, events.Stage{Name: name, MS: ms, Count: count})
}

func (t *tracer) trace() *events.Trace {
	if t == nil {
		return nil
	}
	return &events.Trace{Stages: t.stages, TotalMS: float64(time.Since(t.start).Microseconds()) / 1000}
}

// QueryHybridTraced is QueryHybrid with timings for its stages, in the
// order they run: embed (query vector), bm25 (first pass), vector
// (similarity scan), prf (expanded second pass), fuse (rank fusion).
func (b *FileBrain) QueryHybridTraced(query string) ([]ChunkResult, *events.Trace) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	t := newTracer()
	return b.queryHybridTraced(query, t), t.trace()
}

func (b *FileBrain) queryHybridTraced(query string, t *tracer) []ChunkResult {
	idx, err := b.loadFullIndex()
	if err != nil {
		return nil
	}

	// embed: the query vector (an API call for neural providers).
	since := time.Now()
	vi, _ := b.loadVecIndex()
	var qVec []float32
	if vi != nil && len(vi.ChunkVecs) > 0 {
		qVec = vi.embedQuery(query, b.queryEmbedder(vi.Provider))
	}
	dims := 0
	if qVec != nil {
		dims = len(qVec)
	}
	t.stage("embed", since, dims)

	since = time.Now()
	terms := tokenizeAndStem(query)
	firstPass := queryChunksBM25(idx, terms)
	t.stage("bm25", since, len(firstPass))

	since = time.Now()
	var vecResults []VecResult
	if qVec != nil {
		vecResults = searchBruteForce(vi.ChunkVecs, qVec)
	}
	t.stage("vector", since, len(vecResults))

	since = time.Now()
	bm25Results := prfExpand(idx, terms, firstPass)
	t.stage("prf", since, len(bm25Results))

	since = time.Now()
	results := fuseRRF(idx, bm25Results, vecResults)
	t.stage("fuse", since, len(results))
	return results
}

// fuseRRF merges BM25 and vector rankings by Reciprocal Rank Fusion:
// score = 1/(k+rank_bm25) + 1/(k+rank_vec). Without vector results the
// BM25 ranking is returned unchanged.
func fuseRRF(idx *FullIndex, bm25Results []ChunkResult, vecResults []VecResult) []ChunkResult {
	if len(vecResults) == 0 {
		return bm25Results
	}
	rrfScores := make(map[string]float64)
	for rank, cr := range bm25Results {
		key := fmt.Sprintf("%s#%d", cr.DocPath, cr.ChunkID)
		rrfScores[key] += 1.0 / float64(RRFK+rank+1)
	}
	for rank, vr := range vecResults {
		rrfScores[vr.ChunkKey] += 1.0 / float64(RRFK+rank+1)
	}

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
	sort.Slice(results, func(i, j int) bool { return chunkBefore(results[i], results[j]) })
	return results
}
