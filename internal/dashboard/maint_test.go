package dashboard

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/norenis/kai/internal/brain"
)

func post(t *testing.T, mux http.Handler, path, body string) (*http.Response, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
	return rec.Result(), rec.Body.String()
}

func TestTasksSayWhichFileAndWhetherItIsToday(t *testing.T) {
	mux, _ := newDemoMux(t)
	_, body := get(t, mux, "/api/tasks")
	var res struct {
		Items []brain.TaskItem
		Path  string
		Today bool
	}
	decode(t, body, &res)
	if res.Path != "tasks/2026-10-23.md" || res.Today || len(res.Items) != 3 {
		t.Fatalf("latest demo tasks = %+v", res)
	}

	if r, body := post(t, mux, "/api/tasks/add", `{"text":"Water the basil","priority":"low"}`); r.StatusCode != http.StatusOK {
		t.Fatalf("add: %d %s", r.StatusCode, body)
	}
	_, body = get(t, mux, "/api/tasks")
	res.Items, res.Path, res.Today = nil, "", false
	decode(t, body, &res)
	if res.Path != brain.TaskFilePath(time.Now()) || !res.Today || len(res.Items) != 1 || res.Items[0].Priority != "low" {
		t.Fatalf("today's tasks = %+v", res)
	}
}

func TestReindexPicksUpExternalEdits(t *testing.T) {
	mux, dir := newDemoMux(t)
	if r, _ := get(t, mux, "/api/reindex"); r.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET reindex: %d", r.StatusCode)
	}

	note := "---\ntags: [comets]\nconfidence: low\nsource: cli\nupdated: 2026-10-27\n---\n# Comet tails\n\nA comet's ion tail points away from the sun, whatever way the comet moves.\n"
	if err := os.WriteFile(filepath.Join(dir, "knowledge", "comet-tails.md"), []byte(note), 0o644); err != nil {
		t.Fatal(err)
	}
	if r, body := post(t, mux, "/api/reindex", ""); r.StatusCode != http.StatusOK {
		t.Fatalf("reindex: %d %s", r.StatusCode, body)
	}
	_, body := get(t, mux, "/api/vectors")
	if !strings.Contains(body, "knowledge/comet-tails.md#0") {
		t.Error("new note's chunk missing from /api/vectors after reindex")
	}
}
