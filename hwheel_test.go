package main

// Horizontal wheel surface for the preview pane: Shift+vertical-wheel remaps to
// horizontal pan, and native horizontal wheel (WheelLeft/WheelRight — trackpad
// two-finger sideways swipe) drives the same axis. Both reuse scrollPreviewH,
// the h/l keyboard primitive (prd-horizontal-scroll-preview FR14). Vertical wheel
// remains untouched (scroll_test.go pins that contract).
//
// scrollableModel (hscroll_test.go) builds a preview-focused model with a 300-col
// line so there is something to pan across. Width 120 forces 2-col side-by-side
// so the position-based over-list / over-preview split is exercised.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// hwheelModel returns a horizontal-layout scrollable preview model plus the X
// coordinates of a point inside the preview pane and inside the list pane.
func hwheelModel(t *testing.T) (model, int, int) {
	t.Helper()
	m := scrollableModel(t, 120, 30)
	g := m.layout()
	if g.vertical {
		t.Fatalf("setup: width=120 should be horizontal, got vertical")
	}
	previewX := g.dividerStart + dividerWidth + 3 // well inside the preview pane
	listX := 3                                    // well inside the list pane (left of divider)
	return m, previewX, listX
}

// TestShiftWheelPansPreviewHorizontally: Shift+WheelDown pans right (+col),
// Shift+WheelUp pans left (−col), clamped at the left edge. previewTop must not
// move — Shift converts the gesture to the horizontal axis.
func TestShiftWheelPansPreviewHorizontally(t *testing.T) {
	m, previewX, _ := hwheelModel(t)
	top0 := m.previewTop

	nm, _ := m.handleMouse(tea.MouseWheelMsg{X: previewX, Y: 5, Button: tea.MouseWheelDown, Mod: tea.ModShift})
	m = nm.(model)
	if m.previewHScroll != previewColStep {
		t.Errorf("shift+wheel down: hscroll = %d, want %d (pan right)", m.previewHScroll, previewColStep)
	}
	if m.previewTop != top0 {
		t.Errorf("shift+wheel down: previewTop = %d, want %d (vertical must not move)", m.previewTop, top0)
	}

	nm, _ = m.handleMouse(tea.MouseWheelMsg{X: previewX, Y: 5, Button: tea.MouseWheelUp, Mod: tea.ModShift})
	m = nm.(model)
	if m.previewHScroll != 0 {
		t.Errorf("shift+wheel up: hscroll = %d, want 0 (pan left back)", m.previewHScroll)
	}

	// Clamp at the left edge: another shift+up at 0 is a no-op.
	nm, _ = m.handleMouse(tea.MouseWheelMsg{X: previewX, Y: 5, Button: tea.MouseWheelUp, Mod: tea.ModShift})
	m = nm.(model)
	if m.previewHScroll != 0 {
		t.Errorf("shift+wheel up at left edge: hscroll = %d, want 0 (clamped)", m.previewHScroll)
	}
}

// TestNativeHWheelPansPreview: WheelLeft pans left, WheelRight pans right, with
// no Shift held. These button events were dropped entirely before FR14.
func TestNativeHWheelPansPreview(t *testing.T) {
	m, previewX, _ := hwheelModel(t)

	nm, _ := m.handleMouse(tea.MouseWheelMsg{X: previewX, Y: 5, Button: tea.MouseWheelRight})
	m = nm.(model)
	if m.previewHScroll != previewColStep {
		t.Errorf("wheel right: hscroll = %d, want %d (pan right)", m.previewHScroll, previewColStep)
	}

	nm, _ = m.handleMouse(tea.MouseWheelMsg{X: previewX, Y: 5, Button: tea.MouseWheelLeft})
	m = nm.(model)
	if m.previewHScroll != 0 {
		t.Errorf("wheel left: hscroll = %d, want 0 (pan left)", m.previewHScroll)
	}
}

// TestShiftWheelOverListIgnoresModifier: over the LIST pane, Shift is ignored —
// the list has no horizontal axis, so the gesture stays a normal vertical list
// scroll and never leaks into previewHScroll.
func TestShiftWheelOverListIgnoresModifier(t *testing.T) {
	m, _, listX := hwheelModel(t)
	// Give the list something to scroll so we can prove vertical motion happened.
	m.entries = make([]entry, 100)
	for i := range m.entries {
		m.entries[i] = entry{name: "e"}
	}
	m.listTop = 10

	nm, _ := m.handleMouse(tea.MouseWheelMsg{X: listX, Y: 5, Button: tea.MouseWheelDown, Mod: tea.ModShift})
	m = nm.(model)
	if m.previewHScroll != 0 {
		t.Errorf("shift+wheel over list: previewHScroll = %d, want 0 (must not pan preview)", m.previewHScroll)
	}
	if m.listTop != 11 {
		t.Errorf("shift+wheel over list: listTop = %d, want 11 (normal vertical list scroll)", m.listTop)
	}
}

