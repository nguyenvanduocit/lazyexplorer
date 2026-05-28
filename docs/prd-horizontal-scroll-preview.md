# PRD — Horizontal scroll & wrap toggle cho preview pane (vim-adapted)

> Feature: preview pane hiện **mất nội dung** ở mọi dòng dài hơn chiều rộng pane —
> plain text bị `fitWidth` cắt với `…` (`view.go:467`), code bị `ansi.Truncate`
> cắt với `…` (`fs.go:340`), không có cách nào xem phần bị cắt. Thêm hai cơ chế
> vim-adapted: (1) **horizontal scroll** — pan viewport ngang để lộ nội dung bên
> phải, với edge indicator `‹`/`›` báo còn nội dung; (2) **wrap toggle** — `w`
> chuyển giữa nowrap (mặc định, scrollable) và soft-wrap (xuống dòng, không mất gì).
> Vì preview KHÔNG có cursor, ta adapt phím horizontal-motion của vim (`h`/`l`)
> thành scroll trực tiếp — đúng tinh thần "trong nowrap mode cursor motion scroll
> viewport" của vim, bỏ phần cursor.

Status: **accepted** · Author: feature-dev session · Reviewer: critic (independent pass, 2026-05-28) · Ngày: 2026-05-28 · Shipped: 2026-05-28 (✅ `go build && go vet && go test ./... && go test -race ./...` green)

> **Design resolution at implementation (2026-05-28):** the PRD's `previewPreStyled`
> gate could not distinguish code (pannable) from markdown (not) — both are preStyled.
> Resolved with a `previewScrollable` flag set per preview (plain + code = true;
> markdown/image/folder = false), and `renderCodePreview` now returns **full-width**
> lines (no chroma pre-truncation) so the horizontal window has overflow to pan
> across. Markdown stays verbatim (glamour-wrapped); its lines fit the width so
> hscroll is a natural no-op and `w` is gated off via `previewScrollable`. The
> reflow seam + visual-line `previewTop` is hooked once at syncPreview's head (+
> applyPreview) rather than the PRD's scattered call sites — one cache-guarded
> place covers nav / resize / drag / toggle.

---

> **Baseline note (đọc trước khi implement).** Nhánh **`trial-integration`** đã
> **integrated + committed**: `9859fc1 feat(search)` (modeSearch), `4b18fa9 feat(focus)`
> (focusPane), `e188683 test(integration)`, và `prd-smooth-preview-scroll.md` reconcile.
> Hệ quả:
> - **Phụ thuộc cứng — ship order.** PRD này cần **KeyMap registry** từ
>   `prd-keymap-and-command-palette.md` — PRD đó *accepted nhưng CHƯA implement* →
>   PRD này ship **sau** nó. `focusPane` (pane-focus) thì đã landed (commit 4b18fa9),
>   nên dispatch `if m.focusPane == focusList/Preview` đã có sẵn để mở rộng.
> - **Symbol là anchor ổn định**, không phải số dòng — mọi `model.go:NNN`/`view.go:NNN`
>   là snapshot cũ, re-pin theo cây đã commit. `view.go`/`fs.go` citations
>   (`view.go:444-472` renderPreview, `fs.go:340` ansi.Truncate, `fs.go:430` glamour
>   wrap) đã verify chính xác trên stable files; `model.go` re-pin (`updateNormal`
>   ~L871, focus dispatch L879-944).
> - **ANSI API đã VERIFIED tồn tại** (critic pass): `ansi.TruncateLeft(s, n, prefix)`,
>   `ansi.Hardwrap(s, limit, preserveSpace)`, `ansi.StringWidth(s)` đều có trong
>   `x/ansi v0.11.7`. T1 BLOCKER coi như resolved — vẫn giữ contract test để đóng đinh.
> - Dispatch của `h`/`l` mở rộng case `GoUp`/`OpenEntry` có sẵn (xem §5.6 B1).
## 1. Bối cảnh & vấn đề

Preview pane render qua `renderPreview` (`view.go:444-472`). Mỗi dòng nội dung
đi qua một trong ba đường, tất cả đều **cắt cụt** dòng dài, không cuộn ngang được:

| Loại preview | Đường render | Xử lý dòng dài | Vị trí |
|---|---|---|---|
| Plain text | `m.preview` → `fitWidth(line, w)` | Cắt rune tới `w` cols + chèn `…` | `view.go:467`, `view.go:508-520` |
| Code (chroma) | `renderCodePreview` → `ansi.Truncate(line, width, "…")` | Cắt ANSI-aware tới `width` + `…` | `fs.go:340` |
| Markdown (glamour) | `m.preview` (preStyled) → render thẳng | Glamour đã word-wrap tới `width` | `fs.go:430` (`WithWordWrap`) |

Hệ quả quan sát:

- **Code dòng dài mất phần đuôi.** Một dòng Go 140 cols trong preview pane 60
  cols → user thấy 60 cols đầu + `…`, **không có cách nào** xem cols 61-140.
  Với vibe-code workflow (liếc code agent vừa sửa), đây là pain trực tiếp —
  signature dài, struct tag, URL trong comment đều bị nuốt.
- **Plain text / log cùng vấn đề.** File log cột rộng, CSV, JSON một dòng →
  cắt cụt, mất cột bên phải.
- **Markdown thì OK** — glamour word-wrap sẵn, không mất nội dung; nhưng đó là
  một engine riêng, không phải hành vi thống nhất của preview.

> ĐÃ VERIFY ✅ (2026-05-28): đọc `view.go:444-472`, `view.go:508-520`,
> `fs.go:331-343`, `fs.go:409-447`. Hiện trạng: plain + code = nowrap-lossy
> (cắt, mất), markdown = wrap (glamour). Không có horizontal scroll state nào
> trên model (`grep -n previewHScroll model.go` → 0 hit).

### Vim giải quyết thế nào (và ta adapt ra sao)

Vim có hai chiến lược cho dòng dài, toggle bằng `:set wrap` / `:set nowrap`:

1. **`wrap` (vim default)**: dòng dài xuống dòng visual (không mất nội dung).
   `linebreak`/`breakindent` tinh chỉnh chỗ ngắt + thụt lề.
2. **`nowrap`**: horizontal scroll thực sự. Phím: `zl`/`zh` (±1 col),
   `zL`/`zH` (±nửa màn hình), `zs`/`ze` (cursor về đầu/cuối screen). Option
   `sidescroll`/`sidescrolloff` điều khiển auto-scroll khi cursor quá mép.
   `listchars=extends:>,precedes:<` hiện `<`/`>` ở mép báo nội dung bị cắt.

**Adaptation cho lazyexplorer** (preview KHÔNG có cursor — đây là điểm mấu chốt):

- Vim dùng `h`/`l` để di chuyển **cursor** ngang; ở `nowrap` mode khi cursor
  chạm mép thì viewport scroll theo. Preview pane của ta không có cursor →
  ta map `h`/`l` **thẳng** thành viewport horizontal scroll. Đây là adaptation
  trung thành: bỏ tầng cursor, giữ hành vi "h/l = đi ngang".
- `sidescrolloff` (đệm quanh cursor) **không áp dụng** — không có cursor để đệm.
- `listchars precedes/extends` → ta render `‹`/`›` ở mép pane khi có nội dung
  bị cắt mỗi bên. Đây là phần dịch trực tiếp.
- `zs`/`ze` (scroll cursor to start/end of screen) **không áp dụng** — defer.

> Vì `z`-prefix của vim (`zl`/`zh`/`zL`…) là **multi-key chord**, implement nó
> đòi một pending-key state machine — đúng loại phức tạp mà
> `prd-keymap-and-command-palette.md` đã cố ý defer (no chords, no counts). Phím
> `h`/`l`/`H`/`L`/`0` thì **đang free ở preview focus** (xem dưới) → adaptation
> này vừa đơn giản hơn vừa đúng tinh thần vim hơn. Chi tiết §3 D2.

### Phụ thuộc vào hai PRD khác

PRD này **xây trên** (consume, không re-implement):

- **`prd-pane-focus.md`** (draft) — định nghĩa `focusPane` (list | preview).
  Horizontal scroll keys chỉ active khi `focusPane == focusPreview`. Quan trọng:
  pane-focus FR5 đã cho `h`/`left`/`backspace` thành **no-op khi focusPreview**
  (chúng là list-navigation, không có nghĩa khi đọc preview) → `h`/`l` **trống**
  ở preview focus để PRD này dùng. Đây là tiền đề.
