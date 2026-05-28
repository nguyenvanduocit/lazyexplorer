# PRD — Code Syntax Highlight trong panel preview

> Feature: khi user chọn một file **mã nguồn** (`.go`, `.py`, `.ts`, `.json`,
> `Dockerfile`…) trong list, panel preview bên phải hiển thị nội dung đã được
> **tô màu cú pháp** (keyword, string, comment, number…) thay vì dump plain text —
> dùng [`chroma`](https://github.com/alecthomas/chroma), engine highlight mà
> `glamour`/`glow`/`crush` đều dùng.

Status: **accepted** · Author: feature-dev session · Ngày: 2026-05-27 · Shipped: 2026-05-28 (✅ `go build -o lazyexplorer . && go vet ./... && go test ./...` green)

---

## 1. Bối cảnh & vấn đề

Hiện tại `previewFile()` (`fs.go:107`) đọc file ra `[]string` các dòng raw, và
`renderPreview()` (`view.go:197`) vẽ từng dòng plain. Markdown đã có nhánh render
đẹp riêng qua glamour (`docs/prd-markdown-view.md`, `model.go:188`), nhưng **mọi file
mã nguồn vẫn hiện đơn sắc** — `func`, `import`, string, comment cùng một màu xám,
khó lia mắt tìm cấu trúc đúng lúc user cần liếc nhanh code mà agent vừa sửa bên cạnh.

lazyexplorer sống cạnh coding agent trong một pane terminal: thứ user nhìn nhiều
nhất **chính là code**. Tô màu cú pháp là cải thiện trải nghiệm "glance" cốt lõi,
song song với markdown view, mà **không** thêm panel hay mode mới — chỉ thay đổi
*cách nội dung preview của một loại file được tạo ra*.

**Điểm mạnh về dependency:** `chroma/v2 v2.20.0` **đã nằm sẵn trong build** dưới
dạng indirect dep (glamour kéo vào — xem `go.mod:21`). Feature này import nó trực
tiếp, KHÔNG thêm một download mới nào; `go mod tidy` chỉ chuyển nó từ indirect →
direct.

## 2. Goal (1 câu)

Khi user đặt cursor lên một file mã nguồn mà chroma nhận diện được ngôn ngữ, panel
preview hiển thị code đã tô màu cú pháp (ANSI), dòng dài bị cắt gọn theo bề rộng
panel (`…`), scroll/resize/kéo-divider hoạt động y như preview thường — không thêm
panel, không thêm mode, không thêm keybind.

**Non-goal làm rõ:** đây KHÔNG phải code *editor*, KHÔNG có line numbers, KHÔNG
horizontal-scroll, KHÔNG folding, KHÔNG search-in-file. Chỉ là *cách render nội
dung preview* cho file code. File markdown vẫn đi đường glamour (đẹp hơn cho prose);
file không nhận diện được ngôn ngữ vẫn plain như cũ.

## 3. Quyết định đã chốt

| # | Quyết định | Giá trị | Lý do |
|---|-----------|---------|-------|
| D1 | Deliverable | **PRD + solution + task** trong một file (đồng bộ `docs/prd-markdown-view.md`) | nhà-style của repo |
| D2 | Engine | **chroma/v2** (đã trong build) | không thêm dep mới; cùng hệ charmbracelet |
| D3 | Formatter | **`terminal16m`** (truecolor) + `Transform` xoá nền per-token | theo **crush** (cùng tác giả bubbletea — nguồn tham khảo cao nhất). Truecolor đúng hex của style; Transform set `e.Background = 0` → code thừa hưởng nền panel, không "sọc nền" (§5.6). Giả định terminal hỗ trợ truecolor (mac/modern OK) |
| D4 | Phát hiện code | **`lexers.Match(filename)`** theo tên/đuôi file | tiên đoán được, rẻ; không đoán theo nội dung (§5.3) |
| D5 | Dòng code dài hơn panel | **cắt ngang + `…`** (ANSI-aware, `ansi.Truncate`) | đơn giản nhất; horizontal-scroll = thêm mode → vi phạm tôn chỉ tối giản |
| D6 | Style/theme | một chroma style **tối** (mặc định `github-dark`), đặt làm **hằng số trong `theme.go`** | dễ đổi sau; app palette vốn tối |
| D7 | Markdown vs code | **markdown ưu tiên** (check `isMarkdown` trước) | glamour đẹp hơn cho `.md`, dù chroma cũng có markdown lexer |

## 4. Functional requirements

- **FR1** — File mà `lexers.Match(name)` trả về một lexer ngôn ngữ thật (không phải
  `plaintext`/`fallback`) → preview tô màu bằng chroma. File khác giữ nguyên hành vi
  plain-text hiện tại.
- **FR2** — `.md`/`.markdown` vẫn đi nhánh glamour (markdown ưu tiên), KHÔNG đi nhánh
  chroma — không regression markdown view.
- **FR3** — Mỗi dòng code được cắt theo **bề rộng nội dung panel preview** hiện tại
  (`rightInner`) bằng cách cắt-ANSI-aware (`ansi.Truncate`, thêm `…`), không bao giờ
  để dòng dài tràn panel làm vỡ frame.
- **FR4** — Khi user **kéo divider** đổi rộng panel, hoặc **resize terminal**, code
  được **render lại** (highlight + cắt) theo bề rộng mới.
- **FR5** — Scroll preview (`J/K`, `ctrl+d/u`, wheel) hoạt động trên code đã tô màu y
  như plain text. Số dòng code giữ nguyên 1:1 với file (cắt ngang, không reflow dọc).
- **FR6** — Nếu chroma lỗi (tokenise/format fail), **fallback** về plain-text preview
  của chính file đó + hint ở status bar; KHÔNG crash, KHÔNG panel trống.
- **FR7** — Binary/empty không bao giờ đi đường code (giữ guard `isBinary`/empty của
  `previewFile`). File quá lớn vẫn bị chặn bởi `maxPreviewBytes` (256KB) /
  `maxPreviewLines` (2000) như mọi preview — phần đọc được sẽ highlight.
- **FR8** — Tab trong code được **expand thành 8 space TRƯỚC khi highlight** (tái dùng
  output đã normalize của `previewFile`), nên đo-width khớp draw-width (cùng lý do
  `previewTabWidth`, `fs.go:93`); không có tab thô lọt vào chuỗi ANSI.

## 5. Technical design

> Kim chỉ nam: **dùng chung pipeline renderer registry với markdown.** Pipeline async
> *raw-source → render-theo-width → cache theo width → re-render khi resize/drag*
> (`syncPreview`/`applyPreview`, `model.go:257`, xem [[adr-preview-renderer-registry]]).
> Code dùng **đúng pipeline đó**, chỉ khác hàm render (chroma thay glamour) và bước
> cắt-width (glamour tự wrap; chroma KHÔNG wrap nên ta cắt từng dòng bằng `ansi.Truncate`).
> `m.preview []string` vẫn là nguồn duy nhất `renderPreview` lặp + scroll.

### 5.1 Dependency

KHÔNG có download mới. `chroma/v2 v2.20.0` đã ở `go.mod:21` (indirect qua glamour).
Import trực tiếp các package:

```go
import (
    "github.com/alecthomas/chroma/v2"
    "github.com/alecthomas/chroma/v2/formatters"
    "github.com/alecthomas/chroma/v2/lexers"
    "github.com/alecthomas/chroma/v2/styles"
    "github.com/charmbracelet/x/ansi" // đã là dep (go.mod) — Truncate ANSI-aware
)
```

Sau khi import, chạy `go mod tidy`: chroma chuyển indirect → direct, `ansi` đã direct.
Không kéo thêm module mới (đã verify: `ansi.Truncate` có ở `charmbracelet/x/ansi@v0.10.2`,
chroma `terminal16m`/`lexers`/`styles`/`StyleEntry.Builder().Transform` có ở `v2.20.0` — kiểm bằng `go doc` + đọc module cache 2026-05-27).

### 5.2 Render helper — `highlightCode` (`fs.go`)

Mẫu chuẩn từ `tmp/crush/internal/ui/common/highlight.go` (đã đọc), rút gọn cho ta:

```go
// highlightCode tô màu cú pháp source theo ngôn ngữ suy ra từ name, trả về các
// dòng ANSI (mỗi dòng tự đóng SGR — xem §5.5). lexer nil/plaintext/fallback → trả
// (nil, nil) để caller đi đường plain (FR1).
func highlightCode(source, name string) (lines []string, err error) {
    l := lexers.Match(name)
    if l == nil {
        return nil, nil // không nhận diện được → không phải "code"
    }
    if cfgName := l.Config().Name; cfgName == "plaintext" || cfgName == "fallback" {
        return nil, nil // .txt/plaintext → giữ plain, không tô (no-regression)
    }
    l = chroma.Coalesce(l)

    it, err := l.Tokenise(nil, source)
    if err != nil {
        return nil, err
    }
    // crush-style (tmp/crush/internal/ui/common/highlight.go): truecolor formatter +
    // Transform xoá nền per-token để code thừa hưởng nền panel, không "sọc nền" (§5.6).
    style, err := styles.Get(codeHighlightStyle).Builder().Transform(
        func(e chroma.StyleEntry) chroma.StyleEntry { e.Background = 0; return e },
    ).Build()
    if err != nil {
        style = styles.Get(codeHighlightStyle) // fallback hiếm gặp: dùng style gốc
    }
    var buf bytes.Buffer
    f := formatters.Get("terminal16m") // truecolor; luôn != nil (formatter đã đăng ký)
    if err := f.Format(&buf, style, it); err != nil {
        return nil, err
    }
    out := strings.Split(buf.String(), "\n")
    for len(out) > 0 && strings.TrimSpace(stripANSI(out[len(out)-1])) == "" {
        out = out[:len(out)-1] // bỏ dòng rỗng đuôi (giống renderMarkdown, fs.go:199)
    }
    return out, nil
}
```

> **Lưu ý:** trim dòng-rỗng-cuối có thể chỉ cần `TrimSpace(out[last]) == ""` vì dòng
> trống của chroma không kèm SGR màu nền (Transform đã xoá bg, §5.6). Implementer xác
> nhận khi chạy; nếu dòng cuối là `\e[..m\e[0m` rỗng thì cần so trên phần text. Không
> cần helper `stripANSI` riêng nếu `TrimSpace` đủ — quyết định lúc code, không bắt buộc.

**Việc cắt theo width KHÔNG nằm ở đây** — `highlightCode` width-independent (chroma không
biết width). Cắt thực hiện ở `renderCodePreview` (`fs.go:339`, §5.4) để cache đúng theo width.

### 5.3 Phát hiện code (đã verify bằng thực nghiệm 2026-05-27)

`lexers.Match(filename)` được chạy thử trên các tên file:

| filename | `Match` trả về | đối xử của ta |
|----------|----------------|----------------|
| `main.go` | `Go` | **code** (tô màu) |
| `x.json` | `JSON` | **code** |
| `Dockerfile` | `Docker` | **code** |
| `Makefile` | `Makefile` | **code** |
| `x.md` | `markdown` | (không tới đây — nhánh `isMarkdown` chặn trước, D7) |
| `a.txt` | `plaintext` | **plain** (guard `plaintext` → bỏ tô) |
| `a.log` | `nil` | **plain** |
| `notes` (không đuôi) | `nil` | **plain** |

Vì vậy guard trong `highlightCode`: `l == nil` **hoặc** `Config().Name ∈ {plaintext, fallback}`
→ trả `nil` → caller đi plain. Đây là điều giữ `.txt/.log` y hệt trước (no-regression).
KHÔNG dùng `lexers.Analyse(content)` ở v1: tốn CPU + dễ tô nhầm file văn bản thường.

### 5.4 Pipeline async chung — renderer registry (`model.go`, `fs.go`)

Code là renderer #2 trong registry chung (xem [[adr-preview-renderer-registry]]):

```go
// fs.go:238
var previewRenderers = []previewRenderer{
    {name: "markdown", matches: isMarkdown, render: renderMarkdownPreview},
    {name: "code",     matches: isCode,     render: renderCodePreview},
    {name: "image",    matches: isImage,    binary: true, render: renderImagePreview},
}
```

`isCode(name) bool` (`fs.go:291`) = `matchLang(name) != ""`. `matchLang` (`fs.go:275`)
guard plaintext/fallback → `isCode` trả false cho `.txt`/`.log`/file không đuôi.

`renderCodePreview` (`fs.go:339`) là wrapper theo contract `previewRenderer`: nhận
`(path, content, width, style)`, gọi `highlightCode(string(content), filepath.Base(path))`,
sau đó `ansi.Truncate(line, width, "…")` mỗi dòng (chroma không wrap), trả
`(lines, preStyled=true, err)`. Style bị ignore — code dùng `codeHighlightStyle` hằng số.

**State trên `model`** (`model.go:59`): `srcPath string`, `srcRaw []byte`, `srcWidth int`,
`renderStyle string`, `renderGen uint64`, `pendingWidth int`, `previewPreStyled bool`.
Dispatch theo loại file nằm ở chính registry (`rendererFor` tra theo tên file), nên
không cần enum loại trên `model` (xem [[adr-preview-renderer-registry]] D1).

**Reset hygiene (BẮT BUỘC — đầu mỗi `refreshPreview()`, `model.go:195`).** Vì
`refreshPreview` chạy *mỗi lần di cursor*, ngay đầu hàm default lại:
`m.previewPreStyled = false`, `m.srcPath = ""`, `m.srcRaw = nil`, `m.srcWidth = 0`,
`m.pendingWidth = 0`. **Chỉ** nhánh có renderer set chúng truthy. Bỏ bước này →
sau khi xem 1 file code rồi sang file thường, `previewPreStyled` còn `true` →
`renderPreview` bỏ `fitWidth` → dòng dài tràn panel.

**Thứ tự phát hiện trong `refreshPreview`** (`model.go:189`): sau `readPreviewBytes`,
gọi `rendererFor(sel.name)`; khi khớp và `(r.binary || kind == "text")` → lưu
`srcPath`/`srcRaw`, đặt placeholder tức thì. Không gọi renderer inline. Đuôi `Update`
gọi `syncPreview()` → dispatch `r.render(...)` trong goroutine riêng, trả
`previewRenderedMsg{gen, width, lines, preStyled, err}`. `applyPreview` (`model.go:294`)
set `m.previewPreStyled = msg.preStyled` (từ renderer, không hardcode).

`matchLang(name)` (`fs.go:275`) trả tên ngôn ngữ ("" nếu không phải code) — chỉ dùng làm
cổng phát hiện trong `isCode`; `highlightCode` tự `Match` lại theo tên file lúc render
(re-Match rẻ, tránh lưu lexer vào model).

**HTML:** tự động covered bởi renderer code này — `lexers.Match("x.html")` trả lexer HTML.
Không cần renderer riêng.

**Mermaid:** `lexers.Match("x.mmd")` trả nil → `isCode` false → plain text. Renderer thật
(`.mmd` → image) là một `previewRenderers` entry tương lai.

### 5.5 Multi-line token tự đóng SGR theo từng dòng — ĐÃ VERIFY ✅ (giả định chịu lực)

Lo ngại lớn nhất khi `split("\n")` rồi cắt từng dòng: token nhiều dòng (block comment
`/* … \n … */`, raw string, triple-quote) có bị **rớt màu** từ dòng 2 trở đi không (vì
dòng 1 mở SGR, reset nằm tận dòng cuối)?

**Thực nghiệm 2026-05-27** với block comment Go, formatter `terminal16m`, style `github-dark`:

```
L1: \e[3m\e[38;2;139;148;158m/* a\e[0m
L2: \e[3m\e[38;2;139;148;158m  b */\e[0m
```

→ chroma phát **trọn bộ open+close SGR cho TỪNG dòng** (mỗi dòng tự mở `\e[3m\e[38;2;139;148;158m`
và tự đóng `\e[0m`). Nên `strings.Split(out,"\n")` + `ansi.Truncate(line,w,"…")` mỗi dòng là
**an toàn**: mỗi dòng độc lập, cắt giữa dòng vẫn được `ansi.Truncate` đóng SGR đúng (đã thấy
`block comme…\e[0m`). KHÔNG có hiện tượng rớt màu. Đây là giả định chịu lực của thiết kế và
nó đứng vững — implementer thêm test case multi-line comment (§6 #6) để khoá lại.

### 5.6 Truecolor + Transform xoá nền — không "sọc nền" — ĐÃ VERIFY ✅

Rủi ro kinh điển khi highlight ra terminal: style có `Background` (vd github-dark
`#0d1117`, catppuccin-mocha `#1e1e2e`), và một số token (error, line-highlight) tự mang
background riêng → formatter truecolor sơn nền per-token → code hiện những mảng nền màu
lệch với nền panel ("sọc nền").

Crush xử lý đúng việc này (`tmp/crush/.../common/highlight.go` + `xchroma/chroma.go`):
truecolor formatter + `style.Builder().Transform(...)` ép background mỗi token. lazyexplorer
adapt: vì panel **không** sơn nền riêng (nền = nền terminal mặc định), ta **xoá** background
thay vì ép sang một màu cụ thể — `e.Background = 0` (zero `chroma.Colour` = unset, đã verify
`IsSet() == false`).

**Thực nghiệm 2026-05-27** (`terminal16m` + Transform clear-bg, style github-dark, code Go):
- output **có** foreground truecolor `\e[38;2;R;G;B;m` (màu đúng hex), ví dụ keyword
  `\e[38;2;255;123;114m`, comment `\e[38;2;139;148;158m`;
- output **không chứa một mã `48;` (background) nào** → code thừa hưởng nền panel, không sọc;
- `Builder().Transform(...).Build()` trả `err == nil` — API hợp lệ.

→ Đây là cách crush làm, cho màu truecolor chuẩn xác trên terminal hiện đại (mac của user).
Transform là **phòng thủ**: kể cả style/token có định nghĩa nền, nó vẫn bị xoá → không bao
giờ sọc, bất kể đổi `codeHighlightStyle` sang style nào.

> **Giả định:** terminal hỗ trợ truecolor (24-bit). Hầu hết terminal hiện đại (iTerm2, kitty,
> wezterm, ghostty, VSCode, Apple Terminal mới) đều OK. Trên terminal chỉ-256-màu, escape
> truecolor có thể bị xấp xỉ/bỏ qua → màu lệch. Fallback `terminal256` để dành (§5.11).

### 5.7 `renderPreview` KHÔNG đổi (`view.go:133`)

`renderPreview` đã bỏ `fitWidth` khi `m.previewPreStyled` (`view.go:141`). Vì code cũng
trả `preStyled=true` từ `renderCodePreview` và **các dòng đã được cắt sẵn theo width**
trong `renderCodePreview` (`fs.go:347`), `renderPreview` vẽ verbatim đúng như với markdown —
**không sửa một dòng nào trong `view.go`**. Đây là phần thưởng của việc dùng chung
renderer registry.

### 5.8 Hook re-render: WindowSize + drag-release

Cả hai hook quy về đuôi `Update` → `syncPreview()` (`model.go:257`), không gọi renderer trực tiếp.

- `WindowSizeMsg` (`model.go:327`): chỉ set `m.width`/`m.height`. Đuôi `Update` gọi
  `syncPreview()` → render lần đầu (sau khi biết width) + reflow mỗi lần resize, cho
  **cả** md lẫn code.
- `MouseActionRelease` (`model.go:376`): chỉ set `m.dragging = false`. `syncPreview` trả
  `nil` khi `m.dragging` (`model.go:262`) → trong lúc kéo không dispatch gì. Khi buông,
  đuôi `Update` thấy `dragging == false` + width đổi → re-cắt code đúng một lần.
  `srcWidth` cache → buông mà width không đổi sẽ no-op.

### 5.9 Style là hằng số trong `theme.go` (`theme.go`)

```go
// codeHighlightStyle là tên chroma style cho syntax highlight (xem chroma/styles).
// Dark, vì palette app vốn tối. Đổi 1 chỗ này là đổi toàn bộ màu code preview.
const codeHighlightStyle = "github-dark"
```

Style chỉ ảnh hưởng **màu chữ** (Transform đã xoá nền per-token, §5.6). Lựa chọn khác cùng chất
"tối, hợp 256-color": `catppuccin-mocha` (khớp nền `#1e1e2e` của status bar), `monokai`,
`onedark`, `nord`, `dracula`. v1 chốt `github-dark`; đổi sau là một dòng.

### 5.10 Error & edge cases

- chroma tokenise/format lỗi → FR6 fallback raw plain + status hint (§5.4).
- `lexers.Match` nil / plaintext / fallback → plain path, không tô (§5.3).
- File code là binary/empty → không vào nhánh code (guard `previewFile` chặn trước, §5.4 switch).
- Width cực nhỏ: `minPanelCols = 16` (`view.go:39`) → `rightInner ≥ 14`; `ansi.Truncate(line,14,"…")`
  vẫn cho dòng hợp lệ; không cần xử lý riêng.
- File rất dài: `maxPreviewLines = 2000` (`fs.go:91`) cắt trước khi highlight → token hoá bị
  chặn ở phần đọc được.

### 5.11 Đã cân nhắc & **defer khỏi v1** (ghi rõ để reviewer biết không phải bỏ sót)

- **Cache 2 tầng (highlight-once → truncate-per-width):** chroma tokenise width-independent;
  chỉ bước `ansi.Truncate` mới phụ thuộc width. Lý tưởng là cache token-stream/ANSI-lines
  một lần rồi chỉ re-truncate khi đổi width. v1 re-highlight cả file mỗi lần đổi width cho
  đơn giản — chi phí sub-ms với ≤256KB (markdown path cũng trả y vậy). Tối ưu để v2.
- **`lexers.Analyse(content)` cho file không đuôi:** dễ tô nhầm; bỏ ở v1 (§5.3).
- **Horizontal scroll cho dòng dài:** thêm mode/keybind → vi phạm tối giản. Cắt `…` là đủ (D5).
- **Line numbers / git-blame gutter:** out-of-scope; lazyexplorer là "glance", không phải editor.
- **Fallback `terminal256` cho terminal không-truecolor:** v1 giả định truecolor (§5.6). Nếu
  cần chạy đẹp trên terminal 256-màu, detect color-profile (termenv) rồi đổi formatter — để v2.
- **Auto light/dark detect cho style** (như glamour `WithAutoStyle`): v1 dark-only theo palette;
  detect nền v2.
- **mtime invalidation khi file code bị sửa ngoài lúc cursor đang đặt:** như mọi preview hiện
  tại; out-of-scope v1.

## 6. Acceptance criteria

### Gherkin

```gherkin
Feature: Syntax-highlighted source code in the preview pane

  When the user selects a source file the explorer recognises, the preview pane
  shows the code with syntax coloring; markdown keeps its own glamour rendering;
  unrecognised files stay plain.

  Background:
    Given the explorer is open beside a project

  Scenario: A recognised source file is syntax-highlighted
    When I select the file "main.go"
    Then the preview shows its content with syntax coloring
    And keywords, strings, comments and numbers are visually distinct
    And the code background matches the panel background

  Scenario: Markdown keeps glamour rendering, not chroma
    When I select the file "README.md"
    Then the preview renders it as formatted markdown
    And it is not syntax-highlighted as code

  Scenario: An unrecognised file stays plain text
    When I select the file "notes.log"
    Then the preview shows its content as plain text with no coloring

  Scenario: A multi-line token keeps its color on every line
    Given a source file with a block comment spanning several lines
    When I view that file in the preview
    Then every line of the comment is colored as a comment
    And a comment line wider than the pane is truncated with a trailing ellipsis that still closes the color

  Scenario: Highlighting failure falls back to readable source
    Given a source file the highlighter cannot tokenise
    When I select it
    Then the preview shows the raw source as plain text
    And a status hint reports the fallback
    And the explorer does not crash

  Scenario: Code reflows to the pane width
    Given a highlighted source file is shown
    When I resize the terminal or drag the divider to a new width
    Then each code line is re-truncated to the new width
    And no raw ANSI escape leaks and the frame does not break
```

### Checklist verify

1. Đặt cursor lên `main.go`/`model.go` → preview hiện code đã tô màu (keyword, string,
   comment, number khác màu nhau), nền code khớp nền panel (không có mảng nền lệch màu).
2. File `.md`/`.markdown` → vẫn render glamour như trước (không bị chroma chiếm — D7/FR2).
3. File `.txt`/`.log`/file không đuôi → preview plain-text y hệt trước (no-regression).
4. File khác (binary/empty) → placeholder "(binary…)" / "(empty file)" như cũ.
5. Kéo divider rộng/hẹp rồi buông → code re-cắt khớp bề rộng mới, dòng dài có `…`, không sót
   mã ANSI sống, không vỡ frame.
6. **Multi-line token:** mở file có block comment dài nhiều dòng → mọi dòng của comment đều
   giữ màu (không rớt màu từ dòng 2); dòng comment dài bị cắt `…` vẫn đóng màu đúng.
7. Resize cửa sổ terminal → code re-cắt đúng.
8. Scroll (`J/K`, `ctrl+d/u`, wheel) trên code chạy mượt, không lỗi index; số dòng = số dòng file.
9. Khởi động app, di cursor lên 1 file code khi cursor mặc định rơi vào nó → sau `WindowSizeMsg`
   đầu tiên hiện đúng, **không** render ở width-0 (cùng cơ chế defer của markdown).
10. `go build -o lazyexplorer . && go vet ./... && go test ./...` xanh.

## 7. Task breakdown

> Đây là task breakdown **đã thực hiện** (feature implemented). Ghi lại để trace lịch sử.

- [x] **T1 — Dependency.** Import chroma packages + `x/ansi`; `go mod tidy` (chroma indirect→direct).
  *(go.mod, go.sum)*
- [x] **T2 — Style constant.** `const codeHighlightStyle = "github-dark"` (`theme.go:9`). *(theme.go)*
- [x] **T3 — Highlight helper.** `highlightCode(source, name string) ([]string, error)` (`fs.go:302`):
  guard nil/plaintext/fallback; `terminal16m` + `Builder().Transform(e.Background=0).Build()`
  (crush-style, §5.2/§5.6); trim dòng rỗng cuối. *(fs.go)*
- [x] **T4 — matchLang + isCode.** `matchLang(name) string` (`fs.go:275`), `isCode(name) bool`
  (`fs.go:291`) làm matcher cho registry entry. *(fs.go)*
- [x] **T5 — renderCodePreview wrapper.** `renderCodePreview` (`fs.go:339`): `highlightCode` +
  `ansi.Truncate` mỗi dòng theo width, trả `(lines, preStyled=true, err)`. Code entry trong
  `previewRenderers` (`fs.go:240`). *(fs.go)*
- [x] **T6 — State fields.** `srcPath`, `srcRaw`, `srcWidth`, `renderStyle`, `renderGen`,
  `pendingWidth` (`model.go:59`); `refreshPreview` dùng `rendererFor` + reset hygiene
  (`model.go:189`). *(model.go)*
- [x] **T7 — syncPreview / applyPreview.** `syncPreview() tea.Cmd` (`model.go:257`);
  `applyPreview(msg)` (`model.go:294`) set `previewPreStyled = msg.preStyled`; `Update`
  reconcile ở đuôi (`model.go:357`). *(model.go)*
- [x] **T8 — Error fallback.** chroma lỗi → `plainLines(srcRaw)` + status hint trong
  `applyPreview` (`model.go:299`, FR6). *(model.go)*
- [x] **T9 — Tests code highlight.**
  - `matchLang`/`highlightCode` detection: `main.go`→"Go", `x.json`→"JSON", `Dockerfile`→"Docker";
    `a.txt`/`a.log`/`notes`→"" (plain); `x.md` không tới nhánh code.
  - highlight: `previewPreStyled == true`, dòng vẽ verbatim (bỏ `fitWidth`); dòng dài hơn `w`
    → có `…` và đóng SGR đúng.
  - **multi-line token:** block comment nhiều dòng → mọi dòng giữ màu comment (§5.5).
  - width-0 defer; re-render khi đổi width (cache no-op qua `srcWidth`).
  - **reset hygiene:** di cursor code→`.txt`, assert `previewPreStyled == false`.
- [x] **T10 — Verify.** `go build -o lazyexplorer . && go vet ./... && go test ./...` xanh;
  chạy tay kiểm acceptance §6 (1–9).

## 8. Files chạm tới

| File | Thay đổi |
|------|----------|
| `go.mod` / `go.sum` | chroma indirect→direct; `charmbracelet/x/ansi` direct |
| `theme.go` | `+ const codeHighlightStyle = "github-dark"` (`theme.go:9`) |
| `fs.go` | `+ matchLang` (`fs.go:275`), `+ isCode` (`fs.go:291`), `+ highlightCode` (`fs.go:302`), `+ renderCodePreview` (`fs.go:339`); code entry trong `previewRenderers` (`fs.go:240`) |
| `model.go` | fields `srcPath/srcRaw/srcWidth/renderStyle/renderGen/pendingWidth` (`model.go:59`); `refreshPreview` dùng `rendererFor` (`model.go:189`); `syncPreview`/`applyPreview` (`model.go:257`, `model.go:294`); `previewBodyWidth` (`model.go:239`); `Update` reconcile ở đuôi (`model.go:357`) |
| `view.go` | không đổi — `renderPreview` đã bỏ `fitWidth` khi `previewPreStyled` (`view.go:141`) |
