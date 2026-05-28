package main

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// enterCommandPalette opens the palette at stage 0 (pick a command) with the full
// command list. focusPane is left untouched — the palette is a transient overlay,
// not a focus change (FR7).
func (m *model) enterCommandPalette() {
	m.mode = modeCommandPalette
	m.paletteStage = 0
	m.paletteQuery = ""
	m.paletteSecondaryInput = ""
	m.paletteCursor = 0
	m.paletteFiltered = defaultCommands()
	m.statusMsg = ""
	m.tel.Record("action.command_palette_open", nil)
}

// exitCommandPalette closes the palette and clears its state back to zero. Mode
// returns to normal; cwd/focus are untouched (the palette only edits them on a
// committed Enter, never on close).
func (m *model) exitCommandPalette() {
	m.mode = modeNormal
	m.paletteStage = 0
	m.paletteQuery = ""
	m.paletteSecondaryInput = ""
	m.paletteCursor = 0
	m.paletteFiltered = nil
}

// applyPaletteFilter recomputes paletteFiltered from paletteQuery: substring,
// case-insensitive, over the command name (D8 — a handful of commands doesn't
// warrant fuzzy ranking). Cursor resets to the top match.
func (m *model) applyPaletteFilter() {
	cmds := defaultCommands()
	if m.paletteQuery == "" {
		m.paletteFiltered = cmds
	} else {
		needle := strings.ToLower(m.paletteQuery)
		out := cmds[:0:0] // fresh slice, don't alias defaultCommands' backing array
		for _, c := range cmds {
			if strings.Contains(strings.ToLower(c.Name), needle) {
				out = append(out, c)
			}
		}
		m.paletteFiltered = out
	}
	m.paletteCursor = 0
}

// updateCommandPalette handles keys while the palette is open. Stage 1 collects a
// text argument (cd path); stage 0 filters + picks a command. Bound keys (Back,
// MoveUp/Down, CommandPalette) match via the keymap; enter/backspace/printable
// text follow the same convention as updateRename (msg.String / msg.Text).
func (m model) updateCommandPalette(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	km := m.keymap

	// Stage 1: collecting an argument (currently only `cd`).
	if m.paletteStage == 1 {
		switch {
		case key.Matches(msg, km.Back):
			// Step back to the command list, keep the palette open.
			m.paletteStage = 0
			m.paletteSecondaryInput = ""
			return m, nil
		case msg.String() == "enter":
			sel := m.paletteFiltered[m.paletteCursor]
			cmd := sel.Run(&m, m.paletteSecondaryInput)
			ok := !strings.HasPrefix(m.statusMsg, "⚠")
			m.tel.Record("action.command_run", map[string]any{"name": sel.Name, "success": ok})
			if ok {
				m.exitCommandPalette()
			}
			// On failure keep stage 1 open so the user can fix the path.
			return m, cmd
		case msg.String() == "backspace":
			r := []rune(m.paletteSecondaryInput)
			if len(r) > 0 {
				m.paletteSecondaryInput = string(r[:len(r)-1])
			}
			return m, nil
		default:
			if msg.Text != "" {
				m.paletteSecondaryInput += msg.Text
			}
			return m, nil
		}
	}

	// Stage 0: pick a command.
	switch {
	case key.Matches(msg, km.Back), key.Matches(msg, km.CommandPalette):
		m.exitCommandPalette()
		return m, nil

	case key.Matches(msg, km.MoveDown):
		if m.paletteCursor < len(m.paletteFiltered)-1 {
			m.paletteCursor++
		}
		return m, nil
	case key.Matches(msg, km.MoveUp):
		if m.paletteCursor > 0 {
			m.paletteCursor--
		}
		return m, nil

	case msg.String() == "enter":
		if len(m.paletteFiltered) == 0 {
			return m, nil
		}
		sel := m.paletteFiltered[m.paletteCursor]
		if sel.NeedsArg {
			m.paletteStage = 1
			return m, nil
		}
		cmd := sel.Run(&m, "")
		m.tel.Record("action.command_run", map[string]any{
			"name":    sel.Name,
			"success": !strings.HasPrefix(m.statusMsg, "⚠"),
		})
		m.exitCommandPalette()
		return m, cmd

	case msg.String() == "backspace":
		if m.paletteQuery == "" {
			m.exitCommandPalette() // backspace on an empty query closes (cf. search D13)
			return m, nil
		}
		r := []rune(m.paletteQuery)
		m.paletteQuery = string(r[:len(r)-1])
		m.applyPaletteFilter()
		return m, nil

	default:
		if msg.Text != "" {
			m.paletteQuery += msg.Text
			m.applyPaletteFilter()
		}
	}
	return m, nil
}

// enterHelp opens the full-help overlay scrolled to the top.
func (m *model) enterHelp() {
	m.mode = modeHelp
	m.helpTop = 0
}

// exitHelp closes the help overlay.
func (m *model) exitHelp() {
	m.mode = modeNormal
	m.helpTop = 0
}

// updateHelp handles keys while the help overlay is open: esc/?/q close it
// (q closes rather than quits — surprise avoidance), j/k scroll the body.
func (m model) updateHelp(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	km := m.keymap
	switch {
	case key.Matches(msg, km.Back), key.Matches(msg, km.FullHelp), key.Matches(msg, km.Quit):
		m.exitHelp()
	case key.Matches(msg, km.MoveDown):
		// Clamp on the way down (mirror scrollPreview): without the upper clamp,
		// j-spam grows helpTop unbounded and k then feels laggy as it counts back.
		_, bodyH := m.previewScroll()
		maxTop := max(0, m.helpLineCount()-bodyH)
		m.helpTop = min(m.helpTop+1, maxTop)
	case key.Matches(msg, km.MoveUp):
		if m.helpTop > 0 {
			m.helpTop--
		}
	}
	return m, nil
}

// helpLineCount is the rendered help-body line count (group title + binding rows
// + one blank separator per group) — the same number renderHelp produces, so the
// scroll clamp here and the slice in renderHelp never disagree.
func (m model) helpLineCount() int {
	n := 0
	for _, group := range m.fullHelp() {
		n += 1 + len(group) + 1 // title + rows + blank separator
	}
	return n
}
