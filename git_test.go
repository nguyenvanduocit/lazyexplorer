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

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
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

// ---------------------------------------------------------------------------
// parseIgnored / isIgnored — the gitignore set (prd-ignored-entry-dimming §5.1)
// ---------------------------------------------------------------------------

// TestParseIgnored pins D1: `git status --porcelain -z --ignored` (no -uall) emits a
// "!! path" record per ignored entry — a directory COLLAPSES to "!! dir/" (trailing
// slash), a file is bare. parseIgnored strips the slash and stores the repo-relative
// slash key, skipping the regular change records (" M …") the same stream also carries.
func TestParseIgnored(t *testing.T) {
	data := []byte("!! .claude/\x00!! tmp/\x00 M foo.go\x00!! lazyexplorer\x00!! docs/.omc/\x00")
	ignored := map[string]bool{}
	parseIgnored(data, ignored)

	for _, want := range []string{".claude", "tmp", "lazyexplorer", "docs/.omc"} {
		if !ignored[want] {
			t.Errorf("parseIgnored should record %q (slash stripped); got %v", want, ignored)
		}
	}
	if ignored["foo.go"] {
		t.Errorf("a non-!! change record must not be recorded as ignored; got %v", ignored)
	}
	if ignored[".claude/"] || ignored["tmp/"] {
		t.Errorf("the trailing slash on a dir record must be stripped; got %v", ignored)
	}
}

// TestGitStateIsIgnored pins D7 ancestor-match: a path is ignored when it OR any
// ancestor directory is in the set (the collapsed "!! tmp/" means "tmp and everything
// under it"). A prefix that is not a path boundary ("tmpfoo" vs "tmp") must NOT match,
// and a nil set (git primed, no refresh yet) is safe.
func TestGitStateIsIgnored(t *testing.T) {
	st := gitState{ignored: map[string]bool{"tmp": true, "docs/.omc": true, "lazyexplorer": true}}
	cases := []struct {
		rel  string
		want bool
	}{
		{"tmp", true},                  // self
		{"tmp/a", true},                // ancestor
		{"tmp/a/b/c.go", true},         // deep ancestor
		{"docs/.omc", true},            // nested self
		{"docs/.omc/state.json", true}, // nested ancestor
		{"lazyexplorer", true},         // ignored file at root
		{"src", false},
		{"src/main.go", false},
		{"docs", false},   // parent of an ignored dir is NOT itself ignored
		{"tmpfoo", false}, // prefix that is not a path boundary must not match
	}
	for _, c := range cases {
		if got := st.isIgnored(c.rel); got != c.want {
			t.Errorf("isIgnored(%q) = %v, want %v", c.rel, got, c.want)
		}
	}
	if (gitState{}).isIgnored("anything") {
		t.Errorf("a nil ignored set must report nothing ignored")
	}
}

// TestCollectGitStateIgnoredCollapsesDirAndSkipsTracked drives the ignored collection
// against a real repo, pinning the two load-bearing behaviors: an ignored DIR is
// recorded as one collapsed key (not expanded to its files — D1), and a TRACKED file
// that matches an ignore pattern is NOT reported ignored (git does not ignore tracked
// things — D2, the reason we use `git status` over a pure pattern-matcher).
func TestCollectGitStateIgnoredCollapsesDirAndSkipsTracked(t *testing.T) {
	dir := t.TempDir()
	gitExec(t, dir, "init")
	mustWrite(t, dir, ".gitignore", "ignored/\n*.log\n")
	if err := os.MkdirAll(filepath.Join(dir, "ignored"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "ignored"), "junk.txt", "x\n")
	mustWrite(t, dir, "skip.log", "noise\n")       // untracked + matches *.log → ignored
	mustWrite(t, dir, "keep.log", "tracked\n")     // matches *.log BUT tracked → NOT ignored
	gitExec(t, dir, "add", "-f", "keep.log")       // force-add past the ignore pattern
	mustWrite(t, dir, "main.go", "package main\n") // normal untracked

	root := detectRepoRoot(dir)
	st, _ := collectGitState(root, nil)

	if !st.ignored["ignored"] {
		t.Errorf("ignored dir should be the collapsed key %q; got %v", "ignored", st.ignored)
	}
	if st.ignored["ignored/junk.txt"] {
		t.Errorf("ignored dir must NOT expand to its files (no -uall); got %v", st.ignored)
	}
	if !st.ignored["skip.log"] {
		t.Errorf("untracked ignored file skip.log should be recorded; got %v", st.ignored)
	}
	if st.ignored["keep.log"] {
		t.Errorf("a TRACKED file must never be reported ignored (D2); got %v", st.ignored)
	}
	if st.ignored["main.go"] {
		t.Errorf("a normal file must not be ignored; got %v", st.ignored)
	}
}

