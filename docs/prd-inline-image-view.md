# PRD — Inline image view (raster preview)

Status: **shipped** · Author: phiên 2026-06-04 (goal-driven) · Ngày: 2026-06-04

> Shipped trong sprint goal-driven 2026-06-04: `imageToHalfBlocks` + `renderImagePreview`
> thật (`fs.go`), TDD (`image_test.go`), gate+race xanh, visual verdict **97/PASS** (ảnh quad
> 4 màu vẽ đúng vị trí/tỉ lệ). Hoá ra **đơn giản hơn** PRD dự liệu: async + placeholder +
> stale-guard đã có sẵn cho mọi registry renderer (`syncPreview` chạy render trong `tea.Cmd`),
> nên KHÔNG cần thêm code async (D4) hay đổi render contract (T4 bỏ). Dùng **fit-to-width** thay
> vì fit-box → không cần pane height trong contract (xem D3).

---

## 1. Bối cảnh & vấn đề

Preview pane đã có một registry renderer (`previewRenderers` `fs.go:358`): markdown (glamour),
code (chroma), image. Nhưng entry image hiện là **scaffold** — `renderImagePreview`
(`fs.go:489-503`, `binary: true`) chỉ `image.DecodeConfig` để lấy `W×H` + format rồi trả về một
dòng placeholder mờ: `(image PNG — 1920×1080, 240 KB — inline preview not supported)`. Không vẽ
ảnh.

Trong workflow beside-an-agent, agent hay tạo/sửa ảnh (screenshot, sơ đồ, asset, output của một
bước generate). User liếc sang lazyexplorer để *xem* ảnh đó ngay tại pane, không phải mở app
ngoài. Hôm nay phải tab-away sang một image viewer — đúng loại friction lazyexplorer sinh ra để
xoá. User đã yêu cầu ("để làm sau này", 2026-06-04): **render ảnh thật trong preview pane.**

## 2. Goal (1 câu)

Khi cursor đứng trên một file ảnh raster (`.png/.jpg/.jpeg/.gif/.webp/.bmp`), preview pane **vẽ
ảnh đó** scale vừa pane, đọc được ngay mà không rời lazyexplorer.

**Non-goal làm rõ:**
- KHÔNG **terminal graphics protocol** (kitty / iTerm2 / sixel) ở v1 — xem D1. Một blob escape
  nhị phân không phải `[]string` ANSI nên không scroll/window/copy như mọi preview khác, và phải
  dò terminal — phá kiến trúc preview hiện tại và ethos đơn giản. Half-block ANSI phổ quát, hợp
  pipeline sẵn có.
- KHÔNG **chỉnh/zoom/pan ảnh, animation GIF, EXIF, color-pick** — read-only glance, một khung.
- KHÔNG **vector (SVG) / PDF / video** — chỉ raster Go `image` decode được.
- KHÔNG thêm **panel/mode/keybind** — ảnh là một renderer trong pane sẵn có (như markdown/code),
  không có UI mới.

## 3. Quyết định đã chốt

