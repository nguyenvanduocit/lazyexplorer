# PRD — Pane focus state cho keyboard-driven scroll preview

> Bug-shaped feature: hiện không có khái niệm "focus pane" giữa list và preview.
> `up/down/j/k` **luôn** move cursor trong list pane (`model.go:624-633`). User
> chỉ-bàn-phím (no mouse) không có cách trực quan để scroll preview — phải nhớ
> `J/K` (shift) hoặc `ctrl+d/u`, không có gợi ý nào trên UI. Cùng workflow trên
> chuột thì wheel **đã** context-aware (`model.go:494-516`) — keyboard bị bỏ
> quên.

Status: **accepted** · Author: feature-dev session · Ngày: 2026-05-28 · Shipped: 2026-05-28

---

## 1. Bối cảnh & vấn đề

Phân tích đường keyboard hiện tại trong `updateNormal` (`model.go:619-664`):

| Phím | Hành vi hiện tại | Pane bị ảnh hưởng |
|------|------------------|-------------------|
| `up` / `k` | `m.cursor--` | List |
| `down` / `j` | `m.cursor++` | List |
| `g` | `m.cursor = 0` | List |
| `G` | `m.cursor = lastIndex` | List |
| `enter` / `l` / `right` | `descend()` | List |
| `backspace` / `h` / `left` | `ascend()` | List |
| `J` / `ctrl+d` | `scrollPreview(5)` | Preview (PRD này xoá `J/K`) |
| `K` / `ctrl+u` | `scrollPreview(-5)` | Preview (PRD này xoá `J/K`) |
| `r` | enter `modeRename` | List selection |
| `d` | enter `modeConfirmDelete` | List selection |

Đối chiếu với wheel mouse (`model.go:494-516`):

```go
overLeft := e.X < g.dividerStart
case tea.MouseWheelUp:
    if overLeft { m.cursor-- } else { m.scrollPreview(-3) }
case tea.MouseWheelDown:
    if overLeft { m.cursor++ } else { m.scrollPreview(3) }
```

Chuột **đã** biết "đang ở pane nào → áp action vào pane đó". Bàn phím
chưa — phải nhớ một bộ phím riêng (`J/K`, uppercase, không có gợi ý trong
hint bar — xem `view.go:257` chỉ ghi `[wheel] scroll`).

Hệ quả cho user keyboard-only đứng cạnh agent:

