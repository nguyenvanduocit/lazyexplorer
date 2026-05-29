package main

// Tests for pane-focus state (docs/prd-pane-focus.md). Focus is a sub-state of
// modeNormal that decides which pane the "scroll-ish" keys (up/down/j/k/g/G/
// ctrl+d/u) act on, and which pane a left-click commits to. The focus signal is
// carried by the divider glow (accent half/eighth-block toward the focused pane,
// docs/prd-focus-divider-glow.md) and a dimmed cursor row when the preview is
// focused — panelBorder no longer exists (the borderless middle-divider layout
// shipped first), so the divider-glow + cursor-dim path is the one in effect.
//
// Key construction idioms (locked by a throwaway probe against bubbletea v2.0.6):
//   tab     → tea.KeyPressMsg{Code: tea.KeyTab}
//   esc     → tea.KeyPressMsg{Code: tea.KeyEsc}
//   ctrl+d  → tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}
//   ctrl+u  → tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}
//   j/g/G/J → tea.KeyPressMsg{Code: <r>, Text: "<r>"}
//   up/down → tea.KeyPressMsg{Code: tea.KeyUp / tea.KeyDown}

import (
	"image/color"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// keyT builds a printable KeyPressMsg whose String() is the single rune.
func keyT(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Text: string(r)} }

// pressNormal runs one key through updateNormal and returns the new model.
func pressNormal(t *testing.T, m model, msg tea.KeyPressMsg) model {
	t.Helper()
	nm, _ := m.updateNormal(msg)
	return nm.(model)
}

// longPreviewModel builds a model whose cursor sits on a plain-text file with
// far more lines than the preview body, so previewTop has somewhere to travel.
func longPreviewModel(t *testing.T, width, height int) model {
	t.Helper()
	dir := t.TempDir()
	mustWrite(t, dir, "long.txt", strings.Repeat("line\n", 200))
	m := modelAt(t, dir, width, height)
	if m.entries[m.cursor].name != "long.txt" {
		t.Fatalf("setup: cursor on %q, want long.txt", m.entries[m.cursor].name)
	}
	return m
}

// TestFocusDefaultIsList — zero value of focusPane is focusList (D2/FR1); a
// freshly-built model starts focused on the list.
func TestFocusDefaultIsList(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "a.txt", "x")
	m := newModel(dir, noopRecorder{})
	if m.focusPane != focusList {
		t.Errorf("default focusPane = %v, want focusList (zero value)", m.focusPane)
	}
}

// TestTabTogglesFocus — Tab flips focus list↔preview (FR2/D3), no Shift+Tab.
func TestTabTogglesFocus(t *testing.T) {
	m := longPreviewModel(t, 100, 30)
	if m.focusPane != focusList {
		t.Fatalf("setup: focus = %v, want focusList", m.focusPane)
	}
	tab := tea.KeyPressMsg{Code: tea.KeyTab}

	m = pressNormal(t, m, tab)
	if m.focusPane != focusPreview {
		t.Errorf("after first Tab: focus = %v, want focusPreview", m.focusPane)
	}
	m = pressNormal(t, m, tab)
	if m.focusPane != focusList {
		t.Errorf("after second Tab: focus = %v, want focusList", m.focusPane)
	}
}

