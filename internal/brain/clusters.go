package brain

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// ClustersFileName caches the constellations next to the vector index.
const ClustersFileName = "clusters.json"

// Clustering parameters (spec 6.3).
const (
	clusterSeed        = 20261024
	clusterRestarts    = 5
	clusterMaxIter     = 40
	clusterMinK        = 5
	clusterMaxK        = 12
	clusterChunksPerK  = 40
	clusterRecomputeAt = 0.05 // relative chunk count change that forces a recompute
	clusterLabelTags   = 2
)

var romanNumerals = []string{"I", "II", "III", "IV", "V", "VI", "VII", "VIII", "IX", "X", "XI", "XII"}

// Cluster describes one constellation. IDs are ordered by size: 0 is the largest.
type Cluster struct {
	ID      int      `json:"id"`
	Label   string   `json:"label"` // distinctive tags joined with " · ", or a category
	Terms   []string `json:"terms"` // the tags (or category) behind the label
	Size    int      `json:"size"`  // chunks
	Notes   int      `json:"notes"` // notes with most of their chunks in this cluster
	Numeral string   `json:"numeral"`
}

// ClusterSet is the cached clustering of the vector index.
type ClusterSet struct {
	Provider  string         `json:"provider"`
	Dims      int            `json:"dims"`
	Chunks    int            `json:"chunks"` // chunk count when k-means last ran
	BuiltAt   time.Time      `json:"built_at"`
	Centroids [][]float32    `json:"centroids"`
	Assign    map[string]int `json:"assign"` // chunk key → cluster id
	Clusters  []Cluster      `json:"clusters"`
}

// clusterCount picks k = clamp(round(sqrt(n/40)), 5, 12), never above n.
func clusterCount(n int) int {
	k := int(math.Round(math.Sqrt(float64(n) / clusterChunksPerK)))
	k = max(clusterMinK, min(clusterMaxK, k))
	return min(k, n)
}

// ComputeClusters runs k-means++ (cosine, fixed seed, best of 5 restarts) on
// the full embedding vectors and labels each cluster from its notes' tags.
// Vault chunks are left out. Returns nil when there is nothing to cluster.
func ComputeClusters(vi *VecIndex, idx *FullIndex) *ClusterSet {
	if vi == nil {
		return nil
	}
	keys := make([]string, 0, len(vi.ChunkVecs))
	for k := range vi.ChunkVecs {
		if !IsVaultPath(chunkKeyPath(k)) {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return nil
	}
	sort.Strings(keys)
	vecs := make([][]float32, len(keys))
	for i, k := range keys {
		vecs[i] = unitCopy(vi.ChunkVecs[k])
	}

	k := clusterCount(len(vecs))
	assign, centroids := kmeansCosine(vecs, k, clusterSeed, clusterRestarts)

	cs := &ClusterSet{
		Provider:  vi.Provider,
		Dims:      vi.Dims,
		Chunks:    len(keys),
		BuiltAt:   time.Now().UTC(),
		Centroids: centroids,
		Assign:    make(map[string]int, len(keys)),
	}
	for i, key := range keys {
		cs.Assign[key] = assign[i]
	}
	cs.relabel(idx)
	return cs
}

// relabel orders clusters by size (renumbering ids and centroids so id 0 is
// the largest), recounts notes and recomputes labels and numerals.
func (cs *ClusterSet) relabel(idx *FullIndex) {
	k := len(cs.Centroids)
	sizes := make([]int, k)
	for _, c := range cs.Assign {
		sizes[c]++
	}
	order := make([]int, k)
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return sizes[order[a]] > sizes[order[b]] })
	remap := make([]int, k)
	centroids := make([][]float32, k)
	for newID, old := range order {
		remap[old] = newID
		centroids[newID] = cs.Centroids[old]
	}
	cs.Centroids = centroids
	for key, c := range cs.Assign {
		cs.Assign[key] = remap[c]
	}

	// Per note: how its chunks spread over clusters.
	perNote := map[string]map[int]int{}
	for key, c := range cs.Assign {
		p := chunkKeyPath(key)
		if perNote[p] == nil {
			perNote[p] = map[int]int{}
		}
		perNote[p][c]++
	}

	clusters := make([]Cluster, k)
	for i := range clusters {
		clusters[i] = Cluster{ID: i, Terms: []string{}}
		if i < len(romanNumerals) {
			clusters[i].Numeral = romanNumerals[i]
		} else {
			clusters[i].Numeral = fmt.Sprint(i + 1)
		}
	}
	for _, c := range cs.Assign {
		clusters[c].Size++
	}
	for _, counts := range perNote {
		clusters[majority(counts)].Notes++
	}

	labelClusters(clusters, perNote, idx)
	cs.Clusters = clusters
}

