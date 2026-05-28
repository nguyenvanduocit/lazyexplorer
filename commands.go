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
			Name: "copy path", Description: "copy the selected entry's absolute path",
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
