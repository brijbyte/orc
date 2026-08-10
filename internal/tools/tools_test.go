package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadCapsRequestedLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "many.txt")
	if err := os.WriteFile(path, []byte(strings.Repeat("x\n", readLimit+1)), 0o644); err != nil {
		t.Fatal(err)
	}
	got := toolRead("", args{"path": path, "limit": float64(readLimit * 2)})
	if !strings.Contains(got, "[more lines after 1000]") {
		t.Fatalf("missing limit marker: %.100s", got[len(got)-min(len(got), 100):])
	}
}

func TestReadRejectsOversizedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.txt")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(readSourceMax + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	got := toolRead("", args{"path": path})
	if !strings.Contains(got, "file exceeds 8 MB") {
		t.Fatalf("read = %q", got)
	}
}
