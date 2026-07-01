# PRD — Fix: click lại file *đang mở* không re-render preview

> Bug-shaped fix, nhỏ và tự-chứa. Sibling của `prd-fix-poll-preview-rerender.md`: cùng
> nguyên tắc "chỉ re-render khi *selected file* thực sự đổi", nhưng cho đường **mouse
> click** thay vì poll loop. Bug: click chuột trái vào đúng file đang được chọn (đang hiển
> thị trong preview) chạy `refreshPreview()` vô điều kiện ⇒ reset scroll về đầu + re-read
> disk + re-dispatch render async cho nội dung y hệt.

Status: **accepted** · Author: feature-dev session · Ngày: 2026-07-01 · Shipped: 2026-07-01

---

## 1. Bối cảnh & vấn đề

Nhánh list-click của `handleMouse` (`model.go:1546-1560`) route một click trái trong list
pane:

```go
if idx == m.cursor && m.entries[idx].isDir {
    m.descend() // click again on the selected folder opens it
} else {
    m.cursor = idx
    m.refreshPreview() // ← chạy cả khi idx == m.cursor cho một FILE
}
```

Khi user click đúng **file đang được chọn** (`idx == m.cursor`, không phải dir), nhánh
`else` gọi `refreshPreview()` (`model.go:679-741`) — dù selection không đổi. Hệ quả:
`previewTop = 0` (mất vị trí scroll), `srcWidth = 0` + `pendingWidth = 0` (ép tail
`syncPreview` re-dispatch render), re-read file từ disk (`readPreviewBytes`), huỷ selection
đang active, và nháy placeholder `(rendering…)` một frame trước khi render async land — tất
cả để dựng lại **đúng cùng một nội dung**.

Đường bàn phím **đã** không mắc lỗi này: `moveCursor` (`model.go:1607-1618`) guard
`if target == m.cursor { return }` — không đổi cursor thì không refresh. Nhánh mouse-click
là **path duy nhất** re-render trên một no-op selection ⇒ mouse và keyboard bất nhất
(vi phạm Consistency-is-Kindness).

Đúng nghịch positioning "glance beside agent" (`CLAUDE.md`): user hay click quanh list để
định vị, và click lại file đang đọc là thao tác tự nhiên — mỗi lần lại giật scroll về đầu.

## 2. Goal (1 câu)

Click trái vào **hàng đang được chọn** trong list pane: folder thì mở (click-to-open, giữ
nguyên), file thì **no-op** — preview giữ nguyên byte-for-byte (scroll, buffer đã render,
selection), không re-read disk, không re-dispatch render.

**Non-goal làm rõ:**
- KHÔNG cache nhiều file đã mở (không LRU keyed path+mtime+width). Phạm vi là **đúng file
  đang chọn**, không phải "một file *khác* từng mở" — cache đa-file đi ngược ethos
  simplicity và không phải điều "đang mở" ám chỉ.
- KHÔNG đụng click-to-open của **folder**: re-click một dir đang chọn vẫn `descend()` (D2).
- KHÔNG đụng click vào **hàng khác** (`idx != m.cursor`): vẫn `m.cursor = idx` +
  `refreshPreview()` — đổi selection thật thì refresh là đúng (FR3).
- KHÔNG đụng nhánh preview-pane click (`previewClick`, folder-listing) hay drag-to-select
  arm (`model.go:1522-1536`) — chúng ở nhánh `!overList`, path khác hẳn.
- KHÔNG đụng focus-set (`model.go:1517-1521`): click trong list pane vẫn set
  `focusPane = focusList` **trước** guard này — click list = focus list, bất kể no-op hay
  không (`prd-pane-focus.md` D7/FR8 giữ nguyên).

## 3. Quyết định đã chốt

