# PRD — `Y`: copy the previewed file's content (whole file) + native-selection docs note

Status: **accepted** · Author: preview-copy-spec workflow · Ngày: 2026-06-02 · Shipped: 2026-06-02 (✅ `go build -o lazyexplorer . && go vet ./... && go test ./...` + `go test -race ./...` green)

> **D1/D2 narrowed by `prd-preview-selection`** (2026-06-04): in-app line selection (`V` + mouse drag)
> now exists for a line RANGE — including spans scrolled off-viewport and the 2-col preview column that
> native-drag can't isolate. `Y` (whole file) and native Shift/Option-drag (full-width / single-column
> visible spans) remain exactly as specified here; `V` covers the part native-drag never reached. See
> `prd-preview-selection` §2 for the three-tool split.

---

## 1. Bối cảnh & vấn đề

lazyexplorer sống *cạnh* một coding agent (`CLAUDE.md` §"Goal & Positioning"): user liếc preview pane,
thấy nội dung file agent vừa sửa, rồi muốn **lấy nội dung đó** — dán một đoạn vào chat của agent, hoặc
copy cả file. Pain ghi nhận: *"let me select and copy content"*.

ROOT CAUSE đã trace bằng evidence — app bật **mouse capture**: `View()` set
`v.MouseMode = tea.MouseModeCellMotion` (`view.go:381`, ngay sau `v.AltScreen = true` ở `view.go:380`).
Trong bubbletea v2, `MouseMode` là field per-`View`: `MouseModeCellMotion` phát chuỗi DEC private-mode SGR
enable, khiến terminal **report** mọi click/drag/wheel về app dưới dạng `tea.MouseClickMsg` /
`MouseReleaseMsg` / `MouseWheelMsg` / `MouseMotionMsg`. Hệ quả: **drag-to-select native của terminal bị
nuốt** trên cả preview lẫn list — chuột kéo trở thành event của app (divider-drag `model.go:1328`,
wheel-scroll `model.go:1284`), không còn highlight để Cmd+C.

App đã có sẵn **đường ray copy**: `writeClipboard(text string)` shell ra `pbcopy`/`xclip`/`wl-copy`
(`commands.go:210`), trả `errClipboardUnsupported` (`commands.go:26`) khi không có helper; helper chia sẻ
`yankRelPath` (`model.go:1510`) đã copy **đường dẫn** rel của selection lên clipboard cho phím `y`
(prd-yank-relative-path, accepted 2026-05-30). Thiếu duy nhất là copy **nội dung**.

Trọng tâm điều tra: trong các terminal phổ biến, **giữ Shift hoặc Option/Alt khi kéo có BYPASS mouse
reporting của app và rơi về native selection không?** Đã đối chiếu doc chính chủ cho kitty/wezterm
(**Shift**) và iTerm2 (**Option**) — VERIFIED ✅ (2026-06-02); Terminal.app và tmux ở mức known/community
(§5.1). Câu trả lời: **CÓ** — bypass tồn tại ở mọi terminal phổ biến (modifier do terminal host quy định,
không phải app). Điều này làm sụp không gian thiết kế: phần "chọn một đoạn nhìn thấy" **đã** được terminal
native giải quyết miễn phí (D1); phần app code chỉ còn lo cái native bypass KHÔNG làm được — **copy nguyên
file vượt quá viewport**.

## 2. Goal (1 câu)

Nhấn `Y` để copy **toàn bộ nội dung text của file đang preview** (cả file, raw text) lên clipboard, và ghi
trong docs rằng *chọn một đoạn nhìn thấy* thì giữ Shift (hoặc Option trên iTerm2/macOS) rồi kéo như selection
native của terminal — không cần thêm mode/panel nào trong app.

**Non-goal làm rõ (chặn scope creep):**

- **Không** copy theo **ký tự / sub-line** trong app (chọn nửa dòng, chọn cột) — char-select là cả một
  subsystem (highlight per-cell, anchor 2 chiều); line-granular đủ cho dán-vào-agent. *(Line-visual
  selection trong app — `V` + mouse drag, line-granular — đã được `prd-preview-selection` bổ sung.)*
- **Không** thêm phím toggle tắt mouse capture (`MouseModeNone`) — đó là một stateful mode user phải nhớ
  thoát, và trong lúc bật sẽ giết divider-drag + wheel-scroll + click-to-select; bypass native làm nó
  thừa (§5.5).
- **Không** copy một **sub-range** off-viewport bằng keystroke (vd "copy lines 800–840") — chi phí UI cho
  một line-range picker không tự trang trải; ca hiếm này dùng cả-file rồi trim trong chat (§5.5).
