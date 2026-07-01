package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// clickListRow left-clicks the list-pane row currently showing entry index idx
// (assumes it is visible, i.e. listTop <= idx < listTop+rows). X=2 lands inside
// the list pane in a 2-col layout (< dividerStart); Y maps the row back through
// the same g.firstRow origin the renderer uses.
func clickListRow(t *testing.T, m model, idx int) model {
	t.Helper()
	g := m.layout()
	y := g.firstRow + (idx - m.listTop)
	nm, _ := m.handleMouse(tea.MouseClickMsg{X: 2, Y: y, Button: tea.MouseLeft})
	return nm.(model)
}

// TestListClickSameFileNoRerender is the headline: clicking the file that is
// ALREADY selected must be a no-op for the preview — it keeps the scroll offset
// and the rendered buffer instead of resetting to the top and re-dispatching the
// async render. refreshPreview zeroes previewTop and srcWidth; if the re-click
// wrongly called it, both would reset. Mirrors moveCursor's "target == cursor →
// return" guard so mouse and keyboard agree.
func TestListClickSameFileNoRerender(t *testing.T) {
	dir := t.TempDir()
	// A markdown file long enough that the preview can scroll (previewLen > bodyH).
	var b strings.Builder
	b.WriteString("# Title\n\n")
	for range 60 {
		b.WriteString("Body line with some **bold** words to render.\n\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("plain\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := modelAt(t, dir, 100, 30) // cursor 0 = a.md (dirs-first-alpha)
	if got := m.entries[m.cursor].name; got != "a.md" {
		t.Fatalf("setup: cursor on %q, want \"a.md\"", got)
	}
	m.renderNow() // drive the async markdown render to completion
	if m.srcWidth == 0 {
		t.Fatalf("setup: expected a.md rendered (srcWidth>0), got 0")
	}
	m.scrollPreview(4)
	if m.previewTop == 0 {
		t.Fatalf("setup: expected the preview to scroll (previewTop>0), got 0")
	}
	wantTop, wantWidth := m.previewTop, m.srcWidth

	// Re-click the row a.md already occupies.
	m = clickListRow(t, m, m.cursor)

	if m.previewTop != wantTop {
		t.Errorf("re-clicking the open file reset the scroll: previewTop=%d want %d", m.previewTop, wantTop)
	}
	if m.srcWidth != wantWidth {
		t.Errorf("re-clicking the open file re-rendered: srcWidth=%d want %d (0 means refreshPreview ran)", m.srcWidth, wantWidth)
	}
}

// TestListClickSameFileCancelsSelection guards the one side effect the no-op must
// keep: the click moves focus to the list, and an in-app preview selection is a
// focusPreview sub-state, so it must end (refreshPreview used to cancel it). Left
// armed, the next keypress would route to updateSelecting even though focus is on
// the list. The preview buffer/scroll must still be untouched (no re-render).
func TestListClickSameFileCancelsSelection(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(strings.Repeat("line\n", 40)), 0o644); err != nil {
		t.Fatal(err)
	}

	m := modelAt(t, dir, 100, 30) // cursor 0 = a.txt (scrollable plain text)
	m.renderNow()
	m.focusPane = focusPreview
	m.startSelection()
	if !m.selecting {
		t.Fatalf("setup: selection did not start")
	}
	wantTop := m.previewTop

	m = clickListRow(t, m, m.cursor) // re-click a.txt's own row

	if m.focusPane != focusList {
		t.Errorf("a list click must focus the list: focusPane=%d want focusList", m.focusPane)
	}
	if m.selecting {
		t.Errorf("re-clicking the open file left the selection armed while focus moved to the list")
	}
	if m.previewTop != wantTop {
		t.Errorf("cancelling the selection must not move the preview: previewTop=%d want %d", m.previewTop, wantTop)
	}
}

// TestListClickSameFolderStillDescends guards the intentional exception: a folder
// re-click is click-to-open (enter it), NOT a no-op. Only the file case is
// suppressed.
func TestListClickSameFolderStillDescends(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "inner.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := modelAt(t, dir, 100, 30) // cursor 0 = sub/ (only entry)
	if got := m.entries[m.cursor].name; got != "sub" {
		t.Fatalf("setup: cursor on %q, want \"sub\"", got)
	}

	m = clickListRow(t, m, m.cursor) // re-click the already-selected folder

	if m.cwd != sub {
		t.Errorf("re-clicking the selected folder did not descend: cwd=%q want %q", m.cwd, sub)
	}
}

// TestListClickDifferentFileRefreshes guards that the no-op is scoped to the SAME
// row: clicking a DIFFERENT file still moves the cursor and reloads the preview
// (scroll resets to the top of the new file).
func TestListClickDifferentFileRefreshes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(strings.Repeat("aaaa\n", 50)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("bbbb\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := modelAt(t, dir, 100, 30) // cursor 0 = a.txt
	m.scrollPreview(3)
	if m.previewTop == 0 {
		t.Fatalf("setup: expected a.txt to scroll, got previewTop 0")
	}

	m = clickListRow(t, m, 1) // click b.txt

	if got := m.entries[m.cursor].name; got != "b.txt" {
		t.Errorf("click on a different row: selected %q, want \"b.txt\"", got)
	}
	if m.previewTop != 0 {
		t.Errorf("switching files must reset the scroll: previewTop=%d want 0", m.previewTop)
	}
}
