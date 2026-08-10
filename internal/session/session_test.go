package session

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/brijbyte/orc/internal/config"
)

func TestListAllSkipsEmptySession(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg := &config.Config{SessionID: "12345678-1234-1234-1234-123456789abc", Cwd: "/tmp"}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	info, err := os.Stat(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("session mode = %v", info.Mode().Perm())
	}

	rows, err := ListAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("ListAll() returned %d rows for an empty session", len(rows))
	}

	s.Append(json.RawMessage(`{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}`))
	rows, err = ListAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Title != "hello" {
		t.Fatalf("ListAll() = %#v, want one titled row", rows)
	}
}
