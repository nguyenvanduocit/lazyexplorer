# PRD — Tô mờ & dìm entry bị gitignore xuống đáy list

Status: **draft / chờ review** · Author: phiên Opus 4.8 (1M) · Ngày: 2026-06-04 · Partial 2026-07-01: **sink** (`partitionIgnored`/`orderEntries`, §5.2) đã land + test; **dimming màu (§5.3, T5/T6) CHƯA implement** — `renderEntryRow` chưa nhận `ignored`, entry ignored vẫn tô `dirStyle`/`fileStyle` như thường (chỉ chìm đáy, không xám).

---

## 1. Bối cảnh & vấn đề

lazyexplorer là companion glance-bên-cạnh-agent: user mở nó cạnh Claude Code để liếc và
điều hướng project tree. Nhưng list pane hiện tô **mọi** thư mục như nhau — `node_modules/`,
`dist/`, `tmp/`, `.claude/` sáng rực bằng `dirStyle` (xanh đậm, `theme.go:74`) y hệt `src/`
hay `docs/`. Trong một repo thật, những thư mục build-artifact / tooling bị gitignore chiếm
phần lớn các dòng đầu list (vì `readDir` sắp dirs trước, alpha — `fs.go:54-59`), đẩy các thư
mục project thực sự xuống dưới và làm loãng cái liếc.

Hiện trạng:

- `readDir` (`fs.go:40`) liệt kê một thư mục: dirs trước (alpha), rồi files (alpha). Hoàn
  toàn không biết gì về gitignore.
- Layer git (`git.go`) đã là nguồn git authoritative: nó shell ra `git`, parse porcelain,
  trả `gitState{repoRoot, changes, dirtyDirs}` (`git.go:101-105`) cho model đọc mỗi frame.
  Nhưng nó **chỉ** thu thập thay đổi (M/?/A/D/R/!), chưa hề biết "ignored".
- `walkTree` (search mode, `fs.go:134`) có honor `.gitignore` qua lib `go-gitignore`
  (`fs.go:141`, `fs.go:167-177`) — nhưng đó là đường riêng của search, list pane không dùng.
- Render: `renderEntryRow` (`view.go:496`) là single source of truth cho một dòng listing;
  dir → `dirStyle`, file → `fileStyle` (`view.go:509-512`). `dimStyle` (xám `colDim`
  `#6C757D`, `theme.go:20`/`theme.go:76`) đã có sẵn, đang dùng cho ● rollup và delta mờ.

Vấn đề: không có tín hiệu thị giác nào tách "thư mục/file bị git bỏ qua" khỏi "code thực
sự của project", và chúng còn nằm lẫn ở trên cùng.

## 2. Goal (1 câu)

Trong một git repo, **mọi entry bị git ignore (thư mục lẫn file) được render xám và dìm
xuống đáy list pane**, để cái liếc tập trung vào code thật của project.

**Non-goal làm rõ:**

- Không ẩn entry bị ignore (vẫn là file manager — user phải thấy được `node_modules/` để
  bước vào nếu cần). Chỉ tô mờ + dìm.
- Không thêm keybind / mode / panel mới. Đây thuần là thay đổi màu + thứ tự, zero UI chrome
  thêm (đúng trần simplicity của CLAUDE.md).
- Không đụng search/changes (đã ignored-free by construction — xem FR7).
- Không thêm màu mới vào palette — reuse `dimStyle` sẵn có.

## 3. Quyết định đã chốt

