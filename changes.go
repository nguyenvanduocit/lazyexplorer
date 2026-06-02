package main

// changes.go — the changed-only aggregate view (modeChanges, prd-changed-only-view).
//
// One key (`c`) opens a FLAT list of every working-tree change in the whole repo
// (jail-clamped), Enter teleports to the file's diff. It STRUCTURALLY MIRRORS
// modeSearch: it repurposes m.entries/cursor/listTop as a flat result list of
// root-relative-named entries, snapshots the pre-view state on enter, restores it
// on Esc, and navigates-to-result on Enter via the SAME openResult teleport. The
// only differences from search: no query box, and the rows are SOURCED from the
// in-memory git change set (m.git.changes) instead of a recursive walk.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// changesBaseDir is the directory a changes-mode row's name resolves against — the
// jail root, since each row name is root-relative (mirror of previewBaseDir in the
// flat-list modes). Named separately from previewBaseDir so the changes-specific
// call sites read clearly; the value is the same.
func (m model) changesBaseDir() string { return m.root }

// changedRows derives the flat aggregate list from the in-memory git change set:
// every change in m.git.changes (keyed repo-relative slash) mapped to a ROOT-relative
// entry, jail-clamped (a change outside the jail is dropped), sorted by name. It is a
// PURE inversion of gitRootPrefix — the mirror of repoRelKey, which maps the other
// direction (root-rel → repo-rel). Git mode off (repoRoot=="") or a clean tree yields
// an empty (non-nil-safe) list. Deleted files ARE listed: they live in m.git.changes
// even without a working-tree row (the openResult os.Stat guard handles the dead path).
func (m model) changedRows() []entry {
	if m.git.repoRoot == "" || len(m.git.changes) == 0 {
		return nil
	}
	rows := make([]entry, 0, len(m.git.changes))
	for repoRel := range m.git.changes {
		rootRel, ok := m.rootRelFromRepoRel(repoRel)
		if !ok {
			continue // outside the jail — same discipline as repoRelKey's "../" guard
		}
		// isDir is false: every change is a file (status/numstat operate on files;
		// an untracked DIR is expanded to its files by `status -uall`, git.go §5.1).
		// The badge/delta is resolved at render time via indicatorFor, so the row
		// carries no styling — it stays a plain entry the modeSearch surface accepts.
		e := entry{name: rootRel}
		// Carry the real on-disk size + mtime — like walkTree does via d.Info() — so
		// refreshPreview's readPreviewBytes reads the WHOLE file (a zero size makes it
		// read 0 bytes → kind "empty" → the file mis-renders as "binary files differ").
		// A DELETED change has no file on disk: os.Stat fails, the entry keeps size 0,
		// and the openResult os.Stat guard refuses the jump anyway (D11).
		if info, statErr := os.Stat(filepath.Join(m.root, filepath.FromSlash(rootRel))); statErr == nil {
			e.size = info.Size()
			e.modTime = info.ModTime()
		}
		rows = append(rows, e)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].name < rows[j].name }) // mirror walkTree's sort (fs.go:190)
	return rows
}

// rootRelFromRepoRel inverts gitRootPrefix: a repo-relative slash key → its path
// relative to the jail root, ok=false when the key sits OUTSIDE the jail. It is the
// exact inverse of repoRelKey (root-rel → repo-rel via gitRootPrefix), so a row's
// name round-trips back to the same git key the badge/diff resolve against.
//
//   - prefix == "" (jail root IS repo root): every key is in-jail, name == key.
//   - prefix == "sub": "sub/a.go" → "a.go"; a key that is not under "sub/" (and is
//     not "sub" itself) is outside the jail → ok=false.
func (m model) rootRelFromRepoRel(repoRel string) (string, bool) {
	if m.gitRootPrefix == "" {
		return repoRel, true
	}
	if repoRel == m.gitRootPrefix {
		return "", false // the jail-root dir itself is never a file change row
	}
	p := m.gitRootPrefix + "/"
	if !strings.HasPrefix(repoRel, p) {
		return "", false // outside the jail subtree
	}
	return strings.TrimPrefix(repoRel, p), true
}

