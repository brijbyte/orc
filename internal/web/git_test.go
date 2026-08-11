package web

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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
		patch, root, err := gitDiff(context.Background(), rt, "", "worktree", test.path)
		if err != nil {
			t.Fatalf("diff %s: %v", test.path, err)
		}
		if filepath.Base(root) != filepath.Base(dir) || !strings.Contains(string(patch), test.want) {
			t.Fatalf("diff %s root=%q:\n%s", test.path, root, patch)
		}
	}
}

func TestGitStageAndUnstageFiles(t *testing.T) {
	rt, dir := testRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "tracked file.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	status, err := gitMutate(context.Background(), rt, gitMutationRequest{
		Paths: []string{"tracked file.txt", "new.txt"},
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range status.Changes {
		if change.Index == "." || change.Index == "?" {
			t.Fatalf("change was not staged: %#v", change)
		}
	}
	status, err = gitMutate(context.Background(), rt, gitMutationRequest{
		Paths: []string{"tracked file.txt", "new.txt"},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]gitChange{}
	for _, change := range status.Changes {
		got[change.Path] = change
	}
	if got["tracked file.txt"].Index != "." || got["new.txt"].Index != "?" {
		t.Fatalf("changes were not unstaged: %#v", got)
	}
	if len(status.Activity) != 2 || status.Activity[0].Action != "unstage" {
		t.Fatalf("activity = %#v", status.Activity)
	}
}

func TestGitCommitStagedChanges(t *testing.T) {
	rt, dir := testRepo(t)
	path := filepath.Join(dir, "tracked file.txt")
	if err := os.WriteFile(path, []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	testGit(t, dir, "add", "tracked file.txt")
	if err := os.WriteFile(path, []byte("three\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	status, err := gitCommit(context.Background(), rt, gitCommitRequest{Message: "update tracked file"})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(testGit(t, dir, "log", "-1", "--format=%s")); got != "update tracked file" {
		t.Fatalf("commit subject = %q", got)
	}
	if got := testGit(t, dir, "show", "HEAD:tracked file.txt"); got != "two\n" {
		t.Fatalf("committed content = %q", got)
	}
	if len(status.Changes) != 1 || status.Changes[0].Index != "." || status.Changes[0].Worktree != "M" {
		t.Fatalf("status = %#v", status.Changes)
	}
	if len(status.Activity) != 1 || status.Activity[0].Action != "commit" {
		t.Fatalf("activity = %#v", status.Activity)
	}
}

func TestGitCommitRequiresMessageAndStagedChanges(t *testing.T) {
	rt, dir := testRepo(t)
	if _, err := gitCommit(context.Background(), rt, gitCommitRequest{Message: "  "}); err == nil {
		t.Fatal("blank commit message was accepted")
	}
	if err := os.WriteFile(filepath.Join(dir, "tracked file.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gitCommit(context.Background(), rt, gitCommitRequest{Message: "unstaged"}); err == nil || err.Error() != "no staged changes" {
		t.Fatalf("error = %v", err)
	}
}

func TestGitStageAndUnstageHunks(t *testing.T) {
	rt, dir := testRepo(t)
	lines := make([]string, 24)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %02d", i+1)
	}
	path := filepath.Join(dir, "hunks.txt")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	testGit(t, dir, "add", "hunks.txt")
	testGit(t, dir, "commit", "-m", "add hunks")
	lines[1] = "first change"
	lines[21] = "second change"
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	patch, _, err := gitDiff(context.Background(), rt, "", "worktree", "hunks.txt")
	if err != nil || bytes.Count(patch, []byte("\n@@ ")) != 2 {
		t.Fatalf("worktree patch err=%v:\n%s", err, patch)
	}
	status, err := gitMutate(context.Background(), rt, gitMutationRequest{
		Paths: []string{"hunks.txt"}, Hunks: []int{0}, Hash: diffHash(patch),
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Changes) != 1 || status.Changes[0].Index != "M" || status.Changes[0].Worktree != "M" {
		t.Fatalf("partly staged status = %#v", status.Changes)
	}
	staged, _, err := gitDiff(context.Background(), rt, "", "staged", "hunks.txt")
	if err != nil || !strings.Contains(string(staged), "first change") || strings.Contains(string(staged), "second change") {
		t.Fatalf("staged patch err=%v:\n%s", err, staged)
	}
	_, err = gitMutate(context.Background(), rt, gitMutationRequest{
		Paths: []string{"hunks.txt"}, Hunks: []int{0}, Hash: diffHash(staged),
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	staged, _, err = gitDiff(context.Background(), rt, "", "staged", "hunks.txt")
	if err != nil || len(staged) != 0 {
		t.Fatalf("staged patch after unstage err=%v:\n%s", err, staged)
	}
}

func TestGitHunkRejectsStaleDiff(t *testing.T) {
	rt, dir := testRepo(t)
	path := filepath.Join(dir, "tracked file.txt")
	if err := os.WriteFile(path, []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := gitMutate(context.Background(), rt, gitMutationRequest{
		Paths: []string{"tracked file.txt"}, Hunks: []int{0}, Hash: "stale",
	}, true)
	if !errors.Is(err, errGitStale) {
		t.Fatalf("error = %v", err)
	}
}

func TestGitDiscardFileAndUndo(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	rt, dir := testRepo(t)
	path := filepath.Join(dir, "tracked file.txt")
	if err := os.WriteFile(path, []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	status, err := gitDiscard(context.Background(), rt, gitMutationRequest{
		Paths: []string{"tracked file.txt"}, Confirm: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	content, _ := os.ReadFile(path)
	if string(content) != "one\n" || status.Recovery == nil {
		t.Fatalf("discard content=%q recovery=%#v", content, status.Recovery)
	}
	status, err = gitUndoDiscard(context.Background(), rt)
	if err != nil {
		t.Fatal(err)
	}
	content, _ = os.ReadFile(path)
	if string(content) != "two\n" || status.Recovery != nil {
		t.Fatalf("undo content=%q recovery=%#v", content, status.Recovery)
	}
}

func TestGitDiscardHunkAndUndo(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	rt, dir := testRepo(t)
	lines := make([]string, 24)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %02d", i+1)
	}
	path := filepath.Join(dir, "hunks.txt")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	testGit(t, dir, "add", "hunks.txt")
	testGit(t, dir, "commit", "-m", "add hunks")
	lines[1], lines[21] = "first change", "second change"
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	patch, _, err := gitDiff(context.Background(), rt, "", "worktree", "hunks.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, err = gitDiscard(context.Background(), rt, gitMutationRequest{
		Paths: []string{"hunks.txt"}, Hunks: []int{0}, Hash: diffHash(patch), Confirm: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	content, _ := os.ReadFile(path)
	if strings.Contains(string(content), "first change") || !strings.Contains(string(content), "second change") {
		t.Fatalf("content after discard:\n%s", content)
	}
	if _, err := gitUndoDiscard(context.Background(), rt); err != nil {
		t.Fatal(err)
	}
	content, _ = os.ReadFile(path)
	if !strings.Contains(string(content), "first change") || !strings.Contains(string(content), "second change") {
		t.Fatalf("content after undo:\n%s", content)
	}
}

func TestGitRemoveUntrackedAndUndo(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	rt, dir := testRepo(t)
	path := filepath.Join(dir, "new.txt")
	if err := os.WriteFile(path, []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gitRemoveUntracked(context.Background(), rt, gitMutationRequest{
		Paths: []string{"new.txt"}, Confirm: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("removed file stat error = %v", err)
	}
	if _, err := gitUndoDiscard(context.Background(), rt); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "new\n" {
		t.Fatalf("restored content=%q err=%v", content, err)
	}
}

func TestGitDiscardRestoresDeletedFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	rt, dir := testRepo(t)
	path := filepath.Join(dir, "tracked file.txt")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := gitDiscard(context.Background(), rt, gitMutationRequest{
		Paths: []string{"tracked file.txt"}, Confirm: true,
	}); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "one\n" {
		t.Fatalf("restored content=%q err=%v", content, err)
	}
}

func TestGitDiscardRequiresConfirmation(t *testing.T) {
	rt, _ := testRepo(t)
	_, err := gitDiscard(context.Background(), rt, gitMutationRequest{Paths: []string{"tracked file.txt"}})
	if err == nil || err.Error() != "confirmation required" {
		t.Fatalf("error = %v", err)
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
	patch, _, err := gitDiff(context.Background(), rt, "refs/heads/main", "", "feature.go")
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
