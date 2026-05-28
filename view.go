package main

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// geometry holds the screen layout derived purely from terminal size + cursor.
// Both View (for rendering) and the mouse handler (for hit-testing) call
// layout() so the two can never disagree about where a row or column lives.
//
// Two orientations share one struct, picked by `vertical`:
//
//   - HORIZONTAL (vertical=false, m.width >= widthBreakpoint) — the original
//     side-by-side layout. List covers cols [0, dividerStart), divider covers
//     [dividerStart, dividerStart+dividerWidth), preview covers
//     [dividerStart+dividerWidth, m.width). Y-axis fields (topInner /
//     bottomInner / dividerYStart) stay zero.
//
//   - VERTICAL (vertical=true, m.width < widthBreakpoint) — 1-col stacked. Both
//     panes use full m.width (leftInner = m.width). List covers rows
//     [0, topInner), divider covers rows [dividerYStart, dividerYStart+dividerHeight),
//     preview covers rows [previewFirstRow, previewFirstRow+bottomInner). X-axis
//     fields (rightInner / dividerStart) stay zero.
//
// Body fills rows [0, bodyH); status row sits at m.height-1 in both. firstRow
// stays a named field (always 0) so mouse + click callers read the same name
// list/preview rendering does — see PRD §5.3 of docs/prd-responsive-layout.md.
type geometry struct {
	vertical bool // true → 1-col stacked layout; false → 2-col side-by-side

	// axis-X (horizontal mode primary; in vertical mode: leftInner = m.width,
	// rightInner = 0, dividerStart = 0).
	leftInner    int // content columns of the list pane (vertical: of both panes)
	rightInner   int // content columns of the preview pane (horizontal only)
	dividerStart int // first column of the vertical divider strip (horizontal only)

	// axis-Y (vertical mode primary; horizontal mode: all zero).
	topInner      int // content rows of the list pane (vertical only)
	bottomInner   int // content rows of the preview pane (vertical only)
	dividerYStart int // first row of the horizontal divider strip (vertical only)

	// shared
	bodyH           int // body rows (excludes the 1 status row at m.height-1)
	listTop         int // index of the first visible list entry
	firstRow        int // screen Y of the first body row — always 0 (no top border)
	previewFirstRow int // screen Y of the first preview content row (horizontal: 0; vertical: topInner+dividerHeight)
}

// layout picks 2-col or 1-col stacked purely from m.width — `vertical` is NEVER
// stored on the model, so View() and handleMouse() can never read a stale value.
// The threshold (widthBreakpoint=80) is single (no hysteresis) at v1 (D6); the
// drag-mid-flip flush is handled separately in Update's WindowSizeMsg case.
func (m model) layout() geometry {
	bodyH := max(m.height-1, 3) // status(1); body fills the rest

	if m.width < widthBreakpoint {
		// 1-col stacked. Borderless → list/divider/preview share bodyH directly;
		// listTop must be measured against topInner (NOT bodyH), or a long list
		// would scroll the cursor past the bottom of the list pane into the
		// divider/preview band — PRD §5.4 footgun.
		topInner := topInnerHeight(bodyH, m.topRatio)
		return geometry{
			vertical:        true,
			leftInner:       m.width, // both panes use full width
			bodyH:           bodyH,
			topInner:        topInner,
			bottomInner:     bodyH - topInner - dividerHeight,
			dividerYStart:   topInner, // glyph row Y (0-indexed)
			listTop:         m.listTopFor(topInner),
			firstRow:        0,
			previewFirstRow: topInner + dividerHeight,
		}
	}

	// 2-col side-by-side — geometry shape unchanged from the borderless rewrite
	// (PRD docs/prd-middle-divider.md §5.2); only the explicit `vertical: false`
	// + `previewFirstRow: 0` are new, kept here for parity with the vertical
	// branch so a reader does not have to infer zero values.
	leftInner := m.leftInnerWidth()
	return geometry{
		vertical:        false,
		leftInner:       leftInner,
		rightInner:      m.width - leftInner - dividerWidth,
		dividerStart:    leftInner,
		bodyH:           bodyH,
		listTop:         m.listTopFor(bodyH),
		firstRow:        0,
		previewFirstRow: 0,
	}
}

