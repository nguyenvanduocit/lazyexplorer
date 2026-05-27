# PRD — File listing nhất quán giữa list pane và folder preview

> Feature: khi user chọn một **folder** trong list, panel preview bên phải liệt kê
> nội dung folder đó **bằng đúng một routine** với list pane bên trái — cùng format
> hàng, cùng palette, cùng quy ước dir-suffix — thay vì hai cách vẽ lệch nhau như hiện tại.

Status: **accepted** · Author: gherkin-refine session · Ngày: 2026-05-27 · Shipped: 2026-05-27 (ultragoal G001+G002+G003)

---

## 1. Bối cảnh & vấn đề

Cùng một khái niệm "directory listing" đang được render bởi **hai routine tách rời**,
lệch nhau ở format:

- **List pane** — `renderList` (`view.go:147-179`): dir → `name + "/"` tô `dirStyle`;
  file → name tô `fileStyle`, size tô `dimStyle` (muted, D8/FR9); hàng active →
  prefix `▶ ` + `cursorActiveStyle` (mute không áp — accent bg đè mọi fg). **Không icon.**
- **Folder preview** — `previewDir` (`fs.go:150-167`): dir → `📁 name/` (emoji, plain
  text); file → `   name  <size>` (**có size**, plain text). **Không palette, không caret.**

Khi user đặt cursor lên một folder, `refreshPreview` (`model.go:182-184`) gọi
`previewDir(full)` và đổ string thô vào `m.preview`; panel phải vì vậy hiện nội dung
folder ở format **khác hẳn** list pane bên trái — khác icon (emoji vs không), khác việc
hiển thị size (preview có, list không), khác styling (plain vs lipgloss). Cùng một thứ,
hai bộ mặt → bất nhất, và mỗi lần đổi format list phải sửa hai chỗ (dễ drift).

## 2. Goal (1 câu)

Nội dung một folder trông y hệt nhau dù hiển thị ở list pane hay ở folder preview, vì cả
hai vẽ qua **một routine `renderEntryRow` duy nhất** — khác biệt duy nhất được phép là
caret đánh dấu hàng đang chọn (chỉ list pane mới có).

**Non-goal làm rõ:** KHÔNG thêm panel/mode/keybind. KHÔNG biến preview thành pane thứ hai
có cursor riêng (vi phạm "two panels là trần" trong `CLAUDE.md`). KHÔNG đổi sort order
(đã shared qua `readDir`). KHÔNG thêm cột metadata nào ngoài size (mtime, perm… out-of-scope).

## 3. Quyết định đã chốt

| # | Quyết định | Giá trị | Lý do |
|---|-----------|---------|-------|
| D1 | Deliverable | **PRD + design + task** một file | house style của repo |
| D2 | Routine chung | một `renderEntryRow(e, w, active)` ở `view.go`, dùng cho **cả** list pane lẫn folder preview | Rule 3: một nguồn render → không drift, đổi format một chỗ áp cả hai |
| D3 | File size | hiện ở **cả hai** pane — list pane **thêm** cột size (hiện chưa có) | user chọn; cùng routine thì cùng thông tin |
| D4 | Phân biệt dir/file | **style + trailing `/`**, KHÔNG emoji ở cả hai pane | khớp list pane hiện tại; emoji là chrome thừa, ngược tối giản lazygit-style |
| D5 | Đánh dấu hàng chọn | caret `▶` + `cursorActiveStyle` **chỉ** ở list pane; preview không marker | preview không có cursor riêng (Non-goal); đây là khác biệt được phép duy nhất |
| D6 | Tương tác preview | **giữ** click-to-navigate (`previewClick`, `model.go:381-423`) | tính năng đang có, read-only + click nhẹ; bỏ đi là regression |
| D7 | Nguồn dữ liệu folder preview | lưu `m.previewEntries []entry`, **render at view-time**, KHÔNG thêm cache machinery kiểu markdown | folder row chỉ style + `fitWidth` (không wrap/parse) → rẻ; cache-by-width của markdown giải bài toán glamour-wrap mà folder không có |
| D8 | Styling cột size (file, inactive) | tô `dimStyle` (muted), **không** `fileStyle` | glance UI — mắt cần landing trên `name` trước; bytes là supporting metadata, không phải headline. Hàng active **giữ** style cũ (cursor accent đè toàn fg, dim trên accent unreadable) |