func majority(counts map[int]int) int {
	best, bestN := -1, -1
	for c, n := range counts {
		if n > bestN || (n == bestN && c < best) {
			best, bestN = c, n
		}
	}
	return best
}

// genericTags never name a constellation: they describe the kind of note, not its subject.
var genericTags = map[string]bool{
	"daily": true, "task": true, "tasks": true, "todo": true, "note": true, "notes": true,
	"misc": true, "general": true, "journal": true, "log": true, "episodic": true, "inbox": true,
	"archive": true, "draft": true, "checklist": true, "import": true, "imported": true,
	"conversation": true, "mcp": true, "vault": true, "identity": true, "opinion": true,
	"opinions": true, "style": true, "decision": true, "decisions": true, "knowledge": true,
}

// idLikeTag matches dates and ticket-style ids ("2026-10-18", "ticket-4512").
var idLikeTag = regexp.MustCompile(`^[\d\-_/.]+$|\d{3,}`)

func usableTag(t string) bool {
	t = strings.ToLower(strings.TrimSpace(t))
	return t != "" && !genericTags[t] && !idLikeTag.MatchString(t)
}

// labelClusters names each cluster after its most distinctive tags:
// score = (tag weight in the cluster) × log(1 + nNotes / (1 + notes with the tag)),
// where a note contributes the fraction of its chunks that sit in the cluster.
// Tags carried by more than half of all notes are skipped.
// Clusters without usable tags fall back to their dominant category.
func labelClusters(clusters []Cluster, perNote map[string]map[int]int, idx *FullIndex) {
	tagsOf := func(p string) []string {
		if idx == nil {
			return nil
		}
		return idx.Documents[p].Tags
	}

	globalNotes := map[string]int{}
	for p := range perNote {
		seen := map[string]bool{}
		for _, t := range tagsOf(p) {
			t = strings.ToLower(strings.TrimSpace(t))
			if usableTag(t) && !seen[t] {
				seen[t] = true
				globalNotes[t]++
			}
		}
	}
	nNotes := float64(len(perNote))

	weights := make([]map[string]float64, len(clusters))
	cats := make([]map[string]float64, len(clusters))
	for i := range clusters {
		weights[i] = map[string]float64{}
		cats[i] = map[string]float64{}
	}
	for p, counts := range perNote {
		total := 0
		for _, n := range counts {
			total += n
		}
		tags := map[string]bool{}
		for _, t := range tagsOf(p) {
			t = strings.ToLower(strings.TrimSpace(t))
			if usableTag(t) {
				tags[t] = true
			}
		}
		cat, _, _ := strings.Cut(p, "/")
		for c, n := range counts {
			f := float64(n) / float64(total)
			cats[c][cat] += f
			for t := range tags {
				weights[c][t] += f
			}
		}
	}

	used := map[string]int{}
	for i := range clusters {
		type scored struct {
			tag   string
			score float64
		}
		var s []scored
		for t, w := range weights[i] {
			// A tag on most notes says nothing about this cluster.
			if float64(globalNotes[t]) > nNotes/2 {
				continue
			}
			s = append(s, scored{t, w * math.Log(1+nNotes/(1+float64(globalNotes[t])))})
		}
		sort.Slice(s, func(a, b int) bool {
			if s[a].score != s[b].score {
				return s[a].score > s[b].score
			}
			return s[a].tag < s[b].tag
		})
		terms := []string{}
		for _, x := range s {
			if len(terms) == clusterLabelTags {
				break
			}
			terms = append(terms, x.tag)
		}
		if len(terms) == 0 {
			best, bestW := "", -1.0
			for c, w := range cats[i] {
				if w > bestW || (w == bestW && c < best) {
					best, bestW = c, w
				}
			}
			if best != "" {
				terms = []string{best}
			}
		}
		label := strings.Join(terms, " · ")
		if label == "" {
			label = "unnamed"
		}
		used[label]++
		if used[label] > 1 {
			label = fmt.Sprintf("%s (%d)", label, used[label])
		}
		clusters[i].Label = label
		clusters[i].Terms = terms
	}
}

