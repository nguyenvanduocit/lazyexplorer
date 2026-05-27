package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestPreviewClickOpensFolderAndSelects walks the headline behaviour: with a
// folder selected, its contents show in the right panel; clicking a file name
// there must enter the folder and land the cursor on that file — identical to
// descending via the left panel and selecting it. Matching by name (not index)
// is what survives the synthetic ".." that descend() prepends.
func TestPreviewClickOpensFolderAndSelects(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "file.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := modelAt(t, root, 100, 30) // dividerStart=37; preview cols start at x=40 (post-divider)
	// cursor 0 is "sub/" (only entry); the preview shows its one-item listing.
	if m.entries[m.cursor].name != "sub" {
		t.Fatalf("setup: cursor on %q, want \"sub\"", m.entries[m.cursor].name)
	}

	// Right panel listing starts at the first body row (y=0, firstRow=0). Click "file.txt".
	nm, _ := m.handleMouse(tea.MouseClickMsg{X: 50, Y: 0, Button: tea.MouseLeft})
	m = nm.(model)

	if m.cwd != sub {
		t.Errorf("after click: cwd = %q, want %q", m.cwd, sub)
	}
	if got := m.entries[m.cursor].name; got != "file.txt" {
		t.Errorf("after click: selected %q, want \"file.txt\"", got)
	}
	// State must equal navigating in: a file preview, not the folder listing.
	if m.entries[m.cursor].isDir {
		t.Errorf("after click: selection is a dir, expected the file \"file.txt\"")
	}
}

// TestPreviewClickBelowListingNoop guards the empty/over-click paths: a click
// past the last listing row must not enter the folder or move the cursor.
func TestPreviewClickBelowListingNoop(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "file.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := modelAt(t, root, 100, 30)
	wantCwd := m.cwd

	// y=20 is well below the single listing row (y=0) but still inside the body.
	nm, _ := m.handleMouse(tea.MouseClickMsg{X: 50, Y: 20, Button: tea.MouseLeft})
	m = nm.(model)

	if m.cwd != wantCwd {
		t.Errorf("click below listing changed cwd: %q → %q", wantCwd, m.cwd)
	}
	if m.entries[m.cursor].name != "sub" {
		t.Errorf("click below listing moved cursor off \"sub\" to %q", m.entries[m.cursor].name)
	}
}

