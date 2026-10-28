package dashboard

import (
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/norenis/kai/internal/brain"
	"github.com/norenis/kai/internal/config"
)

func newTestMux(t *testing.T, cfg *config.Config) *http.ServeMux {
	t.Helper()
	dir := t.TempDir()
	if cfg == nil {
		cfg = config.Defaults()
	}
	cfg.SessionsPath = t.TempDir()
	srv := New(brain.New(dir), dir, cfg)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	return mux
}

func get(t *testing.T, mux http.Handler, path string) (*http.Response, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	res := rec.Result()
	body, _ := io.ReadAll(res.Body)
	return res, string(body)
}

func TestShellServedAtRoot(t *testing.T) {
	mux := newTestMux(t, nil)
	res, body := get(t, mux, "/")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
	if !strings.Contains(body, `class="rail"`) {
		t.Error("shell has no rail")
	}
	if strings.Contains(body, assetsPlaceholder) {
		t.Error("assets placeholder not replaced")
	}
	if !strings.Contains(body, "static/"+assetVersion()+"/app.js") {
		t.Error("shell does not reference the versioned app module")
	}
}

func TestClassicRedirectsAndUnknownPathsAre404(t *testing.T) {
	mux := newTestMux(t, nil)
	for p, want := range map[string]string{"/classic": "./", "/classic/": "../"} {
		res, _ := get(t, mux, p)
		if res.StatusCode != http.StatusFound || res.Header.Get("Location") != want {
			t.Errorf("%s: %d -> %q, want 302 -> %q", p, res.StatusCode, res.Header.Get("Location"), want)
		}
	}
	if res, _ := get(t, mux, "/no/such/page"); res.StatusCode != http.StatusNotFound {
		t.Errorf("unknown path: status = %d", res.StatusCode)
	}
}

func TestStaticAssets(t *testing.T) {
	mux := newTestMux(t, nil)
	v := assetVersion()
	cases := []struct {
		path, ctype string
	}{
		{"/static/" + v + "/app.js", "text/javascript; charset=utf-8"},
		{"/static/" + v + "/theme.css", "text/css; charset=utf-8"},
		{"/static/" + v + "/vendor/three/three.module.min.js", "text/javascript; charset=utf-8"},
		{"/static/" + v + "/vendor/three/addons/postprocessing/UnrealBloomPass.js", "text/javascript; charset=utf-8"},
		{"/static/" + v + "/vendor/marked/marked.esm.js", "text/javascript; charset=utf-8"},
		{"/static/" + v + "/fonts/fonts.css", "text/css; charset=utf-8"},
		{"/static/" + v + "/fonts/inter-latin-400-normal.woff2", "font/woff2"},
	}
	for _, c := range cases {
		res, _ := get(t, mux, c.path)
		if res.StatusCode != http.StatusOK {
			t.Errorf("%s: status = %d", c.path, res.StatusCode)
			continue
		}
		if got := res.Header.Get("Content-Type"); got != c.ctype {
			t.Errorf("%s: content type = %q, want %q", c.path, got, c.ctype)
		}
	}

	for _, p := range []string{
		"/static/" + v + "/",
		"/static/" + v + "/vendor",
		"/static/" + v + "/../templates/index.html",
		"/static/" + v + "/missing.js",
		"/static/app.js",
	} {
		if res, _ := get(t, mux, p); res.StatusCode == http.StatusOK {
			t.Errorf("%s: expected an error status, got 200", p)
		}
	}
}

func TestStaticCacheHeaders(t *testing.T) {
	mux := newTestMux(t, nil)

	res, _ := get(t, mux, "/static/"+assetVersion()+"/app.js")
	if got := res.Header.Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Errorf("current assets: Cache-Control = %q, want immutable", got)
	}
	res, _ = get(t, mux, "/static/0123456789ab/app.js")
	if got := res.Header.Get("Cache-Control"); got != "no-cache" {
		t.Errorf("stale asset version: Cache-Control = %q, want no-cache", got)
	}
}

func TestAssetVersionIsContentHash(t *testing.T) {
	v := assetVersion()
	if !regexp.MustCompile(`^[0-9a-f]{12}$`).MatchString(v) {
		t.Errorf("assetVersion() = %q, want 12 hex chars", v)
	}
	if v != assetVersion() {
		t.Error("assetVersion() is not stable")
	}
}

func TestStaticBehindAuthIsPublic(t *testing.T) {
	cfg := config.Defaults()
	cfg.Server.DashboardPIN = "1234"
	mux := newTestMux(t, cfg)

	res, body := get(t, mux, "/")
	if res.StatusCode != http.StatusOK || strings.Contains(body, `class="rail"`) {
		t.Error("shell must sit behind the PIN lock")
	}
	if !strings.Contains(body, `id="pin-input"`) || !strings.Contains(body, "static/"+assetVersion()+"/theme.css") {
		t.Error("lock page is not the themed PIN screen")
	}
	if res, _ := get(t, mux, "/static/"+assetVersion()+"/theme.css"); res.StatusCode != http.StatusOK {
		t.Error("static assets carry no brain data and must load without a session")
	}
}

// The new UI must not depend on any third-party host at runtime.
func TestNewUIHasNoExternalURLs(t *testing.T) {
	external := regexp.MustCompile(`(?i)(src|href)\s*=\s*["']https?://|url\(\s*["']?https?://|from\s+["']https?://|import\(\s*["']https?://|"https?://[^"]*\.(js|css|woff2?)"`)
	check := func(name string, data []byte) {
		if loc := external.FindIndex(data); loc != nil {
			t.Errorf("%s references an external URL: %q", name, data[loc[0]:min(loc[1]+40, len(data))])
		}
	}

	shell, err := templateFS.ReadFile("templates/index.html")
	if err != nil {
		t.Fatal(err)
	}
	check("templates/index.html", shell)
	lock, err := templateFS.ReadFile("templates/lock.html")
	if err != nil {
		t.Fatal(err)
	}
	check("templates/lock.html", lock)

	err = fs.WalkDir(staticFS, "static", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if strings.HasPrefix(p, "static/vendor/") {
			return nil // third-party code; checked by loading in the browser
		}
		data, err := staticFS.ReadFile(p)
		if err != nil {
			return err
		}
		check(p, data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