1. **Không discoverable**: không có nhãn / indicator nào báo "muốn scroll
   preview thì dùng `J/K`". Hint bar (`view.go:257`) liệt kê `[↑↓/jk/click]
   move  [enter/l] open  [h/bksp] up  [r] rename  [d] delete  [wheel] scroll
   [q] quit` — không có dòng nào về preview scroll qua keyboard.
2. **Không trực giác**: user mong "tôi đang đọc preview, ↑↓ phải scroll
   preview" — như editor / pager (less, vim). lazyexplorer phá kỳ vọng đó.
3. **Không có focus signal nào**: layout borderless (một divider 3 cột giữa
   hai pane, không border quanh pane — xem `prd-middle-divider.md`) nên không
   có border màu để báo pane nào đang "active". UI cần một tín hiệu focus mới
   sống độc lập với geometry — chip ở status bar + cursor row dim (§5.6, §5.5).
4. **Mâu thuẫn với ux mouse**: wheel context-aware, keyboard không. Hai
   modality trong cùng app dùng hai mental model khác nhau cho cùng hành
   động "scroll".

Reference clones (không copy):

- `tmp/gh-dash` — section-based focus, `Tab`/`Shift+Tab` switch giữa các
  section, có visual indicator (border + title color) — đọc xem cách họ
  hold focus state trên model + render conditional style.
- `tmp/crush` — coding-agent TUI có multi-pane, focus signal qua title color
  + border weight; reference cho status bar focus chip (xem
  `[[reference_crush_tui]]`).
- `tmp/lipgloss` — `Border` + `BorderForeground` thay đổi runtime — chính
  thư viện styling của project.

> Note: layout borderless của `prd-middle-divider.md` đã ship trước, nên
> focus signal đi hoàn toàn qua status-bar chip + cursor row dim (§5.7).

## 2. Goal (1 câu)

Thêm trạng thái `focusPane` (list | preview) trên `model`; `Tab` toggle
focus; phím `up/down/j/k/g/G/ctrl+d/u` áp dụng cho pane đang focus; focus
có indicator visible (status-bar chip + cursor row dim); click vào pane nào
sẽ set focus vào pane đó — đồng bộ keyboard với mouse mental model.

**Non-goal làm rõ:**
- KHÔNG thêm pane thứ ba — vẫn giữ two-panel ceiling của project.
- KHÔNG mở file trong `$EDITOR` khi focus preview + Enter (defer).
- KHÔNG đổi behavior mouse wheel (đã context-aware, không cần đụng).
- KHÔNG giữ `J/K` legacy shortcut — clean-slate: focus + lowercase đủ,
  không cần 2 cách scroll preview.
- KHÔNG thêm `Shift+Tab` — 2 pane thì `Tab` toggle là đủ, không cần
  forward/backward.
- KHÔNG đụng `mode` enum của rename/delete — focus là sub-state của
  `modeNormal`, orthogonal với mode prompt.
- KHÔNG ship cùng `prd-middle-divider.md` — hai PRD đều chạm `view.go` +
  border, ship riêng và resolve conflict trong PR sau (§5.7).

## 3. Quyết định đã chốt

| # | Quyết định | Giá trị | Lý do |
|---|-----------|---------|-------|
| D1 | Tên state | `focusPane` enum trên `model` (giá trị `focusList`, `focusPreview`) — **orthogonal** với `mode` | `mode` đang để Rename/Delete prompt; focus pane không liên quan. Một enum thứ hai rõ ràng hơn cờ boolean hay merge vào `mode` |
| D2 | Default focus khi launch | `focusList` | Workflow mở app: user thấy danh sách file trước, chọn → preview xuất hiện. List là entry point |
| D3 | Toggle key | `Tab` (one key, no Shift+Tab) | 2 pane thì forward = backward; thêm phím chỉ tạo cognitive load. Một phím đủ |
| D4 | Phím "scroll-ish" context-aware | `up`/`down`/`j`/`k`/`g`/`G`/`ctrl+d`/`ctrl+u` đổi đích theo focus | Mental model thống nhất với wheel; arrow + j/k là phím "navigation trong pane đang xem" tự nhiên |
| D5 | Phím "mutation/navigation list-bound" | `r`/`d`/`enter`/`l`/`h`/`backspace`/`right`/`left` chỉ hoạt động khi `focusPane == focusList` | Mutation cần "list selection" có nghĩa; nếu focus preview mà bấm `d`, ambiguous (delete file đang xem? cancel?). An toàn: noop |
| D6 | `Esc` khi focus preview | Switch focus về list | Esc trong app là "cancel / step back" universal; list là default → quay về |
| D7 | Click chuột set focus | Click bất kỳ ô nào trong list pane → `focusList`; click pane preview → `focusPreview` | Đồng bộ mouse + keyboard mental model: pane bạn vừa interact = pane bạn focus |
| D8 | Focus indicator chính | Status bar **chip** `[ list ]` / `[ preview ]` ở đầu hint string, tô `focusChipStyle` (`colAccent` background) | Layout borderless (one 3-col divider, no pane border) → chip là tín hiệu focus chính, độc lập với geometry; chỉ đổi nội dung khi focus đổi |
| D9 | Focus indicator phụ | **Cursor row dim** khi `focusPane == focusPreview` (D10) | Củng cố chip: list vẫn chỉ rõ "cursor đang ở file nào" nhưng softer, dành accent cho pane đang focus |
| D10 | Cursor row khi `focusPane == focusPreview` | `cursorActiveStyle` nhưng background đổi sang `colDim` | List vẫn cho biết "cursor đang ở đâu" nhưng softer; phục hồi accent khi focus quay lại list |
| D11 | `ctrl+d/u` context-aware | Focus list → cursor xuống/lên `max(1, bodyH/2)` rows; Focus preview → preview scroll `±max(1, bodyH/2)` | Half-page semantics chuyển theo focus; "half-page của pane đang xem" |
| D12 | `g`/`G` context-aware | Focus list → cursor về 0 / `len-1` (giữ nguyên); Focus preview → `previewTop = 0` / `maxTop` | Vi convention "top/bottom of current viewport" |
| D13 | `J`/`K` legacy | **Xoá** | Clean-slate: focus + lowercase `j/k` đã đủ scroll preview; không cần 2 cách cho cùng 1 hành vi |
| D14 | Poll loop khi focus preview | Không đụng — `tickCmd` (`model.go:431-436`) check `mode == modeNormal && !dragging`; focus là sub-state của modeNormal nên poll vẫn chạy bình thường | `syncFromDisk` cập nhật list khi agent ghi file — không phụ thuộc focus. Behavior giữ nguyên |
| D15 | Tab khi đang `modeRename`/`modeConfirmDelete` | Tab fallthrough vào `updateRename`/`updateConfirmDelete` như mọi phím khác. KHÔNG switch focus trong prompt mode | Prompt mode "freeze" pane interaction; focus chỉ relevant ở `modeNormal` |

## 4. Functional requirements

- **FR1** — `model` thêm field `focusPane focusPane` với enum
  `focusList=0` (zero value = default) và `focusPreview=1`. `newModel`
  không cần set explicit — zero value là `focusList` (D2).

- **FR2** — Trong `modeNormal`, `Tab` toggle: `focusList ↔ focusPreview`.
  Không xử lý `Shift+Tab` — Tab toggle là một-key đủ. Statusmsg không đổi.

- **FR3** — `up`/`down`/`j`/`k`/`g`/`G`/`ctrl+d`/`ctrl+u` đọc
  `m.focusPane` và route:
  - `focusList` → behavior hiện tại trong `updateNormal` (cursor move,
    g/G top/bottom của list). `ctrl+d/u` mới: jump cursor `±max(1, bodyH/2)`
    rows (D11) thay vì scroll preview ±5.
  - `focusPreview` → `up/k` = `scrollPreview(-1)`; `down/j` = `scrollPreview(1)`;
    `g` = `m.previewTop = 0`; `G` = `m.previewTop = maxTop`; `ctrl+d/u` =
    `scrollPreview(±max(1, bodyH/2))`.

- **FR4** — `J`/`K` legacy bị **xoá** khỏi `updateNormal` (`model.go:647-650`).
  Sau PRD này, không có phím viết hoa cho preview scroll — focus
  + `j/k` là cách duy nhất.

- **FR5** — `enter`/`l`/`right`/`h`/`backspace`/`left`/`r`/`d` chỉ hoạt
  động khi `focusPane == focusList`. Khi `focusPreview`, các phím này là
  **noop** — không status message, không side effect.

- **FR6** — `Esc` khi `focusPane == focusPreview` ở `modeNormal` →
  `focusPane = focusList`. Không clear `m.statusMsg`. Không đụng các
  hành vi Esc khác (rename mode đã có Esc cancel — `model.go:739`).

- **FR7** — `q` / `ctrl+c` quit bất kể focus (an toàn — universal exit).

- **FR8** — Click chuột trái trong list pane (`overLeft := e.X < g.dividerStart`)
  → `focusPane = focusList` đồng thời với hành vi click hiện tại (select
  entry / start drag). Click trong preview pane (`!overLeft`) →
  `focusPane = focusPreview` đồng thời `previewClick`. Click vào divider
  (drag-start / no-pane zone) không đụng focus — focus-set đặt SAU các
  early-return của divider + non-left + overDivider noop (§5.3).

- **FR9** — Wheel mouse KHÔNG đổi focus. Wheel scroll vẫn context-aware
  theo `overLeft` (giữ nguyên nhánh `tea.MouseWheelMsg`) — không bump
  focus để user scroll-without-commitment vẫn hoạt động.

- **FR10** — Focus signal chính là status-bar chip (FR11) + cursor row
  dim (FR12), độc lập với pane geometry. Layout borderless không có border
  quanh pane → không có border màu để flip; chip + dim mang trọn tín hiệu
  (§5.7).

- **FR11** — `renderStatus` (`view.go:245`) ở `modeNormal` thêm focus chip
  `[ list ]` / `[ preview ]` ở **đầu** hint string. Chip tô `colAccent`
  background. Hint khác nhau theo focus — chỉ liệt kê phím relevant:
  ```
  [ list ] [↑↓/jk] move  [tab] focus preview  [enter/l] open  [h/bksp] up  [r] rename  [d] delete  [q] quit
  ```
  vs
  ```
  [ preview ] [↑↓/jk] scroll  [tab] focus list  [g/G] top/bottom  [ctrl+d/u] half-page  [esc] back  [q] quit
  ```

- **FR12** — `cursorActiveStyle.Width(w)` (`view.go:146`) đổi background
  từ `colAccent` sang `colDim` khi `focusPane == focusPreview` (D10).
  `renderEntryRow` nhận thêm tham số `listFocused bool`, resolve style
  ở chỗ duy nhất — không nest style decision trong nhiều hàm.

- **FR13** — Khi `modeRename` hoặc `modeConfirmDelete` đang active, focus
  pane bị "đông cứng" tại giá trị hiện tại (D15). Tab trong rename → noop
  vào `m.input` (`Text` rỗng cho Tab, `model.go:771` đã guard).
  `updateConfirmDelete` (`model.go:718`) coi mọi phím khác `y/Y` là cancel
  — bao gồm Tab.

- **FR14** — Click trên status bar (`e.Y == m.height-1`) không đổi focus
  — status bar không phải pane.

- **FR15** — Khi list rỗng (`len(m.entries) == 0`), `focusPane` vẫn có
  thể là `focusPreview` (vd user vào folder rỗng từ một folder có
  preview). Up/down/j/k với preview rỗng (`len(m.preview) == 0` và
  `len(m.previewEntries) == 0`) là noop — clamp logic hiện tại của
  `scrollPreview` (`model.go:568-572`) đã handle.

## 5. Technical design

> Kim chỉ nam: **focus là sub-state nhỏ, không phá cấu trúc**. Một enum
> mới + một switch trong `updateNormal` route theo focus + một chip ở status
> bar + cursor row dim. Không cần helper file mới, không cần message type mới,
> không async. Update path zero-allocation overhead (chỉ một comparison).

### 5.1 Enum + field (`model.go`)

Thêm cạnh `mode` enum (`model.go:38-44`):

```go
type focusPane int

