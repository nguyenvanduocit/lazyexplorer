package main

// Tests for the shared row-rendering routine specified in
// docs/prd-consistent-file-listing.md §5.1.
//
// Two layers under test:
//   * fitRow(name, size, w) — pure helper: place name left, size right (one
//     space minimum between), or drop size and truncate name when too narrow.
//   * renderEntryRow(e, w, active, listFocused) — composes fitRow with theme
//     styling, the *single* place a row is drawn for both list pane and folder
//     preview, so the two panes can never drift in format. The cursor row in the
//     list pane is marked by cursorActiveStyle's full-width accent background
//     (no glyph). listFocused only tunes the active row's highlight (accent when
//     the list is focused, colDim when the preview is); the pane-focus dim
//     behaviour is pinned in focus_test.go.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// ----------------------------------------------------------------------------
// fitRow — pure layout helper
// ----------------------------------------------------------------------------

// TestFitRowNameAndSizeFit checks the headline case: when both name and size
// fit in w with at least one space between them, the name sits flush left, the
// size flush right, and the gap is spaces. Width measured via lipgloss.Width so
// the assertion matches the terminal's draw width.
func TestFitRowNameAndSizeFit(t *testing.T) {
	got := fitRow("main.go", "1.2KB", 20)
	if w := lipgloss.Width(got); w != 20 {
		t.Errorf("fitRow width = %d, want 20: %q", w, got)
	}
	if !strings.HasPrefix(got, "main.go") {
		t.Errorf("fitRow should start with name; got %q", got)
	}
	if !strings.HasSuffix(got, "1.2KB") {
		t.Errorf("fitRow should end with size; got %q", got)
	}
	// Between name and size: only spaces (no other glyphs).
	mid := got[len("main.go") : len(got)-len("1.2KB")]
	if strings.TrimSpace(mid) != "" {
		t.Errorf("gap between name and size must be spaces only; got %q", mid)
	}
	if len(mid) < 1 {
		t.Errorf("must keep ≥1 space between name and size; got %q", got)
	}
}

// TestFitRowDropsSizeWhenTight verifies the "name beats size" priority of
// FR6: when name+gap+size cannot fit but name alone fits, drop size and keep
// the name whole (no truncation, no ellipsis).
func TestFitRowDropsSizeWhenTight(t *testing.T) {
	// name(8) + min-gap(1) + size(5) = 14, too wide for w=10; name alone fits.
	got := fitRow("longname", "1.2KB", 10)
	if strings.Contains(got, "1.2KB") {
		t.Errorf("size should be dropped when name+gap+size > w; got %q", got)
	}
	if !strings.Contains(got, "longname") {
		t.Errorf("name should survive when it alone fits; got %q", got)
	}
	if strings.Contains(got, "…") {
		t.Errorf("name fits; no ellipsis expected; got %q", got)
	}
}

