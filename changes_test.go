package main

// changes_test.go — TDD for the changed-only aggregate view (modeChanges):
// a single-key flat list of every working-tree change, Enter jumps to the file's
// diff. Mirrors the modeSearch surface (search_test.go) minus the query box,
// sourced from m.git.changes instead of walkTree.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// allKeyBindings returns every key.Binding field on a KeyMap, paired with its field
// name, so a collision test can table-walk the whole map. It is hand-maintained:
// adding a KeyMap field means adding it here too (a missing field silently narrows
// the collision check, so keep this in sync with the KeyMap struct in keys.go).
func allKeyBindings(km KeyMap) map[string]key.Binding {
	return map[string]key.Binding{
		"MoveUp":                  km.MoveUp,
		"MoveDown":                km.MoveDown,
		"GoTop":                   km.GoTop,
		"GoBottom":                km.GoBottom,
		"OpenEntry":               km.OpenEntry,
		"GoUp":                    km.GoUp,
		"PreviewScrollUp":         km.PreviewScrollUp,
		"PreviewScrollDown":       km.PreviewScrollDown,
		"PreviewHalfPageUp":       km.PreviewHalfPageUp,
		"PreviewHalfPageDown":     km.PreviewHalfPageDown,
		"PreviewJumpTop":          km.PreviewJumpTop,
		"PreviewJumpBottom":       km.PreviewJumpBottom,
		"PreviewScrollLeft":       km.PreviewScrollLeft,
		"PreviewScrollRight":      km.PreviewScrollRight,
		"PreviewHScrollHalfLeft":  km.PreviewHScrollHalfLeft,
		"PreviewHScrollHalfRight": km.PreviewHScrollHalfRight,
		"PreviewHScrollReset":     km.PreviewHScrollReset,
		"PreviewToggleWrap":       km.PreviewToggleWrap,
		"ToggleDiff":              km.ToggleDiff,
		"SelectMode":              km.SelectMode,
		"Rename":                  km.Rename,
		"Delete":                  km.Delete,
		"OpenInEditor":            km.OpenInEditor,
		"FocusToggle":             km.FocusToggle,
		"Search":                  km.Search,
		"Changes":                 km.Changes,
		"CommandPalette":          km.CommandPalette,
		"FullHelp":                km.FullHelp,
		"Back":                    km.Back,
		"Yank":                    km.Yank,
		"CopyContent":             km.CopyContent,
		"CopySelection":           km.CopySelection,
		"Quit":                    km.Quit,
	}
}

// changesModelWith builds a sized model whose git.changes map is set directly to
// the given repo-relative change set, with the given jail prefix. No real repo is
// needed: changedRows is a pure inversion of the in-memory map, so the test drives
// it with a hand-built snapshot (the keys are repo-relative slash paths, exactly
// what collectGitState produces). repoRoot is set non-empty so git mode is ON.
func changesModelWith(t *testing.T, prefix string, changes map[string]gitChange) model {
	t.Helper()
	root := t.TempDir()
	m := modelAt(t, root, 120, 30)
	m.git.repoRoot = "/repo" // non-empty ⇒ git mode on (the actual value is irrelevant to changedRows)
	m.gitRootPrefix = prefix
	m.git.changes = changes
	return m
}

// rowNames extracts the entry names (root-relative slash paths) from a row list.
func rowNames(rows []entry) []string {
	out := make([]string, 0, len(rows))
	for _, e := range rows {
		out = append(out, e.name)
	}
	return out
}

// TestChangedRowsSortedRootRelative pins the core inversion: m.git.changes (keyed
// repo-relative) → a list of entries named ROOT-relative (slash form), sorted by
// name, one per change. With an empty prefix the repo root IS the jail root, so the
// keys pass through unchanged but become sorted entry names.
func TestChangedRowsSortedRootRelative(t *testing.T) {
	m := changesModelWith(t, "", map[string]gitChange{
		"src/a.go":  {code: gitModified, added: 41, deleted: 3, hasDelta: true},
		"README.md": {code: gitUntracked, added: 5, hasDelta: true},
		"x/y/z.txt": {code: gitAdded},
	})
	rows := m.changedRows()
	got := rowNames(rows)
	want := []string{"README.md", "src/a.go", "x/y/z.txt"} // alpha by relPath
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("changedRows names = %v, want %v (sorted root-relative slash paths)", got, want)
	}
}

