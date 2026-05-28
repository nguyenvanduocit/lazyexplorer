package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestDirSigDetectsChange asserts the poll-loop fingerprint reacts to every kind
// of change the user named — add, remove, modify — and to an in-place edit that
// keeps the size identical (caught only via mtime). Equal listings must hash
// equal, or the gate would rebuild the view every tick (flicker).
func TestDirSigDetectsChange(t *testing.T) {
	t0 := time.Unix(1_700_000_000, 0)
	base := []entry{
		{name: "a.txt", size: 10, modTime: t0},
		{name: "sub", isDir: true, modTime: t0},
	}
	sig := dirSig(base)

	cases := map[string][]entry{
		"identical": {
			{name: "a.txt", size: 10, modTime: t0},
			{name: "sub", isDir: true, modTime: t0},
		},
		"added file": append(append([]entry{}, base...), entry{name: "b.txt", size: 1, modTime: t0}),
		"removed file": {
			{name: "sub", isDir: true, modTime: t0},
		},
		"size changed": {
			{name: "a.txt", size: 11, modTime: t0},
			{name: "sub", isDir: true, modTime: t0},
		},
		"mtime changed (same size)": {
			{name: "a.txt", size: 10, modTime: t0.Add(time.Second)},
			{name: "sub", isDir: true, modTime: t0},
		},
		"renamed": {
			{name: "a2.txt", size: 10, modTime: t0},
			{name: "sub", isDir: true, modTime: t0},
		},
	}
	for name, entries := range cases {
		got := dirSig(entries)
		if name == "identical" {
			if got != sig {
				t.Errorf("%s: sig changed (%d != %d) — would cause flicker", name, got, sig)
			}
			continue
		}
		if got == sig {
			t.Errorf("%s: sig unchanged (%d) — change would go unnoticed", name, got)
		}
	}
}

// TestSyncReflectsAdd is the core bug: a file created beside us must appear
// without any user navigation.
func TestSyncReflectsAdd(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newModel(dir, noopRecorder{})
	m.width, m.height = 80, 24

	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.syncFromDisk()

	if !hasEntry(m.entries, "b.txt") {
		t.Fatalf("b.txt added on disk but not reflected; entries=%v", names(m.entries))
	}
}

// TestSyncReflectsDelete asserts a removed file disappears from the list.
func TestSyncReflectsDelete(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := newModel(dir, noopRecorder{})
	m.width, m.height = 80, 24

	if err := os.Remove(filepath.Join(dir, "a.txt")); err != nil {
		t.Fatal(err)
	}
	m.syncFromDisk()

	if hasEntry(m.entries, "a.txt") {
		t.Fatalf("a.txt removed on disk but still listed; entries=%v", names(m.entries))
	}
}

// TestSyncPreservesSelectionByName guards against the cursor silently re-pointing
// at a neighbour: with "b.txt" selected, adding "a.txt" (which sorts above it)
// must keep the selection on "b.txt", not on whatever now sits at the old index.
func TestSyncPreservesSelectionByName(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newModel(dir, noopRecorder{})
	m.width, m.height = 80, 24
	// cursor is on b.txt (the only entry).
	if m.entries[m.cursor].name != "b.txt" {
		t.Fatalf("setup: expected cursor on b.txt, got %q", m.entries[m.cursor].name)
	}

	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.syncFromDisk()

	if got := m.entries[m.cursor].name; got != "b.txt" {
		t.Errorf("selection jumped: cursor on %q, want b.txt (entries=%v)", got, names(m.entries))
	}
}

// TestSyncGateNoChange asserts the dirSig gate makes an unchanged directory a
// true no-op: previewTop (and selection) survive untouched. If the gate were
// missing, refreshPreview would reset the scroll on every tick.
func TestSyncGateNoChange(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newModel(dir, noopRecorder{})
	m.width, m.height = 80, 24
	m.previewTop = 7 // pretend the user scrolled the preview
	sigBefore := m.fsSig

	m.syncFromDisk() // nothing changed on disk

	if m.fsSig != sigBefore {
		t.Errorf("fsSig changed on a no-op sync: %d != %d", m.fsSig, sigBefore)
	}
	if m.previewTop != 7 {
		t.Errorf("previewTop reset to %d on a no-op sync; gate should have returned early", m.previewTop)
	}
}

// TestRecoverVanishedCwd asserts that deleting the directory we're viewing climbs
// us back to a live ancestor instead of stranding the UI on a dead path.
func TestRecoverVanishedCwd(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	m := newModel(dir, noopRecorder{})
	m.width, m.height = 80, 24
	m.descend() // enter sub
	if m.cwd != sub {
		t.Fatalf("setup: expected cwd=%q, got %q", sub, m.cwd)
	}

	if err := os.RemoveAll(sub); err != nil {
		t.Fatal(err)
	}
	m.syncFromDisk()

	if m.cwd != dir {
		t.Errorf("cwd did not climb out of deleted dir: got %q, want %q", m.cwd, dir)
	}
	if hasEntry(m.entries, "sub") {
		t.Errorf("deleted 'sub' still listed after recovery: %v", names(m.entries))
	}
}

