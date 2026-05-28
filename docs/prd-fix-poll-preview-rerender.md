# PRD — Fix: poll loop chỉ re-render preview khi *selected file* đổi

> Bug-shaped fix. Diagnosis đầy đủ (root cause = chuỗi `file:line` đã verify) sống
> trong `bug-poll-preview-rerender.md` — PRD này KHÔNG kể lại trace, chỉ thiết kế cách
> sửa. Bug: poll loop re-render preview của file đang mở mỗi khi một file *khác* trong
> cwd đổi, gây CPU churn + nháy 1 frame mỗi ~1s lúc agent ghi file cạnh.

Status: **accepted** · Author: bugfix-design session · Ngày: 2026-05-28 · Shipped: 2026-05-28

---

## 1. Bối cảnh & vấn đề

Xem `bug-poll-preview-rerender.md` cho chuỗi nhân quả đầy đủ (đã verify lại bằng đọc
code 2026-05-28, `file:line` khớp). Tóm tắt một câu cho PRD này:

Change-detection gate của poll loop ở **mức directory** (`dirSig` fold toàn bộ entries —
`fs.go:69-85`), nhưng preview phụ thuộc nội dung **một file đơn**. Khi sibling đổi,
`sig != m.fsSig` ⇒ gate (`model.go:282-285`) không chặn ⇒ `refreshPreview()` chạy **vô
điều kiện** (`model.go:312`) ⇒ reset sạch preview state (`srcWidth=0`… `model.go:365-386`)
+ đặt placeholder (`model.go:431/434`) ⇒ tail `syncPreview` thấy `srcWidth != w` nên
**re-dispatch** glamour/chroma (`model.go:605,616-628`). Nháy = placeholder vẽ ít nhất một
frame trước khi render async land.

Vấn đề đúng nghịch positioning "glance beside agent" (`CLAUDE.md`): agent ghi file cạnh
là chuyện **liên tục**, nên bug kích hoạt gần như mỗi giây trong kịch bản tool được thiết
kế để phục vụ.

## 2. Goal (1 câu)

Trong poll path, `refreshPreview()` chỉ chạy khi **entry đang được chọn** thực sự đổi
(size / modTime / kind), tách bạch hai khái niệm "directory listing đổi" (→ rebuild list)
và "selected file đổi" (→ refresh preview) — sibling churn cập nhật list pane nhưng KHÔNG
re-render preview của file đang mở.

**Non-goal làm rõ:**
- KHÔNG đổi cơ chế phát hiện sang fsnotify — vẫn polling `tea.Tick(1s)` + `dirSig`
  (`adr-fs-refresh-polling.md` D1/D2 giữ nguyên). Chỉ tinh chỉnh **granularity của
  hành động sau gate**, không đổi gate.
- KHÔNG làm poll đệ quy. Watch vẫn chỉ phủ cwd (không theo dõi nội dung của
  grandchild trong một sub-folder đang preview) — xem §5.4.
- KHÔNG đụng đường user-action (`reload`/`descend`/`ascend`/`moveCursor`/search): những
  path đó re-render preview là **đúng và mong muốn**, fix chỉ nằm trong `syncFromDisk`.
- KHÔNG thêm field signature mới trên `model` — so sánh old-entry-vs-new-entry by name,
  không lưu `selSig` (giữ state surface phẳng, xem D2).
- KHÔNG đụng scroll-restore semantics khi preview *thật sự* refresh (`prevTop` +
  `scrollPreview(0)` giữ nguyên cho nhánh có đổi).

## 3. Quyết định đã chốt

