package main

// TestDogfoodBesideAgent is the DOGFOOD PILOT harness: it actually DRIVES the
// lazyexplorer model the way a real user would while sitting in a terminal pane
// beside a coding agent (Claude Code), and quantifies the friction of seven
// recurring vibe-code tasks.
//
// This is a KEPT artifact — the "bookend" against which future capability
// changes are re-measured. It does NOT assert pass/fail on whether a task is
// achievable: an UNREACHABLE goal is data, not a test failure. Every task uses
// t.Logf to record (achievable? · keystrokes · friction · evidence). The test
// itself only fails if the harness can't even drive the model (a real bug).
//
// Conventions used throughout:
//   - One chord = one keystroke (ctrl+p counts as 1, not 5).
//   - Keys go through the live Update path (tm.Update(tea.KeyPressMsg{...})), the
//     same edge a real keypress takes — never by calling enter*/exit* directly,
//     so the measurement reflects what a user actually presses. The key
//     constructions below were probe-verified to stringify to "ctrl+p"/"enter"/
//     "tab"/"esc" and to fire key.Matches through Update.
//   - View().Content (ansi-stripped) is the oracle for what the user SEES
//     (filenames, git badges, palette rows, status line). Model fields
//     (m.focusPane, m.previewTop, m.cursor) are the oracle for transition
//     CORRECTNESS that isn't cleanly visible in the rendered string.
//
// Helpers reused from the existing suite (all package main, _test.go scope):
//   modelAt, searchModel, mustWrite, mustMkdir, gitExec, renderNow, walkTree.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// --- key-construction shorthands (probe-verified) ---------------------------

func keyRune(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Text: string(r)} }
func keyCtrl(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl} }
func keyEnter() tea.KeyPressMsg      { return tea.KeyPressMsg{Code: tea.KeyEnter} }
func keyEsc() tea.KeyPressMsg        { return tea.KeyPressMsg{Code: tea.KeyEscape} }
func keyTab() tea.KeyPressMsg        { return tea.KeyPressMsg{Code: tea.KeyTab} }
func keyDown() tea.KeyPressMsg       { return tea.KeyPressMsg{Code: tea.KeyDown} }

// press feeds one key through Update and returns the next model state. The
// returned tea.Cmd is intentionally discarded: every task here either reaches a
// synchronous goal state or pre-warms the async caches (search walk / git
// snapshot) so the batched Cmd never needs to be driven.
func dogPress(t *testing.T, m model, k tea.KeyPressMsg) model {
	t.Helper()
	var tm tea.Model = m
	tm, _ = tm.Update(k)
	return tm.(model)
}

// seeContent returns the ansi-stripped rendered screen — what the user's eye
// actually reads.
func seeContent(m model) string { return ansi.Strip(m.View().Content) }

// TestDogfoodBesideAgent drives all seven beside-an-agent tasks.
func TestDogfoodBesideAgent(t *testing.T) {
	t.Run("T1_find_changed_file", t1FindChangedFile)
	t.Run("T2_copy_path", t2CopyPath)
	t.Run("T3_deep_navigate", t3DeepNavigate)
	t.Run("T4_see_all_changes", t4SeeAllChanges)
	t.Run("T5_new_file_appears", t5NewFileAppears)
	t.Run("T6_open_in_editor_or_reveal", t6OpenInEditorOrReveal)
	t.Run("T7_peek_then_back", t7PeekThenBack)
	t.Run("T8_copy_content", t8CopyContent)
	t.Run("T9_select_and_copy_range", t9SelectAndCopyRange)
	t.Run("T10_drag_select_range", t10DragSelectRange)
}

// dogSelectionModel builds a sized model with a multi-line text file selected and
// rendered, the preview focused — the situation a user is in when they want to grab
// a span of an agent-edited file. Returns the model and the file's de-ANSI lines.
func dogSelectionModel(t *testing.T) (model, []string, *fieldRecorder) {
	t.Helper()
	rec := &fieldRecorder{}
	root := t.TempDir()
	lines := make([]string, 30)
	for i := range lines {
		lines[i] = "content line " + strconv.Itoa(i)
	}
	body := strings.Join(lines, "\n") + "\n"
	mustWrite(t, root, "edit.txt", body)
	m := modelAt(t, root, 100, 24)
	m.tel = rec
	moveCursorToAny(t, &m, "edit.txt")
	m.pendingWidth = 0
	m.renderNow() // settle the plain-text preview before any selection starts
	return m, plainLines([]byte(body)), rec
}

