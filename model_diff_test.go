package main

// Tests for the diff-view preview feature (prd-preview-diff-view): the
// state-select predicate (diffApplies), the refreshPreview branch routing, the
// async diff dispatch + gen-gate + error fallback in syncPreview, the `v` toggle
// (flip + re-dispatch + persistence + telemetry), and parked live-refresh.
//
// Helpers reused from the suite: modelAt, gitExec, mustWrite, mustMkdir.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// diffModel builds a real git repo with a committed baseline, applies the
// caller's mutations via `mutate`, detects the repo, and delivers the git
// snapshot through Update (the way the async cmd would) so m.git.changes is
// populated. diffOn defaults TRUE here (mirroring newModel) so a modified file
// lands on the diff path. cwd is the repo root unless mutate changes it.
func diffModel(t *testing.T, mutate func(repo string)) model {
	t.Helper()
	repo := t.TempDir()
	gitExec(t, repo, "init")
	mustWrite(t, repo, "tracked.txt", "alpha\nbeta\ngamma\n")
	gitExec(t, repo, "add", ".")
	gitExec(t, repo, "commit", "-m", "init")
	mutate(repo)

	m := modelAt(t, repo, 120, 30)
	m.diffOn = true // modelAt builds the struct directly (diffOn zero-value=false); newModel ships true (D3)
	if m.git.repoRoot == "" {
		m.git.repoRoot = detectRepoRoot(m.root)
		m.gitRootPrefix = repoRelPrefix(m.git.repoRoot, m.root)
	}
	state, _ := collectGitState(m.git.repoRoot, nil)
	var tm tea.Model = m
	tm, _ = tm.Update(gitRefreshedMsg{gen: m.gitGen, state: state})
	return tm.(model)
}

func selectEntry(t *testing.T, m *model, name string) {
	t.Helper()
	for i, e := range m.entries {
		if e.name == name {
			m.cursor = i
			m.refreshPreview()
			return
		}
	}
	t.Fatalf("entry %q not found in %v", name, entryNames(*m))
}

// ----------------------------------------------------------------------------
// diffApplies — the state-select predicate (T3/§5.2)
// ----------------------------------------------------------------------------

// TestNewModelDiffOnDefault pins D3/FR1's linchpin: the SHIPPED app must land on a
// modified file showing its DIFF by default (the inverse of wrap's default-off).
// Every other diff test forces m.diffOn=true after modelAt (which struct-constructs
// the zero-value false), so none guards the constructor default the /goal depends
// on. This is that guard: flipping newModel's `diffOn: true`→`false` must turn this
// red (and it alone).
func TestNewModelDiffOnDefault(t *testing.T) {
	if !newModel(t.TempDir(), noopRecorder{}).diffOn {
		t.Errorf("ship default must be diffOn=true per D3 (a modified file lands on its diff)")
	}
}

// TestDiffAppliesOnlyForModifiedText pins D6: diffApplies is TRUE only for a
// tracked modified (M) or renamed (R) TEXT file. Untracked, added, conflict,
// clean, dir, binary, and out-of-repo selections are all FALSE → content path.
func TestDiffAppliesModifiedText(t *testing.T) {
	m := diffModel(t, func(repo string) {
		mustWrite(t, repo, "tracked.txt", "alpha\nBETA\ngamma\n") // modify → M
	})
	sel := entryByName(t, m, "tracked.txt")
	if !m.diffApplies(sel, "text") {
		t.Errorf("a modified tracked text file must diff; changes=%v", m.git.changes)
	}
	// A binary kind on the same M file is NOT a diff (FR5 placeholder path instead).
	if m.diffApplies(sel, "binary") {
		t.Errorf("a modified file read as binary must NOT diff (FR5)")
	}
}

func TestDiffAppliesUntrackedAndAddedAreContent(t *testing.T) {
	m := diffModel(t, func(repo string) {
		mustWrite(t, repo, "fresh.txt", "new file\n")    // untracked → ?
		mustWrite(t, repo, "staged.txt", "staged new\n") // will be added → A
		gitExec(t, repo, "add", "staged.txt")
	})
	if u := entryByName(t, m, "fresh.txt"); m.diffApplies(u, "text") {
		t.Errorf("untracked file must NOT diff (content is the useful diff); changes=%v", m.git.changes)
	}
	if a := entryByName(t, m, "staged.txt"); m.diffApplies(a, "text") {
		t.Errorf("added (staged-new) file must NOT diff; changes=%v", m.git.changes)
	}
}

