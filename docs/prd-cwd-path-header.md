# PRD — Path header (luôn-hiện cho biết bạn đang ở đâu)

Status: **accepted** · Author: pain-hunter (cwd-path-header) · Ngày: 2026-05-30

---

## 1. Bối cảnh & vấn đề

lazyexplorer sống cạnh một coding agent trong một pane terminal chật. User descend sâu
(`src/auth/handlers/`) rồi mất dấu — không có gì trên màn hình nói **đang ở thư mục nào**.

Bằng chứng (đã verify ✅ 2026-05-30, `rg -n 'm\.cwd' --type go -g '!*_test.go'`): `m.cwd`
được **đọc 7 lần** trong production code (reload/stat/join — `model.go:473`, `commands.go:142`…)
nhưng **render ra màn hình ZERO lần**. `renderStatus` (`view.go:789`) chỉ mang hints + spinner
+ statusMsg tạm thời — không bao giờ là path. Mọi peer TUI đều hiện path (lazygit, superfile
`model_render.go:76` `fileLocation`, ranger, nnn). Đây là một **navigation fundamental thiếu**.

Đây là passive chrome thuần: **không thêm keybind, không thêm mode** → vượt qua gate "earn its
UI" của `CLAUDE.md` ở mức rủi ro thấp nhất.

## 2. Goal (1 câu)

Một header row luôn-hiện ở đỉnh màn hình, full-width trên cả hai pane, hiện path của thư mục
hiện tại **relative to jail root**, để user navigate sâu cạnh agent không bao giờ mất dấu mình
đang xem folder nào.

**Non-goal làm rõ:** không thêm keybind/toggle (passive chrome); không hiện path tuyệt đối
(jail-relative, slash-form); không thay đổi list/preview render content; không là breadcrumb
click-được (header là no-pane zone).

## 3. Quyết định đã chốt

| # | Quyết định | Giá trị | Lý do |
|---|-----------|---------|-------|
| D1 | Placement | Global top header row, full-width trên cả 2 pane | Geometry đã có một seam `firstRow` luồn vào list hit-test (`model.go:1369`); dịch toàn body xuống 1 row là one-knob, root-cause-correct. List-only title sẽ desync row budget của list vs preview, vỡ JoinHorizontal alignment. |
| D2 | `headerH` | `1` row, hằng số chung (`view.go:134`) | Một nguồn duy nhất cho `layout()` (view) và `setTopFromY` (model) đọc — không drift. Đúng 1 row vì headerStyle không vẽ border. |
| D3 | Chrome style | Accent foreground, bold, **không border, không background** | Crush look (`tmp/crush/internal/ui/styles/quickstyle.go`: Dialog border-only, no Background — không copy code). Border/BorderBottom sẽ render thêm 1 row → header thành 2 row → off-by-one mọi hit-test. |
| D4 | Truncation | Trái/đầu — giữ **tail** (`…/auth/handlers`) | `fitWidth` (`view.go:1085`) cắt PHẢI, giấu mất folder hiện tại — đúng cái header sinh ra để hiện. Cần helper mới giữ đuôi. |
| D5 | Path content | Root: basename của root; dưới root: `<root-base>/<rel-slash>` | Cho user một anchor có tên (project folder) thay vì "." trống. Slash-form (`relRoot` đã `filepath.ToSlash`, `commands.go:188`) → cross-OS. |
| D6 | Mode gate | `modeSearch`/`modeChanges` → label ("search results"/"changes"), không phải cwd | Trong flat-list modes, list là result-set relative-to-root, **không phải** một directory. Hiện cwd ở đó là nói dối về "current directory". |

## 4. Functional requirements

- **FR1** — Header luôn hiện ở screen row 0, full-width, ở mọi orientation (2-col & 1-col stacked).
- **FR2** — `modeNormal` ở root → header = basename của jail root.
- **FR3** — `modeNormal` dưới root → header = `<root-base>/<rel-slash>` (slash-form mọi OS).
- **FR4** — Path quá rộng → cắt từ ĐẦU với "…" dẫn đầu, giữ folder hiện tại (đuôi) luôn nhìn thấy; width không bao giờ vượt screen width.
- **FR5** — `modeSearch` → "search results"; `modeChanges` → "changes" (không bao giờ là cwd cũ).
- **FR6** — Header là **no-pane zone**: left-click trên header không flip focus, không move cursor, không start drag; wheel trên header noop. Mirror status-row exclusion.
- **FR7** — Body (list + divider + preview) + mọi mouse hit-test dịch xuống đúng `headerH` row; click/drag vẫn trúng đúng entry/divider ở cả 2 orientation.

## 5. Technical design