const (
    focusList focusPane = iota // zero value — default trên launch
    focusPreview
)
```

Field trên `model` (`model.go:46-105`), đặt cạnh `mode`:

```go
type model struct {
    // …existing fields…
    mode      mode
    focusPane focusPane // sub-state of modeNormal; orthogonal to mode prompts
    // …rest…
}
```

`newModel` (`model.go:107`) không cần khởi tạo explicit — zero value
`focusList` đúng default (D2).

### 5.2 `updateNormal` route theo focus (`model.go:619-665`)

Refactor thành dispatch: focus-aware keys gọi helper, focus-agnostic
keys giữ inline. Hiện code:

```go
case "down", "j":
    if m.cursor < len(m.entries)-1 {
        m.cursor++
        m.refreshPreview()
    }
case "up", "k":
    if m.cursor > 0 {
        m.cursor--
        m.refreshPreview()
    }
```

Sau:

```go
case "tab":
    if m.focusPane == focusList {
        m.focusPane = focusPreview
    } else {
        m.focusPane = focusList
    }

case "down", "j":
    if m.focusPane == focusList {
        m.moveCursor(1) // existing cursor++ + refreshPreview
    } else {
        m.scrollPreview(1)
    }

case "up", "k":
    if m.focusPane == focusList {
        m.moveCursor(-1)
    } else {
        m.scrollPreview(-1)
    }

case "g":
    if m.focusPane == focusList {
        m.cursor = 0
        m.refreshPreview()
    } else {
        m.previewTop = 0
    }

