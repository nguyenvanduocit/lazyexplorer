package main

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	gitignore "github.com/sabhiram/go-gitignore"
	"github.com/sahilm/fuzzy"
)

// walkNames is a test helper: the relPath names of a walked tree.
func walkNames(entries []entry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.name
	}
	return out
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// TestFuzzyContract pins the sahilm/fuzzy API the search filter relies on
// (PRD §5.1, D9): fuzzy.Find(pattern, data) returns Matches sorted by Score
// desc, each carrying an Index that maps back into data. If a future version
// changes the signature or the Index semantics, filterSearch breaks here first.
func TestFuzzyContract(t *testing.T) {
	data := []string{"food", "far", "abcfoo"}
	matches := fuzzy.Find("foo", data)
	if len(matches) == 0 {
		t.Fatal("fuzzy.Find(\"foo\", …) returned no matches — expected food + abcfoo")
	}
	// Every match's Index must address a real element of data.
	for _, m := range matches {
		if m.Index < 0 || m.Index >= len(data) {
			t.Fatalf("match Index %d out of range for data len %d", m.Index, len(data))
		}
	}
	// "far" has no "foo" subsequence — it must not match.
	for _, m := range matches {
		if data[m.Index] == "far" {
			t.Errorf("%q wrongly matched %q", "far", "foo")
		}
	}
	// Find is case-insensitive by default (D5): "MODEL" should hit "model.go".
	if got := fuzzy.Find("MODEL", []string{"model.go", "view.go"}); len(got) == 0 {
		t.Error("fuzzy.Find is expected to be case-insensitive but \"MODEL\" missed \"model.go\"")
	}
}