| # | Quyết định | Giá trị | Lý do |
|---|-----------|---------|-------|
| D1 | Nguồn detection | `git status --porcelain=v1 -z --ignored` **KHÔNG** `-uall`, call **riêng** trong `collectGitState` | Đã verify (`2026-06-04`): với `-uall`, ignored dir bị **expand** thành từng file (375 records, mất entry thư mục — `.claude/` thành `.claude/hooks/...`). Bỏ `-uall` thu gọn về `!! .claude/`, `!! tmp/`, `!! dist/` (12 records) — đúng mức thư mục cần tô. Call hiện có vẫn cần `-uall` cho untracked rollup, nên đây là call thứ hai độc lập. |
| D2 | Dùng `git status`, **không** `go-gitignore`/`git check-ignore` | git status là nguồn chuẩn | git status loại đúng một file **đang tracked** dù nó khớp pattern `.gitignore` (git không ignore thứ đã track). Pure pattern-matcher (lib go-gitignore, check-ignore) sẽ tô **nhầm** file đó thành ignored. Bonus: git status honor luôn nested `.gitignore`, global excludes, `.git/info/exclude`, negation — miễn phí, đúng ngữ nghĩa. |
| D3 | Lưu trữ | `ignored map[string]bool` trong `gitState`, key repo-rel slash (strip trailing `/`) | Đồng dạng `changes`/`dirtyDirs` (`git.go:101-105`); lookup O(1) qua `repoRelKey` (`model.go:399`) như `indicatorFor` đã làm — không phát minh đường mới. |
| D4 | Scope | Cả **thư mục lẫn file** bị ignore | User xác nhận (2026-06-04). Predicate partition type-agnostic — một binary bị ignore cạnh `node_modules/` cũng mờ đi thay vì sáng lạc lõng. |
| D5 | Vị trí dìm | **Đáy tuyệt đối** của list (dưới cả file thường); stable partition giữ dirs-first-alpha trong từng nhóm | "Order ở dưới cùng của list" → ignored chìm dưới mọi entry không-ignore. Stable partition giữ nguyên thứ tự `readDir` trong mỗi nhóm nên dirs-first-alpha vẫn đúng cho cả hai nhóm. |
| D6 | Màu | `dimStyle` (`colDim #6C757D`) cho **name**, chỉ inactive rows | Reuse palette, không thêm màu. Cursor row giữ accent `cursorActiveStyle` (gray chỉ áp dòng không-active) — type của file đang chọn đọc từ preview pane, không cần badge trên dòng cursor. |
| D7 | Ancestor-match | Entry là ignored nếu **nó hoặc bất kỳ tổ tiên** nằm trong `!!` set | `!! tmp/` nghĩa là "tmp và mọi thứ bên dưới". cd vào `tmp/` → entry là `tmp/foo` (không phải record `!!` literal); ancestor-match khiến "bên trong thư mục ignored, mọi thứ đều xám" rơi ra tự nhiên — đúng mental model. Tiền lệ: `markAncestors` (`git.go:474`). |
| D8 | dirSig giữ thứ tự canonical | `dirSig` tính trên kết quả `readDir` (pre-order); ordering là **display step** áp lên `m.entries` SAU khi `dirSig` đã chốt | Poll loop gate trên `dirSig` (`model.go:529-533`); nếu ordering trộn vào trước, một thay đổi ignored-set sẽ churn order và làm poll thrash. Tách ra giữ gate order-independent. |
| D9 | Reorder reactive chỉ ở `modeNormal` | git-land reorder skip khi `flatListMode()` | Search/changes là flat-list ignored-free (FR7) — reorder vô nghĩa và sẽ phá thứ tự fuzzy-rank / aggregate. |
| D10 | Folder preview pane | Coloring **live** mỗi frame; ordering **snapshot** tại `refreshPreview` | Coloring đọc `m.git` mỗi frame (nhất quán list ↔ preview tức thì). Ordering của `previewEntries` chốt lúc select folder (git.ignored đã load sau startup) — không re-order preview trên mỗi git-land (defer, §5.4). |

## 4. Functional requirements

- **FR1 — Tô mờ.** Trong git repo, một entry (dir hoặc file) git ignore được render name
  bằng `dimStyle` thay cho `dirStyle`/`fileStyle` ở list pane. Áp dòng inactive; dòng cursor
  giữ `cursorActiveStyle` (accent).
- **FR2 — Dìm đáy.** Entry bị ignore được sắp xuống **đáy** list, dưới mọi entry không-ignore.
  Trong nhóm ignored, thứ tự giữ dirs-first-alpha. Synthetic `..` luôn ở top (không bao giờ
  coi là ignored).
- **FR3 — Ancestor-match.** Một entry nằm bên trong một thư mục bị ignore cũng được coi là
  ignored (xám). Ví dụ: cd vào `tmp/` → mọi entry trong đó render xám.
- **FR4 — Tracked file không bị nhầm.** Một file **đang tracked** dù tên khớp pattern trong
  `.gitignore` vẫn render màu thường (git không ignore thứ đã track).
- **FR5 — Nhất quán hai pane.** Folder preview pane áp cùng coloring qua `renderEntryRow`
  (`view.go:637-638`) — một entry render byte-identical (trừ accent dòng cursor) ở list pane
  và folder preview.
- **FR6 — Ngoài repo: OFF.** Khi `repoRoot == ""` (không phải git repo) hoặc `ignored` rỗng,
  feature không tô, không reorder — list giữ nguyên dirs-first-alpha của `readDir`.
