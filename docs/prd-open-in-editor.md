# PRD — `e`: open the selected file in `$VISUAL`/`$EDITOR`

Status: **accepted** · Author: opus 4.8 session · Ngày: 2026-05-30 · Shipped: 2026-05-30 (✅ `go build -o lazyexplorer . && go vet ./... && go test ./...` + `go test -race ./...` green)

---

## 1. Bối cảnh & vấn đề

lazyexplorer sống *cạnh* một coding agent (xem `CLAUDE.md` §"Goal & Positioning"): user liếc cây
dự án, thấy file agent vừa sửa, rồi muốn **mở chính file đó để xem/sửa tay**. Hôm nay đường duy
nhất là: copy path (`copy absolute path`, `commands.go:71`) → nhảy sang pane khác → gõ `$EDITOR <path>`.
Một dogfood test đã đo và ghi nhận đúng friction này: `zz_dogfood_test.go:395-424` (T6) drive thử
palette + phím `o`/`e` và kết luận *"no open-in-$EDITOR … the palette offers only
reload/copy-path/cd/quit"*.

Năng lực spawn-process duy nhất đang có (`split.go`) chỉ mở **một lazyexplorer khác** trong split
pane — không phải editor. Phím `e` thì hoàn toàn **trống** trong keymap (`keys.go:62-96` không có
`WithKeys("e")`), nên không đụng binding nào.

## 2. Goal (1 câu)

Nhấn `e` (hoặc chạy palette command "open in editor") để mở file đang chọn trong `$VISUAL`/`$EDITOR`,
**suspend TUI** cho editor chiếm terminal, rồi **resume** với listing đã reload khi editor thoát.

**Non-goal làm rõ (chặn scope creep):**

- **Không** reveal-in-shell / open-in-Finder ở v1 — một feature một iteration. Defer (§5.4).
- **Không** đoán editor mặc định (no vi/nano fallback): cả `$VISUAL` lẫn `$EDITOR` trống → báo status,
  không exec. Trên máy beside-an-agent `$EDITOR` luôn được set; thả một user-không-dùng-vi vào vi là
  cú rage-quit (inversion lens).
- **Không** thêm panel/mode mới — `e` suspend cả TUI rồi resume, đúng idiom lazygit/glow.
- **Không** hỗ trợ dấu cách *bên trong* tên editor (vd `EDITOR="/my dir/ed"`) ở v1 — `strings.Fields`
  tách nó thành hai token. Norm beside-an-agent là `command + flags` (§5.4).

## 3. Quyết định đã chốt

| # | Quyết định | Giá trị | Lý do |
|---|-----------|---------|-------|
| D1 | Resolve editor | `$VISUAL` trước, rồi `$EDITOR`; cả hai trống → `errNoEditor` | Quy ước POSIX: VISUAL = full-screen editor, EDITOR = fallback. No-guess giữ user khỏi vi bất ngờ |
| D2 | Test seam | Pure builder `editorCommand(getenv func(string) string, absPath string) (*exec.Cmd, error)` (`commands.go`) | `tea.ExecProcess` opaque từ ngoài (closure → execMsg nội bộ, không soi được argv). Mirror `split.go:99-105` buildCmd — unit-test argv không cần terminal thật |
| D3 | Tách flags | `strings.Fields(editor)` + `exec.Command(fields[0], append(fields[1:], absPath)...)` | Flags sống sót (`code --wait`, `emacsclient -t`); path là token argv riêng → injection-safe, no shell (đúng ethos `split.go`) |
| D4 | Whitespace-only var | Không panic — `Fields("   ")` rỗng → fall through sang candidate kế, rồi `errNoEditor` | `fields[0]` trên slice rỗng = index panic; fold precedence + emptiness vào một vòng lặp |
| D5 | Suspend cơ chế | `tea.ExecProcess(cmd, fn)` (`bubbletea/v2 exec.go`) | Canonical: releaseTerminal → wire editor vào TTY thật → `c.Run()` (block) → RestoreTerminal → `Send(fn(err))`. Alt-screen/mouse (`view.go:358-359`) tự release + restore — không cần teardown tay |
| D6 | Guard chọn entry | `focusList` + listing non-empty + không `..` + không dir | Mirror Rename/Delete (`model.go:1532-1542`). `..` và dir không có gì để "edit"; `selectedAbsPath()` trên `..` trả về dir cha → phải chặn ở cả hai entry point |
| D7 | Resume on success | Snapshot tên file đang chọn → `m.reload()` → re-seek tên đó → `refreshPreview()` → `reconcilePreview(nil)` | Snappier hơn chờ poll 1s; reload kéo cả file mới editor tạo + preview re-render bản đã lưu. Giữ selection theo TÊN (không phải index) để file editor tạo sort ABOVE file vừa sửa không re-point cursor/preview sang neighbour — gương `ascend()` (`model.go:1642`) và poll path `syncFromDisk` (`model.go:513-526`). Clamp index của `reload()` là fallback khi tên biến mất (editor rename file giữa chừng) |
| D8 | Resume on error | `statusMsg = "⚠ editor: …"`, listing không đụng | Editor crash/lỗi spawn là best-effort fail — báo, không phá state |
| D9 | Palette twin | Command "open in editor" (`commands.go defaultCommands`), cùng guard | Phím trực tiếp là primary; palette entry trợ discoverability như `copy absolute path`. Guard riêng vì là entry point thứ hai |
| D10 | Telemetry | `m.tel.Record("action.open_editor", {name})` trước khi exec | Đo adoption; không log path (xem invariant telemetry các PRD trước) |

