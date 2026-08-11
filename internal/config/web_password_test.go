package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"testing"
)

func TestWebPasswordIsHashedAndPersistsUntilRotation(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	first, created, err := EnsureWebAuth()
	if err != nil || !created || first == "" {
		t.Fatalf("first password: created=%v err=%v", created, err)
	}
	password, created, err := EnsureWebAuth()
	if err != nil || created || password != "" {
		t.Fatalf("second password: created=%v password=%q err=%v", created, password, err)
	}
	valid, err := VerifyWebPassword(first)
	if err != nil || !valid {
		t.Fatalf("verify first password: valid=%v err=%v", valid, err)
	}
	data, err := os.ReadFile(Path("auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(first)) {
		t.Fatal("auth store contains the plain password")
	}
	var root map[string]json.RawMessage
	if json.Unmarshal(data, &root) != nil || root[webAuthSection] == nil {
		t.Fatalf("auth store = %s", data)
	}

	rotated, err := RotateWebPassword()
	if err != nil || rotated == first {
		t.Fatalf("rotated password: changed=%v err=%v", rotated != first, err)
	}
	oldValid, _ := VerifyWebPassword(first)
	newValid, err := VerifyWebPassword(rotated)
	if err != nil || oldValid || !newValid {
		t.Fatalf("rotation: old=%v new=%v err=%v", oldValid, newValid, err)
	}
	st, err := os.Stat(Path("auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("auth mode = %v", st.Mode().Perm())
	}
}

func TestChangeWebPasswordRequiresCurrentAndInvalidatesSessions(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	current, _, err := EnsureWebAuth()
	if err != nil {
		t.Fatal(err)
	}
	oldKey, err := WebSessionKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := ChangeWebPassword("wrong", "replacement password"); !errors.Is(err, ErrInvalidWebPassword) {
		t.Fatalf("wrong current password error = %v", err)
	}
	if err := ChangeWebPassword(current, "replacement password"); err != nil {
		t.Fatal(err)
	}
	valid, err := VerifyWebPassword("replacement password")
	newKey, keyErr := WebSessionKey()
	if err != nil || keyErr != nil || !valid || newKey == oldKey {
		t.Fatalf("changed password: valid=%v key changed=%v errors=%v, %v", valid, newKey != oldKey, err, keyErr)
	}
}

func TestWebAuthPreservesProviderSections(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := os.MkdirAll(Home(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path("auth.json"), []byte(`{"codex":{"token":"kept"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := EnsureWebAuth(); err != nil {
		t.Fatal(err)
	}
	root, err := loadAuthRoot()
	if err != nil || !bytes.Contains(root["codex"], []byte("kept")) {
		t.Fatalf("codex section was not preserved: %s, %v", root["codex"], err)
	}
}