// eventCountRecorder counts telemetry events by name. refreshPreview fires
// exactly one "view.change" per call (its deferred record — model.go), so the
// "view.change" delta across a syncFromDisk is a direct, type-agnostic observable
// of whether refreshPreview ran — the gate this bug fix turns on (it works for a
// folder/".." selection too, where srcWidth doesn't apply). Active() stays false
// so the model keeps the production-off hot path (no time.Now in syncPreview).
type eventCountRecorder struct{ counts map[string]int }

func (r *eventCountRecorder) Record(name string, _ map[string]any) { r.counts[name]++ }
func (*eventCountRecorder) Shutdown(time.Duration)                 {}
func (*eventCountRecorder) Active() bool                           { return false }

// renderedModelAt builds a model rooted at dir with rec, loads it, and drives the
// async preview render to completion — so the selected file's preview is in the
// "rendered" state (srcWidth > 0) the running program reaches after its first
// frames. Mirrors modelAt + renderNow but lets the test inject a Recorder.
func renderedModelAt(t *testing.T, dir string, rec Recorder) model {
	t.Helper()
	m := model{
		root: dir, cwd: dir,
		leftRatio: 0.38, topRatio: 0.33,
		keymap: defaultKeyMap(),
		width:  100, height: 30,
		tel: rec,
	}
	m.reload()
	m.renderNow()
	return m
}