// ---------------------------------------------------------------------------
// diffHunks — fetch + colorize the unified diff (prd-preview-diff-view T1/§5.1)
// ---------------------------------------------------------------------------

// styledWith reports whether the line carries the ANSI foreground that style
// would render — i.e. diffHunks colored this line through the same diffLineStyle
// the assertion expects. We probe by rendering a marker through the style and
// checking the line opens with the same SGR prefix (escape up to the first 'm').
func sgrPrefix(s string) string {
	if i := strings.IndexByte(s, 'm'); i >= 0 && strings.HasPrefix(s, "\x1b[") {
		return s[:i+1]
	}
	return ""
}

// TestDiffHunksColorizesByPrefix is the headline T1 contract: against a committed
// baseline, diffHunks returns the unified diff with each line colored by its
// leading char — '+' lines in colDiffAdd, '-' lines in colDiffDel, the '@@' hunk
// header + 'diff --git'/'index'/'---'/'+++' headers + ' ' context lines in dimStyle
// (D11). The leading +/-/space character is preserved (readable when color is lost).
func TestDiffHunksColorizesByPrefix(t *testing.T) {
	dir := t.TempDir()
	gitExec(t, dir, "init")
	mustWrite(t, dir, "f.txt", "alpha\nbeta\ngamma\n")
	gitExec(t, dir, "add", "f.txt")
	gitExec(t, dir, "commit", "-m", "init")
	// One line removed (beta), one added (delta) — diff shows -beta +delta plus
	// context lines (alpha, gamma) and the @@ hunk header.
	mustWrite(t, dir, "f.txt", "alpha\ndelta\ngamma\n")

	root := detectRepoRoot(dir)
	lines, preStyled, err := diffHunks(root, "f.txt")
	if err != nil {
		t.Fatalf("diffHunks on a modified tracked file should succeed; err=%v", err)
	}
	if !preStyled {
		t.Errorf("diff lines carry verbatim ANSI → preStyled must be true")
	}
	if len(lines) == 0 {
		t.Fatal("diffHunks returned no lines for a real diff")
	}

	// Classify each line by its ansi-stripped leading char and assert color.
	addPrefix := sgrPrefix(diffLineStyle('+').Render("x"))
	delPrefix := sgrPrefix(diffLineStyle('-').Render("x"))
	dimPrefix := sgrPrefix(dimStyle.Render("x"))

	var sawAdd, sawDel, sawHunk, sawContext bool
	for _, ln := range lines {
		plain := ansi.Strip(ln)
		if plain == "" {
			continue
		}
		got := sgrPrefix(ln)
		switch {
		case strings.HasPrefix(plain, "@@"):
			sawHunk = true
			if got != dimPrefix {
				t.Errorf("hunk header %q not dim-styled: sgr=%q want %q", plain, got, dimPrefix)
			}
		case strings.HasPrefix(plain, "+++") || strings.HasPrefix(plain, "---") ||
			strings.HasPrefix(plain, "diff --git") || strings.HasPrefix(plain, "index "):
			// File headers are dim too (default branch of diffLineStyle).
			if got != dimPrefix {
				t.Errorf("file header %q not dim-styled: sgr=%q want %q", plain, got, dimPrefix)
			}
		case plain[0] == '+':
			sawAdd = true
			if got != addPrefix {
				t.Errorf("added line %q not add-styled: sgr=%q want %q", plain, got, addPrefix)
			}
			if !strings.Contains(plain, "delta") {
				continue
			}
		case plain[0] == '-':
			sawDel = true
			if got != delPrefix {
				t.Errorf("removed line %q not del-styled: sgr=%q want %q", plain, got, delPrefix)
			}
		case plain[0] == ' ':
			sawContext = true
			if got != dimPrefix {
				t.Errorf("context line %q not dim-styled: sgr=%q want %q", plain, got, dimPrefix)
			}
		}
	}
	if !sawAdd || !sawDel || !sawHunk || !sawContext {
		t.Errorf("diff must show an add, a del, a hunk header, and a context line; got add=%v del=%v hunk=%v context=%v",
			sawAdd, sawDel, sawHunk, sawContext)
	}
}

