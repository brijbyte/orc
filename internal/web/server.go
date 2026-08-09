package web

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"crypto/subtle"
	"crypto/tls"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/brijbyte/orc/internal/agent"
	"github.com/brijbyte/orc/internal/config"
	"github.com/brijbyte/orc/internal/provider"
	"github.com/brijbyte/orc/internal/session"
	"github.com/brijbyte/orc/internal/ui"
	"github.com/google/uuid"
	"golang.org/x/crypto/acme/autocert"
)

//go:embed all:dist
var distFS embed.FS

var hlCSS = sync.OnceValue(ui.HighlightCSS)

const placeholder = `<!doctype html><meta charset="utf-8"><title>orc</title>
<body style="font-family:monospace;background:#111;color:#ddd;padding:2em">
🧌 orc: web UI not built — run <b>make web</b> and rebuild.</body>`

// Server manages live session runtimes and exposes them over HTTP.
type Server struct {
	Addr   string
	Domain string // non-empty: TLS via Let's Encrypt on :443 (+ :80 challenge)

	prov     provider.Provider
	base     config.Config // template for new sessions (model/effort/cwd)
	token    string
	mu       sync.Mutex
	openMu   sync.Mutex // serializes open: one runtime per session file
	runtimes map[string]*Runtime
	http     *http.Server
}

func NewServer(prov provider.Provider, base *config.Config, addr, domain string) *Server {
	buf := make([]byte, 16)
	rand.Read(buf)
	return &Server{
		Addr: addr, Domain: domain, prov: prov, base: *base,
		token:    hex.EncodeToString(buf),
		runtimes: map[string]*Runtime{},
	}
}

// Register adds an already-built runtime (the initial --serve session).
func (s *Server) Register(rt *Runtime) {
	s.mu.Lock()
	s.runtimes[rt.ID] = rt
	s.mu.Unlock()
}

// reap drops runtimes whose loop ended (user typed quit).
func (s *Server) reap() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, rt := range s.runtimes {
		select {
		case <-rt.done:
			delete(s.runtimes, id)
		default:
		}
	}
}

// runtime finds a live runtime by handle or current session id (prefix ok).
func (s *Server) runtime(id string) *Runtime {
	s.reap()
	s.mu.Lock()
	defer s.mu.Unlock()
	if rt, ok := s.runtimes[id]; ok {
		return rt
	}
	for _, rt := range s.runtimes {
		if strings.HasPrefix(rt.ID, id) || strings.HasPrefix(rt.Cfg.SessionID, id) {
			return rt
		}
	}
	return nil
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		got := r.URL.Query().Get("token")
		if h := r.Header.Get("Authorization"); got == "" {
			got = strings.TrimPrefix(h, "Bearer ")
		}
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) != 1 {
			http.Error(rw, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(rw, r)
	}
}

func writeJSON(rw http.ResponseWriter, v any) {
	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(v)
}

// withRuntime resolves {id} to a live runtime or 404s.
func (s *Server) withRuntime(next func(http.ResponseWriter, *http.Request, *Runtime)) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		rt := s.runtime(r.PathValue("id"))
		if rt == nil {
			http.Error(rw, "no such live session", http.StatusNotFound)
			return
		}
		next(rw, r, rt)
	}
}

// sessionRow is one /api/sessions entry.
type sessionRow struct {
	ID    string `json:"id"`
	Rid   string `json:"rid,omitempty"` // live runtime handle
	Title string `json:"title"`
	When  string `json:"when"`
	Cwd   string `json:"cwd"`
	Live  bool   `json:"live"`
	Busy  bool   `json:"busy"`
}

