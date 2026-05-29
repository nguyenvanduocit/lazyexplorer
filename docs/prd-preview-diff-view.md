# PRD — Diff view trong preview pane cho file đã đổi

> Feature: khi cursor đứng trên một **file tracked đã sửa**, preview pane hiển thị **diff**
> của nó — các dòng thêm (xanh) / bớt (đỏ) đã tô màu, tính so với **cùng base HEAD** mà badge
> `+N/-N` đếm — thay vì nội dung file đầy đủ. Một phím **`v`** toggle giữa diff và full file.
> File sạch / untracked preview như hôm nay. Mục tiêu: đóng nhịp trung tâm của vibe-code loop
> — *review-the-edit-before-accept* — ngay trong pane, không tab-away ra terminal.

Status: **accepted** · Author: phiên spec preview-diff · Ngày: 2026-05-29

---

## 1. Bối cảnh & vấn đề

lazyexplorer sống cạnh một coding agent (Claude Code / coding agent) trong một terminal pane
hẹp — user mở nó để **glance + act** vào cây project trong khi agent sửa file (root
`CLAUDE.md` §Goal & Positioning). Nhịp trung tâm của loop đó là **review-the-edit-before-accept**:
agent báo "đã sửa `src/handlers/auth.go`", user thấy hàng đó sáng `M  +41 -3`, và muốn **đọc
xem 41 dòng kia đổi gì** ngay tại chỗ — dozens of times a session.

Hiện trạng: badge git đã trả lời "đổi *bao nhiêu*" nhưng không trả lời "đổi *cái gì*".

- `gitChange` (`git.go:71-76`) mang `code`/`added`/`deleted`/`hasDelta`; delta `+41 -3` đến từ
  `git diff HEAD --numstat -z` (`git.go:177-183`), HEAD-aware với fallback `--cached` cho repo
  chưa commit. Nhưng numstat chỉ cho **con số** — không bao giờ là **hunks**.
- Preview pane render nội dung file hiện tại theo loại (markdown/code/plain) qua renderer
  registry (`rendererFor` `fs.go:361-368`; dispatch async ở `syncPreview` `model.go:745-788`).
  Nó hiển thị **file as-is now**, không bao giờ diff.

Hệ quả: để review một edit — lý do cốt lõi user ngồi đây — user phải **rời pane**, tab sang
terminal, gõ `git diff <path>` bằng tay. App dangle đáp án (`+41 -3`) rồi bắt user đi nơi khác
để lấy nốt. Đây là pain **không có in-app workaround nào** (capability map: "NO DIFF VIEW").

**Vì sao feature này là DEPTH play đúng hiến pháp:** nó **hoàn thành** concern git mà badge
`+N/-N` đã mở (`git.go` collect state nhưng chưa bao giờ collect hunks) thay vì bolt-on một
concern mới. Diff là **content mới** trong **pane sẵn có** — đúng như markdown-vs-code routing,
gated bởi một toggle key như wrap toggle (`keys.go:82`). KHÔNG mode mới, KHÔNG panel thứ ba.
Engineering bên dưới sâu và load-bearing: git hunk plumbing codebase chưa có, fetch async off
the Update goroutine qua gen-counter stale guard, base khớp badge để dòng hiện ra reconcile với
con số user đang phản ứng.

**Tham khảo UI/UX (không copy code):** quy ước màu diff (xanh thêm / đỏ bớt / context xám) lấy
từ `git diff` CLI và `tmp/glow` viewport paging idiom (`tmp/glow/ui/pager.go` — async `tea.Cmd`).
Glamour (`renderMarkdownPreview` `fs.go:381`) và chroma (`renderCodePreview` `fs.go:455`) là
hai renderer sống cùng pipeline mà ta mượn contract `(lines, preStyled, err)` cho diff renderer.

## 2. Goal (1 câu)

Khi cursor đứng trên một file tracked đã sửa, preview pane mặc định hiển thị **diff đã tô màu**
của nó (dòng thêm/bớt, tính so với cùng HEAD mà badge đếm), và một phím `v` toggle về **full
file content** — file sạch/untracked vẫn preview nội dung như hôm nay — để user review edit
ngay trong pane.

**Non-goal làm rõ:**
- KHÔNG **diff MODE** mới — không `modeDiff` với nhánh `Update` riêng. Đây là một **content
  variant** của preview pane sẵn có + một toggle key. (root `CLAUDE.md`: "two panels là trần",
  "default answer to 'add this to the UI?' is no".)
- KHÔNG **side-by-side / split diff** — hai pane (list + preview) là trần. Diff render **trong**
  single preview pane sẵn có, không panel/cột thứ ba.
- KHÔNG **staging / unstaging / hunk-apply / edit** từ diff view — read-only review thuần.
  lazyexplorer không phải git client.
- KHÔNG diff với **arbitrary ref/branch** — base cố định khớp badge (HEAD, với fallback
  `--cached` sẵn có khi repo chưa commit). Không cho chọn `--against <ref>`.
- KHÔNG **hunk-to-hunk jump-within-preview** ở v1 — diff dùng đúng vertical/horizontal scroll
  của preview sẵn có; không thêm "next hunk" key. Giữ surface tight; defer rõ ràng.
- KHÔNG bundle **open-in-editor / yank-relative-path / jump-to-changed** vào change này — mỗi
  cái là một follow-up riêng mạch lạc.
- KHÔNG **mode-indicator chip / header** ở status bar hay preview top báo "đang ở diff hay
  content". Bản thân dòng diff tô màu (`+` xanh / `-` đỏ / context xám, D11) **là** tín hiệu —
  diff trông khác hẳn full content. Thêm chip = chrome thừa, phá minimal-chrome (root `CLAUDE.md`).

