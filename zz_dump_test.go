package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDumpFrames is the Level-4 (UI / visual) harness: it writes raw-ANSI frames
// of View() to $LAZYEXPLORER_DUMP_DIR so they can be rendered to images (freeze)
// and judged by eye / an agent against the design intent. It is gated on the env
// var so the normal `go test` run never touches the filesystem outside its temp
// dirs. Run it with:
//
//	LAZYEXPLORER_DUMP_DIR=/tmp/le-visual go test -run TestDumpFrames .
func TestDumpFrames(t *testing.T) {
	outDir := os.Getenv("LAZYEXPLORER_DUMP_DIR")
	if outDir == "" {
		t.Skip("set LAZYEXPLORER_DUMP_DIR to dump View() frames for visual inspection")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// In lipgloss/glamour v2 a style always renders its full truecolor ANSI;
	// color downsampling happens only at the program's output writer, not at
	// render time. So the dumped frames carry the real colors even though go
	// test's stdout is not a TTY — no global color-profile override is needed.

	dir := t.TempDir()
	src := "# Release Notes\n\nlazyexplorer now renders markdown **asynchronously**, so the\nUI never freezes on a big file.\n\n## Highlights\n\n- Instant raw-text placeholder\n- A `rendering…` status chip while glamour works\n- Stale renders discarded on fast navigation\n\n> Glance-friendly, beside your agent.\n"
	if err := os.WriteFile(filepath.Join(dir, "doc.md"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	m := modelAt(t, dir, 90, 24) // cursor on doc.md; placeholder shown, not styled
	m.renderStyle = "dark"       // the style main() resolves on a dark terminal

	// Frame 1: render in flight — raw-text placeholder + the "rendering" chip.
	m.pendingWidth = m.previewBodyWidth()
	if err := os.WriteFile(filepath.Join(outDir, "01-rendering.ansi"), []byte(m.View().Content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Frame 2: render landed — styled markdown, chip gone.
	m.pendingWidth = 0
	m.renderNow()
	if err := os.WriteFile(filepath.Join(outDir, "02-rendered.ansi"), []byte(m.View().Content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Frame 3: a source file syntax-highlighted by the code renderer (chroma).
	code := "package main\n\nimport \"fmt\"\n\n// greet prints a friendly hello.\nfunc greet(name string) string {\n\treturn fmt.Sprintf(\"hello, %s!\", name)\n}\n\nfunc main() {\n\tfmt.Println(greet(\"world\"))\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	m.reload() // re-read dir; main.go sorts before doc.md
	for i, e := range m.entries {
		if e.name == "main.go" {
			m.cursor = i
		}
	}
	m.refreshPreview()
	m.renderNow()
	if err := os.WriteFile(filepath.Join(outDir, "03-code.ansi"), []byte(m.View().Content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestDumpMiddleDividerFrames is the visual-verdict harness for
// docs/prd-middle-divider.md §6 checklist 17. It writes two ANSI frames to
// $LAZYEXPLORER_DUMP_DIR so an external tool (freeze / vhs) can render them
// to images and an agent (oh-my-claudecode:visual-verdict) can check them
// against the design intent:
//
//	Frame A — width=120, height=30, .md selected: rendered markdown fills the
//	          right pane; no border surrounds either pane; a single dim │
//	          separates the two panes with one space of padding on each side.
//	Frame B — width=60, height=24, plain file selected: the divider still
//	          shows 3 cols, panes don't overflow the terminal, content fits.
//
// Run with:
//
//	LAZYEXPLORER_DUMP_DIR=/tmp/le-divider go test -run TestDumpMiddleDividerFrames .
func TestDumpMiddleDividerFrames(t *testing.T) {
	outDir := os.Getenv("LAZYEXPLORER_DUMP_DIR")
	if outDir == "" {
		t.Skip("set LAZYEXPLORER_DUMP_DIR to dump View() frames for visual verdict")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Sample dir matches the borderless verdict shape: a markdown file (for
	// frame A's preview), a plain text file (for frame B), plus a folder so
	// the list pane shows a dir + file mix.
	dir := t.TempDir()
	md := "# Middle Divider\n\nlazyexplorer ships a single 3-col divider between\nthe **list pane** and the **preview pane** — no border\naround either pane.\n\n## Why\n\n- Two extra cols of content per pane\n- Two extra rows of body height\n- Wider drag-target for mouse users\n\n> Glance-friendly · simpler than superfile.\n"
	if err := os.WriteFile(filepath.Join(dir, "doc.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	plain := strings.Repeat("the quick brown fox jumps over the lazy dog\n", 30)
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte(plain), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Frame A: 120x30, markdown selected, render landed.
	mA := modelAt(t, dir, 120, 30)
	mA.renderStyle = "dark"
	for i, e := range mA.entries {
		if e.name == "doc.md" {
			mA.cursor = i
		}
	}
	mA.refreshPreview()
	mA.renderNow()
	if err := os.WriteFile(filepath.Join(outDir, "divider-A-md-120x30.ansi"), []byte(mA.View().Content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Frame B: 60x24, plain text file selected.
	mB := modelAt(t, dir, 60, 24)
	mB.renderStyle = "dark"
	for i, e := range mB.entries {
		if e.name == "notes.txt" {
			mB.cursor = i
		}
	}
	mB.refreshPreview()
	if err := os.WriteFile(filepath.Join(outDir, "divider-B-plain-60x24.ansi"), []byte(mB.View().Content), 0o644); err != nil {
		t.Fatal(err)
	}
}