// TestChangedRowsJailPrefixStrippedAndExcluded pins the jail discipline: when the
// jail root is a SUBDIR of the repo (gitRootPrefix="sub"), a change inside the jail
// has its prefix stripped to a root-relative name, and a change OUTSIDE the jail
// (no "sub/" prefix) is EXCLUDED — the same root-jail guard repoRelKey enforces.
func TestChangedRowsJailPrefixStrippedAndExcluded(t *testing.T) {
	m := changesModelWith(t, "sub", map[string]gitChange{
		"sub/a.go":      {code: gitModified, added: 1, hasDelta: true}, // inside the jail → "a.go"
		"sub/deep/b.go": {code: gitAdded},                              // inside → "deep/b.go"
		"other/c.go":    {code: gitModified},                           // OUTSIDE the jail → excluded
		"top.go":        {code: gitModified},                           // OUTSIDE (no sub/ prefix) → excluded
	})
	rows := m.changedRows()
	got := rowNames(rows)
	want := []string{"a.go", "deep/b.go"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("changedRows under jail prefix = %v, want %v (prefix stripped, out-of-jail excluded)", got, want)
	}
}

// TestChangedRowsEmptyAndNoRepo pins the two degenerate inputs: a clean tree
// (empty changes) yields an empty (non-nil-panic) list, and git mode off
// (repoRoot=="") yields an empty list regardless of any stale map.
func TestChangedRowsEmptyAndNoRepo(t *testing.T) {
	clean := changesModelWith(t, "", map[string]gitChange{})
	if rows := clean.changedRows(); len(rows) != 0 {
		t.Errorf("clean tree: changedRows = %v, want empty", rowNames(rows))
	}

	noRepo := changesModelWith(t, "", map[string]gitChange{"a.go": {code: gitModified}})
	noRepo.git.repoRoot = "" // git mode OFF
	if rows := noRepo.changedRows(); len(rows) != 0 {
		t.Errorf("git mode off: changedRows = %v, want empty", rowNames(rows))
	}
}

// TestChangesViewRowCarriesBadge pins that a derived row, resolved through the
// SAME indicatorFor path the list pane uses (base = root in changes mode), yields
// the file's badge + colored delta — so the aggregate reads identically to the
// inline listing badges (consistency-is-kindness).
func TestChangesViewRowCarriesBadge(t *testing.T) {
	m := changesModelWith(t, "", map[string]gitChange{
		"src/a.go": {code: gitModified, added: 41, deleted: 3, hasDelta: true},
	})
	m.mode = modeChanges
	m.entries = m.changedRows()
	// indicatorFor resolves a changes-mode row against the root (previewBaseDir),
	// so the badge for "src/a.go" must be M with delta "+41 -3".
	var sel entry
	for _, e := range m.entries {
		if e.name == "src/a.go" {
			sel = e
		}
	}
	ind := m.indicatorFor(m.changesBaseDir(), sel)
	if ind == nil {
		t.Fatalf("indicatorFor returned nil for a changed row in changes mode")
	}
	if ind.badge != "M" {
		t.Errorf("badge = %q, want M", ind.badge)
	}
	if ind.delta != "+41 -3" {
		t.Errorf("delta = %q, want %q", ind.delta, "+41 -3")
	}
}

// changesRepoModel builds a real git repo with changes spread across two
// directories plus an untracked file (the same shape as the T4 dogfood case), and
// delivers the git snapshot through Update so View()/changedRows see real badges.
// cwd is the repo root, cursor at top. It is the harness for the mode-transition,
// Enter-to-jump, and telemetry tests. The returned model uses the given recorder so
// telemetry can be asserted; pass noopRecorder{} when telemetry is not under test.
func changesRepoModel(t *testing.T, rec Recorder) model {
	t.Helper()
	repo := t.TempDir()
	gitExec(t, repo, "init")
	mustWrite(t, repo, "README.md", "# Project\n")
	mustMkdir(t, repo, "src")
	mustWrite(t, filepath.Join(repo, "src"), "app.go", "package src\n")
	gitExec(t, repo, "add", ".")
	gitExec(t, repo, "commit", "-m", "init")
	// Change one file in root and one in src/ (different dirs); add an untracked.
	mustWrite(t, repo, "README.md", "# Project\n\nnow with docs\n")
	mustWrite(t, filepath.Join(repo, "src"), "app.go", "package src\n\nfunc App() {}\n")
	mustWrite(t, repo, "scratch.tmp", "untracked\n")

	m := modelAt(t, repo, 120, 30)
	m.tel = rec
	if m.git.repoRoot == "" {
		m.git.repoRoot = detectRepoRoot(m.root)
		m.gitRootPrefix = repoRelPrefix(m.git.repoRoot, m.root)
	}
	state, _ := collectGitState(m.git.repoRoot, nil)
	m = dogPressMsg(t, m, gitRefreshedMsg{gen: m.gitGen, state: state})
	return m
}

