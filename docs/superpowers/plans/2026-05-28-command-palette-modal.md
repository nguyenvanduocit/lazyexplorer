# Command Palette + Help → Floating Modal Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Render the command palette (`Ctrl+P`) and full-help overlay (`?`) as a centered floating modal over the still-visible panes (Raycast/Spotlight style), replacing the current preview-pane takeover.

**Architecture:** `View()` builds the full background string (list + divider + real preview + status) exactly as today, then — when `mode ∈ {modeCommandPalette, modeHelp}` — composites a bordered, opaque box centered over it using lipgloss's built-in `Canvas`/`Compositor`/`Layer` (already shipped in `charm.land/lipgloss/v2 v2.0.3`). Input handling (the palette/help state machine in `model.go`/`palette.go`) is unchanged; only rendering moves.

**Tech Stack:** Go 1.26, `charm.land/bubbletea/v2`, `charm.land/lipgloss/v2` (`Canvas`/`Compositor`/`Layer`), `charm.land/bubbles/v2/key`.

**Spec:** `docs/prd-keymap-and-command-palette.md` — D9/D10/D21/D22, FR5/FR12, §5.6/§5.7.

---

## ⚠ Working-tree note (read before any commit)

The working tree currently holds **uncommitted changes from a separate "footer-fix" session** (`view.go`, `theme.go`, `model.go`, several `*_test.go`, several `docs/*`). Every commit in this plan stages **only the named files** — never `git add -A` / `git add .`. Do not commit the footer-fix files. If `git status` shows footer-fix files staged, unstage them before committing.

---

## File Structure

| File | Responsibility | Change |
|------|----------------|--------|
| `theme.go` | palette + styles | **Add** `modalBoxStyle` + sizing constants |
| `view.go` | rendering + geometry | **Add** `overlayCentered`, `renderModal`, `modalSize`; **rename** `renderPalette`→`renderPaletteBody`, `renderHelp`→`renderHelpBody`; **remove** `renderPreviewRegion`; **rewire** `View()` + `renderStatus` palette/help cases |
| `modal_test.go` | modal unit tests | **Create** — `overlayCentered`, `modalSize`, body rendering, View composition |

No changes to `model.go` / `palette.go` (state machine unchanged). No new dependencies (`Canvas`/`Compositor`/`Layer` ship in lipgloss v2.0.3).

---

## Pre-flight

- [ ] **Step 0: Confirm baseline is green.**

Run: `go build -o lazyexplorer . && go vet ./... && go test ./... 2>&1 | grep -vE "telemetry (enabled|api key|post)" | tail -5`
Expected: build + vet clean, tests PASS (this is the footer-fix baseline; if it is red, stop and tell the user — do not build modal work on a red tree).

---

## Task 1: Modal style + sizing constants (`theme.go`)

**Files:**
- Modify: `theme.go` (append to the `var (...)` style block + a new `const (...)` block)
- Test: `modal_test.go`

- [ ] **Step 1: Write the failing test**

Create `modal_test.go`:

```go
package main

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// modalBoxStyle wraps body content in a rounded border + 1-col horizontal
// padding, so its frame is 2 (border) + 2 (padding) = 4 cols and 2 (border)
// rows. modalSize subtracts exactly this, so the values are load-bearing.
func TestModalBoxStyleFrame(t *testing.T) {
	if got := modalBoxStyle.GetHorizontalFrameSize(); got != 4 {
		t.Errorf("modalBoxStyle horizontal frame = %d, want 4 (border 2 + padding 2)", got)
	}
	if got := modalBoxStyle.GetVerticalFrameSize(); got != 2 {
		t.Errorf("modalBoxStyle vertical frame = %d, want 2 (border only)", got)
	}
	// Rendering through it must carry the rounded-corner glyphs.
	out := modalBoxStyle.Render("x")
	if !strings.Contains(out, "╭") || !strings.Contains(out, "╯") {
		t.Errorf("modalBoxStyle.Render missing rounded border: %q", out)
	}
	_ = lipgloss.Width // keep import if unused after edits
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestModalBoxStyleFrame -v 2>&1 | tail -5`
Expected: FAIL — `undefined: modalBoxStyle`.