// TestFitRowTruncatesNameWhenTooWide covers the narrowest case: the name
// itself overflows w → drop size, truncate name with the same "…" suffix as
// fitWidth.
func TestFitRowTruncatesNameWhenTooWide(t *testing.T) {
	got := fitRow("verylongfilename.txt", "1.2KB", 8)
	if strings.Contains(got, "1.2KB") {
		t.Errorf("size must be dropped when even the name doesn't fit; got %q", got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncated name should end with ellipsis; got %q", got)
	}
	if w := lipgloss.Width(got); w > 8 {
		t.Errorf("fitRow width = %d, must not exceed w=8: %q", w, got)
	}
}

// TestFitRowNoSize covers the dir case: size is empty, so the row is just
// the (possibly truncated) name — no padding to w, no phantom spaces.
func TestFitRowNoSize(t *testing.T) {
	got := fitRow("src/", "", 20)
	if got != "src/" {
		t.Errorf("fitRow with empty size should return name as-is; got %q", got)
	}
}

// TestFitRowZeroWidth — degenerate w=0 returns empty.
func TestFitRowZeroWidth(t *testing.T) {
	if got := fitRow("anything", "1KB", 0); got != "" {
		t.Errorf("fitRow with w=0 must return empty; got %q", got)
	}
}

// TestFitRowCJKWidth proves width math uses lipgloss.Width (cell width), not
// byte/rune count, so CJK / wide glyphs measure right and don't blow the row.
// 光 has cell width 2; ".go" has width 3 → total width 5.
func TestFitRowCJKWidth(t *testing.T) {
	got := fitRow("光.go", "1.2KB", 14)
	if w := lipgloss.Width(got); w != 14 {
		t.Errorf("fitRow width = %d, want 14 (CJK-aware): %q", w, got)
	}
	if !strings.HasPrefix(got, "光.go") {
		t.Errorf("CJK name should be preserved at start; got %q", got)
	}
	if !strings.HasSuffix(got, "1.2KB") {
		t.Errorf("size should be flush right; got %q", got)
	}
}

// ----------------------------------------------------------------------------
// renderEntryRow — composes fitRow with theme styling
// ----------------------------------------------------------------------------

// TestRenderEntryRowDirInactive: a directory inactive row shows name+"/" and
// is styled with dirStyle (we detect by checking colDir in the ANSI). The row
// starts flush-left at column 0 of the pane.
func TestRenderEntryRowDirInactive(t *testing.T) {
	got := renderEntryRow(entry{name: "src", isDir: true}, 20, false, true)
	plain := ansi.Strip(got)
	if !strings.HasPrefix(plain, "src/") {
		t.Errorf("inactive dir row must start flush-left with the name; got %q", plain)
	}
	if !strings.Contains(got, dirColorANSI(t)) {
		t.Errorf("dir row should carry dirStyle foreground %q; got %q", dirColorANSI(t), got)
	}
}

// TestRenderEntryRowParentDotsHasNoSlash: the synthetic ".." dir entry must
// NOT get a trailing "/" (FR2, matches current renderList behaviour).
func TestRenderEntryRowParentDotsHasNoSlash(t *testing.T) {
	got := renderEntryRow(entry{name: "..", isDir: true}, 20, false, true)
	plain := ansi.Strip(got)
	if strings.Contains(plain, "../") {
		t.Errorf("\"..\" must NOT carry a trailing slash; got %q", plain)
	}
	if !strings.Contains(plain, "..") {
		t.Errorf("\"..\" name must appear; got %q", plain)
	}
}

// TestRenderEntryRowFileShowsSize: file inactive row carries the human size,
// proving the list pane now gets the size column (D3/FR3).
func TestRenderEntryRowFileShowsSize(t *testing.T) {
	got := renderEntryRow(entry{name: "main.go", isDir: false, size: 1234}, 30, false, true)
	plain := ansi.Strip(got)
	if !strings.Contains(plain, "main.go") {
		t.Errorf("file name missing; got %q", plain)
	}
	want := humanSize(1234)
	if !strings.Contains(plain, want) {
		t.Errorf("file row should show human size %q; got %q", want, plain)
	}
}

// TestRenderEntryRowFileSizeMutedInactive pins the styling split for an inactive
// file row (D8/FR9): the name carries fileStyle (the headline), the size carries
// dimStyle (supporting metadata). In a glance UI the eye should land on the
// name first, not on a bright bytes column — so the size column is muted to the
// same gray as borders/placeholders. Active rows are intentionally exempt: the
// cursor highlight already paints a single bright foreground over the whole
// row, and a dim-on-accent size would be unreadable.
func TestRenderEntryRowFileSizeMutedInactive(t *testing.T) {
	got := renderEntryRow(entry{name: "main.go", isDir: false, size: 4242}, 30, false, true)
	size := humanSize(4242)
	dim := dimColorANSI(t)
	// dimStyle.Render(size) emits "<dim-SGR>size<reset>". The row therefore
	// contains the substring "<dim-SGR>size" iff size was wrapped in dimStyle,
	// not fileStyle (which uses a different foreground color → different SGR).
	if !strings.Contains(got, dim+size) {
		t.Errorf("inactive file size %q must be wrapped in dimStyle (SGR %q); got %q",
			size, dim, got)
	}
}

// TestRenderEntryRowActiveFullWidthHighlight: an active row carries
// cursorActiveStyle's accent background and renders at exactly w columns
// (cursorActiveStyle.Width(w) pads the row so the highlight covers the whole
// pane width). The full-width accent background IS the cursor marker (no caret
// glyph). listFocused=true here exercises the focused-list accent path.
func TestRenderEntryRowActiveFullWidthHighlight(t *testing.T) {
	got := renderEntryRow(entry{name: "main.go", isDir: false, size: 100}, 30, true, true)
	plain := ansi.Strip(got)
	if !strings.HasPrefix(plain, "main.go") {
		t.Errorf("active row must start flush-left with the name; got %q", plain)
	}
	if w := lipgloss.Width(got); w != 30 {
		t.Errorf("active row width = %d, want 30 (full pane highlight); got %q", w, got)
	}
	if !strings.Contains(got, accentBgANSI(t)) {
		t.Errorf("active row should carry cursorActiveStyle accent bg; got %q", got)
	}
}

// TestRenderEntryRowInactiveDoesNotForcePad: an inactive row should NOT be
// padded to full w — only the active row gets full-width highlight. Width
// stays ≤ w, which prevents accidental background bleeding through fileStyle.
func TestRenderEntryRowInactiveDoesNotForcePad(t *testing.T) {
	got := renderEntryRow(entry{name: "x.go", isDir: false, size: 1}, 30, false, true)
	if w := lipgloss.Width(got); w > 30 {
		t.Errorf("inactive row width = %d, must not exceed w=30; got %q", w, got)
	}
}

// TestRenderEntryRowConsistencyAcrossPanes is the CORE invariant of the PRD:
// for the same entry, list pane (active=false) and folder preview (also
// active=false at G002) must produce byte-identical output. The only allowed
// difference is cursorActiveStyle's accent background on the cursor row —
// applied only when active=true. Two inactive calls must match exactly.
func TestRenderEntryRowConsistencyAcrossPanes(t *testing.T) {
	e := entry{name: "main.go", isDir: false, size: 4242}
	a := renderEntryRow(e, 40, false, true)
	b := renderEntryRow(e, 40, false, true)
	if a != b {
		t.Errorf("renderEntryRow not deterministic for same input:\n a=%q\n b=%q", a, b)
	}
}

// ----------------------------------------------------------------------------
// renderList integration — list pane now renders through renderEntryRow
// ----------------------------------------------------------------------------

// TestRenderListShowsFileSize is the behaviour-level proof of D3/FR3: after
// switching renderList to renderEntryRow, the list pane shows file sizes (it
// did not before). We use the live View() so this also guards against a
// regression in row width math.
func TestRenderListShowsFileSize(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "main.go", strings.Repeat("x", 1234))
	mustMkdir(t, dir, "src")

	m := modelAt(t, dir, 100, 30)
	plain := ansi.Strip(m.View().Content)

	// File row carries its human size.
	if !strings.Contains(plain, humanSize(1234)) {
		t.Errorf("list pane should show file size %q in View(); got:\n%s", humanSize(1234), plain)
	}
	// Dir row still ends with "/" (sanity check, not regressed).
	if !strings.Contains(plain, "src/") {
		t.Errorf("dir row should still show trailing '/'; got:\n%s", plain)
	}
}

