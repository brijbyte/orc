package web

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/brijbyte/orc/internal/config"
	"github.com/google/uuid"
)

const gitOutputMax = 8 << 20

var (
	errNotRepo   = errors.New("not a git repository")
	errGitOutput = errors.New("git output is too large")
	errGitStale  = errors.New("Git diff changed; refresh and try again")
)

type gitBranch struct {
	Name    string `json:"name"`
	Ref     string `json:"ref"`
	Remote  bool   `json:"remote"`
	Current bool   `json:"current"`
}

type gitChange struct {
	Path     string `json:"path"`
	OldPath  string `json:"old_path,omitempty"`
	Status   string `json:"status"`
	Index    string `json:"index"`
	Worktree string `json:"worktree"`
	File     string `json:"file,omitempty"`
}

type gitActivity struct {
	At     string   `json:"at"`
	Action string   `json:"action"`
	Paths  []string `json:"paths"`
	Hunks  int      `json:"hunks,omitempty"`
}

type gitRecoverySummary struct {
	At     string   `json:"at"`
	Action string   `json:"action"`
	Paths  []string `json:"paths"`
	Hunks  int      `json:"hunks,omitempty"`
}

type gitRecovery struct {
	gitRecoverySummary
	Root  string `json:"root"`
	Patch []byte `json:"patch"`
	file  string
}

type gitStatus struct {
	Repo     bool                `json:"repo"`
	Root     string              `json:"root,omitempty"`
	Branch   string              `json:"branch,omitempty"`
	Detached bool                `json:"detached,omitempty"`
	Upstream string              `json:"upstream,omitempty"`
	Ahead    int                 `json:"ahead"`
	Behind   int                 `json:"behind"`
	Clean    bool                `json:"clean"`
	Changes  []gitChange         `json:"changes"`
	Branches []gitBranch         `json:"branches"`
	Activity []gitActivity       `json:"activity"`
	Recovery *gitRecoverySummary `json:"recovery,omitempty"`
}

type gitMutationRequest struct {
	Paths   []string `json:"paths"`
	Hunks   []int    `json:"hunks,omitempty"`
	Hash    string   `json:"hash,omitempty"`
	Confirm bool     `json:"confirm,omitempty"`
}

type gitCompare struct {
	Base    string      `json:"base"`
	Changes []gitChange `json:"changes"`
}

func runGit(ctx context.Context, cwd string, args ...string) ([]byte, error) {
	return runGitInput(ctx, cwd, nil, args...)
}

func runGitInput(ctx context.Context, cwd string, input []byte, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	base := []string{"-C", cwd, "--no-pager", "-c", "color.ui=false", "-c", "core.quotepath=false"}
	cmd := exec.CommandContext(ctx, "git", append(base, args...)...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "GIT_OPTIONAL_LOCKS=0")
	cmd.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	out := new(bytes.Buffer)
	_, readErr := out.ReadFrom(io.LimitReader(stdout, gitOutputMax+1))
	if out.Len() > gitOutputMax {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, errGitOutput
	}
	waitErr := cmd.Wait()
	if readErr != nil {
		return nil, readErr
	}
	if waitErr != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return out.Bytes(), fmt.Errorf("%w: %s", waitErr, msg)
		}
		return out.Bytes(), waitErr
	}
	return out.Bytes(), nil
}

func gitRoot(ctx context.Context, cwd string) (string, error) {
	out, err := runGit(ctx, cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", errNotRepo
	}
	return filepath.Clean(strings.TrimSpace(string(out))), nil
}

func (rt *Runtime) recordGitActivity(action string, paths []string, hunks int) {
	rt.gitActivityMu.Lock()
	defer rt.gitActivityMu.Unlock()
	entry := gitActivity{
		At: time.Now().UTC().Format(time.RFC3339), Action: action,
		Paths: append([]string(nil), paths...), Hunks: hunks,
	}
	rt.gitActivity = append([]gitActivity{entry}, rt.gitActivity...)
	if len(rt.gitActivity) > 50 {
		rt.gitActivity = rt.gitActivity[:50]
	}
}

func (rt *Runtime) gitActivities() []gitActivity {
	rt.gitActivityMu.Lock()
	defer rt.gitActivityMu.Unlock()
	return append([]gitActivity(nil), rt.gitActivity...)
}