// TestEnterChangesFromNormal pins the mode transition: pressing `c` in a repo with
// changes enters modeChanges, repurposes m.entries as the aggregate list, resets
// cursor/scroll, and snapshots the pre-view state. The list must contain BOTH the
// root change (README.md) and the NESTED change (src/app.go) — the latter is the
// proof: the current-dir badges could only show a ● rollup on src/, never app.go.
func TestEnterChangesFromNormal(t *testing.T) {
	m := changesRepoModel(t, noopRecorder{})
	preCwd, preEntries, preCursor := m.cwd, append([]entry(nil), m.entries...), m.cursor

	m = dogPress(t, m, keyRune('c'))
	if m.mode != modeChanges {
		t.Fatalf("after `c`, mode = %v, want modeChanges", m.mode)
	}
	if m.cursor != 0 || m.listTop != 0 {
		t.Errorf("enter changes: cursor=%d listTop=%d, want 0/0", m.cursor, m.listTop)
	}
	names := rowNames(m.entries)
	if !contains(names, "README.md") {
		t.Errorf("changes list %v missing root change README.md", names)
	}
	if !contains(names, "src/app.go") {
		t.Errorf("changes list %v missing NESTED change src/app.go (the aggregate-view payoff)", names)
	}
	if !contains(names, "scratch.tmp") {
		t.Errorf("changes list %v missing untracked scratch.tmp", names)
	}
	// Snapshot taken (asserted via the Esc restore below).
	if m.changesSavedCwd != preCwd || m.changesSavedCursor != preCursor ||
		len(m.changesSavedEntries) != len(preEntries) {
		t.Errorf("pre-view state not snapshotted on enter")
	}
}

// TestChangesNoOpOutsideRepo pins FR9: `c` outside a git repo is a NO-OP — mode
// stays normal, entries untouched (nothing to aggregate, git mode off).
func TestChangesNoOpOutsideRepo(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, "a.txt", "hello\n")
	m := modelAt(t, root, 120, 30) // no git repo → repoRoot stays ""
	if m.git.repoRoot != "" {
		t.Skip("temp dir unexpectedly inside a repo")
	}
	preEntries := rowNames(m.entries)

	m = dogPress(t, m, keyRune('c'))
	if m.mode != modeNormal {
		t.Errorf("outside a repo, `c` must stay modeNormal; got %v", m.mode)
	}
	if strings.Join(rowNames(m.entries), "|") != strings.Join(preEntries, "|") {
		t.Errorf("outside a repo, `c` must not touch the listing")
	}
}

// TestChangesEmptyTreePlaceholder pins the clean-tree branch: a repo with NO
// working-tree changes enters modeChanges with an empty list and a "(no changes)"
// status — the user asked "what changed?", answered "nothing", visibly (not a crash,
// not a silent no-op).
func TestChangesEmptyTreePlaceholder(t *testing.T) {
	repo := t.TempDir()
	gitExec(t, repo, "init")
	mustWrite(t, repo, "committed.go", "package main\n")
	gitExec(t, repo, "add", ".")
	gitExec(t, repo, "commit", "-m", "init")

	m := modelAt(t, repo, 120, 30)
	if m.git.repoRoot == "" {
		m.git.repoRoot = detectRepoRoot(m.root)
		m.gitRootPrefix = repoRelPrefix(m.git.repoRoot, m.root)
	}
	state, _ := collectGitState(m.git.repoRoot, nil)
	m = dogPressMsg(t, m, gitRefreshedMsg{gen: m.gitGen, state: state})

	m = dogPress(t, m, keyRune('c'))
	if m.mode != modeChanges {
		t.Fatalf("clean tree: `c` must still enter modeChanges; got %v", m.mode)
	}
	if len(m.entries) != 0 {
		t.Errorf("clean tree: changes list = %v, want empty", rowNames(m.entries))
	}
	if m.statusMsg != "(no changes)" {
		t.Errorf("clean tree: status = %q, want %q", m.statusMsg, "(no changes)")
	}
}

// TestExitChangesRestoresState pins the Esc round trip (mirror of exitSearchRestore):
// Esc restores the EXACT pre-view cwd / entries / cursor / fsSig.
func TestExitChangesRestoresState(t *testing.T) {
	m := changesRepoModel(t, noopRecorder{})
	// Put the cursor and scroll somewhere non-trivial pre-view.
	moveCursorToAny(t, &m, "src")
	m.listTop = 1
	preCwd, preCursor, preListTop, preFsSig := m.cwd, m.cursor, m.listTop, m.fsSig
	preEntries := rowNames(m.entries)

	m = dogPress(t, m, keyRune('c'))
	if m.mode != modeChanges {
		t.Fatalf("setup: `c` did not enter modeChanges (mode=%v)", m.mode)
	}
	m = dogPress(t, m, keyEsc())

	if m.mode != modeNormal {
		t.Errorf("Esc: mode = %v, want modeNormal", m.mode)
	}
	if m.cwd != preCwd {
		t.Errorf("Esc: cwd = %q, want %q", m.cwd, preCwd)
	}
	if m.cursor != preCursor {
		t.Errorf("Esc: cursor = %d, want %d", m.cursor, preCursor)
	}
	if m.listTop != preListTop {
		t.Errorf("Esc: listTop = %d, want %d", m.listTop, preListTop)
	}
	if m.fsSig != preFsSig {
		t.Errorf("Esc: fsSig = %d, want %d (poll-loop baseline must be restored)", m.fsSig, preFsSig)
	}
	if strings.Join(rowNames(m.entries), "|") != strings.Join(preEntries, "|") {
		t.Errorf("Esc: entries not restored")
	}
}

