// Package session persists history as JSONL: one _meta line, then one
// Responses-API input item per line. Format matches the C implementation.
package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/brijbyte/orc/internal/config"
)

type Session struct {
	Path   string
	Items  int
	Ctx    int64
	Model  string // from resumed _meta; empty otherwise
	Effort string
	f      *os.File
}

type Info struct {
	ID     string
	When   string // created
	Used   string // last turn written
	Title  string
	Cwd    string
	Pinned bool
}

type meta struct {
	ID     string `json:"id,omitempty"`
	Model  string `json:"model,omitempty"`
	Effort string `json:"effort,omitempty"`
	Cwd    string `json:"cwd,omitempty"`
	T      string `json:"t,omitempty"`
	Ctx    int64  `json:"ctx,omitempty"`
}

func now() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000Z") }

func New(cfg *config.Config) (*Session, error) {
	dir := config.Path("sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Session{Path: filepath.Join(dir,
		fmt.Sprintf("%d-%.8s.jsonl", time.Now().Unix(), cfg.SessionID))}
	f, err := os.OpenFile(s.Path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	s.f = f
	cwd := cfg.Cwd
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	s.writeMeta(meta{ID: cfg.SessionID, Model: cfg.Model, Effort: cfg.Effort,
		Cwd: cwd, T: now()})
	return s, nil
}

func (s *Session) writeMeta(m meta) {
	if s.f == nil {
		return
	}
	line, _ := json.Marshal(map[string]meta{"_meta": m})
	fmt.Fprintf(s.f, "%s\n", line)
	s.f.Sync()
}

func (s *Session) Append(item json.RawMessage) {
	if s.f == nil {
		return
	}
	fmt.Fprintf(s.f, "%s\n", item)
	s.f.Sync()
	s.Items++
}

func (s *Session) SetCtx(tokens int64) {
	s.Ctx = tokens
	s.writeMeta(meta{Ctx: tokens})
}

func (s *Session) SetCfg(cfg *config.Config) {
	s.writeMeta(meta{Model: cfg.Model, Effort: cfg.Effort})
}

func (s *Session) Close() {
	if s.f != nil {
		s.f.Close()
		s.f = nil
	}
}

// nameMatchesID reports whether "<ts>-<id8>.jsonl" matches an id prefix.
func nameMatchesID(name, id string) bool {
	dash := strings.IndexByte(name, '-')
	if dash < 0 || !strings.HasSuffix(name, ".jsonl") {
		return false
	}
	part := strings.TrimSuffix(name[dash+1:], ".jsonl")
	return id != "" && (strings.HasPrefix(part, id) || strings.HasPrefix(id, part))
}

// findSession returns the most recent session file, optionally matching an id
// prefix. The timestamp filename prefix makes lexically-greatest most recent.
func findSession(id string) string {
	dir := config.Path("sessions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	best := ""
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		if id != "" && !nameMatchesID(name, id) {
			continue
		}
		if name > best {
			best = name
		}
	}
	if best == "" {
		return ""
	}
	return filepath.Join(dir, best)
}

// Delete removes a session file by id prefix.
func Delete(id string) error {
	path := findSession(id)
	if path == "" {
		return fmt.Errorf("no session matching %s", id)
	}
	Pin(id, false) // no stale ids in the pinned list
	return os.Remove(path)
}