func gitRecoveryDir() string {
	return config.Path("recoveries")
}

func saveGitRecovery(root, action string, paths []string, hunks int, patch []byte) (*gitRecovery, error) {
	dir := gitRecoveryDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	recovery := &gitRecovery{
		gitRecoverySummary: gitRecoverySummary{
			At: time.Now().UTC().Format(time.RFC3339Nano), Action: action,
			Paths: append([]string(nil), paths...), Hunks: hunks,
		},
		Root: root, Patch: append([]byte(nil), patch...),
	}
	recovery.file = filepath.Join(dir, fmt.Sprintf("%020d-%s.json", time.Now().UnixNano(), uuid.NewString()))
	data, err := json.Marshal(recovery)
	if err != nil {
		return nil, err
	}
	if err := config.WriteFileAtomic(recovery.file, data); err != nil {
		return nil, err
	}
	pruneGitRecoveries(dir)
	return recovery, nil
}

func pruneGitRecoveries(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			files = append(files, entry.Name())
		}
	}
	sort.Strings(files)
	for len(files) > 50 {
		_ = os.Remove(filepath.Join(dir, files[0]))
		files = files[1:]
	}
}

func latestGitRecovery(root string) (*gitRecovery, error) {
	entries, err := os.ReadDir(gitRecoveryDir())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() > entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(gitRecoveryDir(), entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var recovery gitRecovery
		if json.Unmarshal(data, &recovery) == nil && recovery.Root == root && len(recovery.Patch) > 0 {
			recovery.file = path
			return &recovery, nil
		}
	}
	return nil, nil
}

func loadGitStatus(ctx context.Context, rt *Runtime) (gitStatus, error) {
	root, err := gitRoot(ctx, rt.Cfg.Cwd)
	if err != nil {
		return gitStatus{}, err
	}
	raw, err := runGit(ctx, root, "status", "--porcelain=v2", "--branch", "-z", "--untracked-files=all")
	if err != nil {
		return gitStatus{}, err
	}
	status := gitStatus{
		Repo: true, Root: root, Changes: []gitChange{}, Branches: []gitBranch{},
		Activity: rt.gitActivities(),
	}
	parseGitStatus(raw, &status)
	branches, err := loadGitBranches(ctx, root, status.Branch)
	if err != nil {
		return gitStatus{}, err
	}
	status.Branches = branches
	status.Clean = len(status.Changes) == 0
	for i := range status.Changes {
		path, ok := repoPath(root, status.Changes[i].Path)
		if !ok {
			continue
		}
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			status.Changes[i].File = rt.IO.allowFile(path)
		}
	}
	if recovery, err := latestGitRecovery(root); err == nil && recovery != nil {
		summary := recovery.gitRecoverySummary
		status.Recovery = &summary
	}
	return status, nil
}

func parseGitStatus(raw []byte, status *gitStatus) {
	parts := bytes.Split(raw, []byte{0})
	for i := 0; i < len(parts); i++ {
		record := string(parts[i])
		if record == "" {
			continue
		}
		if strings.HasPrefix(record, "# ") {
			parseGitHeader(strings.TrimPrefix(record, "# "), status)
			continue
		}
		change, rename := parseGitChange(record)
		if rename && i+1 < len(parts) {
			i++
			change.OldPath = string(parts[i])
		}
		if change.Path != "" {
			status.Changes = append(status.Changes, change)
		}
	}
}

func parseGitHeader(header string, status *gitStatus) {
	key, value, _ := strings.Cut(header, " ")
	switch key {
	case "branch.head":
		status.Branch = value
		status.Detached = value == "(detached)"
	case "branch.upstream":
		status.Upstream = value
	case "branch.ab":
		fmt.Sscanf(value, "+%d -%d", &status.Ahead, &status.Behind)
	}
}

