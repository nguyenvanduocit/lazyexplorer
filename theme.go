package main

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// codeHighlightStyle is the chroma style name for source syntax highlighting (see
// chroma/styles). Dark, to match the app palette. The renderer clears each token's
// background (so code inherits the panel background, no color stripes), so this
// only affects foreground colors — change this one line to recolor code preview.
const codeHighlightStyle = "github-dark"

// Palette — lazygit-flavored: restrained, one accent for focus.
var (
	colAccent = lipgloss.Color("#7D56F4") // active panel border, cursor
	colDir    = lipgloss.Color("#56B6F4") // folders
	colDim    = lipgloss.Color("#6C757D") // muted text, inactive borders
	colDanger = lipgloss.Color("#DC3545") // delete confirm; git deleted/conflict badge
	colWarn   = lipgloss.Color("#FFC107") // rename; git modified/renamed badge
	colFg     = lipgloss.Color("#E6E6E6")
	colSelFg  = lipgloss.Color("#FFFFFF")
	colGitNew = lipgloss.Color("#3FB950") // git new/untracked/added badge (github green)

	// Diff preview colors (prd-preview-diff-view D11). The git-diff CLI convention —
	// additions green, removals red, hunk-headers/context muted — reusing the
	// existing git palette so the diff stays in one accent family (no new color):
	// added reuses colGitNew (the untracked/added green), removed reuses colDanger
	// (the delete/conflict red). Hunk headers + context lines use colDim (dimStyle).
	colDiffAdd = colGitNew // diff "+" lines (additions)
	colDiffDel = colDanger // diff "-" lines (removals)
)

// gitColor maps a git change code to its badge foreground (PRD prd-git-change-indicator D12).
// One accent per status family: new→green, modified/rename→amber, delete/conflict→red.
func gitColor(c gitCode) color.Color {
	switch c {
	case gitUntracked, gitAdded:
		return colGitNew
	case gitDeleted, gitConflict:
		return colDanger
	default: // gitModified, gitRenamed
		return colWarn
	}
}

// diffLineStyle maps a hunk-body line's leading byte to its style (D11):
// '+' → added (colDiffAdd), '-' → removed (colDiffDel), everything else (the
// '@@…' hunk header, the ' '-prefixed context lines, and the '\ No newline'
// marker) → dimStyle (colDim). diffHunks dims the preamble file headers
// positionally (before the first '@@') and renders each hunk-body line through
// this, so the leading +/-/space character is kept readable even when the color
// is lost (a monochrome terminal still reads the diff). The empty-line case
// (a zero-length diff line) falls through to dimStyle, which is harmless.
func diffLineStyle(prefix byte) lipgloss.Style {
	switch prefix {
	case '+':
		return lipgloss.NewStyle().Foreground(colDiffAdd)
	case '-':
		return lipgloss.NewStyle().Foreground(colDiffDel)
	default:
		return dimStyle
	}
}

var (
	// Cursor row in the active list.
	cursorActiveStyle = lipgloss.NewStyle().
				Background(colAccent).
				Foreground(colSelFg).
				Bold(true)

	dirStyle  = lipgloss.NewStyle().Foreground(colDir).Bold(true)
	fileStyle = lipgloss.NewStyle().Foreground(colFg)
	dimStyle  = lipgloss.NewStyle().Foreground(colDim)

	// renderingStyle tints the transient render spinner at the right edge of the
	// status bar while an async preview (markdown/code/image) is in flight. The
	// single accent draws the eye without adding a new color.
	renderingStyle = lipgloss.NewStyle().Foreground(colAccent).Bold(true)

	// spinnerFrames is the braille spinner cycled one frame per ~100ms while a
	// preview render is in flight (see model.spinnerTickCmd). Each glyph is one
	// display column, so the reserved status-bar slot never changes width.
	spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

	// dividerFocusStyle tints the half/eighth-block glyph of the divider toward
	// the focused pane (prd-focus-divider-glow). Foreground only — no background —
	// so the un-inked half blends into the borderless pane. One accent, the same
	// colAccent as the cursor row and the render spinner.
	dividerFocusStyle = lipgloss.NewStyle().Foreground(colAccent)

	statusBarStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#1E1E2E")).
			Foreground(lipgloss.Color("#ADB5BD")).
			Padding(0, 1)

	promptStyle = lipgloss.NewStyle().
			Padding(0, 1).
			Bold(true)

	// modalBoxStyle is the floating command-palette / help box: a rounded accent
	// border around content that floats directly on the panes behind it — no
	// background fill, matching crush's Dialog.View (border only). One accent,
	// the same colAccent as the cursor row.
	modalBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colAccent).
			Foreground(colFg).
			Padding(0, 1)

	// modalAccentStyle tints the modal chrome accents — the "Commands" title
	// label and the "›"/"▏" input caret — with the one accent, bold. Kept
	// separate from renderingStyle (spinner) so the two can never drift onto
	// the same knob despite sharing a value today (crush-style header).
	modalAccentStyle = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
)

// Modal sizing — OUTER box dims; inner content = outer − modalBoxStyle frame
// (subtracted at runtime in modalSize). Clamped to fit narrow/short terminals.
const (
	modalMargin     = 2  // min screen cols/rows kept around the box
	modalTargetCols = 56 // preferred outer width
	modalTargetRows = 16 // preferred outer height
	modalMinCols    = 24 // floor outer width (degenerate terminal)
	modalMinRows    = 6  // floor outer height
)
