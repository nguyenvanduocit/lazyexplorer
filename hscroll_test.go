package main

// Tests for horizontal scroll + wrap toggle in the preview pane
// (prd-horizontal-scroll-preview). The `press` / `typeStr` helpers live in
// palette_test.go (same package).

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// scrollableModel returns a model focused on the preview with a long plain-text
// line so horizontal scroll has something to pan across.
func scrollableModel(t *testing.T, w, h int) model {
	t.Helper()
	m := modelAt(t, t.TempDir(), w, h)
	m.focusPane = focusPreview
	m.previewScrollable = true
	m.previewIsDir = false
	m.preview = []string{strings.Repeat("x", 300), "short", strings.Repeat("y", 300)}
	m.previewWrap = false
	m.reflowPreview(m.previewBodyWidth())
	return m
}

// --- helpers --------------------------------------------------------------

func TestWrapLine(t *testing.T) {
	got := wrapLine(strings.Repeat("a", 25), 10)
	if len(got) != 3 {
		t.Errorf("wrapLine(25 'a', 10) → %d lines, want 3", len(got))
	}
	for i, ln := range got {
		if lineWidth(ln) > 10 {
			t.Errorf("wrapped line %d width %d > 10", i, lineWidth(ln))
		}
	}
	if empty := wrapLine("", 10); len(empty) != 1 || empty[0] != "" {
		t.Errorf("wrapLine empty → %v, want one empty line", empty)
	}
}

func TestHSliceRunes(t *testing.T) {
	// [left=2, width=3) of "abcdefg" → "cde"
	if got := hSlice("abcdefg", 2, 3); got != "cde" {
		t.Errorf("hSlice(abcdefg,2,3) = %q, want \"cde\"", got)
	}
	if got := hSlice("abc", 0, 10); got != "abc" {
		t.Errorf("hSlice(abc,0,10) = %q, want \"abc\"", got)
	}
}

// --- reflow ---------------------------------------------------------------

func TestReflowNowrapIdentity(t *testing.T) {
	m := scrollableModel(t, 100, 30)
	if len(m.previewDisplay) != len(m.preview) {
		t.Errorf("nowrap: previewDisplay len %d != preview len %d (should be identity)",
			len(m.previewDisplay), len(m.preview))
	}
	if m.previewMaxLineWidth != 300 {
		t.Errorf("nowrap: previewMaxLineWidth = %d, want 300 (widest source line)", m.previewMaxLineWidth)
	}
}

func TestReflowWrapExpands(t *testing.T) {
	m := scrollableModel(t, 100, 30)
	m.previewWrap = true
	m.reflowPreview(m.previewBodyWidth())
	if len(m.previewDisplay) <= len(m.preview) {
		t.Errorf("wrap: previewDisplay len %d should exceed source len %d", len(m.previewDisplay), len(m.preview))
	}
	// Source line 1 ("short") begins at some visual index; line 0 (300 x's) wraps
	// into many visual lines before it.
	w := m.previewBodyWidth()
	wantLine0Visual := (300 + w - 1) / w // ceil(300/w)
	if m.visualLineFor(1) != wantLine0Visual {
		t.Errorf("visualLineFor(1) = %d, want %d (after line 0 wraps)", m.visualLineFor(1), wantLine0Visual)
	}
}

// --- horizontal scroll ----------------------------------------------------

func TestHScrollPanClampReset(t *testing.T) {
	m := scrollableModel(t, 100, 30)
	w := m.previewBodyWidth()

	m.scrollPreviewH(previewColStep)
	if m.previewHScroll != 1 {
		t.Errorf("pan right one: hscroll = %d, want 1", m.previewHScroll)
	}
	m.scrollPreviewH(-previewColStep)
	if m.previewHScroll != 0 {
		t.Errorf("pan left back: hscroll = %d, want 0", m.previewHScroll)
	}
	// Clamp at left edge.
	m.scrollPreviewH(-5)
	if m.previewHScroll != 0 {
		t.Errorf("pan left at 0: hscroll = %d, want 0 (clamped)", m.previewHScroll)
	}
	// Clamp at right edge: maxH = maxLineWidth - max(1, w-2).
	m.scrollPreviewH(10000)
	wantMax := max(0, m.previewMaxLineWidth-max(1, w-2))
	if m.previewHScroll != wantMax {
		t.Errorf("pan right past end: hscroll = %d, want clamp %d", m.previewHScroll, wantMax)
	}
}

