package main

import (
	"slices"
	"testing"
)

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
