package main

// Tests for the keymap registry, command palette, and help overlay
// (prd-keymap-and-command-palette). Key construction follows the focus_test
// convention: tea.KeyPressMsg{Code, Text} for printable keys, {Code, Mod} for
// modified keys, {Code: tea.KeyX} for named keys.

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// press routes a key through Update's mode dispatch and returns the new model.
func press(t *testing.T, m model, msg tea.KeyPressMsg) (model, tea.Cmd) {
	t.Helper()
	nm, cmd := m.Update(msg)
	return nm.(model), cmd
}

func typeStr(t *testing.T, m model, s string) model {
	t.Helper()
	for _, r := range s {
		m, _ = press(t, m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return m
}

// TestKeyMatchContract pins the bubbles/v2 key API: Matches compares a key
// press's String() against a binding's key codes (PRD T1).
func TestKeyMatchContract(t *testing.T) {
	km := defaultKeyMap()
	if !key.Matches(tea.KeyPressMsg{Code: 'j', Text: "j"}, km.MoveDown) {
		t.Error("'j' should match MoveDown")
	}
	if !key.Matches(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl}, km.CommandPalette) {
		t.Error("ctrl+p should match CommandPalette")
	}
	if !key.Matches(tea.KeyPressMsg{Code: '?', Text: "?"}, km.FullHelp) {
		t.Error("'?' should match FullHelp")
	}
	if key.Matches(tea.KeyPressMsg{Code: 'x', Text: "x"}, km.MoveDown) {
		t.Error("'x' should NOT match MoveDown")
	}
}

// TestCommandPaletteOpenClose: ctrl+p opens the palette; esc closes it; ctrl+p
// again toggles it closed (FR5/FR6).
func TestCommandPaletteOpenClose(t *testing.T) {
	m := modelAt(t, t.TempDir(), 100, 30)
	ctrlP := tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl}

	m, _ = press(t, m, ctrlP)
	if m.mode != modeCommandPalette {
		t.Fatalf("ctrl+p: mode = %v, want modeCommandPalette", m.mode)
	}
	if len(m.paletteFiltered) != len(defaultCommands()) {
		t.Errorf("palette opened with %d commands, want %d", len(m.paletteFiltered), len(defaultCommands()))
	}

	m, _ = press(t, m, tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.mode != modeNormal {
		t.Errorf("esc: mode = %v, want modeNormal", m.mode)
	}

	m, _ = press(t, m, ctrlP)
	m, _ = press(t, m, ctrlP) // toggle
	if m.mode != modeNormal {
		t.Errorf("ctrl+p toggle: mode = %v, want modeNormal", m.mode)
	}
}

// TestCommandPaletteFilter: typing narrows the list by substring (D8); a
// no-match query yields an empty list.
func TestCommandPaletteFilter(t *testing.T) {
	m := modelAt(t, t.TempDir(), 100, 30)
	m, _ = press(t, m, tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})

	m = typeStr(t, m, "cd")
	if len(m.paletteFiltered) != 1 || m.paletteFiltered[0].Name != "cd" {
		t.Errorf("filter 'cd' → %v, want [cd]", commandNames(m.paletteFiltered))
	}

	// Reset and try a no-match query.
	m, _ = press(t, m, tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl}) // close
	m, _ = press(t, m, tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl}) // reopen
	m = typeStr(t, m, "zzz")
	if len(m.paletteFiltered) != 0 {
		t.Errorf("filter 'zzz' → %v, want empty", commandNames(m.paletteFiltered))
	}
}