// TestShiftTabIgnored — Shift+Tab has no handler, so focus does not change.
func TestShiftTabIgnored(t *testing.T) {
	m := longPreviewModel(t, 100, 30)
	before := m.focusPane
	m = pressNormal(t, m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.focusPane != before {
		t.Errorf("Shift+Tab changed focus %v → %v; want unchanged", before, m.focusPane)
	}
}

// TestArrowsRouteByFocus — when focusList, down/up move the cursor; when
// focusPreview, they scroll the preview (FR3).
func TestArrowsRouteByFocus(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, "a.txt", "x")
	mustWrite(t, root, "b.txt", "y")
	mustWrite(t, root, "c.txt", "z")
	m := modelAt(t, root, 100, 30)
	down := tea.KeyPressMsg{Code: tea.KeyDown}
	up := tea.KeyPressMsg{Code: tea.KeyUp}

	// focusList: down moves cursor, preview stays at 0.
	m = pressNormal(t, m, down)
	if m.cursor != 1 {
		t.Errorf("focusList down: cursor = %d, want 1", m.cursor)
	}
	m = pressNormal(t, m, up)
	if m.cursor != 0 {
		t.Errorf("focusList up: cursor = %d, want 0", m.cursor)
	}

	// Switch to a long file and focus the preview.
	lm := longPreviewModel(t, 100, 30)
	lm.focusPane = focusPreview
	curBefore := lm.cursor
	lm = pressNormal(t, lm, down)
	if lm.previewTop != 1 {
		t.Errorf("focusPreview down: previewTop = %d, want 1", lm.previewTop)
	}
	if lm.cursor != curBefore {
		t.Errorf("focusPreview down moved the cursor %d → %d; cursor must not move", curBefore, lm.cursor)
	}
	for i := 0; i < 4; i++ {
		lm = pressNormal(t, lm, down)
	}
	if lm.previewTop != 5 {
		t.Errorf("focusPreview after 5 downs: previewTop = %d, want 5", lm.previewTop)
	}
	lm = pressNormal(t, lm, up)
	if lm.previewTop != 4 {
		t.Errorf("focusPreview up: previewTop = %d, want 4", lm.previewTop)
	}

	// j/k route the same way as down/up.
	lm = pressNormal(t, lm, keyT('j'))
	if lm.previewTop != 5 {
		t.Errorf("focusPreview 'j': previewTop = %d, want 5", lm.previewTop)
	}
	lm = pressNormal(t, lm, keyT('k'))
	if lm.previewTop != 4 {
		t.Errorf("focusPreview 'k': previewTop = %d, want 4", lm.previewTop)
	}
}

// TestGGRouteByFocus — g/G jump to top/bottom of the focused pane (FR3/D12).
func TestGGRouteByFocus(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 6; i++ {
		mustWrite(t, root, "f"+string(rune('a'+i))+".txt", "x")
	}
	m := modelAt(t, root, 100, 30)

	// focusList: G → last index, g → 0.
	m = pressNormal(t, m, keyT('G'))
	if m.cursor != len(m.entries)-1 {
		t.Errorf("focusList G: cursor = %d, want %d", m.cursor, len(m.entries)-1)
	}
	m = pressNormal(t, m, keyT('g'))
	if m.cursor != 0 {
		t.Errorf("focusList g: cursor = %d, want 0", m.cursor)
	}

	// focusPreview: G → maxTop, g → 0.
	lm := longPreviewModel(t, 100, 30)
	lm.focusPane = focusPreview
	lm = pressNormal(t, lm, keyT('G'))
	_, bodyH := lm.previewScroll()
	wantMax := max(0, lm.previewLen()-bodyH)
	if lm.previewTop != wantMax {
		t.Errorf("focusPreview G: previewTop = %d, want maxTop %d", lm.previewTop, wantMax)
	}
	if wantMax == 0 {
		t.Fatalf("setup: preview not tall enough to scroll (maxTop=0)")
	}
	lm = pressNormal(t, lm, keyT('g'))
	if lm.previewTop != 0 {
		t.Errorf("focusPreview g: previewTop = %d, want 0", lm.previewTop)
	}
}

