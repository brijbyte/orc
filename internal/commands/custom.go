package commands

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Custom slash commands are markdown prompts: .agents/commands/<name>.md in
// the cwd, then ~/.agents/commands. The body is sent as the user message;
// $ARGUMENTS is replaced by the argument string (or it is appended).

var errNoCustom = errors.New("no such custom command")

var customNameRE = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

type CustomCmd struct {
	Name, Desc string // Name includes the leading slash
	path       string
}

var (
	customOnce sync.Once
	customList []CustomCmd
)

func customRoots() []string {
	roots := []string{".agents/commands"}
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Join(home, ".agents/commands"))
	}
	return roots
}

// splitFrontmatter returns the frontmatter lines (nil if none) and the body.
func splitFrontmatter(text string) (fm []string, body string) {
	rest, ok := strings.CutPrefix(text, "---\n")
	if !ok {
		return nil, text
	}
	head, body, ok := strings.Cut(rest, "\n---")
	if !ok {
		return nil, text
	}
	if nl := strings.IndexByte(body, '\n'); nl >= 0 {
		body = body[nl+1:]
	} else {
		body = ""
	}
	return strings.Split(head, "\n"), body
}

// customDesc reads the frontmatter description, else the first body line.
func customDesc(text string) string {
	fm, body := splitFrontmatter(text)
	for _, line := range fm {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "description:"); ok {
			return scalar(v)
		}
	}
	for _, line := range strings.Split(body, "\n") {
		if s := strings.TrimSpace(line); s != "" {
			return s
		}
	}
	return ""
}

func scalar(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		s = s[1 : len(s)-1]
	}
	return s
}

func builtinName(name string) bool {
	for _, c := range Cmds {
		if c.Name == name {
			return true
		}
	}
	return false
}

func loadCustom() {
	seen := map[string]bool{}
	for _, root := range customRoots() {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			base, ok := strings.CutSuffix(e.Name(), ".md")
			if !ok || !customNameRE.MatchString(base) {
				continue
			}
			name := "/" + base
			if seen[name] || builtinName(name) {
				continue
			}
			path := filepath.Join(root, e.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			seen[name] = true
			customList = append(customList,
				CustomCmd{Name: name, Desc: customDesc(string(data)), path: path})
		}
	}
	sort.Slice(customList, func(i, j int) bool { return customList[i].Name < customList[j].Name })
}

// CustomCmds lists discovered custom commands for menus and /help.
func (c *Commands) CustomCmds() []CustomCmd {
	customOnce.Do(loadCustom)
	return customList
}

// customPrompt expands a custom command into the prompt to run.
func (c *Commands) customPrompt(word, arg string) (string, error) {
	for _, cc := range c.CustomCmds() {
		if cc.Name != word {
			continue
		}
		data, err := os.ReadFile(cc.path)
		if err != nil {
			return "", fmt.Errorf("cannot read %s", cc.path)
		}
		_, body := splitFrontmatter(string(data))
		body = strings.TrimSpace(body)
		if body == "" {
			return "", fmt.Errorf("empty prompt in %s", cc.path)
		}
		if strings.Contains(body, "$ARGUMENTS") {
			return strings.ReplaceAll(body, "$ARGUMENTS", arg), nil
		}
		if arg != "" {
			body += "\n\n" + arg
		}
		return body, nil
	}
	return "", errNoCustom
}
