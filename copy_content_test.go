package main

// copy_content_test.go — TDD for the `Y` key: copy the previewed file's WHOLE raw
// text to the clipboard (prd-preview-copy). copyContent is the ONE shared code path
// (the `Y` key in updateNormal AND updateChanges, plus the palette twin), so the
// telemetry fires exactly once per copy. These assertions are clipboard-AGNOSTIC
// (writeClipboard fails in CI without pbcopy/xclip/wl-copy), so the oracle for WHAT
// was copied is the telemetry fieldRecorder's "bytes"/"name" — Record runs BEFORE
// writeClipboard, so the captured size is the true read length even with no helper.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
)

// copyBytes returns the byte count the model's last action.copy_content recorded,
// and whether it was recorded at all. The recorder is the content oracle: it
// captures len(content) at read time, independent of whether the OS clipboard
// helper exists (it does not, in CI).
func copyBytes(rec *fieldRecorder) (int, bool) {
	fields, ok := rec.last("action.copy_content")
	if !ok {
		return 0, false
	}
	n, _ := fields["bytes"].(int)
	return n, true
}

// TestCopyContentKeyBinding pins the new `Y` binding: keyRune('Y') matches
// CopyContent, and `Y` is not bound to any OTHER action in the keymap (the
// single-source-of-truth map must not double-map a key). Mirrors
// TestChangesKeyBinding; `Y` (uppercase) is free, like the G/H/L precedent.
func TestCopyContentKeyBinding(t *testing.T) {
	km := defaultKeyMap()
	if !key.Matches(keyRune('Y'), km.CopyContent) {
		t.Fatalf("`Y` must match the CopyContent binding")
	}
	for name, b := range allKeyBindings(km) {
		if name == "CopyContent" {
			continue
		}
		if key.Matches(keyRune('Y'), b) {
			t.Errorf("`Y` collides with binding %q — it must be free for CopyContent", name)
		}
	}
}

// TestCopyContentNormal drives the `Y` binding through updateNormal directly (the
// same boundary TestYankRelPath uses). It pins the headline FR1/FR6 contract: copy
// the WHOLE raw text of the previewed file. The recorder is the content oracle.
func TestCopyContentNormal(t *testing.T) {
	t.Run("text file records its whole raw byte length, no cmd", func(t *testing.T) {
		rec := &fieldRecorder{}
		m := editorModel(t) // cwd = <root>/sub, holds main.go
		m.tel = rec
		selectEntry(t, &m, "main.go")
		full := filepath.Join(m.previewBaseDir(), "main.go")
		want, err := os.ReadFile(full)
		if err != nil {
			t.Fatalf("read fixture: %v", err)
		}

		nm, cmd := m.updateNormal(keyRune('Y'))
		ym := nm.(model)
		if cmd != nil {
			t.Errorf("copy returned a cmd, want nil (synchronous side-effect, like yank)")
		}

		n, recorded := copyBytes(rec)
		if !recorded {
			t.Fatalf("copy must record action.copy_content; events=%v", rec.names())
		}
		if n != len(want) {
			t.Errorf("recorded bytes = %d, want %d (the whole file's raw length)", n, len(want))
		}
		// Status is clipboard-agnostic: "copied <name> (<n> bytes)" or "⚠ clipboard…".
		copied := strings.HasPrefix(ym.statusMsg, "copied ")
		clipFail := strings.HasPrefix(ym.statusMsg, "⚠ clipboard")
		if !copied && !clipFail {
			t.Fatalf("status = %q, want a 'copied …' or '⚠ clipboard…' outcome", ym.statusMsg)
		}
		if copied && !strings.Contains(ym.statusMsg, "main.go") {
			t.Errorf("copied status %q should name the file", ym.statusMsg)
		}
		if name, _ := rec.last("action.copy_content"); name["name"] != "main.go" {
			t.Errorf("recorded name = %v, want main.go", name["name"])
		}
	})

	t.Run("records exactly once per copy", func(t *testing.T) {
		rec := &fieldRecorder{}
		m := editorModel(t)
		m.tel = rec
		selectEntry(t, &m, "main.go")
		m.updateNormal(keyRune('Y'))
		count := 0
		for _, n := range rec.names() {
			if n == "action.copy_content" {
				count++
			}
		}
		if count != 1 {
			t.Errorf("action.copy_content recorded %d times, want exactly 1 (D9/D10 single code path)", count)
		}
	})
}

