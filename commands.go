package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Command is one row in the command palette. Run is invoked on Enter when this
// Command is selected; it mutates m in place (status message, cwd, …) and returns
// a tea.Cmd that may be nil. The tagged signature lets a future Command dispatch
// async work without changing the palette dispatch site. NeedsArg=true sends the
// palette into a second stage that collects a text argument (only `cd` uses it).
type Command struct {
	Name        string // displayed + filtered against (substring, case-insensitive)
	Description string // shown next to the name in the palette list
	NeedsArg    bool   // true → Enter opens the arg-input stage instead of running
	Run         func(m *model, arg string) tea.Cmd
}

// errClipboardUnsupported is returned by writeClipboard when no clipboard helper
// is available for the current OS (no pbcopy / xclip / wl-copy).
var errClipboardUnsupported = errors.New("no clipboard helper found (need pbcopy, xclip, or wl-copy)")

// errNoEditor is returned by editorCommand when neither $VISUAL nor $EDITOR names
// a runnable editor. We refuse to guess (no vi/nano fallback): on a beside-an-agent
// box $EDITOR is set, and dropping a non-vi user into vi is the rage-quit.
var errNoEditor = errors.New("set $VISUAL or $EDITOR to open files in your editor")

// editorCommand builds the *exec.Cmd that opens absPath in the user's editor,
// preferring $VISUAL then $EDITOR (getenv is injected so tests set env without
// mutating process state — same seam as split.go's buildCmd). The editor string is
// split on whitespace via strings.Fields so flags survive (`code --wait`,
// `emacsclient -t`) and absPath becomes a SEPARATE trailing argv token — no shell,
// so a path with spaces is injection-safe (mirrors split.go's argv ethos).
//
// A whitespace-only var (e.g. EDITOR="   ") yields no fields and FALLS THROUGH to
// the next candidate, then to errNoEditor — never an index panic. Out of scope: a
// quoted space *inside* the editor name itself (e.g. EDITOR=`"/my dir/ed" --wait`)
// is split into two tokens; the beside-an-agent norm is a bare command + flags.
func editorCommand(getenv func(string) string, absPath string) (*exec.Cmd, error) {
	for _, raw := range []string{getenv("VISUAL"), getenv("EDITOR")} {
		if fields := strings.Fields(raw); len(fields) > 0 {
			return exec.Command(fields[0], append(fields[1:], absPath)...), nil
		}
	}
	return nil, errNoEditor
}

// defaultCommands is the v1 palette command set. Four commands that solve a real
// pain today: reload (poll loop missed a change), copy path (paste into the agent
// chat), cd (jump within the jail without descending pane-by-pane), quit. Rename
// and delete stay on their direct keys (high-frequency); toggle-hidden is absent
// (lazyexplorer always shows hidden — see readDir).
func defaultCommands() []Command {
	return []Command{
		{
			Name: "reload", Description: "re-read the current directory",
			Run: func(m *model, _ string) tea.Cmd {
				m.reload()
				m.statusMsg = "reloaded"
				return nil
			},
		},
		{
			Name: "copy absolute path", Description: "copy the selected entry's absolute path",
			Run: func(m *model, _ string) tea.Cmd {
				if len(m.entries) == 0 {
					m.statusMsg = "⚠ nothing selected"
					return nil
				}
				full := m.selectedAbsPath()
				if err := writeClipboard(full); err != nil {
					m.statusMsg = "⚠ clipboard: " + err.Error()
					return nil
				}
				m.statusMsg = "copied " + full
				return nil
			},
		},
		{
			Name: "copy relative path", Description: "copy the selected entry's path relative to the root",
			// Discoverability twin of the `y` key: BOTH route through yankRelPath, the
			// single code path that guards, computes the slash-form rel, copies, and
			// records action.yank_rel exactly once (prd-yank-relative-path D7) — a split
			// twin would double-record. This is the path the agent chat actually wants;
			// "copy absolute path" stays for the rarer absolute case.
			Run: func(m *model, _ string) tea.Cmd {
				m.yankRelPath()
				return nil
			},
		},
		{
			Name: "copy file content", Description: "copy the previewed file's whole raw text",
			// Discoverability twin of the `Y` key (prd-preview-copy D9): BOTH route through
			// copyContent, the single code path that guards, reads the whole file from disk,
			// copies the raw text, and records action.copy_content exactly once — a split
			// twin would double-record. copyContent itself guards the empty listing / dir /
			// binary cases, so no pre-guard is needed here (unlike open-in-editor, whose
			// editorCommand has no such guard).
			Run: func(m *model, _ string) tea.Cmd {
				m.copyContent()
				return nil
			},
		},
		{
			Name: "open in editor", Description: "open the selected file in $EDITOR",
			Run: func(m *model, _ string) tea.Cmd {
				// Discoverability twin of the `e` key — same guard, since the palette is
				// a second entry point: selectedAbsPath() on ".." returns the parent
				// DIRECTORY, so without this an editor would be launched on a dir.
				if len(m.entries) == 0 {
					m.statusMsg = "⚠ nothing selected"
					return nil
				}
				sel := m.entries[m.cursor]
				if sel.name == ".." || sel.isDir {
					m.statusMsg = "⚠ not a file"
					return nil
				}
				cmd, err := editorCommand(os.Getenv, m.selectedAbsPath())
				if err != nil {
					m.statusMsg = "⚠ " + err.Error()
					return nil
				}
				m.tel.Record("action.open_editor", map[string]any{"name": sel.name})
				m.statusMsg = ""
				return tea.ExecProcess(cmd, func(err error) tea.Msg { return editorFinishedMsg{err} })
			},
		},
		{
			Name: "view changes", Description: "list every working-tree change (jump to its diff)",
			// Discoverability/mouse twin of the `c` key (prd-changed-only-view): both
			// route through enterChanges, the single code path that snapshots state,
			// derives the aggregate list, and records action.changes_view_open. Outside
			// a git repo enterChanges is a no-op; the palette is an explicit entry point,
			// so say why rather than close silently (mirror "open in editor"'s refusal).
			Run: func(m *model, _ string) tea.Cmd {
				if m.git.repoRoot == "" {
					m.statusMsg = "⚠ not a git repo — nothing to list"
					return nil
				}
				m.enterChanges()
				return nil
			},
		},
		{
			Name: "cd", Description: "change directory (jail-guarded)", NeedsArg: true,
			Run: func(m *model, path string) tea.Cmd {
				target, err := resolvePath(m.cwd, path)
				if err != nil {
					m.statusMsg = "⚠ " + err.Error()
					return nil
				}
				if !withinRoot(m.root, target) {
					m.statusMsg = "⚠ blocked: outside root"
					return nil
				}
				info, err := os.Stat(target)
				if err != nil {
					m.statusMsg = "⚠ not found: " + path
					return nil
				}
				if !info.IsDir() {
					m.statusMsg = "⚠ not a directory: " + path
					return nil
				}
				m.cwd = target
				m.cursor, m.listTop = 0, 0
				m.reload()
				m.statusMsg = ""
				return nil
			},
		},
		{
			Name: "quit", Description: "exit lazyexplorer",
			Run: func(_ *model, _ string) tea.Cmd { return tea.Quit },
		},
	}
}