- **`prd-keymap-and-command-palette.md`** (draft) — `KeyMap` registry. PRD này
  thêm binding mới vào registry (xem §5.6). Nếu keymap PRD ship trước, thêm field;
  nếu PRD này ship trước, dùng inline `switch` tạm rồi keymap PRD hấp thụ sau.

> Cả ba PRD orthogonal về goal, nhưng PRD này **ưu tiên ship sau** hai PRD kia
> vì nó consume `focusPane` + `KeyMap`. Resolution rules §5.6.

## 2. Goal (1 câu)

Khi `focusPane == focusPreview`, cho preview plain-text/code **cuộn ngang** bằng
`h`/`l` (±1 col), `H`/`L` (±nửa width), `0` (reset) với edge indicator `‹`/`›`
báo nội dung bị cắt, và `w` **toggle wrap** giữa nowrap (scrollable, mặc định)
và soft-wrap (xuống dòng, không mất nội dung).

**Non-goal làm rõ:**
- KHÔNG áp cho **markdown** preview — glamour word-wrap sẵn (`fs.go:430`), không
  có dòng dài để scroll; `w` + hscroll là no-op khi previewing markdown (§5.5).
- KHÔNG áp cho **folder preview** (`previewIsDir`) — entry name đã `fitWidth`,
  hiếm vượt width; giữ nguyên hành vi.
- KHÔNG `sidescrolloff` / `sidescroll` auto-scroll — không có cursor để đệm.
- KHÔNG `zs`/`ze` (scroll-to-start/end-of-screen) — defer.
- KHÔNG `breakindent` (wrap thụt lề theo dòng gốc) — defer; lipgloss/ansi wrap
  flush-left v1.
- KHÔNG `linebreak` (wrap tại word boundary) — v1 hard-wrap tại col; word-aware
  wrap defer.
- KHÔNG horizontal scroll bằng mouse (drag/shift-wheel) — keyboard-only v1.
- KHÔNG `$` (scroll-to-end-of-longest-line) — `L` repeat tới được; defer phím riêng.

## 3. Quyết định đã chốt

| # | Quyết định | Giá trị | Lý do |
|---|-----------|---------|-------|
| D1 | `previewWrap` default | **`false` (nowrap)** | Khớp hành vi hiện tại (plain+code đang truncate, `view.go:467`/`fs.go:340`) và làm horizontal scroll active ngay khi mở preview — đúng pain gốc ("muốn scroll ngang"). `w` toggle sang wrap khi muốn đọc prose. Markdown — prose chính của project — đã wrap sẵn qua glamour (`fs.go:430`), nên nowrap-default chỉ ảnh hưởng code/log, nơi giữ alignment cột là đúng. (vim-default-là-wrap cân nhắc ở §5.7.) |
| D2 | Phím horizontal scroll | **`h`/`l` (±1), `H`/`L` (±half), `0` (reset)** | Preview KHÔNG có cursor → phím horizontal-motion gốc của vim (`h`/`l`) map thẳng thành view-scroll, đúng tinh thần "nowrap mode: cursor motion scroll viewport" của vim. `h`/`l` đang free ở `focusPreview` (pane-focus FR5 cho chúng no-op). Tránh `z`-prefix chord vì cần pending-key state machine — đúng loại phức tạp keymap PRD đã defer; giữ nhất quán (cost của chord ghi ở §5.7). |
| D3 | Phạm vi áp dụng | **plain text + code** preview only | Markdown wrap sẵn (glamour); folder preview entry ngắn. Hai cái đó hscroll/wrap = no-op. |
| D4 | `w` toggle wrap active khi | `focusPane == focusPreview` | Wrap là thuộc tính preview; nhất quán với "preview keys act in preview focus" (pane-focus). Relaxation (cho `w` ở list focus) defer. |
| D5 | Fine step ngang | **`h`/`l` = ±1 col** | Đúng vim `zl`/`zh`; terminal key-repeat làm pan nhanh khi giữ; mirror "fine=1" của `prd-smooth-preview-scroll.md` D1/D2. |
| D6 | Coarse step ngang | **`H`/`L` = ±`max(1, w/2)`** (nửa preview width) | Đúng vim `zL`/`zH` half-screen; tỉ lệ theo width nên scale khi kéo divider/resize; mirror `ctrl+d/u` half-page của smooth-scroll D3. |
| D7 | Reset ngang | **`0` → `previewHScroll = 0`** | Vim `0` = về đầu dòng (leftmost col). `‹` biến mất khi về 0. |
| D8 | Edge indicator | **`‹` (U+2039) mép trái, `›` (U+203A) mép phải**, tô `dimStyle` | Dịch `listchars precedes/extends` của vim. Light glyph cùng họ với `dividerGlyph` (`view.go:129`). |
| D9 | Cột dành cho indicator | **Reserve theo điều kiện global**: 1 col trái nếu `previewHScroll > 0`; 1 col phải nếu **bất kỳ** dòng visible nào bị cắt phải | Content width đồng nhất mọi dòng (không jitter per-line) → code/cột thẳng hàng. Per-line chỉ quyết định `‹`/`›`-hay-space trong cột đã reserve. |
| D10 | Wrap-mode rendering | **Pre-compute** wrapped visual lines vào cache (`reflowPreview`), mirror cách markdown precompute qua glamour | Vertical scroll (`previewTop`/`previewLen`) đếm **visual lines** → math hiện có (`view.go:410-437`) hoạt động không đổi khi `previewLen` đọc cache. Tránh re-wrap mỗi frame. |
| D11 | Cache key của reflow | `(previewDisplayWidth, previewDisplayWrap, content-identity)` | Re-compute chỉ khi width đổi (resize/divider), wrap toggle, hoặc preview content đổi (refreshPreview/applyPreview). Steady-state render = đọc cache. |
| D12 | `previewHScroll` clamp | `[0, max(0, previewMaxLineWidth - contentW)]` | `previewMaxLineWidth` tính trong `reflowPreview`; clamp như `scrollPreview` clamp vertical. Nội dung không vượt width → maxHScroll=0 → `h`/`l`/`L` no-op (giống vertical FR4 của smooth-scroll). |
| D13 | hscroll khi wrap=true | **Forced 0, keys no-op** | Wrap rồi thì không có gì bên phải để pan; `h`/`l`/`H`/`L`/`0` no-op (status hint không liệt kê chúng ở wrap mode). |
| D14 | Reset hscroll khi đổi selection | `previewHScroll = 0` trong `refreshPreview` reset hygiene (`model.go:251-263`) | Cùng discipline `previewTop = 0` reset: chọn file mới → về đầu cả dọc lẫn ngang. |
| D15 | `previewWrap` persist qua selection | **Giữ nguyên** (không reset khi đổi file) | Wrap là preference của user cho session, không phải per-file state. Khác `previewHScroll` (vị trí, reset per-file). |
| D16 | ANSI horizontal ops cho code | `ansi.TruncateLeft` (bỏ `hscroll` cols trái) + `ansi.Truncate` (giới hạn contentW phải); wrap dùng `ansi.Hardwrap` | charmbracelet/x/ansi (`go.mod:10`, đã dùng `ansi.Truncate` ở `fs.go:340`). API exact verify T1. |
| D17 | Telemetry | `action.preview_hscroll{direction}` + `action.preview_wrap_toggle{wrap}` qua `m.tel.Record` | Đo mức dùng hscroll/wrap → input cho v2 (vd có nên default wrap khác). Pattern `model.go:240`. |
| D18 | Visual line cap khi wrap | Wrap có thể nở dòng → tôn trọng `maxPreviewLines = 2000` (`fs.go:101`) **trên source lines**, KHÔNG cap lại sau wrap | Source đã cap 2000; wrap nở ra nhiều hơn nhưng vertical scroll xử lý được. Không cap kép (sẽ cắt giữa dòng wrap, khó hiểu). |

## 4. Functional requirements

- **FR1** — `model` thêm hai field: `previewHScroll int` (offset cột, ≥0) và
  `previewWrap bool` (default `false` per D1). `newModel` (`model.go:115`) không
  cần set explicit nếu default `false` = zero value; nếu D1 flip sang `true`,
  set trong `newModel`.

