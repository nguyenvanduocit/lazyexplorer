# PRD — Preview scroll mượt (bug: step quá lớn)

> Bug: scroll trong panel preview hiện không mượt — mỗi tick wheel nhảy **3 dòng**,
> mỗi lần `J/K` hoặc `ctrl+d/u` nhảy **5 dòng**. Với một workflow "glance" cạnh agent,
> bước nhảy này lớn so với chiều cao panel preview, làm mắt phải bắt lại vị trí sau
> mỗi lần scroll → khó chịu, đặc biệt khi đang đọc code hoặc markdown từng dòng.

Status: **draft / chờ review** · Author: bug-filing session · Ngày: 2026-05-27

---

## 1. Bối cảnh & vấn đề

Scroll panel preview hiện chia ba đường, đều đi qua `scrollPreview(delta int)`
(`model.go:425-430`) và đều dùng `delta` cố định:

| Đường | Vị trí | `delta` hiện tại |
|-------|--------|------------------|
| Wheel mouse | `model.go:377` (up) · `model.go:386` (down) | `±3` dòng / notch |
| `J` / `K` | `model.go:506-509` | `±5` dòng / lần nhấn |
| `ctrl+d` / `ctrl+u` | `model.go:506-509` (cùng case với `J/K`) | `±5` dòng / lần nhấn |

Triệu chứng quan sát:

- **Wheel mịn nhất cũng nhảy 3 dòng/notch.** Trên một panel preview thường cao
  ~20-40 dòng, `3 / 30 ≈ 10%` chiều cao mỗi notch — đủ để mắt mất ngữ cảnh dòng đang
  đọc. So với editor / browser thông dụng (1 dòng / notch), cảm giác "rời rạc".
- **`J/K` cũng nhảy 5 dòng/lần** — không có cách nào "đi 1 dòng" bằng bàn phím.
  Khi cursor đang ở dòng cần đọc kỹ, không có nốt nhạc tinh tế nào để chỉ nhích nhẹ.
- **`ctrl+d/u` chỉ nhảy 5 dòng** — vốn theo convention Vim phải là *half-page*
  (jump lớn để duyệt nhanh). Hiện tại không có tầng "page" nào, mọi đường scroll đều
  ở step size na ná → mất khả năng vừa-fine-vừa-coarse.

Trong lazygit (reference cho keymap), `J/K` line-step 1 dòng, `ctrl+d/u` half-page —
hai tầng phục vụ hai mục đích khác nhau. lazyexplorer hiện chỉ có một tầng "vừa to
vừa thô".

> Bug này độc lập với markdown rendering — áp cho mọi preview (plain text, code,
> markdown). Hành vi scroll mượt mới sẽ tự chảy vào [[prd-markdown-view]] FR4 vì
> markdown dùng cùng `scrollPreview()`.

## 2. Goal (1 câu)

Scroll panel preview cho cảm giác "mượt" và có hai tầng tốc độ: **wheel + `J/K`**
nhích **1 dòng** mỗi tick (fine), **`ctrl+d/u`** nhảy **nửa chiều cao panel** mỗi
lần nhấn (page-half coarse) — tận dụng đúng cấu trúc bàn phím và mouse mà không
thêm keybind mới.

**Non-goal làm rõ:** đây KHÔNG phải kerning-mịn pixel-level (terminal vẫn line-based);
KHÔNG thêm animation/easing (xem [[reference_crush_tui]] cho hệ harmonica — defer);
KHÔNG thay đổi cách scroll panel list bên trái (`j/k` cursor move, đã ổn).

## 3. Quyết định đã chốt

