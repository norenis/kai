package dashboard

import (
	"math"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/norenis/kai/internal/brain"
)

// maxVecNodes is a safety cap on nodes in the 3D graph.
// Link-building uses a sliding-window ANN so this can be much larger than the old O(n²) limit.
const maxVecNodes = 5000

// topNeighbors is the max neighbors per node shown as links.
const topNeighbors = 4

// minSimilarity is the cosine similarity threshold for drawing a link.
const minSimilarity = 0.5

type vecNode struct {
	ID      string  `json:"id"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Z       float64 `json:"z"`
	Doc     string  `json:"doc"`
	Group   string  `json:"group"`
	Label   string  `json:"label"`
	Title   string  `json:"title"`
	Updated string  `json:"updated,omitempty"` // YYYY-MM-DD from front matter
	Views   int     `json:"views"`             // read count of the note
	Words   int     `json:"words"`             // word count of the note
	Cluster int     `json:"cluster"`           // constellation id, -1 when unclustered
}

type vecLink struct {
	Source string  `json:"source"`
	Target string  `json:"target"`
	Value  float64 `json:"value"`
}

type vecEntry struct {
	key string
	vec []float32
}

func (s *Server) handleVectorsBuild(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := s.brain.RebuildFullIndex(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := s.brain.RecomputeClusters(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

// handleReindex does what `kai reindex` does: re-index the notes changed
// since the last index, then recompute the constellations. A full rebuild
// of every embedding is /api/vectors/build.
func (s *Server) handleReindex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := s.brain.RebuildIndexIncremental(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := s.brain.RecomputeClusters(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) handleVectors(w http.ResponseWriter, r *http.Request) {
	type response struct {
		Nodes     []vecNode       `json:"nodes"`
		Links     []vecLink       `json:"links"`
		Provider  string          `json:"provider"`
		Dims      int             `json:"dims"`
		Notes     int             `json:"notes"`                // notes in the index, vault excluded
		LastWrite string          `json:"last_write,omitempty"` // RFC 3339 time of the newest note change
		Clusters  []brain.Cluster `json:"clusters"`             // constellations, largest first as of the last k-means
	}

	idx, _ := s.brain.LoadFullIndexCached()
	notes, lastWrite := s.noteStats(idx)

	vi, err := s.brain.LoadVecIndex()
	if err != nil || vi == nil || len(vi.ChunkVecs) == 0 {
		writeJSON(w, response{Nodes: []vecNode{}, Links: []vecLink{}, Notes: notes, LastWrite: lastWrite, Clusters: []brain.Cluster{}})
		return
	}

	clusters := []brain.Cluster{}
	var assign map[string]int
	if cs, err := s.brain.Clusters(); err == nil && cs != nil {
		clusters, assign = cs.Clusters, cs.Assign
	}

	entries := make([]vecEntry, 0, len(vi.ChunkVecs))
	for k, v := range vi.ChunkVecs {
		// The vault is PIN-gated; its notes never show in the sky.
		if brain.IsVaultPath(chunkKeyDoc(k)) {
			continue
		}
		entries = append(entries, vecEntry{key: k, vec: v})
	}
	if len(entries) == 0 {
		writeJSON(w, response{Nodes: []vecNode{}, Links: []vecLink{}, Notes: notes, LastWrite: lastWrite, Clusters: clusters})
		return
	}
	// Map iteration order is random; sort so node order is stable per index.
	sort.Slice(entries, func(i, j int) bool { return entries[i].key < entries[j].key })

	// Safety cap: truncate only if brain is extremely large.
	if len(entries) > maxVecNodes {
		rand.Shuffle(len(entries), func(i, j int) { entries[i], entries[j] = entries[j], entries[i] })
		entries = entries[:maxVecNodes]
	}

	n := len(entries)
	dims := vi.Dims
	if dims == 0 && n > 0 {
		dims = len(entries[0].vec)
	}

	// Build float64 matrix for PCA.
	X := make([][]float64, n)
	for i, e := range entries {
		X[i] = make([]float64, dims)
		for j, f := range e.vec {
			if j < dims {
				X[i][j] = float64(f)
			}
		}
	}

	coords := pca3D(X, dims)
	scaleCoords(coords)

	nodes := make([]vecNode, n)
	for i, e := range entries {
		doc := chunkKeyDoc(e.key)
		nodes[i] = vecNode{
			ID:      e.key,
			X:       coords[i][0],
			Y:       coords[i][1],
			Z:       coords[i][2],
			Doc:     doc,
			Group:   firstSegment(doc),
			Label:   chunkKeyLabel(e.key),
			Title:   brain.FilenameTitle(doc),
			Cluster: -1,
		}
		if c, ok := assign[e.key]; ok {
			nodes[i].Cluster = c
		}
		if idx != nil {
			if d, ok := idx.Documents[doc]; ok {
				nodes[i].Title = d.DisplayTitle()
				nodes[i].Updated = d.Updated
				nodes[i].Views = d.AccessCount
				nodes[i].Words = d.WordCount
			}
		}
	}

	links := buildVecLinks(entries, coords, topNeighbors, minSimilarity)

	writeJSON(w, response{
		Nodes:     nodes,
		Links:     links,
		Provider:  vi.Provider,
		Dims:      dims,
		Notes:     notes,
		LastWrite: lastWrite,
		Clusters:  clusters,
	})
}

// noteStats counts non-vault notes and finds when the brain last learned
// something. Front matter dates are day-precise; when the newest note's file
// was modified on that same day, its modification time gives the hour.
func (s *Server) noteStats(idx *brain.FullIndex) (notes int, lastWrite string) {
	if idx == nil {
		return 0, ""
	}
	var newest brain.DocEntry
	for _, d := range idx.Documents {
		if brain.IsVaultPath(d.Path) {
			continue
		}
		notes++
		if d.Updated > newest.Updated || (d.Updated == newest.Updated && d.Path < newest.Path) {
			newest = d
		}
	}
	day, err := time.ParseInLocation(brain.DateFormat, newest.Updated, time.Local)
	if err != nil {
		return notes, ""
	}
	when := day
	if fi, err := os.Stat(filepath.Join(s.brainPath, newest.Path)); err == nil {
		if mt := fi.ModTime(); mt.In(time.Local).Format(brain.DateFormat) == newest.Updated {
			when = mt
		}
	}
	return notes, when.Format(time.RFC3339)
}

// pca3D reduces an n×dims matrix to n×3 using power iteration PCA.
func pca3D(X [][]float64, dims int) [][3]float64 {
	n := len(X)
	if n == 0 || dims == 0 {
		return make([][3]float64, n)
	}

	// Center the data.
	mean := make([]float64, dims)
	for _, row := range X {
		for j, v := range row {
			if j < dims {
				mean[j] += v
			}
		}
	}
	for j := range mean {
		mean[j] /= float64(n)
	}
	Xc := make([][]float64, n)
	for i, row := range X {
		Xc[i] = make([]float64, dims)
		for j := range Xc[i] {
			if j < len(row) {
				Xc[i][j] = row[j] - mean[j]
			}
		}
	}

	// Extract top 3 principal components via power iteration.
	k := 3
	if dims < 3 {
		k = dims
	}
	components := make([][]float64, k)
	for c := 0; c < k; c++ {
		v := make([]float64, dims)
		for j := range v {
			v[j] = math.Sin(float64(j+1) * math.Pi / float64(dims+1) * float64(c+1))
		}
		normVec64(v)

		for iter := 0; iter < 60; iter++ {
			Xv := make([]float64, n)
			for i, row := range Xc {
				for j, val := range row {
					Xv[i] += val * v[j]
				}
			}
			newV := make([]float64, dims)
			for i, row := range Xc {
				for j, val := range row {
					newV[j] += val * Xv[i]
				}
			}
			// Gram-Schmidt deflation against previous components.
			for pc := 0; pc < c; pc++ {
				d := dot64(newV, components[pc])
				for j := range newV {
					newV[j] -= d * components[pc][j]
				}
			}
			normVec64(newV)
			v = newV
		}
		components[c] = v
	}

	// Project all points onto the 3 components.
	result := make([][3]float64, n)
	for i, row := range Xc {
		for c := 0; c < k; c++ {
			for j, val := range row {
				result[i][c] += val * components[c][j]
			}
		}
	}
	return result
}

func scaleCoords(coords [][3]float64) {
	if len(coords) == 0 {
		return
	}
	var mn, mx [3]float64
	for i := range mn {
		mn[i] = math.MaxFloat64
		mx[i] = -math.MaxFloat64
	}
	for _, c := range coords {
		for i := range c {
			if c[i] < mn[i] {
				mn[i] = c[i]
			}
			if c[i] > mx[i] {
				mx[i] = c[i]
			}
		}
	}
	const target = 400.0
	for i := range coords {
		for j := range coords[i] {
			span := mx[j] - mn[j]
			if span < 1e-10 {
				coords[i][j] = 0
			} else {
				coords[i][j] = (coords[i][j]-mn[j])/span*target*2 - target
			}
		}
	}
}

// buildVecLinks builds graph edges using a projection-sort sliding window (O(n×W))
// instead of all-pairs comparison (O(n²)). Nodes are sorted by their PCA x-coordinate
// (first principal component, highest variance), and each node only competes against
// its W nearest neighbours in that sorted order. W = topK×20, so for topK=4 we check
// 80 candidates per node — enough to find tight clusters without a full pairwise scan.
func buildVecLinks(entries []vecEntry, coords [][3]float64, topK int, minSim float64) []vecLink {
	n := len(entries)
	if n == 0 {
		return nil
	}

	// Sort indices by PCA x-coordinate (PC1 = most variance).
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(a, b int) bool {
		return coords[order[a]][0] < coords[order[b]][0]
	})

	// pos[i] = position of entry i in the sorted order.
	pos := make([]int, n)
	for p, i := range order {
		pos[i] = p
	}

	window := topK * 20
	if window > n-1 {
		window = n - 1
	}

	type scored struct {
		j     int
		score float64
	}
	var links []vecLink

	for i := 0; i < n; i++ {
		p := pos[i]
		lo := p - window
		hi := p + window
		if lo < 0 {
			lo = 0
		}
		if hi >= n {
			hi = n - 1
		}

		var sims []scored
		for q := lo; q <= hi; q++ {
			j := order[q]
			if j <= i {
				continue // emit each pair once (i < j)
			}
			sc := cosSim32(entries[i].vec, entries[j].vec)
			if sc >= minSim {
				sims = append(sims, scored{j, sc})
			}
		}
		sort.Slice(sims, func(a, b int) bool { return sims[a].score > sims[b].score })
		if len(sims) > topK {
			sims = sims[:topK]
		}
		for _, s := range sims {
			links = append(links, vecLink{
				Source: entries[i].key,
				Target: entries[s.j].key,
				Value:  math.Round(s.score*1000) / 1000,
			})
		}
	}
	return links
}

// cosSim32 computes cosine similarity between two float32 unit vectors.
func cosSim32(a, b []float32) float64 {
	n := len(a)
	if n > len(b) {
		n = len(b)
	}
	var sum float64
	for i := 0; i < n; i++ {
		sum += float64(a[i]) * float64(b[i])
	}
	return sum
}

func normVec64(v []float64) {
	var sum float64
	for _, x := range v {
		sum += x * x
	}
	norm := math.Sqrt(sum)
	if norm < 1e-10 {
		return
	}
	for i := range v {
		v[i] /= norm
	}
}

func dot64(a, b []float64) float64 {
	var sum float64
	for i := range a {
		sum += a[i] * b[i]
	}
	return sum
}

// chunkKeyDoc extracts the document path from a chunk key ("doc/path#chunkID").
func chunkKeyDoc(key string) string {
	if i := strings.LastIndex(key, "#"); i >= 0 {
		return key[:i]
	}
	return key
}

// chunkKeyLabel returns a short display label for a chunk key.
func chunkKeyLabel(key string) string {
	doc := chunkKeyDoc(key)
	parts := strings.Split(doc, "/")
	name := parts[len(parts)-1]
	name = strings.TrimSuffix(name, ".md")
	if i := strings.LastIndex(key, "#"); i >= 0 {
		return name + " #" + key[i+1:]
	}
	return name
}

// firstSegment returns the first path segment (category).
func firstSegment(path string) string {
	if i := strings.Index(path, "/"); i >= 0 {
		return path[:i]
	}
	return path
}