func TestDiffAppliesCleanAndNonRepoAreFalse(t *testing.T) {
	// Clean tracked file in a repo with changes elsewhere.
	m := diffModel(t, func(repo string) {
		mustWrite(t, repo, "other.txt", "untracked\n") // make the repo dirty somewhere
	})
	if c := entryByName(t, m, "tracked.txt"); m.diffApplies(c, "text") {
		t.Errorf("a clean tracked file must NOT diff")
	}

	// Outside a git repo entirely: modelAt does not detect a repo, so repoRoot is
	// empty and diffApplies must short-circuit to false (FR9).
	dir := t.TempDir()
	mustWrite(t, dir, "f.txt", "hi\n")
	nm := modelAt(t, dir, 100, 30)
	nm.diffOn = true
	if nm.git.repoRoot != "" {
		t.Fatalf("a plain temp dir must not be a git repo; repoRoot=%q", nm.git.repoRoot)
	}
	if a := entryByName(t, nm, "f.txt"); nm.diffApplies(a, "text") {
		t.Errorf("outside a repo, diffApplies must be false (repoRoot empty)")
	}
}

// TestDiffAppliesConflictIsContent pins D6 + the Gherkin "conflicted file shows
// its content with the merge markers": a conflict (U/AA/DD) must NOT diff — the
// user reads the <<<<<<< markers straight from the content. This is the safety
// case: a combined-diff of a conflict is exactly the noise the PRD rejects.
func TestDiffAppliesConflictIsContent(t *testing.T) {
	m := diffModel(t, func(repo string) {})
	// Inject a conflict change for the tracked file (a real merge conflict is
	// fiddly to stage; the predicate reads the collapsed code, so injection is the
	// faithful unit-level fixture — same pattern as the empty-diff test).
	m.git.changes["tracked.txt"] = gitChange{code: gitConflict}
	if c := entryByName(t, m, "tracked.txt"); m.diffApplies(c, "text") {
		t.Errorf("a conflicted file must NOT diff (content shows the merge markers, D6)")
	}
	selectEntry(t, &m, "tracked.txt")
	if m.previewIsDiff {
		t.Errorf("a conflicted file must route to the content path, not the diff path")
	}
}

// TestDiffAppliesRenamedDiffs closes the §8 predicate list: a renamed (R, new
// side) text file DOES diff — it is a tracked change vs HEAD just like M.
func TestDiffAppliesRenamedDiffs(t *testing.T) {
	m := diffModel(t, func(repo string) {})
	m.git.changes["tracked.txt"] = gitChange{code: gitRenamed}
	if c := entryByName(t, m, "tracked.txt"); !m.diffApplies(c, "text") {
		t.Errorf("a renamed (R) text file must diff (tracked change vs HEAD, D6)")
	}
}

// ----------------------------------------------------------------------------
// refreshPreview state-select branch routing (T3/§5.2)
// ----------------------------------------------------------------------------

// TestRefreshPreviewMarksDiff: landing on a modified text file with diffOn sets
// previewIsDiff + previewScrollable and stashes the source for the fallback.
func TestRefreshPreviewMarksDiff(t *testing.T) {
	m := diffModel(t, func(repo string) {
		mustWrite(t, repo, "tracked.txt", "alpha\nBETA\ngamma\n")
	})
	selectEntry(t, &m, "tracked.txt")
	if !m.previewIsDiff {
		t.Errorf("modified text file with diffOn must set previewIsDiff")
	}
	if !m.previewScrollable {
		t.Errorf("diff must be scrollable (mirror code, D9)")
	}
	if len(m.srcRaw) == 0 {
		t.Errorf("srcRaw must hold the source for the FR6/FR10 fallback")
	}
}

// TestRefreshPreviewDiffOffShowsContent: with diffOn=false the same modified file
// is NOT a diff — it routes to the content path (previewIsDiff stays false).
func TestRefreshPreviewDiffOffShowsContent(t *testing.T) {
	m := diffModel(t, func(repo string) {
		mustWrite(t, repo, "tracked.txt", "alpha\nBETA\ngamma\n")
	})
	m.diffOn = false
	selectEntry(t, &m, "tracked.txt")
	if m.previewIsDiff {
		t.Errorf("with diffOn=false a modified file must show content, not diff")
	}
}

