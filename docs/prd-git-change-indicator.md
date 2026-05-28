# PRD — Git change indicator thay cho filesize trong listing row

> Feature: bỏ cột **filesize** ở mỗi hàng file trong list pane + folder preview, thay bằng
> một **git change indicator** — badge một chữ có màu (`M`/`?`/`A`/`D`/`R`) cho loại thay đổi,
> kèm line delta (`+41 -3`) cho "đổi bao nhiêu", và một dấu ● mờ roll-up cho folder có thay
> đổi bên trong. Mục tiêu: liếc một cái biết "agent vừa động vào file/folder nào, đổi cỡ nào".

Status: **draft / chờ review** · Author: phiên brainstorm git-indicator · Ngày: 2026-05-29

---

## 1. Bối cảnh & vấn đề

lazyexplorer là companion sống cạnh một coding agent (Claude Code / coding agent) trong một
terminal pane hẹp — user mở nó để **glance** vào cây project trong khi agent sửa file
(root `CLAUDE.md` §Goal & Positioning). Trong workflow đó, câu hỏi user hỏi cây file
mỗi-vài-giây là: **"agent vừa đổi cái gì?"** — chứ không phải "file này nặng bao nhiêu KB".

Hiện mỗi hàng file hiển thị `humanSize(e.size)`:

- `renderEntryRow` (`view.go:346-378`) — hàng file inactive: `name` tô `fileStyle` (headline)
  + `humanSize(size)` tô `dimStyle` (muted), căn phải qua `styleFileRow` (`view.go:386-402`)
  / `fitRow` (`view.go:419-434`).
- Cột size này được spec hoá trong `prd-consistent-file-listing.md` (accepted, shipped
  2026-05-27): D3 (`line 46`), FR3 (`line 61-62`), D8/FR9 (`line 51`, `line 74-77`).

Filesize gần như **vô dụng** cho use-case này: nó không trả lời câu hỏi user thực sự có
(cái gì đổi), tốn đúng dải cột phải đắt giá nhất, và không phản ánh hoạt động của agent.
Trong khi đó lazyexplorer đã chạy **poll loop** (`tickCmd`, `model.go:31-36`, mỗi
`pollInterval=1s`, `model.go:12-16`) phát hiện file đổi trên disk — nền tảng tự nhiên để
thay vào đó một tín hiệu *git* cập-nhật-liên-tục.

Codebase hiện **chưa có git integration nào**: `go.mod` chỉ có `github.com/sabhiram/go-gitignore`
dùng cho search exclusions (`fs.go:141`, `walkTree` `fs.go:134-192`), và chỗ duy nhất nhắc
`.git` là dòng `fs.SkipDir` bỏ qua nó khi walk (`fs.go:156-158`). Không có lib đọc git status,
không exec `git`.

**Tham khảo UI/UX (không copy code):** quy ước badge-chữ-có-màu lấy từ lazygit / VS Code
SCM (M=modified, ?=untracked, A=added, D=deleted, R=renamed), và diffstat `+/-` của
`git diff --numstat`. Các clone trong `tmp/` (superfile/crush/gh-dash) không có git-status
rendering để mượn idiom trực tiếp, nên design này ground vào quy ước git CLI chuẩn.

## 2. Goal (1 câu)

Mỗi hàng file/folder trong list pane và folder preview hiển thị **trạng thái git của nó**
(loại thay đổi + số dòng đã đổi cho file; dấu roll-up cho folder chứa thay đổi) thay cho
filesize, refresh tự động theo poll loop, để user glance-thấy ngay agent vừa đổi gì.

**Non-goal làm rõ:**
- KHÔNG phân biệt **staged vs unstaged** (kiểu `XY` 2 cột của `git status --short`/lazygit) —
  collapse về **một** trạng thái working-tree-vs-HEAD. Lý do: agent sửa file chứ hiếm khi
  `git add`; user muốn thấy "đổi gì so với commit gần nhất", không phải nội dung index.
  Phân biệt staging là khái niệm power-user, thêm cột = ngược "UI đơn giản hơn superfile".