const (
	// minPanelInnerCols is the smallest a pane's content area may shrink to
	// during a divider drag — picked so a name + one space + a size column
	// (the typical row shape in the list pane) still fit at the floor.
	minPanelInnerCols = 14

	// minPanelInnerRows mirrors minPanelInnerCols on the Y axis for the 1-col
	// stacked layout (m.width < widthBreakpoint): each pane keeps at least 4
	// content rows. Below this a pane is too short to be usable beside an
	// agent, and the responsive flip is supposed to make things MORE readable,
	// not less. See PRD docs/prd-responsive-layout.md §5.1 (D8).
	minPanelInnerRows = 4

	// widthBreakpoint is the single threshold (no hysteresis at v1, D6) for
	// switching between 2-col side-by-side and 1-col stacked layouts:
	// m.width < this → vertical. 80 is a round number near lazygit's default
	// (~84) and matches a half-width laptop terminal (~160 cols split 50/50),
	// which is the dominant "lazyexplorer beside an agent" pose this feature
	// is designed for. PRD §1 and §5.1.
	widthBreakpoint = 80

	// Divider geometry — 3 cols total: [pad-left][glyph][pad-right]. The two
	// pad cols widen the drag hit-zone (FR4/D5) without painting a heavier
	// separator. Push these up to 2 each if users complain the bar is hard
	// to grab — see PRD §5.10 (defer).
	dividerPadLeft  = 1
	dividerPadRight = 1
	dividerWidth    = dividerPadLeft + 1 + dividerPadRight // = 3

	// dividerGlyph is the rune painted in the divider's middle column. Light
	// box-drawing weight matches the look of the old RoundedBorder so the
	// migration reads as "border removed, separator kept" rather than a tone
	// shift. Swapping the character is a separate aesthetic decision.
	dividerGlyph = "│"

	// Horizontal-divider geometry for the 1-col stacked layout. dividerHeight
	// is the visible glyph-row count between list and preview. Rows are scarcer
	// than cols (typical 30 rows vs 120 cols), so the strip stays a single row.
	//
	// dividerHitRowsAbove / dividerHitRowsBelow widen the click-target band
	// around the visible glyph row. They are kept at 0 so the "visible width
	// == hit zone" invariant from horizontal mode (3 dedicated cols → 3-col
	// hit zone, no bleed into either pane) carries over to vertical (1
	// dedicated row → 1-row hit zone, no bleed). At width 80+ a single-row
	// click target is ~80+ cells — plenty. If empirical use shows the bar is
	// hard to grab, bump these to 1 BEFORE painting visible pad rows
	// (PRD §5.13 defer).
	dividerHeight       = 1
	dividerHitRowsAbove = 0
	dividerHitRowsBelow = 0

	// dividerHGlyph is the rune painted in every column of the horizontal
	// divider row. Same family as dividerGlyph (light box-drawing) so the two
	// orientations read as the same construct rotated, not two different ideas.
	dividerHGlyph = "─"
)

// topInnerHeight is leftInnerWidth's Y-axis mirror for the 1-col stacked
// layout. topRatio represents the ROW OF THE DIVIDER GLYPH (mirror of
// leftRatio's divider-center semantics in horizontal mode), so:
//
//	dividerCenterY = round(bodyH * topRatio)
//	topInner       = dividerCenterY
//
// dividerHeight is 1 (one visible glyph row, no pad rows — see Constants),
// so no pad-top is subtracted from dividerCenterY. Clamping keeps each pane
// ≥ minPanelInnerRows of content while reserving dividerHeight for the strip.
// On a terminal too short to fit both panes plus the strip, degrade
// best-effort (return the floor) — same defensive discipline as leftInnerWidth.
func topInnerHeight(bodyH int, topRatio float64) int {
	dividerCenterY := int(float64(bodyH)*topRatio + 0.5)
	ti := dividerCenterY // dividerHeight=1, no pad-top to subtract

	hi := bodyH - dividerHeight - minPanelInnerRows // leave room for the preview pane
	if hi < minPanelInnerRows {
		hi = minPanelInnerRows // degenerate tiny terminal: best effort, mirror leftInnerWidth
	}
	if ti < minPanelInnerRows {
		ti = minPanelInnerRows
	}
	if ti > hi {
		ti = hi
	}
	return ti
}

// leftInnerWidth turns the drag-adjustable leftRatio into the list pane's
// content column count. leftRatio represents the COLUMN OF THE DIVIDER GLYPH
// (not the right edge of the left panel — that semantics changed when the
// border went away), so:
//
//	dividerCenter = round(m.width * leftRatio)
//	leftInner     = dividerCenter - dividerPadLeft
//
// Clamping keeps each pane ≥ minPanelInnerCols of content while reserving
// dividerWidth for the separator. On a terminal too narrow to fit both panes
// plus the divider, we degrade best-effort (return the floor) instead of
// panicking — the renderer survives and the user can resize back up.
func (m model) leftInnerWidth() int {
	dividerCenter := int(float64(m.width)*m.leftRatio + 0.5)
	li := dividerCenter - dividerPadLeft

	hi := m.width - dividerWidth - minPanelInnerCols // leave room for the right pane
	if hi < minPanelInnerCols {
		hi = minPanelInnerCols // degenerate tiny terminal: best effort
	}
	if li < minPanelInnerCols {
		li = minPanelInnerCols
	}
	if li > hi {
		li = hi
	}
	return li
}

