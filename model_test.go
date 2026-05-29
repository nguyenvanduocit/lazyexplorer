package main

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// editorModel builds a sized model rooted at a temp dir holding one file and one
// subdirectory, with cwd a level down so the synthetic ".." entry is present —
// exactly the three selection shapes the open-in-editor guard must distinguish
// (a real file, a dir, the synthetic "..").
func editorModel(t *testing.T) model {
	t.Helper()
	root := t.TempDir()
	mustMkdir(t, root, "sub")
	mustWrite(t, filepath.Join(root, "sub"), "main.go", "package main\n")
	mustMkdir(t, filepath.Join(root, "sub"), "child")
	m := modelAt(t, root, 100, 30)
	// Descend into sub/ so reload injects the synthetic ".." at index 0 and the
	// listing carries both a file (main.go) and a dir (child).
	m.cwd = filepath.Join(root, "sub")
	m.reload()
	return m
}

// TestOpenInEditorGuardsSelection drives the 'e' binding through updateNormal
// directly (NOT full Update): at the updateNormal boundary the returned cmd is the
// raw tea.ExecProcess cmd or nil, so cmd-nilness cleanly proves whether the exec
// was wired — at full-Update level reconcilePreview's batch would mask it. The
// guard must act only on a real file in focusList: refuse the synthetic "..", a
// directory, and an empty listing.
func TestOpenInEditorGuardsSelection(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "vim") // a runnable editor so the only nil-cmd cause is the guard

	t.Run("real file returns an exec cmd", func(t *testing.T) {
		m := editorModel(t)
		selectEntry(t, &m, "main.go")
		before := m.statusMsg
		nm, cmd := m.updateNormal(keyRune('e'))
		if cmd == nil {
			t.Fatalf("pressing e on a file returned nil cmd, want exec cmd")
		}
		if got := nm.(model).statusMsg; strings.HasPrefix(got, "⚠") {
			t.Errorf("file case set a warning status %q (was %q)", got, before)
		}
	})

	t.Run("synthetic .. is refused", func(t *testing.T) {
		m := editorModel(t)
		selectEntry(t, &m, "..")
		_, cmd := m.updateNormal(keyRune('e'))
		if cmd != nil {
			t.Errorf("pressing e on synthetic .. returned a cmd, want nil (refused)")
		}
	})

	t.Run("directory is refused", func(t *testing.T) {
		m := editorModel(t)
		selectEntry(t, &m, "child")
		_, cmd := m.updateNormal(keyRune('e'))
		if cmd != nil {
			t.Errorf("pressing e on a directory returned a cmd, want nil (refused)")
		}
	})

	t.Run("empty listing is refused", func(t *testing.T) {
		root := t.TempDir()
		m := modelAt(t, root, 100, 30) // empty dir, no synthetic .. at root
		if len(m.entries) != 0 {
			t.Fatalf("setup: expected empty listing, got %v", entryNames(m))
		}
		_, cmd := m.updateNormal(keyRune('e'))
		if cmd != nil {
			t.Errorf("pressing e with an empty listing returned a cmd, want nil")
		}
	})

	t.Run("file in focusPreview is refused", func(t *testing.T) {
		m := editorModel(t)
		selectEntry(t, &m, "main.go")
		m.focusPane = focusPreview
		_, cmd := m.updateNormal(keyRune('e'))
		if cmd != nil {
			t.Errorf("pressing e while focusPreview returned a cmd, want nil (focusList-only)")
		}
	})

	t.Run("missing editor surfaces a status instead of an exec", func(t *testing.T) {
		t.Setenv("VISUAL", "")
		t.Setenv("EDITOR", "")
		m := editorModel(t)
		selectEntry(t, &m, "main.go")
		nm, cmd := m.updateNormal(keyRune('e'))
		if cmd != nil {
			t.Errorf("pressing e with no editor returned a cmd, want nil")
		}
		if got := nm.(model).statusMsg; !strings.HasPrefix(got, "⚠") {
			t.Errorf("no-editor case status = %q, want a ⚠ warning", got)
		}
	})
}