// TestRefreshPreviewModifiedBinaryPlaceholder pins FR5: a tracked-modified BINARY
// with no image renderer shows the "binary files differ" placeholder, NOT a diff
// and NOT a hunk exec.
func TestRefreshPreviewModifiedBinaryPlaceholder(t *testing.T) {
	m := diffModel(t, func(repo string) {
		// Commit a binary blob, then modify it. .bin has no renderer.
		mustWriteBytes(t, repo, "blob.bin", []byte{0x00, 0x01, 0x02, 0x03})
		gitExec(t, repo, "add", "blob.bin")
		gitExec(t, repo, "commit", "-m", "blob")
		mustWriteBytes(t, repo, "blob.bin", []byte{0x00, 0x09, 0x09, 0x09, 0x09})
	})
	selectEntry(t, &m, "blob.bin")
	if m.previewIsDiff {
		t.Errorf("a modified binary must NOT diff (FR5)")
	}
	body := strings.Join(m.preview, "\n")
	if !strings.Contains(ansi.Strip(body), "binary files differ") {
		t.Errorf("modified binary must show 'binary files differ' placeholder; got %q", body)
	}
}

// TestRefreshPreviewModifiedImageStillRenders is the discriminating case for
// must-fix #1: a MODIFIED .png must still reach the image renderer (binary:true),
// NOT the "binary files differ" placeholder. previewIsDiff is false (it's binary)
// and the preview routes through the image renderer (srcPath set, scrollable off).
func TestRefreshPreviewModifiedImageStillRenders(t *testing.T) {
	m := diffModel(t, func(repo string) {
		writePNG(t, filepath.Join(repo, "pic.png"), 8, 8) // a real PNG header to decode
		gitExec(t, repo, "add", "pic.png")
		gitExec(t, repo, "commit", "-m", "pic")
		// Modify the image: append bytes → different content → M badge. (Still a
		// decodable PNG prefix, so the image renderer reads its header.)
		orig, err := os.ReadFile(filepath.Join(repo, "pic.png"))
		if err != nil {
			t.Fatal(err)
		}
		mustWriteBytes(t, repo, "pic.png", append(orig, 0x00, 0x01, 0x02))
	})
	selectEntry(t, &m, "pic.png")
	if m.previewIsDiff {
		t.Errorf("a modified image must NOT diff (FR5)")
	}
	body := ansi.Strip(strings.Join(m.preview, "\n"))
	if strings.Contains(body, "binary files differ") {
		t.Errorf("a modified IMAGE must render via the image renderer, not the binary-diff placeholder; got %q", body)
	}
	// The image renderer claims it (srcPath set so syncPreview dispatches it).
	if m.srcPath == "" {
		t.Errorf("modified image must be claimed by the image renderer (srcPath set)")
	}
}

// ----------------------------------------------------------------------------
// syncPreview async dispatch + gen-gate + error fallback (T4/§5.3)
// ----------------------------------------------------------------------------

// TestSyncPreviewDispatchesDiff drives the diff render to completion and asserts
// the pane shows actual hunks (colored +/- content), not the plain placeholder.
func TestSyncPreviewDispatchesDiff(t *testing.T) {
	m := diffModel(t, func(repo string) {
		mustWrite(t, repo, "tracked.txt", "alpha\nDELTA\ngamma\n")
	})
	selectEntry(t, &m, "tracked.txt")
	m.renderNow() // run the diff closure + apply
	body := ansi.Strip(strings.Join(m.preview, "\n"))
	if !strings.Contains(body, "+DELTA") || !strings.Contains(body, "-beta") {
		t.Errorf("dispatched diff must show +DELTA and -beta hunks; got:\n%s", body)
	}
	if !m.previewPreStyled {
		t.Errorf("diff lines carry ANSI → previewPreStyled must be true after apply")
	}
}

