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

func TestModalSizeClamps(t *testing.T) {
	fw := modalBoxStyle.GetHorizontalFrameSize() // 4
	fh := modalBoxStyle.GetVerticalFrameSize()   // 2

	cases := []struct {
		name             string
		w, h             int
		wantInW, wantInH int
	}{
		// Wide terminal: target wins. outerW=56, outerH=16.
		{"wide", 120, 40, modalTargetCols - fw, modalTargetRows - fh},
		// Narrow (vertical-mode) terminal: m.width-margin*2 = 60-4 = 56 = target,
		// so outerW=56, inner=52; height still reaches the row target.
		{"narrow60", 60, 24, 56 - fw, modalTargetRows - fh},
		// Very narrow + short: floor then never-exceed-screen.
		//   outerW = min(56,16)=16 → min(max(16,24),20)=min(24,20)=20 → inner 16.
		//   outerH = min(16,5)=5  → min(max(5,6),9)=min(6,9)=6     → inner 4.
		// Invariant: inner+frame ≤ screen (16+4≤20, 4+2≤9).
		{"tiny", 20, 10, 20 - fw, 6 - fh},
	}
	for _, c := range cases {
		m := model{width: c.w, height: c.h}
		gotW, gotH := m.modalSize()
		if gotW != c.wantInW {
			t.Errorf("%s: innerW = %d, want %d", c.name, gotW, c.wantInW)
		}
		if gotH != c.wantInH {
			t.Errorf("%s: innerH = %d, want %d", c.name, gotH, c.wantInH)
		}
		// The load-bearing invariant: the OUTER box never overflows the screen.
		if gotW+fw > c.w {
			t.Errorf("%s: outerW %d exceeds width %d", c.name, gotW+fw, c.w)
		}
		if gotH+fh > c.h-1 {
			t.Errorf("%s: outerH %d exceeds body rows %d", c.name, gotH+fh, c.h-1)
		}
	}
}
