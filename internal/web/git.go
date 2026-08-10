package web

import (
	"bytes"
	"context"
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

	"github.com/brijbyte/orc/internal/ui"
)

const gitOutputMax = 8 << 20

var (
	errNotRepo   = errors.New("not a git repository")
	errGitOutput = errors.New("git output is too large")
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

type gitStatus struct {
	Repo     bool        `json:"repo"`
	Root     string      `json:"root,omitempty"`
	Branch   string      `json:"branch,omitempty"`
	Detached bool        `json:"detached,omitempty"`
	Upstream string      `json:"upstream,omitempty"`
	Ahead    int         `json:"ahead"`
	Behind   int         `json:"behind"`
	Clean    bool        `json:"clean"`
	Changes  []gitChange `json:"changes"`
	Branches []gitBranch `json:"branches"`
}

type gitCompare struct {
	Base    string      `json:"base"`
	Changes []gitChange `json:"changes"`
}

func runGit(ctx context.Context, cwd string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	base := []string{"-C", cwd, "--no-pager", "-c", "color.ui=false", "-c", "core.quotepath=false"}
	cmd := exec.CommandContext(ctx, "git", append(base, args...)...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "GIT_OPTIONAL_LOCKS=0")
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

func loadGitStatus(ctx context.Context, rt *Runtime) (gitStatus, error) {
	root, err := gitRoot(ctx, rt.Cfg.Cwd)
	if err != nil {
		return gitStatus{}, err
	}
	raw, err := runGit(ctx, root, "status", "--porcelain=v2", "--branch", "-z", "--untracked-files=all")
	if err != nil {
		return gitStatus{}, err
	}
	status := gitStatus{Repo: true, Root: root, Changes: []gitChange{}, Branches: []gitBranch{}}
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

func gitDiff(ctx context.Context, rt *Runtime, base, path string) ([]byte, string, error) {
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
	} else if _, err := runGit(ctx, status.Root, "rev-parse", "--verify", "HEAD"); err == nil {
		args = append(args, "HEAD")
	} else {
		args = append(args, "--cached")
	}
	args = append(args, "--")
	if path != "" {
		args = append(args, path)
	}
	out, err := runGit(ctx, status.Root, args...)
	if err != nil {
		return nil, "", err
	}
	if base == "" && path != "" && len(out) == 0 {
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

func gitHTTPError(rw http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errNotRepo):
		http.Error(rw, err.Error(), http.StatusNotFound)
	case errors.Is(err, errGitOutput):
		http.Error(rw, err.Error(), http.StatusRequestEntityTooLarge)
	default:
		http.Error(rw, err.Error(), http.StatusBadRequest)
	}
}

func (s *Server) handleGitStatus(rw http.ResponseWriter, r *http.Request, rt *Runtime) {
	status, err := loadGitStatus(r.Context(), rt)
	if errors.Is(err, errNotRepo) {
		writeJSON(rw, gitStatus{Repo: false, Changes: []gitChange{}, Branches: []gitBranch{}})
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
	patch, root, err := gitDiff(r.Context(), rt, r.URL.Query().Get("base"), r.URL.Query().Get("path"))
	if err != nil {
		gitHTTPError(rw, err)
		return
	}
	writeJSON(rw, map[string]any{
		"path": r.URL.Query().Get("path"), "root": root, "patch": string(patch),
		"html": ui.HighlightHTML("changes.diff", string(patch)), "size": len(patch),
	})
}
