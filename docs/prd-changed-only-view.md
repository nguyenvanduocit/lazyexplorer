# PRD — `c`: changed-only aggregate view

Status: **accepted** · Author: opus 4.8 session · Ngày: 2026-05-30 · Shipped: 2026-05-30 (✅ `go build -o lazyexplorer . && go vet ./... && go test ./...` + `go test -race ./...` green)

---

## 1. Bối cảnh & vấn đề

lazyexplorer sống *cạnh* một coding agent (`CLAUDE.md` §"Goal & Positioning"): agent sửa nhiều file
rải khắp cây dự án, user muốn liếc một phát thấy **mọi file vừa thay đổi** rồi nhảy thẳng vào diff
của file đó.

Hôm nay git layer ĐÃ biết toàn bộ change-set: `m.git.changes` (map repo-rel→`gitChange`, `git.go:96-105`)
giữ cả cây. Nhưng nó là **render-only**: `indicatorFor` (`model.go:392`) chỉ vẽ badge cho thư mục
HIỆN TẠI; change lồng sâu collapse thành một ● roll-up (`model.go:401`, `rollupGlyph` `view.go:394`)
mà user phải descend từng cấp để tới. Dogfood test của dự án đo & ghi đúng hai friction này:

- **T1** (`zz_dogfood_test.go:144,180`): *"no single-key jump-to-change exists; you breadcrumb down ●
  rollups by hand"* — 5 keystrokes root→file lồng sâu, và chỉ work vì ● chỉ đường.
- **T4** (`zz_dogfood_test.go:331,351`): *"no changed-only view/filter and no aggregate list exists"* —
  git biết hết nhưng UI chỉ surface badge của dir hiện tại.

Phím `c` (lowercase) đang **trống**: grep `WithKeys` trong `keys.go`, các `case` inline trong
`updateNormal`, `palette.go`, `commands.go` — `c` không bound ở đâu.

## 2. Goal (1 câu)

Nhấn `c` để mở một **flat list của mọi working-tree change trong cả repo** (badge + delta + đường dẫn
root-relative); Enter teleport thẳng vào file đó, hạ cánh ngay trong diff-view — không còn breadcrumb
xuống ● roll-up từng thư mục.

**Non-goal làm rõ (chặn scope creep):**

- **Không** thêm panel thứ ba — `modeChanges` tái dụng đúng bề mặt flat-list của `modeSearch`
  (list pane + preview pane), KHÔNG phải pane mới. Bề mặt UI giữ tối thiểu.
- **Không** filter/staging/commit — chỉ là một *view* read-only của change-set; mọi thao tác git
  (stage, commit, checkout) vẫn ngoài phạm vi lazyexplorer.
- **Không** mouse-click chọn row trong changes mode — như `modeSearch`, mouse bị ignore khi không ở
  modeNormal (`model.go` `tea.MouseMsg` case); palette twin "view changes" là lối discoverability cho
  chuột.

## 3. Quyết định đã chốt

