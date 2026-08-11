package web

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/brijbyte/orc/internal/agent"
	"github.com/brijbyte/orc/internal/config"
	"github.com/brijbyte/orc/internal/notify"
	"github.com/brijbyte/orc/internal/provider"
	"github.com/brijbyte/orc/internal/session"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/acme/autocert"
)

//go:embed all:dist
var distFS embed.FS

const (
	authCookie = "orc_session"
	authTTL    = 30 * 24 * time.Hour
)

const placeholder = `<!doctype html><meta charset="utf-8"><title>orc</title>
<body style="font-family:monospace;background:#111;color:#ddd;padding:2em">
🧌 orc: web UI not built — run <b>make web</b> and rebuild.</body>`

// Server manages live session runtimes and exposes them over HTTP.
type Server struct {
	Addr   string
	Domain string // non-empty: TLS via Let's Encrypt on :443 (+ :80 challenge)

	prov            provider.Provider
	base            config.Config // template for new sessions (model/effort/cwd)
	baseMu          sync.RWMutex
	initialPassword string
	passwordCreated bool
	mu              sync.Mutex
	openMu          sync.Mutex // serializes open: one runtime per session file
	runtimes        map[string]*Runtime
	scheduler       *wakeScheduler
	http            *http.Server
	acmeHTTP        *http.Server
}

func NewServer(prov provider.Provider, base *config.Config, addr, domain string) (*Server, error) {
	password, created, err := config.EnsureWebAuth()
	if err != nil {
		return nil, fmt.Errorf("web password: %w", err)
	}
	s := &Server{
		Addr: addr, Domain: domain, prov: prov, base: *base,
		initialPassword: password,
		passwordCreated: created,
		runtimes:        map[string]*Runtime{},
	}
	s.scheduler = newWakeScheduler(s)
	return s, nil
}

// Register adds an already-built runtime (the initial --serve session).
func (s *Server) baseConfig() config.Config {
	s.baseMu.RLock()
	defer s.baseMu.RUnlock()
	return s.base
}

func (s *Server) Register(rt *Runtime) {
	scheduledID := rt.Cfg.SessionID
	rt.setAfterTurn(func(rt *Runtime) {
		if s.scheduler != nil {
			current := rt.Cfg.SessionID
			if scheduledID != current {
				s.scheduler.arm(scheduledID, "")
				scheduledID = current
			}
			s.scheduler.arm(current, rt.Ag.Sess.Wake)
		}
	})
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

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(authCookie)
		if err != nil || !validAuthCookie(cookie.Value) {
			http.Error(rw, "unauthorized", http.StatusUnauthorized)
			return
		}
		max := int64(1 << 20)
		switch {
		case strings.HasSuffix(r.URL.Path, "/input"):
			max = inputMax
		case strings.HasSuffix(r.URL.Path, "/file"):
			max = fileMax
		}
		r.Body = http.MaxBytesReader(rw, r.Body, max)
		next.ServeHTTP(rw, r)
	})
}

func validAuthCookie(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return false
	}
	expires, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || expires < time.Now().Unix() {
		return false
	}
	key, err := config.WebSessionKey()
	if err != nil {
		return false
	}
	want := authSignature(key, parts[0])
	return subtle.ConstantTimeCompare([]byte(parts[1]), []byte(want)) == 1
}

