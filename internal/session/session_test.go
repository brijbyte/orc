package session

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/brijbyte/orc/internal/config"
)

const userItem = `{"type":"message","role":"user","content":[{"type":"input_text","text":"%s"}]}`

func appendUser(s *Session, text string) {
	s.Append(json.RawMessage(fmt.Sprintf(userItem, text)))
}

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

func TestCompactionChainListsAndDeletesAsOneConversation(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	rootID := "11111111-1111-1111-1111-111111111111"
	cfg := &config.Config{SessionID: rootID, Cwd: "/tmp"}
	root, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	appendUser(root, "original title")
	root.Close()

	cfg.SessionID = "22222222-2222-2222-2222-222222222222"
	next, err := NewCompacted(cfg, rootID, rootID)
	if err != nil {
		t.Fatal(err)
	}
	appendUser(next, "compaction summary")
	next.Close()

	if err := Pin(rootID, true); err != nil {
		t.Fatal(err)
	}
	rows, err := ListAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != cfg.SessionID ||
		rows[0].Title != "original title" || !rows[0].Pinned {
		t.Fatalf("ListAll() = %#v, want one pinned latest row with original title", rows)
	}
	if err := Pin(cfg.SessionID, false); err != nil {
		t.Fatal(err)
	}
	rows, _ = ListAll()
	if rows[0].Pinned {
		t.Fatal("conversation stayed pinned")
	}
	if err := Delete(cfg.SessionID); err != nil {
		t.Fatal(err)
	}
	rows, _ = ListAll()
	if len(rows) != 0 {
		t.Fatalf("ListAll() after delete = %#v", rows)
	}
}