| # | Quyết định | Giá trị | Lý do |
|---|-----------|---------|-------|
| D1 | Wheel step | **`±1` dòng / notch** (thay cho `±3`) | Mapping 1-1 với notch là chuẩn editor/browser; mỗi notch đã là 1 đơn vị user input, nhân thêm là gấp lần input giả tạo |
| D2 | `J/K` step | **`±1` dòng / lần nhấn** (thay cho `±5`) | Đối xứng với `j/k` ở list pane (di 1 entry); cho phép nhích từng dòng khi đọc kỹ |
| D3 | `ctrl+d/u` step | **nửa chiều cao body preview** (thay cho `±5` cố định) | Convention Vim, có thật một tầng "page" để duyệt nhanh; tỉ lệ theo panel nên scale đúng khi user kéo divider hoặc resize |
| D4 | Đơn vị `delta` của `scrollPreview` | **vẫn là số dòng nguyên** | Không cần đổi signature; D1-D3 chỉ thay đổi caller truyền vào |
| D5 | Hằng số step | Đặt tên có nghĩa (`previewLineStep = 1`) cạnh nơi dùng, không tản mát magic number | Cognitive load: reader đọc `scrollPreview(previewLineStep)` hiểu ngay ý định |

## 4. Functional requirements

- **FR1** — Một notch wheel lên trong panel preview → `previewTop` giảm đúng **1**
  (clamped tại 0). Một notch xuống → tăng đúng **1** (clamped tại `maxTop`).
- **FR2** — Một lần nhấn `J` → `previewTop` tăng đúng **1**; `K` → giảm đúng **1**.
  Giữ phím tự-repeat của terminal → mỗi event repeat tăng/giảm **1**.
- **FR3** — Một lần nhấn `ctrl+d` → `previewTop` tăng đúng `bodyH / 2` (lấy
  `bodyH` từ `m.previewScroll()`, làm tròn xuống, min 1); `ctrl+u` → giảm đúng
  `bodyH / 2`. Cả hai clamp vào `[0, maxTop]`.
- **FR4** — Khi `len(m.preview) <= bodyH` (nội dung không vượt panel), mọi đường scroll
  trên là no-op (`previewTop` giữ nguyên `0`) — clamp hiện tại của `scrollPreview` đã
  đảm bảo điều này; FR này chỉ chốt regression không xảy ra.
- **FR5** — Khi user kéo divider làm panel hẹp/rộng, hoặc resize terminal, step
  `ctrl+d/u` ở lần nhấn kế tiếp dùng `bodyH` mới — không cache step cũ.
- **FR6** — Khi `bodyH` rất nhỏ (`bodyH == 1` lúc terminal cực thấp), `ctrl+d/u` vẫn
  di chuyển đúng **1** dòng (floor `1/2 = 0` → bump lên `1`), không bị "kẹt".
- **FR7** — Hành vi scroll áp đồng đều cho mọi preview: plain text, code highlight,
  markdown đã render (xem [[prd-markdown-view]] FR4).

## 5. Technical design

> Kim chỉ nam: chỉ đổi **giá trị `delta` ở các caller**, không đụng `scrollPreview()`
> (`model.go:426`) — clamp logic đã đúng. `ctrl+d/u` đọc `bodyH` từ
> `m.previewScroll()` ngay tại điểm dùng để giữ tính tường minh (Explicitness over
> Implicitness — root `CLAUDE.md` §5).

### 5.1 Hằng số

Thêm trong `model.go` (gần định nghĩa `previewTop` ở `model.go:56`):

```go
// previewLineStep là số dòng di chuyển mỗi notch wheel hoặc mỗi lần nhấn J/K.
// 1 cho cảm giác mượt từng dòng; tầng "page" lớn hơn nằm ở ctrl+d/u.
const previewLineStep = 1
```

Không hằng cho `ctrl+d/u` vì giá trị động theo `bodyH` — biểu thức `max(1, bodyH/2)`
tính tại chỗ, có comment một dòng giải thích "page-half, tối thiểu 1 dòng".

### 5.2 Đổi caller wheel (`model.go:370-387`)

- `case tea.MouseButtonWheelUp` nhánh `else` → `m.scrollPreview(-previewLineStep)`
- `case tea.MouseButtonWheelDown` nhánh `else` → `m.scrollPreview(previewLineStep)`