func TestHScrollNoopWhenContentFits(t *testing.T) {
	m := modelAt(t, t.TempDir(), 100, 30)
	m.focusPane = focusPreview
	m.previewScrollable = true
	m.preview = []string{"short line", "another"}
	m.reflowPreview(m.previewBodyWidth())
	m.scrollPreviewH(5)
	if m.previewHScroll != 0 {
		t.Errorf("content fits → hscroll = %d, want 0 (no-op)", m.previewHScroll)
	}
}

func TestHScrollNoopInWrapMode(t *testing.T) {
	m := scrollableModel(t, 100, 30)
	m.previewWrap = true
	m.reflowPreview(m.previewBodyWidth())
	m.scrollPreviewH(5)
	if m.previewHScroll != 0 {
		t.Errorf("wrap mode → hscroll = %d, want 0 (no-op)", m.previewHScroll)
	}
}

// TestHScrollKeysViaUpdate drives l/h/L/H/0 through Update in focusPreview.
func TestHScrollKeysViaUpdate(t *testing.T) {
	m := scrollableModel(t, 100, 30)
	w := m.previewBodyWidth()

	m, _ = press(t, m, tea.KeyPressMsg{Code: 'l', Text: "l"})
	if m.previewHScroll != 1 {
		t.Errorf("l: hscroll = %d, want 1", m.previewHScroll)
	}
	m, _ = press(t, m, tea.KeyPressMsg{Code: 'h', Text: "h"})
	if m.previewHScroll != 0 {
		t.Errorf("h: hscroll = %d, want 0", m.previewHScroll)
	}
	m, _ = press(t, m, tea.KeyPressMsg{Code: 'L', Text: "L"})
	if m.previewHScroll != max(1, w/2) {
		t.Errorf("L: hscroll = %d, want %d (half width)", m.previewHScroll, max(1, w/2))
	}
	m, _ = press(t, m, tea.KeyPressMsg{Code: 'H', Text: "H"})
	if m.previewHScroll != 0 {
		t.Errorf("H from half: hscroll = %d, want 0", m.previewHScroll)
	}
	m, _ = press(t, m, tea.KeyPressMsg{Code: 'l', Text: "l"})
	m, _ = press(t, m, tea.KeyPressMsg{Code: '0', Text: "0"})
	if m.previewHScroll != 0 {
		t.Errorf("0: hscroll = %d, want 0 (reset)", m.previewHScroll)
	}
}

// TestHScrollEdgeIndicators: a long line shows › at the right when more content
// is hidden, and ‹ at the left once panned.
func TestHScrollEdgeIndicators(t *testing.T) {
	m := scrollableModel(t, 100, 30)
	out := ansi.Strip(m.renderPreview(m.previewBodyWidth()))
	if !strings.Contains(out, "›") {
		t.Errorf("long line at offset 0 should show a right indicator '›'; got:\n%s", out)
	}
	if strings.Contains(out, "‹") {
		t.Errorf("at offset 0 there should be no left indicator '‹'; got:\n%s", out)
	}
	m.scrollPreviewH(10)
	out = ansi.Strip(m.renderPreview(m.previewBodyWidth()))
	if !strings.Contains(out, "‹") {
		t.Errorf("after panning right, a left indicator '‹' should appear; got:\n%s", out)
	}
}

// --- wrap toggle ----------------------------------------------------------