case "G":
    if m.focusPane == focusList {
        m.cursor = max(0, len(m.entries)-1)
        m.refreshPreview()
    } else {
        _, bodyH := m.previewScroll()
        m.previewTop = max(0, m.previewLen()-bodyH)
    }

case "ctrl+d":
    _, bodyH := m.previewScroll()
    step := max(1, bodyH/2)
    if m.focusPane == focusList {
        m.moveCursor(step)
    } else {
        m.scrollPreview(step)
    }

case "ctrl+u":
    _, bodyH := m.previewScroll()
    step := max(1, bodyH/2)
    if m.focusPane == focusList {
        m.moveCursor(-step)
    } else {
        m.scrollPreview(-step)
    }

// List-only keys — guarded by focus
case "enter", "l", "right":
    if m.focusPane == focusList {
        m.descend()
    }
case "backspace", "h", "left":
    if m.focusPane == focusList {
        m.ascend()
    }
case "r":
    if m.focusPane == focusList && len(m.entries) > 0 && m.entries[m.cursor].name != ".." {
        m.mode = modeRename
        m.input = m.entries[m.cursor].name
        m.statusMsg = ""
    }
case "d":
    if m.focusPane == focusList && len(m.entries) > 0 && m.entries[m.cursor].name != ".." {
        m.mode = modeConfirmDelete
        m.statusMsg = ""
    }

case "esc":
    if m.focusPane == focusPreview {
        m.focusPane = focusList
    }
    // (modeNormal Esc default: noop)

case "q", "ctrl+c":
    return m, tea.Quit
```

Helper mới `moveCursor(delta int)` trên `*model`:

```go
// moveCursor nudges the list cursor by delta rows and refreshes the preview.
// Clamps to [0, len(entries)-1]; a delta that overshoots either end snaps to
// the edge. Empty list → noop. Centralizing the move keeps the Update switch
// flat — j/k, ctrl+d/u, and any future "page list" key call this and get the
// same clamping + preview refresh.
func (m *model) moveCursor(delta int) {
    if len(m.entries) == 0 {
        return
    }
    n := len(m.entries)
    target := m.cursor + delta
    if target < 0 {
        target = 0
    }
    if target >= n {
        target = n - 1
    }
    if target == m.cursor {
        return
    }
    m.cursor = target
    m.refreshPreview()
}
```

### 5.3 Mouse focus update (`handleMouse`, nhánh `tea.MouseClickMsg`)

Set focus theo `overLeft` SAU ba early-return của divider (drag-start +
non-left + overDivider noop) và TRƯỚC khi route click vào pane — để click
no-pane không bao giờ flip focus:

```go
case tea.MouseClickMsg:
    if e.Button == tea.MouseLeft && e.Y < m.height-1 && overDivider {
        m.dragging = true
        m.setLeftFromX(e.X)
        return m, nil // divider drag — do not change focus
    }
    if e.Button != tea.MouseLeft {
        return m, nil
    }
    if overDivider {
        return m, nil // no-pane zone — do not change focus
    }
    overLeft := e.X < g.dividerStart
    if overLeft {
        m.focusPane = focusList // FR8
    } else {
        m.focusPane = focusPreview
    }
    if !overLeft {
        m.previewClick(e.Y, g)
        return m, nil
    }
    // …list pane click handling unchanged…
```

Wheel branch giữ nguyên — KHÔNG set focus (FR9). Motion / release giữ
nguyên — không liên quan focus.

### 5.4 `View()` không cần đổi

Layout borderless: `View()` wrap mỗi pane trong `lipgloss.NewStyle().Width().Height()`
trơn, ngăn cách bằng một divider 3 cột — không có border quanh pane để đổi
màu theo focus. Tín hiệu focus đi qua status-bar chip (§5.6) + cursor row
dim (§5.5), nên `View()` giữ nguyên; diff focus nằm ở `renderList`,
`renderEntryRow`, `renderStatus`.

### 5.5 `renderList` cursor row dim khi focus preview (`view.go`)

Pass focus vào `renderList`:

```go
func (m model) renderList(w, h int) string {
    // …existing prelude…
    for i := top; i < len(m.entries) && i < top+h; i++ {
        b.WriteString(renderEntryRow(m.entries[i], w, i == m.cursor, m.focusPane == focusList))
        // …
    }
}
```

`renderEntryRow` (`view.go:131-153`) thêm tham số `listFocused bool`:

```go
func renderEntryRow(e entry, w int, active bool, listFocused bool) string {
    // …existing name/size/body computation…
    if active {
        st := cursorActiveStyle
        if !listFocused {
            // D11: cursor row dimmed when focus is on the preview pane.
            // The cursor still tells you which file the preview reflects,
            // but the accent is reserved for the focused pane.
            st = cursorActiveStyle.Background(colDim)
        }
        return st.Width(w).Render("▶ " + body)
    }
    // …non-active branch unchanged…
}
```

`cursorActiveStyle` (`theme.go:24-27`) không đổi — chúng ta tạo style mới
**inline** với `.Background(colDim)` khi dim. Lipgloss `Background` trả
copy mới, không mutate gốc.

### 5.6 Status bar focus chip (`view.go:245-270`)

`renderStatus` nhánh default cập nhật:

```go
default:
    var chip, hints string
    if m.focusPane == focusList {
        chip = focusChipStyle.Render(" list ")
        hints = "[↑↓/jk] move  [tab] focus preview  [enter/l] open  [h/bksp] up  [r] rename  [d] delete  [q] quit"
    } else {
        chip = focusChipStyle.Render(" preview ")
        hints = "[↑↓/jk] scroll  [tab] focus list  [g/G] top/bottom  [ctrl+d/u] half-page  [esc] back  [q] quit"
    }
    status := chip + " " + hints
    if m.statusMsg != "" {
        status = chip + " " + m.statusMsg + dimStyle.Render("   "+hints)
    }
    if m.pendingWidth > 0 {
        status = renderingStyle.Render("• rendering… ") + status
    }
    return statusBarStyle.Width(m.width).Render(fitWidth(status, m.width-2))