- **FR7 — Flat-list mode không đụng.** `modeSearch`/`modeChanges` không reorder và không cần
  tô: `walkTree` đã loại ignored khi build kết quả search (`fs.go:167-177`), còn changes là
  tập git-change (ignored không bao giờ là change). Cả hai ignored-free by construction.
- **FR8 — Disjoint với badge.** Entry bị ignore không bao giờ mang badge thay đổi (M/?/A/D/R/!)
  hay ● rollup — git status không report ignored như change, và `dirtyDirs` (ancestor của
  change) không thể chứa thư mục ignored.
- **FR9 — Degrade.** `git status --ignored` lỗi/timeout (kế thừa `gitCmdTimeout`, FR10 của
  prd-git-change-indicator) → `ignored` rỗng → không tô, không reorder, frame không vỡ.
- **FR10 — Reorder reactive.** Khi git snapshot mới land (`gitRefreshedMsg`, `model.go:1117`)
  ở `modeNormal` và `ignored` đổi, list re-order, giữ cursor theo **tên** (không theo index).

## 5. Technical design

Kim chỉ nam: **`ignored` là map thứ ba trong `gitState`, sống cạnh `changes`/`dirtyDirs` và
được lookup qua đúng `repoRelKey` mà badge đang dùng.** Detection là 20% dễ; rủi ro thiết kế
nằm ở lookup (ancestor-match) và ordering (stable partition + cursor-by-name dưới refresh
async). Ba layer tách bạch: git (thu thập), model (predicate + thứ tự), view (màu).

### 5.1 Git layer — thu thập ignored (`git.go`)

1. `gitState` thêm field `ignored map[string]bool` (`git.go:101-105`). `collectGitState`
   (`git.go:160`) khởi tạo nó như `changes`/`dirtyDirs`.
2. Sau bước status hiện có (`git.go:169`, vẫn `-uall` cho change rollup), thêm **call thứ
   hai**:

   ```go
   if ignOut, err := runGit(repoRoot, "status", "--porcelain=v1", "-z", "--ignored"); err == nil {
       parseIgnored(ignOut, st.ignored)
   }
   ```

   Lỗi → bỏ qua (degrade, FR9), `ignored` ở lại rỗng.
3. `parseIgnored(data []byte, ignored map[string]bool)`: dùng `splitNUL` (`git.go:199`), với
   mỗi field bắt đầu bằng `!!` lấy path (`f[3:]`), strip trailing `/`, `filepath.ToSlash`,
   set `ignored[path] = true`. Record `!!` không có rename source field nên không cần skip `i++`.
4. `func (s gitState) isIgnored(rel string) bool` — ancestor-walk (D7): trả true nếu `rel`
   hoặc bất kỳ prefix tổ tiên nào của nó nằm trong `s.ignored`. Cấu trúc đối xứng `markAncestors`
   (`git.go:474`) nhưng đi tra cứu thay vì ghi.

### 5.2 Model — predicate + ordering (`model.go`)

1. `func (m model) isIgnoredEntry(baseDir string, e entry) bool`: `..` → false; `repoRelKey`
   (`model.go:399`) → rel; `!ok` → false; trả `m.git.isIgnored(rel)`. Cùng resolver mà
   `indicatorFor` (`model.go:419`) dùng nên badge-key và ignored-key không bao giờ lệch.
2. `func (m model) partitionIgnored(entries []entry, base string) []entry`: nếu `repoRoot==""`
   hoặc `len(m.git.ignored)==0` → trả `entries` nguyên (no-op). Ngược lại **stable partition**:
   gom `keep` (không-ignored, giữ thứ tự) rồi `sink` (ignored, giữ thứ tự), trả `append(keep, sink...)`.
   Idempotent — gọi lại trên kết quả đã sắp cho ra cùng thứ tự.
3. `func (m *model) orderEntries()`: nếu `flatListMode()` → return (D9); ngược lại
   `m.entries = m.partitionIgnored(m.entries, m.cwd)`.