- KHÔNG thêm panel / mode / keybind mới — indicator sống **trong** hàng listing sẵn có,
  thay đúng chỗ size cũ. (root `CLAUDE.md`: "two panels là trần", "default answer to
  'add this to the UI?' is no".)
- KHÔNG thêm git **action** (stage/unstage/commit/discard) — read-only glance, đúng phạm vi.
- KHÔNG diff view / blame / log — chỉ là indicator một hàng.
- KHÔNG giữ filesize ở đâu trong **listing row** (kể cả ngoài git repo). `humanSize` vẫn
  tồn tại cho preview placeholder của binary/image (`fs.go:295`, `fs.go:485-502`) — đó là
  ngữ cảnh khác, giữ nguyên.
- KHÔNG fsnotify / git hooks — refresh theo poll tick sẵn có là đủ cho "glance".

## 3. Quyết định đã chốt

| # | Quyết định | Giá trị | Lý do |
|---|-----------|---------|-------|
| D1 | Deliverable | **PRD + design + task** một file | house style của repo (`docs/CLAUDE.md` §PRD) |
| D2 | Thông tin hiển thị | badge **loại** thay đổi **+ line delta** (`M  +41 -3`) | user chọn; "à đây là modify" (loại) + "change bao nhiêu" (delta) — cả hai câu hỏi user nêu |
| D3 | Encoding indicator | **letter badge có màu** (M/?/A/D/R), không phải chỉ-màu hay chỉ-dot | tự-giải-nghĩa; **đọc được cả khi mất màu / colorblind** (terminal no-color vẫn thấy chữ). Quy ước lazygit/VS Code |
| D4 | Folder roll-up | folder chứa thay đổi trong subtree → dấu **● mờ** (`colDim`) | user chọn; glance "agent động vào folder nào" là giá trị cao nhất cho workflow cạnh agent |
| D5 | Detection | shell out **`git status --porcelain=v1 -z -uall`** + numstat HEAD-aware (xem D7), chạy **async** qua `tea.Cmd` với timeout | git CLI nhanh + chuẩn (cách lazygit dùng); async = không freeze UI (giống pipeline preview). `-uall` để git **expand folder untracked thành từng file** (mặc định gộp `?? sub/` — ĐÃ VERIFY ✅ 2026-05-29) → file mới agent tạo trong folder mới đều có badge. Bác `go-git`: `Worktree.Status()` hash mọi file → chậm + dependency nặng |
| D6 | Trạng thái gộp | **một** badge theo working-tree-vs-HEAD, bỏ qua tách staged/unstaged (Non-goal) | đơn giản, khớp câu hỏi user; `git diff HEAD` cho delta gộp staged+unstaged đúng "đổi so với commit cuối" |
| D7 | Magnitude source | tracked: numstat **HEAD-aware** (có HEAD → `git diff HEAD --numstat -z`; repo chưa commit → `git diff --cached --numstat -z`); untracked: đếm dòng file (cap `maxPreviewBytes`/file, `maxUntrackedScan` tổng, bỏ binary) | numstat cho delta tracked chính xác; repo no-commit `git diff HEAD` crash exit 128 (ĐÃ VERIFY ✅ 2026-05-29) nên fallback `--cached`. `-uall` liệt kê untracked theo từng file nên đếm dòng per-file → `+N` (mockup `?  +88`) |
| D8 | Priority khi hẹp | name > badge > delta: hẹp dần thì **bỏ delta trước**, rồi bỏ badge, cuối mới truncate name | user chọn; mirror cơ chế "drop size trước name" của `fitRow` hiện tại (FR6 cũ) |
| D9 | Refresh cadence | mỗi poll tick (1s) dispatch lại git async, gen-counter drop kết quả stale, in-flight guard chống pileup | git status đổi **không** kèm mtime/size đổi (vd `git add`/`commit`/`checkout`) → `dirSig` không bắt được; phải refresh độc lập với `dirSig` |
| D10 | Ngoài git repo | `git rev-parse --show-toplevel` lỗi → git mode OFF, listing row **trơn** (không size, không badge) | degrade sạch; lazyexplorer chạy được ở thư mục bất kỳ, không bắt buộc git |
| D11 | Active (cursor) row | indicator render **plain** trong `cursorActiveStyle` (mất màu badge trên hàng cursor) | nhất quán với hàng active hiện tại (một style phủ cả hàng); màu badge trên nền accent dễ unreadable. Loại thay đổi của file đang chọn đọc được qua preview pane |
| D12 | Màu | badge: `?`/`A` xanh lá (new), `M`/`R` vàng (`colWarn`), `D`/conflict đỏ (`colDanger`); delta **diffstat** `+N` xanh (`colGitNew`) / `-N` đỏ (`colDanger`); folder ● `colDim` | tái dùng palette sẵn có; thêm **một** màu xanh (`colGitNew`) dùng chung cho badge-new lẫn `+N`. Delta green/red (user chọn) cho đọc magnitude nhanh kiểu git diffstat quen mắt |

## 4. Functional requirements

- **FR1** — Trong git repo, hàng **file** đổi so với HEAD hiển thị badge loại thay đổi
  (`M`/`?`/`A`/`R`, và `!` cho conflict — hiếm) ở dải cột phải (chỗ size cũ), tô màu theo
  D12. File không đổi → **không** indicator (cột phải trống). *(Badge `D` deleted chủ yếu
  nổi qua **folder ●** vì file xoá hẳn không còn row — xem §5.3; `D` trên row chỉ ở case
  staged-delete giữ file.)*
- **FR2** — Badge file kèm **line delta** khi có: tracked → từ numstat HEAD-aware (D7);
  untracked text → `+<linecount>` (mỗi file untracked liệt kê riêng nhờ `-uall`); binary /
  không đếm được → badge một mình, không delta. Format delta **bỏ side bằng 0**: cả hai →
  `+41 -3`; chỉ thêm → `+88`; chỉ bớt → `-54`.
- **FR3** — Hàng **folder** có ≥1 thay đổi trong subtree (con/cháu) hiển thị **● mờ**
  (`colDim`) ở cột phải. Folder không có thay đổi bên trong → trống. `..` tổng hợp **không**
  bao giờ mang indicator.
- **FR4** — Priority hẹp dần (D8): `name + badge + delta` → (không đủ) `name + badge` →
  (không đủ) `fitWidth(name)` không indicator. Name luôn thắng; delta rớt trước badge.
- **FR5** — List pane và folder preview vẽ indicator qua **cùng** đường render
  (`renderEntryRow`) → cùng một entry ở cùng base-dir hiện indicator byte-identical hai pane
  (giữ core invariant của `prd-consistent-file-listing.md`).
- **FR6** — Hàng **active** (cursor, chỉ list pane) render indicator plain trong
  `cursorActiveStyle` (D11) — badge/delta/● mất màu riêng, nằm trong vùng highlight.
- **FR7** — Git status refresh **async** theo poll tick (1s): khi trạng thái git của một
  entry đang hiển thị đổi (kể cả do `git add`/`commit`/`checkout` — không kèm mtime đổi),
  indicator cập nhật trong ~1s mà **không** chặn keystroke / poll loop.
- **FR8** — Ngoài git repo (`show-toplevel` lỗi) → không indicator, không size; listing row
  chỉ còn name (+`/` cho dir). Không crash, không thông báo lỗi xâm lấn.
- **FR9** — Filesize **bị gỡ** khỏi mọi listing row (list pane + folder preview), kể cả khi
  ngoài git repo. `humanSize` giữ nguyên cho preview placeholder binary/image (out-of-scope).
- **FR10** — Git mode resilient (giống `walkTree` FR13): mọi lỗi git (binary thiếu, repo hỏng,
  command timeout, output không parse được) → degrade về "không indicator", không bao giờ
  crash app hay làm hỏng frame. **Per-command independence**: numstat fail (vd repo no-commit
  trước khi fallback `--cached` cũng fail) → set `hasDelta=false` cho mọi entry, **không** xoá
  badge — `git status` vẫn drive badge. Một lệnh hỏng không kéo cả git state về rỗng.

## 5. Technical design

> Kim chỉ nam: **git là một tầng dữ liệu độc lập (`git.go`), chạy async như preview pipeline,
> nạp vào model một map status; view chỉ tra map và render.** fs trả filesystem, git trả
> change-state, view render — không trộn concern. Functional core (parse/map thuần) + imperative
> shell (exec git ở `tea.Cmd`), đúng Engineering Principle #4.

### 5.1 Tầng git (`git.go` — file mới)

Một file riêng cho concern git, song song `fs.go` (separation of concerns; Rule #3 flat modular
packages — đặt tên theo *what it provides*).

**Kiểu dữ liệu:**

```go
// gitChange là trạng thái git gộp của một path (working-tree vs HEAD).
type gitChange struct {
    code     gitCode // loại thay đổi (collapse từ porcelain XY)
    added    int     // dòng thêm; 0 nếu không rõ
    deleted  int     // dòng bớt; 0 nếu không rõ
    hasDelta bool    // true nếu added/deleted có nghĩa (phân biệt "0 dòng" vs "không biết")
}

type gitCode int
const (
    gitModified gitCode = iota // M  — vàng
    gitUntracked               // ?  — xanh
    gitAdded                   // A  — xanh
    gitDeleted                 // D  — đỏ
    gitRenamed                 // R  — vàng
    gitConflict                // U/!— đỏ
)

// gitState là snapshot nạp vào model mỗi lần refresh.
type gitState struct {
    repoRoot  string                // "" ⇒ không phải git repo (git mode OFF, D10)
    changes   map[string]gitChange  // key = path repo-root-relative (slash-separated)
    dirtyDirs map[string]bool       // mọi thư mục ancestor (repo-rel) của một path đổi — O(1) roll-up (D4)
}
```

**Detect repo root:** `git -C <jailRoot> rev-parse --show-toplevel` một lần (kết quả cache trên
model; jail root cố định nên repoRoot không đổi giữa các tick, kể cả sau `cd` qua command
palette — vẫn jailed dưới root). Lỗi/exit≠0 → `repoRoot=""`. repoRoot **thường là ancestor**
của jail root (jail là subdir của repo) — `rel(repoRoot, …)` ra key đúng cho cả hai chiều.

**Thu thập (mỗi refresh, async):** chạy trong cùng một goroutine của `tea.Cmd`, mỗi exec qua
`exec.CommandContext` với timeout ~2s (FR10 — repo khổng lồ/locked không treo quá in-flight):
1. `git -C <repoRoot> status --porcelain=v1 -z -uall` → status letter per **file** path.
   `-uall` **expand folder untracked thành từng file** (mặc định gộp `?? sub/`, làm folder ●
   không sáng + file bên trong mất badge — ĐÃ VERIFY ✅ 2026-05-29). Honor `.gitignore` native.
2. numstat **HEAD-aware**: `git -C <repoRoot> rev-parse --verify -q HEAD` thành công →
   `git diff HEAD --numstat -z`; repo chưa commit → `git diff --cached --numstat -z`
   (ĐÃ VERIFY ✅ 2026-05-29: `diff HEAD` fatal exit 128 khi no-commit, `diff --cached` chạy).
   → `(added, deleted)` per tracked-changed path. Lệnh này fail riêng → `hasDelta=false`, KHÔNG
   xoá badge (FR10 per-command independence).
3. Với mỗi path **untracked** (`??`, giờ là từng file nhờ `-uall`) là text & ≤ `maxPreviewBytes`:
   đếm `\n` → `added` (tái dùng đọc-có-cap kiểu `readPreviewBytes` `fs.go:245-264`; binary/
   over-cap → bỏ delta). Cap **tổng** số file untracked đọc ở `maxUntrackedScan` (mirror
   `maxWalkEntries` `fs.go:106`) — repo mới / build-output chưa gitignore không làm refresh
   đọc hàng nghìn file mỗi tick; vượt cap → các file còn lại vẫn badge `?`, chỉ không delta.

**Parse porcelain `-z` (gotcha):** entries phân tách bằng `NUL`, path **không** quote. Entry
rename/copy (`R`/`C`) chiếm **hai** field NUL: `XY <new>\0<old>\0` — parser phải consume cả hai.
Mỗi entry: 2 ký tự XY, một space, rồi path tới `NUL`.

**Collapse XY → một `gitCode`** (precedence, D6): `??`→untracked; else có `U`/`AA`/`DD`→conflict;
else có `D`→deleted; else có `R`→renamed; else có `A`→added; else có `M`→modified; else bỏ qua.

**Parse numstat `-z` (gotcha):** `<add>\t<del>\t<path>\0`; `add`/`del` = `-` nghĩa là binary
→ `hasDelta=false`. Rename trong numstat có format khác (`<add>\t<del>\t\0<old>\0<new>\0`) —
parse best-effort; không chắc thì để badge không delta (FR2 cho phép).

**Build `dirtyDirs`:** với mỗi key trong `changes`, mark mọi ancestor dir repo-rel
(`a/b/c.go` → `a`, `a/b`). Một lần per refresh → folder roll-up tra O(1) lúc render (không
quét map mỗi hàng mỗi frame).

**Resilience (FR10):** exec lỗi / output rác → trả `gitState{repoRoot: r}` với map rỗng
(degrade về "không đổi"), không panic. Không bao giờ bubble lỗi lên frame.

**An toàn:** chỉ exec `git` với arg cố định (slice arg, **không** qua shell); path đến từ
output của git, không bao giờ nội suy input user vào command → không injection. `git -C <root>`
với root là jail root (trusted).

### 5.2 Async refresh trong model (`model.go`)

Mirror y hệt async preview pipeline (`previewRenderedMsg` + `renderGen` gen-counter
`model.go:172`, `walkTreeCmd` `model.go:98-103`, `spinning` guard `model.go:180`).

**Field mới trên `model`** (cạnh cụm async sẵn có `model.go:118-238`):

```go
git        gitState // snapshot hiện tại; git.repoRoot=="" ⇒ git mode OFF
gitGen     uint64   // tag mỗi dispatch; chỉ apply kết quả gen mới nhất
gitInFlight bool    // chống pileup khi git chậm hơn 1 tick (giống `spinning`)
```

**Msg + Cmd:**

```go
type gitRefreshedMsg struct { gen uint64; state gitState }

func gitRefreshCmd(repoRoot string, gen uint64) tea.Cmd {
    return func() tea.Msg { return gitRefreshedMsg{gen: gen, state: collectGitState(repoRoot)} }
}
```

**Dispatch** (dùng `tea.Batch` — cả `Init` `model.go:762` lẫn `tickMsg` `model.go:764-775` hiện
trả **một** cmd; phải đổi shape sang batch, KHÔNG thay `tickCmd()` đi mất kẻo chết poll loop):
- `Init`: detect repoRoot (đồng bộ, một lần, rẻ); return `tea.Batch(tickCmd(), <git first refresh
  nếu repo>)`. Repo → `gitInFlight=true`, `gitRefreshCmd(repoRoot, gitGen)` với `gitGen++`.
- `tickMsg` (chỗ `cmd = tickCmd()`): nếu `git.repoRoot != ""` && `!gitInFlight` →
  `gitInFlight=true`, `gitGen++`, `cmd = tea.Batch(tickCmd(), gitRefreshCmd(git.repoRoot, gitGen))`.
  Ngược lại giữ `cmd = tickCmd()`. (Refresh **độc lập** `dirSig` — D9.)

**Apply (`gitRefreshedMsg`):** `gitInFlight=false`; nếu `msg.gen != gitGen` → drop (stale,
giống discard render cũ); else lưu `m.git = msg.state`. View kế tiếp (poll tick luôn trigger
re-render mỗi 1s) tự phản ánh map mới — không cần đụng `dirSig`/`fsSig`.

### 5.3 Indicator builder (`view.go`)

View tra `m.git` theo **base-dir** của hàng (list pane: `m.cwd`; folder preview: thư mục đang
preview) và dựng indicator.

**Resolve repo-rel path:** cho entry `e` ở base-dir `d`:
`rel = filepath.ToSlash(rel(m.git.repoRoot, filepath.Join(d, e.name)))`. Nếu `rel` bắt đầu
`..` (entry ngoài repo — không xảy ra khi repoRoot là ancestor, nhưng guard) → không indicator.

**Cho file:** `chg, ok := m.git.changes[rel]`. `ok` → indicator = badge(`chg.code`) [+ delta nếu
`chg.hasDelta`]. Không → trống.

**Cho dir** (trừ `..`): `m.git.dirtyDirs[rel]` → indicator ●; không → trống.

**Lưu ý file deleted:** file bị xoá hẳn **không** còn trong `os.ReadDir` (`fs.go:40-61`) →
**không có row** để gắn badge `D`. Deletion vì vậy nổi lên chủ yếu qua **folder ●** (dir cha
được mark dirty trong `dirtyDirs`). Badge `D` trên một row chỉ xảy ra ở case hiếm file vẫn
trên disk nhưng staged-deleted (vd `git rm --cached`). Đây là hành vi đúng, không phải thiếu sót.

**Folder preview cần base-dir:** `renderPreview` hiện không biết đường dẫn folder đang
preview. Thêm field `previewDirPath string` set cùng `previewEntries`/`previewIsDir` trong
`refreshPreview` nhánh dir (`model.go`, cạnh `previewIsDir` `model.go:138`), reset "" ở reset
hygiene. `renderPreview` (`view.go:480-497`) tra indicator theo `m.previewDirPath`.

**Priority chain (D8/FR4)** — chọn candidate rộng nhất vừa cột, cho width pane `w` và độ
rộng name:
1. badge + " " + delta (vd `M +41 -3`)
2. badge (vd `M`)
3. (không gì — `fitWidth(name, w)` truncate name)

Chọn candidate lớn nhất sao cho `width(name) + 1 + width(cand) ≤ w`. Không cái nào vừa →
`fitWidth(name, w)` không indicator.

**Signature `renderEntryRow`** đổi để nhận indicator đã-resolve (giữ nó là pure formatter;
việc "tra map theo base-dir" thuộc caller vốn biết base-dir):

```go
// ind: nil ⇒ không indicator (clean file / dir / .. / ngoài repo).
func renderEntryRow(e entry, ind *rowIndicator, w int, active, listFocused bool) string
```

`rowIndicator` mang đủ để dựng cả candidate plain (đo width) lẫn styled (output):

```go
type rowIndicator struct {
    badge string        // "M"/"?"/"A"/"D"/"R"/"!" hoặc "●" (folder)
    color lipgloss.Color
    delta string        // "+41 -3" hoặc ""
}
```

**Styling theo hàng:**
- **active** (D11/FR6): chọn candidate theo plain width, lay out plain qua `fitRow(name, plainRight, w)`,
  bọc `cursorActiveStyle.Width(w)` — y khuôn hàng active hiện tại (`view.go:357-369`).
- **dir inactive**: name `dirStyle` + ● `dimStyle` (nếu có), gap để trống.
- **file inactive**: name `fileStyle` + badge `color` + (space) + delta diffstat (renderer tách
  `+N` tô `colGitNew`, `-N` tô `colDanger`), gap trống.

`fitRow` (`view.go:419-434`) giữ vai trò pure plain layout (đổi tên tham số `size`→`right`,
ngữ nghĩa y nguyên: phải-căn, drop khi hẹp). `styleFileRow` (`view.go:386-402`) tổng quát hoá
thành split-style cho cột phải nhiều mảnh (badge màu + delta dim). Một helper chung
`styleRow(nameStyled, indStyled, plainWidths…, w)` để cả nhánh dir-có-● và file-có-badge dùng
chung (cả hai giờ đều là "name-trái-styled + right-styled").

### 5.4 Caller render (`view.go`)

- `renderList` (`view.go:308-323`): mỗi hàng resolve indicator theo `m.cwd` rồi gọi
  `renderEntryRow(e, ind, w, i==cursor, listFocused)`.
- `renderPreview` nhánh folder (`view.go:483-497`): resolve theo `m.previewDirPath`, gọi
  `renderEntryRow(e, ind, w, false, false)`. FR5 giữ: cùng entry + cùng base-dir → byte-identical.

### 5.5 Palette (`theme.go`)

Thêm **một** màu + style git (tái dùng `colWarn`/`colDanger`/`colDim` sẵn có `theme.go:15-17`):

```go
colGitNew = lipgloss.Color("#3FB950") // new/untracked/added + delta "+N" — xanh git (github green)
// badge Foreground theo gitCode→màu; folder ● dùng dimStyle; delta: "+N" colGitNew, "-N" colDanger.
```

Map `gitCode → color`: modified/renamed → `colWarn`; untracked/added → `colGitNew`;
deleted/conflict → `colDanger`. (Hex chính xác tinh chỉnh được; một dòng.)

### 5.6 Reconcile `prd-consistent-file-listing.md`

PRD đó (accepted/shipped) mô tả **cột size** — feature này thay size→git-indicator. **Core
invariant của nó GIỮ và được củng cố** (một `renderEntryRow`, hai pane byte-identical: D2/FR1
của nó). Tới khi commit implement này land, `prd-consistent-file-listing.md` **vẫn
authoritative** cho hành vi đã ship (code hiện vẫn hiện size) — đây là gap spec-vs-spec tạm
thời trong pha spec-first, không phải doc-vs-code drift mà `docs/CLAUDE.md` cấm. Cần cập nhật
(làm **cùng commit** khi implement, không phải bây giờ — spec-first):
- D3 (`line 46`), FR3 (`line 61-62`): "size ở cả hai pane" → "git indicator ở cả hai pane".
- D8 (`line 51`), FR9 (`line 74-77`): muted-size → "badge màu + delta dim".
- §5.1 sketch (`line 87-130`): signature + nội dung cột phải.
- Gherkin scenario "human-readable size is shown" (`line 244-249`) + checklist mục size
  (`line 284-292`): đổi sang assert git indicator.

Cách reconcile (positive framing, `docs/CLAUDE.md`): viết lại trạng-thái-đích affirmative
("cột phải hiện git indicator"), KHÔNG để "~~size~~ → indicator"; lịch sử "đổi từ size sang
indicator vì…" thuộc git history / phần này của PRD mới, không nhét vào PRD cũ đang sống.

### 5.x Đã cân nhắc & **defer khỏi v1**

- **`go-git` (pure Go, không exec):** bác (D5). `Worktree.Status()` hash mọi file → chậm
  trên repo lớn; dependency nặng. Git CLI nhanh + là cái user đã có.
- **Phân biệt staged/unstaged (XY 2 cột):** defer (Non-goal/D6). Có thể thêm sau bằng màu
  badge (vd staged sáng hơn) mà không thêm cột — nếu user thực sự cần.
- **Untracked line-count chính xác qua `git`:** defer. v1 đếm `\n` có cap (D7) — rẻ, đủ cho
  glance; không chạy `git diff --no-index` per-file (exec bùng nổ).
- **fsnotify / git hooks để refresh tức thì:** defer. Poll 1s đủ "glance"; watcher là máy móc
  thừa (cùng lý do `walkCacheTTL` chọn poll thay fsnotify, `model.go:80-83`).
- **Roll-up badge cho folder chỉ rõ loại (vd M nếu trong toàn modified):** defer. ● trung tính
  "có gì đó đổi bên trong" là đủ glance; phân loại folder = thêm logic + màu, chưa cần.
- **Cache git refresh theo TTL như `walkCacheTTL`:** defer. Poll 1s + in-flight guard đã đủ;
  thêm TTL là tối ưu chưa chứng minh cần.
- **Indicator giữ màu trên hàng active:** defer (D11). Màu trên nền accent dễ unreadable; nhất
  quán "một style phủ hàng active" quan trọng hơn.
- **Case-folding macOS (APFS/HFS+ case-insensitive):** git lưu path đúng-hoa-thường; nếu user
  điều hướng `Foo` mà git track `foo`, lookup map trượt. Edge hiếm; defer cho tới khi gặp thật
  trên repo của dev (confirm before invest). Match key có thể chuẩn hoá case sau nếu cần.

## 6. Acceptance criteria

### Gherkin

```gherkin
Feature: Git change indicator in the file listing

  Each file/folder row shows its git change state instead of its byte size, so a
  glance reveals what an agent has touched. Both the list pane and the folder
  preview render the indicator through one shared routine, so it can never drift
  between them. Outside a git repository the row carries neither size nor badge.

  Background:
    Given the explorer is open inside a git repository

  Scenario: A modified file shows the modified badge with its line delta
    Given a tracked file has been modified since the last commit
    When I view that file in the list pane
    Then its row shows the modified badge in the modified colour
    And its row shows how many lines were added and removed

  Scenario: A new untracked file shows the new badge
    Given an untracked file exists
    When I view that file in the list pane
    Then its row shows the untracked badge in the new colour
    And its row shows the file's line count as additions

  Scenario: An unchanged file shows no indicator
    Given a tracked file matches the last commit exactly
    When I view that file in the list pane
    Then its row shows neither a badge nor a size

  Scenario: A folder with changes inside is flagged
    Given a folder contains a modified file somewhere below it
    When I view that folder in the list pane
    Then its row shows the folder roll-up marker
    And a folder with no changes below it shows nothing

  Scenario: The indicator is identical in both panes
    Given a folder contains a modified file
    When I view that file in the list pane
    And I view the same file in the folder preview of its parent
    Then both rows show the same indicator

  Scenario: A staged change is reflected without a file edit
    Given a modified file is shown with its badge
    When the file is staged with no further edit to its contents
    Then within about a second its row still reflects its change state

  Scenario: Outside a git repository the listing is plain
    Given the explorer is open in a directory that is not a git repository
    When I view any file in the list pane
    Then its row shows only the name, with neither size nor git badge

  Scenario: The cursor row stays legible over the highlight
    Given the cursor is on a modified file in the list pane
    When I view that row
    Then its full-width selection highlight covers the row
    And the indicator stays within the highlight without breaking the row width

  Scenario: A newly created folder of files is flagged and its files are badged
    Given an untracked folder containing untracked files is created
    When I view that folder in the list pane
    Then its row shows the folder roll-up marker
    And when I open that folder
    Then each file inside shows the untracked badge

  Scenario: A brand-new repository with no commits still shows new files
    Given the repository has no commits yet
    And an untracked file exists
    When I view that file in the list pane
    Then its row shows the untracked badge
    And the missing commit history does not blank out the listing

  Scenario: A deleted file surfaces through its folder
    Given a tracked file is deleted from disk
    When I view its parent folder in the list pane
    Then the folder row shows the roll-up marker
    And the deleted file has no row of its own

  Scenario: A narrow pane drops the delta before the badge
    Given a modified file with a line delta
    When the list pane is too narrow to fit name, badge and delta
    Then the delta is dropped first and the badge is kept
    And when even the badge cannot fit, the name is kept and truncated
```

### Checklist verify

1. Trong git repo: sửa một file tracked → hàng nó hiện `M` (vàng) + `+N -M`; tạo file mới →
   `?` (xanh) + `+N`; file sạch → không gì.
2. Folder chứa file đổi bên trong (cả nhiều cấp) → hiện ●; folder sạch → trống; `..` → trống.
3. Cross-pane: cùng file đổi, hàng ở list pane và ở folder preview của parent **byte-identical**
   (so qua `ansi`-aware compare trong test).
4. `git add <file>` (không sửa nội dung) → trong ~1s indicator vẫn đúng trạng thái (chứng minh
   refresh độc lập `dirSig` — D9/FR7).
5. Ngoài git repo (`t.TempDir()` không `git init`) → listing chỉ có name, không size không badge,
   không crash.
6. Hẹp dần (kéo divider nhỏ): delta rớt trước, rồi badge, cuối cùng name truncate `…`; frame
   không vỡ width.
7. Hàng active trên file đổi: width đúng `w`, accent bg phủ cả hàng, indicator nằm trong vùng
   highlight (plain).
8. Filesize **không** còn xuất hiện ở bất kỳ listing row nào; nhưng preview của file **binary**
   vẫn hiện `(binary file — <size>…)` (regression: `humanSize` chỗ placeholder còn sống).
9. Git lỗi giả lập (repoRoot trỏ chỗ không phải repo / git command fail) → degrade "không
   indicator", app không panic (FR10).
10. Parse thuần: porcelain `-z -uall` với rename (`R old new` 2-field) + **collapse XY** từng
    combo (`MM`/`AD`/`RM`/`AA`/`DD`/`UU`) ra đúng `gitCode`; numstat binary (`- -`) → badge
    không delta — unit test trên parser.
11. Folder untracked mới (agent tạo `sub/` chứa file): folder hiện ●, mở vào → mỗi file `?`
    (chứng minh `-uall` expand + key không lệch trailing-slash — B1).
12. Repo no-commit (`git init` chưa commit) + có file untracked/staged: badge hiện đúng,
    listing **không** bị blank (numstat fail không xoá state — M1/FR10).
13. Cap/timeout: thư mục nhiều file untracked vượt `maxUntrackedScan` → vẫn badge `?`, refresh
    không đọc quá cap; git command treo → timeout ~2s, degrade, app sống.
14. Visual verdict (`oh-my-claudecode:visual-verdict`) trên ảnh render: badge màu đúng, căn
    phải đúng, folder ● đúng, hàng cursor đọc được — pass.
15. `go build -o lazyexplorer . && go vet ./... && go test ./... -race` xanh.

## 7. Task breakdown

> Đề xuất, chờ duyệt trước khi tick. TDD: test đỏ trước, code tới xanh (`CLAUDE.md` §Testing).
> Map sát file. Mở rộng hot-path test mọi tầng (fs/git parse, model async, view render).

- [ ] **T1 — Parser thuần (`git.go` + test đỏ trước).** `gitChange`/`gitCode`/`gitState`;
  parse porcelain `-z -uall` (rename 2-field gotcha), collapse XY→code **mọi combo**
  (`MM`/`AD`/`RM`/`AA`/`DD`/`UU` — checklist §6.10), parse numstat `-z` (binary `-`, rename
  format), build `dirtyDirs`. §5.1. *(git.go, git_test.go)*
- [ ] **T2 — Collect async (`git.go`).** `collectGitState(repoRoot)`: detect repoRoot qua
  `rev-parse --show-toplevel`; `status --porcelain=v1 -z -uall`; numstat **HEAD-aware**
  (`rev-parse --verify -q HEAD` → `diff HEAD` | `diff --cached`); đếm dòng untracked cap
  `maxUntrackedScan`; mỗi exec qua `exec.CommandContext` timeout ~2s; **per-command
  independence** (numstat fail ≠ xoá badge); resilient mọi lỗi → degrade (FR10). §5.1.
  *(git.go, git_test.go)*
- [ ] **T3 — Model wiring async.** Field `git`/`gitGen`/`gitInFlight`; `gitRefreshedMsg` +
  `gitRefreshCmd`; detect repoRoot ở `Init`; dispatch qua **`tea.Batch`** ở `Init` + `tickMsg`
  (giữ `tickCmd()`, độc lập `dirSig`, in-flight guard); apply gen-gated. §5.2.
  *(model.go, model_git_test.go)*
- [ ] **T4 — `previewDirPath`.** Lưu đường dẫn folder đang preview cùng `previewEntries`/
  `previewIsDir`; reset hygiene; để `renderPreview` resolve indicator theo nó. §5.3. *(model.go)*
- [ ] **T5 — Indicator builder + `renderEntryRow` signature.** `rowIndicator`; resolve repo-rel
  + tra `changes`/`dirtyDirs`; priority chain name>badge>delta; active-row plain (D11). Gỡ
  `humanSize` khỏi row. §5.3. *(view.go, entryrow_test.go)*
- [ ] **T6 — Layout helpers.** `fitRow` đổi `size`→`right` (ngữ nghĩa giữ); `styleFileRow`→
  split-style cột phải nhiều mảnh; helper `styleRow` chung dir-●/file-badge. §5.3.
  *(view.go, entryrow_test.go)*
- [ ] **T7 — Caller render.** `renderList` resolve theo `m.cwd`; `renderPreview` folder branch
  theo `m.previewDirPath`; FR5 cross-pane byte-identical. §5.4. *(view.go)*
- [ ] **T8 — Palette.** `colGitNew` + map `gitCode→color`; delta/● dùng `dimStyle`. §5.5.
  *(theme.go)*
- [ ] **T9 — Rewrite tests size→indicator.** `entryrow_test.go` (size assertions → badge/delta/
  priority/active-plain/cross-pane); thêm git-mode-off case; giữ binary-preview-size regression.
  §6. *(entryrow_test.go, preview_dir_test.go)*
- [ ] **T10 — Reconcile doc.** Cập nhật `prd-consistent-file-listing.md` (D3/FR3/D8/FR9/§5.1/
  Gherkin/checklist) size→indicator, positive framing (§5.6). *(docs/prd-consistent-file-listing.md)*
- [ ] **T11 — Verify.** `go build -o lazyexplorer . && go vet ./... && go test ./... -race` xanh;
  chạy tay checklist §6; visual verdict trên ảnh render (badge màu/căn/●/cursor).

## 8. Files chạm tới

| File | Thay đổi |
|------|----------|
| `git.go` | **mới** — tầng git: kiểu `gitChange`/`gitCode`/`gitState`; `collectGitState` (exec status+numstat, untracked count, detect repoRoot); parse porcelain/numstat `-z`; build `dirtyDirs`; resilient degrade |
| `git_test.go` | **mới** — unit test parser (rename/binary/collapse), `dirtyDirs`, untracked count cap, degrade-no-repo |
| `model.go` | `+ git/gitGen/gitInFlight`; `+ gitRefreshedMsg`/`gitRefreshCmd`; detect repoRoot ở `Init`; dispatch ở `tickMsg` (độc lập `dirSig`); apply gen-gated; `+ previewDirPath` (set/reset) |
| `model_git_test.go` | **mới** — async dispatch/gen-gate/in-flight, refresh độc lập `dirSig`, degrade |
| `view.go` | `+ rowIndicator` + builder; `renderEntryRow` signature nhận `*rowIndicator`, gỡ `humanSize`; `fitRow`/`styleFileRow`/`styleRow` tổng quát hoá; `renderList`+`renderPreview` resolve indicator |
| `entryrow_test.go` | rewrite size→indicator; thêm priority/active-plain/cross-pane/git-off |
| `preview_dir_test.go` | cross-pane indicator byte-identical; previewDirPath |
| `theme.go` | `+ colGitNew` + map `gitCode→color` |
| `docs/prd-consistent-file-listing.md` | reconcile size→git-indicator (§5.6), positive framing |
| `docs/prd-git-change-indicator.md` | File này |