```

`focusChipStyle` mới ở `theme.go`:

```go
// focusChipStyle is the [ list ] / [ preview ] chip in the status bar that
// signals which pane keyboard navigation is acting on. One accent, inverted
// (background = accent so the chip pops without adding a new color to the
// palette).
focusChipStyle = lipgloss.NewStyle().
    Background(colAccent).
    Foreground(colSelFg).
    Bold(true)
```

### 5.7 Focus signal trong layout borderless

Layout là một divider 3 cột giữa hai pane, không border quanh pane
(`prd-middle-divider.md`). Vì vậy focus signal đi trọn qua hai kênh
độc lập với geometry:

- **Status-bar chip** (D8/§5.6): `[ list ]` / `[ preview ]` ở đầu hint
  string, tô `focusChipStyle` (`colAccent` background). Chip là tín hiệu
  chính — luôn visible, đổi nội dung khi focus đổi, không phụ thuộc kích
  thước pane.
- **Cursor row dim** (D10/§5.5): khi `focusPane == focusPreview`, cursor
  row trong list pane đổi background từ `colAccent` sang `colDim`. Củng cố
  chip: list vẫn chỉ rõ cursor đang ở file nào nhưng nhường accent cho pane
  đang focus.

Hai kênh này mang đủ tín hiệu focus mà không cần border màu — verify bằng
visual khi `focusList` (chip `[ list ]`, cursor row accent) và `focusPreview`
(chip `[ preview ]`, cursor row dim).

### 5.8 `tickMsg` poll loop không đụng

`model.go:431-436` check `m.mode == modeNormal && !m.dragging`. Focus là
sub-state của `modeNormal` — không ảnh hưởng. `syncFromDisk` vẫn chạy
cập nhật list. Behavior khi agent ghi file:

- Focus list: cursor stays by name (`model.go:181-188`), preview refresh
  — không đổi.
- Focus preview: list update background, preview refresh nếu selected
  file đổi. User vẫn đang scroll preview của file đang active — không
  bị churn. Nếu file đang select bị xóa, `refreshPreview` set preview
  về placeholder → user thấy preview content đổi (đúng), focus vẫn
  giữ `focusPreview`. Khi đó j/k scroll preview placeholder = noop
  (clamp). Acceptable.

### 5.9 Edge cases tổng hợp

| Tình huống | Hành vi | Cơ chế |
|---|---|---|
| `focusPreview` + preview rỗng (file empty / placeholder 1 dòng) | j/k/g/G noop | `scrollPreview` clamp (`model.go:570-571`) + `previewTop = 0` không đổi |
| `focusPreview` + folder preview rỗng | j/k/g/G noop | `previewLen()` (`view.go:191-196`) trả 0 → clamp |
| `focusList` + list rỗng (cwd rỗng, không có ..) | j/k/g/G noop | `moveCursor` guard `len(entries) == 0` |
| Tab khi `modeRename` (`m.mode == modeRename`) | `updateRename` xử lý — Tab có `msg.Text == ""` (không phải printable) → noop vào `m.input` | `model.go:768-773` default case check `msg.Text != ""` |
| Tab khi `modeConfirmDelete` | Cancel (mọi key ngoài y/Y cancel) → `mode = modeNormal`, focus giữ nguyên | `model.go:730-733` |
| Click vào divider (`overDivider`) | Drag start (hoặc noop ở status row), focus KHÔNG đổi | Early return của divider branch + overDivider noop, trước nhánh set focus |
| Click vào status row (`e.Y == m.height-1`) | Noop, focus không đổi | `e.Y < m.height-1` check ở divider branch + row bound (`row >= bodyH`) ở list branch |
| Preview re-render in flight khi Tab → focus list | Render tiếp tục, kết quả về `applyPreview` apply như bình thường — focus không ảnh hưởng pipeline | `syncPreview`/`applyPreview` không đọc `focusPane` |
| Switching focus rapid (Tab spam) | Một re-render đổi chip + cursor row dim — cheap; không dispatch render mới | `syncPreview` chỉ trigger trên width/srcPath change |
| Esc khi `modeNormal` + `focusList` | Noop (mọi case hiện tại chưa dùng Esc trong modeNormal) | Switch default fallthrough |

### 5.10 Đã cân nhắc & **defer khỏi v1**

- **Vim-style `Ctrl+W h/l` để switch focus**: thêm chord 2-key cho action
  đã có Tab — không xứng cognitive load. Defer.

- **Trim các alias `right`/`left` cho enter/backspace**: code hiện vẫn
  `case "enter", "l", "right"` + `case "backspace", "h", "left"` —
  duplicate 3 đường cho cùng hành vi. PRD này KHÔNG đụng (out of scope:
  focus state). Cleanup tách PR riêng.

- **Số pane > 2** (tree + list + preview, lazygit-style multi-pane):
  vi phạm two-panel ceiling của project (`CLAUDE.md`). Không xem xét.

- **Open file in $EDITOR khi `focusPreview` + Enter**: tăng surface area
  ngoài "glance companion" — agent đã edit file rồi, app không cần là
  editor. Defer ít nhất tới khi có request thật.

- **Focus indicator qua title bar trên pane** (vd `── List ──` ở top):
  thêm chrome (header bar) — vi phạm "minimal chrome". Chip + cursor row
  dim đã đủ.

- **Persist focus qua session** (file `.config/lazyexplorer/state.json`):
  No Abstractions Until Proven; focus reset về list mỗi launch hợp lý
  (entry point của workflow).

- **Auto-focus preview khi file vừa được agent thay đổi**: cool nhưng
  steal focus = user-hostile (đang gõ Tab xong agent push file → focus
  chuyển → tiếp tục gõ j/k ra preview thay vì list, surprise). Defer
  vĩnh viễn.

- **Mouse hover** focus (không click, chỉ hover) — bubbletea v2 có
  motion msg, nhưng tiếng động cao, dễ flap. Defer.

- **`focusChipStyle` tô màu theo pane** (vd list = `colAccent`, preview
  = `colDir`): nhiều màu → phá restraint của palette. Một accent đủ.

## 6. Acceptance criteria

### Gherkin

```gherkin
Feature: Pane focus state for keyboard-driven scroll

  When the explorer is in normal mode, exactly one of the two panes holds
  "focus". The focused pane is what arrow keys and j/k/g/G/ctrl+d/u act on.
  Tab toggles focus between the two panes. The status bar always shows
  which pane currently has focus.

  Background:
    Given the explorer is open at a project root
    And the cwd has at least one file with previewable content longer than the preview body

  Scenario: Default focus on launch is the list pane
    When the explorer first renders
    Then the focus is on the list pane
    And the status bar shows a "list" focus chip
    And the cursor row in the list draws with the accent background

  Scenario: Tab toggles focus to the preview
    Given the focus is on the list pane
    When I press Tab
    Then the focus moves to the preview pane
    And the status bar shows a "preview" focus chip
    And the cursor row in the list draws with a dimmed background

  Scenario: Arrow keys scroll the preview when focus is on preview
    Given the focus is on the preview pane
    And previewTop is 0
    When I press the down arrow
    Then previewTop becomes 1
    When I press the down arrow four more times
    Then previewTop becomes 5
    When I press the up arrow
    Then previewTop becomes 4

  Scenario: Arrow keys move the list cursor when focus is on list
    Given the focus is on the list pane
    And the list cursor is on index 0
    When I press the down arrow
    Then the cursor moves to index 1
    And the preview refreshes for the new selection

  Scenario: g and G jump to top/bottom of the focused pane
    Given the focus is on the preview pane
    And previewTop is 5
    When I press g
    Then previewTop becomes 0
    When I press G
    Then previewTop is at the maximum scroll offset

  Scenario: ctrl+d/u half-pages the focused pane
    Given the focus is on the preview pane
    And the preview body is 20 rows tall
    And previewTop is 0
    When I press ctrl+d
    Then previewTop becomes 10
    When I press ctrl+u
    Then previewTop becomes 0

  Scenario: Mutation keys are no-ops when focus is on preview
    Given the focus is on the preview pane
    When I press r
    Then no rename prompt appears
    When I press d
    Then no delete prompt appears
    When I press Enter
    Then nothing happens

  Scenario: Navigation keys are no-ops when focus is on preview
    Given the focus is on the preview pane
    And the cwd is a subfolder
    When I press h
    Then the cwd does not change

  Scenario: Esc returns focus to the list pane
    Given the focus is on the preview pane
    When I press Esc
    Then the focus returns to the list pane
    And the status bar shows a "list" focus chip

  Scenario: Clicking in the list pane sets focus to list
    Given the focus is on the preview pane
    When I left-click a row inside the list pane
    Then the focus moves to the list pane
    And the clicked entry becomes selected

  Scenario: Clicking in the preview pane sets focus to preview
    Given the focus is on the list pane
    When I left-click anywhere inside the preview pane
    Then the focus moves to the preview pane

  Scenario: Dragging the divider does not change focus
    Given the focus is on the list pane
    When I drag the divider between the panes
    Then the focus stays on the list pane

  Scenario: Wheel scroll does not change focus
    Given the focus is on the list pane
    When I scroll the wheel over the preview pane
    Then the preview scrolls
    And the focus stays on the list pane

  Scenario: An agent writes a file in cwd while focus is on preview
    Given the focus is on the preview pane
    And I am scrolling through a file's preview
    When an external process adds a new file in cwd
    Then the list pane updates with the new entry
    And the focus stays on the preview pane
    And the preview viewport position is preserved

  Scenario: Folder preview keys are no-op when focus is on preview but folder preview is empty
    Given the selected entry is an empty folder
    And the focus is on the preview pane
    When I press the down arrow
    Then nothing changes

  Scenario: Tab is ignored while a rename prompt is open
    Given a rename prompt is open
    When I press Tab
    Then the rename prompt stays open
    And the focus does not change