- **FR2** — Khi `focusPane == focusPreview` **và** preview là plain/code **và**
  `previewWrap == false`:
  - `l` / `right` → `previewHScroll += previewColStep` (clamp), pan phải.
  - `h` / `left` → `previewHScroll -= previewColStep` (clamp ≥0), pan trái.
  - `L` → `previewHScroll += max(1, contentW/2)` (clamp).
  - `H` → `previewHScroll -= max(1, contentW/2)` (clamp ≥0).
  - `0` → `previewHScroll = 0`.
  (`previewColStep = 1`, D5.)

- **FR3** — `w` khi `focusPane == focusPreview` → toggle wrap qua `toggleWrap()`
  (§5.5): flip `previewWrap`, reset `previewHScroll = 0` (D13), `reflowPreview`,
  re-clamp `previewTop`, **và giữ nguyên vị trí đọc** — source line đang ở đỉnh
  viewport trước toggle vẫn ở đỉnh sau toggle (qua `previewSrcStart` mapping), KHÔNG
  nhảy sang dòng vô quan. (M4: nếu chỉ giữ nguyên số `previewTop` thì meaning đổi
  logical↔visual → viewport nhảy, phá đúng mục đích "đang đọc dòng này, wrap nó ra".)

- **FR4** — Trong `previewWrap == true` mode (plain/code): mỗi source line được
  hard-wrap tới content width thành nhiều visual line; `previewTop`/`previewLen`
  đếm visual lines; `h`/`l`/`H`/`L`/`0` **no-op** (D13).

- **FR5** — Edge indicator (nowrap mode, plain/code):
  - Khi `previewHScroll > 0`: cột trái nhất của **mỗi** dòng render = `‹`
    (dimStyle); content bắt đầu sau nó (D9 reserve global-left).
  - Khi **bất kỳ** dòng visible nào còn nội dung vượt `previewHScroll + contentW`:
    cột phải nhất = `›` (dimStyle) ở những dòng bị cắt, space ở dòng không cắt
    (D9 reserve global-right).
  - Content width `contentW = w - (previewHScroll>0 ? 1 : 0) - (anyRightCut ? 1 : 0)`.

- **FR6** — Khi nội dung không có dòng nào vượt `w` (mọi dòng ≤ `w`):
  `previewHScroll` clamp về 0, không indicator nào hiện, `h`/`l`/`H`/`L` no-op
  (D12). FR này chốt regression "file ngắn dòng → scroll ngang không làm gì".

- **FR7** — Markdown preview (`previewPreStyled == true` qua glamour): `w` toggle
  + hscroll keys **no-op**; markdown luôn wrapped (`fs.go:430`). Status hint khi
  focusPreview + markdown KHÔNG liệt kê hscroll/wrap (FR12). Glamour-rendered
  lines render y như hiện tại (`view.go:466` skip fitWidth path).

- **FR8** — Folder preview (`previewIsDir == true`): hscroll + wrap no-op; render
  giữ nguyên `view.go:447-459`.

- **FR9** — Đổi selection (`refreshPreview`, `model.go:220`): `previewHScroll`
  reset về 0 cùng `previewTop` reset (D14); `previewWrap` GIỮ nguyên (D15);
  `reflowPreview` chạy với wrap state hiện tại cho content mới.

- **FR10** — Resize / kéo divider đổi `contentW`: `reflowPreview` re-compute
  (cache key width đổi — D11); `previewHScroll` re-clamp vào range mới; trong
  wrap mode visual lines re-wrap tới width mới (giống markdown reflow FR7 của
  `prd-markdown-view.md`).

- **FR11** — `previewLen()` (`view.go:410-415`) trả số **visual lines** đang
  hiển thị: wrap=true → len(wrapped cache); wrap=false → len(logical lines) =
  len(m.preview) (hscroll không đổi line count). Vertical scroll math
  (`previewScroll`/`scrollPreview`, `view.go:428-436`/`model.go:682-686`) không
  đổi — chỉ đọc `previewLen()` mới.

- **FR12** — Status hint (cross-ref keymap PRD FR14) khi `focusPane == focusPreview`,
  plain/code:
  - nowrap: thêm `[h/l] scroll  [H/L] half  [0] reset  [w] wrap` vào hint.
  - wrap: thêm `[w] nowrap` (chỉ toggle hiện).
  - markdown/folder: KHÔNG thêm hscroll/wrap hint.

- **FR13** — Telemetry (D17): `action.preview_hscroll` với `{direction:
  "left"|"right", step: "fine"|"half"}` mỗi lần scroll ngang thực sự đổi offset;
  `action.preview_wrap_toggle` với `{wrap: bool}` mỗi lần `w`. Non-blocking
  (`model.go:240` pattern).

- **FR14** — Mouse: hscroll KHÔNG bind mouse v1; wheel vẫn vertical scroll
  (`model.go:563-597` giữ nguyên). Shift+wheel / drag-pan defer.

- **FR15** — hscroll keys + `w` khi `focusPane == focusList`: **no-op** (preview
  state không đổi). Đối xứng pane-focus FR5 (preview keys chỉ act ở preview focus).

## 5. Technical design

> **Kim chỉ nam:** một seam precompute `reflowPreview(contentW)` chứa toàn bộ
> độ phức tạp wrap/visual-line, mirror cách markdown đã precompute qua glamour.
> `previewLen` đọc cache; vertical scroll math không đổi một dòng. nowrap
> hscroll là windowing per-line tại render — không đổi line count. Hai chế độ,
> một cache.

### 5.1 State trên `model` (`model.go`)

```go
type model struct {
    // …existing fields…

    preview    []string // source lines (logical for plain/code; glamour-wrapped for md)
    previewTop int       // scroll offset — NOW indexes previewDisplay (visual lines)

    // Horizontal scroll + wrap (plain/code only — see PRD §3 D3).
    previewHScroll int  // column offset into each line; 0 = flush left. nowrap only.
    previewWrap    bool // false = nowrap+hscroll (default, D1); true = soft-wrap to width

    // Reflow cache. previewDisplay is what the vertical scroller iterates over:
    // in wrap mode it is m.preview hard-wrapped to width (each source line →
    // ≥1 visual line); in nowrap mode it is m.preview itself (logical lines,
    // windowed horizontally at render time, so the count is unchanged).
    // Recomputed by reflowPreview only when the cache key changes (D11).
    previewDisplay      []string
    previewDisplayW     int   // contentW previewDisplay was built at (cache key)
    previewDisplayWrap  bool  // wrap mode previewDisplay was built at (cache key)
    previewMaxLineWidth int   // widest source line (nowrap hscroll clamp, D12)
    previewSrcStart     []int // previewSrcStart[s] = visual index source line s begins (wrap-toggle position, §5.2)
}
```

> **previewTop semantics đổi nhẹ**: trước đây index `m.preview`; giờ index
> `previewDisplay`. Trong nowrap mode `previewDisplay == m.preview` (cùng count)
> nên không có thay đổi quan sát được. Trong wrap mode `previewDisplay` dài hơn
> → previewTop cuộn qua visual lines (đúng kỳ vọng). Markdown: `previewDisplay
> == m.preview` (glamour đã wrap, reflow copy thẳng) — không đổi.

> **M3 — Audit mọi reader của `previewTop`** (semantics đổi từ index-`m.preview`
> sang index-`previewDisplay`; implementer phải sửa TẤT CẢ, không chỉ `previewLen`):
> | Reader (vị trí ~hiện tại) | Trước | Sau |
> |---|---|---|
> | `previewScroll` clamp (`view.go` ~L435) | `min(previewTop, max(0, previewLen()-bodyH))` | giống, nhưng `previewLen()` giờ trả `len(previewDisplay)` |
> | `renderPreview` slice (`view.go` ~L462: `for i := top; i < len(m.preview)`) | iterate `m.preview` trực tiếp | iterate `m.previewDisplay` (§5.4 đã rewrite — đảm bảo đổi cả vòng lặp, không chỉ `previewLen`) |
> | mouse-wheel scroll (`handleMouse` → `scrollPreview`, `model.go` ~L587-602) | scroll logical lines | scroll **visual** lines (wrap mode: wheel đi qua dòng wrap — cần AC, §6) |
> | `syncFromDisk` restore `prevTop` (`model.go` ~L199, `refreshPreview` reset) | restore/reset logical index | giờ là visual index — clamp qua `scrollPreview(0)` sau reflow giữ hợp lệ |
> Số dòng `view.go`/`model.go` là chỉ-dấu (re-pin per Baseline note); symbol +
> hành vi là phần chốt.

