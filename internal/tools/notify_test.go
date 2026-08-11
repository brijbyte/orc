package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/brijbyte/orc/internal/config"
	"github.com/brijbyte/orc/internal/notify"
)

// notifyHome points config at a temp orc home holding one enabled channel
// aimed at srv.
func notifyHome(t *testing.T, srv *httptest.Server) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "orc"), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := config.Settings{Notify: []config.NotifyChannel{{
		Type:     "ntfy",
		Name:     "secret-channel-name",
		Enabled:  true,
		Settings: map[string]string{"url": srv.URL},
	}}}
	data, _ := json.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(home, "orc", "config.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func notifyServer(hits *atomic.Int64, code int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		rw.WriteHeader(code)
	}))
}

func runNotify(t *testing.T, a args) string {
	t.Helper()
	return toolNotify(context.Background(), a)
}

func TestNotifySendsWhenNoUIAttached(t *testing.T) {
	var hits atomic.Int64
	srv := notifyServer(&hits, http.StatusOK)
	defer srv.Close()
	notifyHome(t, srv)

	out := runNotify(t, args{"title": "build done", "body": "all green"})
	if out != "sent" {
		t.Fatalf("got %q, want sent", out)
	}
	if hits.Load() != 1 {
		t.Fatalf("channel got %d messages, want 1", hits.Load())
	}
}

func TestNotifySuppressedWhileUIAttached(t *testing.T) {
	var hits atomic.Int64
	srv := notifyServer(&hits, http.StatusOK)
	defer srv.Close()
	notifyHome(t, srv)

	detach := notify.Attach()
	out := runNotify(t, args{"title": "build done", "body": "all green"})
	if !strings.HasPrefix(out, "not sent") {
		t.Fatalf("got %q, want a not-sent result", out)
	}
	if hits.Load() != 0 {
		t.Fatalf("channel got %d messages while watched, want 0", hits.Load())
	}

	// Detaching restores delivery.
	detach()
	if out := runNotify(t, args{"title": "t", "body": "b"}); out != "sent" {
		t.Fatalf("after detach got %q, want sent", out)
	}
}

// The model chooses the message; the user chooses the transport. No tool
// output may name a channel, so the model cannot learn or target one.
func TestNotifyHidesChannelDetail(t *testing.T) {
	var hits atomic.Int64
	srv := notifyServer(&hits, http.StatusInternalServerError)
	defer srv.Close()
	notifyHome(t, srv)

	out := runNotify(t, args{"title": "t", "body": "b"})
	for _, leak := range []string{"secret-channel-name", "ntfy", srv.URL, "500"} {
		if strings.Contains(out, leak) {
			t.Fatalf("tool output %q leaked %q", out, leak)
		}
	}
	if !strings.HasPrefix(out, "error:") {
		t.Fatalf("got %q, want an error result", out)
	}
}

func TestNotifyWithoutChannels(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if out := runNotify(t, args{"title": "t", "body": "b"}); !strings.HasPrefix(out, "not sent") {
		t.Fatalf("got %q, want a not-sent result", out)
	}
}

func TestNotifyRequiresText(t *testing.T) {
	if out := runNotify(t, args{"urgency": "urgent"}); !strings.HasPrefix(out, "error:") {
		t.Fatalf("got %q, want an error result", out)
	}
}
