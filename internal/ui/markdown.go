package ui

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
)

// trailingPad matches glamour's end-of-line padding: styled spaces.
var trailingPad = regexp.MustCompile(`(?:\x1b\[[0-9;]*m| )+$`)

// mdStream renders streamed markdown block-by-block: lines buffer until a
// blank line outside a code fence, then the block renders through glamour.
type mdStream struct {
	println func(string) // one terminal line, already styled
	width   func() int
	partial strings.Builder
	block   []string
	inFence bool
	lead    string // printed before the first rendered line
	started bool
}

func newMDStream(println func(string), width func() int, lead string) *mdStream {
	return &mdStream{println: println, width: width, lead: lead}
}

func mdRenderer(width int) *glamour.TermRenderer {
	style := styles.LightStyleConfig
	if lipgloss.HasDarkBackground() {
		style = styles.DarkStyleConfig
	}
	var zero uint
	style.Document.Margin = &zero
	style.Document.BlockPrefix = ""
	style.Document.BlockSuffix = ""
	if width < 20 {
		width = 80
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(style),
		glamour.WithWordWrap(width-2),
		glamour.WithEmoji(),
	)
	if err != nil {
		return nil
	}
	return r
}

func isFence(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~")
}

func (m *mdStream) feed(text string) {
	for {
		nl := strings.IndexByte(text, '\n')
		if nl < 0 {
			m.partial.WriteString(text)
			return
		}
		m.partial.WriteString(text[:nl])
		line := m.partial.String()
		m.partial.Reset()
		text = text[nl+1:]
		if isFence(line) {
			m.inFence = !m.inFence
		}
		m.block = append(m.block, line)
		if strings.TrimSpace(line) == "" && !m.inFence {
			m.flushBlock()
		}
	}
}

func (m *mdStream) flushBlock() {
	src := strings.TrimRight(strings.Join(m.block, "\n"), "\n")
	m.block = nil
	if strings.TrimSpace(src) == "" {
		return
	}
	rendered := src
	if r := mdRenderer(m.width()); r != nil {
		if out, err := r.Render(src); err == nil {
			rendered = strings.TrimRight(out, "\n")
		}
	}
	for _, line := range strings.Split(rendered, "\n") {
		if trimmed := trailingPad.ReplaceAllString(line, ""); trimmed != line {
			line = trimmed + "\x1b[0m"
		}
		if !m.started {
			m.started = true
			m.println(m.lead + line)
		} else {
			m.println(line)
		}
	}
	m.println("")
}

// end flushes any partial line and the open block.
func (m *mdStream) end() {
	if m.partial.Len() > 0 {
		m.block = append(m.block, m.partial.String())
		m.partial.Reset()
	}
	m.flushBlock()
}
