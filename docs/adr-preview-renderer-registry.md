# ADR — Renderer registry cho preview pipeline bất đồng bộ

Status: **accepted** · Author: refactor session · Ngày: 2026-05-27

---

## Bối cảnh

[[adr-async-markdown-render]] đặt nền tảng: render chạy ngoài `Update` qua `tea.Cmd`,
gen-counter bỏ kết quả cũ, style resolve một lần lúc startup. Pipeline đó lúc đầu
hard-code cho markdown; mỗi lần thêm loại file mới (code, image, …) sẽ phải đụng
vào `refreshPreview`, `syncPreview`, và `applyPreview` để thêm một nhánh riêng — state
machine song song, dễ drift.

Cùng lúc, code highlight ([[prd-code-highlight]]) và image scaffold cần onboard vào
cùng cơ chế async. Chroma (`highlightCode`) và image metadata (`renderImagePreview`)
có **cùng contract với glamour**: nhận path + bytes + width + style, trả lines +
preStyled + error. Điểm khác biệt duy nhất giữa các loại là hàm render và hàm
nhận dạng file.

Câu hỏi thiết kế: bơm thêm loại file vào pipeline bằng cách nào?

## Quyết định

| # | Quyết định | Giá trị | Lý do |
|---|-----------|---------|-------|
| D1 | Dispatch type-specific | **Renderer registry** (`[]previewRenderer`, `fs.go:238`) | Thêm loại file = một entry trong slice; machinery async (`syncPreview`/`applyPreview`) hoàn toàn type-agnostic |
| D2 | Hợp đồng renderer | **`previewRenderer` struct** với `name`, `matches`, `binary`, `render` (`fs.go:227`) | `matches` quyết định; `binary` phân luồng text/bytes; `render` trả `(lines, preStyled, err)` — preStyled là contract của renderer, không hardcode ở caller |
| D3 | Thứ tự registry | **Markdown #1, code #2, image #3** (`fs.go:238–241`) | `.md` khớp cả `isMarkdown` lẫn chroma's markdown lexer; markdown phải đứng trước code để glamour thắng |
| D4 | HTML | **Auto-covered bởi code renderer** | Chroma có lexer HTML (`lexers.Match("x.html")` → `HTML`) — không cần renderer riêng |
| D5 | Mermaid | **Plain text ở v1**, slot registry để ngỏ | Chroma không có lexer `.mmd`; real renderer (`.mmd` → image qua mermaid CLI) = một registry entry tương lai |
| D6 | Image | **Scaffold renderer** (`binary: true`) | Đọc header (`image.DecodeConfig`) cho metadata thật; terminal-graphics deferred; proves binary-renderer path |
| D7 | Lookup | **`rendererFor(name)` mỗi lần render**, không lưu pointer | Slice `previewRenderers` append-only tại init, không bao giờ mutate runtime; pointer vào slice = unsafe khi slice grow |

**Vì sao registry thắng `srcKind` switch:**

Phương án thay thế (`srcKind srcNone/srcMarkdown/srcCode` enum + switch trong
`ensurePreviewRendered`) đòi chạm vào ba chỗ để thêm loại: khai báo enum, case trong
switch, nhánh trong `refreshPreview`. Registry chỉ đòi một chỗ: thêm entry vào
`previewRenderers`. Renderer **là** dispatch — không cần biết loại ở caller.

**Hợp đồng `binary` flag:**

Text renderer (markdown, code) nhận UTF-8 đã normalize; bỏ qua khi `kind != "text"`.
Binary renderer (image) nhận raw bytes và/hoặc path; chạy bất kể `kind`. `refreshPreview`
thực thi gate này: `r.binary || kind == "text"` (`model.go:218`).

**`preStyled` là contract của renderer, không của caller:**

`applyPreview` set `m.previewPreStyled = msg.preStyled` (`model.go:306`) — giá trị
đến từ renderer, không hardcode. Image scaffold trả `preStyled=false` (dòng plain,
`fitWidth` áp); markdown và code trả `preStyled=true` (ANSI verbatim, bỏ `fitWidth`).
Caller không cần biết.