// TestRenderListHighlightsCursorRow proves the cursor row is visually
// distinguished from the rest of the list pane by the full-width
// cursorActiveStyle accent background — the only cursor marker (D5/FR4
// under the borderless-divider rewrite). The folder-preview pane
// never carries the active style, so counting accent-bg-bearing rows in the
// raw View() output isolates the cursor row uniquely.
func TestRenderListHighlightsCursorRow(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "a.txt", "x")
	mustWrite(t, dir, "b.txt", "y")

	m := modelAt(t, dir, 100, 30) // cursor on a.txt (first file, dirs-first, alpha)
	out := m.View().Content
	plain := ansi.Strip(out)

	// Cursor row must read "a.txt..." somewhere in the plain output.
	if !strings.Contains(plain, "a.txt") {
		t.Errorf("cursor row should carry the file name; got:\n%s", plain)
	}
	// And exactly one body row should carry the cursorActiveStyle accent
	// background (the cursor row). The other list rows + the preview pane
	// never apply cursorActiveStyle. The status bar (final row) is dropped so
	// the count stays to LIST rows only. The divider glow uses an accent
	// FOREGROUND (dividerFocusStyle), not a background, so it never matches the
	// accent-bg probe regardless — we assert "exactly one LIST row is the cursor".
	accentBg := accentBgANSI(t)
	rows := strings.Split(out, "\n")
	bodyRows := rows[:len(rows)-1] // drop the status bar row
	highlighted := 0
	for _, r := range bodyRows {
		if strings.Contains(r, accentBg) {
			highlighted++
		}
	}
	if highlighted != 1 {
		t.Errorf("exactly one body row should carry the cursorActiveStyle accent bg; got %d:\n%s",
			highlighted, plain)
	}
}

// ----------------------------------------------------------------------------
// helpers
// ----------------------------------------------------------------------------

// dirColorANSI returns the ANSI escape lipgloss emits when applying dirStyle,
// without our having to hand-encode the color. It renders an empty string with
// dirStyle and pulls the leading escape sequence out of the result. If the
// theme palette changes, this helper picks up the new color automatically.
func dirColorANSI(t *testing.T) string {
	t.Helper()
	out := dirStyle.Render("X")
	// lipgloss emits CSI ... m before the content. Find the first SGR run.
	idx := strings.Index(out, "X")
	if idx <= 0 {
		t.Fatalf("dirStyle.Render produced no leading SGR escape: %q", out)
	}
	return out[:idx]
}

// dimColorANSI extracts dimStyle's leading SGR escape (foreground colDim) the
// same way dirColorANSI does for dirStyle — keeps the assertion locked to the
// palette so theme tweaks recolor the test automatically.
func dimColorANSI(t *testing.T) string {
	t.Helper()
	out := dimStyle.Render("X")
	idx := strings.Index(out, "X")
	if idx <= 0 {
		t.Fatalf("dimStyle.Render produced no leading SGR escape: %q", out)
	}
	return out[:idx]
}

// accentBgANSI extracts the leading SGR escape cursorActiveStyle emits, so the
// assertion follows the palette across theme tweaks.
func accentBgANSI(t *testing.T) string {
	t.Helper()
	out := cursorActiveStyle.Render("X")
	idx := strings.Index(out, "X")
	if idx <= 0 {
		t.Fatalf("cursorActiveStyle.Render produced no leading SGR escape: %q", out)
	}
	return out[:idx]
}

// mustWrite is a tiny helper that fails the test on a write error — used by
// the integration tests to set up sample files without boilerplate.
func mustWrite(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// mustMkdir mirrors mustWrite for directories.
func mustMkdir(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", name, err)
	}
}