func TestToggleWrapPreservesReadingPosition(t *testing.T) {
	// Tall content (10 long lines) + a short viewport so the wrapped document is
	// taller than the pane — otherwise the scroll clamp legitimately pulls the
	// top to 0 (you cannot hold a mid line at the top when everything fits).
	m := modelAt(t, t.TempDir(), 100, 10) // bodyH ≈ 9
	m.focusPane = focusPreview
	m.previewScrollable = true
	lines := make([]string, 10)
	for i := range lines {
		lines[i] = strings.Repeat("x", 300)
	}
	m.preview = lines
	m.reflowPreview(m.previewBodyWidth())

	m.previewTop = 3 // nowrap: visual index == source index → source line 3 at top
	m.toggleWrap()
	if !m.previewWrap {
		t.Fatalf("toggleWrap did not enable wrap")
	}
	// After wrap, source line 3 should still be the one at the viewport top.
	if got := m.sourceLineAt(m.previewTop); got != 3 {
		t.Errorf("after wrap toggle, source line at top = %d, want 3 (reading position preserved)", got)
	}
}

func TestToggleWrapResetsHScroll(t *testing.T) {
	m := scrollableModel(t, 100, 30)
	m.scrollPreviewH(20)
	m.toggleWrap()
	if m.previewHScroll != 0 {
		t.Errorf("toggleWrap should reset hscroll; got %d", m.previewHScroll)
	}
}

func TestWrapKeyViaUpdate(t *testing.T) {
	m := scrollableModel(t, 100, 30)
	m, _ = press(t, m, tea.KeyPressMsg{Code: 'w', Text: "w"})
	if !m.previewWrap {
		t.Errorf("w: previewWrap = %v, want true", m.previewWrap)
	}
	m, _ = press(t, m, tea.KeyPressMsg{Code: 'w', Text: "w"})
	if m.previewWrap {
		t.Errorf("w again: previewWrap = %v, want false (toggle)", m.previewWrap)
	}
}

// --- gating + reset hygiene ----------------------------------------------

// TestHScrollNotScrollableNoop: markdown/folder/image previews don't pan or wrap.
func TestHScrollNotScrollableNoop(t *testing.T) {
	m := modelAt(t, t.TempDir(), 100, 30)
	m.focusPane = focusPreview
	m.previewScrollable = false // e.g. markdown
	m.preview = []string{strings.Repeat("z", 300)}
	m.scrollPreviewH(10)
	if m.previewHScroll != 0 {
		t.Errorf("non-scrollable: hscroll = %d, want 0", m.previewHScroll)
	}
	m.toggleWrap()
	if m.previewWrap {
		t.Errorf("non-scrollable: toggleWrap should be a no-op; previewWrap = %v", m.previewWrap)
	}
}

// TestHScrollResetOnSelectionChange: navigating to a new entry zeroes hscroll
// but keeps the wrap preference (D14/D15).
func TestHScrollResetOnSelectionChange(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, "a.txt", strings.Repeat("a", 300)+"\n")
	mustWrite(t, root, "b.txt", strings.Repeat("b", 300)+"\n")
	m := modelAt(t, root, 100, 30)
	m.focusPane = focusPreview
	// Land on a.txt and reflow.
	m.reflowPreview(m.previewBodyWidth())
	m.previewWrap = true
	m.previewHScroll = 0 // wrap forces 0 anyway
	m.previewWrap = false
	m.scrollPreviewH(20)
	if m.previewHScroll == 0 {
		t.Fatalf("setup: expected a non-zero hscroll before navigating")
	}
	m.previewWrap = true // set a wrap preference
	m.moveCursor(1)      // navigate to b.txt → refreshPreview
	if m.previewHScroll != 0 {
		t.Errorf("after selection change: hscroll = %d, want 0 (reset)", m.previewHScroll)
	}
	if !m.previewWrap {
		t.Errorf("after selection change: previewWrap = %v, want true (preference persists)", m.previewWrap)
	}
}
