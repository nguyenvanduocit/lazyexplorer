package main

import "charm.land/bubbles/v2/key"

// KeyMap is the single source of truth for which key codes trigger which action
// and what help text describes them. Every binding the app reacts to lives here —
// adding a binding means adding a field, never a stray `case "x":` inline. The
// struct is flat (not nested by domain like crush) because lazyexplorer has one
// normal-mode lane, so flat keeps cognitive load low (prd-keymap-and-command-palette D2).
type KeyMap struct {
	// Navigation (normal mode + focusList)
	MoveUp,
	MoveDown,
	GoTop,
	GoBottom,
	OpenEntry,
	GoUp key.Binding

	// Preview pane scroll (normal mode + focusPreview). Same key codes as the
	// navigation bindings above; dispatch routes by focusPane. Step size lives at
	// the call site (previewLineStep = 1; half-page = bodyH/2) — these bindings
	// only carry key codes + help text.
	PreviewScrollUp,
	PreviewScrollDown,
	PreviewHalfPageUp,
	PreviewHalfPageDown,
	PreviewJumpTop,
	PreviewJumpBottom key.Binding

	// Preview horizontal scroll + wrap (focusPreview, nowrap plain/code). h/l
	// share key codes with GoUp/OpenEntry — dispatch routes by focusPane, so
	// these two carry help text only; H/L/0/w are real, unique-code bindings.
	PreviewScrollLeft,
	PreviewScrollRight,
	PreviewHScrollHalfLeft,
	PreviewHScrollHalfRight,
	PreviewHScrollReset,
	PreviewToggleWrap,
	ToggleDiff key.Binding // v — diff ↔ full content for a modified file (prd-preview-diff-view)

	// Mutation (normal mode + focusList)
	Rename,
	Delete key.Binding

	// Modes
	FocusToggle    key.Binding // Tab — focusList ↔ focusPreview (prd-pane-focus)
	Search         key.Binding // / — recursive fuzzy search (prd-search)
	CommandPalette key.Binding // ctrl+p — command palette (this PRD)
	FullHelp       key.Binding // ? — full-help overlay (this PRD)
	Back           key.Binding // esc — focusPreview→list / palette close / help close

	// Misc
	Quit key.Binding
}

// defaultKeyMap returns the ship default. CHANGE A KEY HERE, NOT IN updateNormal.
//
// Note: MoveUp/MoveDown/GoTop/GoBottom share key codes with their Preview*
// counterparts (up/k, down/j, g, G) but carry different help text ("move" vs
// "scroll"). key.Matches only compares key codes, so a single switch case per
// pair matches either; the dispatch site picks behavior by focusPane.
func defaultKeyMap() KeyMap {
	return KeyMap{
		MoveUp:    key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "move up")),
		MoveDown:  key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "move down")),
		GoTop:     key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "go top")),
		GoBottom:  key.NewBinding(key.WithKeys("G"), key.WithHelp("G", "go bottom")),
		OpenEntry: key.NewBinding(key.WithKeys("enter", "l", "right"), key.WithHelp("enter/l", "open")),
		GoUp:      key.NewBinding(key.WithKeys("h", "left", "backspace"), key.WithHelp("h/bksp", "go up")),

		PreviewScrollUp:     key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "scroll up")),
		PreviewScrollDown:   key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "scroll down")),
		PreviewHalfPageUp:   key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("ctrl+u", "half page up")),
		PreviewHalfPageDown: key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+d", "half page down")),
		PreviewJumpTop:      key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "preview top")),
		PreviewJumpBottom:   key.NewBinding(key.WithKeys("G"), key.WithHelp("G", "preview bottom")),

		PreviewScrollLeft:       key.NewBinding(key.WithKeys("h", "left"), key.WithHelp("h", "scroll left")),
		PreviewScrollRight:      key.NewBinding(key.WithKeys("l", "right"), key.WithHelp("l", "scroll right")),
		PreviewHScrollHalfLeft:  key.NewBinding(key.WithKeys("H"), key.WithHelp("H", "scroll ½ left")),
		PreviewHScrollHalfRight: key.NewBinding(key.WithKeys("L"), key.WithHelp("L", "scroll ½ right")),
		PreviewHScrollReset:     key.NewBinding(key.WithKeys("0"), key.WithHelp("0", "scroll reset")),
		PreviewToggleWrap:       key.NewBinding(key.WithKeys("w"), key.WithHelp("w", "toggle wrap")),
		ToggleDiff:              key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "toggle diff")),

		Rename: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "rename")),
		Delete: key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),

		FocusToggle:    key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "switch focus")),
		Search:         key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
		CommandPalette: key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("ctrl+p", "commands")),
		FullHelp:       key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Back:           key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),

		Quit: key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}
