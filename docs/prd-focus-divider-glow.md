# PRD — Focus indicator: divider phát sáng thay status-bar chip

> Bug-shaped polish: tín hiệu focus hiện tại là một **chip** `[ list ]` / `[ preview ]`
> nền accent đặc ở đầu status bar (`view.go:695-698`, style `focusChipStyle`
> `theme.go:37-44`). Chip này nhìn nặng và lạc chỗ: nó nằm ở **đáy màn hình**, xa hai
> pane mà mắt đang nhìn; là một pill màu đặc trong một UI vốn restrained; tốn ngang
> trên status bar đã chật; và lặp lại thông tin mà vị trí cursor đã ngụ ý. Cần một tín
> hiệu focus **đọc-ở-glance, ngay tại ranh giới hai pane**, không thêm chrome.

Status: **accepted** · Author: brainstorm session (Opus 4.7) · Ngày: 2026-05-28 · Shipped: 2026-05-28

---

## 1. Bối cảnh & vấn đề

`prd-pane-focus.md` (accepted, shipped 2026-05-28) đưa vào trạng thái `focusPane`
(`focusList` / `focusPreview`) và quyết định mang tín hiệu focus qua **hai kênh độc lập
với geometry** (vì layout borderless không có border quanh pane để đổi màu —
`prd-middle-divider.md`):

- **Kênh chính** — status-bar chip `[ list ]` / `[ preview ]` (D8/FR11/§5.6-§5.7 của
  `prd-pane-focus.md`). Code hiện hành: `view.go:695-698` dựng chip, ghép vào `status`
  ở `view.go:700` và `view.go:702`; style `focusChipStyle` ở `theme.go:37-44`.
- **Kênh phụ** — cursor row trong list pane đổi nền `colAccent → colDim` khi focus ở
  preview (D9/D10/FR12; `view.go:348-350` trong `renderEntryRow`).

Vấn đề nằm ở **kênh chính**. Đánh giá UI (screenshot, brainstorm 2026-05-28) chốt: chip
xấu và đặt sai chỗ.

1. **Sai vị trí thị giác.** Mắt user ở trên hai pane; chip ở dòng status đáy màn hình.
   Tín hiệu "pane nào đang nhận phím" lại nằm xa nơi hành động xảy ra.
2. **Nặng so với palette restrained.** Một pill nền accent đặc (`#7D56F4`) phá nhịp một
   UI mà phần còn lại chỉ dùng accent rất tiết chế (cursor row, rendering chip).
3. **Tốn ngang.** Status bar đã chật (hint per-focus + statusMsg + rendering prefix);
   chip ăn thêm `len(" preview ")` cột mỗi frame.
4. **Lặp thông tin.** Vị trí cursor + hint per-focus đã ngụ ý pane nào active; chip nói
   lại bằng chữ.

Comment ngay tại code thừa nhận chip ra đời vì ràng buộc borderless (`view.go:693-694`:
*"the panes are borderless, so there is no border color to flip"*). PRD này lật **chính
giả định đó** một cách có chủ đích: ta **đã có** một element ở ranh giới hai pane — cái
**divider** — và có thể tô nó để báo focus mà không cần thêm border hay chrome nào.

> Quan hệ với `prd-pane-focus.md`: PRD này **thay phần "chip"** của nó (D8, FR11, kênh
> "chip" trong FR10/§5.6/§5.7) bằng "divider glow", và **giữ nguyên** cursor-row dim
> (D9/D10/FR12 — giờ củng cố divider thay vì củng cố chip). `prd-pane-focus.md` chưa sửa
> ở PRD này (chip vẫn đang ship); reconcile khi PRD này ship (§8). Lịch sử "vì sao đổi
> chip → glow" sống ở đây (mục 1) và git, không nhét ngược vào spec cũ.

Reference clones (không copy):

