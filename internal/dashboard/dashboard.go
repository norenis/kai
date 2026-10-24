package dashboard

import (
	"crypto/rand"
	"embed"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"context"

	kai "github.com/norenis/kai"
	"github.com/norenis/kai/internal/brain"
	"github.com/norenis/kai/internal/config"
	"github.com/norenis/kai/internal/module"
	"github.com/norenis/kai/internal/router"
	bsync "github.com/norenis/kai/internal/sync"
	"github.com/xuri/excelize/v2"
)

//go:embed templates/*
var templateFS embed.FS

// Server serves the brain dashboard.
type Server struct {
	brain       *brain.FileBrain
	brainPath   string
	cfg         *config.Config
	sessions    sync.Map // token → expiry (unix seconds)
	sessionsMu  sync.Mutex
	syncer      *bsync.Syncer
	router      *router.Router
	oidc        *oidcAuth
	oauthStates sync.Map // state → expiry (unix seconds), CSRF protection for the OIDC login round-trip
}

// SetRouter sets the router for dashboard chat.
func (s *Server) SetRouter(r *router.Router) {
	s.router = r
}

// New creates a dashboard Server.
func New(b *brain.FileBrain, brainPath string, cfg *config.Config) *Server {
	s := &Server{brain: b, brainPath: brainPath, cfg: cfg}
	s.loadSessions()

	if cfg.Server.SSO.Enabled() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		oa, err := newOIDCAuth(ctx, cfg.Server.SSO)
		if err != nil {
			slog.Warn("sso disabled: failed to initialize oidc provider", "issuer", cfg.Server.SSO.Issuer, "err", err)
		} else {
			s.oidc = oa
		}
	}

	return s
}

func (s *Server) sessionsFile() string {
	return filepath.Join(s.brainPath, ".kai_sessions.json")
}

func (s *Server) loadSessions() {
	data, err := os.ReadFile(s.sessionsFile())
	if err != nil {
		return
	}
	var m map[string]int64
	if err := json.Unmarshal(data, &m); err != nil {
		return
	}
	now := time.Now().Unix()
	for token, expiry := range m {
		if expiry > now {
			s.sessions.Store(token, expiry)
		}
	}
}

func (s *Server) saveSessions() {
	m := map[string]int64{}
	now := time.Now().Unix()
	s.sessions.Range(func(k, v any) bool {
		expiry, _ := v.(int64)
		if expiry > now {
			m[k.(string)] = expiry
		}
		return true
	})
	data, err := json.Marshal(m)
	if err != nil {
		return
	}
	_ = os.WriteFile(s.sessionsFile(), data, 0600)
}

// SetSyncer sets the sync backend for dashboard sync controls.
func (s *Server) SetSyncer(syncer *bsync.Syncer) {
	s.syncer = syncer
}

// serviceStatus represents the status of a service connection.
type serviceStatus struct {
	Name    string `json:"name"`
	Status  string `json:"status"`    // "connected", "disconnected", "not_configured"
	Value   string `json:"value"`     // Primary display value (provider name, repo name, etc.)
	Tooltip string `json:"tooltip"`   // Extended info shown on hover
}

// ToolDef describes an available MCP tool for the dashboard.
type ToolDef struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Params      []ToolParam `json:"params"`
}

// ToolParam describes a tool parameter.
type ToolParam struct {
	Name        string `json:"name"`
	Type        string `json:"type"` // "string", "number", "boolean"
	Required    bool   `json:"required"`
	Description string `json:"description"`
}

// ListenAndServe starts the dashboard as a standalone HTTP server.
func (s *Server) ListenAndServe(addr string) error {
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	return http.ListenAndServe(addr, mux)
}

// RegisterRoutes registers dashboard routes on the given mux at /.
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	// Auth endpoints (no auth required).
	mux.HandleFunc("/api/auth", s.handleAuth)
	mux.HandleFunc("/api/auth/oidc/login", s.handleOIDCLogin)
	mux.HandleFunc("/api/auth/oidc/callback", s.handleOIDCCallback)

	// All other routes require PIN/SSO if configured.
	auth := s.withAuth
	mux.HandleFunc("/", auth(s.handleIndex))
	mux.HandleFunc("/classic", auth(s.handleClassic))
	mux.HandleFunc("/classic/", auth(s.handleClassic))
	mux.HandleFunc("/static/", handleStatic)
	mux.HandleFunc("/api/overview", auth(s.handleOverview))
	mux.HandleFunc("/api/tasks", auth(s.handleTasks))
	mux.HandleFunc("/api/tasks/add", auth(s.handleTasksAdd))
	mux.HandleFunc("/api/activity", auth(s.handleActivity))
	mux.HandleFunc("/api/search", auth(s.handleSearch))
	mux.HandleFunc("/api/info", auth(s.handleInfo))
	mux.HandleFunc("/api/tags", auth(s.handleTags))
	mux.HandleFunc("/api/files", auth(s.handleFiles))
	mux.HandleFunc("/api/file", auth(s.handleFileContent))
	mux.HandleFunc("/api/reorg", auth(s.handleReorg))
	mux.HandleFunc("/api/migrate", auth(s.handleMigrate))
	mux.HandleFunc("/api/decay", auth(s.handleDecay))
	mux.HandleFunc("/api/status", auth(s.handleStatus))
	mux.HandleFunc("/api/tools", auth(s.handleTools))
	mux.HandleFunc("/api/tools/execute", auth(s.handleToolExecute))
	mux.HandleFunc("/api/chat", auth(s.handleChat))
	mux.HandleFunc("/api/chat/stream", auth(s.handleChatStream))
	mux.HandleFunc("/api/import", auth(s.handleImport))
	mux.HandleFunc("/api/sync/push", auth(s.handleSyncPush))
	mux.HandleFunc("/api/sync/pull", auth(s.handleSyncPull))
	mux.HandleFunc("/api/sync/status", auth(s.handleSyncStatus))
	mux.HandleFunc("/api/vectors", auth(s.handleVectors))
	mux.HandleFunc("/api/vectors/build", auth(s.handleVectorsBuild))
	mux.HandleFunc("/api/plugins", auth(s.handlePlugins))
	mux.HandleFunc("/api/plugins/run", auth(s.handlePluginRun))
}