// TestChangesRowPreviewShowsDiffNotBinary pins the bug the visual verdict caught:
// a changes row's entry must carry the file's real size, so navigating onto a
// tracked-modified TEXT file in the changes list previews its DIFF — not a
// "(binary files differ — 0B)" placeholder caused by a zero-size entry making
// readPreviewBytes read 0 bytes (kind "empty" → modifiedBinary path). This is the
// in-place preview (cursor inside the changes list, BEFORE Enter), the surface the
// dump/visual frame exercises.
func TestChangesRowPreviewShowsDiffNotBinary(t *testing.T) {
	m := changesRepoModel(t, noopRecorder{})
	m.diffOn = true
	m = dogPress(t, m, keyRune('c'))

	moveChangesCursorTo(t, &m, "src/app.go") // tracked-modified Go text file
	m.pendingWidth = 0
	m.renderNow()

	if !m.previewIsDiff {
		t.Fatalf("a changes row on a modified text file must preview as a DIFF (previewIsDiff=false); preview=%q",
			strings.Join(m.preview, "\n"))
	}
	plain := ansi.Strip(strings.Join(m.preview, "\n"))
	if strings.Contains(plain, "binary files differ") {
		t.Errorf("changes row preview must not be the binary placeholder; got %q", plain)
	}
	if !strings.Contains(plain, "func App") { // code diff: no gutter, the green wash marks the added line
		t.Errorf("changes row diff must show the added line; got %q", plain)
	}
}

// TestChangesRefreshNoPreviewChurn pins the poll-loop invariant (mirror of
// syncFromDisk's "selected file unchanged → leave the preview alone", model.go):
// while parked in modeChanges, a git refresh that leaves the SELECTED change
// byte-identical must NOT re-render its preview. Without the guard, every ~1s poll
// tick re-stamps a placeholder + re-dispatches the diff (a git exec) for an
// unchanged file — the bug-poll-preview-rerender class. Discriminating check: render
// to completion (srcWidth set), deliver a SECOND identical snapshot, assert
// syncPreview returns nil and srcWidth stays set (no re-dispatch).
func TestChangesRefreshNoPreviewChurn(t *testing.T) {
	m := changesRepoModel(t, noopRecorder{})
	m.diffOn = true
	m = dogPress(t, m, keyRune('c'))
	moveChangesCursorTo(t, &m, "src/app.go")
	m.pendingWidth = 0
	m.renderNow() // diff rendered to completion → srcWidth now set
	if m.srcWidth == 0 {
		t.Fatalf("setup: diff did not render (srcWidth=0); preview=%q", strings.Join(m.preview, "\n"))
	}
	widthBefore := m.srcWidth

	// Deliver a SECOND identical git snapshot the way the 1s poll tick would. The
	// selected change is byte-identical, so the preview must be left alone.
	state, _ := collectGitState(m.git.repoRoot, nil)
	m = dogPressMsg(t, m, gitRefreshedMsg{gen: m.gitGen, state: state})

	if m.srcWidth != widthBefore {
		t.Errorf("an unchanged selected change must NOT reset srcWidth (%d → %d) — that re-renders the diff every poll tick", widthBefore, m.srcWidth)
	}
	if cmd := m.syncPreview(); cmd != nil {
		t.Errorf("syncPreview re-dispatched a render for an unchanged selected change — preview churn on every poll tick")
	}
	// The selection must stay put (the cursor is still on src/app.go).
	if currentName(m) != "src/app.go" {
		t.Errorf("refresh moved the selection to %q, want src/app.go", currentName(m))
	}
}

// TestChangesRefreshPicksUpNewChange pins the complement of the no-churn guard: a
// git refresh that introduces a NEW change while parked in modeChanges must surface
// it in the list (live-refresh, FR10) without the user re-opening the view.
func TestChangesRefreshPicksUpNewChange(t *testing.T) {
	m := changesRepoModel(t, noopRecorder{})
	m = dogPress(t, m, keyRune('c'))
	if contains(rowNames(m.entries), "newly.go") {
		t.Fatal("setup: newly.go should not exist yet")
	}

	// The agent adds a brand-new file while the changes view is open.
	mustWrite(t, m.git.repoRoot, "newly.go", "package main\n")
	state, _ := collectGitState(m.git.repoRoot, nil)
	m = dogPressMsg(t, m, gitRefreshedMsg{gen: m.gitGen, state: state})

	if !contains(rowNames(m.entries), "newly.go") {
		t.Errorf("a new change must appear in the open changes view; rows=%v", rowNames(m.entries))
	}
}