- `tmp/lipgloss` — half-block glyph (`▐`/`▌`/`▀`/`▄`/`▔`/`▁`) + `Foreground` runtime;
  chính thư viện styling của project. Đọc cách render glyph một-nửa-ô để accent "ôm" một
  phía của divider.
- `tmp/crush`, `tmp/gh-dash` — active-pane signal qua border/title color. Ta không có
  border; divider glow là analog "active border" gần nhất mà vẫn giữ borderless
  (xem `[[reference_crush_tui]]`).

## 2. Goal (1 câu)

Thay status-bar chip bằng **divider phát sáng accent về phía pane đang focus** — tô lại
một element đã có (divider), giữ cursor-row dim làm cue phụ, không thêm chrome nào — để
tín hiệu focus đọc-được-ở-glance ngay tại ranh giới hai pane.

**Non-goal làm rõ:**

- KHÔNG thêm border quanh pane (đó là phương án B đã bác — lật borderless + thêm 1 cột
  chrome). Glow tô divider sẵn có, không thêm gì.
- KHÔNG đổi logic `focusPane`, dispatch phím, hay mouse-set-focus — `prd-pane-focus.md`
  giữ nguyên toàn bộ; PRD này chỉ đổi **cách render tín hiệu**.
- KHÔNG bỏ cursor-row dim — giữ làm cue phụ, giờ củng cố divider glow.
- KHÔNG bỏ hint-bar per-focus (`shortHelp`) — vẫn đổi theo focus, là cue text phụ.
- KHÔNG animate / pulse divider (defer — cần `harmonica`, ngoài scope).
- KHÔNG thêm màu mới — một accent (`colAccent`) duy nhất, như cả palette.

## 3. Quyết định đã chốt

| # | Quyết định | Giá trị | Lý do |
|---|-----------|---------|-------|
| D1 | Bỏ status-bar chip | Xoá render chip (`view.go:695-698`, ghép ở `700`/`702`) + style `focusChipStyle` (`theme.go:37-44`) | Chip nặng, sai vị trí thị giác, tốn ngang, lặp thông tin (mục 1). Clean-slate: không để lại pill nào ở status bar |
| D2 | Tín hiệu focus **chính** | Divider phát sáng `colAccent` về phía pane đang focus | Tái dùng element đã có (divider) ở **trung tâm thị giác** giữa hai pane; zero chrome mới; analog "active border" lazygit-style mà vẫn giữ borderless |
| D3 | Glow ở layout **horizontal** (2-col) | Nửa-block accent ở pad col phía pane focus; `│` giữ `dimStyle`. List focus → `▐` (accent) ở pad-**left**; Preview focus → `▌` (accent) ở pad-**right** | Divider có 3 cột (`view.go:122-124`), đủ chỗ đặt accent về **một phía** mà không nới rộng divider. Nửa-block "ôm" sát `│` từ phía pane focus. **(Đã duyệt qua mockup brainstorm: `▐│` / `│▌`.)** |
| D4 | Glow ở layout **vertical** (1-col stacked) | Eighth-block accent full-width thay `─`: List(top) focus → `▔` (accent); Preview(bottom) focus → `▁` (accent) | Divider dọc chỉ **1 hàng** (`dividerHeight=1`, `view.go:144`), không có pad-row để đặt accent → mã hoá top/bottom bằng **sub-cell glyph**. Eighth-block giữ "ink" tương đương nửa-block 1-cột của horizontal (cân trọng lượng thị giác), không thành dải đặc. *(Glyph vertical là đề xuất trong scope A — chốt cuối qua visual-verdict, §5.7.)* |
| D5 | Giữ cursor-row dim | `prd-pane-focus.md` D9/D10/FR12 nguyên trạng (`view.go:348-350`) | Giờ **củng cố divider glow**: list focus = cursor accent + divider glow trái; preview focus = cursor dim + divider glow phải. Tránh hai vùng accent cạnh nhau tranh nhau |
| D6 | Giữ hint-bar per-focus | `shortHelp()` đổi theo focus, render ở `view.go:699` | Cue text phụ thứ ba, miễn phí (đã có). Không phải tín hiệu chính nhưng không bỏ |
| D7 | Style mới | `dividerFocusStyle = lipgloss.NewStyle().Foreground(colAccent)` — **không** set background | Nửa-block tô foreground = accent, nửa còn lại hoà nền pane (panes borderless, nền default) → glow "trong suốt" đúng kỳ vọng. Một accent, không thêm màu |
| D8 | Divider **luôn** glow về một phía | Không có trạng thái "divider trung tính" ở `modeNormal` | `focusPane` luôn có giá trị (default `focusList`), nên divider luôn nghiêng về một pane. `─`/`│` dim thuần chỉ còn là base mà glow override |