// TestGitIgnoreContract pins the sabhiram/go-gitignore API walkTree relies on
// (PRD §5.1, D10): CompileIgnoreFile(path) parses a .gitignore, MatchesPath
// reports whether a relative path is ignored (true == ignored).
func TestGitIgnoreContract(t *testing.T) {
	dir := t.TempDir()
	gi := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(gi, []byte("node_modules/\ndist/\n*.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ignore, err := gitignore.CompileIgnoreFile(gi)
	if err != nil {
		t.Fatalf("CompileIgnoreFile: %v", err)
	}
	if ignore == nil {
		t.Fatal("CompileIgnoreFile returned nil GitIgnore for a valid file")
	}
	cases := []struct {
		path string
		want bool
	}{
		{"node_modules/foo", true},
		// A trailing-slash pattern ("node_modules/") matches the dir's CONTENTS
		// and the dir WITH a trailing slash, but NOT the bare dir name — git's
		// own semantics. walkTree compensates by also testing rel+"/" for dirs
		// (see TestWalkTree / the dir branch in walkTree).
		{"node_modules/", true},
		{"node_modules", false},
		{"dist/bundle.js", true},
		{"app.log", true},
		{"main.go", false},
		{"docs/prd-search.md", false},
	}
	for _, c := range cases {
		if got := ignore.MatchesPath(c.path); got != c.want {
			t.Errorf("MatchesPath(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

// TestWalkTree exercises the recursive walker's whole contract (PRD §5.3, FR3,
// FR13, FR14): names are relPath, root is not emitted, the result is alpha
// sorted, .git/ is hardcoded-skipped, the root .gitignore is applied (incl. the
// trailing-slash dir-pattern quirk), symlinks are neither followed nor included,
// and a permission-denied subdir is skipped without bubbling an error.
func TestWalkTree(t *testing.T) {
	root := t.TempDir()
	// .gitignore: a trailing-slash dir pattern, a glob, a bare name.
	mustWrite(t, root, ".gitignore", "node_modules/\n*.log\nignoredfile\n")

	// Real tree.
	mustWrite(t, root, "main.go", "package main\n")
	mustWrite(t, root, "app.log", "noise\n") // ignored by *.log
	mustWrite(t, root, "ignoredfile", "x\n") // ignored by bare name
	mustMkdir(t, root, "docs")
	mustWrite(t, filepath.Join(root, "docs"), "prd.md", "# prd\n")
	mustMkdir(t, root, "node_modules") // dir, ignored by node_modules/
	mustWrite(t, filepath.Join(root, "node_modules"), "lib.js", "x\n")
	mustMkdir(t, root, ".git") // always skipped, even though not in .gitignore
	mustWrite(t, filepath.Join(root, ".git"), "HEAD", "ref: x\n")

	entries, err := walkTree(root)
	if err != nil {
		t.Fatalf("walkTree: %v", err)
	}
	got := walkNames(entries)

	// Root itself is never emitted.
	if contains(got, ".") || contains(got, "") {
		t.Errorf("walkTree emitted the root itself: %v", got)
	}
	// Present.
	for _, want := range []string{"main.go", "docs", filepath.Join("docs", "prd.md")} {
		if !contains(got, want) {
			t.Errorf("walkTree missing %q; got %v", want, got)
		}
	}
	// .git hardcoded skip (the dir AND anything under it).
	for _, bad := range []string{".git", filepath.Join(".git", "HEAD")} {
		if contains(got, bad) {
			t.Errorf("walkTree did not skip .git: %q present in %v", bad, got)
		}
	}
	// .gitignore applied — the dir (trailing-slash pattern), its contents,
	// the glob, and the bare-name file.
	for _, bad := range []string{"node_modules", filepath.Join("node_modules", "lib.js"), "app.log", "ignoredfile"} {
		if contains(got, bad) {
			t.Errorf("walkTree did not honor .gitignore: %q present in %v", bad, got)
		}
	}
	// Sorted alpha by relPath.
	if !sort.SliceIsSorted(entries, func(i, j int) bool { return entries[i].name < entries[j].name }) {
		t.Errorf("walkTree result not alpha-sorted: %v", got)
	}
}

// TestWalkTreeSkipsSymlinks asserts a symlink is neither followed (no loop, no
// jail leak) nor included in the result (FR3). Skipped on Windows where the
// symlink-creation perms differ.
func TestWalkTreeSkipsSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	root := t.TempDir()
	mustWrite(t, root, "real.go", "package main\n")
	// A symlink pointing OUTSIDE the root — including or following it would
	// leak past the jail.
	outside := t.TempDir()
	mustWrite(t, outside, "secret.txt", "leak\n")
	if err := os.Symlink(outside, filepath.Join(root, "shortcut")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	entries, err := walkTree(root)
	if err != nil {
		t.Fatalf("walkTree: %v", err)
	}
	got := walkNames(entries)
	if !contains(got, "real.go") {
		t.Errorf("walkTree missing real.go; got %v", got)
	}
	if contains(got, "shortcut") {
		t.Errorf("walkTree included the symlink itself: %v", got)
	}
	if contains(got, filepath.Join("shortcut", "secret.txt")) {
		t.Errorf("walkTree FOLLOWED the symlink and leaked outside the jail: %v", got)
	}
}

// TestWalkTreePermissionDeniedSubdir asserts a 0000-mode subdir is skipped and
// the rest of the walk completes without bubbling an error or panicking (FR13).
// Skipped when running as root (root ignores the permission bits).
func TestWalkTreePermissionDeniedSubdir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod 000 does not block traversal on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root — permission bits do not restrict traversal")
	}
	root := t.TempDir()
	mustWrite(t, root, "visible.go", "package main\n")
	locked := filepath.Join(root, "locked")
	if err := os.Mkdir(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, locked, "inside.txt", "x\n")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) }) // so TempDir cleanup can remove it

	entries, err := walkTree(root)
	if err != nil {
		t.Fatalf("walkTree should swallow permission errors, got: %v", err)
	}
	got := walkNames(entries)
	if !contains(got, "visible.go") {
		t.Errorf("walkTree missing visible.go after a perm-denied subdir; got %v", got)
	}
	if contains(got, filepath.Join("locked", "inside.txt")) {
		t.Errorf("walkTree read inside a 0000 dir: %v", got)
	}
	// The locked dir entry itself is fine to include (its name is readable);
	// what matters is the walk finished and its contents were not read.
}

// TestWalkTreeCap asserts the defensive entry cap (FR14, maxWalkEntries): the
// returned slice never exceeds the cap. Tested with a tiny override is not
// possible (const), so this just asserts the const exists and the result obeys
// it for a normal tree (the real cap of 100k is impractical to hit in a test).
func TestWalkTreeCap(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 50; i++ {
		mustWrite(t, root, fileName(i), "x\n")
	}
	entries, err := walkTree(root)
	if err != nil {
		t.Fatalf("walkTree: %v", err)
	}
	if len(entries) > maxWalkEntries {
		t.Errorf("walkTree returned %d entries, exceeds cap %d", len(entries), maxWalkEntries)
	}
	if len(entries) != 50 {
		t.Errorf("walkTree returned %d entries, want 50", len(entries))
	}
}

func fileName(i int) string {
	return "f" + string(rune('a'+i/26)) + string(rune('a'+i%26)) + ".txt"
}

// TestFilterSearch covers the fuzzy filter (PRD §5.4, FR4, FR5, D11): empty
// query returns the first `limit` entries unchanged (already alpha-sorted),
// a non-empty query returns fuzzy matches mapped back to the right entries,
// non-matches are excluded, and the result respects `limit`.
func TestFilterSearch(t *testing.T) {
	entries := []entry{
		{name: "docs/prd-search.md"},
		{name: "main.go"},
		{name: "model.go"},
		{name: "model_test.go"},
		{name: "view.go"},
	}

	// Empty query → first `limit`, in order.
	got := filterSearch(entries, "", 3)
	if len(got) != 3 {
		t.Fatalf("empty query limit=3 returned %d entries, want 3", len(got))
	}
	if got[0].name != "docs/prd-search.md" || got[2].name != "model.go" {
		t.Errorf("empty query did not return the first 3 in order: %v", walkNames(got))
	}

	// Empty query, limit larger than the slice → the whole slice.
	if all := filterSearch(entries, "", 100); len(all) != len(entries) {
		t.Errorf("empty query, big limit returned %d, want %d", len(all), len(entries))
	}

	// Non-empty query → only entries whose name fuzzy-matches.
	got = filterSearch(entries, "model", maxSearchResults)
	if len(got) == 0 {
		t.Fatal("query \"model\" matched nothing")
	}
	for _, e := range got {
		if e.name == "view.go" {
			t.Errorf("query \"model\" wrongly matched %q", e.name)
		}
	}
	// model.go and model_test.go both contain the subsequence.
	gotNames := walkNames(got)
	if !contains(gotNames, "model.go") || !contains(gotNames, "model_test.go") {
		t.Errorf("query \"model\" missed expected matches; got %v", gotNames)
	}

	// Limit caps the non-empty result too.
	many := make([]entry, 0, 20)
	for i := 0; i < 20; i++ {
		many = append(many, entry{name: "model" + fileName(i)})
	}
	if capped := filterSearch(many, "model", 5); len(capped) != 5 {
		t.Errorf("non-empty query with limit=5 returned %d, want 5", len(capped))
	}

	// No match → empty result (FR15).
	if none := filterSearch(entries, "zzqqxx", maxSearchResults); len(none) != 0 {
		t.Errorf("non-matching query returned %d entries, want 0", len(none))
	}
}

// searchModel builds a small project tree and returns a loaded model whose
// search cache is warm (searchAll already populated via a synchronous walk) so
// search tests don't depend on the async walk Cmd. With a warm cache, enterSearch
// is a cache hit (filters immediately, no Cmd) — the async path is covered
// separately by TestEnterSearchCacheTTL / TestSearchGenInvalidatesStaleWalk.
func searchModel(t *testing.T) model {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, root, "main.go", "package main\n")
	mustWrite(t, root, "model.go", "package main\n")
	mustWrite(t, root, "README.md", "# Readme\n\nbody\n")
	mustMkdir(t, root, "docs")
	mustWrite(t, filepath.Join(root, "docs"), "prd-search.md", "# PRD\n\nsearch\n")
	mustMkdir(t, root, "src")
	mustWrite(t, filepath.Join(root, "src"), "lib.go", "package src\n")
	m := modelAt(t, root, 100, 30)
	walked, err := walkTree(root)
	if err != nil {
		t.Fatalf("warm-cache walk: %v", err)
	}
	m.searchAll = walked
	m.searchAllAt = time.Now()
	return m
}