// TestCopyContentBothFocus pins D5: `Y` acts at BOTH focuses — unlike yank
// (focusList-only), the previewed file is well-defined whether the eye is on the
// list or the preview, and a no-op under focusPreview is the reflex trap.
func TestCopyContentBothFocus(t *testing.T) {
	for _, focus := range []struct {
		name string
		pane focusPane
	}{
		{"focusList", focusList},
		{"focusPreview", focusPreview},
	} {
		t.Run(focus.name+" copies the file", func(t *testing.T) {
			rec := &fieldRecorder{}
			m := editorModel(t)
			m.tel = rec
			selectEntry(t, &m, "main.go")
			m.focusPane = focus.pane
			want, _ := os.ReadFile(filepath.Join(m.previewBaseDir(), "main.go"))

			m.updateNormal(keyRune('Y'))
			n, recorded := copyBytes(rec)
			if !recorded {
				t.Fatalf("%s: copy must record action.copy_content (no-op under this focus?)", focus.name)
			}
			if n != len(want) {
				t.Errorf("%s: recorded bytes = %d, want %d", focus.name, n, len(want))
			}
		})
	}
}

// TestCopyContentRawNotDiff pins FR6/D7: on a tracked-MODIFIED text file shown as a
// diff (previewIsDiff true), `Y` copies the file's CURRENT on-disk content — NOT the
// colorized diff text. Discriminator: the recorded byte count equals the raw file
// length, which differs from the diff/preview rendering's length.
func TestCopyContentRawNotDiff(t *testing.T) {
	rec := &fieldRecorder{}
	m := changesRepoModel(t, rec) // real repo; README.md + src/app.go are modified
	m.diffOn = true
	moveCursorToAny(t, &m, "README.md") // tracked-modified text file in cwd (repo root)
	m.pendingWidth = 0
	m.renderNow()
	if !m.previewIsDiff {
		t.Fatalf("setup: README.md must preview as a DIFF (previewIsDiff=false); preview=%q",
			strings.Join(m.preview, "\n"))
	}

	full := filepath.Join(m.previewBaseDir(), "README.md")
	raw, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	m.updateNormal(keyRune('Y'))
	n, recorded := copyBytes(rec)
	if !recorded {
		t.Fatalf("copy on a diff-previewed file must record; events=%v", rec.names())
	}
	if n != len(raw) {
		t.Errorf("recorded bytes = %d, want the RAW file length %d (not the diff/ANSI preview)", n, len(raw))
	}
	// The diff preview body is longer/different from the raw file — prove they diverge
	// so this test would actually catch a regression that copied m.preview.
	previewLen := len(strings.Join(m.preview, "\n"))
	if previewLen == len(raw) {
		t.Skip("preview length coincidentally equals raw length — discriminator weak on this fixture")
	}
}

// TestCopyContentFullReadNotCapped pins FR6's full-read invariant: a file LARGER
// than maxPreviewBytes (256KB) must be copied IN FULL — not truncated by the
// 256KB-capped readPreviewBytes. This is the ONLY assertion that catches a
// regression from os.ReadFile to readPreviewBytes.
func TestCopyContentFullReadNotCapped(t *testing.T) {
	rec := &fieldRecorder{}
	root := t.TempDir()
	big := strings.Repeat("lazyexplorer copies the whole file, not a 256KB slice.\n", 8000) // ~430KB > 256KB
	if len(big) <= maxPreviewBytes {
		t.Fatalf("fixture must exceed maxPreviewBytes (%d); got %d", maxPreviewBytes, len(big))
	}
	mustWrite(t, root, "big.txt", big)
	m := modelAt(t, root, 100, 30)
	m.tel = rec
	selectEntry(t, &m, "big.txt")

	m.updateNormal(keyRune('Y'))
	n, recorded := copyBytes(rec)
	if !recorded {
		t.Fatalf("copy on a >256KB file must record; events=%v", rec.names())
	}
	if n != len(big) {
		t.Errorf("recorded bytes = %d, want the WHOLE file %d (readPreviewBytes would cap at %d)", n, len(big), maxPreviewBytes)
	}
}