// TestDiffHunksSyntaxHighlightsWithFileContext pins the highlight-in-diff contract
// (syntax-highlight-in-diff pain): when the changed file is source code, each diff
// body line carries chroma SYNTAX highlighting on its code, with the +/-/space diff
// signal moved onto a coloured gutter glyph — not the flat whole-line foreground the
// non-code path uses. The fixture is the DISCRIMINATING case the advisor named: an
// added line that lives INSIDE a multi-line block comment. A per-line tokenizer would
// see "docline two" with no open '/*' and colour it as plain code; only tokenizing the
// reconstructed WHOLE new file (which -U<big> gives us) sees it as a comment. We assert
// the added line equals the green gutter glyph followed by EXACTLY the line chroma
// produces for that index of the whole new file — so the diff highlights identically to
// the full-content (`v`) view. The leading "+docline two" plain text is preserved
// (Y/V copy reads ansi.Strip of these lines), so copy stays byte-correct.
func TestDiffHunksSyntaxHighlightsWithFileContext(t *testing.T) {
	dir := t.TempDir()
	gitExec(t, dir, "init")
	const oldContent = "package p\n\n/*\ndocline one\n*/\nfunc f() {}\n"
	const newContent = "package p\n\n/*\ndocline one\ndocline two\n*/\nfunc f() {}\n"
	mustWrite(t, dir, "f.go", oldContent)
	gitExec(t, dir, "add", "f.go")
	gitExec(t, dir, "commit", "-m", "init")
	mustWrite(t, dir, "f.go", newContent)

	root := detectRepoRoot(dir)
	lines, preStyled, err := diffHunks(root, "f.go")
	if err != nil {
		t.Fatalf("diffHunks on a modified .go file should succeed; err=%v", err)
	}
	if !preStyled {
		t.Errorf("highlighted diff lines carry verbatim ANSI → preStyled must be true")
	}

	// The context-aware truth: highlight the whole NEW file WITH the added-line green wash;
	// "docline two" is at index 4. An added line is exactly this — no gutter glyph, the
	// wash IS the signal so code starts at column 0.
	const addedIdx = 4
	wantHL, herr := highlightCodeBg(newContent, "f.go", colDiffAddBg)
	if herr != nil {
		t.Fatalf("highlightCodeBg(newContent) err=%v", herr)
	}
	if addedIdx >= len(wantHL) {
		t.Fatalf("fixture drift: new file has %d highlighted lines, want index %d", len(wantHL), addedIdx)
	}
	wantAdded := wantHL[addedIdx]

	// Sanity: the tinted comment-aware highlight must differ from the UNTINTED highlight —
	// proving both the green wash AND the whole-file (comment) context are applied (a
	// per-line tokenizer would colour "docline two" as plain code, and no-wash would drop
	// the background).
	plainHL, _ := highlightCode(newContent, "f.go")
	if wantAdded == plainHL[addedIdx] {
		t.Fatal("fixture not discriminating: tinted added line equals the untinted highlight")
	}

	// No gutter → ansi.Strip is just the bare code ("docline two"); the Y/V copy buffer
	// reads from this, so a copied diff is plain source lines.
	var gotAdded string
	for _, ln := range lines {
		if ansi.Strip(ln) == "docline two" {
			gotAdded = ln
			break
		}
	}
	if gotAdded == "" {
		t.Fatal("diff did not include the added comment line 'docline two'")
	}
	if gotAdded != wantAdded {
		t.Errorf("added line not highlighted+washed with whole-file context:\n got %q\nwant %q", gotAdded, wantAdded)
	}
}