func authSignature(key, expires string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte("orc-session:" + expires))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *Server) handleLogin(rw http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(rw, r.Body, 4096)
	var body struct {
		Password string `json:"password"`
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil {
		http.Error(rw, "bad request", http.StatusBadRequest)
		return
	}
	valid, err := config.VerifyWebPassword(body.Password)
	if err != nil || !valid {
		http.Error(rw, "unauthorized", http.StatusUnauthorized)
		return
	}
	key, err := config.WebSessionKey()
	if err != nil {
		http.Error(rw, "unauthorized", http.StatusUnauthorized)
		return
	}
	expires := time.Now().Add(authTTL)
	expiresText := strconv.FormatInt(expires.Unix(), 10)
	http.SetCookie(rw, &http.Cookie{
		Name: authCookie, Value: expiresText + "." + authSignature(key, expiresText),
		Path: "/", Expires: expires, MaxAge: int(authTTL.Seconds()), HttpOnly: true,
		Secure: s.Domain != "" || r.TLS != nil, SameSite: http.SameSiteStrictMode,
	})
	rw.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleLogout(rw http.ResponseWriter, r *http.Request) {
	http.SetCookie(rw, &http.Cookie{
		Name: authCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true,
		Secure: s.Domain != "" || r.TLS != nil, SameSite: http.SameSiteStrictMode,
	})
	rw.WriteHeader(http.StatusNoContent)
}

func writeJSON(rw http.ResponseWriter, v any) {
	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(v)
}

func validSessionRef(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, c := range []byte(id) {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') &&
			(c < '0' || c > '9') && c != '-' && c != '_' {
			return false
		}
	}
	return true
}

// withRuntime resolves {id} to a live runtime or 404s.
func (s *Server) withRuntime(next func(http.ResponseWriter, *http.Request, *Runtime)) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if !validSessionRef(id) {
			http.Error(rw, "bad session id", http.StatusBadRequest)
			return
		}
		rt := s.runtime(id)
		if rt == nil {
			http.Error(rw, "no such live session", http.StatusNotFound)
			return
		}
		next(rw, r, rt)
	}
}

// sessionRow is one /api/sessions entry.
type sessionRow struct {
	ID      string `json:"id"`
	Rid     string `json:"rid,omitempty"` // live runtime handle
	Title   string `json:"title"`
	When    string `json:"when"`
	Used    string `json:"used"`
	Cwd     string `json:"cwd"`
	Routine string `json:"routine,omitempty"`
	Wake    string `json:"wake,omitempty"`
	Live    bool   `json:"live"`
	Busy    bool   `json:"busy"`
	Pinned  bool   `json:"pinned"`
}

func truncateTitle(title string) string {
	chars := []rune(title)
	if len(chars) > 50 {
		return string(chars[:50])
	}
	return title
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
		sr := sessionRow{ID: row.ID, Title: truncateTitle(row.Title), When: row.When,
			Used: row.Used, Cwd: row.Cwd, Routine: row.Routine, Wake: row.Wake, Pinned: row.Pinned}
		if rt, ok := live[row.ID]; ok {
			sr.Live, sr.Rid, sr.Busy = true, rt.ID, rt.IO.Busy()
		}
		out = append(out, sr)
	}
	home, _ := os.UserHomeDir()
	writeJSON(rw, map[string]any{"cwd": s.baseConfig().Cwd, "home": home, "sessions": out})
}

// handleNew starts a fresh session, optionally in another directory.
func (s *Server) handleNew(rw http.ResponseWriter, r *http.Request) {
	var in struct {
		Cwd     string `json:"cwd"`
		Routine string `json:"routine"`
	}
	json.NewDecoder(r.Body).Decode(&in)
	cfg := s.baseConfig()
	cfg.SessionID = uuid.NewString()
	cfg.Instructions = ""
	cfg.Routine = strings.TrimSpace(in.Routine)
	if len(cfg.Routine) > 32768 {
		http.Error(rw, "routine is too long", http.StatusBadRequest)
		return
	}
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
	if cfg.Routine != "" {
		rt.IO.UserLine(cfg.Routine)
		rt.IO.q.push(cfg.Routine, nil, false)
	}
	writeJSON(rw, map[string]string{"id": rt.ID})
}

