package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
)

// TestYankRelPath drives the `y` binding through updateNormal directly — the same
// boundary TestOpenInEditorGuardsSelection uses. writeClipboard fails in CI (no
// pbcopy/xclip/wl-copy), so the assertions are clipboard-AGNOSTIC: the status is
// either "copied <rel>" (clipboard worked) or "⚠ clipboard…" (no helper). The
// load-bearing claim is that the rel string is project-relative — the absolute
// path's m.root prefix is GONE — which the pure relRoot already proves in
// TestRelRoot; here we prove the dispatch threads it through and guards the edges.
func TestYankRelPath(t *testing.T) {
	t.Run("file copies its slash-form rel path, no root prefix", func(t *testing.T) {
		m := editorModel(t) // cwd = <root>/sub, holds main.go + child/ + synthetic ..
		selectEntry(t, &m, "main.go")
		nm, cmd := m.updateNormal(keyRune('y'))
		ym := nm.(model)
		if cmd != nil {
			t.Errorf("yank returned a cmd, want nil (pure side-effect, no async)")
		}
		copied := strings.HasPrefix(ym.statusMsg, "copied ")
		clipFail := strings.HasPrefix(ym.statusMsg, "⚠ clipboard")
		if !copied && !clipFail {
			t.Fatalf("status = %q, want a 'copied <rel>' or '⚠ clipboard…' outcome", ym.statusMsg)
		}
		if copied {
			rel := strings.TrimPrefix(ym.statusMsg, "copied ")
			want := filepath.ToSlash(filepath.Join("sub", "main.go"))
			if rel != want {
				t.Errorf("copied rel = %q, want %q", rel, want)
			}
			if strings.Contains(rel, m.root) {
				t.Errorf("copied rel %q still carries the m.root prefix %q", rel, m.root)
			}
			if filepath.IsAbs(rel) {
				t.Errorf("copied rel %q is absolute, want project-relative", rel)
			}
		}
	})

	t.Run("synthetic .. is refused, never copies a useless dot", func(t *testing.T) {
		m := editorModel(t)
		selectEntry(t, &m, "..")
		nm, cmd := m.updateNormal(keyRune('y'))
		ym := nm.(model)
		if cmd != nil {
			t.Errorf("yank on .. returned a cmd, want nil")
		}
		if ym.statusMsg != "⚠ nothing to yank" {
			t.Errorf("yank on .. status = %q, want %q", ym.statusMsg, "⚠ nothing to yank")
		}
		// The crux: yanking ".." must NOT copy the resolved-to-root "." (useless in
		// chat). A "copied" status would mean the refusal failed.
		if strings.HasPrefix(ym.statusMsg, "copied") {
			t.Errorf("yank on .. copied something (%q); the parent-dir resolve to \".\" must be refused", ym.statusMsg)
		}
	})

	t.Run("a real directory IS yankable (unlike open-in-editor)", func(t *testing.T) {
		m := editorModel(t)
		selectEntry(t, &m, "child")
		nm, _ := m.updateNormal(keyRune('y'))
		ym := nm.(model)
		copied := strings.HasPrefix(ym.statusMsg, "copied ")
		clipFail := strings.HasPrefix(ym.statusMsg, "⚠ clipboard")
		if !copied && !clipFail {
			t.Fatalf("dir status = %q, want a 'copied <rel>' or '⚠ clipboard…' outcome (dirs are yankable)", ym.statusMsg)
		}
		if copied {
			rel := strings.TrimPrefix(ym.statusMsg, "copied ")
			want := filepath.ToSlash(filepath.Join("sub", "child"))
			if rel != want {
				t.Errorf("dir copied rel = %q, want %q", rel, want)
			}
		}
	})

	t.Run("empty listing surfaces nothing-selected", func(t *testing.T) {
		m := modelAt(t, t.TempDir(), 100, 30) // empty dir, no synthetic .. at root
		if len(m.entries) != 0 {
			t.Fatalf("setup: expected empty listing, got %v", entryNames(m))
		}
		nm, cmd := m.updateNormal(keyRune('y'))
		ym := nm.(model)
		if cmd != nil {
			t.Errorf("yank on empty listing returned a cmd, want nil")
		}
		if ym.statusMsg != "⚠ nothing selected" {
			t.Errorf("empty-listing status = %q, want %q", ym.statusMsg, "⚠ nothing selected")
		}
	})

	t.Run("acts only on focusList (mirrors the e/r/d cluster)", func(t *testing.T) {
		m := editorModel(t)
		selectEntry(t, &m, "main.go")
		m.focusPane = focusPreview
		nm, cmd := m.updateNormal(keyRune('y'))
		ym := nm.(model)
		if cmd != nil {
			t.Errorf("yank under focusPreview returned a cmd, want nil (focusList-only)")
		}
		if strings.HasPrefix(ym.statusMsg, "copied") {
			t.Errorf("yank under focusPreview copied %q, want a no-op", ym.statusMsg)
		}
	})
}

