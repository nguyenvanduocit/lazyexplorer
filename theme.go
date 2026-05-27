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

	// Transient "rendering…" chip in the status bar (async markdown in flight).
	// Uses the single accent so it draws the eye without adding a new color.
	renderingStyle = lipgloss.NewStyle().Foreground(colAccent).Bold(true)

	statusBarStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#1E1E2E")).
			Foreground(lipgloss.Color("#ADB5BD")).
			Padding(0, 1)

	promptStyle = lipgloss.NewStyle().
			Padding(0, 1).
			Bold(true)
)