func (s *Server) openRuntime(id string) (*Runtime, error) {
	s.openMu.Lock()
	defer s.openMu.Unlock()
	if rt := s.runtime(id); rt != nil {
		return rt, nil
	}
	if cur := session.CurrentID(id); cur != "" {
		if rt := s.runtime(cur); rt != nil {
			return rt, nil
		}
	}
	cfg := s.baseConfig()
	cfg.Instructions = ""
	sess, resumed, err := session.Resume(id, &cfg)
	if err != nil {
		return nil, err
	}
	if sess.Model != "" {
		cfg.Model = sess.Model
	}
	if sess.Effort != "" {
		cfg.Effort = sess.Effort
	}
	rt := NewRuntime(s.prov, &cfg, sess, resumed, true)
	s.Register(rt)
	return rt, nil
}

// handleOpen resumes a session from disk (or returns the live runtime).
func (s *Server) handleOpen(rw http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validSessionRef(id) {
		http.Error(rw, "bad session id", http.StatusBadRequest)
		return
	}
	rt, err := s.openRuntime(id)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(rw, map[string]string{"id": rt.ID})
}

// handleCloseSession stops a runtime; with ?purge=1 it also deletes the
// session file (live or not).
func (s *Server) handleCloseSession(rw http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validSessionRef(id) {
		http.Error(rw, "bad session id", http.StatusBadRequest)
		return
	}
	rt := s.runtime(id)
	if rt != nil {
		s.mu.Lock()
		delete(s.runtimes, rt.ID)
		s.mu.Unlock()
		rt.StopRoutine()
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
	if s.scheduler != nil {
		s.scheduler.arm(id, "")
	}
	rw.WriteHeader(http.StatusNoContent)
}

// handlePin keeps a session at the top of every list, across restarts.
func (s *Server) handlePin(rw http.ResponseWriter, r *http.Request) {
	if !validSessionRef(chi.URLParam(r, "id")) {
		http.Error(rw, "bad session id", http.StatusBadRequest)
		return
	}
	var in struct {
		Pinned bool `json:"pinned"`
	}
	json.NewDecoder(r.Body).Decode(&in)
	if err := session.Pin(chi.URLParam(r, "id"), in.Pinned); err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}
	rw.WriteHeader(http.StatusNoContent)
}

const historyPageSize = 75

func (s *Server) handleState(rw http.ResponseWriter, r *http.Request, rt *Runtime) {
	events, before, last, more, busy, status := rt.IO.hub.page(0, historyPageSize)
	writeJSON(rw, map[string]any{
		"events": events, "before": before, "last_id": last, "has_more": more,
		"busy": busy, "status": status,
	})
}

func (s *Server) handleHistory(rw http.ResponseWriter, r *http.Request, rt *Runtime) {
	before, err := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	if err != nil || before < 1 {
		http.Error(rw, "bad cursor", http.StatusBadRequest)
		return
	}
	events, cursor, _, more, _, _ := rt.IO.hub.page(before, historyPageSize)
	writeJSON(rw, map[string]any{"events": events, "before": cursor, "has_more": more})
}

func (s *Server) handleCatchup(rw http.ResponseWriter, r *http.Request, rt *Runtime) {
	after, err := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	if err != nil || after < 0 {
		http.Error(rw, "bad cursor", http.StatusBadRequest)
		return
	}
	events, last := rt.IO.hub.after(after)
	rw.Header().Set("Cache-Control", "no-store")
	writeJSON(rw, map[string]any{"events": events, "last_id": last})
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

	// Wake the cond wait when the client leaves or its login expires.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	cookie, _ := r.Cookie(authCookie)
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				rt.IO.hub.cond.Broadcast()
				return
			case <-ticker.C:
				if !validAuthCookie(cookie.Value) {
					cancel()
				}
			}
		}
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
const attachmentMax = 16 << 20

func serverAttachment(path string) (agent.Attachment, error) {
	path = filepath.Clean(config.ExpandHome(path))
	if !filepath.IsAbs(path) {
		return agent.Attachment{}, fmt.Errorf("path must be absolute")
	}
	info, err := os.Stat(path)
	if err != nil {
		return agent.Attachment{}, err
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		return agent.Attachment{}, fmt.Errorf("path is not attachable")
	}
	name := path
	if info.IsDir() {
		name += string(filepath.Separator)
	}
	return agent.Attachment{Name: name, Path: path}, nil
}