## 4. Functional requirements

- **FR1** — Ở `modeNormal`, divider render tô `colAccent` về **đúng một phía** = phía
  `m.focusPane`. Luôn có đúng một bên sáng (không bao giờ cả hai, không bao giờ không
  bên nào).

- **FR2** — **Horizontal** (`!g.vertical`, `view.go:275-280`): mỗi dòng divider =
  `pad-left` + `│`(dim) + `pad-right`. Pad col phía focus mang nửa-block accent
  (`▐` khi `focusList` ở pad-left; `▌` khi `focusPreview` ở pad-right); pad col bên kia
  giữ space. Dòng đồng nhất qua mọi row → dựng **một** `dividerLine` rồi repeat `bodyH`
  lần (giữ render rẻ như hiện tại).

- **FR3** — **Vertical** (`g.vertical`, `view.go:253-254`): hàng divider full-width =
  eighth-block accent thay `─` dim — `▔` (accent) khi `focusList`, `▁` (accent) khi
  `focusPreview`.

- **FR4** — Status-bar chip `[ list ]` / `[ preview ]` bị **xoá** khỏi `renderStatus`
  (`view.go:695-698`); `status` thành `hints` thuần (nhánh `statusMsg` + prefix
  `rendering…` giữ nguyên hành vi, chỉ bỏ `chip + " "`). `focusChipStyle` xoá khỏi
  `theme.go`.

- **FR5** — Cursor-row dim giữ nguyên: `focusPreview` → cursor bg `colDim`;
  `focusList` → cursor bg `colAccent` (`view.go:348-350`).

- **FR6** — Hint-bar per-focus (`shortHelp()`) giữ nguyên — vẫn liệt kê đúng phím
  relevant theo focus.

- **FR7** — Divider drag **không** đổi: glow là visual-only; hit-test divider theo
  **dải cột** `[dividerStart, dividerStart+dividerWidth)` (content-agnostic), nên tô
  glyph trong pad col không phá drag. (`prd-pane-focus.md` §5.3 / FR8: drag-start +
  no-pane không đụng focus — vẫn đúng.)

- **FR8** — Ở `modeCommandPalette` / `modeHelp`, divider vẫn render theo `focusPane`
  đang bị "đông cứng" (focus chỉ đổi ở `modeNormal`). Chấp nhận — không phải trạng thái
  cần tín hiệu focus đặc biệt.

## 5. Technical design

> Kim chỉ nam: **đổi pixel, không đổi state machine.** `focusPane` + dispatch + mouse
> giữ y nguyên. Thay đổi gói gọn ở 3 file render: `theme.go` (một style mới, bỏ một
> style cũ), `view.go` (divider build × 2 orientation + bỏ chip ở status), test. Không
> message type mới, không async, không helper mới.

### 5.1 Style (`theme.go`)

Thêm cạnh các style divider/accent hiện có, **xoá** `focusChipStyle`:

```go
// dividerFocusStyle tô nửa-block/eighth-block glyph của divider về phía pane đang
// focus. Chỉ Foreground = accent; KHÔNG set Background, để nửa ô không-tô hoà nền
// pane (panes borderless). Một accent — cùng colAccent với cursor row + rendering chip.
dividerFocusStyle = lipgloss.NewStyle().Foreground(colAccent)
```

