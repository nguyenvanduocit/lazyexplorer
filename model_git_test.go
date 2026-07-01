package main

// Async-wiring tests for the git change layer in the model (prd-git-change-
// indicator §5.2): repo detection + first-refresh priming, gen-gated apply with
// the in-flight guard, the poll-tick dispatch that is deliberately INDEPENDENT
// of the dirSig gate (D9), and the end-to-end View showing a badge.

import (
	"slices"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestNewModelDetectsRepoAndPrimesRefresh(t *testing.T) {
	repo := t.TempDir()
	gitExec(t, repo, "init")
	m := newModel(repo, nil)
	if m.git.repoRoot == "" {
		t.Fatal("newModel in a repo should detect repoRoot")
	}
	if m.gitGen != 1 || !m.gitInFlight {
		t.Errorf("first refresh should be primed (gitGen=1, in-flight); got gen=%d inFlight=%v",
			m.gitGen, m.gitInFlight)
	}

	notRepo := newModel(t.TempDir(), nil)
	if notRepo.git.repoRoot != "" || notRepo.gitInFlight {
		t.Errorf("a non-repo must leave git mode off; got repoRoot=%q inFlight=%v",
			notRepo.git.repoRoot, notRepo.gitInFlight)
	}
}

func TestGitRefreshedMsgAppliedAndGated(t *testing.T) {
	// Matching gen → state AND cache applied together, in-flight cleared.
	m := model{gitGen: 5, gitInFlight: true}
	fresh := gitState{repoRoot: "/r", changes: map[string]gitChange{"a": {code: gitModified}}}
	nm, _ := m.Update(gitRefreshedMsg{gen: 5, state: fresh, cache: untrackedCache{"a": {lines: 7, ok: true}}})
	m = nm.(model)
	if m.gitInFlight {
		t.Error("in-flight must clear after a result lands")
	}
	if _, ok := m.git.changes["a"]; !ok {
		t.Errorf("matching-gen result should be applied; got %v", m.git.changes)
	}
	if m.gitUntrackedCache["a"].lines != 7 {
		t.Errorf("matching-gen result should reassign the untracked cache; got %v", m.gitUntrackedCache)
	}

	// Stale gen → state AND cache both dropped, but in-flight still cleared.
	m2 := model{gitGen: 9, gitInFlight: true, git: fresh}
	stale := gitState{repoRoot: "/r", changes: map[string]gitChange{"b": {code: gitUntracked}}}
	nm2, _ := m2.Update(gitRefreshedMsg{gen: 7, state: stale, cache: untrackedCache{"b": {lines: 3, ok: true}}})
	m2 = nm2.(model)
	if m2.gitInFlight {
		t.Error("in-flight must clear even for a stale result")
	}
	if _, ok := m2.git.changes["b"]; ok {
		t.Errorf("stale-gen result must be dropped; got %v", m2.git.changes)
	}
	if len(m2.gitUntrackedCache) != 0 {
		t.Errorf("stale-gen result must NOT touch the untracked cache; got %v", m2.gitUntrackedCache)
	}
}

func TestTickDispatchesGitRefreshIndependentOfDirSig(t *testing.T) {
	repo := t.TempDir()
	gitExec(t, repo, "init")
	m := newModel(repo, nil)
	m.gitInFlight = false // pretend the Init refresh already landed
	startGen := m.gitGen

	// A tick dispatches a git refresh even though the directory listing is
	// unchanged (dirSig steady) — git state can move without a file mtime change.
	nm, cmd := m.Update(tickMsg{})
	m = nm.(model)
	if !m.gitInFlight || m.gitGen != startGen+1 || cmd == nil {
		t.Errorf("tick should dispatch a refresh (in-flight, gen %d→%d, cmd!=nil); got inFlight=%v gen=%d cmd==nil:%v",
			startGen, startGen+1, m.gitInFlight, m.gitGen, cmd == nil)
	}

	// A second tick while one is in flight must NOT stack another (no pileup).
	preGen := m.gitGen
	nm2, _ := m.Update(tickMsg{})
	m = nm2.(model)
	if m.gitGen != preGen {
		t.Errorf("a tick while in-flight must not re-dispatch; gen moved %d→%d", preGen, m.gitGen)
	}
}

func TestViewShowsGitBadge(t *testing.T) {
	repo := t.TempDir()
	gitExec(t, repo, "init")
	mustWrite(t, repo, "tracked.go", "package main\n")
	gitExec(t, repo, "add", "tracked.go")
	gitExec(t, repo, "commit", "-m", "init")
	mustWrite(t, repo, "tracked.go", "package main\n\nfunc main() {}\n") // +2 lines

	m := newModel(repo, nil)
	m.width, m.height = 120, 30
	// Deliver the git snapshot exactly as the async cmd would.
	state, _ := collectGitState(m.git.repoRoot, nil)
	nm, _ := m.Update(gitRefreshedMsg{gen: m.gitGen, state: state})
	m = nm.(model)

	content := m.View().Content
	plain := ansi.Strip(content)
	if !strings.Contains(plain, "tracked.go") {
		t.Fatalf("listing should contain tracked.go; got:\n%s", plain)
	}
	// tracked.go is an inactive row (the cursor lands on the ".git" dir, which
	// sorts first), so its badge keeps full color. Assert the muted "+2" add
	// delta and the amber "M" badge both rendered through the live View.
	if !strings.Contains(plain, "+2") {
		t.Errorf("modified file should show its +2 add delta; got:\n%s", plain)
	}
	if !strings.Contains(content, leadingSGR(t, dimStyle)+"+2") {
		t.Errorf("the +2 delta should be muted (dimStyle); got:\n%q", content)
	}
	badgeSGR := leadingSGR(t, lipgloss.NewStyle().Foreground(gitColor(gitModified)))
	if !strings.Contains(content, badgeSGR+"M") {
		t.Errorf("the M badge should carry the modified color; got:\n%q", content)
	}
}

// ---------------------------------------------------------------------------
// ignored-entry dimming + ordering (prd-ignored-entry-dimming §5.2)
// ---------------------------------------------------------------------------

// ignoredModel builds a minimal model whose jail root == repo root (gitRootPrefix
// empty), so repoRelKey maps a cwd entry name straight to its repo-relative key —
// enough to drive the ignored predicate + ordering without standing up a git tree.
func ignoredModel(ignored ...string) model {
	set := map[string]bool{}
	for _, k := range ignored {
		set[k] = true
	}
	return model{
		root: "/repo", cwd: "/repo", mode: modeNormal,
		git: gitState{repoRoot: "/repo", changes: map[string]gitChange{}, dirtyDirs: map[string]bool{}, ignored: set},
	}
}

func TestIsIgnoredEntry(t *testing.T) {
	m := ignoredModel("node_modules", "build.log")
	if !m.isIgnoredEntry(m.cwd, entry{name: "node_modules", isDir: true}) {
		t.Error("an ignored dir entry should resolve ignored")
	}
	if !m.isIgnoredEntry(m.cwd, entry{name: "build.log"}) {
		t.Error("an ignored file entry should resolve ignored")
	}
	if m.isIgnoredEntry(m.cwd, entry{name: "src", isDir: true}) {
		t.Error("a normal dir must not be ignored")
	}
	if m.isIgnoredEntry(m.cwd, entry{name: "..", isDir: true}) {
		t.Error("the synthetic .. must never be ignored")
	}
	off := model{root: "/repo", cwd: "/repo"} // git mode off
	if off.isIgnoredEntry(off.cwd, entry{name: "node_modules", isDir: true}) {
		t.Error("with git mode off nothing is ignored")
	}
}

// TestPartitionIgnoredSinksStably pins D5: ignored entries sink BELOW every
// non-ignored one (absolute bottom), each group keeping its incoming dirs-first-alpha
// order, with ".." pinned at the top.
func TestPartitionIgnoredSinksStably(t *testing.T) {
	m := ignoredModel("node_modules", "dist", "build.log")
	in := []entry{
		{name: "..", isDir: true},
		{name: "dist", isDir: true},         // ignored dir
		{name: "node_modules", isDir: true}, // ignored dir
		{name: "src", isDir: true},
		{name: "build.log"}, // ignored file
		{name: "main.go"},
		{name: "readme.md"},
	}
	got := names(m.partitionIgnored(in, m.cwd))
	want := []string{"..", "src", "main.go", "readme.md", "dist", "node_modules", "build.log"}
	if !slices.Equal(got, want) {
		t.Errorf("partition order = %v, want %v", got, want)
	}
}

func TestPartitionIgnoredIdempotent(t *testing.T) {
	m := ignoredModel("node_modules")
	in := []entry{{name: "..", isDir: true}, {name: "node_modules", isDir: true}, {name: "src", isDir: true}, {name: "main.go"}}
	once := m.partitionIgnored(in, m.cwd)
	twice := m.partitionIgnored(once, m.cwd)
	if !slices.Equal(names(once), names(twice)) {
		t.Errorf("partition not idempotent: once=%v twice=%v", names(once), names(twice))
	}
}

func TestPartitionIgnoredNoopOutsideRepo(t *testing.T) {
	m := model{root: "/repo", cwd: "/repo"} // git mode off
	in := []entry{{name: "node_modules", isDir: true}, {name: "src", isDir: true}}
	got := m.partitionIgnored(in, m.cwd)
	if !slices.Equal(names(got), names(in)) {
		t.Errorf("outside a repo the order must be untouched; got %v", names(got))
	}
}

// TestGitRefreshReordersKeepingCursorByName pins FR10: when a git snapshot lands and
// marks an entry ignored, the list re-sinks it and the cursor stays on the SAME entry
// by name (its index shifts as ignored entries move under it).
func TestGitRefreshReordersKeepingCursorByName(t *testing.T) {
	m := ignoredModel() // nothing ignored yet
	m.gitGen, m.gitInFlight = 1, true
	m.entries = []entry{
		{name: "..", isDir: true},
		{name: "node_modules", isDir: true},
		{name: "src", isDir: true},
		{name: "main.go"},
	}
	m.cursor = 3 // on main.go
	state := gitState{repoRoot: "/repo", changes: map[string]gitChange{}, dirtyDirs: map[string]bool{}, ignored: map[string]bool{"node_modules": true}}
	nm, _ := m.Update(gitRefreshedMsg{gen: 1, state: state})
	m = nm.(model)

	want := []string{"..", "src", "main.go", "node_modules"}
	if !slices.Equal(names(m.entries), want) {
		t.Errorf("git-land reorder = %v, want %v", names(m.entries), want)
	}
	if m.entries[m.cursor].name != "main.go" {
		t.Errorf("cursor must stay on main.go by name after reorder; got %q (idx %d)", m.entries[m.cursor].name, m.cursor)
	}
}
