package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brijbyte/orc/internal/config"
)

func TestValidSessionRefRejectsPaths(t *testing.T) {
	for _, ref := range []string{"../session.jsonl", "/tmp/session.jsonl", `..\\session.jsonl`, ""} {
		if validSessionRef(ref) {
			t.Errorf("accepted %q", ref)
		}
	}
	for _, ref := range []string{"abc123", "550e8400-e29b-41d4-a716-446655440000"} {
		if !validSessionRef(ref) {
			t.Errorf("rejected %q", ref)
		}
	}
}

func TestHandleCatchupReturnsEventsAfterCursor(t *testing.T) {
	rt := &Runtime{IO: NewIO(nil)}
	rt.IO.hub.emit("user", nil)
	rt.IO.hub.emit("tool", nil)
	req := httptest.NewRequest(http.MethodGet, "/?after=1", nil)
	rw := httptest.NewRecorder()
	new(Server).handleCatchup(rw, req, rt)
	var got struct {
		Events []Event `json:"events"`
		LastID int64   `json:"last_id"`
	}
	if rw.Code != http.StatusOK || json.NewDecoder(rw.Body).Decode(&got) != nil {
		t.Fatalf("response: status=%d body=%s", rw.Code, rw.Body.String())
	}
	if got.LastID != 2 || len(got.Events) != 1 || got.Events[0].ID != 2 {
		t.Fatalf("catchup = %#v", got)
	}
}

func TestToolCallCreatesFileReference(t *testing.T) {
	cfg := &config.Config{Cwd: t.TempDir()}
	w := NewIO(cfg)
	w.ToolCall("read", `{"path":"src/main.go"}`)
	events, _, _ := w.hub.snapshot()
	var data struct {
		Path string `json:"path"`
		File string `json:"file"`
	}
	if len(events) != 1 || json.Unmarshal(events[0].Data, &data) != nil {
		t.Fatalf("bad tool event: %#v", events)
	}
	want := filepath.Join(cfg.Cwd, "src/main.go")
	cfg.Cwd = t.TempDir()
	path, ok := w.filePath(data.File)
	if data.Path != "src/main.go" || !ok || path != want {
		t.Fatalf("file reference: data=%#v path=%q", data, path)
	}
}

func TestHandleFileRejectsForgedPath(t *testing.T) {
	rt := &Runtime{IO: NewIO(nil)}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Orc-File-Ref", "/etc/passwd")
	rw := httptest.NewRecorder()
	new(Server).handleFile(rw, req, rt)
	if rw.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rw.Code)
	}
}

func TestAuthRejectsTokenInURL(t *testing.T) {
	s := &Server{token: "secret"}
	next := s.auth(func(rw http.ResponseWriter, r *http.Request) { rw.WriteHeader(http.StatusNoContent) })
	req := httptest.NewRequest(http.MethodGet, "/?token=secret", nil)
	rw := httptest.NewRecorder()
	next(rw, req)
	if rw.Code != http.StatusUnauthorized {
		t.Fatalf("query token status = %d", rw.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rw = httptest.NewRecorder()
	next(rw, req)
	if rw.Code != http.StatusNoContent {
		t.Fatalf("header token status = %d", rw.Code)
	}
}

func TestSecurityHeaders(t *testing.T) {
	s := &Server{Domain: "orc.example.com"}
	h := s.secure(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {}))
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, httptest.NewRequest(http.MethodGet, "/", nil))
	for _, name := range []string{
		"Content-Security-Policy", "Permissions-Policy", "Referrer-Policy",
		"Strict-Transport-Security", "X-Content-Type-Options", "X-Frame-Options",
	} {
		if rw.Header().Get(name) == "" {
			t.Errorf("missing %s", name)
		}
	}
}

func TestHandleFileReadsAndHighlightsLatestContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "latest.go")
	content := "package latest\n\nconst Value = 2\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Cwd: dir}
	io := NewIO(cfg)
	io.ToolCall("read", `{"path":"latest.go"}`)
	events, _, _ := io.hub.snapshot()
	var tool struct {
		File string `json:"file"`
	}
	if json.Unmarshal(events[0].Data, &tool) != nil || tool.File == "" {
		t.Fatalf("tool event = %#v", events[0])
	}
	rt := &Runtime{Cfg: cfg, IO: io}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Orc-File-Ref", tool.File)
	rw := httptest.NewRecorder()
	new(Server).handleFile(rw, req, rt)
	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rw.Code, rw.Body.String())
	}
	var got struct {
		Path    string   `json:"path"`
		Content string   `json:"content"`
		HTML    []string `json:"html"`
	}
	if err := json.NewDecoder(rw.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Path != path || got.Content != content {
		t.Fatalf("file = %#v", got)
	}
	if len(got.HTML) != 3 || !strings.Contains(got.HTML[0], `<span class="`) {
		t.Fatalf("file not highlighted by line: %#v", got.HTML)
	}
}