// TestDiffViewShowsWholeFileEndToEnd is the end-to-end proof of the full-file
// context promise (FR1): a modified file's preview, rendered all the way through
// View().Content, shows the WHOLE file — distant unchanged lines kept as context —
// with the edit in place, not a truncated hunk window. The earlier diff tests use a
// 3-line fixture that fits inside git's default context anyway, so they cannot tell
// truncated from whole-file. This commits a 15-line file and edits line 8: git's
// default 3-line context would drop the first and last lines, so finding ctx01 /
// ctx15 in the rendered pane proves the whole file survives the full pipeline
// (diffHunks → applyPreview → renderPreview), the view the user actually reads.
func TestDiffViewShowsWholeFileEndToEnd(t *testing.T) {
	repo := t.TempDir()
	gitExec(t, repo, "init")
	base := "ctx01\nctx02\nctx03\nctx04\nctx05\nctx06\nctx07\nctx08\nctx09\nctx10\nctx11\nctx12\nctx13\nctx14\nctx15\n"
	mustWrite(t, repo, "big.txt", base)
	gitExec(t, repo, "add", ".")
	gitExec(t, repo, "commit", "-m", "init")
	mustWrite(t, repo, "big.txt", strings.Replace(base, "ctx08", "EDITED", 1)) // edit deep in the middle

	m := modelAt(t, repo, 120, 40)
	m.diffOn = true // modelAt struct-constructs diffOn=false; the ship default is true (D3)
	if m.git.repoRoot == "" {
		m.git.repoRoot = detectRepoRoot(m.root)
		m.gitRootPrefix = repoRelPrefix(m.git.repoRoot, m.root)
	}
	state, _ := collectGitState(m.git.repoRoot, nil)
	var tm tea.Model = m
	tm, _ = tm.Update(gitRefreshedMsg{gen: m.gitGen, state: state})
	m = tm.(model)

	selectEntry(t, &m, "big.txt")
	if !m.previewIsDiff {
		t.Fatalf("big.txt (modified text) must be on the diff path; changes=%v", m.git.changes)
	}
	m.renderNow()

	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "EDITED") {
		t.Errorf("rendered diff view must show the edit; got:\n%s", view)
	}
	// Distant unchanged lines present → the WHOLE file is rendered, not a hunk window.
	for _, want := range []string{"ctx01", "ctx02", "ctx15"} {
		if !strings.Contains(view, want) {
			t.Errorf("full-file diff view must keep distant context line %q; got:\n%s", want, view)
		}
	}
}

// TestSyncPreviewDiffStaleGenDropped pins FR7: a diff result whose gen no longer
// matches (the user navigated on) is dropped by the unchanged applyPreview gate.
func TestSyncPreviewDiffStaleGenDropped(t *testing.T) {
	m := diffModel(t, func(repo string) {
		mustWrite(t, repo, "tracked.txt", "alpha\nDELTA\ngamma\n")
	})
	selectEntry(t, &m, "tracked.txt")
	cmd := m.syncPreview()
	if cmd == nil {
		t.Fatal("expected a diff render cmd")
	}
	msg := cmd().(previewRenderedMsg)
	// Simulate the user moving on: bump renderGen so the in-flight result is stale.
	m.renderGen++
	before := append([]string(nil), m.preview...)
	m.applyPreview(msg)
	if strings.Join(m.preview, "\n") != strings.Join(before, "\n") {
		t.Errorf("a stale diff (gen mismatch) must be dropped, leaving the preview unchanged")
	}
}

// TestSyncPreviewDiffEmptyFallsBackToContent pins FR6/D10: a tracked file whose
// `git diff` is empty (mode-only / no textual change) degrades to full content —
// the pane shows the file body, never an empty pane.
func TestSyncPreviewDiffEmptyFallsBackToContent(t *testing.T) {
	// A committed file that is currently dirty by badge but textually identical:
	// stage a change then revert the working tree so the badge persists in our
	// injected state while `git diff HEAD` is empty. Simplest: inject a fake M
	// change for a clean file, then render — diffHunks returns empty → fallback.
	m := diffModel(t, func(repo string) {})
	// Force the diff path on a clean file by injecting an M change into git state.
	m.git.changes["tracked.txt"] = gitChange{code: gitModified}
	selectEntry(t, &m, "tracked.txt")
	if !m.previewIsDiff {
		t.Fatalf("setup: tracked.txt should be on the diff path with the injected M change")
	}
	m.renderNow()
	body := ansi.Strip(strings.Join(m.preview, "\n"))
	if !strings.Contains(body, "alpha") || !strings.Contains(body, "gamma") {
		t.Errorf("empty diff must fall back to full content (the file body); got:\n%s", body)
	}
	if m.previewPreStyled {
		t.Errorf("content fallback is plain (not pre-styled)")
	}
}

