# Responsive Layout Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Cho lazyexplorer tự chuyển 2-col ↔ 1-col stacked layout dựa trên `m.width < 80`, mirror lazygit; thêm Y-drag divider trong stacked mode; preserve user-set ratios qua mode flip.

**Architecture:** Branched `layout()` với `vertical bool` trên `geometry`. View và `handleMouse` đọc cùng flag để rẽ nhánh — single-source-of-geometry. State mới chỉ thêm `topRatio float64` (mirror `leftRatio`) và `lastVertical bool` (chỉ phục vụ drag-mid-flip detection). Tham chiếu PRD: `docs/prd-responsive-layout.md`.

**Tech Stack:** Go 1.x · `charmbracelet/bubbletea` · `charmbracelet/lipgloss` · `charmbracelet/glamour`

---

## Preconditions

- Repository không có commit nào (greenfield). **Trước Task 1**, engineer cần `git add . && git commit -m "chore: initial import"` để có baseline cho các commit theo từng task.
- Verify gate (chạy ở mọi step "verify"): `go build -o lazyexplorer . && go vet ./... && go test ./...`
- Tests sống cùng source theo Go convention (`*_test.go` trong cùng package `main`); hot-path file hiện có: `resize_test.go`, `previewclick_test.go`, `update_markdown_test.go`, `markdown_test.go`, `watch_test.go`, `fs_test.go`, `preview_test.go`, `zz_dump_test.go`.

## File Structure

| File | Trách nhiệm | Hành động |
|---|---|---|
| `view.go` | Geometry, constants, layout(), View(), previewScroll | Modify: thêm constants, mở rộng `geometry`, `topOuterHeight`, branch `layout()`, branch `View()`, branch `previewScroll` |
| `model.go` | State, Update, handleMouse, previewBodyWidth, previewClick, setLeftFromX | Modify: thêm `topRatio`+`lastVertical` fields, init trong `newModel`, branch `previewBodyWidth`, sửa `previewClick`, thêm `setTopFromY`, branch `handleMouse`, mode-flip flush trong `WindowSizeMsg` |
| `resize_test.go` | Tests cho WindowSizeMsg + drag + mode flip | Modify: thêm cases mode flip, state preservation, drag-mid-flip |
| `previewclick_test.go` | Tests cho click hit-test trong preview pane | Modify: thêm cases stacked Y-axis hit-test + `previewFirstRow` semantics |
| `update_markdown_test.go` | Tests cho markdown reflow async | Modify: thêm cases mode flip kéo theo reflow |
| `zz_dump_test.go` | Visual frame dump (gated) | Modify: dump 2 frame cho cả 2 orientation |

Không file mới. Không animation file. `fs.go`/`theme.go`/`main.go` không thay đổi.

---

## Task 1: Constants & default state

**Files:**
- Modify: `view.go` (cạnh `minPanelCols` ở line ~38)
- Modify: `model.go` (struct ở line ~45–83; `newModel` ở line ~85–89)
- Test: `resize_test.go`

PRD references: §5.1, §5.2, D1, D3, D8.

- [ ] **Step 1.1: Write the failing test**

Thêm vào `resize_test.go`:

```go
func TestConstants_ResponsiveLayout(t *testing.T) {
    if minPanelRows != 4 {
        t.Errorf("minPanelRows = %d, want 4", minPanelRows)
    }
    if widthBreakpoint != 80 {
        t.Errorf("widthBreakpoint = %d, want 80", widthBreakpoint)
    }
}

func TestNewModel_DefaultTopRatio(t *testing.T) {
    m := newModel(t.TempDir())
    if m.topRatio != 0.33 {
        t.Errorf("topRatio = %v, want 0.33", m.topRatio)
    }
}
```

- [ ] **Step 1.2: Run tests to verify they fail**

```
go test -v -run "TestConstants_ResponsiveLayout|TestNewModel_DefaultTopRatio" .
```

Expected: FAIL với "undefined: minPanelRows" và "undefined: widthBreakpoint" và "m.topRatio undefined".

- [ ] **Step 1.3: Add constants to view.go**

Trong `view.go`, ngay sau `const minPanelCols = 16`:

```go
const (
    // minPanelRows mirrors minPanelCols for the Y axis. Floor cho stacked pane:
    // 1 border-top + 2 nội dung tối thiểu + 1 border-bottom. Dưới mức này panel
    // rỗng tới khó dùng.
    minPanelRows = 4

    // widthBreakpoint là ngưỡng width để switch sang 1-col stacked.
    // `m.width < widthBreakpoint` → stacked; `>=` → side-by-side (lazygit-style).
    widthBreakpoint = 80
)
```

- [ ] **Step 1.4: Add topRatio + lastVertical to model struct**

Trong `model.go`, thêm vào struct `model` (cạnh `leftRatio float64`):

```go
    leftRatio float64 // left panel width as a fraction of total; drag-adjustable
    topRatio  float64 // list pane height as a fraction of body; drag-adjustable in stacked mode

    dragging     bool // true while the user is dragging the panel divider
    lastVertical bool // remembers prior layout orientation; used to flush dragging on mode flip
```

- [ ] **Step 1.5: Init topRatio in newModel**

```go
func newModel(root string) model {
    m := model{root: root, cwd: root, leftRatio: 0.38, topRatio: 0.33}
    m.reload()
    return m
}
```

- [ ] **Step 1.6: Run tests to verify pass**

```
go test -v -run "TestConstants_ResponsiveLayout|TestNewModel_DefaultTopRatio" .
```

Expected: PASS.

- [ ] **Step 1.7: Run full verify gate**

```
go build -o lazyexplorer . && go vet ./... && go test ./...
```

Expected: all green (existing tests + 2 new ones).

- [ ] **Step 1.8: Commit**

```
git add view.go model.go resize_test.go
git commit -m "feat(layout): add constants and topRatio state for responsive mode"
```

---

## Task 2: Extend `geometry` struct

**Files:**
- Modify: `view.go` (`geometry` struct ở line ~13–20)
- Test: `resize_test.go`

PRD reference: §5.3.

- [ ] **Step 2.1: Write the failing test**

Thêm vào `resize_test.go`:

```go
func TestGeometry_HasVerticalAndPaneFields(t *testing.T) {
    var g geometry
    // Compile-time checks via direct field access — failing build is the assertion.
    _ = g.vertical
    _ = g.topOuter
    _ = g.bottomOuter
    _ = g.innerH2
    _ = g.previewFirstRow
}
```

