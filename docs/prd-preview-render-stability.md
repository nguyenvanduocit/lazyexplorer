# PRD — footer render indicator: spinner mép phải (bỏ reflow)

Status: **accepted** · Author: fix-prd session · Ngày: 2026-05-28 · Shipped: 2026-05-28

---

## 1. Bối cảnh & vấn đề

`bug-footer-flicker.md` (open): indicator `"• rendering…"` được **prepend** vào status
(`view.go:707-709`), nên mỗi lần `pendingWidth` bật/tắt thì chip + dải hint **dịch phải 13
cột** rồi snap về, đồng thời `fitWidth(status, m.width-2)` (`view.go:710`) cắt cụt đuôi hint.
ĐÃ VERIFY ✅ 2026-05-28 (probe trên `renderStatus`): `pendingWidth 0→100` ⇒ chip dịch 13
display cols + trailing-clip. Cú nháy nổ trên mỗi lần render một file renderable (markdown/code/image).

Hai ranh giới đã được giải quyết ở nơi khác — PRD này **không** đụng:

- **Tần suất re-render** (poll re-render thừa selected file khi sibling đổi) đã fix + shipped
  ở `prd-fix-poll-preview-rerender.md` (per-file gate, `model.go:287-333`). Nên PRD này thuần
  là **layout fix**, không còn dính poll.
- **Tín hiệu focus** (chip `[ list ]`/`[ preview ]`) được `prd-focus-divider-glow.md` chuyển
  sang divider glow và **xoá chip**. PRD này **phối hợp**: indicator spinner sống ở footer
  **không-chip** đó. Hai PRD chạm `renderStatus` cùng một pass vào trạng thái cuối (§5.3).

## 2. Goal (1 câu)

Indicator render ở footer không làm dịch/cắt phần còn lại: hints đứng yên tuyệt đối, chỉ một
spinner ở mép phải động khi đang dựng preview.

**Non-goal làm rõ:**
- KHÔNG đụng poll path — tần suất re-render đã do `prd-fix-poll-preview-rerender.md` xử lý.
- KHÔNG đụng logic focus / dispatch / mouse — chip removal + divider glow thuộc `prd-focus-divider-glow.md`.
- KHÔNG đổi nội dung hint / keymap — chỉ vị trí + hình thức indicator.

## 3. Quyết định đã chốt

| # | Quyết định | Giá trị | Lý do |
|---|-----------|---------|-------|
| D1 | Vị trí + hình thức indicator | Spinner glyph ở **mép phải**, slot **2 cột reserve cố định** | Hints đứng yên; reserve cố định bỏ reflow **tận gốc** (slot không đổi bề rộng khi toggle); 2 cột là chrome tối thiểu, hợp ethos glance |
| D2 | Animation | Spinner **animated**, tick `100ms`, **chỉ** chạy khi `pendingWidth > 0` | Feedback "đang làm việc" rõ cho file render lâu; không tick khi idle để glance UI không bị đánh thức 10Hz vô ích |
| D3 | Frames | Braille `⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏` (1 display col) | Đúng 1 cột → không phình slot; lazygit-flavored; quay mượt |
| D4 | Guard tick loop | Field `spinning bool` — đúng **một** loop | Switch file nhanh dispatch nhiều render; không có guard sẽ spawn nhiều tick loop chồng |
| D5 | Style của spinner | Dùng lại `renderingStyle` (accent, `theme.go:35`) | Không thêm màu mới vào palette một-accent |

## 4. Functional requirements

- **FR1** — Khi `pendingWidth > 0`, spinner hiện ở mép phải footer; vị trí cột của toàn bộ
  hints **bằng đúng** lúc `pendingWidth == 0` (không dịch).
- **FR2** — Đuôi hint không bị cắt thêm do indicator: slot 2 cột reserve **bất kể** trạng thái
  render (idle = 2 space; render = space + glyph).
- **FR3** — Spinner đổi frame mỗi ~100ms trong lúc render; ngừng đổi khi render xong.
- **FR4** — Không có render nào đang bay (idle) ⇒ không `tea.Tick` spinner nào chạy (`spinning == false`).
- **FR5** — Switch file nhanh (nhiều dispatch liên tiếp) giữ **đúng một** tick loop.