### 5.2 `reflowPreview` — seam precompute (`model.go`)

```go
// reflowPreview rebuilds previewDisplay (the visual lines the vertical scroller
// iterates) from m.preview, the wrap mode, and the content width. It is the
// single place wrap-expansion happens — mirroring how markdown precomputes its
// wrapped lines through glamour. Cheap to call; guarded by a cache key so the
// render path doesn't re-wrap every frame.
//
//   - markdown (previewPreStyled): glamour already wrapped to width → display
//     = m.preview verbatim; maxLineWidth is irrelevant (hscroll disabled).
//   - folder (previewIsDir): not called (folder render path doesn't reflow).
//   - plain/code, wrap=true: hard-wrap each source line to w → flattened.
//   - plain/code, wrap=false: display = m.preview (logical lines); record the
//     widest line for the hscroll clamp.
func (m *model) reflowPreview(w int) {
    if w <= 0 {
        return // width unknown yet
    }
    if m.previewDisplayW == w && m.previewDisplayWrap == m.previewWrap && m.previewDisplay != nil {
        return // cache hit
    }
    m.previewDisplayW = w
    m.previewDisplayWrap = m.previewWrap

    // previewSrcStart[s] = visual-line index where source line s begins. Drives
    // wrap-toggle reading-position preservation (toggleWrap, §5.5). In nowrap and
    // markdown it is identity (1 source = 1 visual); in wrap it tracks expansion.
    srcStart := make([]int, len(m.preview))

    // Markdown: pre-wrapped by glamour. Verbatim. (hscroll/wrap no-op, FR7.)
    if m.previewPreStyled {
        m.previewDisplay = m.preview
        m.previewMaxLineWidth = 0
        for i := range srcStart {
            srcStart[i] = i // identity (toggle never runs for markdown, kept safe)
        }
        m.previewSrcStart = srcStart
        return
    }

    if m.previewWrap {
        // wrap=true: expand each source line to ≤w visual lines.
        var out []string
        for s, line := range m.preview {
            srcStart[s] = len(out) // this source line begins at the current visual count
            out = append(out, wrapLine(line, w)...) // ANSI-aware for code, rune for plain
        }
        m.previewDisplay = out
        m.previewMaxLineWidth = 0 // no hscroll in wrap mode
        m.previewSrcStart = srcStart
        return
    }

    // wrap=false: logical lines unchanged (1:1); record widest for hscroll clamp.
    m.previewDisplay = m.preview
    maxW := 0
    for s, line := range m.preview {
        srcStart[s] = s // identity
        if lw := lineWidth(line); lw > maxW { // ANSI-aware width for code
            maxW = lw
        }
    }
    m.previewMaxLineWidth = maxW
    m.previewSrcStart = srcStart
}

// sourceLineAt returns the source line index whose visual block contains visual
// line v (largest s with previewSrcStart[s] <= v). visualLineFor is its inverse:
// the first visual line of source line s. Together they let toggleWrap (§5.5)
// keep the same source line pinned to the viewport top across a wrap flip.
func (m model) sourceLineAt(v int) int {
    ss := m.previewSrcStart
    if len(ss) == 0 {
        return 0
    }
    // binary search: rightmost s with ss[s] <= v
    lo, hi := 0, len(ss)-1
    for lo < hi {
        mid := (lo + hi + 1) / 2
        if ss[mid] <= v {
            lo = mid
        } else {
            hi = mid - 1
        }
    }
    return lo
}

func (m model) visualLineFor(s int) int {
    ss := m.previewSrcStart
    if s < 0 || s >= len(ss) {
        return 0
    }
    return ss[s]
}
```

`wrapLine` + `lineWidth` (`fs.go`, cạnh `fitWidth`/`ansi` helpers):

```go
// wrapLine hard-wraps one preview line to w columns, returning ≥1 visual lines.
// Code lines carry ANSI (per-line self-contained SGR, fs.go:288) → use the
// ANSI-aware hard-wrap so escapes survive the split. Plain lines have no ANSI →
// rune-slice. Empty line → one empty visual line (preserve blank rows).
func wrapLine(line string, w int) []string {
    if w <= 0 || line == "" {
        return []string{line}
    }
    if hasANSI(line) {
        // ansi.Hardwrap inserts newlines at width boundaries, ANSI-aware.
        return strings.Split(ansi.Hardwrap(line, w, false), "\n")
    }
    // Plain: rune-slice into w-wide chunks (lipgloss.Width-aware for CJK).
    var out []string
    r := []rune(line)
    for len(r) > 0 {
        cut := runePrefixWidth(r, w) // count of leading runes that fit in w cols
        out = append(out, string(r[:cut]))
        r = r[cut:]
    }
    if len(out) == 0 {
        out = []string{""}
    }
    return out
}

// lineWidth returns the display-column width of a (possibly ANSI) line.
func lineWidth(line string) int {
    if hasANSI(line) {
        return ansi.StringWidth(line)
    }
    return lipgloss.Width(line)
}

func hasANSI(s string) bool { return strings.Contains(s, "\x1b") }
```

> ANSI API (D16) — verify trong T1:
> | Call | Mục đích | Falsification |
> |---|---|---|
> | `ansi.Hardwrap(s, w, false)` | wrap=true cho code | `TestAnsiHardwrap`: code line 120-col, w=40 → 3 dòng, mỗi ≤40, SGR còn nguyên |
> | `ansi.TruncateLeft(s, n, "")` | nowrap pan: bỏ `n` cols trái (§5.4) | `TestAnsiTruncateLeft`: bỏ 10 cols đầu giữ màu phần còn lại |
> | `ansi.Truncate(s, w, "")` | giới hạn contentW phải (đã dùng `fs.go:340`) | đã verified in production |
> | `ansi.StringWidth(s)` | đo width ANSI-aware | đã có trong package |
>
> MEDIUM confidence các API trên tồn tại trong `github.com/charmbracelet/x/ansi`
> (`go.mod:10`); `ansi.Truncate` đã dùng thật. Nếu `TruncateLeft` không có,
> fallback: strip ANSI khi pan code (mất màu lúc hscroll>0) — degradation
> acceptable, ghi ở §5.7.

### 5.3 Reset hygiene + reflow call sites

`refreshPreview` reset hygiene (`model.go:251-263`) thêm hscroll reset (D14):

```go
m.previewTop = 0
m.previewHScroll = 0      // NEW: new selection → flush-left, mirror previewTop reset
m.preview = nil
m.previewDisplay = nil    // NEW: invalidate reflow cache (content changing)
m.previewSrcStart = nil   // NEW: invalidate source→visual mapping (rebuilt by reflow)
m.previewMaxLineWidth = 0 // NEW: exhaustive reset; reflow recomputes, but keep hygiene complete
// previewWrap NOT reset — it's a session preference (D15)
// (previewDisplayW / previewDisplayWrap need NOT reset: reflow's cache-key check
//  rebuilds because previewDisplay is now nil — the nil guard forces a rebuild.)
// …rest of reset unchanged…
```

`reflowPreview` được gọi từ ba chỗ (giống `previewBodyWidth` cache invalidation):

1. **`applyPreview`** (`model.go:428-431`) sau khi set `m.preview = msg.lines`:
   ```go
   m.preview = msg.lines
   m.previewPreStyled = msg.preStyled
   m.srcWidth = msg.width
   m.reflowPreview(m.previewBodyWidth()) // NEW: build display lines
   m.scrollPreview(0)                    // clamp into freshly-sized content
   ```

2. **Plain/placeholder path trong `refreshPreview`** (`model.go:300-309`) sau khi
   set `m.preview = plainLines(...)`: gọi `m.reflowPreview(m.previewBodyWidth())`.
   (Renderable file path đi qua async → applyPreview reflow; plain path đồng bộ
   nên reflow tại chỗ.)

3. **`w` toggle** + **resize** (`Update` WindowSizeMsg, `model.go:457`): reflow
   với width mới. WindowSizeMsg đã trigger `syncPreview` re-render cho renderable;
   thêm `m.reflowPreview(m.previewBodyWidth())` cho non-renderable (plain) reflow.

> Render path (`renderPreview`) gọi `reflowPreview` **defensive đầu hàm** với
> cache guard — nếu state đã reflow thì no-op, nếu chưa (edge) thì build. Single
> safe entry point.

### 5.4 `renderPreview` — windowing ngang + indicator (`view.go:444-472`)

