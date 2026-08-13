package codex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/brijbyte/orc/internal/config"
	"github.com/brijbyte/orc/internal/provider"
)

func TestOnceTimesOutIdleStream(t *testing.T) {
	oldClient, oldIdle := turnHTTPClient, streamIdleTimeout
	streamIdleTimeout = 40 * time.Millisecond
	defer func() { turnHTTPClient, streamIdleTimeout = oldClient, oldIdle }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-time.After(time.Second)
	}))
	defer srv.Close()
	turnHTTPClient = &http.Client{Transport: rewriteTransport{target: srv.URL, base: http.DefaultTransport}}

	st := &streamState{cb: &provider.Callbacks{}}
	status, err := new(Codex).once(context.Background(), []byte(`{}`), "token", "account",
		&config.Config{SessionID: "test"}, st)
	if status != http.StatusOK || err == nil || !strings.Contains(err.Error(), "idle timeout") {
		t.Fatalf("once = status %d, err %v", status, err)
	}
}

type rewriteTransport struct {
	target string
	base   http.RoundTripper
}

func (r rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL, _ = req.URL.Parse(r.target)
	return r.base.RoundTrip(clone)
}

func TestCodexContextWindowOverridesPublicAPIMetadata(t *testing.T) {
	models := []provider.Model{
		{Slug: "gpt-5.6-sol", ContextWindow: 1_050_000},
		{Slug: "other", ContextWindow: 400_000},
	}
	got := applyCodexContextWindows(models)
	if got[0].ContextWindow != 272_000 || got[1].ContextWindow != 400_000 {
		t.Fatalf("context windows = %d, %d", got[0].ContextWindow, got[1].ContextWindow)
	}
}

func TestReadSSE(t *testing.T) {
	var text string
	st := &streamState{cb: &provider.Callbacks{OnTextDelta: func(s string) { text += s }}}
	err := readSSE(strings.NewReader("data: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n\n"), st)
	if err != nil || text != "hi" || len(st.items) != 0 {
		t.Fatalf("readSSE = %q, %v, %#v", text, err, st.items)
	}
}
