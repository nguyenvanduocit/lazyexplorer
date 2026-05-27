# PRD — Recursive fuzzy filename search

> Feature: nhấn `/` trong list panel → status bar trở thành prompt
> `/foo▏`, list panel hiện file/folder fuzzy-match query từ **toàn project**
> (jailed to root, lọc theo `.gitignore`). Enter trên 1 result nhảy `cwd` vào
> parent + cursor land trên file đó; Esc khôi phục nguyên trạng thái cũ. Mục
> tiêu: liếc-và-nhảy đúng file mà agent vừa nói tới, không phải descend từng
> folder.

Status: **draft / chờ review** · Author: feature-dev session · Ngày: 2026-05-27

---

## 1. Bối cảnh & vấn đề

Hiện list panel (`view.go:99`) chỉ hiển thị **một thư mục** — `m.cwd`. Để mở
`docs/prd-markdown-view.md` từ root, user phải `enter` vào `docs/`, scroll
tới file, mới mở. Khi agent bên cạnh nói *"tôi vừa sửa `model.go:189`"*,
user mất 3-5 thao tác mới tới được file đó.

Hai constraint định hình thiết kế:

- **Two-panel ceiling** (`CLAUDE.md`, "Glance-friendly"): không thêm panel
  thứ ba; result list phải **sống trong list panel hiện có**.
- **Jailed to root** (`fs.go:88`, `withinRoot`): search không bao giờ trả về
  file ngoài jail root.

`tmp/superfile` có search recursive nhưng dùng modal CtrlF — vi phạm
"minimal chrome". `tmp/lipgloss` + glow-style status bar prompt (giống mode
`rename` hiện có ở `view.go:170`) cho phép search không thêm panel/overlay.

## 2. Goal (1 câu)

Khi user nhấn `/` ở `modeNormal`, app vào `modeSearch`: status bar hiển thị
prompt fuzzy-input, list panel hiện top N file/folder match query trên toàn
subtree dưới root (lọc theo `.gitignore`, luôn bỏ `.git/`). Enter chọn
result → `cwd` nhảy vào parent của file (hoặc chính folder nếu result là
dir) + cursor land trên đúng item đó + thoát search. Esc thoát giữ nguyên
state trước search.

**Non-goal làm rõ:** đây KHÔNG phải content/grep search (defer v2);
KHÔNG cross-project; KHÔNG modal popup overlay; KHÔNG persist search
history qua session; KHÔNG highlight matched chars trong result text v1;
KHÔNG ranking theo mtime/recency v1.

## 3. Quyết định đã chốt (từ phiên hỏi)

| # | Quyết định | Giá trị | Lý do |
|---|-----------|---------|-------|
| D1 | Deliverable | **PRD đứng riêng** trong `docs/prd-search.md`; implement ở bước sau khi duyệt | house style; chốt thiết kế trước khi đụng code (giống `prd-markdown-view.md` D1) |
| D2 | Scope v1 | **Recursive filename** toàn project (jailed to root); content/grep search defer v2 | content search vi phạm two-panel ceiling (cần result browser file:line:context) — là việc của ripgrep ở pane bên cạnh, không phải lazyexplorer |
| D3 | Activation key | **`/`** (lazygit/vim style) | tham chiếu chính của project là lazygit; `/` quen muộc, không đụng key đã dùng (`view.go:175` hint bar không có `/`) |
| D4 | Mode style | **Status-bar prompt + filter live**, tái dùng pattern `modeRename` (`model.go:629`, `view.go:170`) | tối giản nhất, 0 panel mới, tái dùng `mode` enum + `Update` dispatch sẵn có |
| D5 | Match algorithm | **Fuzzy subsequence** kiểu fzf, case-insensitive | quen với user fzf/Telescope; gõ ít, hit nhanh — phù hợp với "glance" |
| D6 | Enter behavior | **cd vào parent của file + cursor land trên result + exit search**; folder result → cd vào folder | giống fzf cd-style; user thấy ngay file trong context folder bình thường, không bị mắc kẹt trong list result rời rạc |
| D7 | Ignore strategy | **Respect `.gitignore`** ở root + **always-skip `.git/`** (cả khi `.git/` không có trong `.gitignore`) | ripgrep-default; vibe-code user không muốn `node_modules/`/`dist/`/`vendor/` ngập result |
| D8 | Walk timing | **Walk async** trong goroutine, fresh-on-activate; cache trong session với **TTL 30s** | walk repo 5k file ~30-80ms — quá nhỏ để build watcher (overkill), nhưng đủ to để không re-walk mỗi keystroke. 30s TTL cân giữa accuracy và rapid re-activate |
| D9 | Fuzzy library | **`github.com/sahilm/fuzzy`** (dep mới) | industry standard — `tmp/glow`, lazygit, `tmp/gh-dash`, `bubbles.list` filter đều dùng; self-roll ranking chất lượng kém hơn |
| D10 | Gitignore library | **`github.com/sabhiram/go-gitignore`** (dep mới) | nhỏ, focused, KHÔNG kéo theo cả `go-git` ecosystem; API đơn giản (`CompileIgnoreFile` + `MatchesPath`) |
| D11 | Result cap | **Top 500 matches** displayed; empty query → toàn bộ walked alpha-sorted cap 500 | sub-ms render; >500 → user gõ thêm để refine, không scroll-fest |
| D12 | Mouse trong search | **Disabled toàn bộ** (giống `modeRename`/`modeConfirmDelete`) | nhất quán với mode prompt khác; search là keyboard-driven primarily |
| D13 | Cancel & restore | **Esc** *hoặc* **Backspace ở query rỗng** → exit + restore full snapshot (`cwd`, `entries`, `cursor`, `listTop`) trước khi vào search | shell muscle-memory (BS empty = back out); Esc luôn hoạt động |