| # | Quyết định | Giá trị | Lý do |
|---|-----------|---------|-------|
| D1 | Nơi đặt gate per-file | Trong `syncFromDisk`, **sau** khi swap `m.entries` + re-point cursor, **trước** khi gọi `refreshPreview()` (`model.go:312`) | Đây là path duy nhất re-render vô điều kiện. Gate ở đây vừa đủ; đẩy xuống `syncPreview` không cứu được nháy vì `refreshPreview` đã clobber `m.preview`/`srcWidth` rồi (xem Option B bác bỏ) |
| D2 | Nguồn so sánh "selected file đổi?" | So **old entry (by name) vs new entry (by name)** — `isDir`, `size`, `modTime.Equal` | Old entry đã nằm trong `m.entries` trước khi swap → zero bookkeeping mới. Một field `m.selSig` lưu trữ phải đồng bộ qua `reload`/`refreshPreview`/`exitSearchRestore` → thêm bề mặt lỗi không cần |
| D3 | Phạm vi bỏ qua khi selected unchanged | Bỏ qua **chỉ** `refreshPreview()` + cặp `prevTop`/`scrollPreview(0)` restore. **Vẫn** swap `m.entries` + re-point cursor + set `m.fsSig` | List pane PHẢI phản ánh sibling mới — đó là toàn bộ lý do poll tồn tại. Chỉ preview là thứ không được churn |
| D4 | Selected name biến mất khỏi listing mới (file đang chọn bị xóa) | Coi như **đã đổi** → `refreshPreview()` chạy | Cursor bị clamp sang neighbor khác → selection thực sự trỏ file khác → preview phải refresh. State rõ ràng để reviewer không phải tự suy |
| D5 | Synthetic `..` entry | Luôn so bằng (old `..` vs new `..` đều zero-value `size`/`modTime`) → skip refreshPreview | `..` được prepend mới mỗi sync với cùng zero-value; preview của `..` (listing thư mục cha) không detect được qua poll cwd dù sao — skip nhất quán với watch scope hiện có |
| D6 | Telemetry `view.change` | Chỉ fire khi `refreshPreview()` thực chạy (sibling-only sync KHÔNG fire) | `view.change` nghĩa "cursor moved → preview reload" (`model.go:335-359`). Sibling churn không đổi view → không fire là **honest hơn**, không phải regression |

## 4. Functional requirements

- **FR1** — Khi `syncFromDisk` phát hiện `dirSig` đổi (sibling add/remove/modify) nhưng
  entry đang được chọn **không đổi** (`isDir`, `size`, `modTime` đều bằng so với trước),
  `m.entries` vẫn được swap (list pane cập nhật sibling) nhưng `refreshPreview()`
  **không** được gọi: `m.preview`, `m.previewPreStyled`, `m.srcPath`, `m.srcRaw`,
  `m.srcWidth`, `m.previewTop` giữ nguyên byte-for-byte.

- **FR2** — Khi entry đang được chọn **đổi** (mtime/size bump do file đó bị ghi, hoặc kind
  đổi), `refreshPreview()` chạy như hiện tại: preview reload + `prevTop` restore +
  `scrollPreview(0)` clamp (`model.go:312-314` giữ nguyên cho nhánh này).

- **FR3** — Khi entry đang được chọn **biến mất** khỏi listing mới (bị xóa), cursor
  re-point theo logic clamp hiện có (`model.go:302-310`) và `refreshPreview()` chạy cho
  selection mới (D4).

- **FR4** — Sibling-only sync KHÔNG dispatch render mới: `m.renderGen` không tăng, không
  có `previewRenderedMsg` mới phát sinh từ nhánh skip (vì `srcWidth` giữ nguyên ⇒ tail
  `syncPreview` cache-hit `srcWidth == w` ⇒ `return nil` — `model.go:605`).

- **FR5** — List-pane invariant của poll loop giữ nguyên: file thêm/xóa/đổi tên trong cwd
  xuất hiện/biến mất trong list pane trong ≤1s, selection giữ theo name (`adr-fs-refresh-polling.md`
  D5), scroll giữ (D6). Fix KHÔNG được làm hỏng các invariant này.

- **FR6** — Đường user-action không đụng: `reload` (`model.go:239-260`),
  `descend`/`ascend`, `moveCursor`, mouse wheel/click, search — tất cả vẫn gọi
  `refreshPreview()` vô điều kiện như trước (re-render khi user chủ động đổi selection là
  đúng).