- [ ] **Step 2.2: Run to verify it fails to compile**

```
go test -v -run TestGeometry_HasVerticalAndPaneFields .
```

Expected: build error `g.vertical undefined` (et al).

- [ ] **Step 2.3: Extend the geometry struct**

Trong `view.go`, thay struct hiện có:

```go
// geometry holds the screen layout derived purely from terminal size + cursor.
// Both View (for rendering) and the mouse handler (for hit-testing) call
// layout() so the two can never disagree about where a row lives.
type geometry struct {
    vertical bool // true → 1-col stacked; false → 2-col side-by-side

    // 2-col (vertical == false) — y như trước
    leftOuter  int // total cols của left pane (incl border)
    rightOuter int

    // 1-col (vertical == true)
    topOuter    int // total rows của list pane (incl border)
    bottomOuter int // total rows của preview pane (incl border)

    // shared
    leftInner       int // 2-col: cols trong left pane; 1-col: cols trong cả hai pane (= m.width-2)
    innerH          int // 2-col: rows trong pane; 1-col: rows trong list pane
    innerH2         int // 1-col only: rows trong preview pane (0 ở 2-col)
    listTop         int // index của entry đầu tiên visible trong list
    firstRow        int // screen Y của entry đầu trong list (luôn = 1)
    previewFirstRow int // screen Y của row đầu trong preview content (2-col: 1; 1-col: topOuter+1)
}
```

(Cũ chỉ có `leftOuter, rightOuter, innerH, listTop, firstRow, leftInner` — giữ semantics cũ, thêm field mới.)

- [ ] **Step 2.4: Run test to verify pass**

```
go test -v -run TestGeometry_HasVerticalAndPaneFields .
```

Expected: PASS.

- [ ] **Step 2.5: Run verify gate**

`layout()` chưa populate field mới → các test render hiện có vẫn pass vì view code chưa đọc field mới. Build phải xanh.

```
go build -o lazyexplorer . && go vet ./... && go test ./...
```

Expected: all green (compile + existing behaviors unchanged).

- [ ] **Step 2.6: Commit**

```
git add view.go resize_test.go
git commit -m "feat(layout): extend geometry struct with stacked-mode fields"
```

---

## Task 3: `topOuterHeight` helper

**Files:**
- Modify: `view.go` (đặt cạnh `leftOuterWidth` ở line ~44–57)
- Test: `resize_test.go`

PRD reference: §5.5.

- [ ] **Step 3.1: Write failing tests**

Thêm vào `resize_test.go`:

```go
func TestTopOuterHeight_Default(t *testing.T) {
    // bodyH=23, ratio=0.33 → 7.59 → round 8
    if got := topOuterHeight(23, 0.33); got != 8 {
        t.Errorf("topOuterHeight(23, 0.33) = %d, want 8", got)
    }
}

func TestTopOuterHeight_ClampMin(t *testing.T) {
    // ratio bé → clamp lên minPanelRows
    if got := topOuterHeight(20, 0.01); got != minPanelRows {
        t.Errorf("topOuterHeight(20, 0.01) = %d, want %d", got, minPanelRows)
    }
}

func TestTopOuterHeight_ClampMax(t *testing.T) {
    // ratio lớn → clamp xuống bodyH - minPanelRows để chừa cho preview
    bodyH := 20
    if got := topOuterHeight(bodyH, 0.99); got != bodyH-minPanelRows {
        t.Errorf("topOuterHeight(%d, 0.99) = %d, want %d", bodyH, got, bodyH-minPanelRows)
    }
}

func TestTopOuterHeight_DegenerateTinyBody(t *testing.T) {
    // bodyH < 2*minPanelRows: best-effort, không panic, trả minPanelRows.
    if got := topOuterHeight(5, 0.33); got != minPanelRows {
        t.Errorf("topOuterHeight(5, 0.33) = %d, want %d (best-effort)", got, minPanelRows)
    }
}
```

- [ ] **Step 3.2: Run to verify failures**

```
go test -v -run TestTopOuterHeight .
```

Expected: FAIL with `undefined: topOuterHeight`.

- [ ] **Step 3.3: Implement helper**

Trong `view.go`, ngay sau `leftOuterWidth`:

```go
// topOuterHeight converts the drag-adjustable topRatio into concrete rows for
// the list pane in 1-col stacked mode, clamped so both panes keep at least
// minPanelRows. Mirror của leftOuterWidth cho trục Y. Ratio (không phải số rows)
// được lưu để giữ proportional khi terminal resize height.
func topOuterHeight(bodyH int, topRatio float64) int {
    to := int(float64(bodyH)*topRatio + 0.5)
    hi := bodyH - minPanelRows
    if hi < minPanelRows {
        hi = minPanelRows // degenerate tiny height: best effort
    }
    if to < minPanelRows {
        to = minPanelRows
    }
    if to > hi {
        to = hi
    }
    return to
}
```

- [ ] **Step 3.4: Run tests to verify pass**

```
go test -v -run TestTopOuterHeight .
```

Expected: 4 PASS.

- [ ] **Step 3.5: Run verify gate**

```
go build -o lazyexplorer . && go vet ./... && go test ./...
```

Expected: all green.

- [ ] **Step 3.6: Commit**

```
git add view.go resize_test.go
git commit -m "feat(layout): add topOuterHeight helper with clamping"
```

---

## Task 4: Branch `layout()` for vertical mode

**Files:**
- Modify: `view.go` (`layout()` ở line ~22–34)
- Test: `resize_test.go`

PRD reference: §5.4.

- [ ] **Step 4.1: Write failing tests**

Thêm vào `resize_test.go`:

