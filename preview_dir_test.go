package main

// Folder preview rendered through the shared renderEntryRow routine
// (docs/prd-consistent-file-listing.md §5.3-5.5, FR1/FR4/FR7). Together with
// previewclick_test.go these pin the cross-pane consistency contract.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestRefreshPreviewDirSetsPreviewState: selecting a directory in the list
// pane must populate previewEntries with that dir's items (no synthetic "..")
// and flip previewIsDir on, so renderPreview takes the folder branch and
// previewClick can map clicks against the same slice.
func TestRefreshPreviewDirSetsPreviewState(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, sub, "a.txt", "x")
	mustWrite(t, sub, "b.txt", "y")

	m := modelAt(t, root, 100, 30) // cursor 0 = "sub" (the only entry)
	if m.entries[m.cursor].name != "sub" {
		t.Fatalf("setup: cursor on %q, want \"sub\"", m.entries[m.cursor].name)
	}
	if !m.previewIsDir {
		t.Fatalf("after selecting dir, previewIsDir must be true")
	}
	if len(m.previewEntries) != 2 {
		t.Fatalf("previewEntries len = %d, want 2", len(m.previewEntries))
	}
	if m.previewEntries[0].name != "a.txt" || m.previewEntries[1].name != "b.txt" {
		t.Errorf("previewEntries should be alpha-sorted (no synthetic '..'); got %v",
			[]string{m.previewEntries[0].name, m.previewEntries[1].name})
	}
}

// TestRefreshPreviewFileResetsState: navigating to a file must turn
// previewIsDir off and clear previewEntries. Same reset hygiene as the rest
// of the preview state at the top of refreshPreview.
func TestRefreshPreviewFileResetsState(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "a.txt", "hello\n")

	m := modelAt(t, dir, 100, 30) // cursor 0 = a.txt (a file)
	if m.previewIsDir {
		t.Errorf("file preview must NOT flip previewIsDir on")
	}
	if m.previewEntries != nil {
		t.Errorf("file preview must clear previewEntries; got %v", m.previewEntries)
	}
}

// TestRefreshPreviewStateResetsOnDirToFile: switching the cursor from a dir
// to a file must clear previewEntries — stale dir entries leaking into a
// file preview would break the previewClick map (it would resolve clicks
// against the wrong folder's contents).
func TestRefreshPreviewStateResetsOnDirToFile(t *testing.T) {
	dir := t.TempDir()
	mustMkdir(t, dir, "src")
	mustWrite(t, dir, "z.txt", "x") // sorts after src/ (dirs first)

	m := modelAt(t, dir, 100, 30) // cursor 0 = src (dir)
	if !m.previewIsDir {
		t.Fatalf("setup: expected previewIsDir on dir selection")
	}

	// Move to the file row.
	for i, e := range m.entries {
		if e.name == "z.txt" {
			m.cursor = i
		}
	}
	m.refreshPreview()
	if m.previewIsDir {
		t.Errorf("after moving to file, previewIsDir must be off")
	}
	if m.previewEntries != nil {
		t.Errorf("after moving to file, previewEntries must be nil; got %v", m.previewEntries)
	}
}

// TestRenderPreviewFolderUsesRenderEntryRow is the BYTE-LEVEL cross-pane
// consistency proof of FR1: every line of the folder preview equals the
// output of renderEntryRow for that entry, at the preview's own body width,
// with active=false. If the two ever drift (someone formats a row by hand in
// renderPreview), this test fails immediately.
func TestRenderPreviewFolderUsesRenderEntryRow(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, sub, "main.go", strings.Repeat("x", 1234))
	mustMkdir(t, sub, "inner")

	m := modelAt(t, root, 100, 30) // cursor on sub → folder preview
	if !m.previewIsDir {
		t.Fatalf("setup: expected previewIsDir on dir selection")
	}

	w := m.previewBodyWidth()
	out := m.renderPreview(w)
	lines := strings.Split(out, "\n")
	if len(lines) != len(m.previewEntries) {
		t.Fatalf("renderPreview line count = %d, want %d (one per entry)\nout:\n%s",
			len(lines), len(m.previewEntries), out)
	}
	for i, e := range m.previewEntries {
		want := renderEntryRow(e, m.indicatorFor(m.previewDirPath, e), w, false, false)
		if lines[i] != want {
			t.Errorf("preview line %d not byte-equal to renderEntryRow:\n got:  %q\n want: %q",
				i, lines[i], want)
		}
	}
}