// TestSyncPreviewDiffResilientFallback pins FR10: if diffHunks fails (repoRoot is
// not a real repo), the closure degrades to content rather than crashing or
// leaving the pane empty.
func TestSyncPreviewDiffResilientFallback(t *testing.T) {
	m := diffModel(t, func(repo string) {})
	m.git.changes["tracked.txt"] = gitChange{code: gitModified}
	selectEntry(t, &m, "tracked.txt")
	m.git.repoRoot = t.TempDir() // point the diff exec at a non-repo → git diff fails
	m.srcWidth = 0               // force re-dispatch
	m.pendingWidth = 0
	m.renderNow()
	body := ansi.Strip(strings.Join(m.preview, "\n"))
	if !strings.Contains(body, "alpha") {
		t.Errorf("a failed diff must degrade to content; got:\n%s", body)
	}
}

// ----------------------------------------------------------------------------
// `v` toggle: flip + re-dispatch + persistence + telemetry (T5/§5.3)
// ----------------------------------------------------------------------------

func keyV() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 'v', Text: "v"} }

// TestToggleDiffFlipsAndReRenders: pressing `v` on a diff flips to content, and
// `v` again flips back to diff — each toggle re-dispatches (previewIsDiff tracks).
func TestToggleDiffFlipsAndReRenders(t *testing.T) {
	m := diffModel(t, func(repo string) {
		mustWrite(t, repo, "tracked.txt", "alpha\nDELTA\ngamma\n")
	})
	selectEntry(t, &m, "tracked.txt")
	m.renderNow()
	if !m.previewIsDiff {
		t.Fatalf("setup: should start on diff")
	}

	// v → content
	var tm tea.Model = m
	tm, _ = tm.Update(keyV())
	m = tm.(model)
	if m.diffOn {
		t.Errorf("v must flip diffOn to false")
	}
	if m.previewIsDiff {
		t.Errorf("after v→off, previewIsDiff must be false (content path)")
	}
	m.renderNow()
	if m.previewPreStyled {
		// content of a .txt is plain text → not pre-styled
		t.Errorf("content view of a .txt must be plain (not pre-styled)")
	}

	// v → diff again
	tm = m
	tm, _ = tm.Update(keyV())
	m = tm.(model)
	if !m.diffOn || !m.previewIsDiff {
		t.Errorf("v again must restore the diff (diffOn=%v previewIsDiff=%v)", m.diffOn, m.previewIsDiff)
	}
}

// TestToggleDiffPersistsAcrossFiles pins D3/FR3: diffOn is session-sticky — after
// toggling to content, navigating to ANOTHER modified file still shows content.
func TestToggleDiffPersistsAcrossFiles(t *testing.T) {
	m := diffModel(t, func(repo string) {
		mustWrite(t, repo, "tracked.txt", "alpha\nDELTA\ngamma\n")
		mustWrite(t, repo, "second.txt", "x\ny\n")
		gitExec(t, repo, "add", "second.txt")
		gitExec(t, repo, "commit", "-m", "add second")
		mustWrite(t, repo, "second.txt", "x\nY\n") // modify → M
	})
	selectEntry(t, &m, "tracked.txt")
	var tm tea.Model = m
	tm, _ = tm.Update(keyV()) // diffOn → false
	m = tm.(model)
	selectEntry(t, &m, "second.txt")
	if m.previewIsDiff {
		t.Errorf("diffOn=false must persist to the next modified file (session-sticky D3)")
	}
}

// TestToggleDiffRecordsTelemetry pins FR12/D14: `v` records action.preview_diff_toggle
// with diff = the NEW diffOn state.
func TestToggleDiffRecordsTelemetry(t *testing.T) {
	rec := &fieldRecorder{}
	m := diffModel(t, func(repo string) {
		mustWrite(t, repo, "tracked.txt", "alpha\nDELTA\ngamma\n")
	})
	m.tel = rec
	selectEntry(t, &m, "tracked.txt")

	var tm tea.Model = m
	tm, _ = tm.Update(keyV()) // diffOn true → false
	m = tm.(model)
	got, ok := rec.last("action.preview_diff_toggle")
	if !ok {
		t.Fatalf("v must record action.preview_diff_toggle; events=%v", rec.names())
	}
	if got["diff"] != false {
		t.Errorf("first toggle records diff=false (new state); got %v", got["diff"])
	}
	tm = m
	tm, _ = tm.Update(keyV()) // false → true
	m = tm.(model)
	got, _ = rec.last("action.preview_diff_toggle")
	if got["diff"] != true {
		t.Errorf("second toggle records diff=true; got %v", got["diff"])
	}
}