- **Không** copy nội dung **thư mục / image / binary** — clipboard nội dung chỉ có nghĩa với file text.
- **Không** đụng phím `y` (yank rel path) — phím mới là `Y`, lane riêng với confirm-delete `y`/`Y`.
- **Không** wiring `Y` vào **search** — trong modeSearch `Y` là ký tự query (`model.go:1805`), hijack sẽ phá
  việc tìm filename chứa `Y`; copy một search hit = Enter rồi `Y` (D4/FR8). Đây là chủ đích, không phải bỏ sót.
- **Không** copy bản render ANSI (glamour/chroma đã tô màu) — copy **raw text** của file, dán sạch vào chat.

## 3. Quyết định đã chốt

Headline (D1) là lựa chọn **whole-vs-subrange**; mọi D sau nằm dưới nó.

| # | Quyết định | Giá trị | Lý do |
|---|-----------|---------|-------|
| **D1** | **Copy CẢ FILE đang preview, KHÔNG copy một sub-range trong app. Khuyến nghị: `whole`.** | Một keystroke `Y` → toàn bộ nội dung text của file. **Không** copy-mode, không drag-select, không line-range picker | **Copy-whole là 80%-solution.** Phần "chọn đoạn nhìn thấy" đã được **terminal native** trả miễn phí (Shift/Option-drag, D2) — chính xác hơn bất kỳ in-app picker nào (sub-line, multi-column, exactly-what-you-see) và user đã quen. App keystroke chỉ nên sở hữu cái native KHÔNG với được: nội dung **đã cuộn khỏi viewport** = cả file. Một in-app sub-range mode dựng lại việc terminal làm tốt hơn, thêm cả subsystem render+state cho **giá trị biên âm** → KHÔNG out-earn surface của nó. Hai công cụ không chồng lấn: drag = đoạn-nhìn-thấy; `Y` = cả-file |
| D2 | "Chọn đoạn nhìn thấy" = **docs note**, zero app code | README + `?` full-help footnote: giữ Shift (Option trên iTerm2/macOS Terminal/tmux-macOS) rồi kéo để select native | Bypass native verified ở các terminal keystone (kitty/wezterm/iTerm2 — doc; Terminal.app/tmux — known/community; tiering ở §5.1). Native selection đọc đúng cái đang ở trên viewport alt-screen. Thêm code app để làm lại việc terminal đã làm = vi phạm ceiling đơn giản. Đây là chân kia của D1: sub-range thuộc terminal, whole-file thuộc app |
| D3 | Keybind = **`Y` (uppercase)** | `CopyContent: key.NewBinding(key.WithKeys("Y"), key.WithHelp("Y", "copy file content"))` nhóm Misc của `KeyMap` (`keys.go`) | `Y` free trong `updateNormal`; `"y"/"Y"` duy nhất là rune confirm của `updateConfirmDelete` (`model.go:1924`, dispatch dưới `case modeConfirmDelete` `model.go:1131`) — lane mode khác, vào qua `d`, không route qua keymap. prd-yank-relative-path D1 đã chứng minh `y`/`Y` coexist với confirm-delete không shadow. Mnemonic `Y`↔`y` ("yank rel path") rõ ràng. `c` đã là Changes (`keys.go:95`) nên loại |
| **D4** | **MODE gating: `Y` copy được trong modeNormal VÀ modeChanges; KHÔNG wiring trong modeSearch** | `case key.Matches(msg, km.CopyContent): m.copyContent()` trong **cả** `updateNormal` (`model.go:1528`) **và** `updateChanges` (`changes.go:248`); `updateSearch` để nguyên | `updateChanges`/`updateSearch` là **switch đóng**, KHÔNG fall-through về `updateNormal` (`updateChanges` `changes.go:248-279`, `updateSearch` `model.go:1776-1811`) — phải wiring từng mode rõ ràng. **modeChanges** là ngữ cảnh headline (§1: "file agent vừa sửa" = đúng cái changed-only view `c` liệt kê); ở đó `Y` hiện là **phím chết** (printable không-nav bị bỏ qua, `changes.go:246`) → wiring vào, vừa resolve pain vừa **gỡ một dead key** (giảm cognitive load), không thêm mode/panel/phím. **modeSearch** thì `Y` **KHÔNG** chết: `default` nối `msg.Text` vào query (`model.go:1805`), nên `Y` là **ký tự tìm kiếm** — hijack nó sẽ phá việc search filename chứa `Y`. Copy một search hit = Enter (mở/thoát search) rồi `Y`. Đây là quyết định có lý do, không phải bỏ sót |
| D5 | **`Y` tác động ở CẢ HAI focus** (focusList **và** focusPreview) | `case key.Matches(msg, km.CopyContent)` gọi `copyContent()` bất kể `m.focusPane` | KHÁC yankRelPath (focusList-only): rel path chỉ có nghĩa với một *list* selection, còn **nội dung preview là chính cái file đang chọn ở cả hai focus**. Preview luôn track `m.entries[m.cursor]` (`srcPath` set từ cursor `model.go:698,713`), nên file để copy well-defined dù focus ở list hay preview. Copy nội dung tự nhiên nhất **khi mắt đang ở preview** → no-op ở focusPreview là cái bẫy reflex; cho phép cả hai |
| D6 | Đọc nội dung từ **đĩa tại copy-time bằng full read**, **resolve path giống hệt `refreshPreview`** (`previewBaseDir()`, KHÔNG `selectedAbsPath()`), KHÔNG từ `m.srcRaw`, KHÔNG qua `readPreviewBytes` | `os.ReadFile(filepath.Join(m.previewBaseDir(), sel.name))` → guard `!isBinary(content)` (`fs.go:575`, NUL-byte heuristic, chạy trên full content) | **Invariant: copyContent đọc ĐÚNG file mà `refreshPreview` đang hiển thị.** `refreshPreview` build path bằng `base := m.previewBaseDir(); full := filepath.Join(base, sel.name)` (`model.go:670-671`). `selectedAbsPath` (`commands.go:195`) join `m.cwd` (`commands.go:204`) — chỉ **bằng** `previewBaseDir()` trong modeNormal; ở modeChanges (flat-list) entry name là **root-relative** (`model.go:664-665`) nên `previewBaseDir()` trả `m.root` (`model.go:429-433`), join `m.cwd` sẽ đọc **SAI file** đúng ngay ca headline. Dùng `previewBaseDir()` → đúng ở cả hai mode (`..`/dir đã loại bởi guard trước đó, nên không cần nhánh `..` của `selectedAbsPath`). `readPreviewBytes` **cap 256KB** (`buf := make([]byte, min64(size, maxPreviewBytes))` `fs.go:252`, `maxPreviewBytes = 256*1024` `fs.go:224`) — dùng nó sẽ **âm thầm cắt** file >256KB, phá FR6 "luôn cả file". `m.srcRaw` (`model.go:207`) KHÔNG set ở nhánh plain-text-không-renderer, là raw bytes cho image/binary (`model.go:718`) — không tin cậy. Phân loại text/binary tái dùng `isBinary` — chính bộ heuristic `readPreviewBytes` đã dùng |
| D7 | Raw text, KHÔNG ANSI, KHÔNG diff text | copy `string(os.ReadFile(...))`, không bao giờ chạm `m.preview` | `m.preview` mang verbatim glamour/chroma/diff SGR khi `previewPreStyled` true (`model.go:205`, `renderPreview` skip fitWidth `view.go:639`); diff lines preStyled=true ANSI (`git.go:372`, `diffHunks` `git.go:378`) lưu nowhere-raw. Copy chúng = dán escape-code vào chat = rác. Đọc-từ-đĩa cho diff view = copy **nội dung hiện tại thật** của file, không phải text colorized của `git diff` — đúng cả trong modeChanges nơi preview luôn là diff |
| D8 | Guard text-only | dir / `..` / image / binary → từ chối + status; file rỗng → copy hợp lệ (empty) | Clipboard nội dung chỉ có nghĩa với text. Mirror cách open-in-editor từ chối dir/`..` (`commands.go:109`). Image preview là binary renderer + metadata placeholder (không phải content) → từ chối. File rỗng là text hợp lệ → copy chuỗi rỗng (FR4). Path-copy cho non-text đã có sẵn ở `y`/palette nên KHÔNG fallback-to-path tại đây (tránh nhân đôi) |
| D9 | Helper chia sẻ + palette twin, **một** code path | `func (m *model) copyContent()` chứa guard→read→guard-kind→`tel.Record`→`writeClipboard`→status; phím `Y` (cả updateNormal lẫn updateChanges) và palette "copy file content" đều gọi nó | prd-yank-relative-path D7: split twin **double-records** telemetry. Một code path → record đúng một lần dù gọi từ mode/dispatch nào. Status success KHÔNG bắt đầu `⚠` (palette detect fail qua prefix đó: `!strings.HasPrefix(m.statusMsg, "⚠")` `palette.go:79,129`) |
| D10 | Telemetry một lần, log meta KHÔNG log content | `m.tel.Record("action.copy_content", {"name": sel.name, "bytes": len(content)})` một lần, trong `copyContent` | Đo adoption. Log tên + size; KHÔNG log nội dung — tránh rò rỉ file content vào telemetry. `Record(string, map[string]any)` (`telemetry.go:229`) |
| D11 | Native-bypass note ở full-help + README, KHÔNG chrome thường trực | một dòng footnote dưới help groups; một mục README | Ceiling đơn giản: không thêm chrome thường trực. User cần biết "giữ Shift để select" đúng một lần — chỗ tra cứu là `?` và README |

