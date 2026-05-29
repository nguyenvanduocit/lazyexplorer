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
	if want := 30 - 1 - headerH; g.bodyH != want {
		t.Errorf("bodyH = %d, want %d (height-1-headerH)", g.bodyH, want)
	}
	if g.firstRow != headerH {
		t.Errorf("firstRow = %d, want headerH=%d (below the path header)", g.firstRow, headerH)
	}
	if total := g.leftInner + dividerWidth + g.rightInner; total != m.width {
		t.Errorf("leftInner + dividerWidth + rightInner = %d, want m.width=%d (geometry must tile)",
			total, m.width)
	}
}

// TestLayoutAtNarrowWidthIsVertical (PRD docs/prd-responsive-layout.md §6
// checklist 9): width=31 is far below widthBreakpoint=80, so layout() returns
// the 1-col stacked geometry. Both panes share m.width (rightInner = 0 in
// vertical mode), the horizontal divider sits at row dividerYStart = topInner,
// and topInner + dividerHeight + bottomInner tiles bodyH.
//
// This test replaces the old 2-col-min-width snapshot: with the responsive
// trigger, the practical horizontal-mode floor is widthBreakpoint=80, not the
// geometric 2*minPanelInnerCols + dividerWidth = 31 of the middle-divider era.
func TestLayoutAtNarrowWidthIsVertical(t *testing.T) {
	m := model{width: 31, height: 30, leftRatio: 0.5, topRatio: 0.33, tel: noopRecorder{}}
	g := m.layout()
	if !g.vertical {
		t.Fatalf("at width=31 (< widthBreakpoint=%d), expected vertical layout, got horizontal", widthBreakpoint)
	}
	if g.leftInner != 31 {
		t.Errorf("leftInner = %d, want 31 (= m.width in vertical mode)", g.leftInner)
	}
	if g.rightInner != 0 {
		t.Errorf("rightInner = %d, want 0 (vertical mode has no right pane on X axis)", g.rightInner)
	}
	if g.dividerStart != 0 {
		t.Errorf("dividerStart = %d, want 0 (vertical mode)", g.dividerStart)
	}
	// bodyH = height - 1 - headerH = 28; the body sits below the header, so the
	// divider glyph row and the preview's first row both carry firstRow=headerH.
	if want := 30 - 1 - headerH; g.bodyH != want {
		t.Errorf("bodyH = %d, want %d", g.bodyH, want)
	}
	if g.dividerYStart != g.firstRow+g.topInner {
		t.Errorf("dividerYStart = %d, want firstRow+topInner=%d (glyph row below the header)",
			g.dividerYStart, g.firstRow+g.topInner)
	}
	if total := g.topInner + dividerHeight + g.bottomInner; total != g.bodyH {
		t.Errorf("topInner(%d) + dividerHeight(%d) + bottomInner(%d) = %d, want bodyH=%d (geometry must tile)",
			g.topInner, dividerHeight, g.bottomInner, total, g.bodyH)
	}
	if g.previewFirstRow != g.firstRow+g.topInner+dividerHeight {
		t.Errorf("previewFirstRow = %d, want firstRow+topInner+dividerHeight=%d",
			g.previewFirstRow, g.firstRow+g.topInner+dividerHeight)
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

// TestFolderClickRow0SelectsEntry0 pins the folder-preview hit-test against the
// preview's first body row: with the path header reserving firstRow=headerH
// rows at the top, the first preview row sits at Y=g.previewFirstRow and maps to
// previewEntries[0]. PRD prd-cwd-path-header §6.
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
	// X past the divider, Y at the first preview body row (= previewFirstRow).
	nm, _ := m.handleMouse(tea.MouseClickMsg{
		X:      g.dividerStart + dividerWidth + 5,
		Y:      g.previewFirstRow,
		Button: tea.MouseLeft,
	})
	m = nm.(model)
	if m.cwd != sub {
		t.Errorf("after click: cwd = %q, want %q", m.cwd, sub)
	}
	if got := m.entries[m.cursor].name; got != "aaa.txt" {
		t.Errorf("after click on previewFirstRow: cursor on %q, want \"aaa.txt\"", got)
	}
}

// TestListClickRow0SelectsEntry0 mirrors the folder hit-test on the list pane:
// a click at the first list body row (Y=g.firstRow, below the path header) maps
// to entries[0].
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
	nm, _ := m.handleMouse(tea.MouseClickMsg{X: 2, Y: g.firstRow, Button: tea.MouseLeft})
	m = nm.(model)
	if got := m.entries[m.cursor].name; got != "aaa.txt" {
		t.Errorf("list pane click at Y=firstRow: cursor on %q, want \"aaa.txt\"", got)
	}
}