### 5.3 Tách `J/K` khỏi `ctrl+d/u` (`model.go:505-509`)

Hiện hai cặp keybind dùng chung `case`. Tách thành hai cặp để mỗi cặp có step riêng,
và để comment giải thích semantic khác nhau:

```go
// preview scrolling — fine (1 dòng) cho đọc kỹ
case "J":
    m.scrollPreview(previewLineStep)
case "K":
    m.scrollPreview(-previewLineStep)

// preview scrolling — half-page cho duyệt nhanh
case "ctrl+d":
    _, bodyH := m.previewScroll()
    m.scrollPreview(max(1, bodyH/2))
case "ctrl+u":
    _, bodyH := m.previewScroll()
    m.scrollPreview(-max(1, bodyH/2))
```

### 5.4 Đã cân nhắc & defer khỏi v1

- **Easing / animation scroll** (mỗi notch trôi mượt qua vài frame): cần phụ thuộc
  mới (`tmp/harmonica`), thêm `tea.Tick` riêng cho animation, phá tính line-based
  thuần — defer cho tới khi có nhu cầu cụ thể.
- **Config step qua flag / config file**: vi phạm "No Abstractions Until Proven"
  (root `CLAUDE.md`). Giữ hằng `previewLineStep = 1`; nếu thực tế cần đổi, hằng đứng
  một mình dễ chỉnh.
- **Wheel multiplier khi giữ `shift`** (Vim `5J`-style): thêm phụ thuộc terminal
  emit được tổ hợp Shift+Wheel ổn định — defer.
- **`g` / `G` cho preview** (jump đỉnh/đáy): đã chiếm cho list pane (`model.go:493-498`),
  cần keybind mới hoặc context-aware — defer, không phải scope của bug này.

## 6. Acceptance criteria

```gherkin
Feature: Preview scroll mượt với hai tầng tốc độ

  Background:
    Given user đã chọn một file có nội dung dài hơn chiều cao panel preview

  Scenario: Một notch wheel = một dòng
    Given previewTop đang là 10
    When user scroll wheel xuống một notch trên panel preview
    Then previewTop trở thành 11

  Scenario: J nhích đúng một dòng
    Given previewTop đang là 0
    When user nhấn J một lần
    Then previewTop trở thành 1

  Scenario: K nhích lùi đúng một dòng
    Given previewTop đang là 5
    When user nhấn K một lần
    Then previewTop trở thành 4

  Scenario: ctrl+d nhảy nửa chiều cao panel
    Given panel preview có bodyH = 20 dòng
    And previewTop đang là 0
    When user nhấn ctrl+d một lần
    Then previewTop trở thành 10

  Scenario: ctrl+u đối xứng với ctrl+d
    Given panel preview có bodyH = 20 dòng
    And previewTop đang là 30
    When user nhấn ctrl+u một lần
    Then previewTop trở thành 20

  Scenario: ctrl+d trên panel cực thấp vẫn di chuyển ít nhất một dòng
    Given panel preview có bodyH = 1
    And previewTop đang là 0
    And nội dung preview có 50 dòng
    When user nhấn ctrl+d một lần
    Then previewTop trở thành 1

  Scenario: Nội dung ngắn hơn panel — scroll là no-op
    Given nội dung preview ngắn hơn bodyH
    And previewTop đang là 0
    When user scroll wheel xuống một notch
    Then previewTop vẫn là 0

  Scenario: Wheel trên panel list không ảnh hưởng preview
    Given previewTop đang là 7
    When user scroll wheel xuống trên panel list bên trái
    Then previewTop vẫn là 7
    And cursor list di chuyển xuống một entry
```

### Checklist verify

1. Khởi chạy thật, mở một file dài (vd `docs/prd-markdown-view.md`), wheel xuống
   chậm — quan sát preview trôi từng dòng, không nhảy 3.
