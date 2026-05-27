# PRD — Responsive layout (auto 2-col ↔ 1-col stacked)

> Feature: khi terminal đủ rộng, lazyexplorer giữ 2-col **side-by-side** như hiện
> nay; khi user chia màn hình khiến width co lại dưới một ngưỡng, layout **tự**
> chuyển sang **1-col stacked** — list (trên) + preview (dưới) — đúng kiểu
> [`jesseduffield/lazygit`](https://github.com/jesseduffield/lazygit). Không thêm
> keybind, không thêm mode, không thêm config — chỉ thêm một trục.

Status: **draft / chờ review** · Author: brainstorming session · Ngày: 2026-05-27

---

## 1. Bối cảnh & vấn đề

lazyexplorer được thiết kế để **sống cạnh agent** trong một pane terminal — và
trong thực tế dùng, user **rất thường chia màn hình làm hai**: nửa cho coding agent,
nửa cho lazyexplorer. Lúc đó width của lazyexplorer thường rơi vào khoảng **60–80
cols** (laptop 13" split 50/50, hoặc terminal lớn split 60/40 cho agent).

Ở dải đó, hai pane side-by-side **đều bị bóp**: `leftOuterWidth()` (`view.go:44`)
clamp về `minPanelCols=16` (`view.go:38`) cho mỗi bên. Concretely tại width 70 với
`leftRatio=0.38` mặc định (`model.go:86`):

- left pane outer = 27 cols → inner = 25 cols → tên file dài bị `fitWidth` truncate
  với "…" (`view.go:183`)
- right pane outer = 43 cols → inner = 41 cols → markdown wrap rất sớm, code block
  xuống dòng giữa câu

Trong khi nếu **stack** hai pane lên nhau ở chính width 70, mỗi pane được **full 70
cols** — preview thở hẳn ra, list không bị "…", chỉ đổi lại bớt chiều cao mỗi pane.

[`jesseduffield/lazygit`](https://github.com/jesseduffield/lazygit) đã giải bài
này bằng cách: khi terminal đủ hẹp, layout dồn các panel xuống cột dọc. lazyexplorer
chỉ có hai pane (list + preview) nên việc stack đơn giản hơn nhiều: list lên trên,
preview xuống dưới, status bar giữ nguyên đáy.

## 2. Goal (1 câu)

Khi `m.width < 80`, layout **tự** chuyển 1-col stacked (list trên, preview dưới);
khi `m.width ≥ 80`, layout **tự** quay về 2-col side-by-side hiện tại — không thêm
keybind, không thêm mode, không thêm config; cả X-drag (2-col) lẫn Y-drag (1-col)
đều adjust được divider bằng mouse.

**Non-goal làm rõ:**
- KHÔNG thêm keybind toggle layout — auto theo width là toàn bộ trigger.
- KHÔNG cho user override threshold qua flag/env trong v1.
- KHÔNG đổi hành vi 2-col mode (mode rộng giữ y nguyên).
- KHÔNG thêm panel thứ 3 — `docs/CLAUDE.md` "Two panels is the ceiling".
- KHÔNG trigger theo height (chỉ width) — wide+short hiếm và stack ở đó càng tệ.

## 3. Quyết định đã chốt (từ phiên hỏi)

| # | Quyết định | Giá trị | Lý do |
|---|-----------|---------|-------|
| D1 | Trigger | **Auto theo `m.width`**, ngưỡng `80` cols | User cite lazygit (default ~84); 80 là round number gần lazygit và khớp half-width laptop ~160 |
| D2 | Topology khi stacked | **List trên, preview dưới**, status giữ đáy | User request literal; preview là content chính cần thở nên xuống dưới (rộng hơn list về số dòng) |
| D3 | Default split ratio khi stacked | `topRatio = 0.33` (list ~1/3, preview ~2/3) | Lazygit-like; preview là focus chính khi đọc markdown/code |
| D4 | Y-divider drag | **Có**, mirror X-drag hiện có | Consistency: 2-col đã drag được nên 1-col cũng vậy; không thêm keybind |
| D5 | State persistence khi mode flip | `leftRatio` & `topRatio` **đều persist** | Mode flip không reset gì; quay về 2-col phục hồi đúng leftRatio user đã kéo |
| D6 | Hysteresis | **Không** ở v1 (single threshold) | Ship-then-enhance; thêm dải dead-zone nếu user thực sự complain flicker |
| D7 | Approach code | Branched `layout()` + `vertical bool` trên `geometry` | "No abstractions until proven" (`/Users/firegroup/projects/CLAUDE.md` Code Style); ≤2 panes nên pane-primitive YAGNI |
| D8 | minPanelRows | `4` (1 border + 2 content + 1 border) | Mirror `minPanelCols=16` (`view.go:38`); dưới mức này panel rỗng tới khó dùng |

## 4. Functional requirements

- **FR1** — `m.width < 80` → layout render 1-col stacked: list pane chiếm phần
  trên, preview pane chiếm phần dưới, status bar vẫn ở row cuối.
- **FR2** — `m.width ≥ 80` → layout render 2-col side-by-side, hành vi như hiện
  tại không đổi (regression-free).
- **FR3** — Khi resize terminal qua ngưỡng 80, layout chuyển ngay ở frame tiếp
  theo (đáp ứng `WindowSizeMsg`). Cursor selection, scroll, preview content
  được giữ nguyên qua chuyển đổi.
- **FR4** — Trong 1-col mode, default split là `topRatio = 0.33` (list ~1/3, preview
  ~2/3 của body height). User **kéo divider ngang** (row giữa list và preview) bằng
  chuột để điều chỉnh; ratio được clamp với `minPanelRows=4` cho cả hai pane.
- **FR5** — `topRatio` và `leftRatio` **persist** qua các lần chuyển mode: kéo
  divider 2-col → narrow terminal → widen terminal → leftRatio cũ vẫn hiệu lực;
  ngược lại tương tự với topRatio.
- **FR6** — Mouse hit-test trong 1-col đúng: click vào pane list (Y < divider)
  chọn entry, click vào pane preview (Y > divider) hoạt động như 2-col (folder
  listing có thể click để vào, file preview không). Wheel scroll route theo
  pane chứa con trỏ Y.
- **FR7** — Markdown re-render đúng width mới khi mode flip: chuyển 2-col → 1-col
  thì preview body width đổi từ `rightOuter-2` → `m.width-2`, glamour render lại;
  stale render bị drop bằng gen-counter hiện có (`model.go:71`).
- **FR8** — Drag-mid-flip không kẹt: nếu user đang drag X-divider mà resize đẩy
  qua 1-col mode (hoặc ngược lại), `m.dragging` được clear, user có thể bấm
  drag lại bình thường.
- **FR9** — Status bar giữ nguyên ở row cuối trong cả hai mode; `fitWidth`
  (`view.go:173`) tiếp tục truncate hints khi width hẹp — không thêm compact
  hints version riêng.

## 5. Technical design

> Kim chỉ nam: thêm **một trục** (Y) bên cạnh trục X đã có. Mirror các construct
> hiện có (`leftRatio`/`leftOuter`/`setLeftFromX`/`minPanelCols`) sang
> (`topRatio`/`topOuter`/`setTopFromY`/`minPanelRows`). Mọi quyết định orientation
> được tập trung vào **một flag** `g.vertical` mà `layout()` set, View và
> `handleMouse` đều đọc — đảm bảo render và hit-test không lệch nhau (kỷ luật
> single-source-of-geometry đã có trong project, `view.go:13`).

### 5.1 Constants mới (`view.go`)

```go
// view.go — đặt cạnh minPanelCols hiện có
const (
    minPanelCols     = 16  // existing
    minPanelRows     = 4   // mirror của minPanelCols cho trục Y (1 border + 2 nội dung + 1 border)
    widthBreakpoint  = 80  // m.width < này → 1-col stacked; ≥ → 2-col
)
```

**Hysteresis defer:** v1 dùng đúng một threshold (D6). Nếu phải thêm sau, đổi
thành cặp `widthBreakpointDown=80` / `widthBreakpointUp=84` + state
`m.lastVertical` để tránh flicker.

### 5.2 State mới trên `model` (`model.go:45-83`)

| Field | Default | Vai trò |
|-------|---------|---------|
| `topRatio float64` | `0.33` | Tỉ lệ chiều cao của list pane khi 1-col stacked; drag-adjustable theo Y. Mirror của `leftRatio` (`model.go:78`). |
| `lastVertical bool` | `false` | Lưu `vertical` của lần render trước, dùng riêng để phát hiện mode flip mid-drag (FR8). KHÔNG dùng cho hysteresis ở v1. |

`leftRatio`, `dragging`, `cursor`, `listTop`, `previewTop`, các markdown fields:
giữ y nguyên. `vertical` **không** lưu trên model — `layout()` tính purely từ
`m.width` mỗi lần (single source, no drift).

`newModel` thêm khởi tạo: `m := model{root: root, cwd: root, leftRatio: 0.38, topRatio: 0.33}`.

### 5.3 `geometry` mở rộng (`view.go:13-20`)

```go
type geometry struct {
    vertical bool // true → 1-col stacked; false → 2-col side-by-side

    // 2-col (vertical == false) — y như hiện tại
    leftOuter  int // total cols của left pane (incl border)
    rightOuter int

    // 1-col (vertical == true)
    topOuter    int // total rows của list pane (incl border)
    bottomOuter int // total rows của preview pane (incl border)

    // shared
    leftInner       int // 2-col: cols trong left pane; 1-col: cols trong cả hai pane (= m.width-2)
    innerH          int // 2-col: rows trong pane; 1-col: rows trong list pane
    innerH2         int // 1-col only: rows trong preview pane (= 0 ở 2-col)
    listTop         int // index của entry đầu tiên visible trong list
    firstRow        int // screen Y của entry đầu trong list — luôn = 1
    previewFirstRow int // screen Y của row đầu trong preview content (2-col: 1; 1-col: topOuter+1)
}
```

Lý do gom hai mode vào **một struct**: View và mouse handler vẫn gọi `m.layout()`
đúng một chỗ; `g.vertical` là source of truth cho mọi rẽ nhánh sau đó. Không
tách `geometryHoriz/geometryVert` để tránh polymorphism phình ra cho 2 case.

### 5.4 `layout()` — trung tâm rẽ nhánh (`view.go:22-34`)

```go
func (m model) layout() geometry {
    bodyHeight := max(m.height-1, 3) // status(1); body fills the rest

    if m.width < widthBreakpoint {
        // 1-col stacked
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
    // 2-col side-by-side (giữ nguyên)
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

### 5.5 `topOuterHeight` (mirror `leftOuterWidth`, `view.go:44-57`)

```go
func topOuterHeight(bodyH int, topRatio float64) int {
    to := int(float64(bodyH)*topRatio + 0.5)
    hi := bodyH - minPanelRows
    if hi < minPanelRows {
        hi = minPanelRows // terminal cực thấp: best effort
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

### 5.6 `View()` rẽ nhánh (`view.go:59-83`)

```go
func (m model) View() string {
    if m.width == 0 || m.height == 0 {
        return "loading…"
    }
    g := m.layout()

    var body string
    if g.vertical {
        // 1-col stacked: list trên, preview dưới, cùng width
        top := panelBorder(true).
            Width(g.leftInner).Height(g.innerH).
            Render(m.renderList(g.leftInner, g.innerH))
        bottom := panelBorder(false).
            Width(g.leftInner).Height(g.innerH2).
            Render(m.renderPreview(g.leftInner))
        body = lipgloss.JoinVertical(lipgloss.Left, top, bottom)
    } else {
        // 2-col side-by-side (y như hiện tại)
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

### 5.7 `previewBodyWidth` (`model.go:224-227`) phải branch  ⚠️ (đảm bảo markdown reflow)

```go
func (m model) previewBodyWidth() int {
    g := m.layout()
    if g.vertical {
        return g.leftInner // full m.width - 2
    }
    return g.rightOuter - 2
}
```

Mode flip → giá trị trả về thay đổi → `syncMarkdown` (`model.go:242`) so sánh với
`m.mdWidth`, không khớp → dispatch render mới. Gen-counter
(`model.go:71`, `model.go:268-275`) drop kết quả render cũ. FR7 đạt **không cần
code mới** — chỉ cần helper trên branch đúng.

### 5.8 `handleMouse` rẽ nhánh (`model.go:341-412`)  ⚠️ (điểm dễ sai nhất)

**Trục hit-test đổi theo `g.vertical`:**

```go
func (m model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
    g := m.layout()

    // 1. Motion + Release: chung cho cả hai mode
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
        return m, nil
    }

    // 2. Press: detect divider drag (mỗi mode có axis riêng)
    if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress {
        if g.vertical {
            // Y-divider sống ở rows topOuter-1 (đáy list pane) và topOuter (đỉnh preview pane)
            if msg.Y == g.topOuter-1 || msg.Y == g.topOuter {
                m.dragging = true
                m.setTopFromY(msg.Y)
                return m, nil
            }
        } else {
            // X-divider y như hiện tại
            if msg.Y >= 1 && (msg.X == g.leftOuter-1 || msg.X == g.leftOuter) {
                m.dragging = true
                m.setLeftFromX(msg.X)
                return m, nil
            }
        }
    }

    // 3. Pane hit-test: list ở đâu phụ thuộc orientation
    overList := false
    if g.vertical {
        overList = msg.Y < g.topOuter
    } else {
        overList = msg.X < g.leftOuter
    }

    // 4. Wheel + click logic: giữ pattern cũ, chỉ thay overLeft → overList
    switch msg.Button {
    case tea.MouseButtonWheelUp:
        if overList {
            if m.cursor > 0 { m.cursor--; m.refreshPreview() }
        } else {
            m.scrollPreview(-3)
        }
    case tea.MouseButtonWheelDown:
        if overList {
            if m.cursor < len(m.entries)-1 { m.cursor++; m.refreshPreview() }
        } else {
            m.scrollPreview(3)
        }
    case tea.MouseButtonLeft:
        if msg.Action != tea.MouseActionPress { return m, nil }
        if !overList {
            m.previewClick(msg.Y, g)
            return m, nil
        }
        row := msg.Y - g.firstRow
        if row < 0 || row >= g.innerH { return m, nil }
        idx := g.listTop + row
        if idx < 0 || idx >= len(m.entries) { return m, nil }
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

### 5.9 `setTopFromY` (`model.go`, mirror `setLeftFromX` `model.go:418-423`)

```go
func (m *model) setTopFromY(y int) {
    bodyH := max(m.height-1, 3)
    if bodyH <= 0 {
        return
    }
    m.topRatio = float64(y+1) / float64(bodyH)
}
```

Ratio storage (thay vì row tuyệt đối) giữ split proportional khi terminal resize
height — đúng kỷ luật của `setLeftFromX` cho trục X.

### 5.10 `previewClick` (`model.go:437-476`) sửa nhỏ

Hàm hiện tại lấy `row := y - g.firstRow`. Trong 1-col mode, preview content bắt
đầu ở `g.topOuter + 1` (sau border đáy của list pane), không phải row 1. Đổi
sang `row := y - g.previewFirstRow`:

```go
func (m *model) previewClick(y int, g geometry) {
    // ...
    top, bodyH := m.previewScroll()
    row := y - g.previewFirstRow  // 2-col: 1; 1-col: topOuter+1
    if row < 0 || row >= bodyH {
        return
    }
    // ...
}
```

`previewScroll` (`view.go:126-130`) cần đọc `g.innerH2` thay vì `g.innerH` khi
`g.vertical`:

```go
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

### 5.11 Drag-mid-flip handling (FR8)

`Update`'s `tea.WindowSizeMsg` case (`model.go:305-308`) thêm 1 dòng phát hiện
mode flip → flush `dragging`:

```go
case tea.WindowSizeMsg:
    m.width, m.height = msg.Width, msg.Height
    newVertical := m.width < widthBreakpoint
    if newVertical != m.lastVertical {
        m.dragging = false // axis vừa đổi; cancel drag đang chạy nếu có
    }
    m.lastVertical = newVertical
```

`lastVertical` chỉ phục vụ phát hiện flip; không phải hysteresis. Initial value
`false` đúng cho mọi user vì frame trước `WindowSizeMsg` đầu tiên `m.width == 0`
nên cũng không < 80 (giá trị implicit hợp lệ).

### 5.12 Edge cases tổng hợp

| Tình huống | Hành vi | Cơ chế |
|---|---|---|
| `m.width == 0 \|\| m.height == 0` (pre-WindowSizeMsg) | `"loading…"` | `view.go:60` giữ nguyên |
| `m.width = 0`, vẫn < 80 nhưng View thoát sớm | OK | branching `if m.width == 0` chạy trước `layout()` trong View |
| `m.height < 9` (`= 2*minPanelRows + 1`) trong 1-col | Degenerate, `topOuterHeight` clamp best-effort | `topOuterHeight` đặt `hi = minPanelRows` nếu `bodyH - minPanelRows < minPanelRows` |
| Markdown đang render khi mode flip | `syncMarkdown` thấy `previewBodyWidth()` đổi → dispatch lại; gen-counter drop stale | `model.go:242-267`, `model.go:268-289` |
| Wheel ngay tại row divider 1-col | Treat as preview scroll (`msg.Y >= topOuter`); không trigger drag vì wheel không phải `MouseActionPress` của left button | điều kiện press-detect (5.8 §2) |
| `topRatio` persist khi width đổi trong cùng 1-col | Y-clamp tự động qua `topOuterHeight` | natural |
| `widthBreakpoint=80` ↔ `79` flicker | Chấp nhận v1 (D6); thêm hysteresis nếu user complain | doc deferred |

### 5.13 Đã cân nhắc & **defer khỏi v1**

- **Hysteresis** (dải [80, 84) với state `lastVertical` chống flicker): defer. Ship
  single-threshold trước, đo thực tế. Nếu cần, đổi `widthBreakpoint` thành cặp
  `widthBreakpointDown/Up` + check `m.lastVertical` trong `layout()`.
- **Compact status hints** trong narrow mode: `fitWidth` đã truncate hợp lý;
  thêm hints version riêng = 1 string + branching trong `renderStatus`, không
  xứng giá ở v1.
- **Y-divider keyboard adjust** (vd `+`/`-`): X-drag hiện cũng không có keyboard
  adjust → giữ consistency.
- **Height-based trigger**: chỉ width. Wide+short hiếm gặp và stack ở đó càng
  tệ (mỗi pane rất ngắn).
- **Auto-fit list height theo `len(entries)`**: mất tính predictable; fixed 33/67
  + drag rõ ràng hơn.
- **Configurable threshold qua env/flag**: YAGNI. `widthBreakpoint=80` là magic
  number có lý do trong code, đổi bằng PR khác nếu cần.
- **Panel thứ 3**: `docs/CLAUDE.md` cap 2 panes — không xem xét.

## 6. Acceptance criteria

### Gherkin

```gherkin
Feature: Responsive 2-col / 1-col layout

  When the terminal width is comfortable, the explorer shows list and preview
  side-by-side. When the user splits the screen and width drops below 80
  columns, the explorer stacks the panes — list on top, preview below.

  Background:
    Given the explorer is open beside a project

  Scenario: Wide terminal keeps 2-col side-by-side
    Given the terminal width is 120 columns
    Then the list and preview render side-by-side
    And the divider can be dragged left-right with the mouse

  Scenario: Narrow terminal stacks the layout
    Given the terminal width is 70 columns
    Then the list renders above and the preview below
    And both panes use the full terminal width
    And the status bar stays at the bottom

  Scenario: Resizing across the threshold flips layout in the next frame
    Given the terminal width is 120 columns
    When I shrink the terminal to 70 columns
    Then the layout becomes stacked
    And the currently selected entry stays selected
    And the preview reflows to the new width

  Scenario: User-set splits are remembered across mode flips
    Given the terminal is wide and I have dragged the left pane to 60% width
    When I shrink the terminal below 80 columns
    And I widen the terminal again
    Then the left pane returns to 60% width
    And the top-pane share (stacked mode) is independent of that

  Scenario: Y-divider drag adjusts stacked split
    Given the terminal is 70 columns wide and stacked
    When I drag the row between the list and the preview downward
    Then the list pane grows and the preview pane shrinks
    And neither pane shrinks below the readable floor

  Scenario: Mouse hit-test routes by pane in stacked mode
    Given the terminal is stacked
    When I scroll the wheel inside the top pane
    Then the list cursor moves
    When I scroll the wheel inside the bottom pane
    Then the preview scrolls

  Scenario: Markdown reflows when layout flips
    Given a markdown file is selected
    When the terminal narrows below 80 columns
    Then the preview re-renders at the new full-stack width
    And no raw ANSI escape leaks
    And the chip "• rendering…" appears briefly during the re-render

  Scenario: Drag interrupted by mode flip does not jam
    Given I am dragging the X-divider in 2-col mode
    When the terminal resizes to under 80 columns mid-drag
    Then the drag is cancelled
    And I can start a new Y-drag immediately
```

### Checklist verify

1. Khởi động ở `width=120` → 2-col render đúng như hiện tại (regression-free).
2. Khởi động ở `width=70` → 1-col stacked từ frame đầu tiên (kể cả khi selection
   mặc định là markdown — render đúng `m.width-2` không bị width-0).
3. Resize `120 → 70 → 120`: cursor selection, listTop, previewTop được giữ; sau
   khi quay về `leftRatio` cũ còn hiệu lực.
4. Stacked mode `width=70`: kéo divider Y bằng chuột — list/preview thay đổi tỉ
   lệ, không pane nào tụt dưới `minPanelRows=4`.
5. Stacked mode: wheel up khi cursor chuột ở pane trên → list scroll; wheel ở
   pane dưới → preview scroll; click vào tên file trong pane dưới (folder
   listing) → vào folder đó như 2-col.
6. Markdown reflow: đặt cursor trên `README.md` ở `width=120`, resize xuống `70`,
   xem chip "• rendering…" hiện rồi mất; preview wrap đúng width mới, không
   thấy ANSI thừa.
7. Drag-mid-flip: bắt đầu drag X-divider ở `width=120`, resize xuống `70` trong
   lúc giữ chuột → drag dừng; release; bấm lại Y-divider ở `width=70` → drag
   Y chạy bình thường.
8. `topRatio` & `leftRatio` cùng persist: `width=120, leftRatio→0.6`; resize `70`,
   drag `topRatio→0.5`; resize `120`; quan sát: `leftRatio=0.6` vẫn đúng.
9. Width = 79 (just-below) → 1-col; width = 80 (exact threshold) → 2-col;
   width = 81 → 2-col. Đảm bảo `<` (không `≤`).
10. `go build -o lazyexplorer . && go vet ./... && go test ./...` xanh.
11. `go test -race ./...` xanh (resize spam + markdown render concurrent).
12. Visual verdict (qua `oh-my-claudecode:visual-verdict` skill) cho 2 frame:
    - Frame A: `width=120, height=30`, sample dir, .md selected → 2-col render đẹp
    - Frame B: `width=70, height=30`, same dir, same selection → 1-col stacked đẹp

## 7. Task breakdown

> Map 1-1 với file. Theo project memory, mở rộng bộ hot-path test
> `Resize|Divider|LeftOuter|Press` thêm case stacked layout.

- [ ] **T1 — Constants & state.** Thêm `minPanelRows=4`, `widthBreakpoint=80`
  vào `view.go`; thêm `topRatio float64` + `lastVertical bool` vào `model` struct;
  `newModel` khởi tạo `topRatio: 0.33` (§5.1, §5.2). *(view.go, model.go)*

- [ ] **T2 — `geometry` mở rộng.** Thêm `vertical`, `topOuter`, `bottomOuter`,
  `innerH2`, `previewFirstRow` vào `geometry` struct (§5.3). *(view.go)*

- [ ] **T3 — `topOuterHeight` helper.** Mirror của `leftOuterWidth` cho trục Y,
  clamp với `minPanelRows` (§5.5). *(view.go)*

- [ ] **T4 — `layout()` rẽ nhánh.** Thêm branch `if m.width < widthBreakpoint`
  trả geometry 1-col; nhánh else giữ y nguyên 2-col + populate trường mới
  (§5.4). *(view.go)*

- [ ] **T5 — `View()` rẽ nhánh.** Thêm path `g.vertical` dùng `JoinVertical`;
  giữ path 2-col cũ; cả hai cùng append `renderStatus()` (§5.6). *(view.go)*

- [ ] **T6 — `previewBodyWidth` branch.** Trả `g.leftInner` khi vertical,
  `g.rightOuter-2` khi không (§5.7). *(model.go)*

- [ ] **T7 — `previewScroll` branch.** Đọc `g.innerH2` khi vertical, `g.innerH`
  khi không (§5.10). *(view.go)*

- [ ] **T8 — `previewClick` dùng `previewFirstRow`.** Đổi `row := y - g.firstRow`
  → `row := y - g.previewFirstRow` (§5.10). *(model.go)*

- [ ] **T9 — `setTopFromY` helper.** Mirror của `setLeftFromX` cho trục Y
  (§5.9). *(model.go)*

- [ ] **T10 — `handleMouse` rẽ nhánh.** Thêm Y-divider drag detection, axis-aware
  motion handler, `overList` thay cho `overLeft` (§5.8). *(model.go)*

- [ ] **T11 — `WindowSizeMsg` flush drag.** Phát hiện mode flip qua `lastVertical`
  → set `dragging=false` (§5.11). *(model.go)*

- [ ] **T12 — Tests.**
  - `topOuterHeight`: ratio 0.33 với bodyH 23 → 8; clamp dưới `minPanelRows=4`;
    clamp trên `bodyH - minPanelRows`.
  - `layout()` width-79 → `vertical=true`; width-80 → `vertical=false`; check
    các field theo orientation đúng.
  - `previewBodyWidth` branch: vertical = `m.width-2`; horizontal = `rightOuter-2`.
  - Mouse stacked (`width=70, height=24`): click `msg.Y=2` (in list) → cursor
    moves; click trong folder-preview pane (Y > topOuter) → enter folder.
  - Wheel up `msg.Y < topOuter` → cursor up; `msg.Y >= topOuter` → preview scrolls.
  - Y-drag: press `msg.Y=topOuter`, motion `msg.Y=12` → `topRatio` cập nhật;
    release → `dragging=false`.
  - State preservation: WindowSizeMsg `100 → 70 → 100`, `leftRatio` không đổi;
    `topRatio` set trong khi vertical persist khi quay về vertical lần nữa.
  - Drag-mid-flip: set `dragging=true, lastVertical=false`, gửi
    `WindowSizeMsg{Width:70}` → `dragging=false`.
  - Markdown reflow: `width=100, .md selected, mdWidth=98`; gửi
    `WindowSizeMsg{Width:70}` → `previewBodyWidth=68 ≠ 98` → `syncMarkdown`
    trả Cmd non-nil; chip hiện.
  - `-race`: resize spam (100 ↔ 70) song song với markdown render — không
    panic, không race.
  *(thêm vào file test theo set hot-path hiện có: `resize_test.go`,
  `previewclick_test.go`, `update_markdown_test.go`)*

- [ ] **T13 — Visual verdict harness.** Update `zz_dump_test.go` (hoặc tương
  đương) để dump 2 frame: `width=120,height=30` và `width=70,height=30` với
  fixture chuẩn. Gọi `oh-my-claudecode:visual-verdict` cho cả hai (gated). *(zz_dump_test.go)*

- [ ] **T14 — Verify.** `go build -o lazyexplorer . && go vet ./... && go test ./...`
  xanh; `go test -race ./...` xanh; visual verdict 2 frame đạt; chạy tay kiểm
  acceptance §6 (1–9).

## 8. Files chạm tới (dự kiến)

| File | Thay đổi |
|------|----------|
| `view.go` | + `minPanelRows`, `widthBreakpoint`; `geometry` mở rộng; `topOuterHeight`; `layout()` branch; `View()` branch; `previewScroll` branch |
| `model.go` | + `topRatio`, `lastVertical` vào struct; `newModel` init; `setTopFromY`; `previewBodyWidth` branch; `previewClick` dùng `previewFirstRow`; `handleMouse` branch X/Y; `WindowSizeMsg` flush dragging trên mode flip |
| `resize_test.go` | + cases mode flip, state preservation, drag-mid-flip |
| `previewclick_test.go` | + cases hit-test stacked (Y-axis), `previewFirstRow` semantics |
| `update_markdown_test.go` | + cases markdown reflow khi mode flip, gen-counter drop stale render |
| `zz_dump_test.go` | + frame dump cho cả 2 orientation; visual-verdict gate |