## 3. Quyết định đã chốt

| # | Quyết định | Giá trị | Lý do |
|---|-----------|---------|-------|
| D1 | Deliverable | **PRD + design + task** một file | house style của repo (`docs/CLAUDE.md` §PRD) |
| D2 | Kiến trúc dispatch | Diff là content variant **state-selected ở model layer** (`refreshPreview`/`syncPreview`), **KHÔNG** name-matched qua registry | "file này dirty không" là **git/model state**, không phải thuộc tính filename. `previewRenderer.matches func(name string) bool` (`fs.go:345`) quyết định *thuần* theo tên — nó **không thể** thấy git state. Markdown-vs-code là quyết định filename thuần; diff-vs-content là quyết định `(dirty, diffOn)`. Nhồi git-state vào `matches(name)` closure là workaround engineering bar cấm; đổi signature mọi renderer là dead code. Closure diff bespoke capture `repoRoot` + path, giữ shape `previewRenderedMsg` để `applyPreview` không đổi |
| D3 | Mặc định toggle | `diffOn` mặc định **TRUE**, session-sticky | Goal: "khi land vào file agent đổi, preview show diff … một phím toggle về full file". Diff-first khi land; toggle đi về full file. Đây là điểm **ngược** wrap (wrap mặc định off): copy default-off của wrap sẽ lặng lẽ defeat goal. Sticky như wrap (review nhiều file dirty liên tiếp, file sạch auto-fallback content) — không reset per file |
| D4 | Phím toggle | **`v`** (view-diff / view-changes), normal mode, fire ở **mọi focus** | `v` unbound khắp nơi (ĐÃ VERIFY ✅ 2026-05-29 `keys.go`/`model.go`/`palette.go`). Free key mnemonic gần `g G h l H L 0 w` đã taken (`keys.go:74-82`). Flow là scan file dirty ở `focusList` → toggle phải fire ở focusList; cũng fire vô hại ở focusPreview. Keybind surface +1 (từ ~26), proportionate cho action review-before-accept tần suất cao nhất |
| D5 | Áp dụng cho file nào | **Modified text → hunks**; untracked → full content (path sẵn có); binary-modified → placeholder "binary files differ"; sạch/dir/`..` → như hôm nay | Chỉ modified-text có "diff so với HEAD" nghĩa. Untracked là all-new → full content **là** diff hữu ích (đã trong scope content path). Binary diff vô nghĩa trong terminal → placeholder. Deleted/renamed **unreachable** (xem D6) |
| D6 | Codes nào tới được preview & đi đâu | List pane render entry **working-tree** (`os.ReadDir` `fs.go:40-61`), nên cursor chỉ đứng lên được file **còn tồn tại trên đĩa**. Reachable codes (`collapseStatus` `git.go:234-252`) + đích: **modified (`M`)** & **renamed (`R`, phía mới)** text → **diff**; **added (`A`, staged-new)** & **untracked (`?`)** → **full content**; **conflict (`U`/`AA`/`DD`)** → **full content**; binary-modified → placeholder (FR5). Deleted (`D`) & phía cũ của rename **không có row** → không tới được preview | "No abstractions until proven": build nhánh deleted/old-rename = dead code. `A`/untracked là **all-new** → content **là** diff hữu ích (D5). Conflict: full content cho user đọc thẳng marker `<<<<<<<`/`=======`/`>>>>>>>` — combined-diff của file conflict là noise, không phải review-the-edit. ĐÃ VERIFY ✅ 2026-05-29: `collapseStatus` emit đúng 5 code; `D`/old-rename unreachable (`fs.go` chỉ list file working-tree). `diffApplies` chỉ true cho `M`/`R`+text → mọi code khác tự nhiên rơi về content path (D5/FR4), KHÔNG cần nhánh riêng |
| D7 | Base khớp badge | `git diff HEAD -- <path>` khi có HEAD; `git diff --cached -- <path>` khi repo chưa commit | **Cùng** branch HEAD-aware mà numstat dùng (`git.go:177-183`) → dòng hiện ra reconcile với `+N/-N` của badge. ĐÃ VERIFY ✅ 2026-05-29: `diff HEAD` fatal exit 128 khi no-commit, `diff --cached` trả hunks exit 0; `diff HEAD` repo committed trả unified diff với `@@`/` `/`-`/`+` |
| D8 | Async + stale guard | Closure diff chạy off Update goroutine qua `tea.Cmd`, honor `renderGen` gen-counter; `applyPreview` gen-gate **không đổi** | Uncompromising-engineering bar dưới small surface: scroll nhanh qua nhiều file dirty không bao giờ show diff file sai. Khớp mọi renderer khác (`syncPreview`/`applyPreview` `model.go:745-867`) |
| D9 | Render style | Diff `previewScrollable=true` + `preStyled=true` — mirror code | Diff thừa kế vertical scroll (`previewScroll`) + horizontal window (`renderHWindow` `view.go:645`) sẵn có; dòng tô ANSI verbatim như code. Dòng diff dài (vd `+    veryLongLine`) pan được, không wrap-cứng |
| D10 | Empty diff dù badge M | `git diff` rỗng (mode-only / whitespace-config) → fallback **full content** | Badge `M` có thể từ thay đổi mode/EOL mà `git diff` text rỗng. Render pane rỗng là bug nhìn thấy được; fallback content giữ pane hữu ích (cùng tinh thần D5 untracked) |
| D11 | Màu diff | thêm `colDiffAdd` (xanh, tái dùng `colGitNew` `theme.go:24`), `colDiffDel` (đỏ, tái dùng `colDanger` `theme.go:20`); hunk header `@@`/context → `colDim` | Quy ước `git diff` CLI: thêm xanh / bớt đỏ / context mờ. Tái dùng palette git sẵn có → một accent family, không thêm màu lạ. Dấu `+`/`-` đầu dòng giữ nên đọc được cả khi mất màu |
| D12 | Toggle invalidation | Handler `v`: flip `diffOn` rồi gọi `refreshPreview()` | `refreshPreview` reset `srcWidth=0`/`pendingWidth=0` (`model.go:519-525`) → `syncPreview` staleness check `srcWidth==w` (`model.go:761`) fire, re-dispatch. Bỏ `refreshPreview` thì toggle thành no-op (width+srcPath không đổi, cache guard nuốt). KHÔNG bịa invalidation path mới |
| D13 | Live-refresh khi parked | Diff của file đang **đứng cursor** auto cập nhật trên poll loop khi agent ghi lại file — **không** cần navigate-away-and-back; KHÔNG thêm máy móc refresh mới | Đúng nhịp dogfood: agent sửa file user đang nhìn → diff phải tươi. ĐÃ VERIFY ✅ 2026-05-29: agent re-save đổi size/mtime → `dirSig` fire (`model.go:411`) → byte-identity guard (`model.go:457-461`) KHÔNG short-circuit → `syncFromDisk` gọi `refreshPreview()` (`model.go:466`) → `diffApplies`/`previewIsDiff` tính lại + `syncPreview` re-dispatch `diffHunks` vs HEAD hiện tại. Diff thừa kế **cùng** cơ chế live-refresh content preview đã có (consistency, không code mới) |
| D14 | Toggle telemetry | Handler `v` record `action.preview_diff_toggle` `{diff: m.diffOn}` | Mirror wrap toggle `action.preview_wrap_toggle` (`model.go:710`) — consistency-is-kindness. Engineering-beneath, không phải UI surface; non-blocking Record (drop-on-full) nên không kéo dài tick |