2. Trong cùng file, giữ `J` repeat → preview chảy mượt từng dòng theo tốc độ
   key-repeat, không nhảy 5.
3. `ctrl+d` một lần ở terminal cao bình thường (`bodyH ≈ 20-30`) → preview nhảy
   xuống ~một nửa panel.
4. Kéo divider làm panel preview hẹp lại, terminal vẫn cao → `ctrl+d` vẫn nhảy
   ~`bodyH/2` (số `bodyH` không đổi khi kéo *ngang*; FR5 vẫn pass).
5. Thu terminal về cực thấp (`bodyH == 1`) → `ctrl+d` di chuyển đúng **1** dòng,
   không bị "kẹt" hay panic.
6. Mở một file ngắn hơn panel (vd file 3 dòng) → wheel/J/ctrl+d đều no-op,
   `previewTop` vẫn 0.
7. Wheel trên panel list (trái) vẫn di chuyển cursor như cũ, không đụng preview.
8. Mở một file `.md` → mọi scenario trên vẫn pass (markdown dùng chung
   `scrollPreview`).
9. `go build -o lazyexplorer . && go vet ./... && go test ./...` xanh.

## 7. Task breakdown

- [ ] **T1 — Thêm hằng `previewLineStep`.** Định nghĩa cạnh `previewTop` trong
  struct model, kèm comment §5.1. *(model.go)*
- [ ] **T2 — Đổi wheel step.** `tea.MouseButtonWheelUp/Down` nhánh preview dùng
  `±previewLineStep` (§5.2). *(model.go)*
- [ ] **T3 — Tách case `J/K` khỏi `ctrl+d/u` + đổi step.** `J/K` →
  `±previewLineStep`; `ctrl+d/u` → `±max(1, bodyH/2)` lấy từ `m.previewScroll()`
  (§5.3). *(model.go)*
- [ ] **T4 — Test unit cho `scrollPreview` caller.** Viết test (TDD: fail → fix
  → pass) đóng đinh các con số ở §6 Gherkin: wheel = 1, J/K = 1, ctrl+d/u =
  `bodyH/2` với floor-min-1. Test gọi `Update` với `tea.MouseMsg{Button: WheelDown}`
  và `tea.KeyMsg{Type: KeyCtrlD}` để cover cả hai đường. *(scroll_test.go — mới)*
- [ ] **T5 — Soi lại hints status bar.** `view.go:162` ghi `[wheel] scroll`; sau
  bug fix vẫn đúng (wheel vẫn scroll). Không cần đổi, nhưng confirm trong cùng
  commit để tránh drift. *(view.go — không sửa, chỉ ghi nhận)*
- [ ] **T6 — Sync `prd-markdown-view.md` FR4.** FR4 hiện ghi "Scroll preview
  (`J/K`, `ctrl+d/u`, wheel) hoạt động trên markdown đã render y như với plain
  text (đơn vị dòng sau-wrap)" — vẫn đúng sau bug fix vì markdown share
  `scrollPreview`. Confirm wording còn ổn, không cần đổi giá trị step (PRD đó
  không đóng đinh số). *(docs/prd-markdown-view.md — không sửa nếu wording ổn)*
- [ ] **T7 — Verify.** `go build -o lazyexplorer . && go vet ./... && go test ./...`
  xanh; chạy tay kiểm acceptance §6 mục 1-8.

## 8. Files chạm tới

| File | Thay đổi |
|------|----------|
| `model.go` | +`previewLineStep` const; wheel handlers dùng const; tách `J/K` / `ctrl+d/u`; `ctrl+d/u` dùng `max(1, bodyH/2)` |
| `scroll_test.go` (mới) | Unit test đóng đinh step values cho cả ba đường scroll |
| `docs/prd-smooth-preview-scroll.md` | File này |
| `docs/prd-markdown-view.md` | Không sửa (FR4 wording vẫn đúng); confirm trong T6 |