// TestEditorFinishedMsgReloadsOnSuccess: when the editor exits cleanly, the resume
// must reload cwd immediately (don't wait on the 1s poll) so an edit — including a
// brand-new file the editor created — shows at once. The fs is mutated BEFORE the
// message is delivered: reloading an unchanged dir leaves entries identical, so a
// newly-appearing entry is the only signal that distinguishes reload-ran from
// reload-skipped. On error the resume surfaces a status, no reload claim needed.
func TestEditorFinishedMsgReloadsOnSuccess(t *testing.T) {
	t.Run("success reloads cwd", func(t *testing.T) {
		m := editorModel(t) // cwd = sub/, holds main.go + child/
		// Simulate the editor having created a sibling file while suspended.
		mustWrite(t, m.cwd, "created_by_editor.go", "package main\n")
		if containsRow(seeContent(m), "created_by_editor.go") {
			t.Fatalf("setup: new file already visible before reload")
		}
		var tm tea.Model = m
		tm, _ = tm.Update(editorFinishedMsg{err: nil})
		m = tm.(model)
		if !containsRow(seeContent(m), "created_by_editor.go") {
			t.Errorf("after editorFinishedMsg{nil} the new file is not listed; reload did not run\n%s", seeContent(m))
		}
		if strings.HasPrefix(m.statusMsg, "⚠") {
			t.Errorf("clean exit left a warning status %q", m.statusMsg)
		}
	})

	// The resume must keep the selection on the file the user just edited, by NAME —
	// not by clamped index. When the editor creates a file that sorts ABOVE the edited
	// one, an index-only reload leaves the cursor pointing at the new neighbour and the
	// preview pane silently shows that neighbour instead of the file just saved (the
	// poll path syncFromDisk already preserves by name; the resume must match it).
	t.Run("success keeps cursor and preview on the edited file by name", func(t *testing.T) {
		m := editorModel(t) // cwd = sub/, holds child/ + main.go
		// Give the edited file a unique marker so the preview pane can be identified.
		mustWrite(t, m.cwd, "main.go", "// MARKER_EDITED_MAIN\n")
		selectEntry(t, &m, "main.go") // cursor on the file the user is about to edit
		// The editor creates a sibling that sorts ABOVE main.go (files: aaa.go < main.go),
		// shifting main.go's index down — the exact churn the index-clamp mishandles.
		mustWrite(t, m.cwd, "aaa.go", "// MARKER_NEW_NEIGHBOUR\n")
		var tm tea.Model = m
		tm, _ = tm.Update(editorFinishedMsg{err: nil})
		m = tm.(model)
		if got := m.entries[m.cursor].name; got != "main.go" {
			t.Errorf("after resume cursor is on %q, want %q (selection must follow the edited file by name)", got, "main.go")
		}
		// The symptom is the PREVIEW: assert it reflects main.go, not the new neighbour.
		screen := seeContent(m)
		if !strings.Contains(screen, "MARKER_EDITED_MAIN") {
			t.Errorf("preview does not show the edited file (main.go); resume re-pointed it\n%s", screen)
		}
		if strings.Contains(screen, "MARKER_NEW_NEIGHBOUR") {
			t.Errorf("preview shows the new neighbour (aaa.go) instead of the edited file\n%s", screen)
		}
	})

	t.Run("error surfaces a status", func(t *testing.T) {
		m := editorModel(t)
		var tm tea.Model = m
		tm, _ = tm.Update(editorFinishedMsg{err: errNoEditor})
		m = tm.(model)
		if !strings.Contains(m.statusMsg, "⚠ editor") {
			t.Errorf("error exit status = %q, want it to contain %q", m.statusMsg, "⚠ editor")
		}
	})
}