```

### Checklist verify

1. Khởi chạy `./lazyexplorer .` trong project lazyexplorer → status bar bắt
   đầu bằng chip `[ list ]`, cursor row trong list pane tô accent.
2. Bấm Tab → chip đổi sang `[ preview ]`, cursor row trong list pane bị dim
   (background chuyển từ `#7D56F4` sang `#6C757D`), hint đổi sang bộ preview.
3. Tab lại lần nữa → trở về state ban đầu (list focused).
4. Focus preview, mở `docs/prd-search.md` (file dài) → bấm ↓ năm lần,
   `previewTop` đi từ 0 → 5 (xác minh bằng cách quan sát dòng đầu
   preview thay đổi tương ứng).
5. Focus list, bấm ↓ → cursor list di chuyển, preview refresh theo file
   mới (behavior cũ, không regression).
6. Focus preview, bấm `G` → preview nhảy về cuối file; bấm `g` → về
   đầu file. Focus list, bấm `G` → cursor về file cuối list; `g` → về
   file đầu.
7. Focus list, bấm `ctrl+d` → cursor nhảy `~bodyH/2` entries xuống (new
   behavior — không phải scroll preview như trước). Focus preview, bấm
   `ctrl+d` → preview nhảy nửa panel (cùng giá trị `max(1, bodyH/2)`
   của `prd-smooth-preview-scroll.md`).