// TestRenderPreviewEmptyFolderPlaceholder: empty folder shows a dim
// placeholder. FR7 demands the placeholder feels the same as the list pane's
// "(empty directory)"; PRD §5.3 picks "(empty folder)" — both are dimStyle.
func TestRenderPreviewEmptyFolderPlaceholder(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, root, "empty")

	m := modelAt(t, root, 100, 30) // cursor on empty
	if !m.previewIsDir {
		t.Fatalf("setup: expected previewIsDir on dir selection")
	}
	if len(m.previewEntries) != 0 {
		t.Fatalf("setup: empty folder should have 0 entries, got %d", len(m.previewEntries))
	}

	plain := ansi.Strip(m.renderPreview(m.previewBodyWidth()))
	if !strings.Contains(plain, "empty") {
		t.Errorf("empty folder preview should mention 'empty'; got %q", plain)
	}
	// The placeholder must NOT carry the old emoji-formatted text.
	if strings.Contains(plain, "📁") {
		t.Errorf("empty folder preview must not carry emoji from the old format; got %q", plain)
	}
}

// TestPreviewLenSwitchesByMode: previewLen returns the right collection's
// length so scroll math works for both kinds of preview. previewScroll and
// scrollPreview lean on this — without it, scrolling a folder listing would
// clamp against m.preview's length (zero in the dir case) and never move.
func TestPreviewLenSwitchesByMode(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		mustWrite(t, sub, "f"+string(rune('a'+i))+".txt", "x")
	}

	m := modelAt(t, root, 100, 30) // cursor on sub → folder preview
	if !m.previewIsDir {
		t.Fatalf("setup: expected previewIsDir on dir")
	}
	if got := m.previewLen(); got != 5 {
		t.Errorf("previewLen on dir = %d, want 5 (entries count)", got)
	}

	// Switch to a file: previewLen must reflect m.preview lines, NOT entries.
	mustWrite(t, root, "z.txt", "line1\nline2\nline3\n")
	m.reload()
	for i, e := range m.entries {
		if e.name == "z.txt" {
			m.cursor = i
			m.refreshPreview()
		}
	}
	if m.previewIsDir {
		t.Fatalf("after switching to file, previewIsDir must be off")
	}
	if got, want := m.previewLen(), len(m.preview); got != want {
		t.Errorf("previewLen on file = %d, want %d (m.preview lines)", got, want)
	}
}

// TestRenderPreviewFolderScrollsConsistently: when the folder listing is
// taller than the panel and scrolled, the rendered lines are the entries
// starting from previewTop — same offset behaviour TestPreviewClickScrolled-
// MapsWithOffset relies on for the click side. This proves render + click
// share the same scroll math (a click on row 0 of a scrolled-by-10 listing
// resolves to entries[10], not entries[0]).
func TestRenderPreviewFolderScrollsConsistently(t *testing.T) {
	root := t.TempDir()
	big := filepath.Join(root, "big")
	if err := os.Mkdir(big, 0o755); err != nil {
		t.Fatal(err)
	}
	// 40 files, alpha-sorted: index 10 = file10.txt.
	for i := 0; i < 40; i++ {
		name := "file"
		if i < 10 {
			name += "0"
		}
		name += strconv.Itoa(i) + ".txt"
		mustWrite(t, big, name, "x")
	}

	m := modelAt(t, root, 100, 30) // cursor on big
	if !m.previewIsDir {
		t.Fatalf("setup: expected previewIsDir on dir")
	}
	m.scrollPreview(10) // previewTop → 10 (clamped to maxTop = 40-bodyH)

	w := m.previewBodyWidth()
	out := m.renderPreview(w)
	firstLine := strings.SplitN(out, "\n", 2)[0]
	want := renderEntryRow(m.previewEntries[10], m.indicatorFor(m.previewDirPath, m.previewEntries[10]), w, false, false)
	if firstLine != want {
		t.Errorf("first visible line after scrollPreview(10) does not match entries[10]:\n got:  %q\n want: %q",
			firstLine, want)
	}
}