// TestEnterExitSearch is the snapshot/restore contract (FR1, FR10, D13): pressing
// "/" snapshots cwd/entries/cursor/listTop; Esc restores them EXACTLY. The user
// must land back on the precise pre-search state — same directory, same cursor,
// same scroll.
func TestEnterExitSearch(t *testing.T) {
	m := searchModel(t)
	// Put the cursor on a non-trivial entry and a non-zero scroll so the
	// snapshot/restore is meaningful (not the trivial top-of-list state).
	for i, e := range m.entries {
		if e.name == "model.go" {
			m.cursor = i
		}
	}
	m.listTop = 1

	savedCwd := m.cwd
	savedEntries := append([]entry(nil), m.entries...)
	savedCursor := m.cursor
	savedListTop := m.listTop

	// Enter search (cache is warm → no async walk needed, but force a walk path too).
	cmd := m.enterSearch()
	if m.mode != modeSearch {
		t.Fatalf("after enterSearch mode = %v, want modeSearch", m.mode)
	}
	// If a walk command came back (cache miss), drive it synchronously.
	if cmd != nil {
		if msg, ok := cmd().(searchWalkedMsg); ok {
			var tm tea.Model = m
			tm, _ = tm.Update(msg)
			m = tm.(model)
		}
	}
	// Type a query that filters the list down (state diverges from snapshot).
	m.searchQuery = "xyz"
	m.applySearchFilter()

	// Esc restores.
	m.exitSearchRestore()
	if m.mode != modeNormal {
		t.Errorf("after exit mode = %v, want modeNormal", m.mode)
	}
	if m.cwd != savedCwd {
		t.Errorf("cwd not restored: %q, want %q", m.cwd, savedCwd)
	}
	if m.cursor != savedCursor {
		t.Errorf("cursor not restored: %d, want %d", m.cursor, savedCursor)
	}
	if m.listTop != savedListTop {
		t.Errorf("listTop not restored: %d, want %d", m.listTop, savedListTop)
	}
	if len(m.entries) != len(savedEntries) {
		t.Fatalf("entries not restored: len %d, want %d", len(m.entries), len(savedEntries))
	}
	for i := range savedEntries {
		if m.entries[i].name != savedEntries[i].name {
			t.Errorf("entries[%d] = %q, want %q", i, m.entries[i].name, savedEntries[i].name)
		}
	}
}

