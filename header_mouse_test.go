package main

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestHeaderRowIsNoPaneZone proves the header row (Y < firstRow) is passive
// chrome: a left-click on it must NOT flip focus, move the cursor, or start a
// divider drag — mirroring the status-row exclusion. The header sits at screen
// row 0; the body (and thus the list) starts at firstRow=headerH.
func TestHeaderRowIsNoPaneZone(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, "aaa.txt", "x")
	mustWrite(t, root, "bbb.txt", "y")

	m := modelAt(t, root, 100, 30)
	m.cursor = 1
	m.focusPane = focusPreview // start focused on preview to detect a flip
	g := m.layout()
	if g.firstRow == 0 {
		t.Fatalf("setup: firstRow=0, header not reserved")
	}

	// Click on the header row (Y=0), in a column that would land in the list pane
	// were it a body row. Must be a complete noop.
	nm, _ := m.handleMouse(tea.MouseClickMsg{X: 2, Y: 0, Button: tea.MouseLeft})
	m2 := nm.(model)
	if m2.focusPane != focusPreview {
		t.Errorf("header click flipped focus to %v, want unchanged (focusPreview)", m2.focusPane)
	}
	if m2.cursor != 1 {
		t.Errorf("header click moved cursor to %d, want unchanged (1)", m2.cursor)
	}
	if m2.dragging {
		t.Errorf("header click started a divider drag")
	}
}

// TestHeaderColumnClickDoesNotStartDrag guards the horizontal-mode drag-start
// bug: overDivider is X-only in 2-col mode, so a header-row click whose X falls
// in the divider column would wrongly begin a drag without the e.Y >= firstRow
// guard. This pins that guard.
func TestHeaderColumnClickDoesNotStartDrag(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, "aaa.txt", "x")

	m := modelAt(t, root, 100, 30)
	g := m.layout()
	// Click on the header row, in a divider column.
	nm, _ := m.handleMouse(tea.MouseClickMsg{X: g.dividerStart, Y: 0, Button: tea.MouseLeft})
	if nm.(model).dragging {
		t.Errorf("header-row click in divider column started a drag (missing e.Y >= firstRow guard)")
	}
}

// TestHeaderWheelIsNoPaneZone proves a mouse wheel over the header row (Y <
// firstRow) is a noop in BOTH orientations — it must not scroll the list (move
// cursor) nor the preview (move previewTop). This pins the `e.Y < g.firstRow`
// clause of the wheel guard (model.go): without it, the axis-aware overList
// split routes a header wheel into the list pane (horizontal: a list column;
// vertical: the header row is < dividerYStart so it routes to the list
// regardless of X), silently scrolling on what is supposed to be passive chrome.
func TestHeaderWheelIsNoPaneZone(t *testing.T) {
	for _, tc := range []struct {
		name string
		w, h int
	}{
		{"horizontal", 100, 30},
		{"vertical", 70, 24},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			mustWrite(t, root, "aaa.txt", "x")
			mustWrite(t, root, "bbb.txt", "y")
			mustWrite(t, root, "ccc.txt", "z")

			m := modelAt(t, root, tc.w, tc.h)
			m.cursor = 1
			m.previewTop = 0
			g := m.layout()
			if g.firstRow == 0 {
				t.Fatalf("setup: firstRow=0, header not reserved")
			}

			// Wheel down then up at the header row (Y=0), X=2 (a list column in
			// horizontal mode). Both directions must leave the list cursor and the
			// preview scroll untouched. Driving both directions makes removal of the
			// guard detectable regardless of which way the lost wheel would scroll.
			for _, btn := range []tea.MouseButton{tea.MouseWheelDown, tea.MouseWheelUp} {
				nm, _ := m.handleMouse(tea.MouseWheelMsg{X: 2, Y: 0, Button: btn})
				m = nm.(model)
				if m.cursor != 1 {
					t.Errorf("header wheel (%v) moved cursor to %d, want unchanged (1)", btn, m.cursor)
				}
				if m.previewTop != 0 {
					t.Errorf("header wheel (%v) scrolled preview to %d, want unchanged (0)", btn, m.previewTop)
				}
			}
		})
	}
}

// TestListClickHonorsHeaderOffset is the mouse-parity proof: a left-click at the
// screen row of list entry k (= firstRow + k) selects entries[k]. With the
// header reserving firstRow=headerH rows, the click at Y=firstRow must map to
// entries[0] (proving firstRow is threaded into the list hit-test).
func TestListClickHonorsHeaderOffset(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, "aaa.txt", "x")
	mustWrite(t, root, "bbb.txt", "y")
	mustWrite(t, root, "ccc.txt", "z")

	m := modelAt(t, root, 100, 30)
	m.cursor = 2
	g := m.layout()

	// Click at the first list row (Y = firstRow) → entries[0].
	nm, _ := m.handleMouse(tea.MouseClickMsg{X: 2, Y: g.firstRow, Button: tea.MouseLeft})
	m = nm.(model)
	if got := m.entries[m.cursor].name; got != "aaa.txt" {
		t.Errorf("click at Y=firstRow: cursor on %q, want \"aaa.txt\"", got)
	}

	// Click at Y = firstRow+1 → entries[1].
	nm, _ = m.handleMouse(tea.MouseClickMsg{X: 2, Y: g.firstRow + 1, Button: tea.MouseLeft})
	m = nm.(model)
	if got := m.entries[m.cursor].name; got != "bbb.txt" {
		t.Errorf("click at Y=firstRow+1: cursor on %q, want \"bbb.txt\"", got)
	}
}

// TestPreviewClickHonorsHeaderOffset proves previewFirstRow carries the header
// offset: with a folder selected, clicking the first preview row (Y=firstRow in
// horizontal mode) opens the child and lands on previewEntries[0].
func TestPreviewClickHonorsHeaderOffset(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, sub, "aaa.txt", "x")

	m := modelAt(t, root, 100, 30) // cursor on "sub", preview = its listing
	g := m.layout()
	nm, _ := m.handleMouse(tea.MouseClickMsg{
		X:      g.dividerStart + dividerWidth + 5,
		Y:      g.previewFirstRow,
		Button: tea.MouseLeft,
	})
	m = nm.(model)
	if m.cwd != sub {
		t.Errorf("preview click at Y=previewFirstRow: cwd = %q, want %q", m.cwd, sub)
	}
	if got := m.entries[m.cursor].name; got != "aaa.txt" {
		t.Errorf("preview click: cursor on %q, want \"aaa.txt\"", got)
	}
}