func parseGitChange(record string) (gitChange, bool) {
	kind := record[0]
	switch kind {
	case '?':
		return gitChange{Path: strings.TrimPrefix(record, "? "), Status: "Untracked", Index: "?", Worktree: "?"}, false
	case '1':
		fields := strings.SplitN(record, " ", 9)
		if len(fields) == 9 {
			return changeFromXY(fields[8], fields[1]), false
		}
	case '2':
		fields := strings.SplitN(record, " ", 10)
		if len(fields) == 10 {
			return changeFromXY(fields[9], fields[1]), true
		}
	case 'u':
		fields := strings.SplitN(record, " ", 11)
		if len(fields) == 11 {
			change := changeFromXY(fields[10], fields[1])
			change.Status = "Conflicted"
			return change, false
		}
	}
	return gitChange{}, false
}

func changeFromXY(path, xy string) gitChange {
	change := gitChange{Path: path, Status: "Changed", Index: ".", Worktree: "."}
	if len(xy) == 2 {
		change.Index, change.Worktree = xy[:1], xy[1:]
	}
	for _, code := range []byte(xy) {
		switch code {
		case 'U':
			change.Status = "Conflicted"
			return change
		case 'R':
			change.Status = "Renamed"
		case 'C':
			change.Status = "Copied"
		case 'A':
			if change.Status == "Changed" {
				change.Status = "Added"
			}
		case 'D':
			if change.Status == "Changed" {
				change.Status = "Deleted"
			}
		case 'M':
			if change.Status == "Changed" {
				change.Status = "Modified"
			}
		case 'T':
			if change.Status == "Changed" {
				change.Status = "Type changed"
			}
		}
	}
	return change
}

func loadGitBranches(ctx context.Context, root, current string) ([]gitBranch, error) {
	format := "%(refname)%00%(refname:short)%00%(HEAD)%00%(symref)"
	out, err := runGit(ctx, root, "for-each-ref", "--format="+format, "refs/heads", "refs/remotes")
	if err != nil {
		return nil, err
	}
	branches := []gitBranch{}
	for _, line := range bytes.Split(out, []byte{'\n'}) {
		fields := bytes.Split(line, []byte{0})
		if len(fields) < 4 || len(fields[3]) > 0 {
			continue
		}
		ref, name := string(fields[0]), string(fields[1])
		branches = append(branches, gitBranch{
			Name: name, Ref: ref, Remote: strings.HasPrefix(ref, "refs/remotes/"),
			Current: string(fields[2]) == "*" || name == current,
		})
	}
	sort.SliceStable(branches, func(i, j int) bool {
		if branches[i].Remote != branches[j].Remote {
			return !branches[i].Remote
		}
		return branches[i].Name < branches[j].Name
	})
	return branches, nil
}

func repoPath(root, path string) (string, bool) {
	if path == "" || filepath.IsAbs(path) {
		return "", false
	}
	path = filepath.Clean(filepath.FromSlash(path))
	if path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.Join(root, path), true
}

func compareBase(branches []gitBranch, ref string) (string, bool) {
	for _, branch := range branches {
		if branch.Ref == ref {
			return ref, true
		}
	}
	return "", false
}

func loadGitCompare(ctx context.Context, rt *Runtime, base string) (gitCompare, error) {
	status, err := loadGitStatus(ctx, rt)
	if err != nil {
		return gitCompare{}, err
	}
	base, ok := compareBase(status.Branches, base)
	if !ok {
		return gitCompare{}, errors.New("unknown branch")
	}
	out, err := runGit(ctx, status.Root, "diff", "--name-status", "-z", "--find-renames", base+"...HEAD", "--")
	if err != nil {
		return gitCompare{}, err
	}
	changes := parseNameStatus(out)
	for i := range changes {
		path, ok := repoPath(status.Root, changes[i].Path)
		if !ok {
			continue
		}
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			changes[i].File = rt.IO.allowFile(path)
		}
	}
	return gitCompare{Base: base, Changes: changes}, nil
}

func parseNameStatus(raw []byte) []gitChange {
	fields := bytes.Split(raw, []byte{0})
	changes := []gitChange{}
	for i := 0; i < len(fields); {
		code := string(fields[i])
		i++
		if code == "" || i >= len(fields) {
			continue
		}
		oldPath := ""
		path := string(fields[i])
		i++
		if (strings.HasPrefix(code, "R") || strings.HasPrefix(code, "C")) && i < len(fields) {
			oldPath, path = path, string(fields[i])
			i++
		}
		xy := code[:1] + "."
		change := changeFromXY(path, xy)
		change.OldPath = oldPath
		changes = append(changes, change)
	}
	return changes
}