## 4. Functional requirements

- **FR1** — Khi cursor đứng trên một **file tracked đã sửa** (badge `M`/`R`) là **text** và
  `diffOn` (mặc định true, D3), preview pane render **diff** của file đó: dòng thêm tô
  `colDiffAdd`, dòng bớt tô `colDiffDel`, hunk header (`@@…`) + context tô `colDim` (D11).
- **FR2** — Diff tính qua `git diff HEAD -- <path>` (có HEAD) hoặc `git diff --cached -- <path>`
  (repo chưa commit) — **cùng** base HEAD-aware mà badge `+N/-N` dùng (D7/`git.go:177-183`), nên
  số dòng `+`/`-` trong diff reconcile với delta của badge. Diff mang **context mặc định 3 dòng**
  của `git diff` (không truyền `-U`), tô `colDim`. Reconcile với badge **chỉ đếm dòng `+`/`-`** —
  dòng context (` ` đầu dòng) KHÔNG tính vào `+N/-N`.
- **FR3** — Phím **`v`** (D4) toggle giữa diff view và **full-file content view**. Toggle ở
  `diffOn=true` → diff (cho file đủ điều kiện); toggle về `diffOn=false` → nội dung file đầy
  đủ qua renderer sẵn có (markdown/code/plain). `diffOn` **session-sticky**, không reset khi
  điều hướng sang file khác (D3).
- **FR4** — File **untracked** (badge `?`) và **added** (badge `A`, staged-new): preview **full
  content** như hôm nay (D5/D6) — đều all-new nên content path hiện trạng đã là diff hữu ích; `v`
  không đổi (đã là content). File **conflict** (`U`/`AA`/`DD`, D6): preview **full content** —
  user đọc thẳng marker `<<<<<<<`/`=======`/`>>>>>>>`; `v` không kích hoạt diff. File **sạch** /
  **dir** / `..`: preview như hôm nay, `v` không có hiệu lực diff.
- **FR5** — File **binary đã sửa**: thay vì hunks, preview placeholder **"binary files differ"**
  (D5); không exec diff render lên binary.
- **FR6** — Empty diff dù badge `M` (mode-only / whitespace, D10): fallback **full content**, KHÔNG
  render pane rỗng.
- **FR7** — Diff fetch chạy **async** off Update goroutine qua `tea.Cmd`, honor `renderGen`
  gen-counter (D8): điều hướng nhanh qua nhiều file dirty không bao giờ hiển thị diff của file
  sai; render chậm trên repo lớn không freeze keystroke / poll loop. `applyPreview` gen-gate
  giữ nguyên.
- **FR8** — Diff `previewScrollable=true` (D9): thừa kế vertical scroll (j/k/ctrl+d/ctrl+u/g/G ở
  focusPreview) + horizontal window (h/l/H/L/0); dòng diff tô màu sống sót qua `renderHWindow`
  slicing (`view.go:645`) căn cột đúng.
- **FR9** — Ngoài git repo (`m.git.repoRoot == ""`, D10 của `prd-git-change-indicator.md`):
  không có file nào "modified vs HEAD" → diff không bao giờ kích hoạt; `v` no-op; preview như
  hôm nay. Không crash.
- **FR10** — Diff resilient (giống git layer FR10 `prd-git-change-indicator.md`): mọi lỗi
  `git diff` (binary thiếu, command timeout, output không parse được, exit≠0) → degrade về
  **full content** của file, KHÔNG crash app hay làm hỏng frame. Diff exec dùng `runGit`
  (timeout `gitCmdTimeout` `git.go:26`) như mọi git exec khác.
