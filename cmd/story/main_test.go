package main

import (
	"testing"
	"time"
)

func TestFormatTerminalOutputPrefixesNonEmptyLines(t *testing.T) {
	ts := time.Date(2026, time.August, 6, 9, 8, 7, 0, time.Local)
	got := formatTerminalOutput("Compiling manuscript\nScenes: starting", ts)
	want := "[2026-08-06 09:08:07] Compiling manuscript\n[2026-08-06 09:08:07] Scenes: starting"

	if got != want {
		t.Fatalf("formatTerminalOutput() = %q, want %q", got, want)
	}
}

func TestFormatTerminalOutputPreservesBlankLines(t *testing.T) {
	ts := time.Date(2026, time.August, 6, 9, 8, 7, 0, time.Local)
	got := formatTerminalOutput("Answer\n\nEvidence:", ts)
	want := "[2026-08-06 09:08:07] Answer\n\n[2026-08-06 09:08:07] Evidence:"

	if got != want {
		t.Fatalf("formatTerminalOutput() = %q, want %q", got, want)
	}
}

func TestFormatTerminalOutputLeavesEmptyOutputEmpty(t *testing.T) {
	ts := time.Date(2026, time.August, 6, 9, 8, 7, 0, time.Local)
	got := formatTerminalOutput("", ts)

	if got != "" {
		t.Fatalf("formatTerminalOutput() = %q, want empty", got)
	}
}