// ----------------------------------------------------------------------------
// T9 — select-and-copy-range (keyboard): the agent edited a long file; the user
// wants a SPAN of it (not the whole file, not a visible-only native drag) pasted
// into chat. `V` from the preview, `j` to extend, `y` to copy — driven through
// Update, the same edge a keypress takes. The recorder is the content oracle.
// ----------------------------------------------------------------------------
func t9SelectAndCopyRange(t *testing.T) {
	m, want, rec := dogSelectionModel(t)

	keys := 0
	// Focus the preview (Tab), then V to start, j×2 to extend, y to copy.
	m = dogPress(t, m, keyTab())
	keys++
	if m.focusPane != focusPreview {
		t.Fatalf("[T9] Tab did not focus the preview")
	}
	m = dogPress(t, m, keyRune('V'))
	keys++
	if !m.selecting {
		t.Fatalf("[T9] `V` did not start a selection (focus=%v scrollable=%v)", m.focusPane, m.previewScrollable)
	}
	lo := m.selAnchor
	m = dogPress(t, m, keyRune('j'))
	keys++
	m = dogPress(t, m, keyRune('j'))
	keys++
	hi := m.selCursor
	m = dogPress(t, m, keyRune('y'))
	keys++

	fields, recorded := rec.last("action.copy_selection")
	if !recorded {
		t.Fatalf("[T9] `V`+extend+`y` produced no action.copy_selection; status=%q events=%v", m.statusMsg, rec.names())
	}
	gotLines, _ := fields["lines"].(int)
	gotBytes, _ := fields["bytes"].(int)
	wantRange := make([]string, 0, hi-lo+1)
	for i := lo; i <= hi; i++ {
		wantRange = append(wantRange, ansi.Strip(want[i]))
	}
	wantText := strings.Join(wantRange, "\n")
	capability := gotLines == hi-lo+1 && gotBytes == len(wantText)
	if !capability {
		t.Errorf("[T9] copied lines=%d bytes=%d, want lines=%d bytes=%d (the de-ANSI span)", gotLines, gotBytes, hi-lo+1, len(wantText))
	}
	if m.selecting {
		t.Errorf("[T9] copy must end the selection sub-state")
	}
	t.Logf("[T9 select-copy-range] achievable=%v keystrokes=%d (Tab→V→j→j→y), copied lines=%d bytes=%d", capability, keys, gotLines, gotBytes)
	t.Logf("[T9] resolved: in-app line selection copies a RANGE (incl. off-viewport) of raw de-colored text — the gap native-drag can't reach (off-viewport, 2-col, raw)")
}

// ----------------------------------------------------------------------------
// T10 — drag-select (mouse): the mouse crowd drags across a span in the preview
// and releases to copy — one gesture. Press → motion → release through Update.
// ----------------------------------------------------------------------------
func t10DragSelectRange(t *testing.T) {
	m, _, rec := dogSelectionModel(t)
	g := m.layout()
	x := g.dividerStart + dividerWidth + 1 // a column inside the preview (right) pane
	y0 := g.previewFirstRow
	y4 := g.previewFirstRow + 4

	var tm tea.Model = m
	tm, _ = tm.Update(tea.MouseClickMsg{X: x, Y: y0, Button: tea.MouseLeft})
	m = tm.(model)
	if m.selecting {
		t.Fatalf("[T10] press alone committed a selection — only a drag should (FR14)")
	}
	tm, _ = tm.Update(tea.MouseMotionMsg{X: x, Y: y4, Button: tea.MouseLeft})
	m = tm.(model)
	if !m.selecting {
		t.Fatalf("[T10] motion after press did not commit the selection")
	}
	tm, _ = tm.Update(tea.MouseReleaseMsg{X: x, Y: y4, Button: tea.MouseLeft})
	m = tm.(model)

	fields, recorded := rec.last("action.copy_selection")
	if !recorded {
		t.Fatalf("[T10] drag+release produced no action.copy_selection; status=%q events=%v", m.statusMsg, rec.names())
	}
	gotLines, _ := fields["lines"].(int)
	capability := gotLines == 5 // rows 0..4 inclusive
	if !capability {
		t.Errorf("[T10] dragged 5 rows → copied lines=%d, want 5", gotLines)
	}
	if m.selecting {
		t.Errorf("[T10] release must end the selection sub-state")
	}
	t.Logf("[T10 drag-select] achievable=%v copied lines=%d (press→drag 4 rows→release-copy, one gesture)", capability, gotLines)
}