func gitDiff(ctx context.Context, rt *Runtime, base, mode, path string) ([]byte, string, error) {
	status, err := loadGitStatus(ctx, rt)
	if err != nil {
		return nil, "", err
	}
	if path != "" {
		if _, ok := repoPath(status.Root, path); !ok {
			return nil, "", errors.New("bad path")
		}
	}
	args := []string{"diff", "--find-renames", "--src-prefix=a/", "--dst-prefix=b/"}
	if base != "" {
		base, ok := compareBase(status.Branches, base)
		if !ok {
			return nil, "", errors.New("unknown branch")
		}
		args = append(args, base+"...HEAD")
	} else {
		switch mode {
		case "", "worktree":
		case "staged":
			args = append(args, "--cached")
			if _, err := runGit(ctx, status.Root, "rev-parse", "--verify", "HEAD"); err == nil {
				args = append(args, "HEAD")
			}
		default:
			return nil, "", errors.New("unknown diff mode")
		}
	}
	args = append(args, "--")
	if path != "" {
		args = append(args, path)
	}
	out, err := runGit(ctx, status.Root, args...)
	if err != nil {
		return nil, "", err
	}
	untracked := false
	for _, change := range status.Changes {
		if change.Path == path && change.Index == "?" {
			untracked = true
			break
		}
	}
	if base == "" && mode != "staged" && untracked && len(out) == 0 {
		full, _ := repoPath(status.Root, path)
		if info, statErr := os.Stat(full); statErr == nil && info.Mode().IsRegular() {
			out, err = runGit(ctx, status.Root, "diff", "--no-index", "--src-prefix=a/", "--dst-prefix=b/", "--", "/dev/null", path)
			var exit *exec.ExitError
			if err != nil && (!errors.As(err, &exit) || exit.ExitCode() != 1) {
				return nil, "", err
			}
		}
	}
	return out, status.Root, nil
}

func mutationPaths(status gitStatus, requested []string, stage bool) ([]string, []string, error) {
	if len(requested) == 0 || len(requested) > 256 {
		return nil, nil, errors.New("select one or more files")
	}
	changes := make(map[string]gitChange, len(status.Changes))
	for _, change := range status.Changes {
		changes[change.Path] = change
	}
	seen := map[string]bool{}
	paths := make([]string, 0, len(requested)*2)
	selected := make([]string, 0, len(requested))
	for _, path := range requested {
		change, ok := changes[path]
		if !ok {
			return nil, nil, fmt.Errorf("%s is not changed", path)
		}
		action := "unstage"
		eligible := change.Index != "." && change.Index != "?"
		if stage {
			action = "stage"
			eligible = change.Worktree != "." || change.Index == "?"
		}
		if !eligible {
			return nil, nil, fmt.Errorf("%s has no changes to %s", path, action)
		}
		for _, candidate := range []string{change.Path, change.OldPath} {
			if candidate == "" || seen[candidate] {
				continue
			}
			if _, ok := repoPath(status.Root, candidate); !ok {
				return nil, nil, errors.New("bad path")
			}
			seen[candidate] = true
			paths = append(paths, candidate)
		}
		selected = append(selected, path)
	}
	return paths, selected, nil
}

func diffHash(patch []byte) string {
	sum := sha256.Sum256(patch)
	return hex.EncodeToString(sum[:])
}

func selectDiffHunks(patch []byte, selected []int) ([]byte, error) {
	lines := bytes.SplitAfter(patch, []byte{'\n'})
	starts := []int{}
	for i, line := range lines {
		if bytes.HasPrefix(line, []byte("@@ ")) {
			starts = append(starts, i)
		}
	}
	if len(starts) == 0 || len(selected) == 0 || len(selected) > len(starts) {
		return nil, errors.New("select one or more diff hunks")
	}
	wanted := make(map[int]bool, len(selected))
	for _, hunk := range selected {
		if hunk < 0 || hunk >= len(starts) || wanted[hunk] {
			return nil, errors.New("bad diff hunk")
		}
		wanted[hunk] = true
	}
	var out bytes.Buffer
	for _, line := range lines[:starts[0]] {
		out.Write(line)
	}
	for hunk, start := range starts {
		if !wanted[hunk] {
			continue
		}
		end := len(lines)
		if hunk+1 < len(starts) {
			end = starts[hunk+1]
		}
		for _, line := range lines[start:end] {
			out.Write(line)
		}
	}
	return out.Bytes(), nil
}

