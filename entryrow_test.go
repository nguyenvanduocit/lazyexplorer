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
	got := renderEntryRow(entry{name: "src", isDir: true}, nil, 20, false, true)
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
	got := renderEntryRow(entry{name: "..", isDir: true}, nil, 20, false, true)
	plain := ansi.Strip(got)
	if strings.Contains(plain, "../") {
		t.Errorf("\"..\" must NOT carry a trailing slash; got %q", plain)
	}
	if !strings.Contains(plain, "..") {
		t.Errorf("\"..\" name must appear; got %q", plain)
	}
}

// TestRenderEntryRowFileNoSizeWhenClean proves the byte size is gone (FR9): a
// file with no git indicator (clean / git mode off) renders just its name — no
// "1.2KB"-style number anywhere in the row.
func TestRenderEntryRowFileNoSizeWhenClean(t *testing.T) {
	got := renderEntryRow(entry{name: "main.go", isDir: false, size: 1234}, nil, 30, false, true)
	plain := ansi.Strip(got)
	if !strings.Contains(plain, "main.go") {
		t.Errorf("file name missing; got %q", plain)
	}
	if strings.Contains(plain, humanSize(1234)) || strings.Contains(plain, "KB") {
		t.Errorf("the byte size must NOT appear in a listing row (FR9); got %q", plain)
	}
}

// TestRenderEntryRowFileBadgeAndDelta pins an inactive modified file (FR1/FR2/
// D12): the badge "M" carries the modified color while the line delta is muted
// (dimStyle) so the badge stays the focal point — name stays in fileStyle.
func TestRenderEntryRowFileBadgeAndDelta(t *testing.T) {
	ind := &rowIndicator{badge: "M", color: gitColor(gitModified), delta: "+41 -3"}
	got := renderEntryRow(entry{name: "main.go", isDir: false}, ind, 30, false, true)
	plain := ansi.Strip(got)
	for _, want := range []string{"main.go", "M", "+41", "-3"} {
		if !strings.Contains(plain, want) {
			t.Errorf("row should contain %q; got %q", want, plain)
		}
	}
	// The delta "+41 -3" is rendered muted (dimStyle), not bright green/red — the
	// loud diffstat was visually too heavy beside the agent (D12).
	if !strings.Contains(got, leadingSGR(t, dimStyle)+"+41") {
		t.Errorf("the delta must be wrapped in dimStyle (muted); got %q", got)
	}
	// The badge carries the modified (amber) foreground.
	badgeSGR := leadingSGR(t, lipgloss.NewStyle().Foreground(gitColor(gitModified)))
	if !strings.Contains(got, badgeSGR+"M") {
		t.Errorf("badge \"M\" must carry the modified color SGR %q; got %q", badgeSGR, got)
	}
}

// TestRenderEntryRowFolderRollup: a folder with changes inside shows the dim ●
// roll-up marker (FR3/D4) at the right edge, name still in dirStyle.
func TestRenderEntryRowFolderRollup(t *testing.T) {
	ind := &rowIndicator{badge: rollupGlyph, color: colDim}
	got := renderEntryRow(entry{name: "src", isDir: true}, ind, 20, false, true)
	plain := ansi.Strip(got)
	if !strings.HasPrefix(plain, "src/") {
		t.Errorf("dir name should lead the row; got %q", plain)
	}
	if !strings.Contains(plain, rollupGlyph) {
		t.Errorf("a dirty folder should show the roll-up %q; got %q", rollupGlyph, plain)
	}
	if !strings.Contains(got, dimColorANSI(t)+rollupGlyph) {
		t.Errorf("the roll-up marker should be dim; got %q", got)
	}
}

// TestRenderEntryRowActiveFullWidthHighlight: an active row carries
// cursorActiveStyle's accent background and renders at exactly w columns
// (cursorActiveStyle.Width(w) pads the row so the highlight covers the whole
// pane width). The full-width accent background IS the cursor marker (no caret
// glyph). listFocused=true here exercises the focused-list accent path.
func TestRenderEntryRowActiveFullWidthHighlight(t *testing.T) {
	got := renderEntryRow(entry{name: "main.go", isDir: false, size: 100}, nil, 30, true, true)
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
	got := renderEntryRow(entry{name: "x.go", isDir: false, size: 1}, nil, 30, false, true)
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
	a := renderEntryRow(e, nil, 40, false, true)
	b := renderEntryRow(e, nil, 40, false, true)
	if a != b {
		t.Errorf("renderEntryRow not deterministic for same input:\n a=%q\n b=%q", a, b)
	}
}

