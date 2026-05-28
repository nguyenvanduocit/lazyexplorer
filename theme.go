package main

import "charm.land/lipgloss/v2"

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
	colDanger = lipgloss.Color("#DC3545") // delete confirm
	colWarn   = lipgloss.Color("#FFC107") // rename
	colFg     = lipgloss.Color("#E6E6E6")
	colSelFg  = lipgloss.Color("#FFFFFF")
)

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

	// modalBoxStyle is the floating command-palette / help box: rounded accent
	// border + opaque dark background so it reads as lifted off the panes behind
	// it (D9/D22). One accent, the same colAccent as the cursor row.
	modalBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colAccent).
			Background(lipgloss.Color("#1A1A1A")).
			Foreground(colFg).
			Padding(0, 1)
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
