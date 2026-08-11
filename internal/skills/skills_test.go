package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSkill(t *testing.T, root, name string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: test skill\n---\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestQuerySeesSkillsAddedAfterPreviousQuery(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	t.Setenv("HOME", home)

	global := filepath.Join(home, ".agents", "skills")
	writeSkill(t, global, "before")
	if got := Query(cwd, "before"); !strings.Contains(got, "before — test skill") {
		t.Fatalf("initial global skill not found: %s", got)
	}

	writeSkill(t, global, "after")
	if got := Query(cwd, "after"); !strings.Contains(got, "after — test skill") {
		t.Fatalf("skill added after first query not found: %s", got)
	}
}
