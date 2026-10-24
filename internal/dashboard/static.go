package dashboard

import (
	"bytes"
	"embed"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strings"

	kai "github.com/norenis/kai"
)

// staticFS holds the new UI's modules, styles, vendored libraries and fonts.
// Nothing is loaded from a CDN at runtime.
//
//go:embed static
var staticFS embed.FS

// assetsPlaceholder in templates/index.html is replaced with the versioned
// static prefix, e.g. "static/v1.2.0", so every release gets fresh URLs and
// assets can be cached forever.
const assetsPlaceholder = "{{ASSETS}}"

var unsafeVersionChars = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// assetVersion returns the build version made safe for use as a URL segment.
func assetVersion() string {
	v := unsafeVersionChars.ReplaceAllString(kai.Version, "-")
	if v == "" {
		return "dev"
	}
	return v
}

// staticContentTypes pins content types for embedded assets; the system mime
// table differs between hosts (e.g. no woff2 in minimal containers).
var staticContentTypes = map[string]string{
	".js":    "text/javascript; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".woff2": "font/woff2",
	".svg":   "image/svg+xml",
	".png":   "image/png",
	".json":  "application/json",
	".txt":   "text/plain; charset=utf-8",
	".md":    "text/plain; charset=utf-8",
}

// handleStatic serves /static/<version>/<file> from the embedded static tree.
// The version segment is only a cache key: any value resolves to the same
// files, but only the current release version gets long-lived caching.
func handleStatic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/static/")
	version, name, ok := strings.Cut(rest, "/")
	if !ok || version == "" || name == "" {
		http.NotFound(w, r)
		return
	}
	name = path.Clean(name)
	if strings.HasPrefix(name, "..") || strings.HasPrefix(name, "/") {
		http.NotFound(w, r)
		return
	}

	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		http.Error(w, "static assets unavailable", http.StatusInternalServerError)
		return
	}
	info, err := fs.Stat(sub, name)
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}

	if ct, ok := staticContentTypes[path.Ext(name)]; ok {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if kai.Version != "dev" && version == assetVersion() {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	http.ServeFileFS(w, r, sub, name)
}

// serveShell serves the new UI shell with the versioned asset prefix filled in.
func serveShell(w http.ResponseWriter) {
	data, err := templateFS.ReadFile("templates/index.html")
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	data = bytes.ReplaceAll(data, []byte(assetsPlaceholder), []byte("static/"+assetVersion()))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(data)
}