## 5. Technical design

> Kim chỉ nam: một fix nhỏ — đổi cách `renderStatus` đặt indicator thành slot right-anchored
> cố định + một spinner loop tự-dừng. Pipeline async (`syncPreview`/`applyPreview` + gen-counter)
> giữ nguyên. Chip removal là việc của `prd-focus-divider-glow.md`; PRD này chỉ sở hữu indicator.

### 5.1 Footer indicator: slot right-anchored cố định (`view.go renderStatus`)

Footer cuối (sau khi `prd-focus-divider-glow.md` xoá chip) = `hints` (+ `statusMsg`) **bên
trái**, spinner ở slot 2 cột **bên phải**, chuỗi cuối luôn rộng đúng `contentW = m.width - 2`:

```go
contentW := m.width - 2            // statusBarStyle Padding(0,1)
slot := "  "                       // idle: 2 cột trống
if m.pendingWidth > 0 {
    slot = " " + renderingStyle.Render(spinnerFrames[m.spinnerFrame%len(spinnerFrames)])
}
left := fitWidth(status, contentW-2)                 // hints, chừa 2 cột cho slot
pad  := strings.Repeat(" ", max(0, contentW-2-lipgloss.Width(left)))
return statusBarStyle.Width(m.width).Render(left + pad + slot)
```

Slot **luôn** chiếm 2 cột nên `left` luôn fit `contentW-2` bất kể render hay không ⇒ hints
không dịch, đuôi không bị cắt thêm (FR1/FR2). Block intent cũ (`view.go:704-706`) bỏ; ý "đang
format" giờ do spinner mang. `spinnerFrames` định nghĩa ở `theme.go` (§8).

### 5.2 Spinner animation loop (`model.go`)

State mới (cạnh `renderGen`/`pendingWidth`, `model.go:157-158`): `spinnerFrame int`,
`spinning bool`. Msg + cmd (cạnh `tickMsg`, `model.go:33-37`):

```go
type spinnerTickMsg struct{}
const spinnerInterval = 100 * time.Millisecond
func spinnerTickCmd() tea.Cmd {
    return tea.Tick(spinnerInterval, func(time.Time) tea.Msg { return spinnerTickMsg{} })
}
```

Khởi động trong `syncPreview` tại điểm dispatch (ngay sau `pendingWidth = w`, `model.go:617`):
nếu `!m.spinning` → `m.spinning = true`, trả `tea.Batch(renderClosure, spinnerTickCmd())`;
đã spinning → chỉ trả `renderClosure`. Handler trong `Update` (cạnh case `previewRenderedMsg`,
`model.go:749-751`):

```go
case spinnerTickMsg:
    if m.pendingWidth > 0 {
        m.spinnerFrame++
        return m, spinnerTickCmd()   // còn render → quay tiếp
    }
    m.spinning, m.spinnerFrame = false, 0   // render xong → dừng, reset
    return m, nil
```

`applyPreview` set `pendingWidth = 0` (`model.go:641`) khi render land ⇒ tick kế thấy
`pendingWidth == 0` ⇒ dừng. Switch nhanh: `refreshPreview` reset `pendingWidth = 0` rồi
`syncPreview` set lại `w` cùng tick; `spinning` còn `true` ⇒ không spawn loop thứ hai (FR5).

### 5.3 Phối hợp với `prd-focus-divider-glow.md` + reconcile

- `prd-focus-divider-glow.md` sở hữu việc **xoá chip** + divider glow (D1/FR4/§5.4 của nó);
  PRD này sở hữu **slot spinner**. Hai cái chạm `renderStatus` default branch **cùng một pass**
  vào trạng thái cuối: `status = hints` (glow PRD) + slot spinner right-anchored (PRD này).
- Reconcile khi ship (positive framing — tả trạng thái đích, lý do đổi ở git/ADR *Hệ quả*):
  - `adr-async-markdown-render.md:31` (D5) + `prd-markdown-view.md` + `prd-code-highlight.md`:
    indicator là **spinner mép phải** thay chip `"• rendering…"` prepend.
  - `theme.go:35` comment `renderingStyle` → "spinner mép phải".
  - `bug-footer-flicker.md` → status `fixed by prd-preview-render-stability` (+ ghi glow PRD
    xoá chip cùng pass).

