package web

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brijbyte/orc/internal/config"
)

func testGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func testRepo(t *testing.T) (*Runtime, string) {
	t.Helper()
	dir := t.TempDir()
	testGit(t, dir, "init", "-b", "main")
	testGit(t, dir, "config", "user.email", "orc@example.com")
	testGit(t, dir, "config", "user.name", "orc")
	if err := os.WriteFile(filepath.Join(dir, "tracked file.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	testGit(t, dir, "add", ".")
	testGit(t, dir, "commit", "-m", "initial")
	cfg := &config.Config{Cwd: dir}
	return &Runtime{Cfg: cfg, IO: NewIO(cfg)}, dir
}

func TestLoadGitStatusIncludesChangesAndBranches(t *testing.T) {
	rt, dir := testRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "tracked file.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	status, err := loadGitStatus(context.Background(), rt)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Repo || status.Clean || status.Branch != "main" || len(status.Changes) != 2 {
		t.Fatalf("status = %#v", status)
	}
	got := map[string]gitChange{}
	for _, change := range status.Changes {
		got[change.Path] = change
	}
	if got["tracked file.txt"].Status != "Modified" || got["new.txt"].Status != "Untracked" {
		t.Fatalf("changes = %#v", got)
	}
	if got["tracked file.txt"].File == "" || got["new.txt"].File == "" {
		t.Fatalf("missing file references: %#v", got)
	}
	if len(status.Branches) != 1 || !status.Branches[0].Current {
		t.Fatalf("branches = %#v", status.Branches)
	}
}

func TestGitDiffIncludesTrackedAndUntrackedFiles(t *testing.T) {
	rt, dir := testRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "tracked file.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		path string
		want string
	}{
		{"tracked file.txt", "+two"},
		{"new.txt", "+new"},
	} {
		patch, root, err := gitDiff(context.Background(), rt, "", test.path)
		if err != nil {
			t.Fatalf("diff %s: %v", test.path, err)
		}
		if filepath.Base(root) != filepath.Base(dir) || !strings.Contains(string(patch), test.want) {
			t.Fatalf("diff %s root=%q:\n%s", test.path, root, patch)
		}
	}
}

func TestGitBranchCompare(t *testing.T) {
	rt, dir := testRepo(t)
	testGit(t, dir, "checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(dir, "feature.go"), []byte("package feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	testGit(t, dir, "add", ".")
	testGit(t, dir, "commit", "-m", "feature")
	compare, err := loadGitCompare(context.Background(), rt, "refs/heads/main")
	if err != nil {
		t.Fatal(err)
	}
	if len(compare.Changes) != 1 || compare.Changes[0].Path != "feature.go" || compare.Changes[0].Status != "Added" {
		t.Fatalf("compare = %#v", compare)
	}
	patch, _, err := gitDiff(context.Background(), rt, "refs/heads/main", "feature.go")
	if err != nil || !strings.Contains(string(patch), "+package feature") {
		t.Fatalf("patch err=%v:\n%s", err, patch)
	}
}

func TestRepoPathRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{"../secret", "/etc/passwd", ""} {
		if got, ok := repoPath(root, path); ok {
			t.Errorf("repoPath(%q) = %q", path, got)
		}
	}
}
