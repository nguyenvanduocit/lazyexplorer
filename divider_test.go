package main

// Tests for the borderless 2-pane layout with a single 3-col middle divider
// (docs/prd-middle-divider.md). These pin the contract that resize_test.go and
// previewclick_test.go lean on but don't articulate as standalone behavior.

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestDividerWidthConstant locks the divider width to 3 cols (pad-left +
// glyph + pad-right). Hit-test and layout math both assume it; widening to 5
// cols is the explicit "easier to grab" toggle PRD §5.10 defers — a future
// one-liner, not a silent constant flip.
func TestDividerWidthConstant(t *testing.T) {
	if dividerWidth != 3 {
		t.Errorf("dividerWidth = %d, want 3 (PRD §5.1 D3)", dividerWidth)
	}
	if dividerWidth != dividerPadLeft+1+dividerPadRight {
		t.Errorf("dividerWidth(%d) != padLeft(%d)+1+padRight(%d) — geometry inconsistent",
			dividerWidth, dividerPadLeft, dividerPadRight)
	}
}

// TestLayoutSnapshot pins the headline layout numbers from PRD §6 checklist 2.
// At the default leftRatio=0.38 the divider glyph sits on col 38, leftInner=37,
// rightInner=60, and the body fills 29 of 30 rows (status takes the last).
func TestLayoutSnapshot(t *testing.T) {
	m := model{width: 100, height: 30, leftRatio: 0.38, tel: noopRecorder{}}
	g := m.layout()
	if g.leftInner != 37 {
		t.Errorf("leftInner = %d, want 37", g.leftInner)
	}
	if g.rightInner != 60 {
		t.Errorf("rightInner = %d, want 60", g.rightInner)
	}
	if g.dividerStart != 37 {
		t.Errorf("dividerStart = %d, want 37 (= leftInner)", g.dividerStart)
	}
	if g.bodyH != 29 {
		t.Errorf("bodyH = %d, want 29 (m.height-1)", g.bodyH)
	}
	if g.firstRow != 0 {
		t.Errorf("firstRow = %d, want 0 (no top border)", g.firstRow)
	}
	if total := g.leftInner + dividerWidth + g.rightInner; total != m.width {
		t.Errorf("leftInner + dividerWidth + rightInner = %d, want m.width=%d (geometry must tile)",
			total, m.width)
	}
}

// TestLayoutSnapshotAtMinWidth (PRD §6 checklist 9): at the exact minimum
// total width (2*minPanelInnerCols + dividerWidth = 31), both panes get the
// floor and the divider keeps its 3 cols.
func TestLayoutSnapshotAtMinWidth(t *testing.T) {
	m := model{width: 31, height: 30, leftRatio: 0.5, tel: noopRecorder{}}
	g := m.layout()
	if g.leftInner != minPanelInnerCols {
		t.Errorf("leftInner = %d, want %d", g.leftInner, minPanelInnerCols)
	}
	if g.rightInner != minPanelInnerCols {
		t.Errorf("rightInner = %d, want %d", g.rightInner, minPanelInnerCols)
	}
	if g.dividerStart != minPanelInnerCols {
		t.Errorf("dividerStart = %d, want %d", g.dividerStart, minPanelInnerCols)
	}
}

// TestDragZoneBoundaries walks every column in [dividerStart-1, dividerStart+3]
// and asserts the drag-start hit-test fires for the 3-col band only — wider
// than the old single-edge zone but bounded on both sides (PRD §6 checklist 4).
func TestDragZoneBoundaries(t *testing.T) {
	cases := []struct {
		name string
		dx   int
		want bool // expect dragging=true after the click
	}{
		{"one col left of divider", -1, false},
		{"pad-left col", 0, true},
		{"glyph col", 1, true},
		{"pad-right col", 2, true},
		{"one col right of divider", 3, false},
	}
	for _, c := range cases {
		m := model{width: 100, height: 30, leftRatio: 0.38, tel: noopRecorder{}}
		g := m.layout()
		x := g.dividerStart + c.dx
		nm, _ := m.handleMouse(tea.MouseClickMsg{X: x, Y: 5, Button: tea.MouseLeft})
		got := nm.(model).dragging
		if got != c.want {
			t.Errorf("%s (X=%d, dividerStart=%d): dragging=%v, want %v",
				c.name, x, g.dividerStart, got, c.want)
		}
	}
}

// TestSetLeftFromXSnap pins the dropped-+1 semantics from PRD §5.9: x is the
// dividerCenter column, so setLeftFromX(50)/width=100 → leftRatio=0.500. The
// pre-divider formula would have produced 0.510.
func TestSetLeftFromXSnap(t *testing.T) {
	m := model{width: 100, height: 30, leftRatio: 0.38, tel: noopRecorder{}}
	m.setLeftFromX(50)
	if m.leftRatio != 0.5 {
		t.Errorf("setLeftFromX(50)/width=100: leftRatio = %.3f, want 0.500", m.leftRatio)
	}
}

// TestWheelOnDividerNoop walks both wheel directions over each divider col
// and asserts neither the list cursor nor the preview scroll changes. PRD FR9
// — without the overDivider guard, the e.X &lt; dividerStart split would route
// divider-col wheel events into the list pane.
func TestWheelOnDividerNoop(t *testing.T) {
	root := t.TempDir()
	// 10 entries so wheel-up from cursor=5 has somewhere to go.
	for i := range 10 {
		mustWrite(t, root, "f"+string(rune('a'+i))+".txt", "x")
	}

	for _, dx := range []int{0, 1, 2} {
		for _, btn := range []tea.MouseButton{tea.MouseWheelUp, tea.MouseWheelDown} {
			m := modelAt(t, root, 100, 30)
			m.cursor = 5
			m.previewTop = 0
			g := m.layout()
			x := g.dividerStart + dx
			nm, _ := m.handleMouse(tea.MouseWheelMsg{X: x, Y: 5, Button: btn})
			m2 := nm.(model)
			if m2.cursor != 5 {
				t.Errorf("dx=%d btn=%v: cursor moved 5 → %d (wheel over divider must noop)",
					dx, btn, m2.cursor)
			}
			if m2.previewTop != 0 {
				t.Errorf("dx=%d btn=%v: previewTop moved 0 → %d", dx, btn, m2.previewTop)
			}
		}
	}
}

// TestRightClickOnDividerNoop guards "left button only" for drag-start. A
// right-click on the divider must not start a drag or move the ratio.
func TestRightClickOnDividerNoop(t *testing.T) {
	m := model{width: 100, height: 30, leftRatio: 0.38, tel: noopRecorder{}}
	g := m.layout()
	ratioBefore := m.leftRatio
	nm, _ := m.handleMouse(tea.MouseClickMsg{X: g.dividerStart, Y: 5, Button: tea.MouseRight})
	m2 := nm.(model)
	if m2.dragging {
		t.Error("right-click on divider wrongly started a drag")
	}
	if m2.leftRatio != ratioBefore {
		t.Errorf("right-click moved leftRatio: %.3f → %.3f", ratioBefore, m2.leftRatio)
	}
}

// TestClickOnDividerAtStatusRowNoop guards the e.Y bound on drag-start: a
// left-click inside the divider's X range but on the status row (m.height-1)
// must NOT start a drag. PRD §5.8.
func TestClickOnDividerAtStatusRowNoop(t *testing.T) {
	m := model{width: 100, height: 30, leftRatio: 0.38, tel: noopRecorder{}}
	g := m.layout()
	nm, _ := m.handleMouse(tea.MouseClickMsg{
		X:      g.dividerStart + 1, // glyph col
		Y:      m.height - 1,       // status row
		Button: tea.MouseLeft,
	})
	if nm.(model).dragging {
		t.Error("click on divider's X range at status row started a drag")
	}
}

// TestClickOnDividerInsideBodyNonLeftNoop guards FR7: the divider band is a
// "no-pane" zone, so a non-left click inside it (already handled by the
// non-left guard) routes to nothing — never the list, never the preview.
// Specifically, a middle-click on the glyph col must not move the cursor
// nor scroll the preview.
func TestClickOnDividerInsideBodyNonLeftNoop(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, "a.txt", "x")
	mustWrite(t, root, "b.txt", "y")
	m := modelAt(t, root, 100, 30)
	cursorBefore := m.cursor
	g := m.layout()
	nm, _ := m.handleMouse(tea.MouseClickMsg{X: g.dividerStart + 1, Y: 1, Button: tea.MouseMiddle})
	if got := nm.(model).cursor; got != cursorBefore {
		t.Errorf("middle-click on divider moved cursor %d → %d", cursorBefore, got)
	}
}

