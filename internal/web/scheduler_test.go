package web

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/brijbyte/orc/internal/config"
	"github.com/brijbyte/orc/internal/provider"
	"github.com/brijbyte/orc/internal/session"
)

type routineProvider struct {
	calls chan string
	count atomic.Int64
	sleep bool
}

func (p *routineProvider) Name() string                              { return "routine-test" }
func (p *routineProvider) DefaultModel() string                      { return "test" }
func (p *routineProvider) AuthStatus() error                         { return nil }
func (p *routineProvider) Authenticated() bool                       { return true }
func (p *routineProvider) Login(context.Context, func(string)) error { return nil }
func (p *routineProvider) Models() []provider.Model                  { return nil }
func (p *routineProvider) Turn(_ context.Context, history []json.RawMessage, _ json.RawMessage,
	_ *config.Config, cb *provider.Callbacks) error {
	var text string
	if len(history) > 0 {
		var message struct {
			Content []struct{ Text string }
		}
		json.Unmarshal(history[len(history)-1], &message)
		if len(message.Content) > 0 {
			text = message.Content[0].Text
		}
	}
	p.calls <- text
	if p.sleep {
		id := p.count.Add(1)
		call, _ := json.Marshal(map[string]any{
			"type": "function_call", "name": "sleep", "call_id": "sleep-" + time.Now().Format("150405.000000"),
			"arguments": `{"seconds":60,"reason":"check again"}`, "n": id,
		})
		cb.OnItemDone(call)
	}
	return nil
}

func TestSchedulerFiresPastWakeAndRearms(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg := &config.Config{SessionID: "abcd1234-1234-1234-1234-123456789abc", Cwd: t.TempDir(), Routine: "check time"}
	sess, err := session.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	sess.Append(json.RawMessage(`{"type":"message","role":"user","content":[{"type":"input_text","text":"check time"}]}`))
	sess.Append(json.RawMessage(`{"type":"function_call","name":"sleep","call_id":"previous-sleep","arguments":"{\"seconds\":60,\"reason\":\"last reason\"}"}`))
	sess.Append(json.RawMessage(`{"type":"function_call_output","call_id":"previous-sleep","output":"sleeping"}`))
	sess.SetWake(time.Now().UTC().Add(-time.Minute).Format(time.RFC3339))
	sess.Close()

	prov := &routineProvider{calls: make(chan string, 2), sleep: true}
	server, err := NewServer(prov, cfg, "127.0.0.1:0", "")
	if err != nil {
		t.Fatal(err)
	}
	server.scheduler.start()
	defer server.Shutdown()

	select {
	case text := <-prov.calls:
		if text != "⏰ wake: last reason" {
			t.Fatalf("wake text = %q", text)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("scheduler did not fire")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rows, _ := session.ListAll()
		if len(rows) == 1 && rows[0].Wake != "" {
			wake, _ := time.Parse(time.RFC3339, rows[0].Wake)
			if wake.After(time.Now()) {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("routine was not rearmed")
}

func TestRoutineStopsAfterMissingToolsTwice(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg := &config.Config{SessionID: "efgh5678-1234-1234-1234-123456789abc", Cwd: t.TempDir(), Routine: "check time"}
	sess, err := session.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	prov := &routineProvider{calls: make(chan string, 2)}
	rt := NewRuntime(prov, cfg, sess, nil, false)
	rt.IO.q.push(cfg.Routine, nil, false)
	defer rt.Close()

	for _, want := range []string{"check time", "call sleep or stop"} {
		select {
		case got := <-prov.calls:
			if got != want {
				t.Fatalf("turn = %q, want %q", got, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("missing turn %q", want)
		}
	}
	deadline := time.Now().Add(time.Second)
	for rt.IO.Busy() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if cfg.Routine != "" || sess.Routine != "" {
		t.Fatalf("routine remained active: config=%q session=%q", cfg.Routine, sess.Routine)
	}
}