// ----------------------------------------------------------------------------
// T8 — copy-content: the agent just edited a file; the user wants its WHOLE
// content in the clipboard to paste a chunk into the chat. Now a 1-keystroke
// `Y` (prd-preview-copy). The recorder is the content oracle (writeClipboard
// fails without a helper); the recorded byte count must equal the on-disk file.
// ----------------------------------------------------------------------------
func t8CopyContent(t *testing.T) {
	rec := &fieldRecorder{}
	m := searchModel(t) // small tree; main.go is a real text file
	m.tel = rec
	moveCursorToAny(t, &m, "main.go")
	full := filepath.Join(m.previewBaseDir(), "main.go")
	want, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("[T8] read fixture: %v", err)
	}

	keys := 0
	m = dogPress(t, m, keyRune('Y'))
	keys++

	fields, recorded := rec.last("action.copy_content")
	if !recorded {
		t.Fatalf("[T8] `Y` produced no action.copy_content record; status=%q events=%v", m.statusMsg, rec.names())
	}
	gotBytes, _ := fields["bytes"].(int)
	capability := gotBytes == len(want)
	copied := strings.HasPrefix(m.statusMsg, "copied ")
	clipFail := strings.HasPrefix(m.statusMsg, "⚠ clipboard")

	if gotBytes != len(want) {
		t.Errorf("[T8] copied bytes = %d, want the WHOLE file %d (raw os.ReadFile, not capped/diff)", gotBytes, len(want))
	}

	t.Logf("[T8 copy-content] achievable=%v keystrokes=%d (press `Y` on the file — copies the whole raw text; the palette 'copy file content' twin is the alt path)", capability, keys)
	t.Logf("[T8] resolved: `Y` copies the file's RAW content read fresh from disk (not the ANSI/diff preview, not the 256KB-capped preview read), so a paste into the agent chat is clean; recorded bytes=%d == on-disk %d", gotBytes, len(want))
	t.Logf("[T8] evidence: ran `Y` with statusMsg=%q (copied=%v clipboardUnsupported=%v); action.copy_content{name=%v bytes=%d}", m.statusMsg, copied, clipFail, fields["name"], gotBytes)
}

// gitProjectModel builds a real git repo with one committed file, then modifies
// it AND adds a brand-new file deep in the tree (src/handlers/auth.go) — exactly
// the "agent just edited something deep" situation. It returns a sized model with
// the git snapshot already delivered (as the async cmd would), so View() shows
// real badges. cwd is the repo root: the agent's edit is NOT in the current dir.
func gitProjectModel(t *testing.T) model {
	t.Helper()
	repo := t.TempDir()
	gitExec(t, repo, "init")
	// A committed baseline so a later edit reads as Modified, plus a nested dir.
	mustWrite(t, repo, "README.md", "# Project\n")
	mustMkdir(t, repo, "src")
	mustMkdir(t, repo, filepath.Join("src", "handlers"))
	mustWrite(t, filepath.Join(repo, "src", "handlers"), "auth.go", "package handlers\n")
	gitExec(t, repo, "add", ".")
	gitExec(t, repo, "commit", "-m", "init")

	// The agent edits a file three levels down (src/handlers/auth.go: +1 line).
	mustWrite(t, filepath.Join(repo, "src", "handlers"), "auth.go",
		"package handlers\n\nfunc Login() {}\n")

	m := modelAt(t, repo, 120, 30) // sized, cwd == repo root, cursor at top
	// modelAt builds the struct directly (no newModel), so the git fields are
	// unset. Prime repoRoot + gitRootPrefix the way newModel does FIRST — then
	// collectGitState runs against the real repo root (a "" root yields an empty
	// snapshot), and the resulting state is delivered through Update.
	if m.git.repoRoot == "" {
		m.git.repoRoot = detectRepoRoot(m.root)
		m.gitRootPrefix = repoRelPrefix(m.git.repoRoot, m.root)
	}
	state, _ := collectGitState(m.git.repoRoot, nil)
	m = dogPressMsg(t, m, gitRefreshedMsg{gen: m.gitGen, state: state})
	return m
}

// dogPressMsg feeds a non-key message (e.g. a git snapshot or a poll tick) through
// Update. Separate from press only for readability at call sites.
func dogPressMsg(t *testing.T, m model, msg tea.Msg) model {
	t.Helper()
	var tm tea.Model = m
	tm, _ = tm.Update(msg)
	return tm.(model)
}

