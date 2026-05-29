package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// TestLayoutHeaderGeometry pins the +1 header-row shift that this feature
// introduces: layout() reserves headerH rows at the top (firstRow == headerH),
// the body excludes BOTH the header (top) and the status row (bottom), and every
// Y-origin field (previewFirstRow in both orientations, dividerYStart in
// vertical) carries firstRow so the mouse hit-tests stay aligned. This is the
// purest assertion and fails first (firstRow=0, previewFirstRow=0 pre-change).
func TestLayoutHeaderGeometry(t *testing.T) {
	// Horizontal: width=100, height=30 → bodyH = 30-1-headerH = 28.
	mh := model{width: 100, height: 30, leftRatio: 0.38, tel: noopRecorder{}}
	gh := mh.layout()
	if gh.firstRow != headerH {
		t.Errorf("horizontal: firstRow = %d, want headerH=%d", gh.firstRow, headerH)
	}
	if want := 30 - 1 - headerH; gh.bodyH != want {
		t.Errorf("horizontal: bodyH = %d, want %d (height-1-headerH)", gh.bodyH, want)
	}
	if gh.previewFirstRow != headerH {
		t.Errorf("horizontal: previewFirstRow = %d, want headerH=%d (preview starts below header)",
			gh.previewFirstRow, headerH)
	}

	// Vertical: width=70, height=24, topRatio=0.33 → bodyH = 24-1-headerH.
	mv := model{width: 70, height: 24, leftRatio: 0.38, topRatio: 0.33, tel: noopRecorder{}}
	gv := mv.layout()
	if !gv.vertical {
		t.Fatalf("setup: not vertical at width=70")
	}
	if gv.firstRow != headerH {
		t.Errorf("vertical: firstRow = %d, want headerH=%d", gv.firstRow, headerH)
	}
	if want := 24 - 1 - headerH; gv.bodyH != want {
		t.Errorf("vertical: bodyH = %d, want %d", gv.bodyH, want)
	}
	// dividerYStart is the glyph row's screen Y: firstRow + topInner.
	if want := gv.firstRow + gv.topInner; gv.dividerYStart != want {
		t.Errorf("vertical: dividerYStart = %d, want firstRow+topInner=%d", gv.dividerYStart, want)
	}
	// previewFirstRow is firstRow + topInner + dividerHeight.
	if want := gv.firstRow + gv.topInner + dividerHeight; gv.previewFirstRow != want {
		t.Errorf("vertical: previewFirstRow = %d, want firstRow+topInner+dividerHeight=%d",
			gv.previewFirstRow, want)
	}
	// Geometry must still tile the body region exactly.
	if total := gv.topInner + dividerHeight + gv.bottomInner; total != gv.bodyH {
		t.Errorf("vertical: topInner(%d)+dividerHeight(%d)+bottomInner(%d) = %d, want bodyH=%d",
			gv.topInner, dividerHeight, gv.bottomInner, total, gv.bodyH)
	}
}

// TestFitPathRight drives the new left/middle-truncation helper. Unlike fitWidth
// (which truncates the RIGHT, hiding the current folder — the one thing the
// header exists to show), fitPathRight keeps the TAIL: the leading segments are
// dropped and replaced with a single "…", so the deepest folder always survives.
// Rune/CJK-aware: the result's display width never exceeds w.
func TestFitPathRight(t *testing.T) {
	cases := []struct {
		name string
		s    string
		w    int
		want string
	}{
		// Fits whole → unchanged.
		{"fits whole", "a/b/c", 5, "a/b/c"},
		{"exact fit", "abc", 3, "abc"},
		// Too wide → leading "…" + the tail that fits within w.
		{"drops head keeps tail", "a/very/deep/path", 8, "…ep/path"},
		// CJK: each wide rune is 2 cols. Budget after "…" is w-1=4 cols; "/认证"
		// is 5 cols (too wide), so the "/" drops too → "…认证" (1+4 = 5 cols).
		{"cjk width-aware", "项目/源码/认证", 5, "…认证"},
		// One more col of budget keeps the slash: w=6 → "…/认证" (1+5 = 6 cols).
		{"cjk keeps slash at w6", "项目/源码/认证", 6, "…/认证"},
		// Degenerate width.
		{"zero width", "anything", 0, ""},
		{"width one", "abc", 1, "…"},
	}
	for _, c := range cases {
		got := fitPathRight(c.s, c.w)
		if got != c.want {
			t.Errorf("%s: fitPathRight(%q, %d) = %q, want %q", c.name, c.s, c.w, got, c.want)
		}
		if lipgloss.Width(got) > c.w {
			t.Errorf("%s: fitPathRight(%q, %d) width %d exceeds %d (result %q)",
				c.name, c.s, c.w, lipgloss.Width(got), c.w, got)
		}
	}
}

// TestHeaderPath pins the header's content string (BEFORE truncation), mode-aware:
//   - modeNormal at root → the root's basename (relRoot==".").
//   - modeNormal one level down → "<root-base>/<rel-slash>".
//   - modeSearch / modeChanges → a mode label, never a stale cwd (the list there
//     is a flat root-relative result set, not a directory).
func TestHeaderPath(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "src", "auth")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	base := filepath.Base(root)

	// At root.
	m := modelAt(t, root, 100, 30)
	if got := m.headerPath(); got != base {
		t.Errorf("at root: headerPath() = %q, want root basename %q", got, base)
	}

	// One/two levels down (slash-form on all OS).
	m.cwd = sub
	if got, want := m.headerPath(), base+"/src/auth"; got != want {
		t.Errorf("below root: headerPath() = %q, want %q", got, want)
	}

	// Search mode → a label, not the cwd.
	m.mode = modeSearch
	if got := m.headerPath(); strings.Contains(got, "auth") {
		t.Errorf("modeSearch: headerPath() = %q, must not show stale cwd path", got)
	}
	if got := m.headerPath(); got == "" {
		t.Errorf("modeSearch: headerPath() empty, want a mode label")
	}

	// Changes mode → a label, not the cwd.
	m.mode = modeChanges
	if got := m.headerPath(); strings.Contains(got, "auth") {
		t.Errorf("modeChanges: headerPath() = %q, must not show stale cwd path", got)
	}
}

// TestViewHeaderRow asserts the rendered View() carries the header on row 0 and
// the body/status sit below it. A deep cwd shows the leading "…" (left-truncated
// tail). This is the end-to-end render contract.
func TestViewHeaderRow(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "src", "auth", "handlers", "internal")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deep, "x.txt"), []byte("y\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	const w = 20 // narrow enough that the deep "<base>/src/auth/handlers/internal" overflows
	m := modelAt(t, root, w, 20)
	m.cwd = deep
	m.reload()
	rows := strings.Split(m.View().Content, "\n")
	if len(rows) < 3 {
		t.Fatalf("View() has %d rows, want >=3 (header+body+status)", len(rows))
	}
	header := ansi.Strip(rows[0])
	// The deepest folder ("internal") must survive on the header row.
	if !strings.Contains(header, "internal") {
		t.Errorf("header row = %q, want it to contain the current folder \"internal\"", header)
	}
	// The full path overflows width 20 → the header is left-truncated with a
	// leading "…".
	if !strings.Contains(header, "…") {
		t.Errorf("header row = %q, want a leading \"…\" (deep path truncated)", header)
	}
	// The header must not exceed the screen width.
	if got := lipgloss.Width(rows[0]); got > w {
		t.Errorf("header row width %d exceeds screen width %d", got, w)
	}
}