| # | Quyết định | Giá trị | Lý do |
|---|-----------|---------|-------|
| D1 | Kỹ thuật vẽ | **Half-block ANSI** (`▀`, fg = pixel trên, bg = pixel dưới) — mỗi cell = 1 cột × 2 hàng pixel. KHÔNG dùng kitty/iTerm2/sixel ở v1 | Half-block render ra **`[]string` ANSI** — đúng shape `m.preview` mọi renderer khác trả, nên thừa kế scroll dọc (`previewScroll`), window ngang (`renderHWindow`), stale-guard, async pipeline **không thêm code mới**. Phổ quát: chạy mọi terminal truecolor (lipgloss v2 render full truecolor sẵn), không cần dò terminal hay capability negotiation. Protocol cho ảnh nét hơn nhưng là escape nhị phân bypass text model → fight kiến trúc + thêm bậc phức tạp lớn. Defer protocol thành follow-up nếu nét trở thành nhu cầu thật |
| D2 | Decode | Go stdlib `image` (`image/png`, `image/jpeg`, `image/gif`, `golang.org/x/image/...` cho webp/bmp nếu cần) — `image.Decode` (không chỉ `DecodeConfig`) | `DecodeConfig` đã dùng cho scaffold; `Decode` là bước thật. webp/bmp cần `golang.org/x/image` (đã trong cây dep transitively? kiểm khi implement — nếu chưa, thêm 1 dep nhỏ hoặc thu hẹp format set ở v1). GIF: decode frame đầu (no animation, D non-goal) |
| D3 | Scale | **Fit-to-WIDTH** (shipped): ảnh scale về `min(innerW, imgW)` cells rộng — không upscale quá cỡ gốc (icon nhỏ giữ nét) — chiều cao px tỉ lệ theo aspect; rows = `ceil(scaledHpx/2)` (half-block = 2 px/cell). **nearest-neighbor inline** (không thêm dep). Tỉ lệ 1:2 của cell rơi ra tự nhiên từ phép tính px (mỗi cell 1px rộng × 2px cao), KHÔNG cần hằng fudge | Fit-width thay fit-box: ảnh cao hơn pane **scroll dọc** (preview sẵn có), nên renderer KHÔNG cần pane height → render contract `(path,content,width,hint)` không đổi (T4 bỏ). Nearest-neighbor đủ nét cho preview + zero dep. Đã ship: `imageToHalfBlocks` `fs.go` |
| D4 | Async | Decode + scale chạy **off Update goroutine** qua `tea.Cmd`, honor `renderGen` stale-guard — như markdown/code | Decode + scale một ảnh lớn (vài MB, vài triệu pixel) là **nặng**, chạy inline sẽ freeze keystroke/poll. `renderImagePreview` là `binary: true` đã nhận `path`; chuyển nó sang nhánh async của `syncPreview` (hiện markdown/code async, image scaffold sync vì rẻ). Scroll nhanh qua nhiều ảnh không vẽ nhầm ảnh (gen-gate) |
| D5 | Placeholder khi đang vẽ | Trong lúc decode/scale chạy: giữ **dòng metadata scaffold** (`(image PNG — W×H, size…)`) + chip "rendering" như markdown/code | Pane không bao giờ rỗng/giật; user thấy ảnh gì đang tới. Tái dùng spinner chip sẵn có |
| D6 | Fallback | Decode lỗi / format không support / ảnh 0px / pane quá nhỏ (`< vài cell`) → **dòng metadata mờ** (scaffold hôm nay), không vỡ | Degrade-không-bao-giờ-rỗng như mọi nhánh preview. Một ảnh hỏng vẫn cho biết "ảnh PNG 240KB, không vẽ được" |
| D7 | preStyled | Ảnh vẽ ra ANSI verbatim → `preStyled = true` (bỏ qua `fitWidth`); fallback line `preStyled = false` (qua `fitWidth`) | Mirror code/markdown: `applyPreview` set `previewPreStyled = msg.preStyled` (`fs.go` registry contract). Mỗi half-block line là ANSI tự-đóng (SGR mở+đóng) nên `renderHWindow` slice ngang không cắt giữa escape (FR8 của diff cũng vậy) |
| D8 | previewScrollable | Ảnh **không** horizontal-pan (`previewScrollable = false`) — ảnh đã fit width; chỉ scroll dọc nếu cao hơn pane | Ảnh fit đúng innerW nên không có cột tràn để pan; pan ngang một ảnh vô nghĩa. Cao hơn pane (ảnh dọc) thì scroll dọc bình thường |

## 4. Functional requirements

- **FR1** — Cursor trên file ảnh raster (`isImage` `fs.go:475`) + có width → preview pane render
  ảnh bằng half-block: mỗi text cell là `▀` với foreground = màu pixel hàng-trên, background =
  màu pixel hàng-dưới, tại độ phân giải đã scale.
- **FR2** — Ảnh scale-to-fit: vừa trong pane (`innerW × bodyH` cells) giữ aspect ratio gốc, hiệu
  chỉnh tỉ lệ cell (target pixel `innerW × bodyH·2`). Ảnh nhỏ hơn pane: vẽ nguyên cỡ (không
  upscale mờ) hoặc upscale tùy D3-implement; không bao giờ tràn pane.
