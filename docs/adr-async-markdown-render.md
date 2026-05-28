# ADR — Render markdown bất đồng bộ ngoài Update goroutine

Status: **accepted** · Author: perf/UX session · Ngày: 2026-05-27

---

## Bối cảnh

Markdown được render đồng bộ ngay trong vòng `Update`: `refreshPreview` (`model.go`)
gọi thẳng glamour qua `renderMarkdown` (`fs.go`). Bubbletea xử lý **mọi** message trên
một goroutine duy nhất, nên một lần `glamour.Render` chậm chặn toàn bộ vòng lặp — không
phím nào, không tick poll nào, không một frame nào được xử lý cho tới khi render xong.

Với file `.md` lớn (tới `maxPreviewBytes = 256KB` / `maxPreviewLines = 2000`,
`fs.go`), render mất hàng trăm ms tới vài giây → app **đơ** mà không một dấu hiệu phản
hồi nào. Đúng tình huống user báo: "preview markdown tốn thời gian, nhưng thay vì feedback
tốt thì nó làm đơ app, user không hiểu vì sao."

Một yếu tố phụ làm tệ hơn: `glamour.WithAutoStyle()` dò nền terminal qua
`termenv.HasDarkBackground()` — một vòng OSC escape đọc/ghi trên stdin/stdout. Chạy giữa
lúc Bubbletea đang giữ terminal thì cũng đua với input-reader của nó.

## Quyết định

| # | Quyết định | Giá trị | Lý do |
|---|-----------|---------|-------|
| D1 | Nơi chạy render | **Ngoài Update goroutine**, qua `tea.Cmd` trả `markdownRenderedMsg` | glamour không bao giờ được chặn vòng lặp; UI luôn phản hồi |
| D2 | Chống kết quả cũ | **Generation counter** (`mdGen`); chỉ apply khi gen còn khớp | điều hướng nhanh qua nhiều `.md` sinh nhiều render; kết quả về trễ của file cũ phải bị bỏ |
| D3 | Điểm dispatch | **Một điểm reconcile ở đuôi `Update`** (`syncMarkdown`) | mọi message làm đổi selection/width/divider đều quy về một chỗ quyết định có cần render |
| D4 | Glamour style | **Resolve một lần lúc startup** (`detectMarkdownStyle`, `main.go`), truyền tường minh vào mỗi render | render thành hàm thuần, goroutine-safe; không query terminal từ goroutine |
| D5 | Feedback | **Spinner braille ở mép phải** status bar khi `mdPendingWidth > 0` + raw source làm placeholder tức thì | user thấy nội dung ngay và biết bản đẹp đang tới — không còn "đơ không rõ lý do" |

**Vì sao style phải resolve một lần (D4) là mấu chốt:** với async, nhiều goroutine render
chạy song song khi cuộn nhanh. Nếu mỗi render tự `WithAutoStyle()` → mỗi goroutine query
nền terminal đồng thời, đua với reader/writer của Bubbletea → hỏng output, có thể treo.
Một style name đã resolve (`"dark"`/`"light"`/`"notty"`) đi vào nhánh tường minh của
glamour (không I/O terminal), nên `renderMarkdown` thuần và an toàn khi gọi song song
(`TestConcurrentMarkdownRendersAreSafe`, `-race`).

## Các phương án đã cân nhắc

- **Giữ sync + debounce theo phím** — *bác bỏ*: debounce giảm số lần render khi cuộn,
  nhưng một file lớn vẫn block vòng lặp lúc render thật. Không trị gốc cái đơ.
- **Luồn `tea.Cmd` qua từng hàm mutator** (`refreshPreview`/`descend`/`ascend`/…
  cùng trả `tea.Cmd`) — *bác bỏ*: đổi signature lan vào `handleMouse` và bộ test
  `resize_test`/`previewclick_test` đang gọi trực tiếp; nhiều churn, dễ sót một path.
- **Reconcile ở đuôi `Update`** (`syncMarkdown` gọi một lần sau khi gán model con về) —
  **chọn**: các mutator giữ nguyên signature (gán `nm.(model)` lại rồi reconcile), một
  nguồn-sự-thật duy nhất cho "khi nào cần render" — cùng tinh thần "một nguồn geometry"
  của `layout()`.

`tmp/glow/ui/pager.go:410` (`renderWithGlamour` → `tea.Cmd`) xác nhận pattern async là
chuẩn của hệ charmbracelet — **không copy code**, mượn idiom. glow chỉ mở một doc tại một
thời điểm nên không cần gen-counter; ta điều hướng giữa nhiều file nên D2 là phần thêm.

## Hệ quả

**Tích cực**
- UI không bao giờ đơ vì render markdown; file lớn vẫn cuộn/gõ phím mượt.
- Render là hàm thuần, goroutine-safe, deterministic → test được không cần terminal thật.
- Bỏ một vòng query terminal mỗi lần render (nhanh hơn, hết đua stdin với Bubbletea).
- Placeholder raw + spinner mép phải cho feedback tức thì.

**Đánh đổi / giới hạn**
- Cuộn rất nhanh qua nhiều `.md` sinh vài goroutine render bị bỏ kết quả (gen mismatch) —
  phí CPU nhỏ, đổi lấy tính đúng. Có thể thêm debounce sau nếu thành vấn đề thật.
- Có một nhịp chớp ngắn raw→styled trên mỗi lần mở file (chủ đích: feedback tức thì).
- `detectMarkdownStyle` chốt sáng/tối lúc khởi động; đổi theme terminal giữa chừng không
  được nhận (chấp nhận được — hiếm, và tránh query mỗi render).

## Phạm vi thay đổi

| File | Thay đổi |
|------|----------|
| `main.go` | `detectMarkdownStyle()` (termenv) resolve style một lần trước `tea.Run`; set `m.mdStyle` |
| `model.go` | + `markdownRenderedMsg`; + field `mdStyle`/`mdGen`/`mdPendingWidth`; `refreshPreview` chỉ đặt placeholder; `syncMarkdown`/`applyMarkdown` thay `ensureMarkdownRendered`; `Update` reconcile ở đuôi + case `markdownRenderedMsg` |
| `fs.go` | `renderMarkdown(raw, width, style)` — `WithStandardStyle` khi có style, fallback `WithAutoStyle` |
| `view.go` | spinner mép phải trong `renderStatus` khi `mdPendingWidth > 0` |
| `theme.go` | + `renderingStyle` (accent) tô spinner |
| `*_test.go` | contract async, stale-guard (gen), drag-defer, concurrency `-race`, Update end-to-end |

Verify: `go build -o lazyexplorer . && go vet ./... && go test ./...` xanh; `go test -race ./...`
xanh; visual verdict hai frame (đang render / đã render) đạt.