func (s *Server) handleSessions(rw http.ResponseWriter, r *http.Request) {
	s.reap()
	rows, _ := session.ListAll()
	s.mu.Lock()
	live := map[string]*Runtime{}
	for _, rt := range s.runtimes {
		live[rt.Cfg.SessionID] = rt
	}
	s.mu.Unlock()

	out := make([]sessionRow, 0, len(rows))
	for _, row := range rows {
		sr := sessionRow{ID: row.ID, Title: row.Title, When: row.When, Cwd: row.Cwd}
		if rt, ok := live[row.ID]; ok {
			sr.Live, sr.Rid, sr.Busy = true, rt.ID, rt.IO.Busy()
		}
		out = append(out, sr)
	}
	home, _ := os.UserHomeDir()
	writeJSON(rw, map[string]any{"cwd": s.base.Cwd, "home": home, "sessions": out})
}

// handleNew starts a fresh session, optionally in another directory.
func (s *Server) handleNew(rw http.ResponseWriter, r *http.Request) {
	var in struct {
		Cwd string `json:"cwd"`
	}
	json.NewDecoder(r.Body).Decode(&in)
	cfg := s.base
	cfg.SessionID = uuid.NewString()
	cfg.Instructions = ""
	if in.Cwd != "" {
		dir := config.ExpandHome(in.Cwd)
		st, err := os.Stat(dir)
		if err != nil || !st.IsDir() || !filepath.IsAbs(dir) {
			http.Error(rw, "not a directory", http.StatusBadRequest)
			return
		}
		cfg.Cwd = dir
	}
	sess, err := session.New(&cfg)
	if err != nil {
		http.Error(rw, "cannot create session file", http.StatusInternalServerError)
		return
	}
	rt := NewRuntime(s.prov, &cfg, sess, nil, false)
	s.Register(rt)
	writeJSON(rw, map[string]string{"id": rt.ID})
}

// handleOpen resumes a session from disk (or returns the live runtime).
func (s *Server) handleOpen(rw http.ResponseWriter, r *http.Request) {
	s.openMu.Lock()
	defer s.openMu.Unlock()
	id := r.PathValue("id")
	if rt := s.runtime(id); rt != nil {
		writeJSON(rw, map[string]string{"id": rt.ID})
		return
	}
	cfg := s.base
	cfg.Instructions = ""
	sess, resumed, err := session.Resume(id, &cfg)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusNotFound)
		return
	}
	if sess.Model != "" {
		cfg.Model = sess.Model
	}
	if sess.Effort != "" {
		cfg.Effort = sess.Effort
	}
	rt := NewRuntime(s.prov, &cfg, sess, resumed, true)
	s.Register(rt)
	writeJSON(rw, map[string]string{"id": rt.ID})
}