// TestDiffShowsFullFileContext pins the core promise of the diff preview: the
// WHOLE file is shown — every unchanged line as dim context — with the change
// coloured in place, NOT just the few lines around each hunk. The fixture is
// discriminating: a 15-line file with a single edit at line 8. Git's default
// 3-line context would truncate the diff to lines ~5–11, dropping l1/l2/l15;
// full-file context keeps them. We assert the far-away unchanged lines survive
// as context (` l1`, ` l15`) alongside the `-l8`/`+CHANGED` edit, so a reviewer
// reads the change without losing the rest of the file.
func TestDiffShowsFullFileContext(t *testing.T) {
	dir := t.TempDir()
	gitExec(t, dir, "init")
	mustWrite(t, dir, "f.txt", "l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nl10\nl11\nl12\nl13\nl14\nl15\n")
	gitExec(t, dir, "add", "f.txt")
	gitExec(t, dir, "commit", "-m", "init")
	// Edit a single line deep in the middle; default-context hunks would omit the
	// first and last lines, full-file context keeps them.
	mustWrite(t, dir, "f.txt", "l1\nl2\nl3\nl4\nl5\nl6\nl7\nCHANGED\nl9\nl10\nl11\nl12\nl13\nl14\nl15\n")

	root := detectRepoRoot(dir)
	lines, _, err := diffHunks(root, "f.txt")
	if err != nil {
		t.Fatalf("diffHunks err=%v", err)
	}

	// The edit itself.
	if findDiffBodyLine(lines, "-l8") == "" || findDiffBodyLine(lines, "+CHANGED") == "" {
		t.Fatalf("diff must show the edit (-l8 +CHANGED); got:\n%s", ansi.Strip(strings.Join(lines, "\n")))
	}
	// The far-away unchanged lines must be present as context — proof the whole
	// file is shown, not a truncated hunk window.
	for _, want := range []string{" l1", " l2", " l15"} {
		if findDiffBodyLine(lines, want) == "" {
			t.Errorf("full-file diff must keep distant context line %q; got:\n%s",
				want, ansi.Strip(strings.Join(lines, "\n")))
		}
	}
}