- **FR11** — Khi agent ghi lại file đang **đứng cursor**, diff trong pane **auto cập nhật** trên
  poll loop (~1s) — không cần navigate-away-and-back (D13). Đi qua **cùng** path content preview:
  `dirSig` fire vì size/mtime đổi (`model.go:411`) → `syncFromDisk` gọi `refreshPreview()`
  (`model.go:466`) → `syncPreview` re-dispatch `diffHunks` vs HEAD hiện tại. File parked
  byte-identical (sibling churn) → diff KHÔNG re-render (byte-identity guard `model.go:457-461`).
- **FR12** — Toggle `v` record `action.preview_diff_toggle` `{diff: <new diffOn>}` (D14), mirror
  `action.preview_wrap_toggle` (`model.go:710`). Non-blocking (drop-on-full), không kéo dài tick.

## 5. Technical design

> Kim chỉ nam: **diff là content variant state-selected ở model layer, không name-matched qua
> registry** (D2). `refreshPreview` quyết định "file này hiện diff hay content" từ git state +
> `diffOn`; `syncPreview` dispatch một **closure bespoke** capture `repoRoot` (registry signature
> `render(path,content,width,style)` không mang nổi repoRoot — đó là *lý do* state-selected, không
> registry-matched). Closure exec `git diff` qua `runGit`, parse + tô màu hunks, trả đúng
> `previewRenderedMsg`. `applyPreview` không đổi — gen-gate + `previewPreStyled` đã làm việc.
> Functional core (parse/colorize thuần) + imperative shell (exec git ở `tea.Cmd`), Engineering
> Principle #4.

### 5.1 Diff render layer (`git.go`)

Diff plumbing sống trong `git.go` cạnh git state layer sẵn có (cùng concern: git). Thuần data +
exec, không lipgloss color (color thuộc theme.go/view.go — nhưng vì diff trả `preStyled=true`,
nó tô ANSI tại đây qua style đã resolve, giống `renderCodePreview` tô chroma ở `fs.go`).

**Exec + fetch (`diffHunks`):**

```go
// diffHunks fetches the unified diff of one repo-relative path against the same
// HEAD-aware base the +N/-N badge uses (FR2/D7), colorizes each line, and returns
// preview lines. preStyled=true (ANSI verbatim, like code). On any failure or an
// empty diff it returns (nil, false, err/sentinel) so syncPreview's caller degrades
// to full content (FR6/FR10). repoRoot + relPath are captured from model state by
// the syncPreview closure — NOT passed through the registry render signature (D2).
func diffHunks(repoRoot, relPath string, width int) ([]string, bool, error)
```

Bên trong:
1. HEAD-aware base (D7) + `--no-color`: `runGit(repoRoot, "rev-parse", "--verify", "-q", "HEAD")`
   thành công → `runGit(repoRoot, "diff", "--no-color", "HEAD", "--", relPath)`; lỗi →
   `runGit(repoRoot, "diff", "--no-color", "--cached", "--", relPath)`. Cùng branch numstat dùng
   (`git.go:177-183`). `--no-color` ép git xuất plain ngay cả khi repo/global đặt
   `color.ui=always` (hoặc `color.diff=always`) — không có nó git tự nhét SGR escape vào mỗi dòng
   `+`/`-`, khiến `diffPrefix` đọc `line[0]==0x1b` và cả diff tô mờ thay vì xanh/đỏ (D11/FR1).
2. Output rỗng (empty diff, D10/FR6) → trả sentinel "no diff" → caller fallback content.
3. Parse + tô từng dòng theo **vị trí**, không theo prefix 3-ký-tự: preamble (`diff --git`/`index`/
   `--- a/…`/`+++ b/…`) xuất hiện **một lần** trước `@@` hunk header đầu tiên → tô `colDim`; từ
   `@@` trở đi mỗi dòng key thuần theo `line[0]`: `+` → `colDiffAdd`, `-` → `colDiffDel`, ` `/`@`/`\`
   → `colDim`. Vì diff chạy trên một path nên output là một preamble rồi tới hunks — `seenHunk`
   bool tách hai vùng (set true ở `@@` đầu tiên). Key header theo prefix `---`/`+++` sẽ đọc nhầm
   dòng **content** mở đầu `-`/`+` (dòng xóa `-- comment` render `--- keep comment`; dòng thêm
   `++x` render `+++x`) thành header → tô mờ thay vì đỏ/xanh. Giữ nguyên ký tự đầu dòng (`+`/`-`/` `)
   để đọc được khi mất màu (D11). Mỗi dòng style **độc lập, self-contained ANSI** (như
   `renderCodePreview` `fs.go:455` để `renderHWindow` slicing không cắt giữa escape — FR8).
4. Không wrap ở đây (như code): trả dòng logic, để `previewScrollable` + `renderHWindow` lo
   horizontal (D9).

**Resilience (FR10):** mọi `runGit` lỗi / timeout → trả error → caller degrade content. Không
panic, không bubble lên frame. Diff exec dùng `runGit` (timeout `gitCmdTimeout` `git.go:109-119`).

**An toàn:** chỉ exec `git` arg cố định (slice arg, không qua shell); `relPath` đến từ model
state (entry name dưới jail root), đi sau `--` nên không bị hiểu là flag. `git -C <repoRoot>`
với repoRoot trusted (jail root resolved).

### 5.2 State-selection trong `refreshPreview` (`model.go`)

Thêm field `diffOn bool` trên `model` (cạnh `previewWrap` session-pref, init **true** ở
`newModel`, D3). Thêm `previewIsDiff bool` (cạnh `previewIsDir` `model.go:157`, reset hygiene
như nó).

Trong `refreshPreview` (`model.go:487-601`), **trước** block `rendererFor` (`model.go:580`) —
diff applicability độc lập với việc highlighter có match hay không (một `.xyz` text modified vẫn
được diff):

```go
// reset hygiene block đã có thêm: m.previewIsDiff = false (mọi nhánh non-diff để off)

