# PRD — `y`: yank the selection's project-relative path

Status: **accepted** · Author: opus 4.8 session · Ngày: 2026-05-30 · Shipped: 2026-05-30 (✅ `go build -o lazyexplorer . && go vet ./... && go test ./...` + `go test -race ./...` green)

---

## 1. Bối cảnh & vấn đề

lazyexplorer sống *cạnh* một coding agent (`CLAUDE.md` §"Goal & Positioning"): user liếc cây dự án,
thấy file agent vừa sửa, rồi muốn **dán đường dẫn file đó vào chat của agent**. Agent kỳ vọng đường
dẫn **project-relative** (`src/auth.go`), không phải absolute (`/Users/…/proj/src/auth.go`).

Hôm nay năng lực copy duy nhất là palette command "copy path" (`commands.go:71`) — và nó copy
**absolute path**. Chính dogfood test của dự án đã đo & ghi nhận đúng friction này:
`zz_dogfood_test.go:227-229` log *"achievable=false … there is no project-relative option, so you
hand-trim the prefix before pasting into the agent"*. User phải tự xoá tay tiền tố repo trước khi dán.

Phím `y` (lowercase) đang **trống** trong `updateNormal` (`keys.go:63-99` không có `WithKeys("y")`).
`"y"`/`"Y"` duy nhất trong codebase là rune confirm của `updateConfirmDelete` (`model.go:1826`) — một
**lane mode khác** (modeConfirmDelete, vào qua `d`), không đụng `updateNormal`. Cùng cách `d`→confirm→`y`
đã coexist sẵn.

## 2. Goal (1 câu)

Nhấn `y` để copy đường dẫn của entry đang chọn **relative tới jail root, dạng slash**, lên clipboard
— dán thẳng vào chat agent, không phải xoá tay tiền tố repo.

**Non-goal làm rõ (chặn scope creep):**

- **Không** bỏ năng lực copy-absolute — nó được đổi tên "copy path" → "copy absolute path" để đọc rõ
  bên cạnh twin mới, vẫn còn cho ca hiếm cần absolute.
- **Không** thêm panel/mode mới — chỉ một keybind + một status-line message. Bề mặt UI tối thiểu.
- **Không** bind `"Y"` (uppercase) — chỉ `"y"`; `"Y"` ở yên là rune confirm-delete.
- **Không** copy `".."` (parent dir) — dán đường dẫn thư mục cha vào chat agent không phải use case.

## 3. Quyết định đã chốt

| # | Quyết định | Giá trị | Lý do |
|---|-----------|---------|-------|
| D1 | Keybind | `Yank: key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "yank rel path"))` trong nhóm Misc của `KeyMap` (`keys.go`) | `y` trống, lane riêng với confirm-delete `y`. 1 keystroke vs 6 của palette path. keys.go là single source |
| D2 | Pure builder | `relRoot(root, abs string) string` = `filepath.ToSlash(filepath.Rel(root, abs))`; Rel error (không xảy ra dưới-root, defensive) → trả abs nguyên (`commands.go`) | CRUX: rel string phải tính bằng hàm thuần test **độc lập** với `writeClipboard` — clipboard fail trong CI (no pbcopy/xclip), nên đúng-tính của rel phải chứng minh không cần clipboard. `ToSlash` chặn backslash Windows lọt vào chat |
| D3 | Wiring | `case key.Matches(msg, km.Yank)` trong `updateNormal` (`model.go`), focusList-only | rel path chỉ có nghĩa với một list selection. focusList-only theo đúng cluster e/r/d (selection-acting) — nhất quán hơn act-at-any-focus như ToggleDiff |
| D4 | Edge `..` | `sel.name == ".."` → từ chối, status `⚠ nothing to yank` | `selectedAbsPath()` trên `..` trả về parent dir; `relRoot(root, root) == "."` — copy `"."` vô dụng trong chat. Từ chối bằng TÊN (như open-in-editor) ở tầng dispatch, không string-match `"."` |
| D5 | Dir thật yankable | Real directory (không `..`) → copy bình thường | Khác open-in-editor (chỉ file): dán đường dẫn thư mục vào chat agent là hợp lệ ("look at src/handlers/") |
| D6 | Help surfaces | `Yank` vào nhóm **Misc** của `fullHelp` (`view.go`, cạnh `Quit`) + footer `shortHelp` focusList + dòng Keys của `--help` | clipboard utility, không phải mutation/mode. Giữ `fullHelp` đúng **5 nhóm** (titles `renderHelpBody` phụ thuộc) — join nhóm có sẵn, không tạo nhóm thứ 6 |
| D7 | Shared helper + twin | `func (m *model) yankRelPath()` chứa guard→`relRoot`→`writeClipboard`→status→**một** `tel.Record`; cả phím `y` lẫn palette "copy relative path" gọi nó | Ledger note của open-in-editor cảnh báo split twin **double-records** telemetry. Một code path → record đúng một lần. (Khá hơn guard *nhân đôi* của open-in-editor) |
| D8 | Telemetry | `m.tel.Record("action.yank_rel", {rel})` một lần, trong `yankRelPath` | Đo adoption. Log `rel` (đã project-relative, không lộ machine-absolute prefix) |