// TestCtrlDURouteByFocus — ctrl+d/u half-page the focused pane (FR3/D11). In
// preview mode they move previewTop by max(1, bodyH/2); in list mode they move
// the cursor by the same step.
func TestCtrlDURouteByFocus(t *testing.T) {
	ctrlD := tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}
	ctrlU := tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}

	// Preview side.
	lm := longPreviewModel(t, 100, 30)
	lm.focusPane = focusPreview
	_, bodyH := lm.previewScroll()
	step := max(1, bodyH/2)
	lm = pressNormal(t, lm, ctrlD)
	if lm.previewTop != step {
		t.Errorf("focusPreview ctrl+d: previewTop = %d, want %d (bodyH/2)", lm.previewTop, step)
	}
	lm = pressNormal(t, lm, ctrlU)
	if lm.previewTop != 0 {
		t.Errorf("focusPreview ctrl+u: previewTop = %d, want 0", lm.previewTop)
	}

	// List side: ctrl+d jumps the cursor by the same step (not the preview).
	root := t.TempDir()
	for i := 0; i < 60; i++ {
		mustWrite(t, root, "f"+twoDigits(i)+".txt", "x")
	}
	m := modelAt(t, root, 100, 30)
	_, bodyH = m.previewScroll()
	step = max(1, bodyH/2)
	previewBefore := m.previewTop
	m = pressNormal(t, m, ctrlD)
	if m.cursor != step {
		t.Errorf("focusList ctrl+d: cursor = %d, want %d (bodyH/2 jump)", m.cursor, step)
	}
	if m.previewTop != previewBefore {
		t.Errorf("focusList ctrl+d scrolled the preview %d → %d; it must move the cursor instead",
			previewBefore, m.previewTop)
	}
	m = pressNormal(t, m, ctrlU)
	if m.cursor != 0 {
		t.Errorf("focusList ctrl+u: cursor = %d, want 0", m.cursor)
	}
}

// TestUppercaseJKRemoved — the legacy J/K preview-scroll aliases are gone
// (FR4/D13): pressing J or K must not move previewTop (focus + lowercase j/k is
// the only way to scroll the preview).
func TestUppercaseJKRemoved(t *testing.T) {
	lm := longPreviewModel(t, 100, 30) // focus defaults to list
	for _, r := range []rune{'J', 'K'} {
		before := lm.previewTop
		nm := pressNormal(t, lm, keyT(r))
		if nm.previewTop != before {
			t.Errorf("%q moved previewTop %d → %d; J/K legacy must be removed",
				string(r), before, nm.previewTop)
		}
	}
}

// TestMutationKeysGuardedByFocus — r/d/enter/l/h are no-ops when focusPreview
// (FR5): no mode change, no navigation.
func TestMutationKeysGuardedByFocus(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	mustMkdir(t, root, "sub")
	mustWrite(t, sub, "inner.txt", "x")
	mustWrite(t, root, "file.txt", "hello\n")

	// Put cursor on file.txt and focus the preview.
	base := modelAt(t, root, 100, 30)
	fileIdx := -1
	for i, e := range base.entries {
		if e.name == "file.txt" {
			fileIdx = i
		}
	}
	if fileIdx < 0 {
		t.Fatal("setup: file.txt not found")
	}

	// r → no rename prompt.
	m := base
	m.cursor = fileIdx
	m.refreshPreview()
	m.focusPane = focusPreview
	if got := pressNormal(t, m, keyT('r')); got.mode != modeNormal {
		t.Errorf("r under focusPreview entered mode %v; want modeNormal (noop)", got.mode)
	}
	// d → no delete prompt.
	if got := pressNormal(t, m, keyT('d')); got.mode != modeNormal {
		t.Errorf("d under focusPreview entered mode %v; want modeNormal (noop)", got.mode)
	}
	// enter → no descend (cwd unchanged); use a dir selection to make descend observable.
	dm := modelAt(t, root, 100, 30) // cursor on sub (dir, sorts first)
	if !dm.entries[dm.cursor].isDir {
		t.Fatalf("setup: cursor not on a dir (%q)", dm.entries[dm.cursor].name)
	}
	dm.focusPane = focusPreview
	if got := pressNormal(t, dm, tea.KeyPressMsg{Code: tea.KeyEnter}); got.cwd != root {
		t.Errorf("enter under focusPreview descended to %q; want cwd unchanged (%q)", got.cwd, root)
	}
	if got := pressNormal(t, dm, keyT('l')); got.cwd != root {
		t.Errorf("l under focusPreview descended to %q; want cwd unchanged", got.cwd)
	}

	// h under focusPreview from a subfolder → cwd does not ascend.
	hm := modelAt(t, sub, 100, 30) // cwd = sub; root jail above
	hm.root = root
	hm.focusPane = focusPreview
	if got := pressNormal(t, hm, keyT('h')); got.cwd != sub {
		t.Errorf("h under focusPreview ascended to %q; want cwd unchanged (%q)", got.cwd, sub)
	}
}

