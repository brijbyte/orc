package ui

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func editArgs(t *testing.T, path, old, new string) string {
	t.Helper()
	args, err := json.Marshal(map[string]string{"path": path, "old": old, "new": new})
	if err != nil {
		t.Fatal(err)
	}
	return string(args)
}

// A Python edit diff must carry both the diff paint (red/green backgrounds,
// ± markers) and Python token colors from the lexer.
func TestEditPreviewHighlightsPython(t *testing.T) {
	args := editArgs(t, "script.py",
		"def greet(name):\n    return \"hi\"",
		"def greet(name):\n    return f\"hi {name}\"")
	_, full := ToolPreview("edit", args, true, "")
	lines := strings.Split(full, "\n")
	if len(lines) != 4 {
		t.Fatalf("want 4 diff lines, got %d: %q", len(lines), full)
	}
	del, add := lines[0], lines[2]
	if !strings.Contains(del, bgDel) {
		t.Errorf("removed line lacks red diff background: %q", del)
	}
	if !strings.Contains(add, bgAdd) {
		t.Errorf("added line lacks green diff background: %q", add)
	}
	if !strings.Contains(del, "- ") || !strings.Contains(add, "+ ") {
		t.Error("missing ± diff markers")
	}
	// chroma terminal256 emits 256-color foregrounds for Python tokens
	for _, l := range []string{del, add} {
		if !strings.Contains(stripDiffPaint(l), "\x1b[38;5;") {
			t.Errorf("code not syntax highlighted: %q", l)
		}
	}
}

// The browser preview of an edit is a ± diff too: gutter, marker, and
// chroma token spans on every line.
func TestDiffHTMLHighlightsSourceWithoutDiffPaint(t *testing.T) {
	patch := "diff --git a/app.ts b/app.ts\n--- a/app.ts\n+++ b/app.ts\n@@ -1 +1 @@\n-const oldValue = 1;\n+const newValue = 2;\n"
	lines := DiffHTML("app.ts", patch)
	if len(lines) != 6 {
		t.Fatalf("want 6 lines, got %d: %q", len(lines), lines)
	}
	for _, i := range []int{4, 5} {
		if !strings.Contains(lines[i], `<span class="`) {
			t.Errorf("line %d not syntax highlighted: %q", i, lines[i])
		}
		if strings.Contains(lines[i], `class="gd"`) || strings.Contains(lines[i], `class="gi"`) {
			t.Errorf("line %d has diff token paint: %q", i, lines[i])
		}
	}
	if !strings.HasPrefix(lines[4], "-") || !strings.HasPrefix(lines[5], "+") {
		t.Fatalf("missing diff markers: %q", lines[4:])
	}
}

func TestEditPreviewHTML(t *testing.T) {
	args := editArgs(t, "script.py",
		"def greet(name):\n    return \"hi\"",
		"def greet(name):\n    return f\"hi {name}\"")
	lines := PreviewHTML("edit", args)
	if len(lines) != 4 {
		t.Fatalf("want 4 diff lines, got %d: %q", len(lines), lines)
	}
	for i, want := range []string{" - ", " - ", " + ", " + "} {
		if !strings.Contains(lines[i], want) {
			t.Errorf("line %d lacks %q marker: %q", i, want, lines[i])
		}
		if !strings.Contains(lines[i], `<span class="`) {
			t.Errorf("line %d not syntax highlighted: %q", i, lines[i])
		}
	}
}

// stripDiffPaint removes the diff's own color codes so assertions see only
// what the lexer added.
func stripDiffPaint(s string) string {
	for _, c := range []string{bgDel, bgAdd, fgDel, fgAdd} {
		s = strings.ReplaceAll(s, c, "")
	}
	return s
}

// Long bash commands clamp in the one-liner and expand via the preview;
// short ones get neither.
func TestBashCommandPreview(t *testing.T) {
	long := "echo " + strings.Repeat("x", 120)
	args, _ := json.Marshal(map[string]string{"cmd": long})
	if desc := ToolDesc("bash", string(args)); !strings.HasSuffix(desc, "…") {
		t.Errorf("clamped desc must end with ellipsis: %q", desc)
	}
	_, full := ToolPreview("bash", string(args), false, "")
	if !strings.Contains(full, "echo") || !strings.Contains(full, "   1 ") {
		t.Errorf("expected numbered full-command preview: %q", full)
	}
	short, _ := json.Marshal(map[string]string{"cmd": "ls -la"})
	if s, _ := ToolPreview("bash", string(short), false, ""); s != "" {
		t.Errorf("short command must have no preview: %q", s)
	}
}

// The truncation marker is metadata: no diff backgrounds, no token colors.
func TestPreviewMarkerNotHighlighted(t *testing.T) {
	var old strings.Builder
	for i := 0; i < previewMax+5; i++ {
		fmt.Fprintf(&old, "x%d = %d\n", i, i)
	}
	args := editArgs(t, "script.py", old.String(), "y = 1")
	short, _ := ToolPreview("edit", args, true, "")
	lines := strings.Split(short, "\n")
	marker := lines[len(lines)-1]
	if !strings.Contains(marker, "more lines") {
		t.Fatalf("expected truncation marker, got %q", marker)
	}
	if strings.Contains(marker, "\x1b[38;5;") ||
		strings.Contains(marker, bgDel) || strings.Contains(marker, bgAdd) {
		t.Errorf("marker must not be highlighted: %q", marker)
	}
}
