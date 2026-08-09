package web

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/brijbyte/orc/internal/config"
	"golang.org/x/crypto/acme/autocert"
)

//go:embed all:dist
var distFS embed.FS

const placeholder = `<!doctype html><meta charset="utf-8"><title>orc</title>
<body style="font-family:monospace;background:#111;color:#ddd;padding:2em">
🧌 orc: web UI not built — run <b>make web</b> and rebuild.</body>`

// Server exposes one session over HTTP for the web IO.
type Server struct {
	IO     *IO
	Addr   string
	Domain string // non-empty: TLS via Let's Encrypt on :443 (+ :80 challenge)

	http *http.Server
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		got := r.URL.Query().Get("token")
		if h := r.Header.Get("Authorization"); got == "" {
			got = strings.TrimPrefix(h, "Bearer ")
		}
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.IO.token)) != 1 {
			http.Error(rw, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(rw, r)
	}
}

func (s *Server) handleState(rw http.ResponseWriter, r *http.Request) {
	events, busy, status := s.IO.hub.snapshot()
	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(map[string]any{
		"events": events, "busy": busy, "status": status,
	})
}

func (s *Server) handleEvents(rw http.ResponseWriter, r *http.Request) {
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
		s.IO.hub.cond.Broadcast()
	}()
	for ctx.Err() == nil {
		evs, done := s.IO.hub.waitAfter(after)
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

func (s *Server) handleInput(rw http.ResponseWriter, r *http.Request) {
	var in struct {
		Text string `json:"text"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil || strings.TrimSpace(in.Text) == "" {
		http.Error(rw, "bad input", http.StatusBadRequest)
		return
	}
	line := strings.TrimSpace(in.Text)
	_, busy, _ := s.IO.hub.snapshot()
	if busy {
		s.IO.hub.emit("pending", text(line))
	} else {
		s.IO.UserLine(line)
	}
	s.IO.q.push(line, busy)
	rw.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleInterrupt(rw http.ResponseWriter, r *http.Request) {
	s.IO.Interrupt()
	rw.WriteHeader(http.StatusNoContent)
}

// handleModels serves the provider's model list (prefetched at startup, so
// this reads a cached slice).
func (s *Server) handleModels(rw http.ResponseWriter, r *http.Request) {
	out := []map[string]string{}
	if s.IO.cmds != nil {
		for _, m := range s.IO.cmds.Models() {
			out = append(out, map[string]string{"slug": m.Slug, "description": m.Description})
		}
	}
	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(map[string]any{"models": out})
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
	mux.HandleFunc("GET /api/state", s.auth(s.handleState))
	mux.HandleFunc("GET /api/events", s.auth(s.handleEvents))
	mux.HandleFunc("POST /api/input", s.auth(s.handleInput))
	mux.HandleFunc("POST /api/interrupt", s.auth(s.handleInterrupt))
	mux.HandleFunc("GET /api/models", s.auth(s.handleModels))
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
	return fmt.Sprintf("http://%s/#%s", ln.Addr(), s.IO.token), nil
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
	return fmt.Sprintf("https://%s/#%s", s.Domain, s.IO.token), nil
}

func (s *Server) Shutdown() {
	if s.http != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		s.http.Shutdown(ctx)
	}
}
