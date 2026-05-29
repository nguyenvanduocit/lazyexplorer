package main

// Tests for the git change-state layer (docs/prd-git-change-indicator.md §5.1).
// Two tiers:
//   * pure parsers (collapseStatus / parseStatus / applyNumstat / markAncestors /
//     deltaString / countLines) — fed synthetic -z bytes, no git needed.
//   * collectGitState — driven against a real throwaway repo built with the git
//     CLI, pinning the empirically-verified behaviors the PRD relies on (-uall
//     expanding untracked dirs, the no-commit `diff HEAD` fallback).

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// collapseStatus — XY → one code (PRD D6 precedence)
// ---------------------------------------------------------------------------

func TestCollapseStatus(t *testing.T) {
	cases := []struct {
		x, y byte
		want gitCode
		ok   bool
	}{
		{'?', '?', gitUntracked, true},
		{' ', 'M', gitModified, true},
		{'M', ' ', gitModified, true},
		{'M', 'M', gitModified, true}, // staged + unstaged modify
		{'A', ' ', gitAdded, true},
		{' ', 'D', gitDeleted, true},
		{'D', ' ', gitDeleted, true},
		{'R', ' ', gitRenamed, true},
		{'R', 'M', gitRenamed, true}, // rename precedence over the trailing modify
		{'A', 'D', gitDeleted, true}, // added-then-deleted: delete wins (not conflict)
		{'A', 'A', gitConflict, true},
		{'D', 'D', gitConflict, true},
		{'U', 'U', gitConflict, true},
		{'U', 'D', gitConflict, true},
		{' ', ' ', 0, false}, // nothing changed
	}
	for _, c := range cases {
		got, ok := collapseStatus(c.x, c.y)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("collapseStatus(%q,%q) = (%v,%v), want (%v,%v)",
				c.x, c.y, got, ok, c.want, c.ok)
		}
	}
}

// ---------------------------------------------------------------------------
// parseStatus — porcelain -z, incl. the rename 2-field gotcha
// ---------------------------------------------------------------------------

func TestParseStatusUntrackedAndModified(t *testing.T) {
	// "?? new.txt\0 M mod.txt\0"
	data := []byte("?? new.txt\x00 M mod.txt\x00")
	changes := map[string]gitChange{}
	parseStatus(data, changes)

	if got := changes["new.txt"].code; got != gitUntracked {
		t.Errorf("new.txt code = %v, want untracked", got)
	}
	if got := changes["mod.txt"].code; got != gitModified {
		t.Errorf("mod.txt code = %v, want modified", got)
	}
	if len(changes) != 2 {
		t.Errorf("want 2 changes, got %d: %v", len(changes), changes)
	}
}

func TestParseStatusRenameSkipsOldPath(t *testing.T) {
	// A staged rename in -z: "R  new\0old\0" — XY="R ", then a SECOND NUL field
	// holding the OLD path. The new path must be recorded; the old must NOT leak
	// in as its own (mis-parsed) entry.
	data := []byte("R  newname.go\x00oldname.go\x00 M other.go\x00")
	changes := map[string]gitChange{}
	parseStatus(data, changes)

	if got := changes["newname.go"].code; got != gitRenamed {
		t.Errorf("newname.go code = %v, want renamed", got)
	}
	if _, ok := changes["oldname.go"]; ok {
		t.Errorf("old rename path must not become its own entry: %v", changes)
	}
	if got := changes["other.go"].code; got != gitModified {
		t.Errorf("entry after the rename must still parse; got %v", changes)
	}
}

// ---------------------------------------------------------------------------
// applyNumstat — deltas, binary, rename shape
// ---------------------------------------------------------------------------

func TestApplyNumstatNormalAndBinary(t *testing.T) {
	changes := map[string]gitChange{
		"a.go":    {code: gitModified},
		"img.png": {code: gitModified},
	}
	// "41\t3\ta.go\0-\t-\timg.png\0"  (binary shows dashes)
	data := []byte("41\t3\ta.go\x00-\t-\timg.png\x00")
	applyNumstat(data, changes)

	a := changes["a.go"]
	if !a.hasDelta || a.added != 41 || a.deleted != 3 {
		t.Errorf("a.go delta = +%d -%d (hasDelta=%v), want +41 -3", a.added, a.deleted, a.hasDelta)
	}
	if changes["img.png"].hasDelta {
		t.Errorf("binary file must keep badge but get NO delta; got %+v", changes["img.png"])
	}
}

