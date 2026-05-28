package main

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
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

// modalSize returns the INNER content dimensions handed to renderPaletteBody /
// renderHelpBody. The OUTER box (inner + modalBoxStyle frame) is clamped to fit
// m.width/height minus a margin each side, with a floor — same best-effort
// discipline as leftInnerWidth: a narrow (<80, vertical) or short terminal
// shrinks the box but it never overflows. Subtracting the frame here (not in the
// caller) is why a bordered+padded box still fits at width 60.
func (m model) modalSize() (innerW, innerH int) {
	fw := modalBoxStyle.GetHorizontalFrameSize()
	fh := modalBoxStyle.GetVerticalFrameSize()
	outerW := min(modalTargetCols, m.width-modalMargin*2)
	outerH := min(modalTargetRows, (m.height-1)-modalMargin*2) // -1: status row
	outerW = min(max(outerW, modalMinCols), m.width)           // floor, then never exceed screen
	outerH = min(max(outerH, modalMinRows), m.height-1)
	return max(1, outerW-fw), max(1, outerH-fh)
}

// overlayCentered draws box centered over bg (a full w×h rendered screen) and
// returns the composited string. The box is centered within the body region
// (rows [0, h-1)) so the status row at h-1 — which carries the modal hints —
// stays visible. Background shows through everywhere the box does not cover
// (D21/D22 — no dim): the bg layer at z=0 paints every cell, the box layer at
// z=1 only the cells it occupies.
func overlayCentered(bg, box string, w, h int) string {
	boxW, boxH := lipgloss.Width(box), lipgloss.Height(box)
	cx := max(0, (w-boxW)/2)
	cy := max(0, ((h-1)-boxH)/2)
	canvas := lipgloss.NewCanvas(w, h)
	return canvas.Compose(lipgloss.NewCompositor(
		lipgloss.NewLayer(bg).Z(0),
		lipgloss.NewLayer(box).X(cx).Y(cy).Z(1),
	)).Render()
}