// TestCopyContentGuards pins FR2/FR3/FR4/FR5: dir/`..` refuse with "⚠ not a file",
// binary/image refuse with "⚠ not text", empty copies a valid 0-byte content, an
// empty listing refuses with "⚠ nothing selected". A refused copy records NOTHING
// (the guards return before tel.Record — D9/D10).
func TestCopyContentGuards(t *testing.T) {
	t.Run("directory is refused, records nothing", func(t *testing.T) {
		rec := &fieldRecorder{}
		m := editorModel(t)
		m.tel = rec
		selectEntry(t, &m, "child") // a real subdirectory
		nm, _ := m.updateNormal(keyRune('Y'))
		if got := nm.(model).statusMsg; got != "⚠ not a file" {
			t.Errorf("dir status = %q, want %q", got, "⚠ not a file")
		}
		if _, recorded := copyBytes(rec); recorded {
			t.Errorf("a refused dir copy must NOT record action.copy_content")
		}
	})

	t.Run("synthetic .. is refused", func(t *testing.T) {
		m := editorModel(t)
		selectEntry(t, &m, "..")
		nm, _ := m.updateNormal(keyRune('Y'))
		if got := nm.(model).statusMsg; got != "⚠ not a file" {
			t.Errorf(".. status = %q, want %q", got, "⚠ not a file")
		}
	})

	t.Run("binary file is refused", func(t *testing.T) {
		rec := &fieldRecorder{}
		root := t.TempDir()
		mustWriteBytes(t, root, "blob.bin", []byte{0x00, 0x01, 0x02, 0xff, 0x00}) // NUL → isBinary
		m := modelAt(t, root, 100, 30)
		m.tel = rec
		selectEntry(t, &m, "blob.bin")
		nm, _ := m.updateNormal(keyRune('Y'))
		if got := nm.(model).statusMsg; got != "⚠ not text" {
			t.Errorf("binary status = %q, want %q", got, "⚠ not text")
		}
		if _, recorded := copyBytes(rec); recorded {
			t.Errorf("a refused binary copy must NOT record action.copy_content")
		}
	})

	t.Run("empty file copies a valid 0-byte content", func(t *testing.T) {
		rec := &fieldRecorder{}
		root := t.TempDir()
		mustWrite(t, root, "empty.txt", "")
		m := modelAt(t, root, 100, 30)
		m.tel = rec
		selectEntry(t, &m, "empty.txt")
		nm, _ := m.updateNormal(keyRune('Y'))
		got := nm.(model).statusMsg
		n, recorded := copyBytes(rec)
		if !recorded {
			t.Fatalf("an empty text file is a valid copy and must record; events=%v", rec.names())
		}
		if n != 0 {
			t.Errorf("empty file recorded bytes = %d, want 0", n)
		}
		// Status confirms 0 bytes on clipboard success, or the clipboard warning.
		copied := strings.Contains(got, "(0 bytes)")
		clipFail := strings.HasPrefix(got, "⚠ clipboard")
		if !copied && !clipFail {
			t.Errorf("empty-file status = %q, want a '(0 bytes)' or '⚠ clipboard…' outcome", got)
		}
	})

	t.Run("empty listing is refused", func(t *testing.T) {
		rec := &fieldRecorder{}
		m := modelAt(t, t.TempDir(), 100, 30) // empty dir, no synthetic .. at root
		m.tel = rec
		if len(m.entries) != 0 {
			t.Fatalf("setup: expected empty listing, got %v", entryNames(m))
		}
		nm, _ := m.updateNormal(keyRune('Y'))
		if got := nm.(model).statusMsg; got != "⚠ nothing selected" {
			t.Errorf("empty-listing status = %q, want %q", got, "⚠ nothing selected")
		}
		if _, recorded := copyBytes(rec); recorded {
			t.Errorf("an empty-listing copy must NOT record action.copy_content")
		}
	})
}