// TestDiffHunksExpandsTabs pins that the diff preview expands tabs to spaces like
// every other preview path (normalizeText). A tab-indented source file's diff must
// NOT carry a raw '\t': an unexpanded tab measures as ~0 columns in lipgloss/ansi
// width while the terminal jumps a whole tab stop, so the indentation would desync
// from the plain code preview (and the horizontal-scroll clamp would mis-size the
// line). Diff lines are also what `Y`/`V` copy out, so the buffer itself must be
// tab-free, not just the rendered frame.
func TestDiffHunksExpandsTabs(t *testing.T) {
	dir := t.TempDir()
	gitExec(t, dir, "init")
	mustWrite(t, dir, "f.go", "package p\nfunc f() {\n\tx := 1\n}\n")
	gitExec(t, dir, "add", "f.go")
	gitExec(t, dir, "commit", "-m", "init")
	// Edit the tab-indented body line so the diff carries a '+' and a '-' line that
	// each begin (after the diff gutter char) with a single source tab.
	mustWrite(t, dir, "f.go", "package p\nfunc f() {\n\tx := 2\n}\n")

	root := detectRepoRoot(dir)
	lines, _, err := diffHunks(root, "f.go")
	if err != nil {
		t.Fatalf("diffHunks err=%v", err)
	}
	sawAdded := false
	for _, ln := range lines {
		plain := ansi.Strip(ln)
		if strings.ContainsRune(plain, '\t') {
			t.Errorf("diff line still carries a raw tab: %q", plain)
		}
		if strings.Contains(plain, "x := 2") {
			sawAdded = true
			// f.go is code → no gutter glyph (the green wash is the signal), so the line is
			// just the tab expanded to previewTabWidth spaces + the content, starting at
			// column 0 — exactly normalizeText's mapping (the wash never reintroduces a tab).
			want := strings.Repeat(" ", previewTabWidth) + "x := 2"
			if plain != want {
				t.Errorf("added line not tab-expanded:\n got %q\nwant %q", plain, want)
			}
		}
	}
	if !sawAdded {
		t.Fatal("diff did not include the changed tab-indented line")
	}
}

// TestDiffHunksReconcilesBadgeCount pins FR2/D7: the +/- line count in the diff
// must equal the +N/-N the BADGE counts, because both compute against the same
// HEAD-aware base. The fixture is DISCRIMINATING: one tracked file carries both a
// STAGED hunk and a further UNSTAGED hunk, so `git diff HEAD` (HEAD-aware, what
// numstat counts) and `git diff` (worktree-vs-index, the drift this guards
// against) return DIFFERENT counts. The test then cross-checks the diff's own
// +/- count against the model's badge numbers (gitChange.added/deleted from the
// SAME collectGitState), so any drift off the HEAD base fails equality.
func TestDiffHunksReconcilesBadgeCount(t *testing.T) {
	dir := t.TempDir()
	gitExec(t, dir, "init")
	mustWrite(t, dir, "f.txt", "one\ntwo\nthree\n")
	gitExec(t, dir, "add", "f.txt")
	gitExec(t, dir, "commit", "-m", "init")

	// Staged hunk: edit line 2, then `git add` it into the index.
	mustWrite(t, dir, "f.txt", "one\nTWO\nthree\n")
	gitExec(t, dir, "add", "f.txt")
	// Further UNSTAGED hunk on the same file: append a line, leave it in the worktree.
	mustWrite(t, dir, "f.txt", "one\nTWO\nthree\nfour\n")
	// Now: `git diff HEAD`  → -two +TWO +four  (+2 -1) ← numstat / badge base
	//      `git diff` (idx) → +four            (+1 -0) ← the off-HEAD drift to catch.

	root := detectRepoRoot(dir)

	// The badge's own numbers come straight from collectGitState's HEAD-aware numstat.
	state, _ := collectGitState(root, nil)
	chg, ok := state.changes["f.txt"]
	if !ok || !chg.hasDelta {
		t.Fatalf("badge state for f.txt missing/no-delta: %+v (ok=%v)", chg, ok)
	}

	lines, _, err := diffHunks(root, "f.txt")
	if err != nil {
		t.Fatalf("diffHunks err=%v", err)
	}
	adds, dels := 0, 0
	for _, ln := range lines {
		plain := ansi.Strip(ln)
		if plain == "" {
			continue
		}
		// Exclude the +++/--- file headers from the +/- content count.
		if strings.HasPrefix(plain, "+++") || strings.HasPrefix(plain, "---") {
			continue
		}
		switch plain[0] {
		case '+':
			adds++
		case '-':
			dels++
		}
	}
	// numstat's added/deleted is line-for-line the diff's +/- count on the same
	// base — equality pins both to HEAD. (Off-HEAD worktree-vs-index would give
	// +1/-0, failing against the badge's +2/-1.)
	if adds != chg.added || dels != chg.deleted {
		t.Errorf("diff +/- count must reconcile with the badge (same HEAD base): diff +%d/-%d, badge +%d/-%d",
			adds, dels, chg.added, chg.deleted)
	}
}