// TestCommandPaletteNav: j/k move the cursor within the filtered list, clamped.
func TestCommandPaletteNav(t *testing.T) {
	m := modelAt(t, t.TempDir(), 100, 30)
	m, _ = press(t, m, tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	if m.paletteCursor != 0 {
		t.Fatalf("palette opens with cursor 0, got %d", m.paletteCursor)
	}
	m, _ = press(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	if m.paletteCursor != 1 {
		t.Errorf("down: cursor = %d, want 1", m.paletteCursor)
	}
	m, _ = press(t, m, tea.KeyPressMsg{Code: tea.KeyUp})
	if m.paletteCursor != 0 {
		t.Errorf("up: cursor = %d, want 0", m.paletteCursor)
	}
	m, _ = press(t, m, tea.KeyPressMsg{Code: tea.KeyUp}) // clamp at top
	if m.paletteCursor != 0 {
		t.Errorf("up at top: cursor = %d, want 0 (clamped)", m.paletteCursor)
	}
}

// TestCommandReload runs the reload command via the palette and confirms the
// status line and a return to normal mode.
func TestCommandReload(t *testing.T) {
	m := modelAt(t, t.TempDir(), 100, 30)
	m, _ = press(t, m, tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	m = typeStr(t, m, "reload")
	m, _ = press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.mode != modeNormal {
		t.Errorf("after reload: mode = %v, want modeNormal", m.mode)
	}
	if m.statusMsg != "reloaded" {
		t.Errorf("after reload: statusMsg = %q, want \"reloaded\"", m.statusMsg)
	}
}

// TestCommandCdValid: cd into a real subdirectory changes cwd and closes the
// palette (FR10).
func TestCommandCdValid(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, root, "sub")
	m := modelAt(t, root, 100, 30)

	m, _ = press(t, m, tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	m = typeStr(t, m, "cd")
	m, _ = press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter}) // NeedsArg → stage 1
	if m.paletteStage != 1 {
		t.Fatalf("cd: paletteStage = %d, want 1 (arg input)", m.paletteStage)
	}
	m = typeStr(t, m, "sub")
	m, _ = press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.mode != modeNormal {
		t.Errorf("cd sub: mode = %v, want modeNormal", m.mode)
	}
	if m.cwd != filepath.Join(root, "sub") {
		t.Errorf("cd sub: cwd = %q, want %q", m.cwd, filepath.Join(root, "sub"))
	}
}

// TestCommandCdRejects covers the cd failure branches: a file target, a path
// outside the jail root, and a non-existent path. Each keeps the palette open
// in stage 1 with a ⚠ status (FR10).
func TestCommandCdRejects(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, "file.txt", "x")
	cmds := defaultCommands()
	var cd Command
	for _, c := range cmds {
		if c.Name == "cd" {
			cd = c
		}
	}

	cases := []struct {
		name, arg, wantPrefix string
	}{
		{"file target", "file.txt", "⚠ not a directory"},
		{"outside root", "/", "⚠ blocked"},
		{"not found", "nope", "⚠ not found"},
	}
	for _, c := range cases {
		m := modelAt(t, root, 100, 30)
		cd.Run(&m, c.arg)
		if !strings.HasPrefix(m.statusMsg, c.wantPrefix) {
			t.Errorf("%s: status = %q, want prefix %q", c.name, m.statusMsg, c.wantPrefix)
		}
		if m.cwd != root {
			t.Errorf("%s: cwd changed to %q, want unchanged %q", c.name, m.cwd, root)
		}
	}
}

// TestCommandOpenInEditor drives the palette command's Run closure directly — the
// discoverability twin of the `e` key (PRD D9) is a SECOND entry point that duplicates
// the keypath guard, so each of its branches needs its own coverage (a future fix to
// the key guard could miss this copy). Mirrors TestCommandCdRejects: pull the Command
// from defaultCommands() and call Run, asserting cmd-nilness + status per branch.
func TestCommandOpenInEditor(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "vim") // a runnable editor so the only nil-cmd cause is the guard

	var openCmd Command
	for _, c := range defaultCommands() {
		if c.Name == "open in editor" {
			openCmd = c
		}
	}
	if openCmd.Run == nil {
		t.Fatal(`"open in editor" command not found in defaultCommands()`)
	}

	t.Run("real file returns an exec cmd, no warning", func(t *testing.T) {
		m := editorModel(t) // cwd = sub/, holds child/ + main.go + synthetic ..
		selectEntry(t, &m, "main.go")
		cmd := openCmd.Run(&m, "")
		if cmd == nil {
			t.Errorf("file: Run returned nil cmd, want a tea.ExecProcess cmd")
		}
		if strings.HasPrefix(m.statusMsg, "⚠") {
			t.Errorf("file: Run set a warning status %q, want none", m.statusMsg)
		}
	})

	t.Run("directory is refused", func(t *testing.T) {
		m := editorModel(t)
		selectEntry(t, &m, "child")
		cmd := openCmd.Run(&m, "")
		if cmd != nil {
			t.Errorf("dir: Run returned a cmd, want nil")
		}
		if m.statusMsg != "⚠ not a file" {
			t.Errorf("dir: status = %q, want %q", m.statusMsg, "⚠ not a file")
		}
	})

	t.Run("synthetic .. is refused", func(t *testing.T) {
		m := editorModel(t)
		selectEntry(t, &m, "..")
		cmd := openCmd.Run(&m, "")
		if cmd != nil {
			t.Errorf("..: Run returned a cmd, want nil")
		}
		if m.statusMsg != "⚠ not a file" {
			t.Errorf("..: status = %q, want %q", m.statusMsg, "⚠ not a file")
		}
	})

	t.Run("empty listing is refused", func(t *testing.T) {
		m := modelAt(t, t.TempDir(), 100, 30) // empty dir, no synthetic .. at root
		if len(m.entries) != 0 {
			t.Fatalf("setup: expected empty listing, got %v", entryNames(m))
		}
		cmd := openCmd.Run(&m, "")
		if cmd != nil {
			t.Errorf("empty: Run returned a cmd, want nil")
		}
		if m.statusMsg != "⚠ nothing selected" {
			t.Errorf("empty: status = %q, want %q", m.statusMsg, "⚠ nothing selected")
		}
	})

	t.Run("no editor set surfaces a status, no exec", func(t *testing.T) {
		t.Setenv("VISUAL", "")
		t.Setenv("EDITOR", "")
		m := editorModel(t)
		selectEntry(t, &m, "main.go")
		cmd := openCmd.Run(&m, "")
		if cmd != nil {
			t.Errorf("no-editor: Run returned a cmd, want nil")
		}
		if !strings.HasPrefix(m.statusMsg, "⚠") {
			t.Errorf("no-editor: status = %q, want a ⚠ warning", m.statusMsg)
		}
	})
}