// TestChangedRowsCarrySize pins the underlying fix: a derived row entry carries the
// file's real on-disk size (so readPreviewBytes reads the whole file, not 0 bytes).
func TestChangedRowsCarrySize(t *testing.T) {
	m := changesRepoModel(t, noopRecorder{})
	for _, e := range m.changedRows() {
		if e.name == "src/app.go" {
			if e.size <= 0 {
				t.Errorf("changes row %q must carry a non-zero size; got %d", e.name, e.size)
			}
		}
	}
}

// TestChangesEnterJumpToDiff is the /goal proof: from the aggregate list select the
// nested modified file src/app.go → Enter → land in src/ with the cursor on app.go,
// and (diffOn default true) the preview shows its DIFF hunk. list→Enter→review, the
// cross-directory teleport + diff landing in one move.
func TestChangesEnterJumpToDiff(t *testing.T) {
	m := changesRepoModel(t, noopRecorder{})
	m.diffOn = true // ship default (newModel); modelAt builds the struct directly

	m = dogPress(t, m, keyRune('c'))
	if m.mode != modeChanges {
		t.Fatalf("setup: `c` did not enter modeChanges (mode=%v)", m.mode)
	}
	// Move onto src/app.go in the (sorted) list.
	moveChangesCursorTo(t, &m, "src/app.go")

	m = dogPress(t, m, keyEnter())
	if m.mode != modeNormal {
		t.Fatalf("Enter: mode = %v, want modeNormal", m.mode)
	}
	if filepath.Base(m.cwd) != "src" {
		t.Errorf("Enter: cwd = %q, want it to end in src", m.cwd)
	}
	if currentName(m) != "app.go" {
		t.Errorf("Enter: cursor on %q, want app.go", currentName(m))
	}
	// Drive the async diff render to completion (the event loop would run the
	// batched Cmd dogPress discards). previewIsDiff was state-selected on landing.
	m.pendingWidth = 0
	m.renderNow()
	if !m.previewIsDiff {
		t.Fatalf("landing on modified app.go must select the diff path (previewIsDiff=false)")
	}
	plain := ansi.Strip(strings.Join(m.preview, "\n"))
	if !strings.Contains(plain, "func App") { // code diff: no gutter, the green wash marks the added line
		t.Errorf("diff hunk must show the added line; preview =\n%s", plain)
	}
}

// TestChangesEnterDeletedFileTolerated pins the deleted-file edge: a change whose
// file was deleted on disk IS listed (it is a change), but Enter on it must NOT cd
// into a dead path — it surfaces "⚠ file no longer on disk" and stays in the changes
// list, no panic.
func TestChangesEnterDeletedFileTolerated(t *testing.T) {
	repo := t.TempDir()
	gitExec(t, repo, "init")
	mustWrite(t, repo, "keep.go", "package main\n")
	mustWrite(t, repo, "gone.go", "package main\n")
	gitExec(t, repo, "add", ".")
	gitExec(t, repo, "commit", "-m", "init")
	// Delete gone.go from disk (git sees it as a working-tree deletion).
	if err := os.Remove(filepath.Join(repo, "gone.go")); err != nil {
		t.Fatal(err)
	}

	m := modelAt(t, repo, 120, 30)
	if m.git.repoRoot == "" {
		m.git.repoRoot = detectRepoRoot(m.root)
		m.gitRootPrefix = repoRelPrefix(m.git.repoRoot, m.root)
	}
	state, _ := collectGitState(m.git.repoRoot, nil)
	m = dogPressMsg(t, m, gitRefreshedMsg{gen: m.gitGen, state: state})

	m = dogPress(t, m, keyRune('c'))
	if m.mode != modeChanges {
		t.Fatalf("setup: `c` did not enter modeChanges (mode=%v)", m.mode)
	}
	// gone.go IS in the list (it's a change).
	if !contains(rowNames(m.entries), "gone.go") {
		t.Fatalf("deleted file gone.go must still be LISTED as a change; got %v", rowNames(m.entries))
	}
	moveChangesCursorTo(t, &m, "gone.go")
	m = dogPress(t, m, keyEnter())
	if !strings.Contains(m.statusMsg, "no longer on disk") {
		t.Errorf("Enter on a deleted change: status = %q, want a 'no longer on disk' warning", m.statusMsg)
	}
	if m.mode != modeChanges {
		t.Errorf("Enter on a deleted change: must stay in modeChanges; got %v", m.mode)
	}
}