| # | Quyết định | Giá trị | Lý do |
|---|-----------|---------|-------|
| D1 | Điều kiện no-op | `idx == m.cursor` **và** entry không phải dir | Selection không đổi ⇒ preview đã đúng. Khớp guard `target == m.cursor` của `moveCursor` (`model.go:1612`) để mouse ≡ keyboard |
| D2 | Folder re-click | Vẫn `descend()` | Click-to-open là hành động chủ đích duy nhất trên một hàng đang chọn (double-click-to-enter). Chỉ file bị suppress |
| D3 | Nguồn "đang mở" | Đúng entry tại `m.cursor` hiện tại | "Đang mở" = file đang hiển thị trong preview = selection hiện tại. Không thêm state/cache — giữ state surface phẳng |
| D4 | Focus khi no-op | Vẫn set `focusList` | Focus-set nằm **trước** guard (`model.go:1518`); click list = focus list là invariant riêng của `prd-pane-focus.md`, không được no-op nuốt |
| D5 | Selection đang active khi no-op | Nhánh file no-op gọi `m.cancelSelection()` | Click đã dời focus sang list; in-app preview selection là sub-state của `focusPreview` (`prd-preview-selection.md`) nên phải kết thúc — đúng như `FocusToggle` làm. Trước fix, `refreshPreview` huỷ nó như side effect; no-op path phải huỷ tường minh, nếu không focus=list mà key vẫn route vào `updateSelecting`. Chỉ clear cờ selection, KHÔNG đụng preview buffer/scroll (FR1 giữ) |

## 4. Functional requirements

- **FR1** — Click trái vào hàng của **file đang được chọn** (`idx == m.cursor`, không phải
  dir): `refreshPreview()` **không** chạy. `m.preview`, `m.previewTop`, `m.srcWidth`,
  `m.srcPath`, `m.srcRaw`, `m.previewPreStyled`, trạng thái selection giữ nguyên
  byte-for-byte; không có `previewRenderedMsg` mới phát sinh (`srcWidth` giữ ⇒ tail
  `syncPreview` cache-hit `srcWidth == w` ⇒ `return nil`).

- **FR2** — Click trái vào hàng của **folder đang được chọn** (`idx == m.cursor`, dir):
  `descend()` chạy như trước — vào folder đó (click-to-open, `prd D2`).

- **FR3** — Click vào một hàng **khác** (`idx != m.cursor`): `m.cursor = idx` +
  `refreshPreview()` chạy như trước — selection đổi, preview reload, scroll về đầu file mới.

- **FR4** — Focus không regression: click bất kỳ trong list pane set
  `focusPane = focusList` kể cả khi no-op (`prd-pane-focus.md` FR8).

- **FR5** — Nếu một in-app preview selection đang active (`m.selecting`) khi user re-click
  file đang mở: selection kết thúc (`m.selecting == false`) đồng bộ với việc focus dời sang
  list — nhưng `m.previewTop`/`m.preview` vẫn giữ nguyên (chỉ cờ selection bị clear, không
  re-render).

## 5. Technical design

> Kim chỉ nam: **một guard tại đúng một chỗ** — nhánh list-click của `handleMouse`. Không
> field mới, không message mới, không async mới. Chỉ tách "re-click file đang mở" (no-op)
> khỏi "re-click folder đang chọn" (descend) và "click hàng khác" (refresh).

### 5.1 `handleMouse` — guard no-op trong nhánh list-click (`model.go:1546-1560`)

Trước (nháy + mất scroll khi re-click file):

```go
if idx == m.cursor && m.entries[idx].isDir {
    m.descend()
} else {
    m.cursor = idx
    m.refreshPreview()
}
```

Sau (re-click hàng đang chọn: dir mở, file no-op):

```go
if idx == m.cursor {
    // Re-clicking the row that is already selected: a folder opens
    // (click-to-open, the one intentional action), a file is a NO-OP.
    // The preview is already showing this exact file, so re-running
    // refreshPreview would only reset the scroll to the top and re-read
    // + re-dispatch the async render for identical content — a wasted
    // render plus a lost scroll/selection position. Mirrors moveCursor's
    // "target == cursor → return" guard so mouse and keyboard agree.
    if m.entries[idx].isDir {
        m.descend()
    } else {
        // The click still moved focus to the list (set above), and an
        // in-app preview selection is a focusPreview sub-state — so end
        // it, exactly as FocusToggle does. refreshPreview used to cancel
        // it as a side effect; the no-op path must do it explicitly.
        m.cancelSelection()
    }
} else {
    m.cursor = idx
    m.refreshPreview()
}
```