- **FR3** — Decode + scale chạy async (`tea.Cmd`), gen-gated; scroll nhanh qua nhiều ảnh chỉ
  apply ảnh của selection hiện tại (stale drop).
- **FR4** — Trong lúc render: placeholder metadata + chip "rendering"; render xong: ảnh thay
  placeholder. Pane không rỗng tại bất kỳ thời điểm nào.
- **FR5** — Decode lỗi / format unsupported / pane quá nhỏ → dòng metadata mờ (fallback D6),
  không panic, không pane rỗng.
- **FR6** — Ảnh cao hơn pane scroll dọc được (j/k/wheel) như mọi preview; không pan ngang (D8).
- **FR7** — File ảnh đang đứng cursor được agent ghi đè (poll loop, `dirSig` đổi) → ảnh tự
  re-render bản mới — cùng cơ chế live-refresh content (consistency, không code mới).

## 5. Technical design

**Kim chỉ nam:** ảnh là **một renderer trả `[]string` ANSI**, không phải một channel render mới.
Mọi thứ pane đã làm cho code/markdown (async dispatch, gen-gate, scroll, window, placeholder,
live-refresh) áp y nguyên — chỉ thêm bước "ảnh → half-block lines".

- **`imageToHalfBlocks(img image.Image, cols, rows int) []string`** (pure, `fs.go`): scale `img`
  về `cols × rows·2` pixel (aspect-correct), rồi với mỗi cặp hàng pixel (trên/dưới) của mỗi cột,
  emit một cell `lipgloss.NewStyle().Foreground(top).Background(bottom).Render("▀")`. Hàng pixel
  lẻ cuối (ảnh cao lẻ) → bottom = terminal default (chỉ vẽ nửa trên). Pure → test bằng ảnh
  synthetic nhỏ (2×2, 1×3) assert số dòng + màu cell.
- **`renderImagePreview`** (`fs.go:489`): từ scaffold → renderer thật. Vì `binary: true` đã nhận
  `path` + (mới) `width`/`height` của pane: `image.Decode` → `imageToHalfBlocks` → `(lines, true,
  nil)`. Lỗi → fallback metadata `(lines, false, nil)` (D6). Cần pane **height** → registry render
  signature hiện `(path, content, width, hint)`; thêm height hoặc để renderer đọc từ model — quyết
  ở implement (giữ contract gọn).
- **Async dispatch** (`syncPreview` `model.go:1005`): image hiện đi nhánh sync (scaffold rẻ);
  chuyển sang nhánh async cùng markdown/code (decode/scale nặng — D4). `previewRenderedMsg` shape
  không đổi → `applyPreview` không đổi.
- **Cell aspect**: hằng `cellAspect = 2` (cell cao gấp 2 rộng) cho bước scale (FR2). Tinh chỉnh nếu
  ảnh trông bẹt/cao trên terminal thật (visual verdict).

### Đã cân nhắc & defer khỏi v1

- **Kitty / iTerm2 / sixel protocol** (ảnh nét pixel-perfect) — defer. Lý do D1: escape nhị phân
  không hợp `[]string` model, phải dò terminal. Follow-up riêng nếu half-block không đủ nét.
- **GIF animation** — defer; v1 vẽ frame đầu. Animation cần một ticker render loop = phức tạp lớn.
- **webp/bmp** — gate ở format set nếu thêm dep `golang.org/x/image` không muốn ở v1; png/jpeg/gif
  (stdlib) là core.
- **Upscale chất lượng cao** ảnh nhỏ — v1 vẽ nguyên cỡ hoặc nearest; smooth upscale defer.

## 6. Acceptance criteria