func gitMutate(ctx context.Context, rt *Runtime, request gitMutationRequest, stage bool) (gitStatus, error) {
	status, err := loadGitStatus(ctx, rt)
	if err != nil {
		return gitStatus{}, err
	}
	paths, selected, err := mutationPaths(status, request.Paths, stage)
	if err != nil {
		return gitStatus{}, err
	}
	if len(request.Hunks) > 0 {
		if len(selected) != 1 || request.Hash == "" {
			return gitStatus{}, errors.New("hunks require one file and a diff hash")
		}
		mode := "staged"
		if stage {
			mode = "worktree"
		}
		patch, _, err := gitDiff(ctx, rt, "", mode, selected[0])
		if err != nil {
			return gitStatus{}, err
		}
		if diffHash(patch) != request.Hash {
			return gitStatus{}, errGitStale
		}
		patch, err = selectDiffHunks(patch, request.Hunks)
		if err != nil {
			return gitStatus{}, err
		}
		args := []string{"apply", "--cached", "--whitespace=nowarn"}
		if !stage {
			args = append(args, "--reverse")
		}
		if _, err := runGitInput(ctx, status.Root, patch, append(args, "--check")...); err != nil {
			return gitStatus{}, err
		}
		if _, err := runGitInput(ctx, status.Root, patch, args...); err != nil {
			return gitStatus{}, err
		}
	} else if stage {
		if _, err := runGit(ctx, status.Root, append([]string{"add", "-A", "--"}, paths...)...); err != nil {
			return gitStatus{}, err
		}
	} else if _, err := runGit(ctx, status.Root, "rev-parse", "--verify", "HEAD"); err == nil {
		if _, err := runGit(ctx, status.Root, append([]string{"reset", "-q", "HEAD", "--"}, paths...)...); err != nil {
			return gitStatus{}, err
		}
	} else if _, err := runGit(ctx, status.Root, append([]string{"rm", "--cached", "-q", "--ignore-unmatch", "--"}, paths...)...); err != nil {
		return gitStatus{}, err
	}
	action := "unstage"
	if stage {
		action = "stage"
	}
	rt.recordGitActivity(action, selected, len(request.Hunks))
	return loadGitStatus(ctx, rt)
}

func discardPaths(status gitStatus, requested []string, untracked bool) ([]string, []string, error) {
	if len(requested) == 0 || len(requested) > 256 {
		return nil, nil, errors.New("select one or more files")
	}
	changes := make(map[string]gitChange, len(status.Changes))
	for _, change := range status.Changes {
		changes[change.Path] = change
	}
	seen := map[string]bool{}
	paths := []string{}
	selected := []string{}
	for _, path := range requested {
		change, ok := changes[path]
		if !ok || (change.Index == "?") != untracked {
			return nil, nil, fmt.Errorf("%s has no matching changes", path)
		}
		if !untracked && change.Worktree == "." {
			return nil, nil, fmt.Errorf("%s has no unstaged changes", path)
		}
		for _, candidate := range []string{change.Path, change.OldPath} {
			if candidate == "" || seen[candidate] {
				continue
			}
			if _, ok := repoPath(status.Root, candidate); !ok {
				return nil, nil, errors.New("bad path")
			}
			seen[candidate] = true
			paths = append(paths, candidate)
		}
		selected = append(selected, path)
	}
	return paths, selected, nil
}