// ----------------------------------------------------------------------------
// Responsive layout — vertical-mode (1-col stacked) hit-test + drag suite.
// Fixture: width=70 (< widthBreakpoint=80), height=24, leftRatio=0.38,
// topRatio=0.33 → bodyH=23, topInner=8, dividerYStart=8, previewFirstRow=9,
// bottomInner=14. PRD §6 checklist 4-9.
// ----------------------------------------------------------------------------

// TestYDividerHitZoneIsExactlyGlyphRow walks five rows centred on the glyph
// row and asserts drag-start fires for the glyph row ONLY. The 0/0 hit-zone
// constants preserve the "visible width == hit zone" invariant from
// horizontal mode (PRD §3 D9, §5.1 dividerHitRowsAbove/Below=0): rows just
// above and below the glyph route to list/preview as ordinary clicks, never
// to a surprise drag.
func TestYDividerHitZoneIsExactlyGlyphRow(t *testing.T) {
	cases := []struct {
		name string
		dy   int
		want bool // expect dragging=true after the click
	}{
		{"two rows above glyph", -2, false},
		{"one row above glyph", -1, false},
		{"glyph row", 0, true},
		{"one row below glyph", 1, false},
		{"two rows below glyph", 2, false},
	}
	for _, c := range cases {
		m := model{width: 70, height: 24, leftRatio: 0.38, topRatio: 0.33, tel: noopRecorder{}}
		g := m.layout()
		if !g.vertical {
			t.Fatalf("setup: g.vertical = false at width=70, want true")
		}
		y := g.dividerYStart + c.dy
		nm, _ := m.handleMouse(tea.MouseClickMsg{X: 5, Y: y, Button: tea.MouseLeft})
		got := nm.(model).dragging
		if got != c.want {
			t.Errorf("%s (Y=%d, dividerYStart=%d): dragging=%v, want %v",
				c.name, y, g.dividerYStart, got, c.want)
		}
	}
}

// TestYDividerDrag walks the full press → motion → release cycle on the Y
// axis. setTopFromY maps a SCREEN row y back through the header offset:
// topRatio = (y-headerH) / bodyH, where bodyH = height-1-headerH (the inverse
// of layout's dividerYStart = headerH + topInner).
func TestYDividerDrag(t *testing.T) {
	m := model{width: 70, height: 24, leftRatio: 0.38, topRatio: 0.33, tel: noopRecorder{}}
	g := m.layout()
	if !g.vertical {
		t.Fatalf("setup: g.vertical = false, want true at width=70")
	}
	bodyH := float64(24 - 1 - headerH)

	step := func(msg tea.MouseMsg) {
		nm, _ := m.handleMouse(msg)
		m = nm.(model)
	}

	// Press on the glyph row (a screen Y in the divider band, below the header).
	pressY := g.dividerYStart
	step(tea.MouseClickMsg{X: 5, Y: pressY, Button: tea.MouseLeft})
	if !m.dragging {
		t.Fatal("press on Y divider did not start a drag")
	}
	wantPress := float64(pressY-headerH) / bodyH
	if m.topRatio != wantPress {
		t.Errorf("after press at Y=%d: topRatio = %.6f, want %.6f", pressY, m.topRatio, wantPress)
	}

	// Drag down: motion to Y=12 → topRatio = (12-headerH) / bodyH.
	step(tea.MouseMotionMsg{X: 5, Y: 12, Button: tea.MouseLeft})
	want := float64(12-headerH) / bodyH
	if m.topRatio != want {
		t.Errorf("after drag down: topRatio = %.6f, want %.6f", m.topRatio, want)
	}

	// Release ends the drag.
	step(tea.MouseReleaseMsg{X: 5, Y: 12, Button: tea.MouseLeft})
	if m.dragging {
		t.Fatal("release did not end the Y drag")
	}

	// Motion after release must NOT move the divider.
	ratioBefore := m.topRatio
	step(tea.MouseMotionMsg{X: 5, Y: 18, Button: tea.MouseLeft})
	if m.topRatio != ratioBefore {
		t.Errorf("motion after release moved Y divider: ratio %.3f → %.3f", ratioBefore, m.topRatio)
	}
}