// renderModal returns the styled, bordered box for the active overlay mode and
// ok=true; in normal mode it returns ok=false (no overlay). modalSize hands the
// body its inner dimensions; modalBoxStyle adds the border + padding frame.
// .Width(bw) pins the box to the clamped inner width so short content does not
// produce a too-narrow box and the border stays rectangular.
func (m model) renderModal() (string, bool) {
	bw, bh := m.modalSize()
	switch m.mode {
	case modeCommandPalette:
		return modalBoxStyle.Width(bw).Render(m.renderPaletteBody(bw, bh)), true
	case modeHelp:
		return modalBoxStyle.Width(bw).Render(m.renderHelpBody(bw, bh)), true
	default:
		return "", false
	}
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

			// Horizontal divider strip glows toward the focused pane: ▔ rides the
			// top edge (hugging the list pane above) when the list is focused, ▁
			// rides the bottom (hugging the preview below) when the preview is —
			// the vertical analogue of the half-block glow, sized as an eighth-block
			// so its visual weight matches the 1-col half-block in 2-col mode.
			glyph := "▔"
			if m.focusPane == focusPreview {
				glyph = "▁"
			}
			dividerRow := dividerFocusStyle.Render(strings.Repeat(glyph, g.leftInner))
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

			// Divider column: bodyH copies of the 3-col strip. The middle glyph stays
			// colDim; the pad col hugging the focused pane carries a half-block accent
			// (▐ from the list side, ▌ from the preview side) so focus reads at the
			// pane boundary without a chip. The other pad col stays blank — still the
			// wider drag hit-target, no heavier line painted.
			padL := strings.Repeat(" ", dividerPadLeft)
			padR := strings.Repeat(" ", dividerPadRight)
			if m.focusPane == focusList {
				padL = strings.Repeat(" ", dividerPadLeft-1) + dividerFocusStyle.Render("▐")
			} else {
				padR = dividerFocusStyle.Render("▌") + strings.Repeat(" ", dividerPadRight-1)
			}
			dividerLine := padL + dimStyle.Render(dividerGlyph) + padR
			divider := strings.TrimRight(strings.Repeat(dividerLine+"\n", g.bodyH), "\n")

			body = lipgloss.JoinHorizontal(lipgloss.Top, left, divider, right)
		}
		content = strings.Join([]string{body, m.renderStatus()}, "\n")

		// Floating modal overlay (palette / help) drawn OVER the screen.
		if box, ok := m.renderModal(); ok {
			content = overlayCentered(content, box, m.width, m.height)
		}
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
	listFocused := m.focusPane == focusList
	var b strings.Builder
	for i := top; i < len(m.entries) && i < top+h; i++ {
		b.WriteString(renderEntryRow(m.entries[i], w, i == m.cursor, listFocused))
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
// listFocused tunes the cursor-row highlight (D10/FR12): when the list pane
// holds focus the row uses the accent background; when focus is on the preview
// the row dims to colDim, so the cursor still shows which file the preview
// reflects without competing with the focused pane for the accent. listFocused
// is only consulted on the active row — inactive rows ignore it.
//
// Layout:
//   - dir → name + "/" tô dirStyle (the synthetic ".." keeps no slash, FR2).
//   - file (inactive) → name tô fileStyle (bright headline), humanSize(size)
//     tô dimStyle (muted supporting info, D8/FR9) — eye lands on the name
//     first; the bytes column is metadata, not the headline.
//   - file (active) → whole row uses one style so name AND size stay legible on
//     the background (a dim foreground on accent would be unreadable); the mute
//     rule does NOT apply to the cursor row.
func renderEntryRow(e entry, w int, active, listFocused bool) string {
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
		// (no per-segment styling) keeps name + size on the same fg.
		st := cursorActiveStyle
		if !listFocused {
			// Focus is on the preview: keep the cursor visible but soften it to
			// colDim so it reinforces the divider glow (which carries the accent
			// toward the preview) instead of competing with it (D10).
			// Background returns a copy — cursorActiveStyle is not mutated.
			st = cursorActiveStyle.Background(colDim)
		}
		return st.Width(w).Render(fitRow(name, size, w))
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
	// In wrap mode the vertical scroller iterates the wrapped visual lines; in
	// nowrap (and before any reflow) it iterates the logical lines directly —
	// previewDisplay equals m.preview in nowrap, so this is the same count.
	if m.previewWrap && m.previewDisplay != nil {
		return len(m.previewDisplay)
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
			// Preview rows are never the list's cursor row → active=false, which
			// short-circuits the listFocused dim path; pass false for honesty.
			lines = append(lines, renderEntryRow(m.previewEntries[i], w, false, false))
		}
		return strings.Join(lines, "\n")
	}

	// Non-scrollable previews (markdown via glamour, image/placeholder lines):
	// markdown carries ANSI already wrapped to width → render verbatim; plain
	// placeholders get fitWidth. No horizontal window (nothing overflows).
	if !m.previewScrollable {
		var lines []string
		for i := top; i < len(m.preview) && i < top+bodyH; i++ {
			line := m.preview[i]
			if !m.previewPreStyled {
				line = fitWidth(line, w)
			}
			lines = append(lines, line)
		}
		return strings.Join(lines, "\n")
	}

	// Scrollable preview (plain text or code).
	if m.previewWrap {
		// Wrapped: previewDisplay holds the hard-wrapped visual lines, each ≤ w.
		var lines []string
		for i := top; i < len(m.previewDisplay) && i < top+bodyH; i++ {
			line := m.previewDisplay[i]
			if !hasANSI(line) { // plain wrapped line → rune-fit; code carries ANSI → verbatim
				line = fitWidth(line, w)
			}
			lines = append(lines, line)
		}
		return strings.Join(lines, "\n")
	}

	// Nowrap: a horizontal window over the logical lines + ‹/› edge indicators.
	end := min(top+bodyH, len(m.preview))
	return m.renderHWindow(m.preview[top:end], w)
}

// renderHWindow renders nowrap scrollable lines: each is sliced to the window
// [previewHScroll, previewHScroll+contentW) with ‹/› edge indicators. The
// indicator columns are reserved by a GLOBAL condition (does any visible line
// overflow that side?) so content width is uniform across lines and code stays
// column-aligned instead of jittering line to line (D9).
func (m model) renderHWindow(visible []string, w int) string {
	left := m.previewHScroll
	showLeft := left > 0

	provW := w
	if showLeft {
		provW--
	}
	anyRightCut := false
	for _, line := range visible {
		if lineWidth(line)-left > provW {
			anyRightCut = true
			break
		}
	}

	contentW := w
	if showLeft {
		contentW--
	}
	if anyRightCut {
		contentW--
	}
	if contentW < 1 {
		contentW = 1 // degenerate narrow pane: best effort
	}

	var out []string
	for _, line := range visible {
		var b strings.Builder
		if showLeft {
			b.WriteString(dimStyle.Render("‹"))
		}
		b.WriteString(hSlice(line, left, contentW))
		if anyRightCut {
			if lineWidth(line)-left > contentW {
				b.WriteString(dimStyle.Render("›"))
			} else {
				b.WriteByte(' ') // reserved column, this line not cut
			}
		}
		out = append(out, b.String())
	}
	return strings.Join(out, "\n")
}