// enterChanges transitions normal → changes (prd-changed-only-view §5.2). It is a
// NO-OP outside a git repo (nothing to list, mirrors prd-preview-diff-view FR9): the
// caller checks repoRoot before invoking, but the guard here is defensive. Inside a
// repo it snapshots the pre-view state (for Esc restore, exactly like enterSearch),
// derives the row list from the current git snapshot, and resets the cursor/scroll.
// A clean tree enters the mode with an empty list + a "(no changes)" status — the
// user asked "what changed?", so "nothing" is answered visibly, never a silent no-op.
func (m *model) enterChanges() {
	if m.git.repoRoot == "" {
		return // not a repo → c is a no-op (git mode off)
	}
	m.changesSavedCwd = m.cwd
	m.changesSavedEntries = append([]entry(nil), m.entries...) // defensive copy
	m.changesSavedFsSig = m.fsSig
	m.changesSavedCursor = m.cursor
	m.changesSavedListTop = m.listTop

	m.mode = modeChanges
	m.entries = m.changedRows()
	m.cursor = 0
	m.listTop = 0
	if len(m.entries) == 0 {
		m.statusMsg = "(no changes)" // clean tree: answer "nothing", visibly (mirror applySearchFilter's 0-results hint)
	} else {
		m.statusMsg = ""
	}
	m.tel.Record("action.changes_view_open", nil)
	m.refreshPreview()
}

// refreshChanges re-derives the row list in place from the latest git snapshot,
// preserving the cursor BY NAME so a live git refresh (an edit the agent just made)
// appears without re-opening the view, and the selection doesn't jump under the user.
// Called when a git snapshot lands while parked in modeChanges. The "(no changes)"
// hint tracks the (possibly now empty) list. Only the status hint is touched on the
// empty branch — never an existing user-set message on the non-empty branch (we
// clear only the stale "(no changes)" so a real edit landing flips it back).
//
// CRITICAL — no preview churn (mirror syncFromDisk model.go:536-543): the poll loop
// dispatches a git refresh every ~1s, so this runs once a second while parked here.
// refreshPreview resets srcWidth/pendingWidth and re-stamps a placeholder, forcing a
// diff re-fetch (a git exec) + re-render. So re-render the preview ONLY when the
// SELECTED change actually changed — compare its name+size+mtime before/after, the
// same fields dirSig folds. An unchanged selection leaves the preview alone; the list
// still updates live (rows appear/disappear on real edits) and the scroll is kept.
func (m *model) refreshChanges() {
	var oldSel entry
	hadSel := m.cursor >= 0 && m.cursor < len(m.entries)
	if hadSel {
		oldSel = m.entries[m.cursor]
	}
	prevTop := m.previewTop

	m.entries = m.changedRows()
	m.cursor = min(m.cursor, max(0, len(m.entries)-1))
	foundSameName := false
	if hadSel && oldSel.name != "" {
		for i, e := range m.entries {
			if e.name == oldSel.name {
				m.cursor = i
				foundSameName = true
				break
			}
		}
	}

	switch {
	case len(m.entries) == 0:
		m.statusMsg = "(no changes)"
	case m.statusMsg == "(no changes)":
		m.statusMsg = "" // a change just appeared → drop the stale empty hint
	}

	// Selected change unchanged (same name + byte-identical size/mtime)? The list is
	// already re-derived above — leave the preview alone, exactly as syncFromDisk does
	// for an unchanged selected file. This is what stops a per-second diff re-fetch.
	if foundSameName && m.cursor < len(m.entries) {
		newSel := m.entries[m.cursor]
		if oldSel.size == newSel.size && oldSel.modTime.Equal(newSel.modTime) {
			return
		}
	}

	// Selected change moved, vanished, or its content changed: re-read the preview and
	// restore the scroll offset (refreshPreview reset it to 0), clamped into range.
	m.refreshPreview()
	m.previewTop = prevTop
	m.scrollPreview(0)
}