func (s *Server) handleInput(rw http.ResponseWriter, r *http.Request, rt *Runtime) {
	var in struct {
		Text  string `json:"text"`
		Files []struct {
			Name string `json:"name"`
			Type string `json:"type"`
			Data string `json:"data"` // base64
		} `json:"files"`
		Paths []string `json:"paths"`
	}
	r.Body = http.MaxBytesReader(rw, r.Body, inputMax)
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		http.Error(rw, "bad input", http.StatusBadRequest)
		return
	}
	line := strings.TrimSpace(in.Text)
	var atts []agent.Attachment
	total := 0
	for _, f := range in.Files {
		data, err := base64.StdEncoding.DecodeString(f.Data)
		if err != nil || f.Name == "" {
			http.Error(rw, "bad attachment", http.StatusBadRequest)
			return
		}
		total += len(data)
		atts = append(atts, agent.Attachment{Name: f.Name, Mime: f.Type, Data: data})
	}
	if len(in.Paths) > 64 {
		http.Error(rw, "too many attachments", http.StatusBadRequest)
		return
	}
	for _, path := range in.Paths {
		attachment, err := serverAttachment(path)
		if err != nil {
			http.Error(rw, "cannot attach server path", http.StatusBadRequest)
			return
		}
		total += len(attachment.Data)
		atts = append(atts, attachment)
	}
	if total > attachmentMax {
		http.Error(rw, "attachments are too large", http.StatusRequestEntityTooLarge)
		return
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

func (s *Server) handleWake(rw http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validSessionRef(id) {
		http.Error(rw, "bad session id", http.StatusBadRequest)
		return
	}
	rt, err := s.openRuntime(id)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusNotFound)
		return
	}
	if rt.Cfg.Routine == "" {
		http.Error(rw, "routine is stopped", http.StatusConflict)
		return
	}
	if rt.IO.Busy() {
		http.Error(rw, "busy", http.StatusConflict)
		return
	}
	if s.scheduler != nil {
		s.scheduler.arm(rt.Cfg.SessionID, "")
	}
	rt.IO.q.push("/wake", nil, false)
	rw.WriteHeader(http.StatusNoContent)
}

// handleControl queues a command without a user echo.
func handleControl(line string) func(http.ResponseWriter, *http.Request, *Runtime) {
	return func(rw http.ResponseWriter, _ *http.Request, rt *Runtime) {
		if rt.IO.Busy() {
			http.Error(rw, "busy", http.StatusConflict)
			return
		}
		rt.IO.q.push(line, nil, false)
		rw.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleInterrupt(rw http.ResponseWriter, r *http.Request, rt *Runtime) {
	rt.IO.Interrupt()
	rw.WriteHeader(http.StatusNoContent)
}

const fileMax = 8 << 20

var (
	errFileTooLarge   = errors.New("file is too large")
	errNotRegularFile = errors.New("not a regular file")
)

func readEditorFile(path string) ([]byte, fs.FileMode, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, 0, err
	}
	if !info.Mode().IsRegular() {
		return nil, 0, errNotRegularFile
	}
	if info.Size() > fileMax {
		return nil, 0, errFileTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(f, fileMax+1))
	if err != nil {
		return nil, 0, err
	}
	if len(data) > fileMax {
		return nil, 0, errFileTooLarge
	}
	return data, info.Mode().Perm(), nil
}

func editorFileError(rw http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errFileTooLarge):
		http.Error(rw, err.Error(), http.StatusRequestEntityTooLarge)
	case errors.Is(err, errNotRegularFile):
		http.Error(rw, err.Error(), http.StatusBadRequest)
	case errors.Is(err, os.ErrNotExist), errors.Is(err, os.ErrPermission):
		http.Error(rw, "cannot read file", http.StatusNotFound)
	default:
		http.Error(rw, "cannot read file", http.StatusInternalServerError)
	}
}

