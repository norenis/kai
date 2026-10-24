package dashboard

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/norenis/kai/internal/brain"
	"github.com/norenis/kai/internal/config"
)

// newDemoMux serves a reindexed copy of the synthetic demo brain.
func newDemoMux(t *testing.T) (*http.ServeMux, string) {
	t.Helper()
	return newDemoMuxWith(t, config.Defaults())
}

func newDemoMuxWith(t *testing.T, cfg *config.Config) (*http.ServeMux, string) {
	t.Helper()
	src := "../../tests/fixtures/demo-brain"
	dir := t.TempDir()
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dir, rel), 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, rel), data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	b := brain.New(dir)
	b.SetEmbedOptions(brain.EmbedOptions{Provider: brain.EmbedLSI})
	if err := b.RebuildIndex(); err != nil {
		t.Fatal(err)
	}
	cfg.SessionsPath = t.TempDir()
	srv := New(brain.New(dir), dir, cfg)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	return mux, dir
}

func decode(t *testing.T, body string, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(body), v); err != nil {
		t.Fatalf("decode: %v\n%s", err, body[:min(200, len(body))])
	}
}

func TestVectorsCarryNoteFieldsAndHideVault(t *testing.T) {
	mux, dir := newDemoMux(t)
	_, body := get(t, mux, "/api/vectors")
	var res struct {
		Nodes     []vecNode `json:"nodes"`
		Notes     int       `json:"notes"`
		LastWrite string    `json:"last_write"`
		Clusters  []struct {
			ID      int    `json:"id"`
			Label   string `json:"label"`
			Size    int    `json:"size"`
			Numeral string `json:"numeral"`
		} `json:"clusters"`
	}
	decode(t, body, &res)

	if len(res.Clusters) < 5 {
		t.Fatalf("clusters = %d, want >= 5", len(res.Clusters))
	}
	sizes := map[int]int{}
	for _, n := range res.Nodes {
		if n.Cluster < 0 || n.Cluster >= len(res.Clusters) {
			t.Fatalf("node %s has cluster %d", n.ID, n.Cluster)
		}
		sizes[n.Cluster]++
	}
	for i, c := range res.Clusters {
		if c.ID != i || c.Label == "" || c.Numeral == "" || c.Size != sizes[i] {
			t.Errorf("cluster %d = %+v (counted %d nodes)", i, c, sizes[i])
		}
		if i > 0 && c.Size > res.Clusters[i-1].Size {
			t.Errorf("clusters not sorted by size at %d", i)
		}
	}

	if len(res.Nodes) == 0 {
		t.Fatal("no nodes")
	}
	if strings.Contains(body, "vault/") || strings.Contains(body, "locker") {
		t.Error("vault note leaked into /api/vectors")
	}
	var found bool
	for _, n := range res.Nodes {
		if n.Title == "" || n.Updated == "" || n.Words == 0 {
			t.Fatalf("node %s missing fields: %+v", n.ID, n)
		}
		if n.Doc == "knowledge/table-driven-tests-in-go.md" {
			found = true
			if n.Title != "Table driven tests in Go" || n.Views != 1 {
				t.Errorf("node fields = title %q views %d", n.Title, n.Views)
			}
		}
	}
	if !found {
		t.Error("demo note missing from nodes")
	}

	files, _ := filepath.Glob(filepath.Join(dir, "*", "*.md"))
	if want := len(files) - 1; res.Notes != want { // minus the vault note
		t.Errorf("notes = %d, want %d", res.Notes, want)
	}
	lw, err := time.Parse(time.RFC3339, res.LastWrite)
	if err != nil {
		t.Fatalf("last_write %q: %v", res.LastWrite, err)
	}
	// The demo brain's newest note is dated 2026-10-24; the copied files were
	// modified today, so the date alone is used unless today is that day.
	if got := lw.In(time.Local).Format(brain.DateFormat); got != "2026-10-24" {
		t.Errorf("last_write day = %s, want 2026-10-24", got)
	}
}

func TestFilesAndFileCarryTitles(t *testing.T) {
	mux, _ := newDemoMux(t)
	_, body := get(t, mux, "/api/files")
	var files struct {
		Files []struct{ Path, Title string } `json:"files"`
	}
	decode(t, body, &files)
	titles := map[string]string{}
	for _, f := range files.Files {
		titles[f.Path] = f.Title
	}
	if got := titles["knowledge/nau-pho-bo-tai-nha.md"]; got != "Nấu phở bò tại nhà" {
		t.Errorf("files title = %q", got)
	}
	if got := titles["tasks/2026-10-23.md"]; got != "2026-10-23" {
		t.Errorf("task file title = %q", got)
	}

	_, body = get(t, mux, "/api/file?path=knowledge/table-driven-tests-in-go.md")
	var file struct{ Title string }
	decode(t, body, &file)
	if file.Title != "Table driven tests in Go" {
		t.Errorf("file title = %q", file.Title)
	}
}