## 4. Functional requirements

- **FR1** — Cursor trên một **file text** (không `..`, không dir, không binary/image), ở **bất kỳ focus**
  (list hoặc preview): nhấn `Y` → đọc nội dung file từ đĩa, copy **raw text** (không ANSI) lên clipboard;
  status `copied <name> (<n> bytes)` (clipboard ok) hoặc `⚠ clipboard: …` (no helper).
- **FR2** — Cursor trên dir thật hoặc `..` synthetic → không copy, status `⚠ not a file`.
- **FR3** — Cursor trên file **binary/image** → không copy, status `⚠ not text`.
- **FR4** — Cursor trên file **rỗng** → copy chuỗi rỗng hợp lệ, status `copied <name> (0 bytes)`.
- **FR5** — Listing rỗng → không copy, status `⚠ nothing selected`.
- **FR6** — Nội dung copy là **raw text của file**, KHÔNG phải bản render (markdown/code đã tô màu), KHÔNG
  phải bản diff (kể cả khi đang ở diff view), KHÔNG bị cắt theo viewport/scroll — luôn **cả file**.
- **FR7** — Trong **changed-only view** (modeChanges, vào qua `c`): nhấn `Y` trên một dòng change copy
  **nội dung thật hiện tại trên đĩa** của file đó (không phải text diff đang hiển thị), đọc đúng path mà
  preview của row đó dùng (`previewBaseDir()` = root, vì name là root-relative). Đây là ca headline. Một
  row là file **đã bị xoá** (deletion cũng xuất hiện trong `c`): `os.ReadFile` fail → từ chối nhẹ nhàng
  `⚠ <err>` qua nhánh error chung (D6) — chủ đích, không phải lỗi chưa xử lý.