// TestToggleDiffNoOpOutsideRepo pins FR9: outside a git repo `v` flips the flag but
// never produces a diff (no file is modified-vs-HEAD), and never crashes.
func TestToggleDiffNoOpOutsideRepo(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "f.txt", "hello\nworld\n")
	m := modelAt(t, dir, 100, 30)
	m.diffOn = true
	selectEntry(t, &m, "f.txt")
	if m.previewIsDiff {
		t.Fatalf("outside a repo nothing diffs")
	}
	var tm tea.Model = m
	tm, _ = tm.Update(keyV())
	m = tm.(model)
	if m.previewIsDiff {
		t.Errorf("v outside a repo must not produce a diff")
	}
}

// ----------------------------------------------------------------------------
// Parked live-refresh (T_FR11/D13)
// ----------------------------------------------------------------------------

// TestDiffLiveRefreshParked pins FR11/D13: while the cursor sits on a modified
// file showing its diff, an agent re-saving the SAME file (changing its size)
// makes the poll loop re-dispatch the diff against the new HEAD-delta — no
// navigate-away needed. The new hunk content appears in the pane.
func TestDiffLiveRefreshParked(t *testing.T) {
	repo := t.TempDir()
	gitExec(t, repo, "init")
	mustWrite(t, repo, "tracked.txt", "alpha\nbeta\ngamma\n")
	gitExec(t, repo, "add", ".")
	gitExec(t, repo, "commit", "-m", "init")
	mustWrite(t, repo, "tracked.txt", "alpha\nDELTA\ngamma\n") // first edit

	m := modelAt(t, repo, 120, 30)
	m.diffOn = true
	m.git.repoRoot = detectRepoRoot(m.root)
	m.gitRootPrefix = repoRelPrefix(m.git.repoRoot, m.root)
	state, _ := collectGitState(m.git.repoRoot, nil)
	var tm tea.Model = m
	tm, _ = tm.Update(gitRefreshedMsg{gen: m.gitGen, state: state})
	m = tm.(model)
	selectEntry(t, &m, "tracked.txt")
	m.renderNow()
	if !strings.Contains(ansi.Strip(strings.Join(m.preview, "\n")), "+DELTA") {
		t.Fatalf("setup: should show +DELTA before the second edit")
	}

	// Agent re-saves the SAME parked file with a DIFFERENT-SIZED change so the
	// dirSig byte-identity guard does NOT short-circuit (size differs). The poll-loop
	// tick goes through Update, whose tail reconcilePreview re-dispatches the diff as
	// a batched Cmd — drive that Cmd to completion (the running program's event loop
	// would), exactly as a parked live-refresh does without any navigation.
	mustWrite(t, repo, "tracked.txt", "alpha\nDELTA\ngamma\nEPSILON ADDED LINE\n")
	tm = m
	var cmd tea.Cmd
	tm, cmd = tm.Update(tickMsg{})
	m = tm.(model)
	if !m.previewIsDiff {
		t.Fatalf("after the parked re-save tick, the selection must still be on the diff path")
	}
	drainPreviewRender(t, &m, cmd)
	body := ansi.Strip(strings.Join(m.preview, "\n"))
	if !strings.Contains(body, "+EPSILON ADDED LINE") {
		t.Errorf("parked diff must live-refresh to the new edit without navigating away; got:\n%s", body)
	}
}

// drainPreviewRender runs the (possibly batched) Cmd a tick produced, finds the
// previewRenderedMsg it carries, and applies it — the synchronous stand-in for the
// Bubbletea event loop driving an async preview render to completion.
func drainPreviewRender(t *testing.T, m *model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	msg := cmd()
	applyAnyPreviewMsg(m, msg)
}

// applyAnyPreviewMsg unwraps a tea.BatchMsg (a slice of Cmds) or a single
// previewRenderedMsg and applies the render result, so a test can drive a render
// dispatched through Update's batched tail.
func applyAnyPreviewMsg(m *model, msg tea.Msg) {
	switch v := msg.(type) {
	case previewRenderedMsg:
		m.applyPreview(v)
	case tea.BatchMsg:
		for _, c := range v {
			if c == nil {
				continue
			}
			applyAnyPreviewMsg(m, c())
		}
	}
}