// exitChangesRestore leaves changes mode and restores the EXACT pre-view state
// (cwd, entries, fsSig poll baseline, cursor, scroll) — the mirror of
// exitSearchRestore (model.go §5.5, FR10). refreshPreview re-syncs the right panel
// to the restored selection.
func (m *model) exitChangesRestore() {
	m.entries = m.changesSavedEntries
	m.cwd = m.changesSavedCwd
	m.fsSig = m.changesSavedFsSig
	m.cursor = m.changesSavedCursor
	m.listTop = m.changesSavedListTop
	m.mode = modeNormal
	m.statusMsg = "changes view closed"
	m.refreshPreview()
}

// openChangesResult acts on the highlighted change (the Enter/l teleport): cd into
// the file's parent and land the cursor on its basename, leaving the diff showing in
// the preview (diffOn defaults true, so a modified file lands in its diff view —
// list→Enter→review-the-edit, zero tab-away). It reuses openSearchResult's exact
// cross-directory cd+seek, jail-checked via withinRoot. A change whose file was
// DELETED has no target on disk: rather than cd into a dead path, surface
// "⚠ file no longer on disk" and stay in the changes list so the user can pick
// another row. An empty list (clean tree) backs out cleanly.
func (m *model) openChangesResult() {
	if m.cursor < 0 || m.cursor >= len(m.entries) {
		m.exitChangesRestore()
		return
	}
	sel := m.entries[m.cursor]
	target := filepath.Join(m.root, sel.name)
	if !withinRoot(m.root, target) {
		m.statusMsg = "⚠ blocked: outside root"
		return
	}
	// A deleted change has a row here (it IS a change) but no file on disk: cd-ing
	// into its parent + seeking the basename would land on nothing. Report it and
	// keep the list open rather than teleport into a dead path.
	if _, err := os.Stat(target); err != nil {
		m.statusMsg = "⚠ file no longer on disk: " + sel.name
		return
	}

	rel := relRoot(m.root, target)
	m.tel.Record("action.changes_jump", map[string]any{"rel": rel})

	m.mode = modeNormal
	m.statusMsg = ""
	m.cursor = 0
	m.listTop = 0

	// cd into the file's parent, then land the cursor on the basename — the same
	// teleport openSearchResult does for a file result.
	m.cwd = filepath.Dir(target)
	m.reload()
	base := filepath.Base(sel.name)
	for i, e := range m.entries {
		if e.name == base {
			m.cursor = i
			break
		}
	}
	m.refreshPreview()
}

// updateChanges handles keypresses while in modeChanges (prd-changed-only-view §5.2).
// It is modeSearch's key handler MINUS the query box: Esc restores the pre-view
// state, Enter (or l/right) opens the selected change, up/down (and j/k, ctrl+p/n)
// move within the list. There is no text input — a change list is a fixed set, not a
// query — so a printable key other than the nav keys is ignored. Keys go through the
// keymap (single source) where one exists; the list-navigation pairs match by code.
func (m model) updateChanges(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	km := m.keymap
	switch {
	case key.Matches(msg, km.Back):
		m.exitChangesRestore()
		return m, nil
	case key.Matches(msg, km.OpenEntry): // enter / l / right
		m.openChangesResult()
		return m, nil
	// Y copies the row's CURRENT on-disk content (prd-preview-copy D4/FR7) — the
	// headline context: a file the agent just changed (exactly what this view lists).
	// copyContent resolves via previewBaseDir()=root (the row name is root-relative), so
	// it copies the real file, not the diff text shown in the preview. This switch is
	// closed (no fall-through to updateNormal), so without this case Y would be a dead
	// key here. Shared code path → telemetry records once.
	case key.Matches(msg, km.CopyContent):
		m.copyContent()
		return m, nil
	case key.Matches(msg, km.MoveDown): // down / j
		if m.cursor < len(m.entries)-1 {
			m.cursor++
			m.refreshPreview()
		}
	case key.Matches(msg, km.MoveUp): // up / k
		if m.cursor > 0 {
			m.cursor--
			m.refreshPreview()
		}
	case key.Matches(msg, km.GoTop): // g
		if m.cursor != 0 {
			m.cursor = 0
			m.refreshPreview()
		}
	case key.Matches(msg, km.GoBottom): // G
		if last := len(m.entries) - 1; last >= 0 && m.cursor != last {
			m.cursor = last
			m.refreshPreview()
		}
	}
	return m, nil
}
