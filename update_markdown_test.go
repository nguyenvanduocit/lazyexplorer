package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// markdownFromCmd runs a Cmd produced by Update and digs out the
// previewRenderedMsg, unwrapping the tea.Batch (Update batches the per-message
// cmd with the tail syncPreview render). It never runs a tick Cmd, so it must
// only be used on cmds from WindowSizeMsg / navigation KeyMsgs (no tickCmd).
func markdownFromCmd(t *testing.T, cmd tea.Cmd) previewRenderedMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a non-nil Cmd carrying a markdown render")
	}
	switch v := cmd().(type) {
	case previewRenderedMsg:
		return v
	case tea.BatchMsg:
		for _, c := range v {
			if c == nil {
				continue
			}
			if mr, ok := c().(previewRenderedMsg); ok {
				return mr
			}
		}
	}
	t.Fatal("no previewRenderedMsg found in the Cmd")
	return previewRenderedMsg{}
}

// TestUpdateDispatchesAndAppliesMarkdown drives the real Update path end-to-end,
// the way the Bubbletea runtime would: a window size arrives, the cursor is on a
// markdown file, Update hands back a render Cmd (without blocking), and feeding
// the resulting message back through Update swaps in the styled preview and
// clears the "rendering" chip. This covers the Update rewiring itself — the
// nm.(model) hand-back, the tail syncPreview batch, and the previewRenderedMsg
// case — which the isolated syncPreview/applyPreview tests never exercise.
func TestUpdateDispatchesAndAppliesMarkdown(t *testing.T) {
	dir := t.TempDir()
	src := "# Heading\n\nbody **text** here\n"
	if err := os.WriteFile(filepath.Join(dir, "doc.md"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	var m tea.Model = newModel(dir, noopRecorder{}) // cursor lands on doc.md
	m, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	mm := m.(model)
	if mm.previewPreStyled {
		t.Fatal("preview must not be styled synchronously inside Update — the render runs off-loop")
	}
	if mm.pendingWidth == 0 {
		t.Fatal("a render should be in flight (pendingWidth>0) after the first WindowSizeMsg")
	}
	// Indicator is now a right-edge braille spinner (not the "rendering" text).
	// No spinnerTickMsg has been processed, so the frame is still 0.
	spin := spinnerFrames[mm.spinnerFrame%len(spinnerFrames)]
	if !strings.Contains(mm.View().Content, spin) {
		t.Error("status bar should show the render spinner while a render is in flight")
	}

	// The runtime would run cmd in a goroutine and route its message back. Do that.
	msg := markdownFromCmd(t, cmd)
	if msg.err != nil {
		t.Fatalf("render reported an error: %v", msg.err)
	}
	m, _ = m.Update(msg)

	mm = m.(model)
	if !mm.previewPreStyled {
		t.Error("preview should be styled after the render message is applied through Update")
	}
	if mm.pendingWidth != 0 {
		t.Error("pendingWidth should be 0 after the render lands")
	}
	if strings.Contains(mm.View().Content, spin) {
		t.Error("render spinner should be gone after the render lands")
	}
}

// TestUpdateNavigationDispatchesRender proves a keyboard navigation onto a
// markdown file (not just the initial size message) also dispatches an async
// render through Update's tail reconciliation.
func TestUpdateNavigationDispatchesRender(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("plain\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.md"), []byte("# B\n\nbravo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var m tea.Model = newModel(dir, noopRecorder{}) // a.txt (0), b.md (1); cursor on a.txt
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if m.(model).entries[m.(model).cursor].name != "a.txt" {
		t.Fatalf("setup: cursor on %q, want a.txt", m.(model).entries[m.(model).cursor].name)
	}

	// Press 'j' to move onto b.md → Update should dispatch a render.
	m, cmd := m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if m.(model).entries[m.(model).cursor].name != "b.md" {
		t.Fatalf("after 'j': cursor on %q, want b.md", m.(model).entries[m.(model).cursor].name)
	}
	msg := markdownFromCmd(t, cmd) // dispatch happened; pull the styled result
	if msg.err != nil {
		t.Fatalf("render reported an error: %v", msg.err)
	}
	m, _ = m.Update(msg)
	if !m.(model).previewPreStyled {
		t.Error("b.md preview should be styled after navigating onto it and the render lands")
	}
}

// TestUpdateRedispatchesRenderOnResponsiveModeFlip is the FR7 contract: when a
// markdown render is current at one orientation's preview width and the user
// resizes through widthBreakpoint, previewBodyWidth changes, the cached
// m.srcWidth no longer matches, and the tail syncPreview dispatches a fresh
// render at the new width. The gen-counter inside applyPreview drops any
// stale in-flight render. Without this the preview pane would render at the
// old (wrong) width after the flip, leaving torn ANSI on the resized frame.
func TestUpdateRedispatchesRenderOnResponsiveModeFlip(t *testing.T) {
	dir := t.TempDir()
	src := "# Heading\n\nbody **text** here\n"
	if err := os.WriteFile(filepath.Join(dir, "doc.md"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	// Land at horizontal mode (width=100) and complete the first render so the
	// model carries m.srcWidth = previewBodyWidth(horizontal) = g.rightInner = 60.
	var m tea.Model = newModel(dir, noopRecorder{})
	m, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m, _ = m.Update(markdownFromCmd(t, cmd))
	mm := m.(model)
	if !mm.previewPreStyled {
		t.Fatalf("setup: preview not styled after horizontal render")
	}
	if mm.srcWidth != 60 {
		t.Fatalf("setup: srcWidth = %d, want 60 (= g.rightInner at width=100)", mm.srcWidth)
	}

	// Shrink below widthBreakpoint → vertical mode. previewBodyWidth becomes
	// m.width = 70 (full pane in 1-col stacked, borderless). The new width is
	// !=  m.srcWidth, so the tail syncPreview must hand back a render Cmd.
	m, cmd = m.Update(tea.WindowSizeMsg{Width: 70, Height: 24})
	mm = m.(model)
	if !mm.lastVertical {
		t.Fatalf("after shrink to 70: lastVertical = false, want true (mode should flip)")
	}
	if mm.pendingWidth != 70 {
		t.Errorf("after mode flip: pendingWidth = %d, want 70 (re-render at vertical full width)", mm.pendingWidth)
	}
	if !strings.Contains(mm.View().Content, spinnerFrames[mm.spinnerFrame%len(spinnerFrames)]) {
		t.Error("render spinner should reappear while the post-flip render is in flight")
	}

	// Drive the new render to completion and confirm the preview is restyled
	// at the new width.
	m, _ = m.Update(markdownFromCmd(t, cmd))
	mm = m.(model)
	if !mm.previewPreStyled {
		t.Error("preview should be styled again after the post-flip render lands")
	}
	if mm.srcWidth != 70 {
		t.Errorf("after post-flip render: srcWidth = %d, want 70", mm.srcWidth)
	}
}

// TestConcurrentMarkdownRendersAreSafe is the empirical guard behind resolving the
// render style from a passed-in name: fast navigation spawns many render Cmds that
// the runtime executes on separate goroutines. renderMarkdown builds its style from
// the style argument alone — no per-render terminal query, no shared state — so
// concurrent renders are pure and race-free. Run under -race, this must be clean.
// (Mirrors what the real program does when you hold 'j' across .md files.)
func TestConcurrentMarkdownRendersAreSafe(t *testing.T) {
	srcs := []string{
		"# Alpha\n\nalpha body with **bold**\n",
		"# Bravo\n\n- one\n- two\n",
		"# Charlie\n\n```go\nfmt.Println(\"hi\")\n```\n",
	}
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := renderMarkdown(srcs[i%len(srcs)], 60, "dark"); err != nil {
				t.Errorf("concurrent render %d failed: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
}