8. Focus preview, bấm `r` / `d` / `Enter` / `l` / `h` → không gì xảy ra
   (no prompt, no nav, status không đổi).
9. Focus preview, bấm `Esc` → focus về list, chip đổi.
10. `rg '"J"|"K"' model.go` → 0 hit (J/K legacy đã xoá hoàn toàn — sau
    PRD này không còn case phím viết hoa cho preview scroll).
11. Focus list, click vào một dòng trong preview → focus chuyển preview,
    chip đổi (entry list không thay đổi — chỉ focus đổi).
12. Focus preview, click một entry trong list → focus về list + entry đó
    được chọn.
13. Drag divider giữa hai pane khi đang focus list → focus vẫn list sau
    khi release (drag không steal focus).
14. Focus list, scroll wheel trong preview pane → preview scroll, focus
    vẫn list (wheel không steal focus).
15. Mở vim/agent bên cạnh, agent tạo file mới trong cwd → file mới xuất
    hiện trong list pane bất kể focus đang ở đâu; nếu focus preview,
    `previewTop` không reset, viewport giữ nguyên vị trí cũ trên file
    đang xem (trừ khi chính file đó bị xóa).
16. Vào `modeRename` (focus list, bấm `r`) → Tab trong prompt: không
    đổi focus, không append vào `m.input` (Text rỗng).
17. Vào `modeConfirmDelete` (bấm `d`) → Tab cancel delete (mọi key
    ngoài y/Y là cancel), focus giữ list.
18. Click vào divider band (drag-start hoặc status row) khi focus list →
    focus vẫn list (focus-set đặt sau early-return của divider).
19. `rg 'focusPane'` trả hit ở `model.go`, `view.go` (đủ bao phủ); KHÔNG
    hit ở `fs.go`, `theme.go`, `main.go` (focus là model+view concern).
    ĐÃ VERIFY ✅ 2026-05-28 — model.go 21 hit, view.go 3 hit, còn lại 0.
20. `go build -o lazyexplorer . && go vet ./... && go test ./...` xanh.
    ĐÃ VERIFY ✅ 2026-05-28.
21. `go test -race ./...` xanh. ĐÃ VERIFY ✅ 2026-05-28.
22. Visual smoke (status bar, ANSI-stripped) cho 2 frame — ĐÃ VERIFY ✅ 2026-05-28:
    - Frame A: focus=list — chip `[ list ]`, hint `[tab] focus preview …`,
      cursor row tô accent.
    - Frame B: focus=preview — chip `[ preview ]`, hint `[ctrl+d/u] half-page …`,
      cursor row tô dim.

## 7. Task breakdown

> Trước khi sửa: chạy `gitnexus_impact` cho `updateNormal`, `scrollPreview`,
> `renderList`, `renderEntryRow`, `renderStatus`, `handleMouse`. Risk hiện
> tại đã verify: `updateNormal` CRITICAL (1 caller
> = `Update`, 7 processes), `scrollPreview` CRITICAL (3 callers, 7 processes).
> Test coverage trên các vùng này là gate. Sau khi sửa: `gitnexus_detect_changes`
> verify scope khớp.

- [x] **T1 — Enum + field.** Thêm `focusPane` enum + field `focusPane`
  trên `model` (§5.1). *(model.go)*

- [x] **T2 — Helper `moveCursor`.** Hàm `(*model).moveCursor(delta int)`
  centralize cursor jump + clamp + refreshPreview (§5.2). *(model.go)*

- [x] **T3 — `updateNormal` dispatch theo focus.** Tab toggle focus
  (không Shift+Tab); up/down/j/k/g/G/ctrl+d/u branch theo focus; **xoá**
  case `"J", "ctrl+d"` và `"K", "ctrl+u"` cũ — tách thành `case "ctrl+d"`
  và `case "ctrl+u"` riêng theo focus; mutation + navigation keys guard
  theo focus; Esc khi focusPreview → focusList (§5.2, FR2-FR7, D11, D12,
  D13). *(model.go)*

- [x] **T4 — Mouse click set focus.** `handleMouse` set `m.focusPane`
  trong nhánh `MouseClickMsg` sau ba early-return của divider (§5.3, FR8).
  Wheel KHÔNG đổi focus (FR9). *(model.go)*