`colAccent` (`theme.go:13`), `dimStyle` (`theme.go:31`) giữ nguyên.

### 5.2 Divider glow — horizontal (`view.go:275-280`)

Hiện tại:

```go
dividerLine := strings.Repeat(" ", dividerPadLeft) +
    dimStyle.Render(dividerGlyph) +
    strings.Repeat(" ", dividerPadRight)
divider := strings.TrimRight(strings.Repeat(dividerLine+"\n", g.bodyH), "\n")
```

Sau (pad col phía focus mang nửa-block accent, `│` giữ dim):

```go
left, right := " ", " " // pad cols, default trống
if m.focusPane == focusList {
    left = dividerFocusStyle.Render("▐")  // accent ôm sát │ từ phía list
} else {
    right = dividerFocusStyle.Render("▌") // accent ôm sát │ từ phía preview
}
dividerLine := left + dimStyle.Render(dividerGlyph) + right
divider := strings.TrimRight(strings.Repeat(dividerLine+"\n", g.bodyH), "\n")
```

`dividerPadLeft = dividerPadRight = 1` (`view.go:122-124`) — nửa-block lấp đúng 1 pad
col, `dividerWidth = 3` không đổi → geometry + hit-zone bất biến.

### 5.3 Divider glow — vertical (`view.go:253-254`)

Hiện tại:

```go
dividerRow := dimStyle.Render(strings.Repeat(dividerHGlyph, g.leftInner))
divider := strings.TrimRight(strings.Repeat(dividerRow+"\n", dividerHeight), "\n")
```

Sau (eighth-block accent hugging pane focus thay `─` dim):

```go
glyph := "▔" // focusList: line rides the top, hugging the list pane above
if m.focusPane == focusPreview {
    glyph = "▁" // line rides the bottom, hugging the preview pane below
}
dividerRow := dividerFocusStyle.Render(strings.Repeat(glyph, g.leftInner))
divider := strings.TrimRight(strings.Repeat(dividerRow+"\n", dividerHeight), "\n")
```

### 5.4 Bỏ chip khỏi `renderStatus` (`view.go:689-711`)

Nhánh `default`:

```go
default:
    hints := renderShortHelp(m.shortHelp())
    status := hints
    if m.statusMsg != "" {
        status = m.statusMsg + dimStyle.Render("   "+hints)
    }
    if m.pendingWidth > 0 {
        status = renderingStyle.Render("• rendering… ") + status
    }
    return statusBarStyle.Width(m.width).Render(fitWidth(status, m.width-2))
```

Xoá `chip := …` (695-698) + mọi `chip + " "` (700, 702). Comment focus-signal
(693-694) viết lại: tín hiệu focus giờ là **divider glow + cursor row dim**.

### 5.5 Cursor-row dim giữ nguyên (`view.go:348-350`)

Không đổi. Comment §5.5/D10 của `renderEntryRow` cập nhật một câu: cursor-dim giờ
**củng cố divider glow** (không còn "dành accent cho chip").

### 5.6 Hit-zone bất biến

`overDivider` test theo dải cột (`prd-pane-focus.md` §5.3). Glow chỉ đổi **nội dung
glyph** trong pad col, không đổi `dividerStart`/`dividerWidth`/`g.leftInner` → drag
vẫn grab đúng vùng. FR7 verify lại bằng tay + test drag hiện có.

### 5.7 Đã cân nhắc & **defer khỏi v1**

- **Glyph vertical: half-block (`▀`/`▄`) vs eighth-block (`▔`/`▁`).** Half-block là
  "rotation" trung thực của nửa-block horizontal nhưng full-width → đặc/loud. Eighth-block
  cân **trọng lượng thị giác** với nửa-block-1-cột của horizontal (đề xuất default). Chốt
  cuối bằng visual-verdict 2 orientation (§6).