// TestDiffHunksNoCommitUsesCached pins D7's fallback: a repo with NO commits has
// no HEAD, so `git diff HEAD` aborts — diffHunks must fall back to `git diff
// --cached` and still return the staged change as a diff.
func TestDiffHunksNoCommitUsesCached(t *testing.T) {
	dir := t.TempDir()
	gitExec(t, dir, "init")
	mustWrite(t, dir, "staged.txt", "first\nsecond\n")
	gitExec(t, dir, "add", "staged.txt") // staged in a no-commit repo

	root := detectRepoRoot(dir)
	lines, _, err := diffHunks(root, "staged.txt")
	if err != nil {
		t.Fatalf("diffHunks must fall back to --cached in a no-commit repo; err=%v", err)
	}
	joined := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "+first") || !strings.Contains(joined, "+second") {
		t.Errorf("staged-change diff (--cached) must show the added lines; got:\n%s", joined)
	}
}

// TestDiffHunksEmptyDiffIsError pins D10/FR6: a tracked file with NO textual diff
// (e.g. nothing changed) yields an empty `git diff` → diffHunks returns a non-nil
// error so the caller degrades to full content rather than render an empty pane.
func TestDiffHunksEmptyDiffIsError(t *testing.T) {
	dir := t.TempDir()
	gitExec(t, dir, "init")
	mustWrite(t, dir, "f.txt", "unchanged\n")
	gitExec(t, dir, "add", "f.txt")
	gitExec(t, dir, "commit", "-m", "init")
	// No modification: `git diff HEAD -- f.txt` is empty.

	root := detectRepoRoot(dir)
	_, _, err := diffHunks(root, "f.txt")
	if err == nil {
		t.Errorf("an empty diff must return a non-nil error so the caller falls back to content")
	}
}

// TestDiffHunksResilientOnNonRepo pins FR10: pointing diffHunks at a directory
// that is not a git repo (git diff fails) returns an error, never panics — the
// caller degrades to full content.
func TestDiffHunksResilientOnNonRepo(t *testing.T) {
	dir := t.TempDir() // no `git init`
	_, _, err := diffHunks(dir, "whatever.txt")
	if err == nil {
		t.Errorf("diffHunks on a non-repo dir must return an error (degrade signal)")
	}
}

// lineStyledLike reports whether `line`'s leading SGR matches what `style` renders
// — i.e. the line was colored through that style. Compares the escape up to the
// first 'm' against a marker rendered through the style.
func lineStyledLike(line string, style lipgloss.Style) bool {
	return sgrPrefix(line) == sgrPrefix(style.Render("x"))
}

// findDiffBodyLine returns the colorized diff body line whose ansi-stripped text
// equals `want` (a full diff line incl. its +/-/space prefix), or "" if absent.
func findDiffBodyLine(lines []string, want string) string {
	for _, ln := range lines {
		if ansi.Strip(ln) == want {
			return ln
		}
	}
	return ""
}