## 4. Functional requirements

- **FR1** — Trong `modeNormal`, nhấn `/` → vào `modeSearch`. Status bar đổi
  thành prompt `/▏` (caret blink, hint bar ẩn). Selection hiện tại + cwd +
  list state được snapshot (cho FR9 restore).

- **FR2** — Lần đầu vào `modeSearch` trong session, hoặc khi cache walk >
  30s, app dispatch walk async (goroutine). Trong khi walk: chip
  `• indexing…` hiện trong status bar bên cạnh prompt; list panel rỗng;
  user vẫn gõ được query (filter sẽ apply ngay khi walk xong).

- **FR3** — Walk recursive từ `m.root` dùng `filepath.WalkDir`. **Bỏ
  qua**: (a) thư mục tên `.git` (cứng, regardless of `.gitignore`); (b)
  bất kỳ entry nào khớp pattern trong `<root>/.gitignore`; (c) symlink
  (không follow, không include — tránh loop + leak ngoài jail). Trả về
  flat list `[]entry` với `name = relPath` so với root (vd `docs/prd-…md`).

- **FR4** — Khi query rỗng: list hiện **toàn bộ walked entries** sắp xếp
  alpha theo `relPath`, cap 500 đầu.

- **FR5** — Khi query không rỗng: chạy `fuzzy.Find(query, names)` (case-
  insensitive — sahilm/fuzzy mặc định), trả top 500 match sort theo score
  giảm dần. Tie-break: path ngắn trước (`len(relPath)` tăng dần).

- **FR6** — Mỗi keypress (gõ ký tự / backspace) trong `modeSearch` →
  recompute filter ngay, list panel cập nhật, `m.cursor` reset về 0 (top
  match), `m.listTop` reset về 0.

- **FR7** — Preview panel **tiếp tục hoạt động** cho selected result: nó
  hiển thị file/folder đang được highlight trong result list. Pipeline
  preview hiện tại (`refreshPreview`/`syncPreview`, renderer registry —
  markdown/code/image) không đổi; chỉ điểm `base` resolve khác: trong
  `modeSearch`, base = `m.root`; ngoài, base = `m.cwd` (§5.6).

- **FR8** — **Enter trên result là file**: tính `parent = filepath.Dir(
  filepath.Join(m.root, sel.name))`; jail-check; set `m.cwd = parent`,
  reload, cursor land trên `filepath.Base(sel.name)`, `mode = modeNormal`.

- **FR9** — **Enter trên result là folder**: tính `target = filepath.Join(
  m.root, sel.name)`; jail-check; set `m.cwd = target`, reload (cursor
  về top), `mode = modeNormal`.

- **FR10** — **Esc** *hoặc* **Backspace ở query rỗng** → restore full
  snapshot (cwd, entries, cursor, listTop, fsSig) đã lưu ở FR1, set
  `mode = modeNormal`, `refreshPreview()` đồng bộ. User thấy app đúng như
  trước khi nhấn `/`.

- **FR11** — `tickCmd` poll loop (`model.go:316`) **skip `syncFromDisk`**
  khi `mode == modeSearch` (mở rộng guard hiện có dành cho
  `modeRename`/`modeConfirmDelete` ở `model.go:326`). Tránh churn list
  result khi agent ghi file bên cạnh — search vẫn fresh khi user thoát.

- **FR12** — `handleMouse` (`model.go:369`) early-return khi
  `mode != modeNormal` (đã đúng) → mouse mặc nhiên vô hiệu trong search,
  không cần code mới (D12).

- **FR13** — Walk lỗi (permission denied dir): skip dir đó, continue (Walk
  callback trả `nil` khi gặp err non-fatal). Toàn cuộc walk không bao giờ
  panic. `.gitignore` parse lỗi: warn ở status (`⚠ .gitignore parse:
  …`) + walk tiếp **không** ignore (degrade gracefully, không fail).