// --- small test helpers -----------------------------------------------------

func entryByName(t *testing.T, m model, name string) entry {
	t.Helper()
	for _, e := range m.entries {
		if e.name == name {
			return e
		}
	}
	t.Fatalf("entry %q not found in %v", name, entryNames(m))
	return entry{}
}

func mustWriteBytes(t *testing.T, dir, name string, b []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// fieldRecorder captures every Record call with its fields so a test can assert
// the exact event payload (not just a count). Active() stays false to keep the
// model on its production-off hot path.
type fieldRecorder struct {
	events []recEvent
}

type recEvent struct {
	name   string
	fields map[string]any
}

func (r *fieldRecorder) Record(name string, fields map[string]any) {
	r.events = append(r.events, recEvent{name: name, fields: fields})
}
func (r *fieldRecorder) Shutdown(_ time.Duration) {}
func (r *fieldRecorder) Active() bool             { return false }

func (r *fieldRecorder) last(name string) (map[string]any, bool) {
	for i := len(r.events) - 1; i >= 0; i-- {
		if r.events[i].name == name {
			return r.events[i].fields, true
		}
	}
	return nil, false
}

func (r *fieldRecorder) names() []string {
	out := make([]string, 0, len(r.events))
	for _, e := range r.events {
		out = append(out, e.name)
	}
	return out
}

// TestChangesPreviewOfDeletedFileShowsPlaceholder pins the deleted-row preview
// (changed-only-view residual): a git-deleted file is gone from the working tree, so
// reading it would surface a raw "open …: no such file" errno. Selecting it in the
// changes view must show a clean "(deleted)" placeholder instead.
func TestChangesPreviewOfDeletedFileShowsPlaceholder(t *testing.T) {
	m := diffModel(t, func(repo string) {
		if err := os.Remove(filepath.Join(repo, "tracked.txt")); err != nil {
			t.Fatal(err)
		}
	})
	// Enter the changes view so the deleted path appears as a selectable entry.
	var tm tea.Model = m
	tm, _ = tm.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	m = tm.(model)
	selectEntry(t, &m, "tracked.txt")
	got := ansi.Strip(strings.Join(m.preview, "\n"))
	if got != "(deleted)" {
		t.Errorf("preview of a git-deleted file must be the (deleted) placeholder; got %q", got)
	}
}

// TestChangesEntersFromPreviewFocus pins that `c` opens the changes view regardless of
// which pane is focused (changed-only-view residual: no focusList gate on km.Changes).
func TestChangesEntersFromPreviewFocus(t *testing.T) {
	m := diffModel(t, func(repo string) {
		mustWrite(t, repo, "tracked.txt", "alpha\nBETA\ngamma\n")
	})
	m.focusPane = focusPreview
	var tm tea.Model = m
	tm, _ = tm.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	m = tm.(model)
	if m.mode != modeChanges {
		t.Errorf("c from preview focus must enter the changes view; mode=%v", m.mode)
	}
}

// TestChangesViewCleanTreePreviewsNoChanges pins the clean-tree changes preview
// (changed-only-view residual): opening the changes view with nothing changed shows a
// "(no changes)" placeholder in the preview pane, not a stale or empty preview.
func TestChangesViewCleanTreePreviewsNoChanges(t *testing.T) {
	repo := t.TempDir()
	gitExec(t, repo, "init")
	mustWrite(t, repo, "tracked.txt", "alpha\n")
	gitExec(t, repo, "add", ".")
	gitExec(t, repo, "commit", "-m", "init")

	m := modelAt(t, repo, 120, 30)
	m.diffOn = true
	if m.git.repoRoot == "" {
		m.git.repoRoot = detectRepoRoot(m.root)
		m.gitRootPrefix = repoRelPrefix(m.git.repoRoot, m.root)
	}
	state, _ := collectGitState(m.git.repoRoot, nil)
	var tm tea.Model = m
	tm, _ = tm.Update(gitRefreshedMsg{gen: m.gitGen, state: state})
	tm, _ = tm.Update(tea.KeyPressMsg{Code: 'c', Text: "c"}) // open changes view on a clean tree
	m = tm.(model)
	got := ansi.Strip(strings.Join(m.preview, "\n"))
	if got != "(no changes)" {
		t.Errorf("changes view on a clean tree must preview (no changes); got %q", got)
	}
}