// ----------------------------------------------------------------------------
// renderList integration — list pane now renders through renderEntryRow
// ----------------------------------------------------------------------------

// TestRenderListNoSizeOutsideRepo is the behaviour-level proof of FR8/FR9: a
// t.TempDir() is not a git repo, so the list pane shows plain names — no byte
// size (it used to), and no git badge. We use the live View() so this also
// guards against a regression in row width math.
func TestRenderListNoSizeOutsideRepo(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "main.go", strings.Repeat("x", 1234))
	mustMkdir(t, dir, "src")

	m := modelAt(t, dir, 100, 30)
	if m.git.repoRoot != "" {
		t.Skip("temp dir unexpectedly inside a git repo; FR8 path not exercised")
	}
	plain := ansi.Strip(m.View().Content)

	// The byte size must be gone entirely (FR9).
	if strings.Contains(plain, humanSize(1234)) || strings.Contains(plain, "KB") {
		t.Errorf("listing must not show a byte size; got:\n%s", plain)
	}
	// Names still render — file and dir (dir keeps its trailing "/").
	if !strings.Contains(plain, "main.go") {
		t.Errorf("file name should still show; got:\n%s", plain)
	}
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

// TestChooseIndicatorPriority pins the name > badge > delta drop order (D8/FR4):
// at a generous width the full "badge + delta" candidate wins; squeezed, the
// delta drops and the badge survives; squeezed further, nothing fits.
func TestChooseIndicatorPriority(t *testing.T) {
	ind := &rowIndicator{badge: "M", color: gitColor(gitModified), delta: "+41 -3"}
	const nameW = 8 // pretend the name occupies 8 cols

	// Wide: name(8) + 1 + "M +41 -3"(8) = 17 ≤ 20 → full candidate.
	if plain, _ := chooseIndicator(ind, nameW, 20); plain != "M +41 -3" {
		t.Errorf("wide pane should keep badge+delta; got %q", plain)
	}
	// Tight: full(8) won't fit (8+1+8=17 > 12) but badge(1) does (8+1+1=10 ≤ 12).
	if plain, _ := chooseIndicator(ind, nameW, 12); plain != "M" {
		t.Errorf("tight pane should drop the delta and keep the badge; got %q", plain)
	}
	// Narrower than name+badge → nothing fits, caller truncates the name.
	if plain, _ := chooseIndicator(ind, nameW, 9); plain != "" {
		t.Errorf("when even the badge can't fit, no indicator; got %q", plain)
	}
}

// TestRenderEntryRowActiveIndicatorPlain pins D11/FR6: the cursor row carries the
// indicator (so the selected file's change is still visible) but renders it PLAIN
// inside the accent highlight — the muted delta color does NOT leak onto the
// accent background. Row width stays exactly w.
func TestRenderEntryRowActiveIndicatorPlain(t *testing.T) {
	ind := &rowIndicator{badge: "M", color: gitColor(gitModified), delta: "+41 -3"}
	got := renderEntryRow(entry{name: "main.go", isDir: false}, ind, 30, true, true)

	if w := lipgloss.Width(got); w != 30 {
		t.Errorf("active row width = %d, want 30 (full pane highlight)", w)
	}
	if !strings.Contains(got, accentBgANSI(t)) {
		t.Errorf("active row should carry the accent bg; got %q", got)
	}
	if !strings.Contains(ansi.Strip(got), "+41") {
		t.Errorf("active row should still show the delta text; got %q", ansi.Strip(got))
	}
	// The muted delta color must NOT appear — the indicator is laid out plain on the
	// accent so it can't wash out (a colored delta over accent is the bug D11 avoids).
	if strings.Contains(got, leadingSGR(t, dimStyle)) {
		t.Errorf("active row indicator must be plain (no muted delta SGR); got %q", got)
	}
}

// ----------------------------------------------------------------------------
// helpers
// ----------------------------------------------------------------------------

// leadingSGR renders "X" with st and returns the leading SGR escape lipgloss
// emits before the content — the generic form of dirColorANSI/dimColorANSI, used
// to assert a substring carries a given style without hand-encoding the color.
func leadingSGR(t *testing.T, st lipgloss.Style) string {
	t.Helper()
	out := st.Render("X")
	idx := strings.Index(out, "X")
	if idx <= 0 {
		t.Fatalf("style.Render produced no leading SGR escape: %q", out)
	}
	return out[:idx]
}

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