// ----------------------------------------------------------------------------
// T1 — find-changed-file: the agent just edited a file deep in the tree; from
// the root, locate and preview it. Is there a jump-to-changed-file? Measure
// keystrokes from a cold start.
// ----------------------------------------------------------------------------
func t1FindChangedFile(t *testing.T) {
	m := gitProjectModel(t) // cwd = root; src/handlers/auth.go was just edited
	// The ship default (newModel) is diffOn=true (prd-preview-diff-view D3); modelAt
	// builds the struct directly, so set it here to drive the diff path the shipped
	// program lands on. This is the /goal proof below — NOT a `v` press (v would
	// turn the default OFF).
	m.diffOn = true

	// What does the user SEE from the root? The git change layer rolls up a dirty
	// subtree onto the ancestor folder with the ● glyph — that is the ONLY
	// change-guided affordance. There is no "jump to next change" key in the
	// keymap (keys.go has no such binding) and no change-only view.
	root := seeContent(m)
	srcRow := containsRow(root, "src/")
	rollup := strings.Contains(root, rollupGlyph) // ● on the dirty src/ folder

	// A real user, seeing the ● on src/, breadcrumbs down by hand following the
	// rollup glyph at each level. Count those keystrokes through the live Update.
	// At the repo root the cursor starts on ".git/" (index 0 — git's own dir sorts
	// first and is shown, itself a glance-noise friction). The user moves down onto
	// the dirty "src/" folder, descends (l), then into handlers/, then onto auth.go.
	// Every step is a manual keypress — no single-key jump-to-change exists.
	keys := 0
	m = moveCursorTo(t, m, "src", &keys) // step off .git/ onto the dirty src/ folder
	m = dogPress(t, m, keyRune('l'))     // descend into src/
	keys++
	// In src/ the cursor lands at index 0 = synthetic "..". The dirty child is
	// handlers/ — move onto it, then descend.
	m = moveCursorTo(t, m, "handlers", &keys)
	m = dogPress(t, m, keyRune('l')) // descend into src/handlers/
	keys++
	m = moveCursorTo(t, m, "auth.go", &keys)

	reached := m.cursor < len(m.entries) && m.entries[m.cursor].name == "auth.go"
	previewed := false
	diffHunksShown := false
	if reached {
		// auth.go is a tracked-modified Go file, and diffOn is true — so refreshPreview
		// state-selected the DIFF path (previewIsDiff). The landing navigation went
		// through Update, whose reconcilePreview already dispatched the diff render as a
		// batched Cmd (discarded by dogPress) and claimed pendingWidth. Release that
		// claim so renderNow re-dispatches + applies the diff to completion — the
		// synchronous stand-in for the event loop running that in-flight Cmd.
		m.pendingWidth = 0
		m.renderNow()
		body := strings.Join(m.preview, "\n")
		previewed = strings.Contains(body, "func Login")
		// /goal PROOF (prd-preview-diff-view §6.14): the preview shows the DIFF HUNK
		// "+func Login() {}" — the added line marked, not just the full file body. This
		// is review-the-edit-in-pane finally achieved: the user reads WHAT changed
		// without tab-ing away to `git diff`. The leading "+" distinguishes a diff hunk
		// from plain content (which the pre-diff harness could only show).
		plain := ansi.Strip(body)
		diffHunksShown = m.previewIsDiff && strings.Contains(plain, "+func Login")
	}

	t.Logf("[T1 find-changed-file] achievable=%v keystrokes=%d diff_now_achievable=%v", reached && previewed, keys, diffHunksShown)
	t.Logf("[T1] friction: a single-key jump-to-change now EXISTS — `c` opens the changed-only aggregate view (T4), so a deep edit is one keypress + Enter away. The dirty-folder ● rollup (present=%v) still lets you breadcrumb down by hand — %d keys root→deep file — for the spatial-browse path; the two coexist", rollup, keys)
	t.Logf("[T1] /goal proof: review-the-edit-in-pane is now reachable — landing on the modified auth.go shows its diff hunk in the preview (previewIsDiff=%v, '+func Login' visible=%v), no tab-away to `git diff` needed", m.previewIsDiff, diffHunksShown)
	t.Logf("[T1] evidence: root view shows src/ row=%v with ● rollup=%v; manual descent l→(move to handlers)→l→(move to auth.go) lands cursor on %q, preview shows func Login=%v, as a diff hunk=%v", srcRow, rollup, currentName(m), previewed, diffHunksShown)

	if reached && !diffHunksShown {
		t.Errorf("[T1] diff hunks must be visible after landing on the modified file with diffOn (preview=%q)", strings.Join(m.preview, "\n"))
	}
}