// hSlice extracts display columns [left, left+width) from a line: ANSI-aware for
// code (TruncateLeft drops the left cols preserving SGR, then Truncate caps the
// right), rune-aware for plain (CJK width via lipgloss.Width).
func hSlice(line string, left, width int) string {
	if width <= 0 {
		return ""
	}
	if hasANSI(line) {
		return ansi.Truncate(ansi.TruncateLeft(line, left, ""), width, "")
	}
	r := []rune(line)
	r = r[runePrefixWidth(r, left):]
	return string(r[:runePrefixWidth(r, width)])
}

// runePrefixWidth returns the count of leading runes whose cumulative display
// width is ≤ w (CJK/wide-glyph aware). Used to slice plain lines on column
// boundaries.
func runePrefixWidth(r []rune, w int) int {
	if w <= 0 {
		return 0
	}
	acc, n := 0, 0
	for _, c := range r {
		cw := lipgloss.Width(string(c))
		if acc+cw > w {
			break
		}
		acc += cw
		n++
	}
	return n
}

// wrapLine hard-wraps one preview line to w columns → ≥1 visual lines. Code
// lines carry ANSI → ANSI-aware hard-wrap so escape sequences survive the split;
// plain lines → rune-slice (CJK-aware). An empty line yields one empty visual
// line so blank rows are preserved.
func wrapLine(line string, w int) []string {
	if w <= 0 || line == "" {
		return []string{line}
	}
	if hasANSI(line) {
		return strings.Split(ansi.Hardwrap(line, w, false), "\n")
	}
	var out []string
	r := []rune(line)
	for len(r) > 0 {
		cut := runePrefixWidth(r, w)
		if cut == 0 { // a single glyph wider than w: emit it alone to make progress
			cut = 1
		}
		out = append(out, string(r[:cut]))
		r = r[cut:]
	}
	if len(out) == 0 {
		out = []string{""}
	}
	return out
}

// lineWidth is the display-column width of a (possibly ANSI) line.
func lineWidth(line string) int {
	if hasANSI(line) {
		return ansi.StringWidth(line)
	}
	return lipgloss.Width(line)
}

// hasANSI reports whether a line carries an escape sequence (code/markdown) and
// so must be handled ANSI-aware rather than rune-sliced.
func hasANSI(s string) bool { return strings.Contains(s, "\x1b") }

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
	case modeCommandPalette:
		// The prompt + command list + any submit error (cd jail-block) live in
		// the modal box now; the status bar just carries the modal short-help.
		return statusBarStyle.Width(m.width).Render(fitWidth(
			"[enter] run   [esc] close   "+dimStyle.Render("[↑↓] move"), m.width-2))
	case modeHelp:
		return statusBarStyle.Width(m.width).Render(fitWidth(
			"[j/k] scroll   [esc] close", m.width-2))
	default:
		// Focus is signalled by the divider glow (renderList/View draw it toward
		// m.focusPane) plus the dimmed cursor row — not by a status-bar chip — so
		// the footer is just the focus-relevant hints, sourced from the keymap so
		// help text never drifts from the bindings.
		hints := renderShortHelp(m.shortHelp())
		status := hints
		if m.statusMsg != "" {
			status = m.statusMsg + dimStyle.Render("   "+hints)
		}
		// The render spinner lives in a fixed 2-col slot at the RIGHT edge: a
		// reserved slot (space + glyph while rendering, two spaces when idle) keeps
		// the hints flush-left at a constant width, so an in-flight render never
		// shifts or clips them. Prepending the indicator used to reflow the whole
		// bar — that was the footer flicker (bug-footer-flicker). The braille frame
		// advances via spinnerTickMsg while pendingWidth > 0.
		contentW := m.width - 2 // statusBarStyle Padding(0,1) eats one col each side
		slot := "  "
		if m.pendingWidth > 0 {
			slot = " " + renderingStyle.Render(spinnerFrames[m.spinnerFrame%len(spinnerFrames)])
		}
		left := fitWidth(status, contentW-2)
		pad := strings.Repeat(" ", max(0, contentW-2-lipgloss.Width(left)))
		return statusBarStyle.Width(m.width).Render(left + pad + slot)
	}
}

