package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// splitEnv is one terminal lazyexplorer knows how to split. detected() reports
// whether we are running inside it (by environment variable); buildCmd assembles
// the *exec.Cmd that opens a split pane running `self root`. buildCmd returns the
// command WITHOUT running it so the exact argv / AppleScript can be unit-tested
// without a real terminal (see split_test.go).
type splitEnv struct {
	name     string
	detected func() bool
	buildCmd func(direction, root, self string) (*exec.Cmd, error)
}

// splitEnvs is the detection registry, tried in order. Multiplexers (tmux,
// zellij) come BEFORE terminal emulators: when the user is inside tmux/zellij the
// intent is to split that multiplexer's pane, not to open a second emulator
// window — and an emulator's own env var (GHOSTTY_RESOURCES_DIR, WEZTERM_PANE) can
// still be set while nested. Ordering multiplexers first is correct either way.
// Append-only; the order is a load-bearing invariant (docs/prd-split-respawn.md D4).
var splitEnvs = []splitEnv{
	{name: "tmux", detected: detectedTmux, buildCmd: buildTmux},
	{name: "zellij", detected: detectedZellij, buildCmd: buildZellij},
	{name: "wezterm", detected: detectedWezterm, buildCmd: buildWezterm},
	{name: "kitty", detected: detectedKitty, buildCmd: buildKitty},
	{name: "ghostty", detected: detectedGhostty, buildCmd: buildGhostty},
	{name: "iterm2", detected: detectedITerm, buildCmd: buildITerm},
}

func detectedTmux() bool   { return os.Getenv("TMUX") != "" }
func detectedZellij() bool { return os.Getenv("ZELLIJ") != "" }
func detectedWezterm() bool {
	return os.Getenv("WEZTERM_PANE") != "" || os.Getenv("TERM_PROGRAM") == "WezTerm"
}
func detectedKitty() bool { return os.Getenv("KITTY_WINDOW_ID") != "" }
func detectedGhostty() bool {
	return os.Getenv("GHOSTTY_RESOURCES_DIR") != "" || os.Getenv("TERM_PROGRAM") == "ghostty"
}
func detectedITerm() bool { return os.Getenv("TERM_PROGRAM") == "iTerm.app" }

// detectSplitEnv returns the first registered terminal we appear to be inside, or
// nil if none match.
func detectSplitEnv() *splitEnv {
	for i := range splitEnvs {
		if splitEnvs[i].detected() {
			return &splitEnvs[i]
		}
	}
	return nil
}

// spawnSplit detects the current terminal and opens a split pane running
// lazyexplorer rooted at root. direction is "right" or "below". It returns an
// error when no supported terminal is detected or when the spawn command fails;
// main turns that into a warning + a normal run in the current pane (D8/D9).
func spawnSplit(direction, root string) error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot resolve own executable path: %w", err)
	}
	env := detectSplitEnv()
	if env == nil {
		return fmt.Errorf("no supported terminal detected (tmux, zellij, wezterm, kitty, ghostty, iterm2)")
	}
	cmd, err := env.buildCmd(direction, root, self)
	if err != nil {
		return fmt.Errorf("%s: %w", env.name, err)
	}
	return runSpawn(env.name, cmd)
}

// runSpawn runs cmd and folds its stderr into the returned error, so the caller's
// warning explains *why* the split failed (e.g. kitty remote-control disabled,
// macOS Accessibility permission missing) instead of a bare "exit status 1".
func runSpawn(name string, cmd *exec.Cmd) error {
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(errBuf.String()); msg != "" {
			return fmt.Errorf("%s: %w: %s", name, err, msg)
		}
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

// buildTmux: tmux's -h splits left|right (panes side by side), -v splits
// top/bottom — counterintuitive naming but correct. self and root are passed as
// separate argv tokens so tmux execs them directly (no shell), keeping paths with
// spaces safe.
func buildTmux(direction, root, self string) (*exec.Cmd, error) {
	dir := "-h"
	if direction == "below" {
		dir = "-v"
	}
	return exec.Command("tmux", "split-window", dir, "-c", root, self, root), nil
}

func buildZellij(direction, root, self string) (*exec.Cmd, error) {
	dir := "right"
	if direction == "below" {
		dir = "down"
	}
	return exec.Command("zellij", "action", "new-pane", "--direction", dir, "--cwd", root, "--", self, root), nil
}

func buildWezterm(direction, root, self string) (*exec.Cmd, error) {
	dir := "--right"
	if direction == "below" {
		dir = "--bottom"
	}
	return exec.Command("wezterm", "cli", "split-pane", dir, "--cwd", root, "--", self, root), nil
}

func buildKitty(direction, root, self string) (*exec.Cmd, error) {
	loc := "vsplit"
	if direction == "below" {
		loc = "hsplit"
	}
	return exec.Command("kitty", "@", "launch", "--type=window", "--location="+loc, "--cwd", root, self, root), nil
}

// buildGhostty drives Ghostty via AppleScript keystrokes because Ghostty has no
// CLI to open a split running a given command. It sends cmd+d (split right) /
// cmd+shift+d (split down) — the default keybinds — then types the lazyexplorer
// command into the new pane's shell. This is fragile by nature: it fails silently
// if the user remapped those keybinds, or if macOS Accessibility permission is not
// granted to the parent process. The wrapped error names both so the user can fix
// it (docs/prd-split-respawn.md §5.3).
func buildGhostty(direction, root, self string) (*exec.Cmd, error) {
	if runtime.GOOS != "darwin" {
		return nil, fmt.Errorf("--split for Ghostty is only supported on macOS")
	}
	combo := `keystroke "d" using command down`
	if direction == "below" {
		combo = `keystroke "d" using {command down, shift down}`
	}
	// delay 0.5 lets the new pane's shell come up before we type into it.
	script := fmt.Sprintf(`tell application "Ghostty" to activate
delay 0.2
tell application "System Events"
	%s
	delay 0.5
	keystroke %s
	key code 36
end tell`, combo, appleScriptString(shellJoin(self, root)))
	return exec.Command("osascript", "-e", script), nil
}

// buildITerm uses iTerm2's scripting API: split the current session, then run the
// command in the new one via `write text`. Cleaner than keystrokes — no timing or
// keybind assumptions. iTerm's "split vertically" puts the new pane to the right;
// "split horizontally" puts it below.
func buildITerm(direction, root, self string) (*exec.Cmd, error) {
	if runtime.GOOS != "darwin" {
		return nil, fmt.Errorf("--split for iTerm2 is only supported on macOS")
	}
	verb := "split vertically with same profile"
	if direction == "below" {
		verb = "split horizontally with same profile"
	}
	script := fmt.Sprintf(`tell application "iTerm2"
	tell current session of current window
		set newSession to (%s)
	end tell
	tell newSession
		write text %s
	end tell
end tell`, verb, appleScriptString(shellJoin(self, root)))
	return exec.Command("osascript", "-e", script), nil
}

// shellJoin joins a command and its arguments into a single shell-safe line, for
// contexts where the command is typed into a shell (AppleScript keystroke / iTerm
// write text) rather than exec'd directly.
func shellJoin(parts ...string) string {
	quoted := make([]string, len(parts))
	for i, p := range parts {
		quoted[i] = shellQuote(p)
	}
	return strings.Join(quoted, " ")
}

// shellQuote wraps s in single quotes, escaping any embedded single quote, so it
// survives as one argument when typed into a POSIX shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// appleScriptString renders s as an AppleScript string literal (double-quoted,
// with \ and " escaped) for embedding into an `osascript -e` program.
func appleScriptString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}