// TestYankInFullHelpMisc pins the discoverability surface (D6/FR4): the `y` binding
// lives in the Misc group of the `?` full-help — a clipboard utility, not a
// mutation — and fullHelp stays exactly 5 groups (renderHelpBody titles depend on
// it). Asserting at fullHelp() (the source of truth) is height-clamp-robust: the
// `?` overlay scrolls, so Misc rows aren't all at the unscrolled top.
func TestYankInFullHelpMisc(t *testing.T) {
	m := modelAt(t, t.TempDir(), 100, 30)
	groups := m.fullHelp()
	if len(groups) != 5 {
		t.Fatalf("fullHelp returned %d groups, want 5 (Yank must join an existing group, not add one)", len(groups))
	}
	misc := groups[4] // Navigation, Preview, Mutation, Modes, Misc
	found := false
	for _, b := range misc {
		if b.Help().Key == "y" && strings.Contains(b.Help().Desc, "yank") {
			found = true
		}
	}
	if !found {
		t.Errorf("Yank binding (y · yank …) not in the Misc full-help group; got %v", helpKeys(misc))
	}
}

// helpKeys lists the short key labels of a binding group (test diagnostic).
func helpKeys(bs []key.Binding) []string {
	out := make([]string, len(bs))
	for i, b := range bs {
		out[i] = b.Help().Key
	}
	return out
}

// TestYankDoesNotShadowDeleteConfirm is the regression guard for the mode-lane
// separation the keybind relies on: `y` is bound in modeNormal, but the
// modeConfirmDelete prompt (reached via `d`) reads "y"/"Y" as its own confirm rune
// in updateConfirmDelete — a DIFFERENT dispatch lane. Pressing `d` then `y` must
// still DELETE, not yank. (Same coexistence `d`→confirm→`y` already had.)
func TestYankDoesNotShadowDeleteConfirm(t *testing.T) {
	m := editorModel(t) // cwd = <root>/sub, holds main.go + child/
	selectEntry(t, &m, "main.go")
	target := filepath.Join(m.cwd, "main.go")

	// d → enter the confirm prompt.
	nm, _ := m.updateNormal(keyRune('d'))
	m = nm.(model)
	if m.mode != modeConfirmDelete {
		t.Fatalf("d did not open the delete-confirm prompt (mode=%v)", m.mode)
	}
	// y in the confirm lane must DELETE, not yank.
	nm2, _ := m.updateConfirmDelete(keyRune('y'))
	m = nm2.(model)
	if _, err := os.Stat(target); err == nil {
		t.Errorf("confirm-delete `y` did not delete %q (yank shadowed the confirm rune)", target)
	}
	if !strings.HasPrefix(m.statusMsg, "deleted ") {
		t.Errorf("after confirm `y` status = %q, want a 'deleted …' message", m.statusMsg)
	}
}
