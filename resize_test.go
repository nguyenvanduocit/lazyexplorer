package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestLeftInnerWidthClamp pins the leftRatio → leftInner conversion under the
// divider-center semantics: ratio is the column of the glyph, so leftInner =
// dividerCenter - dividerPadLeft, then clamped so each pane keeps at least
// minPanelInnerCols of content and the divider gets its dividerWidth cols.
// Degenerate-narrow terminals fall back to the floor instead of panicking
// (PRD §5.4, §5.10 / D9-D10).
func TestLeftInnerWidthClamp(t *testing.T) {
	cases := []struct {
		name  string
		width int
		ratio float64
		want  int
	}{
		// At ratio=0.38 the glyph sits on col 38, so leftInner = 37 — 1 col
		// wider than the old leftInner=36 because the left border is gone.
		{"normal split", 100, 0.38, 37},
		// ratio=0.05 → dividerCenter=5 → leftInner candidate 4 → clamped.
		{"clamp too small", 100, 0.05, minPanelInnerCols},
		// ratio=0.95 → leftInner candidate 94, hi = 100-3-14 = 83.
		{"clamp too large", 100, 0.95, 100 - dividerWidth - minPanelInnerCols},
		// At ratio=0.15 the floor catches: dividerCenter=15 → li=14.
		{"at floor", 100, 0.15, minPanelInnerCols},
		// Half-cent rounding: 100*0.380=38.0 → +0.5 → int=38.
		{"rounds to nearest", 100, 0.380, 37},
		// Exact minimum total width: 14 + dividerWidth + 14 = 31.
		{"min total width", 31, 0.55, minPanelInnerCols},
		// Below minimum total: best-effort degenerate, no panic.
		{"degenerate tiny", 20, 0.50, minPanelInnerCols},
	}
	for _, c := range cases {
		m := model{width: c.width, leftRatio: c.ratio, tel: noopRecorder{}}
		if got := m.leftInnerWidth(); got != c.want {
			t.Errorf("%s: leftInnerWidth(width=%d, ratio=%.3f) = %d, want %d",
				c.name, c.width, c.ratio, got, c.want)
		}
	}
}

// TestDividerDrag walks the full press→motion→release cycle and asserts the
// divider follows the cursor and the drag state flips correctly. Under the
// new semantics a press at column x snaps the glyph to col x (so leftInner =
// x - 1), and the entire 3-col divider band is a drag-start hit-zone.
func TestDividerDrag(t *testing.T) {
	m := model{width: 100, height: 30, leftRatio: 0.38, tel: noopRecorder{}}
	g := m.layout()
	if g.dividerStart != 37 {
		t.Fatalf("setup: dividerStart = %d, want 37", g.dividerStart)
	}
	if g.leftInner != 37 {
		t.Fatalf("setup: leftInner = %d, want 37", g.leftInner)
	}

	step := func(msg tea.MouseMsg) {
		nm, _ := m.handleMouse(msg)
		m = nm.(model)
	}

	// Press on the divider's pad-left col (dividerStart). Glyph snaps to col
	// 37 → dividerCenter=37 → leftInner=36.
	step(tea.MouseClickMsg{X: 37, Y: 5, Button: tea.MouseLeft})
	if !m.dragging {
		t.Fatal("press on divider did not start a drag")
	}
	if got := m.leftInnerWidth(); got != 36 {
		t.Errorf("after press at dividerStart: leftInner = %d, want 36", got)
	}

	// Drag right: motion to x=50 → dividerCenter=50 → leftInner=49.
	step(tea.MouseMotionMsg{X: 50, Y: 5, Button: tea.MouseLeft})
	if got := m.leftInnerWidth(); got != 49 {
		t.Errorf("after drag right: leftInner = %d, want 49", got)
	}

	// Drag far left past the clamp: motion to x=2 → leftInner clamped to floor.
	step(tea.MouseMotionMsg{X: 2, Y: 5, Button: tea.MouseLeft})
	if got := m.leftInnerWidth(); got != minPanelInnerCols {
		t.Errorf("after drag past min: leftInner = %d, want %d", got, minPanelInnerCols)
	}

	// Release ends the drag.
	step(tea.MouseReleaseMsg{X: 2, Y: 5, Button: tea.MouseLeft})
	if m.dragging {
		t.Fatal("release did not end the drag")
	}

	// Motion after release must NOT move the divider.
	ratioBefore := m.leftRatio
	step(tea.MouseMotionMsg{X: 70, Y: 5, Button: tea.MouseLeft})
	if m.leftRatio != ratioBefore {
		t.Errorf("motion after release moved divider: ratio %.3f → %.3f", ratioBefore, m.leftRatio)
	}
}