// TestEscReturnsFocusToList — Esc while focusPreview returns focus to list
// (FR6); Esc while focusList is a no-op.
func TestEscReturnsFocusToList(t *testing.T) {
	m := longPreviewModel(t, 100, 30)
	m.focusPane = focusPreview
	esc := tea.KeyPressMsg{Code: tea.KeyEsc}

	m = pressNormal(t, m, esc)
	if m.focusPane != focusList {
		t.Errorf("Esc under focusPreview: focus = %v, want focusList", m.focusPane)
	}
	// Esc again (now focusList) is a noop.
	m = pressNormal(t, m, esc)
	if m.focusPane != focusList {
		t.Errorf("Esc under focusList changed focus to %v; want focusList (noop)", m.focusPane)
	}
}

// TestClickSetsFocus — a left-click in the list pane sets focusList; in the
// preview pane sets focusPreview; on the divider leaves focus unchanged (FR8).
func TestClickSetsFocus(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, "a.txt", "x")
	mustWrite(t, root, "b.txt", "y")
	m := modelAt(t, root, 100, 30)
	g := m.layout()

	// Click in the preview pane → focusPreview.
	m.focusPane = focusList
	nm, _ := m.handleMouse(tea.MouseClickMsg{X: g.dividerStart + dividerWidth + 3, Y: 1, Button: tea.MouseLeft})
	if nm.(model).focusPane != focusPreview {
		t.Errorf("click in preview pane: focus = %v, want focusPreview", nm.(model).focusPane)
	}

	// Click in the list pane (at the first body row, below the header) → focusList.
	m.focusPane = focusPreview
	nm, _ = m.handleMouse(tea.MouseClickMsg{X: 2, Y: g.firstRow, Button: tea.MouseLeft})
	if nm.(model).focusPane != focusList {
		t.Errorf("click in list pane: focus = %v, want focusList", nm.(model).focusPane)
	}

	// Click on the divider (drag-start) → focus unchanged.
	m.focusPane = focusList
	nm, _ = m.handleMouse(tea.MouseClickMsg{X: g.dividerStart, Y: 5, Button: tea.MouseLeft})
	if nm.(model).focusPane != focusList {
		t.Errorf("divider drag-start changed focus to %v; want focusList unchanged", nm.(model).focusPane)
	}
	if !nm.(model).dragging {
		t.Errorf("divider drag-start should set dragging=true")
	}
}

// TestWheelDoesNotChangeFocus — wheel scroll never changes focus (FR9): a wheel
// over the preview while focusList scrolls the preview but keeps focus on list.
func TestWheelDoesNotChangeFocus(t *testing.T) {
	lm := longPreviewModel(t, 100, 30) // focus list, cursor on a long file
	g := lm.layout()
	lm.focusPane = focusList
	x := g.dividerStart + dividerWidth + 3
	nm, _ := lm.handleMouse(tea.MouseWheelMsg{X: x, Y: 1, Button: tea.MouseWheelDown})
	m2 := nm.(model)
	if m2.focusPane != focusList {
		t.Errorf("wheel over preview changed focus to %v; want focusList unchanged", m2.focusPane)
	}
	if m2.previewTop == 0 {
		t.Errorf("wheel over preview should still scroll it; previewTop stayed 0")
	}
}

// TestRenameModeFreezesFocus — entering rename, then pressing Tab, must not
// change focus (Tab is consumed by the rename prompt; FR13/D15).
func TestRenameModeFreezesFocus(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, "file.txt", "x")
	m := modelAt(t, root, 100, 30)
	if m.focusPane != focusList {
		t.Fatalf("setup: focus = %v, want focusList", m.focusPane)
	}

	// r enters rename.
	m = pressNormal(t, m, keyT('r'))
	if m.mode != modeRename {
		t.Fatalf("after r: mode = %v, want modeRename", m.mode)
	}
	inputBefore := m.input
	focusBefore := m.focusPane

	// Tab goes through updateRename (the dispatcher routes by mode).
	nm, _ := m.updateRename(tea.KeyPressMsg{Code: tea.KeyTab})
	m2 := nm.(model)
	if m2.focusPane != focusBefore {
		t.Errorf("Tab in rename changed focus %v → %v; focus must freeze in prompt mode",
			focusBefore, m2.focusPane)
	}
	if m2.input != inputBefore {
		t.Errorf("Tab in rename appended to input %q → %q; Tab has empty Text → noop",
			inputBefore, m2.input)
	}
}

