package dashboard

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/norenis/kai/internal/config"
)

const demoVaultNote = "vault/demo-locker-codes.md"

func TestVaultHiddenFromListings(t *testing.T) {
	mux, _ := newDemoMux(t)
	for _, p := range []string{
		"/api/files", "/api/search?q=locker+codes+gym+bike", "/api/tags", "/api/overview", "/api/activity", "/api/vectors",
	} {
		_, body := get(t, mux, p)
		if strings.Contains(body, "vault") || strings.Contains(body, "Gym locker") || strings.Contains(body, "demo-locker") {
			t.Errorf("%s exposes the vault: %.200s", p, body)
		}
	}
}

func TestVaultFileNeedsPin(t *testing.T) {
	cfg := config.Defaults()
	cfg.Brain.VaultPin = "4321"
	mux, _ := newDemoMuxWith(t, cfg)

	read := func(pin string) (int, string) {
		req := httptest.NewRequest(http.MethodGet, "/api/file?path="+demoVaultNote, nil)
		if pin != "" {
			req.Header.Set(vaultPinHeader, pin)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}
	if code, body := read(""); code != http.StatusForbidden || strings.Contains(body, "0000") {
		t.Errorf("no pin: %d %s", code, body)
	}
	if code, _ := read("1111"); code != http.StatusForbidden {
		t.Errorf("wrong pin: %d", code)
	}
	if code, body := read("4321"); code != http.StatusOK || !strings.Contains(body, "Gym locker") {
		t.Errorf("right pin: %d %s", code, body)
	}
	// Path tricks resolve to the vault too.
	req := httptest.NewRequest(http.MethodGet, "/api/file?path=knowledge/../vault/demo-locker-codes.md", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if strings.Contains(rec.Body.String(), "Gym locker") {
		t.Error("path traversal reached the vault without a pin")
	}
}

func TestVaultClosedWithoutConfiguredPin(t *testing.T) {
	mux, _ := newDemoMux(t) // no vault_pin configured
	req := httptest.NewRequest(http.MethodGet, "/api/file?path="+demoVaultNote, nil)
	req.Header.Set(vaultPinHeader, "")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestVaultToolsNeedPin(t *testing.T) {
	cfg := config.Defaults()
	cfg.Brain.VaultPin = "4321"
	mux, _ := newDemoMuxWith(t, cfg)
	tool := func(name string, params map[string]string) string {
		body, _ := json.Marshal(map[string]any{"tool": name, "params": params})
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/tools/execute", bytes.NewReader(body)))
		return rec.Body.String()
	}
	if out := tool("brain_list", nil); strings.Contains(out, "vault") {
		t.Errorf("brain_list: %s", out)
	}
	if out := tool("brain_search", map[string]string{"query": "locker codes"}); strings.Contains(out, "vault") {
		t.Errorf("brain_search: %s", out)
	}
	if out := tool("ask", map[string]string{"question": "gym locker code"}); strings.Contains(out, "0000") {
		t.Errorf("ask: %s", out)
	}
	if out := tool("brain_read", map[string]string{"path": demoVaultNote}); strings.Contains(out, "Gym locker") {
		t.Errorf("brain_read without pin: %s", out)
	}
	if out := tool("brain_read", map[string]string{"path": demoVaultNote, "pin": "4321"}); !strings.Contains(out, "Gym locker") {
		t.Errorf("brain_read with pin: %s", out)
	}
	if out := tool("brain_write", map[string]string{"path": demoVaultNote, "content": "x", "tags": "x"}); !strings.Contains(out, "pin") {
		t.Errorf("brain_write without pin: %s", out)
	}
}

func TestImportRejectsVaultCategory(t *testing.T) {
	mux, _ := newDemoMux(t)
	for _, cat := range []string{"vault", "../vault", "knowledge/x"} {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		mw.WriteField("category", cat)
		fw, _ := mw.CreateFormFile("files", "note.md")
		fw.Write([]byte("# Note\n\nhello"))
		mw.Close()
		req := httptest.NewRequest(http.MethodPost, "/api/import", &buf)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if !strings.Contains(rec.Body.String(), "invalid category") {
			t.Errorf("category %q: %s", cat, rec.Body.String())
		}
	}
}

func TestImportKeepsTheWholeFile(t *testing.T) {
	mux, dir := newDemoMux(t)
	// Large enough to span many reads; the last line must survive.
	body := "# Long import\n\n" + strings.Repeat("A line of imported text that repeats.\n", 20000) + "The very last line.\n"
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.WriteField("category", "knowledge")
	mw.WriteField("auto_chunk", "false")
	fw, _ := mw.CreateFormFile("files", "long-import.md")
	fw.Write([]byte(body))
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/import", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `"error"`) {
		t.Fatalf("import: %d %s", rec.Code, rec.Body.String())
	}
	var saved []byte
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && strings.HasPrefix(d.Name(), "long-import") {
			saved, _ = os.ReadFile(p)
		}
		return nil
	})
	if !bytes.Contains(saved, []byte("The very last line.")) {
		t.Fatalf("saved import is cut short: %d bytes", len(saved))
	}
}
