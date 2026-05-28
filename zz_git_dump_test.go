package main

// Visual-test harness for the git change indicator (prd-git-change-indicator):
// builds a repo with a spread of git states, drives the async git snapshot into
// the model, and dumps View() as a raw-ANSI frame so it can be rendered to an
// image (freeze) and judged by eye / an agent. Gated on an env var so a normal
// `go test` run never touches the filesystem outside its temp dirs.
//
//	LAZYEXPLORER_GIT_DUMP=/tmp/le-git go test -run TestDumpGitFrame .

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestDumpGitFrame(t *testing.T) {
	out := os.Getenv("LAZYEXPLORER_GIT_DUMP")
	if out == "" {
		t.Skip("set LAZYEXPLORER_GIT_DUMP to dump a git-indicator frame")
	}

	repo := t.TempDir()
	gitExec(t, repo, "init")
	// Baseline commit: a couple of tracked files + a package dir.
	mustWrite(t, repo, "keep.go", "package main\n\nfunc keep() {}\n")
	if err := os.MkdirAll(filepath.Join(repo, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(repo, "pkg"), "util.go", "package pkg\n\nfunc Util() {}\n")
	gitExec(t, repo, "add", "-A")
	gitExec(t, repo, "commit", "-m", "baseline")

	// Spread of states:
	mustWrite(t, repo, "keep.go", "package main\n\nfunc keep() {}\n\nfunc more() {}\nfunc evenMore() {}\n") // modified +3
	mustWrite(t, filepath.Join(repo, "pkg"), "util.go", "package pkg\n\nfunc Util() int { return 1 }\n")    // modified inside pkg/ → folder ●
	mustWrite(t, repo, "notes.txt", "alpha\nbeta\ngamma\n")                                                 // untracked ? +3
	if err := os.MkdirAll(filepath.Join(repo, "feature"), 0o755); err != nil {                              // untracked folder → ●
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(repo, "feature"), "a.go", "package feature\n")
	mustWrite(t, filepath.Join(repo, "feature"), "b.go", "package feature\n\nvar X = 1\n")
	mustWrite(t, repo, "staged.go", "package main\n") // staged new → A
	gitExec(t, repo, "add", "staged.go")

	m := newModel(repo, nil)
	m.width, m.height = 90, 26
	nm, _ := m.Update(gitRefreshedMsg{gen: m.gitGen, state: collectGitState(m.git.repoRoot)})
	m = nm.(model)
	// Move the cursor off ".git" onto a real changed file so the screenshot shows
	// both an active row (plain indicator) and inactive rows (colored badges).
	for i, e := range m.entries {
		if e.name == "keep.go" {
			m.cursor = i
			break
		}
	}
	m.refreshPreview()
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 26})
	m = m2.(model)

	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(out, "git-indicator.ansi")
	if err := os.WriteFile(path, []byte(m.View().Content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s", path)
}
