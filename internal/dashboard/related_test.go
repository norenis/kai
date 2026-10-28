package dashboard

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/norenis/kai/internal/config"
)

func TestRelatedOnDemoBrain(t *testing.T) {
	mux, _ := newDemoMux(t)
	const note = "knowledge/shaping-a-batard.md"
	_, body := get(t, mux, "/api/related?path="+note)
	var res struct{ Related []relatedNote }
	decode(t, body, &res)

	if len(res.Related) == 0 || len(res.Related) > maxRelated {
		t.Fatalf("related = %d notes, want 1..%d: %s", len(res.Related), maxRelated, body)
	}
	for i, n := range res.Related {
		if n.Path == note {
			t.Error("note is related to itself")
		}
		if n.Title == "" || n.Score <= 0 || n.Cluster < 0 {
			t.Errorf("related[%d] = %+v", i, n)
		}
		if i > 0 && n.Score > res.Related[i-1].Score {
			t.Errorf("not sorted by score at %d", i)
		}
	}
	// The demo brain's bread notes are each other's nearest neighbours.
	bread := map[string]bool{
		"knowledge/baking-in-a-dutch-oven.md": true, "knowledge/cold-retard-schedule.md": true,
		"knowledge/feeding-a-sourdough-starter.md": true, "knowledge/focaccia-for-a-crowd.md": true,
		"knowledge/hydration-and-dough-strength.md": true,
	}
	if !bread[res.Related[0].Path] {
		t.Errorf("nearest to %s is %s, want a bread note", note, res.Related[0].Path)
	}
}

func TestRelatedUnknownNoteAndMissingPath(t *testing.T) {
	mux, _ := newDemoMux(t)
	if res, _ := get(t, mux, "/api/related"); res.StatusCode != http.StatusBadRequest {
		t.Errorf("missing path: %d", res.StatusCode)
	}
	_, body := get(t, mux, "/api/related?path=knowledge/no-such-note.md")
	if strings.TrimSpace(body) != `{"related":[]}` {
		t.Errorf("unknown note: %s", body)
	}
}

func TestRelatedNeverListsOrRevealsTheVault(t *testing.T) {
	cfg := config.Defaults()
	cfg.Brain.VaultPin = "4321"
	mux, _ := newDemoMuxWith(t, cfg)

	for _, p := range []string{"knowledge/table-driven-tests-in-go.md", "identity/profile.md"} {
		if _, body := get(t, mux, "/api/related?path="+p); strings.Contains(body, "vault") {
			t.Errorf("%s: vault note listed: %s", p, body)
		}
	}
	read := func(pin string) int {
		req := httptest.NewRequest(http.MethodGet, "/api/related?path="+demoVaultNote, nil)
		if pin != "" {
			req.Header.Set(vaultPinHeader, pin)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := read(""); code != http.StatusForbidden {
		t.Errorf("vault note without pin: %d", code)
	}
	if code := read("4321"); code != http.StatusOK {
		t.Errorf("vault note with pin: %d", code)
	}
}