- **FR14** — Walk cap **100 000 entries** defensive (project quá lớn ngoài
  scope vibe-code companion); vượt → cắt + status `(walk capped at 100k —
  refine project scope)`. Trong thực tế repo <50k.

- **FR15** — Empty result (query không match gì) → list pane trống, status
  hiện hint `(0 results — refine or Esc)`.

## 5. Technical design

> **Kim chỉ nam:** tái dùng `mode` enum + `Update` dispatch + preview
> pipeline hiện có. Search **không** thay đổi `renderList`/`renderPreview`
> — chỉ thêm một mode nữa, swap `m.entries` sang result list khi vào search
> (snapshot/restore khi ra), branch `base` resolve trong `refreshPreview`.
> Renderer registry (markdown/code/image) hoạt động nguyên xi cho result
> đang được highlight: preview của `docs/prd-markdown-view.md` chọn từ
> search vẫn render glamour, `model.go` vẫn highlight chroma.

### 5.1 Dependency

Hai dep mới, đều nhỏ và stable:

```go
import (
    "github.com/sahilm/fuzzy"        // ranking fuzzy subsequence
    gitignore "github.com/sabhiram/go-gitignore" // parse + match .gitignore
)
```

| Dep | Verify tại T1 (implementer chạy `go get` + `go doc`) | Falsification |
|---|---|---|
| `sahilm/fuzzy` | `fuzzy.Find(pattern string, data []string) fuzzy.Matches` tồn tại; `Matches[i].Index` map ngược về `data`; sort theo Score desc | `go test -run TestFuzzyContract` (T9) — pass nếu `Find("foo", []string{"food","far","abcfoo"})` trả >0 match, Index hợp lệ |
| `sabhiram/go-gitignore` | `gitignore.CompileIgnoreFile(path) (*GitIgnore, error)` + `(*GitIgnore).MatchesPath(string) bool` | `go test -run TestGitIgnoreContract` (T9) — compile mẫu chứa `node_modules/`, assert `MatchesPath("node_modules/foo")` true |

Cả hai đều MIT, low-dep tree, **không** kéo theo module mới đáng kể (sahilm/fuzzy
zero-dep; sabhiram/go-gitignore chỉ kéo `regexp` stdlib). Implementer chạy
`go get … && go mod tidy`, không cần `-u` upgrade dep hiện có.

### 5.2 Mode + state on `model` (`model.go`)

Mở rộng enum `mode` (`model.go:38`):

```go
const (
    modeNormal mode = iota
    modeConfirmDelete
    modeRename
    modeSearch // mới
)
```

Thêm field vào `model` (`model.go:46`):

| Field | Ý nghĩa |
|-------|---------|
| `searchQuery string` | Query đang gõ (rune-string, không bao gồm `/` activation prefix) |
| `searchAll []entry` | Walked entries — flat list, `entry.name = relPath`. Nguồn cho filter. |
| `searchAllAt time.Time` | Thời điểm walk hoàn thành — TTL check (D8, 30s). |
| `searchIndexing bool` | `true` khi walk đang chạy → status bar hiện chip `indexing…`. |
| `searchGen uint64` | Gen-counter cho walk async — kết quả về trễ của walk cũ bị drop (cùng pattern với `renderGen`, `model.go:73`). |
| `searchSavedCwd string` | Snapshot `m.cwd` lúc vào search (FR10 restore). |
| `searchSavedEntries []entry` | Snapshot `m.entries`. |
| `searchSavedFsSig uint64` | Snapshot `m.fsSig` — restore baseline cho poll loop. |
| `searchSavedCursor int` | Snapshot `m.cursor`. |
| `searchSavedListTop int` | Snapshot `m.listTop`. |

`m.entries`, `m.cursor`, `m.listTop` được **re-purpose** khi vào search: từ
"entries của cwd" sang "result list theo query". `entry.name` trong search
là `relPath` (vd `docs/prd-markdown-view.md`); `entry.isDir`/`size`/
`modTime` vẫn đúng. `renderList` (`view.go:99`) không cần biết mode —
chỉ vẽ slice nó nhận.

### 5.3 Walking — `walkTree` (`fs.go`)