// TestDiffHunksContentLinesNotSpoofedByHeaderPrefix pins D11/FR1's color contract
// against content that COLLIDES with the +++/--- file-header prefix. A removed
// source line `-- keep comment` (SQL/Lua comment) renders in unified diff as
// `--- keep comment`; an added `++counter` (C/C++ increment) renders as
// `+++counter`. A prefix-based header check would misread both as headers and dim
// them — so a removed `--`-comment must stay RED and an added `++`-line GREEN.
// These tokens are common in exactly the code an agent edits.
func TestDiffHunksContentLinesNotSpoofedByHeaderPrefix(t *testing.T) {
	dir := t.TempDir()
	gitExec(t, dir, "init")
	// Baseline holds the `-- keep comment` line; the new revision removes it and
	// adds a `++counter` line. The diff body then carries `--- keep comment` (a
	// removal) and `+++counter` (an addition) — both spoof the header prefix.
	mustWrite(t, dir, "f.txt", "alpha\n-- keep comment\nbeta\n")
	gitExec(t, dir, "add", "f.txt")
	gitExec(t, dir, "commit", "-m", "init")
	mustWrite(t, dir, "f.txt", "alpha\n++counter\nbeta\n")

	root := detectRepoRoot(dir)
	lines, _, err := diffHunks(root, "f.txt")
	if err != nil {
		t.Fatalf("diffHunks err=%v", err)
	}

	removed := findDiffBodyLine(lines, "--- keep comment")
	if removed == "" {
		t.Fatalf("diff body must contain the removed line %q; got:\n%s",
			"--- keep comment", ansi.Strip(strings.Join(lines, "\n")))
	}
	if !lineStyledLike(removed, diffLineStyle('-')) {
		t.Errorf("removed `-- comment` (renders %q) must be RED (colDiffDel), got sgr=%q want %q",
			"--- keep comment", sgrPrefix(removed), sgrPrefix(diffLineStyle('-').Render("x")))
	}

	added := findDiffBodyLine(lines, "+++counter")
	if added == "" {
		t.Fatalf("diff body must contain the added line %q; got:\n%s",
			"+++counter", ansi.Strip(strings.Join(lines, "\n")))
	}
	if !lineStyledLike(added, diffLineStyle('+')) {
		t.Errorf("added `++counter` (renders %q) must be GREEN (colDiffAdd), got sgr=%q want %q",
			"+++counter", sgrPrefix(added), sgrPrefix(diffLineStyle('+').Render("x")))
	}
}

// TestDiffHunksColorSurvivesColorUiAlways pins D11/FR1 against a repo configured
// with color.ui=always: git would otherwise inject its OWN ANSI into the piped
// diff, so every content line would start with an escape (line[0]==0x1b) and read
// as neither '+' nor '-' → the whole diff dims, defeating the add-green/remove-red
// signal. diffHunks must force --no-color so its own colorization keys on the real
// +/- prefix.
func TestDiffHunksColorSurvivesColorUiAlways(t *testing.T) {
	dir := t.TempDir()
	gitExec(t, dir, "init")
	gitExec(t, dir, "config", "color.ui", "always")
	mustWrite(t, dir, "f.txt", "alpha\nbeta\ngamma\n")
	gitExec(t, dir, "add", "f.txt")
	gitExec(t, dir, "commit", "-m", "init")
	mustWrite(t, dir, "f.txt", "alpha\nDELTA\ngamma\n") // -beta +DELTA

	root := detectRepoRoot(dir)
	lines, _, err := diffHunks(root, "f.txt")
	if err != nil {
		t.Fatalf("diffHunks err=%v", err)
	}

	added := findDiffBodyLine(lines, "+DELTA")
	if added == "" {
		t.Fatalf("diff body must contain `+DELTA` (git's color escapes leaked into the line text); got:\n%s",
			ansi.Strip(strings.Join(lines, "\n")))
	}
	if !lineStyledLike(added, diffLineStyle('+')) {
		t.Errorf("added line under color.ui=always must be GREEN (colDiffAdd), got sgr=%q want %q",
			sgrPrefix(added), sgrPrefix(diffLineStyle('+').Render("x")))
	}
	removed := findDiffBodyLine(lines, "-beta")
	if removed == "" {
		t.Fatalf("diff body must contain `-beta`; got:\n%s", ansi.Strip(strings.Join(lines, "\n")))
	}
	if !lineStyledLike(removed, diffLineStyle('-')) {
		t.Errorf("removed line under color.ui=always must be RED (colDiffDel), got sgr=%q want %q",
			sgrPrefix(removed), sgrPrefix(diffLineStyle('-').Render("x")))
	}
}