## 4. Functional requirements

- **FR1** — Folder listing ở list pane và ở folder preview đi qua **cùng** `renderEntryRow`;
  output một hàng cho cùng một `entry` giống hệt nhau ở hai pane, trừ caret (D5).
- **FR2** — Dir hiển thị `name + "/"` tô `dirStyle`; file hiển thị `name` tô `fileStyle`;
  **không** emoji ở cả hai pane (D4). `..` tổng hợp ở list pane vẫn là dir, không thêm `/`
  (giữ hành vi `renderList` hiện tại, `view.go:158`).
- **FR3** — Size người-đọc-được (`humanSize`, `fs.go:214`) hiển thị cho **file** ở **cả
  hai** pane; dir không hiện size (D3).
- **FR4** — Hàng đang chọn ở list pane có caret `▶` + `cursorActiveStyle`; folder preview
  **không** vẽ marker nào (D5).
- **FR5** — Folder preview vẫn click-to-navigate: click một hàng trong preview → vào folder
  + đặt cursor lên item đó, đúng end-state như descend từ list pane (`previewClick` giữ, D6).
- **FR6** — Truncation nhất quán: khi `name` rộng hơn chỗ trống, cắt + `…` giống nhau hai
  pane (qua `fitWidth`, `view.go:251`); khi width quá hẹp, **bỏ size trước**, rồi mới cắt
  `name` (name quan trọng hơn size).
- **FR7** — Folder rỗng hiện **cùng** placeholder ở hai pane. Thứ tự liệt kê dirs-first,
  case-insensitive theo name — đã shared qua `readDir` (`fs.go:42-47`), không đổi.
- **FR8** — Khi poll loop phát hiện folder đang preview thay đổi (`syncFromDisk` →
  `refreshPreview`), `m.previewEntries` được cập nhật và preview vẽ lại nội dung mới.
- **FR9** — Cột size của một **file inactive** (cả hai pane) tô `dimStyle` (muted),
  còn `name` tô `fileStyle` (D8). Gap giữa hai cột để trống — panel background hiện
  xuyên qua. Hàng **active** ở list pane vẫn dùng `cursorActiveStyle` phủ toàn hàng;
  mute không áp ở đây để size còn đọc được trên nền accent.

## 5. Technical design

> Kim chỉ nam: **fs trả dữ liệu, view render.** Hiện folder preview vi phạm điều này —
> `previewDir` (`fs.go`, tầng filesystem) tự format string có cả layout. Ta kéo việc render
> hàng về **một chỗ duy nhất ở view layer** (`renderEntryRow`), để list pane và folder
> preview cùng gọi nó. `previewDir` rút về chỉ cung cấp `[]entry` + header (đúng vai trò
> filesystem). Đây vừa sửa bất nhất, vừa đúng *functional core / imperative shell*.

### 5.1 `renderEntryRow` — routine vẽ một hàng (`view.go`)

Nguồn sự thật duy nhất cho format một hàng listing. List pane và folder preview đều gọi:

```go
// renderEntryRow vẽ một hàng entry ở bề rộng w cột, giống hệt nhau ở mọi pane.
// active = true chỉ ở hàng cursor của list pane (caret + highlight); preview luôn false.
// Format: caret(2 cột) + name(+"/" nếu dir) tô style + size phải-căn cho file.
// Inactive file: name tô fileStyle, size tô dimStyle (muted, D8/FR9).
func renderEntryRow(e entry, w int, active bool) string {
    name := e.name
    if e.isDir && e.name != ".." {
        name += "/"
    }
    // size chỉ cho file; bỏ trước khi cắt name khi hẹp (FR6)
    size := ""
    if !e.isDir {
        size = humanSize(e.size)
    }
    if active {
        return cursorActiveStyle.Width(w).Render("▶ " + fitRow(name, size, w-2))
    }
    if e.isDir {
        return "  " + dirStyle.Render(fitRow(name, "", w-2))
    }
    // Inactive file: name + size styled riêng để mute cột size.
    return "  " + styleFileRow(name, size, w-2)
}
```