Ghi chú correctness:

- Focus-set (`model.go:1517-1521`) chạy **trước** khối này ⇒ FR4 tự thoả, guard chỉ bao
  hành vi preview (D4).
- `idx` đã được clamp `0 <= idx < len(m.entries)` bởi hai early-return phía trên
  (`model.go:1539-1545`) ⇒ `m.entries[idx]` an toàn.
- Nhánh no-op **không đụng** `m.preview`/`srcWidth`/`pendingWidth` ⇒ tail `reconcilePreview`
  → `syncPreview` thấy `srcWidth == w` → cache-hit → `return nil`: không render mới, không
  churn (FR1).

### 5.2 Vì sao không cần đụng gì khác

- `refreshPreview` / `syncPreview` / `applyPreview` **giữ nguyên** — vẫn là single source
  cho preview pipeline; ta chỉ đổi *điều kiện gọi* `refreshPreview` trong nhánh click.
- `moveCursor` đã có sẵn guard tương đương (`model.go:1612`); fix này chỉ đưa nhánh click
  về cùng invariant, không phát minh cơ chế mới.
- Không state mới trên `model`: "đang mở" đọc trực tiếp từ `m.cursor` — cùng nguồn
  `refreshPreview` dùng.

### 5.3 Đã cân nhắc & **defer / bác bỏ**

- **Cache đa-file (LRU keyed path+mtime+width)** — **bác bỏ (ngoài scope + nghịch ethos)**.
  Sẽ cần eviction, invalidation theo mtime/width, memory cap — nhiều bề mặt cho một tool
  "glance beside agent" cố ý nhỏ. "Đang mở" ám chỉ file **hiện tại**, không phải lịch sử
  file đã xem.
- **Guard trong `refreshPreview` (early-return nếu `full == m.srcPath`)** — **bác bỏ**.
  `refreshPreview` là path chung cho mọi cursor-move (keyboard, search, poll-refresh,
  editor-return); nhét điều kiện "cùng path" vào đó làm nó gánh ngữ nghĩa của caller, mờ
  trách nhiệm. Guard thuộc về **call site** click — nơi biết đây là một re-click.
- **So sánh theo `m.srcPath` thay vì `idx == m.cursor`** — **defer (YAGNI)**. `idx ==
  m.cursor` đã đúng nghĩa "cùng hàng đang chọn" và rẻ hơn (không join path). `srcPath` chỉ
  set cho file renderable; dùng nó sẽ bỏ sót file plain/placeholder cũng nên no-op.

## 6. Acceptance criteria

### Gherkin

```gherkin
Feature: Clicking the already-open file does not re-render the preview

  Background:
    Given the explorer is open in normal mode with a list and a preview
    And a file is selected and its preview has rendered
    And the preview has been scrolled down

  Scenario: Re-clicking the selected file keeps the preview intact
    When I left-click the row of the file that is already selected
    Then the preview keeps its scroll position
    And no new preview render is dispatched

  Scenario: Re-clicking the selected folder opens it
    Given a folder is selected and shown as a listing in the preview
    When I left-click the row of that already-selected folder
    Then the explorer enters that folder

  Scenario: Clicking a different file reloads the preview
    When I left-click the row of a different file
    Then that file becomes selected
    And its preview is shown from the top

  Scenario: Re-clicking the open file ends an active preview selection
    Given I have started a line selection in the preview
    When I left-click the row of the file that is already selected
    Then focus moves to the list
    And the line selection is cancelled
    And the preview keeps its scroll position
```

### Checklist verify