// TestFolderClickRow0SelectsEntry0 is the regression test for firstRow=0:
// previously the first body row sat at y=1 (after top border); now y=0 maps
// directly to entries[0] of the folder preview. PRD §6 checklist 8.
func TestFolderClickRow0SelectsEntry0(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	// Alpha-sorted: aaa.txt is previewEntries[0].
	mustWrite(t, sub, "aaa.txt", "x")
	mustWrite(t, sub, "bbb.txt", "y")

	m := modelAt(t, root, 100, 30)
	if !m.previewIsDir || len(m.previewEntries) != 2 {
		t.Fatalf("setup: previewIsDir=%v, previewEntries=%d, want true, 2",
			m.previewIsDir, len(m.previewEntries))
	}

	g := m.layout()
	// X past the divider, Y at the first body row (0).
	nm, _ := m.handleMouse(tea.MouseClickMsg{
		X:      g.dividerStart + dividerWidth + 5,
		Y:      0,
		Button: tea.MouseLeft,
	})
	m = nm.(model)
	if m.cwd != sub {
		t.Errorf("after click: cwd = %q, want %q", m.cwd, sub)
	}
	if got := m.entries[m.cursor].name; got != "aaa.txt" {
		t.Errorf("after click on y=0: cursor on %q, want \"aaa.txt\"", got)
	}
}

// TestListClickRow0SelectsEntry0 mirrors the folder regression on the list
// pane side: click y=0 in the list pane → cursor on entries[0] (previously
// would have been entries[-1] / noop because firstRow=1 was subtracted).
func TestListClickRow0SelectsEntry0(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, "aaa.txt", "x")
	mustWrite(t, root, "bbb.txt", "y")

	m := modelAt(t, root, 100, 30)
	m.cursor = 1 // start on b.txt
	g := m.layout()
	if g.dividerStart < 5 {
		t.Fatalf("setup: dividerStart=%d too small to put a list-pane click at X=2", g.dividerStart)
	}
	nm, _ := m.handleMouse(tea.MouseClickMsg{X: 2, Y: 0, Button: tea.MouseLeft})
	m = nm.(model)
	if got := m.entries[m.cursor].name; got != "aaa.txt" {
		t.Errorf("list pane click y=0: cursor on %q, want \"aaa.txt\"", got)
	}
}