// TestCursorRowDimWhenFocusPreview — renderEntryRow dims the cursor row when
// the list is NOT focused (D10/FR12): active+!listFocused → colDim background;
// active+listFocused → colAccent background. We assert on the SGR background
// parameter (48;2;R;G;B) rather than a pre-rendered escape, because lipgloss
// packs bold+fg+bg into one SGR run — so the bg color appears verbatim inside
// that run but a standalone "bg only" escape never matches as a substring.
func TestCursorRowDimWhenFocusPreview(t *testing.T) {
	e := entry{name: "main.go", isDir: false, size: 100}
	accentBg := bgParam(t, colAccent)
	dimBg := bgParam(t, colDim)

	focused := renderEntryRow(e, nil, 30, true, true)
	if !strings.Contains(focused, accentBg) {
		t.Errorf("active+listFocused row should carry the accent bg %q; got %q", accentBg, focused)
	}
	if strings.Contains(focused, dimBg) {
		t.Errorf("active+listFocused row must not carry the dim bg %q; got %q", dimBg, focused)
	}

	dimmed := renderEntryRow(e, nil, 30, true, false)
	if !strings.Contains(dimmed, dimBg) {
		t.Errorf("active+!listFocused row should carry the dim bg %q; got %q", dimBg, dimmed)
	}
	// And it must NOT carry the accent background in the dimmed state.
	if strings.Contains(dimmed, accentBg) {
		t.Errorf("dimmed cursor row must not also carry the accent bg %q; got %q", accentBg, dimmed)
	}
	// Both still render at full width (the highlight covers the whole pane).
	if w := lipgloss.Width(dimmed); w != 30 {
		t.Errorf("dimmed cursor row width = %d, want 30", w)
	}
}

// TestStatusHintsReflectFocusNoChip — the status bar carries focus-specific hints
// (sourced from the keymap via shortHelp) but NO focus chip: focus is signalled by
// the divider glow + dimmed cursor row (prd-focus-divider-glow). The hint wording
// still distinguishes focus — the navigation verb is "move" on the list vs "scroll"
// on the preview — and the Tab binding reads "switch focus" in both states.
func TestStatusHintsReflectFocusNoChip(t *testing.T) {
	m := longPreviewModel(t, 120, 30)
	// The old chip was the only accent-BACKGROUND element in the footer (the hints
	// are plain fg, the render spinner is accent fg). So "no accent bg in the status
	// bar" == "no chip" — and it never false-matches hint words like "preview top".
	accentBg := bgParam(t, colAccent)

	m.focusPane = focusList
	listRaw := m.renderStatus()
	if strings.Contains(listRaw, accentBg) {
		t.Error("status bar must not carry an accent-bg focus chip (focusList)")
	}
	listStatus := ansi.Strip(listRaw)
	if !strings.Contains(listStatus, "move") {
		t.Errorf("focusList hints should offer a 'move' binding; got %q", listStatus)
	}
	if !strings.Contains(listStatus, "switch focus") {
		t.Errorf("focusList hints should offer '[tab] switch focus'; got %q", listStatus)
	}

	m.focusPane = focusPreview
	prevRaw := m.renderStatus()
	if strings.Contains(prevRaw, accentBg) {
		t.Error("status bar must not carry an accent-bg focus chip (focusPreview)")
	}
	prevStatus := ansi.Strip(prevRaw)
	if !strings.Contains(prevStatus, "scroll") {
		t.Errorf("focusPreview hints should offer a 'scroll' binding; got %q", prevStatus)
	}
	// Back (esc) belongs to the focusPreview shortHelp set. The rendered hint can
	// truncate it at a given width once the hscroll/wrap bindings are added, so
	// assert on the binding set rather than the width-clamped string.
	hasBack := false
	for _, b := range m.shortHelp() {
		if b.Help().Key == "esc" {
			hasBack = true
		}
	}
	if !hasBack {
		t.Error("focusPreview shortHelp should include the esc/back binding")
	}
}

