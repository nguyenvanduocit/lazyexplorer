package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
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

// TestDumpPaletteModalFrame is the visual-verdict harness for the crush-style
// command-palette restyle (docs/prd-keymap-and-command-palette.md). It writes
// two ANSI frames to $LAZYEXPLORER_DUMP_DIR so an external tool (freeze / vhs)
// can render them to images and an agent (oh-my-claudecode:visual-verdict) can
// check them against the design intent:
//
//	Frame wide   — 90x28: the floating box centered over the list/preview, a
//	               "Commands" title with the accent→dim ╱ rule, a plain "› "
//	               input (no prompt bar), and the muted command rows with the
//	               cursor row a full-width accent bar.
//	Frame narrow — 60x24: the same modal at the narrow horizontal-mode width;
//	               title rule + rows still fit, nothing overflows or wraps.
//
// Run with:
//
//	LAZYEXPLORER_DUMP_DIR=/tmp/le-palette go test -run TestDumpPaletteModalFrame .
func TestDumpPaletteModalFrame(t *testing.T) {
	outDir := os.Getenv("LAZYEXPLORER_DUMP_DIR")
	if outDir == "" {
		t.Skip("set LAZYEXPLORER_DUMP_DIR to dump View() frames for visual verdict")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// A small mix so the background panes have content behind the modal.
	dir := t.TempDir()
	for _, name := range []string{"main.go", "README.md", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}

	for _, f := range []struct {
		name string
		w, h int
	}{
		{"palette-wide-90x28", 90, 28},
		{"palette-narrow-60x24", 60, 24},
	} {
		m := modelAt(t, dir, f.w, f.h)
		m.renderStyle = "dark"
		m.enterCommandPalette() // mode=palette, full command list, empty query
		if err := os.WriteFile(filepath.Join(outDir, f.name+".ansi"), []byte(m.View().Content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestDumpOpenInEditorFrames is the visual-verdict harness for
// docs/prd-open-in-editor.md §6 checklist 5. It writes three ANSI frames to
// $LAZYEXPLORER_DUMP_DIR for freeze → PNG → agent verdict:
//
//	fullhelp-mutation-90x32 — the `?` overlay scrolled to the Mutation group,
//	                    the CANONICAL visible reference: it must render
//	                    rename · delete · open in editor, the new `e` row
//	                    aligned with its siblings.
//	footer-90x24      — the file list focused. `e` lives in the footer keyhint's
//	                    binding source (shortHelp), but at 90 cols the trailing
//	                    hints clip — as ctrl+p/?/q already do pre-this-change.
//	                    The complete, always-visible reference is the `?` overlay
//	                    above; this frame just confirms the footer still reads
//	                    cleanly and nothing earlier in it broke.
//	no-editor-90x24   — pressing e with no $VISUAL/$EDITOR shows the
//	                    "⚠ set $VISUAL or $EDITOR …" status, no exec.
//
// Run with:
//
//	LAZYEXPLORER_DUMP_DIR=/tmp/le-editor go test -run TestDumpOpenInEditorFrames .
func TestDumpOpenInEditorFrames(t *testing.T) {
	outDir := os.Getenv("LAZYEXPLORER_DUMP_DIR")
	if outDir == "" {
		t.Skip("set LAZYEXPLORER_DUMP_DIR to dump View() frames for visual verdict")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	for _, name := range []string{"main.go", "model.go", "README.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("package main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Full-help overlay scrolled so the Mutation group (rename · delete · open in
	// editor) is in the modal's visible window — the new `e` row shown in context.
	mHelp := modelAt(t, dir, 90, 32)
	mHelp.renderStyle = "dark"
	mHelp.enterHelp()
	// Scroll so the Mutation group (index 2) sits at the top of the modal window.
	// renderHelpBody emits, per group, one title line + one row per binding + one
	// blank separator — so the Mutation group's first line is the summed height of
	// Navigation (group 0) and Preview (group 1). Computed from fullHelp() so it
	// stays correct if a group gains/loses a binding.
	groups := mHelp.fullHelp()
	mutationTop := 0
	for gi := 0; gi < 2; gi++ {
		mutationTop += 1 + len(groups[gi]) + 1
	}
	mHelp.helpTop = mutationTop
	if err := os.WriteFile(filepath.Join(outDir, "fullhelp-mutation-90x32.ansi"), []byte(mHelp.View().Content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Footer: list focused, cursor on a file — confirms the footer still reads
	// cleanly (the `e` hint clips at 90 cols, like ctrl+p/?/q already did).
	mFooter := modelAt(t, dir, 90, 24)
	mFooter.renderStyle = "dark"
	if err := os.WriteFile(filepath.Join(outDir, "footer-90x24.ansi"), []byte(mFooter.View().Content), 0o644); err != nil {
		t.Fatal(err)
	}

	// No-editor status: press e with both env unset (no exec, just the warning).
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	mStatus := modelAt(t, dir, 90, 24)
	mStatus.renderStyle = "dark"
	var tm tea.Model = mStatus
	tm, _ = tm.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	mStatus = tm.(model)
	if err := os.WriteFile(filepath.Join(outDir, "no-editor-90x24.ansi"), []byte(mStatus.View().Content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestDumpCopyContentHelpFrames is the visual-verdict harness for
// docs/prd-preview-copy.md T8. It writes ANSI frames to $LAZYEXPLORER_DUMP_DIR for
// freeze → PNG → agent verdict on the `?` full-help overlay:
//
//	fullhelp-misc-90x40 — the overlay scrolled so the Misc group (yank rel path ·
//	                    copy file content · quit) AND the "Selecting text" footnote
//	                    are in the modal's visible window. The new `Y copy file
//	                    content` row must align with its siblings, and the footnote
//	                    must read cleanly: "Y copies the whole file. To select a
//	                    visible span, hold Shift (Option on iTerm2/macOS …) then drag".
//
// Run with:
//
//	LAZYEXPLORER_DUMP_DIR=/tmp/le-copy go test -run TestDumpCopyContentHelpFrames .
func TestDumpCopyContentHelpFrames(t *testing.T) {
	outDir := os.Getenv("LAZYEXPLORER_DUMP_DIR")
	if outDir == "" {
		t.Skip("set LAZYEXPLORER_DUMP_DIR to dump View() frames for visual verdict")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	for _, name := range []string{"main.go", "model.go", "README.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("package main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// A tall overlay scrolled to the tail so the Misc group + the native-selection
	// footnote both sit in the modal window. Scroll target computed from fullHelp() +
	// helpNoteLines so it stays correct if a group gains/loses a binding: put the
	// Misc-group title at the top of the visible window.
	mHelp := modelAt(t, dir, 90, 40)
	mHelp.renderStyle = "dark"
	mHelp.enterHelp()
	groups := mHelp.fullHelp()
	miscTop := 0
	for gi := 0; gi < len(groups)-1; gi++ { // every group before Misc (the last)
		miscTop += 1 + len(groups[gi]) + 1
	}
	mHelp.helpTop = miscTop
	if err := os.WriteFile(filepath.Join(outDir, "fullhelp-misc-90x40.ansi"), []byte(mHelp.View().Content), 0o644); err != nil {
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
//	Frame B — width=80, height=24, plain file selected: at the minimum
//	          horizontal-mode width (= widthBreakpoint, the responsive trigger
//	          sits below this), the 3-col divider still renders and panes
//	          don't overflow.
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

	// Frame B: 80x24, plain text file selected. 80 = widthBreakpoint, the
	// minimum horizontal-mode width post-responsive layout (any narrower flips
	// to 1-col stacked — exercised separately in TestDumpResponsiveFrames).
	mB := modelAt(t, dir, 80, 24)
	mB.renderStyle = "dark"
	for i, e := range mB.entries {
		if e.name == "notes.txt" {
			mB.cursor = i
		}
	}
	mB.refreshPreview()
	if err := os.WriteFile(filepath.Join(outDir, "divider-B-plain-80x24.ansi"), []byte(mB.View().Content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestDumpResponsiveFrames is the visual-verdict harness for
// docs/prd-responsive-layout.md §6 checklist 12. It writes two ANSI frames to
// $LAZYEXPLORER_DUMP_DIR so an external tool (freeze / vhs) can render them
// to images and an agent (oh-my-claudecode:visual-verdict) can check them
// against the design intent:
//
//	Frame H — width=120, height=30, .md selected: 2-col side-by-side, list
//	          on the left, 3-col " │ " divider, rendered markdown on the
//	          right; status bar at the last row.
//	Frame V — width=70, height=30, .md selected: 1-col stacked, list pane on
//	          top (rows = topInner ≈ 9 at default topRatio=0.33), 1-row "─"
//	          divider strip across the full width, rendered markdown preview
//	          below filling the rest; status bar at the last row.
//
// Both frames share the same fixture (markdown + plain text + sub-folder) so
// only the terminal size drives the layout difference — the visual verdict
// can focus on "did the orientation flip correctly" without confounding it
// with content variance.
//
// Run with:
//
//	LAZYEXPLORER_DUMP_DIR=/tmp/le-responsive go test -run TestDumpResponsiveFrames .
func TestDumpResponsiveFrames(t *testing.T) {
	outDir := os.Getenv("LAZYEXPLORER_DUMP_DIR")
	if outDir == "" {
		t.Skip("set LAZYEXPLORER_DUMP_DIR to dump View() frames for visual verdict")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Shared fixture: a markdown file is the most discriminating preview for
	// both orientations (width reflow shows up clearly in glamour's wrapping).
	dir := t.TempDir()
	md := "# Responsive Layout\n\nlazyexplorer adapts the pane layout to the\nterminal width: side-by-side when there is room,\nstacked when the user splits the screen narrow.\n\n## How\n\n- Width >= 80 → 2-col side-by-side\n- Width < 80 → 1-col stacked (list above, preview below)\n- The divider drag-target works on both axes\n\n> No keybind, no mode, no config — the layout follows the screen.\n"
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

	// Frame H: 120x30, horizontal 2-col, markdown selected + rendered.
	mH := modelAt(t, dir, 120, 30)
	mH.renderStyle = "dark"
	for i, e := range mH.entries {
		if e.name == "doc.md" {
			mH.cursor = i
		}
	}
	mH.refreshPreview()
	mH.renderNow()
	if err := os.WriteFile(filepath.Join(outDir, "responsive-H-md-120x30.ansi"), []byte(mH.View().Content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Frame V: 70x30, vertical 1-col stacked, markdown selected + rendered.
	// At width=70 < widthBreakpoint=80, layout flips to vertical.
	mV := modelAt(t, dir, 70, 30)
	mV.renderStyle = "dark"
	for i, e := range mV.entries {
		if e.name == "doc.md" {
			mV.cursor = i
		}
	}
	mV.refreshPreview()
	mV.renderNow()
	if err := os.WriteFile(filepath.Join(outDir, "responsive-V-md-70x30.ansi"), []byte(mV.View().Content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestDumpChangesViewFrames is the visual-verdict harness for the changed-only
// aggregate view (docs/prd-changed-only-view.md §6 checklist 10). It writes ANSI
// frames to $LAZYEXPLORER_DUMP_DIR so freeze/vhs can render them to images and an
// agent (oh-my-claudecode:visual-verdict) can judge:
//
//	changes-90x30   — the aggregate list: rows are "<badge> <root-rel path> <delta>",
//	                  the colored badge + muted delta flush-right, paths flush-left;
//	                  the right pane previews the diff of the selected change; the
//	                  status bar shows move/open/back hints (no rename/delete).
//	changes-diff-90x30 — cursor on a modified file; preview shows its diff hunk.
//
// Run with:
//
//	LAZYEXPLORER_DUMP_DIR=/tmp/le-changes go test -run TestDumpChangesViewFrames .
func TestDumpChangesViewFrames(t *testing.T) {
	outDir := os.Getenv("LAZYEXPLORER_DUMP_DIR")
	if outDir == "" {
		t.Skip("set LAZYEXPLORER_DUMP_DIR to dump View() frames for visual verdict")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}

	repo := t.TempDir()
	gitExec(t, repo, "init")
	mustWrite(t, repo, "README.md", "# Project\n")
	mustMkdir(t, repo, "src")
	mustMkdir(t, repo, filepath.Join("src", "handlers"))
	mustWrite(t, filepath.Join(repo, "src"), "app.go", "package src\n")
	mustWrite(t, filepath.Join(repo, "src", "handlers"), "auth.go", "package handlers\n")
	gitExec(t, repo, "add", ".")
	gitExec(t, repo, "commit", "-m", "init")
	// Spread changes across dirs: modify two, add an untracked deep file.
	mustWrite(t, repo, "README.md", "# Project\n\nnow with docs\nand more\n")
	mustWrite(t, filepath.Join(repo, "src"), "app.go", "package src\n\nfunc App() {}\nfunc Run() {}\n")
	mustWrite(t, filepath.Join(repo, "src", "handlers"), "auth.go", "package handlers\n\nfunc Login() {}\n")
	mustWrite(t, repo, "scratch.tmp", "untracked line one\nuntracked line two\n")

	m := modelAt(t, repo, 90, 30)
	m.renderStyle = "dark"
	m.diffOn = true
	if m.git.repoRoot == "" {
		m.git.repoRoot = detectRepoRoot(m.root)
		m.gitRootPrefix = repoRelPrefix(m.git.repoRoot, m.root)
	}
	state, _ := collectGitState(m.git.repoRoot, nil)
	var tm tea.Model = m
	tm, _ = tm.Update(gitRefreshedMsg{gen: m.gitGen, state: state})
	tm, _ = tm.Update(tea.KeyPressMsg{Code: 'c', Text: "c"}) // open the changed-only view
	m = tm.(model)
	m.renderNow()
	if err := os.WriteFile(filepath.Join(outDir, "changes-90x30.ansi"), []byte(m.View().Content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Move the cursor onto the nested modified file so the preview shows its diff.
	for i, e := range m.entries {
		if e.name == "src/handlers/auth.go" {
			m.cursor = i
		}
	}
	m.refreshPreview()
	m.pendingWidth = 0
	m.renderNow()
	if err := os.WriteFile(filepath.Join(outDir, "changes-diff-90x30.ansi"), []byte(m.View().Content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestDumpCwdHeaderFrames is the visual-verdict harness for the always-visible
// path header (docs/prd-cwd-path-header.md §6 checklist 8). It writes ANSI
// frames to $LAZYEXPLORER_DUMP_DIR so freeze/vhs can render them to images and
// an agent (oh-my-claudecode:visual-verdict) can judge against the design intent:
//
//	header-root-90x24    — at the jail root: row 0 shows the root basename, accent
//	                       foreground, NO border, NO background fill (must read as a
//	                       floating label on the terminal, not a panel / double-frame);
//	                       the list/divider/preview body starts at row 1, status last.
//	header-deep-90x24    — a deep cwd that overflows the width: the header is
//	                       left-truncated with a leading "…" and the CURRENT folder
//	                       (the tail) survives; body still aligned below.
//	header-vertical-60x20 — 1-col stacked (width < 80): header row 0 spans full
//	                       width above the stacked list / "─" divider / preview;
//	                       everything shifted down by exactly one row.
//
// Run with:
//
//	LAZYEXPLORER_DUMP_DIR=/tmp/le-header go test -run TestDumpCwdHeaderFrames .
func TestDumpCwdHeaderFrames(t *testing.T) {
	outDir := os.Getenv("LAZYEXPLORER_DUMP_DIR")
	if outDir == "" {
		t.Skip("set LAZYEXPLORER_DUMP_DIR to dump View() frames for visual verdict")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// A deep tree so the header has a path to truncate, plus a couple of files
	// so the panes have content behind the header.
	dir := t.TempDir()
	deep := filepath.Join(dir, "src", "auth", "handlers", "internal")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, deep, "login.go", "package handlers\n\nfunc Login() {}\n")
	mustWrite(t, deep, "logout.go", "package handlers\n")
	mustWrite(t, dir, "README.md", "# Project\n")

	// Frame: at root — header shows the root basename.
	mRoot := modelAt(t, dir, 90, 24)
	mRoot.renderStyle = "dark"
	if err := os.WriteFile(filepath.Join(outDir, "header-root-90x24.ansi"), []byte(mRoot.View().Content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Frame: deep cwd — header left-truncates, the tail folder survives.
	mDeep := modelAt(t, dir, 90, 24)
	mDeep.renderStyle = "dark"
	mDeep.cwd = deep
	mDeep.reload()
	mDeep.renderNow()
	if err := os.WriteFile(filepath.Join(outDir, "header-deep-90x24.ansi"), []byte(mDeep.View().Content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Frame: vertical (1-col stacked) — header spans full width above the stack.
	mVert := modelAt(t, dir, 60, 20)
	mVert.renderStyle = "dark"
	mVert.cwd = deep
	mVert.reload()
	mVert.renderNow()
	if err := os.WriteFile(filepath.Join(outDir, "header-vertical-60x20.ansi"), []byte(mVert.View().Content), 0o644); err != nil {
		t.Fatal(err)
	}
}