- [ ] **Step 3: Add the style + constants**

In `theme.go`, append to the existing `var (...)` style block (after `promptStyle`):

```go
	// modalBoxStyle is the floating command-palette / help box: rounded accent
	// border + opaque dark background so it reads as lifted off the panes behind
	// it (D9/D22). One accent, the same colAccent as the cursor row.
	modalBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colAccent).
			Background(lipgloss.Color("#1A1A1A")).
			Foreground(colFg).
			Padding(0, 1)
```

Then add a new `const (...)` block (top-level, near the bottom of the file):

```go
// Modal sizing — OUTER box dims; inner content = outer − modalBoxStyle frame
// (subtracted at runtime in modalSize). Clamped to fit narrow/short terminals.
const (
	modalMargin     = 2  // min screen cols/rows kept around the box
	modalTargetCols = 56 // preferred outer width
	modalTargetRows = 16 // preferred outer height
	modalMinCols    = 24 // floor outer width (degenerate terminal)
	modalMinRows    = 6  // floor outer height
)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -run TestModalBoxStyleFrame -v 2>&1 | tail -5`
Expected: PASS. (If horizontal frame ≠ 4, the `Padding(0, 1)` is the only knob — keep it at 1 col each side.)

- [ ] **Step 5: Commit**

```bash
git add theme.go modal_test.go
git commit -m "feat(modal): add modalBoxStyle + sizing constants"
```

---

## Task 2: `modalSize` — chrome-aware clamp (`view.go`)

**Files:**
- Modify: `view.go` (add `modalSize` method)
- Test: `modal_test.go`

- [ ] **Step 1: Write the failing test**

Append to `modal_test.go`:

```go
func TestModalSizeClamps(t *testing.T) {
	fw := modalBoxStyle.GetHorizontalFrameSize() // 4
	fh := modalBoxStyle.GetVerticalFrameSize()   // 2

	cases := []struct {
		name              string
		w, h              int
		wantInW, wantInH  int
	}{
		// Wide terminal: target wins. outerW=56, outerH=16.
		{"wide", 120, 40, modalTargetCols - fw, modalTargetRows - fh},
		// Narrow (vertical-mode) terminal: width clamps. outerW=60-4=56→min(56,56)=56? no:
		// m.width-margin*2 = 60-4 = 56 = target, so outerW=56, inner=52.
		{"narrow60", 60, 24, 56 - fw, modalTargetRows - fh},
		// Very narrow: floor then never-exceed-screen. m.width=20<modalMinCols=24
		// → outerW=min(max(min(56,16),24),20)=min(24,20)=20, inner=16.
		{"tiny", 20, 10, 20 - fw, (10 - 1) - fh}, // outerH=min(max(min(16,5),6),9)=min(6,9)=6→ wait, see note
	}
	for _, c := range cases {
		m := model{width: c.w, height: c.h}
		gotW, gotH := m.modalSize()
		if gotW != c.wantInW {
			t.Errorf("%s: innerW = %d, want %d", c.name, gotW, c.wantInW)
		}
		if gotH != c.wantInH {
			t.Errorf("%s: innerH = %d, want %d", c.name, gotH, c.wantInH)
		}
	}
}
```

> Compute the expected values by hand from the formula in Step 3 before running — if a case is wrong, fix the *expectation*, not the formula, unless the formula genuinely overflows the screen. The invariant that matters: `innerW + fw ≤ m.width` and `innerH + fh ≤ m.height-1` for every case.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestModalSizeClamps -v 2>&1 | tail -5`
Expected: FAIL — `m.modalSize undefined`.

- [ ] **Step 3: Implement `modalSize`**

Add to `view.go` (near `leftInnerWidth`):

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -run TestModalSizeClamps -v 2>&1 | tail -5`
Expected: PASS. (If the `tiny` case disagrees, recompute its expectation from the formula — the formula is correct by construction; only the hand-computed expectation can be wrong.)

