package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRoutineToolsAreConditionalAndClampSleep(t *testing.T) {
	var normal, routine []struct{ Name string }
	if err := json.Unmarshal(Schema(false), &normal); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(Schema(true), &routine); err != nil {
		t.Fatal(err)
	}
	if len(routine) != len(normal)+2 || routine[len(normal)].Name != "sleep" || routine[len(normal)+1].Name != "stop" {
		t.Fatalf("routine schema names = %#v", routine)
	}

	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	var gotWake time.Time
	var gotReason string
	cb := &RoutineCallbacks{
		now: func() time.Time { return now },
		Sleep: func(wake time.Time, reason string) {
			gotWake, gotReason = wake, reason
		},
	}
	out, end := RunWithRoutine(context.Background(), "", "sleep", `{"seconds":1,"reason":"check again"}`, cb)
	if !end || gotWake != now.Add(time.Minute) || gotReason != "check again" ||
		out != "sleeping until 2026-01-02T03:05:05Z — check again" {
		t.Fatalf("sleep = %q, %v, %v, %q", out, end, gotWake, gotReason)
	}
	if out := Run(context.Background(), "", "sleep", `{}`); !strings.Contains(out, "unknown tool") {
		t.Fatalf("normal sleep = %q", out)
	}
}

func TestRoutineStopEndsTurn(t *testing.T) {
	var reason string
	out, end := RunWithRoutine(context.Background(), "", "stop", `{"reason":"done"}`,
		&RoutineCallbacks{Stop: func(got string) { reason = got }})
	if !end || reason != "done" || out != "routine stopped — done" {
		t.Fatalf("stop = %q, %v, %q", out, end, reason)
	}
}

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
