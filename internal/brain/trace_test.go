package brain

import (
	"reflect"
	"testing"
)

func TestQueryHybridTracedStages(t *testing.T) {
	b := New(copyDemoBrain(t))
	b.SetEmbedOptions(EmbedOptions{Provider: EmbedLSI})
	if err := b.RebuildIndex(); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"sourdough starter hydration", "phở nước dùng", "zzzz-no-match"} {
		results, trace := b.QueryHybridTraced(q)
		if trace == nil {
			t.Fatal("no trace")
		}
		var names []string
		for _, s := range trace.Stages {
			names = append(names, s.Name)
			if s.MS < 0 || s.Count < 0 {
				t.Errorf("%q: stage %+v has negative values", q, s)
			}
		}
		if !reflect.DeepEqual(names, []string{"embed", "bm25", "vector", "prf", "fuse"}) {
			t.Errorf("%q: stages %v", q, names)
		}
		if trace.TotalMS < 0 {
			t.Errorf("total %v", trace.TotalMS)
		}
		if got := trace.Stages[4].Count; got != len(results) {
			t.Errorf("%q: fuse count %d, results %d", q, got, len(results))
		}
		// Same ranking as the untraced search.
		plain := b.QueryHybrid(q)
		if len(plain) != len(results) {
			t.Fatalf("%q: %d vs %d results", q, len(plain), len(results))
		}
		for i := range plain {
			if plain[i].DocPath != results[i].DocPath || plain[i].ChunkID != results[i].ChunkID {
				t.Fatalf("%q: rank %d differs", q, i)
			}
		}
	}
	res, tr := b.QueryHybridTraced("sourdough starter hydration")
	if len(res) == 0 || tr.Stages[0].Count == 0 || tr.Stages[2].Count == 0 {
		t.Errorf("expected embed dims and vector hits: %+v", tr.Stages)
	}
}