// TestPreviewClickOnFilePreviewNoop guards that when the right panel shows a
// file's contents (not a folder listing), clicking it does nothing.
func TestPreviewClickOnFilePreviewNoop(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("line one\nline two\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := modelAt(t, root, 100, 30) // cursor 0 = "a.txt", right panel = its contents
	if m.entries[m.cursor].name != "a.txt" {
		t.Fatalf("setup: cursor on %q, want \"a.txt\"", m.entries[m.cursor].name)
	}
	wantCwd := m.cwd

	nm, _ := m.handleMouse(tea.MouseClickMsg{X: 50, Y: 3, Button: tea.MouseLeft})
	m = nm.(model)

	if m.cwd != wantCwd {
		t.Errorf("click on file preview changed cwd: %q → %q", wantCwd, m.cwd)
	}
}

// TestPreviewClickScrolledMapsWithOffset proves the row→line math honours the
// preview scroll offset: when the listing is taller than the panel and scrolled,
// a click on a visible row must resolve to top+row, not row. Without the offset
// it would select the wrong (off-by-`top`) file.
func TestPreviewClickScrolledMapsWithOffset(t *testing.T) {
	root := t.TempDir()
	big := filepath.Join(root, "big")
	if err := os.Mkdir(big, 0o755); err != nil {
		t.Fatal(err)
	}
	// 40 files: sorted alpha, file10.txt sits at index 10 in the listing.
	for i := 0; i < 40; i++ {
		name := fmt.Sprintf("file%02d.txt", i)
		if err := os.WriteFile(filepath.Join(big, name), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	m := modelAt(t, root, 100, 30) // bodyH = max(30-1,3) = 29
	if m.entries[m.cursor].name != "big" {
		t.Fatalf("setup: cursor on %q, want \"big\"", m.entries[m.cursor].name)
	}
	m.scrollPreview(10) // previewTop → 10 (clamped below maxTop=11)

	// First visible listing row is y=0 (firstRow=0). With top=10 it shows
	// item 10 → file10.txt.
	nm, _ := m.handleMouse(tea.MouseClickMsg{X: 50, Y: 0, Button: tea.MouseLeft})
	m = nm.(model)

	if m.cwd != big {
		t.Errorf("after click: cwd = %q, want %q", m.cwd, big)
	}
	if got := m.entries[m.cursor].name; got != "file10.txt" {
		t.Errorf("scrolled click selected %q, want \"file10.txt\" (offset not applied?)", got)
	}
}

// TestPreviewClickStatusRowBelowScrolledNoop guards the upper-bound on the row:
// a click below the last visible listing row (panel border / status area) must
// not map to a hidden, scrolled-past item. This is the exact off-by-window bug
// the bodyH guard prevents.
func TestPreviewClickStatusRowBelowScrolledNoop(t *testing.T) {
	root := t.TempDir()
	big := filepath.Join(root, "big")
	if err := os.Mkdir(big, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 40; i++ {
		name := fmt.Sprintf("file%02d.txt", i)
		if err := os.WriteFile(filepath.Join(big, name), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	m := modelAt(t, root, 100, 30) // bodyH=29; visible body rows y=0..28; status at y=29
	m.scrollPreview(10)            // top=10; without the guard, y=29 → a hidden item
	wantCwd := m.cwd

	// y=29 is the status row, one past the last visible body row (y=28). The
	// guard row >= bodyH must noop the click — without it the e.X branch would
	// resolve top+row to a scrolled-past hidden entry (PRD §5.8 row check).
	nm, _ := m.handleMouse(tea.MouseClickMsg{X: 50, Y: 29, Button: tea.MouseLeft})
	m = nm.(model)

	if m.cwd != wantCwd {
		t.Errorf("click below visible listing entered a folder: cwd %q → %q", wantCwd, m.cwd)
	}
	if m.entries[m.cursor].name != "big" {
		t.Errorf("click below visible listing moved cursor off \"big\" to %q", m.entries[m.cursor].name)
	}
}

// TestPreviewClickOpensSubfolder proves the behaviour generalises beyond files:
// clicking a sub-folder's name in the listing enters the parent and selects that
// sub-folder, so the right panel then shows the sub-folder's own contents.
func TestPreviewClickOpensSubfolder(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "parent")
	child := filepath.Join(parent, "child")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	// "child/" sorts before "zzz.txt" (dirs first), so it is listing row 0.
	if err := os.WriteFile(filepath.Join(parent, "zzz.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := modelAt(t, root, 100, 30) // cursor 0 = "parent", preview = its listing
	nm, _ := m.handleMouse(tea.MouseClickMsg{X: 50, Y: 0, Button: tea.MouseLeft})
	m = nm.(model)

	if m.cwd != parent {
		t.Errorf("after click: cwd = %q, want %q", m.cwd, parent)
	}
	sel := m.entries[m.cursor]
	if sel.name != "child" || !sel.isDir {
		t.Errorf("after click: selected %q (isDir=%v), want \"child\" dir", sel.name, sel.isDir)
	}
}

// TestPreviewClickEmptyFolderNoop guards the empty-listing path: the selected
// folder is empty, so its preview is just a placeholder line. Clicking that line
// resolves to no real item and must not descend.
func TestPreviewClickEmptyFolderNoop(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}

	m := modelAt(t, root, 100, 30) // cursor 0 = "empty"; preview = "(empty folder)"
	wantCwd := m.cwd

	nm, _ := m.handleMouse(tea.MouseClickMsg{X: 50, Y: 0, Button: tea.MouseLeft})
	m = nm.(model)

	if m.cwd != wantCwd {
		t.Errorf("click on empty-folder placeholder entered it: cwd %q → %q", wantCwd, m.cwd)
	}
}
