# PRD — Responsive layout (auto 2-col ↔ 1-col stacked)

> Feature: khi terminal đủ rộng, lazyexplorer giữ 2-col **side-by-side** như hiện
> nay; khi user chia màn hình khiến width co lại dưới một ngưỡng, layout **tự**
> chuyển sang **1-col stacked** — list (trên) + preview (dưới) — đúng kiểu
> [`jesseduffield/lazygit`](https://github.com/jesseduffield/lazygit). Không thêm
> keybind, không thêm mode, không thêm config — chỉ thêm một trục.

Status: **accepted** · Author: brainstorming session · Ngày: 2026-05-27 · Shipped: 2026-05-28 (✅ `go build && go vet && go test ./...` green)

---

## 1. Bối cảnh & vấn đề

lazyexplorer được thiết kế để **sống cạnh agent** trong một pane terminal — và
trong thực tế dùng, user **rất thường chia màn hình làm hai**: nửa cho coding agent,
nửa cho lazyexplorer. Lúc đó width của lazyexplorer thường rơi vào khoảng **60–80
cols** (laptop 13" split 50/50, hoặc terminal lớn split 60/40 cho agent).

Ở dải đó, hai pane side-by-side **đều bị bóp**: `leftInnerWidth()` (`view.go:76-91`)
clamp về `minPanelInnerCols=14` (`view.go:43-47`) cho mỗi bên, divider chiếm thêm 3
cols nữa (`dividerWidth`, `view.go:55`). Concretely tại width 70 với `leftRatio=0.38`
mặc định (`model.go:111`):

- `dividerCenter = round(70*0.38) = 27`, `leftInner = 27 - 1 = 26` → tên file dài bị
  `fitWidth` truncate với "…" (`view.go:346`)
- `rightInner = 70 - 26 - 3 = 41` → markdown wrap rất sớm, code block xuống dòng giữa
  câu

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
| D8 | minPanelInnerRows | `4` (≥4 content rows mỗi pane vertical) | Mirror `minPanelInnerCols=14` (`view.go:43-47`) ở trục Y; dưới mức này panel rỗng tới khó dùng |
| D9 | Vertical divider construct | 1-row glyph `─` ở giữa; hit-zone = đúng 1 row glyph (`dividerHitRowsAbove/Below=0`) | **Visible-equals-hit invariant**: horizontal mode dùng 3 cols dedicated (pad-glyph-pad), hit-zone là chính 3 cols đó — KHÔNG bleed sang list/preview. Mirror sang vertical: 1 row dedicated = 1 row hit. Tránh "fake affordance" (3-row hit-zone) vì sẽ làm first preview row + last list row vừa click vừa drag — vi phạm "click = chọn entry" promise. Single-row click target ở width 80+ là ~80 cells — đủ lớn. Nếu hard to grab empirically → widen `dividerHitRowsAbove/Below` trước khi paint thêm visible pad rows (defer, §5.13) |

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

Đặt cạnh block `minPanelInnerCols` / `dividerWidth` đã có (`view.go:43-62`):

```go
const (
    minPanelInnerCols = 14   // existing
    // … existing dividerPadLeft/dividerPadRight/dividerWidth/dividerGlyph …

    // minPanelInnerRows mirrors minPanelInnerCols ở trục Y khi 1-col stacked: số
    // content rows tối thiểu mỗi pane phải còn để không tụt xuống "panel rỗng tới
    // khó dùng". 4 ≈ 4 entries listing / 4 dòng preview — dưới mức đó panel kém
    // hữu dụng hơn là không hiện ra.
    minPanelInnerRows = 4

    // widthBreakpoint là single threshold cho trigger 1-col vs 2-col (D6, không
    // hysteresis ở v1). m.width < này → vertical. 80 round number gần lazygit
    // default ~84 và khớp half-width laptop ~160.
    widthBreakpoint = 80

    // dividerHeight = 1 row visible glyph (Y-axis mirror của dividerWidth=3).
    // dividerHitRowsAbove/Below = 0 giữ visible-equals-hit invariant của
    // horizontal (3 cols dedicated = 3 cols hit, no bleed). Single-row click
    // target ở width 80+ vẫn ~80 cells — đủ. Nếu khó grab empirically thì bump
    // hit-rows trước khi paint pad row (§5.13 defer).
    dividerHeight       = 1
    dividerHitRowsAbove = 0
    dividerHitRowsBelow = 0
    dividerHGlyph       = "─" // light box-drawing horizontal, cùng family với dividerGlyph
)
```

**Hysteresis defer:** v1 dùng đúng một threshold (D6). Nếu phải thêm sau, đổi
thành cặp `widthBreakpointDown=80` / `widthBreakpointUp=84` + check `m.lastVertical`
trong `layout()` để tránh flicker (state `lastVertical` đã có sẵn cho drag-mid-flip).

### 5.2 State mới trên `model` (`model.go:46-105`)

| Field | Default | Vai trò |
|-------|---------|---------|
| `topRatio float64` | `0.33` | Tỉ lệ chiều cao của list pane khi 1-col stacked; drag-adjustable theo Y. Mirror của `leftRatio` (`model.go:89`). |
| `lastVertical bool` | `false` | Lưu `vertical` của lần render trước, dùng riêng để phát hiện mode flip mid-drag (FR8). KHÔNG dùng cho hysteresis ở v1. |

`leftRatio`, `dragging`, `cursor`, `listTop`, `previewTop`, các async-render fields
(`renderGen`, `pendingWidth`, `renderStartedAt`): giữ y nguyên. `vertical` **không**
lưu trên model — `layout()` tính purely từ `m.width` mỗi lần (single source, no drift).

`newModel` thêm khởi tạo: `m := model{root: root, cwd: root, leftRatio: 0.38, topRatio: 0.33, tel: tel}`
(`model.go:107-114`).

### 5.3 `geometry` mở rộng (`view.go:21-28`)

```go
type geometry struct {
    vertical bool // true → 1-col stacked; false → 2-col side-by-side

    // axis-X (horizontal mode primary; vertical mode: leftInner = m.width, others = 0)
    leftInner    int // cols trong list pane (horizontal); cols trong cả hai pane (vertical, = m.width)
    rightInner   int // cols trong preview pane (horizontal only); 0 khi vertical
    dividerStart int // first col của vertical divider strip (horizontal only); 0 khi vertical

    // axis-Y (vertical mode primary; horizontal mode: 0)
    topInner     int // rows nội dung của list pane (vertical only); 0 khi horizontal
    bottomInner  int // rows nội dung của preview pane (vertical only); 0 khi horizontal
    dividerYStart int // first row của horizontal divider strip (vertical only); 0 khi horizontal

    // shared
    bodyH           int // total body rows = m.height - 1 (status sits at row m.height-1)
    listTop         int // index của entry đầu tiên visible trong list
    firstRow        int // screen Y của entry đầu trong list — luôn = 0 (no top border, borderless layout)
    previewFirstRow int // screen Y của row đầu trong preview content (horizontal: 0; vertical: topInner+dividerHeight)
}
```

Lý do gom hai mode vào **một struct**: `View()` và `handleMouse` vẫn gọi `m.layout()`
đúng một chỗ; `g.vertical` là source of truth cho mọi rẽ nhánh sau đó. Không
tách `geometryHoriz/geometryVert` để tránh polymorphism phình ra cho 2 case. Mặc
định int = 0 cho field không liên quan tới mode hiện tại đủ để render path nào
đọc nhầm cũng sẽ render rỗng thay vì crash — cùng kỷ luật defensive với
`leftInnerWidth` clamp khi terminal cực hẹp (`view.go:81-83`).

### 5.4 `layout()` — trung tâm rẽ nhánh (`view.go:30-41`)

```go
func (m model) layout() geometry {
    bodyH := max(m.height-1, 3) // status(1); body fills the rest

    if m.width < widthBreakpoint {
        // 1-col stacked. Borderless → list/preview/divider chia bodyH theo rows
        // không có chrome row nào trừ chính dividerHeight=1 row glyph.
        topInner := topInnerHeight(bodyH, m.topRatio)
        return geometry{
            vertical:        true,
            leftInner:       m.width, // cả hai pane đều dùng full width khi vertical
            bodyH:           bodyH,
            topInner:        topInner,
            bottomInner:     bodyH - topInner - dividerHeight,
            dividerYStart:   topInner, // glyph row Y (0-indexed)
            listTop:         m.listTopFor(topInner),
            firstRow:        0,
            previewFirstRow: topInner + dividerHeight, // ngay sau row glyph
        }
    }

    // 2-col side-by-side — y như current borderless implementation, chỉ thêm
    // vertical:false explicit và previewFirstRow=0 cho consistency.
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
```

**Footgun đã chốt:** `listTop` MUST dùng `topInner` (không phải `bodyH`) khi vertical
— list pane chỉ cao `topInner` rows, scroll math sai sẽ làm cursor lệch khỏi vùng
visible. Pre-mortem case này được trace trong test §6 checklist mục 4.

### 5.5 `topInnerHeight` (mirror `leftInnerWidth`, `view.go:76-91`)

```go
// topInnerHeight: dividerYStart = round(bodyH * topRatio), nên list pane cao
// dividerYStart rows. Mirror semantics của leftInnerWidth (divider-center
// storage) — giữ proportional khi terminal resize height.
func topInnerHeight(bodyH int, topRatio float64) int {
    dividerCenterY := int(float64(bodyH)*topRatio + 0.5)
    ti := dividerCenterY // dividerHeight=1 nên không có pad-top trừ vào

    // Chừa ≥ minPanelInnerRows cho preview pane + dividerHeight cho row glyph.
    hi := bodyH - dividerHeight - minPanelInnerRows
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
```

### 5.6 `View()` rẽ nhánh (`view.go:106-135`)

`View()` đã return `tea.View` (struct của Bubbletea v2, content + AltScreen +
MouseMode). Mọi return path phải set AltScreen + MouseMode, kể cả early "loading…"
frame, nếu không program toggle out of alt screen + mất mouse reporting trước
`WindowSizeMsg` đầu tiên — kỷ luật này đã có sẵn, **không** đổi.

```go
func (m model) View() tea.View {
    content := "loading…"
    if m.width != 0 && m.height != 0 {
        g := m.layout()

        if g.vertical {
            // 1-col stacked: full-width list pane (rows = topInner), 1-row horizontal
            // divider, full-width preview pane (rows = bottomInner). Borderless: Style
            // chỉ đặt Width/Height; không có panelBorder() wrapper. Mirror cách
            // horizontal mode build divider line (Repeat dividerGlyph) — nhưng theo
            // chiều ngang: dividerLine là m.width copies of dividerHGlyph với dimStyle.
            list := lipgloss.NewStyle().
                Width(g.leftInner).Height(g.topInner).
                Render(m.renderList(g.leftInner, g.topInner))

            dividerRow := dimStyle.Render(strings.Repeat(dividerHGlyph, g.leftInner))

            preview := lipgloss.NewStyle().
                Width(g.leftInner).Height(g.bottomInner).
                Render(m.renderPreview(g.leftInner))

            body := lipgloss.JoinVertical(lipgloss.Left, list, dividerRow, preview)
            content = strings.Join([]string{body, m.renderStatus()}, "\n")
        } else {
            // 2-col side-by-side: y như current borderless implementation
            // (view.go:111-128) — không đổi.
            left := lipgloss.NewStyle().
                Width(g.leftInner).Height(g.bodyH).
                Render(m.renderList(g.leftInner, g.bodyH))

            right := lipgloss.NewStyle().
                Width(g.rightInner).Height(g.bodyH).
                Render(m.renderPreview(g.rightInner))

            dividerLine := strings.Repeat(" ", dividerPadLeft) +
                dimStyle.Render(dividerGlyph) +
                strings.Repeat(" ", dividerPadRight)
            divider := strings.TrimRight(strings.Repeat(dividerLine+"\n", g.bodyH), "\n")

            body := lipgloss.JoinHorizontal(lipgloss.Top, left, divider, right)
            content = strings.Join([]string{body, m.renderStatus()}, "\n")
        }
    }

    v := tea.NewView(content)
    v.AltScreen = true
    v.MouseMode = tea.MouseModeCellMotion
    return v
}
```

### 5.7 `previewBodyWidth` (`model.go:304-309`) phải branch  ⚠️ (đảm bảo markdown reflow)

```go
func (m model) previewBodyWidth() int {
    g := m.layout()
    if g.vertical {
        return g.leftInner // = m.width, borderless không trừ chrome
    }
    return g.rightInner
}
```

Mode flip → giá trị trả về thay đổi → `syncPreview` (`model.go:324-359`) so sánh với
`m.srcWidth` / `m.pendingWidth`, không khớp → dispatch render mới. Gen-counter
(`model.go:82`, `model.go:367-421`) drop kết quả render cũ. FR7 đạt **không cần
code mới** — chỉ cần helper trên branch đúng. Lưu ý: `syncPreview` xử lý cả markdown
**và** code highlight (chroma) qua cùng renderer registry (`fs.go:219-244`); mode flip
re-dispatch tự động cho mọi renderer.

### 5.8 `handleMouse` rẽ nhánh (`model.go:482-573`)  ⚠️ (điểm dễ sai nhất)

Bubbletea v2 encode mouse action qua message TYPE (`tea.MouseMotionMsg` /
`MouseReleaseMsg` / `MouseWheelMsg` / `MouseClickMsg`), KHÔNG có `msg.Action`.
`e := msg.Mouse()` lấy `{X, Y, Button}` chung. Hit-test divider đổi axis theo
`g.vertical`; mọi nhánh khác đi qua `overList` flag.

```go
func (m model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
    g := m.layout()
    e := msg.Mouse()

    // overDivider band rộng theo orientation:
    //   horizontal — 3 cols [dividerStart, dividerStart+dividerWidth) (giữ nguyên)
    //   vertical   — 3 rows [dividerYStart-dividerHitRowsAbove, dividerYStart+dividerHitRowsBelow]
    var overDivider bool
    if g.vertical {
        overDivider = e.Y >= g.dividerYStart-dividerHitRowsAbove &&
            e.Y <= g.dividerYStart+dividerHeight-1+dividerHitRowsBelow
    } else {
        overDivider = e.X >= g.dividerStart && e.X < g.dividerStart+dividerWidth
    }

    switch msg.(type) {
    case tea.MouseMotionMsg:
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
        return m, nil

    case tea.MouseWheelMsg:
        // Wheel over divider → noop (FR9). Neither pane owns those rows/cols.
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
                if m.cursor > 0 { m.cursor--; m.refreshPreview() }
            } else {
                m.scrollPreview(-3)
            }
        case tea.MouseWheelDown:
            if overList {
                if m.cursor < len(m.entries)-1 { m.cursor++; m.refreshPreview() }
            } else {
                m.scrollPreview(3)
            }
        }
        return m, nil

    case tea.MouseClickMsg:
        // Left-press in divider band → start drag (snap glyph to cursor).
        // Excluding the status row (m.height-1) so click on status text under
        // divider's range không accidentally start drag (mirror horizontal).
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
        // Left-click on divider that wasn't a drag-start → noop (FR7).
        if overDivider {
            return m, nil
        }
        overList := false
        if g.vertical {
            overList = e.Y < g.dividerYStart
        } else {
            overList = e.X < g.dividerStart
        }
        if !overList {
            m.previewClick(e.Y, g)
            return m, nil
        }
        // List click: row index relative to firstRow (=0 in current borderless).
        row := e.Y - g.firstRow
        listH := g.bodyH
        if g.vertical {
            listH = g.topInner
        }
        if row < 0 || row >= listH {
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

### 5.9 `setTopFromY` (`model.go`, mirror `setLeftFromX` `model.go:581-586`)

```go
// setTopFromY pins the horizontal divider glyph under cursor row y:
// row y becomes dividerYStart, so topRatio = y / bodyH. Mirror semantics
// của setLeftFromX (post-borderless: x = dividerCenter, không có +1) cho trục Y.
func (m *model) setTopFromY(y int) {
    bodyH := max(m.height-1, 3)
    if bodyH <= 0 {
        return
    }
    m.topRatio = float64(y) / float64(bodyH)
}
```

Ratio storage (thay vì row tuyệt đối) giữ split proportional khi terminal resize
height — đúng kỷ luật của `setLeftFromX` cho trục X. Formula `y / bodyH` khớp với
choice `dividerHeight=1` + glyph row Ở row `dividerYStart` (D9).

### 5.10 `previewClick` (`model.go:602-640`) + `previewScroll` (`view.go:271-275`)

`previewClick` đang dùng `row := y - g.firstRow` (firstRow=0 trong current borderless).
Đổi sang `row := y - g.previewFirstRow` — trong horizontal mode `previewFirstRow=0`
nên hành vi giữ nguyên; trong vertical mode `previewFirstRow = topInner + dividerHeight`
nên click trong preview pane (row ≥ dividerYStart + dividerHeight) được normalize đúng.

```go
func (m *model) previewClick(y int, g geometry) {
    // ...
    top, bodyH := m.previewScroll()
    row := y - g.previewFirstRow  // horizontal: 0; vertical: topInner+dividerHeight
    if row < 0 || row >= bodyH {
        return
    }
    // ...
}
```

`previewScroll` cần branch trả `g.bottomInner` khi vertical, `g.bodyH` khi horizontal —
mirror cùng pattern `previewClick`:

```go
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
```

Lưu ý: `previewLen()` (đã có ở `view.go:258-263`) đã trừu tượng over file lines vs
folder entries — `previewScroll` chỉ cần đổi `bodyH` source theo orientation, không
phải gọi-trực-tiếp `len(m.preview)`.

### 5.11 Drag-mid-flip handling (FR8)

`Update`'s `tea.WindowSizeMsg` case (`model.go:437-440`) thêm flip detection để
flush `dragging`. **Ordering quan trọng:** set `dragging=false` TRƯỚC khi update
`lastVertical`, để frame kế tiếp (gồm tail `syncPreview` cùng turn) đã thấy state
sạch không còn drag-defer.

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
`false` an toàn cho mọi user **không phải** vì frame trước `WindowSizeMsg` đầu tiên
có width<80 (`0 < 80` là true), mà vì `m.dragging` cũng default `false` lúc đó —
nên kể cả khi initial WindowSizeMsg dispatch frame-1 flip detection nó cũng chỉ
là no-op flush. Cụ thể:
- Initial frame width ≥ 80 → `newVertical=false`, `lastVertical` chuyển false→false, không flush — đúng.
- Initial frame width < 80 → `newVertical=true`, `lastVertical` chuyển false→true, `m.dragging=false` được set false (đã false sẵn) — vẫn đúng.

### 5.12 Edge cases tổng hợp

| Tình huống | Hành vi | Cơ chế |
|---|---|---|
| `m.width == 0 \|\| m.height == 0` (pre-WindowSizeMsg) | `"loading…"` content; AltScreen + MouseMode vẫn set | `view.go:107-108` giữ nguyên |
| `m.width = 0`, vẫn < 80 nhưng View thoát sớm | OK | branching `if m.width != 0 && m.height != 0` chạy trước `layout()` trong View |
| `m.height < 2*minPanelInnerRows + dividerHeight = 9` trong vertical | Degenerate, `topInnerHeight` clamp best-effort | `topInnerHeight` đặt `hi = minPanelInnerRows` nếu `bodyH - dividerHeight - minPanelInnerRows < minPanelInnerRows` |
| Markdown / code đang render khi mode flip | `syncPreview` thấy `previewBodyWidth()` đổi → dispatch lại; gen-counter drop stale | `model.go:324-359`, `model.go:367-421` |
| Wheel ngay tại divider band vertical | Noop (FR9) — `overDivider` gate ở §5.8 wheel branch | điều kiện `overDivider` check trước `overList` |
| Click status row trong vertical mode | `e.Y < m.height-1` guard trên click-press → noop nếu click vào row status text dù X/Y match divider | điều kiện press-detect §5.8 MouseClickMsg |
| `topRatio` persist khi height đổi trong cùng vertical mode | Y-clamp tự động qua `topInnerHeight` (giá trị ratio không đổi) | natural |
| `widthBreakpoint=80` ↔ `79` flicker quanh ngưỡng | Chấp nhận v1 (D6); thêm hysteresis nếu user complain | doc deferred (§5.13) |
| Click 1 row sát ngoài glyph (`dividerYStart - 1` hoặc `dividerYStart + 1`) | Route theo pane: row trên → list-click entry tương ứng; row dưới → preview-click (folder listing entry hoặc noop file preview); KHÔNG accidentally trigger drag | `dividerHitRowsAbove=0`, `dividerHitRowsBelow=0` — visible-equals-hit invariant |

### 5.13 Đã cân nhắc & **defer khỏi v1**

- **Hysteresis** (dải [80, 84) với state `lastVertical` chống flicker): defer. Ship
  single-threshold trước, đo thực tế. Nếu cần, đổi `widthBreakpoint` thành cặp
  `widthBreakpointDown/Up` + check `m.lastVertical` trong `layout()` (state đã sẵn
  cho drag-mid-flip — đổi lý do dùng nó là OK).
- **Widen vertical-divider hit-zone** (bump `dividerHitRowsAbove/Below` lên 1):
  defer. Single-row hit ở width 80+ là ~80 cells click target — đủ trên thực tế.
  Nếu user complain bar khó grab thì widen hit-rows (giữ visible 1 row) TRƯỚC
  khi paint thêm visible pad rows (cost = rows). One-constant flip.
- **Visible pad rows cho horizontal divider** (mirror `dividerPadLeft/Right`):
  defer xa hơn. Chỉ xem xét nếu hit-zone widening (step trên) vẫn không đủ.
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

- [ ] **T1 — Constants & state.** Thêm `widthBreakpoint=80`, `minPanelInnerRows=4`,
  `dividerHeight=1`, `dividerHitRowsAbove=1`, `dividerHitRowsBelow=1`, `dividerHGlyph='─'`
  vào constants block của `view.go`; thêm `topRatio float64` + `lastVertical bool`
  vào `model` struct; `newModel` khởi tạo `topRatio: 0.33` (§5.1, §5.2). *(view.go, model.go)*

- [ ] **T2 — `geometry` mở rộng.** Thêm `vertical`, `topInner`, `bottomInner`,
  `dividerYStart`, `previewFirstRow` vào `geometry` struct; giữ field set
  hiện có (§5.3). *(view.go)*

- [ ] **T3 — `topInnerHeight` helper.** Mirror của `leftInnerWidth` cho trục Y,
  clamp với `minPanelInnerRows` + `dividerHeight` (§5.5). *(view.go)*

- [ ] **T4 — `layout()` rẽ nhánh.** Thêm branch `if m.width < widthBreakpoint`
  trả geometry 1-col stacked; nhánh else giữ 2-col + thêm `vertical:false`
  + `previewFirstRow:0` explicit (§5.4). *(view.go)*

- [ ] **T5 — `View()` rẽ nhánh.** Thêm path `g.vertical` dùng `JoinVertical`
  với 1-row horizontal divider strip (`dividerHGlyph` repeat); giữ path 2-col
  cũ; cả hai cùng append `renderStatus()`; `tea.View` AltScreen + MouseMode
  set ở mọi return path (§5.6). *(view.go)*

- [ ] **T6 — `previewBodyWidth` branch.** Trả `g.leftInner` (= m.width) khi
  vertical, `g.rightInner` khi không (§5.7). *(model.go)*

- [ ] **T7 — `previewScroll` branch.** Đọc `g.bottomInner` khi vertical,
  `g.bodyH` khi không (§5.10). *(view.go)*

- [ ] **T8 — `previewClick` dùng `previewFirstRow`.** Đổi `row := y - g.firstRow`
  → `row := y - g.previewFirstRow` (§5.10). *(model.go)*

- [ ] **T9 — `setTopFromY` helper.** Mirror của `setLeftFromX` cho trục Y
  (post-borderless: y = dividerYStart không có +1) (§5.9). *(model.go)*

- [ ] **T10 — `handleMouse` rẽ nhánh.** Thêm Y-divider hit-zone band, axis-aware
  motion handler, `overList` thay cho `overLeft`, list-click bound check
  dùng `g.topInner` vs `g.bodyH` theo orientation (§5.8). *(model.go)*

- [ ] **T11 — `WindowSizeMsg` flush drag.** Phát hiện mode flip qua `lastVertical`,
  set `dragging=false` TRƯỚC khi update `lastVertical` (§5.11). *(model.go)*

- [ ] **T12 — Tests.**
  - `topInnerHeight`: ratio 0.33 với bodyH=23 → 8 (= round(23*0.33)); clamp dưới
    `minPanelInnerRows=4`; clamp trên `bodyH - dividerHeight - minPanelInnerRows`;
    degenerate `bodyH=8` → hi=minPanelInnerRows best-effort.
  - `layout()` boundaries: width=79 → `vertical=true`; width=80 → `vertical=false`;
    width=81 → `vertical=false`; check tất cả field per orientation đúng (zero-value
    cho field không thuộc orientation hiện tại).
  - `previewBodyWidth` branch: vertical = `m.width`; horizontal = `g.rightInner`.
  - Mouse stacked (`width=70, height=24, topRatio=0.33`): bodyH=23, topInner=8,
    dividerYStart=8, previewFirstRow=9, bottomInner=14. Click `e.Y=2` (in list)
    → cursor moves; click `e.Y=9` (first preview row) trong folder-preview → enter
    folder (visible-equals-hit invariant — first preview row is NOT drag-zone).
  - Wheel `e.Y < dividerYStart` → cursor; `e.Y > dividerYStart` → preview scrolls;
    `e.Y == dividerYStart` → noop (FR9, single-row hit zone).
  - Y-drag press walk: `e.Y` từ `dividerYStart-2` đến `dividerYStart+2` (`6..10` ở
    fixture trên) — assert drag-start ở `[8,8]` ONLY (single visible glyph row);
    không drag ở `6,7,9,10` (adjacent rows route to list/preview như mọi click khác).
  - Y-drag motion: press `e.Y=8`, motion `e.Y=12` → `topRatio = 12/23 ≈ 0.522`;
    release → `dragging=false`.
  - State preservation: WindowSizeMsg `100 → 70 → 100`, `leftRatio` không đổi;
    `topRatio` set trong khi vertical persist khi quay về vertical lần nữa.
  - Drag-mid-flip ordering: `dragging=true, lastVertical=false`, gửi
    `WindowSizeMsg{Width:70}` → `dragging=false` (cleared trước update lastVertical).
  - Markdown reflow: `width=100, .md selected, m.srcWidth=g.rightInner cũ`; gửi
    `WindowSizeMsg{Width:70}` → `previewBodyWidth` đổi từ rightInner → m.width=70 →
    `syncPreview` thấy mismatch → trả Cmd non-nil; chip "• rendering…" hiện.
  - Listing-row 0 click in vertical: `e.Y=0` chọn entry 0 đúng (firstRow=0 same
    as horizontal — guards against off-by-one).
  - List-click bound check vertical: `e.Y=topInner-1` (row cuối list pane) hợp lệ;
    `e.Y=topInner` (= dividerYStart) đi vào divider branch không list branch.
  - `-race`: resize spam (100 ↔ 70) song song với markdown render — không
    panic, không race.
  *(thêm vào file test theo set hot-path hiện có: `resize_test.go`,
  `previewclick_test.go`, `update_markdown_test.go`, `divider_test.go`)*

- [ ] **T13 — Visual verdict harness.** Update `zz_dump_test.go` (hoặc tương
  đương) để dump 2 frame: `width=120,height=30` và `width=70,height=30` với
  fixture chuẩn. Gọi `oh-my-claudecode:visual-verdict` cho cả hai (gated). *(zz_dump_test.go)*

- [ ] **T14 — Verify.** `go build -o lazyexplorer . && go vet ./... && go test ./...`
  xanh; `go test -race ./...` xanh; visual verdict 2 frame đạt; chạy tay kiểm
  acceptance §6 (1–9).

## 8. Files chạm tới (dự kiến)

| File | Thay đổi |
|------|----------|
| `view.go` | + constants `widthBreakpoint=80`, `minPanelInnerRows=4`, `dividerHeight=1`, `dividerHitRowsAbove/Below=1`, `dividerHGlyph="─"`; `geometry` mở rộng (`vertical`, `topInner`, `bottomInner`, `dividerYStart`, `previewFirstRow`); `topInnerHeight` helper; `layout()` branch X/Y; `View()` branch JoinHorizontal/JoinVertical; `previewScroll` branch theo `g.vertical` |
| `model.go` | + `topRatio float64`, `lastVertical bool` vào struct; `newModel` init `topRatio: 0.33`; `setTopFromY` helper; `previewBodyWidth` branch; `previewClick` dùng `g.previewFirstRow`; `handleMouse` branch X/Y axis hit-zone band; `Update`'s `WindowSizeMsg` case flush `dragging` trên mode flip TRƯỚC khi update `lastVertical` |
| `resize_test.go` | + cases mode flip boundaries (79/80/81), state preservation (leftRatio + topRatio persist), drag-mid-flip ordering, `topInnerHeight` clamp |
| `previewclick_test.go` | + cases hit-test stacked (Y-axis), `previewFirstRow` semantics, list-click bound check trong vertical |
| `divider_test.go` | + cases Y-drag zone walk [dividerYStart±2], wheel-noop trong band, setTopFromY snap |
| `update_markdown_test.go` | + cases preview reflow khi mode flip (markdown + code), gen-counter drop stale render |
| `zz_dump_test.go` | + frame dump cho cả 2 orientation (120×30 horizontal + 70×30 vertical); visual-verdict gate |