- [x] **T5 — Focus signal không qua border.** Layout borderless → `View()`
  không đổi; focus signal đi qua chip (T7) + cursor row dim (T6) (§5.4,
  §5.7, FR10). *(không sửa view.go ở task này)*

- [x] **T6 — `renderEntryRow` + `renderList` listFocused param.** Pass
  `m.focusPane == focusList` xuống; dim cursor row khi false (§5.5, FR12).
  *(view.go)*

- [x] **T7 — `renderStatus` focus chip.** Render `[ list ]` / `[ preview ]`
  chip đầu hint string; nội dung hint khác nhau theo focus (§5.6, FR11).
  *(view.go)*

- [x] **T8 — `focusChipStyle` trong theme.** Thêm style mới (§5.6).
  *(theme.go)*

- [x] **T9 — Tests** (15 test, tất cả pass + `-race` clean): *(`focus_test.go` mới; cập nhật call sites của
  `renderEntryRow` trong `entryrow_test.go` + `preview_dir_test.go` cho
  chữ ký 4-tham số)*
  - `TestFocusDefaultIsList`: `newModel(root, nil).focusPane == focusList`.
  - `TestTabTogglesFocus`: Update với `KeyPressMsg` cho `"tab"` toggle 2 chiều (FR2).
  - `TestShiftTabIgnored`: Update với `KeyPressMsg` cho `"shift+tab"` → focus không đổi (no handler).
  - `TestArrowsRouteByFocus`: Update down/up khi focusList move cursor; khi focusPreview move previewTop (FR3).
  - `TestGGRouteByFocus`: g/G top/bottom (FR3, D12).
  - `TestCtrlDURouteByFocus`: `ctrl+d`/`ctrl+u` half-page focused pane (FR3, D11).
  - `TestUppercaseJKRemoved`: Update với `KeyPressMsg` cho `"J"`/`"K"` → `previewTop` không đổi (FR4, D13).
  - `TestMutationKeysGuardedByFocus`: r/d/enter/l/h khi focusPreview = noop (FR5).
  - `TestEscReturnsFocusToList`: Esc khi focusPreview → focusList (FR6).
  - `TestClickSetsFocus`: MouseClick trong list pane → focusList; trong preview pane → focusPreview; trên divider → focus không đổi (FR8, FR9).
  - `TestWheelDoesNotChangeFocus`: WheelMsg trên preview pane khi focusList → focus vẫn list (FR9).
  - `TestRenameModeFreezesFocus`: focus list → r → modeRename → Tab → focus giữ list (FR13, D15).
  - `TestCursorRowDimWhenFocusPreview`: `renderEntryRow(e, w, true, false)` → output chứa background `colDim`; với `listFocused=true` → `colAccent` (§5.5).
  - `TestStatusChipReflectsFocus`: `renderStatus` chứa substring `list` khi focusList, `preview` khi focusPreview, hint khác nhau (§5.6).
  - `TestEmptyPanesNoop`: empty preview + focusPreview + j/k = `previewTop` không đổi; empty list + focusList + j/k = noop (FR15).
  - `-race`: parallel Update calls với mixed messages không race trên `focusPane`.

- [x] **T10 — Visual smoke.** Render status bar (ANSI-stripped) cho 2 frame
  (focus=list / focus=preview) → chip + hint per-focus đúng, cursor row
  accent vs dim (§6 checklist 22). *(verify bằng harness throwaway)*

- [x] **T11 — Hint bar wording sync.** Hint bar mới (§5.6) liệt kê đúng
  phím relevant per focus; `[esc] back` chỉ ở bộ preview (Esc handle ở
  focusPreview), không lệch với binding. *(view.go)*

- [x] **T12 — Verify.** `go build -o lazyexplorer . && go vet ./... &&
  go test ./...` xanh; `go test -race ./...` xanh — ĐÃ VERIFY ✅ 2026-05-28;
  visual smoke 2 frame đạt; `gitnexus_detect_changes` scope khớp với §8.

## 8. Files chạm tới

| File | Thay đổi |
|------|----------|
| `model.go` | + enum `focusPane` + field `focusPane`; + helper `moveCursor`; `updateNormal` refactor route theo focus (Tab toggle; up/down/j/k/g/G/ctrl+d/u branch theo focus; **xoá** `case "J", "ctrl+d"` và `case "K", "ctrl+u"` cũ; mutation/nav guard; Esc focus preview → list); `handleMouse` set `focusPane` trong `MouseClickMsg` (divider drag + no-pane + wheel không đụng) |
| `view.go` | `renderList` pass `listFocused` xuống `renderEntryRow`; `renderEntryRow` thêm tham số `listFocused` + dim cursor row khi false; `renderStatus` thêm focus chip + hint per-focus. `View()` không đổi (borderless) |
| `theme.go` | + `focusChipStyle` (`colAccent` background, `colSelFg` foreground, bold) |
| `focus_test.go` *(mới)* | Tests T9 (focus-specific) |
| `entryrow_test.go`, `preview_dir_test.go` | Cập nhật call sites `renderEntryRow` cho chữ ký 4-tham số |
| `docs/prd-pane-focus.md` | File này |
| `docs/prd-smooth-preview-scroll.md` | + một dòng §1 ghi D2 (fine 1-dòng) + D3 (ctrl+d/u half-page) đã ship qua PRD này (trên `j/k`+focus thay vì `J/K`) |