```go
func TestLayout_Vertical_AtNarrowWidth(t *testing.T) {
    m := newModel(t.TempDir())
    m.width, m.height = 70, 24
    g := m.layout()
    if !g.vertical {
        t.Fatalf("vertical=false at width=70; want true")
    }
    if g.leftInner != 68 {
        t.Errorf("leftInner = %d, want 68 (m.width-2)", g.leftInner)
    }
    if g.previewFirstRow != g.topOuter+1 {
        t.Errorf("previewFirstRow = %d, want topOuter+1 = %d", g.previewFirstRow, g.topOuter+1)
    }
    if g.topOuter+g.bottomOuter != 23 {
        t.Errorf("topOuter+bottomOuter = %d+%d, want 23 (body height)",
            g.topOuter, g.bottomOuter)
    }
}

func TestLayout_Horizontal_AtWideWidth(t *testing.T) {
    m := newModel(t.TempDir())
    m.width, m.height = 120, 30
    g := m.layout()
    if g.vertical {
        t.Fatalf("vertical=true at width=120; want false")
    }
    if g.previewFirstRow != 1 {
        t.Errorf("previewFirstRow = %d, want 1 in 2-col", g.previewFirstRow)
    }
    if g.leftOuter+g.rightOuter != 120 {
        t.Errorf("leftOuter+rightOuter = %d+%d, want 120", g.leftOuter, g.rightOuter)
    }
}

func TestLayout_StrictLessThanBreakpoint(t *testing.T) {
    m := newModel(t.TempDir())
    m.height = 24

    m.width = 79
    if !m.layout().vertical {
        t.Errorf("width=79 should be vertical")
    }

    m.width = 80
    if m.layout().vertical {
        t.Errorf("width=80 should be horizontal (strict <)")
    }

    m.width = 81
    if m.layout().vertical {
        t.Errorf("width=81 should be horizontal")
    }
}
```

- [ ] **Step 4.2: Run to verify failures**

```
go test -v -run TestLayout_ .
```

Expected: FAIL — current layout() doesn't set `vertical`/`previewFirstRow`/`topOuter`/`bottomOuter`.

- [ ] **Step 4.3: Branch `layout()`**

Thay toàn bộ `layout()` trong `view.go`:

```go
func (m model) layout() geometry {
    bodyHeight := max(m.height-1, 3) // status(1); body fills the rest

    if m.width < widthBreakpoint {
        // 1-col stacked: list trên, preview dưới
        topOuter := topOuterHeight(bodyHeight, m.topRatio)
        return geometry{
            vertical:        true,
            leftOuter:       m.width,
            rightOuter:      0,
            topOuter:        topOuter,
            bottomOuter:     bodyHeight - topOuter,
            leftInner:       m.width - 2,
            innerH:          topOuter - 2,
            innerH2:         bodyHeight - topOuter - 2,
            listTop:         m.listTopFor(topOuter - 2),
            firstRow:        1,
            previewFirstRow: topOuter + 1,
        }
    }

    // 2-col side-by-side (unchanged behavior)
    leftOuter := m.leftOuterWidth()
    innerH := bodyHeight - 2
    return geometry{
        vertical:        false,
        leftOuter:       leftOuter,
        rightOuter:      m.width - leftOuter,
        leftInner:       leftOuter - 2,
        innerH:          innerH,
        listTop:         m.listTopFor(innerH),
        firstRow:        1,
        previewFirstRow: 1,
    }
}
```

- [ ] **Step 4.4: Run new tests to verify pass**

```
go test -v -run TestLayout_ .
```

Expected: 3 PASS.

- [ ] **Step 4.5: Run verify gate**

```
go build -o lazyexplorer . && go vet ./... && go test ./...
```

Expected: existing 2-col tests still PASS (unchanged behavior); new 1-col tests PASS.

- [ ] **Step 4.6: Commit**

```
git add view.go resize_test.go
git commit -m "feat(layout): branch layout() for vertical stacked mode"
```

---

## Task 5: Branch `View()` for stacked render

**Files:**
- Modify: `view.go` (`View()` ở line ~59–83)
- Test: `resize_test.go`

PRD reference: §5.6.

- [ ] **Step 5.1: Write failing test**

Thêm vào `resize_test.go`:

```go
func TestView_StackedHasJoinVerticalStructure(t *testing.T) {
    m := newModel(t.TempDir())
    m.width, m.height = 70, 24
    out := m.View()

    // Stacked layout: list pane top border at row 0, preview pane top border
    // somewhere in the middle (after topOuter rows of list pane). Cả hai pane
    // cùng width nên ký tự ╭/┌/└/╯ ở rìa trái mỗi pane.
    lines := strings.Split(out, "\n")
    if len(lines) < 24 {
        t.Fatalf("got %d lines, want >= 24", len(lines))
    }

    // Row 0 phải là top border của list pane
    if !strings.ContainsAny(string([]rune(lines[0])[0]), "╭┌") {
        t.Errorf("line 0 starts with %q, want a border corner", lines[0])
    }
}

func TestView_HorizontalRendersTwoPanes(t *testing.T) {
    m := newModel(t.TempDir())
    m.width, m.height = 120, 30
    out := m.View()
    lines := strings.Split(out, "\n")
    if len(lines) < 30 {
        t.Fatalf("got %d lines, want >= 30", len(lines))
    }
    // Row 0 phải chứa 2 vertical separators (one per pane)
    barCount := strings.Count(lines[0], "─") + strings.Count(lines[0], "━")
    if barCount < 20 {
        t.Errorf("line 0 has %d horizontal bars, want >= 20 (two pane tops)", barCount)
    }
}
```

- [ ] **Step 5.2: Run to verify failures**

```
go test -v -run "TestView_Stacked|TestView_Horizontal" .
```

Expected: Stacked test FAIL — current View() always uses `JoinHorizontal`. Horizontal test maybe PASS already.

- [ ] **Step 5.3: Branch View()**

Thay toàn bộ `View()` trong `view.go`:

```go
func (m model) View() string {
    if m.width == 0 || m.height == 0 {
        return "loading…"
    }
    g := m.layout()

    var body string
    if g.vertical {
        // 1-col stacked: list trên, preview dưới, cùng width.
        top := panelBorder(true).
            Width(g.leftInner).Height(g.innerH).
            Render(m.renderList(g.leftInner, g.innerH))
        bottom := panelBorder(false).
            Width(g.leftInner).Height(g.innerH2).
            Render(m.renderPreview(g.leftInner))
        body = lipgloss.JoinVertical(lipgloss.Left, top, bottom)
    } else {
        // 2-col side-by-side (unchanged).
        left := panelBorder(true).
            Width(g.leftInner).Height(g.innerH).
            Render(m.renderList(g.leftInner, g.innerH))
        right := panelBorder(false).
            Width(g.rightOuter-2).Height(g.innerH).
            Render(m.renderPreview(g.rightOuter-2))
        body = lipgloss.JoinHorizontal(lipgloss.Top, left, right)
    }
    return strings.Join([]string{body, m.renderStatus()}, "\n")
}
```

- [ ] **Step 5.4: Run new tests to verify pass**

```
go test -v -run "TestView_Stacked|TestView_Horizontal" .
```

Expected: 2 PASS.

- [ ] **Step 5.5: Run verify gate**

```
go build -o lazyexplorer . && go vet ./... && go test ./...
```