| # | Quyết định | Giá trị | Lý do |
|---|-----------|---------|-------|
| D1 | Mode mới, KHÔNG panel | `modeChanges` cạnh `modeSearch` trong enum (`model.go:93`) | superfile có nhiều panel/mode; ta giữ nhỏ hơn (`CLAUDE.md` §Design Principles). Mode tái dụng list+preview có sẵn |
| D2 | Mirror modeSearch | repurpose `m.entries`/`cursor`/`listTop` thành flat result list tên root-relative; snapshot pre-state, restore trên Esc | modeSearch là pattern đã chứng minh (`enterSearch`/`exitSearchRestore` `model.go`). Changes = modeSearch MINUS query box, SOURCED từ `m.git.changes` thay vì `walkTree` |
| D3 | Keybind | `Changes: key.NewBinding(key.WithKeys("c"), key.WithHelp("c","changes"))` nhóm Modes (`keys.go`) | `c` trống, mnemonic (changes), song song với `/` (search). keys.go là single source → `?` overlay tự surface |
| D4 | Predicate dùng chung | `flatListMode()` = `mode==modeSearch \|\| mode==modeChanges` (`model.go:97`) | `previewBaseDir`/`renderList`/`refreshPreview` đã branch `mode==modeSearch` để resolve base→root; gom vào một predicate để hai mode không drift khi thêm mode flat-list thứ hai (consistency-is-kindness) |
| D5 | Inverse prefix map | `rootRelFromRepoRel(repoRel)` = nghịch đảo `gitRootPrefix`; strip prefix, drop key ngoài jail (`changes.go`) | `m.git.changes` keyed REPO-rel; row phải root-rel để tái dụng `openSearchResult`'s `filepath.Join(m.root, sel.name)`. Cùng kỷ luật jail như `repoRelKey`'s "../" guard (`model.go:381`) |
| D6 | Badge qua indicatorFor | row mang TÊN trần; badge/delta resolve lúc render qua `indicatorFor(changesBaseDir(), e)` | base=root trong changes mode (D4), nên `indicatorFor` tra đúng `m.git.changes[rel]` → badge + delta y hệt listing-row inline (`code.badge()` `git.go:52` + `deltaString()` `git.go:82` + `gitColor()`) |
| D7 | Sort | `sort.Slice` theo `name` (`changes.go`) | mirror `walkTree`'s sort (`fs.go:190`) → thứ tự ổn định, đoán được |
| D8 | Enter = teleport + diff | `openChangesResult` cd vào parent + seek basename (như `openSearchResult` `model.go:1815`); diffOn mặc định true (`newModel` `model.go:317`) → hạ cánh hiện diff ngay | hai feature compose: list → Enter → review-the-edit-in-pane (prd-preview-diff-view D3), zero tab-away |
| D9 | Empty tree | enter mode với list rỗng + status `(no changes)` + pane `(no changes)` | user hỏi "what changed?" → trả lời "nothing", VISIBLY (mirror applySearchFilter's 0-results `model.go`); KHÔNG crash, KHÔNG silent no-op |
| D10 | Outside repo | `c` no-op khi `repoRoot==""` (enterChanges guard) | không có gì để list, git mode off (mirror prd-preview-diff-view FR9) |
| D11 | Deleted file | vẫn LIST (nó LÀ change); Enter → `os.Stat` fail → `⚠ file no longer on disk`, ở lại list (`changes.go`) | gitDeleted không có row trên đĩa; cd vào path chết là vô nghĩa — báo & ở lại để pick row khác |
| D12 | Live-refresh, KHÔNG churn | parked trong modeChanges + git snapshot landed (`gitRefreshedMsg`) → `refreshChanges` re-derive list, giữ cursor theo TÊN; preview CHỈ re-render khi selected change đổi (so name+size+mtime, như syncFromDisk) | change agent vừa tạo hiện ra mà không cần re-open. Poll loop dispatch git refresh ~1s/lần khi đang ở mode này, nên `refreshPreview` vô điều kiện sẽ re-fetch diff (git exec) + re-render mỗi giây cho file không đổi — đúng class `bug-poll-preview-rerender` (`model.go:536-543`). Re-derive list trên state APPLY (không phải mỗi render) — khớp snapshot semantics của search |
| D13 | Palette twin | command "view changes" gọi `enterChanges`; ngoài repo → status `⚠ not a git repo` (`commands.go`) | discoverability/mouse parity (mirror "open in editor" `commands.go`). `exitCommandPalette` giữ mode command đã set (không clobber về normal) |
| D14 | Telemetry | `action.changes_view_open` lúc enter + `action.changes_jump {rel}` lúc Enter-to-file (`changes.go`) | đo adoption (mirror `action.command_palette_open` `palette.go:21` + `action.yank_rel` `model.go`); non-blocking Record |

## 4. Functional requirements

- **FR1** — Trong repo, modeNormal, nhấn `c` → `modeChanges`; `m.entries` thành flat list mọi change
  (file, root-relative slash name), sort theo name; cursor/listTop reset; pre-state snapshot.
- **FR2** — Mỗi row render `<badge> <relPath> <delta>`: badge tô màu qua `gitColor`, delta muted, đường
  dẫn flush-left — y hệt indicator của listing row inline (`renderEntryRow`).
- **FR3** — Change ngoài jail (gitRootPrefix-trim fail / "../" escape) bị LOẠI khỏi list.
- **FR4** — Enter (hoặc `l`/`right`) trên một change → cd vào parent của file + đặt cursor lên basename,
  về modeNormal; diffOn mặc định true → preview hiện diff hunk của file (list→Enter→review).
- **FR5** — Esc → restore EXACT pre-view state (cwd, entries, fsSig, cursor, listTop).
- **FR6** — j/k (↓/↑), g/G di chuyển trong list; ký tự in được khác bị ignore (list cố định, không query).
- **FR7** — Outside a git repo: `c` no-op (mode giữ normal, listing không đổi).
- **FR8** — Clean tree (changes rỗng): enter modeChanges với list rỗng, status + pane `(no changes)`.
- **FR9** — Deleted-file change: vẫn LIST; Enter → `⚠ file no longer on disk: <rel>`, ở lại modeChanges.
- **FR10** — Parked trong modeChanges, một git refresh mới → list re-derive, cursor giữ theo tên; một
  change vừa xuất hiện → `(no changes)` hint bị flip về rỗng.
- **FR11** — Palette command "view changes" vào modeChanges (= phím `c`); ngoài repo → status giải thích.
- **FR12** — `c` hiện trong nhóm Modes của `?` full-help (keymap là single source).

## 5. Technical design

Kim chỉ nam: **modeChanges = modeSearch MINUS query, SOURCED từ git map**. Tái dụng tối đa teleport
`openSearchResult`, base-resolution flat-list, và indicator render có sẵn; chỉ thêm phần derive list
từ `m.git.changes` và phần xử lý mode/Enter riêng.

### 5.1 Predicate dùng chung (`model.go`)

`func (m model) flatListMode() bool { return m.mode == modeSearch || m.mode == modeChanges }`.
`previewBaseDir` (`model.go:417`), `renderList` base (`view.go`), và `refreshPreview` base
(`model.go`) đổi từ `mode==modeSearch` sang `flatListMode()` → changes mode resolve entry name &
git key against `m.root` y hệt search.

### 5.2 Derive list (`changes.go`)

`changedRows()` lặp `m.git.changes`, map mỗi key repo-rel → root-rel qua `rootRelFromRepoRel` (drop
key ngoài jail), sort theo name, trả `[]entry` tên trần. Nghịch đảo chính xác `repoRelKey` (root-rel→
repo-rel): prefix=="" → name==key; prefix=="sub" → "sub/a.go"→"a.go", key không dưới "sub/" → loại.
`repoRoot==""` hoặc map rỗng → trả `nil`. Deleted files VẪN ở trong `m.git.changes` nên vẫn được list.

### 5.3 Mode transitions (`changes.go`)

- `enterChanges`: guard `repoRoot==""` (no-op); snapshot pre-state (như `enterSearch`); `entries =
  changedRows()`; reset cursor/listTop; rỗng → status `(no changes)`; record `changes_view_open`;
  `refreshPreview`.
- `refreshChanges`: re-derive `entries`, giữ cursor theo tên, track `(no changes)` hint. KHÔNG churn:
  snapshot selected entry (name+size+mtime) trước khi re-derive, chỉ `refreshPreview` khi selection đổi
  identity hoặc dời/biến mất (mirror `syncFromDisk` `model.go:536-543`) — chặn diff re-fetch mỗi poll tick.
- `exitChangesRestore`: restore 5 trường saved (mirror `exitSearchRestore`).
- `openChangesResult`: jail-check `withinRoot`; `os.Stat` fail → `⚠ file no longer on disk` + ở lại;
  ngược lại record `changes_jump {rel}`, cd vào `filepath.Dir(target)`, reload, seek basename.
- `updateChanges`: Back→restore, OpenEntry→openResult, MoveDown/Up/GoTop/GoBottom→di chuyển + refreshPreview.

### 5.4 Wiring (`model.go`)

`case key.Matches(msg, km.Changes): m.enterChanges()` trong `updateNormal`; `case modeChanges:
m.updateChanges(msg)` trong Update's KeyPressMsg switch; trong `gitRefreshedMsg` case, sau khi apply
state, `if m.mode == modeChanges { m.refreshChanges() }`.

### 5.5 View (`view.go`)

`renderList` đã iterate `m.entries` + `indicatorFor(previewBaseDir(), …)` → row changes mode tự render
đúng badge/delta. Empty list trong modeChanges → list pane `(no changes)` (thay `(empty directory)`).
PREVIEW pane đối xứng: `refreshPreview`'s empty-entries branch (`model.go:651`) gate cùng `m.mode ==
modeChanges` → preview cũng đọc `(no changes)`, nên cả hai pane nhất quán khi clean tree (D9/FR8).
`renderStatus` thêm case `modeChanges`: hint move/open/back/quit từ keymap (KHÔNG mutation hint) + statusMsg.
`fullHelp` nhóm Modes thêm `km.Changes`.

### 5.6 Palette twin (`commands.go`, `palette.go`)

Command "view changes" `Run` gọi `enterChanges` (ngoài repo → `⚠ not a git repo`). `exitCommandPalette`
chỉ set modeNormal khi mode VẪN là modeCommandPalette — command đã chuyển sang modeChanges thì giữ
nguyên (tổng quát cho mọi command mở mode khác, không special-case).

### 5.7 Đã cân nhắc & defer khỏi v1

- **Mouse click chọn row** — modeSearch cũng ignore mouse; giữ nhất quán, palette twin là lối chuột.
- **Filter/group theo status (M/A/D)** — thêm UI phức tạp; v1 là flat sorted list, user liếc là đủ.
- **Stage/commit từ view** — ngoài phạm vi (lazyexplorer là explorer, không phải git client).
- **Re-derive list mỗi render** — chọn re-derive lúc enter + lúc git-apply (D12) để khớp snapshot
  semantics của search; mỗi-render sẽ làm cursor nhảy dưới tay user.

## 6. Acceptance criteria

```gherkin
Feature: Changed-only aggregate view

  Background:
    Given lazyexplorer is launched at a git repo's root
    And the agent has changed files in several directories

  Scenario: Open the aggregate of every change
    When I open the changed-only view
    Then I see a flat list of every working-tree change in the repo
    And a nested change appears as its own row, not folded into a folder roll-up

  Scenario: Jump to a change and land in its diff
    Given the changed-only view is open
    When I open a modified file from the list
    Then the explorer moves to that file's directory with it selected
    And the preview shows that file's diff

  Scenario: A clean tree answers "nothing"
    Given the working tree has no changes
    When I open the changed-only view
    Then the view opens with an empty list
    And it states that there are no changes

  Scenario: Esc returns to where I was
    Given I opened the changed-only view from a sub-directory
    When I close it
    Then I am back in that sub-directory with my previous selection

  Scenario: A deleted file is still listed
    Given a tracked file was deleted on disk
    When I open the changed-only view
    Then the deleted file appears as a change
    And opening it reports that the file is no longer on disk without leaving the view

  Scenario: Outside a git repo the view is inert
    Given the launch directory is not inside a git repo
    When I press the changes key
    Then nothing opens and the file list is unchanged
```

Checklist verify:

1. `changedRows` trả list root-rel sorted; prefix subdir strip đúng + loại change ngoài jail; empty
   changes / `repoRoot==""` → list rỗng (`changes_test.go` TestChangedRows*).
2. `c` qua Update: trong repo → modeChanges + list chứa README.md (root) + src/app.go (nested);
   ngoài repo → no-op; clean tree → modeChanges + `(no changes)` (`changes_test.go` TestEnterChanges*,
   TestChangesNoOpOutsideRepo, TestChangesEmptyTreePlaceholder).
3. Esc → restore exact cwd/entries/cursor/listTop/fsSig (`changes_test.go` TestExitChangesRestoresState).
4. Enter trên src/app.go → cwd=src, cursor=app.go, previewIsDiff=true, hunk `+func App` hiện
   (`changes_test.go` TestChangesEnterJumpToDiff). Deleted file → `⚠ file no longer on disk`, ở lại
   modeChanges (TestChangesEnterDeletedFileTolerated). Row preview trong list (trước Enter) hiện diff,
   không phải `(binary files differ — 0B)` — row mang size thật (TestChangesRowPreviewShowsDiffNotBinary,
   TestChangedRowsCarrySize). Live-refresh KHÔNG churn: snapshot thứ hai giống hệt → `syncPreview` trả
   nil, `srcWidth` giữ nguyên (TestChangesRefreshNoPreviewChurn); change mới → xuất hiện trong list
   (TestChangesRefreshPicksUpNewChange).
5. Row render `<badge> <relpath> <delta>`, width ≤ pane (`changes_test.go`
   TestChangesViewRendersBadgeRelpathDelta, TestChangesRowWidthFits); empty pane `(no changes)`
   (TestChangesEmptyListPanePlaceholder); status bar hint move/open/back, KHÔNG rename/delete
   (TestChangesStatusBarHints).
6. Palette "view changes" qua full flow → modeChanges + list; ngoài repo → status giải thích
   (`commands_test.go` TestCommandViewChanges*).
7. Telemetry: `c` → `action.changes_view_open`; Enter-to-file → `action.changes_jump {rel}`
   (`changes_test.go` TestChangesTelemetry).
8. Keymap: `c` match Changes, không collide binding nào khác (`changes_test.go` TestChangesKeyBinding).
9. Dogfood T4 FLIPPED: `c` lists all N changes một keypress, aggregate chứa root + nested change
   (`zz_dogfood_test.go` T4); T1 friction log ghi single-key path coexist với ● breadcrumb.
   ĐÃ VERIFY ✅ 2026-05-30: `T4 achievable=true keystrokes=1 … list=[README.md scratch.tmp src/app.go]`.
10. `go build -o lazyexplorer . && go vet ./... && go test ./...` xanh; `go test -race ./...` xanh.

## 7. Task breakdown

- [x] **T1 — Keybind.** Field `Changes` + `c` binding (nhóm Modes) + `fullHelp` Modes. *(keys.go, view.go)*
- [x] **T2 — Mode enum + predicate.** `modeChanges` + `flatListMode()`; saved-state fields. *(model.go)*
- [x] **T3 — Derive list.** `changedRows` + `rootRelFromRepoRel` inverse prefix map. *(changes.go)*
- [x] **T4 — Transitions.** enter/refresh/exit/openResult/updateChanges. *(changes.go)*
- [x] **T5 — Base-resolution share.** `previewBaseDir`/`renderList`/`refreshPreview` → `flatListMode()`. *(model.go, view.go)*
- [x] **T6 — Wiring.** `case km.Changes`; `case modeChanges` route; `refreshChanges` trên git-apply. *(model.go)*
- [x] **T7 — View.** modeChanges status branch; empty-pane `(no changes)`. *(view.go)*
- [x] **T8 — Palette twin.** "view changes" command; `exitCommandPalette` giữ command-set mode. *(commands.go, palette.go)*
- [x] **T9 — Dogfood reframe.** T4 flip sang /goal proof; T1 friction log update. *(zz_dogfood_test.go)*
- [x] **T10 — Verify.** Gate + race xanh; visual verdict trên ảnh changes list (màu/align/truncation).

## 8. Files chạm tới

| File | Thay đổi |
|------|----------|
| `keys.go` | Field `Changes` + `c` binding (nhóm Modes) |
| `model.go` | `modeChanges` enum + `flatListMode()`; saved-state fields; `case km.Changes`; `case modeChanges` route; `refreshChanges` trên `gitRefreshedMsg`; base-resolution → `flatListMode()` |
| `changes.go` | (mới) `changedRows`, `rootRelFromRepoRel`, `changesBaseDir`, enter/refresh/exit/openResult/updateChanges |
| `view.go` | `renderList` base → `previewBaseDir()` + empty `(no changes)`; `renderStatus` modeChanges branch; `fullHelp` Modes thêm `km.Changes` |
| `commands.go` | "view changes" command |
| `palette.go` | `exitCommandPalette` giữ command-set mode |
| `changes_test.go` | (mới) derive/transition/jump/render/telemetry/keymap tests |
| `commands_test.go` | TestCommandViewChanges + no-op-outside-repo |
| `zz_dogfood_test.go` | T4 flip sang /goal proof; T1 friction log update |
| `docs/prd-changed-only-view.md` | PRD này |
