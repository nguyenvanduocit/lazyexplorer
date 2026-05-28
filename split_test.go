package main

import (
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestParseArgs(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		want    cliArgs
		wantErr bool
	}{
		{name: "empty", args: nil, want: cliArgs{start: ".", splitDir: "right"}},
		{name: "dir only", args: []string{"sub"}, want: cliArgs{start: "sub", splitDir: "right"}},
		{name: "split bare", args: []string{"--split"}, want: cliArgs{start: ".", split: true, splitDir: "right"}},
		{name: "split right", args: []string{"--split=right"}, want: cliArgs{start: ".", split: true, splitDir: "right"}},
		{name: "split below", args: []string{"--split=below"}, want: cliArgs{start: ".", split: true, splitDir: "below"}},
		{name: "dir then split", args: []string{"sub", "--split"}, want: cliArgs{start: "sub", split: true, splitDir: "right"}},
		{name: "split then dir", args: []string{"--split", "sub"}, want: cliArgs{start: "sub", split: true, splitDir: "right"}},
		{name: "split below then dir", args: []string{"--split=below", "sub"}, want: cliArgs{start: "sub", split: true, splitDir: "below"}},
		{name: "version long", args: []string{"--version"}, want: cliArgs{start: ".", splitDir: "right", showVersion: true}},
		{name: "version short", args: []string{"-v"}, want: cliArgs{start: ".", splitDir: "right", showVersion: true}},
		{name: "version word", args: []string{"version"}, want: cliArgs{start: ".", splitDir: "right", showVersion: true}},
		{name: "help long", args: []string{"--help"}, want: cliArgs{start: ".", splitDir: "right", showHelp: true}},
		{name: "help short", args: []string{"-h"}, want: cliArgs{start: ".", splitDir: "right", showHelp: true}},
		{name: "split bad value", args: []string{"--split=sideways"}, wantErr: true},
		{name: "unknown flag", args: []string{"--foo"}, wantErr: true},
		{name: "two positionals", args: []string{"a", "b"}, wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseArgs(c.args)
			if c.wantErr {
				if err == nil {
					t.Fatalf("parseArgs(%v) = %+v, want error", c.args, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseArgs(%v) unexpected error: %v", c.args, err)
			}
			if got != c.want {
				t.Errorf("parseArgs(%v) = %+v, want %+v", c.args, got, c.want)
			}
		})
	}
}

// clearSplitEnv blanks every env var the split registry inspects so a test starts
// from a known "no terminal" state. t.Setenv restores the originals after the test.
func clearSplitEnv(t *testing.T) {
	for _, k := range []string{"TMUX", "ZELLIJ", "WEZTERM_PANE", "KITTY_WINDOW_ID", "GHOSTTY_RESOURCES_DIR", "TERM_PROGRAM"} {
		t.Setenv(k, "")
	}
}

func detectedName() string {
	if env := detectSplitEnv(); env != nil {
		return env.name
	}
	return ""
}

func TestDetectSplitEnvPriority(t *testing.T) {
	t.Run("none detected", func(t *testing.T) {
		clearSplitEnv(t)
		if got := detectedName(); got != "" {
			t.Fatalf("detectSplitEnv() = %q, want none", got)
		}
	})
	t.Run("tmux beats ghostty when nested", func(t *testing.T) {
		clearSplitEnv(t)
		t.Setenv("TMUX", "/tmp/tmux-1000/default,123,0")
		t.Setenv("GHOSTTY_RESOURCES_DIR", "/Applications/Ghostty.app/Contents/Resources")
		if got := detectedName(); got != "tmux" {
			t.Fatalf("nested tmux-in-ghostty: got %q, want tmux", got)
		}
	})
	t.Run("zellij beats wezterm when nested", func(t *testing.T) {
		clearSplitEnv(t)
		t.Setenv("ZELLIJ", "0")
		t.Setenv("WEZTERM_PANE", "3")
		if got := detectedName(); got != "zellij" {
			t.Fatalf("nested zellij-in-wezterm: got %q, want zellij", got)
		}
	})
	t.Run("wezterm via TERM_PROGRAM", func(t *testing.T) {
		clearSplitEnv(t)
		t.Setenv("TERM_PROGRAM", "WezTerm")
		if got := detectedName(); got != "wezterm" {
			t.Fatalf("got %q, want wezterm", got)
		}
	})
	t.Run("kitty via window id", func(t *testing.T) {
		clearSplitEnv(t)
		t.Setenv("KITTY_WINDOW_ID", "1")
		if got := detectedName(); got != "kitty" {
			t.Fatalf("got %q, want kitty", got)
		}
	})
	t.Run("ghostty via TERM_PROGRAM", func(t *testing.T) {
		clearSplitEnv(t)
		t.Setenv("TERM_PROGRAM", "ghostty")
		if got := detectedName(); got != "ghostty" {
			t.Fatalf("got %q, want ghostty", got)
		}
	})
	t.Run("iterm via TERM_PROGRAM", func(t *testing.T) {
		clearSplitEnv(t)
		t.Setenv("TERM_PROGRAM", "iTerm.app")
		if got := detectedName(); got != "iterm2" {
			t.Fatalf("got %q, want iterm2", got)
		}
	})
}

func TestBuildCLISplitArgs(t *testing.T) {
	const self, root = "/usr/local/bin/lazyexplorer", "/home/u/proj"
	cases := []struct {
		name      string
		build     func(direction, root, self string) (*exec.Cmd, error)
		direction string
		wantArgs  []string
	}{
		{"tmux right", buildTmux, "right", []string{"tmux", "split-window", "-h", "-c", root, self, root}},
		{"tmux below", buildTmux, "below", []string{"tmux", "split-window", "-v", "-c", root, self, root}},
		{"zellij right", buildZellij, "right", []string{"zellij", "action", "new-pane", "--direction", "right", "--cwd", root, "--", self, root}},
		{"zellij below", buildZellij, "below", []string{"zellij", "action", "new-pane", "--direction", "down", "--cwd", root, "--", self, root}},
		{"wezterm right", buildWezterm, "right", []string{"wezterm", "cli", "split-pane", "--right", "--cwd", root, "--", self, root}},
		{"wezterm below", buildWezterm, "below", []string{"wezterm", "cli", "split-pane", "--bottom", "--cwd", root, "--", self, root}},
		{"kitty right", buildKitty, "right", []string{"kitty", "@", "launch", "--type=window", "--location=vsplit", "--cwd", root, self, root}},
		{"kitty below", buildKitty, "below", []string{"kitty", "@", "launch", "--type=window", "--location=hsplit", "--cwd", root, self, root}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cmd, err := c.build(c.direction, root, self)
			if err != nil {
				t.Fatalf("%s build error: %v", c.name, err)
			}
			if !slices.Equal(cmd.Args, c.wantArgs) {
				t.Errorf("%s args = %v, want %v", c.name, cmd.Args, c.wantArgs)
			}
			// No builder may carry --split, or the spawned pane would split again.
			for _, a := range cmd.Args {
				if a == "--split" || strings.HasPrefix(a, "--split=") {
					t.Errorf("%s args contain --split (would recurse): %v", c.name, cmd.Args)
				}
			}
		})
	}
}