Expected: all green. Existing 2-col View tests unchanged behavior (path went through `else` branch).

- [ ] **Step 5.6: Commit**

```
git add view.go resize_test.go
git commit -m "feat(layout): branch View() to JoinVertical for stacked mode"
```

---

## Task 6: Branch `previewBodyWidth`

**Files:**
- Modify: `model.go` (`previewBodyWidth` ở line ~224–227)
- Test: `update_markdown_test.go`

PRD reference: §5.7.

- [ ] **Step 6.1: Write failing test**

Thêm vào `update_markdown_test.go`:

```go
func TestPreviewBodyWidth_Stacked(t *testing.T) {
    m := newModel(t.TempDir())
    m.width, m.height = 70, 24
    if got := m.previewBodyWidth(); got != 68 {
        t.Errorf("stacked previewBodyWidth = %d, want 68 (m.width-2)", got)
    }
}

func TestPreviewBodyWidth_Horizontal(t *testing.T) {
    m := newModel(t.TempDir())
    m.width, m.height = 120, 30
    g := m.layout()
    want := g.rightOuter - 2
    if got := m.previewBodyWidth(); got != want {
        t.Errorf("horizontal previewBodyWidth = %d, want %d", got, want)
    }
}
```

- [ ] **Step 6.2: Run to verify failures**

```
go test -v -run TestPreviewBodyWidth_ .
```

Expected: Stacked test FAIL — current `previewBodyWidth` returns `g.rightOuter - 2` luôn, và `rightOuter=0` khi stacked → `-2`.

- [ ] **Step 6.3: Branch the helper**

Thay `previewBodyWidth` trong `model.go`:

```go
// previewBodyWidth returns the content columns of the preview panel, matching
// the body width View() renders into, so glamour wraps to the real draw area.
// Trong 2-col đó là right pane inner width; trong 1-col stacked, bottom pane
// chiếm full m.width nên dùng leftInner (= m.width-2).
func (m model) previewBodyWidth() int {
    g := m.layout()
    if g.vertical {
        return g.leftInner
    }
    return g.rightOuter - 2
}
```

- [ ] **Step 6.4: Run tests to verify pass**

```
go test -v -run TestPreviewBodyWidth_ .
```

Expected: 2 PASS.

- [ ] **Step 6.5: Run verify gate**

```
go build -o lazyexplorer . && go vet ./... && go test ./...
```

Expected: all green.

- [ ] **Step 6.6: Commit**

```
git add model.go update_markdown_test.go
git commit -m "feat(layout): branch previewBodyWidth to match stacked-mode pane"
```

---

## Task 7: Branch `previewScroll`

**Files:**
- Modify: `view.go` (`previewScroll` ở line ~126–130)
- Test: `previewclick_test.go`

PRD reference: §5.10.

- [ ] **Step 7.1: Write failing test**

Thêm vào `previewclick_test.go`:

```go
func TestPreviewScroll_StackedUsesInnerH2(t *testing.T) {
    m := newModel(t.TempDir())
    m.width, m.height = 70, 24
    m.preview = make([]string, 100) // ensure scroll has range
    _, bodyH := m.previewScroll()
    g := m.layout()
    if bodyH != g.innerH2 {
        t.Errorf("stacked previewScroll bodyH = %d, want %d (innerH2)", bodyH, g.innerH2)
    }
}

func TestPreviewScroll_HorizontalUsesInnerH(t *testing.T) {
    m := newModel(t.TempDir())
    m.width, m.height = 120, 30
    m.preview = make([]string, 100)
    _, bodyH := m.previewScroll()
    g := m.layout()
    if bodyH != g.innerH {
        t.Errorf("horizontal previewScroll bodyH = %d, want %d (innerH)", bodyH, g.innerH)
    }
}
```

- [ ] **Step 7.2: Run to verify failures**

```
go test -v -run TestPreviewScroll_ .
```

Expected: Stacked test FAIL — current `previewScroll` returns `m.layout().innerH` unconditionally.

- [ ] **Step 7.3: Branch the helper**

Thay `previewScroll` trong `view.go`:

```go
// previewScroll returns the clamped top index and body height of the preview
// pane: bodyH là số rows preview content có thể chiếm, top là dòng preview
// đầu tiên hiển thị. Trong 2-col bodyH = pane height = innerH; trong 1-col
// stacked bodyH = bottom pane content rows = innerH2.
func (m model) previewScroll() (top, bodyH int) {
    g := m.layout()
    if g.vertical {
        bodyH = g.innerH2
    } else {
        bodyH = g.innerH
    }
    top = min(m.previewTop, max(0, len(m.preview)-bodyH))
    return top, bodyH
}
```

- [ ] **Step 7.4: Run tests to verify pass**

```
go test -v -run TestPreviewScroll_ .
```

Expected: 2 PASS.

- [ ] **Step 7.5: Run verify gate**

```
go build -o lazyexplorer . && go vet ./... && go test ./...
```

Expected: all green. Existing preview render tests unchanged (path went through `else`).

- [ ] **Step 7.6: Commit**

```
git add view.go previewclick_test.go
git commit -m "feat(layout): branch previewScroll bodyH between innerH and innerH2"
```

---

## Task 8: `previewClick` uses `previewFirstRow`

**Files:**
- Modify: `model.go` (`previewClick` ở line ~437–476)
- Test: `previewclick_test.go`

PRD reference: §5.10.

- [ ] **Step 8.1: Write failing test**

Thêm vào `previewclick_test.go`:

```go
func TestPreviewClick_Stacked_ClicksFolderListing(t *testing.T) {
    // Setup: stacked mode, cursor trên một folder → preview là folder listing.
    tmp := t.TempDir()
    sub := filepath.Join(tmp, "sub")
    if err := os.Mkdir(sub, 0o755); err != nil {
        t.Fatal(err)
    }
    if err := os.WriteFile(filepath.Join(sub, "child.txt"), []byte("hi"), 0o644); err != nil {
        t.Fatal(err)
    }

    m := newModel(tmp)
    m.width, m.height = 70, 30
    // Cursor đặt lên "sub" (entries dirs-first sorted; "sub" là entry đầu).
    m.cursor = 0
    m.refreshPreview()

    g := m.layout()
    // Click vào dòng đầu tiên CỦA preview pane — = g.previewFirstRow (= topOuter+1).
    nm, _ := m.handleMouse(tea.MouseMsg{
        Button: tea.MouseButtonLeft,
        Action: tea.MouseActionPress,
        X:      5,
        Y:      g.previewFirstRow, // first row of preview content
    })
    m2 := nm.(model)
    // After click, descend() should have run and cwd should now be inside sub.
    if !strings.HasSuffix(m2.cwd, "sub") {
        t.Errorf("cwd = %q, want suffix /sub (click should have descended)", m2.cwd)
    }
}
```