```go
func (m model) renderPreview(w int) string {
    m.reflowPreview(w) // cache-guarded; ensures previewDisplay built at this w
    top, bodyH := m.previewScroll()

    if m.previewIsDir {
        // unchanged — folder branch (view.go:447-459)
    }

    // Visible slice of display lines.
    end := min(top+bodyH, len(m.previewDisplay))
    visible := m.previewDisplay[top:end]

    // wrap=true OR markdown: lines already fit w; render verbatim (markdown
    // skips fitWidth for ANSI, FR7; wrapped plain/code already ≤ w).
    if m.previewWrap || m.previewPreStyled {
        var lines []string
        for _, line := range visible {
            if !m.previewPreStyled { // plain wrapped line: safe to fit (no ANSI)
                line = fitWidth(line, w)
            }
            lines = append(lines, line)
        }
        return strings.Join(lines, "\n")
    }

    // nowrap plain/code: horizontal window + edge indicators.
    return m.renderHWindow(visible, w)
}

// renderHWindow renders visible lines in nowrap mode: each line is sliced to the
// horizontal window [previewHScroll, previewHScroll+contentW), with ‹ / › edge
// indicators (D8/D9). Indicator columns are reserved by GLOBAL condition so
// content width is uniform across lines (no per-line jitter, code stays aligned).
func (m model) renderHWindow(visible []string, w int) string {
    left := m.previewHScroll
    showLeft := left > 0

    // Probe: does any visible line extend past the window's right edge?
    // contentW provisional (without right indicator) to decide reservation.
    provW := w
    if showLeft {
        provW--
    }
    anyRightCut := false
    for _, line := range visible {
        if lineWidth(line)-left > provW {
            anyRightCut = true
            break
        }
    }

    contentW := w
    if showLeft {
        contentW--
    }
    if anyRightCut {
        contentW--
    }
    if contentW < 1 {
        contentW = 1 // degenerate narrow pane: best-effort, mirror leftInnerWidth floor
    }

    var out []string
    for _, line := range visible {
        seg := hSlice(line, left, contentW) // [left, left+contentW) ANSI/rune-aware
        var b strings.Builder
        if showLeft {
            b.WriteString(dimStyle.Render("‹"))
        }
        b.WriteString(seg)
        if anyRightCut {
            if lineWidth(line)-left > contentW {
                b.WriteString(dimStyle.Render("›"))
            } else {
                b.WriteByte(' ') // reserved col, this line not cut
            }
        }
        out = append(out, b.String())
    }
    return strings.Join(out, "\n")
}

// hSlice extracts display columns [left, left+width) from a line. ANSI-aware for
// code (TruncateLeft drops the left cols preserving SGR, then Truncate caps the
// right); rune-aware for plain. (D16)
func hSlice(line string, left, width int) string {
    if width <= 0 {
        return ""
    }
    if hasANSI(line) {
        s := ansi.TruncateLeft(line, left, "")
        return ansi.Truncate(s, width, "")
    }
    r := []rune(line)
    // drop `left` display-columns from the front (CJK-aware)
    start := runePrefixWidth(r, left)
    r = r[start:]
    cut := runePrefixWidth(r, width)
    return string(r[:cut])
}
```

`runePrefixWidth` (`view.go`, cạnh `fitWidth`):

```go
// runePrefixWidth returns the count of leading runes from r whose cumulative
// display width is ≤ w. Used to slice plain (non-ANSI) lines on column
// boundaries, CJK/wide-glyph aware via lipgloss.Width.
func runePrefixWidth(r []rune, w int) int {
    if w <= 0 {
        return 0
    }
    acc, n := 0, 0
    for _, c := range r {
        cw := lipgloss.Width(string(c))
        if acc+cw > w {
            break
        }
        acc += cw
        n++
    }
    return n
}
```

### 5.5 hscroll clamp + handlers (`model.go`)

```go
// scrollPreviewH pans the preview viewport horizontally by delta columns,
// clamped to [0, maxHScroll]. maxHScroll is the widest line minus the content
// width — when content fits, maxHScroll is 0 and any pan is a no-op (D12, FR6).
// No-op entirely in wrap mode or for markdown/folder (no horizontal overflow).
func (m *model) scrollPreviewH(delta int) {
    if m.previewWrap || m.previewPreStyled || m.previewIsDir {
        return // D13 / FR7 / FR8
    }
    _, _ = m.previewScroll() // (kept for parity; width via previewBodyWidth)
    w := m.previewBodyWidth()
    // Approximate content width (indicators shrink it by ≤2); clamp against the
    // widest line so the last column is reachable. Over-clamp by indicator cols
    // is harmless — render re-derives the exact window.
    maxH := max(0, m.previewMaxLineWidth-max(1, w-2))
    m.previewHScroll = min(max(0, m.previewHScroll+delta), maxH)
}
```

`updateNormal` (composing với pane-focus dispatch + keymap registry):

> **B1 — `h`/`l` KHÔNG có case riêng.** `PreviewScrollLeft` ≡ `GoUp` và
> `PreviewScrollRight` ≡ `OpenEntry` cùng key code (`h`/`left`, `l`/`right`).
> Trong Go `switch` một key chỉ trúng **một** case → nếu thêm
> `case key.Matches(msg, km.PreviewScrollLeft)` riêng thì hoặc nó hoặc case
> `GoUp` thành dead code (tuỳ thứ tự). Vì vậy PRD này **mở rộng nhánh `else`
> (focusPreview) của case `GoUp`/`OpenEntry` có sẵn** (keymap PRD §5.3, nơi
> hiện chỉ xử lý `focusList`). Chỉ `H`/`L`/`0`/`w` (key độc nhất) mới là case mới.

```go
// EXTEND existing GoUp case (keys h/left/backspace) — was: focusList → ascend.
case key.Matches(msg, km.GoUp): // ≡ PreviewScrollLeft (h/left) in focusPreview
    if m.focusPane == focusList {
        m.ascend()
    } else { // focusPreview: pan left
        before := m.previewHScroll
        m.scrollPreviewH(-previewColStep)
        if m.previewHScroll != before {
            m.tel.Record("action.preview_hscroll", map[string]any{"direction": "left", "step": "fine"})
        }
    }
    // NOTE: backspace also maps to GoUp; in focusPreview it pans left too — harmless,
    // backspace has no other preview meaning. If undesirable, drop "backspace" from
    // GoUp's WithKeys (keymap PRD) and keep h/left only.

// EXTEND existing OpenEntry case (keys enter/l/right) — was: focusList → descend.
case key.Matches(msg, km.OpenEntry): // ≡ PreviewScrollRight (l/right) in focusPreview
    if m.focusPane == focusList {
        m.descend()
    } else { // focusPreview: pan right
        before := m.previewHScroll
        m.scrollPreviewH(previewColStep)
        if m.previewHScroll != before {
            m.tel.Record("action.preview_hscroll", map[string]any{"direction": "right", "step": "fine"})
        }
    }
    // NOTE: enter also maps to OpenEntry; in focusPreview it pans right. If a
    // future "enter opens file in $EDITOR" lands (pane-focus defer list), this
    // branch needs splitting enter from l/right.

// --- new cases unique to this PRD (no collision) ---
case key.Matches(msg, km.PreviewHScrollHalfRight): // L
    if m.focusPane == focusPreview {
        m.scrollPreviewH(max(1, m.previewBodyWidth()/2))
    }
case key.Matches(msg, km.PreviewHScrollHalfLeft): // H
    if m.focusPane == focusPreview {
        m.scrollPreviewH(-max(1, m.previewBodyWidth()/2))
    }
case key.Matches(msg, km.PreviewHScrollReset): // 0
    if m.focusPane == focusPreview {
        m.previewHScroll = 0
    }
case key.Matches(msg, km.PreviewToggleWrap): // w
    if m.focusPane == focusPreview {
        m.toggleWrap() // §5.5a — captures + restores reading position (M4)
    }
```

`toggleWrap` preserves reading position (M4 — không nhảy viewport khi toggle):

```go
// toggleWrap flips wrap mode while keeping the same SOURCE line at the top of
// the viewport. Without this, previewTop (a visual-line index) keeps its numeric
// value as its meaning flips logical↔visual, jerking the viewport to an unrelated
// line — defeating the "I'm reading this line, let me wrap it" intent.
func (m *model) toggleWrap() {
    srcAtTop := m.sourceLineAt(m.previewTop) // which source line is at the top now
    m.previewWrap = !m.previewWrap
    m.previewHScroll = 0
    m.reflowPreview(m.previewBodyWidth())   // rebuild previewDisplay + mappings
    m.previewTop = m.visualLineFor(srcAtTop) // put that source line back at the top
    m.scrollPreview(0)                       // clamp into range
    m.tel.Record("action.preview_wrap_toggle", map[string]any{"wrap": m.previewWrap})
}
```

