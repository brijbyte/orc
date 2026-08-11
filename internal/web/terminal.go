package web

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

const (
	terminalCols = 80
	terminalRows = 24
)

type terminalMessage struct {
	Type string `json:"type"`
	Data string `json:"data,omitempty"`
	Cols uint16 `json:"cols,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
}

type terminalSession struct {
	conn *websocket.Conn
	pty  *os.File
	cmd  *exec.Cmd
	done chan struct{}
	once sync.Once
}

var terminalUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 32 << 10,
}

func terminalEnvironment() []string {
	env := make([]string, 0, len(os.Environ())+3)
	for _, value := range os.Environ() {
		name, _, _ := strings.Cut(value, "=")
		switch name {
		case "TERM", "COLORTERM", "TERM_PROGRAM":
			continue
		}
		env = append(env, value)
	}
	return append(env, "TERM=xterm-256color", "COLORTERM=truecolor", "TERM_PROGRAM=orc")
}

func newTerminalSession(conn *websocket.Conn, cwd string) (*terminalSession, error) {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	path, err := exec.LookPath(shell)
	if err != nil {
		return nil, fmt.Errorf("shell not found")
	}
	cmd := exec.Command(path)
	cmd.Dir = cwd
	cmd.Env = terminalEnvironment()
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: terminalCols, Rows: terminalRows})
	if err != nil {
		return nil, err
	}
	t := &terminalSession{conn: conn, pty: ptmx, cmd: cmd, done: make(chan struct{})}
	go func() {
		_ = cmd.Wait()
	}()
	return t, nil
}

func (t *terminalSession) Close() error {
	t.once.Do(func() {
		close(t.done)
		_ = t.conn.Close()
		_ = t.pty.Close()
		_ = t.cmd.Process.Kill()
	})
	return nil
}

func (t *terminalSession) watchAuth(cookie string) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-t.done:
			return
		case <-ticker.C:
			if !validAuthCookie(cookie) {
				_ = t.Close()
				return
			}
		}
	}
}

func (t *terminalSession) readInput() {
	defer t.Close()
	t.conn.SetReadLimit(1 << 20)
	for {
		_, data, err := t.conn.ReadMessage()
		if err != nil {
			return
		}
		var message terminalMessage
		if json.Unmarshal(data, &message) != nil {
			continue
		}
		switch message.Type {
		case "input":
			_, _ = io.WriteString(t.pty, message.Data)
		case "resize":
			if message.Cols >= 2 && message.Cols <= 1000 &&
				message.Rows >= 1 && message.Rows <= 1000 {
				_ = pty.Setsize(t.pty, &pty.Winsize{Cols: message.Cols, Rows: message.Rows})
			}
		}
	}
}

func (t *terminalSession) serve() {
	defer t.Close()
	go t.readInput()
	buffer := make([]byte, 32<<10)
	for {
		n, err := t.pty.Read(buffer)
		if n > 0 && t.conn.WriteMessage(websocket.BinaryMessage, buffer[:n]) != nil {
			return
		}
		if err != nil {
			select {
			case <-t.done:
			default:
				_ = t.conn.WriteControl(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, "shell exited"),
					time.Now().Add(time.Second))
			}
			return
		}
	}
}

func (s *Server) handleTerminal(rw http.ResponseWriter, r *http.Request, rt *Runtime) {
	conn, err := terminalUpgrader.Upgrade(rw, r, nil)
	if err != nil {
		return
	}
	t, err := newTerminalSession(conn, rt.Cfg.Cwd)
	if err != nil {
		_ = conn.WriteMessage(websocket.BinaryMessage, []byte("\r\norc: cannot start shell\r\n"))
		_ = conn.Close()
		return
	}
	if !rt.addTerminal(t) {
		_ = t.Close()
		return
	}
	defer rt.removeTerminal(t)
	if cookie, err := r.Cookie(authCookie); err == nil {
		go t.watchAuth(cookie.Value)
	}
	t.serve()
}