func fileRevision(data []byte) string {
	sum := sha256.Sum256(data)
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func writeEditorFile(path string, data []byte, mode fs.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// handleFile reads the current file from the session working directory.
func (s *Server) handleFile(rw http.ResponseWriter, r *http.Request, rt *Runtime) {
	path, ok := rt.IO.filePath(r.Header.Get("X-Orc-File-Ref"))
	if !ok {
		http.Error(rw, "unknown file reference", http.StatusNotFound)
		return
	}
	data, _, err := readEditorFile(path)
	if err != nil {
		editorFileError(rw, err)
		return
	}
	rw.Header().Set("Cache-Control", "no-store")
	displayPath := path
	if rt.Cfg != nil && rt.Cfg.Cwd != "" {
		if rel, err := filepath.Rel(rt.Cfg.Cwd, path); err == nil {
			displayPath = rel
		}
	}
	response := map[string]any{
		"path": displayPath, "content": string(data), "revision": fileRevision(data),
		"editable": utf8.Valid(data),
	}
	if original, ok := gitOriginal(r.Context(), rt, path); ok && utf8.Valid(original) {
		response["original"] = string(original)
	}
	writeJSON(rw, response)
}

// handleSaveFile writes a text file when its loaded revision is current.
func (s *Server) handleSaveFile(rw http.ResponseWriter, r *http.Request, rt *Runtime) {
	path, ok := rt.IO.filePath(r.Header.Get("X-Orc-File-Ref"))
	if !ok {
		http.Error(rw, "unknown file reference", http.StatusNotFound)
		return
	}
	revision := r.Header.Get("X-Orc-File-Revision")
	if revision == "" {
		http.Error(rw, "missing file revision", http.StatusBadRequest)
		return
	}
	current, mode, err := readEditorFile(path)
	if err != nil {
		editorFileError(rw, err)
		return
	}
	if !utf8.Valid(current) {
		http.Error(rw, "file is not text", http.StatusUnsupportedMediaType)
		return
	}
	if fileRevision(current) != revision {
		http.Error(rw, "file changed", http.StatusConflict)
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, fileMax+1))
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			http.Error(rw, "file is too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(rw, "cannot read content", http.StatusBadRequest)
		}
		return
	}
	if len(data) > fileMax {
		http.Error(rw, "file is too large", http.StatusRequestEntityTooLarge)
		return
	}
	if !bytes.Equal(current, data) {
		if err := writeEditorFile(path, data, mode); err != nil {
			http.Error(rw, "cannot save file", http.StatusInternalServerError)
			return
		}
	}
	writeJSON(rw, map[string]string{"revision": fileRevision(data)})
}

