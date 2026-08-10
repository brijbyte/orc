package ui

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
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

// highlightTermLines renders content as ANSI-highlighted lines; nil when
// the language is unknown.
func highlightTermLines(path, content string) []string {
	tl := tokenLines(path, content)
	if tl == nil {
		return nil
	}
	formatter := formatters.Get("terminal256")
	out := make([]string, 0, len(tl))
	for _, line := range tl {
		var sb strings.Builder
		if formatter.Format(&sb, hlStyle, chroma.Literator(line...)) != nil {
			return nil
		}
		out = append(out, strings.ReplaceAll(sb.String(), "\n", ""))
	}
	return out
}