`fitRow(name, size, w)` (helper nhỏ ở `view.go`): nếu `name + gap + size` vừa `w` → name
trái, size căn phải (đệm khoảng giữa); nếu không đủ chỗ → bỏ size, `fitWidth(name, w)`.
`fitRow` trả plain string — phục vụ hàng active (một style phủ cả) và hàng dir (không có
size). `styleFileRow(name, size, w)` (cùng `view.go`) là biến thể cho **inactive file**:
cùng layout của `fitRow` nhưng tô `fileStyle` cho `name`, `dimStyle` cho `size`, gap để
trống — D8/FR9. Khi `active`, style nền phủ cả size nên size nằm trong vùng highlight
(mute không áp — sẽ unreadable trên nền accent).

> **Lưu ý styling khi active:** `cursorActiveStyle.Width(w).Render(...)` phủ nền accent
> toàn hàng (giống `view.go:166` hiện tại). Vì `renderEntryRow` tự bọc style, **không**
> tô `dirStyle/fileStyle` chồng lên hàng active — tránh ANSI lồng nhau. Đây là giả định
> chịu lực; implementer xác nhận màu hàng active đúng khi chạy.

### 5.2 List pane gọi routine chung (`view.go`)

`renderList` (`view.go:147-179`) thay vòng tự-format bằng việc gọi `renderEntryRow` mỗi
hàng visible, `active = (i == m.cursor)`. Logic scroll (`listTopFor`) + chừa caret giữ
nguyên; chỉ phần dựng chuỗi hàng được thay bằng routine chung. List pane nhờ đó **thêm
cột size** (D3/FR3) tự động.

### 5.3 Folder preview lưu entries, render at view-time (`model.go` + `fs.go`)

- **`fs.go`:** `previewDir` thôi trả `[]string`; trả `[]entry` — entries thô của
  folder (không `..` tổng hợp). Folder rỗng → slice rỗng → placeholder ở view (§5.5).
  **Lỗi đọc folder** cần kênh báo riêng (status bar / sentinel entry): slice `[]entry`
  rỗng không phân biệt được empty-vs-error → implementer chốt cách surface lúc code.
- **`model.go`:** thêm field `previewEntries []entry` (nil ⇒ preview hiện đang là **file**,
  không phải listing) và `previewIsDir bool` (phân biệt rõ nil-vs-empty: folder rỗng vẫn là
  dir preview). `refreshPreview` nhánh dir (`model.go:182-184`):

  ```go
  if sel.isDir {
      m.previewEntries, m.previewIsDir = previewDir(full), true
      return
  }
  ```

- **Reset hygiene** (đầu `refreshPreview`, cạnh reset markdown sẵn có ở `model.go:170-173`):
  thêm `m.previewEntries = nil`, `m.previewIsDir = false`. Mọi nhánh không-phải-dir để chúng
  off, hệt kỷ luật reset của markdown state.