4. **Hook vào các đường:**
   - `reload` (`model.go:487`): gọi `orderEntries()` sau khi set `m.entries` (append `..`),
     trước khi clamp cursor. dirSig đã lấy từ `readDir` thô (`model.go:496`) nên không ảnh hưởng.
   - `syncFromDisk` (`model.go:515`): dirSig vẫn tính trên `readDir` thô (`model.go:529`,
     giữ D8). Sau khi set `m.entries` (`model.go:549`), gọi `orderEntries()` **trước** vòng
     seek-cursor-theo-tên (`model.go:556-564`) để seek tìm đúng index trong list đã sắp.
   - `gitRefreshedMsg` (`model.go:1117`, nhánh `msg.gen == m.gitGen`): nếu `m.mode == modeNormal`,
     snapshot tên entry đang chọn → `orderEntries()` → seek lại cursor theo tên (FR10). Idempotent
     nên gọi mỗi git-land an toàn; khi order không đổi, seek tìm lại đúng index cũ.
   - `refreshPreview` nhánh dir (`model.go:692-707`): `m.previewEntries =
     m.partitionIgnored(entries, full)` để folder preview cũng dìm ignored (D10 snapshot).

### 5.3 View — màu (`view.go`)

1. `renderEntryRow` (`view.go:496`) thêm tham số `ignored bool`. Nhánh `active` giữ nguyên
   (cursor row luôn accent, D6). Nhánh inactive: nếu `ignored` → `styleRow(name, dimStyle, ind, w)`
   thay cho `dirStyle`/`fileStyle`. (`ind` là nil cho entry ignored theo FR8, nên `styleRow`
   chỉ render tên xám.)
2. `renderList` (`view.go:407-409`): cạnh `ind := m.indicatorFor(base, e)`, thêm
   `ig := m.isIgnoredEntry(base, e)`, truyền vào `renderEntryRow`.
3. `renderPreview` nhánh dir (`view.go:637-638`): tương tự với base `m.previewDirPath`.

### Đã cân nhắc & defer khỏi v1

- **`go-gitignore` / `git check-ignore`** — bác bỏ (D2): pure matcher tô nhầm file tracked
  khớp pattern; lib chỉ honor một `.gitignore` (walkTree đã chịu hạn chế này). git status
  đúng hơn và còn miễn phí nested/global/exclude.
- **Self-match-only (không ancestor-walk)** — bác bỏ (D7): sẽ để entry bên trong `tmp/` sáng
  màu dù `tmp/` đã xám — sai mental model.
- **Re-order `previewEntries` trên mỗi git-land** — defer (D10): coloring preview đã live;
  thứ tự folder preview chốt lúc select là đủ (git.ignored đã load sau startup). Thêm reactive
  cho preview là độ phức tạp không tương xứng cho v1.
- **Một call status gộp cả change lẫn ignored** — bác bỏ: change cần `-uall` (expand untracked),
  ignored cần KHÔNG `-uall` (thu gọn dir). Hai nhu cầu xung khắc → hai call (D1). Call ignored
  không `-uall` không đệ quy vào dir ignored nên nhanh.
- **Lưu `ignored` thành field trên `entry`** — bác bỏ: sẽ làm bẩn `dirSig` (D8). ignored-ness
  là dữ liệu dẫn xuất từ git state, tính lúc render/order, không lưu trên struct.

## 6. Acceptance criteria

### Gherkin

```gherkin
Feature: Entry bị gitignore mờ đi và chìm xuống đáy list

  Background:
    Given lazyexplorer mở tại root của một git repo
    And repo có .gitignore liệt kê "node_modules/" và "build.log"

  Scenario: Thư mục bị ignore được tô xám và dìm xuống đáy
    Given thư mục hiện tại chứa "src/", "node_modules/" và "main.go"
    When list pane render
    Then "node_modules/" hiện màu mờ
    And "node_modules/" nằm dưới "src/" và "main.go" trong list

  Scenario: File bị ignore cũng mờ và chìm đáy
    Given thư mục hiện tại chứa "main.go" và "build.log"
    When list pane render
    Then "build.log" hiện màu mờ và nằm dưới "main.go"

  Scenario: File đang tracked khớp pattern vẫn màu thường
    Given "keep.log" đã được git add (tracked) dù pattern "*.log" có trong .gitignore
    When list pane render
    Then "keep.log" hiện màu thường, không bị dìm

  Scenario: Bên trong thư mục bị ignore, mọi entry đều mờ
    Given thư mục hiện tại là "node_modules/"
    When list pane render
    Then mọi entry trong list hiện màu mờ

  Scenario: Folder preview phản chiếu cùng cách tô của list
    Given con trỏ đứng trên một thư mục chứa "node_modules/"
    When folder preview render thư mục đó
    Then "node_modules/" trong preview mờ và chìm đáy y như khi mở thư mục đó ở list

  Scenario: Ngoài git repo không có gì đổi
    Given lazyexplorer mở tại một thư mục KHÔNG thuộc git repo
    When list pane render
    Then không entry nào bị tô mờ
    And thứ tự vẫn là thư mục trước, rồi file, theo alpha

  Scenario: Dòng cursor giữ highlight accent dù entry bị ignore
    Given con trỏ đứng trên "node_modules/"
    When list pane render
    Then dòng "node_modules/" hiện nền accent của cursor, không phải màu mờ
```