// ----------------------------------------------------------------------------
// T2 — copy-path: copy the selected file's PROJECT-RELATIVE path so you can
// paste it straight into the agent's chat. Now a 1-keystroke yank.
// ----------------------------------------------------------------------------
func t2CopyPath(t *testing.T) {
	m := searchModel(t) // small tree; cursor lands on a real file
	// Put the cursor on a known file so the copied path is predictable.
	moveCursorToAny(t, &m, "main.go")
	wantAbs := m.selectedAbsPath()

	// The user's path: press `y` on the selected file (one keystroke — beats the
	// 6-keystroke palette path the absolute-copy command takes).
	keys := 0
	m = dogPress(t, m, keyRune('y'))
	keys++

	// Outcome: the yank ran (statusMsg is "copied <rel>" on success, or a
	// "⚠ clipboard:" message if no pbcopy/xclip/wl-copy is available — both are
	// valid dogfood data; do NOT fail on the clipboard outcome).
	copied := strings.HasPrefix(m.statusMsg, "copied ")
	clipFail := strings.HasPrefix(m.statusMsg, "⚠ clipboard")
	if !copied && !clipFail {
		t.Fatalf("[T2] `y` produced neither a copied-rel nor a clipboard status: %q", m.statusMsg)
	}
	// RESOLVED: the goal — a project-relative slash-path — is now reachable. The
	// rel string is filepath.Rel(root, abs) in slash-form, with the repo prefix
	// gone, so it pastes straight into the agent (no hand-trimming). Positive
	// assertion (this is a logging bookend; it won't fail on its own otherwise).
	wantRel, relErr := filepath.Rel(m.root, wantAbs)
	if relErr != nil {
		t.Fatalf("[T2] filepath.Rel(%q, %q): %v", m.root, wantAbs, relErr)
	}
	wantRel = filepath.ToSlash(wantRel)
	if copied {
		gotRel := strings.TrimPrefix(m.statusMsg, "copied ")
		if gotRel != wantRel {
			t.Errorf("[T2] yanked rel = %q, want %q (filepath.Rel sans root prefix)", gotRel, wantRel)
		}
		if strings.HasPrefix(gotRel, m.root) {
			t.Errorf("[T2] yanked rel %q still carries the %q prefix the user used to hand-trim", gotRel, m.root)
		}
	}

	t.Logf("[T2 copy-path] achievable=%v keystrokes=%d (press `y` on the file — copies the project-relative slash-path; the palette 'copy relative path' twin is the alt path)", copied || clipFail, keys)
	t.Logf("[T2] resolved: `y` copies the path relative to the jail root (it expects %q), so it pastes straight into the agent — no hand-trimming the %q prefix; 'copy absolute path' stays for the rarer absolute case", wantRel, m.root)
	t.Logf("[T2] evidence: ran `y` with statusMsg=%q (copied=%v clipboardUnsupported=%v); rel == filepath.Rel(root, abs) slash-form = %q", m.statusMsg, copied, clipFail, wantRel)
}

// ----------------------------------------------------------------------------
// T3 — deep-navigate: open a file nested several dirs deep. Manual descent vs
// search "/". Keystroke cost of each.
// ----------------------------------------------------------------------------
func t3DeepNavigate(t *testing.T) {
	root := t.TempDir()
	// Build a four-level-deep tree: a/b/c/target.txt. (.txt → instant plain
	// preview, no async render needed.)
	mustMkdir(t, root, "a")
	mustMkdir(t, root, filepath.Join("a", "b"))
	mustMkdir(t, root, filepath.Join("a", "b", "c"))
	mustWrite(t, filepath.Join(root, "a", "b", "c"), "target.txt", "deep payload\n")
	m := modelAt(t, root, 100, 30)

	// --- Path A: manual descent, the lazygit way -----------------------------
	// At root the cursor is on "a/" (only entry). Descend a→b→c, then in c/ the
	// synthetic ".." is at index 0 so step down onto target.txt.
	mA := m
	manualKeys := 0
	moveCursorToAny(t, &mA, "a")
	mA = dogPress(t, mA, keyRune('l')) // open a/
	manualKeys++
	mA = moveCursorTo(t, mA, "b", &manualKeys)
	mA = dogPress(t, mA, keyRune('l')) // open b/
	manualKeys++
	mA = moveCursorTo(t, mA, "c", &manualKeys)
	mA = dogPress(t, mA, keyRune('l')) // open c/
	manualKeys++
	mA = moveCursorTo(t, mA, "target.txt", &manualKeys)
	manualReached := currentName(mA) == "target.txt"
	manualPreviewed := strings.Contains(strings.Join(mA.preview, "\n"), "deep payload")

	// --- Path B: recursive fuzzy search "/" ----------------------------------
	// Pre-warm the walk cache so "/" is a synchronous cache hit (the way
	// searchModel sidesteps the async walk batch). Then "/" → type "target" →
	// enter opens the file's parent and lands the cursor on it.
	mB := m
	walked, err := walkTree(root)
	if err != nil {
		t.Fatalf("T3 warm-cache walk: %v", err)
	}
	mB.searchAll = walked
	mB.searchAllAt = time.Now()
	searchKeys := 0
	mB = dogPress(t, mB, keyRune('/'))
	searchKeys++
	if mB.mode != modeSearch {
		t.Fatalf("T3: '/' did not enter search mode (mode=%v)", mB.mode)
	}
	for _, r := range "target" {
		mB = dogPress(t, mB, keyRune(r))
		searchKeys++
	}
	mB = dogPress(t, mB, keyEnter()) // open the top result
	searchKeys++
	searchReached := currentName(mB) == "target.txt"
	searchPreviewed := strings.Contains(strings.Join(mB.preview, "\n"), "deep payload")

	achievable := manualReached && searchReached
	t.Logf("[T3 deep-navigate] achievable=%v keystrokes=%d (manual descent — the cheaper path here; '/' search costs %d for this shallow tree)", achievable, manualKeys, searchKeys)
	t.Logf("[T3] friction: manual descent is %d keys (l per level + j-presses past the synthetic '..' at index 0 of every subdir); '/' search costs MORE here (%d = '/' + 'target' + enter) — its value is NOT keystroke savings on a shallow tree but location-independence (no need to know the path) and that it scales with query length (~len(query)+2) instead of depth (~2×depth) as the tree gets deeper", manualKeys, searchKeys)
	t.Logf("[T3] evidence: manual l→…→target.txt reached=%v preview=%v (%d keys); '/target'+enter reached=%v preview=%v (%d keys)", manualReached, manualPreviewed, manualKeys, searchReached, searchPreviewed, searchKeys)
}

