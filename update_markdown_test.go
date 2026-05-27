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
	if !strings.Contains(mm.View().Content, "rendering") {
		t.Error("status bar should show the rendering chip while a render is in flight")
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
	if strings.Contains(mm.View().Content, "rendering") {
		t.Error("rendering chip should be gone after the render lands")
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