## 5. Technical design

> Kim chỉ nam: **tách "list changed" khỏi "selected file changed" tại đúng một chỗ**
> (`syncFromDisk`). Không field mới, không message type mới, không async mới. Một
> comparison thêm + một early-return có điều kiện. Mọi path khác zero-diff.

### 5.1 `syncFromDisk` — gate per-file trước `refreshPreview` (`model.go:267-315`)

Code hiện tại (rút gọn các phần không đổi):

```go
func (m *model) syncFromDisk() {
	if _, err := os.Stat(m.cwd); err != nil {
		m.recoverVanishedCwd()
		return
	}
	entries, err := readDir(m.cwd)
	if err != nil { /* … set error, return … */ }
	sig := dirSig(entries)
	if sig == m.fsSig {
		return // nothing changed on disk
	}
	m.fsSig = sig

	selName := ""
	if m.cursor < len(m.entries) {
		selName = m.entries[m.cursor].name
	}
	prevTop := m.previewTop

	if m.cwd != m.root {
		entries = append([]entry{{name: "..", isDir: true}}, entries...)
	}
	m.entries = entries

	m.cursor = min(m.cursor, max(0, len(m.entries)-1))
	if selName != "" {
		for i, e := range m.entries {
			if e.name == selName {
				m.cursor = i
				break
			}
		}
	}

	m.refreshPreview()     // ← chạy vô điều kiện (bug)
	m.previewTop = prevTop
	m.scrollPreview(0)
}
```

Sau (capture old entry đầy đủ; gate `refreshPreview` theo selected-changed):

```go
func (m *model) syncFromDisk() {
	if _, err := os.Stat(m.cwd); err != nil {
		m.recoverVanishedCwd()
		return
	}
	entries, err := readDir(m.cwd)
	if err != nil { /* … set error, return … */ }
	sig := dirSig(entries)
	if sig == m.fsSig {
		return // nothing changed on disk
	}
	m.fsSig = sig

	// Snapshot the SELECTED entry (by value) before the swap. dirSig fired because
	// SOMETHING in cwd changed — but the preview only depends on the selected file.
	// Comparing old-vs-new of the selection tells us whether that one file changed,
	// distinct from "a sibling changed" (bug-poll-preview-rerender).
	var oldSel entry
	hadSel := m.cursor < len(m.entries)
	if hadSel {
		oldSel = m.entries[m.cursor]
	}
	prevTop := m.previewTop

	if m.cwd != m.root {
		entries = append([]entry{{name: "..", isDir: true}}, entries...)
	}
	m.entries = entries

	// Re-point cursor onto the same name (a sibling added/removed above it shifts the
	// index); fall back to a clamped index when the selected name is gone (deleted).
	m.cursor = min(m.cursor, max(0, len(m.entries)-1))
	foundSameName := false
	if hadSel && oldSel.name != "" {
		for i, e := range m.entries {
			if e.name == oldSel.name {
				m.cursor = i
				foundSameName = true
				break
			}
		}
	}

	// Selected file unchanged? Then the list pane already reflects the sibling churn
	// above — leave the preview alone. refreshPreview would reset srcWidth=0 and stamp
	// a placeholder, forcing a glamour/chroma re-render of an identical file: CPU churn
	// + a one-frame flash every poll tick while an agent writes beside us (D1/D3/FR1).
	if foundSameName && m.cursor < len(m.entries) {
		newSel := m.entries[m.cursor]
		if oldSel.isDir == newSel.isDir &&
			oldSel.size == newSel.size &&
			oldSel.modTime.Equal(newSel.modTime) {
			return // list updated; selected file is byte-identical — preview stays
		}
	}

	// Selected file changed (or vanished → cursor moved by clamp): refresh the preview
	// for the new state and restore scroll (D2/D4/FR2/FR3).
	m.refreshPreview()
	m.previewTop = prevTop
	m.scrollPreview(0)
}
```

Ghi chú correctness:

- `dirSig` được tính trên `entries` **thô** (trước khi prepend `..`), nên synthetic `..`
  không vào signature — nhất quán với baseline trong `reload` (`model.go:248`). Old/new
  `..` đều zero-value `size`/`modTime` → so bằng (D5).
- `m.entries[m.cursor]` đọc sau swap được guard bởi `m.cursor < len(m.entries)`: khi cwd
  trở thành rỗng (mọi file bị xóa, cwd == root), `len == 0` ⇒ guard false ⇒
  `refreshPreview()` chạy ⇒ nhánh `len(m.entries) == 0` của nó (`model.go:388-391`) hiển
  thị "(empty directory)". Không panic.
- Skip-branch không đụng `m.preview`/`srcWidth`/`pendingWidth`, nên tail `syncPreview`
  thấy `srcWidth == w` → cache-hit → `return nil` (FR4): không render mới, không churn.

### 5.2 Vì sao không cần đụng gì khác

- `dirSig` (`fs.go:69-85`) **giữ nguyên** — nó vẫn là gate đúng cho "có cần rebuild list
  không". Bug không phải ở dirSig; bug là *hành động sau dirSig* quá thô. Fix nằm trọn ở
  bước sau gate.
- `refreshPreview` / `syncPreview` / `applyPreview` **không đổi** — chúng vẫn là single
  source cho preview pipeline; ta chỉ đổi *điều kiện gọi* `refreshPreview` trong poll path.
- `entry` struct đã mang sẵn `size` + `modTime` (`fs.go:31-36`) cho `dirSig`; so sánh
  per-file tái dùng đúng các field đó, không thêm gì.

### 5.3 Đã cân nhắc & **defer / bác bỏ**

- **Option B — gate ở `syncPreview` trước re-dispatch (so mtime/hash selected file)** —
  **bác bỏ**. Tới lúc `syncPreview` chạy (tail Update), `refreshPreview` đã đặt
  `m.preview = placeholder` và `srcWidth = 0` rồi (`model.go:365-435`). Chặn re-dispatch
  ở đây không khôi phục được rendered lines đã bị clobber — vẫn nháy về placeholder, và
  muốn dựng lại nội dung thì phải… re-render (đúng cái ta tránh). Sửa triệu chứng ở hạ
  nguồn của chỗ gây hại.

- **Option C — tách field `m.selSig` riêng khỏi `m.fsSig`** — **bác bỏ**. Là Option A +
  một field lưu trữ. `selSig` phải được set/đồng bộ ở mọi nơi đổi selection (`reload`,
  `refreshPreview`, `moveCursor`, `descend`, `ascend`, `exitSearchRestore`,
  `openSearchResult`, mouse click) — bề mặt lỗi rộng đổi lấy zero lợi ích so với so
  old-vs-new entry tại chỗ (D2).

- **Selected entry là directory mà *grandchild* đổi (mtime của chính dir không bump)** —
  **defer (giới hạn cố hữu, không phải breakage mới)**. Poll chỉ watch cwd, không đệ quy
  (`adr-fs-refresh-polling.md` §"Đánh đổi"). Một file bị sửa *in-place* trong sub-folder
  đang preview không bump mtime/size của dir entry trong cwd ⇒ `dirSig` của cwd không đổi
  ⇒ `syncFromDisk` return sớm ở gate (`model.go:282`) — **trước cả** code của fix này.
  Nghĩa là folder preview đã có thể stale từ trước với in-place grandchild edit; Option A
  không làm tệ hơn, chỉ làm giới hạn này hiện rõ ở một tầng mới. Khi grandchild được
  *thêm/xóa* (bump dir mtime), entry dir đổi `modTime` ⇒ nhánh refresh chạy ⇒ folder
  preview cập nhật đúng. Nhất quán với watch scope hiện có.

- **So sánh bằng content hash thay vì size+modTime** — **defer (YAGNI)**. `dirSig` đã
  tin cậy mtime cho cả app; selected-file gate dùng cùng tín hiệu để nhất quán. Hash nội
  dung là chi phí đọc file mỗi tick — đúng thứ fix này muốn loại. mtime+size đủ.