// TestChangesTelemetry pins the two telemetry events (mirror prd-preview-diff-view
// checklist 11b): `c` records action.changes_view_open; Enter-to-file records
// action.changes_jump{rel}.
func TestChangesTelemetry(t *testing.T) {
	rec := &fieldRecorder{}
	m := changesRepoModel(t, rec)
	m.diffOn = true

	m = dogPress(t, m, keyRune('c'))
	if _, ok := rec.last("action.changes_view_open"); !ok {
		t.Errorf("`c` must record action.changes_view_open; events=%v", rec.names())
	}

	moveChangesCursorTo(t, &m, "src/app.go")
	m = dogPress(t, m, keyEnter())
	fields, ok := rec.last("action.changes_jump")
	if !ok {
		t.Fatalf("Enter-to-file must record action.changes_jump; events=%v", rec.names())
	}
	if fields["rel"] != "src/app.go" {
		t.Errorf("changes_jump rel = %v, want src/app.go", fields["rel"])
	}
}

// TestCopyContentInChanges is the HEADLINE catch (FR7/D6): in modeChanges `Y` copies
// the change's CURRENT on-disk content, resolving the path via previewBaseDir()=root —
// NOT selectedAbsPath()/m.cwd. The discriminator is m.cwd != m.root: we descend into
// src/ BEFORE entering changes, so a wrong m.cwd-based read would target
// root/src/src/app.go (a non-existent file → "⚠ <err>", no record), while
// previewBaseDir() reads the real root/src/app.go. The recorded byte count must equal
// the real on-disk content (NOT the diff text shown in the preview).
func TestCopyContentInChanges(t *testing.T) {
	rec := &fieldRecorder{}
	m := changesRepoModel(t, rec)
	m.diffOn = true
	// Descend into src/ so m.cwd != m.root — the discriminator for the D6 catch.
	moveCursorToAny(t, &m, "src")
	m = dogPress(t, m, keyRune('l'))
	if filepath.Base(m.cwd) != "src" {
		t.Fatalf("setup: cwd should be …/src, got %q", m.cwd)
	}

	m = dogPress(t, m, keyRune('c'))
	if m.mode != modeChanges {
		t.Fatalf("setup: `c` did not enter modeChanges (mode=%v)", m.mode)
	}
	moveChangesCursorTo(t, &m, "src/app.go") // root-relative name in the flat list
	m.pendingWidth = 0
	m.renderNow()
	if !m.previewIsDiff {
		t.Fatalf("setup: src/app.go must preview as a DIFF in changes mode (previewIsDiff=false)")
	}

	// The real on-disk content lives at <root>/src/app.go (resolved via previewBaseDir).
	raw, err := os.ReadFile(filepath.Join(m.root, "src", "app.go"))
	if err != nil {
		t.Fatalf("read on-disk fixture: %v", err)
	}

	m = dogPress(t, m, keyRune('Y'))
	fields, recorded := rec.last("action.copy_content")
	if !recorded {
		t.Fatalf("`Y` in modeChanges must copy the change (record action.copy_content); status=%q events=%v", m.statusMsg, rec.names())
	}
	if n, _ := fields["bytes"].(int); n != len(raw) {
		t.Errorf("recorded bytes = %d, want the real on-disk content %d at root/src/app.go (resolve via previewBaseDir, not m.cwd; not the diff text)", n, len(raw))
	}
	if fields["name"] != "src/app.go" {
		t.Errorf("recorded name = %v, want the root-relative src/app.go", fields["name"])
	}
	// Still parked in modeChanges (copy is a side-effect, not a teleport).
	if m.mode != modeChanges {
		t.Errorf("`Y` in modeChanges must stay in modeChanges; got %v", m.mode)
	}
}

// TestCopyContentChangesDeletedFile pins FR7's deleted-file edge: a change whose file
// was deleted on disk IS listed, but `Y` on it cannot read content — os.ReadFile fails,
// surfacing "⚠ <err>" via the shared error branch (D6), recording nothing. No panic.
func TestCopyContentChangesDeletedFile(t *testing.T) {
	rec := &fieldRecorder{}
	repo := t.TempDir()
	gitExec(t, repo, "init")
	mustWrite(t, repo, "gone.go", "package main\n")
	gitExec(t, repo, "add", ".")
	gitExec(t, repo, "commit", "-m", "init")
	if err := os.Remove(filepath.Join(repo, "gone.go")); err != nil {
		t.Fatal(err)
	}
	m := modelAt(t, repo, 120, 30)
	m.tel = rec
	if m.git.repoRoot == "" {
		m.git.repoRoot = detectRepoRoot(m.root)
		m.gitRootPrefix = repoRelPrefix(m.git.repoRoot, m.root)
	}
	state, _ := collectGitState(m.git.repoRoot, nil)
	m = dogPressMsg(t, m, gitRefreshedMsg{gen: m.gitGen, state: state})
	m = dogPress(t, m, keyRune('c'))
	moveChangesCursorTo(t, &m, "gone.go")

	m = dogPress(t, m, keyRune('Y'))
	if !strings.HasPrefix(m.statusMsg, "⚠ ") {
		t.Errorf("`Y` on a deleted change: status = %q, want a '⚠ <err>' refusal", m.statusMsg)
	}
	if _, recorded := rec.last("action.copy_content"); recorded {
		t.Errorf("a failed read must NOT record action.copy_content")
	}
}