### Checklist verify

1. Parse: `git status --porcelain=v1 -z --ignored` với một repo seed → `ignored` chứa đúng
   key dir (đã strip `/`) và file root-level; không chứa record bị expand.
2. `isIgnored` ancestor-match: `isIgnored("tmp/a/b")` true khi chỉ `tmp` có trong set.
3. Stable partition: thứ tự nhóm keep và nhóm sink đều giữ dirs-first-alpha; `..` luôn index 0.
4. Idempotent: `partitionIgnored` chạy hai lần cho cùng kết quả (không thrash).
5. Cursor-by-name: sau git-land reorder, cursor vẫn trỏ đúng entry theo tên (không phải index).
6. Disjoint: một entry ignored không bao giờ đồng thời có badge/● (assert trên map).
7. dirSig bất biến với ignored-set: đổi `.gitignore` (không đổi file khác) không làm
   `syncFromDisk` thrash thứ tự qua poll gate.
8. Visual verdict (render-to-image + agent): entry ignored đọc rõ là "mờ/phụ" so với code
   thật; dòng cursor vẫn nổi accent; không vỡ cột badge/size.
9. `go build -o lazyexplorer . && go vet ./... && go test ./...` xanh.

## 7. Task breakdown

- [ ] **T1 — Test git layer (đỏ trước).** Bảng test cho `parseIgnored` (record `!!`, strip `/`,
  ToSlash) và `gitState.isIgnored` (self + ancestor + miss). *(git_test.go)*
- [ ] **T2 — Thu thập ignored.** Thêm `ignored` vào `gitState`; call status thứ hai không
  `-uall`; `parseIgnored`; `isIgnored` ancestor-walk. §5.1. *(git.go)*
- [ ] **T3 — Test model predicate + ordering (đỏ trước).** `isIgnoredEntry`, `partitionIgnored`
  (stable + idempotent + `..` pinned), reorder giữ cursor-by-name trên git-land. §5.2. *(model_git_test.go)*
- [ ] **T4 — Predicate + ordering + hooks.** `isIgnoredEntry`, `partitionIgnored`, `orderEntries`;
  cắm vào `reload`/`syncFromDisk`/`gitRefreshedMsg`/`refreshPreview`. §5.2. *(model.go)*
- [ ] **T5 — Test view coloring (đỏ trước).** `renderEntryRow` với `ignored=true` inactive →
  dùng `dimStyle`; `active=true` → giữ accent. §5.3. *(entryrow_test.go)*
- [ ] **T6 — Màu.** `renderEntryRow` thêm param `ignored`; wire `renderList` + `renderPreview`
  dir branch. §5.3. *(view.go)*
- [ ] **T7 — Docs sync.** Cập nhật `CLAUDE.md` (mô tả layer git + list ordering) nếu mô tả
  hiện hành lệch; PRD này chuyển `accepted` khi land. *(CLAUDE.md, docs/)*
- [ ] **T8 — Verify.** `go build -o lazyexplorer . && go vet ./... && go test ./...` xanh; chạy
  tay kiểm acceptance §6 + visual verdict cho coloring.

## 8. Files chạm tới

| File | Thay đổi |
|------|----------|
| `git.go` | `gitState.ignored`; call status thứ hai (no `-uall`) trong `collectGitState`; `parseIgnored`; `gitState.isIgnored` ancestor-walk. |
| `model.go` | `isIgnoredEntry`, `partitionIgnored`, `orderEntries`; hook trong `reload`/`syncFromDisk`/`gitRefreshedMsg`/`refreshPreview`. |
| `view.go` | `renderEntryRow` thêm param `ignored`; wire `renderList` + `renderPreview` dir branch. |
| `git_test.go` | Test `parseIgnored` + `isIgnored`. |
| `model_git_test.go` | Test `isIgnoredEntry` + `partitionIgnored` + cursor-by-name reorder. |
| `entryrow_test.go` | Test coloring dòng ignored (inactive vs active). |
| `CLAUDE.md` / `docs/` | Docs sync (layer git + list ordering); PRD này → `accepted` khi land. |