**Kim chỉ nam:** `headerH` là một-knob duy nhất. `layout()` là single source of geometry cho cả
render lẫn mouse; mọi Y-origin field mang `firstRow` để render và hit-test không thể bất đồng.

### 5.1 Hằng số (`view.go:134`)

`headerH = 1` đặt cạnh `widthBreakpoint`/`dividerHeight`. Cả `view.go` và `model.go` đọc cùng hằng
này — đó là contract chống drift giữa `layout()` và `setTopFromY`.

### 5.2 `layout()` thread `firstRow` (`view.go:64,78-81,98-99`)

- `bodyH := max(m.height-1-headerH, 3)` (`view.go:64`) — body loại trừ header (đỉnh) + status (đáy).
- 1-col stacked: `firstRow: headerH`, `dividerYStart: headerH + topInner`, `previewFirstRow: headerH + topInner + dividerHeight` (`view.go:78-81`).
- 2-col side-by-side: `firstRow: headerH`, `previewFirstRow: headerH` (`view.go:98-99`).

CRITICAL Y-origin: thiếu `headerH` trong `previewFirstRow`/`dividerYStart` thì `previewClick`
(`model.go:1458` `row := y - g.previewFirstRow`) và divider drag (`model.go:1258` overDivider)
off-by-one.

### 5.3 `setTopFromY` — inverse của `dividerYStart` (`model.go:1417-1423`)

Đây là **công thức cặp đôi** thứ hai (không nằm trong `layout()`), dễ quên:

```
bodyH := max(m.height-1-headerH, 3)
m.topRatio = float64(y-headerH) / float64(bodyH)
```

Cả tử (`y-headerH`) lẫn mẫu (`bodyH`) mang offset header — đây là nghịch đảo của
`dividerYStart = headerH + topInner`: screen-Y drag phải trừ `headerH` để về body-relative row,
và `bodyH` phải **bằng** `bodyH` của `layout()` nếu không divider nhảy dưới ngón tay user.

### 5.4 Render (`view.go:371`, `headerPath` `view.go:799`, `renderHeader` `view.go:819`)

`View()` ráp `strings.Join([header, body, status], "\n")` (`view.go:371`) — KHÔNG `JoinVertical`
(nó left-pad mọi dòng tới block rộng nhất → thêm trailing space cho status mode prompt → churn
snapshot). `renderHeader(w)` (`view.go:819`) style qua `headerStyle.Width(w)`, text = `fitPathRight`
trên inner width (`w - GetHorizontalFrameSize()`, vì lipgloss v2 `.Width` là OUTER width).

`headerPath()` (`view.go:799`) mode-aware: `modeSearch`→"search results", `modeChanges`→"changes",
else `filepath.Base(m.root)` + (rel nếu != ".").

### 5.5 `fitPathRight` (`view.go:1113`)

Mirror của `fitWidth`: giữ **đuôi**, drop rune từ ĐẦU, prefix "…". Rune/width-aware (CJK 2 cols);
result width không vượt `w`. "…" tốn 1 col → khi cắt, fit đuôi vào `w-1` rồi prefix ellipsis.

### 5.6 `headerStyle` (`theme.go:101`)

`lipgloss.NewStyle().Foreground(colAccent).Bold(true).Padding(0,1)` — không Border, không Background.
Một accent, đồng bộ cursor row / divider glow / render spinner.

### 5.7 Mouse gating (`model.go:1290,1328,1351`)

- Wheel: `if e.Y < g.firstRow || overDivider { return }` (`model.go:1290`).
- Drag-start: thêm `e.Y >= g.firstRow` (`model.go:1328`) — horizontal `overDivider` là X-only, thiếu guard thì header-row click ở divider column sẽ start drag.
- Click no-pane: `if e.Y < g.firstRow { return }` (`model.go:1351`) trước focus-set — mirror overDivider noop, header không flip focus.

### 5.8 Đã cân nhắc & **defer khỏi v1**

- **`modalSize`/`overlayCentered` không đổi**: modal float centered trên TOÀN màn hình (`view.go:236`), header chỉ là một phần của background nó phủ lên — độc lập với body geometry.
- **Breadcrumb click-được**: header là passive chrome; click-segment-để-nhảy sẽ thêm hit-zone phức tạp — defer, chưa có nhu cầu.
- **Segment-boundary truncation** (cắt đúng ranh "/"): `fitPathRight` drop rune thuần từ đầu; ví dụ rơi đúng `/auth` chỉ là tình cờ — không gold-plate.

## 6. Acceptance criteria

### Gherkin