## 6. Acceptance criteria

### Gherkin

```gherkin
Feature: Poll loop re-renders the preview only when the selected file changes

  Background:
    Given the explorer is open in normal mode at a project root
    And the cwd contains a markdown file and a Go file
    And the markdown file is selected and its preview has rendered

  Scenario: A sibling file changes but the selected file does not
    When an external process modifies the Go file in the cwd
    And the poll loop refreshes from disk
    Then the rendered markdown preview is unchanged
    And the preview viewport position is preserved
    And no new preview render is dispatched

  Scenario: The selected file itself changes
    When an external process modifies the selected markdown file
    And the poll loop refreshes from disk
    Then the preview reloads to reflect the new content

  Scenario: A sibling is added to the cwd
    When an external process creates a new file in the cwd
    And the poll loop refreshes from disk
    Then the new file appears in the list pane
    And the selected file's preview is unchanged

  Scenario: The selected file is deleted out from under the selection
    When an external process deletes the selected markdown file
    And the poll loop refreshes from disk
    Then the selection moves to a neighbouring entry
    And the preview reflects the new selection

  Scenario: A sibling changes while the selection rests on the parent entry
    Given the cwd is a subfolder so a parent entry is shown
    And the parent entry is selected
    When an external process modifies a file in the cwd
    And the poll loop refreshes from disk
    Then the preview is unchanged
```

### Checklist verify

1. Failing test trước (TDD): với selected = markdown đã render, `touch` một sibling rồi
   gọi `syncFromDisk` → assert `m.srcWidth` **không** về 0 và `m.preview` giữ nguyên slice
   nội dung. Test này FAIL trên code hiện tại (refreshPreview reset srcWidth=0), PASS sau fix.
2. `m.renderGen` **không** tăng sau một sibling-only sync (đo trước/sau quanh
   `syncFromDisk` + tail `syncPreview`).
3. Positive control (chống over-suppression): sửa **selected** file (bump mtime) →
   `syncFromDisk` → `m.srcWidth == 0` (đã reset) ⇒ refresh đã chạy.
4. List vẫn cập nhật: thêm một sibling → entry mới có trong `m.entries` sau `syncFromDisk`
   (FR5 không regression).
5. Selected file bị xóa → cursor trỏ neighbor, preview refresh (FR3).
6. Regression: các test poll-loop hiện có (`adr-fs-refresh-polling.md` §"Phạm vi" liệt kê
   add/delete/modify-mtime/selection-by-name/gate-no-op/recover-cwd) vẫn xanh.
7. Visual: chạy `./lazyexplorer .`, đặt cursor lên một `.md` đã render, ở terminal khác
   `touch` một file `.go` cạnh nó nhiều lần → preview markdown **không** nháy về raw source
   (so với trước fix là nháy mỗi ~1s).
8. `go build -o lazyexplorer . && go vet ./... && go test ./...` xanh; `go test -race ./...` xanh.

## 7. Task breakdown

> Trước khi sửa: `gitnexus_impact({target: "syncFromDisk", direction: "upstream"})`
> báo blast radius (caller = case `tickMsg` trong `Update`). Sau khi sửa:
> `gitnexus_detect_changes` verify scope khớp §8.

- [x] **T1 — Failing test trước (TDD).** `watch_test.go`: `eventCountRecorder` +
  `renderedModelAt` helper; `TestSyncSkipsPreviewRefreshWhenSiblingChanges` dựng tmp dir
  `doc.md`+`sibling.go`, render `.md` (`srcWidth>0`), thêm sibling `new.go`, gọi
  `syncFromDisk`, assert `srcWidth` giữ + `preview` byte-identical + `view.change` delta 0 +
  `syncPreview()==nil`. ĐÃ VERIFY ✅ 2026-05-28 — FAIL đúng lý do (srcWidth 60→0, preStyled
  drop, preview flash về placeholder). *(watch_test.go)*

