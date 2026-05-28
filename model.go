package main

import (
	"os"
	"path/filepath"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// pollInterval is how often the poll loop re-checks cwd for external changes
// (files an agent adds/edits/removes beside us). One second is responsive
// enough for a glance-beside-the-agent companion and dirt cheap: a steady-state
// tick is one os.ReadDir gated by dirSig (see syncFromDisk).
const pollInterval = time.Second

// previewLineStep is how many lines the preview pane moves per wheel notch.
// One line per notch reads as "smooth" — the mapping is 1-1 with the input
// the OS already aggregates, so multiplying it is gratuitous extra travel.
// Half-page jumps for ctrl+d/u are computed per call from bodyH (see
// prd-smooth-preview-scroll §5.1, D1); fine-step J/K is folded into the
// focus-aware route in prd-pane-focus.
const previewLineStep = 1

// previewColStep is how many columns h/l pan the preview per press — the
// horizontal mirror of previewLineStep. H/L jump half the pane width
// (prd-horizontal-scroll-preview D5/D6).
const previewColStep = 1

// tickMsg drives the poll loop. tickCmd schedules the next one; Init kicks off
// the first and every tickMsg reschedules, so the loop self-sustains.
type tickMsg struct{}

func tickCmd() tea.Cmd {
	return tea.Tick(pollInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

// spinnerInterval is how fast the footer render spinner advances. ~100ms reads as
// a smooth spin without flooding the Update loop; the loop runs ONLY while a
// preview render is in flight (see spinnerTickCmd dispatch in syncPreview), so an
// idle glance UI is never woken at this rate.
const spinnerInterval = 100 * time.Millisecond

// spinnerTickMsg advances the footer render spinner. Unlike tickMsg it does NOT
// self-sustain unconditionally: the Update handler reschedules only while
// pendingWidth > 0, so the loop dies the first tick after the render lands.
type spinnerTickMsg struct{}

func spinnerTickCmd() tea.Cmd {
	return tea.Tick(spinnerInterval, func(time.Time) tea.Msg { return spinnerTickMsg{} })
}

// previewRenderedMsg carries the result of an async renderer (glamour/chroma/…)
// back to the Update loop. gen identifies which dispatch produced it (stale
// results are dropped); width is the body width it was rendered at; lines are the
// output (valid only when err is nil); preStyled reports whether those lines
// carry verbatim ANSI / are pre-fit to width (renderPreview then skips fitWidth).
type previewRenderedMsg struct {
	gen       uint64
	width     int
	lines     []string
	preStyled bool
	err       error
}

type mode int

const (
	modeNormal mode = iota
	modeConfirmDelete
	modeRename
	modeSearch
	modeCommandPalette // ctrl+p — pick/run a command (prd-keymap-and-command-palette)
	modeHelp           // ? — full-help overlay
)

// walkCacheTTL is how long a recursive walk stays fresh enough to reuse without
// re-walking (PRD D8). Re-activating search within this window skips the walk
// entirely — long enough that a search → Enter → glance-back cycle reuses the
// cache, short enough that a stale tree never lingers. A walk of a 5k-file repo
// is ~30-80ms, too small to justify an fsnotify watcher (deferred, §5.11).
const walkCacheTTL = 30 * time.Second

// searchWalkedMsg carries an async recursive walk back to the Update loop. gen
// identifies which enterSearch dispatched it; a stale walk (the user already
// Esc'd and re-entered, bumping searchGen) is dropped so its results never
// clobber a newer walk's — the same gen-counter discipline as previewRenderedMsg.
type searchWalkedMsg struct {
	gen     uint64
	results []entry
	err     error
}

// walkTreeCmd runs walkTree off the Update goroutine (a recursive walk of a
// large tree is too slow to block every keystroke) and returns the result tagged
// with gen so Update can discard a stale walk.
func walkTreeCmd(root string, gen uint64) tea.Cmd {
	return func() tea.Msg {
		out, err := walkTree(root)
		return searchWalkedMsg{gen: gen, results: out, err: err}
	}
}

// focusPane is which of the two panes keyboard navigation acts on. It is a
// sub-state of modeNormal — orthogonal to mode (which owns the rename/delete
// prompts) — so the "scroll-ish" keys (up/down/j/k/g/G/ctrl+d/u) and a
// left-click can route to either pane while the mode machinery stays untouched.
// Zero value = focusList, so a freshly-built model starts on the list (D2): the
// user picks a file there, the preview follows.
type focusPane int

const (
	focusList focusPane = iota // zero value — default on launch
	focusPreview
)

type model struct {
	root string // jail root — cwd may equal or descend from this, never above
	cwd  string

	entries []entry
	cursor  int
	listTop int // scroll offset of the left list

	fsSig uint64 // dirSig of the current listing; the poll loop diffs against it

	preview    []string
	previewTop int // scroll offset of the right panel

	// Folder-preview state. previewIsDir distinguishes a folder listing (drawn
	// row-by-row via renderEntryRow at view time, FR1) from a file/markdown
	// preview (drawn from m.preview). It must default OFF — renderPreview and
	// previewClick both branch on it, so a stale "true" after navigating to a
	// file would mis-route them. previewEntries is the data; nil-vs-empty is
	// the empty-folder placeholder signal.
	previewEntries []entry
	previewIsDir   bool

	// Horizontal scroll + wrap (plain text + code only — prd-horizontal-scroll-preview).
	// previewScrollable is true when the preview can pan horizontally (plain/code);
	// false for markdown (glamour already wrapped), images, folders. previewHScroll
	// is the column offset in nowrap mode; previewWrap toggles soft-wrap (a session
	// preference, NOT reset per file). The reflow cache (previewDisplay + key fields)
	// holds the visual lines the vertical scroller iterates: in wrap mode each source
	// line expands to ≥1 wrapped lines, in nowrap it equals m.preview. previewSrcStart
	// maps a source line to its first visual line (wrap-toggle position preservation);
	// previewMaxLineWidth is the widest source line (nowrap hscroll clamp).
	previewScrollable   bool
	previewHScroll      int
	previewWrap         bool
	previewDisplay      []string
	previewDisplayW     int
	previewDisplayWrap  bool
	previewMaxLineWidth int
	previewSrcStart     []int

	previewPreStyled bool   // preview lines carry verbatim ANSI (markdown/code) → renderPreview skips fitWidth
	srcPath          string // abs path of the selected renderable file; "" = selection has no preview renderer
	srcRaw           []byte // its content (normalized text for text renderers; raw bytes otherwise)
	srcWidth         int    // body width m.preview was rendered at (cache key); 0 = not yet rendered
	renderStyle      string // app-level style hint resolved once at startup ("dark"/"light"/"notty"); "" → auto

	// Async preview render bookkeeping. A renderer (glamour/chroma/…) is too slow
	// to run on the Update goroutine (a large file would freeze every keystroke and
	// the poll loop), so rendering is dispatched as a tea.Cmd and the result returns
	// as a previewRenderedMsg. renderGen tags each dispatch; a result is applied
	// only if its gen still matches, which discards a stale render that lands after
	// the user has already navigated on. pendingWidth is the body width of the
	// in-flight render (0 = none), so syncPreview doesn't re-dispatch the same work
	// every Update tick — and drives the "rendering…" chip.
	renderGen    uint64
	pendingWidth int

	// Footer render spinner. spinnerFrame is the current braille frame, cycled
	// while a preview render is in flight; spinning guards the tick loop so a
	// rapid file switch (which re-dispatches before the prior render lands) never
	// spawns a second loop. Both reset when the render completes.
	spinnerFrame int
	spinning     bool

	mode      mode
	focusPane focusPane // sub-state of modeNormal; orthogonal to mode prompts (D1)
	input     string    // buffer for rename
	statusMsg string

	leftRatio float64 // left panel width as a fraction of total (2-col mode); drag-adjustable along X
	topRatio  float64 // top panel height as a fraction of total body rows (1-col stacked mode); drag-adjustable along Y, mirror of leftRatio
	dragging  bool    // true while the user is dragging the panel divider (either axis)

	// lastVertical caches the previous layout()'s orientation (m.width <
	// widthBreakpoint) for one purpose only: detecting a mode flip during
	// Update's WindowSizeMsg case so an in-flight drag can be cancelled
	// before the axis changes under the user's finger. NOT a hysteresis
	// state — single threshold at v1 (D6, PRD §5.11).
	lastVertical bool

	// Recursive fuzzy search (PRD §5.2). modeSearch re-purposes m.entries/cursor/
	// listTop as the result list, so the pre-search values are snapshotted below
	// and restored on Esc (FR10). searchAll is the walked tree (flat, names are
	// relPaths) that filterSearch ranks over; searchAllAt + walkCacheTTL gate
	// re-walking; searchIndexing drives the status-bar chip while a walk runs;
	// searchGen tags each async walk so a stale one (rapid /→Esc→/) is dropped
	// — same discipline as renderGen for previews.
	searchQuery    string
	searchAll      []entry
	searchAllAt    time.Time
	searchIndexing bool
	searchGen      uint64

	searchSavedCwd     string
	searchSavedEntries []entry
	searchSavedFsSig   uint64
	searchSavedCursor  int
	searchSavedListTop int

	// Key bindings — the single source of truth for key codes + help text
	// (prd-keymap-and-command-palette). Set once in newModel; updateNormal and the
	// help/status renderers match against it instead of literal key strings.
	keymap KeyMap

	// Command palette state (modeCommandPalette). paletteStage 0 = pick a command
	// (filter via paletteQuery over paletteFiltered, cursor = paletteCursor);
	// stage 1 = collect a text argument (paletteSecondaryInput, only `cd` uses it).
	// All reset to zero by exitCommandPalette.
	paletteStage          int
	paletteQuery          string
	paletteSecondaryInput string
	paletteCursor         int
	paletteFiltered       []Command

	// Help overlay state (modeHelp): helpTop is the scroll offset into the
	// rendered help body (fullHelp groups). Reset on enter/exit.
	helpTop int

	width  int
	height int

	// Telemetry boundary (PRD §5.2). tel is the only telemetry surface the
	// model touches — Update / refreshPreview / applyPreview / syncPreview
	// call tel.Record(name, fields); the recorder owns all I/O. Default is
	// noopRecorder when telemetry is off, so the call site stays uniform.
	// renderStartedAt holds the time syncPreview dispatched the in-flight
	// render — applyPreview reads it to compute action.preview_rendered's
	// duration_ms (FR10/§5.3). syncPreview only sets it when tel.Active() so
	// the no-op path skips the time.Now syscall.
	tel             Recorder
	renderStartedAt time.Time
}

func newModel(root string, tel Recorder) model {
	if tel == nil {
		tel = noopRecorder{}
	}
	m := model{root: root, cwd: root, leftRatio: 0.38, topRatio: 0.33, keymap: defaultKeyMap(), tel: tel}
	m.reload()
	return m
}

// reload re-reads m.cwd into entries and refreshes the preview for the cursor.
func (m *model) reload() {
	entries, err := readDir(m.cwd)
	if err != nil {
		m.statusMsg = "⚠ " + err.Error()
		m.entries = nil
		m.preview = nil
		m.fsSig = 0
		return
	}
	m.fsSig = dirSig(entries) // baseline for the poll loop, taken from the real entries
	// Inject a synthetic ".." at the top so the user can navigate (and click) up,
	// except at the jail root where ascending is forbidden. It is not a real
	// filesystem entry: descend() routes it to ascend(), and rename/delete refuse it.
	if m.cwd != m.root {
		entries = append([]entry{{name: "..", isDir: true}}, entries...)
	}
	m.entries = entries
	if m.cursor >= len(m.entries) {
		m.cursor = max(0, len(m.entries)-1)
	}
	m.refreshPreview()
}

// syncFromDisk is the poll loop's refresh. It re-reads cwd and, only when the
// listing actually changed (dirSig gate), rebuilds the view — preserving the
// user's selection by NAME and the preview scroll offset. The gate is what keeps
// a steady-state poll from re-reading the preview and re-rendering markdown every
// tick: an unchanged directory costs one readDir and returns early.
func (m *model) syncFromDisk() {
	if _, err := os.Stat(m.cwd); err != nil {
		m.recoverVanishedCwd() // cwd was deleted out from under us
		return
	}

	entries, err := readDir(m.cwd)
	if err != nil {
		m.statusMsg = "⚠ " + err.Error()
		m.entries = nil
		m.preview = nil
		m.fsSig = 0
		return
	}
	sig := dirSig(entries)
	if sig == m.fsSig {
		return // nothing changed on disk
	}
	m.fsSig = sig

	// Snapshot the selected entry (by value) before the swap. dirSig fired because
	// SOMETHING in cwd changed, but the preview depends only on the selected file —
	// comparing this snapshot against the post-swap selection tells us whether that
	// one file changed, distinct from "a sibling changed" (bug-poll-preview-rerender).
	var oldSel entry
	hadSel := m.cursor < len(m.entries)
	if hadSel {
		oldSel = m.entries[m.cursor]
	}
	prevTop := m.previewTop

	if m.cwd != m.root {
		entries = append([]entry{{name: "..", isDir: true}}, entries...)
	}
	m.entries = entries

	// Keep the cursor on the same name, not the same index, so a file added or
	// removed above the selection doesn't silently re-point it at a neighbour.
	// Fall back to a clamped index when the selected name is gone (e.g. deleted).
	m.cursor = min(m.cursor, max(0, len(m.entries)-1))
	foundSameName := false
	if hadSel && oldSel.name != "" {
		for i, e := range m.entries {
			if e.name == oldSel.name {
				m.cursor = i
				foundSameName = true
				break
			}
		}
	}

	// Selected file unchanged? The list pane already reflects the sibling churn
	// (swapped above) — so leave the preview alone. refreshPreview would reset
	// srcWidth to 0 and stamp a placeholder, forcing a glamour/chroma re-render of an
	// identical file: pure CPU churn plus a one-frame flash every poll tick while an
	// agent writes files beside us. Compare the same fields dirSig folds (size+mtime,
	// plus isDir). The synthetic ".." carries zero size/modTime, so it always matches
	// and never re-renders its parent-listing preview — consistent with the cwd-only
	// (non-recursive) watch scope, which couldn't detect a parent change anyway.
	if foundSameName && m.cursor < len(m.entries) {
		newSel := m.entries[m.cursor]
		if oldSel.isDir == newSel.isDir &&
			oldSel.size == newSel.size &&
			oldSel.modTime.Equal(newSel.modTime) {
			return // list updated; selected file is byte-identical — preview stays
		}
	}

	// Selected file changed (or vanished → cursor moved by the clamp above): re-read
	// the preview for the new state and restore the scroll offset.
	m.refreshPreview()     // re-read the (possibly modified) selected file/dir
	m.previewTop = prevTop // refreshPreview reset scroll to 0; restore it...
	m.scrollPreview(0)     // ...then clamp it into the new content's range
}

// recoverVanishedCwd handles cwd being deleted while we're viewing it (e.g. an
// agent removes a scratch folder). It climbs to the nearest still-existing
// ancestor — never above the jail root — and reloads there, rather than leaving
// the user stuck on a dead directory.
func (m *model) recoverVanishedCwd() {
	for m.cwd != m.root {
		m.cwd = filepath.Dir(m.cwd)
		if _, err := os.Stat(m.cwd); err == nil {
			break
		}
	}
	m.cursor, m.listTop, m.previewTop = 0, 0, 0
	m.statusMsg = "⚠ folder removed — moved up"
	m.reload() // re-establishes fsSig, entries and preview at the new cwd
}

// refreshPreview loads the right panel for the currently selected entry.
func (m *model) refreshPreview() {
	// view.change telemetry (PRD §5.3, FR9): one event per refreshPreview
	// invocation — that IS the "cursor moved → preview reload" surface. Deferred
	// so it fires on every early-return branch with the post-state, and so the
	// reset hygiene below stays the visible top-of-function block. Record is
	// non-blocking (drop-on-full, FR14), so this never extends the Update
	// goroutine's tick.
	defer func() {
		kind := "file"
		var name string
		if m.cursor < len(m.entries) {
			sel := m.entries[m.cursor]
			name = sel.name
			switch {
			case name == "..":
				kind = "parent"
			case sel.isDir:
				kind = "dir"
			}
		}
		m.tel.Record("view.change", map[string]any{
			"entry_kind": kind,
			"ext_class":  extClass(name),
			"cwd_depth":  cwdDepth(m.root, m.cwd),
		})
	}()

	// Reset hygiene: this runs on every cursor move, so default the renderable
	// state OFF here — only the renderer branch below turns it on. Skipping this
	// would leave previewPreStyled true after navigating a rendered file → a plain
	// one, making renderPreview skip fitWidth on plain text (long lines overflow).
	m.previewTop = 0
	m.preview = nil // every branch below populates it (or leaves the dir branch to set previewEntries instead) — same reset discipline as the rest
	m.previewPreStyled = false
	m.srcPath = ""
	m.srcRaw = nil
	m.srcWidth = 0
	m.pendingWidth = 0 // the selection is changing; cancel any in-flight render's claim so syncPreview re-dispatches
	// Horizontal-scroll reset (prd-horizontal-scroll-preview). New selection →
	// flush-left + invalidate the reflow cache; only the plain/code branches below
	// turn previewScrollable back on. previewWrap is NOT reset — it is a session
	// preference, not per-file state (D15).
	m.previewScrollable = false
	m.previewHScroll = 0
	m.previewDisplay = nil
	m.previewSrcStart = nil
	m.previewMaxLineWidth = 0
	// Folder-preview state defaults OFF, same discipline: every non-dir branch
	// below leaves it off, only the dir branch turns it on. Skipping this
	// reset would carry a previous dir's entries into a file preview and
	// confuse previewClick + renderPreview's mode switch.
	m.previewEntries = nil
	m.previewIsDir = false

	if len(m.entries) == 0 {
		m.preview = []string{dimStyle.Render("(empty directory)")}
		return
	}
	sel := m.entries[m.cursor]
	// In search mode the entry name is a path relative to the jail root (e.g.
	// "docs/prd-search.md"), so resolve it against root; otherwise it is a bare
	// name in the current directory. One branch keeps the whole async preview
	// pipeline (syncPreview/applyPreview, renderer registry) unchanged (FR7).
	base := m.cwd
	if m.mode == modeSearch {
		base = m.root
	}
	full := filepath.Join(base, sel.name)
	if sel.isDir {
		entries, err := previewDir(full)
		if err != nil {
			// Surface the read error through the file-preview channel: keep
			// previewIsDir false so renderPreview takes the m.preview path
			// and shows this single line, the same channel a binary/empty
			// file error uses. previewClick guards on previewEntries length,
			// so it stays a noop on an unreadable folder.
			m.preview = []string{"⚠ " + err.Error()}
			return
		}
		m.previewEntries = entries
		m.previewIsDir = true
		return
	}

	content, kind := readPreviewBytes(full, sel.size)
	// A registered renderer (markdown/code/image/…) takes over when it matches the
	// file AND the content suits it: text renderers need real text (skipped on a
	// binary file), binary renderers (image) run regardless. Rendering itself is
	// async (see syncPreview) — here we only stash the source and show an instant
	// placeholder, never run the heavy renderer inline (that would freeze Update).
	if r, ok := rendererFor(sel.name); ok && (r.binary || kind == "text") {
		m.srcPath = full
		// Code is horizontally scrollable (long lines pan); markdown wraps via
		// glamour and images are placeholders — neither pans (FR7/D3).
		m.previewScrollable = r.name == "code"
		if r.binary {
			m.srcRaw = content // raw bytes (image reads the path; content is incidental)
			m.preview = []string{dimStyle.Render("(rendering " + r.name + "…)")}
		} else {
			m.srcRaw = []byte(normalizeText(content)) // text renderers get normalized source (FR8)
			m.preview = plainLines(content)           // readable raw-source placeholder until the render lands
		}
		return
	}
	// No renderer (or a text renderer facing a binary file) → plain/placeholder.
	if kind == "text" {
		m.preview = plainLines(content)
		m.previewScrollable = true // plain text pans horizontally
	} else {
		m.preview = placeholderLines(kind, content, sel.size)
	}
}

// reflowPreview rebuilds previewDisplay — the visual lines the vertical scroller
// iterates — from m.preview, the wrap mode, and the content width. It is the
// single place wrap-expansion happens, cache-guarded by (width, wrap) so the
// render path never re-wraps every frame. Folder + markdown + non-scrollable
// previews don't need it (renderPreview handles them without previewDisplay), so
// it returns early for them. Called from syncPreview's head (covers nav / resize
// / drag) and toggleWrap.
func (m *model) reflowPreview(w int) {
	if w <= 0 || m.previewIsDir || !m.previewScrollable {
		return
	}
	if m.previewDisplayW == w && m.previewDisplayWrap == m.previewWrap && m.previewDisplay != nil {
		return // cache hit
	}
	m.previewDisplayW = w
	m.previewDisplayWrap = m.previewWrap

	srcStart := make([]int, len(m.preview))
	if m.previewWrap {
		// wrap=true: expand each source line to ≤w visual lines.
		var out []string
		for s, line := range m.preview {
			srcStart[s] = len(out)
			out = append(out, wrapLine(line, w)...)
		}
		m.previewDisplay = out
		m.previewMaxLineWidth = 0 // no hscroll in wrap mode
		m.previewSrcStart = srcStart
		return
	}
	// wrap=false: logical lines unchanged (1:1); record the widest for the clamp.
	m.previewDisplay = m.preview
	maxW := 0
	for s, line := range m.preview {
		srcStart[s] = s
		if lw := lineWidth(line); lw > maxW {
			maxW = lw
		}
	}
	m.previewMaxLineWidth = maxW
	m.previewSrcStart = srcStart
}

// sourceLineAt returns the source line whose visual block contains visual line v
// (the largest s with previewSrcStart[s] <= v). visualLineFor is its inverse.
// Together they let toggleWrap keep the same source line pinned to the viewport
// top across a wrap flip, so the view doesn't jump to an unrelated line.
func (m model) sourceLineAt(v int) int {
	ss := m.previewSrcStart
	if len(ss) == 0 {
		return 0
	}
	lo, hi := 0, len(ss)-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if ss[mid] <= v {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo
}

func (m model) visualLineFor(s int) int {
	ss := m.previewSrcStart
	if s < 0 || s >= len(ss) {
		return 0
	}
	return ss[s]
}

// scrollPreviewH pans the preview viewport horizontally by delta columns, clamped
// to [0, maxHScroll]. maxHScroll is the widest source line minus the content
// width — when content fits, it is 0 and any pan is a no-op. No-op entirely in
// wrap mode or for markdown/folder (nothing overflows there).
func (m *model) scrollPreviewH(delta int) {
	if m.previewWrap || !m.previewScrollable {
		return
	}
	w := m.previewBodyWidth()
	maxH := max(0, m.previewMaxLineWidth-max(1, w-2)) // -2 ≈ both edge indicators
	before := m.previewHScroll
	m.previewHScroll = min(max(0, m.previewHScroll+delta), maxH)
	if m.previewHScroll != before {
		dir := "right"
		if delta < 0 {
			dir = "left"
		}
		m.tel.Record("action.preview_hscroll", map[string]any{"direction": dir})
	}
}

// toggleWrap flips wrap mode while keeping the same SOURCE line at the top of the
// viewport. previewTop is a visual-line index whose meaning flips logical↔visual
// on toggle; without re-mapping it, the viewport would jerk to an unrelated line
// — defeating the "I'm reading this line, wrap it" intent.
func (m *model) toggleWrap() {
	if !m.previewScrollable {
		return
	}
	srcAtTop := m.sourceLineAt(m.previewTop)
	m.previewWrap = !m.previewWrap
	m.previewHScroll = 0
	m.reflowPreview(m.previewBodyWidth())
	m.previewTop = m.visualLineFor(srcAtTop)
	m.scrollPreview(0) // clamp into range
	m.tel.Record("action.preview_wrap_toggle", map[string]any{"wrap": m.previewWrap})
}

// previewBodyWidth returns the content columns of the preview pane at the
// current layout. In 2-col side-by-side mode it is g.rightInner (preview is
// the right pane). In 1-col stacked mode preview spans the full m.width
// (borderless, no chrome to subtract) — leftInner is populated to m.width
// for both panes in vertical layout, so the same field serves here too.
//
// A renderer (markdown/code) wraps to this width; when the orientation
// changes (resize across widthBreakpoint), the returned value changes, and
// syncPreview re-dispatches because m.srcWidth no longer matches. That is
// what makes FR7 (markdown reflow on mode flip) work with no extra code in
// the render pipeline.
func (m model) previewBodyWidth() int {
	g := m.layout()
	if g.vertical {
		return g.leftInner // = m.width in vertical mode
	}
	return g.rightInner
}

// syncPreview is the single reconciliation point for async preview rendering.
// Called once at the tail of Update, it inspects the current state and returns a
// render Cmd when — and only when — the displayed preview is out of date with the
// selected renderable file at the current width. It returns nil (no work) for a
// non-renderable selection, an unknown width (deferred until WindowSizeMsg), a
// preview already rendered at this width (cache hit), an in-flight render already
// targeting this width, or while the divider is being dragged (reflow waits for
// release so a drag doesn't spawn a render per motion event).
//
// Each dispatch bumps renderGen and records pendingWidth, so the result can be
// matched back (stale ones discarded) and repeated Update ticks don't re-spawn
// the same render. The heavy renderer runs inside the returned closure, off the
// Update goroutine — this is what keeps the UI responsive.
func (m *model) syncPreview() tea.Cmd {
	// Keep the wrap/visual-line reflow cache current at the tail of every Update —
	// this single hook covers navigation (m.preview changed), resize, and divider
	// drag (preview body width changed). Cache-guarded, so it is a cheap no-op when
	// nothing relevant changed, and it returns early for non-scrollable previews.
	m.reflowPreview(m.previewBodyWidth())
	if m.srcPath == "" {
		return nil // selection has no renderer
	}
	if m.dragging {
		return nil // defer reflow until the divider settles (avoid a render per motion)
	}
	w := m.previewBodyWidth()
	if w <= 0 {
		return nil // width not known yet (initial load before first WindowSizeMsg)
	}
	if m.srcWidth == w {
		return nil // already rendered this source at this width (cache hit)
	}
	if m.pendingWidth == w {
		return nil // a render for this exact width is already in flight
	}
	r, ok := rendererFor(filepath.Base(m.srcPath))
	if !ok {
		return nil // defensive: srcPath is only set when a renderer matched
	}

	m.renderGen++
	m.pendingWidth = w
	// Stamp the dispatch time only when telemetry is on (PRD §5.3) — the no-op
	// path stays bytes-identical (FR6) without the time.Now syscall. applyPreview
	// reads + clears renderStartedAt when the result lands.
	if m.tel.Active() {
		m.renderStartedAt = time.Now()
	}
	gen, path, raw, style := m.renderGen, m.srcPath, m.srcRaw, m.renderStyle
	// Returns ONLY the render Cmd (its message is previewRenderedMsg) — the spinner
	// loop is kicked separately in reconcilePreview so this contract stays simple
	// for every caller that runs the Cmd and matches on previewRenderedMsg.
	return func() tea.Msg {
		lines, preStyled, err := r.render(path, raw, w, style)
		return previewRenderedMsg{gen: gen, width: w, lines: lines, preStyled: preStyled, err: err}
	}
}

// reconcilePreview is the tail preview step shared by Update's fall-through and the
// searchWalkedMsg branch: dispatch a render if the displayed preview is stale
// (syncPreview), then start the footer spinner loop when a render is in flight and
// no loop is running yet. Batching the spinner kick HERE rather than inside
// syncPreview keeps syncPreview's Cmd a bare previewRenderedMsg producer. The
// guard (pendingWidth>0 && !spinning) only fires right after a fresh dispatch —
// while a render is pending the loop keeps spinning itself, so it never doubles.
func (m *model) reconcilePreview(extra tea.Cmd) tea.Cmd {
	render := m.syncPreview()
	var spin tea.Cmd
	if m.pendingWidth > 0 && !m.spinning {
		m.spinning = true
		spin = spinnerTickCmd()
	}
	return tea.Batch(extra, render, spin)
}

// applyPreview applies a completed render. It drops a stale result — one whose
// gen no longer matches, meaning the user navigated (or resized) and a newer
// render now owns the preview — so fast scrolling never shows the wrong file's
// content. previewPreStyled is taken from the result (a plain placeholder render
// sets it false → renderPreview keeps fitWidth). On renderer error it falls back
// to the raw source as plain text.
func (m *model) applyPreview(msg previewRenderedMsg) {
	if msg.gen != m.renderGen {
		return // stale: a newer render was dispatched since; it owns pendingWidth
	}
	m.pendingWidth = 0

	// duration_ms for action.preview_rendered (PRD §5.3). renderStartedAt was
	// stamped in syncPreview only when telemetry is active; a zero value means
	// telemetry is off (or this is a stale-but-reaching path) — report 0.
	var durationMS int64
	if !m.renderStartedAt.IsZero() {
		durationMS = time.Since(m.renderStartedAt).Milliseconds()
		m.renderStartedAt = time.Time{} // clear so the next dispatch starts fresh
	}

	// Renderer name comes from the registry — one indirection through srcPath
	// keeps telemetry honest about which renderer produced the result.
	var renderer string
	if r, ok := rendererFor(filepath.Base(m.srcPath)); ok {
		renderer = r.name
	}

	if msg.err != nil {
		// plainLines on srcRaw is safe for text renderers (srcRaw is normalized
		// text) and for image (which never errors → never reaches here). A future
		// binary renderer that DOES error should fall back to a typed placeholder,
		// not raw bytes — which would render as garbage.
		m.preview = plainLines(m.srcRaw)
		m.previewPreStyled = false
		m.srcWidth = 0
		m.reflowPreview(m.previewBodyWidth()) // source fallback is still scrollable; rebuild the cache
		m.statusMsg = "⚠ preview render failed, showing source"

		// error.render_fail (FR11). errorClass enums the renderer error origin;
		// the raw msg.err.Error() string is intentionally NOT included — it may
		// carry a path (see PRD §5.4 invariant).
		m.tel.Record("error.render_fail", map[string]any{
			"renderer":    renderer,
			"error_class": errorClass(msg.err),
		})
		return
	}
	m.preview = msg.lines
	m.previewPreStyled = msg.preStyled
	m.srcWidth = msg.width
	m.reflowPreview(m.previewBodyWidth()) // rebuild visual-line cache before the clamp reads previewLen
	m.scrollPreview(0)                    // clamp the viewport into the freshly-sized content

	// action.preview_rendered (FR10). lines is the rendered line count, NOT
	// the file's logical line count — backend can correlate against width.
	m.tel.Record("action.preview_rendered", map[string]any{
		"renderer":    renderer,
		"width":       msg.width,
		"lines":       len(msg.lines),
		"duration_ms": durationMS,
	})
}

func (m model) Init() tea.Cmd { return tickCmd() }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tickMsg:
		// Poll cwd for changes an agent (or anything else) made beside us. Skip
		// while the user is mid-prompt or dragging the divider so we never yank
		// the selection or churn the layout out from under them. Always reschedule
		// so the loop keeps running regardless.
		if m.mode == modeNormal && !m.dragging {
			m.syncFromDisk()
		}
		cmd = tickCmd()
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		// Width is now known (or changed) — the tail syncPreview renders the
		// deferred preview / reflows it to the new width.
		//
		// Drag-mid-flip handling (PRD §5.11, FR8): if the responsive trigger
		// just flipped orientation (e.g. user shrank a 2-col session below 80
		// cols), the active drag's axis is about to swap under the user's
		// finger. Clear m.dragging BEFORE updating m.lastVertical so the tail
		// syncPreview (which gates re-render on `!m.dragging`) sees the cleaned
		// state on this very tick — not one frame late.
		newVertical := m.width < widthBreakpoint
		if newVertical != m.lastVertical {
			m.dragging = false
		}
		m.lastVertical = newVertical
	case tea.MouseMsg:
		if m.mode != modeNormal { // ignore mouse while in a prompt
			return m, nil
		}
		var nm tea.Model
		nm, cmd = m.handleMouse(msg)
		m = nm.(model)
	case tea.KeyPressMsg:
		var nm tea.Model
		switch m.mode {
		case modeConfirmDelete:
			nm, cmd = m.updateConfirmDelete(msg)
		case modeRename:
			nm, cmd = m.updateRename(msg)
		case modeSearch:
			nm, cmd = m.updateSearch(msg)
		case modeCommandPalette:
			nm, cmd = m.updateCommandPalette(msg)
		case modeHelp:
			nm, cmd = m.updateHelp(msg)
		default:
			nm, cmd = m.updateNormal(msg)
		}
		m = nm.(model)
	case previewRenderedMsg:
		m.applyPreview(msg)
		return m, nil
	case spinnerTickMsg:
		// Advance the footer spinner only while a render is in flight; otherwise
		// let the loop die (don't reschedule) so an idle UI isn't woken at 10Hz.
		if m.pendingWidth > 0 {
			m.spinnerFrame++
			return m, spinnerTickCmd()
		}
		m.spinning, m.spinnerFrame = false, 0
		return m, nil
	case searchWalkedMsg:
		// A walk completed. Drop it entirely if the gen no longer matches — the
		// user re-entered search, so a newer walk owns the result list (FR2).
		if msg.gen != m.searchGen {
			return m, nil
		}
		// Always warm the cache from a current-gen walk, even if the user has
		// since Esc'd back to normal mode: it makes the next "/" a cache hit.
		m.searchIndexing = false
		m.searchAll = msg.results
		m.searchAllAt = time.Now()
		// But only swap the walk INTO the visible list when still searching. A
		// walk that lands after Esc (Esc does not bump the gen) must not clobber
		// the restored normal-mode listing — populating the cache above is enough.
		if m.mode != modeSearch {
			return m, nil
		}
		m.applySearchFilter() // sets the result list + the FR15 "0 results" hint
		if msg.err != nil {
			// filepath.WalkDir can return both partial results AND an error; keep
			// what we gathered and surface the error (overriding the hint above) so
			// the list is still useful while the user knows the walk was partial.
			m.statusMsg = "⚠ walk error: " + msg.err.Error()
		}
		return m, m.reconcilePreview(cmd)
	default:
		return m, nil
	}
	// Single reconciliation point: whatever the message above did to the
	// selection, width, or divider, decide here whether a preview (re)render
	// must be dispatched off the Update goroutine. nil when nothing's needed.
	return m, m.reconcilePreview(cmd)
}

// handleMouse maps clicks and wheel events onto the two panels using the same
// geometry the renderer uses, so hit-testing can never drift from the layout.
// In bubbletea v2 the mouse action is encoded by the message TYPE (click /
// release / motion / wheel) rather than an Action field, so we switch on the
// concrete type; e holds the shared cursor data (button + position).
//
// The divider is a "no-pane" zone: a left-press anywhere in its hit-zone
// starts a drag (wider hit-zone is the whole point of the padding — PRD
// FR4/D5), a wheel over it noops (FR9), and a left-click without drag intent
// on the status row noops (FR7). All other clicks route to list or preview by
// an axis-appropriate `overList` split.
//
// Orientation comes from g.vertical (single source of truth via layout()):
//
//   - HORIZONTAL — divider band is 3 cols [dividerStart, dividerStart+dividerWidth);
//     overList = e.X < dividerStart; drag tracks the X axis (setLeftFromX);
//     list pane height is g.bodyH.
//
//   - VERTICAL — divider band is 3 rows centred on dividerYStart (one glyph row
//
//   - dividerHitRowsAbove above + dividerHitRowsBelow below); overList = e.Y <
//     dividerYStart; drag tracks the Y axis (setTopFromY); list pane height is
//     g.topInner.
func (m model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	g := m.layout()
	e := msg.Mouse()

	// overDivider: hit-zone band of the divider in the current orientation.
	// In horizontal mode the band coincides exactly with the visible 3 cols.
	// In vertical mode the visible strip is 1 row but the hit-zone widens
	// to ±dividerHitRowsAbove/Below — same affordance as horizontal's 3 cols
	// without spending screen rows on a painted pad row.
	var overDivider bool
	if g.vertical {
		overDivider = e.Y >= g.dividerYStart-dividerHitRowsAbove &&
			e.Y <= g.dividerYStart+dividerHeight-1+dividerHitRowsBelow
	} else {
		overDivider = e.X >= g.dividerStart && e.X < g.dividerStart+dividerWidth
	}

	switch msg.(type) {
	case tea.MouseMotionMsg:
		// Divider drag in progress: track the cursor on the active axis. The
		// drag-start branch below picked the axis based on g.vertical at press
		// time; motion just continues that same axis here.
		if m.dragging {
			if g.vertical {
				m.setTopFromY(e.Y)
			} else {
				m.setLeftFromX(e.X)
			}
		}
		return m, nil

	case tea.MouseReleaseMsg:
		m.dragging = false
		// Reflow to the divider's new dimensions happens in Update's tail
		// syncPreview now that dragging is false (deferred during motion).
		return m, nil

	case tea.MouseWheelMsg:
		// Wheel over the divider is a noop (FR9). Without this guard, the
		// axis-aware overList split below would route divider-zone wheel
		// events into the list pane (horizontal: divider cols; vertical:
		// divider band rows).
		if overDivider {
			return m, nil
		}
		overList := false
		if g.vertical {
			overList = e.Y < g.dividerYStart
		} else {
			overList = e.X < g.dividerStart
		}
		switch e.Button {
		case tea.MouseWheelUp:
			if overList {
				if m.cursor > 0 {
					m.cursor--
					m.refreshPreview()
				}
			} else {
				m.scrollPreview(-previewLineStep)
			}
		case tea.MouseWheelDown:
			if overList {
				if m.cursor < len(m.entries)-1 {
					m.cursor++
					m.refreshPreview()
				}
			} else {
				m.scrollPreview(previewLineStep)
			}
		}
		return m, nil

	case tea.MouseClickMsg:
		// Divider drag start: a left-press anywhere in the divider's hit-zone
		// starts a drag and snaps the glyph to the cursor (col in horizontal,
		// row in vertical). Excluding the status row (m.height-1) means a
		// click on the status text whose X/Y happens to fall in the divider
		// band doesn't accidentally start a drag.
		if e.Button == tea.MouseLeft && e.Y < m.height-1 && overDivider {
			m.dragging = true
			if g.vertical {
				m.setTopFromY(e.Y)
			} else {
				m.setLeftFromX(e.X)
			}
			return m, nil
		}
		if e.Button != tea.MouseLeft {
			return m, nil
		}
		// Left-click on the divider that wasn't a drag-start (status row, or
		// future per-pane modes) is a noop — divider is a "no-pane" zone and
		// must never route to list or preview (FR7). It also must not change
		// focus, so this returns before the focus-set below.
		if overDivider {
			return m, nil
		}
		overList := false
		listH := g.bodyH
		if g.vertical {
			overList = e.Y < g.dividerYStart
			listH = g.topInner
		} else {
			overList = e.X < g.dividerStart
		}
		// A committed left-click sets focus to the pane it landed in (FR8), in
		// sync with the wheel's mental model: the pane you just interacted with
		// is the pane keyboard navigation now acts on. Set it here — after the
		// divider/non-left early-returns, so a no-pane click never flips focus —
		// and before routing the click into the pane's own handling. overList is
		// axis-aware (X split in 2-col, Y split in 1-col stacked), so focus
		// follows the click correctly in either orientation.
		if overList {
			m.focusPane = focusList
		} else {
			m.focusPane = focusPreview
		}
		if !overList {
			m.previewClick(e.Y, g) // click a name in a folder listing → open + select it
			return m, nil
		}
		row := e.Y - g.firstRow
		if row < 0 || row >= listH {
			return m, nil
		}
		idx := g.listTop + row
		if idx < 0 || idx >= len(m.entries) {
			return m, nil
		}
		if idx == m.cursor && m.entries[idx].isDir {
			m.descend() // click again on the selected folder opens it
		} else {
			m.cursor = idx
			m.refreshPreview()
		}
	}
	return m, nil
}

// setLeftFromX pins the divider glyph under the cursor: column x becomes the
// dividerCenter, so leftRatio = x / m.width. Click on either pad column of the
// divider snaps the glyph to that col (FR4) — a one-col visual jump that
// matches the click-to-snap pattern from the border era. The value is stored
// as a ratio and only clamped at render time (leftInnerWidth), keeping the
// split proportional across terminal resizes.
func (m *model) setLeftFromX(x int) {
	if m.width <= 0 {
		return
	}
	m.leftRatio = float64(x) / float64(m.width)
}

// setTopFromY is setLeftFromX's Y-axis mirror for the 1-col stacked layout:
// row y becomes dividerYStart, so topRatio = y / bodyH (no +1 — the
// post-borderless semantics treat the cursor row directly as the divider row,
// same discipline as setLeftFromX). Stored as a ratio so the split stays
// proportional across terminal height resizes (topInnerHeight does the clamp).
func (m *model) setTopFromY(y int) {
	bodyH := max(m.height-1, 3)
	if bodyH <= 0 {
		return
	}
	m.topRatio = float64(y) / float64(bodyH)
}

// moveCursor nudges the list cursor by delta rows and refreshes the preview.
// Clamps to [0, len(entries)-1]; a delta that overshoots either end snaps to
// the edge. Empty list → noop. Centralizing the move keeps updateNormal flat —
// j/k, ctrl+d/u (list half-page), and any future "page list" key call this and
// inherit the same clamping + preview refresh.
func (m *model) moveCursor(delta int) {
	if len(m.entries) == 0 {
		return
	}
	target := min(max(0, m.cursor+delta), len(m.entries)-1)
	if target == m.cursor {
		return
	}
	m.cursor = target
	m.refreshPreview()
}

// scrollPreview moves the right-panel viewport by delta lines, clamped to
// range. The length comes from previewLen so a folder listing (entries) and
// a file preview (lines) share the same scroll math — no branch needed here.
func (m *model) scrollPreview(delta int) {
	_, bodyH := m.previewScroll()
	maxTop := max(0, m.previewLen()-bodyH)
	m.previewTop = min(max(0, m.previewTop+delta), maxTop)
}

// previewClick handles a left-click inside the right panel. When that panel is
// showing a folder listing (the selected entry is a directory), clicking one of
// its rows enters that folder and lands the cursor on the clicked item — the
// same end state as descending via the left panel and moving onto it. Clicks on
// a file preview, the panel border, or empty space do nothing.
func (m *model) previewClick(y int, g geometry) {
	if len(m.entries) == 0 {
		return
	}
	sel := m.entries[m.cursor]
	if !sel.isDir {
		return // the right panel is a file preview, not a clickable listing
	}

	top, bodyH := m.previewScroll()
	// previewFirstRow is 0 in horizontal mode (preview starts at body top) and
	// g.topInner+dividerHeight in vertical mode (preview starts after the list
	// pane + the horizontal divider strip). Branching is centralised in
	// layout(); previewClick stays orientation-agnostic.
	row := y - g.previewFirstRow
	if row < 0 || row >= bodyH {
		return // outside the preview pane (status row, divider, or list pane area)
	}

	// The listing rows map 1:1, in order, to m.previewEntries (no synthetic
	// "..") — the SAME slice renderPreview drew. So resolve the clicked item
	// straight from that cached slice: render + click can never disagree
	// about which entry sits on which row. A click on empty space or an
	// empty-folder placeholder (len==0) falls through the bound and noops.
	lineIdx := top + row
	if lineIdx >= len(m.previewEntries) {
		return
	}
	clicked := m.previewEntries[lineIdx].name

	// Enter the folder (jail-guarded) and land on the clicked item. descend()
	// reloads entries (prepending the synthetic ".." for a sub-folder), so match
	// by name rather than index. When sel is the synthetic "..", descend() routes
	// to ascend() and the same name lookup finds the item in the parent.
	m.descend()
	for i, e := range m.entries {
		if e.name == clicked {
			m.cursor = i
			break
		}
	}
	m.refreshPreview()
}

func (m model) updateNormal(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	km := m.keymap

	switch {
	case key.Matches(msg, km.Quit):
		return m, tea.Quit

	case key.Matches(msg, km.CommandPalette):
		m.enterCommandPalette()
		return m, nil

	case key.Matches(msg, km.FullHelp):
		m.enterHelp()
		return m, nil

	// Tab toggles which pane the navigation keys act on (prd-pane-focus D3).
	// Two panes → forward == backward, so one key is enough; no shift+tab.
	case key.Matches(msg, km.FocusToggle):
		if m.focusPane == focusList {
			m.focusPane = focusPreview
		} else {
			m.focusPane = focusList
		}

	// "Scroll-ish" keys route to the focused pane (prd-pane-focus D4): on the
	// list they move the cursor, on the preview they scroll the viewport — the
	// same mental model the mouse wheel uses. MoveDown ≡ PreviewScrollDown by key
	// code; key.Matches compares codes only, so one case per pair matches either
	// and the focusPane branch picks the behavior.
	case key.Matches(msg, km.MoveDown):
		if m.focusPane == focusList {
			m.moveCursor(1)
		} else {
			m.scrollPreview(1)
		}
	case key.Matches(msg, km.MoveUp):
		if m.focusPane == focusList {
			m.moveCursor(-1)
		} else {
			m.scrollPreview(-1)
		}
	case key.Matches(msg, km.GoTop): // ≡ km.PreviewJumpTop
		if m.focusPane == focusList {
			m.moveCursor(-len(m.entries)) // overshoots → clamps to index 0
		} else {
			m.previewTop = 0
		}
	case key.Matches(msg, km.GoBottom): // ≡ km.PreviewJumpBottom
		if m.focusPane == focusList {
			m.moveCursor(len(m.entries)) // overshoots → clamps to last index
		} else {
			_, bodyH := m.previewScroll()
			m.previewTop = max(0, m.previewLen()-bodyH)
		}

	// ctrl+d/u is the coarse "half-page" tier (prd-pane-focus D11). Step is half
	// the preview body, recomputed each press so it tracks resizes/divider drags
	// (min 1 so a 1-row body still moves).
	case key.Matches(msg, km.PreviewHalfPageDown):
		_, bodyH := m.previewScroll()
		step := max(1, bodyH/2)
		if m.focusPane == focusList {
			m.moveCursor(step)
		} else {
			m.scrollPreview(step)
		}
	case key.Matches(msg, km.PreviewHalfPageUp):
		_, bodyH := m.previewScroll()
		step := max(1, bodyH/2)
		if m.focusPane == focusList {
			m.moveCursor(-step)
		} else {
			m.scrollPreview(-step)
		}

	// Mutation + list-navigation keys need a meaningful list selection, so they
	// only act when the list is focused (D5/FR5). Under focusPreview they are
	// no-ops — pressing d while reading a preview is ambiguous, so it does
	// nothing rather than guess.
	case key.Matches(msg, km.OpenEntry): // l/right ≡ PreviewScrollRight in focusPreview
		if m.focusPane == focusList {
			m.descend()
		} else {
			m.scrollPreviewH(previewColStep) // pan right
		}
	case key.Matches(msg, km.GoUp): // h/left/backspace ≡ PreviewScrollLeft in focusPreview
		if m.focusPane == focusList {
			m.ascend()
		} else {
			m.scrollPreviewH(-previewColStep) // pan left
		}

	// Coarse half-width pan + reset + wrap toggle (focusPreview, plain/code only —
	// scrollPreviewH/toggleWrap are no-ops otherwise). These keys are unused on the
	// list, so they only act under focusPreview.
	case key.Matches(msg, km.PreviewHScrollHalfRight): // L
		if m.focusPane == focusPreview {
			m.scrollPreviewH(max(1, m.previewBodyWidth()/2))
		}
	case key.Matches(msg, km.PreviewHScrollHalfLeft): // H
		if m.focusPane == focusPreview {
			m.scrollPreviewH(-max(1, m.previewBodyWidth()/2))
		}
	case key.Matches(msg, km.PreviewHScrollReset): // 0
		if m.focusPane == focusPreview {
			m.previewHScroll = 0
		}
	case key.Matches(msg, km.PreviewToggleWrap): // w
		if m.focusPane == focusPreview {
			m.toggleWrap()
		}

	case key.Matches(msg, km.Rename):
		if m.focusPane == focusList && len(m.entries) > 0 && m.entries[m.cursor].name != ".." {
			m.mode = modeRename
			m.input = m.entries[m.cursor].name
			m.statusMsg = ""
		}
	case key.Matches(msg, km.Delete):
		if m.focusPane == focusList && len(m.entries) > 0 && m.entries[m.cursor].name != ".." {
			m.mode = modeConfirmDelete
			m.statusMsg = ""
		}

	// Search is a mode switch, not a list mutation — fires regardless of focusPane.
	// enterSearch snapshots state (for Esc restore) and returns a walk Cmd on a
	// cache miss (nil on a cache hit) — forward it so the async walk dispatches.
	case key.Matches(msg, km.Search):
		return m, m.enterSearch()

	// Esc steps back: from preview-focus it returns to the list (D6/FR6). On the
	// list it is a no-op (modeNormal has no other Esc behavior).
	case key.Matches(msg, km.Back):
		if m.focusPane == focusPreview {
			m.focusPane = focusList
		}
	}
	return m, nil
}

// descend enters the selected folder (if it is one).
func (m *model) descend() {
	if len(m.entries) == 0 {
		return
	}
	sel := m.entries[m.cursor]
	if sel.name == ".." {
		m.ascend() // the synthetic parent entry navigates up, with the root guard
		return
	}
	if !sel.isDir {
		return
	}
	target := filepath.Join(m.cwd, sel.name)
	if !withinRoot(m.root, target) {
		m.statusMsg = "⚠ blocked: outside root"
		return
	}
	m.cwd = target
	m.cursor = 0
	m.listTop = 0
	m.statusMsg = ""
	m.reload()
}

// ascend goes up one level, refusing to cross above the jail root.
func (m *model) ascend() {
	if m.cwd == m.root {
		m.statusMsg = "⚠ at root — cannot go higher"
		return
	}
	parent := filepath.Dir(m.cwd)
	if !withinRoot(m.root, parent) {
		m.statusMsg = "⚠ at root — cannot go higher"
		return
	}
	prev := filepath.Base(m.cwd) // remember where we came from to place the cursor
	m.cwd = parent
	m.listTop = 0
	m.reload()
	// position cursor on the folder we just left
	for i, e := range m.entries {
		if e.name == prev {
			m.cursor = i
			break
		}
	}
	m.refreshPreview()
	m.statusMsg = ""
}

// updateSearch handles keypresses while in modeSearch (PRD §5.5). Esc and a
// backspace on an empty query exit + restore (FR10/D13); Enter opens the
// selected result (FR8/FR9); up/down (and ctrl+p/ctrl+n) move within the result
// list; backspace trims a rune; any printable text appends to the query. Every
// query mutation re-filters (FR6). Keys are tea.KeyPressMsg in v2 — printable
// input arrives in msg.Text (empty for named keys), the same pattern updateRename
// uses; named keys are matched via msg.String().
func (m model) updateSearch(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.exitSearchRestore()
		return m, nil
	case "enter":
		m.openSearchResult()
		return m, nil
	case "up", "ctrl+p":
		if m.cursor > 0 {
			m.cursor--
			m.refreshPreview()
		}
	case "down", "ctrl+n":
		if m.cursor < len(m.entries)-1 {
			m.cursor++
			m.refreshPreview()
		}
	case "backspace":
		if m.searchQuery == "" {
			m.exitSearchRestore() // D13: backspace on an empty query backs out
			return m, nil
		}
		r := []rune(m.searchQuery)
		m.searchQuery = string(r[:len(r)-1])
		m.applySearchFilter()
	default:
		// msg.Text holds the printable characters of the key press (empty for
		// arrows / function keys). Appending it builds the query a rune at a time.
		if msg.Text != "" {
			m.searchQuery += msg.Text
			m.applySearchFilter()
		}
	}
	return m, nil
}

// enterSearch transitions normal → search (PRD §5.5, FR1/FR2). It snapshots the
// pre-search state for Esc restore, then either reuses a fresh walk (cache hit
// within walkCacheTTL → no Cmd) or dispatches an async walk and shows the
// indexing chip (cache miss). It returns a tea.Cmd the caller must forward; nil
// on a cache hit.
func (m *model) enterSearch() tea.Cmd {
	m.searchSavedCwd = m.cwd
	m.searchSavedEntries = append([]entry(nil), m.entries...) // defensive copy
	m.searchSavedFsSig = m.fsSig
	m.searchSavedCursor = m.cursor
	m.searchSavedListTop = m.listTop

	m.mode = modeSearch
	m.searchQuery = ""
	m.statusMsg = ""

	// Cache hit: the last walk is still fresh — filter it immediately, no re-walk.
	if len(m.searchAll) > 0 && time.Since(m.searchAllAt) < walkCacheTTL {
		m.applySearchFilter()
		return nil
	}
	// Cache miss: walk async. List is empty (refreshPreview shows the placeholder)
	// and the indexing chip explains why until the walk lands. Bumping searchGen
	// invalidates any in-flight walk from a previous activation.
	m.searchIndexing = true
	m.searchGen++
	m.entries = nil
	m.cursor = 0
	m.listTop = 0
	m.refreshPreview()
	return walkTreeCmd(m.root, m.searchGen)
}

// exitSearchRestore leaves search mode and restores the exact pre-search state
// (PRD §5.5, FR10): cwd, entries, fsSig (the poll-loop baseline), cursor, and
// scroll. refreshPreview then re-syncs the right panel to the restored selection.
func (m *model) exitSearchRestore() {
	m.entries = m.searchSavedEntries
	m.cwd = m.searchSavedCwd
	m.fsSig = m.searchSavedFsSig
	m.cursor = m.searchSavedCursor
	m.listTop = m.searchSavedListTop
	m.mode = modeNormal
	m.searchQuery = ""
	m.statusMsg = "search cancelled"
	m.refreshPreview()
}

// openSearchResult acts on the highlighted result (PRD §5.5, FR8/FR9). A file
// result cd's into its parent and lands the cursor on the basename; a folder
// result cd's into the folder. Both are jail-checked via withinRoot. An empty
// result list (walk still running / no matches) falls back to a clean restore
// rather than indexing out of bounds.
func (m *model) openSearchResult() {
	if m.cursor >= len(m.entries) {
		m.exitSearchRestore()
		return
	}
	sel := m.entries[m.cursor]
	target := filepath.Join(m.root, sel.name)
	if !withinRoot(m.root, target) {
		m.statusMsg = "⚠ blocked: outside root"
		return
	}

	m.mode = modeNormal
	m.searchQuery = ""
	m.statusMsg = ""
	m.cursor = 0
	m.listTop = 0

	if sel.isDir {
		m.cwd = target // FR9: cd into the folder itself
		m.reload()
		return
	}

	// FR8: cd into the file's parent, then land the cursor on the basename.
	m.cwd = filepath.Dir(target)
	m.reload()
	base := filepath.Base(sel.name)
	for i, e := range m.entries {
		if e.name == base {
			m.cursor = i
			break
		}
	}
	m.refreshPreview()
}

// applySearchFilter recomputes the result list from the current query (PRD §5.5,
// FR6). The cursor and scroll reset to the top match, and the preview re-syncs to
// the new selection.
func (m *model) applySearchFilter() {
	m.entries = filterSearch(m.searchAll, m.searchQuery, maxSearchResults)
	m.cursor = 0
	m.listTop = 0
	// A non-empty query that matched nothing gets an explicit hint (FR15) so the
	// empty list reads as "no match", distinct from the indexing/empty-tree case.
	// An empty query (the browse-everything view) clears any stale hint.
	switch {
	case m.searchQuery != "" && len(m.entries) == 0:
		m.statusMsg = "(0 results — refine or Esc)"
	default:
		m.statusMsg = ""
	}
	m.refreshPreview()
}

func (m model) updateConfirmDelete(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		sel := m.entries[m.cursor]
		target := filepath.Join(m.cwd, sel.name)
		if err := os.RemoveAll(target); err != nil {
			m.statusMsg = "⚠ delete failed: " + err.Error()
		} else {
			m.statusMsg = "deleted " + sel.name
		}
		m.mode = modeNormal
		m.reload()
	default: // any other key cancels
		m.mode = modeNormal
		m.statusMsg = "delete cancelled"
	}
	return m, nil
}

func (m model) updateRename(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeNormal
		m.statusMsg = "rename cancelled"
	case "enter":
		newName := m.input
		m.mode = modeNormal
		if newName == "" || newName == m.entries[m.cursor].name {
			m.statusMsg = "rename cancelled"
			return m, nil
		}
		// Guard against path traversal in the typed name.
		if filepath.Base(newName) != newName {
			m.statusMsg = "⚠ name cannot contain a path separator"
			return m, nil
		}
		old := filepath.Join(m.cwd, m.entries[m.cursor].name)
		dst := filepath.Join(m.cwd, newName)
		if err := os.Rename(old, dst); err != nil {
			m.statusMsg = "⚠ rename failed: " + err.Error()
		} else {
			m.statusMsg = "renamed to " + newName
		}
		m.reload()
	case "backspace":
		if len(m.input) > 0 {
			// trim one rune, not one byte
			r := []rune(m.input)
			m.input = string(r[:len(r)-1])
		}
	default:
		// Key.Text holds the printable characters of a key press (empty for
		// non-text keys like arrows / function keys).
		if msg.Text != "" {
			m.input += msg.Text
		}
	}
	return m, nil
}
