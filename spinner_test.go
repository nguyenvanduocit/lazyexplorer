package main

// Tests for the footer render indicator (docs/prd-preview-render-stability.md): a
// right-edge braille spinner in a fixed 2-col slot, animated only while a preview
// render is in flight. The headline guarantee is that the indicator never shifts or
// clips the hints — the bug-footer-flicker regression.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// TestFooterDoesNotShiftWhenRendering is the bug-footer-flicker regression guard.
// Before the fix the indicator was prepended, shoving the hints right by 13 columns
// (and clipping the tail). With the right-edge reserved slot, the hint text is fit to
// the same width regardless of render state, so a stable hint sits at the SAME column
// whether or not a render is in flight, and the hint region is byte-identical.
func TestFooterDoesNotShiftWhenRendering(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, "a.md", "# A\n\nbody\n")
	m := modelAt(t, root, 120, 30) // focusList by default

	m.pendingWidth = 0
	idle := ansi.Strip(m.renderStatus())

	m.pendingWidth = 100 // simulate a render in flight (spinnerFrame still 0)
	rendering := ansi.Strip(m.renderStatus())

	// No horizontal shift: a stable hint sits at the same column in both frames.
	const probe = "switch focus"
	i0, i1 := strings.Index(idle, probe), strings.Index(rendering, probe)
	if i0 < 0 || i1 < 0 {
		t.Fatalf("probe %q missing:\n idle=%q\n rend=%q", probe, idle, rendering)
	}
	if i0 != i1 {
		t.Errorf("hint shifted: %q at col %d idle vs %d rendering", probe, i0, i1)
	}

	// No extra clip: the hint region (everything but the 2-col right slot) matches.
	spin := spinnerFrames[m.spinnerFrame%len(spinnerFrames)]
	idleHint := strings.TrimRight(idle, " ")
	renderHint := strings.TrimRight(strings.ReplaceAll(rendering, spin, ""), " ")
	if idleHint != renderHint {
		t.Errorf("hint region differs idle vs rendering:\n idle=%q\n rend=%q", idleHint, renderHint)
	}
}

// TestSpinnerShownOnlyWhileRendering — the spinner glyph appears in the footer when
// a render is in flight and is absent when idle (FR1/FR4 visual side).
func TestSpinnerShownOnlyWhileRendering(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, "a.md", "# A\n\nbody\n")
	m := modelAt(t, root, 120, 30)
	glyph := spinnerFrames[0]

	m.pendingWidth = 0
	if strings.Contains(m.renderStatus(), glyph) {
		t.Errorf("idle footer must not show a spinner glyph %q", glyph)
	}
	m.pendingWidth = 80
	if !strings.Contains(m.renderStatus(), glyph) {
		t.Errorf("in-flight footer should show the spinner glyph %q", glyph)
	}
}

// TestSpinnerTickAdvancesWhileRendering — spinnerTickMsg advances the frame and
// reschedules while a render is in flight (pendingWidth>0), then stops + resets once
// the render lands (FR3). The stop branch must NOT reschedule, so an idle UI is never
// woken at the spinner rate.
func TestSpinnerTickAdvancesWhileRendering(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, "a.md", "# A\n\nbody\n")
	m := modelAt(t, root, 120, 30)
	m.pendingWidth, m.spinning, m.spinnerFrame = 80, true, 0

	var tm tea.Model = m
	tm, cmd := tm.Update(spinnerTickMsg{})
	m = tm.(model)
	if m.spinnerFrame != 1 {
		t.Errorf("spinnerFrame = %d, want 1 after a tick while rendering", m.spinnerFrame)
	}
	if cmd == nil {
		t.Error("a spinner tick while rendering must reschedule (non-nil Cmd)")
	}
	if !m.spinning {
		t.Error("spinning must stay true while rendering")
	}

	// Render landed → next tick stops the loop and resets the frame.
	m.pendingWidth = 0
	tm = m
	tm, cmd = tm.Update(spinnerTickMsg{})
	m = tm.(model)
	if m.spinning {
		t.Error("spinning must clear once the render has landed")
	}
	if m.spinnerFrame != 0 {
		t.Errorf("spinnerFrame must reset to 0 when the loop stops; got %d", m.spinnerFrame)
	}
	if cmd != nil {
		t.Error("a spinner tick after the render landed must NOT reschedule")
	}
}

// TestSpinnerDoesNotStartWhenIdle — a non-render Update (here, a focus toggle on a
// plain-text selection with no renderer) must not kick the spinner loop (FR4).
func TestSpinnerDoesNotStartWhenIdle(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, "a.txt", "plain text\n") // no renderer → no render dispatched
	m := modelAt(t, root, 120, 30)
	if m.spinning {
		t.Fatal("setup: spinning should start false")
	}
	var tm tea.Model = m
	tm, _ = tm.Update(tea.KeyPressMsg{Code: tea.KeyTab}) // toggle focus, no render
	if tm.(model).spinning {
		t.Error("spinner must not start when no render is in flight")
	}
}