```gherkin
Feature: Path header luôn hiện cho biết đang ở đâu

  Scenario: Ở root
    Given lazyexplorer mở tại jail root "proj"
    When màn hình render
    Then screen row 0 hiện "proj"

  Scenario: Dưới root
    Given cwd là "<root>/src/auth"
    When màn hình render
    Then screen row 0 hiện "proj/src/auth" (slash-form)

  Scenario: Path sâu quá rộng
    Given cwd sâu khiến path vượt screen width
    When màn hình render
    Then header bắt đầu bằng "…" và vẫn chứa folder hiện tại ở cuối
    And width của header không vượt screen width

  Scenario: Flat-list mode không nói dối
    Given user vào modeSearch
    When màn hình render
    Then header hiện "search results", không phải cwd cũ

  Scenario: Header là no-pane zone
    Given focus đang ở preview
    When user left-click trên header row (Y=0)
    Then focus không đổi, cursor không đổi, không có drag

  Scenario: Body dịch xuống đúng 1 row
    Given header chiếm row 0
    When user click ở screen row firstRow trong list pane
    Then cursor nhảy tới entries[0]
```

### Checklist verify

1. ✅ `TestLayoutHeaderGeometry` — `firstRow==headerH`, `bodyH==height-1-headerH`, `previewFirstRow`/`dividerYStart` mang `firstRow` ở cả 2 orientation.
2. ✅ `TestFitPathRight` — giữ đuôi + "…" dẫn đầu, rune/CJK-aware, width ≤ w.
3. ✅ `TestHeaderPath` — root→basename; dưới root→`<base>/<rel>`; search/changes→label.
4. ✅ `TestViewHeaderRow` — View() row 0 chứa folder hiện tại + "…" khi path sâu; width ≤ screen.
5. ✅ `TestHeaderRowIsNoPaneZone` / `TestHeaderColumnClickDoesNotStartDrag` — click header không flip focus / move cursor / start drag. `TestHeaderWheelIsNoPaneZone` — wheel (up & down) trên header không scroll list (cursor không đổi) lẫn preview (`previewTop` không đổi) ở cả 2 orientation, pin clause `e.Y < g.firstRow` của wheel guard (`model.go:1290`).
6. ✅ `TestListClickHonorsHeaderOffset` / `TestPreviewClickHonorsHeaderOffset` — click ở `firstRow`/`previewFirstRow` map đúng entry (parity proof).
7. ✅ Geometry-coupled tests (`divider_test.go`, `resize_test.go`, `focus_test.go`, `previewclick_test.go`) đã dịch +headerH, xanh.
8. ✅ Visual verdict (image) — đã verify ✅ 2026-05-30. `TestDumpCwdHeaderFrames` (`zz_dump_test.go`, env-gated `LAZYEXPLORER_DUMP_DIR`) sinh 3 frame durable; render qua `freeze` → PNG → soi mắt: header đúng **1 row, accent purple bold, KHÔNG border, KHÔNG background** (floating label, no double-frame — đúng crush look), body (list/divider/preview) + status dịch xuống đúng 1 row ở cả 2 orientation, divider align. Truncation `…<tail>` chứng minh bởi `TestViewHeaderRow` (width 20). Text dump (`ansi.Strip`) chỉ xác nhận layout/row-shift; màu + frame xác nhận từ image.

Gate (đã verify ✅ 2026-05-30):

```
go build -o lazyexplorer . && go vet ./... && go test ./...
```

plus `go test -race ./...` (ok 12.2s) + visual verdict image (xem item 8).

## 7. Task breakdown

- **T1** — `headerH` const + `layout()` thread `firstRow`/`previewFirstRow`/`dividerYStart` (`view.go`).
- **T2** — `setTopFromY` inverse offset (`model.go`).
- **T3** — `headerPath` + `renderHeader` + `fitPathRight` + `View()` join (`view.go`); `headerStyle` (`theme.go`).
- **T4** — Mouse gating: wheel / drag-start / click no-pane (`model.go`).
- **T5** — Tests mới (`header_test.go`, `header_mouse_test.go`) + dịch geometry-coupled tests.

## 8. Files chạm tới

| File | Thay đổi |
|------|----------|
| `view.go` | `headerH` const; `layout()` `bodyH`/`firstRow`/`previewFirstRow`/`dividerYStart`; `View()` join header; `headerPath`/`renderHeader`/`fitPathRight`; geometry comment. |
| `model.go` | `setTopFromY` offset; mouse guards (wheel/drag-start/click no-pane). |
| `theme.go` | `headerStyle` (accent fg, no border/background). |
| `header_test.go` | Geometry + truncation + content + View() substring tests (mới). |
| `header_mouse_test.go` | No-pane zone + drag guard + click parity tests (mới). |
| `divider_test.go`, `resize_test.go`, `focus_test.go`, `previewclick_test.go` | Dịch hardcoded-Y + geometry assertions theo `headerH`. |
