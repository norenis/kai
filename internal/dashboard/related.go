package dashboard

import (
	"math"
	"net/http"
	"sort"

	"github.com/norenis/kai/internal/brain"
)

// maxRelated is how many notes "Nearest in the sky" lists.
const maxRelated = 5

type relatedNote struct {
	Path    string  `json:"path"`
	Title   string  `json:"title"`
	Cluster int     `json:"cluster"` // constellation holding most of the note's chunks, -1 when unclustered
	Score   float64 `json:"score"`
}

// handleRelated lists the notes nearest to one note: each of its chunks links
// to its topNeighbors most similar chunks of other notes (similarity at least
// minSimilarity, as in the Sky), and a note's score is the summed strength of
// those links.
func (s *Server) handleRelated(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "missing path parameter", http.StatusBadRequest)
		return
	}
	// Similar notes reveal what a vault note is about, so it needs the PIN too.
	if brain.IsVaultPath(path) && !s.vaultPinOK(r.Header.Get(vaultPinHeader)) {
		http.Error(w, `{"error":"vault pin required"}`, http.StatusForbidden)
		return
	}

	type response struct {
		Related []relatedNote `json:"related"`
	}
	vi, err := s.brain.LoadVecIndexCached()
	if err != nil || vi == nil {
		writeJSON(w, response{Related: []relatedNote{}})
		return
	}
	idx, _ := s.brain.LoadFullIndexCached()
	var assign map[string]int
	if cs, err := s.brain.Clusters(); err == nil && cs != nil {
		assign = cs.Assign
	}
	writeJSON(w, response{Related: relatedNotes(vi.ChunkVecs, path, idx, assign, maxRelated)})
}

// relatedNotes scores the other notes against path's chunks; vault notes and
// notes no longer in the index are never listed.
func relatedNotes(vecs map[string][]float32, path string, idx *brain.FullIndex, assign map[string]int, limit int) []relatedNote {
	var own, others []vecEntry
	for k, v := range vecs {
		doc := chunkKeyDoc(k)
		switch {
		case doc == path:
			own = append(own, vecEntry{key: k, vec: v})
		case brain.IsVaultPath(doc):
		case idx != nil && !hasDoc(idx, doc):
		default:
			others = append(others, vecEntry{key: k, vec: v})
		}
	}

	type link struct {
		doc   string
		score float64
	}
	scores := map[string]float64{}
	for _, c := range own {
		var best []link
		for _, o := range others {
			sc := cosSim32(c.vec, o.vec)
			if sc < minSimilarity {
				continue
			}
			best = append(best, link{chunkKeyDoc(o.key), sc})
		}
		sort.Slice(best, func(i, j int) bool { return best[i].score > best[j].score })
		if len(best) > topNeighbors {
			best = best[:topNeighbors]
		}
		for _, l := range best {
			scores[l.doc] += l.score
		}
	}

	out := make([]relatedNote, 0, len(scores))
	for doc, sc := range scores {
		out = append(out, relatedNote{Path: doc, Score: math.Round(sc*1000) / 1000})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Path < out[j].Path
	})
	if len(out) > limit {
		out = out[:limit]
	}
	for i := range out {
		out[i].Title = brain.FilenameTitle(out[i].Path)
		if idx != nil {
			out[i].Title = idx.Documents[out[i].Path].DisplayTitle()
		}
		out[i].Cluster = noteCluster(out[i].Path, vecs, assign)
	}
	return out
}

func hasDoc(idx *brain.FullIndex, doc string) bool {
	_, ok := idx.Documents[doc]
	return ok
}

// noteCluster is the constellation holding most of a note's chunks (ties go
// to the lower id), or -1.
func noteCluster(doc string, vecs map[string][]float32, assign map[string]int) int {
	counts := map[int]int{}
	for k := range vecs {
		if chunkKeyDoc(k) != doc {
			continue
		}
		if c, ok := assign[k]; ok {
			counts[c]++
		}
	}
	best, bestN := -1, 0
	for c, n := range counts {
		if n > bestN || (n == bestN && c < best) {
			best, bestN = c, n
		}
	}
	return best
}
