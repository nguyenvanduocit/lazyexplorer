package main

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// modalBoxStyle wraps body content in a rounded border + 1-col horizontal
// padding, so its frame is 2 (border) + 2 (padding) = 4 cols and 2 (border)
// rows. modalSize subtracts exactly this, so the values are load-bearing.
func TestModalBoxStyleFrame(t *testing.T) {
	if got := modalBoxStyle.GetHorizontalFrameSize(); got != 4 {
		t.Errorf("modalBoxStyle horizontal frame = %d, want 4 (border 2 + padding 2)", got)
	}
	if got := modalBoxStyle.GetVerticalFrameSize(); got != 2 {
		t.Errorf("modalBoxStyle vertical frame = %d, want 2 (border only)", got)
	}
	// Rendering through it must carry the rounded-corner glyphs.
	out := modalBoxStyle.Render("x")
	if !strings.Contains(out, "╭") || !strings.Contains(out, "╯") {
		t.Errorf("modalBoxStyle.Render missing rounded border: %q", out)
	}
	_ = lipgloss.Width // keep import if unused after edits
}
