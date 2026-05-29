package main

import (
	"slices"
	"strings"
	"testing"
)

// TestCommandViewChanges pins the palette twin "view changes" (prd-changed-only-view):
// it appears in defaultCommands, and running it through the LIVE palette flow
// (ctrl+p → filter → enter) leaves the model in modeChanges with the aggregate list —
// the mouse/discoverability parity for the `c` key. Driving the full Update path is
// the real contract: it catches the palette-close path clobbering the mode the
// command established.
func TestCommandViewChanges(t *testing.T) {
	if !slices.ContainsFunc(defaultCommands(), func(c Command) bool { return c.Name == "view changes" }) {
		t.Fatal(`"view changes" command not found in defaultCommands()`)
	}

	m := changesRepoModel(t, noopRecorder{})

	// Open the palette, filter down to the changes command, run it.
	m = dogPress(t, m, keyCtrl('p'))
	if m.mode != modeCommandPalette {
		t.Fatalf("ctrl+p did not open the palette (mode=%v)", m.mode)
	}
	for _, r := range "view chang" {
		m = dogPress(t, m, keyRune(r))
	}
	if len(m.paletteFiltered) == 0 || m.paletteFiltered[m.paletteCursor].Name != "view changes" {
		t.Fatalf("filtering 'view chang' did not select 'view changes'; filtered=%v cursor=%d",
			commandNames(m.paletteFiltered), m.paletteCursor)
	}
	m = dogPress(t, m, keyEnter())

	if m.mode != modeChanges {
		t.Fatalf("after running 'view changes', mode = %v, want modeChanges", m.mode)
	}
	if !contains(rowNames(m.entries), "src/app.go") {
		t.Errorf("view-changes command did not populate the aggregate list; rows=%v", rowNames(m.entries))
	}
}

// TestCommandViewChangesNoOpOutsideRepo pins that the palette twin, like the `c`
// key, is inert outside a git repo: running it leaves the model in normal mode.
func TestCommandViewChangesNoOpOutsideRepo(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, "a.txt", "hi\n")
	m := modelAt(t, root, 120, 30)
	if m.git.repoRoot != "" {
		t.Skip("temp dir unexpectedly inside a repo")
	}
	var viewCmd Command
	for _, c := range defaultCommands() {
		if c.Name == "view changes" {
			viewCmd = c
		}
	}
	viewCmd.Run(&m, "")
	if m.mode != modeNormal {
		t.Errorf("view-changes outside a repo must stay modeNormal; got %v", m.mode)
	}
	if !strings.Contains(m.statusMsg, "not a git repo") {
		t.Errorf("view-changes outside a repo should explain why; status=%q", m.statusMsg)
	}
}

// envStub returns a getenv func backed by a fixed map — lets a test set $VISUAL /
// $EDITOR without mutating process state (so tests stay parallel-safe and never
// leak env into siblings), the same injection discipline split.go's buildCmd uses
// to stay unit-testable without a real terminal.
func envStub(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

// TestEditorCommand is the driver test for the pure command builder — the only
// unit-inspectable surface of open-in-editor (the tea.ExecProcess wrapper is
// opaque). Same discipline as split_test.go's TestBuildCLISplitArgs: assert the
// assembled argv, never Run() (no real editor spawn in CI).
func TestEditorCommand(t *testing.T) {
	const abs = "/abs/file.go"
	cases := []struct {
		name     string
		env      map[string]string
		wantArgs []string
		wantErr  bool
	}{
		{
			name:     "prefers VISUAL over EDITOR",
			env:      map[string]string{"VISUAL": "code", "EDITOR": "vim"},
			wantArgs: []string{"code", abs},
		},
		{
			name:     "falls back to EDITOR when VISUAL empty",
			env:      map[string]string{"VISUAL": "", "EDITOR": "vim"},
			wantArgs: []string{"vim", abs},
		},
		{
			name:     "splits flags, path is a separate trailing token",
			env:      map[string]string{"EDITOR": "code --wait"},
			wantArgs: []string{"code", "--wait", abs},
		},
		{
			name:     "emacsclient flags survive",
			env:      map[string]string{"EDITOR": "emacsclient -t"},
			wantArgs: []string{"emacsclient", "-t", abs},
		},
		{
			name:    "both unset is an error",
			env:     map[string]string{},
			wantErr: true,
		},
		{
			name:    "whitespace-only EDITOR errors, never panics",
			env:     map[string]string{"EDITOR": "   "},
			wantErr: true,
		},
		{
			name:     "whitespace VISUAL falls through to EDITOR",
			env:      map[string]string{"VISUAL": "   ", "EDITOR": "vim"},
			wantArgs: []string{"vim", abs},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cmd, err := editorCommand(envStub(c.env), abs)
			if c.wantErr {
				if err == nil {
					t.Fatalf("editorCommand(%v) = %+v, want error", c.env, cmd)
				}
				if cmd != nil {
					t.Errorf("editorCommand(%v) returned non-nil cmd with error: %+v", c.env, cmd)
				}
				return
			}
			if err != nil {
				t.Fatalf("editorCommand(%v) unexpected error: %v", c.env, err)
			}
			if !slices.Equal(cmd.Args, c.wantArgs) {
				t.Errorf("editorCommand(%v) args = %v, want %v", c.env, cmd.Args, c.wantArgs)
			}
		})
	}
}

// TestRelRoot is the driver for the pure rel-path builder — the only
// unit-inspectable surface of yank (writeClipboard is opaque + fails in CI with no
// pbcopy/xclip, so the rel string MUST be computed by a function tested
// independently of the clipboard side effect). Mirrors TestEditorCommand's table
// discipline. ToSlash on every case proves slash-form on all OSes (Windows path
// separators would otherwise leak into the agent chat).
func TestRelRoot(t *testing.T) {
	const root = "/proj"
	cases := []struct {
		name string
		abs  string
		want string
	}{
		{name: "file at root", abs: "/proj/foo.go", want: "foo.go"},
		{name: "nested file is slash-form", abs: "/proj/a/b/c.go", want: "a/b/c.go"},
		{name: "a directory under root", abs: "/proj/sub", want: "sub"},
		{name: "root itself resolves to dot (dispatch then refuses)", abs: "/proj", want: "."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := relRoot(root, c.abs); got != c.want {
				t.Errorf("relRoot(%q, %q) = %q, want %q", root, c.abs, got, c.want)
			}
		})
	}
}