```go
// walkTree walks root recursively as a flat list of file/folder entries with
// names relative to root, respecting .gitignore at root and always skipping
// .git/. Symlinks are not followed and not included (jail safety + no loops).
// Permission errors on a sub-directory skip that directory and continue.
func walkTree(root string) ([]entry, error) {
    ignore, _ := gitignore.CompileIgnoreFile(filepath.Join(root, ".gitignore"))
    // ignore == nil khi không có .gitignore — coi như allow-all (kết hợp với
    // hardcode skip .git/ ở dưới là đủ default sane).

    var out []entry
    err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
        if err != nil {
            if d != nil && d.IsDir() {
                return fs.SkipDir // permission denied trên dir → bỏ subtree, không panic
            }
            return nil
        }
        if path == root {
            return nil // không emit root chính nó
        }
        name := d.Name()
        if d.IsDir() && name == ".git" {
            return fs.SkipDir // D7: always skip .git/
        }
        if d.Type()&fs.ModeSymlink != 0 {
            return nil // không follow + không emit symlink (FR3)
        }
        rel, err := filepath.Rel(root, path)
        if err != nil {
            return nil
        }
        if ignore != nil && ignore.MatchesPath(rel) {
            if d.IsDir() {
                return fs.SkipDir // không vào subtree đã bị ignore
            }
            return nil
        }
        info, _ := d.Info() // info nil-safe — fallback size=0/modTime zero
        e := entry{name: rel, isDir: d.IsDir()}
        if info != nil {
            e.size = info.Size()
            e.modTime = info.ModTime()
        }
        out = append(out, e)
        if len(out) >= maxWalkEntries { // FR14, cap defensive
            return fs.SkipAll
        }
        return nil
    })
    sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
    return out, err
}

const maxWalkEntries = 100_000
```

**Async wrapper** (`model.go`):

```go
type searchWalkedMsg struct {
    gen     uint64
    results []entry
    err     error
}

func walkTreeCmd(root string, gen uint64) tea.Cmd {
    return func() tea.Msg {
        out, err := walkTree(root)
        return searchWalkedMsg{gen: gen, results: out, err: err}
    }
}
```

Pattern y hệt `previewRenderedMsg` (`model.go:30`) — gen-counter để bỏ
walk cũ về trễ (vd user Esc rồi `/` lại nhanh trong khi walk1 chưa xong).

### 5.4 Filtering — `filterSearch` (`fs.go`)

```go
// filterSearch returns the top `limit` entries that match query, ranked by
// fuzzy score (desc). Empty query → first `limit` entries (already alpha-
// sorted by walkTree). Caller picks limit = maxSearchResults.
func filterSearch(entries []entry, query string, limit int) []entry {
    if query == "" {
        if len(entries) > limit {
            return entries[:limit]
        }
        return entries
    }
    names := make([]string, len(entries))
    for i, e := range entries {
        names[i] = e.name
    }
    matches := fuzzy.Find(query, names) // sahilm.Find sorts by Score desc
    out := make([]entry, 0, min(len(matches), limit))
    for i, m := range matches {
        if i >= limit {
            break
        }
        out = append(out, entries[m.Index])
    }
    return out
}

const maxSearchResults = 500 // D11
```

`fuzzy.Find` đã case-insensitive (D5) — không cần `strings.ToLower` trước
gọi. Tie-break theo path-length: sahilm sort theo Score chính, score ties
có thể không deterministic → wrap thêm sort.SliceStable nếu cần
deterministic tests (xem T9).

### 5.5 Update wiring — `updateSearch` (`model.go`)

```go
func (m model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
    switch msg.String() {
    case "esc":
        m.exitSearchRestore()
        return m, nil
    case "enter":
        m.openSearchResult()
        return m, nil
    case "up", "ctrl+p":
        if m.cursor > 0 {
            m.cursor--
            m.refreshPreview()
        }
    case "down", "ctrl+n":
        if m.cursor < len(m.entries)-1 {
            m.cursor++
            m.refreshPreview()
        }
    case "backspace":
        if m.searchQuery == "" {
            m.exitSearchRestore() // D13: BS empty → exit
            return m, nil
        }
        r := []rune(m.searchQuery)
        m.searchQuery = string(r[:len(r)-1])
        m.applySearchFilter() // FR6
    default:
        if len(msg.Runes) > 0 {
            m.searchQuery += string(msg.Runes)
            m.applySearchFilter()
        }
    }
    return m, nil
}
```

`enterSearch`/`exitSearchRestore`/`openSearchResult`/`applySearchFilter`
là helper trên `*model` (state mutation tập trung). `applySearchFilter`:

```go
func (m *model) applySearchFilter() {
    m.entries = filterSearch(m.searchAll, m.searchQuery, maxSearchResults)
    m.cursor = 0
    m.listTop = 0
    m.refreshPreview() // selected result đổi → preview cập nhật
}
```

`Update` (`model.go:343`) thêm nhánh `modeSearch` trong switch theo mode
và thêm case `searchWalkedMsg`:

```go
case searchWalkedMsg:
    if msg.gen != m.searchGen {
        return m, nil // walk cũ về trễ, bỏ
    }
    m.searchIndexing = false
    if msg.err != nil {
        m.statusMsg = "⚠ walk error: " + msg.err.Error()
        // không return — vẫn áp dụng partial results (filepath.WalkDir trả
        // err sau khi đã thu thập được nhiều entries trước lỗi)
    }
    m.searchAll = msg.results
    m.searchAllAt = time.Now()
    m.applySearchFilter()
```

**`/` keypress** trong `updateNormal` (`model.go:511`):

