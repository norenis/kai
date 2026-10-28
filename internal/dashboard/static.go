package dashboard

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"sync"
)

// staticFS holds the new UI's modules, styles, vendored libraries and fonts.
// Nothing is loaded from a CDN at runtime.
//
//go:embed static
var staticFS embed.FS

// assetsPlaceholder in templates/index.html is replaced with the versioned
// static prefix, e.g. "static/3f9a0c1b2d4e", so every change to the assets
// gets fresh URLs and assets can be cached forever.
const assetsPlaceholder = "{{ASSETS}}"

// assetVersion returns a short hash of the embedded static tree. Hashing the
// content (instead of using the build version) keeps caching correct for
// rebuilt development binaries too.
var assetVersion = sync.OnceValue(func() string {
	h := sha256.New()
	_ = fs.WalkDir(staticFS, "static", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := staticFS.ReadFile(p)
		if err != nil {
			return err
		}
		h.Write([]byte(p))
		h.Write([]byte{0})
		h.Write(data)
		return nil
	})
	return hex.EncodeToString(h.Sum(nil))[:12]
})

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
// files, but only the current content hash gets long-lived caching.
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
	if version == assetVersion() {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	http.ServeFileFS(w, r, sub, name)
}

// serveTemplate serves a page template (the shell or the lock screen) with
// the versioned asset prefix filled in.
func serveTemplate(w http.ResponseWriter, name string) {
	data, err := templateFS.ReadFile(name)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	data = bytes.ReplaceAll(data, []byte(assetsPlaceholder), []byte("static/"+assetVersion()))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(data)
}