- **`renderPreview` (`view.go:197`):** nếu `m.previewIsDir` → vẽ listing qua `renderEntryRow`:

  ```go
  if m.previewIsDir {
      top, bodyH := m.previewScroll()
      if len(m.previewEntries) == 0 {
          return dimStyle.Render("(empty folder)") // FR7, khớp list pane "(empty directory)" tinh thần
      }
      var lines []string
      for i := top; i < len(m.previewEntries) && i < top+bodyH; i++ {
          lines = append(lines, renderEntryRow(m.previewEntries[i], w, false)) // active luôn false (D5)
      }
      return strings.Join(lines, "\n")
  }
  // … nhánh file/markdown giữ nguyên (m.preview lines, previewPreStyled, fitWidth) …
  ```

  `previewPreStyled` **không liên quan** nhánh folder (nó là cờ "skip `fitWidth` cho lines
  ANSI" của markdown — folder đi đường render riêng).

### 5.4 Scroll math dùng độ dài thống nhất (`view.go` + `model.go`)

`previewScroll` (`view.go:187`) và `scrollPreview` (`model.go:370`) đang đo `len(m.preview)`.
Folder preview giờ không đổ vào `m.preview`, nên thêm helper:

```go
func (m model) previewLen() int {
    if m.previewIsDir {
        return len(m.previewEntries)
    }
    return len(m.preview)
}
```

Thay `len(m.preview)` bằng `m.previewLen()` trong cả hai chỗ trên. Scroll folder listing
nhờ đó hoạt động đúng như scroll file preview.

### 5.5 `previewClick` đọc từ entries đã lưu (`model.go:381-423`)

`previewClick` hiện `readDir` lại folder mỗi click để map row→item. Sau khi đã lưu
`m.previewEntries`, dùng thẳng nó — bỏ `readDir` (tiết kiệm I/O, một nguồn dữ liệu):

```go
lineIdx := top + row
if lineIdx >= len(m.previewEntries) {
    return
}
clicked := m.previewEntries[lineIdx].name
```

Row mapping vẫn 1:1, in-order với listing đã vẽ (FR5) vì cùng `m.previewEntries`.

### 5.6 Đã cân nhắc & defer khỏi v1

- **Cache folder rows theo width (kiểu `ensureMarkdownRendered`):** thừa. Folder row chỉ
  style + `fitWidth`, không wrap/parse → render mỗi frame rẻ. Cache chỉ thêm máy móc cho
  thứ vốn rẻ (D7). Không làm.
- **Size căn cột cho dir (vd số item):** chỉ file có size; dir để trống cho gọn. Hiển thị
  "N items" cho dir trong hàng → thêm chrome, defer.
- **Icon theo loại file (ext-based):** ngược D4 (phân biệt bằng style, không icon). Defer.
- **Caret/selection trong preview:** ngược Non-goal (preview không cursor riêng). Defer.
- **Cột mtime/permission:** out-of-scope; lazyexplorer là "glance".

## 6. Acceptance criteria

### Gherkin

```gherkin
Feature: Consistent file listing across the list pane and the folder preview

  A directory's contents look identical whether shown in the left list pane
  or in the right content pane (when a folder is selected). Both panes render
  rows through one shared routine, so listing format can never drift between them.

  Background:
    Given the explorer is open at a directory
    And that directory contains the folder "src" and the file "main.go"

  Scenario: A folder is shown the same way in both panes
    When I view the folder "src" in the list pane
    And I view the folder "src" in the folder preview
    Then in both panes it appears as the name "src" with a trailing "/"
    And in both panes it carries the directory style
    And neither pane shows a folder emoji icon

  Scenario: A file is shown the same way in both panes
    When I view the file "main.go" in the list pane
    And I view the file "main.go" in the folder preview
    Then in both panes it appears as the name "main.go" with no trailing slash
    And in both panes it carries the file style
    And in both panes its human-readable size is shown

  Scenario: The active row is marked only in the list pane
    Given the cursor is on "main.go" in the list pane
    When I view "main.go" in the list pane
    Then its row shows the active-selection marker
    But when I view "main.go" in the folder preview
    Then its row shows no selection marker
    And every other aspect of the row matches the list pane

  Scenario: The folder preview stays clickable to navigate
    Given a folder preview lists the folder "src"
    When I click the "src" row in the preview
    Then the explorer navigates into "src"

  Scenario: An empty folder reads the same in both panes
    Given the folder "src" is empty
    When I view "src" in the list pane
    And I view "src" in the folder preview
    Then both panes show the same empty-directory message

  Scenario: A long entry name is truncated identically in both panes
    Given a file whose name is wider than the pane
    When it is shown in the list pane at a given width
    And it is shown in the folder preview at the same width
    Then both truncate the name the same way with a trailing ellipsis

  Scenario: Listing order is identical in both panes
    When I view a directory's contents in either pane
    Then directories appear first, then files
    And within each group entries are ordered case-insensitively by name
```

### Checklist verify

1. Đặt cursor lên một folder → preview liệt kê nội dung **không** emoji, dir có `/`, file
   có size, màu dir/file khớp y list pane bên trái.
2. So sánh trực tiếp: cũng folder đó, một hàng dir và một hàng file trông byte-giống nhau ở
   hai pane, chỉ list pane mới có caret `▶` ở hàng cursor.
3. List pane (regression có chủ đích): file giờ hiện size; name dài + size vẫn không tràn
   panel, không vỡ frame.
4. Folder rỗng → cùng placeholder hai pane.
5. Click một hàng trong folder preview → vào folder + cursor đúng item (FR5 không regress).
6. Width hẹp (kéo divider nhỏ): size bị bỏ trước, name cắt `…`, hai pane cắt giống nhau.
7. Poll: agent thêm/xóa file trong folder đang preview → preview cập nhật trong ~1s.
8. `go build -o lazyexplorer . && go vet ./... && go test ./...` xanh.

## 7. Task breakdown

> Đề xuất, chờ duyệt trước khi tick. Map sát file; mở rộng bộ hot-path test theo project memory.

- [x] **T1 — `renderEntryRow` + `fitRow`.** Routine vẽ một hàng + helper căn-phải-size/
  truncate (§5.1). *(view.go)*
- [x] **T2 — List pane dùng routine chung.** `renderList` gọi `renderEntryRow` mỗi hàng,
  `active=(i==cursor)`; list pane thêm size tự động (§5.2). *(view.go)*
- [x] **T3 — `previewDir` trả entries.** Đổi `previewDir` trả `[]entry` (entries thô),
  bỏ format string (§5.3). *(fs.go)*
- [x] **T4 — State + refreshPreview.** Thêm `previewEntries`/`previewIsDir`; nhánh dir lưu
  entries; reset hygiene off chúng đầu hàm (§5.3). *(model.go)*
- [x] **T5 — `renderPreview` nhánh folder.** Vẽ listing qua `renderEntryRow` khi
  `previewIsDir`; placeholder folder rỗng (§5.3). *(view.go)*
- [x] **T6 — `previewLen` + scroll.** Helper độ dài thống nhất; `previewScroll` +
  `scrollPreview` dùng nó (§5.4). *(view.go, model.go)*
- [x] **T7 — `previewClick` dùng entries.** Map row→`m.previewEntries[lineIdx]`, bỏ
  `readDir` lại (§5.5). *(model.go)*
- [x] **T8 — Test cross-pane consistency + truncation.** Hot-path tests sống trong
  `entryrow_test.go` (fitRow/renderEntryRow contract, truncation, size-drop, CJK width,
  list-pane size column visible) và `preview_dir_test.go` (state set/cleared, byte-equal
  cross-pane render, list↔preview byte-equal cùng entry, empty placeholder, previewLen
  mode switch, scroll consistency). `preview_test.go`/`previewclick_test.go` không cần
  sửa: chúng test behavior (cwd/cursor sau click, frame width/height), không phải string
  shape của folder preview — nên giữ nguyên là chính xác.
  *(entryrow_test.go, preview_dir_test.go)*
- [x] **T9 — Verify.** `go build -o lazyexplorer . && go vet ./... && go test ./...` xanh
  (cộng `-race`). Checklist §6 verified qua tests + visual smoke (`zz_dump_test.go`).

## 8. Files chạm tới (dự kiến)

| File | Thay đổi |
|------|----------|
| `view.go` | `+ renderEntryRow`, `+ fitRow`; `renderList` dùng routine chung; `renderPreview` nhánh folder; `previewScroll` dùng `previewLen` |
| `model.go` | `+ previewEntries`, `+ previewIsDir`, `+ previewLen`; `refreshPreview` nhánh dir + reset hygiene; `scrollPreview` dùng `previewLen`; `previewClick` đọc entries |
| `fs.go` | `previewDir` trả `[]entry` thay vì `[]string` |
| `theme.go` | **không đổi** (tái dùng `dirStyle`/`fileStyle`/`cursorActiveStyle`) |
| `preview_test.go` / `previewclick_test.go` | cập nhật sang shape entries + test nhất quán hai pane (T8) |
