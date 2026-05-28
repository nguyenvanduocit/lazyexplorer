package main

// Cross-feature interaction guards. pane-focus (focus-routed keys) and search
// ("/" activation) were developed on separate branches and merged together;
// neither PRD's own test suite exercises the other. These tests pin the
// interactions that only exist once both features are in the same binary.

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestSlashEntersSearchFromPreviewFocus: "/" is a mode switch, not a list
// mutation, so it must enter search regardless of which pane holds focus.
// pane-focus guards the mutation/nav keys (r/d/enter/l/h/backspace) behind
// focusList, but "/" is intentionally NOT guarded (it sits outside the
// focus-routed block in updateNormal). Without this, a user reading a long
// preview (focusPreview) could never start a search.
func TestSlashEntersSearchFromPreviewFocus(t *testing.T) {
	m := searchModel(t)
	m.focusPane = focusPreview

	var tm tea.Model = m
	tm, _ = tm.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = tm.(model)

	if m.mode != modeSearch {
		t.Fatalf("'/' from focusPreview: mode = %v, want modeSearch (search is a mode switch, not focus-guarded)", m.mode)
	}
}

// TestSearchModeIgnoresFocusKeys: once in search mode, keystrokes are the
// query, not focus/navigation commands. Pressing "j" types into the query
// rather than toggling focus or moving a list cursor — Update routes
// modeSearch keys to updateSearch, never updateNormal, so focusPane is
// irrelevant while searching.
func TestSearchModeIgnoresFocusKeys(t *testing.T) {
	m := searchModel(t)
	m.enterSearch()
	if m.mode != modeSearch {
		t.Fatalf("setup: enterSearch did not enter modeSearch")
	}
	focusBefore := m.focusPane

	var tm tea.Model = m
	tm, _ = tm.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	m = tm.(model)

	if m.searchQuery != "j" {
		t.Errorf("'j' in search mode should type into the query; searchQuery = %q, want \"j\"", m.searchQuery)
	}
	if m.focusPane != focusBefore {
		t.Errorf("focusPane changed during search typing: %v → %v (search keys must not touch focus)", focusBefore, m.focusPane)
	}
}