// View renders the whole screen. In bubbletea v2 the alt-screen and mouse modes
// are declared on the returned View (they are no longer program options), so
// every return path sets them — including the early "loading…" frame, otherwise
// the program would toggle out of the alt screen and drop mouse reporting on the
// first paint before WindowSizeMsg arrives.
//
// Two orientations share one renderer, picked by g.vertical:
//
//   - HORIZONTAL — borderless 2-col side-by-side: list on the left, a single
//     3-col divider (" │ ") down the middle, preview on the right. The entire
//     4 cols + 2 rows of chrome the rounded border used to consume ships back
//     to content (PRD docs/prd-middle-divider.md §5.5).
//
//   - VERTICAL — borderless 1-col stacked: list pane on top (rows=topInner),
//     a single 1-row horizontal divider strip of "─" glyphs at row
//     dividerYStart, preview pane below (rows=bottomInner). The flip is
//     triggered purely by m.width < widthBreakpoint inside layout(); View()
//     never decides orientation itself (PRD docs/prd-responsive-layout.md §5.6).
//
// Each pane is wrapped in a plain Style with Width/Height set to its inner
// dimensions; lipgloss pads short content out so JoinHorizontal / JoinVertical
// align rows even when renderList / renderPreview emits fewer lines than the
// pane's row count.
func (m model) View() tea.View {
	content := "loading…"
	if m.width != 0 && m.height != 0 {
		g := m.layout()

		var body string
		if g.vertical {
			// 1-col stacked. Both panes use g.leftInner (= m.width) for cols;
			// list gets g.topInner rows, preview gets g.bottomInner rows, and
			// dividerHeight rows sit between them as a styled "─" strip.
			list := lipgloss.NewStyle().
				Width(g.leftInner).Height(g.topInner).
				Render(m.renderList(g.leftInner, g.topInner))

			// Horizontal divider strip: dividerHeight (=1) rows of the glyph
			// repeated leftInner times. Same dimStyle as the vertical divider
			// glyph so the two orientations read as one construct rotated.
			dividerRow := dimStyle.Render(strings.Repeat(dividerHGlyph, g.leftInner))
			divider := strings.TrimRight(strings.Repeat(dividerRow+"\n", dividerHeight), "\n")

			preview := lipgloss.NewStyle().
				Width(g.leftInner).Height(g.bottomInner).
				Render(m.renderPreview(g.leftInner))

			body = lipgloss.JoinVertical(lipgloss.Left, list, divider, preview)
		} else {
			// 2-col side-by-side — unchanged from the borderless middle-divider
			// rewrite (PRD docs/prd-middle-divider.md §5.5).
			left := lipgloss.NewStyle().
				Width(g.leftInner).Height(g.bodyH).
				Render(m.renderList(g.leftInner, g.bodyH))

			right := lipgloss.NewStyle().
				Width(g.rightInner).Height(g.bodyH).
				Render(m.renderPreview(g.rightInner))

			// Divider column: bodyH copies of " │ ". Only the glyph carries colDim;
			// the two pad cols stay un-styled so they read as the pane background
			// and double as a wider drag hit-target without painting a heavier line.
			dividerLine := strings.Repeat(" ", dividerPadLeft) +
				dimStyle.Render(dividerGlyph) +
				strings.Repeat(" ", dividerPadRight)
			divider := strings.TrimRight(strings.Repeat(dividerLine+"\n", g.bodyH), "\n")

			body = lipgloss.JoinHorizontal(lipgloss.Top, left, divider, right)
		}
		content = strings.Join([]string{body, m.renderStatus()}, "\n")
	}

	v := tea.NewView(content)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

// renderList draws the left file/folder list with cursor + scrolling. Each
// visible row is drawn by renderEntryRow — the SINGLE source of truth for
// listing-row format (PRD §5.2). G002 will wire folder preview through the
// same routine so the two panes can never drift in format.
func (m model) renderList(w, h int) string {
	if len(m.entries) == 0 {
		return dimStyle.Render(fitWidth("(empty directory)", w))
	}

	top := m.listTopFor(h)
	var b strings.Builder
	for i := top; i < len(m.entries) && i < top+h; i++ {
		b.WriteString(renderEntryRow(m.entries[i], w, i == m.cursor))
		if i < len(m.entries)-1 && i < top+h-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// renderEntryRow draws ONE listing row at w display columns. It is the single
// place a row is formatted, so list pane and folder preview (G002) never
// disagree on row format — see docs/prd-consistent-file-listing.md §5.1.
// active=true marks the cursor row in the list pane; the full-width accent
// highlight from cursorActiveStyle IS the cursor marker. The folder preview
// always passes active=false.
//
// Layout:
//   - dir → name + "/" tô dirStyle (the synthetic ".." keeps no slash, FR2).
//   - file (inactive) → name tô fileStyle (bright headline), humanSize(size)
//     tô dimStyle (muted supporting info, D8/FR9) — eye lands on the name
//     first; the bytes column is metadata, not the headline.
//   - file (active) → whole row uses cursorActiveStyle so name AND size stay
//     legible on the accent background (a dim foreground on accent would be
//     unreadable); the mute rule does NOT apply to the cursor row.
func renderEntryRow(e entry, w int, active bool) string {
	name := e.name
	if e.isDir && e.name != ".." {
		name += "/"
	}
	// Size is shown only for real files. Dirs (incl. "..") get no size: there
	// is no meaningful single number for a directory in this glance UI.
	size := ""
	if !e.isDir {
		size = humanSize(e.size)
	}
	if active {
		// cursorActiveStyle.Width(w) pads the rendered string to exactly w
		// columns so the highlight covers the whole pane width. Plain body
		// (no per-segment styling) keeps name + size on the same accent fg.
		return cursorActiveStyle.Width(w).Render(fitRow(name, size, w))
	}
	if e.isDir {
		// Dirs have no size column → fitRow with empty size returns the bare
		// (possibly truncated) name, then dirStyle paints it whole.
		return dirStyle.Render(fitRow(name, "", w))
	}
	// Inactive file row: split styling — name in fileStyle, size in dimStyle.
	return styleFileRow(name, size, w)
}

// styleFileRow is fitRow's layout for an inactive file row, but with the
// styling split: name tô fileStyle (bright), size tô dimStyle (muted), gap
// between them left unstyled so the panel background shows through cleanly.
// Single helper so the muted-size invariant (D8/FR9) has one source of truth
// — and so fitRow stays a pure plain-string layout helper (the rest of the
// callers and its existing tests do not need to know about styling).
func styleFileRow(name, size string, w int) string {
	if w <= 0 {
		return ""
	}
	if size == "" {
		return fileStyle.Render(fitWidth(name, w))
	}
	nw := lipgloss.Width(name)
	sw := lipgloss.Width(size)
	if nw+1+sw <= w {
		gap := w - nw - sw
		return fileStyle.Render(name) + strings.Repeat(" ", gap) + dimStyle.Render(size)
	}
	// Combined too wide: drop size, keep (or truncate) the name — same FR6
	// priority as fitRow's plain path.
	return fileStyle.Render(fitWidth(name, w))
}

// fitRow lays out name (flush left) and size (flush right) in w display
// columns, returning a plain string (no ANSI) — caller wraps it in a single
// style so escapes never nest.
//
// Priority is name > size (FR6): when both can't fit, the size is dropped
// before the name is truncated. Cases:
//   - w ≤ 0 → "".
//   - size == "" (dirs / "..") → fitWidth(name, w) — no padding pretense.
//   - name + ≥1 space + size ≤ w → name, spaces filling the gap, size at the
//     right edge; total width == w.
//   - otherwise → fitWidth(name, w) (drop size; truncate name with "…" if it
//     still overflows).
//
// All measurements go through lipgloss.Width so CJK / wide glyphs measure as
// the terminal draws them (a tab is already expanded in normalizeText).
func fitRow(name, size string, w int) string {
	if w <= 0 {
		return ""
	}
	if size == "" {
		return fitWidth(name, w)
	}
	nw := lipgloss.Width(name)
	sw := lipgloss.Width(size)
	if nw+1+sw <= w {
		gap := w - nw - sw
		return name + strings.Repeat(" ", gap) + size
	}
	// Combined too wide: drop size, keep (or truncate) the name.
	return fitWidth(name, w)
}

// previewLen returns the count the scroll math should clamp against: when
// the right panel is a folder listing the unit is "entries", otherwise it is
// "preview lines". Folding both into one helper lets previewScroll and
// scrollPreview stay mode-agnostic — they never branch on previewIsDir.
func (m model) previewLen() int {
	if m.previewIsDir {
		return len(m.previewEntries)
	}
	return len(m.preview)
}

// previewScroll returns the clamped top index and body height of the preview
// pane: bodyH is the rows available for preview content, top is the first
// preview line shown. renderPreview (to draw), scrollPreview (to clamp the
// viewport), and previewClick (to hit-test a click on a directory listing) all
// read it, so the screen-row → preview-line mapping can never drift — same
// single-source-of-geometry discipline as layout().
//
// The pane's row count depends on orientation: in 2-col mode the preview
// shares bodyH with the list; in 1-col stacked mode the preview pane has its
// own row budget (bottomInner). Branching here keeps every caller
// (renderPreview / scrollPreview / previewClick) mode-agnostic.
func (m model) previewScroll() (top, bodyH int) {
	g := m.layout()
	if g.vertical {
		bodyH = g.bottomInner
	} else {
		bodyH = g.bodyH
	}
	top = min(m.previewTop, max(0, m.previewLen()-bodyH))
	return top, bodyH
}

// renderPreview draws the right panel: a folder listing (G002) or a file's
// content. The folder branch renders one row per entry through renderEntryRow
// at width w with active=false — the SAME routine the list pane uses (FR1),
// so a row for the same entry is byte-identical in both panes (the list pane's
// cursor row carries cursorActiveStyle, the only allowed visual difference).
func (m model) renderPreview(w int) string {
	top, bodyH := m.previewScroll()

	if m.previewIsDir {
		// Empty folder: dim placeholder, matching the spirit of the list
		// pane's "(empty directory)" (FR7). Same dimStyle so the two read
		// as cousins, even when the word differs.
		if len(m.previewEntries) == 0 {
			return dimStyle.Render(fitWidth("(empty folder)", w))
		}
		var lines []string
		for i := top; i < len(m.previewEntries) && i < top+bodyH; i++ {
			lines = append(lines, renderEntryRow(m.previewEntries[i], w, false))
		}
		return strings.Join(lines, "\n")
	}

	var lines []string
	for i := top; i < len(m.preview) && i < top+bodyH; i++ {
		line := m.preview[i]
		// Markdown lines carry ANSI from glamour and are already wrapped to w;
		// fitWidth's rune-slicing would corrupt the escape sequences, so skip it.
		if !m.previewPreStyled {
			line = fitWidth(line, w)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// renderStatus is the footer: either a mode prompt or the keybind hints.
func (m model) renderStatus() string {
	switch m.mode {
	case modeConfirmDelete:
		sel := m.entries[m.cursor].name
		p := promptStyle.Background(colDanger).Foreground(colSelFg).
			Render(fmt.Sprintf("Delete %q ? (y / n)", sel))
		return p
	case modeRename:
		p := promptStyle.Background(colWarn).Foreground(lipgloss.Color("#000000")).
			Render("Rename: " + m.input + "▏")
		return p
	case modeSearch:
		// Fuzzy-input prompt "/query▏" (the hint bar is intentionally hidden in
		// search). While the recursive walk runs, an "indexing…" chip sits beside
		// the prompt so the empty list reads as "still loading", not "no results".
		p := promptStyle.Background(colAccent).Foreground(colSelFg).
			Render("/" + m.searchQuery + "▏")
		if m.searchIndexing {
			return p + " " + renderingStyle.Render("• indexing…")
		}
		if m.statusMsg != "" {
			return p + " " + dimStyle.Render(m.statusMsg)
		}
		return p
	default:
		hints := "[↑↓/jk/click] move  [enter/l] open  [h/bksp] up  [r] rename  [d] delete  [wheel] scroll  [q] quit"
		status := hints
		if m.statusMsg != "" {
			status = m.statusMsg + dimStyle.Render("   "+hints)
		}
		// While a markdown render is in flight the preview shows the raw source as
		// a placeholder; this chip tells the user the styled version is on its way,
		// so a brief raw→styled transition reads as "formatting", not a glitch.
		if m.pendingWidth > 0 {
			status = renderingStyle.Render("• rendering… ") + status
		}
		return statusBarStyle.Width(m.width).Render(fitWidth(status, m.width-2))
	}
}

// listTopFor computes the scroll offset so the cursor stays visible in h rows.
func (m model) listTopFor(h int) int {
	return max(0, m.cursor-h+1)
}

// fitWidth truncates s to w display columns (rune-aware), padding is left to lipgloss.
func fitWidth(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r))+1 > w {
		r = r[:len(r)-1]
	}
	return string(r) + "…"
}