// TestCommandQuit: the quit command returns tea.Quit (yields tea.QuitMsg).
func TestCommandQuit(t *testing.T) {
	m := modelAt(t, t.TempDir(), 100, 30)
	m, _ = press(t, m, tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	m = typeStr(t, m, "quit")
	_, cmd := press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("quit command returned nil cmd, want tea.Quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("quit cmd yielded %T, want tea.QuitMsg", cmd())
	}
}

// TestHelpOpenScrollClose: ? opens help, j/k scroll within bounds, esc closes.
func TestHelpOpenScrollClose(t *testing.T) {
	m := modelAt(t, t.TempDir(), 100, 12) // short height so help body overflows
	m, _ = press(t, m, tea.KeyPressMsg{Code: '?', Text: "?"})
	if m.mode != modeHelp {
		t.Fatalf("?: mode = %v, want modeHelp", m.mode)
	}
	if m.helpTop != 0 {
		t.Fatalf("help opens at top, helpTop = %d", m.helpTop)
	}
	m, _ = press(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	if m.helpTop != 1 {
		t.Errorf("help down: helpTop = %d, want 1", m.helpTop)
	}
	m, _ = press(t, m, tea.KeyPressMsg{Code: tea.KeyUp})
	if m.helpTop != 0 {
		t.Errorf("help up: helpTop = %d, want 0", m.helpTop)
	}
	m, _ = press(t, m, tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.mode != modeNormal {
		t.Errorf("help esc: mode = %v, want modeNormal", m.mode)
	}
}

// TestHelpScrollClamps: j-spam never pushes helpTop past the content end.
func TestHelpScrollClamps(t *testing.T) {
	m := modelAt(t, t.TempDir(), 100, 12)
	m, _ = press(t, m, tea.KeyPressMsg{Code: '?', Text: "?"})
	for i := 0; i < 200; i++ {
		m, _ = press(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	}
	_, bodyH := m.previewScroll()
	maxTop := max(0, m.helpLineCount()-bodyH)
	if m.helpTop != maxTop {
		t.Errorf("help j-spam: helpTop = %d, want clamped to %d", m.helpTop, maxTop)
	}
}

// TestHelpLineCountMatchesRenderedBody pins the invariant the palette.go comment
// PROMISES but TestHelpScrollClamps cannot pin (it reads helpLineCount on both
// sides of its assertion, so a desync moves both together and slips through):
// helpLineCount() must equal the line count renderHelpBody actually emits — groups
// + the native-selection footnote (view.go:1030). If it overcounts, the scroll
// clamp overshoots into blanks; if it undercounts, the footnote is clamped
// off-screen. The render height is a large CONSTANT, deliberately NOT helpLineCount()
// — sizing by the quantity under test would re-clip end to an undercounted height and
// pass tautologically, reproducing the very gap this guards (the undercount direction
// is exactly the footnote-clamped-off case). 1000 dwarfs the ~30-line body, so
// renderHelpBody never clips and the rendered line count is the true emitted count.
// It also pins FR12: the footnote string ("Selecting text" / "hold Shift") must
// actually render — it is appended AFTER the fullHelp group loop, so a fullHelp-only
// assertion (TestCopyContentInFullHelpMisc) is structurally blind to it.
func TestHelpLineCountMatchesRenderedBody(t *testing.T) {
	m := modelAt(t, t.TempDir(), 100, 30)
	// helpTop is 0 by default (help not yet opened/scrolled), so start=0 and the
	// large h makes renderHelpBody emit every line: end == len(lines).
	const tall = 1000
	rendered := m.renderHelpBody(80, tall)
	got := len(strings.Split(rendered, "\n"))
	if want := m.helpLineCount(); got != want {
		t.Errorf("renderHelpBody emitted %d lines, helpLineCount() = %d: the scroll-clamp invariant is broken (clamp would overshoot blanks or clip the footnote off-screen)", got, want)
	}
	if !strings.Contains(rendered, "Selecting text") {
		t.Errorf("renderHelpBody output missing the 'Selecting text' footnote title (FR12); got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "hold Shift") {
		t.Errorf("renderHelpBody output missing the 'hold Shift' native-selection note (FR12); got:\n%s", rendered)
	}
}

// TestHelpNoteFitsModalWidth pins FR12 readability: the native-selection footnote
// must read cleanly inside the modal, with NO content clipped. The modal's inner
// text area is modalTargetCols (56) minus the modalBoxStyle frame (4) = 52 cols,
// and modalSize hands renderHelpBody exactly that at any terminal ≥ 60 wide. A bare
// `lipgloss.Width(line) <= 52` check is a false-green proxy: fitWidth already
// truncates to fit, so a clipped line still satisfies it. The real discriminator is
// LOSS — fitWidth appends "…" (U+2026) only when it had to clip — so assert no note
// line at the true modal width carries that marker. Each authored line must already
// be <= 52 display cols so the modifier list (D2/D11) survives intact.
func TestHelpNoteFitsModalWidth(t *testing.T) {
	m := modelAt(t, t.TempDir(), 100, 30)
	innerW, _ := m.modalSize()
	for i, l := range helpNoteLines(innerW) {
		if strings.Contains(l, "…") {
			t.Errorf("helpNoteLines[%d] clipped at modal inner width %d (loses content, breaks FR12): %q", i, innerW, l)
		}
	}
}

// TestHelpQuitClosesNotExits: q inside help closes the overlay rather than
// quitting the app (surprise avoidance).
func TestHelpQuitClosesNotExits(t *testing.T) {
	m := modelAt(t, t.TempDir(), 100, 30)
	m, _ = press(t, m, tea.KeyPressMsg{Code: '?', Text: "?"})
	m, cmd := press(t, m, tea.KeyPressMsg{Code: 'q', Text: "q"})
	if m.mode != modeNormal {
		t.Errorf("q in help: mode = %v, want modeNormal (closed)", m.mode)
	}
	if cmd != nil {
		t.Error("q in help should not return a quit cmd")
	}
}

// TestShortHelpByFocus: the status-bar bindings differ by focus, sourced from
// the keymap (FR14).
func TestShortHelpByFocus(t *testing.T) {
	m := modelAt(t, t.TempDir(), 100, 30)

	m.focusPane = focusList
	if got := renderShortHelp(m.shortHelp()); !strings.Contains(got, "move down") {
		t.Errorf("focusList shortHelp = %q, want a 'move down' binding", got)
	}
	m.focusPane = focusPreview
	if got := renderShortHelp(m.shortHelp()); !strings.Contains(got, "scroll down") {
		t.Errorf("focusPreview shortHelp = %q, want a 'scroll down' binding", got)
	}
}

// TestFullHelpGroups: fullHelp returns five non-empty groups (Navigation,
// Preview, Mutation, Modes, Misc) — matching the titles in renderHelpBody.
func TestFullHelpGroups(t *testing.T) {
	m := modelAt(t, t.TempDir(), 100, 30)
	groups := m.fullHelp()
	if len(groups) != 5 {
		t.Fatalf("fullHelp returned %d groups, want 5", len(groups))
	}
	for i, g := range groups {
		if len(g) == 0 {
			t.Errorf("fullHelp group %d is empty", i)
		}
	}
}

// TestResolvePath covers ~, relative, and absolute expansion.
func TestResolvePath(t *testing.T) {
	home, _ := os.UserHomeDir()
	cases := []struct {
		cwd, in, want string
	}{
		{"/a/b", "c", "/a/b/c"},
		{"/a/b", "../c", "/a/c"},
		{"/a/b", "/x/y", "/x/y"},
		{"/a/b", "~", home},
	}
	for _, c := range cases {
		got, err := resolvePath(c.cwd, c.in)
		if err != nil {
			t.Errorf("resolvePath(%q,%q) error: %v", c.cwd, c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("resolvePath(%q,%q) = %q, want %q", c.cwd, c.in, got, c.want)
		}
	}
	if _, err := resolvePath("/a", ""); err == nil {
		t.Error("resolvePath with empty input should error")
	}
}

// TestSelectedAbsPathDotDot: the synthetic ".." entry resolves to the real
// parent (jail-clamped), never the literal "<cwd>/.." string (FR9).
func TestSelectedAbsPathDotDot(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, root, "sub")
	m := modelAt(t, root, 100, 30) // root == cwd → no ".." yet
	m.cwd = filepath.Join(root, "sub")
	m.reload() // cwd is now below root → ".." prepended

	if m.entries[0].name != ".." {
		t.Fatalf("setup: entries[0] = %q, want \"..\"", m.entries[0].name)
	}
	m.cursor = 0
	if got := m.selectedAbsPath(); got != root {
		t.Errorf("selectedAbsPath on \"..\" = %q, want parent %q", got, root)
	}
}

// TestPaletteBodyRenders: the palette body leads with the "Commands" title row
// and the "›" input prompt, then lists every command (name + description). This
// is the body-level contract; modal composition into View() is covered by
// TestModalRendersPaletteInView (modal_test.go).
func TestPaletteBodyRenders(t *testing.T) {
	m := modelAt(t, t.TempDir(), 120, 30)
	m, _ = press(t, m, tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	body := stripANSI(m.renderPaletteBody(56, 16))
	rows := strings.Split(body, "\n")
	if !strings.Contains(rows[0], "Commands") {
		t.Errorf("palette body row 0 should be the 'Commands' title; got %q", rows[0])
	}
	if !strings.Contains(rows[1], "›") {
		t.Errorf("palette body row 1 should be the '›' input prompt; got %q", rows[1])
	}
	for _, name := range []string{"reload", "copy absolute path", "copy relative path", "copy file content", "open in editor", "cd", "quit"} {
		if !strings.Contains(body, name) {
			t.Errorf("palette body should list command %q; full:\n%s", name, body)
		}
	}
}

// TestCommandCopyRelative drives the palette twin's Run closure directly — the
// discoverability sibling of the `y` key (PRD D7). Both route through the SAME
// yankRelPath helper, so this asserts the relative-path outcome (clipboard-agnostic,
// T2 discipline) and proves the embedded rel string carries NO m.root prefix.
func TestCommandCopyRelative(t *testing.T) {
	var relCmd Command
	for _, c := range defaultCommands() {
		if c.Name == "copy relative path" {
			relCmd = c
		}
	}
	if relCmd.Run == nil {
		t.Fatal(`"copy relative path" command not found in defaultCommands()`)
	}

	t.Run("real file copies a project-relative slash-path", func(t *testing.T) {
		m := editorModel(t) // cwd = <root>/sub, holds main.go
		selectEntry(t, &m, "main.go")
		cmd := relCmd.Run(&m, "")
		if cmd != nil {
			t.Errorf("copy relative: Run returned a cmd, want nil")
		}
		copied := strings.HasPrefix(m.statusMsg, "copied ")
		clipFail := strings.HasPrefix(m.statusMsg, "⚠ clipboard")
		if !copied && !clipFail {
			t.Fatalf("status = %q, want a 'copied <rel>' or '⚠ clipboard…' outcome", m.statusMsg)
		}
		if copied {
			rel := strings.TrimPrefix(m.statusMsg, "copied ")
			if strings.Contains(rel, m.root) {
				t.Errorf("copied rel %q still carries the m.root prefix %q", rel, m.root)
			}
			if rel != filepath.ToSlash(filepath.Join("sub", "main.go")) {
				t.Errorf("copied rel = %q, want %q", rel, filepath.ToSlash(filepath.Join("sub", "main.go")))
			}
		}
	})

	t.Run("empty listing is refused", func(t *testing.T) {
		m := modelAt(t, t.TempDir(), 100, 30)
		if len(m.entries) != 0 {
			t.Fatalf("setup: expected empty listing, got %v", entryNames(m))
		}
		cmd := relCmd.Run(&m, "")
		if cmd != nil {
			t.Errorf("empty: Run returned a cmd, want nil")
		}
		if m.statusMsg != "⚠ nothing selected" {
			t.Errorf("empty: status = %q, want %q", m.statusMsg, "⚠ nothing selected")
		}
	})

	t.Run("the absolute twin is still offered alongside it", func(t *testing.T) {
		names := commandNames(defaultCommands())
		if !slices.Contains(names, "copy absolute path") {
			t.Errorf("the absolute-path capability was dropped; commands = %v", names)
		}
	})
}

// TestCommandCopyFileContent drives the palette twin's Run closure directly — the
// discoverability sibling of the `Y` key (PRD D9/FR10). Both route through the SAME
// copyContent helper, so this asserts the copy outcome via the telemetry oracle
// (clipboard-agnostic) and proves the success status does NOT start with "⚠" (the
// palette success-detect keys on that prefix, palette.go).
func TestCommandCopyFileContent(t *testing.T) {
	var copyCmd Command
	for _, c := range defaultCommands() {
		if c.Name == "copy file content" {
			copyCmd = c
		}
	}
	if copyCmd.Run == nil {
		t.Fatal(`"copy file content" command not found in defaultCommands()`)
	}

	t.Run("text file copies its whole content, records once", func(t *testing.T) {
		rec := &fieldRecorder{}
		m := editorModel(t) // cwd = <root>/sub, holds main.go
		m.tel = rec
		selectEntry(t, &m, "main.go")
		want, _ := os.ReadFile(filepath.Join(m.previewBaseDir(), "main.go"))

		cmd := copyCmd.Run(&m, "")
		if cmd != nil {
			t.Errorf("copy file content: Run returned a cmd, want nil")
		}
		fields, recorded := rec.last("action.copy_content")
		if !recorded {
			t.Fatalf("palette twin must record action.copy_content; events=%v", rec.names())
		}
		if n, _ := fields["bytes"].(int); n != len(want) {
			t.Errorf("recorded bytes = %d, want %d", n, len(want))
		}
		count := 0
		for _, n := range rec.names() {
			if n == "action.copy_content" {
				count++
			}
		}
		if count != 1 {
			t.Errorf("palette twin recorded %d times, want exactly 1 (shared code path, D9)", count)
		}
		// On clipboard success the status must NOT start with "⚠" (palette detects
		// success by the absence of that prefix).
		if strings.HasPrefix(m.statusMsg, "copied ") && strings.HasPrefix(m.statusMsg, "⚠") {
			t.Errorf("success status %q must not start with ⚠", m.statusMsg)
		}
	})

	t.Run("empty listing is refused", func(t *testing.T) {
		m := modelAt(t, t.TempDir(), 100, 30)
		if len(m.entries) != 0 {
			t.Fatalf("setup: expected empty listing, got %v", entryNames(m))
		}
		cmd := copyCmd.Run(&m, "")
		if cmd != nil {
			t.Errorf("empty: Run returned a cmd, want nil")
		}
		if m.statusMsg != "⚠ nothing selected" {
			t.Errorf("empty: status = %q, want %q", m.statusMsg, "⚠ nothing selected")
		}
	})
}

// TestHelpRendersInView: with help open, View() composites a floating modal —
// rounded border, the first group title visible at the box top, and the
// background list pane still showing through. The modal box is height-clamped,
// so lower groups (Mutation/Modes/Misc) reach view via the helpTop scroll and
// are not all visible at the unscrolled top — that scroll is covered by
// TestHelpScrollClamps. The full group set is asserted at the body level by
// TestFullHelpGroups.
func TestHelpRendersInView(t *testing.T) {
	m := modelAt(t, t.TempDir(), 120, 40)
	m, _ = press(t, m, tea.KeyPressMsg{Code: '?', Text: "?"})
	out := ansi.Strip(m.View().Content)
	if !strings.Contains(out, "╭") {
		t.Errorf("help View should composite a modal border; full:\n%s", out)
	}
	if !strings.Contains(out, "Navigation") {
		t.Errorf("help modal should show the first group title 'Navigation'; full:\n%s", out)
	}
	// Background list pane still visible behind/around the floating box.
	if !strings.Contains(out, "(empty directory)") {
		t.Errorf("background list pane not visible behind the help modal; full:\n%s", out)
	}
}

func commandNames(cs []Command) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Name
	}
	return out
}
