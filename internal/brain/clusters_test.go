package brain

import (
	"fmt"
	"math/rand"
	"reflect"
	"testing"
	"time"
)

// separableIndex builds groups of noisy vectors around random unit directions.
func separableIndex(groups, perGroup, dims int, noise float64, seed int64) (*VecIndex, map[string]int) {
	rng := rand.New(rand.NewSource(seed))
	vi := &VecIndex{Dims: dims, Provider: "test", ChunkVecs: map[string][]float32{}}
	truth := map[string]int{}
	for g := 0; g < groups; g++ {
		center := make([]float64, dims)
		for j := range center {
			center[j] = rng.NormFloat64()
		}
		for i := 0; i < perGroup; i++ {
			v := make([]float32, dims)
			for j := range v {
				v[j] = float32(center[j] + noise*rng.NormFloat64())
			}
			key := fmt.Sprintf("knowledge/g%d-n%d.md#0", g, i)
			vi.ChunkVecs[key] = v
			truth[key] = g
		}
	}
	return vi, truth
}

func TestClusterCount(t *testing.T) {
	for n, want := range map[int]int{3: 3, 40: 5, 1000: 5, 1500: 6, 3200: 9, 5000: 11, 100000: 12} {
		if got := clusterCount(n); got != want {
			t.Errorf("clusterCount(%d) = %d, want %d", n, got, want)
		}
	}
}

func TestKMeansRecoversSeparableGroups(t *testing.T) {
	// 6 groups x 50 = 300 chunks -> k = 5 by the formula; use exactly 5 groups.
	vi, truth := separableIndex(5, 60, 64, 0.35, 1)
	cs := ComputeClusters(vi, nil)
	if len(cs.Clusters) != 5 {
		t.Fatalf("clusters = %d, want 5", len(cs.Clusters))
	}
	// Every true group must map to exactly one cluster and vice versa.
	pair := map[int]map[int]bool{}
	for key, g := range truth {
		if pair[g] == nil {
			pair[g] = map[int]bool{}
		}
		pair[g][cs.Assign[key]] = true
	}
	seen := map[int]bool{}
	for g, cl := range pair {
		if len(cl) != 1 {
			t.Errorf("group %d split over clusters %v", g, cl)
		}
		for c := range cl {
			if seen[c] {
				t.Errorf("cluster %d holds more than one group", c)
			}
			seen[c] = true
		}
	}
}

func TestClustersAreDeterministicAndOrderedBySize(t *testing.T) {
	vi, _ := separableIndex(7, 40, 32, 0.6, 2)
	// Uneven sizes: drop part of one group so ordering by size is testable.
	for i := 0; i < 25; i++ {
		delete(vi.ChunkVecs, fmt.Sprintf("knowledge/g3-n%d.md#0", i))
	}
	a := ComputeClusters(vi, nil)
	b := ComputeClusters(vi, nil)
	if !reflect.DeepEqual(a.Assign, b.Assign) || !reflect.DeepEqual(a.Clusters, b.Clusters) {
		t.Fatal("clustering is not deterministic for an unchanged index")
	}
	for i := 1; i < len(a.Clusters); i++ {
		if a.Clusters[i].Size > a.Clusters[i-1].Size {
			t.Errorf("cluster %d larger than cluster %d", i, i-1)
		}
		if a.Clusters[i].ID != i || a.Clusters[i].Numeral != romanNumerals[i] {
			t.Errorf("cluster %d: id %d numeral %q", i, a.Clusters[i].ID, a.Clusters[i].Numeral)
		}
	}
}

func TestLabelsPickDistinctiveTags(t *testing.T) {
	clusters := []Cluster{{ID: 0}, {ID: 1}, {ID: 2}}
	perNote := map[string]map[int]int{
		"knowledge/a.md":   {0: 2},
		"knowledge/b.md":   {0: 1},
		"knowledge/c.md":   {0: 3, 1: 1},
		"knowledge/d.md":   {1: 2},
		"knowledge/e.md":   {1: 2},
		"tasks/2026-10-01": {2: 1},
		"tasks/2026-10-02": {2: 1},
	}
	idx := &FullIndex{Documents: map[string]DocEntry{
		// "go" is on most notes, so it never names a cluster; "pprof" is rare and specific.
		"knowledge/a.md":   {Tags: []string{"go", "pprof", "daily"}},
		"knowledge/b.md":   {Tags: []string{"go", "pprof"}},
		"knowledge/c.md":   {Tags: []string{"go", "pprof", "ticket-4512"}},
		"knowledge/d.md":   {Tags: []string{"go", "baking"}},
		"knowledge/e.md":   {Tags: []string{"go", "baking", "2026-10-18"}},
		"tasks/2026-10-01": {Tags: []string{"tasks", "daily"}},
		"tasks/2026-10-02": {Tags: []string{"tasks"}},
	}}
	labelClusters(clusters, perNote, idx)

	if clusters[0].Label != "pprof" {
		t.Errorf("cluster 0 label = %q terms %v", clusters[0].Label, clusters[0].Terms)
	}
	if clusters[1].Terms[0] != "baking" {
		t.Errorf("cluster 1 label = %q", clusters[1].Label)
	}
	// Only generic or id-like tags: fall back to the dominant category.
	if clusters[2].Label != "tasks" || !reflect.DeepEqual(clusters[2].Terms, []string{"tasks"}) {
		t.Errorf("cluster 2 label = %q terms %v", clusters[2].Label, clusters[2].Terms)
	}
	for _, c := range clusters {
		for _, term := range c.Terms {
			if term == "go" || term == "daily" || term == "ticket-4512" || term == "2026-10-18" {
				t.Errorf("generic tag %q used in a label", term)
			}
		}
	}
}