```go
case "/":
    m.enterSearch()
```

`enterSearch()`:

```go
func (m *model) enterSearch() tea.Cmd {
    m.searchSavedCwd = m.cwd
    m.searchSavedEntries = m.entries
    m.searchSavedFsSig = m.fsSig
    m.searchSavedCursor = m.cursor
    m.searchSavedListTop = m.listTop
    m.mode = modeSearch
    m.searchQuery = ""
    m.statusMsg = ""

    // Cache hit? Nếu walk còn fresh trong TTL, dùng luôn — KHÔNG dispatch walk.
    if len(m.searchAll) > 0 && time.Since(m.searchAllAt) < walkCacheTTL {
        m.applySearchFilter()
        return nil
    }
    // Cache miss → walk async, set indexing flag, list rỗng đến khi walk về.
    m.searchIndexing = true
    m.searchGen++
    m.entries = nil
    m.cursor = 0
    m.listTop = 0
    m.refreshPreview() // empty entries → preview hiện "(empty directory)" tạm
    return walkTreeCmd(m.root, m.searchGen)
}

const walkCacheTTL = 30 * time.Second
```

Vì `enterSearch` trả `tea.Cmd`, `updateNormal` cần forward nó:

```go
case "/":
    return m, m.enterSearch()
```

`exitSearchRestore()`:

```go
func (m *model) exitSearchRestore() {
    m.entries = m.searchSavedEntries
    m.cwd = m.searchSavedCwd
    m.fsSig = m.searchSavedFsSig
    m.cursor = m.searchSavedCursor
    m.listTop = m.searchSavedListTop
    m.mode = modeNormal
    m.searchQuery = ""
    m.statusMsg = "search cancelled"
    m.refreshPreview()
}
```

`openSearchResult()`:

```go
func (m *model) openSearchResult() {
    if m.cursor >= len(m.entries) {
        m.exitSearchRestore()
        return
    }
    sel := m.entries[m.cursor]
    target := filepath.Join(m.root, sel.name)
    if !withinRoot(m.root, target) {
        m.statusMsg = "⚠ blocked: outside root"
        return
    }
    m.mode = modeNormal
    m.searchQuery = ""
    if sel.isDir {
        // FR9: cd vào chính folder
        m.cwd = target
        m.cursor = 0
        m.listTop = 0
        m.reload()
    } else {
        // FR8: cd vào parent, cursor land trên basename
        m.cwd = filepath.Dir(target)
        m.cursor = 0
        m.listTop = 0
        m.reload()
        base := filepath.Base(sel.name)
        for i, e := range m.entries {
            if e.name == base {
                m.cursor = i
                break
            }
        }
        m.refreshPreview()
    }
}
```

`reload()` đã prepend `..` khi `cwd != root` (`model.go:107`) — name lookup
ở trên match `base` bỏ qua `..` đúng vì `..` không trùng tên file thường.

### 5.6 `refreshPreview` branch on mode (`model.go:189`)

Một sửa đổi nhỏ: resolve `base` theo mode để preview của search result
nhìn được vào path tuyệt đối đúng.

```go
func (m *model) refreshPreview() {
    // …reset hygiene như cũ…
    if len(m.entries) == 0 {
        m.preview = []string{dimStyle.Render("(empty directory)")}
        return
    }
    sel := m.entries[m.cursor]
    base := m.cwd
    if m.mode == modeSearch {
        base = m.root // sel.name là relPath so với root
    }
    full := filepath.Join(base, sel.name)
    // …phần còn lại (sel.isDir, renderer registry…) GIỮ NGUYÊN…
}
```

Đây là **một dòng branch duy nhất**. Pipeline async preview (`syncPreview`/
`applyPreview`, `model.go:257`/`294`) không đụng đến — `srcPath` đã là
abs path, mode không ảnh hưởng.

**Lưu ý nhanh `..` handling:** synthetic `..` (`model.go:107`) chỉ tồn
tại trong list **bình thường** khi `cwd != root`. Trong `modeSearch`,
`m.entries` là result list — không có `..`. Nên `descend()`/`ascend()`
(không gọi trong search mode anyway) cũng không phải lo case này.

### 5.7 Status bar prompt + indexing chip (`view.go:163`)

`renderStatus` thêm nhánh `modeSearch` trước nhánh default:

```go
case modeSearch:
    // Prompt giống modeRename (view.go:170-173) nhưng prefix "/"
    p := promptStyle.Background(colAccent).Foreground(colSelFg).
        Render("/" + m.searchQuery + "▏")
    if m.searchIndexing {
        return p + " " + renderingStyle.Render("• indexing…")
    }
    return p
```

`renderingStyle` (`theme.go:35`) đã có sẵn từ markdown PRD — tái dùng nguyên
xi.

### 5.8 Poll loop guard mở rộng (`model.go:326`)

Hiện tại:

```go
if m.mode == modeNormal && !m.dragging {
    m.syncFromDisk()
}
```

`m.mode == modeNormal` đã đúng — `modeSearch` không phải normal nên `syncFromDisk`
**đã** bị skip mà không cần thay đổi (FR11). Đây là phần thưởng của việc
chọn mode enum thay vì flag boolean rời rạc.

### 5.9 Reset hygiene khi exit search

`refreshPreview` (`model.go:189`) đã có reset hygiene đầy đủ (`previewPreStyled`,
`srcPath`, `srcRaw`, `srcWidth`, `pendingWidth` set về zero ở đầu hàm). Khi
`exitSearchRestore`/`openSearchResult` gọi `refreshPreview()`, reset chạy
như bình thường — không leak `previewPreStyled = true` từ result code-file
sang plain file ở list cũ.

### 5.10 Error & edge cases

- **Repo không có `.gitignore`**: `CompileIgnoreFile` trả `err` → ignore =
  nil, walk tiếp với chỉ `.git/` skip. Hành vi sane (FR13).
- **`.gitignore` ở subfolder**: bỏ qua v1 — chỉ load root `.gitignore`.
  Defer v2 (ripgrep cũng làm vậy với `-g`; thiếu sót nhỏ).
- **File bị xóa giữa walk và Enter**: `reload()` ở `openSearchResult` đọc
  thực tế disk — file gone → cursor lookup không match → cursor về 0,
  preview hiện "(empty)" hoặc folder thường. Không crash.
- **Walk vẫn đang chạy khi user Enter**: list rỗng → `openSearchResult`
  gặp `m.cursor >= len(m.entries)` → fallback `exitSearchRestore`.
- **Walk vẫn đang chạy khi user `/` lại** (vd Esc → / rapid): `searchGen++`
  trong `enterSearch` invalidate walk cũ; gen check trong `searchWalkedMsg`
  bỏ kết quả về trễ.
