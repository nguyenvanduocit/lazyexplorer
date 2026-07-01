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

// TestPaletteBodyTitleAndInput pins the crush-style header: row 0 is the
// "Commands" title followed by a ╱ rule, row 1 is the plain "› query" input.
// The old full-width accent prompt bar is gone — the input row carries no
// accent *background* (the selected command row still does; that's the cursor
// bar). #7D56F4 renders as truecolor bg "48;2;125;86;244" (lipgloss v2).
func TestPaletteBodyTitleAndInput(t *testing.T) {
	m := model{
		mode:            modeCommandPalette,
		paletteStage:    0,
		paletteQuery:    "re",
		paletteFiltered: defaultCommands(),
	}
	body := m.renderPaletteBody(50, 10)
	rows := strings.Split(body, "\n")
	title := stripANSI(rows[0])
	if !strings.Contains(title, "Commands") || !strings.Contains(title, "╱") {
		t.Errorf("row 0 = %q, want 'Commands' title + ╱ rule", title)
	}
	input := stripANSI(rows[1])
	if !strings.Contains(input, "› re") {
		t.Errorf("row 1 = %q, want input '› re…'", input)
	}
	if strings.Contains(rows[1], "48;2;125;86;244") {
		t.Errorf("input row still paints the accent prompt bar:\n%q", rows[1])
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

func TestModalNoOverflow(t *testing.T) {
	for _, sz := range []struct{ w, h int }{{80, 24}, {60, 24}} {
		m := model{
			mode: modeCommandPalette, width: sz.w, height: sz.h,
			paletteFiltered: defaultCommands(), keymap: defaultKeyMap(),
		}
		m.entries = []entry{{name: "x.go"}}
		out := m.View().Content
		for i, ln := range strings.Split(out, "\n") {
			if lipgloss.Width(ln) > sz.w {
				t.Errorf("%dx%d row %d width %d > %d", sz.w, sz.h, i, lipgloss.Width(ln), sz.w)
			}
		}
	}
}

// TestPaletteModalNoWrap guards the width contract between modalSize (returns
// the inner text width) and renderModal. lipgloss v2 .Width is the TOTAL outer
// width — frame included — so renderModal must pass outer (inner+frame), else
// the text area is short by the frame and the longest command row ("copy absolute path")
// wraps, inflating the box by a row. At a width where every row fits, the box
// height is exactly border(2) + header(3: title/input/blank) + one row/command.
func TestPaletteModalNoWrap(t *testing.T) {
	m := model{
		mode: modeCommandPalette, width: 90, height: 28,
		paletteFiltered: defaultCommands(), keymap: defaultKeyMap(),
	}
	box, ok := m.renderModal()
	if !ok {
		t.Fatal("palette mode should produce a modal")
	}
	want := 2 + 3 + len(defaultCommands())
	if got := lipgloss.Height(box); got != want {
		t.Errorf("modal box height = %d, want %d (a wrapped command row inflates it):\n%s", got, want, box)
	}
}

func TestStatusBarModalHints(t *testing.T) {
	pal := stripANSI((model{mode: modeCommandPalette, width: 100, height: 30}).renderStatus())
	if !strings.Contains(pal, "enter") || !strings.Contains(pal, "esc") {
		t.Errorf("palette status missing run/close hints: %q", pal)
	}
	// The query/prompt must NOT be in the status bar anymore (it lives in the box).
	if strings.Contains(pal, "▏") {
		t.Errorf("palette status still shows the input caret: %q", pal)
	}
	help := stripANSI((model{mode: modeHelp, width: 100, height: 30}).renderStatus())
	if !strings.Contains(help, "scroll") || !strings.Contains(help, "esc") {
		t.Errorf("help status missing scroll/close hints: %q", help)
	}
}

func TestModalRendersPaletteInView(t *testing.T) {
	m := model{
		mode: modeCommandPalette, width: 100, height: 30,
		paletteFiltered: defaultCommands(), keymap: defaultKeyMap(),
	}
	// Give it some entries so the background (list pane) has content.
	m.entries = []entry{{name: "alpha.go"}, {name: "beta.go"}}
	out := m.View().Content
	plain := stripANSI(out)
	// The modal border is present (palette is a floating box, not a pane).
	if !strings.Contains(plain, "╭") {
		t.Errorf("View() did not composite the palette modal border")
	}
	// The background list is still visible behind/around the box.
	if !strings.Contains(plain, "alpha.go") {
		t.Errorf("background list pane not visible behind the modal")
	}
}

func TestRenderModal(t *testing.T) {
	// Normal mode → no modal.
	if _, ok := (model{mode: modeNormal, width: 100, height: 30}).renderModal(); ok {
		t.Errorf("normal mode should not produce a modal")
	}
	// Palette mode → bordered box.
	m := model{
		mode: modeCommandPalette, width: 100, height: 30,
		paletteFiltered: defaultCommands(),
	}
	box, ok := m.renderModal()
	if !ok {
		t.Fatal("palette mode should produce a modal")
	}
	if !strings.Contains(stripANSI(box), "╭") {
		t.Errorf("modal missing border: %q", stripANSI(box))
	}
	// Box must fit the screen width.
	if lipgloss.Width(box) > m.width {
		t.Errorf("box width %d exceeds screen %d", lipgloss.Width(box), m.width)
	}
	// Help mode → modal too.
	if _, ok := (model{mode: modeHelp, width: 100, height: 30, keymap: defaultKeyMap()}).renderModal(); !ok {
		t.Errorf("help mode should produce a modal")
	}
}

// TestGradientLine pins the title rule's fade: glyphs are preserved, the first
// is exactly the accent (#7D56F4 → fg 38;2;125;86;244) and the last exactly the
// dim border (#6C757D → fg 38;2;108;117;125); empty input yields empty output.
func TestGradientLine(t *testing.T) {
	out := gradientLine("╱╱╱", colAccent, colDim)
	if stripANSI(out) != "╱╱╱" {
		t.Errorf("gradientLine altered glyphs: %q", stripANSI(out))
	}
	if !strings.Contains(out, "38;2;125;86;244") {
		t.Errorf("first glyph not accent: %q", out)
	}
	if !strings.Contains(out, "38;2;108;117;125") {
		t.Errorf("last glyph not dim: %q", out)
	}
	if gradientLine("", colAccent, colDim) != "" {
		t.Error("gradientLine(\"\") should be empty")
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
