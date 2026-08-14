package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brijbyte/orc/internal/agent"
	"github.com/brijbyte/orc/internal/config"
	"github.com/brijbyte/orc/internal/session"
)

type testUI struct{ status string }

func (*testUI) Printf(string, ...any)     {}
func (u *testUI) SetStatus(status string) { u.status = status }

func TestSetCwdPersistsAndRebuildsInstructions(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	from, to := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(to, "AGENTS.md"), []byte("new rules"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{SessionID: "cwdtest1-1234-1234-1234-123456789abc", Cwd: from, Model: "test", Effort: "low"}
	sess, err := session.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	ag := &agent.Agent{Cfg: cfg, Sess: sess}
	ui := &testUI{}
	cmd := &Commands{cfg: cfg, ui: ui}
	if err := cmd.SetCwd(ag, to); err != nil {
		t.Fatal(err)
	}
	if cfg.Cwd != to || !strings.Contains(cfg.Instructions, "new rules") || !strings.Contains(ui.status, filepath.Base(to)) {
		t.Fatalf("cwd=%q instructions=%q status=%q", cfg.Cwd, cfg.Instructions, ui.status)
	}
	sess.Close()
	resumeCfg := &config.Config{Cwd: from}
	resumed, _, err := session.Resume(cfg.SessionID, resumeCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	if resumeCfg.Cwd != to {
		t.Fatalf("resumed cwd = %q, want %q", resumeCfg.Cwd, to)
	}
}