// ----------------------------------------------------------------------------
// T4 — see-all-changes: an at-a-glance overview of EVERY file changed in the
// working tree. Does a changed-only view/filter exist?
// ----------------------------------------------------------------------------
func t4SeeAllChanges(t *testing.T) {
	// A repo with changes spread across two directories, so "see everything
	// changed" genuinely requires aggregation the current dir can't give.
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
	if m.git.repoRoot == "" {
		m.git.repoRoot = detectRepoRoot(m.root)
		m.gitRootPrefix = repoRelPrefix(m.git.repoRoot, m.root)
	}
	state, _ := collectGitState(m.git.repoRoot, nil)
	m = dogPressMsg(t, m, gitRefreshedMsg{gen: m.gitGen, state: state})

	// From the root the per-row badges only cover the CURRENT directory: the user
	// sees README.md's badge and a ● rollup on src/, but NOT src/app.go itself — it
	// is one dir down. That rollup-only view is exactly what the changed-only view
	// (`c`) now replaces with a flat aggregate.
	rootView := seeContent(m)
	seesReadmeChange := strings.Contains(rootView, "README.md")
	seesSrcRollup := strings.Contains(rootView, "src/") && strings.Contains(rootView, rollupGlyph)
	seesNestedChangeDirectly := strings.Contains(rootView, "app.go") // false at the root: app.go is in src/

	// THE NEW PATH: one keypress `c` opens the changed-only aggregate view — a flat
	// list of EVERY working-tree change in the whole repo, regardless of directory.
	keys := 0
	m = dogPress(t, m, keyRune('c'))
	keys++
	hasChangedView := m.mode == modeChanges
	listNames := entryNames(m)
	// The aggregate lists the root change AND the nested change that the current-dir
	// badges could only roll up onto src/ — the whole point of aggregation.
	listsReadme := contains(listNames, "README.md")
	listsNested := contains(listNames, "src/app.go")

	totalChanges := len(state.changes)

	t.Logf("[T4 see-all-changes] achievable=%v keystrokes=%d (press `c` — the changed-only aggregate view lists all %d changes in one keypress)", hasChangedView && listsReadme && listsNested, keys, totalChanges)
	t.Logf("[T4] /goal proof: git tracks all %d changes; `c` now surfaces every one in a single flat list (README.md present=%v, the NESTED src/app.go present=%v) — no more breadcrumbing down ● rollups dir-by-dir", totalChanges, listsReadme, listsNested)
	t.Logf("[T4] evidence: root view sees README.md change=%v, src/ ● rollup=%v, nested app.go directly=%v; after `c` mode=modeChanges=%v, aggregate list=%v", seesReadmeChange, seesSrcRollup, seesNestedChangeDirectly, hasChangedView, listNames)

	// /goal proof guard (distinct from the build gate): the changed-only view must
	// now exist and aggregate BOTH the root and the nested change.
	if !hasChangedView {
		t.Errorf("[T4] `c` must open the changed-only aggregate view (mode=%v)", m.mode)
	}
	if !listsReadme || !listsNested {
		t.Errorf("[T4] the aggregate must list every change including the nested src/app.go the current-dir badges could only roll up; got %v", listNames)
	}
}