- **FR8** — Trong **search** (modeSearch): `Y` là **ký tự query** (gõ vào ô tìm kiếm), KHÔNG copy. Để copy
  một search hit: Enter mở/thoát search rồi `Y` ở modeNormal.
- **FR9** — `Y` trong modeConfirmDelete (vào qua `d`) vẫn DELETE — copy-content không shadow rune confirm.
- **FR10** — Palette command "copy file content" làm y hệt phím `Y` (cùng `copyContent`, record một lần).
- **FR11** — `Y` xuất hiện trong source của footer keyhint (`shortHelp`), nhóm Misc của `?` full-help, và
  dòng Keys của `--help` CLI.
- **FR12** — `?` full-help (và README) chứa một dòng note: *để chọn một đoạn nhìn thấy, giữ Shift (hoặc
  Option trên iTerm2/macOS Terminal/tmux-macOS) rồi kéo chuột — terminal sẽ select native, không qua app.*

## 5. Technical design

Kim chỉ nam: **chia đôi vấn đề theo cái native bypass làm được vs không** (D1/D2). Phần nhìn-thấy → bypass
native (docs note, zero code). Phần cả-file → một helper side-effect chia sẻ đọc-từ-đĩa, guard text-only,
copy raw — mirror `yankRelPath` line-for-line (D9).

### 5.1 Native-selection bypass — bằng chứng (nền của D1/D2/D11)

App bật alt-screen (`v.AltScreen = true` `view.go:380`) + mouse capture (`v.MouseMode =
tea.MouseModeCellMotion` `view.go:381`). Bypass chỉ với được nội dung **đang trên viewport** — đó là ranh
giới đẩy "cả file" sang D1/D3. Cơ chế bypass nằm ở **chính terminal emulator host**: khi nó nhận thấy
modifier được giữ trong lúc drag, nó **không** chuyển event thành chuỗi mouse-report cho app mà xử lý như
native selection. Đây là tính năng của terminal, không phải của app — lazyexplorer không cần code gì.

Phân loại nguồn rạch ròi (theo `docs/CLAUDE.md`): **VERIFIED ✅ (doc, 2026-06-02)** = đã đọc trang doc
chính chủ trong phiên này, có trích nguyên văn; **known/community** = hành vi nhất quán theo cộng đồng,
chưa có một trang doc chính chủ nêu thẳng modifier-override; **giả định** = suy luận, nêu cách falsify.