// TestWheelInVerticalRoutesByY: wheel in the list region (Y < dividerYStart)
// moves the cursor; wheel in the preview region (Y > dividerYStart) scrolls
// the preview; wheel exactly on the glyph row noops (FR9). Single-row hit
// zone keeps the divider's own row as the only noop band.
func TestWheelInVerticalRoutesByY(t *testing.T) {
	root := t.TempDir()
	for i := range 20 {
		mustWrite(t, root, "f"+string(rune('a'+i))+".txt", "x")
	}

	cases := []struct {
		name        string
		y           int
		btn         tea.MouseButton
		wantCursor  int // -1 means unchanged from setup
		wantPreview int // -1 means unchanged from setup
	}{
		{"wheel up in list region", 3, tea.MouseWheelUp, 4, 0},     // cursor 5 → 4
		{"wheel down in list region", 3, tea.MouseWheelDown, 6, 0}, // cursor 5 → 6
		{"wheel on glyph row noop", 8, tea.MouseWheelUp, 5, 0},
		{"wheel on glyph row down noop", 8, tea.MouseWheelDown, 5, 0},
		{"wheel up in preview region", 15, tea.MouseWheelUp, 5, 0},     // already at top, scroll noops
		{"wheel down in preview region", 15, tea.MouseWheelDown, 5, 1}, // one notch = previewLineStep (1), smooth scroll
	}
	for _, c := range cases {
		m := modelAt(t, root, 70, 24)
		m.cursor = 5
		m.previewTop = 0
		// Need previewLen > bottomInner for scroll-down to register; the file
		// preview is short, but a folder with 20 entries gives us length 20.
		// Setup: cursor=5 points at a file → previewLen = len(m.preview);
		// for the scroll-down test we want a previewLen big enough. Force
		// preview lines to 50 so one wheel notch (previewLineStep) registers
		// without clamping to 0.
		fakePreview := make([]string, 50)
		for i := range fakePreview {
			fakePreview[i] = "x"
		}
		m.preview = fakePreview
		m.previewIsDir = false

		nm, _ := m.handleMouse(tea.MouseWheelMsg{X: 5, Y: c.y, Button: c.btn})
		m2 := nm.(model)

		if m2.cursor != c.wantCursor {
			t.Errorf("%s: cursor = %d, want %d", c.name, m2.cursor, c.wantCursor)
		}
		if m2.previewTop != c.wantPreview {
			t.Errorf("%s: previewTop = %d, want %d", c.name, m2.previewTop, c.wantPreview)
		}
	}
}