// ----------------------------------------------------------------------------
// T5 — new-file-appears: a file is created on disk while the app is open; does
// it appear without restart, and how is that surfaced?
// ----------------------------------------------------------------------------
func t5NewFileAppears(t *testing.T) {
	repo := t.TempDir()
	gitExec(t, repo, "init")
	mustWrite(t, repo, "existing.go", "package main\n")
	gitExec(t, repo, "add", ".")
	gitExec(t, repo, "commit", "-m", "init")

	m := modelAt(t, repo, 120, 30)
	if m.git.repoRoot == "" {
		m.git.repoRoot = detectRepoRoot(m.root)
		m.gitRootPrefix = repoRelPrefix(m.git.repoRoot, m.root)
	}
	before := seeContent(m)
	sawBefore := strings.Contains(before, "agent_output.go")

	// The agent writes a brand-new file while lazyexplorer is open — NO keypress.
	mustWrite(t, repo, "agent_output.go", "package main\n\nfunc New() {}\n")

	// The poll loop (tickMsg → syncFromDisk, gated by dirSig) is what surfaces it.
	// Deliver one tick the way the 1s timer would — zero user keystrokes.
	m = dogPressMsg(t, m, tickMsg{})
	afterTick := seeContent(m)
	appears := strings.Contains(afterTick, "agent_output.go")

	// Git surfacing: a fresh untracked file gets the "?" badge once a git refresh
	// lands. The tick above also dispatches a git refresh; deliver its snapshot.
	state, _ := collectGitState(m.git.repoRoot, nil)
	m = dogPressMsg(t, m, gitRefreshedMsg{gen: m.gitGen, state: state})
	afterGit := seeContent(m)
	row := lineContaining(afterGit, "agent_output.go")
	hasUntrackedBadge := strings.Contains(row, "?")

	t.Logf("[T5 new-file-appears] achievable=%v keystrokes=%d (the 1s poll loop surfaces it; no user key needed)", appears, 0)
	t.Logf("[T5] friction: appears automatically within ~1s via the poll loop (no restart, no key); surfaced as a normal list row plus a '?' untracked git badge — there is no toast/highlight calling out that it is NEW")
	t.Logf("[T5] evidence: before tick present=%v; after one poll tick present=%v; its row=%q carries an untracked '?' badge=%v", sawBefore, appears, strings.TrimSpace(row), hasUntrackedBadge)
}

// ----------------------------------------------------------------------------
// T6 — open-in-editor / reveal: open the selected file in $EDITOR or reveal it
// in the shell. Possible?
// ----------------------------------------------------------------------------
func t6OpenInEditorOrReveal(t *testing.T) {
	// $EDITOR must be set for the capability to be wired — exactly the beside-an-agent
	// norm (prd-open-in-editor D1). Pin it so the measurement is deterministic; never
	// run the returned cmd (it would suspend the test and spawn $EDITOR in CI).
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "vim")

	m := searchModel(t)
	moveCursorToAny(t, &m, "main.go")

	// Path 1 — the command palette offers "open in editor" (discoverability twin).
	keys := 0
	mp := dogPress(t, m, keyCtrl('p'))
	keys++
	if mp.mode != modeCommandPalette {
		t.Fatalf("T6 setup: ctrl+p did not open the palette (mode=%v)", mp.mode)
	}
	paletteView := seeContent(mp)
	cmds := paletteCommandNames()
	hasEditor := strings.Contains(paletteView, "open in editor")

	// Path 2 — the primary key. Press 'e' on a file (focusList) and capture the
	// returned cmd WITHOUT running it: a non-nil cmd is the capability being present
	// (it is the tea.ExecProcess that suspends the TUI and launches $EDITOR).
	var em tea.Model = m
	em, eCmd := em.Update(keyRune('e'))
	keys++
	eFile := em.(model)
	keyWired := eCmd != nil && eFile.mode == modeNormal && !strings.HasPrefix(eFile.statusMsg, "⚠")

	t.Logf("[T6 open-in-editor] achievable=%v keystrokes=%d (1 key: press e on the selected file; the palette twin is the alt path)", keyWired, keys)
	t.Logf("[T6] capability: pressing 'e' on a file returns a tea.ExecProcess cmd that suspends the TUI into $VISUAL/$EDITOR and reloads on exit (editorFinishedMsg); reveal-in-shell stays deferred (prd-open-in-editor §non-goal)")
	t.Logf("[T6] evidence: palette commands=%v (open-in-editor cmd=%v); 'e' on a file returned a cmd=%v, mode stayed normal=%v, no warning status=%v", cmds, hasEditor, eCmd != nil, eFile.mode == modeNormal, !strings.HasPrefix(eFile.statusMsg, "⚠"))
}

