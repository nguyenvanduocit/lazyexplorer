package main

// Tests for the keymap registry, command palette, and help overlay
// (prd-keymap-and-command-palette). Key construction follows the focus_test
// convention: tea.KeyPressMsg{Code, Text} for printable keys, {Code, Mod} for
// modified keys, {Code: tea.KeyX} for named keys.

import (
	"os"
	"path/filepath"
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

// TestPaletteBodyRenders: the palette body shows the search prompt at its top
// and lists every command (name + description). This is the body-level
// contract; modal composition into View() is covered by
// TestModalRendersPaletteInView (modal_test.go).
func TestPaletteBodyRenders(t *testing.T) {
	m := modelAt(t, t.TempDir(), 120, 30)
	m, _ = press(t, m, tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	body := stripANSI(m.renderPaletteBody(56, 16))
	first := strings.TrimSpace(strings.Split(body, "\n")[0])
	if !strings.HasPrefix(first, ">") {
		t.Errorf("palette body row 0 should start with the search prompt '>'; got %q", first)
	}
	for _, name := range []string{"reload", "copy path", "cd", "quit"} {
		if !strings.Contains(body, name) {
			t.Errorf("palette body should list command %q; full:\n%s", name, body)
		}
	}
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