`sourceLineAt`/`visualLineFor` dựa trên mapping `reflowPreview` dựng (§5.2 bổ
sung): mảng `previewSrcStart []int` — `previewSrcStart[s]` = visual index dòng
visual đầu tiên của source line `s`. `visualLineFor(s) = previewSrcStart[s]`;
`sourceLineAt(v)` = binary-search source `s` lớn nhất với `previewSrcStart[s] <= v`.
Trong nowrap mode mapping là identity (1 source = 1 visual) nên cả hai trả `v`/`s`
trực tiếp.

`previewColStep` const (`model.go`, cạnh `previewLineStep` từ smooth-scroll D5):

```go
// previewColStep is the column step for fine horizontal scroll (h/l), mirror of
// previewLineStep for vertical. 1 column; H/L jump half the pane width (D6).
const previewColStep = 1
```

### 5.6 KeyMap registry entries (cross-ref `prd-keymap-and-command-palette.md`)

Thêm vào `KeyMap` struct (`keys.go`, keymap PRD §5.2):

```go
// Preview horizontal scroll + wrap (focusPreview, nowrap plain/code). h/l ≡
// GoUp/OpenEntry by key code — dispatch routes by focusPane (prd-horizontal-
// scroll-preview.md §5.5).
PreviewScrollLeft,
PreviewScrollRight,
PreviewHScrollHalfLeft,
PreviewHScrollHalfRight,
PreviewHScrollReset,
PreviewToggleWrap key.Binding
```

`defaultKeyMap` (`keys.go`):

```go
PreviewScrollLeft:       key.NewBinding(key.WithKeys("h", "left"), key.WithHelp("h", "scroll left")),
PreviewScrollRight:      key.NewBinding(key.WithKeys("l", "right"), key.WithHelp("l", "scroll right")),
PreviewHScrollHalfLeft:  key.NewBinding(key.WithKeys("H"), key.WithHelp("H", "scroll half left")),
PreviewHScrollHalfRight: key.NewBinding(key.WithKeys("L"), key.WithHelp("L", "scroll half right")),
PreviewHScrollReset:     key.NewBinding(key.WithKeys("0"), key.WithHelp("0", "scroll reset")),
PreviewToggleWrap:       key.NewBinding(key.WithKeys("w"), key.WithHelp("w", "toggle wrap")),
```

`shortHelp` (keymap PRD §5.7) cho `focusPreview` + plain/code thêm các binding
trên; `fullHelp` group *Preview* thêm chúng. Markdown/folder preview: shortHelp
KHÔNG thêm (FR12) — cần `shortHelp` đọc `m.previewPreStyled`/`m.previewIsDir`:

```go
func (m model) shortHelp() []key.Binding {
    km := m.keymap
    if m.focusPane == focusList {
        return []key.Binding{ /* …list… */ }
    }
    // focusPreview
    base := []key.Binding{km.PreviewScrollDown, km.FocusToggle, km.PreviewJumpTop, km.PreviewJumpBottom, km.PreviewHalfPageDown}
    if !m.previewPreStyled && !m.previewIsDir { // plain/code → hscroll+wrap apply
        if m.previewWrap {
            base = append(base, km.PreviewToggleWrap) // "w nowrap"
        } else {
            base = append(base, km.PreviewScrollRight, km.PreviewHScrollHalfRight, km.PreviewHScrollReset, km.PreviewToggleWrap)
        }
    }
    return append(base, km.Back, km.CommandPalette, km.FullHelp, km.Quit)
}
```

**Resolution rule (ship order) — B1 bắt buộc bất kể thứ tự:**

`h`/`left` ≡ `GoUp` và `l`/`right` ≡ `OpenEntry` về key code. Quy tắc cứng:
**horizontal scroll luôn nằm trong nhánh `else`(focusPreview) của case
`GoUp`/`OpenEntry` đang xử lý navigation — KHÔNG BAO GIỜ là một case `switch`
riêng cho `h`/`l`** (Go chỉ chạy case trúng đầu tiên → case thứ hai chết).

- **Keymap PRD ship trước (khuyến nghị, baseline hiện tại):** case `GoUp`/`OpenEntry`
  đã là `key.Matches`-based với nhánh `if focusList`. PRD này **chỉ thêm nhánh
  `else`** (§5.5) + 4 case mới độc lập (`H`/`L`/`0`/`w`) + 6 field KeyMap. Sạch.
- **PRD này (giả định) ship trước keymap PRD:** vẫn KHÔNG tạo case riêng cho
  `h`/`l`. Mở rộng nhánh `else`(focusPreview) của case string có sẵn
  (`case "enter", "l", "right":` / `case "h", "left", "backspace":`) trong
  `updateNormal`. Khi keymap PRD landed, case string → `key.Matches`, nhánh `else`
  đi theo. Không có giai đoạn nào tồn tại hai case cho cùng `h`/`l`.

### 5.7 Đã cân nhắc & defer khỏi v1

- **Vim `z`-prefix chords** (`zl`/`zh`/`zL`/`zH`/`zs`/`ze`):
  Cần pending-key state machine (`m.pendingPrefix rune` + timeout/clear logic) —
  cùng độ phức tạp multi-key mà keymap PRD đã defer. `h`/`l` free ở preview focus
  cho cùng kết quả, đơn giản hơn (D2). Nếu user **thật sự** muốn `zl/zh` literal,
  thêm chord state là một PR riêng — ghi cost: +1 model field, +clear-on-any-other-key
  logic, +2 dispatch layer. Không xứng cho v1.

- **`previewWrap` default = true** (vim default):
  D1 chọn `false` để khớp current behavior + hscroll active ngay. Reviewer flip
  được. Nếu flip: `newModel` set `previewWrap = true`; mọi plain/code preview
  wrap mặc định, user `w` để nowrap+hscroll. Trade-off: prose dễ đọc hơn nhưng
  pain "scroll ngang" cần một `w` trước.

- **`breakindent`** (wrap thụt lề theo dòng gốc — code dễ đọc khi wrap):
  v1 hard-wrap flush-left. Thêm indent-aware wrap cần parse leading whitespace
  mỗi dòng + carry sang visual line con. Defer; lipgloss có thể hỗ trợ sau.

- **`linebreak`** (wrap tại word boundary thay vì giữa từ):
  v1 hard-wrap tại col (giống `ansi.Hardwrap`). Word-aware wrap (`ansi.Wrap`)
  defer — đẹp hơn cho prose nhưng code/log không cần.

- **`$` scroll-to-end-of-longest-line**:
  `L` repeat tới được; phím riêng defer (cần `previewMaxLineWidth` đã có nhưng
  thêm phím = thêm surface).

- **`zs`/`ze`** (vim scroll cursor to screen start/end):
  Không có cursor trong preview → không map được. Defer vĩnh viễn (semantic
  không tồn tại ở read-only pane).

- **`sidescrolloff` đệm**: không cursor → không đệm. N/A.

- **Mouse horizontal pan** (shift+wheel / drag):
  bubbletea v2 có wheel + motion msg nhưng horizontal wheel hiếm + dễ flap.
  Keyboard-only v1. Defer.

- **Per-line indicator (thay global reserve D9)**:
  Per-line `›` chỉ ở dòng bị cắt làm content width nhảy giữa các dòng → code
  lệch cột. Global reserve (D9) đổi lấy alignment ổn định. Per-line defer (nếu
  user thấy reserve tốn cột ở file ít dòng dài).

- **Wrap line caching theo content-hash** (thay width+wrap key):
  D11 key `(width, wrap)` đủ — content đổi đã set `previewDisplay = nil` ở reset
  hygiene (§5.3). Content-hash overkill.

- **Markdown nowrap+hscroll**: glamour structural-wrap; re-render không-wrap mất
  layout (bảng, code fence). Defer — markdown wrap là đúng cho markdown.

- **Folder preview hscroll**: entry name `fitWidth` đã đủ; long name hiếm.
  Defer.

## 6. Acceptance criteria

### Gherkin