// moveChangesCursorTo snaps the changes-list cursor onto a row by name (test SETUP),
// refreshing the preview the way j/k would. Fails if the row is absent.
func moveChangesCursorTo(t *testing.T, m *model, name string) {
	t.Helper()
	for i, e := range m.entries {
		if e.name == name {
			m.cursor = i
			m.refreshPreview()
			return
		}
	}
	t.Fatalf("changes list has no row %q; rows=%v", name, rowNames(m.entries))
}

// TestChangesViewRendersBadgeRelpathDelta pins the render: each changes-list row
// renders "<badge> <relpath> <delta>" — the relpath flush-left, the colored badge +
// muted delta flush-right — IDENTICAL in shape to the inline listing-row indicator
// (it reuses renderEntryRow via the same indicatorFor path). The README.md row must
// carry its untracked "?" badge; the src/app.go row its modified "M" + a delta.
func TestChangesViewRendersBadgeRelpathDelta(t *testing.T) {
	m := changesRepoModel(t, noopRecorder{})
	m = dogPress(t, m, keyRune('c'))

	screen := seeContent(m) // ansi-stripped
	// scratch.tmp is the UNTRACKED file (committed-then-modified README.md is M);
	// its row must carry the untracked "?" badge.
	scratchRow := lineContaining(screen, "scratch.tmp")
	if scratchRow == "" {
		t.Fatalf("changes view did not render a scratch.tmp row:\n%s", screen)
	}
	if !strings.Contains(scratchRow, "?") {
		t.Errorf("scratch.tmp row should carry its untracked '?' badge; row=%q", strings.TrimSpace(scratchRow))
	}
	appRow := lineContaining(screen, "app.go")
	if appRow == "" {
		t.Fatalf("changes view did not render a src/app.go row:\n%s", screen)
	}
	if !strings.Contains(appRow, "src/app.go") {
		t.Errorf("nested change must render its ROOT-relative path src/app.go; row=%q", strings.TrimSpace(appRow))
	}
	if !strings.Contains(appRow, "M") {
		t.Errorf("src/app.go row should carry its modified 'M' badge; row=%q", strings.TrimSpace(appRow))
	}
}

// TestChangesRowWidthFits pins width-fitting: a long root-relative path must not
// blow past the list pane width — every rendered changes row stays ≤ the list-pane
// inner columns (the same fit discipline renderEntryRow gives every listing row).
func TestChangesRowWidthFits(t *testing.T) {
	repo := t.TempDir()
	gitExec(t, repo, "init")
	mustWrite(t, repo, "seed.go", "package main\n")
	gitExec(t, repo, "add", ".")
	gitExec(t, repo, "commit", "-m", "init")
	// A deeply nested untracked file whose root-relative path is far wider than a
	// narrow list pane.
	deep := filepath.Join("a", "very", "deeply", "nested", "directory", "structure")
	if err := os.MkdirAll(filepath.Join(repo, deep), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(repo, deep), "a_file_with_a_long_name.go", "package x\n")

	m := modelAt(t, repo, 90, 30)
	if m.git.repoRoot == "" {
		m.git.repoRoot = detectRepoRoot(m.root)
		m.gitRootPrefix = repoRelPrefix(m.git.repoRoot, m.root)
	}
	state, _ := collectGitState(m.git.repoRoot, nil)
	m = dogPressMsg(t, m, gitRefreshedMsg{gen: m.gitGen, state: state})
	m = dogPress(t, m, keyRune('c'))

	g := m.layout()
	for _, row := range strings.Split(m.renderList(g.leftInner, g.bodyH), "\n") {
		if w := lipgloss.Width(row); w > g.leftInner {
			t.Errorf("changes row width %d exceeds list pane %d: %q", w, g.leftInner, ansi.Strip(row))
		}
	}
}

