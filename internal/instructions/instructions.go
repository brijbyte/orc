// Package instructions builds the agent instructions on the first turn.
package instructions

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/brijbyte/orc/internal/config"
)

const agentsMax = 32768

func appendAgents(sb *strings.Builder, path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	fmt.Fprintf(sb, "\n\n# User instructions (%s)\n", path)
	if len(data) > agentsMax {
		sb.Write(data[:agentsMax])
		sb.WriteString("\n[truncated]")
	} else {
		sb.Write(data)
	}
}

func Build(cwd string, routine ...string) string {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	var sb strings.Builder
	fmt.Fprintf(&sb,
		"You are orc, a terse coding agent running in a terminal at %s on %s %s. "+
			"Use the tools to complete the user's task. Prefer acting over asking. "+
			"Read files before editing them. "+
			"Keep answers short; no preamble, no summaries of what you did unless asked.",
		cwd, sysname(), runtime.GOARCH)
	appendAgents(&sb, config.ExpandHome("~/.agents/AGENTS.md"))
	appendAgents(&sb, filepath.Join(cwd, "AGENTS.md"))
	if len(routine) > 0 && routine[0] != "" {
		fmt.Fprintf(&sb, "\n\n# Routine\nMission: %s\nEnd every turn with the sleep or stop tool. "+
			"A wake message has this form: ⏰ wake: <reason>.", routine[0])
	}
	return sb.String()
}

func sysname() string {
	switch runtime.GOOS {
	case "darwin":
		return "Darwin"
	case "linux":
		return "Linux"
	}
	return runtime.GOOS
}