// TestNativeHWheelOverListNoop: a native horizontal wheel over the list pane is
// a no-op (the list cannot pan horizontally) and must not touch the preview.
func TestNativeHWheelOverListNoop(t *testing.T) {
	m, _, listX := hwheelModel(t)
	m.entries = make([]entry, 100)
	for i := range m.entries {
		m.entries[i] = entry{name: "e"}
	}
	m.listTop = 10

	nm, _ := m.handleMouse(tea.MouseWheelMsg{X: listX, Y: 5, Button: tea.MouseWheelRight})
	m = nm.(model)
	if m.previewHScroll != 0 {
		t.Errorf("wheel right over list: previewHScroll = %d, want 0", m.previewHScroll)
	}
	if m.listTop != 10 {
		t.Errorf("wheel right over list: listTop = %d, want 10 (no-op)", m.listTop)
	}
}

// TestShiftWheelNoopInWrapMode: in wrap mode there is no horizontal axis, so
// Shift+wheel over the preview is a no-op (scrollPreviewH gates on previewWrap).
func TestShiftWheelNoopInWrapMode(t *testing.T) {
	m, previewX, _ := hwheelModel(t)
	m.previewWrap = true
	m.reflowPreview(m.previewBodyWidth())

	nm, _ := m.handleMouse(tea.MouseWheelMsg{X: previewX, Y: 5, Button: tea.MouseWheelDown, Mod: tea.ModShift})
	m = nm.(model)
	if m.previewHScroll != 0 {
		t.Errorf("shift+wheel in wrap mode: hscroll = %d, want 0 (no horizontal axis when wrapped)", m.previewHScroll)
	}
}

// TestShiftWheelPansPreviewInVerticalLayout confirms the horizontal-pan routing
// also fires in stacked (vertical) layout, where the preview sits BELOW the
// divider and overList is decided by Y, not X. Guards that the axis-aware
// overList split feeds the new Shift branch correctly in both layouts.
func TestShiftWheelPansPreviewInVerticalLayout(t *testing.T) {
	m := modelAt(t, t.TempDir(), 70, 24) // narrow → stacked layout
	g := m.layout()
	if !g.vertical {
		t.Fatalf("setup: width=70 should be vertical (stacked), got horizontal")
	}
	m.focusPane = focusPreview
	m.previewScrollable = true
	m.previewIsDir = false
	m.preview = []string{strings.Repeat("x", 300)}
	m.previewWrap = false
	m.reflowPreview(m.previewBodyWidth())

	previewY := g.dividerYStart + 2 // in the preview region, below the divider row
	nm, _ := m.handleMouse(tea.MouseWheelMsg{X: 3, Y: previewY, Button: tea.MouseWheelDown, Mod: tea.ModShift})
	m = nm.(model)
	if m.previewHScroll != previewColStep {
		t.Errorf("shift+wheel down (vertical layout): hscroll = %d, want %d", m.previewHScroll, previewColStep)
	}
}

// TestPlainWheelStillScrollsVertically guards the regression: an unmodified
// wheel over the preview still moves the vertical viewport, not the horizontal.
func TestPlainWheelStillScrollsVertically(t *testing.T) {
	m, previewX, _ := hwheelModel(t)
	// Enough source lines to move the vertical viewport.
	m.preview = make([]string, 100)
	for i := range m.preview {
		m.preview[i] = strings.Repeat("x", 300)
	}
	m.reflowPreview(m.previewBodyWidth())
	m.previewTop = 10

	nm, _ := m.handleMouse(tea.MouseWheelMsg{X: previewX, Y: 5, Button: tea.MouseWheelDown})
	m = nm.(model)
	if m.previewTop != 11 {
		t.Errorf("plain wheel down: previewTop = %d, want 11 (vertical)", m.previewTop)
	}
	if m.previewHScroll != 0 {
		t.Errorf("plain wheel down: previewHScroll = %d, want 0 (no horizontal without shift)", m.previewHScroll)
	}
}