| Terminal | Modifier bypass | Mức | Nguồn (trích nguyên văn) |
|----------|-----------------|-----|-------|
| kitty | **Shift** | **VERIFIED ✅ (doc, 2026-06-02)** | sw.kovidgoyal.net/kitty/overview, đã fetch: *"You can select text with kitty even when a terminal program has grabbed the mouse by holding down the Shift key"* |
| wezterm | **Shift** (default) | **VERIFIED ✅ (doc, 2026-06-02)** | wezterm.org `bypass_mouse_reporting_modifiers` doc, đã fetch — default `"SHIFT"`: *"Holding down shift while clicking will not send the mouse event to eg: vim running in mouse mode and will instead treat the event as though `SHIFT` was not pressed…"* |
| iTerm2 | **Option (⌥/Alt)** | **VERIFIED ✅ (doc, 2026-06-02)** | iterm2.com/faq.html, đã fetch — mục "What modifier keys affect marking a selection…": *"Alt/Option: Mouse reporting will be disabled. If you're using vim and you can't make a selection, try holding down the alt key…"* |
| macOS Terminal.app | **Option (⌥)** | **known/community** | Apple support doc xác nhận toggle "Allow Mouse Reporting" nhưng KHÔNG document modifier-override; cộng đồng nhất quán cho Option. Không tìm được trang Apple chính chủ nêu thẳng Option-bypass (2026-06-02) |
| tmux (mouse on) | **của terminal host** — Shift-drag (kitty/wezterm) / Option-drag (iTerm2/Terminal.app); hoặc **Shift + right-click** để bypass riêng tmux | **known/community** | Bypass thực chất do terminal host làm: khi `set -g mouse on` thì giữ modifier của **host terminal** (Shift/Option) để rơi về native, hoặc Shift+right-click bypass tmux. Nhiều nguồn cộng đồng nhất quán; chưa đối chiếu một trang `man tmux` chính chủ nêu thẳng modifier (2026-06-02) |

KẾT LUẬN: universal bypass **CÓ** tồn tại — Shift là modifier phổ biến (kitty/wezterm), Option trên các
terminal macOS (iTerm2/Terminal.app); dưới tmux thì giữ modifier của terminal host. Đủ vững để D1/D2 dựa
lên: phần "chọn đoạn nhìn thấy" giải quyết bằng terminal native, zero app code.

GIẢ ĐỊNH (nêu cách falsify): modifier có thể bị user rebind (kitty `mouse_map`, wezterm
`bypass_mouse_reporting_modifiers` nhận list khác `SHIFT`); note (D11) nói "thường là Shift; Option trên
iTerm2/macOS" chứ không tuyệt đối hoá. Cách falsify từng terminal: chạy lazyexplorer trong terminal đó,
drag-with-modifier trên preview, xem có highlight native + copy được không.

### 5.2 Helper side-effect chia sẻ (`model.go`) — settled-by-construction

```go
// copyContent copies the previewed file's RAW text to the clipboard — the ONE
// code path shared by the `Y` key (in updateNormal AND updateChanges) and the
// palette twin, so telemetry records exactly once. Reads the WHOLE file from disk
// at copy time, resolving the path the SAME way refreshPreview does
// (previewBaseDir()+name, NOT selectedAbsPath which joins m.cwd) so it reads the
// exact file shown — correct in both modeNormal and the flat-list modeChanges
// (where names are root-relative). os.ReadFile (NOT 256KB-capped readPreviewBytes)
// so the result is the true, complete content regardless of render/diff/scroll.
func (m *model) copyContent() {
    if len(m.entries) == 0 { m.statusMsg = "⚠ nothing selected"; return }
    sel := m.entries[m.cursor]
    if sel.name == ".." || sel.isDir { m.statusMsg = "⚠ not a file"; return }
    full := filepath.Join(m.previewBaseDir(), sel.name) // model.go:670-671 invariant
    content, err := os.ReadFile(full)
    if err != nil { m.statusMsg = "⚠ " + err.Error(); return }
    if isBinary(content) { m.statusMsg = "⚠ not text"; return } // fs.go:575
    m.tel.Record("action.copy_content", map[string]any{"name": sel.name, "bytes": len(content)})
    if err := writeClipboard(string(content)); err != nil { m.statusMsg = "⚠ clipboard: " + err.Error(); return }
    m.statusMsg = "copied " + sel.name + " (" + strconv.Itoa(len(content)) + " bytes)"
}
```

**Path resolve = đúng cái `refreshPreview` hiển thị (invariant của D6).** `refreshPreview` build path
bằng `base := m.previewBaseDir(); full := filepath.Join(base, sel.name)` (`model.go:670-671`).
`copyContent` lặp lại **y hệt** construction đó thay vì `selectedAbsPath()` (`commands.go:195`) — vì
`selectedAbsPath` join `m.cwd` (`commands.go:204`), chỉ trùng `previewBaseDir()` ở modeNormal; ở modeChanges
(flat-list, name root-relative `model.go:664-665`) `previewBaseDir()` trả `m.root` (`model.go:429-433`) nên
join `m.cwd` đọc **sai file** đúng ca headline. Guard `..`/dir đứng trước nên không cần nhánh `..` của
`selectedAbsPath`.

**Settled-by-construction (sidestep gen-counter guard).** Async preview pipeline có một stale guard:
`applyPreview` drop khi `msg.gen != m.renderGen` (`model.go:999-1001`), bảo vệ **chỉ** `m.preview` — buffer
DISPLAY đang in-flight. Đọc-từ-đĩa khiến `copyContent` **không phụ thuộc** vào renderGen/pendingWidth/
srcWidth: không cần check render đã settled chưa, vì `os.ReadFile` luôn trả nội dung **đã settled** của
selection hiện tại bất kể có render đang chờ (`m.pendingWidth > 0`). Đây là điểm mạnh: đọc đĩa **vững hơn**
đọc bất kỳ field in-memory nào (`m.preview` styled, `m.srcRaw` không tin cậy — D6).