## 4. Functional requirements

- **FR1** — `focusList` + cursor trên một file (không phải dir, không phải `..`): nhấn `e` → trả về
  một `tea.ExecProcess` cmd suspend TUI vào `$VISUAL`/`$EDITOR <absPath>`.
- **FR2** — `$VISUAL` ưu tiên `$EDITOR`; `$VISUAL` rỗng (hoặc chỉ whitespace) → dùng `$EDITOR`.
- **FR3** — Flags trong editor string sống sót, path là token cuối tách rời (`code --wait /abs/f`).
- **FR4** — Cả hai env trống → không exec, `statusMsg` báo `⚠ set $VISUAL or $EDITOR …`.
- **FR5** — Guard từ chối: `..` synthetic, directory, listing rỗng, và `focusPreview` → cmd `nil`,
  không exec.
- **FR6** — Editor thoát sạch (`editorFinishedMsg{nil}`) → reload cwd ngay + preview re-render; file
  editor mới tạo xuất hiện trong listing không cần chờ poll. Cursor + preview **bám theo TÊN** file
  vừa sửa: file mới sort phía trên không kéo selection sang neighbour.
- **FR7** — Editor lỗi (`editorFinishedMsg{err}`) → `statusMsg` chứa `⚠ editor:`; listing giữ nguyên.
- **FR8** — Palette command "open in editor" làm y hệt phím `e` (cùng guard, cùng cmd).
- **FR9** — `e` nằm trong source của footer keyhint (`shortHelp` focusList), nhóm Mutation của `?`
  full-help, và dòng Keys của `--help` CLI. Footer clip ở bề rộng hẹp nên surface luôn-hiện-đủ là `?`.

## 5. Technical design

Kim chỉ nam: **một pure builder soi-được + một suspend cmd opaque**. Mọi logic kiểm-được
(resolve editor, tách flags, guard) ở tầng pure/Update; `tea.ExecProcess` chỉ là wrapper cuối.

### 5.1 Builder thuần (`commands.go`)

`editorCommand(getenv, absPath)` — vòng lặp fold precedence:

```go
for _, raw := range []string{getenv("VISUAL"), getenv("EDITOR")} {
    if fields := strings.Fields(raw); len(fields) > 0 {
        return exec.Command(fields[0], append(fields[1:], absPath)...), nil
    }
}
return nil, errNoEditor
```

`getenv` được inject (test set env không mutate process state — seam giống `split.go`). Whitespace-only
var cho `Fields` rỗng → fall through, không panic (D4).

### 5.2 Wiring (`model.go` updateNormal, ~`model.go:1544`)

`case key.Matches(msg, km.OpenInEditor)`: check `focusPane`/len trước (tránh index panic trên listing
rỗng), rồi `sel.name == ".." || sel.isDir` → `nil`. Resolve qua `editorCommand(os.Getenv,
m.selectedAbsPath())`; lỗi → status + `nil`; thành công → `tel.Record` + return
`tea.ExecProcess(cmd, func(err error) tea.Msg { return editorFinishedMsg{err} })` **trực tiếp** (không
qua tail `reconcilePreview` — exec phải là cmd duy nhất keypress này sinh ra).

### 5.3 Resume (`model.go` Update, `editorFinishedMsg`)

`type editorFinishedMsg struct{ err error }`. Handler: `err != nil` → `statusMsg = "⚠ editor: " +
err.Error()`, return `m, nil`. Ngược lại: snapshot tên file đang chọn → `m.reload()` → re-seek tên
đó trong `m.entries` (clamp của reload là fallback khi tên biến mất) → `refreshPreview()` → clear
status → `reconcilePreview(nil)` (D7/D8). Snapshot-rồi-reseek-theo-tên giữ cursor/preview trên file
vừa sửa khi editor tạo một file sort ABOVE nó — gương `ascend()` (`model.go:1642`) và `syncFromDisk`.

### 5.4 Đã cân nhắc & defer khỏi v1

- **Reveal-in-shell / open-in-Finder** — feature riêng, iteration sau (Non-goal).
- **Dấu cách trong tên editor** (`EDITOR="/my dir/ed"`) — `strings.Fields` tách nhầm; hiếm trên
  máy dev, defer. Falsify: set `EDITOR='"/a b/ed"'` → token[0] = `"/a`. Norm là bare command + flags.
- **fsnotify thay reload** — reload đồng bộ là đủ snappy; watcher đã defer ở `prd-search` §5.11.

## 6. Acceptance criteria