// TestChangesEmptyListPanePlaceholder pins the clean-tree LIST PANE text: an empty
// changes list reads "(no changes)" in the pane, not the misleading "(empty
// directory)" — the user is looking at "what changed", and the answer is "nothing".
func TestChangesEmptyListPanePlaceholder(t *testing.T) {
	repo := t.TempDir()
	gitExec(t, repo, "init")
	mustWrite(t, repo, "committed.go", "package main\n")
	gitExec(t, repo, "add", ".")
	gitExec(t, repo, "commit", "-m", "init")

	m := modelAt(t, repo, 120, 30)
	if m.git.repoRoot == "" {
		m.git.repoRoot = detectRepoRoot(m.root)
		m.gitRootPrefix = repoRelPrefix(m.git.repoRoot, m.root)
	}
	state, _ := collectGitState(m.git.repoRoot, nil)
	m = dogPressMsg(t, m, gitRefreshedMsg{gen: m.gitGen, state: state})
	m = dogPress(t, m, keyRune('c'))

	g := m.layout()
	pane := ansi.Strip(m.renderList(g.leftInner, g.bodyH))
	if !strings.Contains(pane, "(no changes)") {
		t.Errorf("clean-tree changes pane should read '(no changes)'; got %q", strings.TrimSpace(pane))
	}
	if strings.Contains(pane, "empty directory") {
		t.Errorf("clean-tree changes pane must not say 'empty directory'; got %q", strings.TrimSpace(pane))
	}
}

// TestChangesEmptyPreviewPanePlaceholder pins the clean-tree PREVIEW pane (right pane)
// via the FULL View() — not renderList. refreshPreview's empty-entries branch must
// mirror renderList's mode gate: in modeChanges an empty list answers "what changed?"
// with "(no changes)", so the preview pane must NOT show the misleading "(empty
// directory)" (FR8/D9). The discriminating assertion is the ABSENCE of "empty
// directory" — the buggy View() already carries "(no changes)" twice (list pane +
// status bar), so only the absence check fails-first on the preview-pane bug.
func TestChangesEmptyPreviewPanePlaceholder(t *testing.T) {
	repo := t.TempDir()
	gitExec(t, repo, "init")
	mustWrite(t, repo, "committed.go", "package main\n")
	gitExec(t, repo, "add", ".")
	gitExec(t, repo, "commit", "-m", "init")

	m := modelAt(t, repo, 120, 30)
	if m.git.repoRoot == "" {
		m.git.repoRoot = detectRepoRoot(m.root)
		m.gitRootPrefix = repoRelPrefix(m.git.repoRoot, m.root)
	}
	state, _ := collectGitState(m.git.repoRoot, nil)
	m = dogPressMsg(t, m, gitRefreshedMsg{gen: m.gitGen, state: state})
	m = dogPress(t, m, keyRune('c'))

	view := ansi.Strip(m.View().Content)
	if strings.Contains(view, "empty directory") {
		t.Errorf("clean-tree changes preview pane must not say 'empty directory'; full View=%q", view)
	}
	// The list pane + the now-fixed preview pane both read "(no changes)": at least two
	// occurrences prove the preview pane carries it, not only the list pane/status bar.
	if n := strings.Count(view, "(no changes)"); n < 2 {
		t.Errorf("clean-tree View should read '(no changes)' in both list and preview panes; got %d occurrence(s) in %q", n, view)
	}
}

// TestChangesStatusBarHints pins the footer in changes mode: it surfaces the
// changes-view-relevant keys (open, back) sourced from the keymap, NOT the
// normal-mode mutation hints (rename/delete are meaningless on an aggregate row).
func TestChangesStatusBarHints(t *testing.T) {
	m := changesRepoModel(t, noopRecorder{})
	m = dogPress(t, m, keyRune('c'))
	status := ansi.Strip(m.renderStatus())
	if !strings.Contains(status, "esc") {
		t.Errorf("changes status bar should hint esc (back); got %q", status)
	}
	if strings.Contains(status, "rename") || strings.Contains(status, "delete") {
		t.Errorf("changes status bar must not show mutation hints; got %q", status)
	}
}

// TestChangesKeyBinding pins the new `c` binding: keyRune('c') matches Changes,
// and `c` is not bound to any OTHER action (the single-source-of-truth keymap
// must not double-map a key). Bindings that legitimately share a code (the
// navigation/preview-scroll pairs like MoveDown≡PreviewScrollDown) are excluded
// from the collision check by name, since they share by design.
func TestChangesKeyBinding(t *testing.T) {
	km := defaultKeyMap()
	if !key.Matches(keyRune('c'), km.Changes) {
		t.Fatalf("`c` must match the Changes binding")
	}

	// `c` must not collide with any binding outside the Changes field. The
	// navigation⇄preview-scroll pairs share key codes on purpose; none of them
	// uses `c`, so any other `c` match is a real collision.
	for name, b := range allKeyBindings(km) {
		if name == "Changes" {
			continue
		}
		if key.Matches(keyRune('c'), b) {
			t.Errorf("`c` collides with binding %q — it must be free for Changes", name)
		}
	}
}