```gherkin
Feature: Inline raster image preview

  Background:
    Given lazyexplorer mở ở một thư mục có file ảnh "diagram.png"

  Scenario: Xem một ảnh vừa pane
    Given cursor đứng trên "diagram.png"
    When  preview render xong
    Then  preview pane hiện ảnh vẽ bằng half-block, scale vừa pane, đúng tỉ lệ

  Scenario: Ảnh đang được vẽ
    Given cursor vừa chuyển sang "diagram.png" (ảnh lớn)
    When  decode/scale còn chạy
    Then  pane hiện dòng metadata + chip "rendering", không rỗng

  Scenario: File không decode được
    Given cursor đứng trên một ".png" hỏng/không phải ảnh thật
    When  preview render
    Then  pane hiện dòng metadata mờ "(image … )", không vỡ, không panic

  Scenario: Scroll nhanh qua nhiều ảnh
    Given nhiều ảnh liên tiếp trong list
    When  user scroll nhanh qua chúng
    Then  pane chỉ vẽ ảnh của selection cuối, không vẽ nhầm ảnh cũ
```

Checklist verify:
1. Ảnh nhỏ synthetic (2×2 đỏ/xanh) → `imageToHalfBlocks` ra số dòng = `ceil(rows)`, màu cell đúng
   (fg = pixel trên, bg = pixel dưới).
2. Ảnh rộng và ảnh dọc đều fit pane, không tràn `innerW`, giữ aspect.
3. Decode lỗi → fallback line, `preStyled=false`, không panic.
4. Async: `renderImagePreview` chạy qua `syncPreview` gen-gate; stale ảnh bị drop (mirror
   `TestSyncPreviewDiffStaleGenDropped`).
5. Visual verdict (Mức 4): render một ảnh thật ra frame → ảnh nhận ra được, tỉ lệ đúng, không méo.
6. `go build -o lazyexplorer . && go vet ./... && go test ./...` xanh.

## 7. Task breakdown

- [x] **T1 — `imageToHalfBlocks` pure.** Scale (nearest-neighbor) + half-block `▀` emit,
  aspect-correct, odd-height last row top-only. TDD `TestImageToHalfBlocks` (2×2 exact colors +
  portrait row count). *(fs.go)*
- [x] **T2 — `renderImagePreview` thật.** Scaffold → `image.Decode` → T1 → pre-styled lines; lỗi
  → `imageFallback` metadata (D6). TDD `TestRenderImagePreviewDraws`/`…Fallback`. *(fs.go)*
- [x] **T3 — Async (đã có sẵn).** Registry renderer đã chạy trong `tea.Cmd` của `syncPreview` +
  gen-gate, placeholder "(rendering image…)" sẵn từ `refreshPreview` → KHÔNG cần code mới.
- [x] **~~T4 — Height vào contract~~ — BỎ.** Fit-to-width (D3) không cần pane height; render
  contract không đổi.
- [x] **T5 — Scroll/fallback/live-refresh.** `previewScrollable=false` (image) sẵn có; ảnh cao
  hơn pane scroll dọc qua `previewTop`/`previewLen`; poll re-render thừa kế content live-refresh.
- [x] **T6 — Verify.** `go build && go vet && go test ./...` xanh; race xanh; visual verdict
  **97/PASS** (`TestDumpImageFrame` → quad 4 màu); `TestImagePreviewThroughModel` qua model.

## 8. Files chạm tới

| File | Thay đổi |
|------|----------|
| `fs.go` | `imageToHalfBlocks` mới; `renderImagePreview` scaffold → renderer thật; (có thể) render contract +height |
| `model.go` | image vào nhánh async `syncPreview` + gen-gate + placeholder; `previewScrollable=false` cho ảnh |
| `view.go` | (nếu T4) truyền height cho renderer |
| `go.mod` | (nếu webp/bmp/scale) thêm `golang.org/x/image` |
| `docs/prd-inline-image-view.md` | PRD này |

> **Cảnh báo phức tạp (simplicity ethos):** đây là feature **lớn nhất** từng thêm — decode + scale
> + async + một thuật toán render mới. Nó vẫn nằm trong "một renderer trả `[]string`" nên KHÔNG
> thêm panel/mode/keybind (UI surface phẳng). Nhưng engineering bên dưới đáng kể; v1 cố ý thu hẹp
> (half-block, no protocol, no animation, png/jpeg/gif) để mỗi mảnh test được. Chờ review trước khi
> implement (spec-first).
