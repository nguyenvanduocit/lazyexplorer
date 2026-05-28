package main

// Smooth preview scroll — wheel surface (prd-smooth-preview-scroll.md §6, D1).
//
// One wheel notch over the preview pane moves the viewport exactly ONE line
// (previewLineStep), not the old ±3. This file pins that contract directly in
// 2-col side-by-side mode; the vertical-mode routing is covered separately by
// TestWheelInVerticalRoutesByY in divider_test.go. The fine-step keyboard
// (J/K) and half-page (ctrl+d/u) surfaces ship via prd-pane-focus and are
// tested there.

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestWheelStepIsOneLine drives a wheel notch over the preview pane in
// horizontal layout (width 120 ≥ widthBreakpoint) and asserts previewTop moves
// by exactly previewLineStep (1) per notch, in both directions, clamped at the
// top.
func TestWheelStepIsOneLine(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, "a.txt", "x")

	m := modelAt(t, root, 120, 30) // horizontal: 2-col side-by-side
	g := m.layout()
	if g.vertical {
		t.Fatalf("setup: width=120 should be horizontal, got vertical")
	}
	// A preview longer than the pane so the viewport can actually move.
	long := make([]string, 100)
	for i := range long {
		long[i] = "line"
	}
	m.preview = long
	m.previewIsDir = false
	m.previewTop = 10

	previewX := g.dividerStart + dividerWidth + 3 // well inside the preview pane

	// One notch down → +1 line.
	nm, _ := m.handleMouse(tea.MouseWheelMsg{X: previewX, Y: 5, Button: tea.MouseWheelDown})
	m = nm.(model)
	if m.previewTop != 11 {
		t.Errorf("wheel down one notch: previewTop = %d, want 11 (+previewLineStep)", m.previewTop)
	}

	// One notch up → -1 line.
	nm, _ = m.handleMouse(tea.MouseWheelMsg{X: previewX, Y: 5, Button: tea.MouseWheelUp})
	m = nm.(model)
	if m.previewTop != 10 {
		t.Errorf("wheel up one notch: previewTop = %d, want 10 (-previewLineStep)", m.previewTop)
	}
}

// TestWheelStepClampsAtTop confirms a wheel-up at the very top is a no-op, not
// a negative previewTop (clamp lives in scrollPreview, this just guards the
// smooth-scroll wheel path against regression).
func TestWheelStepClampsAtTop(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, "a.txt", "x")

	m := modelAt(t, root, 120, 30)
	g := m.layout()
	long := make([]string, 100)
	for i := range long {
		long[i] = "line"
	}
	m.preview = long
	m.previewIsDir = false
	m.previewTop = 0

	previewX := g.dividerStart + dividerWidth + 3
	nm, _ := m.handleMouse(tea.MouseWheelMsg{X: previewX, Y: 5, Button: tea.MouseWheelUp})
	m = nm.(model)
	if m.previewTop != 0 {
		t.Errorf("wheel up at top: previewTop = %d, want 0 (clamped)", m.previewTop)
	}
}
