package main

// preview_selection_test.go — TDD for in-app line-visual selection in the preview
// pane (prd-preview-selection): `V` starts a line selection from the top visible
// line, the scroll keys extend it, `y`/Enter copies the range's raw de-colored
// text, mouse drag selects + copies on release. copySelection is the ONE code path
// (keyboard `y`/`enter` AND mouse release) so action.copy_selection records exactly
// once. These assertions are clipboard-AGNOSTIC (writeClipboard fails in CI without
// pbcopy/xclip/wl-copy), so the oracle for WHAT was copied is the telemetry
// fieldRecorder's {lines,bytes} — Record runs BEFORE writeClipboard, so the captured
// values are honest even with no helper. Mirrors copy_content_test.go.

import (
	"strconv"
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// selLines returns the {lines} the model's last action.copy_selection recorded,
// and whether it was recorded at all — the content oracle independent of any OS
// clipboard helper (there is none in CI).
func selLines(rec *fieldRecorder) (int, bool) {
	fields, ok := rec.last("action.copy_selection")
	if !ok {
		return 0, false
	}
	n, _ := fields["lines"].(int)
	return n, true
}

func selBytes(rec *fieldRecorder) (int, bool) {
	fields, ok := rec.last("action.copy_selection")
	if !ok {
		return 0, false
	}
	n, _ := fields["bytes"].(int)
	return n, true
}

// selectionFixture writes a multi-line text file long enough to overflow the
// preview viewport, selects it, and renders so previewScrollable + the visual-line
// cache (previewSrcStart) are populated. Returns the model and the file's lines.
func selectionFixture(t *testing.T, rec *fieldRecorder) (model, []string) {
	t.Helper()
	root := t.TempDir()
	lines := make([]string, 40)
	for i := range lines {
		lines[i] = "line " + strconv.Itoa(i)
	}
	mustWrite(t, root, "long.txt", strings.Join(lines, "\n")+"\n")
	m := modelAt(t, root, 100, 24) // 2-col side-by-side (width >= widthBreakpoint)
	if rec != nil {
		m.tel = rec
	}
	selectEntry(t, &m, "long.txt")
	m.pendingWidth = 0
	m.renderNow() // populate previewScrollable + previewSrcStart for the plain file
	if !m.previewScrollable {
		t.Fatalf("setup: long.txt must be a scrollable plain-text preview")
	}
	return m, plainLines([]byte(strings.Join(lines, "\n") + "\n"))
}

// TestSelectModeKeyBinding pins the new bindings (T1/D7): keyRune('V') matches
// SelectMode and nothing else (V is free, like the G/H/L uppercase precedent);
// CopySelection matches `y` AND `enter` — those overlap Yank (`y`) and OpenEntry
// (`enter`) INTENTIONALLY (the y/enter lane is resolved by updateSelecting gating
// on m.selecting), so this asserts positive matches only, never uniqueness.
func TestSelectModeKeyBinding(t *testing.T) {
	km := defaultKeyMap()
	if !key.Matches(keyRune('V'), km.SelectMode) {
		t.Fatalf("`V` must match the SelectMode binding")
	}
	for name, b := range allKeyBindings(km) {
		if name == "SelectMode" {
			continue
		}
		if key.Matches(keyRune('V'), b) {
			t.Errorf("`V` collides with binding %q — it must be free for SelectMode", name)
		}
	}
	// CopySelection: positive matches only. `y` and `enter` are SHARED with Yank /
	// OpenEntry on purpose (D7) — the mode lane (m.selecting) disambiguates, so no
	// uniqueness loop here.
	if !key.Matches(keyRune('y'), km.CopySelection) {
		t.Errorf("CopySelection must match `y`")
	}
	if !key.Matches(keyEnter(), km.CopySelection) {
		t.Errorf("CopySelection must match `enter`")
	}
}

// TestStartSelection pins FR1/D6: `V` at focusPreview on a scrollable file starts a
// selection anchored at the top visible source line, entering the selecting sub-state.
func TestStartSelection(t *testing.T) {
	m, _ := selectionFixture(t, nil)
	m.focusPane = focusPreview
	nm, _ := m.updateNormal(keyRune('V'))
	sm := nm.(model)
	if !sm.selecting {
		t.Fatalf("`V` at focusPreview on a scrollable file must start selecting")
	}
	if sm.selAnchor != sm.sourceLineAt(sm.previewTop) {
		t.Errorf("anchor = %d, want sourceLineAt(previewTop) = %d", sm.selAnchor, sm.sourceLineAt(sm.previewTop))
	}
	if sm.selCursor != sm.selAnchor {
		t.Errorf("cursor = %d, want it to start at the anchor %d", sm.selCursor, sm.selAnchor)
	}
}

// TestStartSelectionNoOp pins FR5/FR8: `V` is a no-op at focusList, on a
// non-scrollable preview, and while a render is in flight (pendingWidth > 0).
func TestStartSelectionNoOp(t *testing.T) {
	t.Run("focusList", func(t *testing.T) {
		m, _ := selectionFixture(t, nil)
		m.focusPane = focusList
		nm, _ := m.updateNormal(keyRune('V'))
		if nm.(model).selecting {
			t.Errorf("`V` at focusList must NOT start selecting")
		}
	})
	t.Run("pendingWidth in flight", func(t *testing.T) {
		m, _ := selectionFixture(t, nil)
		m.focusPane = focusPreview
		m.pendingWidth = 80 // a render is in flight — refuse to anchor on a doomed buffer
		nm, _ := m.updateNormal(keyRune('V'))
		if nm.(model).selecting {
			t.Errorf("`V` while a render is in flight (pendingWidth>0) must NOT start selecting (FR8)")
		}
	})
	t.Run("non-scrollable markdown", func(t *testing.T) {
		root := t.TempDir()
		mustWrite(t, root, "doc.md", "# Title\n\nbody\n")
		m := modelAt(t, root, 100, 24)
		selectEntry(t, &m, "doc.md")
		m.pendingWidth = 0
		m.renderNow()
		if m.previewScrollable {
			t.Skip("markdown unexpectedly scrollable on this build")
		}
		m.focusPane = focusPreview
		nm, _ := m.updateNormal(keyRune('V'))
		if nm.(model).selecting {
			t.Errorf("`V` on a non-scrollable markdown preview must NOT start selecting (FR5)")
		}
	})
}

// TestSelectCopySingleLine pins FR11: `V` then `y` with no extend copies exactly
// one line — the top visible source line, raw, de-colored.
func TestSelectCopySingleLine(t *testing.T) {
	rec := &fieldRecorder{}
	m, want := selectionFixture(t, rec)
	m.focusPane = focusPreview
	nm, _ := m.updateNormal(keyRune('V'))
	m = nm.(model)
	top := m.selAnchor
	nm, _ = m.updateSelecting(keyRune('y'))
	m = nm.(model)

	n, recorded := selLines(rec)
	if !recorded {
		t.Fatalf("copy must record action.copy_selection; events=%v", rec.names())
	}
	if n != 1 {
		t.Errorf("recorded lines = %d, want 1 (single-line selection)", n)
	}
	b, _ := selBytes(rec)
	wantBytes := len(ansi.Strip(want[top]))
	if b != wantBytes {
		t.Errorf("recorded bytes = %d, want %d (one de-ANSI line)", b, wantBytes)
	}
	if m.selecting {
		t.Errorf("copy must exit the selecting sub-state")
	}
}

// TestSelectCopyMultiLine pins FR2/FR3: extend down a few lines, copy — the recorded
// lines = the inclusive range, bytes = len(join de-ANSI). The copied text is the
// raw lines joined by "\n".
func TestSelectCopyMultiLine(t *testing.T) {
	rec := &fieldRecorder{}
	m, want := selectionFixture(t, rec)
	m.focusPane = focusPreview
	nm, _ := m.updateNormal(keyRune('V'))
	m = nm.(model)
	lo := m.selAnchor
	for i := 0; i < 3; i++ { // extend down 3 lines → 4-line range
		nm, _ = m.updateSelecting(keyRune('j'))
		m = nm.(model)
	}
	hi := m.selCursor
	if hi-lo+1 != 4 {
		t.Fatalf("after 3 downs the range should be 4 lines; lo=%d hi=%d", lo, hi)
	}
	nm, _ = m.updateSelecting(keyRune('y'))

	n, recorded := selLines(rec)
	if !recorded {
		t.Fatalf("copy must record; events=%v", rec.names())
	}
	if n != 4 {
		t.Errorf("recorded lines = %d, want 4", n)
	}
	raw := make([]string, 0, 4)
	for i := lo; i <= hi; i++ {
		raw = append(raw, ansi.Strip(want[i]))
	}
	wantBytes := len(strings.Join(raw, "\n"))
	b, _ := selBytes(rec)
	if b != wantBytes {
		t.Errorf("recorded bytes = %d, want %d", b, wantBytes)
	}
}

// TestSelectCopyRawNotANSI pins FR6/D5: on a syntax-highlighted code file, the
// copied text contains NO escape codes and equals ansi.Strip of the preview lines.
func TestSelectCopyRawNotANSI(t *testing.T) {
	rec := &fieldRecorder{}
	root := t.TempDir()
	src := "package main\n\nfunc main() {\n\tprintln(\"hi\")\n}\n"
	mustWrite(t, root, "x.go", src)
	m := modelAt(t, root, 100, 24)
	m.tel = rec
	selectEntry(t, &m, "x.go")
	m.pendingWidth = 0
	m.renderNow()
	if !m.previewScrollable {
		t.Fatalf("setup: x.go must be a scrollable code preview")
	}
	// Prove the code preview actually carries ANSI (so de-color is meaningful).
	joined := strings.Join(m.preview, "\n")
	if !strings.Contains(joined, "\x1b") {
		t.Skip("code preview carries no ANSI on this build — de-color discriminator weak")
	}
	m.focusPane = focusPreview
	nm, _ := m.updateNormal(keyRune('V'))
	m = nm.(model)
	lo := m.selAnchor
	nm, _ = m.updateSelecting(keyRune('j'))
	m = nm.(model)
	hi := m.selCursor
	m.updateSelecting(keyRune('y'))

	// The recorded byte count must equal the de-ANSI join, never the ANSI length.
	raw := make([]string, 0)
	for i := lo; i <= hi; i++ {
		raw = append(raw, ansi.Strip(m.preview[i]))
	}
	text := strings.Join(raw, "\n")
	if strings.Contains(text, "\x1b") {
		t.Fatalf("de-ANSI strip left escape codes in the copy text")
	}
	b, _ := selBytes(rec)
	if b != len(text) {
		t.Errorf("recorded bytes = %d, want the de-ANSI length %d (not the ANSI preview)", b, len(text))
	}
}

// TestSelectOffViewport pins FR2's scroll-follow + the off-viewport scenario:
// extending past the bottom of the viewport moves previewTop so the cursor's
// visual line stays in view, and the copied range includes every scrolled line.
func TestSelectOffViewport(t *testing.T) {
	rec := &fieldRecorder{}
	m, want := selectionFixture(t, rec)
	m.focusPane = focusPreview
	nm, _ := m.updateNormal(keyRune('V'))
	m = nm.(model)
	lo := m.selAnchor
	_, bodyH := m.previewScroll()
	// Extend well past the viewport.
	for i := 0; i < bodyH+5; i++ {
		nm, _ = m.updateSelecting(keyRune('j'))
		m = nm.(model)
	}
	hi := m.selCursor
	if hi <= lo+bodyH {
		t.Fatalf("cursor did not move past the viewport: lo=%d hi=%d bodyH=%d", lo, hi, bodyH)
	}
	// scrollSelectionIntoView invariant: the cursor's visual line is inside the window.
	top, bodyH2 := m.previewScroll()
	vis := m.visualLineFor(m.selCursor)
	if vis < top || vis >= top+bodyH2 {
		t.Errorf("cursor visual line %d not in window [%d,%d)", vis, top, top+bodyH2)
	}
	nm, _ = m.updateSelecting(keyRune('y'))
	n, _ := selLines(rec)
	if n != hi-lo+1 {
		t.Errorf("recorded lines = %d, want %d (full off-viewport range)", n, hi-lo+1)
	}
	_ = want
}

// TestSelectWrapMode pins checklist item 10's wrap half: with previewWrap on, a
// source line maps to several visual lines, so scrollSelectionIntoView uses
// visualLineFor. Extend past the viewport and assert the cursor's visual line stays
// in the window, and that copy still yields the correct source-line range.
func TestSelectWrapMode(t *testing.T) {
	rec := &fieldRecorder{}
	root := t.TempDir()
	// Lines wide enough to wrap into multiple visual lines at the pane width.
	lines := make([]string, 30)
	for i := range lines {
		lines[i] = "wrap line " + strconv.Itoa(i) + " " + strings.Repeat("x", 90)
	}
	body := strings.Join(lines, "\n") + "\n"
	mustWrite(t, root, "wide.txt", body)
	m := modelAt(t, root, 100, 24)
	m.tel = rec
	selectEntry(t, &m, "wide.txt")
	m.pendingWidth = 0
	m.renderNow()
	if !m.previewScrollable {
		t.Fatalf("setup: wide.txt must be scrollable")
	}
	m.toggleWrap()
	if !m.previewWrap {
		t.Fatalf("setup: wrap must be on")
	}
	if len(m.previewDisplay) <= len(m.preview) {
		t.Fatalf("setup: wrap must expand lines (display=%d src=%d)", len(m.previewDisplay), len(m.preview))
	}

	m.focusPane = focusPreview
	nm, _ := m.updateNormal(keyRune('V'))
	m = nm.(model)
	lo := m.selAnchor
	_, bodyH := m.previewScroll()
	for i := 0; i < bodyH+3; i++ { // extend well past the viewport in source lines
		nm, _ = m.updateSelecting(keyRune('j'))
		m = nm.(model)
	}
	hi := m.selCursor
	// scrollSelectionIntoView invariant (wrap): cursor's VISUAL line is in the window.
	top, bodyH2 := m.previewScroll()
	vis := m.visualLineFor(m.selCursor)
	if vis < top || vis >= top+bodyH2 {
		t.Errorf("wrap: cursor visual line %d not in window [%d,%d)", vis, top, top+bodyH2)
	}
	nm, _ = m.updateSelecting(keyRune('y'))
	n, recorded := selLines(rec)
	if !recorded {
		t.Fatalf("wrap: copy must record; events=%v", rec.names())
	}
	if n != hi-lo+1 {
		t.Errorf("wrap: recorded lines = %d, want %d (source-line range, not visual)", n, hi-lo+1)
	}
}

// TestSelectHighlightFullWidth pins D12: selected rows render a FULL-WIDTH highlight
// block (selectionStyle background padded to the pane width), so the block is a clean
// rect, not a ragged bar only as wide as the text.
func TestSelectHighlightFullWidth(t *testing.T) {
	m, _ := selectionFixture(t, nil)
	m.focusPane = focusPreview
	nm, _ := m.updateNormal(keyRune('V'))
	m = nm.(model)
	nm, _ = m.updateSelecting(keyRune('j'))
	m = nm.(model)
	w := m.previewBodyWidth()
	raw := m.renderPreview(w)
	hl := 0
	for _, ln := range strings.Split(raw, "\n") {
		if !strings.Contains(ln, "\x1b") || !strings.Contains(ansi.Strip(ln), "line ") {
			continue
		}
		hl++
		if got := lipgloss.Width(ln); got != w {
			t.Errorf("selected row display width = %d, want full pane width %d (ragged highlight)", got, w)
		}
	}
	if hl < 2 {
		t.Errorf("expected >=2 highlighted rows, got %d", hl)
	}
}

// TestSelectCancel pins FR4: `esc` (and `V`) cancel without copying; a subsequent
// `esc` returns focus to the list. `Tab` cancels and switches focus.
func TestSelectCancel(t *testing.T) {
	t.Run("esc cancels, does not copy, then esc returns to list", func(t *testing.T) {
		rec := &fieldRecorder{}
		m, _ := selectionFixture(t, rec)
		m.focusPane = focusPreview
		nm, _ := m.updateNormal(keyRune('V'))
		m = nm.(model)
		nm, _ = m.updateSelecting(keyEsc())
		m = nm.(model)
		if m.selecting {
			t.Fatalf("esc must cancel selecting")
		}
		if _, recorded := selLines(rec); recorded {
			t.Errorf("a cancelled selection must NOT record action.copy_selection")
		}
		if m.focusPane != focusPreview {
			t.Errorf("after cancel, focus stays on preview until the next esc")
		}
		// esc again → back to list (prd-pane-focus unchanged).
		nm, _ = m.updateNormal(keyEsc())
		if nm.(model).focusPane != focusList {
			t.Errorf("second esc must return focus to the list")
		}
	})
	t.Run("V cancels", func(t *testing.T) {
		m, _ := selectionFixture(t, nil)
		m.focusPane = focusPreview
		nm, _ := m.updateNormal(keyRune('V'))
		m = nm.(model)
		nm, _ = m.updateSelecting(keyRune('V'))
		if nm.(model).selecting {
			t.Errorf("`V` while selecting must cancel")
		}
	})
	t.Run("Tab cancels and switches focus to list", func(t *testing.T) {
		rec := &fieldRecorder{}
		m, _ := selectionFixture(t, rec)
		m.focusPane = focusPreview
		nm, _ := m.updateNormal(keyRune('V'))
		m = nm.(model)
		nm, _ = m.updateSelecting(keyTab())
		m = nm.(model)
		if m.selecting {
			t.Errorf("Tab must cancel selecting")
		}
		if m.focusPane != focusList {
			t.Errorf("Tab must switch focus to the list")
		}
		if _, recorded := selLines(rec); recorded {
			t.Errorf("Tab cancel must NOT copy")
		}
	})
}

// TestSelectRightDoesNotCopy pins FR12/D7: `l`/`right` while selecting is a no-op —
// it must NOT copy and must NOT exit. Only `y`/`enter` copy.
func TestSelectRightDoesNotCopy(t *testing.T) {
	for _, k := range []struct {
		name string
		key  tea.KeyPressMsg
	}{
		{"l", keyRune('l')},
		{"right", tea.KeyPressMsg{Code: tea.KeyRight}},
	} {
		t.Run(k.name, func(t *testing.T) {
			rec := &fieldRecorder{}
			m, _ := selectionFixture(t, rec)
			m.focusPane = focusPreview
			nm, _ := m.updateNormal(keyRune('V'))
			m = nm.(model)
			nm, _ = m.updateSelecting(k.key)
			m = nm.(model)
			if !m.selecting {
				t.Errorf("%s while selecting must NOT exit the selecting sub-state", k.name)
			}
			if _, recorded := selLines(rec); recorded {
				t.Errorf("%s while selecting must NOT copy (FR12)", k.name)
			}
		})
	}
}

// TestSelectTelemetryOnce pins FR9: one copy records action.copy_selection exactly once.
func TestSelectTelemetryOnce(t *testing.T) {
	rec := &fieldRecorder{}
	m, _ := selectionFixture(t, rec)
	m.focusPane = focusPreview
	nm, _ := m.updateNormal(keyRune('V'))
	m = nm.(model)
	m.updateSelecting(keyRune('y'))
	count := 0
	for _, n := range rec.names() {
		if n == "action.copy_selection" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("action.copy_selection recorded %d times, want exactly 1 (D9/D10)", count)
	}
}

// TestApplyPreviewCancelsSelection pins the CRITICAL race (T2b/D11/FR7): when an
// async render lands in applyPreview during a selection — on BOTH the success and
// error paths — m.selecting is cleared so copySelection can never grab a stale
// range on the new buffer. The headline case is a diff: the placeholder is the
// whole file, the async diff is a few hunk lines, so a leftover selection would
// silently copy the wrong lines. Set the selection fields DIRECTLY (startSelection
// refuses while pendingWidth>0), then land a gen-matching render.
func TestApplyPreviewCancelsSelection(t *testing.T) {
	t.Run("success path clears selecting", func(t *testing.T) {
		m, _ := selectionFixture(t, nil)
		m.selecting = true
		m.selAnchor = 0
		m.selCursor = 20 // anchored deep into the long placeholder
		// A gen-matching success render lands and reassigns m.preview to a short buffer.
		m.applyPreview(previewRenderedMsg{gen: m.renderGen, width: m.previewBodyWidth(), lines: []string{"a", "b"}, preStyled: false, err: nil})
		if m.selecting {
			t.Errorf("applyPreview success path must clear selecting (D11) — a stale range on the new buffer would copy wrong")
		}
	})
	t.Run("error path clears selecting", func(t *testing.T) {
		m, _ := selectionFixture(t, nil)
		m.selecting = true
		m.selAnchor = 0
		m.selCursor = 20
		m.applyPreview(previewRenderedMsg{gen: m.renderGen, width: m.previewBodyWidth(), err: errTestRender})
		if m.selecting {
			t.Errorf("applyPreview error path must clear selecting (D11)")
		}
	})
}

// TestResizeCancelsSelection pins FR7 END-TO-END: a width change re-renders a
// width-dependent (code) preview, and the landing render — driven through the REAL
// syncPreview→applyPreview pipeline (not a hand-built message) — cancels the
// selection. A plain-text preview is width-independent (srcPath=="", no re-render),
// so this uses a code file where a resize genuinely re-renders.
func TestResizeCancelsSelection(t *testing.T) {
	root := t.TempDir()
	var b strings.Builder
	for i := 0; i < 40; i++ {
		b.WriteString("func f" + strconv.Itoa(i) + "() int { return " + strconv.Itoa(i) + " }\n")
	}
	mustWrite(t, root, "code.go", b.String())
	m := modelAt(t, root, 100, 24) // 2-col
	selectEntry(t, &m, "code.go")
	m.pendingWidth = 0
	m.renderNow() // land the code render at width 100 → srcPath + srcWidth set
	if !m.previewScrollable || m.srcPath == "" {
		t.Fatalf("setup: code.go must be a scrollable code preview with a renderer (srcPath=%q)", m.srcPath)
	}
	m.focusPane = focusPreview
	m.startSelection()
	m.moveSelection(5)
	if !m.selecting {
		t.Fatalf("setup: selection did not start")
	}

	// Real resize through the WindowSizeMsg path. The resize itself does not touch
	// m.preview (the selection is still active right after it); it makes srcWidth
	// stale, so the next render dispatch re-renders at the new width. Drive that
	// render to completion the way the program loop would — it lands in applyPreview,
	// which cancels the selection (D11/FR7).
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 130, Height: 24})
	m = tm.(model)
	m.pendingWidth = 0
	m.renderNow()

	if m.selecting {
		t.Errorf("a resize that re-renders the preview must cancel the selection (FR7/D11)")
	}
}