1. Failing test trước (TDD): selected = file đã render + đã scroll, `handleMouse` click lại
   đúng hàng đó → assert `m.previewTop` giữ + `m.srcWidth` **không** về 0. FAIL trên code cũ
   (refreshPreview reset cả hai), PASS sau fix. ĐÃ VERIFY ✅ 2026-07-01 (`previewTop 0` +
   `srcWidth 0` khi chưa fix).
2. Regression folder: re-click folder đang chọn → `m.cwd` đổi sang folder đó (FR2). ĐÃ
   VERIFY ✅ 2026-07-01 (PASS cả trước lẫn sau fix).
3. Positive control (chống over-suppression): click hàng **khác** → cursor đổi + `previewTop`
   về 0 (FR3). ĐÃ VERIFY ✅ 2026-07-01.
4. Selection-cancel (FR5/D5): bắt đầu selection ở preview (`focusPreview` + `startSelection`),
   re-click hàng file đang mở → `m.selecting == false` + `m.focusPane == focusList` +
   `previewTop` giữ. FAIL trước khi thêm `cancelSelection` (`focusList` nhưng `selecting` còn
   true), PASS sau. ĐÃ VERIFY ✅ 2026-07-01.
5. `go build -o lazyexplorer . && go vet ./... && go test ./...` xanh; `go test -race` trên
   surface click/focus/scroll xanh. ĐÃ VERIFY ✅ 2026-07-01.

## 7. Task breakdown

- [x] **T1 — Failing test trước (TDD).** `clickreopen_test.go`: `clickListRow` helper;
  `TestListClickSameFileNoRerender` (re-click file đang mở → `previewTop`+`srcWidth` giữ),
  `TestListClickSameFolderStillDescends` (re-click folder → descend),
  `TestListClickDifferentFileRefreshes` (click hàng khác → refresh + scroll reset). ĐÃ
  VERIFY ✅ 2026-07-01 — same-file FAIL đúng lý do, hai regression PASS sẵn. *(clickreopen_test.go)*
- [x] **T2 — Guard no-op trong `handleMouse`.** Implement §5.1: `idx == m.cursor` → `descend()`
  khi dir, else `cancelSelection()` (no-op preview); giữ nhánh `refreshPreview` cho
  `idx != m.cursor`. ĐÃ VERIFY ✅ 2026-07-01 — T1 chuyển PASS. *(model.go)*
- [x] **T3 — Test edge selection-cancel.** `TestListClickSameFileCancelsSelection`: start
  selection ở preview, re-click file đang mở → `selecting == false` + `focusList` +
  `previewTop` giữ. FAIL trước khi thêm `cancelSelection` (đã empirically confirm
  `focusList`+`selecting=true`), PASS sau (D5/FR5). ĐÃ VERIFY ✅ 2026-07-01. *(clickreopen_test.go)*
- [x] **T4 — Verify.** `go build -o lazyexplorer . && go vet ./... && go test ./...` xanh
  (ĐÃ VERIFY ✅ 2026-07-01); `go test -race` trên surface click/focus/scroll xanh. Fix chỉ
  quyết *khi nào* `refreshPreview` chạy trong nhánh click; khi no-op `m.preview`/`previewTop`
  byte-identical ⇒ `View()` vẽ frame y hệt ⇒ không nháy — bất biến này chốt ở mức model
  (T1/T3), nên live-TTY smoke không thêm bằng chứng. *(gate)*

## 8. Files chạm tới

| File | Thay đổi |
|------|----------|
| `model.go` | `handleMouse` nhánh list-click: guard `idx == m.cursor` → `descend()` khi dir, `cancelSelection()` khi file (no-op preview); giữ `m.cursor = idx`+`refreshPreview()` cho `idx != m.cursor` (§5.1). Không field/struct/message mới |
| `clickreopen_test.go` | + `clickListRow` helper; `TestListClickSameFileNoRerender`, `TestListClickSameFileCancelsSelection`, `TestListClickSameFolderStillDescends`, `TestListClickDifferentFileRefreshes` (T1/T3) |
| `docs/prd-fix-click-preview-rerender.md` | File này |
