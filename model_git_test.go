package main

// Async-wiring tests for the git change layer in the model (prd-git-change-
// indicator §5.2): repo detection + first-refresh priming, gen-gated apply with
// the in-flight guard, the poll-tick dispatch that is deliberately INDEPENDENT
// of the dirSig gate (D9), and the end-to-end View showing a badge.

import (
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
