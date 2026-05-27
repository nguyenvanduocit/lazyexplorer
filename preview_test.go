package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// TestPreviewResizeNoOverflow guards the height/scroll bug: tab-indented source
// (Go especially) used to defeat width accounting because lipgloss.Width counts
// a tab as 0 columns while a terminal expands it to the next tab stop. The
// under-measured line escaped truncation, the terminal hard-wrapped the
// overflow onto an extra row, the preview panel outgrew its declared height, and
// the frame spilled past the screen so it scrolled. The fix expands tabs at load
// (previewFile), so this asserts both the contract (no raw tabs/CR reach the
// renderer) and the invariant (the rendered frame fits within the terminal).
func TestPreviewResizeNoOverflow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deep.go")
	// Deeply tab-indented long lines — the exact shape that wrapped before.
	var sb strings.Builder
	for range 50 {
		sb.WriteString("\t\t\tstep(tea.MouseMsg{X: 37, Y: 5, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})\r\n")
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	lines, _ := previewFile(path, fi.Size())

	// Contract: previewFile output is display-ready — no tabs, no carriage
	// returns. This is the assertion that fails before the fix.
	for i, ln := range lines {
		if strings.ContainsAny(ln, "\t\r") {
			t.Fatalf("preview line %d still contains a tab/CR: %q", i, ln)
		}
	}

	// Invariant: render at a width/height that previously overflowed and assert
	// the frame fits the screen — no row past the terminal width (would wrap),
	// no more rows than the terminal is tall (would scroll).
	m := model{
		width: 50, height: 14, leftRatio: 0.38,
		root: dir, cwd: dir,
		entries: []entry{{name: "deep.go", size: fi.Size()}},
		cursor:  0,
		preview: lines,
		tel:     noopRecorder{},
	}
	frame := m.View().Content
	rows := strings.Split(frame, "\n")
	if len(rows) > m.height {
		t.Errorf("frame has %d rows, exceeds terminal height %d", len(rows), m.height)
	}
	for i, r := range rows {
		if w := lipgloss.Width(r); w > m.width {
			t.Errorf("row %d width %d exceeds terminal width %d: %q", i, w, m.width, r)
		}
	}
}