Full read là điểm mấu chốt: `readPreviewBytes` cap `maxPreviewBytes = 256KB` (`fs.go:224`, buf `fs.go:252`),
nên copy phải dùng `os.ReadFile` cho **cả file** (FR6). `isBinary` (`fs.go:575`) chạy trên full content. File
rỗng: `os.ReadFile` trả `[]byte{}`, `isBinary` false → copy chuỗi rỗng (FR4). Không trả `tea.Cmd` —
`writeClipboard` đồng bộ (one-shot pipe tới pbcopy), như `yankRelPath` (`model.go:1509`).

**Đánh đổi sync I/O (cross-ref `adr-async-markdown-render.md`).** ADR đó dựng async pipeline (`syncPreview`/
`tea.Cmd`) **chính xác để** đẩy việc render-file nặng ra khỏi Update goroutine, tránh freeze UI. `Y` ở đây
tái-nhập một `os.ReadFile` + clipboard pipe **đồng bộ** trên đúng goroutine đó. Lý do chấp nhận trong v1:
(a) đây là một **one-shot user action** chủ ý (không phải re-render mỗi nav/poll/resize như preview), nên
độ trễ một nhịp `Y` khác về bản chất với freeze của render-loop mà ADR chống; (b) precedent `yankRelPath`
đã chạy `writeClipboard` đồng bộ trên goroutine này và accepted — `Y` chỉ thêm một `os.ReadFile` trước nó;
(c) beside-an-agent đa số là source/text file nhỏ. Falsify: copy file vài chục MB, đo độ trễ nhịp `Y`. Nếu
đau → §5.5 (đẩy read vào `tea.Cmd`, hoặc cap size) — đổi shape sau, không phải bây giờ.

### 5.3 Keybind (`keys.go`)

Field `CopyContent key.Binding` cạnh `Yank` trong nhóm Misc; `defaultKeyMap` thêm
`CopyContent: key.NewBinding(key.WithKeys("Y"), key.WithHelp("Y", "copy file content"))`. `Y` đã xác nhận
free trong `updateNormal` (chỉ là rune confirm của `updateConfirmDelete` `model.go:1924`, lane khác).

### 5.4 Wiring (`updateNormal` + `updateChanges`) + palette twin (`commands.go`)

**modeNormal** (`model.go:1528`): `case key.Matches(msg, km.CopyContent): m.copyContent()` — **không gate theo
focusPane** (D5): preview track `m.entries[m.cursor]` nên file để copy giống nhau ở cả hai focus.

**modeChanges** (`changes.go:248`): thêm `case key.Matches(msg, km.CopyContent): m.copyContent()` vào switch
của `updateChanges` (D4) — match đúng pattern "keymap là single source" của file đó (`changes.go:250`). Switch
này đóng (không fall-through về `updateNormal`), nên không thêm case thì `Y` là phím chết. Nhờ D6 resolve qua
`previewBaseDir()`, `copyContent` đọc đúng file root-relative của row change (không phải text diff đang hiển
thị) — FR7.

**modeSearch** (`model.go:1776`): **KHÔNG** wiring (D4). Switch của `updateSearch` route printable key vào
`default` nối `msg.Text` vào query (`model.go:1805`), nên `Y` là ký tự tìm kiếm — để nguyên (FR8).

**Palette** (`commands.go`): thêm Command `"copy file content"` với
`Run: func(m *model, _ string) tea.Cmd { m.copyContent(); return nil }` — cùng code path, record một lần
(D9), cạnh "copy relative path"/"copy absolute path" (`commands.go:86`).

### 5.5 Đã cân nhắc & defer khỏi v1

- **In-app sub-range copy (line-range picker / copy-mode kiểu vim/tmux / drag-to-select)** — REJECTED (D1):
  bypass native đã làm "chọn đoạn nhìn thấy" với zero code và chính xác hơn; một mode/picker mới phá ceiling
  đơn giản cho giá trị biên âm. Off-viewport sub-range bằng keystroke (vd lines 800–840): dùng cả-file rồi
  trim trong chat, hoặc scroll + native-drag. Ca hiếm cho một glance-companion — không trang trải UI cost.
- **Phím toggle tắt mouse capture (`v.MouseMode = tea.MouseModeNone`)** — REJECTED dù *khả thi*: bubbletea v2
  diff `view.MouseMode` mỗi frame nên set có điều kiện sẽ phát chuỗi disable. Nhưng đó là **stateful mode**
  user phải nhớ thoát, và trong lúc bật giết divider-drag (`model.go:1328`) + wheel-scroll (`model.go:1284`)
  + click-to-select (`model.go:1374`). Bypass native làm nó **thừa**.