## 4. Functional requirements

- **FR1** — focusList + cursor trên một entry (file **hoặc** dir thật, không `..`): nhấn `y` → copy
  `relRoot(root, selectedAbsPath)` lên clipboard; status `copied <rel>` (clipboard ok) hoặc
  `⚠ clipboard: …` (no helper). rel là slash-form, không tiền tố root.
- **FR2** — Cursor trên `..` synthetic → không copy, status `⚠ nothing to yank` (không copy `"."`).
- **FR3** — Listing rỗng → không copy, status `⚠ nothing selected`. focusPreview → no-op.
- **FR4** — `y` nằm trong source của footer keyhint (`shortHelp` focusList), nhóm Misc của `?`
  full-help, và dòng Keys của `--help` CLI.
- **FR5** — Palette command "copy relative path" làm y hệt phím `y` (cùng `yankRelPath`, record một lần).
  Command "copy absolute path" (đổi tên từ "copy path") giữ năng lực copy absolute.
- **FR6** — `y` trong modeConfirmDelete (vào qua `d`) vẫn DELETE — yank không shadow rune confirm.

## 5. Technical design

Kim chỉ nam: **một pure builder soi-được + một helper side-effect chia sẻ**. Tính-rel kiểm được ở
tầng pure (`relRoot`); guard + clipboard + telemetry gom vào `yankRelPath` để phím và palette dùng chung.

### 5.1 Builder thuần (`commands.go`)

```go
func relRoot(root, abs string) string {
    rel, err := filepath.Rel(root, abs)
    if err != nil {
        return abs
    }
    return filepath.ToSlash(rel)
}
```

`getenv`-free, không I/O — test bảng (`TestRelRoot`) chứng minh slash-form + `.` cho root-itself, độc
lập với clipboard. `relRoot(root, root) == "."` là output trung thực; dispatch từ chối `..` nên `"."`
không bao giờ tới clipboard (hai tầng, cả hai assert).

### 5.2 Helper side-effect chia sẻ (`model.go`)

`func (m *model) yankRelPath()`: guard `len(entries)==0` → `⚠ nothing selected`; `name==".."` →
`⚠ nothing to yank`; rồi `rel := relRoot(m.root, m.selectedAbsPath())` → `tel.Record("action.yank_rel",
{rel})` (một lần) → `writeClipboard(rel)`; lỗi → `⚠ clipboard: …`, ok → `copied <rel>`. Không trả
`tea.Cmd` — `writeClipboard` đồng bộ (one-shot pipe tới pbcopy).

### 5.3 Wiring (`model.go` updateNormal)

`case key.Matches(msg, km.Yank)`: `if m.focusPane == focusList { m.yankRelPath() }`. focusList-only
như cluster e/r/d. `selectedAbsPath()` (`commands.go:150`) đã resolve `..` synthetic về parent dir
jail-clamped — nhưng dispatch chặn `..` trước nên không tới đó cho ca yank.

### 5.4 Palette twin (`commands.go` defaultCommands)

"copy path" → đổi tên "copy absolute path" (giữ logic absolute). Thêm "copy relative path" với
`Run: func(m *model, _ string) tea.Cmd { m.yankRelPath(); return nil }` — cùng code path với phím `y`,
record một lần.

### 5.5 Đã cân nhắc & defer khỏi v1

- **Bind `"Y"` uppercase cho yank** — thừa; `"Y"` đang là rune confirm-delete, để yên tránh nhầm lẫn lane.
- **Yank `..` (parent dir)** — dán parent dir vào chat agent không phải use case; refuse (D4).
- **Chỉ giữ phím, bỏ palette twin** — twin chỉ để discoverability (`y` đã hiện trong `?`); giữ vì một
  shared helper khiến twin gần như free (không nhân đôi guard/telemetry).

## 6. Acceptance criteria

