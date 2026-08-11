package web

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brijbyte/orc/internal/config"
	"github.com/gorilla/websocket"
)

func TestTerminalRunsShellInSessionDirectory(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	dir := t.TempDir()
	rt := &Runtime{Cfg: &config.Config{Cwd: dir}}
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		new(Server).handleTerminal(rw, r, rt)
	}))
	defer server.Close()

	header := http.Header{"Origin": []string{server.URL}}
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), header)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(terminalMessage{Type: "resize", Cols: 100, Rows: 40}); err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(terminalMessage{
		Type: "input",
		Data: "printf 'ORC_TEST:%s:%s:%s\\n' \"$PWD\" \"$TERM\" \"$TERM_PROGRAM\"; stty size; exit\n",
	}); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var output strings.Builder
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := "ORC_TEST:" + resolved + ":xterm-256color:orc"
	for !strings.Contains(output.String(), want) || !strings.Contains(output.String(), "40 100") {
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("terminal output: %v: %q", err, output.String())
		}
		output.Write(data)
	}
	for {
		_, _, err := conn.ReadMessage()
		if err == nil {
			continue
		}
		var closeError *websocket.CloseError
		if !errors.As(err, &closeError) || closeError.Code != websocket.CloseNormalClosure {
			t.Fatalf("terminal close = %v", err)
		}
		break
	}
}
