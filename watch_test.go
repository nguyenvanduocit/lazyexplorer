package main

import (
	"os"
	"path/filepath"
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