// ... sau readPreviewBytes(full, sel.size) → content, kind ...
if m.diffOn && m.diffApplies(sel, kind) { // tracked-modified (M/R) & text & in-repo
    m.srcPath = full
    m.previewIsDiff = true
    m.previewScrollable = true            // mirror code (D9): scroll + hwindow
    m.srcRaw = []byte(normalizeText(content)) // error/empty-diff fallback (FR6/FR10) renders this as full content
    m.preview = plainLines(content)        // instant placeholder until diff lands
    return
}
// ... binary-modified placeholder (FR5) hoặc tiếp tục rendererFor block sẵn có ...
```

`diffApplies(sel, kind)` (model method, đọc `m.git`): true **chỉ** khi `m.git.repoRoot != ""`,
`kind == "text"`, và `m.git.changes[rel].code` ∈ {`gitModified`, `gitRenamed`} cho rel resolve
qua **cùng** `indicatorFor` path (`model.go:339-365`, tái dùng resolve logic — repo-rel key đúng
cả khi jail là subdir). Mọi code khác rơi về content path (D6): `gitAdded`/`gitUntracked` (all-new
→ content **là** diff hữu ích) và `gitConflict` (content show marker `<<<<<<<` thẳng) → false →
renderer block sẵn có (FR4). Binary-modified (`kind != "text"` & code `M`/`R`) → nhánh placeholder
riêng (FR5), không vào content renderer.

### 5.3 Dispatch trong `syncPreview` (`model.go`)

Trong `syncPreview` (`model.go:745-788`), **trước** `rendererFor(...)` (`model.go:767`):

```go
if m.previewIsDiff {
    m.renderGen++
    m.pendingWidth = w
    if m.tel.Active() { m.renderStartedAt = time.Now() }
    gen, repoRoot, raw := m.renderGen, m.git.repoRoot, m.srcRaw
    // resolve repo-rel path once here (model state), capture into closure
    relPath := m.diffRelPath() // same resolve as indicatorFor, for m.srcPath
    return func() tea.Msg {
        lines, preStyled, err := diffHunks(repoRoot, relPath, w)
        if err != nil {
            // FR6/FR10 degrade: render captured source as full content
            return previewRenderedMsg{gen: gen, width: w, lines: plainLines(raw), preStyled: false, err: nil}
        }
        return previewRenderedMsg{gen: gen, width: w, lines: lines, preStyled: preStyled, err: nil}
    }
}
r, ok := rendererFor(filepath.Base(m.srcPath)) // existing path unchanged
```

Closure capture `repoRoot` + `relPath` + `raw` (cho fallback) — **không** đi qua registry
`render(path,content,width,style)` signature (D2). Trả đúng `previewRenderedMsg` → **`applyPreview`
(`model.go:813-867`) không đổi**: gen-gate drop stale, set `previewPreStyled = msg.preStyled`,
error-fallback đã có. `reconcilePreview` spinner loop (`model.go:797-805`) cũng không đổi —
`pendingWidth > 0` kick spinner như mọi render.

**Toggle handler (`updateNormal`, `model.go:1262+`):** thêm `case key.Matches(msg, km.ToggleDiff)`
cạnh `PreviewToggleWrap` (`model.go:1369`):

```go
case key.Matches(msg, km.ToggleDiff): // v — diff ↔ full content (this PRD D4/D12)
    m.diffOn = !m.diffOn
    m.tel.Record("action.preview_diff_toggle", map[string]any{"diff": m.diffOn}) // mirror wrap (D14/FR12)
    m.refreshPreview() // resets srcWidth/pendingWidth → syncPreview re-dispatches (D12)
    // fall through to reconcilePreview at Update tail