// TestViewFillsHeight locks the invariant that View renders exactly m.height
// rows — no blank "spare" line at the bottom, so the UI sits flush against the
// terminal floor beside the agent pane. Regression guard for the old layout()
// that subtracted an extra spare row and left a gap.
func TestViewFillsHeight(t *testing.T) {
	for _, h := range []int{10, 20, 30, 50} {
		m := newModel(".", noopRecorder{})
		nm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: h})
		m = nm.(model)
		lines := strings.Count(m.View().Content, "\n") + 1
		if lines != h {
			t.Errorf("height=%d: View rendered %d lines, want %d (gap=%d)", h, lines, h, h-lines)
		}
	}
}

// TestPressOffDividerDoesNotDrag guards that ordinary clicks in the list area
// are not swallowed by the divider hit-test.
func TestPressOffDividerDoesNotDrag(t *testing.T) {
	m := model{width: 100, height: 30, leftRatio: 0.38, tel: noopRecorder{}}
	nm, _ := m.handleMouse(tea.MouseClickMsg{X: 10, Y: 5, Button: tea.MouseLeft})
	if nm.(model).dragging {
		t.Error("press in list area wrongly started a divider drag")
	}
}

// ----------------------------------------------------------------------------
// Responsive layout — Y-axis mirror of the X-axis tests above. PRD §7 T12.
// ----------------------------------------------------------------------------

// TestTopInnerHeightClamp mirrors TestLeftInnerWidthClamp for the Y axis.
// topInnerHeight stores topRatio as the row of the horizontal divider glyph
// (dividerYStart = round(bodyH * topRatio)), then clamps so each pane keeps
// at least minPanelInnerRows of content and the divider gets its dividerHeight
// row. Degenerate-short terminals fall back to the floor instead of panicking.
func TestTopInnerHeightClamp(t *testing.T) {
	cases := []struct {
		name  string
		bodyH int
		ratio float64
		want  int
	}{
		// Default 1-col split at a 30-row terminal (bodyH=29, status=1):
		// round(29*0.33)=10 → no clamp.
		{"default 33% at h=30", 29, 0.33, 10},
		// Smaller body: 23 rows. round(23*0.33)=8 → no clamp.
		{"33% at bodyH=23", 23, 0.33, 8},
		// Very small ratio → floor catches.
		{"clamp floor", 30, 0.05, minPanelInnerRows},
		// Very large ratio → ceiling catches: hi = 30 - 1 - 4 = 25.
		{"clamp ceiling", 30, 0.95, 30 - dividerHeight - minPanelInnerRows},
		// Exact minimum total height: 2*minPanelInnerRows + dividerHeight = 9.
		{"min total height", 9, 0.55, minPanelInnerRows},
		// Below minimum total: best-effort degenerate, no panic.
		{"degenerate tiny", 5, 0.5, minPanelInnerRows},
	}
	for _, c := range cases {
		if got := topInnerHeight(c.bodyH, c.ratio); got != c.want {
			t.Errorf("%s: topInnerHeight(bodyH=%d, ratio=%.3f) = %d, want %d",
				c.name, c.bodyH, c.ratio, got, c.want)
		}
	}
}

