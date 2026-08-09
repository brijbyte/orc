// Package skills discovers SKILL.md files under .agents/skills directories
// (cwd up to the git root, plus ~/.agents/skills) and searches them.
package skills

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

type skill struct {
	name        string
	description string
	path        string
}

var (
	once     sync.Once
	index    []skill
	warnings strings.Builder
	visited  map[string]bool
)

var nameRE = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func warn(path, reason string) {
	warnings.WriteString("warning: " + path + ": " + reason + "\n")
}

// parseSkill reads name and description from SKILL.md frontmatter.
func parseSkill(path string) (name, description string, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		warn(path, "cannot read SKILL.md")
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 4096), 1024*1024)
	if !sc.Scan() || strings.TrimSpace(sc.Text()) != "---" {
		warn(path, "missing frontmatter")
		return
	}
	closed := false
	tooLong := false
	for sc.Scan() {
		line := sc.Text()
		if len(line) > 2048 {
			tooLong = true
		}
		text := strings.TrimSpace(line)
		if text == "---" {
			closed = true
			break
		}
		key, value, found := strings.Cut(text, ":")
		if !found {
			continue
		}
		switch strings.TrimSpace(key) {
		case "name":
			name = scalar(value)
		case "description":
			description = scalar(value)
		}
	}
	reason := ""
	switch {
	case tooLong:
		reason = "frontmatter line is too long"
	case !closed:
		reason = "unterminated frontmatter"
	case name == "" || len(name) > 64 || !nameRE.MatchString(name):
		reason = "invalid name"
	case description == "":
		reason = "missing description"
	case len(description) > 1024:
		reason = "description is too long"
	}
	if reason != "" {
		warn(path, reason)
		return "", "", false
	}
	return name, description, true
}

// scalar strips whitespace and one level of quotes from a YAML scalar.
func scalar(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		s = s[1 : len(s)-1]
	}
	return s
}

func haveName(name string) bool {
	for _, s := range index {
		if s.name == name {
			return true
		}
	}
	return false
}

func addSkill(file string) {
	resolved, err := filepath.EvalSymlinks(file)
	if err != nil {
		return
	}
	resolved, _ = filepath.Abs(resolved)
	name, description, ok := parseSkill(resolved)
	if !ok || haveName(name) {
		return
	}
	index = append(index, skill{name, description, resolved})
}

func sortedDirs(path string) []string {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		name := e.Name()
		if name[0] == '.' || name == "node_modules" {
			continue
		}
		child := filepath.Join(path, name)
		if st, err := os.Stat(child); err == nil && st.IsDir() {
			names = append(names, child)
		}
	}
	sort.Strings(names)
	return names
}

func discoverDir(path string) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || visited[resolved] {
		return
	}
	visited[resolved] = true
	file := filepath.Join(resolved, "SKILL.md")
	if st, err := os.Stat(file); err == nil && st.Mode().IsRegular() {
		addSkill(file)
		return
	}
	for _, child := range sortedDirs(resolved) {
		discoverDir(child)
	}
}

func discoverRoot(root string) {
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil || visited["root:"+resolved] {
		return
	}
	visited["root:"+resolved] = true
	for _, child := range sortedDirs(resolved) {
		discoverDir(child)
	}
}

func load() {
	visited = map[string]bool{}
	current, err := os.Getwd()
	if err != nil {
		return
	}
	for {
		discoverRoot(filepath.Join(current, ".agents/skills"))
		if _, err := os.Lstat(filepath.Join(current, ".git")); err == nil {
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	if home, err := os.UserHomeDir(); err == nil {
		discoverRoot(filepath.Join(home, ".agents/skills"))
	}
}

// Query returns matching skills: exact name match first, else case-insensitive
// substring over names and descriptions. Empty or "*" lists all.
func Query(query string) string {
	once.Do(load)
	if query == "*" {
		query = ""
	}
	var out strings.Builder
	out.WriteString(warnings.String())

	appendResult := func(s skill) {
		out.WriteString(s.name + " — " + s.description + "\n" + s.path + "\n")
	}
	found := false
	for _, s := range index {
		if s.name == query {
			appendResult(s)
			found = true
			break
		}
	}
	if !found {
		q := strings.ToLower(query)
		for _, s := range index {
			if strings.Contains(strings.ToLower(s.name), q) ||
				strings.Contains(strings.ToLower(s.description), q) {
				appendResult(s)
				found = true
			}
		}
	}
	if !found {
		if query != "" {
			out.WriteString("No matching skills found.\n")
		} else {
			out.WriteString("No skills found.\n")
		}
	}
	return out.String()
}