// renderPaletteBody draws the modal box content: the search/arg prompt at the
// box top (Raycast-style — the prompt lives in the box, not the status bar),
// then the filtered command list. In the cd arg stage the body shows the
// command description plus any submit error (e.g. a jail-block) next to the
// input the user is correcting.
func (m model) renderPaletteBody(w, h int) string {
	var lines []string

	// Row 0: search prompt (stage 0) or cd-arg prompt (stage 1).
	if m.paletteStage == 0 {
		lines = append(lines, promptStyle.Background(colAccent).Foreground(colSelFg).
			Render(fitWidth("> "+m.paletteQuery+"▏", w)))
	} else {
		sel := m.paletteFiltered[m.paletteCursor]
		lines = append(lines, promptStyle.Background(colAccent).Foreground(colSelFg).
			Render(fitWidth(sel.Name+" > "+m.paletteSecondaryInput+"▏", w)))
	}
	lines = append(lines, "") // blank between prompt and body

	// Stage 1: description + any submit error, both inside the box.
	if m.paletteStage == 1 {
		sel := m.paletteFiltered[m.paletteCursor]
		lines = append(lines, dimStyle.Render(fitWidth(sel.Description, w)))
		if m.statusMsg != "" {
			lines = append(lines, "", dimStyle.Render(fitWidth(m.statusMsg, w)))
		}
		return strings.Join(lines, "\n")
	}

	// Stage 0: filtered command rows; cursor row = full-width accent.
	if len(m.paletteFiltered) == 0 {
		lines = append(lines, dimStyle.Render(fitWidth("(no matching commands)", w)))
		return strings.Join(lines, "\n")
	}
	bodyRows := h - len(lines) // rows left under the prompt + blank
	for i, c := range m.paletteFiltered {
		if i >= bodyRows {
			break
		}
		row := c.Name + "  — " + c.Description
		if i == m.paletteCursor {
			lines = append(lines, cursorActiveStyle.Width(w).Render(fitWidth(row, w)))
		} else {
			lines = append(lines, fileStyle.Render(fitWidth(row, w)))
		}
	}
	return strings.Join(lines, "\n")
}

// renderHelpBody draws the full-help body for the modal: bindings grouped under
// titles, scrolled by helpTop. The group order matches fullHelp; helpLineCount
// counts the same lines so the scroll clamp never overshoots.
func (m model) renderHelpBody(w, h int) string {
	titles := []string{"Navigation", "Preview", "Mutation", "Modes", "Misc"}
	var lines []string
	for gi, group := range m.fullHelp() {
		title := ""
		if gi < len(titles) {
			title = titles[gi]
		}
		lines = append(lines, renderingStyle.Render(title))
		for _, b := range group {
			hb := b.Help()
			lines = append(lines, fitWidth(fmt.Sprintf("  %-12s  %s", hb.Key, hb.Desc), w))
		}
		lines = append(lines, "") // blank separator between groups
	}
	start := min(max(0, m.helpTop), len(lines))
	end := min(start+h, len(lines))
	return strings.Join(lines[start:end], "\n")
}

// renderShortHelp joins key bindings into a "[key] desc" hint string for the
// status bar.
func renderShortHelp(bs []key.Binding) string {
	parts := make([]string, 0, len(bs))
	for _, b := range bs {
		hb := b.Help()
		parts = append(parts, "["+hb.Key+"] "+hb.Desc)
	}
	return strings.Join(parts, "  ")
}

// shortHelp returns the status-bar bindings for the current focus (FR14). The
// help text comes straight from the keymap, so it can never drift from the
// actual bindings.
func (m model) shortHelp() []key.Binding {
	km := m.keymap
	if m.focusPane == focusList {
		return []key.Binding{
			km.MoveDown, km.FocusToggle, km.OpenEntry, km.GoUp,
			km.Rename, km.Delete,
			km.CommandPalette, km.FullHelp, km.Quit,
		}
	}
	// focusPreview. For a scrollable preview (plain/code) surface the hscroll +
	// wrap keys; in wrap mode only the toggle is relevant (FR12). Markdown/image/
	// folder previews are not scrollable, so those keys are omitted.
	base := []key.Binding{km.PreviewScrollDown, km.FocusToggle, km.PreviewJumpTop, km.PreviewJumpBottom, km.PreviewHalfPageDown}
	if m.previewScrollable {
		if m.previewWrap {
			base = append(base, km.PreviewToggleWrap)
		} else {
			base = append(base, km.PreviewScrollRight, km.PreviewHScrollHalfRight, km.PreviewHScrollReset, km.PreviewToggleWrap)
		}
	}
	return append(base, km.Back, km.CommandPalette, km.FullHelp, km.Quit)
}

// fullHelp returns the bindings grouped for the help overlay (FR15). Group order
// matches the titles in renderHelp: Navigation, Preview, Mutation, Modes, Misc.
func (m model) fullHelp() [][]key.Binding {
	km := m.keymap
	return [][]key.Binding{
		{km.MoveUp, km.MoveDown, km.GoTop, km.GoBottom, km.OpenEntry, km.GoUp},
		{km.PreviewScrollUp, km.PreviewScrollDown, km.PreviewHalfPageUp, km.PreviewHalfPageDown, km.PreviewJumpTop, km.PreviewJumpBottom, km.PreviewScrollLeft, km.PreviewScrollRight, km.PreviewHScrollHalfLeft, km.PreviewHScrollHalfRight, km.PreviewHScrollReset, km.PreviewToggleWrap},
		{km.Rename, km.Delete},
		{km.FocusToggle, km.Search, km.CommandPalette, km.FullHelp, km.Back},
		{km.Quit},
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
