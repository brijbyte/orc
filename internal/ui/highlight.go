package ui

import (
	"encoding/json"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

var hlStyle = styles.Get("github-dark")

// hlHTML emits class-based spans so the browser can theme them; the
// palettes ship via HighlightCSS.
var hlHTML = html.New(html.WithClasses(true), html.WithCSSComments(false),
	html.PreventSurroundingPre(true))

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

// PreviewHTML pre-highlights a tool call's code preview into per-line HTML
// with inline styles, so browsers need no highlighter: write content by its
// path, clamped bash commands as shell. nil otherwise or when the language
// is unknown.
func PreviewHTML(name, argsJSON string) []string {
	var a struct{ Path, Content, Cmd string }
	if json.Unmarshal([]byte(argsJSON), &a) != nil {
		return nil
	}
	var path, src string
	switch {
	case name == "write" && a.Content != "":
		path, src = a.Path, a.Content
	case name == "bash" && (len([]rune(a.Cmd)) > DescMax || strings.Contains(a.Cmd, "\n")):
		path, src = "command.sh", a.Cmd
	default:
		return nil
	}
	tl := tokenLines(path, strings.TrimRight(src, "\n"))
	if tl == nil {
		return nil
	}
	return formatLines(tl, hlHTML)
}
