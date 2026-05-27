# PRD — Markdown View trong panel preview

> Feature: khi user chọn một file markdown trong list, panel preview bên phải
> render markdown **đẹp** (heading, list, code block, bold/italic, link…) thay vì
> dump plain text — giống component render markdown của
> [`charmbracelet/glow`](https://github.com/charmbracelet/glow).

Status: **accepted** · Author: feature-dev session · Ngày: 2026-05-27

---

## 1. Bối cảnh & vấn đề

Hiện tại `previewFile()` (`fs.go:65`) đọc file ra `[]string` các dòng raw, và
`renderPreview()` (`view.go:182`) vẽ từng dòng plain. Với một file `.md`, user
nhìn thấy `# Heading`, `**bold**`, ``` ``` ``` fences… ở dạng thô — khó đọc, đúng lúc
họ cần liếc nhanh `README.md` / spec / `CLAUDE.md` của project bên cạnh agent.

lazyexplorer sống cạnh coding agent trong một pane terminal chật → markdown là
định dạng tài liệu **phổ biến nhất** trong một repo. Render nó đẹp là cải thiện
trải nghiệm "glance" cốt lõi mà **không** phải thêm panel hay mode mới.

## 2. Goal (1 câu)

Khi user đặt cursor lên một file `.md` / `.markdown`, panel preview hiển thị
markdown đã được render ANSI đẹp (qua `glamour`), word-wrap đúng theo bề rộng panel,
scroll được như preview thường — không thêm panel, không thêm mode.

**Non-goal làm rõ:** đây KHÔNG phải markdown *editor*, KHÔNG phải full glow
(không TOC, không pager riêng, không file-picker của glow). Chỉ là *cách
render nội dung preview* cho đúng một loại file.

## 3. Quyết định đã chốt (từ phiên hỏi)

| # | Quyết định | Giá trị | Lý do |
|---|-----------|---------|-------|
| D1 | Deliverable | **PRD + task** trước; implement ở bước sau khi duyệt | house style; chốt thiết kế trước khi đụng code |
| D2 | Toggle rendered ↔ raw source | **Không** toggle, markdown luôn render | tối giản nhất, 0 keybind mới (giống glow) |
| D3 | Đuôi file coi là markdown | **`.md`** và **`.markdown`** (case-insensitive) | hai đuôi phổ biến; case-insensitive tránh sót `.MD` |
| D4 | Glamour style | **dò một lần lúc startup** (`detectRenderStyle`, `main.go`), truyền tường minh vào mỗi render | tự dò sáng/tối, không cần config; render thành hàm thuần goroutine-safe (xem [[adr-async-markdown-render]] D4) |

## 4. Functional requirements

- **FR1** — File có đuôi `.md`/`.markdown` (không phân biệt hoa thường) → preview
  render bằng glamour. Mọi file khác giữ nguyên hành vi plain-text hiện tại.
- **FR2** — Render word-wrap đúng theo **bề rộng nội dung của panel preview** hiện tại
  (`rightInner`), không phải 80 cố định.
- **FR3** — Khi user **kéo divider** đổi rộng panel, hoặc **resize terminal**, markdown
  được **render lại** theo bề rộng mới — không bị wrap sai/lệch.
- **FR4** — Scroll preview (`J/K`, `ctrl+d/u`, wheel) hoạt động trên markdown đã render
  y như với plain text (đơn vị dòng sau-wrap).
- **FR5** — Nếu glamour render lỗi (input lạ), **fallback** về plain-text preview của
  chính file đó + một hint ở status bar; KHÔNG crash, KHÔNG panel trống.
- **FR6** — Binary/empty không bao giờ đi đường markdown (giữ guard `isBinary` hiện có;
  một `.md` rỗng vẫn hiện "(empty file)" như cũ).
- **FR7** — File markdown quá lớn vẫn bị chặn bởi `maxPreviewBytes` (256KB) như mọi
  preview khác; phần đọc được sẽ render.
- **FR8** — Render chạy **ngoài** vòng `Update` nên UI không bao giờ đơ khi render file
  lớn. Trong lúc render, preview hiện ngay raw source làm placeholder và status bar hiện
  chip "• rendering…" báo bản đẹp đang tới. Khi render xong, preview đổi sang bản styled
  và chip biến mất. Điều hướng nhanh qua nhiều `.md`: kết quả render về trễ của file đã
  rời cursor bị bỏ, không bao giờ đè nội dung file đang chọn.

## 5. Technical design

> Kim chỉ nam: tái dùng pipeline preview line-based hiện có. `m.preview []string`
> vẫn là **nguồn duy nhất** mà `renderPreview()` lặp + scroll. Markdown chỉ thay đổi
> *cách `m.preview` được tạo ra* (glamour render → split `\n`) và *cách nó được vẽ*
> (bỏ qua `fitWidth`). Không đụng cơ chế scroll, layout, mouse hit-test.
>
> Render glamour chạy **ngoài** vòng `Update` (qua `tea.Cmd`), không đồng bộ, nên một
> file lớn không bao giờ chặn vòng lặp. Cơ chế async + chống kết quả cũ + style
> resolve-một-lần được ghi đầy đủ ở [[adr-async-markdown-render]]; phần dưới mô tả
> trạng thái đích.

### 5.1 Dependency

Thêm `github.com/charmbracelet/glamour` (đã verify API ở **v1.0.0**,
2026-05-27 qua pkg.go.dev). API dùng tới:

```go
r, err := glamour.NewTermRenderer(
    glamour.WithAutoStyle(),     // D4: tự dò nền sáng/tối
    glamour.WithWordWrap(width), // FR2: wrap theo bề rộng panel
)
out, err := r.Render(rawMarkdown) // out là string ANSI, đã wrap
```

Implementer chạy `go get github.com/charmbracelet/glamour@latest` rồi `go mod tidy`;
glamour kéo theo `goldmark` + chroma làm dependency mới (chấp nhận được — đây là
thư viện official charmbracelet, cùng hệ với bubbletea/lipgloss đang dùng).

### 5.2 Phát hiện markdown (`fs.go`)

Helper thuần, dễ test:

```go
func isMarkdown(name string) bool {
    switch strings.ToLower(filepath.Ext(name)) {
    case ".md", ".markdown":
        return true
    }
    return false
}
```

### 5.3 State trên `model` (`model.go`)

Đủ để (a) giữ source render lại khi đổi width, (b) cache tránh render thừa, (c) báo
`renderPreview` bỏ qua `fitWidth`, (d) điều phối render async + bỏ kết quả cũ.
Markdown là renderer #1 trong registry chung (`fs.go:238–241`, xem [[adr-preview-renderer-registry]]);
các field dưới đây phục vụ pipeline chung cho mọi renderer, không chỉ markdown:

| Field | Ý nghĩa |
|-------|---------|
| `srcPath string` | Abs path của file đang chọn có renderer khớp. `""` = selection hiện tại không có renderer. |
| `srcRaw []byte` | Content của file: text đã normalize (text renderer) hoặc raw bytes (binary renderer như image). |
| `srcWidth int` | Bề rộng mà `m.preview` styled đã render tại đó (cache key). `0` = chưa render styled. |
| `renderStyle string` | Style hint resolve một lần lúc startup (`"dark"`/`"light"`/`"notty"`); `""` → auto (fallback cho caller không phải program, vd test). |
| `previewPreStyled bool` | `true` khi `m.preview` chứa dòng ANSI verbatim từ renderer → `renderPreview` **không** gọi `fitWidth`. Giá trị đến từ renderer (field `preStyled` của `previewRenderedMsg`), không hardcode. |
| `renderGen uint64` | Đánh số mỗi lần dispatch render; kết quả chỉ apply khi gen còn khớp → bỏ render cũ về trễ. |
| `pendingWidth int` | Bề rộng của render đang bay (`0` = không có) → `syncPreview` không re-dispatch cùng việc mỗi tick; cũng là tín hiệu hiện chip "• rendering…". |

`m.preview`, `m.previewTop` giữ nguyên vai trò.

### 5.4 Timing — load tức thì, render *async theo width*

Hai ràng buộc: (a) `newModel()` → `reload()` → `refreshPreview()` chạy **khi `m.width == 0`**,
*trước khi* `WindowSizeMsg` đầu tiên tới (`main.go`, `model.go`) — render ở width 0 sẽ
hỏng; (b) glamour quá chậm để chạy trong vòng `Update` — file lớn sẽ chặn mọi message.

**Giải pháp:** tách "load nguồn (đồng bộ, tức thì)" khỏi "render (async, theo width)", và
quy mọi quyết định render về **một điểm reconcile ở đuôi `Update`**.

0. **Reset hygiene (BẮT BUỘC, đầu mỗi `refreshPreview()`, `model.go:195`).** Vì
   `refreshPreview` chạy *mỗi lần di cursor*, ngay đầu hàm default lại:
   `m.previewPreStyled = false`, `m.srcPath = ""`, `m.srcRaw = nil`, `m.srcWidth = 0`,
   `m.pendingWidth = 0`. **Chỉ** nhánh renderer set chúng thành truthy. (`pendingWidth = 0`
   huỷ "claim" của render đang bay khi selection đổi, để `syncPreview` dispatch lại cho file
   mới.) Áp cho cả nhánh file lẫn directory. Bỏ bước này → sau khi xem 1 file `.md` rồi
   sang file thường/thư mục, `previewPreStyled` còn `true` → §5.5 bỏ qua `fitWidth` cho
   plain text → dòng dài tràn panel.
1. `refreshPreview()` (`model.go:189`) khi gặp file markdown: gọi `readPreviewBytes` +
   `rendererFor(sel.name)`; khi renderer khớp và `kind == "text"`, lưu `m.srcPath` và
   `m.srcRaw` (normalized text), đặt `m.preview = plainLines(content)` (raw source làm
   **placeholder tức thì** — hiện ngay, không chặn). **Không** render glamour ở đây.
2. `syncPreview() tea.Cmd` (`model.go:257`, điểm reconcile, gọi một lần ở đuôi `Update`):
   trả `nil` khi không cần render — `srcPath == ""` / đang kéo divider (§5.6) / `w <= 0`
   (chưa biết width) / `srcWidth == w` (cache hit) / `pendingWidth == w` (render đang bay).
   Khi cần: `renderGen++`, `pendingWidth = w`, trả một `tea.Cmd` gọi
   `r.render(path, raw, w, style)` **trong goroutine riêng** rồi trả
   `previewRenderedMsg{gen, width, lines, preStyled, err}`.
3. `applyPreview(msg)` (`model.go:294`, xử lý `previewRenderedMsg` trong `Update`): nếu
   `msg.gen != m.renderGen` → **bỏ** (kết quả cũ; user đã điều hướng/resize). Khớp gen:
   `pendingWidth = 0`; nếu OK → `m.preview = msg.lines`,
   `m.previewPreStyled = msg.preStyled` (lấy từ renderer, không hardcode), `srcWidth = msg.width`,
   clamp scroll; nếu lỗi (FR5) → fallback `plainLines(srcRaw)` +
   `statusMsg = "⚠ preview render failed, showing source"`.
4. `Update` (`model.go:314`) gán model con (`updateNormal`/`handleMouse`/…) trở lại rồi gọi
   `tea.Batch(cmd, m.syncPreview())` ở đuôi → mọi message làm đổi selection/width đều quy
   về một chỗ dispatch. `WindowSizeMsg` chỉ set `m.width/height` rồi để đuôi này render
   (lần đầu khi có width, và mỗi lần resize).

`renderMarkdown` (`fs.go:402`) trim dòng rỗng cuối (`glamour.Render` thêm `\n` đuôi →
`Split` đẻ phần tử rỗng). `previewBodyWidth()` helper (`model.go:239`):
`g := m.layout(); return g.rightOuter - 2` — đúng bằng `rightInner` mà `View()` dùng,
nên wrap-width khớp vùng vẽ thật.

Gen-counter (D2 của [[adr-async-markdown-render]]) là phần bắt buộc vì ta điều hướng giữa
**nhiều** `.md`: cuộn nhanh sinh nhiều render song song, chỉ kết quả của gen mới nhất được
apply. glow không cần (một doc một thời điểm).

### 5.5 `fitWidth` bypass khi vẽ (`view.go`)  ⚠️

`fitWidth()` (`view.go:232`) cắt theo **rune** → sẽ **cắt nát escape-sequence ANSI** của
glamour. Trong `renderPreview()`:

```go
for i := top; i < len(m.preview) && i < top+bodyH; i++ {
    line := m.preview[i]
    if !m.previewPreStyled {
        line = fitWidth(line, w)   // chỉ truncate plain text
    }
    lines = append(lines, line)
}
```

Lý do an toàn khi bỏ `fitWidth` cho markdown: glamour đã word-wrap tại đúng `w`
(`WithWordWrap`), nên dòng không vượt bề rộng panel; `lipgloss.Width` (đo) xử lý ANSI
đúng, chỉ riêng **rune-slicing trong `fitWidth`** là không ANSI-aware.

### 5.6 Re-render khi kéo divider — chỉ ở release, KHÔNG ở motion (perf)

`handleMouse` gọi `setLeftFromX` mỗi `MouseActionMotion` (`model.go:369`). Render glamour
ở **mỗi pixel-step** sẽ làm thao tác kéo divider giật/lag.

**Quyết định:** `syncPreview` trả `nil` khi `m.dragging` (`model.go:262`) → trong lúc kéo,
đuôi `Update` không dispatch render nào, panel vẫn vẽ markdown ở width cũ (hơi lệch tạm
thời, chấp nhận được). Nhánh `case tea.MouseActionRelease` (`model.go:376`) chỉ set
`m.dragging = false`; đuôi `Update` chạy `syncPreview` lúc này thấy `dragging == false` +
width đổi → reflow đúng một lần. Cache theo `srcWidth` nên release mà width không đổi
sẽ no-op.

### 5.7 Error & edge cases

- glamour lỗi → FR5 fallback (đã mô tả §5.4).
- File `.md` là binary/empty → không vào nhánh markdown (đặt check markdown **sau**
  guard binary/empty của `readPreviewBytes`, hoặc set `srcPath=""` ở các nhánh đó).
- Width cực nhỏ (terminal hẹp): `minPanelCols = 16` (`view.go:39`) đảm bảo `rightInner ≥ 14`,
  glamour wrap được ở width nhỏ; không cần xử lý riêng.

### 5.8 Đã cân nhắc & **defer khỏi v1** (ghi rõ để reviewer biết không phải bỏ sót)

- **mtime invalidation:** nếu user sửa file `.md` từ ngoài trong lúc cursor đang đặt trên
  nó, preview không tự cập nhật (như mọi preview hiện tại). Out-of-scope v1.
- **Toggle raw/rendered** (D2): không làm.
- **Custom glamour style khớp accent `#7D56F4`**: dùng auto-style ở v1; custom để v2.
- **Cache nhiều file / LRU:** chỉ cache file đang chọn (theo width). Đủ cho v1.

## 6. Acceptance criteria

### Gherkin

```gherkin
Feature: Rendered markdown in the preview pane

  When the user selects a markdown file, the preview pane shows it rendered
  (headings, lists, code blocks, emphasis, links) rather than raw text; other
  files keep their plain-text preview.

  Background:
    Given the explorer is open beside a project

  Scenario: A markdown file is shown rendered
    When I select the file "README.md"
    Then the preview shows styled headings, lists, code blocks and emphasis
    And I do not see raw "#", "**" or fence markers

  Scenario: A non-markdown file stays plain text
    When I select a non-markdown file
    Then the preview shows its content as plain text, unchanged from before

  Scenario: Markdown reflows to the pane width
    Given a rendered markdown file is shown
    When I resize the terminal or drag the divider to a new width
    Then the markdown reflows to the new width
    And no word is cut mid-line and no raw ANSI escape leaks

  Scenario: Rendering failure falls back to readable source
    Given a markdown file the renderer cannot process
    When I select it
    Then the preview shows the raw source as plain text
    And a status hint reports the fallback
    And the explorer does not crash

  Scenario: An empty or binary markdown file keeps its placeholder
    When I select an empty ".md" file
    Then the preview shows the empty-file placeholder
    And when I select a ".md" file that contains NUL bytes
    Then the preview shows the binary placeholder, not rendered markdown
```

### Checklist verify

1. Đặt cursor lên `README.md`/`CLAUDE.md` → preview hiện heading, list, code block,
   bold/italic, link đã styled (không thấy `#`, `**`, fence thô).
2. File không phải markdown → preview plain-text y hệt trước (không regression).
3. Kéo divider rộng/hẹp rồi buông → markdown reflow khớp bề rộng mới, không cắt chữ giữa dòng,
   không sót mã ANSI sống.
4. Resize cửa sổ terminal → markdown reflow đúng.
5. Scroll (`J/K`, `ctrl+d/u`, wheel) trên markdown chạy mượt, không lỗi index.
6. Khởi động app trong `lazyexplorer/`, di cursor xuống `CLAUDE.md` (entries sort
   dirs-first nên file `.md` không nằm đầu list) → markdown render đúng; và case quan
   trọng: nếu cursor mặc định rơi vào markdown, sau `WindowSizeMsg` đầu tiên nó hiện
   đúng, **không** bị render ở width-0.
7. File `.md` rỗng → "(empty file)"; file `.md` "giả" chứa byte NUL → "(binary…)".
8. `go build -o lazyexplorer . && go vet ./... && go test ./...` xanh.

## 7. Task breakdown

> Đây là task breakdown **đã thực hiện** (feature implemented). Ghi lại để trace lịch sử.

- [x] **T1 — Dependency.** `go get github.com/charmbracelet/glamour@latest` → `go mod tidy`.
  *(go.mod, go.sum)*
- [x] **T2 — Detection helper.** `isMarkdown(name string) bool` (`fs.go:255`). *(fs.go)*
- [x] **T3 — Render helper.** `renderMarkdown(raw string, width int, style string) ([]string, error)`
  (`fs.go:402`): `WithStandardStyle(style)|WithAutoStyle`, `WithWordWrap(width)`, trim rỗng cuối. *(fs.go)*
- [x] **T4 — Renderer registry.** `previewRenderer` struct + `previewRenderers` slice +
  `renderMarkdownPreview` wrapper + `rendererFor` (`fs.go:227–251`); `previewRenderedMsg`
  (`model.go:30`). *(fs.go, model.go)*
- [x] **T5 — State fields.** `srcPath`, `srcRaw`, `srcWidth`, `renderStyle`, `renderGen`,
  `pendingWidth`, `previewPreStyled` (`model.go:59–74`). *(model.go)*
- [x] **T6 — Load + async render.** `refreshPreview()` (`model.go:189`) dùng `rendererFor`,
  lưu `srcPath`/`srcRaw`, đặt `plainLines` làm placeholder; `syncPreview() tea.Cmd`
  (`model.go:257`) + `applyPreview(msg)` (`model.go:294`) + `previewBodyWidth()` (`model.go:239`). *(model.go)*
- [x] **T7 — Update wiring.** `Update` xử lý `previewRenderedMsg`, `tea.Batch(cmd, m.syncPreview())`
  ở đuôi (`model.go:357`). *(model.go)*
- [x] **T8 — Drag defer.** `syncPreview` trả `nil` khi `m.dragging` (`model.go:262`); release
  chỉ set `dragging=false`, đuôi `Update` reflow (§5.6). *(model.go)*
- [x] **T9 — fitWidth bypass.** `renderPreview()` bỏ `fitWidth` khi `previewPreStyled`
  (`view.go:141`). *(view.go)*
- [x] **T10 — Error fallback.** Renderer lỗi → `plainLines(srcRaw)` + status hint trong
  `applyPreview` (`model.go:299`, FR5). *(model.go)*
- [x] **T11 — Style resolve + chip.** `detectRenderStyle()` (termenv) ở `main.go:18` set
  `m.renderStyle` một lần trước `tea.Run`; chip "• rendering…" trong `renderStatus` khi
  `m.pendingWidth > 0` (`view.go:170`) + `renderingStyle` ở `theme.go:35`. *(main.go, view.go, theme.go)*
- [x] **T12 — Verify.** `go build -o lazyexplorer . && go vet ./... && go test ./...` xanh;
  `go test -race ./...` xanh; visual verdict hai frame (đang render / đã render) đạt.

## 8. Files chạm tới

| File | Thay đổi |
|------|----------|
| `go.mod` / `go.sum` | + glamour (+ transitive: goldmark, chroma…); termenv, charmbracelet/x/ansi direct |
| `main.go` | `detectRenderStyle()` resolve style một lần, set `m.renderStyle` trước `tea.Run` |
| `fs.go` | `isMarkdown`, `renderMarkdown(raw, width, style)`, `renderMarkdownPreview` wrapper, `previewRenderer` struct, `previewRenderers` registry, `rendererFor` |
| `model.go` | fields `srcPath`/`srcRaw`/`srcWidth`/`renderStyle`/`renderGen`/`pendingWidth` + `previewRenderedMsg`; `refreshPreview` dùng registry + placeholder; `syncPreview`/`applyPreview`; `previewBodyWidth`; `Update` reconcile ở đuôi + `case previewRenderedMsg` |
| `view.go` | `renderPreview` bypass `fitWidth` khi `previewPreStyled`; chip "• rendering…" khi `pendingWidth > 0` |
| `theme.go` | `renderingStyle` (accent) cho chip |
| `*_test.go` | detection + async contract + stale-guard (gen) + drag-defer + concurrency `-race` + Update end-to-end + reset hygiene; `zz_dump_test.go` harness visual (gated) |