// TestVerticalListRow0Click: click at the first list body row (Y=g.firstRow,
// below the path header) in vertical mode selects entries[0].
func TestVerticalListRow0Click(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, "aaa.txt", "x")
	mustWrite(t, root, "bbb.txt", "y")

	m := modelAt(t, root, 70, 24)
	m.cursor = 1 // start on bbb.txt
	g := m.layout()
	if !g.vertical {
		t.Fatalf("setup: not vertical at width=70")
	}

	nm, _ := m.handleMouse(tea.MouseClickMsg{X: 5, Y: g.firstRow, Button: tea.MouseLeft})
	m = nm.(model)
	if got := m.entries[m.cursor].name; got != "aaa.txt" {
		t.Errorf("vertical list pane click at Y=firstRow: cursor on %q, want \"aaa.txt\"", got)
	}
}

// TestVerticalPreviewRow0ClickEntersFolder: with cursor on a sub-folder, the
// preview pane shows that folder's listing. Click at Y=previewFirstRow (first
// row of preview, immediately below the glyph row) descends into the folder
// and lands on the clicked entry — same end state as descend-from-list.
// This is the test that LOCKS the 0/0 hit-zone choice: with 1/1 (advisor's
// original recommendation), Y=previewFirstRow would have been in the drag
// zone and the first preview entry would have been unclickable.
func TestVerticalPreviewRow0ClickEntersFolder(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	// Alpha-sorted: aaa.txt is previewEntries[0].
	mustWrite(t, sub, "aaa.txt", "x")
	mustWrite(t, sub, "bbb.txt", "y")

	m := modelAt(t, root, 70, 24)
	if !m.previewIsDir || len(m.previewEntries) != 2 {
		t.Fatalf("setup: previewIsDir=%v, previewEntries=%d, want true, 2",
			m.previewIsDir, len(m.previewEntries))
	}
	g := m.layout()
	if !g.vertical {
		t.Fatalf("setup: not vertical at width=70")
	}

	// First preview row sits at previewFirstRow = topInner+dividerHeight = 9.
	// X anywhere in the pane — vertical mode has no X split inside the pane.
	nm, _ := m.handleMouse(tea.MouseClickMsg{X: 30, Y: g.previewFirstRow, Button: tea.MouseLeft})
	m = nm.(model)

	if m.cwd != sub {
		t.Errorf("after click at Y=previewFirstRow=%d: cwd = %q, want %q", g.previewFirstRow, m.cwd, sub)
	}
	if got := m.entries[m.cursor].name; got != "aaa.txt" {
		t.Errorf("after click: cursor on %q, want \"aaa.txt\"", got)
	}
}

// TestVerticalRowAdjacentToDividerRoutesToPane confirms the design choice
// from PRD §3 D9 + §5.12: the single-row hit zone keeps both pane edges
// clickable. Click at Y=dividerYStart-1 → list entries[topInner-1] (the
// bottom-visible list row). Click at Y=dividerYStart+1 → first preview entry.
func TestVerticalRowAdjacentToDividerRoutesToPane(t *testing.T) {
	root := t.TempDir()
	// Need at least topInner=8 entries for the list-bottom click test.
	for i := range 10 {
		mustWrite(t, root, "f"+string(rune('a'+i))+".txt", "x")
	}

	m := modelAt(t, root, 70, 24)
	g := m.layout()
	if !g.vertical || g.dividerYStart != 8 {
		t.Fatalf("setup: vertical=%v dividerYStart=%d, want true 8", g.vertical, g.dividerYStart)
	}
	// Last visible list row is at Y = g.topInner-1 = 7. The list slice starts
	// at listTop=0 (cursor=0), so row 7 maps to entries[7].
	nm, _ := m.handleMouse(tea.MouseClickMsg{X: 5, Y: g.dividerYStart - 1, Button: tea.MouseLeft})
	m2 := nm.(model)
	if m2.dragging {
		t.Error("click one row above glyph wrongly started a drag")
	}
	if m2.cursor != g.topInner-1 {
		t.Errorf("click at Y=dividerYStart-1: cursor = %d, want %d", m2.cursor, g.topInner-1)
	}
}