```gherkin
Feature: Horizontal scroll & wrap toggle for the preview pane

  Background:
    Given the explorer is open at a project root
    And focus is on the preview pane
    And the previewed file is a code or plain-text file with lines wider than the pane

  Scenario: Default mode shows a right-edge indicator on long lines
    Given the preview is in nowrap mode
    And a visible line is wider than the pane
    When the preview renders
    Then a "›" indicator appears at the right edge of that line
    And no "‹" indicator appears while the horizontal offset is zero

  Scenario: Scroll right reveals cut-off content
    Given the preview is in nowrap mode at horizontal offset zero
    When I press l several times
    Then the content pans to the right
    And a "‹" indicator appears at the left edge
    And content previously hidden on the right becomes visible

  Scenario: Scroll left returns toward the start
    Given the preview is panned to the right
    When I press h until the offset reaches zero
    Then the "‹" indicator disappears
    And the line is shown from its first column

  Scenario: Half-width jump pans by half the pane
    Given the preview is in nowrap mode at offset zero
    And the pane content width is 40 columns
    When I press L
    Then the horizontal offset becomes 20

  Scenario: Zero resets the horizontal offset
    Given the preview is panned to the right
    When I press 0
    Then the horizontal offset becomes zero
    And the line is shown from its first column

  Scenario: Toggling wrap removes horizontal scrolling
    Given the preview is in nowrap mode with long lines
    When I press w
    Then the long lines wrap onto multiple visual rows
    And no "‹" or "›" indicators appear
    And pressing l or h does nothing

  Scenario: Toggling wrap back restores horizontal scrolling
    Given the preview is in wrap mode
    When I press w
    Then the lines stop wrapping
    And long lines show a "›" indicator again
    And l/h pan horizontally again

  Scenario: Wrap preference persists across selections
    Given the preview is in wrap mode
    When I move the cursor to a different file
    Then the new file's preview is also in wrap mode
    And the horizontal offset is reset to zero

  Scenario: Short lines disable horizontal scrolling
    Given every line of the previewed file fits within the pane
    When I press l
    Then the horizontal offset stays zero
    And no indicators appear

  Scenario: Markdown preview ignores horizontal scroll and wrap toggle
    Given the previewed file is markdown
    And focus is on the preview pane
    When I press l, then w
    Then the markdown preview is unchanged
    And no horizontal indicators appear

  Scenario: Horizontal keys are no-ops when focus is on the list
    Given focus is on the list pane
    And the previewed file has long lines
    When I press l
    Then the preview horizontal offset stays zero

  Scenario: Resizing reflows wrapped lines and re-clamps the offset
    Given the preview is in wrap mode
    When the terminal width changes
    Then the wrapped lines reflow to the new content width
    When the preview is in nowrap mode panned near the right edge
    And the pane becomes wider
    Then the horizontal offset clamps so content stays visible
```

### Checklist verify

1. Mở một file code dòng dài (vd `model.go`) trong preview, focus preview
   (`Tab`) → dòng dài hiện `›` ở mép phải; chưa scroll thì không có `‹`.
2. Bấm `l` vài lần → content pan phải, `‹` xuất hiện mép trái, nội dung trước
   bị cắt nay hiện ra; màu chroma vẫn đúng phần đang xem (ANSI-aware slice).
3. Bấm `h` về 0 → `‹` biến mất, dòng hiện từ cột đầu.
4. `L` (preview content width 40) → offset thành 20; `H` → về 0.
5. `0` → offset về 0 tức thì.
6. `w` → dòng dài wrap xuống nhiều dòng visual; `‹`/`›` biến mất; `l`/`h` no-op;
   vertical scroll (`j`/`k`) cuộn qua visual lines (số dòng > số dòng logical).
7. `w` lần nữa → về nowrap; `›` hiện lại; `l`/`h` pan lại.
8. Wrap on, di chuyển sang file khác → file mới cũng wrap (D15 persist);
   `previewHScroll` về 0 (D14 reset).
9. Mở file dòng ngắn (mọi dòng ≤ width) → `l` no-op, không indicator (FR6).
10. Mở file `.md` (markdown), focus preview → `l`/`w` no-op, markdown không đổi,
    không indicator (FR7).
11. Focus list (Tab), file preview có dòng dài → `l`/`h`/`w` no-op, preview
    không đổi (FR15).
12. Resize terminal hẹp lại khi wrap on → wrapped lines re-wrap tới width mới
    (FR10); khi nowrap panned gần mép phải + pane rộng ra → offset clamp giữ
    nội dung visible.
13. Folder preview (chọn một directory) → `l`/`w` no-op (FR8).
14. Status hint khi focus preview + code nowrap → hiện `[h/l] scroll [H/L]
    half [0] reset [w] wrap`; wrap on → chỉ `[w] nowrap`; markdown/folder →
    không có hscroll/wrap hint (FR12).
15. `rg 'previewHScroll' model.go view.go` → có hit; `rg 'reflowPreview'` →
    định nghĩa + call sites (applyPreview, refreshPreview plain path, w toggle,
    resize, renderPreview guard).
16. `go build -o lazyexplorer . && go vet ./... && go test ./...` xanh.
17. `go test -race ./...` xanh (reflow trên Update goroutine; không có goroutine
    mới — race surface thấp nhưng confirm).
18. Visual verdict (`oh-my-claudecode:visual-verdict`) cho 4 frame:
    - Frame A: code nowrap offset 0 — `›` mép phải, không `‹`.
    - Frame B: code nowrap panned — `‹` mép trái + `›` mép phải, màu đúng.
    - Frame C: code wrap on — dòng wrap, không indicator, alignment đẹp.
    - Frame D: markdown focus preview — không indicator (FR7 regression guard).
19. `gitnexus_impact({target: "renderPreview", direction: "upstream"})` +
    `gitnexus_impact({target: "previewLen"})` trước sửa; `gitnexus_detect_changes`
    sau, scope khớp §8.

## 7. Task breakdown

> Trước khi sửa: `gitnexus_impact` cho `renderPreview`, `previewLen`,
> `previewScroll`, `refreshPreview`, `applyPreview`. `previewLen`/`previewScroll`
> CRITICAL (mọi đường scroll đọc). Test coverage vùng này là gate.
>
> **Shipped 2026-05-28.** T1–T9 + T12 done; tests in `hscroll_test.go`
> (reflow nowrap/wrap, hSlice, pan/clamp/reset, key dispatch h/l/H/L/0/w, wrap
> toggle reading-position preservation, edge indicators, non-scrollable no-op,
> reset-on-selection). T10 = manual render smoke (nowrap+indicators / wrap
> frames eyeballed) rather than a gated zz_dump + visual-verdict fixture. T11
> (prose cross-ref in the keymap PRD) deferred — the 6 KeyMap entries ARE in
> `keys.go`/`defaultKeyMap`, so the code is consistent.

- [x] **T1 — ANSI API verify (BLOCKER — làm TRƯỚC mọi task khác).**
  `ansi.TruncateLeft` là **linchpin** của toàn bộ nowrap+code render path
  (`hSlice`, §5.4) — chính là headline use case (pan code dòng dài, giữ màu).
  **Action đầu tiên:** `go doc github.com/charmbracelet/x/ansi | rg -i 'TruncateLeft|Hardwrap|StringWidth'`
  (hoặc `rg 'func TruncateLeft' tmp/crush` nếu clone có). Nếu `TruncateLeft`
  **thiếu**: fallback strip-ANSI-on-pan nghĩa là pan code = **mất màu hoàn
  toàn** (monochrome) — UX tệ hơn nhiều, phải **revisit D2/§5.4 trước khi
  commit** (cân nhắc: tự implement ANSI-left-cut, hoặc đổi sang chỉ-pan-plain).
  Sau khi confirm, viết `ansi_contract_test.go`: `TestAnsiHardwrap`,
  `TestAnsiTruncateLeft`, `TestAnsiStringWidth` (§5.2 D16). *(ansi_contract_test.go)*

- [x] **T2 — State + const.** Thêm `previewHScroll`, `previewWrap`,
  `previewDisplay`, `previewDisplayW`, `previewDisplayWrap`, `previewMaxLineWidth`
  lên `model`; `previewColStep` const (§5.1, §5.5). *(model.go)*

- [x] **T3 — `reflowPreview` + `wrapLine` + `lineWidth` + `hasANSI`.** Seam
  precompute (§5.2). *(model.go, fs.go)*