- [ ] **Step 8.2: Run to verify failure**

```
go test -v -run TestPreviewClick_Stacked_ClicksFolderListing .
```

Expected: FAIL — current `previewClick` uses `g.firstRow` (=1) làm anchor, mà trong stacked Y=topOuter+1 (vd 9) sẽ tính row = 9 - 1 = 8 — vượt innerH2 hoặc trỏ sai dòng.

- [ ] **Step 8.3: Switch anchor to previewFirstRow**

Trong `previewClick` (`model.go`), tìm dòng:

```go
    row := y - g.firstRow
```

Đổi thành:

```go
    row := y - g.previewFirstRow
```

(Chỉ một thay đổi. `g.previewFirstRow` đã được `layout()` populate đúng cho cả 2 mode.)

- [ ] **Step 8.4: Run test to verify pass**

```
go test -v -run TestPreviewClick_Stacked_ClicksFolderListing .
```

Expected: PASS.

- [ ] **Step 8.5: Run verify gate**

```
go build -o lazyexplorer . && go vet ./... && go test ./...
```

Expected: existing previewClick tests (2-col, Y=row 1 click) still PASS vì `previewFirstRow=1` ở 2-col mode.

- [ ] **Step 8.6: Commit**

```
git add model.go previewclick_test.go
git commit -m "fix(layout): previewClick anchors row to previewFirstRow not firstRow"
```

---

## Task 9: `setTopFromY` helper

**Files:**
- Modify: `model.go` (đặt cạnh `setLeftFromX` ở line ~418–423)
- Test: `resize_test.go`

PRD reference: §5.9.

- [ ] **Step 9.1: Write failing test**

Thêm vào `resize_test.go`:

```go
func TestSetTopFromY_UpdatesRatio(t *testing.T) {
    m := newModel(t.TempDir())
    m.width, m.height = 70, 24
    // bodyHeight = max(24-1, 3) = 23. Y=11 → ratio = 12/23 ≈ 0.5217
    m.setTopFromY(11)
    want := float64(12) / float64(23)
    if math.Abs(m.topRatio-want) > 1e-9 {
        t.Errorf("topRatio = %v, want %v", m.topRatio, want)
    }
}

func TestSetTopFromY_GuardsZeroBody(t *testing.T) {
    m := newModel(t.TempDir())
    m.width, m.height = 70, 0 // bodyHeight clamped to 3 by max()
    before := m.topRatio
    m.setTopFromY(0)
    // bodyHeight=3 nên Y=0 → (0+1)/3 = 0.333 — vẫn cập nhật, không panic.
    if m.topRatio == before {
        t.Errorf("expected update with bodyH=3, got unchanged %v", m.topRatio)
    }
}
```

(Cần import `math` trong test file nếu chưa có.)

- [ ] **Step 9.2: Run to verify failure**

```
go test -v -run TestSetTopFromY .
```

Expected: FAIL with `m.setTopFromY undefined`.

- [ ] **Step 9.3: Add helper**

Trong `model.go`, ngay sau `setLeftFromX`:

```go
// setTopFromY pins the Y-divider under the cursor: row y becomes the list
// pane's bottom border, so topOuter = y+1. Lưu dưới dạng ratio (không phải số
// rows tuyệt đối) để split giữ proportional khi terminal resize height.
// Mirror của setLeftFromX cho trục Y.
func (m *model) setTopFromY(y int) {
    bodyH := max(m.height-1, 3)
    if bodyH <= 0 {
        return
    }
    m.topRatio = float64(y+1) / float64(bodyH)
}
```

- [ ] **Step 9.4: Run tests to verify pass**

```
go test -v -run TestSetTopFromY .
```

Expected: 2 PASS.

- [ ] **Step 9.5: Run verify gate**

```
go build -o lazyexplorer . && go vet ./... && go test ./...
```

Expected: all green.

- [ ] **Step 9.6: Commit**

```
git add model.go resize_test.go
git commit -m "feat(layout): add setTopFromY helper mirroring setLeftFromX"
```

---

## Task 10: Branch `handleMouse` for vertical hit-test

**Files:**
- Modify: `model.go` (`handleMouse` ở line ~341–412)
- Test: `resize_test.go`, `previewclick_test.go`

PRD reference: §5.8. **Đây là task lớn nhất; chia nhỏ steps.**

- [ ] **Step 10.1: Write failing tests for stacked behavior**

Thêm vào `resize_test.go`:

```go
func TestHandleMouse_Stacked_YDividerPressStartsDrag(t *testing.T) {
    m := newModel(t.TempDir())
    m.width, m.height = 70, 24
    g := m.layout()
    // Press tại row đáy của list pane (= topOuter-1) → bắt đầu drag.
    nm, _ := m.handleMouse(tea.MouseMsg{
        Button: tea.MouseButtonLeft,
        Action: tea.MouseActionPress,
        X:      10, Y: g.topOuter - 1,
    })
    m2 := nm.(model)
    if !m2.dragging {
        t.Errorf("expected dragging=true after press on Y-divider")
    }
}

func TestHandleMouse_Stacked_MotionUpdatesTopRatio(t *testing.T) {
    m := newModel(t.TempDir())
    m.width, m.height = 70, 24
    m.dragging = true
    bodyH := max(m.height-1, 3) // = 23
    want := float64(12) / float64(bodyH)
    nm, _ := m.handleMouse(tea.MouseMsg{Action: tea.MouseActionMotion, X: 5, Y: 11})
    m2 := nm.(model)
    if math.Abs(m2.topRatio-want) > 1e-9 {
        t.Errorf("topRatio = %v, want %v", m2.topRatio, want)
    }
}

func TestHandleMouse_Stacked_WheelOnTopPaneMovesCursor(t *testing.T) {
    tmp := t.TempDir()
    for _, n := range []string{"a.txt", "b.txt", "c.txt"} {
        os.WriteFile(filepath.Join(tmp, n), nil, 0o644)
    }
    m := newModel(tmp)
    m.width, m.height = 70, 24
    m.cursor = 0
    g := m.layout()
    // Wheel down inside top pane (Y < topOuter) → cursor moves to 1.
    nm, _ := m.handleMouse(tea.MouseMsg{
        Button: tea.MouseButtonWheelDown,
        Action: tea.MouseActionPress,
        X: 5, Y: g.topOuter - 1,
    })
    m2 := nm.(model)
    if m2.cursor != 1 {
        t.Errorf("cursor = %d, want 1 after wheel down in top pane", m2.cursor)
    }
}

func TestHandleMouse_Stacked_WheelOnBottomPaneScrollsPreview(t *testing.T) {
    m := newModel(t.TempDir())
    m.width, m.height = 70, 24
    m.preview = make([]string, 50) // populate so scroll has range
    m.previewTop = 0
    g := m.layout()
    nm, _ := m.handleMouse(tea.MouseMsg{
        Button: tea.MouseButtonWheelDown,
        Action: tea.MouseActionPress,
        X: 5, Y: g.topOuter + 2, // safely inside bottom pane
    })
    m2 := nm.(model)
    if m2.previewTop == 0 {
        t.Errorf("previewTop unchanged after wheel down in bottom pane; want > 0")
    }
}
```