// withAuth wraps a handler with PIN and/or SSO authentication if configured.
func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.Server.DashboardPIN == "" && s.oidc == nil {
			next(w, r)
			return
		}

		// Check token from header or cookie.
		token := r.Header.Get("X-Kai-Token")
		if token == "" {
			if c, err := r.Cookie("kai_session"); err == nil {
				token = c.Value
			}
		}
		if token != "" {
			if expiry, ok := s.sessions.Load(token); ok {
				if expiry.(int64) > time.Now().Unix() {
					next(w, r)
					return
				}
				s.sessions.Delete(token)
			}
		}

		// For API calls, return 401.
		if strings.HasPrefix(r.URL.Path, "/api/") ||
			strings.Contains(r.URL.Path, "/api/") {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}

		// For page requests, serve the lock screen.
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		serveEmbedded(w, "templates/lock.html")
	}
}

// handleAuth handles PIN verification.
func (s *Server) handleAuth(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		// GET: check which auth methods are available/required.
		pinSet := s.cfg.Server.DashboardPIN != ""
		writeJSON(w, map[string]bool{
			"required": pinSet || s.oidc != nil,
			"pin":      pinSet,
			"sso":      s.oidc != nil,
		})
		return
	}

	var req struct {
		PIN string `json:"pin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}

	if req.PIN != s.cfg.Server.DashboardPIN {
		http.Error(w, `{"error":"invalid pin"}`, http.StatusUnauthorized)
		return
	}

	s.startSession(w)
	writeJSON(w, map[string]string{"status": "ok"})
}

// startSession creates a new session token, stores it, and sets the session
// cookie. Used after both PIN and SSO login succeed.
func (s *Server) startSession(w http.ResponseWriter) {
	token := generateToken()
	expiry := time.Now().Add(7 * 24 * time.Hour).Unix()
	s.sessions.Store(token, expiry)
	s.saveSessions()

	http.SetCookie(w, &http.Cookie{
		Name:     "kai_session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   7 * 24 * 3600,
	})
}

func generateToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	serveShell(w)
}

// handleClassic serves the previous dashboard UI until the new one reaches parity.
func (s *Server) handleClassic(w http.ResponseWriter, r *http.Request) {
	serveEmbedded(w, "templates/classic.html")
}

// serveEmbedded serves a raw embedded file as HTML.
func serveEmbedded(w http.ResponseWriter, name string) {
	data, err := templateFS.ReadFile(name)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	type response struct {
		TotalFiles  int            `json:"total_files"`
		Categories  map[string]int `json:"categories"`
		TotalWords  int            `json:"total_words"`
		TotalChunks int            `json:"total_chunks"`
		IndexHealth string         `json:"index_health"`
		UniqueTags  int            `json:"unique_tags"`
		VecProvider string         `json:"vec_provider"`
		VecDims     int            `json:"vec_dims"`
		VecChunks   int            `json:"vec_chunks"`
		VecStatus   string         `json:"vec_status"`
	}

	resp := response{Categories: make(map[string]int)}

	idx, err := s.brain.LoadFullIndexCached()
	if err != nil {
		// Fall back to basic file list.
		files, _ := s.brain.ListAll()
		resp.TotalFiles = len(files)
		resp.IndexHealth = "missing"
		for _, f := range files {
			cat := strings.SplitN(f, "/", 2)[0]
			resp.Categories[cat]++
		}
		writeJSON(w, resp)
		return
	}

	resp.TotalFiles = idx.TotalDocs
	resp.TotalChunks = idx.TotalChunks

	if idx.TotalChunks > 0 {
		resp.IndexHealth = "ok"
	} else {
		resp.IndexHealth = "stale"
	}

	tags := make(map[string]bool)
	for _, doc := range idx.Documents {
		cat := strings.SplitN(doc.Path, "/", 2)[0]
		resp.Categories[cat]++
		resp.TotalWords += doc.WordCount
		for _, t := range doc.Tags {
			tags[t] = true
		}
	}
	resp.UniqueTags = len(tags)

	// Load vector index info.
	if vi, err := s.brain.LoadVecIndexCached(); err == nil && vi != nil {
		resp.VecProvider = vi.Provider
		resp.VecDims = vi.Dims
		resp.VecChunks = len(vi.ChunkVecs)
		resp.VecStatus = "ready"
	} else {
		resp.VecStatus = "none"
	}

	writeJSON(w, resp)
}

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.handleTasksToggle(w, r)
		return
	}
	s.handleTasksGet(w, r)
}

func (s *Server) handleTasksGet(w http.ResponseWriter, r *http.Request) {
	type response struct {
		Items []brain.TaskItem `json:"items"`
	}

	// Prefer today's tasks file; otherwise the most recent DATE-named file.
	// The tasks/ dir may also contain non-dated notes categorized as "tasks"
	// (e.g. planning notes) — those are not daily task lists and must not be
	// picked here, or ParseTasks yields nil and the dashboard shows nothing.
	candidate := brain.TaskFilePath(time.Now())
	if _, err := os.Stat(filepath.Join(s.brainPath, candidate)); err != nil {
		tasksDir := filepath.Join(s.brainPath, "tasks")
		files, _ := filepath.Glob(filepath.Join(tasksDir, "*.md"))
		var dated []string
		for _, f := range files {
			name := strings.TrimSuffix(filepath.Base(f), ".md")
			if _, perr := time.Parse(brain.DateFormat, name); perr == nil {
				dated = append(dated, f)
			}
		}
		if len(dated) == 0 {
			writeJSON(w, response{Items: []brain.TaskItem{}})
			return
		}
		// Sort descending to get the most recent date first.
		sort.Sort(sort.Reverse(sort.StringSlice(dated)))
		candidate = "tasks/" + filepath.Base(dated[0])
	}

	bf, err := s.brain.Load(candidate)
	if err != nil {
		writeJSON(w, response{Items: []brain.TaskItem{}})
		return
	}

	items := brain.ParseTasks(bf.Content)
	if items == nil {
		items = []brain.TaskItem{}
	}
	writeJSON(w, response{Items: items})
}
func (s *Server) handleTasksToggle(w http.ResponseWriter, r *http.Request) {
	type toggleReq struct {
		Index int  `json:"index"`
		Done  bool `json:"done"`
	}
	type response struct {
		Items []brain.TaskItem `json:"items"`
	}

	var req toggleReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	path := brain.TaskFilePath(time.Now())
	bf, err := s.brain.Load(path)
	if err != nil {
		http.Error(w, "no tasks for today", http.StatusNotFound)
		return
	}

	newContent, err := brain.ToggleTask(bf.Content, req.Index, req.Done)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	bf.Content = newContent
	bf.Updated = brain.DateOnly{Time: time.Now()}
	if err := s.brain.Save(bf); err != nil {
		http.Error(w, "failed to save tasks", http.StatusInternalServerError)
		return
	}

	writeJSON(w, response{Items: brain.ParseTasks(bf.Content)})
}

func (s *Server) handleTasksAdd(w http.ResponseWriter, r *http.Request) {
	type addReq struct {
		Text     string `json:"text"`
		Priority string `json:"priority"`
	}
	type response struct {
		Items []brain.TaskItem `json:"items"`
	}

	var req addReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Text == "" {
		http.Error(w, "invalid request: text is required", http.StatusBadRequest)
		return
	}

	path := brain.TaskFilePath(time.Now())
	bf, err := s.brain.Load(path)
	if err != nil {
		bf = brain.NewTaskFile(time.Now())
	}

	bf.Content = brain.AppendTask(bf.Content, req.Text, req.Priority)
	bf.Updated = brain.DateOnly{Time: time.Now()}
	if err := s.brain.Save(bf); err != nil {
		http.Error(w, "failed to save tasks", http.StatusInternalServerError)
		return
	}

	writeJSON(w, response{Items: brain.ParseTasks(bf.Content)})
}

func (s *Server) handleActivity(w http.ResponseWriter, r *http.Request) {
	type activityEntry struct {
		Path        string `json:"path"`
		WordCount   int    `json:"word_count"`
		AccessCount int    `json:"access_count"`
		Updated     string `json:"updated"`
	}
	type response struct {
		Recent []activityEntry `json:"recent"`
	}

	idx, err := s.brain.LoadFullIndexCached()
	if err != nil {
		writeJSON(w, response{Recent: []activityEntry{}})
		return
	}

	entries := make([]activityEntry, 0, len(idx.Documents))
	for _, doc := range idx.Documents {
		entries = append(entries, activityEntry{
			Path:        doc.Path,
			WordCount:   doc.WordCount,
			AccessCount: doc.AccessCount,
			Updated:     doc.Updated,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Updated == entries[j].Updated {
			return entries[i].AccessCount > entries[j].AccessCount
		}
		return entries[i].Updated > entries[j].Updated
	})

	if len(entries) > 20 {
		entries = entries[:20]
	}

	writeJSON(w, response{Recent: entries})
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	type searchResult struct {
		DocPath string  `json:"doc_path"`
		ChunkID int     `json:"chunk_id"`
		Score   float64 `json:"score"`
		Snippet string  `json:"snippet"`
	}
	type response struct {
		Query   string         `json:"query"`
		Results []searchResult `json:"results"`
	}

	q := r.URL.Query().Get("q")
	if q == "" {
		writeJSON(w, response{Query: q, Results: []searchResult{}})
		return
	}

	chunks := s.brain.QueryWithPRF(q)

	var results []searchResult
	seen := make(map[string]int) // doc path → chunk count
	for _, cr := range chunks {
		if seen[cr.DocPath] >= 2 {
			continue // max 2 chunks per file
		}
		if len(results) >= 20 {
			break
		}

		snippet := ""
		bf, err := s.brain.Load(cr.DocPath)
		if err == nil {
			content := strings.TrimSpace(bf.Content)
			snippet = extractSnippet(content, cr.Offset, cr.Length, 200)
		}

		results = append(results, searchResult{
			DocPath: cr.DocPath,
			ChunkID: cr.ChunkID,
			Score:   cr.Score,
			Snippet: snippet,
		})
		seen[cr.DocPath]++
	}

	writeJSON(w, response{Query: q, Results: results})
}

func extractSnippet(content string, offset, length, maxLen int) string {
	if offset >= len(content) {
		if len(content) > maxLen {
			return content[:maxLen] + "..."
		}
		return content
	}
	end := offset + length
	if end > len(content) {
		end = len(content)
	}
	s := strings.TrimSpace(content[offset:end])
	if len(s) > maxLen {
		s = s[:maxLen] + "..."
	}
	return s
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	type response struct {
		AgentName  string `json:"agent_name"`
		UserName   string `json:"user_name"`
		Version    string `json:"version"`
		Commit     string `json:"commit"`
		BuildDate  string `json:"build_date"`
		ListenAddr string `json:"listen_addr"`
	}

	writeJSON(w, response{
		AgentName:  s.cfg.AgentName(),
		UserName:   s.cfg.UserName(),
		Version:    kai.Version,
		Commit:     kai.Commit,
		BuildDate:  kai.BuildDate,
		ListenAddr: s.cfg.Server.ListenAddr,
	})
}

func (s *Server) handleTags(w http.ResponseWriter, r *http.Request) {
	type tagEntry struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	type response struct {
		Tags []tagEntry `json:"tags"`
	}

	idx, err := s.brain.LoadFullIndexCached()
	if err != nil {
		writeJSON(w, response{Tags: []tagEntry{}})
		return
	}

	counts := make(map[string]int)
	for _, doc := range idx.Documents {
		for _, t := range doc.Tags {
			counts[t]++
		}
	}

	tags := make([]tagEntry, 0, len(counts))
	for name, count := range counts {
		tags = append(tags, tagEntry{Name: name, Count: count})
	}
	sort.Slice(tags, func(i, j int) bool { return tags[i].Count > tags[j].Count })

	if len(tags) > 50 {
		tags = tags[:50]
	}

	writeJSON(w, response{Tags: tags})
}

func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	type fileEntry struct {
		Path        string   `json:"path"`
		Category    string   `json:"category"`
		Tags        []string `json:"tags"`
		Confidence  string   `json:"confidence"`
		WordCount   int      `json:"word_count"`
		AccessCount int      `json:"access_count"`
		Updated     string   `json:"updated"`
	}
	type response struct {
		Files []fileEntry `json:"files"`
	}

	// Fast path: use cached index (no per-file disk reads).
	idx, _ := s.brain.LoadFullIndexCached()

	var entries []fileEntry
	if idx != nil && len(idx.Documents) > 0 {
		entries = make([]fileEntry, 0, len(idx.Documents))
		for _, doc := range idx.Documents {
			cat := strings.SplitN(doc.Path, "/", 2)[0]
			tags := doc.Tags
			if tags == nil {
				tags = []string{}
			}
			entries = append(entries, fileEntry{
				Path:        doc.Path,
				Category:    cat,
				Tags:        tags,
				Confidence:  doc.Confidence,
				WordCount:   doc.WordCount,
				AccessCount: doc.AccessCount,
				Updated:     doc.Updated,
			})
		}
	} else {
		// Fallback: no index available, list files and load individually.
		files, err := s.brain.ListAll()
		if err != nil {
			writeJSON(w, response{Files: []fileEntry{}})
			return
		}
		entries = make([]fileEntry, 0, len(files))
		for _, f := range files {
			cat := strings.SplitN(f, "/", 2)[0]
			entry := fileEntry{Path: f, Category: cat}
			bf, err := s.brain.Load(f)
			if err == nil {
				entry.Tags = bf.Tags
				entry.Confidence = bf.Confidence
			}
			if entry.Tags == nil {
				entry.Tags = []string{}
			}
			entries = append(entries, entry)
		}
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	writeJSON(w, response{Files: entries})
}

func (s *Server) handleFileContent(w http.ResponseWriter, r *http.Request) {
	type response struct {
		Path       string   `json:"path"`
		Tags       []string `json:"tags"`
		Confidence string   `json:"confidence"`
		Content    string   `json:"content"`
		Updated    string   `json:"updated"`
	}

	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "missing path parameter", http.StatusBadRequest)
		return
	}

	bf, err := s.brain.Load(path)
	if err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	writeJSON(w, response{
		Path:       bf.Path,
		Tags:       bf.Tags,
		Confidence: bf.Confidence,
		Content:    strings.TrimSpace(bf.Content),
		Updated:    bf.Updated.Time.Format(brain.DateFormat),
	})
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse multipart form for file uploads
	if err := r.ParseMultipartForm(32 << 20); err != nil { // 32MB max
		writeJSON(w, map[string]interface{}{"error": "failed to parse upload"})
		return
	}

	category := r.FormValue("category")
	if category == "" {
		category = "knowledge"
	}
	tagsStr := r.FormValue("tags")
	confidence := r.FormValue("confidence")
	if confidence == "" {
		confidence = "medium"
	}
	autoTag := r.FormValue("autoTag") == "true"
	autoChunk := r.FormValue("autoChunk") == "true"

	var tags []string
	if tagsStr != "" {
		for _, t := range strings.Split(tagsStr, ",") {
			if t = strings.TrimSpace(t); t != "" {
				tags = append(tags, t)
			}
		}
	}

	type importResult struct {
		Path  string `json:"path"`
		Error string `json:"error,omitempty"`
	}
	var results []importResult

	for _, fileHeader := range r.MultipartForm.File["files"] {
		file, err := fileHeader.Open()
		if err != nil {
			results = append(results, importResult{
				Path:  fileHeader.Filename,
				Error: "failed to open file",
			})
			continue
		}
		defer file.Close()

		data := make([]byte, fileHeader.Size)
		if _, err := file.Read(data); err != nil {
			results = append(results, importResult{
				Path:  fileHeader.Filename,
				Error: "failed to read file",
			})
			continue
		}

		bf, err := importFileData(fileHeader.Filename, data, category, tags, confidence, autoTag, autoChunk)
		if err != nil {
			results = append(results, importResult{
				Path:  fileHeader.Filename,
				Error: err.Error(),
			})
			continue
		}

		if err := s.brain.Save(bf); err != nil {
			results = append(results, importResult{
				Path:  fileHeader.Filename,
				Error: "failed to save: " + err.Error(),
			})
			continue
		}

		results = append(results, importResult{Path: bf.Path})
	}

	// Rebuild indexes
	if len(results) > 0 {
		_ = s.brain.RebuildIndex()
	}

	writeJSON(w, map[string]interface{}{
		"results": results,
		"count":   len(results),
	})
}

func (s *Server) handleReorg(w http.ResponseWriter, r *http.Request) {
	type response struct {
		Actions []brain.ReorgAction `json:"actions"`
		Count   int                 `json:"count"`
	}

	opts := brain.ReorgOptions{
		Mode:                brain.ModeAll,
		SimilarityThreshold: 0.7,
		SmallFileThreshold:  50,
		DryRun:              true,
	}

	plan, err := s.brain.Reorganize(opts)
	if err != nil || plan == nil {
		writeJSON(w, response{Actions: []brain.ReorgAction{}, Count: 0})
		return
	}

	actions := plan.Actions
	if actions == nil {
		actions = []brain.ReorgAction{}
	}

	writeJSON(w, response{Actions: actions, Count: len(actions)})
}

func (s *Server) handleMigrate(w http.ResponseWriter, r *http.Request) {
	apply := r.URL.Query().Get("apply") == "true"
	if r.Method == "POST" {
		var body struct {
			Apply bool `json:"apply"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		apply = apply || body.Apply
	}

	type actionResult struct {
		brain.ReorgAction
		Confidence float64 `json:"confidence"`
		LowConf    bool    `json:"low_confidence"`
	}
	type response struct {
		Actions []actionResult `json:"actions"`
		Count   int            `json:"count"`
		Applied bool           `json:"applied"`
	}

	opts := brain.DefaultMigrateOptions()
	opts.DryRun = !apply

	plan, err := s.brain.Migrate(opts)
	if err != nil || plan == nil {
		writeJSON(w, response{Actions: []actionResult{}, Count: 0, Applied: apply})
		return
	}

	var results []actionResult
	for _, a := range plan.Actions {
		results = append(results, actionResult{
			ReorgAction: a,
			Confidence:  a.Similarity,
			LowConf:     a.Similarity < opts.LowConfidenceThreshold,
		})
	}
	if results == nil {
		results = []actionResult{}
	}
	writeJSON(w, response{Actions: results, Count: len(results), Applied: apply})
}

