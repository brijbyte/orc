package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brijbyte/orc/internal/config"
)

type telegramCall struct {
	path string
	body map[string]string
}

// telegramStub answers as the Bot API and records what it was sent.
func telegramStub(t *testing.T, code int, reply string) *telegramCall {
	t.Helper()
	var got telegramCall
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		got.path = r.URL.Path
		json.NewDecoder(r.Body).Decode(&got.body)
		rw.WriteHeader(code)
		rw.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	old := telegramAPI
	telegramAPI = srv.URL
	t.Cleanup(func() { telegramAPI = old })
	return &got
}

func telegramChannel() config.NotifyChannel {
	return config.NotifyChannel{
		Type:    "telegram",
		Name:    "telegram",
		Enabled: true,
		Settings: map[string]string{
			"token":   "123456:secret-token",
			"chat_id": "987",
		},
	}
}

func TestTelegramSend(t *testing.T) {
	got := telegramStub(t, http.StatusOK, `{"ok":true}`)
	m := Message{Title: "build done", Body: "all green", URL: "https://orc.example/s/1"}
	if err := SendTo(context.Background(), telegramChannel(), m); err != nil {
		t.Fatal(err)
	}
	if got.path != "/bot123456:secret-token/sendMessage" {
		t.Fatalf("path %q", got.path)
	}
	if got.body["chat_id"] != "987" {
		t.Fatalf("chat_id %q", got.body["chat_id"])
	}
	for _, want := range []string{"build done", "all green", "https://orc.example/s/1"} {
		if !strings.Contains(got.body["text"], want) {
			t.Fatalf("text %q missing %q", got.body["text"], want)
		}
	}
	// Literal text: no parse_mode means stray markup cannot break the message.
	if _, ok := got.body["parse_mode"]; ok {
		t.Fatal("parse_mode must stay unset")
	}
}

func TestTelegramMarksUrgency(t *testing.T) {
	for _, tc := range []struct{ urgency, want string }{
		{"info", "hi"}, {"warn", "⚠️ hi"}, {"urgent", "🚨 hi"},
	} {
		got := telegramStub(t, http.StatusOK, `{"ok":true}`)
		m := Message{Title: "hi", Urgency: tc.urgency}
		if err := SendTo(context.Background(), telegramChannel(), m); err != nil {
			t.Fatal(err)
		}
		if got.body["text"] != tc.want {
			t.Fatalf("urgency %q gave %q, want %q", tc.urgency, got.body["text"], tc.want)
		}
	}
}

func TestTelegramReportsAPIDescription(t *testing.T) {
	telegramStub(t, http.StatusBadRequest, `{"ok":false,"description":"chat not found"}`)
	err := SendTo(context.Background(), telegramChannel(), Message{Title: "t"})
	if err == nil || !strings.Contains(err.Error(), "chat not found") {
		t.Fatalf("got %v, want the API description", err)
	}
	// The token lives in the URL; an error must not carry it back.
	if strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("error leaked the bot token: %v", err)
	}
}

func TestTelegramRequiresBothFields(t *testing.T) {
	ch := telegramChannel()
	ch.Settings = map[string]string{"token": "123:abc"}
	if err := Validate(ch); err == nil {
		t.Fatal("missing chat ID accepted")
	}
	ch.Settings = map[string]string{"chat_id": "987"}
	if err := Validate(ch); err == nil {
		t.Fatal("missing token accepted")
	}
}

func TestTelegramIsOffered(t *testing.T) {
	for _, ty := range Types() {
		if ty.ID != "telegram" {
			continue
		}
		if ty.Label != "Telegram" || len(ty.Fields) != 2 {
			t.Fatalf("bad type: %+v", ty)
		}
		if !ty.Fields[0].Secret {
			t.Fatal("bot token must be a secret field")
		}
		return
	}
	t.Fatal("telegram not registered")
}
