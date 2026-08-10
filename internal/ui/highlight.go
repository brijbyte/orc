package ui

import (
	"encoding/json"
	"fmt"
	stdhtml "html"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

var hlStyle = styles.Get("github-dark")

// hlHTML emits class-based spans so the browser can theme them; the
// palettes ship via HighlightCSS.
var hlHTML = chromahtml.New(chromahtml.WithClasses(true), chromahtml.WithCSSComments(false),
	chromahtml.PreventSurroundingPre(true))

// HighlightCSS is the stylesheet for class-based preview spans: github-dark
// by default, github (light) when the light theme is active. Background
// rules are dropped — the preview card paints its own.
func HighlightCSS() string {
	var sb strings.Builder
	emit := func(style *chroma.Style, scope string) {
		var buf strings.Builder
		hlHTML.WriteCSS(&buf, style)
		for _, line := range strings.Split(buf.String(), "\n") {
			// keep token rules only: no wrapper/background/line-machinery
			if line == "" || strings.HasPrefix(line, ".bg") ||
				(strings.HasPrefix(line, ".chroma") && !strings.HasPrefix(line, ".chroma .")) ||
				strings.HasPrefix(line, ".chroma .hl") ||
				strings.HasPrefix(line, ".chroma .ln") ||
				strings.HasPrefix(line, ".chroma .line") {
				continue
			}
			sb.WriteString(scope + line + "\n")
		}
	}
	emit(hlStyle, "")
	emit(styles.Get("github"), `:root[data-theme="light"] `)
	return sb.String()
}

// tokenLines lexes content by the file's language; nil when unknown.
func tokenLines(path, content string) [][]chroma.Token {
	lexer := lexers.Match(path)
	if lexer == nil {
		lexer = lexers.Analyse(content)
	}
	if lexer == nil {
		return nil
	}
	it, err := chroma.Coalesce(lexer).Tokenise(nil, content)
	if err != nil {
		return nil
	}
	return chroma.SplitTokensIntoLines(it.Tokens())
}

func formatLines(tl [][]chroma.Token, f chroma.Formatter) []string {
	out := make([]string, 0, len(tl))
	for _, line := range tl {
		var sb strings.Builder
		if f.Format(&sb, hlStyle, chroma.Literator(line...)) != nil {
			return nil
		}
		out = append(out, strings.ReplaceAll(sb.String(), "\n", ""))
	}
	return out
}

// highlightTermLines renders content as ANSI-highlighted lines; nil when
// the language is unknown.
func highlightTermLines(path, content string) []string {
	tl := tokenLines(path, content)
	if tl == nil {
		return nil
	}
	return formatLines(tl, formatters.Get("terminal256"))
}

// HighlightHTML lexes file content into one HTML string per source line.
func HighlightHTML(path, text string) []string {
	if text == "" {
		return nil
	}
	tl := tokenLines(path, text)
	if tl == nil {
		return nil
	}
	return formatLines(tl, hlHTML)
}

type diffHTMLRef struct {
	line  int
	side  byte
	index int
}

// DiffHTML keeps diff markers plain and highlights source with the file lexer.
func DiffHTML(path, patch string) []string {
	lines := strings.Split(patch, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = stdhtml.EscapeString(line)
	}
	for start := 0; start < len(lines); start++ {
		if !strings.HasPrefix(lines[start], "@@ ") {
			continue
		}
		end := start + 1
		for end < len(lines) && !strings.HasPrefix(lines[end], "@@ ") &&
			!strings.HasPrefix(lines[end], "diff --git ") {
			end++
		}
		oldLines, newLines := []string{}, []string{}
		refs := []diffHTMLRef{}
		for i := start + 1; i < end; i++ {
			line := lines[i]
			if line == "" {
				continue
			}
			switch line[0] {
			case '-':
				refs = append(refs, diffHTMLRef{i, 'o', len(oldLines)})
				oldLines = append(oldLines, line[1:])
			case '+':
				refs = append(refs, diffHTMLRef{i, 'n', len(newLines)})
				newLines = append(newLines, line[1:])
			case ' ':
				refs = append(refs, diffHTMLRef{i, 'n', len(newLines)})
				oldLines = append(oldLines, line[1:])
				newLines = append(newLines, line[1:])
			}
		}
		oldHTML := HighlightHTML(path, strings.Join(oldLines, "\n"))
		newHTML := HighlightHTML(path, strings.Join(newLines, "\n"))
		for _, ref := range refs {
			highlighted := newHTML
			if ref.side == 'o' {
				highlighted = oldHTML
			}
			if ref.index < len(highlighted) {
				out[ref.line] = stdhtml.EscapeString(lines[ref.line][:1]) + highlighted[ref.index]
			}
		}
		start = end - 1
	}
	return out
}

// hlLines highlights preview text without trailing blank lines.
func hlLines(path, text string) []string {
	return HighlightHTML(path, strings.TrimRight(text, "\n"))
}

// PreviewHTML pre-highlights a tool call's preview into per-line HTML, so
// browsers need no lexer: an edit as a ± diff, write content by its path,
// clamped bash commands as shell. Lines carry the same gutter and markers
// as the text preview. nil otherwise or when the language is unknown.
func PreviewHTML(name, argsJSON string) []string {
	var a struct{ Path, Content, Cmd, Old, New string }
	if json.Unmarshal([]byte(argsJSON), &a) != nil {
		return nil
	}
	var lines []string
	half := func(marker string, hl []string, start int) {
		for i, l := range hl {
			lines = append(lines, fmt.Sprintf("%4d %s %s", start+i, marker, l))
		}
	}
	switch {
	case name == "edit":
		del, add := hlLines(a.Path, a.Old), hlLines(a.Path, a.New)
		// an unknown language falls back to the plain-text preview
		if (a.Old != "" && del == nil) || (a.New != "" && add == nil) {
			return nil
		}
		start := editStartLine(a.Path, a.Old, a.New)
		half("-", del, start)
		half("+", add, start)
	case name == "write":
		for i, l := range hlLines(a.Path, a.Content) {
			lines = append(lines, fmt.Sprintf("%4d %s", i+1, l))
		}
	case name == "bash" && (len([]rune(a.Cmd)) > DescMax || strings.Contains(a.Cmd, "\n")):
		lines = hlLines("command.sh", a.Cmd) // bare: bash has no gutter
	}
	if len(lines) == 0 {
		return nil
	}
	return lines
}