- [x] **T4 — Reset hygiene + reflow call sites.** `refreshPreview` reset
  `previewHScroll`/`previewDisplay` (§5.3 D14); `applyPreview` + plain path +
  WindowSizeMsg gọi `reflowPreview` (§5.3). *(model.go)*

- [x] **T5 — `previewLen` đọc display lines.** `previewLen()` (`view.go:410`)
  trả `len(m.previewDisplay)` khi set (fallback `len(m.preview)`); folder branch
  giữ nguyên (§5.1 FR11). *(view.go)*

- [x] **T6 — `renderPreview` window + indicator.** `reflowPreview` guard đầu hàm;
  wrap/markdown verbatim path; nowrap `renderHWindow` + `hSlice` + `runePrefixWidth`
  (§5.4). *(view.go)*

- [x] **T7 — hscroll clamp + handlers + toggleWrap.** `scrollPreviewH` clamp
  (§5.5); **mở rộng nhánh `else`(focusPreview) của case `GoUp`/`OpenEntry` có sẵn**
  cho `h`/`l` (B1 — KHÔNG tạo case riêng); thêm case mới `H`/`L`/`0`/`w`;
  `toggleWrap` + `sourceLineAt`/`visualLineFor` (M4 position-preserve). Telemetry
  FR13. *(model.go)*

- [x] **T8 — KeyMap entries.** 6 binding mới vào `KeyMap` + `defaultKeyMap`
  (§5.6); `shortHelp` focusPreview branch theo wrap + previewPreStyled/IsDir
  (§5.6 FR12); `fullHelp` group Preview thêm. *(keys.go, model.go)*
  > B1: `PreviewScrollLeft/Right` chỉ là entry KeyMap cho help text — dispatch
  > của chúng KHÔNG có case riêng, nằm trong nhánh `else` của `GoUp`/`OpenEntry`
  > (§5.6 resolution rule). Nếu keymap PRD chưa ship: mở rộng nhánh `else` của
  > case string `"enter","l","right"` / `"h","left","backspace"` có sẵn.

- [x] **T9 — Tests.** *(`*_test.go`)*
  - `TestReflowNowrapKeepsLogicalCount`: wrap=false → `len(previewDisplay) ==
    len(m.preview)`.
  - `TestReflowWrapExpandsLines`: wrap=true, một dòng 120-col, w=40 → 3 visual
    lines.
  - `TestReflowMarkdownVerbatim`: previewPreStyled → display == preview, maxLineWidth=0.
  - `TestReflowCacheHit`: gọi reflow 2 lần cùng (w, wrap) → không rebuild (spy/
    instrument hoặc kiểm previewDisplay pointer identity).
  - `TestHSliceANSIPreservesColor`: code line, slice [10, 30) → còn SGR đúng
    (depends T1 API).
  - `TestHSlicePlainRuneAware`: CJK line slice đúng cột.
  - `TestScrollPreviewHClamp`: pan phải quá maxHScroll → clamp; pan trái dưới 0
    → 0.
  - `TestScrollPreviewHNoopWrap`: wrap=true → scrollPreviewH no-op.
  - `TestScrollPreviewHNoopMarkdown`: previewPreStyled → no-op.
  - `TestHKeysRouteByFocus`: `l` focusPreview → hscroll++; focusList → **descend**
    (OpenEntry), KHÔNG đổi hscroll; `h` focusPreview → hscroll--; focusList →
    **ascend** (GoUp). Đóng đinh B1: một case per key, branch theo focus — `h`/`l`
    vừa navigate (focusList) vừa scroll (focusPreview), không cái nào là dead code
    (FR15, two-binding-one-key).
  - `TestWToggleWrap`: `w` → previewWrap flip + previewHScroll=0 + reflow +
    previewTop re-clamp.
  - `TestWrapTogglePreservesReadingPosition` (M4): nowrap, cursor preview ở
    previewTop = source line 30 (có vài dòng dài phía trên); `w` → wrap on; assert
    source line 30 vẫn ở đỉnh viewport (previewTop == visualLineFor(30)), KHÔNG
    nhảy về line ~30-visual. Toggle lại → về đúng source line 30.
  - `TestWheelScrollsVisualLinesInWrap`: wrap on, file có dòng wrap; wheel down 3
    → previewTop +3 **visual** lines (đi qua trong-dòng-wrap), clamp tại
    len(previewDisplay)-bodyH. (mouse-wheel path sau khi previewTop = visual index.)
  - `TestWrapPersistsHScrollResetsOnSelection`: wrap on, đổi cursor → wrap giữ,
    hscroll=0 (D14/D15).
  - `TestIndicatorsNowrap`: render nowrap panned → output chứa `‹` + `›`;
    offset 0 + dòng ngắn → không indicator (FR5/FR6).
  - `TestMarkdownIgnoresHScrollWrap`: markdown + `l`/`w` → no change (FR7).
  - `TestEdgeIndicatorReservesUniformWidth`: hai dòng (một dài một ngắn) cùng
    contentW (D9 global reserve).
  - `TestResizeReflowAndClamp`: width đổi → reflow rebuild + hscroll re-clamp
    (FR10).
  - `-race`: Update với mixed hscroll/wrap/resize messages không race.

- [x] **T10 — Visual verdict.** `zz_dump_test.go` add 4 frame (code nowrap
  offset0, code nowrap panned, code wrap, markdown focus-preview) §6.18;
  `oh-my-claudecode:visual-verdict` evaluate. *(zz_dump_test.go)*

- [x] **T11 — Sync docs cross-ref.** `prd-keymap-and-command-palette.md` §5.6
  (registry có 6 binding preview-hscroll); `prd-pane-focus.md` (h/l ở
  focusPreview giờ có handler — không còn pure no-op); `prd-smooth-preview-scroll.md`
  (vertical + horizontal cùng `previewColStep`/`previewLineStep` family). Chỉnh
  wording, không đổi code spec PRD khác. *(docs/*.md)*

- [x] **T12 — Verify.** `go build -o lazyexplorer . && go vet ./... && go test
  ./...` xanh; `go test -race ./...` xanh; visual verdict 4 frame đạt; chạy tay
  acceptance §6 (1-14); `gitnexus_detect_changes` scope khớp §8.

## 8. Files chạm tới

| File | Thay đổi |
|------|----------|
| `model.go` | + field `previewHScroll`, `previewWrap`, `previewDisplay`, `previewDisplayW`, `previewDisplayWrap`, `previewMaxLineWidth`, `previewSrcStart`; + const `previewColStep`; + `reflowPreview(w)` (dựng `previewSrcStart` mapping), `sourceLineAt`/`visualLineFor`, `scrollPreviewH(delta)`, `toggleWrap()` (M4 position-preserve); `refreshPreview` reset hygiene (`previewHScroll`/`previewDisplay`/`previewSrcStart`/`previewMaxLineWidth`); `applyPreview` + plain path + WindowSizeMsg gọi `reflowPreview`; `updateNormal`: **mở rộng nhánh `else`(focusPreview) của case `GoUp`/`OpenEntry`** cho `h`/`l` (B1, KHÔNG case riêng) + case mới `H`/`L`/`0`/`w` + telemetry |
| `view.go` | `renderPreview` reflow guard + wrap/markdown verbatim + nowrap `renderHWindow`; + `renderHWindow`, `hSlice`, `runePrefixWidth`; `previewLen` đọc `previewDisplay` |
| `fs.go` | + `wrapLine(line, w)`, `lineWidth(line)`, `hasANSI(s)` (ANSI/rune-aware helpers cạnh `fitWidth`/`ansi` usage) |
| `keys.go` | + 6 binding (`PreviewScrollLeft/Right`, `PreviewHScrollHalfLeft/Right`, `PreviewHScrollReset`, `PreviewToggleWrap`) — cross-ref keymap PRD §5.6 |
| `ansi_contract_test.go` *(mới)* | T1 API verify (`ansi.Hardwrap`/`TruncateLeft`/`StringWidth`) |
| `hscroll_test.go` *(mới)* | reflow, hSlice, clamp, key routing, wrap toggle, indicators, resize (T9) |
| `zz_dump_test.go` | + 4 frame fixture cho visual verdict (T10) |
| `docs/prd-horizontal-scroll-preview.md` | File này |
| `docs/prd-keymap-and-command-palette.md` · `docs/prd-pane-focus.md` · `docs/prd-smooth-preview-scroll.md` | Cross-ref note (T11); no code-spec change |