// unitCopy returns v scaled to unit length (cosine distance becomes a dot product).
func unitCopy(v []float32) []float32 {
	out := make([]float32, len(v))
	copy(out, v)
	normalizeF32(out)
	return out
}

func dotF32(a, b []float32) float64 {
	n := min(len(a), len(b))
	var s float32
	for i := 0; i < n; i++ {
		s += a[i] * b[i]
	}
	return float64(s)
}

// kmeansCosine clusters unit vectors with spherical k-means and k-means++
// seeding, keeping the restart with the highest total similarity. Restarts
// use seed, seed+1, ... so results are reproducible.
func kmeansCosine(vecs [][]float32, k int, seed int64, restarts int) ([]int, [][]float32) {
	type result struct {
		assign    []int
		centroids [][]float32
		score     float64
	}
	results := make([]result, restarts)
	var wg sync.WaitGroup
	for r := 0; r < restarts; r++ {
		wg.Add(1)
		go func(r int) {
			defer wg.Done()
			a, c, s := kmeansOnce(vecs, k, rand.New(rand.NewSource(seed+int64(r))))
			results[r] = result{a, c, s}
		}(r)
	}
	wg.Wait()
	best := 0
	for r := 1; r < restarts; r++ {
		if results[r].score > results[best].score {
			best = r
		}
	}
	return results[best].assign, results[best].centroids
}

func kmeansOnce(vecs [][]float32, k int, rng *rand.Rand) ([]int, [][]float32, float64) {
	n, dims := len(vecs), len(vecs[0])
	centroids := seedPlusPlus(vecs, k, rng)
	assign := make([]int, n)
	for i := range assign {
		assign[i] = -1
	}
	sims := make([]float64, n)

	workers := max(1, min(runtime.GOMAXPROCS(0), n/64))
	assignStep := func() int {
		changed := make([]int, workers)
		var wg sync.WaitGroup
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func(w int) {
				defer wg.Done()
				for i := w; i < n; i += workers {
					best, bestSim := 0, math.Inf(-1)
					for c := range centroids {
						if s := dotF32(vecs[i], centroids[c]); s > bestSim {
							best, bestSim = c, s
						}
					}
					if assign[i] != best {
						assign[i] = best
						changed[w]++
					}
					sims[i] = bestSim
				}
			}(w)
		}
		wg.Wait()
		total := 0
		for _, c := range changed {
			total += c
		}
		return total
	}

	for iter := 0; iter < clusterMaxIter; iter++ {
		if assignStep() == 0 && iter > 0 {
			break
		}
		sums := make([][]float64, k)
		counts := make([]int, k)
		for c := range sums {
			sums[c] = make([]float64, dims)
		}
		for i, c := range assign {
			counts[c]++
			for j, x := range vecs[i] {
				sums[c][j] += float64(x)
			}
		}
		for c := range centroids {
			if counts[c] == 0 {
				// Empty cluster: restart it at the point least similar to its centroid.
				worst := 0
				for i := range sims {
					if sims[i] < sims[worst] {
						worst = i
					}
				}
				centroids[c] = unitCopy(vecs[worst])
				sims[worst] = 1
				continue
			}
			v := make([]float32, dims)
			for j := range v {
				v[j] = float32(sums[c][j])
			}
			normalizeF32(v)
			centroids[c] = v
		}
	}
	assignStep()

	score := 0.0
	for _, s := range sims {
		score += s
	}
	return assign, centroids, score
}

// seedPlusPlus picks initial centroids with probability proportional to the
// squared cosine distance from the nearest centroid chosen so far.
func seedPlusPlus(vecs [][]float32, k int, rng *rand.Rand) [][]float32 {
	n := len(vecs)
	centroids := [][]float32{unitCopy(vecs[rng.Intn(n)])}
	dist := make([]float64, n)
	for i := range dist {
		dist[i] = math.Inf(1)
	}
	for len(centroids) < k {
		last := centroids[len(centroids)-1]
		total := 0.0
		for i, v := range vecs {
			d := 1 - dotF32(v, last)
			d *= d
			if d < dist[i] {
				dist[i] = d
			}
			total += dist[i]
		}
		if total <= 0 {
			centroids = append(centroids, unitCopy(vecs[rng.Intn(n)]))
			continue
		}
		target := rng.Float64() * total
		pick := n - 1
		for i, d := range dist {
			target -= d
			if target <= 0 {
				pick = i
				break
			}
		}
		centroids = append(centroids, unitCopy(vecs[pick]))
	}
	return centroids
}