func TestDuplicateLabelsAreDistinguished(t *testing.T) {
	clusters := []Cluster{{ID: 0}, {ID: 1}}
	perNote := map[string]map[int]int{"knowledge/a.md": {0: 1}, "knowledge/b.md": {1: 1}, "knowledge/c.md": {1: 1}, "knowledge/d.md": {1: 1}}
	idx := &FullIndex{Documents: map[string]DocEntry{
		"knowledge/a.md": {Tags: []string{"go"}},
		"knowledge/b.md": {Tags: []string{"go"}},
		"knowledge/c.md": {Tags: []string{"daily"}},
		"knowledge/d.md": {Tags: []string{"daily"}},
	}}
	labelClusters(clusters, perNote, idx)
	if clusters[0].Label != "go" || clusters[1].Label != "go (2)" {
		t.Errorf("labels = %q, %q", clusters[0].Label, clusters[1].Label)
	}
}

func TestClusterCacheInvalidation(t *testing.T) {
	b := New(copyDemoBrain(t))
	b.SetEmbedOptions(EmbedOptions{Provider: EmbedLSI})
	if err := b.RebuildIndex(); err != nil {
		t.Fatal(err)
	}
	first, err := b.Clusters()
	if err != nil || first == nil {
		t.Fatalf("Clusters: %v", err)
	}
	for _, c := range first.Clusters {
		if c.Label == "" || c.Numeral == "" {
			t.Errorf("cluster %d has no label or numeral", c.ID)
		}
	}
	for key := range first.Assign {
		if IsVaultPath(chunkKeyPath(key)) {
			t.Errorf("vault chunk %s was clustered", key)
		}
	}

	// A restart (fresh FileBrain) reuses the cached file unchanged.
	again, err := New(b.root).Clusters()
	if err != nil || !again.BuiltAt.Equal(first.BuiltAt) || !reflect.DeepEqual(again.Assign, first.Assign) {
		t.Fatalf("cached clusters not reused after restart (err %v)", err)
	}

	vi, _ := b.LoadVecIndex()

	// A small change (< 5%) keeps k-means results and assigns the new chunk.
	var anyKey string
	for k := range vi.ChunkVecs {
		anyKey = k
		break
	}
	small := copyVecIndex(vi)
	small.ChunkVecs["knowledge/new-note.md#0"] = small.ChunkVecs[anyKey]
	cached, err := b.loadClusters()
	if err != nil {
		t.Fatal(err)
	}
	got := cached.reconciled(small, nil)
	if got.Assign["knowledge/new-note.md#0"] != first.Assign[anyKey] {
		t.Error("new chunk not assigned to the nearest centroid")
	}
	if got.needsRecompute(small, len(small.ChunkVecs)-1) {
		t.Error("small change should not force a recompute")
	}

	// A large change (> 5%) or a provider change forces a recompute.
	if !first.needsRecompute(vi, int(float64(first.Chunks)*1.06)+1) {
		t.Error("6% growth should force a recompute")
	}
	other := copyVecIndex(vi)
	other.Provider = "gemini"
	if !first.needsRecompute(other, first.Chunks) {
		t.Error("provider change should force a recompute")
	}

	// Background rebuilds (after every write) keep the constellations.
	time.Sleep(10 * time.Millisecond)
	if err := b.Save(&BrainFile{Path: "knowledge/extra-note.md", Tags: []string{"go"}, Confidence: "medium",
		Content: "\nGo channels and goroutines for backend services.\n"}); err != nil {
		t.Fatal(err)
	}
	if err := b.RebuildIndex(); err != nil {
		t.Fatal(err)
	}
	after, _ := b.Clusters()
	if !after.BuiltAt.Equal(first.BuiltAt) {
		t.Error("a background rebuild reran k-means")
	}
	for key, c := range first.Assign {
		if after.Assign[key] != c {
			t.Errorf("chunk %s moved from cluster %d to %d", key, c, after.Assign[key])
			break
		}
	}
	if _, ok := after.Assign["knowledge/extra-note.md#0"]; !ok {
		t.Error("new chunk was not placed in a constellation")
	}
	for i, c := range after.Clusters {
		if c.Numeral != first.Clusters[i].Numeral || c.ID != i {
			t.Errorf("cluster %d renumbered: %+v", i, c)
		}
	}

	// An explicit reindex recomputes.
	if err := b.RecomputeClusters(); err != nil {
		t.Fatal(err)
	}
	rebuilt, _ := b.Clusters()
	if !rebuilt.BuiltAt.After(first.BuiltAt) {
		t.Error("RecomputeClusters did not rerun k-means")
	}
	if rebuilt.Chunks != first.Chunks+1 {
		t.Errorf("recomputed over %d chunks, want %d", rebuilt.Chunks, first.Chunks+1)
	}
}

func copyVecIndex(vi *VecIndex) *VecIndex {
	out := *vi
	out.ChunkVecs = make(map[string][]float32, len(vi.ChunkVecs))
	for k, v := range vi.ChunkVecs {
		out.ChunkVecs[k] = v
	}
	return &out
}

// BenchmarkComputeClusters3k measures the reindex-time cost on a brain the
// size of the largest real one (3k chunks of 1536-dimensional embeddings).
func BenchmarkComputeClusters3k(b *testing.B) {
	vi, _ := separableIndex(9, 340, 1536, 1.2, 3)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ComputeClusters(vi, nil)
	}
}

// BenchmarkComputeClusters3kNoStructure is the worst case: no real groups, so
// k-means runs to its iteration cap on every restart.
func BenchmarkComputeClusters3kNoStructure(b *testing.B) {
	vi, _ := separableIndex(1, 3060, 1536, 50, 4)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ComputeClusters(vi, nil)
	}
}