// TestCopyContentInFullHelpMisc pins the discoverability surface (FR11): CopyContent
// lives in the Misc group of the `?` full-help (a clipboard utility, not a mutation),
// and fullHelp stays exactly 5 groups (renderHelpBody titles depend on it). Mirror
// of TestYankInFullHelpMisc.
func TestCopyContentInFullHelpMisc(t *testing.T) {
	m := modelAt(t, t.TempDir(), 100, 30)
	groups := m.fullHelp()
	if len(groups) != 5 {
		t.Fatalf("fullHelp returned %d groups, want 5 (CopyContent must join Misc, not add a group)", len(groups))
	}
	misc := groups[4] // Navigation, Preview, Mutation, Modes, Misc
	found := false
	for _, b := range misc {
		if b.Help().Key == "Y" && strings.Contains(b.Help().Desc, "content") {
			found = true
		}
	}
	if !found {
		t.Errorf("CopyContent binding (Y · copy file content) not in the Misc full-help group; got %v", helpKeys(misc))
	}
}

// TestCopyContentDoesNotShadowDeleteConfirm is the FR9 regression guard for the
// mode-lane separation the new `Y` binding relies on: `Y` is bound to CopyContent in
// modeNormal, but the modeConfirmDelete prompt (reached via `d`) reads "y"/"Y" as its
// own confirm rune in updateConfirmDelete — a DIFFERENT dispatch lane that Update
// routes to BEFORE updateNormal can see the key. Pressing `d` then `Y` must still
// DELETE, not copy. (The lowercase-`y` twin TestYankDoesNotShadowDeleteConfirm guards
// yank; THIS PRD introduces the uppercase `Y`, so it needs its own guard.)
func TestCopyContentDoesNotShadowDeleteConfirm(t *testing.T) {
	m := editorModel(t) // cwd = <root>/sub, holds main.go
	selectEntry(t, &m, "main.go")
	target := filepath.Join(m.cwd, "main.go")

	// d → enter the confirm prompt.
	nm, _ := m.updateNormal(keyRune('d'))
	m = nm.(model)
	if m.mode != modeConfirmDelete {
		t.Fatalf("d did not open the delete-confirm prompt (mode=%v)", m.mode)
	}
	// Y in the confirm lane must DELETE, not copy.
	nm2, _ := m.updateConfirmDelete(keyRune('Y'))
	m = nm2.(model)
	if _, err := os.Stat(target); err == nil {
		t.Errorf("confirm-delete `Y` did not delete %q (copy-content shadowed the confirm rune)", target)
	}
	if !strings.HasPrefix(m.statusMsg, "deleted ") {
		t.Errorf("after confirm `Y` status = %q, want a 'deleted …' message", m.statusMsg)
	}
}

// TestCopyContentInHelp pins FR11: the `Y` binding is reachable from the UI via
// the `?` full-help overlay (fullHelp's Misc group). The lean status bar no
// longer carries the long tail — discoverability lives in `?`, sourced from the
// keymap so it can't drift.
func TestCopyContentInHelp(t *testing.T) {
	m := modelAt(t, t.TempDir(), 100, 30)
	groups := m.fullHelp()
	misc := groups[len(groups)-1] // Navigation, Preview, Mutation, Modes, Misc
	found := false
	for _, b := range misc {
		if b.Help().Key == "Y" {
			found = true
		}
	}
	if !found {
		t.Errorf("CopyContent (Y) not in the Misc full-help group")
	}
}
