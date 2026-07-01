package main

import (
	"strconv"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// Tests for the independent list scroll offset (wheel-pans-list-not-cursor pain):
// a mouse-wheel / touchpad scroll over the list pane pans the VIEWPORT (m.listTop)
// without moving the selection (m.cursor), while keyboard navigation still keeps the
// cursor visible. The cursor may scroll out of view — the user asked to look around
// the file list without changing the selected file.

// longListModel builds a horizontal-mode model whose list (n entries) overflows the
// visible rows, so the list scroll offset has room to move.
func longListModel(n, w, h int) model {
	entries := make([]entry, n)
	for i := range entries {
		entries[i] = entry{name: "file" + strconv.Itoa(i) + ".txt"}
	}
	return model{width: w, height: h, leftRatio: 0.4, topRatio: 0.33, tel: noopRecorder{}, entries: entries}
}

// TestWheelOverListScrollsViewportNotCursor is the headline contract: a wheel-down
// over the list pane scrolls the list (listTop grows) and leaves the selection put.
func TestWheelOverListScrollsViewportNotCursor(t *testing.T) {
	m := longListModel(50, 100, 24)
	m.cursor = 5
	g := m.layout()
	nm, _ := m.handleMouse(tea.MouseWheelMsg{X: 10, Y: g.firstRow + 2, Button: tea.MouseWheelDown})
	m = nm.(model)
	if m.cursor != 5 {
		t.Errorf("wheel over the list must NOT change the selection; cursor=%d want 5", m.cursor)
	}
	if m.listTop <= 0 {
		t.Errorf("wheel down must scroll the list viewport; listTop=%d want >0", m.listTop)
	}
}

// TestWheelListClampsAtBounds pins scrollList's range: it never goes negative and
// never past the last full window (len - visible rows).
func TestWheelListClampsAtBounds(t *testing.T) {
	m := longListModel(50, 100, 24)
	h := m.listRows()
	maxTop := 50 - h
	if maxTop <= 0 {
		t.Fatalf("fixture not tall enough to scroll: listRows=%d", h)
	}
	m.scrollList(-5) // already at top
	if m.listTop != 0 {
		t.Errorf("scroll up at the top must clamp to 0; got %d", m.listTop)
	}
	m.scrollList(1000) // far past the end
	if m.listTop != maxTop {
		t.Errorf("scroll past the end must clamp to maxTop=%d; got %d", maxTop, m.listTop)
	}
}

// TestKeyboardNavRevealsScrolledCursor: after the viewport is parked away from the
// cursor (by a wheel scroll), a keyboard move slides the viewport back so the cursor
// is visible again — keyboard nav always reveals the selection.
func TestKeyboardNavRevealsScrolledCursor(t *testing.T) {
	m := longListModel(50, 100, 24)
	m.cursor = 0
	m.scrollList(30) // park the viewport far below the cursor
	if m.listTop == 0 {
		t.Fatal("precondition: list should have scrolled away from the cursor")
	}
	h := m.listRows()
	m.moveCursor(1) // cursor 0→1, above the window → must be revealed
	top := m.listTopFor(h)
	if m.cursor < top || m.cursor >= top+h {
		t.Errorf("after keyboard nav the cursor %d must be visible in [%d,%d)", m.cursor, top, top+h)
	}
}

// TestNavBelowWindowScrollsToFollow: moving the cursor past the bottom of the window
// bottom-aligns the viewport to it — the classic scroll-to-follow on downward nav.
func TestNavBelowWindowScrollsToFollow(t *testing.T) {
	m := longListModel(50, 100, 24)
	m.cursor = 0
	m.listTop = 0
	h := m.listRows()
	m.moveCursor(49) // jump to the last entry
	if m.cursor != 49 {
		t.Fatalf("moveCursor to end: cursor=%d want 49", m.cursor)
	}
	top := m.listTopFor(h)
	if m.cursor < top || m.cursor >= top+h {
		t.Errorf("cursor at the end not revealed: window=[%d,%d)", top, top+h)
	}
	if top != 49-h+1 {
		t.Errorf("window should bottom-align the cursor: top=%d want %d", top, 49-h+1)
	}
}

// TestListClickHitTestFollowsScroll: after the list is scrolled, clicking the first
// visible body row selects the entry now at the top of the window (index == listTop),
// proving render and hit-testing share the same scroll offset.
func TestListClickHitTestFollowsScroll(t *testing.T) {
	m := longListModel(50, 100, 24)
	m.scrollList(10)
	g := m.layout()
	nm, _ := m.handleMouse(tea.MouseClickMsg{X: 10, Y: g.firstRow, Button: tea.MouseLeft})
	m = nm.(model)
	if m.cursor != 10 {
		t.Errorf("click first visible row at listTop=10 must select index 10; cursor=%d", m.cursor)
	}
}

// TestSearchNavRevealsCursor guards the flat-list (search) mode: its down-nav must
// also reveal the cursor, so routing it through moveCursor (not an inline cursor++)
// is load-bearing — a scrolled-away search result snaps back into view on nav.
func TestSearchNavRevealsCursor(t *testing.T) {
	m := longListModel(50, 100, 24)
	m.mode = modeSearch
	m.cursor = 0
	m.scrollList(30)
	h := m.listRows()
	nm, _ := m.updateSearch(tea.KeyPressMsg{Code: tea.KeyDown})
	m = nm.(model)
	if m.cursor != 1 {
		t.Fatalf("search down: cursor=%d want 1", m.cursor)
	}
	top := m.listTopFor(h)
	if m.cursor < top || m.cursor >= top+h {
		t.Errorf("search nav must reveal the cursor; cursor=1 window=[%d,%d)", top, top+h)
	}
}