// TestLayoutBoundariesAroundBreakpoint pins the responsive trigger to the
// exact widthBreakpoint constant: m.width < widthBreakpoint → vertical;
// m.width >= widthBreakpoint → horizontal. PRD §6 checklist 9: confirm `<`
// not `≤` so width=widthBreakpoint stays horizontal.
func TestLayoutBoundariesAroundBreakpoint(t *testing.T) {
	cases := []struct {
		width        int
		wantVertical bool
	}{
		{widthBreakpoint - 1, true},  // 79 → vertical
		{widthBreakpoint, false},     // 80 → horizontal (strict <)
		{widthBreakpoint + 1, false}, // 81 → horizontal
	}
	for _, c := range cases {
		m := model{width: c.width, height: 30, leftRatio: 0.38, topRatio: 0.33, tel: noopRecorder{}}
		g := m.layout()
		if g.vertical != c.wantVertical {
			t.Errorf("width=%d: g.vertical = %v, want %v (widthBreakpoint=%d, < is strict)",
				c.width, g.vertical, c.wantVertical, widthBreakpoint)
		}
	}
}

// TestPreviewBodyWidthBranch covers FR7's reflow trigger: previewBodyWidth
// returns g.rightInner in horizontal mode and g.leftInner (= m.width) in
// vertical mode, so mode flip changes the value and syncPreview re-dispatches.
func TestPreviewBodyWidthBranch(t *testing.T) {
	// Horizontal: width=100, leftRatio=0.38 → leftInner=37, rightInner=60.
	mH := model{width: 100, height: 30, leftRatio: 0.38, topRatio: 0.33, tel: noopRecorder{}}
	if got := mH.previewBodyWidth(); got != 60 {
		t.Errorf("horizontal previewBodyWidth = %d, want 60 (= g.rightInner)", got)
	}
	// Vertical: width=70 → leftInner=70 (full width, borderless).
	mV := model{width: 70, height: 30, leftRatio: 0.38, topRatio: 0.33, tel: noopRecorder{}}
	if got := mV.previewBodyWidth(); got != 70 {
		t.Errorf("vertical previewBodyWidth = %d, want 70 (= m.width)", got)
	}
}

// TestPreviewScrollBranch covers the preview pane's row budget per mode:
// horizontal uses g.bodyH (whole body), vertical uses g.bottomInner (only the
// preview pane's rows). previewScroll feeds renderPreview / scrollPreview /
// previewClick, so a wrong branch silently breaks scroll math + click maps.
func TestPreviewScrollBranch(t *testing.T) {
	// Horizontal: bodyH = height-1-headerH = 28.
	mH := model{width: 100, height: 30, leftRatio: 0.38, topRatio: 0.33, tel: noopRecorder{}}
	wantH := mH.layout().bodyH
	if _, h := mH.previewScroll(); h != wantH {
		t.Errorf("horizontal previewScroll bodyH = %d, want %d (= g.bodyH)", h, wantH)
	}
	// Vertical: previewScroll returns g.bottomInner (the preview pane's own rows).
	mV := model{width: 70, height: 30, leftRatio: 0.38, topRatio: 0.33, tel: noopRecorder{}}
	wantV := mV.layout().bottomInner
	if _, h := mV.previewScroll(); h != wantV {
		t.Errorf("vertical previewScroll bodyH = %d, want %d (= g.bottomInner)", h, wantV)
	}
}