```

Gọi `refreshPreview()` là bắt buộc (D12): nó reset `srcWidth=0`/`pendingWidth=0`
(`model.go:519-525`) nên `syncPreview` staleness check `srcWidth==w` (`model.go:761`) fire +
re-dispatch. Bỏ nó → toggle no-op.

### 5.4 View (`view.go`)

`renderPreview` (`view.go:584-638`) **không cần nhánh mới**: diff đi qua đúng nhánh
`previewScrollable` (D9) — `previewPreStyled=true` (ANSI verbatim) + nowrap → `renderHWindow`
(`view.go:645`); wrapped → `previewDisplay` path. Dòng diff tô ANSI self-contained nên slicing
an toàn (FR8). Đây là lợi ích của việc trả `previewRenderedMsg` chuẩn: view layer type-agnostic
với "content này là diff hay code".

### 5.5 Palette / theme (`theme.go`, `keys.go`)

- `keys.go`: thêm field `ToggleDiff key.Binding` (cụm Preview), `key.NewBinding(key.WithKeys("v"),
  key.WithHelp("v", "toggle diff"))`. Help text hiện trong full-help overlay `?` tự động (keymap
  là single source).
- `theme.go`: `colDiffAdd = colGitNew` (xanh, `theme.go:24`), `colDiffDel = colDanger` (đỏ,
  `theme.go:20`) — alias cho rõ ý ngữ cảnh diff, không màu mới; hunk/context dùng `dimStyle`
  (`colDim`). Hàm `diffLineStyle(prefix byte) lipgloss.Style` map `+`/`-`/khác → style.

### 5.x Đã cân nhắc & **defer khỏi v1**

- **Diff MODE (`modeDiff` + nhánh Update riêng):** bác (Non-goal/D2). Content variant + toggle key
  đủ; mode mới phá "two panels là trần".
- **Side-by-side / split diff:** bác (Non-goal). Single preview pane là trần; unified diff đủ
  glance cạnh agent.
- **Staging / unstage / hunk-apply:** bác (Non-goal). Read-only review; lazyexplorer không phải
  git client. Drift sang git-client là chống lại positioning.
- **Diff vs arbitrary ref/branch (`--against <ref>`):** bác (Non-goal/D7). Base cố định khớp badge
  để dòng reconcile con số; chọn ref = UI complexity + decouple khỏi badge.
- **Hunk-to-hunk jump (`]c`/`[c` next/prev hunk):** defer. v1 dùng scroll sẵn có; "next hunk" là
  key thứ hai, thêm sau nếu file dirty lớn chứng minh cần.
- **Word-level intra-line diff (`--word-diff`):** defer. Line-level đủ glance; word-diff = parse
  phức tạp hơn, chưa chứng minh cần.
- **Deleted/renamed preview branch:** bác (D6) — unreachable, build = dead code.
- **Bundle open-in-editor / yank-relative-path / jump-to-changed:** defer (Non-goal). Mỗi cái là
  follow-up riêng; gộp = scope creep phá single-coherent-change.
- **Syntax-highlight TRONG dòng diff (chroma + diff màu chồng):** defer. v1 tô theo diff prefix
  (xanh/đỏ/mờ); highlight nội dung trong dòng thêm là tô-chồng phức tạp, chưa cần cho review.

## 6. Acceptance criteria

### Gherkin

```gherkin
Feature: Diff view in the preview pane for a changed file

  When the cursor lands on a file an agent has modified, the preview pane shows
  the file's diff — added lines coloured, removed lines coloured, computed
  against the same HEAD the +N/-N badge counts — so a change can be reviewed
  without leaving the pane. One key toggles back to the full file. A clean or
  untracked file previews its content as before.

  Background:
    Given the explorer is open inside a git repository

  Scenario: A modified file shows its diff by default on landing
    Given a tracked file has been modified since the last commit
    When I move the cursor onto that file
    Then the preview pane shows the file's diff
    And the added lines are coloured as additions
    And the removed lines are coloured as removals

  Scenario: The shown diff reconciles with the badge count
    Given a tracked file shows a "+2 -1" change badge
    When I view that file's diff in the preview pane
    Then the diff shows two added lines and one removed line

  Scenario: Toggling shows the full file content
    Given a modified file is showing its diff in the preview pane
    When I press the diff toggle
    Then the preview pane shows the full file content instead

  Scenario: Toggling back returns to the diff
    Given a modified file is showing its full content after a toggle
    When I press the diff toggle again
    Then the preview pane shows the file's diff again

  Scenario: The diff preference persists across files
    Given I have toggled the preview to show full content
    When I move the cursor to another modified file
    Then that file also shows its full content, not its diff

  Scenario: An untracked file shows its content
    Given an untracked file exists
    When I move the cursor onto that file
    Then the preview pane shows the file's content

  Scenario: A newly staged file shows its content
    Given a brand-new file has been staged with git add
    When I move the cursor onto that file
    Then the preview pane shows the file's content

  Scenario: A conflicted file shows its content with the merge markers
    Given a tracked file is in a merge conflict
    When I move the cursor onto that file
    Then the preview pane shows the file's content

  Scenario: The diff refreshes while the cursor stays on the file
    Given a modified file is showing its diff in the preview pane
    When the agent saves further changes to that same file
    Then the preview pane updates to show the new diff without my navigating away

  Scenario: A clean file shows its content
    Given a tracked file matches the last commit exactly
    When I move the cursor onto that file
    Then the preview pane shows the file's content
    And the diff toggle has no diff to show for it

  Scenario: A modified binary file reports that it differs
    Given a tracked binary file has been modified
    When I move the cursor onto that file
    Then the preview pane reports that the binary files differ
    And no diff hunks are shown

  Scenario: A mode-only change with no textual diff falls back to content
    Given a tracked file is flagged changed but its text is unchanged
    When I move the cursor onto that file
    Then the preview pane shows the file's content rather than an empty pane

  Scenario: A brand-new repository with no commits still diffs a staged file
    Given the repository has no commits yet
    And a staged file has been changed
    When I view that file in the preview pane
    Then the preview pane shows the staged change as a diff

  Scenario: Outside a git repository the preview is unchanged
    Given the explorer is open in a directory that is not a git repository
    When I move the cursor onto any file
    Then the preview pane shows the file's content
    And the diff toggle never produces a diff

  Scenario: A long diff scrolls within the pane
    Given a modified file with a diff longer than the preview pane
    When I focus the preview and scroll down
    Then the diff scrolls within the pane without breaking the row width
```

### Checklist verify

1. File tracked sửa (text) → land vào → preview hiện **diff** (default `diffOn=true`); dòng `+`
   tô `colDiffAdd`, `-` tô `colDiffDel`, `@@`/context mờ. `v` → full content; `v` lần nữa → diff.
2. Reconcile badge: file badge `+2 -1` → diff hiện đúng 2 dòng `+` + 1 dòng `-` (cùng base HEAD
   D7 → số khớp). Đếm **chỉ** dòng `+`/`-`; dòng context (3 dòng mặc định, đầu dòng ` `) KHÔNG
   tính vào con số (FR2).
3. Persistence: `v` về content, điều hướng sang file modified khác → vẫn content (`diffOn`
   session-sticky, D3/FR3).
4. Untracked (`?`) **và** staged-new (`A`) **và** conflict (`U`) → preview content (FR4/D6); `v`
   không đổi nội dung. File conflict show marker `<<<<<<<` trong content.
5. File sạch / dir / `..` → preview như hôm nay; `v` không kích hoạt diff (FR4).
6. Binary modified → placeholder "binary files differ", không exec diff render (FR5).
7. Empty diff dù badge `M` (vd `chmod +x` file đã commit) → fallback full content, pane không
   rỗng (FR6/D10).
8. Repo no-commit (`git init` chưa commit) + file staged đã đổi → `diff --cached` cho hunks,
   diff hiện đúng (D7/FR2 — ĐÃ VERIFY ✅ 2026-05-29 ở git CLI).
9. Ngoài git repo (`t.TempDir()` không `git init`) → diff không kích hoạt, `v` no-op, preview
   content, không crash (FR9).
10. Async/stale: scroll nhanh qua nhiều file modified → mỗi pane hiện diff đúng file của nó (không
    file sai); gen-gate drop kết quả stale (FR7) — unit test `applyPreview` drop `gen != renderGen`.
11. Resilience: `diffHunks` lỗi giả lập (repoRoot trỏ chỗ không phải repo / git fail / timeout) →
    degrade về full content, app không panic (FR10) — unit test.
11a. Live-refresh parked (FR11/D13): file đang đứng cursor đang show diff → agent ghi thêm vào
    file đó → một `tickMsg` qua `Update` (size/mtime đổi → `dirSig` fire) → diff pane cập nhật
    sang state mới, KHÔNG cần điều hướng — unit/teatest. Sibling churn (file khác đổi, file parked
    byte-identical) → diff KHÔNG re-render (byte-identity guard `model.go:457-461`).
11b. Telemetry (FR12/D14): `v` toggle → recorder bắt `action.preview_diff_toggle` với `diff` =
    state mới — unit test trên fake Recorder.
12. Scroll + hwindow: diff dài → vertical scroll (focusPreview j/k/ctrl+d/G) + nowrap horizontal
    window (h/l/H/L/0) hoạt động; dòng diff tô màu sống sót `renderHWindow` slicing, căn cột,
    frame không vỡ width (FR8/D9).
13. Parse thuần (unit test trên `diffHunks` colorize): unified diff với `diff --git`/`index`/
    `---`/`+++`/`@@`/` `/`+`/`-` → đúng style mỗi dòng; ký tự đầu dòng giữ nguyên (đọc được khi
    mất màu).
14. **Dogfood (`zz_dogfood_test.go` T1):** re-run `TestDogfoodBesideAgent/T1_find_changed_file` —
    sau change này, sau khi land vào `src/handlers/auth.go` và toggle/diff-on, preview hiển thị
    **hunks** (`+func Login() {}`) chứ không chỉ full file body; log ghi `diff_now_achievable=true`
    là **/goal proof** (review-the-edit-in-pane giờ đạt được), phân biệt với build gate. (Cập nhật
    T1 assertion để đo "diff hunks visible" thay vì chỉ "func Login visible in full content".)
15. Visual verdict (`oh-my-claudecode:visual-verdict`) trên ảnh render diff: màu thêm/bớt/context
    đúng, căn cột đúng, dòng dài pan không vỡ, đọc được — pass.
16. `go build -o lazyexplorer . && go vet ./... && go test ./...` xanh.

## 7. Task breakdown

> Đề xuất, chờ duyệt trước khi tick. TDD: test đỏ trước, code tới xanh (`CLAUDE.md` §Testing).
> Map sát file. Cover mọi tầng (git diff fetch/colorize, model state-select + async, view render).

- [x] **T1 — Diff fetch + colorize thuần (`git.go` + test đỏ trước).** `diffHunks(repoRoot,
  relPath, width)`: HEAD-aware base (`diff HEAD` | `diff --cached` fallback, D7); parse unified
  diff; tô mỗi dòng theo prefix (`+`/`-`/`@@`/context) self-contained ANSI; giữ ký tự đầu; empty
  diff → sentinel; resilient mọi lỗi → error (caller fallback). §5.1. *(git.go, git_test.go)*
- [x] **T2 — Theme diff styles (`theme.go`).** `colDiffAdd`/`colDiffDel` alias + `diffLineStyle`
  map prefix→style; hunk/context dùng `dimStyle`. §5.5. *(theme.go)*
- [x] **T3 — `diffApplies` + state-select ở `refreshPreview` (`model.go`).** `diffOn bool` (init
  true ở `newModel`, D3), `previewIsDiff bool` (+ reset hygiene); `diffApplies(sel, kind)` đọc
  `m.git` (modified/renamed & text & in-repo); nhánh diff **trước** `rendererFor`; binary-modified
  → placeholder (FR5); empty/untracked/clean → content path. §5.2. *(model.go, model_diff_test.go)*
- [x] **T4 — Dispatch async ở `syncPreview` (`model.go`).** Nhánh `previewIsDiff` trước
  `rendererFor`: bump `renderGen`, closure bespoke capture `repoRoot`/`relPath`/`raw`, exec
  `diffHunks`, error → fallback `plainLines(raw)`; trả `previewRenderedMsg`. `applyPreview` KHÔNG
  đổi. `diffRelPath()` helper resolve repo-rel (tái dùng `indicatorFor` logic). §5.3.
  *(model.go, model_diff_test.go)*
- [x] **T5 — Toggle key + telemetry (`keys.go` + `model.go`).** `ToggleDiff` binding `v` (cụm
  Preview); `case km.ToggleDiff` ở `updateNormal`: flip `diffOn` + `m.tel.Record("action.preview_
  diff_toggle", {diff})` (D14/FR12) + `refreshPreview()` (D12). §5.3/§5.5.
  *(keys.go, model.go, model_diff_test.go)*
- [x] **T6 — Render diff tests (`view.go` path).** Verify diff đi qua `previewScrollable` nhánh
  của `renderPreview` không nhánh mới; dòng tô màu sống sót `renderHWindow` slicing (nowrap
  horizontal) + wrapped path; căn cột; width-fit. §5.4. *(diff_render_test.go)*
- [x] **T7 — Reconcile ADR.** Cập nhật `adr-preview-renderer-registry.md`: thêm Quyết định ghi
  diff variant **state-selected ở model layer, KHÔNG name-matched** — distinction kiến trúc với
  markdown/code (registry quyết theo filename; diff quyết theo `(dirty, diffOn)` state). Positive
  framing. §5.x. *(docs/adr-preview-renderer-registry.md)*
- [x] **T8 — Dogfood /goal proof.** Cập nhật `zz_dogfood_test.go` T1 `find-changed-file`: sau land
  vào `auth.go` + diff-on, assert preview hiển thị **diff hunks** (`+func Login`) không chỉ full
  body; log `diff_now_achievable=true` + keystroke. §6.14. *(zz_dogfood_test.go)*
- [x] **T9 — Verify.** `go build -o lazyexplorer . && go vet ./... && go test ./...` xanh (+ `-race`);
  chạy tay checklist §6; visual verdict trên ảnh render diff (màu thêm/bớt/context/căn/pan).

## 8. Files chạm tới

| File | Thay đổi |
|------|----------|
| `git.go` | + `diffHunks(repoRoot, relPath, width)` — HEAD-aware `git diff HEAD`/`--cached` qua `runGit`; parse unified diff; colorize self-contained ANSI theo prefix; empty→sentinel; resilient degrade |
| `git_test.go` | + unit test `diffHunks`: colorize từng dòng (`+`/`-`/`@@`/context), HEAD vs `--cached` fallback, empty diff, lỗi→error |
| `theme.go` | + `colDiffAdd`/`colDiffDel` (alias `colGitNew`/`colDanger`) + `diffLineStyle(prefix)` map; hunk/context dùng `dimStyle` |
| `keys.go` | + `ToggleDiff key.Binding` (`v`, cụm Preview, help "toggle diff") |
| `model.go` | + `diffOn bool` (init true `newModel`); + `previewIsDiff bool` (+ reset hygiene); + `diffApplies` (chỉ `M`/`R`+text; added/untracked/conflict → content)/`diffRelPath`; nhánh diff state-select ở `refreshPreview` (trước `rendererFor`); dispatch async ở `syncPreview` (closure bespoke); `case ToggleDiff` ở `updateNormal` (flip + `Record(action.preview_diff_toggle)` + refreshPreview). `applyPreview`/`syncFromDisk` KHÔNG đổi (live-refresh thừa kế) |
| `model_diff_test.go` | **mới** — `diffApplies` predicate (modified/renamed/added/untracked/conflict/binary/clean/no-repo); state-select branch; async dispatch + gen-gate/stale + error-fallback; toggle flip + re-dispatch + persistence + telemetry; parked live-refresh qua tick |
| `diff_render_test.go` | **mới** — diff render qua `renderPreview` scrollable path; tô màu sống sót `renderHWindow` slicing; width-fit; wrapped/nowrap |
| `zz_dogfood_test.go` | T1 `find-changed-file`: assert diff hunks visible sau diff-on (/goal proof) |
| `docs/adr-preview-renderer-registry.md` | + Quyết định: diff variant state-selected ở model layer (không name-matched) — distinction với markdown/code |
| `docs/prd-preview-diff-view.md` | File này |

## Clarifications

### Session 2026-05-29

- Q: Code git nào (ngoài `M`/`R`) tới được preview, và đi đâu? → A: `A` (staged-new) & `?`
  (untracked) → **full content** (all-new, content là diff hữu ích); conflict (`U`/`AA`/`DD`) →
  **full content** (đọc thẳng marker `<<<<<<<`); chỉ `M`/`R`+text → diff. `D`/old-rename
  unreachable (`fs.go` list working-tree). Đã rewrite D6 + FR4 + `diffApplies` §5.2 + Gherkin.
- Q: Diff của file đang đứng cursor có tự cập nhật khi agent ghi lại không, hay phải navigate
  away rồi back? → A: **Auto cập nhật** trên poll loop (~1s) — `dirSig` fire (`model.go:411`) →
  `syncFromDisk` gọi `refreshPreview` (`model.go:466`) → `syncPreview` re-dispatch; KHÔNG máy móc
  mới. Đã thêm D13 + FR11 + Gherkin + checklist 11a.
- Q: Có chip/header báo "đang ở diff hay content" không? → A: **Không** — dòng `+`/`-` tô màu
  (D11) đã là tín hiệu; chip = chrome thừa (minimal-chrome). Đã thêm Non-goal.
- Q: Toggle `v` có ghi telemetry như wrap không? → A: **Có** — `action.preview_diff_toggle`
  `{diff}` mirror `action.preview_wrap_toggle` (`model.go:710`). Đã thêm D14 + FR12 + handler
  §5.3 + checklist 11b + T5.
- Q: Diff dùng bao nhiêu dòng context, và reconcile với badge tính dòng nào? → A: **3 dòng**
  context mặc định của `git diff` (tô `colDim`); reconcile `+N/-N` **chỉ** đếm dòng `+`/`-`, KHÔNG
  tính context. Đã tighten FR2 + checklist 2.