- **Phương án B — border quanh pane focus** (1 cột `┃` ở mép pane): rõ nhất nhưng **lật
  borderless** + thêm chrome → vi phạm "minimal chrome". Bác.
- **Phương án C — bỏ luôn cursor-dim, chỉ dựa divider + preview scroll gutter**: rủi ro
  preview ngắn → không có gutter để hiện. Giữ cursor-dim (D5) an toàn hơn. Defer.
- **Chevron/arrow trong divider** (`◂`/`▸`) thay glow: một glyph đổi hướng giữa divider
  subtle hơn glow một-phía; glow đọc tốt hơn ở glance. Bác.
- **Animate/pulse glow** (`harmonica`): thêm dependency + tick, ngoài scope polish. Defer.
- **Tô nguyên `│`/`─` thành accent** (không dùng half-block): không chỉ được **phía** nào
  focus (glyph đối xứng) → không đạt mục tiêu. Bác.

## 6. Acceptance criteria

### Gherkin

```gherkin
Feature: Focus signalled by a glowing divider, not a status-bar chip

  In normal mode the divider between the two panes glows in the accent color
  toward whichever pane currently holds focus. There is no status-bar focus chip.
  The dimmed cursor row reinforces the glow as a secondary cue.

  Background:
    Given the explorer is open at a project root in a wide terminal
    And the cwd has at least one previewable file

  Scenario: Default focus glows the divider toward the list
    When the explorer first renders
    Then the divider glows in the accent color on its list side
    And the status bar shows no focus chip
    And the list cursor row draws with the accent background

  Scenario: Tab moves the glow to the preview side
    Given the focus is on the list pane
    When I press Tab
    Then the divider glow moves to its preview side
    And the list cursor row draws with a dimmed background
    And the status bar still shows no focus chip

  Scenario: Esc returns the glow to the list side
    Given the focus is on the preview pane
    When I press Esc
    Then the divider glow returns to its list side

  Scenario: Clicking a pane moves the glow to that pane
    Given the focus is on the list pane
    When I left-click anywhere inside the preview pane
    Then the divider glow moves to its preview side

  Scenario: Narrow terminal glows the horizontal divider toward the focused pane
    Given the terminal is narrow enough for the stacked layout
    And the focus is on the list pane
    Then the horizontal divider glows along its top edge, hugging the list pane
    When I press Tab
    Then the horizontal divider glows along its bottom edge, hugging the preview pane

  Scenario: Dragging the divider still works with the glow present
    Given the focus is on the list pane
    When I drag the divider between the panes
    Then the panes resize
    And the focus stays on the list pane
```

### Checklist verify

1. Khởi chạy `./lazyexplorer .` (wide) → divider glow accent về phía **list** (horizontal:
   nửa-block accent sát mép trái `│`); status bar **không** còn chip `[ list ]`.
2. Tab → glow chuyển sang phía **preview** (mép phải `│`); cursor row list dim
   (`#7D56F4 → #6C757D`); vẫn không chip.
3. Tab lại → về list glow + cursor accent.
4. Co terminal `< 80` cột (stacked) → divider ngang glow ở **mép trên** (list focus) /
   **mép dưới** (preview focus) theo Tab.
5. Drag divider khi focus list → panes resize, focus vẫn list (glow không phá hit-zone).
6. Click vào preview pane → glow chuyển preview; click list → glow về list.
7. `rg 'focusChipStyle' .` → **0 hit** (style đã xoá hoàn toàn).
8. `rg '\[ list \]|\[ preview \]' *.go` → **0 hit** ở code production (chip strings gone).
9. `go build -o lazyexplorer . && go vet ./... && go test ./...` xanh.
10. `go test -race ./...` xanh.
11. **Visual-verdict** (`oh-my-claudecode:visual-verdict`) — 4 frame: {list, preview} ×
    {horizontal, vertical}. Pass khi: glow đọc rõ ở glance, accent đúng phía pane focus,
    không còn chip; **chốt glyph vertical** (eighth `▔`/`▁` vs half `▀`/`▄`).

