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

func TestHandleCatchupReturnsEventsAfterCursor(t *testing.T) {
	rt := &Runtime{IO: NewIO()}
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

func TestToolCallIncludesFilePath(t *testing.T) {
	w := NewIO()
	w.ToolCall("read", `{"path":"src/main.go"}`)
	events, _, _ := w.hub.snapshot()
	var data struct {
		Path string `json:"path"`
	}
	if len(events) != 1 || json.Unmarshal(events[0].Data, &data) != nil {
		t.Fatalf("bad tool event: %#v", events)
	}
	if data.Path != "src/main.go" {
		t.Fatalf("path = %q", data.Path)
	}
}

func TestHandleFileReadsAndHighlightsLatestContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "latest.go")
	content := "package latest\n\nconst Value = 2\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	rt := &Runtime{Cfg: &config.Config{Cwd: dir}}
	req := httptest.NewRequest(http.MethodGet, "/?path=latest.go", nil)
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