```gherkin
Feature: Yank the selection's project-relative path

  Background:
    Given lazyexplorer is focused on the file list
    And the launch directory is the jail root

  Scenario: Yank a file's relative path
    Given the cursor is on a file nested under the root
    When I yank its path
    Then the clipboard receives the path relative to the root, in slash-form
    And the status line confirms what was copied

  Scenario: Yank a directory's relative path
    Given the cursor is on a real folder
    When I yank its path
    Then the clipboard receives the folder's path relative to the root

  Scenario: Refuse the parent entry
    Given the cursor is on the ".." entry
    When I ask to yank its path
    Then nothing is copied
    And the status line says there is nothing to yank

  Scenario: Nothing selected
    Given the directory is empty
    When I ask to yank a path
    Then nothing is copied
    And the status line says nothing is selected

  Scenario: The delete-confirm prompt is untouched
    Given I have asked to delete the selected file
    When I confirm with "y"
    Then the file is deleted, not yanked
```

Checklist verify:

1. `relRoot` trả slash-form rel cho file/dir under root; root-itself → `.`; Rel error → abs nguyên
   (`commands_test.go` TestRelRoot).
2. `y` qua `updateNormal`: file/dir → `copied <rel>` HOẶC `⚠ clipboard` (clipboard-agnostic), rel
   không tiền tố root + không absolute; `..` → `⚠ nothing to yank` (KHÔNG copy `.`); empty →
   `⚠ nothing selected`; focusPreview → no-op (`yank_test.go` TestYankRelPath).
3. `y` trong nhóm Misc của `fullHelp`, `fullHelp` vẫn đúng 5 nhóm (`yank_test.go` TestYankInFullHelpMisc).
4. `d`→confirm→`y` vẫn DELETE (regression: yank không shadow confirm rune) (`yank_test.go`
   TestYankDoesNotShadowDeleteConfirm).
5. Palette "copy relative path" gọi `yankRelPath` (rel không tiền tố root); "copy absolute path" vẫn
   được offer (`palette_test.go` TestCommandCopyRelative); palette body liệt kê cả hai
   (`palette_test.go` TestPaletteBodyRenders).
6. Dogfood T2 RESOLVED: drive `y`, rel == `filepath.Rel(root, abs)` slash-form, log confirm-resolved
   (`zz_dogfood_test.go` T2). ĐÃ VERIFY ✅ 2026-05-30: `achievable=true keystrokes=1 statusMsg="copied main.go"`.
7. `go build -o lazyexplorer . && go vet ./... && go test ./...` xanh; `go test -race ./...` xanh.

## 7. Task breakdown

- [x] **T1 — Pure builder.** `relRoot(root, abs)` = `ToSlash(Rel(...))`, Rel-error fallback. *(commands.go)*
- [x] **T2 — Keybind.** Field `Yank` + `y` binding (nhóm Misc). *(keys.go)*
- [x] **T3 — Shared helper + dispatch.** `yankRelPath()` (guard/rel/clipboard/telemetry-once) +
  `case km.Yank` focusList-only. *(model.go)*
- [x] **T4 — Palette twin.** Đổi tên "copy path"→"copy absolute path"; thêm "copy relative path" gọi
  `yankRelPath`. *(commands.go)*
- [x] **T5 — Help surfaces.** `Yank` vào `fullHelp` Misc + `shortHelp` focusList + dòng Keys `--help`.
  *(view.go, main.go)*
- [x] **T6 — Dogfood reframe.** T2 drive `y`, assert rel resolved, log confirm-resolved. *(zz_dogfood_test.go)*
- [x] **T7 — Verify.** `go build -o lazyexplorer . && go vet ./... && go test ./...` xanh;
  `go test -race ./...` xanh; visual verdict help overlay (Misc nhóm hiện `y yank rel path`).

## 8. Files chạm tới

| File | Thay đổi |
|------|----------|
| `commands.go` | `relRoot` pure builder; đổi tên "copy path"→"copy absolute path"; thêm "copy relative path" twin |
| `keys.go` | Field `Yank` + `y` binding (nhóm Misc) |
| `model.go` | `yankRelPath()` shared helper; `case km.Yank` trong updateNormal |
| `view.go` | `Yank` vào `fullHelp` Misc + `shortHelp` focusList |
| `main.go` | `printHelp` Keys line thêm `y yank rel path` |
| `commands_test.go` | TestRelRoot (driver) |
| `yank_test.go` | TestYankRelPath, TestYankInFullHelpMisc, TestYankDoesNotShadowDeleteConfirm |
| `palette_test.go` | TestCommandCopyRelative; TestPaletteBodyRenders name-set update |
| `zz_dogfood_test.go` | T2 reframe: capability resolved (drive `y`) |
| `docs/prd-yank-relative-path.md` | PRD này |
