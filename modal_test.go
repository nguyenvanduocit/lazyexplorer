package main

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// modalBoxStyle wraps body content in a rounded border + 1-col horizontal
// padding, so its frame is 2 (border) + 2 (padding) = 4 cols and 2 (border)
// rows. modalSize subtracts exactly this, so the values are load-bearing.
func TestModalBoxStyleFrame(t *testing.T) {
	if got := modalBoxStyle.GetHorizontalFrameSize(); got != 4 {
		t.Errorf("modalBoxStyle horizontal frame = %d, want 4 (border 2 + padding 2)", got)
	}
	if got := modalBoxStyle.GetVerticalFrameSize(); got != 2 {
		t.Errorf("modalBoxStyle vertical frame = %d, want 2 (border only)", got)
	}
	// Rendering through it must carry the rounded-corner glyphs.
	out := modalBoxStyle.Render("x")
	if !strings.Contains(out, "╭") || !strings.Contains(out, "╯") {
		t.Errorf("modalBoxStyle.Render missing rounded border: %q", out)
	}
	_ = lipgloss.Width // keep import if unused after edits
}

func TestModalSizeClamps(t *testing.T) {
	fw := modalBoxStyle.GetHorizontalFrameSize() // 4
	fh := modalBoxStyle.GetVerticalFrameSize()   // 2

	cases := []struct {
		name             string
		w, h             int
		wantInW, wantInH int
	}{
		// Wide terminal: target wins. outerW=56, outerH=16.
		{"wide", 120, 40, modalTargetCols - fw, modalTargetRows - fh},
		// Narrow (vertical-mode) terminal: m.width-margin*2 = 60-4 = 56 = target,
		// so outerW=56, inner=52; height still reaches the row target.
		{"narrow60", 60, 24, 56 - fw, modalTargetRows - fh},
		// Very narrow + short: floor then never-exceed-screen.
		//   outerW = min(56,16)=16 → min(max(16,24),20)=min(24,20)=20 → inner 16.
		//   outerH = min(16,5)=5  → min(max(5,6),9)=min(6,9)=6     → inner 4.
		// Invariant: inner+frame ≤ screen (16+4≤20, 4+2≤9).
		{"tiny", 20, 10, 20 - fw, 6 - fh},
	}
	for _, c := range cases {
		m := model{width: c.w, height: c.h}
		gotW, gotH := m.modalSize()
		if gotW != c.wantInW {
			t.Errorf("%s: innerW = %d, want %d", c.name, gotW, c.wantInW)
		}
		if gotH != c.wantInH {
			t.Errorf("%s: innerH = %d, want %d", c.name, gotH, c.wantInH)
		}
		// The load-bearing invariant: the OUTER box never overflows the screen.
		if gotW+fw > c.w {
			t.Errorf("%s: outerW %d exceeds width %d", c.name, gotW+fw, c.w)
		}
		if gotH+fh > c.h-1 {
			t.Errorf("%s: outerH %d exceeds body rows %d", c.name, gotH+fh, c.h-1)
		}
	}
}

func TestOverlayCenteredComposites(t *testing.T) {
	w, h := 40, 12
	// Background: h rows of styled (ANSI) content, padded to full width.
	rowStyle := lipgloss.NewStyle().Background(lipgloss.Color("#333333")).Foreground(colFg)
	var rows []string
	for i := 0; i < h; i++ {
		rows = append(rows, rowStyle.Width(w).Render("bgrow"))
	}
	bg := strings.Join(rows, "\n")
	box := modalBoxStyle.Width(20).Render("hello\nworld")

	out := overlayCentered(bg, box, w, h)
	lines := strings.Split(out, "\n")

	if len(lines) != h {
		t.Fatalf("rows = %d, want %d", len(lines), h)
	}
	for i, ln := range lines {
		if lipgloss.Width(ln) != w {
			t.Errorf("row %d width = %d, want %d", i, lipgloss.Width(ln), w)
		}
	}
	// Top row (above the centered box) keeps background content.
	if !strings.Contains(stripANSI(lines[0]), "bgrow") {
		t.Errorf("row 0 lost background: %q", stripANSI(lines[0]))
	}
	// Some middle row carries the rounded border (the box landed on screen).
	joined := stripANSI(out)
	if !strings.Contains(joined, "╭") || !strings.Contains(joined, "╯") {
		t.Errorf("composited output missing box border")
	}
}

func TestPaletteBodyPromptAtTop(t *testing.T) {
	m := model{
		mode:            modeCommandPalette,
		paletteStage:    0,
		paletteQuery:    "re",
		paletteFiltered: defaultCommands(),
	}
	body := m.renderPaletteBody(50, 10)
	first := stripANSI(strings.Split(body, "\n")[0])
	if !strings.HasPrefix(strings.TrimSpace(first), "> re") {
		t.Errorf("row 0 = %q, want search prompt '> re…'", first)
	}
}

func TestPaletteBodyCdErrorInBox(t *testing.T) {
	cmds := defaultCommands()
	m := model{
		mode:            modeCommandPalette,
		paletteStage:    1, // cd arg input
		paletteFiltered: cmds,
		paletteCursor:   indexOfCmd(cmds, "cd"),
		statusMsg:       "⚠ blocked: outside root",
	}
	body := stripANSI(m.renderPaletteBody(50, 10))
	if !strings.Contains(body, "⚠ blocked: outside root") {
		t.Errorf("cd-stage body missing error message:\n%s", body)
	}
}

// indexOfCmd returns the position of the named command in cmds (test helper).
func indexOfCmd(cmds []Command, name string) int {
	for i, c := range cmds {
		if c.Name == name {
			return i
		}
	}
	return 0
}

// stripANSI removes SGR escapes for plain-text assertions.
func stripANSI(s string) string {
	var b strings.Builder
	esc := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			esc = true
		case esc && r == 'm':
			esc = false
		case !esc:
			b.WriteRune(r)
		}
	}
	return b.String()
}