## Các phương án đã cân nhắc

- **`srcKind` enum + switch trong `ensurePreviewRendered`** (như phác thảo ban đầu ở
  [[prd-code-highlight]] §5.4) — *bác bỏ*: ba điểm chạm mỗi loại file mới; machinery
  async vẫn phải "biết" về markdown vs code để dispatch. Registry đóng gói hoàn toàn
  trong `fs.go`.

- **Interface `previewRenderer`** (Go interface, nhiều type) — *bác bỏ cho v1*: struct
  với function fields đủ linh hoạt và không cần type assertion; dễ đọc hơn khi số
  renderer nhỏ. Có thể refactor sau nếu renderer cần state riêng.

- **Dedicated HTML renderer** — *bác bỏ*: chroma's lexer bao phủ HTML đầy đủ; renderer
  riêng chỉ thêm entry trùng lặp mà không cải thiện output.

- **Mermaid renderer trong v1** — *defer*: không có chroma lexer `.mmd`; real renderer
  cần ngoại lực (mermaid CLI / wasm) ngoài scope v1. Slot registry sẵn sàng — thêm một
  entry là xong.

Tham khảo `tmp/glow/ui/pager.go` (async `tea.Cmd` pattern) và
`tmp/crush/internal/ui/common/highlight.go` (chroma truecolor + Transform) — **không copy
code**, mượn idiom.

## Hệ quả

**Tích cực**
- Thêm loại file = một `previewRenderer` entry trong `fs.go`; không đụng `model.go`.
- `syncPreview`/`applyPreview`/`previewRenderedMsg` hoàn toàn type-agnostic — test một
  lần, cover mọi renderer.
- Binary renderer path (image) được verify qua pipeline thật, không phải nhánh dead code.
- HTML preview tự động, không cần thêm code.

**Đánh đổi / giới hạn**
- Thứ tự registry quan trọng (markdown #1 trước code #2); append-only + comment giải thích
  giảm rủi ro nhầm thứ tự.
- Image scaffold hiện chỉ metadata; terminal-graphics (Kitty/Sixel) sẽ cần renderer thật —
  đổi `renderImagePreview` là đủ, không đụng pipeline.
- Style hint (`renderStyle`) truyền vào mọi renderer; code/image ignore nó — convention rõ,
  không phí bộ nhớ đáng kể.

**Hướng mở rộng**
- Mermaid: thêm `{name:"mermaid", matches:isMermaid, binary:true, render:renderMermaid}` vào
  `previewRenderers`.
- Terminal-graphics image: đổi `renderImagePreview` tại chỗ, registry không đổi.
- Renderer per-user-config: inject vào `previewRenderers` lúc init, không đụng machinery.

## Phạm vi thay đổi

| File | Thay đổi |
|------|----------|
| `fs.go` | + `previewRenderer` struct (`fs.go:227`); + `previewRenderers` slice (`fs.go:238`); + `rendererFor` (`fs.go:244`); + `renderMarkdownPreview`, `renderCodePreview`, `renderImagePreview` wrappers; + `isImage` (`fs.go:353`); + blank imports `image/png`, `image/jpeg`, `image/gif` |
| `model.go` | Rename fields: `mdSource/mdWidth/mdGen/mdPendingWidth/mdStyle` → `srcRaw/srcWidth/renderGen/pendingWidth/renderStyle`; rename msg: `markdownRenderedMsg` → `previewRenderedMsg`; rename fns: `syncMarkdown/applyMarkdown` → `syncPreview/applyPreview`; `refreshPreview` dùng `rendererFor` thay switch; `applyPreview` set `previewPreStyled = msg.preStyled` |
| `main.go` | `detectRenderStyle()` (đổi tên từ `detectMarkdownStyle`); set `m.renderStyle` |
| `view.go` | Render spinner mép phải check `m.pendingWidth > 0` (field mới) |
| `theme.go` | + `const codeHighlightStyle = "github-dark"` |

Verify: `go build -o lazyexplorer . && go vet ./... && go test ./...` xanh; `go test -race ./...`
xanh.