// TestRefreshPreviewResetsSelection pins the reset hygiene (FR8): refreshPreview
// (cursor move) clears selecting.
func TestRefreshPreviewResetsSelection(t *testing.T) {
	m, _ := selectionFixture(t, nil)
	m.selecting = true
	m.selAnchor = 1
	m.selCursor = 3
	m.refreshPreview()
	if m.selecting {
		t.Errorf("refreshPreview must reset selecting (reset hygiene, FR8)")
	}
}

// TestPollGateDoesNotResync pins FR8: while selecting, a poll tick must NOT
// syncFromDisk (which would re-read/re-render the buffer and shift indices).
func TestPollGateDoesNotResync(t *testing.T) {
	m, _ := selectionFixture(t, nil)
	m.selecting = true
	before := strings.Join(m.preview, "\n")
	var tm tea.Model = m
	tm, _ = tm.Update(tickMsg{})
	after := strings.Join(tm.(model).preview, "\n")
	if before != after {
		t.Errorf("poll tick while selecting must not re-sync the buffer (FR8 gate)")
	}
	if !tm.(model).selecting {
		t.Errorf("a poll tick must not cancel a selection")
	}
}

// --- mouse drag-to-select (D13 / FR13-16) ----------------------------------

// previewPressXY returns a screen X,Y that lands inside the preview pane for the
// fixture's 2-col layout (right pane), at the given preview body row.
func previewPressXY(m model, bodyRow int) (int, int) {
	g := m.layout()
	x := g.dividerStart + dividerWidth + 1 // a column inside the right (preview) pane
	y := g.previewFirstRow + bodyRow
	return x, y
}