// relRoot returns abs expressed RELATIVE to root, in slash-form — the shape that
// pastes straight into the agent's chat (it expects "src/auth.go", not the
// machine-absolute "/Users/…/proj/src/auth.go" the user would otherwise hand-trim).
// filepath.ToSlash normalizes the OS separator so a Windows backslash never leaks
// into the chat. Defensive: an under-root abs (the only input the dispatch ever
// passes — selectedAbsPath is jail-clamped) cannot error; on the impossible Rel
// error we return abs unchanged rather than an empty/garbage string. This is a PURE
// builder, tested independently of writeClipboard (TestRelRoot) — the clipboard
// helper fails in CI, so the rel string's correctness must be provable without it.
func relRoot(root, abs string) string {
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return abs
	}
	return filepath.ToSlash(rel)
}

// selectedAbsPath returns the absolute path of the entry under the cursor. The
// synthetic ".." entry (prepended by reload when cwd != root) resolves to the
// real parent directory, jail-clamped to root — never the literal "<cwd>/.."
// string. Caller guarantees len(m.entries) > 0.
func (m model) selectedAbsPath() string {
	name := m.entries[m.cursor].name
	if name == ".." {
		parent := filepath.Dir(m.cwd)
		if !withinRoot(m.root, parent) {
			return m.root // clamp: never escape the jail
		}
		return parent
	}
	return filepath.Join(m.cwd, name)
}

// writeClipboard ships text to the OS clipboard by shelling out — no CGo
// clipboard dep. pbcopy on darwin; xclip (X11) then wl-copy (Wayland) on linux.
// Returns errClipboardUnsupported when no helper is available.
func writeClipboard(text string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "linux":
		if _, err := exec.LookPath("xclip"); err == nil {
			cmd = exec.Command("xclip", "-selection", "clipboard")
		} else if _, err := exec.LookPath("wl-copy"); err == nil {
			cmd = exec.Command("wl-copy")
		} else {
			return errClipboardUnsupported
		}
	default:
		return errClipboardUnsupported
	}
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

// resolvePath expands ~/, ./, ../, and absolute prefixes against cwd, then
// cleans the result. It does NOT check existence — the caller does (jail check
// + os.Stat). Empty input is an error rather than silently meaning cwd.
func resolvePath(cwd, in string) (string, error) {
	if in == "" {
		return "", errors.New("empty path")
	}
	if strings.HasPrefix(in, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		in = filepath.Join(home, in[1:])
	}
	if !filepath.IsAbs(in) {
		in = filepath.Join(cwd, in)
	}
	return filepath.Clean(in), nil
}