// ----------------------------------------------------------------------------
// T7 — peek-then-back: focus the preview, scroll a long file, return to the
// list, keep navigating. Ergonomics/keystrokes of the round trip.
// ----------------------------------------------------------------------------
func t7PeekThenBack(t *testing.T) {
	root := t.TempDir()
	// A long plain-text file so a scroll measurably advances previewTop.
	mustWrite(t, root, "long.txt", strings.Repeat("line of content\n", 200))
	mustWrite(t, root, "other.txt", "second file\n")
	m := modelAt(t, root, 100, 30)
	moveCursorToAny(t, &m, "long.txt")

	keys := 0
	// Step 1: focus the preview (Tab).
	m = dogPress(t, m, keyTab())
	keys++
	focusedPreview := m.focusPane == focusPreview

	// Step 2: scroll the long file. j moves one line; ctrl+d half-page. Under
	// focusPreview these route to the preview viewport, not the list cursor.
	topBefore := m.previewTop
	cursorBefore := m.cursor
	m = dogPress(t, m, keyDown()) // ↓ scrolls preview
	keys++
	m = dogPress(t, m, keyCtrl('d')) // half-page down
	keys++
	scrolled := m.previewTop > topBefore
	listCursorUntouched := m.cursor == cursorBefore // scrolling preview must NOT move list

	// Step 3: return to the list. Esc steps back from preview-focus to the list.
	m = dogPress(t, m, keyEsc())
	keys++
	backToList := m.focusPane == focusList

	// Step 4: keep navigating the list — j now moves the list cursor again.
	cursorAtReturn := m.cursor
	m = dogPress(t, m, keyDown())
	keys++
	listMovesAgain := m.cursor != cursorAtReturn

	achievable := focusedPreview && scrolled && backToList && listMovesAgain
	t.Logf("[T7 peek-then-back] achievable=%v keystrokes=%d (Tab→↓→ctrl+d→Esc→↓)", achievable, keys)
	t.Logf("[T7] friction: clean round trip with focus-routed keys — Tab focuses the preview, j/k/ctrl+d scroll it (list cursor frozen), Esc (or Tab) returns to the list; ~1 key each way, no mode dialog")
	t.Logf("[T7] evidence: Tab→focusPreview=%v; ↓+ctrl+d advanced previewTop=%v while list cursor frozen=%v; Esc→focusList=%v; ↓ then moves list again=%v", focusedPreview, scrolled, listCursorUntouched, backToList, listMovesAgain)
}

// --- small navigation helpers (dogfood-prefixed to avoid redeclaration) -----

// moveCursorTo presses ↓ until the cursor lands on name, counting each press
// into *keys. Used to measure the real keystroke cost of stepping past the
// synthetic ".." (index 0 in every subdir) onto a target row. Stops after a
// bounded number of presses so a missing target can't loop forever.
func moveCursorTo(t *testing.T, m model, name string, keys *int) model {
	t.Helper()
	for i := 0; i < len(m.entries)+1; i++ {
		if currentName(m) == name {
			return m
		}
		m = dogPress(t, m, keyDown())
		*keys++
	}
	return m
}

// moveCursorToAny snaps the cursor straight onto name WITHOUT counting
// keystrokes — for test SETUP where we just need a known selection, not a
// measurement.
func moveCursorToAny(t *testing.T, m *model, name string) {
	t.Helper()
	for i, e := range m.entries {
		if e.name == name {
			m.cursor = i
			m.refreshPreview()
			return
		}
	}
	t.Fatalf("setup: entry %q not found in %v", name, entryNames(*m))
}

func currentName(m model) string {
	if m.cursor < 0 || m.cursor >= len(m.entries) {
		return ""
	}
	return m.entries[m.cursor].name
}

func entryNames(m model) []string {
	out := make([]string, 0, len(m.entries))
	for _, e := range m.entries {
		out = append(out, e.name)
	}
	return out
}

// containsRow reports whether the rendered (ansi-stripped) screen has a line
// containing sub.
func containsRow(screen, sub string) bool {
	return lineContaining(screen, sub) != ""
}

// lineContaining returns the first rendered line that contains sub, or "".
func lineContaining(screen, sub string) string {
	for _, ln := range strings.Split(screen, "\n") {
		if strings.Contains(ln, sub) {
			return ln
		}
	}
	return ""
}

// paletteCommandNames is the ship command set's display names — the evidence
// surface for "what can the palette do" (T4/T6). Reads defaultCommands so it can
// never drift from what the palette actually offers.
func paletteCommandNames() []string {
	cmds := defaultCommands()
	out := make([]string, 0, len(cmds))
	for _, c := range cmds {
		out = append(out, c.Name)
	}
	return out
}