func (s *Server) handleDecay(w http.ResponseWriter, r *http.Request) {
	apply := r.URL.Query().Get("apply") == "true"
	if r.Method == "POST" {
		var body struct {
			Apply bool `json:"apply"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		apply = apply || body.Apply
	}

	results, err := s.brain.Decay(!apply)
	if err != nil {
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	if results == nil {
		results = []brain.DecayResult{}
	}
	writeJSON(w, map[string]interface{}{
		"results": results,
		"count":   len(results),
		"applied": apply,
	})
}

func (s *Server) handleTools(w http.ResponseWriter, r *http.Request) {
	tools := s.getToolDefs()
	writeJSON(w, map[string]interface{}{"tools": tools})
}

func (s *Server) handleToolExecute(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Tool   string            `json:"tool"`
		Params map[string]string `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, map[string]string{"error": "bad request"})
		return
	}

	result, err := s.executeTool(req.Tool, req.Params)
	if err != nil {
		writeJSON(w, map[string]interface{}{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]interface{}{"result": result})
}

// getToolDefs returns the list of available tools with their parameters.
func (s *Server) getToolDefs() []ToolDef {
	return []ToolDef{
		{Name: "brain_list", Description: "List all brain knowledge files", Params: nil},
		{Name: "brain_read", Description: "Read a brain file", Params: []ToolParam{
			{Name: "path", Type: "string", Required: true, Description: "File path (e.g. opinions/testing.md)"},
		}},
		{Name: "brain_search", Description: "Search brain by keywords", Params: []ToolParam{
			{Name: "query", Type: "string", Required: true, Description: "Search keywords"},
		}},
		{Name: "brain_write", Description: "Write a brain file", Params: []ToolParam{
			{Name: "path", Type: "string", Required: true, Description: "File path"},
			{Name: "content", Type: "string", Required: true, Description: "Content to write"},
			{Name: "tags", Type: "string", Required: true, Description: "Comma-separated tags"},
			{Name: "confidence", Type: "string", Required: false, Description: "high, medium, or low"},
		}},
		{Name: "ask", Description: "Ask a question using brain context", Params: []ToolParam{
			{Name: "question", Type: "string", Required: true, Description: "The question to ask"},
		}},
		{Name: "brain_reorganize", Description: "Analyze brain for reorganization", Params: []ToolParam{
			{Name: "mode", Type: "string", Required: false, Description: "all, dedup, recategorize, or consolidate"},
			{Name: "apply", Type: "boolean", Required: false, Description: "Execute plan (default: false)"},
		}},
		{Name: "brain_migrate", Description: "Move brain files from invalid categories into valid ones", Params: []ToolParam{
			{Name: "apply", Type: "boolean", Required: false, Description: "Execute migration (default: false = dry-run)"},
		}},
		{Name: "brain_decay", Description: "Lower confidence of stale brain files (high→medium after 180d, medium→low after 365d)", Params: []ToolParam{
			{Name: "apply", Type: "boolean", Required: false, Description: "Apply decay (default: false = dry-run)"},
		}},
	}
}

// executeTool runs a tool and returns the result as a string.
func (s *Server) executeTool(toolName string, params map[string]string) (string, error) {
	switch toolName {
	case "brain_list":
		files, err := s.brain.ListAll()
		if err != nil {
			return "", err
		}
		data, _ := json.MarshalIndent(files, "", "  ")
		return string(data), nil

	case "brain_read":
		path := params["path"]
		if path == "" {
			return "", fmt.Errorf("path is required")
		}
		bf, err := s.brain.Load(path)
		if err != nil {
			return "", err
		}
		type readResult struct {
			Path       string   `json:"path"`
			Tags       []string `json:"tags"`
			Confidence string   `json:"confidence"`
			Content    string   `json:"content"`
		}
		data, _ := json.MarshalIndent(readResult{
			Path: bf.Path, Tags: bf.Tags, Confidence: bf.Confidence,
			Content: strings.TrimSpace(bf.Content),
		}, "", "  ")
		return string(data), nil

	case "brain_search":
		query := params["query"]
		if query == "" {
			return "", fmt.Errorf("query is required")
		}
		chunks := s.brain.QueryHybrid(query)
		type searchResult struct {
			Path    string  `json:"path"`
			ChunkID int     `json:"chunk_id"`
			Score   float64 `json:"score"`
		}
		var results []searchResult
		for i, cr := range chunks {
			if i >= 10 {
				break
			}
			results = append(results, searchResult{Path: cr.DocPath, ChunkID: cr.ChunkID, Score: cr.Score})
		}
		data, _ := json.MarshalIndent(results, "", "  ")
		return string(data), nil

	case "brain_write":
		path := params["path"]
		content := params["content"]
		tags := params["tags"]
		if path == "" || content == "" || tags == "" {
			return "", fmt.Errorf("path, content, and tags are required")
		}
		confidence := params["confidence"]
		if confidence == "" {
			confidence = "medium"
		}
		tagList := strings.Split(tags, ",")
		for i := range tagList {
			tagList[i] = strings.TrimSpace(tagList[i])
		}
		bf := &brain.BrainFile{
			Path:       path,
			Tags:       tagList,
			Confidence: confidence,
			Source:     "dashboard",
			Content:    "\n" + content + "\n",
		}
		if err := s.brain.Save(bf); err != nil {
			return "", err
		}
		if err := s.brain.RebuildIndex(); err != nil {
			return "", fmt.Errorf("saved but index rebuild failed: %w", err)
		}
		return fmt.Sprintf("Saved %s", path), nil

	case "ask":
		question := params["question"]
		if question == "" {
			return "", fmt.Errorf("question is required")
		}
		chunks := s.brain.QueryHybrid(question)
		var sb strings.Builder
		seen := make(map[string]bool)
		count := 0
		for _, cr := range chunks {
			if count >= 3 || seen[cr.DocPath] {
				continue
			}
			seen[cr.DocPath] = true
			bf, err := s.brain.Load(cr.DocPath)
			if err != nil {
				continue
			}
			content := strings.TrimSpace(bf.Content)
			if cr.Offset < len(content) {
				end := cr.Offset + cr.Length
				if end > len(content) {
					end = len(content)
				}
				content = strings.TrimSpace(content[cr.Offset:end])
			}
			if sb.Len() > 0 {
				sb.WriteString("\n\n")
			}
			sb.WriteString("## ")
			sb.WriteString(cr.DocPath)
			sb.WriteString("\n")
			sb.WriteString(content)
			count++
		}
		if sb.Len() == 0 {
			return "No relevant brain files found.", nil
		}
		return sb.String(), nil

	case "brain_reorganize":
		opts := brain.DefaultReorgOptions()
		opts.DryRun = params["apply"] != "true"
		if m := params["mode"]; m != "" {
			opts.Mode = m
		}
		plan, err := s.brain.Reorganize(opts)
		if err != nil {
			return "", err
		}
		if plan == nil || len(plan.Actions) == 0 {
			return "No reorganization needed.", nil
		}
		data, _ := json.MarshalIndent(plan.Actions, "", "  ")
		return string(data), nil

	case "brain_migrate":
		opts := brain.DefaultMigrateOptions()
		opts.DryRun = params["apply"] != "true"
		plan, err := s.brain.Migrate(opts)
		if err != nil {
			return "", err
		}
		if plan == nil || len(plan.Actions) == 0 {
			return "All brain files are already in valid categories.", nil
		}
		data, _ := json.MarshalIndent(plan.Actions, "", "  ")
		return string(data), nil

	case "brain_decay":
		apply := params["apply"] == "true"
		results, err := s.brain.Decay(!apply)
		if err != nil {
			return "", err
		}
		if len(results) == 0 {
			return "No stale brain files found.", nil
		}
		data, _ := json.MarshalIndent(results, "", "  ")
		return string(data), nil

	default:
		return "", fmt.Errorf("unknown tool: %s", toolName)
	}
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Message string `json:"message"`
		Mode    string `json:"mode"` // "brain" (default) or "free"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Message == "" {
		writeJSON(w, map[string]string{"error": "message is required"})
		return
	}
	if req.Mode == "" {
		req.Mode = "brain"
	}

	// Free chat: direct LLM call, no brain context.
	if req.Mode == "free" {
		if s.router == nil {
			writeJSON(w, map[string]string{"error": "no LLM provider configured"})
			return
		}
		result, err := s.router.FreeChat(r.Context(), req.Message)
		if err != nil {
			writeJSON(w, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, map[string]interface{}{"response": result.Response})
		return
	}

	// Brain chat: retrieve context + LLM.
	if s.router != nil {
		result, err := s.router.Ask(r.Context(), req.Message)
		if err != nil {
			writeJSON(w, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, map[string]interface{}{
			"response":         result.Response,
			"confidence":       result.Confidence,
			"brain_files_used": result.BrainFilesUsed,
		})
		return
	}

	// Without LLM: return brain search results as context.
	chunks := s.brain.QueryHybrid(req.Message)
	var sb strings.Builder
	seen := make(map[string]bool)
	count := 0
	for _, cr := range chunks {
		if count >= 3 || seen[cr.DocPath] {
			continue
		}
		seen[cr.DocPath] = true
		bf, err := s.brain.Load(cr.DocPath)
		if err != nil {
			continue
		}
		content := strings.TrimSpace(bf.Content)
		if cr.Offset < len(content) {
			end := cr.Offset + cr.Length
			if end > len(content) {
				end = len(content)
			}
			content = strings.TrimSpace(content[cr.Offset:end])
		}
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString("## ")
		sb.WriteString(cr.DocPath)
		sb.WriteString("\n")
		sb.WriteString(content)
		count++
	}
	if sb.Len() == 0 {
		writeJSON(w, map[string]string{"response": "No relevant brain files found."})
		return
	}
	writeJSON(w, map[string]interface{}{
		"response":   sb.String(),
		"confidence": "context_only",
	})
}

func (s *Server) handleChatStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Message string `json:"message"`
		Mode    string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Message == "" {
		http.Error(w, `{"error":"message is required"}`, http.StatusBadRequest)
		return
	}
	if req.Mode == "" {
		req.Mode = "brain"
	}

	if s.router == nil {
		http.Error(w, `{"error":"no LLM provider configured"}`, http.StatusServiceUnavailable)
		return
	}

	// Set SSE headers.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	var brainPaths []string

	if req.Mode == "free" {
		ch, err := s.router.StreamFreeChat(r.Context(), req.Message)
		if err != nil {
			fmt.Fprintf(w, "data: {\"error\":%q}\n\n", err.Error())
			flusher.Flush()
			return
		}
		for chunk := range ch {
			if chunk.Done {
				break
			}
			data, _ := json.Marshal(map[string]string{"content": chunk.Content})
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	} else {
		ch, paths, err := s.router.StreamAsk(r.Context(), req.Message)
		if err != nil {
			fmt.Fprintf(w, "data: {\"error\":%q}\n\n", err.Error())
			flusher.Flush()
			return
		}
		brainPaths = paths
		for chunk := range ch {
			if chunk.Done {
				break
			}
			data, _ := json.Marshal(map[string]string{"content": chunk.Content})
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}

	// Send final event with metadata.
	done, _ := json.Marshal(map[string]interface{}{
		"done":             true,
		"brain_files_used": brainPaths,
	})
	fmt.Fprintf(w, "data: %s\n\n", done)
	flusher.Flush()
}

func (s *Server) handleSyncPush(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.syncer == nil || !s.syncer.HasBackends() {
		writeJSON(w, map[string]string{"error": "no sync backends configured"})
		return
	}
	if err := s.syncer.Push(r.Context(), false, false); err != nil {
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]string{"status": "pushed"})
}

func (s *Server) handleSyncPull(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.syncer == nil || !s.syncer.HasBackends() {
		writeJSON(w, map[string]string{"error": "no sync backends configured"})
		return
	}
	if err := s.syncer.Pull(r.Context(), false, false); err != nil {
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	// Rebuild index after pull.
	if err := s.brain.RebuildIndex(); err != nil {
		writeJSON(w, map[string]string{"status": "pulled", "warning": "index rebuild failed: " + err.Error()})
		return
	}
	writeJSON(w, map[string]string{"status": "pulled"})
}

func (s *Server) handleSyncStatus(w http.ResponseWriter, r *http.Request) {
	if s.syncer == nil || !s.syncer.HasBackends() {
		writeJSON(w, map[string]interface{}{"configured": false})
		return
	}
	gitStatus, cloudStatus, err := s.syncer.StatusAll(r.Context())
	if err != nil {
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	resp := map[string]interface{}{"configured": true}
	if gitStatus != nil {
		resp["git"] = gitStatus
	}
	if cloudStatus != nil {
		resp["cloud"] = cloudStatus
	}
	writeJSON(w, resp)
}

// extractRepoName extracts the bare repo name from a git URL
// Examples: "git@github.com:user/kai-brain.git" -> "kai-brain"
//          "https://github.com/user/kai-brain.git" -> "kai-brain"
func extractRepoName(gitURL string) string {
	// Remove .git suffix
	name := strings.TrimSuffix(gitURL, ".git")
	// Get the last path segment
	parts := strings.Split(name, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return "repo"
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	type response struct {
		Services []serviceStatus `json:"services"`
	}

	var services []serviceStatus

	// 1. LLM Provider status
	provName := s.cfg.Provider
	if provName == "" {
		services = append(services, serviceStatus{
			Name: "LLM Provider", Status: "not_configured", Value: "—", Tooltip: "No provider configured",
		})
	} else {
		provCfg, ok := s.cfg.Providers[provName]
		if !ok {
			services = append(services, serviceStatus{
				Name: "LLM Provider", Status: "disconnected", Value: provName, Tooltip: "Provider not found in config",
			})
		} else {
			tooltip := provCfg.Model
			if provCfg.BaseURL != "" {
				tooltip += " @ " + provCfg.BaseURL
			}
			if provCfg.APIKey == "" {
				tooltip += " (no API key)"
			}
			status := "connected"
			if provCfg.APIKey == "" {
				status = "disconnected"
			}
			services = append(services, serviceStatus{
				Name: "LLM Provider", Status: status, Value: provName, Tooltip: tooltip,
			})
		}
	}

	// 2. Embeddings status
	embedProvider := s.cfg.Brain.Embeddings.Provider
	if embedProvider == "" {
		embedProvider = "auto"
	}

	vi, viErr := s.brain.LoadVecIndexCached()
	if viErr != nil {
		// No vector index yet — report configured provider without network probing.
		if embedProvider == "ollama" {
			ollamaURL := s.cfg.Brain.Embeddings.Ollama.URL
			if ollamaURL == "" {
				ollamaURL = brain.DefaultOllamaURL
			}
			model := s.cfg.Brain.Embeddings.Ollama.Model
			if model == "" {
				model = brain.DefaultEmbedModel
			}
			services = append(services, serviceStatus{
				Name: "Embeddings", Status: "not_configured",
				Value: "Ollama", Tooltip: model + " @ " + ollamaURL + " (index not built — run reindex)",
			})
		} else {
			value := "LSI"
			if embedProvider == "auto" {
				value = "auto"
			}
			services = append(services, serviceStatus{
				Name: "Embeddings", Status: "not_configured", Value: value, Tooltip: "No vector index yet — run reindex",
			})
		}
	} else {
		tooltip := fmt.Sprintf("%s: %d dims, %d chunks", vi.Provider, vi.Dims, len(vi.ChunkVecs))
		services = append(services, serviceStatus{
			Name: "Embeddings", Status: "connected", Value: vi.Provider, Tooltip: tooltip,
		})
	}

	// 3. Brain Sync status
	if s.cfg.Brain.Sync.Git.Enabled {
		repoName := "git"
		tooltip := "Git sync enabled"
		if s.cfg.Brain.Sync.Git.Remote != "" {
			repoName = extractRepoName(s.cfg.Brain.Sync.Git.Remote)
			tooltip = s.cfg.Brain.Sync.Git.Remote
		}
		services = append(services, serviceStatus{
			Name: "Brain Sync", Status: "connected", Value: repoName, Tooltip: tooltip,
		})
	} else {
		services = append(services, serviceStatus{
			Name: "Brain Sync", Status: "not_configured", Value: "—", Tooltip: "Git sync not enabled",
		})
	}

	writeJSON(w, response{Services: services})
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func importFileData(filename string, data []byte, category string, tags []string, confidence string, autoTag bool, autoChunk bool) (*brain.BrainFile, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	baseFileName := strings.TrimSuffix(filename, ext)

	var content string
	switch ext {
	case ".md":
		content = string(data)

	case ".csv":
		// Parse CSV and convert to markdown table
		records, err := csv.NewReader(strings.NewReader(string(data))).ReadAll()
		if err != nil {
			return nil, fmt.Errorf("parse CSV: %w", err)
		}

		if len(records) == 0 {
			return nil, fmt.Errorf("CSV is empty")
		}

		// Build markdown table with proper escaping
		content = brain.BuildMarkdownTable(records)

	case ".xlsx":
		// Parse Excel file and convert to markdown tables
		f, err := excelize.OpenReader(io.NopCloser(strings.NewReader(string(data))))
		if err != nil {
			return nil, fmt.Errorf("open Excel file: %w", err)
		}
		defer f.Close()

		sheetNames := f.GetSheetList()
		if len(sheetNames) == 0 {
			return nil, fmt.Errorf("Excel file has no sheets")
		}

		// Get all rows from all sheets
		var buf strings.Builder
		for i, sheetName := range sheetNames {
			if i > 0 {
				buf.WriteString("\n\n")
			}
			if len(sheetNames) > 1 {
				buf.WriteString("## " + sheetName + "\n\n")
			}

			rows, err := f.GetRows(sheetName)
			if err != nil {
				return nil, fmt.Errorf("read sheet %s: %w", sheetName, err)
			}

			if len(rows) == 0 {
				continue
			}

			// Build markdown table with proper escaping
			if len(rows[0]) > 0 {
				buf.WriteString(brain.BuildMarkdownTable(rows))
			}
		}
		content = buf.String()

	default:
		return nil, fmt.Errorf("unsupported file type: %s (only .md, .csv, and .xlsx supported)", ext)
	}

	// Construct brain file path
	brainPath := filepath.Join(category, baseFileName+".md")
	brainPath = filepath.Clean(strings.ReplaceAll(brainPath, "\\", "/"))

	// Auto-extract tags if enabled
	finalTags := tags
	if autoTag {
		extractedTags := brain.ExtractTags(content)
		// Merge extracted tags with provided tags
		seen := make(map[string]bool)
		for _, t := range tags {
			seen[t] = true
		}
		for _, t := range extractedTags {
			if !seen[t] {
				finalTags = append(finalTags, t)
				seen[t] = true
			}
		}
	}

	return &brain.BrainFile{
		Path:       brainPath,
		Content:    "\n" + strings.TrimSpace(content) + "\n",
		Tags:       finalTags,
		Confidence: confidence,
		Source:     "imported:" + filename,
		Updated:    brain.DateOnly{Time: time.Now()},
	}, nil
}

func (s *Server) handlePlugins(w http.ResponseWriter, r *http.Request) {
	reg := module.Default()
	available := reg.Available()
	sort.Strings(available)

	enabled := make(map[string]bool, len(s.cfg.Modules))
	for _, mc := range s.cfg.Modules {
		if mc.Enabled {
			enabled[mc.Name] = true
		}
	}

	type pluginEntry struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Enabled     bool   `json:"enabled"`
	}
	entries := make([]pluginEntry, 0, len(available))
	for _, name := range available {
		entries = append(entries, pluginEntry{
			Name:        name,
			Description: reg.Description(name),
			Enabled:     enabled[name],
		})
	}
	writeJSON(w, map[string]interface{}{"plugins": entries})
}

func (s *Server) handlePluginRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, map[string]string{"error": "bad request"})
		return
	}
	if req.Name == "" {
		writeJSON(w, map[string]string{"error": "name is required"})
		return
	}

	reg := module.Default()

	var modCfg map[string]interface{}
	for _, mc := range s.cfg.Modules {
		if mc.Name == req.Name {
			modCfg = mc.Config
			break
		}
	}

	mod, err := reg.Build(req.Name, modCfg)
	if err != nil {
		writeJSON(w, map[string]interface{}{"error": err.Error()})
		return
	}

	learnings, err := mod.Fetch(context.Background())
	if err != nil {
		writeJSON(w, map[string]interface{}{"error": fmt.Sprintf("fetch: %v", err)})
		return
	}

	if len(learnings) == 0 {
		writeJSON(w, map[string]interface{}{"name": req.Name, "imported": 0, "message": "nothing fetched"})
		return
	}

	if err := s.brain.Learn(learnings); err != nil {
		writeJSON(w, map[string]interface{}{"error": fmt.Sprintf("save: %v", err)})
		return
	}
	if err := s.brain.RebuildTagIndex(); err != nil {
		writeJSON(w, map[string]interface{}{"error": fmt.Sprintf("index: %v", err)})
		return
	}

	writeJSON(w, map[string]interface{}{"name": req.Name, "imported": len(learnings)})
}