// TestMouseDragSelectsAndCopies pins FR13: press in the preview, drag across lines,
// release → the dragged range is copied, recorded once.
func TestMouseDragSelectsAndCopies(t *testing.T) {
	rec := &fieldRecorder{}
	m, _ := selectionFixture(t, rec)
	x, y0 := previewPressXY(m, 0)
	_, y3 := previewPressXY(m, 3)

	nm, _ := m.handleMouse(tea.MouseClickMsg{X: x, Y: y0, Button: tea.MouseLeft})
	m = nm.(model)
	if m.selecting {
		t.Errorf("a press alone must NOT commit a selection (drag commits, FR14)")
	}
	if m.focusPane != focusPreview {
		t.Errorf("a preview press must set focus to the preview")
	}
	nm, _ = m.handleMouse(tea.MouseMotionMsg{X: x, Y: y3, Button: tea.MouseLeft})
	m = nm.(model)
	if !m.selecting {
		t.Fatalf("motion after press must commit the selection")
	}
	nm, _ = m.handleMouse(tea.MouseReleaseMsg{X: x, Y: y3, Button: tea.MouseLeft})
	m = nm.(model)

	n, recorded := selLines(rec)
	if !recorded {
		t.Fatalf("release after a drag must copy; events=%v", rec.names())
	}
	if n != 4 {
		t.Errorf("dragged 4 rows → recorded lines = %d, want 4", n)
	}
	if m.selecting {
		t.Errorf("release must end the selecting sub-state")
	}
}

