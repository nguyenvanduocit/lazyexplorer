package main

// Render-layer tests for the diff preview (prd-preview-diff-view T6/§5.4): the
// diff goes through renderPreview's EXISTING previewScrollable path (no new view
// branch) — its colored ANSI lines survive renderHWindow horizontal slicing
// (nowrap) and the wrapped path, stay column-aligned, and fit the pane width.

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// renderedDiffModel builds a repo with a modified file whose diff has a LONG line
// (wider than the pane) so horizontal windowing is exercised, drives the diff
// render to completion, and returns the sized model focused on the preview.
func renderedDiffModel(t *testing.T, width int) model {
	t.Helper()
	repo := t.TempDir()
	gitExec(t, repo, "init")
	mustWrite(t, repo, "code.txt", "short\nbeta\n")
	gitExec(t, repo, "add", ".")
	gitExec(t, repo, "commit", "-m", "init")
	// Add a very long added line so it overflows a narrow pane (tests hwindow).
	longLine := "this_is_a_very_long_added_line_" + strings.Repeat("x", 200)
	mustWrite(t, repo, "code.txt", "short\nDELTA\n"+longLine+"\n")

	m := modelAt(t, repo, width, 30)
	m.diffOn = true
	m.git.repoRoot = detectRepoRoot(m.root)
	m.gitRootPrefix = repoRelPrefix(m.git.repoRoot, m.root)
	state, _ := collectGitState(m.git.repoRoot, nil)
	mm := applyGitState(t, m, state)
	selectEntry(t, &mm, "code.txt")
	mm.renderNow()
	return mm
}

// applyGitState delivers a snapshot through Update (the async cmd's effect).
func applyGitState(t *testing.T, m model, state gitState) model {
	t.Helper()
	out, _ := m.Update(gitRefreshedMsg{gen: m.gitGen, state: state})
	return out.(model)
}

// TestDiffRendersThroughScrollablePath: a diff preview is rendered through the
// SAME previewScrollable nowrap path code uses — the rendered output keeps the
// colored +/- content (ANSI survives), and never exceeds the pane width.
func TestDiffRendersThroughScrollablePath(t *testing.T) {
	w := 60
	m := renderedDiffModel(t, w)
	if !m.previewIsDiff || !m.previewScrollable {
		t.Fatalf("diff must be scrollable (previewIsDiff=%v scrollable=%v)", m.previewIsDiff, m.previewScrollable)
	}
	out := m.renderPreview(w)
	for i, ln := range strings.Split(out, "\n") {
		if lw := lineWidth(ln); lw > w {
			t.Errorf("rendered diff row %d width %d exceeds pane width %d: %q", i, lw, w, ansi.Strip(ln))
		}
	}
	plain := ansi.Strip(out)
	if !strings.Contains(plain, "+DELTA") {
		t.Errorf("rendered diff must show the +DELTA hunk; got:\n%s", plain)
	}
	// The colored content carries ANSI (preStyled diff) — the row is not bare text.
	if !strings.Contains(out, "\x1b[") {
		t.Errorf("diff rows must carry ANSI color escapes")
	}
}

// TestDiffHWindowSurvivesScroll pins FR8/D9: panning the long diff line right via
// the horizontal window keeps each row within the pane width and the leading-edge
// ‹ indicator shows; the ANSI color is not corrupted by the slice (no dangling
// escape that would blow the measured width past w).
func TestDiffHWindowSurvivesScroll(t *testing.T) {
	w := 40
	m := renderedDiffModel(t, w)
	m.focusPane = focusPreview
	m.previewHScroll = 30 // pan right into the long line
	out := m.renderPreview(w)
	for i, ln := range strings.Split(out, "\n") {
		if lw := lineWidth(ln); lw > w {
			t.Errorf("panned diff row %d width %d exceeds pane width %d", i, lw, w)
		}
	}
	// At a non-zero hscroll the left edge indicator ‹ is present (something is cut
	// off to the left).
	if !strings.Contains(ansi.Strip(out), "‹") {
		t.Errorf("panned diff should show the left-edge ‹ indicator; got:\n%s", ansi.Strip(out))
	}
}