// nearestCentroid returns the cluster whose centroid is most similar to v.
func (cs *ClusterSet) nearestCentroid(v []float32) int {
	u := unitCopy(v)
	best, bestSim := 0, math.Inf(-1)
	for c, centroid := range cs.Centroids {
		if s := dotF32(u, centroid); s > bestSim {
			best, bestSim = c, s
		}
	}
	return best
}

// needsRecompute reports whether the cached set no longer fits the index:
// another provider or dimension, or a chunk count change above 5%.
func (cs *ClusterSet) needsRecompute(vi *VecIndex, chunks int) bool {
	if cs == nil || len(cs.Centroids) == 0 || cs.Chunks == 0 {
		return true
	}
	if cs.Provider != vi.Provider || cs.Dims != vi.Dims {
		return true
	}
	return math.Abs(float64(chunks-cs.Chunks))/float64(cs.Chunks) > clusterRecomputeAt
}

// reconciled follows small index changes without rerunning k-means: new
// chunks join their nearest centroid, removed chunks are dropped. It returns
// a new set; sets handed out earlier are never mutated.
func (cs *ClusterSet) reconciled(vi *VecIndex, idx *FullIndex) *ClusterSet {
	out := *cs
	out.Centroids = append([][]float32(nil), cs.Centroids...)
	assign := make(map[string]int, len(vi.ChunkVecs))
	for key, v := range vi.ChunkVecs {
		if IsVaultPath(chunkKeyPath(key)) {
			continue
		}
		if c, ok := cs.Assign[key]; ok {
			assign[key] = c
		} else {
			assign[key] = cs.nearestCentroid(v)
		}
	}
	out.Assign = assign
	out.relabel(idx)
	return &out
}

func (b *FileBrain) saveClusters(cs *ClusterSet) error {
	if cs == nil {
		return nil
	}
	data, err := json.Marshal(cs)
	if err != nil {
		return fmt.Errorf("marshal clusters: %w", err)
	}
	path := filepath.Join(b.root, ClustersFileName)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, FilePermissions); err != nil {
		return fmt.Errorf("write clusters: %w", err)
	}
	return os.Rename(tmp, path)
}

func (b *FileBrain) loadClusters() (*ClusterSet, error) {
	data, err := os.ReadFile(filepath.Join(b.root, ClustersFileName))
	if err != nil {
		return nil, err
	}
	var cs ClusterSet
	if err := json.Unmarshal(data, &cs); err != nil {
		return nil, fmt.Errorf("decode clusters: %w", err)
	}
	return &cs, nil
}

// Clusters returns the constellations for the current vector index. The
// request path only reads the cache; k-means runs here only when the cache is
// missing or out of date (normally it runs during reindex). Returns nil when
// there is no vector index.
func (b *FileBrain) Clusters() (*ClusterSet, error) {
	b.mu.RLock()
	vi, err := b.loadVecIndexCached()
	if err != nil || vi == nil {
		b.mu.RUnlock()
		return nil, nil
	}
	idx, _ := b.loadFullIndexCached()
	b.mu.RUnlock()

	chunks := 0
	for key := range vi.ChunkVecs {
		if !IsVaultPath(chunkKeyPath(key)) {
			chunks++
		}
	}

	b.clMu.Lock()
	defer b.clMu.Unlock()
	cs := b.clCache
	if cs == nil {
		cs, _ = b.loadClusters()
	}
	if cs.needsRecompute(vi, chunks) {
		cs = ComputeClusters(vi, idx)
		if err := b.saveClusters(cs); err != nil {
			return cs, err
		}
	} else if b.clCacheFor != vi || b.clCacheIdx != idx {
		cs = cs.reconciled(vi, idx)
	}
	b.clCache, b.clCacheFor, b.clCacheIdx = cs, vi, idx
	return cs, nil
}

// storeClusters saves constellations computed during a reindex and makes
// them the cached set for that index.
func (b *FileBrain) storeClusters(cs *ClusterSet, vi *VecIndex, idx *FullIndex) error {
	b.clMu.Lock()
	defer b.clMu.Unlock()
	if cs == nil {
		os.Remove(filepath.Join(b.root, ClustersFileName))
		b.clCache, b.clCacheFor, b.clCacheIdx = nil, nil, nil
		return nil
	}
	if err := b.saveClusters(cs); err != nil {
		return err
	}
	b.clCache, b.clCacheFor, b.clCacheIdx = cs, vi, idx
	return nil
}