// Resume loads history from a session ref (empty = most recent, id prefix, or
// path) and reopens the file for appending. Restores cfg.SessionID.
func Resume(ref string, cfg *config.Config) (*Session, []json.RawMessage, error) {
	var resolved string
	switch {
	case ref == "":
		resolved = findSession("")
	case strings.ContainsRune(ref, '/') || fileExists(ref):
		resolved = ref
	default:
		resolved = findSession(ref)
	}
	if resolved == "" {
		if ref != "" {
			return nil, nil, fmt.Errorf("no session matching %s to resume", ref)
		}
		return nil, nil, fmt.Errorf("no session to resume")
	}

	f, err := os.Open(resolved)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot read %s", resolved)
	}
	s := &Session{Path: resolved}
	var history []json.RawMessage
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 64*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var probe struct {
			Meta *meta `json:"_meta"`
		}
		if json.Unmarshal([]byte(line), &probe) == nil && probe.Meta != nil {
			m := probe.Meta
			if m.ID != "" {
				cfg.SessionID = m.ID // prompt_cache_key survives resumes
			}
			// Tools run in the session's original directory when it still exists.
			if m.Cwd != "" {
				if st, err := os.Stat(m.Cwd); err == nil && st.IsDir() {
					cfg.Cwd = m.Cwd
				}
			}
			if m.Ctx != 0 {
				s.Ctx = m.Ctx
			}
			if m.Model != "" {
				s.Model = m.Model
			}
			if m.Effort != "" {
				s.Effort = m.Effort
			}
			continue
		}
		if json.Valid([]byte(line)) {
			history = append(history, json.RawMessage(line))
		}
	}
	scanErr := sc.Err()
	f.Close()
	if scanErr != nil {
		// Partial history would desync function_call/output pairs.
		return nil, nil, fmt.Errorf("cannot read %s: %v", resolved, scanErr)
	}

	if err := os.Chmod(resolved, 0o600); err != nil {
		return nil, nil, fmt.Errorf("cannot secure %s", resolved)
	}
	s.f, err = os.OpenFile(resolved, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot open %s", resolved)
	}
	s.Items = len(history)
	return s, history, nil
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

// listOne reads one resumable row: id, time, cwd, and first user line.
func listOne(path string) (Info, bool) {
	f, err := os.Open(path)
	if err != nil {
		return Info{}, false
	}
	defer f.Close()
	var info Info
	hasItems := false
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		var probe struct {
			Meta    *meta `json:"_meta"`
			Role    string
			Content []struct {
				Text string
			}
		}
		if json.Unmarshal(line, &probe) != nil {
			continue
		}
		if probe.Meta != nil {
			if probe.Meta.Cwd != "" {
				info.Cwd = probe.Meta.Cwd
			}
			if probe.Meta.ID != "" {
				info.ID = probe.Meta.ID
			}
			if probe.Meta.T != "" {
				info.When = probe.Meta.T
			}
			continue
		}
		hasItems = true
		if probe.Role == "user" && len(probe.Content) > 0 {
			title := probe.Content[0].Text
			info.Title = strings.Map(func(r rune) rune {
				if r == '\n' || r == '\t' {
					return ' '
				}
				return r
			}, title)
			break
		}
	}
	if sc.Err() != nil {
		return Info{}, false
	}
	info.When = strings.Replace(info.When, "T", " ", 1)
	return info, info.ID != "" && hasItems
}

// ListAll returns every session, pinned first, then most recently used. The
// file is appended on every turn, so its mtime is the last interaction.
func ListAll() ([]Info, error) {
	dir := config.Path("sessions")
	entries, _ := os.ReadDir(dir)
	pinned := map[string]bool{}
	for _, id := range config.LoadSettings().Pinned {
		pinned[id] = true
	}
	type row struct {
		Info
		used time.Time
	}
	var rows []row
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		st, err := e.Info()
		if err != nil {
			continue
		}
		info, ok := listOne(filepath.Join(dir, e.Name()))
		if !ok {
			continue
		}
		info.Used = st.ModTime().UTC().Format("2006-01-02 15:04:05.000Z")
		info.Pinned = pinned[info.ID]
		rows = append(rows, row{info, st.ModTime()})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Pinned != rows[j].Pinned {
			return rows[i].Pinned
		}
		return rows[i].used.After(rows[j].used)
	})
	out := make([]Info, len(rows))
	for i, r := range rows {
		out[i] = r.Info
	}
	return out, nil
}

// Pin adds or removes a session id from the pinned list in settings.
func Pin(id string, on bool) error {
	s := config.LoadSettings()
	kept := s.Pinned[:0:0]
	for _, p := range s.Pinned {
		if p != id {
			kept = append(kept, p)
		}
	}
	if on {
		kept = append(kept, id)
	}
	s.Pinned = kept
	return config.SaveSettings(s)
}

// List returns sessions for one directory (empty = current), in ListAll order.
func List(cwd string) ([]Info, error) {
	if cwd == "" {
		var err error
		if cwd, err = os.Getwd(); err != nil {
			return nil, fmt.Errorf("cannot get current directory")
		}
	}
	all, err := ListAll()
	if err != nil {
		return nil, err
	}
	var rows []Info
	for _, info := range all {
		if info.Cwd == cwd {
			rows = append(rows, info)
		}
	}
	return rows, nil
}