### 5.4 Đã cân nhắc & **defer khỏi v1**

- **Static dot** (không animation): bác bỏ — animated cho file render lâu feedback rõ.
- **Left gutter** / **giữ full text "• rendering…"**: bác bỏ — right-edge + glyph gọn hợp minimal-chrome (D1).
- **Component `bubbles/spinner`**: bác bỏ — tick liên tục sau start; ta chỉ muốn tick khi render (D2).

## 6. Acceptance criteria

### Gherkin

```gherkin
Feature: Footer render indicator giữ yên layout

  Background:
    Given explorer ở normal mode với list + preview, không còn chip focus ở status bar

  Scenario: Indicator không đẩy lệch phần còn lại của footer
    Given con trỏ đang ở một file cần render (markdown/code/image)
    When  bản preview đẹp đang được dựng
    Then  một spinner hiện ở mép phải của footer
    And   dải phím tắt giữ nguyên vị trí so với khi không render

  Scenario: Spinner chỉ động khi đang dựng preview
    Given không có bản preview nào đang được dựng
    Then  footer không có spinner động
    And   không có vòng animation nào chạy nền

  Scenario: Chuyển sang file không cần render thì không có spinner
    Given con trỏ chuyển sang một file plain-text không có renderer
    Then  footer không hiện spinner
```

### Checklist verify

1. Vị trí hints (sau `ansi.Strip`) **bằng nhau** giữa `pendingWidth == 0` và `> 0`; đuôi hint
   không bị cắt thêm (failing test của `bug-footer-flicker.md`).
2. `spinnerTickMsg` khi `pendingWidth > 0` → `spinnerFrame` tăng + reschedule; khi `== 0`
   → `spinning` về false, không reschedule.
3. `syncPreview` dispatch lần đầu set `spinning = true` + trả batch có spinner tick; dispatch
   khi đã spinning không spawn loop thứ hai.
4. `go build -o lazyexplorer . && go vet ./... && go test ./...` xanh; + visual verdict footer
   khi render (hints không nhảy, spinner mép phải).

## 7. Task breakdown

- [ ] **T1 — Slot indicator right-anchored.** `renderStatus` default: bỏ prepend, đặt spinner
  vào slot 2 cột mép phải, `left` fit `contentW-2` (§5.1). *(view.go)*
- [ ] **T2 — Spinner loop.** Fields `spinnerFrame`/`spinning`; `spinnerTickMsg`/`spinnerTickCmd`;
  start trong `syncPreview` khi dispatch + `!spinning`; case `spinnerTickMsg` (§5.2). *(model.go)*
- [ ] **T3 — Tests (TDD).** Hints-không-dịch giữa `pendingWidth` 0/>0; spinner tick start/stop;
  không loop khi idle (§6 checklist 1-3, viết đỏ trước). *(view_test/spinner_test)*
- [ ] **T4 — Docs reconcile.** chip→spinner ở `adr-async-markdown-render.md`/`prd-markdown-view.md`/
  `prd-code-highlight.md`; comment `theme.go:35`; flip `bug-footer-flicker.md` (§5.3). *(docs/, theme.go)*
- [ ] **T5 — Verify.** `go build -o lazyexplorer . && go vet ./... && go test ./...` xanh;
  visual verdict footer khi render.

## 8. Files chạm tới

| File | Thay đổi |
|------|----------|
| `view.go` | `renderStatus` default: slot spinner right-anchored thay prepend (§5.1) |
| `model.go` | fields `spinnerFrame`/`spinning`; `spinnerTickMsg`/`spinnerTickCmd`; start trong `syncPreview`; case `spinnerTickMsg` (§5.2) |
| `theme.go` | `spinnerFrames` braille slice; comment `renderingStyle` → spinner (§5.3) |
| `docs/bug-footer-flicker.md` | status → `fixed by prd-preview-render-stability` |
| `docs/adr-async-markdown-render.md`, `docs/prd-markdown-view.md`, `docs/prd-code-highlight.md` | chip prepend → spinner mép phải (§5.3) |
| `docs/prd-preview-render-stability.md` | File này |
