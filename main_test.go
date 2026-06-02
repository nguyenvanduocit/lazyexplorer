package main

import (
	"strings"
	"testing"
)

// TestHelpTextSurfaces pins the FR11/FR12 `--help` CLI surfaces (prd-preview-copy):
// the Keys line must advertise `Y copy file content`, and the footnote must teach the
// Shift/Option native-selection bypass. helpText() is the pure string printHelp emits,
// extracted so this assertion needs no os.Stdout capture (Functional Core / Imperative
// Shell) — the static fmt.Print line was previously the one FR11 surface with no test.
func TestHelpTextSurfaces(t *testing.T) {
	h := helpText()
	for _, want := range []string{
		"Y copy file content",                  // FR11: the Y key advertised on the CLI
		"hold Shift",                           // FR12: native-selection bypass instruction
		"Option on iTerm2/macOS Terminal/tmux", // FR12: the per-terminal modifier list (D11)
	} {
		if !strings.Contains(h, want) {
			t.Errorf("helpText() missing %q (FR11/FR12); got:\n%s", want, h)
		}
	}
}