- [ ] **Step 10.2: Run to verify failures**

```
go test -v -run "TestHandleMouse_Stacked" .
```

Expected: All 4 FAIL — current `handleMouse` uses X-axis only.

- [ ] **Step 10.3: Rewrite handleMouse with branched hit-test**

Thay toàn bộ `handleMouse` trong `model.go`:

```go
// handleMouse maps clicks and wheel events onto the panes using the same
// geometry the renderer uses, so hit-testing can never drift from the layout.
// 2-col mode: trục X (overLeft / X-divider). 1-col stacked: trục Y (overTop / Y-divider).
// g.vertical là switch duy nhất rẽ tất cả các nhánh.
func (m model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
    g := m.layout()

    // Motion & release: chung — chỉ axis của setX/setY là khác.
    switch msg.Action {
    case tea.MouseActionMotion:
        if m.dragging {
            if g.vertical {
                m.setTopFromY(msg.Y)
            } else {
                m.setLeftFromX(msg.X)
            }
        }
        return m, nil
    case tea.MouseActionRelease:
        m.dragging = false
        // Reflow xảy ra ở Update's tail syncMarkdown khi dragging=false.
        return m, nil
    }

    // Press: divider drag detection theo axis.
    if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress {
        if g.vertical {
            // Y-divider sống ở row topOuter-1 (đáy list pane) và row topOuter (đỉnh preview pane).
            if msg.Y == g.topOuter-1 || msg.Y == g.topOuter {
                m.dragging = true
                m.setTopFromY(msg.Y)
                return m, nil
            }
        } else {
            // X-divider y như trước.
            if msg.Y >= 1 && (msg.X == g.leftOuter-1 || msg.X == g.leftOuter) {
                m.dragging = true
                m.setLeftFromX(msg.X)
                return m, nil
            }
        }
    }

    // Pane hit-test: trục đổi theo orientation.
    overList := false
    if g.vertical {
        overList = msg.Y < g.topOuter
    } else {
        overList = msg.X < g.leftOuter
    }

    switch msg.Button {
    case tea.MouseButtonWheelUp:
        if overList {
            if m.cursor > 0 {
                m.cursor--
                m.refreshPreview()
            }
        } else {
            m.scrollPreview(-3)
        }
    case tea.MouseButtonWheelDown:
        if overList {
            if m.cursor < len(m.entries)-1 {
                m.cursor++
                m.refreshPreview()
            }
        } else {
            m.scrollPreview(3)
        }
    case tea.MouseButtonLeft:
        if msg.Action != tea.MouseActionPress {
            return m, nil
        }
        if !overList {
            m.previewClick(msg.Y, g)
            return m, nil
        }
        row := msg.Y - g.firstRow
        if row < 0 || row >= g.innerH {
            return m, nil
        }
        idx := g.listTop + row
        if idx < 0 || idx >= len(m.entries) {
            return m, nil
        }
        if idx == m.cursor && m.entries[idx].isDir {
            m.descend()
        } else {
            m.cursor = idx
            m.refreshPreview()
        }
    }
    return m, nil
}
```

- [ ] **Step 10.4: Run new tests to verify pass**

```
go test -v -run "TestHandleMouse_Stacked" .
```

Expected: 4 PASS.

- [ ] **Step 10.5: Run full mouse test suite**

```
go test -v -run "TestHandleMouse|TestPreviewClick|TestSetLeft|TestSetTop" .
```

Expected: existing 2-col handleMouse tests still PASS (path went through `else` branches).

- [ ] **Step 10.6: Run verify gate**

```
go build -o lazyexplorer . && go vet ./... && go test ./...
```

Expected: all green.

- [ ] **Step 10.7: Commit**

```
git add model.go resize_test.go previewclick_test.go
git commit -m "feat(layout): branch handleMouse hit-test by orientation"
```

---

## Task 11: WindowSizeMsg flushes drag on mode flip

**Files:**
- Modify: `model.go` (`Update` ở line ~305–308)
- Test: `resize_test.go`

PRD reference: §5.11.

- [ ] **Step 11.1: Write failing test**

Thêm vào `resize_test.go`:

```go
func TestUpdate_WindowSizeFlush_DraggingOnModeFlip(t *testing.T) {
    m := newModel(t.TempDir())
    m.width, m.height = 120, 30
    m.dragging = true
    m.lastVertical = false

    // Resize qua threshold → vertical=true → flip → dragging phải = false.
    nm, _ := m.Update(tea.WindowSizeMsg{Width: 70, Height: 30})
    m2 := nm.(model)
    if m2.dragging {
        t.Errorf("dragging should be flushed on mode flip; got true")
    }
    if !m2.lastVertical {
        t.Errorf("lastVertical should be true after flip to vertical")
    }
}

func TestUpdate_WindowSizeFlush_NoFlushWithoutFlip(t *testing.T) {
    m := newModel(t.TempDir())
    m.width, m.height = 120, 30
    m.dragging = true
    m.lastVertical = false

    // Resize trong cùng 2-col mode → không flip → dragging giữ nguyên.
    nm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
    m2 := nm.(model)
    if !m2.dragging {
        t.Errorf("dragging should persist when mode does not flip; got false")
    }
}
```

- [ ] **Step 11.2: Run to verify failures**

```
go test -v -run TestUpdate_WindowSizeFlush .
```

Expected: First test FAIL — current Update doesn't flush dragging.

- [ ] **Step 11.3: Add flip-detect in WindowSizeMsg case**