- [ ] **Step 5: Commit**

```bash
git add view.go modal_test.go
git commit -m "feat(modal): modalSize chrome-aware clamp"
```

---

## Task 3: `overlayCentered` — composite box over background (`view.go`)

**Files:**
- Modify: `view.go` (add `overlayCentered`)
- Test: `modal_test.go`

- [ ] **Step 1: Write the failing test**

Append to `modal_test.go`:

```go
func TestOverlayCenteredComposites(t *testing.T) {
	w, h := 40, 12
	// Background: h rows of styled (ANSI) content, padded to full width.
	rowStyle := lipgloss.NewStyle().Background(lipgloss.Color("#333333")).Foreground(colFg)
	var rows []string
	for i := 0; i < h; i++ {
		rows = append(rows, rowStyle.Width(w).Render("bgrow"))
	}
	bg := strings.Join(rows, "\n")
	box := modalBoxStyle.Width(20).Render("hello\nworld")

	out := overlayCentered(bg, box, w, h)
	lines := strings.Split(out, "\n")

	if len(lines) != h {
		t.Fatalf("rows = %d, want %d", len(lines), h)
	}
	for i, ln := range lines {
		if lipgloss.Width(ln) != w {
			t.Errorf("row %d width = %d, want %d", i, lipgloss.Width(ln), w)
		}
	}
	// Top row (above the centered box) keeps background content.
	if !strings.Contains(stripANSI(lines[0]), "bgrow") {
		t.Errorf("row 0 lost background: %q", stripANSI(lines[0]))
	}
	// Some middle row carries the rounded border (the box landed on screen).
	joined := stripANSI(out)
	if !strings.Contains(joined, "╭") || !strings.Contains(joined, "╯") {
		t.Errorf("composited output missing box border")
	}
}

// stripANSI removes SGR escapes for plain-text assertions.
func stripANSI(s string) string {
	var b strings.Builder
	esc := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			esc = true
		case esc && r == 'm':
			esc = false
		case !esc:
			b.WriteRune(r)
		}
	}
	return b.String()
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestOverlayCenteredComposites -v 2>&1 | tail -5`
Expected: FAIL — `undefined: overlayCentered`.

- [ ] **Step 3: Implement `overlayCentered`**

Add to `view.go`:

```go
// overlayCentered draws box centered over bg (a full w×h rendered screen) and
// returns the composited string. The box is centered within the body region
// (rows [0, h-1)) so the status row at h-1 — which carries the modal hints —
// stays visible. Background shows through everywhere the box does not cover
// (D21/D22 — no dim).
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -run TestOverlayCenteredComposites -v 2>&1 | tail -5`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add view.go modal_test.go
git commit -m "feat(modal): overlayCentered compositing helper"
```

---

## Task 4: Rename `renderHelp` → `renderHelpBody` (`view.go`)

**Files:**
- Modify: `view.go` (rename method; logic unchanged)
- Test: existing help tests in `palette_test.go` continue to pass

> The help body content/scroll logic is unchanged — only the name changes (it now fills the modal box instead of the preview pane). The wiring into `View()` happens in Task 7.

- [ ] **Step 1: Rename the function**

In `view.go`, change the signature line:

```go
// renderHelpBody draws the full-help body for the modal: bindings grouped under
// titles, scrolled by helpTop. The group order matches fullHelp; helpLineCount
// counts the same lines so the scroll clamp never overshoots.
func (m model) renderHelpBody(w, h int) string {
```

(Body of the function is unchanged from the current `renderHelp`.)

- [ ] **Step 2: Update the one caller**

In `renderPreviewRegion` (will be removed in Task 7, but keep the tree compiling now), change `m.renderHelp(w, h)` → `m.renderHelpBody(w, h)`.

- [ ] **Step 3: Run build + help tests**

Run: `go build -o lazyexplorer . && go test -run 'Help' ./... 2>&1 | grep -vE "telemetry (enabled|api key|post)" | tail -5`
Expected: build clean, help tests PASS.

> If a test references `renderHelp` by name (grep: `rg 'renderHelp\b' *_test.go`), update it to `renderHelpBody`.

- [ ] **Step 4: Commit**

```bash
git add view.go
git commit -m "refactor(modal): rename renderHelp → renderHelpBody"
```

---

## Task 5: `renderPaletteBody` — prompt at box top + cd error in box (`view.go`)

**Files:**
- Modify: `view.go` (rewrite `renderPalette` → `renderPaletteBody`)
- Test: `modal_test.go`

- [ ] **Step 1: Write the failing test**

Append to `modal_test.go`:

```go
func TestPaletteBodyPromptAtTop(t *testing.T) {
	m := model{
		mode:            modeCommandPalette,
		paletteStage:    0,
		paletteQuery:    "re",
		paletteFiltered: defaultCommands(),
	}
	body := m.renderPaletteBody(50, 10)
	first := stripANSI(strings.Split(body, "\n")[0])
	if !strings.HasPrefix(strings.TrimSpace(first), "> re") {
		t.Errorf("row 0 = %q, want search prompt '> re…'", first)
	}
}

func TestPaletteBodyCdErrorInBox(t *testing.T) {
	cmds := defaultCommands()
	m := model{
		mode:            modeCommandPalette,
		paletteStage:    1, // cd arg input
		paletteFiltered: cmds,
		paletteCursor:   indexOfCmd(cmds, "cd"),
		statusMsg:       "⚠ blocked: outside root",
	}
	body := stripANSI(m.renderPaletteBody(50, 10))
	if !strings.Contains(body, "⚠ blocked: outside root") {
		t.Errorf("cd-stage body missing error message:\n%s", body)
	}
}

// indexOfCmd returns the position of the named command in cmds (test helper).
func indexOfCmd(cmds []Command, name string) int {
	for i, c := range cmds {
		if c.Name == name {
			return i
		}
	}
	return 0
}
```

> Verify the field names against `model.go` (`paletteStage`, `paletteQuery`, `paletteFiltered`, `paletteCursor`, `statusMsg`) and the `Command` struct (`commands.go`: `Name`, `Description`) before running — adjust the literals if a name differs.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run 'TestPaletteBody' -v 2>&1 | tail -10`
Expected: FAIL — `m.renderPaletteBody undefined`.

- [ ] **Step 3: Rewrite `renderPalette` → `renderPaletteBody`**

Replace the current `renderPalette` function in `view.go` with:

```go
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
```

- [ ] **Step 4: Update the caller + run tests**

In `renderPreviewRegion` (removed in Task 7) change `m.renderPalette(w, h)` → `m.renderPaletteBody(w, h)`. Then:

Run: `go build -o lazyexplorer . && go test -run 'TestPaletteBody|Palette' ./... 2>&1 | grep -vE "telemetry (enabled|api key|post)" | tail -8`
Expected: build clean, tests PASS.

> If `palette_test.go` has a `TestPaletteRendersInView`-style test asserting the old preview-pane format (e.g. `▶ ` cursor glyph), it will break — that assertion is superseded by the modal. Update or delete it (it is replaced by `TestModalRendersPaletteInView` in Task 7).

- [ ] **Step 5: Commit**

```bash
git add view.go modal_test.go
git commit -m "feat(modal): renderPaletteBody with prompt at box top + in-box cd error"
```

---

## Task 6: `renderModal` — wrap body in the box (`view.go`)

**Files:**
- Modify: `view.go` (add `renderModal`)
- Test: `modal_test.go`

- [ ] **Step 1: Write the failing test**

Append to `modal_test.go`:

```go
func TestRenderModal(t *testing.T) {
	// Normal mode → no modal.
	if _, ok := (model{mode: modeNormal, width: 100, height: 30}).renderModal(); ok {
		t.Errorf("normal mode should not produce a modal")
	}
	// Palette mode → bordered box.
	m := model{
		mode: modeCommandPalette, width: 100, height: 30,
		paletteFiltered: defaultCommands(),
	}
	box, ok := m.renderModal()
	if !ok {
		t.Fatal("palette mode should produce a modal")
	}
	if !strings.Contains(stripANSI(box), "╭") {
		t.Errorf("modal missing border: %q", stripANSI(box))
	}
	// Box must fit the screen width.
	if lipgloss.Width(box) > m.width {
		t.Errorf("box width %d exceeds screen %d", lipgloss.Width(box), m.width)
	}
	// Help mode → modal too.
	if _, ok := (model{mode: modeHelp, width: 100, height: 30, keymap: defaultKeyMap()}).renderModal(); !ok {
		t.Errorf("help mode should produce a modal")
	}
}
```

> `model{... keymap: defaultKeyMap()}` is required for the help branch because `renderHelpBody` → `fullHelp()` reads `m.keymap`. Confirm `defaultKeyMap` exists in `keys.go`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestRenderModal -v 2>&1 | tail -5`
Expected: FAIL — `m.renderModal undefined`.

- [ ] **Step 3: Implement `renderModal`**

Add to `view.go`:

```go
// renderModal returns the styled, bordered box for the active overlay mode and
// ok=true; in normal mode it returns ok=false (no overlay). modalSize hands the
// body its inner dimensions; modalBoxStyle adds the border + padding frame.
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
```

> `.Width(bw)` pins the box to the clamped inner width so short content does not produce a too-narrow box and the border stays rectangular.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -run TestRenderModal -v 2>&1 | tail -5`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add view.go modal_test.go
git commit -m "feat(modal): renderModal wraps body in modalBoxStyle"
```

---

## Task 7: Wire `View()` to compose the modal; remove `renderPreviewRegion` (`view.go`)

**Files:**
- Modify: `view.go` (`View()` both orientations; delete `renderPreviewRegion`)
- Test: `modal_test.go`

- [ ] **Step 1: Write the failing test**

Append to `modal_test.go`:

```go
func TestModalRendersPaletteInView(t *testing.T) {
	m := model{
		mode: modeCommandPalette, width: 100, height: 30,
		paletteFiltered: defaultCommands(), keymap: defaultKeyMap(),
	}
	// Give it some entries so the background (list pane) has content.
	m.entries = []entry{{name: "alpha.go"}, {name: "beta.go"}}
	out := m.View().Content
	plain := stripANSI(out)
	// The modal border is present (palette is a floating box, not a pane).
	if !strings.Contains(plain, "╭") {
		t.Errorf("View() did not composite the palette modal border")
	}
	// The background list is still visible behind/around the box.
	if !strings.Contains(plain, "alpha.go") {
		t.Errorf("background list pane not visible behind the modal")
	}
}
```

> Confirm `entry` field name (`name`) and that `tea.View` exposes `.Content` (it does in bubbletea v2 — `View()` returns `tea.View{Content: ...}`).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestModalRendersPaletteInView -v 2>&1 | tail -8`
Expected: FAIL — the palette currently renders inside the preview pane (no border), so the `╭` assertion fails.

- [ ] **Step 3a: Stop the preview pane from branching on mode**

In `View()`, the vertical branch currently builds the preview via `m.renderPreviewRegion(g.leftInner, g.bottomInner)` and the horizontal branch via `m.renderPreviewRegion(g.rightInner, g.bodyH)`. Change both to call `m.renderPreview` directly:

Vertical branch:

```go
		preview := lipgloss.NewStyle().
			Width(g.leftInner).Height(g.bottomInner).
			Render(m.renderPreview(g.leftInner))
```

Horizontal branch:

```go
		right := lipgloss.NewStyle().
			Width(g.rightInner).Height(g.bodyH).
			Render(m.renderPreview(g.rightInner))
```

- [ ] **Step 3b: Composite the modal after the status row**

In `View()`, after `content = strings.Join([]string{body, m.renderStatus()}, "\n")` and before `v := tea.NewView(content)`, add:

```go
		// Floating modal overlay (palette / help) drawn OVER the screen.
		if box, ok := m.renderModal(); ok {
			content = overlayCentered(content, box, m.width, m.height)
		}
```

(This stays inside the `if m.width != 0 && m.height != 0 {` block.)

- [ ] **Step 3c: Delete `renderPreviewRegion`**

Remove the now-unused `renderPreviewRegion` function from `view.go` entirely (the doc comment + the func). Nothing else calls it after Step 3a.

- [ ] **Step 4: Run build + the View test + full suite**

Run: `go build -o lazyexplorer . && go vet ./... && go test ./... 2>&1 | grep -vE "telemetry (enabled|api key|post)" | tail -10`
Expected: build + vet clean; `TestModalRendersPaletteInView` PASS; whole suite PASS.

> Expect breakage in any pre-existing test that asserted the palette/help rendered *in the preview pane* (e.g. `TestPaletteRendersInView`, `TestHelpRendersInView` in `palette_test.go`). Those assertions are now wrong by design — update them to assert modal composition (border present, background visible) or delete them in favor of the `modal_test.go` coverage. Do NOT weaken a real assertion to make it pass; replace it with the correct modal assertion.

- [ ] **Step 5: Commit**

```bash
git add view.go modal_test.go palette_test.go
git commit -m "feat(modal): composite palette/help as floating modal in View; drop renderPreviewRegion"
```

---

## Task 8: `renderStatus` palette/help cases → modal short-help (`view.go`)

**Files:**
- Modify: `view.go` (`renderStatus` `modeCommandPalette` + `modeHelp` cases)
- Test: `modal_test.go`

- [ ] **Step 1: Write the failing test**

Append to `modal_test.go`:

```go
func TestStatusBarModalHints(t *testing.T) {
	pal := stripANSI((model{mode: modeCommandPalette, width: 100, height: 30}).renderStatus())
	if !strings.Contains(pal, "enter") || !strings.Contains(pal, "esc") {
		t.Errorf("palette status missing run/close hints: %q", pal)
	}
	// The query/prompt must NOT be in the status bar anymore (it lives in the box).
	if strings.Contains(pal, "▏") {
		t.Errorf("palette status still shows the input caret: %q", pal)
	}
	help := stripANSI((model{mode: modeHelp, width: 100, height: 30}).renderStatus())
	if !strings.Contains(help, "scroll") || !strings.Contains(help, "esc") {
		t.Errorf("help status missing scroll/close hints: %q", help)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestStatusBarModalHints -v 2>&1 | tail -6`
Expected: FAIL — the palette case still renders `"> "+m.paletteQuery+"▏"`, so the `▏` assertion fails.

- [ ] **Step 3: Rewrite the two cases**

In `renderStatus`, replace the `modeCommandPalette` and `modeHelp` cases with:

```go
	case modeCommandPalette:
		// The prompt + command list + any submit error (cd jail-block) live in
		// the modal box now; the status bar just carries the modal short-help.
		return statusBarStyle.Width(m.width).Render(fitWidth(
			"[enter] run   [esc] close   "+dimStyle.Render("[↑↓] move"), m.width-2))
	case modeHelp:
		return statusBarStyle.Width(m.width).Render(fitWidth(
			"[j/k] scroll   [esc] close", m.width-2))
```

- [ ] **Step 4: Run test + full suite**

Run: `go test -run TestStatusBarModalHints -v 2>&1 | tail -6 && go test ./... 2>&1 | grep -vE "telemetry (enabled|api key|post)" | tail -5`
Expected: target test PASS; full suite PASS.

- [ ] **Step 5: Commit**

```bash
git add view.go modal_test.go
git commit -m "feat(modal): status bar shows modal short-help (prompt moved into box)"
```

---

## Task 9: Golden frames + narrow-terminal proof + final gate

**Files:**
- Modify: `modal_test.go` (golden/snapshot frames)
- Test: full suite + manual visual verdict

- [ ] **Step 1: Write snapshot tests for two sizes**

Append to `modal_test.go`:

```go
func TestModalNoOverflow(t *testing.T) {
	for _, sz := range []struct{ w, h int }{{80, 24}, {60, 24}} {
		m := model{
			mode: modeCommandPalette, width: sz.w, height: sz.h,
			paletteFiltered: defaultCommands(), keymap: defaultKeyMap(),
		}
		m.entries = []entry{{name: "x.go"}}
		out := m.View().Content
		for i, ln := range strings.Split(out, "\n") {
			if lipgloss.Width(ln) > sz.w {
				t.Errorf("%dx%d row %d width %d > %d", sz.w, sz.h, i, lipgloss.Width(ln), sz.w)
			}
		}
	}
}
```

- [ ] **Step 2: Run it**

Run: `go test -run TestModalNoOverflow -v 2>&1 | tail -6`
Expected: PASS (proves the chrome-aware clamp holds at the responsive breakpoint).

- [ ] **Step 3: Visual verdict (manual)**

Render palette + help frames to an image and evaluate against design intent (per `docs/CLAUDE.md` "UI tests"). Use the project's existing `zz_dump`/freeze path or the `oh-my-claudecode:visual-verdict` skill. Check: box centered, rounded accent border, search prompt at box top, background panes visible around the box, no row overflow at 80×24 and 60×24.

> If the project's `/test` skill documents the canonical golden-snapshot mechanism (color-profile determinism, freeze invocation), follow it for any committed golden fixture rather than hand-rolling one.

- [ ] **Step 4: Final gate**

Run: `go build -o lazyexplorer . && go vet ./... && go test ./... && go test -race ./... 2>&1 | grep -vE "telemetry (enabled|api key|post)" | tail -8`
Expected: all green.

- [ ] **Step 5: Commit**

```bash
git add modal_test.go
git commit -m "test(modal): no-overflow snapshot at 80x24 and 60x24"
```

- [ ] **Step 6: Reconcile the PRD task checkboxes**

In `docs/prd-keymap-and-command-palette.md` §7, tick `T8`–`T14` (the modal tasks) and flip the header `Rev 2026-05-28b` note from "implementation pending" to shipped with the verify-gate result. Commit:

```bash
git add docs/prd-keymap-and-command-palette.md
git commit -m "docs(prd): mark command-palette modal shipped"
```

---

## Self-Review checklist (run before handing to execution)

- **Spec coverage:** D9/D10 (modal) → Tasks 6–7; D21 (Canvas/Compositor/Layer) → Task 3; D22 (no dim) → Task 3 (`overlayCentered` no bg restyle); modal sizing/clamp → Tasks 1–2; search-at-top → Task 5; cd-error-in-box → Task 5; status short-help → Task 8; narrow-terminal AC → Tasks 2 & 9; "panes visible behind modal" AC → Tasks 7 & 9. ✅
- **No placeholders:** every code step shows full code; every run step shows the command + expected result. ✅
- **Type consistency:** `modalSize` returns `(innerW, innerH)`; `renderModal` consumes them and calls `renderPaletteBody(bw, bh)` / `renderHelpBody(bw, bh)`; `overlayCentered(bg, box, w, h)` is called in `View()` with `(content, box, m.width, m.height)`. Names match across tasks. ✅
- **Working-tree safety:** every commit stages only named files (never `-A`); footer-fix's uncommitted files are never staged. ✅
