# PRD — Middle divider (bỏ border bao quanh, thay bằng một divider giữa)

> Feature: bỏ rounded border bao quanh mỗi pane, thay bằng **một** vertical
> divider duy nhất giữa list pane và preview pane. Divider có **spacing trái +
> phải** vừa làm chỗ thở visual, vừa nới rộng vùng hit-test để chuột bám divider
> dễ hơn khi drag — đúng tinh thần "glance-friendly · UI simpler than superfile"
> trong project `CLAUDE.md`.

Status: **accepted** · Author: brainstorming session · Ngày: 2026-05-27 · Shipped: 2026-05-28 (✅ borderless 2-col + " │ " divider live in `view.go`; `go build && go vet && go test ./...` green)

---

## 1. Bối cảnh & vấn đề

Hiện tại cả hai pane đều được bọc trong rounded border riêng:

- `view.go:80-86` — `panelBorder(true)` cho left, `panelBorder(false)` cho right;
  cả hai gọi `.Width(g.leftOuter)` / `.Width(g.rightOuter)` rồi render.
- `theme.go:48-56` — `panelBorder` trả `lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(bc)`,
  border color đổi theo `focused` (active=`colAccent`, inactive=`colDim`).
- `view.go:14-21, 23-35` — `geometry` đo theo "outer" (incl. border):
  `leftInner = leftOuter - 2`, `innerH = bodyHeight - 2` (trừ 2 hàng border trên/dưới).

Hệ quả khi đứng cạnh agent trong terminal hẹp:

1. **Tốn cột & hàng cho border**: mỗi pane mất 2 cột (trái/phải) + 2 hàng (trên/dưới)
   cho rounded border, tổng cộng **4 cột + 2 hàng** chrome thuần. Ở `width=70` (case
   half-screen 13" thường gặp) chrome chiếm ~6% chiều ngang, lấy mất chỗ của file
   name + preview content.
2. **Hai border kề nhau ở giữa "dày đặc"** (`view.go:80-87` + `JoinHorizontal`):
   cột `leftOuter-1` (border phải của left) đứng sát cột `leftOuter` (border trái
   của right) — visually đọc như **hai đường thẳng dính nhau**, không phải một
   divider. Phá trực giác "có một đường chia ở giữa".
3. **Drag zone hẹp**: `model.go:442-447` quy định drag chỉ bắt 2 cột (`leftOuter-1`
   và `leftOuter`). Người dùng chuột phải canh chuẩn 2 px — không thân thiện cho
   workflow glance-and-drag.
4. **Focus signal "border color" gần như vô dụng**: lazyexplorer không có khái niệm
   chuyển focus giữa hai pane bằng bàn phím — list pane **luôn** là pane "active"
   (mọi keybind định hướng vào list, preview chỉ scroll). Đổi màu border giữa
   `colAccent` / `colDim` để biểu thị focus là **signal không bao giờ thay đổi**
   trong vòng đời chương trình → chrome trang trí, không phải affordance.

Reference: `tmp/lipgloss` (border helpers — không copy), `tmp/superfile` (full file
manager bọc 3 pane đều border — chúng ta cố ý đi ngược: **fewer chrome, more content**).

## 2. Goal (1 câu)

Layout 2-pane render **không có border quanh pane**, chỉ có **một vertical divider 3
cột** (` │ ` — 1 space + `│` + 1 space) ngăn giữa list và preview, với toàn bộ
3 cột divider là vùng hit-test để bắt đầu drag — giải phóng 4 cột + 2 hàng chrome
cho content, đồng thời làm divider dễ bám tay hơn.

**Non-goal làm rõ:**
- KHÔNG thêm tab, breadcrumb, header bar thay thế cho border trên — bỏ border là
  bỏ luôn, không bù bằng UI khác.
- KHÔNG đổi default `leftRatio` (`model.go:97` — giữ `0.38`).
- KHÔNG đổi divider character thành `┃` / `╎` / `║` ở v1 (giữ `│` light).
- KHÔNG tô màu divider theo focus (focus signal đã có ở cursor row,
  `view.go:146` — `cursorActiveStyle`).
- KHÔNG đụng status bar (`view.go:245-270`) — vẫn ở `row = m.height-1`.
- KHÔNG ship cùng PRD responsive layout (`prd-responsive-layout.md`) — hai PRD
  orthogonal nhưng đều chạm `geometry`, ship riêng để conflict resolution rõ.

## 3. Quyết định đã chốt

| # | Quyết định | Giá trị | Lý do |
|---|-----------|---------|-------|
| D1 | Border quanh pane | **Bỏ hoàn toàn** ở cả hai pane | User request literal; border-as-focus-signal là noop trong lazyexplorer (list luôn active) |
| D2 | Divider character | `│` (U+2502 BOX DRAWINGS LIGHT VERTICAL) | Khớp với weight của `RoundedBorder` hiện dùng — đổi character là quyết định thẩm mỹ riêng, defer |
| D3 | Divider total width | **3 cột**: `[space][│][space]` | "Một chút spacing" theo user; 3 là tối thiểu thoả 2 yêu cầu (spacing + line); rộng hơn xét ở §5.10 (defer) |
| D4 | Divider color | `colDim` (`theme.go:17`), **không** đổi theo focus / drag | Một accent duy nhất theo `theme.go:11` ("lazygit-flavored: restrained"); không thêm tone mới |
| D5 | Drag hit zone | **Cả 3 cột** của divider | Mục đích spacing chính là để hit-test dễ — bắt cả 3 thay vì chỉ cột `│` |
| D6 | `setLeftFromX` semantics | `x` được set là **vị trí cột `│`** (divider center) | Click bất kỳ cột nào của divider → `│` nhảy về cột đó; khớp behavior "snap to cursor" hiện tại (`model.go:478-483`) |
| D7 | `leftRatio` semantics | Ratio = `dividerCenterX / m.width` | Mirror cú pháp cũ (ratio normalized 0..1); persist tự nhiên qua resize |
| D8 | Body height | `bodyH = m.height - 1` (toàn bộ body, không trừ border) | Free win: +2 hàng content cho mỗi pane vì bỏ top/bottom border |
| D9 | Min pane inner width | `minPanelInnerCols = 14` | Parity với floor cũ (`minPanelCols=16` − 2 cột border = 14 cột content) |
| D10 | Min total width | `2*14 + 3 = 31` cột | Khi `m.width < 31`, clamp best-effort (mirror behavior degenerate-tiny cũ ở `view.go:48`) |
| D11 | `panelBorder` trong `theme.go` | **Xoá** | Greenfield principle (`/Users/firegroup/projects/CLAUDE.md` Code Style): không giữ unused code |
| D12 | Divider khi mode prompt (`modeRename`/`modeConfirmDelete`) | Giữ nguyên — divider vẫn render, status bar đổi thành prompt như hiện tại | Prompt sống ở status row, không can thiệp body; divider không thay đổi theo mode |

## 4. Functional requirements

- **FR1** — Render body 2-pane **không bọc border**: list pane chiếm cột
  `[0, dividerStart)`, divider chiếm cột `[dividerStart, dividerStart+3)`,
  preview pane chiếm cột `[dividerStart+3, m.width)`. Tổng width khớp `m.width`
  tới đúng 1 cột.
- **FR2** — Divider render **mỗi hàng body** là chuỗi `" │ "` với `│` tô `colDim`
  (`theme.go:17`), 2 cột space để mộc; cao `bodyH = m.height - 1` hàng (không
  lấn xuống status row).
- **FR3** — Mỗi pane có thêm **+1 cột inner** (do mất 1 cột border của chính nó
  về phía divider) và **+2 hàng inner** (do bỏ top + bottom border) so với hành
  vi hiện tại tại cùng `leftRatio` và `m.width / m.height`.
- **FR4** — Click chuột trái vào **bất kỳ cột nào của divider** (`e.X ∈
  [dividerStart, dividerStart+2]`) với `e.Y < m.height-1` bắt đầu drag; `│`
  snap về cột `e.X`. Drag motion tiếp tục snap `│` về cột `e.X` hiện tại;
  release kết thúc drag (giữ pattern `model.go:401-413`).
- **FR5** — `leftRatio` clamp để pane nào cũng có ít nhất `minPanelInnerCols=14`
  cột content; khi `m.width < 31`, clamp best-effort (không panic, không overflow).
- **FR6** — Cursor row trong list pane vẫn highlight full-width của pane qua
  `cursorActiveStyle.Width(w)` (`view.go:146`) — width = `leftInner` mới (không
  `leftOuter - 2`).
- **FR7** — Mouse hit-test pane đúng theo trục mới: `e.X < dividerStart` = list,
  `e.X >= dividerStart+3` = preview. Cột divider không bao giờ route vào list
  hay preview cho click/wheel — chỉ cho drag.
- **FR8** — Folder-preview click (`model.go:499-537` qua `previewClick`) tính
  `row := y - g.firstRow` với `g.firstRow = 0` (body bắt đầu từ row 0, không có
  top border) — entry index ánh xạ 1:1 với hàng body như cũ.
- **FR9** — Wheel scroll qua divider không gây side effect: `e.X ∈ [dividerStart,
  dividerStart+2]` cho `MouseWheelMsg` → noop. Wheel chỉ effect khi cursor chuột
  rõ ràng nằm trong list hoặc preview.
- **FR10** — Khi `m.width = 0 || m.height = 0` (pre-`WindowSizeMsg`), `View()`
  giữ branch `"loading…"` hiện tại (`view.go:67`) — không cố layout với divider.
- **FR11** — Khi user resize terminal, divider snap theo `leftRatio` đã lưu;
  không có drag-in-flight nào bị kẹt (release tự xảy ra khi user thả chuột;
  resize không tự `dragging=false`).

## 5. Technical design

> Kim chỉ nam: **giảm khái niệm thay vì thêm**. Bỏ "outer/inner" dichotomy ra
> khỏi geometry, thay bằng `leftInner` + `dividerStart` + `rightInner` trực
> tiếp. Toàn bộ render và hit-test đọc cùng `geometry` (single-source kỷ luật
> đã có từ `view.go:13-21`), không có chỗ nào tự suy ra cột divider riêng.

### 5.1 Constants (`view.go`)

Thay block hiện tại tại `view.go:38-39`:

```go
const (
    // minPanelInnerCols là cột content tối thiểu trong mỗi pane (không tính
    // divider). Parity với floor cũ minPanelCols=16 trừ 2 cột border = 14.
    minPanelInnerCols = 14

    // divider geometry — 3 cột tổng: [space][│][space].
    dividerPadLeft  = 1
    dividerPadRight = 1
    dividerWidth    = dividerPadLeft + 1 + dividerPadRight // = 3

    // dividerGlyph là ký tự rune dùng cho cột giữa của divider. Light box
    // drawing để khớp weight của RoundedBorder cũ — đổi character là quyết
    // định thẩm mỹ riêng, defer khỏi v1 (xem §5.10).
    dividerGlyph = "│"
)
```

`minPanelCols` cũ (`view.go:38`) bị **xoá** — không còn dùng (border đã bị bỏ).

### 5.2 `geometry` viết lại (`view.go:14-21`)

```go
// geometry holds the screen layout derived purely from terminal size + cursor.
// Both View (for rendering) and the mouse handler (for hit-testing) call
// layout() so the two can never disagree about where a row or column lives.
type geometry struct {
    leftInner    int // cols của list pane content (không bao gồm divider)
    rightInner   int // cols của preview pane content
    dividerStart int // cột đầu tiên của divider (= leftInner); divider chiếm 3 cột [dividerStart, dividerStart+3)
    bodyH        int // số hàng của body (đã trừ status row); content fills exactly bodyH rows
    listTop      int // index của entry đầu visible trong list
    firstRow     int // screen Y của hàng đầu trong body — luôn = 0 vì không còn top border
}
```

Field cũ bị **xoá**: `leftOuter`, `rightOuter`, `innerH`. Việc gọi tên đổi rộng
khắp — tham chiếu `g.leftOuter` ở `model.go:397, 415, 442-447, 451` và
`view.go:69-86` cần update theo §5.3-§5.7.

### 5.3 `layout()` đơn giản hoá (`view.go:23-35`)

```go
func (m model) layout() geometry {
    bodyH := max(m.height-1, 3) // status(1); body fills the rest
    leftInner := m.leftInnerWidth()
    return geometry{
        leftInner:    leftInner,
        rightInner:   m.width - leftInner - dividerWidth,
        dividerStart: leftInner,
        bodyH:        bodyH,
        listTop:      m.listTopFor(bodyH),
        firstRow:     0,
    }
}
```

So với layout cũ: `bodyH` thay `innerH` và **không trừ 2** (free 2 hàng content);
`firstRow` đổi từ `1` (sau top border) sang `0` (body bắt đầu ngay).

### 5.4 `leftInnerWidth` thay `leftOuterWidth` (`view.go:44-58`)

```go
// leftInnerWidth biến leftRatio (drag-adjustable) thành cột content cho list
// pane. Ratio đại diện cho vị trí của cột │ (divider center), nên:
//     dividerCenter = round(m.width * leftRatio)
//     leftInner     = dividerCenter - dividerPadLeft
// Clamp để mỗi pane giữ tối thiểu minPanelInnerCols cột content, cộng đủ chỗ
// cho divider. Khi terminal hẹp tới mức không nhét nổi cả hai pane + divider,
// trả best-effort thay vì panic — mirror behavior degenerate-tiny cũ.
func (m model) leftInnerWidth() int {
    dividerCenter := int(float64(m.width)*m.leftRatio + 0.5)
    li := dividerCenter - dividerPadLeft

    hi := m.width - dividerWidth - minPanelInnerCols // chừa chỗ cho right pane
    if hi < minPanelInnerCols {
        hi = minPanelInnerCols // terminal cực hẹp: best effort
    }
    if li < minPanelInnerCols {
        li = minPanelInnerCols
    }
    if li > hi {
        li = hi
    }
    return li
}
```

### 5.5 `View()` viết lại (`view.go:65-96`)

```go
func (m model) View() tea.View {
    content := "loading…"
    if m.width != 0 && m.height != 0 {
        g := m.layout()

        // Render từng pane đến đúng kích thước inner — không panelBorder,
        // không Width/Height tăng thêm cho border. lipgloss.Place lo padding
        // hàng còn thiếu (content < bodyH) để JoinHorizontal align đẹp.
        left := lipgloss.NewStyle().
            Width(g.leftInner).Height(g.bodyH).
            Render(m.renderList(g.leftInner, g.bodyH))

        right := lipgloss.NewStyle().
            Width(g.rightInner).Height(g.bodyH).
            Render(m.renderPreview(g.rightInner))

        // Divider: bodyH hàng, mỗi hàng " │ " (1 space + glyph + 1 space).
        // Chỉ glyph được tô màu — 2 space để mộc để khớp background pane.
        dividerLine := strings.Repeat(" ", dividerPadLeft) +
            dimStyle.Render(dividerGlyph) +
            strings.Repeat(" ", dividerPadRight)
        divider := strings.Repeat(dividerLine+"\n", g.bodyH)
        divider = strings.TrimRight(divider, "\n")

        body := lipgloss.JoinHorizontal(lipgloss.Top, left, divider, right)
        content = strings.Join([]string{body, m.renderStatus()}, "\n")
    }

    v := tea.NewView(content)
    v.AltScreen = true
    v.MouseMode = tea.MouseModeCellMotion
    return v
}
```

`panelBorder` không còn được gọi.

### 5.6 `previewBodyWidth` đổi nguồn (`model.go:266-269`)

Hiện tại: `return g.rightOuter - 2`. Mới:

```go
func (m model) previewBodyWidth() int {
    g := m.layout()
    return g.rightInner
}
```

`syncPreview` (`model.go:284-313`) so sánh `m.srcWidth` với giá trị mới này; khi
user kéo divider hoặc resize, `rightInner` đổi → dispatch render mới — gen-counter
tự drop stale render. **Không có code mới ở async pipeline**, chỉ đổi nguồn width.

### 5.7 `previewScroll` cập nhật (`view.go:204-208`)

```go
func (m model) previewScroll() (top, bodyH int) {
    bodyH = m.layout().bodyH
    top = min(m.previewTop, max(0, m.previewLen()-bodyH))
    return top, bodyH
}
```

Đổi từ `innerH` sang `bodyH` — chỉ đổi field name (semantic identical: hàng content
trong pane preview, bằng số hàng trong list pane).

### 5.8 `handleMouse` đổi hit-test (`model.go:396-472`)

Các sửa đổi chính (giữ nguyên cấu trúc switch hiện tại):

```go
case tea.MouseClickMsg:
    // Divider drag: bất kỳ cột nào trong [dividerStart, dividerStart+3) đều
    // bắt drag; e.Y bound trên là m.height-1 (không cho click vào status row
    // thành drag). Khi click "padding space" của divider, │ snap về cột đó —
    // giữ pattern click-to-snap hiện tại của leftOuter-1/leftOuter cũ.
    if e.Button == tea.MouseLeft && e.Y < m.height-1 &&
        e.X >= g.dividerStart && e.X < g.dividerStart+dividerWidth {
        m.dragging = true
        m.setLeftFromX(e.X)
        return m, nil
    }
    if e.Button != tea.MouseLeft {
        return m, nil
    }
    overLeft := e.X < g.dividerStart
    // (cột divider rơi vào "không-pane" — không route click vào pane nào;
    //  chỉ bắt drag ở nhánh trên. Click ngoài cả ba vùng → noop.)
    if e.X >= g.dividerStart && e.X < g.dividerStart+dividerWidth {
        return m, nil
    }
    if !overLeft {
        m.previewClick(e.Y, g)
        return m, nil
    }
    row := e.Y - g.firstRow              // firstRow=0 → row = e.Y
    if row < 0 || row >= g.bodyH {       // dùng bodyH, không innerH
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

case tea.MouseWheelMsg:
    // Wheel trên cột divider: noop (FR9). Không scroll list cũng không scroll
    // preview — divider là "no-pane" zone.
    if e.X >= g.dividerStart && e.X < g.dividerStart+dividerWidth {
        return m, nil
    }
    overLeft := e.X < g.dividerStart
    // ... (logic cũ, đổi mỗi overLeft check)
```

Motion + Release branch không đổi cấu trúc; chỉ `setLeftFromX` được gọi với
semantics mới (§5.9).

### 5.9 `setLeftFromX` semantics mới (`model.go:478-483`)

```go
// setLeftFromX pin cột │ của divider về cột x: leftRatio = x / m.width. Click
// vào "padding space" của divider (x = dividerStart hoặc dividerStart+2) cũng
// snap │ về đó — gây jump 1 cột so với vị trí cũ, nhưng nhất quán với behavior
// click-to-snap hiện có và nhỏ tới mức user không cảm.
func (m *model) setLeftFromX(x int) {
    if m.width <= 0 {
        return
    }
    m.leftRatio = float64(x) / float64(m.width)
}
```

So với cũ (`float64(x+1) / float64(m.width)`): bỏ `+1` vì semantics đổi từ
"x = cột border phải của left pane" (cộng 1 thành leftOuter) sang "x = cột center
divider" (chính là dividerCenter, không cần cộng).

### 5.10 Edge cases tổng hợp

| Tình huống | Hành vi | Cơ chế |
|---|---|---|
| `m.width = 0 \|\| m.height = 0` | `"loading…"` | branch `if m.width != 0 && m.height != 0` (§5.5) |
| `m.width < 31` (= 2*14 + 3) | Degenerate, `leftInnerWidth` clamp best-effort | `hi = minPanelInnerCols` khi `m.width - dividerWidth - minPanelInnerCols < minPanelInnerCols` (§5.4) |
| `m.height = 2` | `bodyH = max(1, 3) = 3` → body 3 hàng, status 1 hàng | `view.go:25` đã có `max(m.height-1, 3)`, giữ nguyên |
| Click `e.X = dividerStart + 2` (padding phải của divider) | Drag start, `│` snap về cột `dividerStart+2` | §5.8 + §5.9 |
| Click `e.Y = m.height - 1` trên cột divider | Noop (status row, không phải body) | điều kiện `e.Y < m.height-1` (§5.8) |
| Wheel trên cột divider | Noop (FR9) | §5.8 wheel branch |
| Markdown đang render khi resize → divider qua | `previewBodyWidth` đổi → `syncPreview` dispatch lại; gen-counter drop stale | §5.6 + `model.go:284-313` |
| Drag đang chạy khi terminal resize | Drag giữ — `dragging` không tự `false` (mirror behavior hiện tại) | `model.go:357-360` không đụng |
| `cursorActiveStyle.Width(w)` với `w = leftInner` mới | Highlight full new width của list pane | `view.go:146` chỉ phụ thuộc `w` |
| Folder preview click row đầu | `row = e.Y - 0 = e.Y` → đúng index 0 | `previewClick` (§5.8) + `firstRow=0` |

### 5.11 Đã cân nhắc & **defer khỏi v1**

- **Divider 5 cột** (`  │  ` — 2 space + glyph + 2 space): drag dễ hơn nữa nhưng
  ăn thêm 2 cột content. Defer; nâng `dividerPadLeft/Right` lên 2 là one-liner
  nếu user thực sự thấy 3 cột chưa đủ "bám tay".
- **Đổi divider character** (`┃` heavy, `╎` dashed, `║` double): thẩm mỹ; giữ
  `│` để khớp weight của `RoundedBorder` (cũng light).
- **Divider tô màu khi drag**: thêm state + branch theo `m.dragging` trong
  `View()`. Defer; visual jump của `│` khi drag đã là feedback đủ rõ.
- **Focus signal mới thay border color**: chuyển `cursorActiveStyle` thành chấm
  caret nổi bật hơn, hoặc tag `[list]` trong status bar. Defer — list luôn focus,
  signal hiện đã đủ.
- **Top/bottom header bar bù cho việc bỏ border** (vd. breadcrumb path, hotkey
  row): vi phạm "two panels is the ceiling, fewer chrome" của project — không
  xem xét trong PRD này.
- **Lipgloss border-edge approach** (`BorderRight(true)` trên left pane +
  `MarginRight(1)` + `MarginLeft(1)` trên right pane): code ngắn hơn nhưng
  ràng buộc tightly vào lipgloss border model — khi muốn customize spacing
  hoặc render dashed divider trong tương lai, khó hơn manual approach (§5.5).
  Manual chosen.
- **Snap divider về cột `│` khi click padding** (xem D6): có thể "smooth"
  bằng cách chỉ activate drag mà không jump `│` — defer; consistency với
  click-to-snap hiện tại quan trọng hơn.

## 6. Acceptance criteria

### Gherkin

```gherkin
Feature: Borderless 2-pane layout with single middle divider

  When the explorer renders, panes have no border around them. Instead, a single
  vertical bar between them marks the divide, with one space of breathing room
  on each side that doubles as a generous drag target.

  Background:
    Given the explorer is open beside a project

  Scenario: Body renders without border around either pane
    Given the terminal width is 100 columns and height is 30 rows
    Then no rounded border surrounds the list pane
    And no rounded border surrounds the preview pane
    And a single vertical bar separates the two panes
    And the bar has one blank column of space on each side

  Scenario: Each pane gains the chrome columns and rows it used to lose to border
    Given the terminal width is 100 columns and the divider sits at the default split
    Then the list pane content area is exactly one column wider than before
    And the preview pane content area is the same width as before
    And both pane bodies fill the full body height (no top or bottom border row)

  Scenario: Clicking any column of the divider starts a drag
    Given the explorer is rendered with a divider visible on the screen
    When I press the left mouse button on the divider's left padding column
    Then a drag begins
    And the vertical bar snaps to the clicked column
    When I press the left mouse button on the divider's right padding column
    Then a drag begins
    And the vertical bar snaps to the clicked column

  Scenario: Drag motion keeps the bar under the cursor
    Given I have started a divider drag
    When I move the cursor left without releasing
    Then the vertical bar follows the cursor column
    And the list pane shrinks while the preview pane grows
    When I release the mouse button
    Then the drag ends and the bar stays at the released column

  Scenario: Wheel scroll over the divider is a no-op
    Given the cursor is over the divider column
    When I scroll the wheel up
    Then neither the list cursor nor the preview viewport moves

  Scenario: Click over the divider but not on left-button does nothing
    Given the cursor is over the divider
    When I right-click
    Then nothing happens

  Scenario: Click on list pane still selects the entry under the row
    Given the explorer has at least three entries
    When I left-click on the row of the second entry inside the list pane
    Then the cursor moves to that entry
    And the preview refreshes for the new selection

  Scenario: Click on folder preview enters the folder
    Given the selected entry is a folder
    And the preview pane shows the folder's listing
    When I left-click a row of that listing
    Then the explorer enters that folder
    And lands on the clicked item

  Scenario: Divider stays clamped at narrow widths
    Given the terminal width is 31 columns
    Then both panes have at least 14 content columns
    And the divider has exactly 3 columns

  Scenario: Divider survives a terminal resize
    Given the divider is at 40 percent from the left
    When the terminal is resized wider
    Then the divider stays at 40 percent of the new width
```

### Checklist verify

1. Render ở `width=100, height=30` — kiểm tay không thấy `╭ ╮ ╰ ╯` hay đường
   ngang nào ở body; chỉ thấy đúng 1 cột `│` (dim color) chạy dọc body, cách
   list content 1 space, cách preview content 1 space.
2. Render ở `width=100, leftRatio=0.38` — đo `leftInner` = `int(100*0.38+0.5) - 1
   = 38 - 1 = 37` cột; `rightInner = 100 - 37 - 3 = 60` cột. Confirm bằng test
   layout (T9).
3. Body fills full `m.height-1`: ở `height=30`, body 29 hàng (status row 1),
   xác nhận hàng đầu list = file đầu tiên (không phải border `╭───╮`); hàng
   cuối body = entry/preview line, không phải `╰───╯`.
4. Click trái tại `(e.X = dividerStart-1, e.Y = 5)` → noop drag (chỉ chọn entry
   list pane); tại `(e.X = dividerStart, e.Y = 5)` → drag bắt đầu, `│` ở
   `dividerStart`; tại `(e.X = dividerStart+1, e.Y = 5)` → drag, `│` ở
   `dividerStart+1`; tại `(e.X = dividerStart+2, e.Y = 5)` → drag, `│` ở
   `dividerStart+2`; tại `(e.X = dividerStart+3, e.Y = 5)` → noop drag.
5. Drag motion (`MouseMotionMsg`) từ `e.X = 40` → `e.X = 50` (trong khi
   `dragging=true`) → `leftRatio` cập nhật mỗi step, `│` di chuyển theo.
6. `MouseWheelMsg` với `e.X = dividerStart+1` → noop (cursor list không đổi,
   `previewTop` không đổi).
7. Click chuột phải tại divider → noop (xét `e.Button != tea.MouseLeft`
   trong drag-detect branch).
8. Folder preview row index: chọn folder, click `e.Y = 0` → entry index 0 của
   preview listing (không phải bị border lệch).
9. Width=31 (edge): `leftInner=14, rightInner=14, dividerStart=14, dividerWidth=3`;
   không panic, không overflow; render đủ.
10. Width=30 (dưới min): clamp best-effort, không panic; layout không cố render
    số cột âm.
11. Resize `100 → 50 → 100`: `leftRatio` không đổi, `│` snap về `int(50*0.38+0.5)=19`
    rồi `int(100*0.38+0.5)=38` — same ratio, different absolute col.
12. Markdown reflow: chọn `README.md` ở `width=100`, drag divider qua trái →
    `rightInner` giảm → `syncPreview` dispatch render mới, spinner render
    mép phải xuất hiện rồi mất; preview wrap đúng
    width mới.
13. `panelBorder` không còn tham chiếu nào trong codebase: `rg 'panelBorder'`
    trả 0 hit (đã xoá khỏi `theme.go`).
14. `minPanelCols` không còn tham chiếu nào: `rg 'minPanelCols'` trả 0 hit.
15. `go build -o lazyexplorer . && go vet ./... && go test ./...` xanh.
16. `go test -race ./...` xanh (drag spam + markdown render concurrent).
17. Visual verdict (qua `oh-my-claudecode:visual-verdict`) cho 2 frame fixture:
    - Frame A: `width=120, height=30`, sample dir, `.md` selected → render
      borderless với divider, preview markdown chiếm trọn vùng phải.
    - Frame B: `width=60, height=24`, same dir, file selected → render
      borderless, divider có 3 cột, panes không bị overflow.

## 7. Task breakdown

> Map 1-1 với file. Theo project memory + CLAUDE.md GitNexus section: TRƯỚC khi
> sửa, chạy `gitnexus_impact` cho `panelBorder`, `leftOuterWidth`,
> `setLeftFromX`, `handleMouse`, `previewBodyWidth`, `previewScroll`,
> `previewClick`, `View`, `layout` — báo blast radius cho user, đặc biệt nếu có
> HIGH/CRITICAL warning trên render path. Sau khi sửa, `gitnexus_detect_changes`
> verify scope khớp dự kiến.

- [ ] **T1 — Constants.** Trong `view.go`: xoá `minPanelCols`; thêm
  `minPanelInnerCols=14`, `dividerPadLeft=1`, `dividerPadRight=1`,
  `dividerWidth=3`, `dividerGlyph="│"` (§5.1). *(view.go)*

- [ ] **T2 — `geometry` viết lại.** Đổi field `leftOuter/rightOuter/innerH/
  leftInner` → `leftInner/rightInner/dividerStart/bodyH`; cập nhật doc comment
  (§5.2). *(view.go)*

- [ ] **T3 — `leftInnerWidth` thay `leftOuterWidth`.** Rename + viết lại theo
  semantics divider-center (§5.4). Update mọi caller trong package. *(view.go)*

- [ ] **T4 — `layout()` đơn giản hoá.** Trả `geometry` mới với `bodyH = bodyHeight`
  (không trừ 2) và `firstRow = 0` (§5.3). *(view.go)*

- [ ] **T5 — `View()` viết lại.** Bỏ `panelBorder(...)` ở cả hai pane; render
  divider bằng `strings.Repeat`; `JoinHorizontal(Top, left, divider, right)`
  (§5.5). *(view.go)*

- [ ] **T6 — `previewScroll` đổi field.** `bodyH = m.layout().bodyH` (§5.7).
  *(view.go)*

- [ ] **T7 — `previewBodyWidth` đổi nguồn.** `return g.rightInner` (§5.6).
  *(model.go)*

- [ ] **T8 — `setLeftFromX` semantics mới.** Bỏ `+1` trong công thức ratio
  (§5.9). *(model.go)*

- [ ] **T9 — `handleMouse` hit-test mới.** Drag-detect zone = `[dividerStart,
  dividerStart+dividerWidth)`; wheel/click trên divider = noop; `overLeft = e.X
  < dividerStart`; row check dùng `bodyH` (§5.8). *(model.go)*

- [ ] **T10 — `theme.go` cleanup.** Xoá `panelBorder` function (`theme.go:47-56`).
  Không thêm `dividerStyle` riêng — dùng `dimStyle` có sẵn (`theme.go:31`).
  *(theme.go)*

- [ ] **T11 — Tests update.** Sửa các test break vì đổi geometry semantics:
  - `resize_test.go` — đổi assertion từ `leftOuter` sang `leftInner` /
    `dividerStart`; thêm case clamp `width<31`.
  - `entryrow_test.go` — nếu test dùng `panelBorder`, đổi sang style mới (chỉ
    `lipgloss.NewStyle().Width(w)`).
  - `previewclick_test.go` — `firstRow=0` (row index trực tiếp là `e.Y`).
  - `previewdir_test.go` — tương tự.
  - `previewtest.go` — cập nhật width assertion (`rightInner` thay `rightOuter-2`).
  - Mọi golden snapshot trong `zz_dump_test.go` cần regenerate (border đã bỏ).
  *(resize_test.go, entryrow_test.go, previewclick_test.go, preview_dir_test.go, preview_test.go, zz_dump_test.go)*

- [ ] **T12 — Tests mới cho divider behavior:**
  - `dividerWidth` constant = 3.
  - `layout()` ở `width=100, leftRatio=0.38, height=30` → `leftInner=37,
    rightInner=60, dividerStart=37, bodyH=29, firstRow=0`.
  - `leftInnerWidth` clamp: `width=20` → `leftInner=14` (minPanelInnerCols),
    `rightInner=20-14-3=3` (under floor → degenerate; vẫn không panic).
  - Drag-zone: gửi `MouseClickMsg{X: dividerStart-1}` → no drag; `X: dividerStart`
    → drag, ratio = `dividerStart / width`; `X: dividerStart+1`/`+2` → drag;
    `X: dividerStart+3` → no drag.
  - Wheel-on-divider: `MouseWheelMsg{X: dividerStart+1, Button: WheelUp}` →
    cursor không đổi, `previewTop` không đổi.
  - Click-on-divider with non-left button: `MouseClickMsg{X: dividerStart, Button:
    MouseRight}` → no drag, no side effect.
  - `setLeftFromX(50)` ở `width=100` → `leftRatio=0.5` (bỏ `+1`).
  - Folder click row 0: `MouseClickMsg{X: dividerStart+5, Y: 0}` → entry 0 của
    folder preview được mở (regression test cho `firstRow=0`).
  *(thêm vào file test phù hợp; nếu test riêng cho divider, đặt
  `divider_test.go`)*

- [ ] **T13 — Visual verdict.** Update `zz_dump_test.go` fixture: dump 2 frame
  `width=120/height=30` và `width=60/height=24` với cùng sample dir; chạy
  `oh-my-claudecode:visual-verdict` so với mô tả "borderless, single dim divider
  with 1-col padding each side". *(zz_dump_test.go)*

- [ ] **T14 — Verify.** `go build -o lazyexplorer . && go vet ./... && go test
  ./...` xanh; `go test -race ./...` xanh; visual verdict 2 frame đạt; chạy tay
  kiểm acceptance §6 (1–14); `rg 'panelBorder|minPanelCols|leftOuter|rightOuter|innerH'`
  trả 0 hit (kỷ luật Positive framing — không còn dấu vết khái niệm cũ trong
  spec/code đang sống).

## 8. Files chạm tới

| File | Thay đổi |
|------|----------|
| `view.go` | Xoá `minPanelCols`; thêm `minPanelInnerCols`, `dividerPadLeft`, `dividerPadRight`, `dividerWidth`, `dividerGlyph`; `geometry` viết lại (4 field mới, 4 field cũ xoá); `leftOuterWidth` → `leftInnerWidth` (rename + new formula); `layout()` đơn giản hoá; `View()` viết lại bỏ `panelBorder`, thêm divider render; `previewScroll` đổi `innerH` → `bodyH` |
| `theme.go` | Xoá `panelBorder` function (`theme.go:47-56`); palette giữ nguyên |
| `model.go` | `setLeftFromX` bỏ `+1`; `handleMouse` thêm divider drag-zone (3 cột), wheel-on-divider noop, `overLeft` = `e.X < dividerStart`, row check dùng `bodyH`; `previewBodyWidth` trả `g.rightInner` |
| `resize_test.go` | Cập nhật assertion `leftOuter` → `leftInner` / `dividerStart`; thêm case clamp & drag-zone mới |
| `entryrow_test.go` | Cập nhật bất kỳ tham chiếu `panelBorder` (đổi sang style trần) |
| `previewclick_test.go` | `firstRow = 0`; row hit-test trực tiếp `e.Y` |
| `preview_dir_test.go` | Tương tự `previewclick_test.go` |
| `preview_test.go` | Cập nhật width assertion theo `rightInner` (không `rightOuter-2`) |
| `zz_dump_test.go` | Regenerate golden snapshot (border đã bỏ); 2 frame visual verdict |
| `divider_test.go` *(mới)* | Tests cho divider behavior (T12 list) — nếu không muốn thêm file mới, gộp vào `resize_test.go` |