Trong `Update` (`model.go`), tìm case hiện tại:

```go
    case tea.WindowSizeMsg:
        m.width, m.height = msg.Width, msg.Height
        // Width is now known (or changed) — the tail syncMarkdown renders the
        // deferred markdown / reflows it to the new width.
```

Thay bằng:

```go
    case tea.WindowSizeMsg:
        m.width, m.height = msg.Width, msg.Height
        // Width vừa đổi → kiểm tra layout có flip giữa 2-col / 1-col không.
        // Flip đổi axis của divider; drag đang chạy ở axis cũ phải bị huỷ.
        // Tail syncMarkdown sẽ reflow markdown sang width pane mới của mode mới.
        newVertical := m.width < widthBreakpoint
        if newVertical != m.lastVertical {
            m.dragging = false
        }
        m.lastVertical = newVertical
```

- [ ] **Step 11.4: Run tests to verify pass**

```
go test -v -run TestUpdate_WindowSizeFlush .
```

Expected: 2 PASS.

- [ ] **Step 11.5: Run verify gate**

```
go build -o lazyexplorer . && go vet ./... && go test ./...
```

Expected: all green.

- [ ] **Step 11.6: Commit**

```
git add model.go resize_test.go
git commit -m "feat(layout): flush dragging on responsive mode flip"
```

---

## Task 12: Markdown reflow on mode flip — integration test

**Files:**
- Test only: `update_markdown_test.go`

PRD reference: §5.7, FR7. Không code mới — verify rằng cơ chế hiện có cộng với Task 6 đủ để trigger re-render khi mode flip.

- [ ] **Step 12.1: Write the integration test**

Thêm vào `update_markdown_test.go`:

```go
func TestUpdate_MarkdownReflowsOnModeFlip(t *testing.T) {
    // Setup: 2-col mode, một file .md selected, đã render xong ở widthA.
    tmp := t.TempDir()
    mdPath := filepath.Join(tmp, "doc.md")
    if err := os.WriteFile(mdPath, []byte("# Header\n\nbody text\n"), 0o644); err != nil {
        t.Fatal(err)
    }
    m := newModel(tmp)
    m.mdStyle = "notty" // deterministic; không phụ thuộc terminal probe
    m.width, m.height = 120, 30

    // Force render đồng bộ cho widthA bằng cách trigger syncMarkdown's Cmd.
    m.refreshPreview()
    widthA := m.previewBodyWidth()
    cmd := m.syncMarkdown()
    if cmd == nil {
        t.Fatal("expected syncMarkdown to return Cmd at widthA")
    }
    msg := cmd().(markdownRenderedMsg)
    nm, _ := m.Update(msg)
    m = nm.(model)
    if !m.previewPreStyled {
        t.Fatal("expected previewPreStyled=true after render at widthA")
    }

    // Resize qua threshold → vertical=true → previewBodyWidth thay đổi → syncMarkdown
    // phải trả Cmd non-nil cho width mới.
    nm, batched := m.Update(tea.WindowSizeMsg{Width: 70, Height: 30})
    m = nm.(model)
    if batched == nil {
        t.Fatal("expected non-nil Cmd batch from Update after resize")
    }
    widthB := m.previewBodyWidth()
    if widthA == widthB {
        t.Fatalf("expected previewBodyWidth to change on mode flip; both = %d", widthA)
    }
    // mdPendingWidth phải = widthB (render đã được dispatched).
    if m.mdPendingWidth != widthB {
        t.Errorf("mdPendingWidth = %d, want %d (in-flight render at new width)",
            m.mdPendingWidth, widthB)
    }
}
```

- [ ] **Step 12.2: Run to verify it passes**

```
go test -v -run TestUpdate_MarkdownReflowsOnModeFlip .
```

Expected: PASS — Task 6 đã branch `previewBodyWidth` đúng; cơ chế reconcile ở tail Update tự kích.

- [ ] **Step 12.3: Run verify gate**

```
go build -o lazyexplorer . && go vet ./... && go test ./...
```

Expected: all green.

- [ ] **Step 12.4: Commit**

```
git add update_markdown_test.go
git commit -m "test(layout): verify markdown re-renders on responsive mode flip"
```

---

## Task 13: Race-safety test

**Files:**
- Test only: `update_markdown_test.go`

- [ ] **Step 13.1: Write race-safety test**

Thêm vào `update_markdown_test.go`:

```go
func TestUpdate_NoRace_ResizeSpamWithMarkdownRender(t *testing.T) {
    // Smoke test: spam resize qua threshold + markdown render concurrent.
    // Test runner phải chạy với -race flag để có giá trị; không race ⇒ PASS.
    tmp := t.TempDir()
    if err := os.WriteFile(filepath.Join(tmp, "a.md"), []byte("# A\nbody\n"), 0o644); err != nil {
        t.Fatal(err)
    }
    m := newModel(tmp)
    m.mdStyle = "notty"
    m.width, m.height = 120, 30
    m.refreshPreview()

    widths := []int{120, 70, 90, 60, 100, 50, 120}
    for _, w := range widths {
        nm, cmd := m.Update(tea.WindowSizeMsg{Width: w, Height: 30})
        m = nm.(model)
        if cmd != nil {
            // Drain bất kỳ markdownRenderedMsg nào để verify cmd thực sự chạy
            // không panic / không race với syncMarkdown ở vòng kế.
            for _, c := range []tea.Cmd{cmd} {
                if c == nil {
                    continue
                }
                msg := c()
                if mdMsg, ok := msg.(markdownRenderedMsg); ok {
                    nm, _ := m.Update(mdMsg)
                    m = nm.(model)
                }
            }
        }
    }
}
```

- [ ] **Step 13.2: Run with -race flag**

```
go test -race -v -run TestUpdate_NoRace_ResizeSpamWithMarkdownRender .
```

Expected: PASS, không có "DATA RACE" output.

- [ ] **Step 13.3: Run full -race suite**

```
go test -race ./...
```

Expected: all PASS (đảm bảo các test khác cũng không race với code mới).

- [ ] **Step 13.4: Commit**

```
git add update_markdown_test.go
git commit -m "test(layout): verify resize spam + markdown render is race-free"
```

---

## Task 14: Visual verdict — 2 frame dumps

**Files:**
- Modify: `zz_dump_test.go`

PRD references: §6 checklist item 12, T13.

- [ ] **Step 14.1: Read current zz_dump_test.go to understand harness**

```
cat zz_dump_test.go
```