```gherkin
Feature: Open the selected file in the user's editor

  Background:
    Given lazyexplorer is focused on the file list
    And $EDITOR names a runnable editor

  Scenario: Open a file in the editor
    Given the cursor is on a file
    When I open it in the editor
    Then lazyexplorer suspends and the editor runs on that file
    And on exit the listing is reloaded so any new or changed file shows

  Scenario: Refuse a directory
    Given the cursor is on a folder
    When I ask to open it in the editor
    Then nothing is launched

  Scenario: Refuse the parent entry
    Given the cursor is on the ".." entry
    When I ask to open it in the editor
    Then nothing is launched

  Scenario: No editor configured
    Given neither $VISUAL nor $EDITOR is set
    When I ask to open a file in the editor
    Then nothing is launched
    And the status line tells me to set $VISUAL or $EDITOR

  Scenario: The editor failed
    Given the editor exits with an error
    When lazyexplorer resumes
    Then the status line surfaces the editor error
    And the listing is left untouched
```

Checklist verify:

1. `editorCommand` ưu tiên VISUAL→EDITOR; tách flags; path là token cuối; cả hai trống → error;
   whitespace-only không panic (`commands_test.go` TestEditorCommand).
2. Guard từ chối `..`/dir/empty/focusPreview, chấp nhận file (cmd non-nil) — drive qua `updateNormal`
   trực tiếp để cmd-nilness chứng minh đúng việc wiring exec (`model_test.go` TestOpenInEditorGuardsSelection).
3. `editorFinishedMsg{nil}` → file mới (ghi vào cwd trước khi gửi msg) xuất hiện sau reload; cursor
   + preview bám theo TÊN file vừa sửa khi một file mới sort ABOVE nó (không re-point sang neighbour);
   `{err}` → status `⚠ editor` (`model_test.go` TestEditorFinishedMsgReloadsOnSuccess).
4. Palette command "open in editor" (entry point thứ hai) guard từng nhánh khi gọi `Run` trực tiếp:
   file → cmd non-nil, không ⚠; dir + `..` → cmd nil + `⚠ not a file`; listing rỗng → `⚠ nothing
   selected`; không editor → ⚠ + cmd nil (`palette_test.go` TestCommandOpenInEditor).
5. Dogfood T6 đo capability hiện diện (palette command + `e` trả cmd) — KHÔNG chạy cmd (sẽ spawn
   $EDITOR trong CI) (`zz_dogfood_test.go` T6).
6. `e` nằm trong source của footer keyhint (`shortHelp` focusList) + nhóm Mutation của `?`
   full-help + dòng Keys của `--help`. Footer là một dòng `fitWidth`-clip: ở bề rộng hẹp (vd 90
   cột) các hint đuôi bị cắt — y như `ctrl+p`/`?`/`q` đã bị cắt TRƯỚC thay đổi này; surface tham
   chiếu luôn-hiện-đủ là `?`. Visual verdict pass: `?` overlay scroll tới nhóm Mutation render
   `r rename · d delete · e open in editor` thẳng cột, và status no-editor render gọn không tràn
   (`zz_dump_test.go` TestDumpOpenInEditorFrames; ĐÃ VERIFY ✅ 2026-05-30).
7. `go build -o lazyexplorer . && go vet ./... && go test ./...` xanh; `go test -race ./...` xanh.

## 7. Task breakdown

- [x] **T1 — Pure builder.** `editorCommand` + `errNoEditor`, fold-precedence loop. *(commands.go)*
- [x] **T2 — Keybind.** Field `OpenInEditor` + `e` binding (mutation group). *(keys.go)*
- [x] **T3 — updateNormal case + resume.** Guard + `tea.ExecProcess`; `editorFinishedMsg` struct +
  Update handler. *(model.go)*
- [x] **T4 — Palette twin.** Command "open in editor" với guard riêng. *(commands.go)*
- [x] **T5 — Help surfaces.** Footer keyhint, `?` full-help mutation group, `--help` CLI Keys line.
  *(view.go, main.go)*
- [x] **T6 — Dogfood reframe.** T6 đo capability present thay vì friction. *(zz_dogfood_test.go)*
- [x] **T7 — Verify.** `go build -o lazyexplorer . && go vet ./... && go test ./...` xanh;
  `go test -race ./...` xanh; visual verdict footer/status.

## 8. Files chạm tới

| File | Thay đổi |
|------|----------|
| `commands.go` | `editorCommand` + `errNoEditor`; palette command "open in editor" |
| `keys.go` | Field `OpenInEditor` + `e` binding |
| `model.go` | `editorFinishedMsg` struct; updateNormal case; Update resume handler |
| `view.go` | `OpenInEditor` vào `shortHelp` (focusList) + `fullHelp` (Mutation) |
| `main.go` | `printHelp` Keys line thêm `e open in editor` |
| `commands_test.go` | TestEditorCommand (driver) |
| `model_test.go` | TestOpenInEditorGuardsSelection, TestEditorFinishedMsgReloadsOnSuccess (+ resume giữ selection theo tên) |
| `palette_test.go` | TestCommandOpenInEditor — guard từng nhánh của palette command "open in editor" |
| `zz_dogfood_test.go` | T6 reframe: capability present |
| `docs/prd-open-in-editor.md` | PRD này |