// TestDividerGlowReflectsFocus — the divider glows in the accent toward the focused
// pane (prd-focus-divider-glow), in both orientations: horizontal uses a half-block
// (▐ list / ▌ preview) in the pad col hugging the glyph; vertical uses an
// eighth-block full-width (▔ list / ▁ preview). The glyphs appear nowhere else in
// these plain-file frames, so a substring check on the rendered View is sufficient.
func TestDividerGlowReflectsFocus(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, "a.txt", "x")

	cases := []struct {
		name   string
		w, h   int
		focus  focusPane
		glyph  string
		absent string // the opposite-side glyph must NOT appear
	}{
		{"horizontal list", 120, 30, focusList, "▐", "▌"},
		{"horizontal preview", 120, 30, focusPreview, "▌", "▐"},
		{"vertical list", 60, 30, focusList, "▔", "▁"},
		{"vertical preview", 60, 30, focusPreview, "▁", "▔"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := modelAt(t, root, c.w, c.h)
			m.focusPane = c.focus
			content := m.View().Content
			if !strings.Contains(content, c.glyph) {
				t.Errorf("%s: divider should glow with %q toward the focused pane", c.name, c.glyph)
			}
			if strings.Contains(content, c.absent) {
				t.Errorf("%s: opposite-side glyph %q must not appear", c.name, c.absent)
			}
		})
	}
}

// TestEmptyPanesNoop — j/k are no-ops when the focused collection is empty
// (FR15): empty preview + focusPreview, and empty list + focusList.
func TestEmptyPanesNoop(t *testing.T) {
	// Empty preview: select an empty file (1-line placeholder shorter than body).
	root := t.TempDir()
	mustWrite(t, root, "empty.txt", "")
	m := modelAt(t, root, 100, 30)
	m.focusPane = focusPreview
	for _, msg := range []tea.KeyPressMsg{{Code: tea.KeyDown}, keyT('j'), keyT('G'), keyT('g')} {
		got := pressNormal(t, m, msg)
		if got.previewTop != 0 {
			t.Errorf("focusPreview on short preview: %q moved previewTop to %d; want 0",
				msg.String(), got.previewTop)
		}
	}

	// Empty list: a folder with nothing in it, focusList.
	emptyDir := t.TempDir() // brand new temp dir, no entries, at jail root
	em := modelAt(t, emptyDir, 100, 30)
	if len(em.entries) != 0 {
		t.Fatalf("setup: expected empty list, got %d entries", len(em.entries))
	}
	em.focusPane = focusList
	for _, msg := range []tea.KeyPressMsg{{Code: tea.KeyDown}, keyT('j'), keyT('G')} {
		got := pressNormal(t, em, msg)
		if got.cursor != 0 {
			t.Errorf("focusList on empty list: %q moved cursor to %d; want 0", msg.String(), got.cursor)
		}
	}
}

// twoDigits zero-pads i to two digits so 60 generated filenames sort in
// numeric order (file00 < file01 < ... < file59).
func twoDigits(i int) string {
	if i < 10 {
		return "0" + string(rune('0'+i))
	}
	return string(rune('0'+i/10)) + string(rune('0'+i%10))
}

// bgParam returns the SGR background-color parameter ("48;2;R;G;B") lipgloss
// emits for color c. It renders c as a *background* on a bare style and strips
// the surrounding "\x1b[" + "m" so the result is the substring that appears
// verbatim inside any SGR run that sets this background — even when lipgloss
// packs it together with bold + a foreground. Locking it to the palette means a
// theme tweak recolors the assertion automatically.
func bgParam(t *testing.T, c color.Color) string {
	t.Helper()
	out := lipgloss.NewStyle().Background(c).Render("X")
	x := strings.Index(out, "X")
	if x <= 0 || !strings.HasPrefix(out, "\x1b[") {
		t.Fatalf("Background(%v).Render produced no leading SGR escape: %q", c, out)
	}
	return strings.TrimSuffix(out[len("\x1b["):x], "m")
}