func gitDiscard(ctx context.Context, rt *Runtime, request gitMutationRequest) (gitStatus, error) {
	if !request.Confirm {
		return gitStatus{}, errors.New("confirmation required")
	}
	status, err := loadGitStatus(ctx, rt)
	if err != nil {
		return gitStatus{}, err
	}
	paths, selected, err := discardPaths(status, request.Paths, false)
	if err != nil {
		return gitStatus{}, err
	}
	var patch []byte
	if len(request.Hunks) > 0 {
		if len(selected) != 1 || request.Hash == "" {
			return gitStatus{}, errors.New("hunks require one file and a diff hash")
		}
		patch, _, err = gitDiff(ctx, rt, "", "worktree", selected[0])
		if err != nil {
			return gitStatus{}, err
		}
		if diffHash(patch) != request.Hash {
			return gitStatus{}, errGitStale
		}
		patch, err = selectDiffHunks(patch, request.Hunks)
		if err != nil {
			return gitStatus{}, err
		}
	} else {
		args := append([]string{"diff", "--binary", "--find-renames", "--src-prefix=a/", "--dst-prefix=b/", "--"}, paths...)
		patch, err = runGit(ctx, status.Root, args...)
		if err != nil {
			return gitStatus{}, err
		}
	}
	if len(patch) == 0 {
		return gitStatus{}, errors.New("no recoverable changes")
	}
	recovery, err := saveGitRecovery(status.Root, "discard", selected, len(request.Hunks), patch)
	if err != nil {
		return gitStatus{}, fmt.Errorf("save recovery patch: %w", err)
	}
	if len(request.Hunks) > 0 {
		args := []string{"apply", "--binary", "--reverse", "--whitespace=nowarn"}
		if _, err = runGitInput(ctx, status.Root, patch, append(args, "--check")...); err == nil {
			_, err = runGitInput(ctx, status.Root, patch, args...)
		}
	} else {
		_, err = runGit(ctx, status.Root, append([]string{"restore", "--worktree", "--"}, paths...)...)
	}
	if err != nil {
		_ = os.Remove(recovery.file)
		return gitStatus{}, err
	}
	rt.recordGitActivity("discard", selected, len(request.Hunks))
	return loadGitStatus(ctx, rt)
}

func untrackedDiff(ctx context.Context, root, path string) ([]byte, error) {
	out, err := runGit(ctx, root, "diff", "--no-index", "--binary", "--src-prefix=a/", "--dst-prefix=b/", "--", "/dev/null", path)
	var exit *exec.ExitError
	if err != nil && (!errors.As(err, &exit) || exit.ExitCode() != 1) {
		return nil, err
	}
	return out, nil
}

func gitRemoveUntracked(ctx context.Context, rt *Runtime, request gitMutationRequest) (gitStatus, error) {
	if !request.Confirm {
		return gitStatus{}, errors.New("confirmation required")
	}
	status, err := loadGitStatus(ctx, rt)
	if err != nil {
		return gitStatus{}, err
	}
	paths, selected, err := discardPaths(status, request.Paths, true)
	if err != nil {
		return gitStatus{}, err
	}
	var patch bytes.Buffer
	for _, path := range paths {
		full, _ := repoPath(status.Root, path)
		info, err := os.Lstat(full)
		if err != nil || !info.Mode().IsRegular() {
			return gitStatus{}, fmt.Errorf("%s is not a regular file", path)
		}
		part, err := untrackedDiff(ctx, status.Root, path)
		if err != nil {
			return gitStatus{}, err
		}
		if patch.Len()+len(part) > gitOutputMax {
			return gitStatus{}, errGitOutput
		}
		patch.Write(part)
	}
	if patch.Len() == 0 {
		return gitStatus{}, errors.New("no recoverable changes")
	}
	recovery, err := saveGitRecovery(status.Root, "remove", selected, 0, patch.Bytes())
	if err != nil {
		return gitStatus{}, fmt.Errorf("save recovery patch: %w", err)
	}
	removed := false
	for _, path := range paths {
		full, _ := repoPath(status.Root, path)
		if err := os.Remove(full); err != nil {
			if !removed {
				_ = os.Remove(recovery.file)
			}
			return gitStatus{}, err
		}
		removed = true
	}
	rt.recordGitActivity("remove", selected, 0)
	return loadGitStatus(ctx, rt)
}