## 7. Task breakdown

> Trước khi sửa: `gitnexus_impact` cho `renderStatus`, `renderEntryRow`, `View`
> (divider render). Invoke `/test` trước khi viết/sửa test (level-picker + pitfalls
> color-profile determinism cho golden/visual). Sau khi sửa: `gitnexus_detect_changes`
> verify scope khớp §8.

- [ ] **T1 — Style.** Thêm `dividerFocusStyle`; xoá `focusChipStyle` + comment của nó
  (§5.1). *(theme.go)*
- [ ] **T2 — Divider glow horizontal.** Pad col phía focus mang `▐`/`▌` accent, `│` giữ
  dim; vẫn dựng một `dividerLine` repeat `bodyH` (§5.2, FR2). *(view.go)*
- [ ] **T3 — Divider glow vertical.** Hàng divider eighth-block accent `▔`/`▁` theo focus
  (§5.3, FR3). *(view.go)*
- [ ] **T4 — Bỏ chip khỏi `renderStatus`.** Xoá `chip` + ghép; `status = hints`; giữ
  nhánh statusMsg/rendering (§5.4, FR4). Cập nhật comment focus-signal (693-694). *(view.go)*
- [ ] **T5 — Comment cursor-dim.** `renderEntryRow` (§5.5): cursor-dim giờ củng cố divider
  glow. *(view.go)*
- [ ] **T6 — Tests.** *(focus_test.go + cập nhật comment liên quan)*
  - Sửa header comment `focus_test.go:6-8` (chip → divider glow + cursor dim).
  - Thay `TestStatusChipReflectsFocus` (`focus_test.go:429-434`) bằng test assert: status
    bar **không** chứa `" list "`/`" preview "` chip; và divider render (ANSI-stripped)
    chứa glyph accent đúng phía theo `focusPane`, cho cả 2 orientation.
  - Sửa comment `entryrow_test.go:277` (không còn "focus chip").
  - Giữ các test focus-logic khác (`TestTabTogglesFocus`, `TestClickSetsFocus`, …) — logic
    không đổi.
- [ ] **T7 — Visual-verdict.** 4 frame {list,preview}×{horizontal,vertical} → verdict pass
   + chốt glyph vertical (§6.11). *(harness throwaway)*
- [ ] **T8 — Reconcile `prd-pane-focus.md`.** Cập nhật D8/FR10(chip)/FR11/§5.6/§5.7 sang
  "divider glow"; giữ D9/D10/FR12 (cursor dim) + ghi nó củng cố glow. Positive framing —
  không để lại "~~chip~~". *(docs/prd-pane-focus.md)*
- [ ] **Tn — Verify.** `go build -o lazyexplorer . && go vet ./... && go test ./...` +
  `go test -race ./...` xanh; visual-verdict đạt; `gitnexus_detect_changes` scope khớp §8.

## 8. Files chạm tới

| File | Thay đổi |
|------|----------|
| `theme.go` | + `dividerFocusStyle` (`Foreground(colAccent)`); − `focusChipStyle` + comment (`theme.go:37-44`) |
| `view.go` | Divider horizontal glow (`view.go:275-280`); divider vertical glow (`view.go:253-254`); − chip ở `renderStatus` (`view.go:695-698`, `700`, `702`); cập nhật comment focus-signal (`693-694`) + cursor-dim (`348-350`). `focusPane` state + `View()` shape không đổi |
| `focus_test.go` | Header comment (6-8); thay `TestStatusChipReflectsFocus` (429-434) bằng assert no-chip + divider-glow-per-focus |
| `entryrow_test.go` | Comment (277) bỏ tham chiếu "focus chip" |
| `docs/prd-pane-focus.md` | **Reconcile khi ship** (T8): chip → divider glow ở D8/FR10/FR11/§5.6/§5.7; giữ cursor-dim. KHÔNG sửa cho tới khi PRD này thực thi |