// TestOpenSearchResultFileRoot: Enter on a root-level file result jumps cwd to
// root (the file's parent) and lands the cursor on the file (FR8).
func TestOpenSearchResultFileRoot(t *testing.T) {
	m := searchModel(t)
	m.enterSearch()
	m.searchQuery = "main.go"
	m.applySearchFilter()
	if len(m.entries) == 0 || m.entries[0].name != "main.go" {
		t.Fatalf("expected main.go as top result, got %v", walkNames(m.entries))
	}
	m.cursor = 0
	m.openSearchResult()
	if m.mode != modeNormal {
		t.Errorf("mode = %v, want modeNormal", m.mode)
	}
	if m.cwd != m.root {
		t.Errorf("cwd = %q, want root %q", m.cwd, m.root)
	}
	if m.entries[m.cursor].name != "main.go" {
		t.Errorf("cursor landed on %q, want main.go", m.entries[m.cursor].name)
	}
}

// TestOpenSearchResultFileNested: Enter on a nested file (docs/prd-search.md)
// jumps cwd to docs/ and lands the cursor on the basename (FR8).
func TestOpenSearchResultFileNested(t *testing.T) {
	m := searchModel(t)
	m.enterSearch()
	m.searchQuery = "prdsearch"
	m.applySearchFilter()
	// Find the nested result in the filtered list.
	idx := -1
	for i, e := range m.entries {
		if e.name == filepath.Join("docs", "prd-search.md") {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("docs/prd-search.md not in results: %v", walkNames(m.entries))
	}
	m.cursor = idx
	m.openSearchResult()
	if m.mode != modeNormal {
		t.Errorf("mode = %v, want modeNormal", m.mode)
	}
	wantCwd := filepath.Join(m.root, "docs")
	if m.cwd != wantCwd {
		t.Errorf("cwd = %q, want %q", m.cwd, wantCwd)
	}
	if m.entries[m.cursor].name != "prd-search.md" {
		t.Errorf("cursor landed on %q, want prd-search.md", m.entries[m.cursor].name)
	}
}

// TestOpenSearchResultFolder: Enter on a folder result cd's into the folder
// (FR9), cursor at top.
func TestOpenSearchResultFolder(t *testing.T) {
	m := searchModel(t)
	m.enterSearch()
	m.searchQuery = "docs"
	m.applySearchFilter()
	idx := -1
	for i, e := range m.entries {
		if e.name == "docs" && e.isDir {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("docs/ folder not in results: %v", walkNames(m.entries))
	}
	m.cursor = idx
	m.openSearchResult()
	if m.mode != modeNormal {
		t.Errorf("mode = %v, want modeNormal", m.mode)
	}
	wantCwd := filepath.Join(m.root, "docs")
	if m.cwd != wantCwd {
		t.Errorf("cwd = %q, want %q", m.cwd, wantCwd)
	}
}

// TestOpenSearchResultJailBlock: a fabricated result whose name escapes the root
// must be refused by the jail guard — cwd unchanged, no navigation (FR8/FR9 jail
// check via withinRoot).
func TestOpenSearchResultJailBlock(t *testing.T) {
	m := searchModel(t)
	m.enterSearch()
	// Fabricate a malicious result that resolves above the root.
	m.entries = []entry{{name: filepath.Join("..", "..", "etc"), isDir: true}}
	m.cursor = 0
	beforeCwd := m.cwd
	m.openSearchResult()
	if m.cwd != beforeCwd {
		t.Errorf("jail breached: cwd moved to %q (was %q)", m.cwd, beforeCwd)
	}
	if !strings.Contains(m.statusMsg, "outside root") {
		t.Errorf("expected an outside-root warning, got status %q", m.statusMsg)
	}
}

// TestOpenSearchResultEmptyFallback: Enter while the result list is empty (walk
// still running / no matches) falls back to a clean restore rather than indexing
// out of bounds (PRD §5.10).
func TestOpenSearchResultEmptyFallback(t *testing.T) {
	m := searchModel(t)
	savedCwd := m.cwd
	m.enterSearch()
	m.entries = nil // simulate "walk not done yet"
	m.cursor = 0
	m.openSearchResult() // must not panic
	if m.mode != modeNormal {
		t.Errorf("mode = %v, want modeNormal after empty-result Enter", m.mode)
	}
	if m.cwd != savedCwd {
		t.Errorf("cwd = %q, want restored %q", m.cwd, savedCwd)
	}
}

// TestUpdateSearchActivation: "/" in normal mode enters search; the status bar
// shows the prompt and the hint bar is gone (FR1).
func TestUpdateSearchActivation(t *testing.T) {
	m := searchModel(t)
	var tm tea.Model = m
	tm, _ = tm.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = tm.(model)
	if m.mode != modeSearch {
		t.Fatalf("after '/' mode = %v, want modeSearch", m.mode)
	}
	view := m.View().Content
	if !strings.Contains(view, "/▏") {
		t.Errorf("status bar should show the search prompt '/▏'; view tail = %q", lastLine(view))
	}
	if strings.Contains(view, "[r] rename") {
		t.Errorf("hint bar should be hidden in search mode; view tail = %q", lastLine(view))
	}
}

// TestUpdateSearchTyping: printable runes append to the query and re-filter;
// backspace deletes the last rune (FR6).
func TestUpdateSearchTyping(t *testing.T) {
	m := searchModel(t)
	m.enterSearch()
	var tm tea.Model = m
	for _, r := range "model" {
		tm, _ = tm.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	m = tm.(model)
	if m.searchQuery != "model" {
		t.Fatalf("searchQuery = %q, want \"model\"", m.searchQuery)
	}
	// Results should be the model* files, cursor reset to 0.
	if m.cursor != 0 {
		t.Errorf("cursor = %d after typing, want 0 (FR6 reset)", m.cursor)
	}
	if !contains(walkNames(m.entries), "model.go") {
		t.Errorf("typing \"model\" should surface model.go; got %v", walkNames(m.entries))
	}
	// Backspace once → "mode".
	tm, _ = tm.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	m = tm.(model)
	if m.searchQuery != "mode" {
		t.Errorf("after backspace searchQuery = %q, want \"mode\"", m.searchQuery)
	}
}

// TestUpdateSearchBackspaceEmptyExits: backspace on an empty query exits search
// and restores prior state (FR10, D13).
func TestUpdateSearchBackspaceEmptyExits(t *testing.T) {
	m := searchModel(t)
	savedCwd := m.cwd
	m.enterSearch()
	if m.searchQuery != "" {
		t.Fatalf("setup: query not empty")
	}
	var tm tea.Model = m
	tm, _ = tm.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	m = tm.(model)
	if m.mode != modeNormal {
		t.Errorf("backspace on empty query should exit; mode = %v", m.mode)
	}
	if m.cwd != savedCwd {
		t.Errorf("cwd = %q, want restored %q", m.cwd, savedCwd)
	}
}

// TestCopyContentNotWiredInSearch pins FR8/D4: in modeSearch `Y` is a QUERY
// character — it appends to searchQuery and re-filters — it must NEVER copy. The
// updateSearch switch is closed (no fall-through to updateNormal), so a `Y` keypress
// lands in the printable-default branch. Driven through the live Update edge so the
// `Y` takes the exact path a real keypress takes. The recorder proves no copy fired.
func TestCopyContentNotWiredInSearch(t *testing.T) {
	rec := &fieldRecorder{}
	root := t.TempDir()
	mustWrite(t, root, "Yacht.go", "package main\n") // a file whose name contains a 'Y'
	mustWrite(t, root, "main.go", "package main\n")
	m := modelAt(t, root, 100, 30)
	m.tel = rec
	walked, err := walkTree(root)
	if err != nil {
		t.Fatalf("warm-cache walk: %v", err)
	}
	m.searchAll = walked
	m.searchAllAt = time.Now()
	m.enterSearch()

	var tm tea.Model = m
	tm, _ = tm.Update(tea.KeyPressMsg{Code: 'Y', Text: "Y"})
	m = tm.(model)

	if m.searchQuery != "Y" {
		t.Errorf("typing `Y` in search: searchQuery = %q, want \"Y\" (it is a query character, not a copy)", m.searchQuery)
	}
	// Re-filtered: only the Y-bearing file survives an exact-cased 'Y' subsequence.
	if !contains(walkNames(m.entries), "Yacht.go") {
		t.Errorf("typing `Y` should re-filter to Yacht.go; got %v", walkNames(m.entries))
	}
	if _, recorded := rec.last("action.copy_content"); recorded {
		t.Errorf("`Y` in search must NOT copy (it is a query character); a copy was recorded")
	}
}

// TestUpdateSearchNavigation: up/down move the cursor within the result list.
func TestUpdateSearchNavigation(t *testing.T) {
	m := searchModel(t)
	m.enterSearch()
	m.applySearchFilter() // empty query → all walked
	if len(m.entries) < 2 {
		t.Fatalf("need ≥2 results to test navigation, got %d", len(m.entries))
	}
	var tm tea.Model = m
	tm, _ = tm.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = tm.(model)
	if m.cursor != 1 {
		t.Errorf("after down cursor = %d, want 1", m.cursor)
	}
	tm, _ = tm.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m = tm.(model)
	if m.cursor != 0 {
		t.Errorf("after up cursor = %d, want 0", m.cursor)
	}
}

// TestModeSearchPreviewBase is the FR7 contract: the preview pipeline works for
// a highlighted search result whose name is a root-relative path. The base for
// path resolution must be m.root in search mode (not m.cwd), so a nested .md
// result still renders via the glamour pipeline.
func TestModeSearchPreviewBase(t *testing.T) {
	m := searchModel(t)
	m.enterSearch()
	m.searchQuery = "prdsearch"
	m.applySearchFilter()
	idx := -1
	for i, e := range m.entries {
		if e.name == filepath.Join("docs", "prd-search.md") {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("docs/prd-search.md not in results: %v", walkNames(m.entries))
	}
	m.cursor = idx
	m.refreshPreview()
	// srcPath must resolve against root, so it points at the real nested file.
	want := filepath.Join(m.root, "docs", "prd-search.md")
	if m.srcPath != want {
		t.Fatalf("srcPath = %q, want %q (base must be root in search mode)", m.srcPath, want)
	}
	// Drive the async markdown render and assert it produced styled output.
	m.renderNow()
	if !m.previewPreStyled {
		t.Error("nested .md result should render via glamour (previewPreStyled) in search mode")
	}
}

// TestSearchPollLoopSkipped: a tickMsg while in modeSearch must NOT call
// syncFromDisk (FR11) — the existing guard is `m.mode == modeNormal`, so search
// is excluded. Proven by writing a new file to cwd then ticking: the result list
// must stay the search results, not get clobbered by the cwd listing.
func TestSearchPollLoopSkipped(t *testing.T) {
	m := searchModel(t)
	m.enterSearch()
	m.applySearchFilter() // empty query → all walked entries (relPaths)
	resultsBefore := walkNames(m.entries)
	if len(resultsBefore) == 0 {
		t.Fatal("setup: no search results")
	}
	// Write a new file into cwd (root). If syncFromDisk ran, it would rebuild
	// m.entries from the cwd listing (bare names) and lose the relPath results.
	mustWrite(t, m.cwd, "newfile.go", "package main\n")

	var tm tea.Model = m
	tm, _ = tm.Update(tickMsg{})
	m = tm.(model)

	if m.mode != modeSearch {
		t.Fatalf("tick should not change mode; got %v", m.mode)
	}
	gotResults := walkNames(m.entries)
	if len(gotResults) != len(resultsBefore) {
		t.Errorf("tick clobbered the result list: %v (want %v)", gotResults, resultsBefore)
	}
	// A relPath result like docs/prd-search.md proves these are search results,
	// not a flat cwd listing.
	if !contains(gotResults, filepath.Join("docs", "prd-search.md")) {
		t.Errorf("result list lost its relPath entries after a tick: %v", gotResults)
	}
}

// TestSearchGenInvalidatesStaleWalk: a rapid /→Esc→/ bumps searchGen, so a walk
// dispatched by the FIRST activation that lands AFTER the second must be dropped.
// Driven synchronously: feed a stale-gen searchWalkedMsg and a current-gen one,
// assert only the current one mutates searchAll.
func TestSearchGenInvalidatesStaleWalk(t *testing.T) {
	m := searchModel(t)
	// Clear the warm cache so enterSearch dispatches a real walk and bumps gen.
	m.searchAll = nil
	m.enterSearch()
	staleGen := m.searchGen
	// Simulate Esc then re-enter: bump the gen again (cache is empty).
	m.exitSearchRestore()
	m.searchAll = nil
	m.enterSearch()
	freshGen := m.searchGen
	if freshGen == staleGen {
		t.Fatalf("re-entering search did not bump searchGen (stale=%d fresh=%d)", staleGen, freshGen)
	}

	// The stale walk lands late with the OLD gen → must be ignored.
	stale := searchWalkedMsg{gen: staleGen, results: []entry{{name: "STALE_ONLY.go"}}}
	var tm tea.Model = m
	tm, _ = tm.Update(stale)
	m = tm.(model)
	if contains(walkNames(m.searchAll), "STALE_ONLY.go") {
		t.Errorf("stale walk (gen %d) was applied despite current gen %d", staleGen, freshGen)
	}
	if m.searchIndexing != true {
		t.Errorf("stale walk should not clear the indexing flag; searchIndexing=%v", m.searchIndexing)
	}

	// The fresh walk lands → applied.
	fresh := searchWalkedMsg{gen: freshGen, results: []entry{{name: "FRESH.go"}}}
	tm, _ = tm.Update(fresh)
	m = tm.(model)
	if !contains(walkNames(m.searchAll), "FRESH.go") {
		t.Errorf("fresh walk (gen %d) was not applied; searchAll=%v", freshGen, walkNames(m.searchAll))
	}
	if m.searchIndexing {
		t.Error("fresh walk should clear the indexing flag")
	}
}

// TestWalkLandsAfterEsc is the regression guard for a subtle race: a cold-cache
// "/" dispatches a walk, the user hits Esc BEFORE it lands (Esc does not bump
// the gen), then the walk lands. The current-gen walk must warm the cache but
// must NOT clobber the restored normal-mode list — the user is no longer
// searching.
func TestWalkLandsAfterEsc(t *testing.T) {
	m := searchModel(t)
	m.searchAll = nil // cold cache
	normalEntriesBefore := walkNames(m.entries)

	cmd := m.enterSearch() // dispatches walk, mode=search, entries=nil
	if cmd == nil {
		t.Fatal("cold-cache enterSearch should dispatch a walk Cmd")
	}
	m.exitSearchRestore() // Esc before the walk lands → back to normal mode
	if m.mode != modeNormal {
		t.Fatalf("should be normal after esc, got %v", m.mode)
	}

	// The late walk lands (matching gen).
	msg := cmd().(searchWalkedMsg)
	var tm tea.Model = m
	tm, _ = tm.Update(msg)
	m = tm.(model)

	if m.mode != modeNormal {
		t.Errorf("a late walk flipped the mode to %v in normal mode", m.mode)
	}
	got := walkNames(m.entries)
	if len(got) != len(normalEntriesBefore) {
		t.Errorf("late walk clobbered the normal-mode list: got %v, want %v", got, normalEntriesBefore)
	}
	// The cache must still be warmed (so the next "/" is a cache hit).
	if len(m.searchAll) == 0 {
		t.Error("late walk should still warm searchAll even though Esc returned to normal mode")
	}
	if m.searchIndexing {
		t.Error("late walk should clear the indexing flag")
	}
}

// TestEnterSearchCacheTTL: re-activating search within walkCacheTTL reuses the
// cached walk (no new walk Cmd, no indexing chip); after the TTL it re-walks (D8).
func TestEnterSearchCacheTTL(t *testing.T) {
	m := searchModel(t)
	// Warm the cache as if a walk just completed.
	m.searchAll = []entry{{name: "main.go"}}
	m.searchAllAt = time.Now()

	cmd := m.enterSearch()
	if cmd != nil {
		t.Error("fresh cache (within TTL) should NOT dispatch a walk Cmd")
	}
	if m.searchIndexing {
		t.Error("fresh cache should not set the indexing flag")
	}
	m.exitSearchRestore()

	// Expire the cache.
	m.searchAllAt = time.Now().Add(-walkCacheTTL - time.Second)
	cmd = m.enterSearch()
	if cmd == nil {
		t.Error("expired cache should dispatch a fresh walk Cmd")
	}
	if !m.searchIndexing {
		t.Error("expired cache should set the indexing flag while walking")
	}
}

// TestSearchNoResultsHint: a non-empty query that matches nothing shows the
// "0 results" hint and an empty list (FR15). An empty query (browse-all) clears it.
func TestSearchNoResultsHint(t *testing.T) {
	m := searchModel(t)
	m.enterSearch()
	m.searchQuery = "zzqqxxnotathing"
	m.applySearchFilter()
	if len(m.entries) != 0 {
		t.Fatalf("expected 0 results, got %v", walkNames(m.entries))
	}
	if !strings.Contains(m.statusMsg, "0 results") {
		t.Errorf("expected a 0-results hint, got status %q", m.statusMsg)
	}
	// The status bar must surface that hint in search mode.
	if !strings.Contains(m.View().Content, "0 results") {
		t.Errorf("status bar should show the 0-results hint; tail = %q", lastLine(m.View().Content))
	}
	// Clearing the query (browse-all) clears the hint.
	m.searchQuery = ""
	m.applySearchFilter()
	if m.statusMsg != "" {
		t.Errorf("empty query should clear the hint, got %q", m.statusMsg)
	}
}

// TestSearchIndexingChip: while a walk is in flight the status bar shows the
// indexing chip and the list is empty (FR2). Driven via a cold-cache enterSearch.
func TestSearchIndexingChip(t *testing.T) {
	m := searchModel(t)
	m.searchAll = nil // cold cache → enterSearch dispatches a walk + sets indexing
	cmd := m.enterSearch()
	if cmd == nil {
		t.Fatal("cold-cache enterSearch should dispatch a walk Cmd")
	}
	if !m.searchIndexing {
		t.Fatal("searchIndexing should be true while the walk runs")
	}
	if len(m.entries) != 0 {
		t.Errorf("list should be empty while indexing, got %v", walkNames(m.entries))
	}
	if !strings.Contains(m.View().Content, "indexing") {
		t.Errorf("status bar should show the indexing chip; tail = %q", lastLine(m.View().Content))
	}
	// Drive the walk to completion → chip clears, list fills.
	msg := cmd().(searchWalkedMsg)
	var tm tea.Model = m
	tm, _ = tm.Update(msg)
	m = tm.(model)
	if m.searchIndexing {
		t.Error("indexing chip should clear once the walk lands")
	}
	if len(m.entries) == 0 {
		t.Error("list should fill once the walk lands")
	}
}

// lastLine returns the final non-empty line of s (the status bar row).
func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return lines[len(lines)-1]
}