// TestMousePlainClickNoCopy pins FR14: a press+release with NO motion copies nothing
// and leaves no active selection (it is a plain focus-set click).
func TestMousePlainClickNoCopy(t *testing.T) {
	rec := &fieldRecorder{}
	m, _ := selectionFixture(t, rec)
	x, y := previewPressXY(m, 2)
	nm, _ := m.handleMouse(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	m = nm.(model)
	nm, _ = m.handleMouse(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
	m = nm.(model)
	if _, recorded := selLines(rec); recorded {
		t.Errorf("a plain click (no motion) must NOT copy (FR14)")
	}
	if m.selecting {
		t.Errorf("a plain click must leave no active selection (FR14)")
	}
}

// TestMouseDragDividerNotDisturbed pins FR16: a left-press on the divider hit-zone
// still starts a divider drag, NOT a selection.
func TestMouseDragDividerNotDisturbed(t *testing.T) {
	m, _ := selectionFixture(t, nil)
	g := m.layout()
	nm, _ := m.handleMouse(tea.MouseClickMsg{X: g.dividerStart, Y: g.firstRow + 1, Button: tea.MouseLeft})
	m = nm.(model)
	if m.selecting {
		t.Errorf("a divider press must NOT start a selection (FR16)")
	}
	if !m.dragging {
		t.Errorf("a divider press must start a divider drag (FR16)")
	}
}

// TestMouseDragEdgeScroll pins FR15: dragging below the bottom edge of the preview
// edge-scrolls so the selection can pass the viewport via the mouse alone.
func TestMouseDragEdgeScroll(t *testing.T) {
	m, _ := selectionFixture(t, nil)
	x, y0 := previewPressXY(m, 0)
	nm, _ := m.handleMouse(tea.MouseClickMsg{X: x, Y: y0, Button: tea.MouseLeft})
	m = nm.(model)
	_, bodyH := m.previewScroll()
	g := m.layout()
	belowY := g.previewFirstRow + bodyH + 1 // past the bottom edge
	beforeTop := m.previewTop
	nm, _ = m.handleMouse(tea.MouseMotionMsg{X: x, Y: belowY, Button: tea.MouseLeft})
	m = nm.(model)
	if m.previewTop <= beforeTop {
		t.Errorf("dragging past the bottom edge must edge-scroll the preview down (FR15): top %d → %d", beforeTop, m.previewTop)
	}
}

// TestSelectModeInHelp pins FR10: SelectMode is reachable from the UI via the `?`
// full-help — it appears in the Preview group, and fullHelp stays exactly 5
// groups. The lean status bar no longer carries `V` (the select/wrap/hscroll tail
// moved to `?`), so the overlay is its single discoverability surface.
func TestSelectModeInHelp(t *testing.T) {
	m := modelAt(t, t.TempDir(), 100, 24)
	groups := m.fullHelp()
	if len(groups) != 5 {
		t.Fatalf("fullHelp returned %d groups, want 5 (SelectMode joins Preview, not a new group)", len(groups))
	}
	preview := groups[1] // Navigation, Preview, Mutation, Modes, Misc
	found := false
	for _, b := range preview {
		if b.Help().Key == "V" {
			found = true
		}
	}
	if !found {
		t.Errorf("SelectMode (V) not in the Preview full-help group; got %v", helpKeys(preview))
	}
}

// TestSelectingStatusHint pins FR10: while selecting, the status bar shows a hint
// mentioning the copy (`y`) and cancel (`esc`) keys.
func TestSelectingStatusHint(t *testing.T) {
	m, _ := selectionFixture(t, nil)
	m.focusPane = focusPreview
	nm, _ := m.updateNormal(keyRune('V'))
	m = nm.(model)
	status := ansi.Strip(m.renderStatus())
	if !strings.Contains(status, "y") || !strings.Contains(strings.ToLower(status), "copy") {
		t.Errorf("while selecting, status should hint the copy key; got %q", status)
	}
	if !strings.Contains(strings.ToLower(status), "cancel") && !strings.Contains(status, "esc") {
		t.Errorf("while selecting, status should hint the cancel key; got %q", status)
	}
}

// errTestRender is a sentinel error for the applyPreview error-path test.
var errTestRender = errTestRenderType{}

type errTestRenderType struct{}

func (errTestRenderType) Error() string { return "test render failure" }
