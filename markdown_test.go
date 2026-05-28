package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestIsMarkdown(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"README.md", true},
		{"NOTES.MD", true}, // case-insensitive
		{"spec.markdown", true},
		{"Doc.Markdown", true},
		{"main.go", false},
		{"notes.txt", false},
		{"page.mdx", false}, // .mdx is not in scope
		{"README", false},   // no extension
		{".md", true},       // dotfile whose ext is .md
	}
	for _, c := range cases {
		if got := isMarkdown(c.name); got != c.want {
			t.Errorf("isMarkdown(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

// modelAt builds a model rooted at dir with a known terminal size, loaded.
// Markdown is NOT styled yet: rendering is async (off the Update goroutine), so
// a test that wants the styled result calls m.renderNow() after this.
//
// Mirrors newModel's defaults — including topRatio: 0.33 — so tests built on
// modelAt see the same vertical-mode geometry the program ships with. Direct
// struct construction (instead of calling newModel) avoids reload() touching
// any cwd the test didn't ask about, which is why this helper exists in the
// first place.
func modelAt(t *testing.T, dir string, width, height int) model {
	t.Helper()
	m := model{
		root: dir, cwd: dir,
		leftRatio: 0.38, topRatio: 0.33,
		width: width, height: height,
		tel: noopRecorder{},
	}
	m.reload()
	return m
}

// renderNow drives the async markdown pipeline to completion synchronously, the
// way the Bubbletea event loop eventually would: dispatch the render Cmd, run it
// inline, and apply the result. This lets a unit test assert the styled preview
// without spinning the real program (and its 1s poll loop). No-op when nothing
// needs rendering (not markdown / width unknown / already current).
func (m *model) renderNow() {
	if cmd := m.syncPreview(); cmd != nil {
		if msg, ok := cmd().(previewRenderedMsg); ok {
			m.applyPreview(msg)
		}
	}
}

// TestMarkdownRenderIsAsync is the headline contract behind the freeze fix:
// selecting a markdown file must NOT run glamour inline in refreshPreview (that
// blocks the single Update goroutine → the whole UI freezes with no feedback).
// Instead refreshPreview shows the raw source instantly as a placeholder, and
// syncPreview hands back a Cmd that does the heavy render off-loop.
func TestMarkdownRenderIsAsync(t *testing.T) {
	dir := t.TempDir()
	src := "# Title\n\nSome **bold** body text that glamour will restyle.\n"
	if err := os.WriteFile(filepath.Join(dir, "doc.md"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	m := modelAt(t, dir, 100, 30) // cursor lands on doc.md

	// Synchronous step (refreshPreview, run by reload) must only place the plain
	// source — never the styled output. previewPreStyled proves no inline render.
	if m.previewPreStyled {
		t.Fatal("markdown was rendered synchronously in refreshPreview — that blocks Update and freezes the UI")
	}
	if len(m.srcRaw) == 0 {
		t.Fatal("srcRaw must be captured so the async render has something to work on")
	}
	if strings.Join(m.preview, "\n") != string(m.srcRaw) {
		t.Error("preview placeholder should be the raw source shown instantly, before styling")
	}

	// The async step: syncPreview dispatches a real render Cmd.
	cmd := m.syncPreview()
	if cmd == nil {
		t.Fatal("syncPreview returned nil — no async render was dispatched for a markdown selection")
	}
	msg, ok := cmd().(previewRenderedMsg)
	if !ok {
		t.Fatalf("render Cmd returned %T, want previewRenderedMsg", cmd())
	}
	if msg.err != nil {
		t.Fatalf("render Cmd reported an error: %v", msg.err)
	}
	if strings.Join(msg.lines, "\n") == string(m.srcRaw) {
		t.Error("rendered lines equal the raw source — glamour did not actually run inside the Cmd")
	}
}

// TestApplyPreviewRespectsPreStyled locks the hinge of the generic pipeline:
// previewPreStyled is taken from the renderer's result, never hardcoded. A
// pre-styled result (markdown/code → verbatim ANSI) makes renderPreview skip
// fitWidth; a plain result (a placeholder/scaffold renderer, e.g. image) keeps
// fitWidth so a long line is still truncated. A regression here silently strips
// color (prestyled forced false) or overflows the panel (plain forced true).
func TestApplyPreviewRespectsPreStyled(t *testing.T) {
	long := strings.Repeat("x", 300)
	m := model{width: 100, height: 30, leftRatio: 0.38, tel: noopRecorder{}}
	w := m.previewBodyWidth()

	// preStyled=false (plain placeholder) → previewPreStyled false → fitWidth runs.
	m.renderGen = 1
	m.applyPreview(previewRenderedMsg{gen: 1, width: w, lines: []string{long}, preStyled: false})
	if m.previewPreStyled {
		t.Fatal("applyPreview ignored msg.preStyled=false — it must not hardcode true")
	}
	for _, ln := range strings.Split(m.renderPreview(w), "\n") {
		if lipgloss.Width(ln) > w {
			t.Errorf("plain result line width %d exceeds panel %d — fitWidth was skipped", lipgloss.Width(ln), w)
		}
	}

	// preStyled=true (renderer already fit + colored) → previewPreStyled true →
	// fitWidth skipped, the ANSI line is emitted verbatim.
	m.renderGen = 2
	styled := "\x1b[31m" + long + "\x1b[0m"
	m.applyPreview(previewRenderedMsg{gen: 2, width: w, lines: []string{styled}, preStyled: true})
	if !m.previewPreStyled {
		t.Fatal("applyPreview ignored msg.preStyled=true")
	}
	if !strings.Contains(m.renderPreview(w), styled) {
		t.Error("pre-styled line was altered — fitWidth should be skipped, ANSI left verbatim")
	}
}

func TestMarkdownPreviewIsRendered(t *testing.T) {
	dir := t.TempDir()
	src := "# Title\n\nSome **bold** and a list:\n\n- one\n- two\n"
	if err := os.WriteFile(filepath.Join(dir, "doc.md"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	m := modelAt(t, dir, 100, 30) // cursor lands on the only entry, doc.md
	m.renderNow()                 // drive the async render to completion

	if !m.previewPreStyled {
		t.Fatal("markdown preview should be pre-styled (glamour-rendered) after the render lands")
	}
	if len(m.srcRaw) == 0 {
		t.Fatal("srcRaw should hold the raw markdown")
	}
	if m.srcWidth != m.previewBodyWidth() {
		t.Errorf("srcWidth = %d, want %d (current body width)", m.srcWidth, m.previewBodyWidth())
	}
	// glamour transforms the source (margins, styling, stripped '#'); the rendered
	// lines must differ from the raw source split — proof rendering happened.
	if strings.Join(m.preview, "\n") == src {
		t.Error("preview equals raw source — glamour did not render")
	}
}

func TestMarkdownDeferredAtZeroWidth(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "doc.md"), []byte("# Hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// width 0: WindowSizeMsg has not arrived yet (the real startup order).
	m := modelAt(t, dir, 0, 0)
	if m.previewPreStyled {
		t.Fatal("must NOT render markdown at width 0 (would wrap to 0)")
	}
	if len(m.srcRaw) == 0 {
		t.Fatal("srcRaw should still be captured so a later render can use it")
	}
	if cmd := m.syncPreview(); cmd != nil {
		t.Fatal("must NOT dispatch a render at width 0 — defer until WindowSizeMsg gives a real width")
	}

	// Width arrives → deferred render fires.
	m.width, m.height = 100, 30
	m.renderNow()
	if !m.previewPreStyled {
		t.Error("markdown should render once width is known")
	}
}

func TestPreviewResetHygiene(t *testing.T) {
	dir := t.TempDir()
	longLine := strings.Repeat("x", 200)
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("# Heading\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte(longLine+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := modelAt(t, dir, 100, 30) // entries sorted alpha: a.md (0), b.txt (1)
	m.renderNow()
	if !m.previewPreStyled {
		t.Fatal("setup: a.md should be pre-styled after the render lands")
	}

	// Navigate to the plain .txt file.
	m.cursor = 1
	m.refreshPreview()
	if m.previewPreStyled {
		t.Fatal("after .md → .txt, previewPreStyled must reset to false (else fitWidth is skipped on plain text)")
	}
	// The long plain line must be truncated by fitWidth to the panel width.
	w := m.previewBodyWidth()
	body := m.renderPreview(w)
	for _, line := range strings.Split(body, "\n") {
		if lipgloss.Width(line) > w {
			t.Errorf("plain line width %d exceeds panel width %d — fitWidth was skipped", lipgloss.Width(line), w)
		}
	}
}

func TestMarkdownReRenderOnWidthChange(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "doc.md"), []byte("# Title\n\nA paragraph that is long enough to wrap differently at different widths, yes indeed.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := modelAt(t, dir, 100, 30)
	m.renderNow()
	w1 := m.srcWidth
	if w1 <= 0 {
		t.Fatal("setup: expected a rendered width")
	}

	// Cache hit: same width → syncPreview has nothing to dispatch.
	if cmd := m.syncPreview(); cmd != nil {
		t.Error("cache: syncPreview dispatched a render at an unchanged width")
	}

	// Widen the terminal → body width grows → re-render at the new width.
	m.width = 160
	m.renderNow()
	if m.srcWidth == w1 {
		t.Errorf("srcWidth did not update after width change (still %d)", w1)
	}
	if m.srcWidth != m.previewBodyWidth() {
		t.Errorf("srcWidth = %d, want %d after reflow", m.srcWidth, m.previewBodyWidth())
	}
}

// TestStaleMarkdownResultDiscarded guards rapid navigation: a render dispatched
// for file A can land *after* the user has moved to file B. The generation
// counter must discard that stale result so B's preview is never clobbered by
// A's bytes. This is the case glow never hits (one doc at a time) and we do (a
// file list you scroll through).
func TestStaleMarkdownResultDiscarded(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("# AAA\n\nalpha body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.md"), []byte("# BBB\n\nbravo body\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := modelAt(t, dir, 100, 30) // a.md sorts first → cursor 0
	if m.entries[m.cursor].name != "a.md" {
		t.Fatalf("setup: cursor on %q, want a.md", m.entries[m.cursor].name)
	}

	// Dispatch A's render and capture its (soon-to-be-stale) result without
	// applying it yet — simulating the render goroutine still in flight.
	cmdA := m.syncPreview()
	if cmdA == nil {
		t.Fatal("expected a render dispatch for a.md")
	}
	genA := m.renderGen
	staleMsg, ok := cmdA().(previewRenderedMsg)
	if !ok {
		t.Fatalf("a.md render returned %T, want previewRenderedMsg", cmdA())
	}
	if staleMsg.gen != genA {
		t.Fatalf("dispatched gen %d but msg carries gen %d", genA, staleMsg.gen)
	}

	// User navigates to b.md before A's result lands.
	m.cursor = 1
	m.refreshPreview()
	if m.pendingWidth != 0 {
		t.Fatal("refreshPreview must reset pendingWidth — a new selection cancels the in-flight render's claim")
	}
	if !strings.Contains(string(m.srcRaw), "BBB") {
		t.Fatalf("setup: expected b.md selected, srcRaw=%q", m.srcRaw)
	}

	cmdB := m.syncPreview()
	if cmdB == nil {
		t.Fatal("expected a render dispatch for b.md")
	}
	if m.renderGen == genA {
		t.Fatal("renderGen did not advance on the new dispatch — stale guard is broken")
	}

	// The stale A result now lands. It must be discarded (gen mismatch), leaving
	// B's placeholder untouched.
	before := slices.Clone(m.preview)
	m.applyPreview(staleMsg)
	if !slices.Equal(m.preview, before) {
		t.Error("stale gen-A render overwrote the preview after the user moved to b.md")
	}
	// A stale apply must NOT clear pendingWidth — b.md's render is still in
	// flight, so the "rendering…" chip has to stay up until *its* result lands.
	// Guards against a refactor moving the clear above the gen check.
	if m.pendingWidth == 0 {
		t.Error("stale apply cleared pendingWidth — the chip would vanish before b.md's render lands")
	}

	// B's own fresh result still applies normally.
	freshB, ok := cmdB().(previewRenderedMsg)
	if !ok {
		t.Fatalf("b.md render returned %T, want previewRenderedMsg", cmdB())
	}
	m.applyPreview(freshB)
	if !m.previewPreStyled {
		t.Error("b.md's fresh render was not applied")
	}
	if !strings.Contains(strings.Join(m.preview, "\n"), "BBB") {
		t.Error("preview does not reflect b.md after its render landed")
	}
}

// TestMarkdownRenderDeferredWhileDragging asserts the reflow optimisation: while
// the user drags the divider, the body width changes every motion event, but we
// must NOT spawn a render per pixel. syncPreview defers until the drag ends,
// then reflows once to the settled width.
func TestMarkdownRenderDeferredWhileDragging(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "doc.md"), []byte("# Hi\n\nbody paragraph here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := modelAt(t, dir, 100, 30)
	m.renderNow()
	w1 := m.srcWidth
	if w1 <= 0 {
		t.Fatal("setup: expected an initial rendered width")
	}

	// Begin a drag and move the divider (body width changes).
	m.dragging = true
	m.leftRatio = 0.7
	if cmd := m.syncPreview(); cmd != nil {
		t.Error("syncPreview dispatched a render mid-drag — reflow must defer until release")
	}

	// Release → reflow to the new width fires exactly once.
	m.dragging = false
	cmd := m.syncPreview()
	if cmd == nil {
		t.Fatal("expected a reflow dispatch on drag release (width changed)")
	}
	if msg, ok := cmd().(previewRenderedMsg); ok {
		m.applyPreview(msg)
	} else {
		t.Fatalf("reflow Cmd returned %T, want previewRenderedMsg", cmd())
	}
	if m.srcWidth == w1 {
		t.Errorf("reflow did not update srcWidth after the width change (still %d)", w1)
	}
	if m.srcWidth != m.previewBodyWidth() {
		t.Errorf("srcWidth = %d, want %d after reflow", m.srcWidth, m.previewBodyWidth())
	}
}