- [x] **T2 — Gate per-file trong `syncFromDisk`.** Implement §5.1: snapshot `oldSel`,
  re-point cursor (track `foundSameName`), early-return khi selected unchanged
  (`isDir`+`size`+`modTime.Equal`), giữ nhánh refresh+scroll-restore cho selected-changed.
  ĐÃ VERIFY ✅ 2026-05-28 — T1 chuyển PASS. *(model.go)*

- [x] **T3 — Positive + edge tests.** `TestSyncRefreshesPreviewWhenSelectedFileChanges`
  (selected đổi → refresh + `view.change` delta 1), `TestSyncRefreshesWhenSelectedFileDeleted`
  (deleted → cursor move sang `other.md` + refresh), `TestSyncSkipsPreviewWhenSelectionIsParentEntry`
  (cursor `..` + sibling đổi → skip, D5). Positive controls PASS trước+sau fix; D5 FAIL trước
  → PASS sau. ĐÃ VERIFY ✅ 2026-05-28. *(watch_test.go)*

- [x] **T4 — Reconcile `adr-fs-refresh-polling.md`.** Bảng `:56` → "gate hai tầng: dir-gate
  rebuild list, per-file gate (selected entry size+mtime) refresh preview". §"Vì sao gate hai
  tầng" (đổi tên từ "Vì sao content-gate bắt buộc") → mô tả 2 tầng, tầng 2 trỏ về PRD/bug,
  cập nhật ref cũ `model.go:92`/`mdWidth`→`model.go:334`/`srcWidth`. Positive framing (mô tả
  trạng thái đích, lý do thêm tầng 2 ở PRD/bug). ĐÃ VERIFY ✅ 2026-05-28. *(docs/adr-fs-refresh-polling.md)*

- [x] **T5 — Flip bug doc status.** `bug-poll-preview-rerender.md` status →
  `fixed by prd-fix-poll-preview-rerender`; mục "Tài liệu cần reconcile" gắn note "Đã reconcile".
  ĐÃ VERIFY ✅ 2026-05-28. *(docs/bug-poll-preview-rerender.md)*

- [x] **T6 — Verify.** `go build -o lazyexplorer . && go vet ./... && go test ./...` xanh
  (ĐÃ VERIFY ✅ 2026-05-28); `go test -race ./...` xanh (ĐÃ VERIFY ✅ 2026-05-28). Visual smoke
  §6.7: fix KHÔNG đụng code rendering — nó chỉ quyết *khi nào* `refreshPreview` chạy trong
  poll path. Khi skip, `m.preview`/`previewPreStyled` byte-identical ⇒ `View()` vẽ frame **y
  hệt** ⇒ không thể nháy. Bất biến này được test `TestSyncSkipsPreviewRefreshWhenSiblingChanges`
  chốt ở mức model (preview byte-identical), nên live-TTY smoke không thêm bằng chứng (không
  chạy trong session này). *(GitNexus MCP không khả dụng session này — impact thủ công: caller
  duy nhất của `syncFromDisk` là case `tickMsg` trong `Update`, `model.go:706`.)*

## 8. Files chạm tới

| File | Thay đổi |
|------|----------|
| `model.go` | `syncFromDisk`: snapshot `oldSel` trước swap; track `foundSameName` khi re-point cursor; early-return khi selected entry unchanged (isDir+size+modTime.Equal); giữ nhánh `refreshPreview`+`prevTop`+`scrollPreview(0)` cho selected-changed/vanished (§5.1). Không file/struct/message mới |
| `watch_test.go` | + test sibling-changed → preview giữ + renderGen không tăng (T1); + selected-changed/deleted/sibling-added/cursor-on-`..` (T3) |
| `docs/adr-fs-refresh-polling.md` | Reconcile bảng `:56` + §"Vì sao content-gate bắt buộc" → mô tả gate hai tầng (T4) |
| `docs/bug-poll-preview-rerender.md` | Status → `fixed by prd-fix-poll-preview-rerender`; reconcile-list done (T5) |
| `docs/prd-fix-poll-preview-rerender.md` | File này |