func TestApplyNumstatRename(t *testing.T) {
	changes := map[string]gitChange{"new.go": {code: gitRenamed}}
	// rename: "<add>\t<del>\t\0<old>\0<new>\0"
	data := []byte("2\t1\t\x00old.go\x00new.go\x00")
	applyNumstat(data, changes)
	n := changes["new.go"]
	if !n.hasDelta || n.added != 2 || n.deleted != 1 {
		t.Errorf("rename delta should attach to new path: got +%d -%d (hasDelta=%v)", n.added, n.deleted, n.hasDelta)
	}
}

// ---------------------------------------------------------------------------
// deltaString — omit a zero side
// ---------------------------------------------------------------------------

func TestDeltaString(t *testing.T) {
	cases := []struct {
		g    gitChange
		want string
	}{
		{gitChange{added: 41, deleted: 3, hasDelta: true}, "+41 -3"},
		{gitChange{added: 88, deleted: 0, hasDelta: true}, "+88"},
		{gitChange{added: 0, deleted: 54, hasDelta: true}, "-54"},
		{gitChange{added: 0, deleted: 0, hasDelta: true}, ""},  // both zero
		{gitChange{added: 5, deleted: 5, hasDelta: false}, ""}, // unknown delta
	}
	for _, c := range cases {
		if got := c.g.deltaString(); got != c.want {
			t.Errorf("deltaString(%+v) = %q, want %q", c.g, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// markAncestors — folder roll-up keys
// ---------------------------------------------------------------------------

func TestMarkAncestors(t *testing.T) {
	d := map[string]bool{}
	markAncestors("a/b/c.go", d)
	if !d["a"] || !d["a/b"] {
		t.Errorf("ancestors of a/b/c.go must include a, a/b; got %v", d)
	}
	if d["a/b/c.go"] {
		t.Errorf("the path itself must NOT be marked dirty; got %v", d)
	}
	markAncestors("top.txt", d) // no slash → no ancestors
	if len(d) != 2 {
		t.Errorf("top-level file adds no ancestor dirs; got %v", d)
	}
}

// ---------------------------------------------------------------------------
// countLines — files, dirs, binary
// ---------------------------------------------------------------------------

func TestCountLines(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	if n, ok := countLines(write("three.txt", "a\nb\nc\n")); !ok || n != 3 {
		t.Errorf("three.txt = (%d,%v), want (3,true)", n, ok)
	}
	if n, ok := countLines(write("notrail.txt", "a\nb")); !ok || n != 2 {
		t.Errorf("notrail.txt = (%d,%v), want (2,true) — final unterminated line counts", n, ok)
	}
	if n, ok := countLines(write("empty.txt", "")); !ok || n != 0 {
		t.Errorf("empty.txt = (%d,%v), want (0,true)", n, ok)
	}
	if _, ok := countLines(write("bin.dat", "ab\x00cd")); ok {
		t.Errorf("binary file must return ok=false")
	}
	if _, ok := countLines(dir); ok {
		t.Errorf("a directory must return ok=false")
	}
}

// ---------------------------------------------------------------------------
// countUntracked — line-count cache (skip re-reading unchanged files)
// ---------------------------------------------------------------------------

func TestCountUntrackedCacheHitSkipsReread(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(p, []byte("a\nb\nc\n"), 0o644); err != nil { // 3 lines on disk
		t.Fatal(err)
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	mt, sz := info.ModTime().UnixNano(), info.Size()

	// prev claims a DIFFERENT (wrong) line count for the same mtime+size. A cache
	// hit must reuse it verbatim — proof the file was not re-read.
	prev := untrackedCache{"notes.txt": {mtime: mt, size: sz, lines: 999, ok: true}}
	changes := map[string]gitChange{"notes.txt": {code: gitUntracked}}

	next := countUntracked(dir, changes, prev, maxUntrackedScan)

	if got := changes["notes.txt"]; !got.hasDelta || got.added != 999 {
		t.Errorf("cache hit must reuse the cached count (999) without re-reading; got +%d (hasDelta=%v)", got.added, got.hasDelta)
	}
	if next["notes.txt"].lines != 999 {
		t.Errorf("the refreshed cache should carry the reused count forward; got %d", next["notes.txt"].lines)
	}
}

func TestCountUntrackedCacheMissRereads(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(p, []byte("a\nb\nc\n"), 0o644); err != nil { // 3 lines
		t.Fatal(err)
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	// A STALE mtime (file looks changed since it was cached) must trigger a re-read,
	// yielding the real count (3) — never the stale cached value.
	prev := untrackedCache{"notes.txt": {mtime: info.ModTime().UnixNano() - 1, size: info.Size(), lines: 999, ok: true}}
	changes := map[string]gitChange{"notes.txt": {code: gitUntracked}}

	next := countUntracked(dir, changes, prev, maxUntrackedScan)

	if got := changes["notes.txt"]; !got.hasDelta || got.added != 3 {
		t.Errorf("a stale cache entry must trigger a re-read (real count 3); got +%d", got.added)
	}
	if next["notes.txt"].lines != 3 {
		t.Errorf("the refreshed cache should store the freshly counted value 3; got %d", next["notes.txt"].lines)
	}
}

// ---------------------------------------------------------------------------
// collectGitState — against a real repo
// ---------------------------------------------------------------------------

// gitExec runs a git command in dir for test setup, failing the test on error.
func gitExec(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	// Deterministic identity so `commit` works in CI without a global config.
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func TestDetectRepoRootNonRepo(t *testing.T) {
	if root := detectRepoRoot(t.TempDir()); root != "" {
		t.Errorf("a non-repo dir must yield empty repoRoot; got %q", root)
	}
}

func TestCollectGitStateModifiedAndUntrackedDir(t *testing.T) {
	dir := t.TempDir()
	gitExec(t, dir, "init")
	mustWrite(t, dir, "tracked.txt", "one\ntwo\nthree\n")
	gitExec(t, dir, "add", "tracked.txt")
	gitExec(t, dir, "commit", "-m", "init")

	// Modify the tracked file (+1 line) and create an untracked folder of files.
	mustWrite(t, dir, "tracked.txt", "one\ntwo\nthree\nfour\n")
	if err := os.MkdirAll(filepath.Join(dir, "sub", "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "sub"), "x.txt", "a\nb\n")
	mustWrite(t, filepath.Join(dir, "sub", "deep"), "y.txt", "z\n")

	root := detectRepoRoot(dir)
	if root == "" {
		t.Fatal("repoRoot should be detected for an initialized repo")
	}
	st, _ := collectGitState(root, nil)

	// tracked.txt: modified, +1 -0.
	tr := st.changes["tracked.txt"]
	if tr.code != gitModified || !tr.hasDelta || tr.added != 1 {
		t.Errorf("tracked.txt = %+v, want modified +1", tr)
	}
	// -uall must expand the untracked dir into individual files (BLOCKER fix).
	if st.changes["sub/x.txt"].code != gitUntracked {
		t.Errorf("sub/x.txt should be untracked; changes=%v", st.changes)
	}
	if st.changes["sub/deep/y.txt"].code != gitUntracked {
		t.Errorf("sub/deep/y.txt should be untracked; changes=%v", st.changes)
	}
	// untracked file line count surfaces as "+N".
	if x := st.changes["sub/x.txt"]; !x.hasDelta || x.added != 2 {
		t.Errorf("sub/x.txt delta = +%d (hasDelta=%v), want +2", x.added, x.hasDelta)
	}
	// folder roll-up: sub and sub/deep are dirty (contain changes).
	if !st.dirtyDirs["sub"] || !st.dirtyDirs["sub/deep"] {
		t.Errorf("sub and sub/deep should be dirty; got %v", st.dirtyDirs)
	}
}

func TestCollectGitStateNoCommitRepo(t *testing.T) {
	// A repo with NO commits: `git diff HEAD` aborts (exit 128). The status pass
	// must still surface untracked files, and collect must not blank the state.
	dir := t.TempDir()
	gitExec(t, dir, "init")
	mustWrite(t, dir, "fresh.go", "package main\n")

	root := detectRepoRoot(dir)
	st, _ := collectGitState(root, nil)
	if st.changes["fresh.go"].code != gitUntracked {
		t.Errorf("fresh file in a no-commit repo must show untracked; changes=%v", st.changes)
	}
}