- **Đọc từ `m.srcRaw` / `m.preview` thay vì đĩa** — REJECTED (D6/D7): `srcRaw` không set ở nhánh plain-text,
  là raw bytes cho image/binary; `m.preview` là ANSI styled. Không tin cậy/không sạch cho mọi file.
- **Cap size khi copy** — DEFER: file rất lớn (vài chục MB) đọc trọn vào RAM một nhịp. Beside-an-agent đa số
  nhỏ; một guard `len(content) > N → ⚠ too large` thêm sau **không đổi shape** `copyContent`. Gắn với đánh
  đổi sync I/O ở §5.2 (`adr-async-markdown-render.md`): nếu đo thấy đau, đẩy `os.ReadFile` vào `tea.Cmd`.
- **Fallback copy-path cho non-text** — REJECTED (D8): đã có `y`/palette path-copy; fallback tại đây nhân đôi.

## 6. Acceptance criteria

```gherkin
Feature: Copy the previewed file's content

  Background:
    Given the launch directory is the jail root

  Scenario: Copy a text file's whole content
    Given the cursor is on a text file with several lines
    When I copy its content
    Then the clipboard receives the file's raw text, not the rendered preview
    And the content is the whole file, regardless of how far the preview is scrolled
    And the status line confirms the file name and byte count

  Scenario: Copy works while the preview pane is focused
    Given the preview pane is focused on a text file
    When I copy its content
    Then the clipboard receives that file's whole raw text

  Scenario: Copy ignores diff view and copies the real file
    Given the cursor is on a modified text file shown as a diff
    When I copy its content
    Then the clipboard receives the file's current content, not the diff text

  Scenario: Copy a changed file from the changed-only view
    Given the changed-only view lists the files the agent just changed
    And the cursor is on one of those changed text files
    When I copy its content
    Then the clipboard receives that file's current on-disk content, not the diff text

  Scenario: Y types into the search query, it does not copy
    Given the search box is open with a query being typed
    When I type the character "Y"
    Then the query gains a "Y" character and the results re-filter
    And nothing is copied to the clipboard

  Scenario: Refuse a directory
    Given the cursor is on a folder
    When I ask to copy its content
    Then nothing is copied
    And the status line says it is not a file

  Scenario: Refuse a binary file
    Given the cursor is on a binary file
    When I ask to copy its content
    Then nothing is copied
    And the status line says it is not text

  Scenario: An empty file copies empty content
    Given the cursor is on an empty text file
    When I copy its content
    Then the clipboard receives empty content
    And the status line reports zero bytes

  Scenario: No clipboard helper available
    Given the host has no clipboard helper
    And the cursor is on a text file
    When I copy its content
    Then nothing reaches the clipboard
    And the status line reports a clipboard error

  Scenario: Nothing selected
    Given the directory is empty
    When I ask to copy content
    Then nothing is copied
    And the status line says nothing is selected

  Scenario: The delete-confirm prompt is untouched
    Given I have asked to delete the selected file
    When I confirm with "Y"
    Then the file is deleted, not copied
```

Checklist verify:

1. `copyContent` qua `updateNormal`: file text → clipboard nhận **raw text** (byte-for-byte với
   `os.ReadFile` của file), status `copied <name> (<n> bytes)` HOẶC `⚠ clipboard` (clipboard-agnostic);
   nội dung KHÔNG chứa ANSI escape, KHÔNG là bản diff dù `diffOn`, KHÔNG bị cắt theo `previewTop`
   (`copy_content_test.go` TestCopyContent).
2. Copy chạy ở **cả hai focus**: focusList và focusPreview đều copy đúng file (không no-op ở focusPreview).
3. **MODE coverage** (D4): trong **modeChanges** (`updateChanges` `changes.go:248`), `Y` trên một row change
   copy nội dung thật trên đĩa của file đó (resolve qua `previewBaseDir()`=root, KHÔNG `m.cwd`, KHÔNG là text
   diff) — assert clipboard == on-disk content, KHÔNG `os.ReadFile(filepath.Join(m.cwd, name))`; trong
   **modeSearch** (`updateSearch` `model.go:1776`), gõ `Y` nối vào `searchQuery` và re-filter, KHÔNG copy.
4. Guard: dir/`..` → `⚠ not a file`; binary/image → `⚠ not text`; empty → `copied … (0 bytes)`; listing
   rỗng → `⚠ nothing selected`.
5. `Y` trong nhóm Misc của `fullHelp` (`view.go:1080`), `fullHelp` vẫn đúng **5 nhóm** (titles `view.go:1015`
   không đổi); `Y` trong `shortHelp` (`view.go:1053`) và dòng Keys `--help` (`helpText()` trong `main.go`).