// handleCloseSession stops a runtime; with ?purge=1 it also deletes the
// session file (live or not).
func (s *Server) handleCloseSession(rw http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rt := s.runtime(id)
	if rt != nil {
		s.mu.Lock()
		delete(s.runtimes, rt.ID)
		s.mu.Unlock()
		rt.Close()
	}
	if r.URL.Query().Get("purge") == "1" {
		// A stopped empty runtime already dropped its file; that is success.
		if err := session.Delete(id); err != nil && rt == nil {
			http.Error(rw, err.Error(), http.StatusNotFound)
			return
		}
	} else if rt == nil {
		http.Error(rw, "no such live session", http.StatusNotFound)
		return
	}
	rw.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleState(rw http.ResponseWriter, r *http.Request, rt *Runtime) {
	events, busy, status := rt.IO.hub.snapshot()
	writeJSON(rw, map[string]any{"events": events, "busy": busy, "status": status})
}

func (s *Server) handleEvents(rw http.ResponseWriter, r *http.Request, rt *Runtime) {
	fl, ok := rw.(http.Flusher)
	if !ok {
		http.Error(rw, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	if id := r.Header.Get("Last-Event-ID"); id != "" {
		after, _ = strconv.ParseInt(id, 10, 64)
	}
	rw.Header().Set("Content-Type", "text/event-stream")
	rw.Header().Set("Cache-Control", "no-store")
	fl.Flush()

	// Wake the cond wait when the client goes away.
	ctx := r.Context()
	go func() {
		<-ctx.Done()
		rt.IO.hub.cond.Broadcast()
	}()
	for ctx.Err() == nil {
		evs, done := rt.IO.hub.waitAfter(after)
		for _, ev := range evs {
			data, _ := json.Marshal(ev)
			fmt.Fprintf(rw, "id: %d\ndata: %s\n\n", ev.ID, data)
			after = ev.ID
		}
		fl.Flush()
		if done {
			return
		}
	}
}

// inputMax caps one input request (base64 attachments included).
const inputMax = 24 << 20

func (s *Server) handleInput(rw http.ResponseWriter, r *http.Request, rt *Runtime) {
	var in struct {
		Text  string `json:"text"`
		Files []struct {
			Name string `json:"name"`
			Type string `json:"type"`
			Data string `json:"data"` // base64
		} `json:"files"`
	}
	r.Body = http.MaxBytesReader(rw, r.Body, inputMax)
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		http.Error(rw, "bad input", http.StatusBadRequest)
		return
	}
	line := strings.TrimSpace(in.Text)
	var atts []agent.Attachment
	for _, f := range in.Files {
		data, err := base64.StdEncoding.DecodeString(f.Data)
		if err != nil || f.Name == "" {
			http.Error(rw, "bad attachment", http.StatusBadRequest)
			return
		}
		atts = append(atts, agent.Attachment{Name: f.Name, Mime: f.Type, Data: data})
	}
	if line == "" && len(atts) == 0 {
		http.Error(rw, "bad input", http.StatusBadRequest)
		return
	}
	display := agent.Echo(line, atts)
	w := rt.IO
	busy := w.Busy()
	if busy {
		w.hub.emit("pending", text(display))
	} else {
		w.UserLine(display)
	}
	w.q.push(line, atts, busy)
	rw.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleInterrupt(rw http.ResponseWriter, r *http.Request, rt *Runtime) {
	rt.IO.Interrupt()
	rw.WriteHeader(http.StatusNoContent)
}

// handleModels serves the provider's model list.
func (s *Server) handleModels(rw http.ResponseWriter, r *http.Request) {
	out := []map[string]string{}
	for _, m := range s.prov.Models() {
		out = append(out, map[string]string{"slug": m.Slug, "description": m.Description})
	}
	writeJSON(rw, map[string]any{"models": out})
}

// handleDirs lists subdirectories for the new-session directory picker.
func (s *Server) handleDirs(rw http.ResponseWriter, r *http.Request) {
	path := config.ExpandHome(r.URL.Query().Get("path"))
	if path == "" {
		path = s.base.Cwd
	}
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		http.Error(rw, "path must be absolute", http.StatusBadRequest)
		return
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		http.Error(rw, "cannot read directory", http.StatusNotFound)
		return
	}
	dirs := []string{}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		dirs = append(dirs, e.Name())
	}
	sort.Strings(dirs)
	parent := filepath.Dir(path)
	if parent == path {
		parent = ""
	}
	writeJSON(rw, map[string]any{"path": path, "parent": parent, "dirs": dirs})
}

// handleMkdir creates a directory for the new-session picker.
func (s *Server) handleMkdir(rw http.ResponseWriter, r *http.Request) {
	var in struct {
		Path string `json:"path"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil || in.Path == "" {
		http.Error(rw, "bad input", http.StatusBadRequest)
		return
	}
	path := filepath.Clean(config.ExpandHome(in.Path))
	if !filepath.IsAbs(path) {
		http.Error(rw, "path must be absolute", http.StatusBadRequest)
		return
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		http.Error(rw, "cannot create directory", http.StatusBadRequest)
		return
	}
	writeJSON(rw, map[string]string{"path": path})
}

// handleStatic serves the embedded frontend; unknown paths fall back to
// index.html (client-side routing), missing build falls back to placeholder.
func handleStatic(rw http.ResponseWriter, r *http.Request) {
	dist, _ := fs.Sub(distFS, "dist")
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	if _, err := fs.Stat(dist, path); err != nil {
		path = "index.html"
	}
	if _, err := fs.Stat(dist, path); err != nil {
		rw.Header().Set("Content-Type", "text/html")
		fmt.Fprint(rw, placeholder)
		return
	}
	http.ServeFileFS(rw, r, dist, path)
}

func (s *Server) mux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/sessions", s.auth(s.handleSessions))
	mux.HandleFunc("POST /api/sessions", s.auth(s.handleNew))
	mux.HandleFunc("POST /api/sessions/{id}/open", s.auth(s.handleOpen))
	mux.HandleFunc("DELETE /api/sessions/{id}", s.auth(s.handleCloseSession))
	mux.HandleFunc("GET /api/sessions/{id}/state", s.auth(s.withRuntime(s.handleState)))
	mux.HandleFunc("GET /api/sessions/{id}/events", s.auth(s.withRuntime(s.handleEvents)))
	mux.HandleFunc("POST /api/sessions/{id}/input", s.auth(s.withRuntime(s.handleInput)))
	mux.HandleFunc("POST /api/sessions/{id}/interrupt", s.auth(s.withRuntime(s.handleInterrupt)))
	mux.HandleFunc("GET /api/models", s.auth(s.handleModels))
	mux.HandleFunc("GET /api/dirs", s.auth(s.handleDirs))
	mux.HandleFunc("POST /api/dirs", s.auth(s.handleMkdir))
	// chroma palettes for preview spans; colors only, no auth needed
	mux.HandleFunc("GET /hl.css", func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "text/css")
		fmt.Fprint(rw, hlCSS())
	})
	mux.HandleFunc("/", handleStatic)
	return mux
}

// Start begins serving and returns the URL to open (with the token).
// notify receives progress lines; the server runs until Shutdown.
func (s *Server) Start(notify func(string)) (string, error) {
	if s.Domain != "" {
		return s.startTLS(notify)
	}
	host, _, err := net.SplitHostPort(s.Addr)
	if err != nil {
		return "", fmt.Errorf("bad --serve address %q", s.Addr)
	}
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return "", fmt.Errorf("refusing plain HTTP on %s — non-loopback needs --domain for TLS", s.Addr)
	}
	ln, err := net.Listen("tcp", s.Addr)
	if err != nil {
		return "", err
	}
	s.http = &http.Server{Handler: s.mux()}
	go s.http.Serve(ln)
	return fmt.Sprintf("http://%s/#%s", ln.Addr(), s.token), nil
}

func (s *Server) startTLS(notify func(string)) (string, error) {
	m := &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(s.Domain),
		Cache:      autocert.DirCache(config.Path("autocert")),
	}
	ln80, err := net.Listen("tcp", ":80")
	if err != nil {
		return "", fmt.Errorf("port 80 (ACME challenge): %w", err)
	}
	go http.Serve(ln80, m.HTTPHandler(nil)) // also redirects to https
	ln, err := tls.Listen("tcp", ":443", m.TLSConfig())
	if err != nil {
		return "", fmt.Errorf("port 443: %w", err)
	}
	notify(fmt.Sprintf("🔒 TLS for %s via Let's Encrypt (cert cached in %s)",
		s.Domain, config.Path("autocert")))
	s.http = &http.Server{Handler: s.mux()}
	go s.http.Serve(ln)
	return fmt.Sprintf("https://%s/#%s", s.Domain, s.token), nil
}

// Shutdown closes every runtime, then the HTTP server.
func (s *Server) Shutdown() {
	s.mu.Lock()
	all := make([]*Runtime, 0, len(s.runtimes))
	for _, rt := range s.runtimes {
		all = append(all, rt)
	}
	s.runtimes = map[string]*Runtime{}
	s.mu.Unlock()
	for _, rt := range all {
		rt.Close()
	}
	if s.http != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		s.http.Shutdown(ctx)
	}
}
