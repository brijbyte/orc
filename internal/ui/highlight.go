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

// WritePreviewHTML pre-highlights a write call's content into per-line HTML
// with inline styles, so browsers need no highlighter. nil when not a write
// call or the language is unknown.
func WritePreviewHTML(name, argsJSON string) []string {
	if name != "write" {
		return nil
	}
	var a struct{ Path, Content string }
	if json.Unmarshal([]byte(argsJSON), &a) != nil || a.Content == "" {
		return nil
	}
	tl := tokenLines(a.Path, strings.TrimRight(a.Content, "\n"))
	if tl == nil {
		return nil
	}
	return formatLines(tl, html.New(html.PreventSurroundingPre(true)))
}