// TestSyncSkipsPreviewRefreshWhenSiblingChanges is the bug (bug-poll-preview-rerender):
// when a file BESIDE the open one changes, the poll loop must update the list but
// NOT re-render the open file's preview. The selected markdown is rendered, then a
// new sibling appears; the list must gain it while the markdown preview stays
// byte-identical with no fresh render. FAILS before the fix (refreshPreview runs
// unconditionally → srcWidth reset to 0, preview flashed back to the raw placeholder).
func TestSyncSkipsPreviewRefreshWhenSiblingChanges(t *testing.T) {
	dir := t.TempDir()
	// "doc.md" sorts before "sibling.go" ('d' < 's') → cursor lands on doc.md.
	if err := os.WriteFile(filepath.Join(dir, "doc.md"), []byte("# Title\n\nbody **text** here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sibling.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := &eventCountRecorder{counts: map[string]int{}}
	m := renderedModelAt(t, dir, rec)

	if m.entries[m.cursor].name != "doc.md" {
		t.Fatalf("setup: cursor on %q, want doc.md", m.entries[m.cursor].name)
	}
	if !m.previewPreStyled || m.srcWidth <= 0 {
		t.Fatalf("setup: doc.md not rendered (preStyled=%v, srcWidth=%d)", m.previewPreStyled, m.srcWidth)
	}
	srcWidthBefore := m.srcWidth
	previewBefore := slices.Clone(m.preview)
	viewChangeBefore := rec.counts["view.change"]

	// The agent writes a NEW file next to doc.md: dirSig changes (new name), but
	// doc.md itself is untouched.
	if err := os.WriteFile(filepath.Join(dir, "new.go"), []byte("package main\n\nfunc x() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.syncFromDisk()

	// List pane MUST reflect the new sibling — that is the whole point of polling.
	if !hasEntry(m.entries, "new.go") {
		t.Errorf("new sibling not reflected in list: %v", names(m.entries))
	}
	// Preview MUST be left untouched — byte-identical, still pre-styled, no reset.
	if m.srcWidth != srcWidthBefore {
		t.Errorf("srcWidth reset %d → %d: preview re-rendered though only a sibling changed", srcWidthBefore, m.srcWidth)
	}
	if !m.previewPreStyled {
		t.Error("previewPreStyled dropped to false: refreshPreview reset preview state for a sibling-only change")
	}
	if !slices.Equal(m.preview, previewBefore) {
		t.Error("preview content changed though only a sibling changed (flashed back to the raw placeholder)")
	}
	if delta := rec.counts["view.change"] - viewChangeBefore; delta != 0 {
		t.Errorf("refreshPreview ran %d time(s) for a sibling-only change; want 0", delta)
	}
	// FR4: no fresh render dispatched — srcWidth still equals the body width → cache hit.
	if cmd := m.syncPreview(); cmd != nil {
		t.Error("syncPreview re-dispatched a render for an unchanged selected file")
	}
}

// TestSyncRefreshesPreviewWhenSelectedFileChanges is the positive control: when the
// SELECTED file itself changes, the preview MUST refresh. Guards the fix against
// over-suppression (passes before and after the fix).
func TestSyncRefreshesPreviewWhenSelectedFileChanges(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "doc.md"), []byte("# Title\n\nshort\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sibling.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := &eventCountRecorder{counts: map[string]int{}}
	m := renderedModelAt(t, dir, rec)
	if m.entries[m.cursor].name != "doc.md" || m.srcWidth <= 0 {
		t.Fatalf("setup: doc.md must be selected + rendered (cursor=%q srcWidth=%d)", m.entries[m.cursor].name, m.srcWidth)
	}
	viewChangeBefore := rec.counts["view.change"]

	// Rewrite the SELECTED file longer (size differs → dirSig changes AND the
	// selected entry's size differs → must re-render).
	if err := os.WriteFile(filepath.Join(dir, "doc.md"), []byte("# Title\n\nmuch longer body paragraph than before, plainly different in size.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.syncFromDisk()

	if m.srcWidth != 0 {
		t.Errorf("selected file changed but srcWidth=%d (refreshPreview did not run)", m.srcWidth)
	}
	if delta := rec.counts["view.change"] - viewChangeBefore; delta != 1 {
		t.Errorf("refreshPreview ran %d time(s) for a selected-file change; want 1", delta)
	}
	m.renderNow()
	if !m.previewPreStyled || m.srcWidth <= 0 {
		t.Error("preview did not re-render after the selected file changed")
	}
}

// TestSyncRefreshesWhenSelectedFileDeleted: deleting the selected file moves the
// cursor to a neighbour, and the preview MUST refresh for that new selection (D4).
func TestSyncRefreshesWhenSelectedFileDeleted(t *testing.T) {
	dir := t.TempDir()
	// "doc.md" sorts before "other.md" → doc.md selected; other.md survives.
	if err := os.WriteFile(filepath.Join(dir, "doc.md"), []byte("# Doc\n\nalpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "other.md"), []byte("# Other\n\nbravo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := &eventCountRecorder{counts: map[string]int{}}
	m := renderedModelAt(t, dir, rec)
	if m.entries[m.cursor].name != "doc.md" {
		t.Fatalf("setup: cursor on %q, want doc.md", m.entries[m.cursor].name)
	}
	viewChangeBefore := rec.counts["view.change"]

	if err := os.Remove(filepath.Join(dir, "doc.md")); err != nil {
		t.Fatal(err)
	}
	m.syncFromDisk()

	if m.entries[m.cursor].name != "other.md" {
		t.Errorf("after deleting the selected file, cursor on %q, want other.md", m.entries[m.cursor].name)
	}
	if delta := rec.counts["view.change"] - viewChangeBefore; delta != 1 {
		t.Errorf("refreshPreview ran %d time(s) after the selected file was deleted; want 1", delta)
	}
	m.renderNow()
	if !strings.Contains(strings.Join(m.preview, "\n"), "Other") {
		t.Errorf("preview did not refresh to other.md after deletion: %q", m.preview)
	}
}

// TestSyncSkipsPreviewWhenSelectionIsParentEntry covers D5: with the synthetic ".."
// selected, a sibling change in cwd must update the list but never refresh the
// preview — "..", with its zero-value size/modTime, always compares equal.
func TestSyncSkipsPreviewWhenSelectionIsParentEntry(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "x.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := &eventCountRecorder{counts: map[string]int{}}
	m := model{
		root: root, cwd: root,
		leftRatio: 0.38, topRatio: 0.33,
		keymap: defaultKeyMap(),
		width:  100, height: 30,
		tel: rec,
	}
	m.reload()
	m.descend() // enter sub; cursor lands on the synthetic ".." (index 0)
	if m.cwd != sub {
		t.Fatalf("setup: cwd=%q, want %q", m.cwd, sub)
	}
	if m.entries[m.cursor].name != ".." {
		t.Fatalf("setup: cursor on %q, want '..'", m.entries[m.cursor].name)
	}
	viewChangeBefore := rec.counts["view.change"]

	// A file appears inside cwd (sub) — dirSig of sub changes — but the selection
	// rests on "..", whose synthetic entry never changes (D5).
	if err := os.WriteFile(filepath.Join(sub, "y.txt"), []byte("y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.syncFromDisk()

	if !hasEntry(m.entries, "y.txt") {
		t.Errorf("new file in cwd not reflected: %v", names(m.entries))
	}
	if m.entries[m.cursor].name != ".." {
		t.Errorf("selection drifted off '..' to %q", m.entries[m.cursor].name)
	}
	if delta := rec.counts["view.change"] - viewChangeBefore; delta != 0 {
		t.Errorf("refreshPreview ran %d time(s) while '..' selected and only a sibling changed; want 0", delta)
	}
}

func hasEntry(entries []entry, name string) bool {
	for _, e := range entries {
		if e.name == name {
			return true
		}
	}
	return false
}

func names(entries []entry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.name
	}
	return out
}