// handleBrowse lists files and directories for server-side attachments.
func (s *Server) handleBrowse(rw http.ResponseWriter, r *http.Request, rt *Runtime) {
	path := config.ExpandHome(r.URL.Query().Get("path"))
	if path == "" {
		path = rt.Cfg.Cwd
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
	type entry struct {
		Name string `json:"name"`
		Dir  bool   `json:"dir"`
		Size int64  `json:"size,omitempty"`
	}
	out := make([]entry, 0, len(entries))
	for _, item := range entries {
		info, err := item.Info()
		if err != nil {
			continue
		}
		out = append(out, entry{Name: item.Name(), Dir: info.IsDir(), Size: info.Size()})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	parent := filepath.Dir(path)
	if parent == path {
		parent = ""
	}
	writeJSON(rw, map[string]any{"path": path, "parent": parent, "entries": out})
}

// handleModels serves the provider's model list.
func (s *Server) handleModels(rw http.ResponseWriter, r *http.Request) {
	out := []map[string]any{}
	for _, m := range s.prov.Models() {
		out = append(out, map[string]any{"slug": m.Slug, "name": m.Name,
			"description": m.Description, "efforts": m.Efforts})
	}
	writeJSON(rw, map[string]any{"models": out})
}

// handleSettings serves defaults used for newly-created sessions.
func (s *Server) handleSettings(rw http.ResponseWriter, r *http.Request) {
	base := s.baseConfig()
	writeJSON(rw, map[string]string{"model": base.Model, "effort": base.Effort})
}

// handleSettingsSave persists and applies defaults for newly-created sessions.
func (s *Server) handleSettingsSave(rw http.ResponseWriter, r *http.Request) {
	var in struct {
		Model  string `json:"model"`
		Effort string `json:"effort"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		http.Error(rw, "bad request", http.StatusBadRequest)
		return
	}
	in.Model = strings.TrimSpace(in.Model)
	in.Effort = strings.TrimSpace(in.Effort)
	if in.Model == "" || len(in.Model) > 256 || !validDefaultEffort(s.prov.Models(), in.Model, in.Effort) {
		http.Error(rw, "invalid model or effort", http.StatusBadRequest)
		return
	}
	settings := config.LoadSettings()
	settings.Model, settings.Effort = in.Model, in.Effort
	if err := config.SaveSettings(settings); err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}
	s.baseMu.Lock()
	s.base.Model, s.base.Effort = in.Model, in.Effort
	s.baseMu.Unlock()
	rw.WriteHeader(http.StatusNoContent)
}

func validDefaultEffort(models []provider.Model, model, effort string) bool {
	if effort == "" || len(effort) > 64 {
		return false
	}
	efforts := []string{"low", "medium", "high"}
	for _, m := range models {
		if m.Slug == model && len(m.Efforts) > 0 {
			efforts = m.Efforts
			break
		}
	}
	for _, allowed := range efforts {
		if effort == allowed {
			return true
		}
	}
	return false
}

// handlePassword changes the web password and invalidates signed-in clients.
func (s *Server) handlePassword(rw http.ResponseWriter, r *http.Request) {
	var in struct {
		Current string `json:"current"`
		Next    string `json:"next"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		http.Error(rw, "bad request", http.StatusBadRequest)
		return
	}
	if utf8.RuneCountInString(in.Next) < 8 || len(in.Next) > 72 {
		http.Error(rw, "new password must be at least 8 characters and at most 72 bytes", http.StatusBadRequest)
		return
	}
	if err := config.ChangeWebPassword(in.Current, in.Next); err != nil {
		if errors.Is(err, config.ErrInvalidWebPassword) {
			http.Error(rw, err.Error(), http.StatusUnauthorized)
		} else {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	s.handleLogout(rw, r)
}

// handleNotify serves channel providers and the user's configured channels.
func (s *Server) handleNotify(rw http.ResponseWriter, r *http.Request) {
	channels := config.LoadSettings().Notify
	if channels == nil {
		channels = []config.NotifyChannel{}
	}
	writeJSON(rw, map[string]any{"types": notify.Types(), "channels": channels})
}

// handleNotifySave replaces the configured channel list.
func (s *Server) handleNotifySave(rw http.ResponseWriter, r *http.Request) {
	var in struct {
		Channels []config.NotifyChannel `json:"channels"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		http.Error(rw, "bad request", http.StatusBadRequest)
		return
	}
	for _, ch := range in.Channels {
		if err := notify.Validate(ch); err != nil {
			http.Error(rw, err.Error(), http.StatusBadRequest)
			return
		}
	}
	settings := config.LoadSettings()
	settings.Notify = in.Channels
	if err := config.SaveSettings(settings); err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}
	rw.WriteHeader(http.StatusNoContent)
}

// handleNotifyTest sends a test message on one (possibly unsaved) channel.
func (s *Server) handleNotifyTest(rw http.ResponseWriter, r *http.Request) {
	var ch config.NotifyChannel
	if json.NewDecoder(r.Body).Decode(&ch) != nil {
		http.Error(rw, "bad request", http.StatusBadRequest)
		return
	}
	err := notify.SendTo(r.Context(), ch, notify.Message{
		Title:   "orc test notification",
		Body:    "channel “" + ch.Name + "” works",
		Urgency: "info",
	})
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadGateway)
		return
	}
	rw.WriteHeader(http.StatusNoContent)
}

// handleDirs lists subdirectories for the new-session directory picker.
func (s *Server) handleDirs(rw http.ResponseWriter, r *http.Request) {
	path := config.ExpandHome(r.URL.Query().Get("path"))
	if path == "" {
		path = s.baseConfig().Cwd
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

// gzipped caches compressed embedded assets; they are immutable per build.
var gzipped sync.Map

func compressible(path string) bool {
	switch filepath.Ext(path) {
	case ".js", ".css", ".html", ".svg", ".json", ".map", ".txt":
		return true
	}
	return false
}

// handleStatic serves the embedded frontend; unknown paths fall back to
// index.html (client-side routing), missing build falls back to placeholder.
func handleStatic(rw http.ResponseWriter, r *http.Request) {
	dist, _ := fs.Sub(distFS, "dist")
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" || !fs.ValidPath(path) {
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
	// hashed build output never changes; index.html revalidates by ETag
	etag := `"` + config.Version + "/" + path + `"`
	if strings.HasPrefix(path, "assets/") {
		rw.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		rw.Header().Set("Cache-Control", "no-cache")
	}
	rw.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		rw.WriteHeader(http.StatusNotModified)
		return
	}
	if compressible(path) && r.Header.Get("Range") == "" &&
		strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		body, ok := gzipped.Load(path)
		if !ok {
			raw, err := fs.ReadFile(dist, path)
			if err != nil {
				http.Error(rw, "read error", http.StatusInternalServerError)
				return
			}
			var buf bytes.Buffer
			zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
			zw.Write(raw)
			zw.Close()
			body, _ = gzipped.LoadOrStore(path, buf.Bytes())
		}
		data := body.([]byte)
		h := rw.Header()
		h.Set("Content-Encoding", "gzip")
		h.Set("Content-Type", mime.TypeByExtension(filepath.Ext(path)))
		h.Set("Content-Length", strconv.Itoa(len(data)))
		h.Set("Vary", "Accept-Encoding")
		rw.Write(data)
		return
	}
	http.ServeFileFS(rw, r, dist, path)
}

func (s *Server) secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		h := rw.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; img-src 'self' data:; object-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'; style-src 'self' 'unsafe-inline'")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		if s.Domain != "" {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(rw, r)
	})
}

func (s *Server) router() http.Handler {
	router := chi.NewRouter()
	router.Route("/api", func(api chi.Router) {
		api.Post("/login", s.handleLogin)
		api.Group(func(api chi.Router) {
			api.Use(s.auth)
			api.Post("/logout", s.handleLogout)
			api.Get("/settings", s.handleSettings)
			api.Put("/settings", s.handleSettingsSave)
			api.Post("/password", s.handlePassword)
			api.Get("/sessions", s.handleSessions)
			api.Post("/sessions", s.handleNew)
			api.Post("/sessions/{id}/open", s.handleOpen)
			api.Delete("/sessions/{id}", s.handleCloseSession)
			api.Post("/sessions/{id}/pin", s.handlePin)
			api.Get("/sessions/{id}/state", s.withRuntime(s.handleState))
			api.Get("/sessions/{id}/history", s.withRuntime(s.handleHistory))
			api.Get("/sessions/{id}/catchup", s.withRuntime(s.handleCatchup))
			api.Get("/sessions/{id}/events", s.withRuntime(s.handleEvents))
			api.Get("/sessions/{id}/terminal", s.withRuntime(s.handleTerminal))
			api.Post("/sessions/{id}/input", s.withRuntime(s.handleInput))
			api.Post("/sessions/{id}/compact", s.withRuntime(handleControl("/compact")))
			api.Post("/sessions/{id}/retry", s.withRuntime(handleControl("/retry")))
			api.Post("/sessions/{id}/wake", s.handleWake)
			api.Post("/sessions/{id}/interrupt", s.withRuntime(s.handleInterrupt))
			api.Get("/sessions/{id}/browse", s.withRuntime(s.handleBrowse))
			api.Get("/sessions/{id}/file", s.withRuntime(s.handleFile))
			api.Put("/sessions/{id}/file", s.withRuntime(s.handleSaveFile))
			api.Get("/sessions/{id}/git/status", s.withRuntime(s.handleGitStatus))
			api.Get("/sessions/{id}/git/compare", s.withRuntime(s.handleGitCompare))
			api.Get("/sessions/{id}/git/diff", s.withRuntime(s.handleGitDiff))
			api.Post("/sessions/{id}/git/stage", s.withRuntime(s.handleGitMutation(true)))
			api.Post("/sessions/{id}/git/unstage", s.withRuntime(s.handleGitMutation(false)))
			api.Post("/sessions/{id}/git/commit", s.withRuntime(s.handleGitCommit))
			api.Post("/sessions/{id}/git/switch", s.withRuntime(s.handleGitBranch(false)))
			api.Post("/sessions/{id}/git/create-branch", s.withRuntime(s.handleGitBranch(true)))
			api.Post("/sessions/{id}/git/discard", s.withRuntime(s.handleGitRecoveryMutation("discard")))
			api.Post("/sessions/{id}/git/remove", s.withRuntime(s.handleGitRecoveryMutation("remove")))
			api.Post("/sessions/{id}/git/undo-discard", s.withRuntime(s.handleGitRecoveryMutation("undo")))
			api.Get("/models", s.handleModels)
			api.Get("/notify", s.handleNotify)
			api.Put("/notify", s.handleNotifySave)
			api.Post("/notify/test", s.handleNotifyTest)
			api.Get("/dirs", s.handleDirs)
			api.Post("/dirs", s.handleMkdir)
		})
		api.NotFound(http.NotFound)
	})
	router.Get("/*", handleStatic)
	return router
}

func (s *Server) httpServer() *http.Server {
	return &http.Server{
		Handler:           s.secure(s.router()),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    64 << 10,
	}
}

// Start begins serving and returns the URL to open.
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
	s.http = s.httpServer()
	go s.http.Serve(ln)
	s.scheduler.start()
	if s.passwordCreated {
		notify("🔑 web password: " + s.initialPassword)
	}
	return fmt.Sprintf("http://%s/", ln.Addr()), nil
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
	s.acmeHTTP = &http.Server{
		Handler:           m.HTTPHandler(nil),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    64 << 10,
	}
	go s.acmeHTTP.Serve(ln80) // also redirects to https
	tlsConfig := m.TLSConfig()
	tlsConfig.MinVersion = tls.VersionTLS12
	ln, err := tls.Listen("tcp", ":443", tlsConfig)
	if err != nil {
		s.acmeHTTP.Close()
		return "", fmt.Errorf("port 443: %w", err)
	}
	notify(fmt.Sprintf("🔒 TLS for %s via Let's Encrypt (cert cached in %s)",
		s.Domain, config.Path("autocert")))
	s.http = s.httpServer()
	go s.http.Serve(ln)
	s.scheduler.start()
	if s.passwordCreated {
		notify("🔑 web password: " + s.initialPassword)
	}
	return fmt.Sprintf("https://%s/", s.Domain), nil
}

// Shutdown closes every runtime, then the HTTP server.
func (s *Server) Shutdown() {
	if s.scheduler != nil {
		s.scheduler.close()
	}
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
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if s.http != nil {
		s.http.Shutdown(ctx)
	}
	if s.acmeHTTP != nil {
		s.acmeHTTP.Shutdown(ctx)
	}
}