6. `d`→confirm→`Y` vẫn DELETE (regression: copy-content không shadow confirm rune `model.go:1924`).
7. Palette "copy file content" gọi `copyContent` (cùng kết quả phím `Y`); status success KHÔNG bắt đầu `⚠`
   (palette success-detect `palette.go:79,129`); palette body liệt kê command mới (`palette_test.go`).
8. `action.copy_content` record **đúng một lần** mỗi copy (phím lẫn palette), log `name`+`bytes`, KHÔNG log
   nội dung.
9. Dogfood: drive `Y` trên một file text, assert clipboard == file content, log capability resolved
   (`zz_dogfood_test.go`).
10. `go build -o lazyexplorer . && go vet ./... && go test ./...` xanh.

## 7. Task breakdown

- [x] **T1 — Keybind.** Field `CopyContent` + `Y` binding (nhóm Misc) trong `KeyMap`/`defaultKeyMap`. §5.3. *(keys.go)*
- [x] **T2 — Shared helper + modeNormal dispatch.** `copyContent()` (guard→`os.ReadFile(filepath.Join(
  previewBaseDir(), name))`→`isBinary`-guard→telemetry-once→`writeClipboard`→status) + `case km.CopyContent`
  **bất kể focus** trong `updateNormal`. §5.2/§5.4. *(model.go)*
- [x] **T3 — modeChanges dispatch.** Thêm `case key.Matches(msg, km.CopyContent): m.copyContent()` vào switch
  của `updateChanges` (D4/FR7) — gỡ dead-key, copy được file change. §5.4. *(changes.go)*
- [x] **T4 — Palette twin.** Command "copy file content" gọi `copyContent`, cạnh các copy command sẵn có. §5.4. *(commands.go)*
- [x] **T5 — Help surfaces + native-bypass note.** `CopyContent` vào `fullHelp` Misc + `shortHelp` + dòng Keys
  `--help`; thêm dòng note Shift/Option-to-select-native (D11/FR12). §5.1. *(view.go, main.go)*
- [x] **T6 — README note.** Một mục README: phím `Y` copy cả file; giữ Shift (Option trên iTerm2/macOS) rồi
  kéo để select native một đoạn nhìn thấy. *(README.md)*
- [x] **T7 — Tests.** Unit TestCopyContent (raw/not-ANSI/not-diff/not-scrolled, both-focus, guards,
  telemetry-once); **mode coverage** — `Y` copy đúng file trong modeChanges via `previewBaseDir()`
  (`changes_test.go`) và `Y` nối query trong modeSearch không copy (`search_test.go`); help-surface +
  palette + confirm-delete-regression tests; dogfood drive `Y`. §6.
  *(copy_content_test.go, changes_test.go, search_test.go, palette_test.go, zz_dogfood_test.go, main_test.go)*
- [x] **T8 — Verify.** `go build -o lazyexplorer . && go vet ./... && go test ./...` xanh; `go test -race ./...`
  xanh; visual verdict help overlay (Misc nhóm hiện `Y copy file content` + dòng note bypass).

## 8. Files chạm tới

| File | Thay đổi |
|------|----------|
| `keys.go` | Field `CopyContent` + `Y` binding (nhóm Misc) |
| `model.go` | `copyContent()` shared helper (resolve qua `previewBaseDir()`); `case km.CopyContent` (không gate focus) trong `updateNormal` |
| `changes.go` | `case km.CopyContent: m.copyContent()` trong `updateChanges` (gỡ dead-key, copy file change) |
| `commands.go` | Palette command "copy file content" gọi `copyContent` |
| `view.go` | `CopyContent` vào `fullHelp` Misc + `shortHelp`; dòng note native-selection bypass trong help |
| `main.go` | `helpText()` (Functional Core tách khỏi `printHelp` cho testable) Keys line thêm `Y copy file content` + dòng note Shift/Option bypass |
| `README.md` | Note: `Y` copy cả file + giữ Shift/Option kéo để select native |
| `copy_content_test.go` (mới) | TestCopyContent (raw/not-ANSI/not-diff/not-scrolled, both-focus, guards, telemetry-once) |
| `changes_test.go` | `Y` trong modeChanges copy đúng file (resolve qua `previewBaseDir()`) |
| `search_test.go` | `Y` trong modeSearch nối vào `searchQuery` + re-filter, KHÔNG copy (FR8) |
| `palette_test.go` | "copy file content" command test + body name-set update |
| `zz_dogfood_test.go` | Dogfood: drive `Y`, assert clipboard == file content, log capability resolved |
| `main_test.go` (mới) | `TestHelpTextSurfaces`: `helpText()` chứa `Y copy file content` + dòng note Shift/Option (FR11/FR12 CLI surface) |
| `docs/prd-preview-copy.md` | PRD này |