func TestBuildAppleScriptSplit(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("AppleScript split builders are darwin-only")
	}
	const self, root = "/usr/local/bin/lazyexplorer", "/home/u/proj"
	scriptOf := func(t *testing.T, cmd *exec.Cmd, err error) string {
		t.Helper()
		if err != nil {
			t.Fatalf("build error: %v", err)
		}
		return cmd.Args[len(cmd.Args)-1] // osascript -e <script>
	}

	t.Run("ghostty right uses cmd+d and types command", func(t *testing.T) {
		cmd, err := buildGhostty("right", root, self)
		script := scriptOf(t, cmd, err)
		for _, want := range []string{`tell application "Ghostty"`, `keystroke "d" using command down`, self, root, "key code 36"} {
			if !strings.Contains(script, want) {
				t.Errorf("ghostty right script missing %q:\n%s", want, script)
			}
		}
		if strings.Contains(script, "shift down") {
			t.Errorf("ghostty right must not use shift:\n%s", script)
		}
	})
	t.Run("ghostty below uses cmd+shift+d", func(t *testing.T) {
		cmd, err := buildGhostty("below", root, self)
		script := scriptOf(t, cmd, err)
		if !strings.Contains(script, "{command down, shift down}") {
			t.Errorf("ghostty below missing shift combo:\n%s", script)
		}
	})
	t.Run("iterm right splits vertically and writes text", func(t *testing.T) {
		cmd, err := buildITerm("right", root, self)
		script := scriptOf(t, cmd, err)
		for _, want := range []string{"split vertically with same profile", "write text", self, root} {
			if !strings.Contains(script, want) {
				t.Errorf("iterm right script missing %q:\n%s", want, script)
			}
		}
	})
	t.Run("iterm below splits horizontally", func(t *testing.T) {
		cmd, err := buildITerm("below", root, self)
		script := scriptOf(t, cmd, err)
		if !strings.Contains(script, "split horizontally with same profile") {
			t.Errorf("iterm below missing horizontal split:\n%s", script)
		}
	})
}