Verify nó tạo View output và ghi ra file (visual harness gated bởi env hoặc tag).

- [ ] **Step 14.2: Add two-frame dump for responsive layout**

Thêm vào `zz_dump_test.go` (theo style hiện tại của file đó):

```go
func TestZZ_DumpResponsiveFrames(t *testing.T) {
    if os.Getenv("LE_DUMP") == "" {
        t.Skip("set LE_DUMP=1 to dump visual frames for verdict")
    }

    tmp := t.TempDir()
    for _, n := range []string{"docs", "src", "README.md", "CLAUDE.md", "go.mod", "main.go"} {
        if strings.HasSuffix(n, "/") || !strings.Contains(n, ".") {
            os.Mkdir(filepath.Join(tmp, n), 0o755)
        } else {
            content := []byte("# " + n + "\n\nSample content for visual verdict.\n")
            os.WriteFile(filepath.Join(tmp, n), content, 0o644)
        }
    }

    m := newModel(tmp)
    m.mdStyle = "notty"

    // Frame A: 2-col (width 120, height 30)
    m.width, m.height = 120, 30
    m.refreshPreview()
    if err := os.WriteFile("frame_a_horizontal.txt", []byte(m.View()), 0o644); err != nil {
        t.Fatal(err)
    }

    // Frame B: 1-col stacked (width 70, height 30)
    m.width, m.height = 70, 30
    m.refreshPreview()
    if err := os.WriteFile("frame_b_stacked.txt", []byte(m.View()), 0o644); err != nil {
        t.Fatal(err)
    }
}
```

- [ ] **Step 14.3: Generate frames**

```
LE_DUMP=1 go test -run TestZZ_DumpResponsiveFrames .
```

Expected: 2 files `frame_a_horizontal.txt` và `frame_b_stacked.txt` xuất hiện.

- [ ] **Step 14.4: Inspect manually**

```
head -35 frame_a_horizontal.txt
echo "---"
head -35 frame_b_stacked.txt
```

Verify: Frame A có 2 pane side-by-side, Frame B có list pane trên + preview pane dưới.

- [ ] **Step 14.5: Run visual-verdict skill**

Trigger `oh-my-claudecode:visual-verdict` (hoặc skill tương đương) với hai file vừa dump làm input, so sánh với expected design intent:
- Frame A: 2-col, divider cột giữa, status bar đáy
- Frame B: 1-col stacked, divider ngang, preview chiếm ~2/3 chiều cao body, status bar đáy

Note kết quả visual verdict vào commit message hoặc note file.

- [ ] **Step 14.6: Commit**

Add the harness file (frame_a/b outputs are gitignored or committed as goldens at engineer discretion — recommended: gitignore generated `frame_*.txt`, keep test code only):

```
git add zz_dump_test.go
# optionally add a .gitignore entry for frame_*.txt if not already ignored
git commit -m "test(layout): add visual frame dump harness for both orientations"
```

---

## Task 15: Final verify gate

**Files:** none (verification only)

- [ ] **Step 15.1: Run canonical verify gate**

```
go build -o lazyexplorer . && go vet ./... && go test ./...
```

Expected: build succeeds, vet clean, all tests PASS.

- [ ] **Step 15.2: Run race suite**

```
go test -race ./...
```

Expected: all PASS, zero race reports.

- [ ] **Step 15.3: Hand-test acceptance scenarios per PRD §6**

Khởi `./lazyexplorer .` trong một terminal có thể resize. Check qua từng item PRD §6 checklist (1–9):

1. width=120 → 2-col render đúng
2. width=70 ban đầu → 1-col từ frame đầu
3. Resize `120 → 70 → 120` → state preserved
4. Stacked: kéo Y-divider, không pane nào dưới minPanelRows
5. Stacked: wheel routing theo pane
6. Markdown reflow trong khi resize
7. Drag-mid-flip không jam
8. topRatio & leftRatio cùng persist
9. width=79/80/81 boundary đúng

- [ ] **Step 15.4: Generate + visual-verdict 2 frames**

```
LE_DUMP=1 go test -run TestZZ_DumpResponsiveFrames .
```

Run `oh-my-claudecode:visual-verdict` trên `frame_a_horizontal.txt` và `frame_b_stacked.txt`.

Expected: cả hai verdict PASS với design intent.

- [ ] **Step 15.5: Mark PRD as ready**

Update `docs/prd-responsive-layout.md` line 9: đổi `Status: **draft / chờ review**` → `Status: **accepted**` (per `docs/CLAUDE.md` status vocabulary).

- [ ] **Step 15.6: Final commit**

```
git add docs/prd-responsive-layout.md
git commit -m "docs(layout): mark responsive-layout PRD as accepted after verify"
```

---

## Self-Review

**1. Spec coverage** (PRD §4 FR1–FR9):
- FR1 (auto switch < 80): Task 4 layout branch, Task 5 View branch ✓
- FR2 (≥ 80 unchanged): Task 4 `else` path, Task 5 `else` path, all existing tests must still pass ✓
- FR3 (flip preserves state): Task 4, 6, 7 — selection/scroll fields not touched on flip ✓
- FR4 (default 33/67 + Y-drag): Task 1 default, Task 3 + 9 + 10 drag ✓
- FR5 (ratios persist): No reset code added; verified by Task 11 stress + manual §15.3 ✓
- FR6 (mouse hit-test): Task 8 + 10 ✓
- FR7 (markdown reflow): Task 6 + 12 ✓
- FR8 (drag-mid-flip flush): Task 11 ✓
- FR9 (status bar unchanged): no code change to `renderStatus`; existing tests cover ✓

**2. Placeholder scan:** No "TBD" / "implement later" / "fill in details" / "similar to Task N". Every code-bearing step has the actual code. All commands are concrete with expected output. ✓

**3. Type consistency:**
- `widthBreakpoint`, `minPanelRows`, `topRatio`, `lastVertical`, `previewFirstRow`, `innerH2`, `topOuter`, `bottomOuter`, `topOuterHeight`, `setTopFromY` — same spelling everywhere in plan ✓
- `markdownRenderedMsg`, `previewPreStyled`, `mdPendingWidth` — used in Task 12 match `model.go` existing names ✓

Nothing requires fix.

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-05-27-responsive-layout.md`. Two execution options:

**1. Subagent-Driven (recommended)** — dispatch fresh subagent per task; review between tasks; fast iteration; isolates per-task context.

**2. Inline Execution** — execute tasks in this session using executing-plans; batch with checkpoints for review.

**Which approach?**