- **Query có ký tự đặc biệt (`/`, `\`, `:`, regex meta)**: `fuzzy.Find` xử
  lý plain string, không phải regex — an toàn. `/` trong query gõ bình
  thường (không trigger lại `/` activation vì đang ở `modeSearch`, không
  phải `modeNormal`).
- **Project huge (>100k entries)**: FR14 cap. Status hint cho user biết
  scope quá lớn cho companion tool.

### 5.11 Đã cân nhắc & **defer khỏi v1** (ghi rõ để reviewer biết không phải bỏ sót)

- **Content search (grep-style)** — feature riêng, panel kết quả file:line,
  jump-to-line. Phá two-panel ceiling; là việc của ripgrep. Defer v2 (hoặc
  không bao giờ — agent thường làm sẵn).
- **mtime-based ranking** ("recently modified first" boost): defer; v1
  trust sahilm score. Có thể thêm `recencyBoost` ở v2 — entry có walk
  modTime sẵn (`fs.go:48`).
- **Highlight matched chars trong result text** (vd bold ký tự match):
  cần đổi `renderList` để diễn giải per-rune style — nontrivial. Defer.
- **`.gitignore` ở subfolder** (composable ignore): chỉ load root `.gitignore`
  v1. Đa số repo Go/JS có một `.gitignore` root đã đủ.
- **File watcher (fsnotify) thay TTL cache**: TTL 30s đủ cho vibe-code
  workflow (user thường search → Enter rồi quay lại folder, ít khi giữ
  cache lâu). Watcher overkill v1.
- **Search history persist across session**: không thấy use case mạnh —
  user không "tìm cùng query lần nữa" cách 1 tuần.
- **Multi-root search** (nhiều project trong cùng phiên): out-of-scope —
  jail design giả định 1 root.
- **`bubbles/list` filter mode** sẵn có (chỉ filter trong slice nhỏ):
  không khớp scope (recursive walk + ranking) và sẽ kéo `bubbles` làm dep.

## 6. Acceptance criteria

### Gherkin

```gherkin
Feature: Recursive fuzzy filename search

  When the user presses "/", the explorer enters search mode: the status bar
  becomes a fuzzy-input prompt, the list pane shows project-wide entries
  matching the query (respecting .gitignore, always skipping .git/), Enter on
  a result jumps the working directory to that result, and Esc restores the
  pre-search state.

  Background:
    Given the explorer is open in a project rooted at "proj"
    And the project has a typical mixed tree (docs/, src/, .git/, .gitignore)

  Scenario: Activate search and see all entries
    Given the explorer is in normal mode
    When I press "/"
    Then the status bar shows a search prompt
    And the list pane lists project-wide entries (no query → all walked)
    And ".git/" entries do not appear
    And entries listed in .gitignore (e.g. "node_modules/", "dist/") do not appear

  Scenario: Type to filter results
    Given the search prompt is open with an empty query
    When I type "model"
    Then the list pane shows entries whose paths fuzzy-match "model"
    And the top result is the closest match
    And the preview pane reflects the highlighted result

  Scenario: Enter on a file result jumps into its parent folder
    Given I have searched and "model.go" at the project root is selected
    When I press Enter
    Then the explorer leaves search mode
    And the current directory is the project root
    And the cursor is on "model.go"

  Scenario: Enter on a nested file result jumps into its nested parent
    Given I have searched and "docs/prd-markdown-view.md" is selected
    When I press Enter
    Then the explorer leaves search mode
    And the current directory is "docs"
    And the cursor is on "prd-markdown-view.md"

  Scenario: Enter on a folder result enters that folder
    Given I have searched and "docs/" is selected
    When I press Enter
    Then the explorer leaves search mode
    And the current directory is "docs"

  Scenario: Esc restores the pre-search state
    Given I was browsing "src/" with the cursor on "main.go"
    And I pressed "/" and typed "xyz"
    When I press Esc
    Then the explorer leaves search mode
    And the current directory is "src"
    And the cursor is on "main.go"

  Scenario: Backspace on an empty query exits search
    Given the search prompt is open and the query is empty
    When I press Backspace
    Then the explorer leaves search mode
    And the prior list state is restored

  Scenario: No matches shows an empty list with a hint
    Given the search prompt is open
    When I type a query that matches nothing
    Then the list pane shows zero entries
    And the status bar hints "0 results"

  Scenario: Preview keeps working for the highlighted result
    Given the search results include "README.md" as the top match
    When that result is highlighted
    Then the preview pane shows it rendered as markdown
    When I navigate down to a ".go" file result
    Then the preview pane shows that file with syntax highlighting

  Scenario: Walk runs async with an indexing chip
    Given the project is large enough for the walk to take more than one frame
    When I press "/" for the first time in the session
    Then an "indexing…" chip appears in the status bar
    And I can keep typing the query while indexing
    And once the walk completes the results update without further input

  Scenario: Symlinks and the project's .git/ are excluded from results
    Given the project root contains a symlink "shortcut" to "/etc"
    And the project root contains a ".git/" directory
    When I open search with no query
    Then "shortcut" does not appear in the list pane
    And no entry under ".git/" appears in the list pane
```

### Checklist verify

1. `/` ở `modeNormal` → status bar đổi sang prompt `/▏`; mặt nạ hint biến mất.
2. Lần đầu `/` trong session → chip `• indexing…` hiện, list ban đầu rỗng,
   xong walk → list điền tự động không cần gõ thêm.
3. Empty query: list hiện toàn bộ walked, alpha-sort theo `relPath`, cap 500.
4. Gõ "model" → top result chứa "model" theo subsequence (`model.go`,
   `model_test.go`, `docs/prd-markdown-view.md`…), score giảm dần.
5. `.git/` không xuất hiện trong list, kể cả khi `.gitignore` không nhắc
   tới nó.
6. Pattern trong `.gitignore` của project (vd `tmp/`, `lazyexplorer`,
   `.claude/`) → không xuất hiện trong list.
7. Symlink trong repo → không xuất hiện, walk không hang.
8. Enter trên file root-level (vd `main.go`): `cwd` về root, cursor trên
   `main.go`, `mode = modeNormal`.
9. Enter trên file nested (vd `docs/prd-markdown-view.md`): `cwd = docs`,
   cursor trên `prd-markdown-view.md`.
10. Enter trên folder result (vd `docs/`): `cwd = docs`, cursor top.
11. Esc bất kỳ điểm nào → `cwd` + `cursor` + `entries` + `listTop` trùng
    state trước khi nhấn `/`.
12. Backspace với query rỗng → exit (FR10), không treo.
13. Re-activate `/` trong vòng 30s → KHÔNG re-walk (cache TTL), chip không
    hiện.
14. Re-activate `/` sau >30s → re-walk, chip hiện lại.
15. Preview pane khi highlight result `.md` → render glamour như bình
    thường; result `.go` → highlight chroma; result thường → plain.
16. Walk gặp folder permission denied (test bằng dir `chmod 000`) →
    skip folder, walk hoàn thành, không crash.
17. Poll loop trong khi `modeSearch`: agent ghi/xóa file bên cạnh →
    list result KHÔNG churn; Esc xong, `syncFromDisk` chạy tiếp bình
    thường (file mới hiện ở list cwd).
18. `go build -o lazyexplorer . && go vet ./... && go test ./...` xanh.
19. `go test -race ./...` xanh (walk goroutine + Update goroutine không
    race trên `searchAll`/`searchGen`).

## 7. Task breakdown

- [ ] **T1 — Dependencies.** `go get github.com/sahilm/fuzzy@latest` +
  `go get github.com/sabhiram/go-gitignore@latest`; `go mod tidy`.
  Verify API contract bằng test ngắn (T9 đầu tiên). *(go.mod, go.sum)*

- [ ] **T2 — Walker.** `walkTree(root) ([]entry, error)` (`fs.go`) — skip
  `.git/` cứng, skip symlink, áp `.gitignore` root, cap `maxWalkEntries`,
  sort alpha. Permission-denied subdir → `fs.SkipDir`, không bubble error.
  *(fs.go)*

- [ ] **T3 — Filter.** `filterSearch(entries, query, limit) []entry`
  (`fs.go`) — empty query trả `entries[:limit]`, non-empty gọi
  `fuzzy.Find` + map index về entry. *(fs.go)*

- [ ] **T4 — Mode + state.** Thêm `modeSearch` vào enum (`model.go:38`);
  thêm các field `searchQuery`, `searchAll`, `searchAllAt`,
  `searchIndexing`, `searchGen`, `searchSaved{Cwd,Entries,FsSig,Cursor,
  ListTop}` (§5.2). *(model.go)*

- [ ] **T5 — Async walk msg.** `searchWalkedMsg` + `walkTreeCmd(root, gen)`
  (`model.go`); case trong `Update` áp dụng kết quả, drop walk cũ theo gen.
  *(model.go)*

- [ ] **T6 — `enterSearch` / `exitSearchRestore` / `openSearchResult` /
  `applySearchFilter`.** Helper trên `*model` (§5.5). *(model.go)*

- [ ] **T7 — `updateSearch`.** Handler keypress trong `modeSearch`
  (§5.5). Wire vào `Update` switch theo `m.mode` (`model.go:343`).
  *(model.go)*

- [ ] **T8 — `/` keybind activate.** `updateNormal` (`model.go:511`) thêm
  case `"/"` → `m.enterSearch()`, return cmd. *(model.go)*

- [ ] **T9 — `refreshPreview` mode-aware base.** Branch một dòng:
  `base := m.cwd; if m.mode == modeSearch { base = m.root }` (`model.go:189`).
  *(model.go)*

- [ ] **T10 — Status bar prompt + chip.** `renderStatus` (`view.go:163`)
  thêm case `modeSearch` với prompt `/query▏` và chip `• indexing…` khi
  `searchIndexing`. *(view.go)*

- [ ] **T11 — Tests.** *(*_test.go)*
  - `TestFuzzyContract`, `TestGitIgnoreContract` (T1 verify dep API).
  - `TestWalkTree`: skip `.git/`, skip symlink, áp `.gitignore`, cap
    entries, permission-denied subdir.
  - `TestFilterSearch`: empty query, non-empty, tie-break, limit cap.
  - `TestEnterExitSearch`: snapshot/restore exact (cwd, entries, cursor,
    listTop).
  - `TestOpenSearchResult`: file root, file nested, folder; jail block
    cho path bịa ngoài root.
  - `TestModeSearchPreviewBase`: preview của result `.md` ở nested
    folder hiện đúng nội dung (render glamour pipeline).
  - `TestSearchPollLoopSkipped`: tickMsg trong `modeSearch` không gọi
    `syncFromDisk` (mock cwd thay đổi giữa chừng).
  - `TestSearchGenInvalidatesStaleWalk`: rapid `/` → Esc → `/` không
    apply walk1 sau khi walk2 đã về.
  - `-race`: walk goroutine + Update goroutine không data-race trên
    `searchAll`.

- [ ] **T12 — Verify.** `go build -o lazyexplorer . && go vet ./... &&
  go test ./...` xanh; `go test -race ./...` xanh; kiểm tay acceptance
  §6 (1–17).

## 8. Files chạm tới

| File | Thay đổi |
|------|----------|
| `go.mod` / `go.sum` | + `github.com/sahilm/fuzzy`, + `github.com/sabhiram/go-gitignore` (direct) |
| `fs.go` | `+ walkTree` (recursive walk + ignore + cap), `+ filterSearch` (sahilm wrapper + limit), `+ maxWalkEntries`, `+ maxSearchResults` |
| `model.go` | enum `+ modeSearch`; fields `searchQuery`/`searchAll`/`searchAllAt`/`searchIndexing`/`searchGen` + 5 snapshot fields; type `searchWalkedMsg`; func `walkTreeCmd`, `enterSearch`, `exitSearchRestore`, `openSearchResult`, `applySearchFilter`, `updateSearch`; `Update` dispatch + case `searchWalkedMsg`; `updateNormal` case `"/"`; `refreshPreview` base branch on mode |
| `view.go` | `renderStatus` case `modeSearch` (prompt `/query▏` + chip `indexing…`) |
| `theme.go` | (optional) `+ searchPromptStyle` nếu muốn accent khác `promptStyle` của rename; v1 tái dùng `promptStyle` + `colAccent` (`theme.go:13`) |
| `*_test.go` | walker, filter, mode transitions, snapshot/restore, jail guard, async gen, race, preview base — xem T11 |