// TestStateRatiosPersistAcrossModeFlips checks FR5: dragging the X divider in
// 2-col mode, then narrowing through widthBreakpoint, then widening back,
// returns to the same leftRatio (and topRatio set while vertical persists
// when we come back to vertical again).
func TestStateRatiosPersistAcrossModeFlips(t *testing.T) {
	m := model{width: 100, height: 30, leftRatio: 0.38, topRatio: 0.33, tel: noopRecorder{}}

	// User drags X-divider in 2-col to leftRatio=0.6.
	m.setLeftFromX(60)
	if m.leftRatio != 0.6 {
		t.Fatalf("setup: setLeftFromX(60)/width=100 → leftRatio = %.3f, want 0.6", m.leftRatio)
	}

	// Narrow below threshold — mode flip to vertical.
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 70, Height: 24})
	m = nm.(model)
	if !m.lastVertical {
		t.Fatalf("after shrink to 70: lastVertical = false, want true")
	}

	// User drags Y-divider in 1-col. setTopFromY maps the screen row 12 back
	// through the header offset: topRatio = (12-headerH) / (height-1-headerH).
	m.setTopFromY(12)
	wantTop := float64(12-headerH) / float64(24-1-headerH)
	if m.topRatio != wantTop {
		t.Fatalf("setup: setTopFromY(12) → topRatio = %.6f, want %.6f", m.topRatio, wantTop)
	}

	// Widen back above threshold — mode flip to horizontal.
	nm, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = nm.(model)

	if m.leftRatio != 0.6 {
		t.Errorf("after widen back to 100: leftRatio = %.3f, want 0.6 (persist across mode flip)", m.leftRatio)
	}
	if m.topRatio != wantTop {
		t.Errorf("after widen back to 100: topRatio = %.6f, want %.6f (persist across mode flip)", m.topRatio, wantTop)
	}

	// Narrow once more — same topRatio still in effect.
	nm, _ = m.Update(tea.WindowSizeMsg{Width: 70, Height: 24})
	m = nm.(model)
	if m.topRatio != wantTop {
		t.Errorf("on second shrink: topRatio = %.6f, want %.6f", m.topRatio, wantTop)
	}
}

// TestDragMidFlipClearsDragging covers FR8: an in-flight drag is cancelled
// when WindowSizeMsg crosses widthBreakpoint, because the divider's axis is
// about to swap under the user's finger. Crucially, m.dragging must be
// cleared BEFORE m.lastVertical is updated (PRD §5.11 ordering footgun) so
// the tail syncPreview on the same tick sees the cleaned state.
func TestDragMidFlipClearsDragging(t *testing.T) {
	// Start in horizontal mode, simulating a drag in progress.
	m := model{
		width: 100, height: 30, leftRatio: 0.38, topRatio: 0.33,
		dragging: true, lastVertical: false,
		tel: noopRecorder{},
	}

	nm, _ := m.Update(tea.WindowSizeMsg{Width: 70, Height: 24})
	m = nm.(model)

	if m.dragging {
		t.Error("after shrink below widthBreakpoint mid-drag: dragging = true, want false")
	}
	if !m.lastVertical {
		t.Errorf("after shrink below widthBreakpoint: lastVertical = false, want true")
	}

	// Reverse flip: vertical → horizontal also clears.
	m.dragging = true
	nm, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = nm.(model)
	if m.dragging {
		t.Error("after widen above widthBreakpoint mid-drag: dragging = true, want false")
	}
	if m.lastVertical {
		t.Errorf("after widen above widthBreakpoint: lastVertical = true, want false")
	}
}

// TestResizeWithinSameModeDoesNotFlushDragging guards against an over-eager
// flush: resizing the terminal width while staying on the same side of
// widthBreakpoint must NOT cancel the active drag.
func TestResizeWithinSameModeDoesNotFlushDragging(t *testing.T) {
	// Horizontal 100 → horizontal 120: still horizontal, no flip.
	m := model{
		width: 100, height: 30, leftRatio: 0.38, topRatio: 0.33,
		dragging: true, lastVertical: false,
		tel: noopRecorder{},
	}
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = nm.(model)
	if !m.dragging {
		t.Error("100 → 120 same-mode resize wrongly cleared dragging")
	}

	// Vertical 70 → vertical 60: still vertical, no flip.
	m = model{
		width: 70, height: 24, leftRatio: 0.38, topRatio: 0.33,
		dragging: true, lastVertical: true,
		tel: noopRecorder{},
	}
	nm, _ = m.Update(tea.WindowSizeMsg{Width: 60, Height: 24})
	m = nm.(model)
	if !m.dragging {
		t.Error("70 → 60 same-mode resize wrongly cleared dragging")
	}
}