func gitUndoDiscard(ctx context.Context, rt *Runtime) (gitStatus, error) {
	status, err := loadGitStatus(ctx, rt)
	if err != nil {
		return gitStatus{}, err
	}
	recovery, err := latestGitRecovery(status.Root)
	if err != nil {
		return gitStatus{}, err
	}
	if recovery == nil {
		return gitStatus{}, errors.New("no discard to undo")
	}
	args := []string{"apply", "--binary", "--whitespace=nowarn"}
	if _, err := runGitInput(ctx, status.Root, recovery.Patch, append(args, "--check")...); err != nil {
		return gitStatus{}, err
	}
	if _, err := runGitInput(ctx, status.Root, recovery.Patch, args...); err != nil {
		return gitStatus{}, err
	}
	if err := os.Remove(recovery.file); err != nil {
		used := strings.TrimSuffix(recovery.file, ".json") + ".used"
		if renameErr := os.Rename(recovery.file, used); renameErr != nil {
			return gitStatus{}, err
		}
	}
	rt.recordGitActivity("undo", recovery.Paths, recovery.Hunks)
	return loadGitStatus(ctx, rt)
}

func gitHTTPError(rw http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errNotRepo):
		http.Error(rw, err.Error(), http.StatusNotFound)
	case errors.Is(err, errGitOutput):
		http.Error(rw, err.Error(), http.StatusRequestEntityTooLarge)
	case errors.Is(err, errGitStale):
		http.Error(rw, err.Error(), http.StatusConflict)
	default:
		http.Error(rw, err.Error(), http.StatusBadRequest)
	}
}

func (s *Server) handleGitStatus(rw http.ResponseWriter, r *http.Request, rt *Runtime) {
	status, err := loadGitStatus(r.Context(), rt)
	if errors.Is(err, errNotRepo) {
		writeJSON(rw, gitStatus{
			Repo: false, Changes: []gitChange{}, Branches: []gitBranch{}, Activity: []gitActivity{},
		})
		return
	}
	if err != nil {
		gitHTTPError(rw, err)
		return
	}
	writeJSON(rw, status)
}

func (s *Server) handleGitCompare(rw http.ResponseWriter, r *http.Request, rt *Runtime) {
	compare, err := loadGitCompare(r.Context(), rt, r.URL.Query().Get("base"))
	if err != nil {
		gitHTTPError(rw, err)
		return
	}
	writeJSON(rw, compare)
}

func (s *Server) handleGitDiff(rw http.ResponseWriter, r *http.Request, rt *Runtime) {
	patch, root, err := gitDiff(
		r.Context(), rt, r.URL.Query().Get("base"), r.URL.Query().Get("mode"),
		r.URL.Query().Get("path"),
	)
	if err != nil {
		gitHTTPError(rw, err)
		return
	}
	writeJSON(rw, map[string]any{
		"path": r.URL.Query().Get("path"), "root": root, "patch": string(patch),
		"size": len(patch), "hash": diffHash(patch),
	})
}

func (s *Server) handleGitMutation(stage bool) func(http.ResponseWriter, *http.Request, *Runtime) {
	return func(rw http.ResponseWriter, r *http.Request, rt *Runtime) {
		var request gitMutationRequest
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			http.Error(rw, "bad input", http.StatusBadRequest)
			return
		}
		rt.gitMu.Lock()
		defer rt.gitMu.Unlock()
		status, err := gitMutate(r.Context(), rt, request, stage)
		if err != nil {
			gitHTTPError(rw, err)
			return
		}
		writeJSON(rw, status)
	}
}

func (s *Server) handleGitRecoveryMutation(kind string) func(http.ResponseWriter, *http.Request, *Runtime) {
	return func(rw http.ResponseWriter, r *http.Request, rt *Runtime) {
		var request gitMutationRequest
		if kind != "undo" && json.NewDecoder(r.Body).Decode(&request) != nil {
			http.Error(rw, "bad input", http.StatusBadRequest)
			return
		}
		rt.gitMu.Lock()
		defer rt.gitMu.Unlock()
		var status gitStatus
		var err error
		switch kind {
		case "discard":
			status, err = gitDiscard(r.Context(), rt, request)
		case "remove":
			status, err = gitRemoveUntracked(r.Context(), rt, request)
		case "undo":
			status, err = gitUndoDiscard(r.Context(), rt)
		default:
			err = errors.New("unknown Git mutation")
		}
		if err != nil {
			gitHTTPError(rw, err)
			return
		}
		writeJSON(rw, status)
	}
}
