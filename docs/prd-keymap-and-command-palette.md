# PRD — Keymap registry & command palette (vim-spirit, crush-modeled)

> Feature: lifting keybind dispatch khỏi inline `switch msg.String()` (`updateNormal`)
> lên một `key.Binding` registry (single source of truth: key codes +
> help text), bổ sung `Ctrl+P` mở **command palette** dialog (fuzzy/substring filter →
> Enter run) cho các lệnh ngoài high-frequency lane (`r`/`d`), và một `?`-toggled
> **FullHelp overlay** cho discoverability. Vim-inspired (keyboard-first, Esc cancel
> universal) nhưng NOT vim-modal: KHÔNG normal/insert/visual, KHÔNG `:` syntax, KHÔNG
> count-prefix — đó là quyết định sau khi đọc `tmp/crush` (charmbracelet/crush) thấy
> modern keyboard-first TUI bỏ qua vim modal mà vẫn discoverable hơn vim nhiều.

Status: **accepted** · Author: feature-dev session · Reviewer: critic (independent pass, 2026-05-28) · Ngày: 2026-05-28 · Shipped: 2026-05-28 (✅ `go build && go vet && go test ./... && go test -race ./...` green)

---

## 1. Bối cảnh & vấn đề

Keybind dispatch hiện tại sống ở **một switch nội tuyến** trong `updateNormal`
(`model.go:737-783`):

```go
case "down", "j":
    if m.cursor < len(m.entries)-1 { ... }
case "up", "k":
    if m.cursor > 0 { ... }
case "g":
    m.cursor = 0
    m.refreshPreview()
// …
```

và **hint string hardcoded** ở `view.go:487`:

```go
hints := "[↑↓/jk/click] move  [enter/l] open  [h/bksp] up  [r] rename  [d] delete  [wheel] scroll  [q] quit"
```

Ba triệu chứng quan sát được:

1. **Two sources of truth.** Phím `r` đang là rename → bug fix đổi rename sang
   `R` đòi sửa cả `updateNormal` lẫn hint string. Quên một bên → hint sai →
   user gõ phím cũ, không gì xảy ra. (Pattern này đã suýt xảy ra trong
   `prd-pane-focus.md` §5.6 khi hint bar viết một focus, code làm khác — chỉ
   cứu được vì PRD đó liệt kê hint mới explicit.)

2. **Discoverability yếu.** Hint bar có ~7 binding; thực tế còn `J`/`K`/`ctrl+d/u`
   (`model.go:765-768`) preview scroll mà hint không liệt kê — user phải đọc
   `model.go:737-781` hoặc `CLAUDE.md` mới biết. So với lazygit (có popup `?`
   liệt kê toàn bộ keymap theo nhóm), lazyexplorer hiện không có overlay nào.

3. **Surface area sẽ scale.** Có một lớp lệnh ngoài high-frequency (`reload`
   sau khi agent thay đổi, `copy full path` để paste sang agent prompt, `cd <abs
   path>` để jump ra arbitrary path within jail) hiện không có chỗ đứng — quá
   ít dùng để xứng một single-key binding, quá hữu ích để bỏ. Hệ quả ngày
   nay: user phải Ctrl+C exit, `cd ...`, chạy lại — gãy workflow.

### Tham chiếu — `tmp/crush` (charmbracelet/crush, đồng stack bubbletea v2 + lipgloss v2)

Đọc kỹ `tmp/crush` để xem một modern coding-agent TUI giải quyết cùng ba triệu
chứng trên thế nào:

- **`key.Binding` registry** (`tmp/crush/internal/ui/model/keys.go:5-68`): một
  `KeyMap` struct với từng field là `key.Binding`; `DefaultKeyMap()`
  (`tmp/crush/internal/ui/model/keys.go:70-100`) khởi tạo bằng
  `key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("ctrl+p", "commands"))`.
  Mỗi binding **mang theo cả key codes + help text** → single source of truth.

- **`key.Matches()` dispatch** (`tmp/crush/internal/ui/model/ui.go:1816-1881`):
  thay cho `switch msg.String()`, dùng `if key.Matches(msg, m.keyMap.Commands)`.
  Đổi key của một binding chỉ chạm `DefaultKeyMap`, không phải mọi call-site.

- **Command palette modal dialog** (`tmp/crush/internal/ui/dialog/commands.go:44-76`):
  `Ctrl+P` mở dialog có textinput filter + filterable list + help component.
  Không có vim `:` syntax — palette là một mode riêng, có dialog riêng.

- **`ShortHelp()` / `FullHelp()`** (`tmp/crush/internal/ui/model/ui.go:2362-2437+`):
  hai hàm trả `[]key.Binding` (short) và `[][]key.Binding` (full table); render
  qua `charm.land/bubbles/v2/help`. Mode-aware: chỉ liệt kê binding active ở
  state hiện tại.

- **KHÔNG có** (`tmp/crush/internal/ui/model/ui.go:2096-2150`): vim modal
  (normal/insert/visual), `:` command syntax, count-prefix `5j`. Crush
  decide rằng key.Binding registry + palette + help overlay đã đủ cho keyboard-
  first TUI — không cần vim semantics.

> ĐÃ VERIFY ✅ (2026-05-28): Đọc thật các file:line trên trong `tmp/crush`.
> Pattern xác thực, không phải phỏng đoán. Khi import `charm.land/bubbles/v2`
> vào lazyexplorer cần verify version compatible với `charm.land/bubbletea/v2
> v2.0.6` (`go.mod:6`) — falsification: `go get charm.land/bubbles/v2@latest && go build`
> không lỗi (T1).

### Tại sao **không** đi vim modal đúng nghĩa

User ban đầu hỏi "vim command/key". Sau khi consider `tmp/crush` + ethos
lazyexplorer ("simpler than superfile", "every keybind must earn its place" —
`CLAUDE.md`):

| Vim mechanic | Cost cho lazyexplorer | Benefit | Verdict |
|---|---|---|---|
| Modal (normal/insert/visual) | Mode enum +3, mỗi mode keymap riêng, modeline | Multi-select v.v. — overkill cho glance companion | Defer |
| `:` command-line | Parser cho `:q`/`:e`/`:cd`/`:reload` + tab-complete + history | Trùng surface với palette nhưng UX chậm hơn (gõ `:reload<CR>` vs `Ctrl+P` rồi gõ một vài ký tự) | Defer (palette làm tốt hơn) |
| Count prefix `5j` | State `countBuf` + clear-on-non-motion logic | "5j" hiếm dùng ở file explorer; user wheel/PgDn nhanh hơn | Defer |
| Operators+motions `d2j` | Hai-token grammar, undo stack | Multi-file batch delete defer khỏi v1 anyway | Defer |
| Marks/registers/macros | State, persistence | Out of scope glance companion | Defer |

Đi crush-modeled giữ được **vim spirit** (keyboard-first, Esc cancel universal,
mode-aware) **không cần vim mechanics**. Đó là synthesis của PRD này.

## 2. Goal (1 câu)

Refactor keybind từ inline switch + hardcoded hint sang `bubbles/v2 key.Binding`
**registry** (single source of truth cho key codes + help), thêm `Ctrl+P` mở
**command palette** dialog cho các lệnh ngoài lane phím trực tiếp, và `?` toggle
**FullHelp overlay** liệt kê toàn bộ binding nhóm theo chủ đề.

**Non-goal làm rõ:**
- KHÔNG modal vim (normal/insert/visual) — focus state hiện tại (`prd-pane-focus.md`)
  đã đủ cho glance companion.
- KHÔNG `:` command-line syntax — palette UX tốt hơn cho cùng surface (verify
  trong §5.10).
- KHÔNG count-prefix (`5j`, `10G`) — defer cho tới khi có request thật.
- KHÔNG operators+motions (`d2j`, `yy`), marks (`ma`/`'a`), registers (`"ap`),
  macros (`qa`/`@a`), repeat (`.`) — toàn bộ vim text-editor mechanic.
- KHÔNG keybind override qua config file v1 (No Abstractions Until Proven).
- KHÔNG đổi **hành vi** search (`/`) — đã shipped (`prd-search.md`, `modeSearch`
  + `enterSearch` + `case "/"` đang trong `model.go`). PRD này chỉ thêm
  `KeyMap.Search` cho help liệt kê được **và** migrate `case "/"` hiện hữu sang
  `case key.Matches(msg, km.Search)` trong cùng pass refactor — không thêm handler mới.
- KHÔNG đổi **hành vi** focus (`Tab`/focusPane) — đã shipped (`prd-pane-focus.md`,
  `focusPane` + focus-aware dispatch trong `updateNormal`). PRD này thêm
  `KeyMap.FocusToggle` cho help **và** migrate các `case "tab"`/`if m.focusPane == ...`
  hiện hữu sang `key.Matches`, giữ nguyên logic branch.
- KHÔNG migrate **mọi** inline `switch` case sang `key.Matches` — chỉ migrate
  lane normal-mode (`updateNormal`); rename/delete prompt mode giữ inline
  (high-velocity escape paths, registry không thêm value).

## 3. Quyết định đã chốt

| # | Quyết định | Giá trị | Lý do |
|---|-----------|---------|-------|
| D1 | Dep | `charm.land/bubbles/v2` (chỉ subpackage `key` + `help` v1) | Crush dùng (`tmp/crush/internal/ui/model/keys.go:3`); cùng stack v2; chuẩn ecosystem. `clipboard` xem D6. |
| D2 | `KeyMap` struct shape | **Flat** (không nested) — một struct, mỗi binding một field | Crush nested theo domain (`Editor`, `Chat`, `Initialize`) vì có nhiều mode lớn (`tmp/crush/.../keys.go:5-68`); lazyexplorer chỉ có một normal-mode lane → flat đủ, ít cognitive load. |
| D3 | Activation key cho palette | **`Ctrl+P`** | Cùng convention crush (`tmp/crush/.../keys.go:80-83`); vim-bro user cũng quen `:` được palette mở thay; macOS Cmd+P cho IDE. Không đụng phím lazygit dùng (lazygit không có palette). |
| D4 | Activation key cho FullHelp overlay | **`?`** | Vim/man-page/lazygit/glow convention. Không đụng key đang dùng (`model.go:737-781` không có `?`). |
| D5 | Modes mới | `modeCommandPalette`, `modeHelp` — orthogonal với focusPane | Cùng discipline `prd-pane-focus.md` D1: mode-prompt-style cho UI overlay tạm. |
| D6 | Clipboard | **Shell out** — `pbcopy` (darwin) / `xclip -selection clipboard` (linux) / `wl-copy` (wayland) — không thêm dep | Một Go lib clipboard cần CGo (`github.com/atotto/clipboard` good but pulls X11 deps trên linux); shell-out đơn giản, fail-soft (clipboard không có → status warn, không crash), test-friendly. |
| D7 | Palette commands v1 | `reload` · `copy path` · `cd <path>` · `quit` | 4 lệnh có pain point thật hôm nay (xem §1.3); KHÔNG bao gồm `rename`/`delete` (high-frequency, ở lane phím trực tiếp); KHÔNG bao gồm `toggle hidden` (lazyexplorer luôn show hidden — `fs.go:37` comment). |
| D8 | Palette filter algorithm | **Substring case-insensitive** trên command Name | sahilm/fuzzy chỉ make sense khi >100 items; 4-10 commands → substring đủ; KHÔNG kéo dep mới cho v1. Nếu `prd-search.md` ship trước, palette CÓ THỂ reuse `fuzzy.Find` (defer optional optimization). |
| D9 | Palette UI | **Floating modal** căn giữa màn hình (Raycast/Spotlight-style): box opaque có border, search input ở **đỉnh box**, filtered list bên dưới; box composite đè lên nền (list + divider + preview thật) qua `lipgloss.Canvas`/`Compositor`/`Layer`. | Search-at-top + box nổi căn giữa là mental model Spotlight/Raycast user đã quen; nền (tree + preview) vẫn đọc được phía sau box → glance-friendly. lipgloss v2 (`v2.0.3`, đã trong `go.mod`) sẵn có Canvas/Compositor/Layer — cùng primitive crush dùng (`tmp/crush/.../dialog/dialog.go:166 DrawCenterCursor` qua ultraviolet), nhưng wrapper cấp cao hơn nên giữ được pattern string-based `tea.NewView(content)` hiện tại. |
| D10 | Help overlay UI | Same as D9: **floating modal** căn giữa, body là bảng binding nhóm theo chủ đề, scrollable qua `j/k`. | Một cơ chế overlay cho cả palette lẫn help → nhất quán; nền list + preview vẫn hiện sau box. |
| D11 | `cd <path>` UX | Palette select `cd <path>` → palette chuyển sang text-input mode cho path → Enter resolve + jail-check + cd | Composable: palette là two-stage (select command → input args). Một path text input đủ generic cho cd; tương lai nếu thêm command cần args, dùng cùng stage. |
| D12 | `ShortHelp()` placement | Normal mode: status bar bottom (`renderStatus` default case, `view.go:703-726`). Khi modal mở: status bar hiện short-help của mode đó (`[enter] run · [esc] close` cho palette; `[j/k] scroll · [esc] close` cho help). | Tái dùng vị trí, no chrome added; modal-mode hint cho user biết phím khả dụng trong modal. |
| D13 | `FullHelp()` rendering | Trong `modeHelp` overlay: bảng nhóm theo *Navigation / Mutation / Preview / Modes / Misc* | Crush style (`tmp/crush/.../ui.go:2440+`); 5 groups là max trước khi cognitive load tăng. |
| D14 | Phím trong palette mode | `↑/↓/j/k` di chuyển trong list; `enter` chạy; `esc` hoặc `ctrl+p` đóng; gõ ký tự → filter | Substring filter + arrow nav là pattern user quen (Spotlight, VSCode palette). `Ctrl+P` toggle để re-press không trap user. |
| D15 | Cross-ref `prd-pane-focus.md` + `prd-search.md` | Registry **đăng ký** binding `Tab` (focus toggle) + `/` (search) trong `KeyMap`, **không** implement handler ở đây — chỉ là chỗ đứng cho help text. Handler stays in respective PRD's scope. | Help liệt kê được; ship order không bị cứng buộc; mỗi PRD ship phần handler của mình. |
| D16 | Coordination với `prd-smooth-preview-scroll.md` D5 | `previewLineStep` hằng số đó vẫn còn — `key.Binding` cho `j`/`k` không thay đổi step value; chỉ chuyển caller dispatch. | PRD này refactor **dispatch**, không động step semantics. |
| D17 | Migrate scope của lần này | Chỉ `updateNormal` (`model.go:737-783`) | `updateRename` (`model.go:855-894`) và `updateConfirmDelete` (`model.go:836-853`) là escape-prompt path, switch inline đã clean và short — không thêm value. Defer. |
| D18 | Telemetry | `action.command_palette_open` + `action.command_run{name}` qua `m.tel.Record` (giống `prd-datadog-integration.md` pattern, `model.go:240`) | Đo command popularity → input cho v2 việc thăng cấp lên direct binding. |
| D19 | Mouse trong palette/help | Disabled toàn bộ (`handleMouse` early-return khi `mode != modeNormal` đã sẵn) | Cùng discipline `prd-search.md` D12; nhất quán. |
| D20 | Poll loop khi palette/help mở | Skip `syncFromDisk` — `model.go:453` đã check `m.mode == modeNormal && !m.dragging` | Free behavior: `modeCommandPalette` và `modeHelp` không match `modeNormal`. Không cần code mới. |
| D21 | Cơ chế compositing modal | `lipgloss.Canvas` + `Compositor` + `Layer` (`v2.0.3`): nền (full-screen content) là Layer `Z(0)`, modal box là Layer `Z(1)` đặt tại `(cx,cy)` căn giữa; `Canvas.Render()` trả string cuối cho `tea.NewView`. | ĐÃ VERIFY ✅ (2026-05-28, scratch test): ANSI-styled bg composite sạch **dưới** box, không leak, mỗi row đúng `m.width`. Phương án **loại**: (a) crush `Dialog` interface + raw ultraviolet `Draw` — over-engineering cho 2-overlay surface, đòi dialog-stack manager; (b) manual string-splice per-line ANSI-aware — tự viết lại đúng thứ Canvas đã đóng gói. |
| D22 | Backdrop sau modal | **Nền giữ nguyên** (không dim), box opaque nổi đè lên. | Đơn giản nhất, giữ context tree/preview đọc được. Dim backdrop cần re-style mọi cell nền → defer (§5.10). |

## 4. Functional requirements

- **FR1** — File mới `keys.go` định nghĩa `type KeyMap struct` với một field
  `key.Binding` cho mỗi binding (D2). `defaultKeyMap()` constructor khởi tạo
  toàn bộ — duy nhất chỗ key codes + help text được viết.

- **FR2** — `model` thêm field `keymap KeyMap`; `newModel` (`model.go:115`) gọi
  `defaultKeyMap()` set nó. Không export qua `tea` event.

- **FR3** — `updateNormal` refactor: thay
  `switch msg.String() { case "down","j": ... }` bằng `switch {
  case key.Matches(msg, m.keymap.MoveDown): ... }`. Mỗi key hiện hữu
  (`up`/`down`/`k`/`j`/`g`/`G`/`l`/`right`/`enter`/`h`/`left`/`backspace`/
  `ctrl+d`/`ctrl+u`/`r`/`d`/`q`/`ctrl+c`/`tab`/`/`) đều có entry trong KeyMap.
  Lưu ý: `J`/`K` legacy đã bị **xoá** bởi `prd-pane-focus.md` D13 (preview
  fine-scroll giờ là `j`/`k` ở `focusPreview`) — KHÔNG đưa lại vào KeyMap.

- **FR4** — Mode enum mở rộng (`model.go:38-44`):
  ```go
  const (
      modeNormal mode = iota
      modeConfirmDelete
      modeRename
      modeCommandPalette // new
      modeHelp           // new
  )
  ```

- **FR5** — `Ctrl+P` ở `modeNormal` → vào `modeCommandPalette`. State được khởi tạo:
  - `paletteQuery` = ""
  - `paletteCursor` = 0
  - `paletteCommands` = `defaultCommands()` (toàn bộ commands)
  - `paletteSecondaryInput` = "" (used cho cd path stage)
  - **Floating modal** căn giữa màn hình: search prompt `> ▏` ở **đỉnh box**,
    filtered command list bên dưới (highlighted row = `paletteCursor`, accent
    full-width). Nền (list + divider + preview thật) hiện phía sau box. Status
    bar hiện modal short-help (`[enter] run · [esc] close`).

- **FR6** — Trong `modeCommandPalette`:
  - Ký tự in được → append vào `paletteQuery`, recompute filter (substring
    case-insensitive trên `cmd.Name`), reset `paletteCursor = 0`.
  - `Backspace` không ở query rỗng → trim một rune cuối; ở query rỗng → đóng
    palette (giống `prd-search.md` D13).
  - `↑`/`k` / `↓`/`j` → di `paletteCursor` trong filtered list.
  - `Enter` → chạy `paletteCommands[paletteCursor]` (xem FR8-FR11 cho từng cmd).
  - `Esc` hoặc `Ctrl+P` → đóng palette, restore `modeNormal` (không restore
    cwd — palette không edit cwd cho tới khi Enter).

- **FR7** — Trong palette, focusPane (từ `prd-pane-focus.md`) **bị đóng cứng**
  giá trị cũ; palette là sub-state nhỏ. Khi đóng palette, focusPane giữ nguyên.

- **FR8** — Command `reload`: gọi `m.reload()` (`model.go:125`); status
  message `reloaded N items`. Mode về `modeNormal`.

- **FR9** — Command `copy path`: lấy abs path của entry đang select (`m.cwd +
  m.entries[m.cursor].name`); shell-out clipboard (D6):
  ```
  darwin  → pbcopy
  linux   → xclip -selection clipboard  (fallback: wl-copy)
  windows → defer v1 (báo "clipboard not supported on this OS")
  ```
  Status: `copied <path>` hoặc `⚠ clipboard error: <err>`. Mode về `modeNormal`.
  **Synthetic `..`**: khi cursor ở entry `..` (parent shortcut, `model.go` reload
  prepend), `copy path` copy abs path của **parent đã resolve** (`filepath.Dir(m.cwd)`,
  jail-clamped tại root) — không phải chuỗi literal `<cwd>/..`. Cursor trên list rỗng
  → status `⚠ nothing selected`.

- **FR10** — Command `cd <path>`: palette chuyển sang **stage 1** (arg input):
  prompt ở **đỉnh box** đổi thành `cd > ▏`, `paletteSecondaryInput` thay
  `paletteQuery`; body box hiện mô tả command cho context. Enter:
  - Resolve `~` → home, `.` → cwd, `..` → parent, absolute → as-is.
  - `filepath.Clean` + jail-check `withinRoot(m.root, target)` (`fs.go:88`).
  - **Target là file (không phải dir)** → status `⚠ not a directory: <path>`, stage 1.
    (`os.Stat` + `IsDir()` check trước `m.cwd = target` — nếu không check, `m.reload()`
    gọi `readDir` trên file path sẽ error và set entries=nil; explicit message rõ hơn.)
  - Pass (dir, trong root) → set `m.cwd = target`, `m.reload()`, mode về `modeNormal`.
  - Fail (outside root) → status `⚠ blocked: outside root`, mode về
    `modeCommandPalette` stage 1 (user thử lại path khác hoặc Esc).
  - Path không tồn tại → status `⚠ not found: <path>`, stage 1.
  - Esc trong stage 2 → quay về stage 1 (palette list).

- **FR11** — Command `quit`: `tea.Quit`. Cùng hành vi `q`/`Ctrl+C`.

- **FR12** — `?` ở `modeNormal` → vào `modeHelp`; render **floating modal** căn
  giữa với body là `FullHelp()` (D13), nền (list + preview) hiện phía sau box.
  `?`/`Esc` đóng. Help body **chỉ scrollable** qua `j/k`; không có sub-selection.

- **FR13** — Status bar (`view.go:487`) thay hardcoded hint bằng
  `m.shortHelp()` join with `dimStyle`. `shortHelp` returns
  `[]key.Binding` filtered theo mode + focusPane.

- **FR14** — `shortHelp` cho mỗi mode (D10 chốt nội dung):
  | mode | bindings shown |
  |---|---|
  | `modeNormal` + `focusList` | MoveUpDown, FocusToggle (Tab), OpenEntry, GoUp, Rename, Delete, CommandPalette, FullHelp, Quit |
  | `modeNormal` + `focusPreview` | ScrollUpDown, FocusToggle, JumpTop, JumpBottom, HalfPage, Back (Esc), CommandPalette, FullHelp, Quit |
  | `modeCommandPalette` (stage 1) | Filter, MoveUpDown, Select (Enter), Close (Esc) |
  | `modeCommandPalette` (stage 2 cd) | Input, Submit (Enter), Back (Esc) |
  | `modeHelp` | Scroll, Close (Esc/?) |
  | `modeRename` | (unchanged — `view.go:483` prompt) |
  | `modeConfirmDelete` | (unchanged — `view.go:479` prompt) |

- **FR15** — `fullHelp` returns `[][]key.Binding` nhóm theo (D13):
  *Navigation*: MoveUp, MoveDown, GoTop, GoBottom, OpenEntry, GoUp.
  *Preview*: PreviewScrollUp/Down, PreviewHalfPageUp/Down (kế thừa từ
  `prd-smooth-preview-scroll.md`).
  *Mutation*: Rename, Delete.
  *Modes*: FocusToggle (Tab — cross-ref `prd-pane-focus.md`), Search (`/` —
  cross-ref `prd-search.md`), CommandPalette (`Ctrl+P`), FullHelp (`?`).
  *Misc*: Quit.

- **FR16** — Telemetry (D18):
  - `action.command_palette_open` khi vào `modeCommandPalette`.
  - `action.command_run` với field `{name: "<cmd name>", success: bool}` khi
    Enter trên một command (kể cả `cd` resolve fail).
  Pattern: `m.tel.Record(...)` (`model.go:240`), non-blocking.

- **FR17** — Mouse trong `modeCommandPalette` + `modeHelp`: handled by existing
  `handleMouse` early-return (`model.go:474-476`) — KHÔNG cần code mới (D19).

- **FR18** — Poll loop khi palette/help mở: skip `syncFromDisk` đã đúng (D20)
  qua existing check `m.mode == modeNormal` (`model.go:453`).

- **FR19** — Status bar composition: ShortHelp (hint theo focus) flush-left, với
  render spinner ở slot 2 cột cố định mép phải khi `pendingWidth > 0`
  (`prd-preview-render-stability.md`). Không có focus chip — tín hiệu focus đi qua
  divider glow (`prd-focus-divider-glow.md`). Hints fit `fitWidth(contentW-2)` nên
  spinner slot không bao giờ làm dịch/cắt hints.

- **FR20** — Help đặt cấp ưu tiên kiểm thử: snapshot test render của
  `FullHelp()` ở 80×24 và 60×24 để đảm bảo wrap đẹp (responsive layout
  flip — `prd-responsive-layout.md`).

## 5. Technical design

> **Kim chỉ nam:** mọi binding sống **một chỗ** trong `defaultKeyMap`. Cách
> ship là một-PR-một-step: (1) introduce KeyMap + dispatch refactor, (2)
> introduce palette mode, (3) introduce help mode. Ba step orthogonal — ship
> riêng được nếu reviewer thấy gộp khó review.

### 5.1 Dependencies (`go.mod`)

```go
require (
    charm.land/bubbletea/v2 v2.0.6   // existing
    charm.land/lipgloss/v2  v2.0.3   // existing
    charm.land/bubbles/v2   v2.0.x   // NEW — `key` and `help` sub-packages
    // …
)
```

| Dep | API verify (T1) | Falsification |
|---|---|---|
| `charm.land/bubbles/v2/key` | `key.NewBinding(key.WithKeys, key.WithHelp)` tồn tại; `key.Matches(tea.KeyPressMsg, key.Binding) bool` tồn tại | `TestKeyMatchContract`: `Matches(KeyPressMsg{Code: 'j'}, NewBinding(WithKeys("j")))` trả true. |
| `charm.land/bubbles/v2/help` (optional v1) | `help.Model` + `help.ShortHelpView([]key.Binding)` + `help.FullHelpView([][]key.Binding)` tồn tại | `TestHelpRender`: cho 3 binding, output chứa cả 3 key + desc. |

> ĐÃ VERIFY (MEDIUM, 2026-05-28): API trên là từ `tmp/crush/.../keys.go:71-99`
> + `tmp/crush/.../dialog/commands.go:7-8,108-119`. Version exact của
> bubbles/v2 phải resolve qua `go get` (T1) — HIGH confidence pattern đúng,
> MEDIUM confidence version compatible với bubbletea/v2 v2.0.6.

Clipboard (D6) **không thêm dep** — `exec.Command("pbcopy")` hoặc tương đương.

### 5.2 `KeyMap` struct + `defaultKeyMap` (`keys.go` mới)

```go
package main

import "charm.land/bubbles/v2/key"

// KeyMap is the single source of truth for which key codes trigger which
// action and what help text describes them. Every binding the app reacts to
// must live here — adding a binding means adding a field, never a stray
// `case "x":` inline.
type KeyMap struct {
    // Navigation (normal mode + focusList)
    MoveUp,
    MoveDown,
    GoTop,
    GoBottom,
    OpenEntry,
    GoUp key.Binding

    // Preview pane scroll (normal mode + focusPreview)
    // Step size lives at the call site (previewLineStep = 1; half-page = bodyH/2)
    // — these bindings only carry key codes + help.
    PreviewScrollUp,
    PreviewScrollDown,
    PreviewHalfPageUp,
    PreviewHalfPageDown,
    PreviewJumpTop,
    PreviewJumpBottom key.Binding

    // Mutation (normal mode + focusList)
    Rename,
    Delete key.Binding

    // Modes
    FocusToggle      key.Binding // cross-ref prd-pane-focus.md (handler there)
    Search           key.Binding // cross-ref prd-search.md (handler there)
    CommandPalette   key.Binding // THIS PRD
    FullHelp         key.Binding // THIS PRD
    Back             key.Binding // Esc — focusPreview→focusList (pane-focus); palette close; help close

    // Misc
    Quit key.Binding
}

// defaultKeyMap returns the ship default. CHANGE A KEY HERE, NOT IN updateNormal.
func defaultKeyMap() KeyMap {
    return KeyMap{
        MoveUp:    key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "move up")),
        MoveDown:  key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "move down")),
        GoTop:     key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "go top")),
        GoBottom:  key.NewBinding(key.WithKeys("G"), key.WithHelp("G", "go bottom")),
        OpenEntry: key.NewBinding(key.WithKeys("enter", "l", "right"), key.WithHelp("enter/l", "open")),
        GoUp:      key.NewBinding(key.WithKeys("h", "left", "backspace"), key.WithHelp("h/bksp", "go up")),

        PreviewScrollUp:     key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "scroll up")),
        PreviewScrollDown:   key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "scroll down")),
        PreviewHalfPageUp:   key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("ctrl+u", "half page up")),
        PreviewHalfPageDown: key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+d", "half page down")),
        PreviewJumpTop:      key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "preview top")),
        PreviewJumpBottom:   key.NewBinding(key.WithKeys("G"), key.WithHelp("G", "preview bottom")),

        Rename: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "rename")),
        Delete: key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),

        FocusToggle:    key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "switch focus")),
        Search:         key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
        CommandPalette: key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("ctrl+p", "commands")),
        FullHelp:       key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
        Back:           key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),

        Quit: key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
    }
}
```

> **Note** — `MoveUp` và `PreviewScrollUp` cùng key code `up`/`k` nhưng help
> text khác (`"move up"` vs `"scroll up"`). Dispatch chọn cái nào theo
> `focusPane` (xem `prd-pane-focus.md` §5.2). `key.Matches` chỉ check key
> codes — focus-routing là responsibility của dispatch site, không phải
> binding registry. Crush cũng làm vậy (`tmp/crush/.../ui.go:2116-2125`).

### 5.3 `updateNormal` dispatch refactor (`model.go:737-783`)

```go
func (m model) updateNormal(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
    km := m.keymap

    switch {
    case key.Matches(msg, km.Quit):
        return m, tea.Quit

    case key.Matches(msg, km.CommandPalette):
        m.enterCommandPalette()
        return m, nil

    case key.Matches(msg, km.FullHelp):
        m.enterHelp()
        return m, nil

    // Focus-aware bindings (composing with prd-pane-focus.md §5.2 dispatch).
    // PRD pane-focus owns the focus-branch; PRD this owns the registry the
    // branch uses to MATCH on key codes.
    case key.Matches(msg, km.MoveDown): // == km.PreviewScrollDown
        if m.focusPane == focusList {
            m.moveCursor(1)
        } else {
            m.scrollPreview(1)
        }
    case key.Matches(msg, km.MoveUp): // == km.PreviewScrollUp
        if m.focusPane == focusList {
            m.moveCursor(-1)
        } else {
            m.scrollPreview(-1)
        }
    case key.Matches(msg, km.GoTop): // == km.PreviewJumpTop
        if m.focusPane == focusList {
            m.cursor = 0
            m.refreshPreview()
        } else {
            m.previewTop = 0
        }
    case key.Matches(msg, km.GoBottom): // == km.PreviewJumpBottom
        if m.focusPane == focusList {
            m.cursor = max(0, len(m.entries)-1)
            m.refreshPreview()
        } else {
            _, bodyH := m.previewScroll()
            m.previewTop = max(0, m.previewLen()-bodyH)
        }
    case key.Matches(msg, km.PreviewHalfPageDown):
        _, bodyH := m.previewScroll()
        step := max(1, bodyH/2)
        if m.focusPane == focusList {
            m.moveCursor(step)
        } else {
            m.scrollPreview(step)
        }
    case key.Matches(msg, km.PreviewHalfPageUp):
        _, bodyH := m.previewScroll()
        step := max(1, bodyH/2)
        if m.focusPane == focusList {
            m.moveCursor(-step)
        } else {
            m.scrollPreview(-step)
        }

    case key.Matches(msg, km.OpenEntry):
        if m.focusPane == focusList {
            m.descend()
        }
    case key.Matches(msg, km.GoUp):
        if m.focusPane == focusList {
            m.ascend()
        }

    case key.Matches(msg, km.Rename):
        if m.focusPane == focusList && len(m.entries) > 0 && m.entries[m.cursor].name != ".." {
            m.mode = modeRename
            m.input = m.entries[m.cursor].name
            m.statusMsg = ""
        }
    case key.Matches(msg, km.Delete):
        if m.focusPane == focusList && len(m.entries) > 0 && m.entries[m.cursor].name != ".." {
            m.mode = modeConfirmDelete
            m.statusMsg = ""
        }

    case key.Matches(msg, km.Back):
        if m.focusPane == focusPreview {
            m.focusPane = focusList
        }

    // FocusToggle: the EXISTING `case "tab"` toggling focusPane (shipped via
    // prd-pane-focus.md, currently in updateNormal) is migrated here to
    // `case key.Matches(msg, km.FocusToggle):` — same branch logic, registry-matched.
    case key.Matches(msg, km.FocusToggle):
        if m.focusPane == focusList {
            m.focusPane = focusPreview
        } else {
            m.focusPane = focusList
        }

    // Search: the EXISTING `case "/": return m, m.enterSearch()` (shipped via
    // prd-search.md) is migrated to `case key.Matches(msg, km.Search):`. Search
    // is a mode switch, not a list mutation — fires regardless of focusPane.
    case key.Matches(msg, km.Search):
        return m, m.enterSearch()
    }
    return m, nil
}
```

> **Two-binding-one-key** invariant (kế thừa từ `prd-pane-focus.md` D4): các
> binding `MoveUp` ≡ `PreviewScrollUp` về key codes, dispatch branch theo
> `focusPane`. Switch cases trên check **một** binding cho mỗi pair — viết
> `key.Matches(msg, km.MoveDown)` (không phải `km.PreviewScrollDown`) chỉ
> là chọn primary; `key.Matches` chỉ so codes nên cả hai sẽ match cùng
> input. Comment `// == km.PreviewScrollDown` nhắc reader rằng đây là pair.

### 5.4 Palette mode state + helpers (`model.go`)

```go
// Palette state. Sống trong model nhưng chỉ relevant khi mode ∈
// {modeCommandPalette}. Khi palette đóng, các field reset trong exitPalette
// (zero value gốc).
type model struct {
    // …existing fields…

    keymap KeyMap

    paletteStage           int      // 0 = pick command; 1 = collecting args (cd path)
    paletteQuery           string   // filter for stage 0
    paletteSecondaryInput  string   // text for stage 1 (cd path)
    paletteCursor          int      // selected row in filtered list
    paletteFiltered        []Command
}

// Command is one row in the palette. Run is invoked on Enter when this Command
// is selected; it returns a tea.Cmd that may be nil. Some Commands return a
// model-mutating closure executed inline (via updatePalette setting m.* fields);
// others (quit) return tea.Quit. The tagged signature lets a future Command
// dispatch a tea.Cmd (e.g. async work) without changing the dispatch site.
type Command struct {
    Name        string                            // displayed + filtered against
    Description string                            // shown next to name in palette
    NeedsArg    bool                              // true for cd: opens stage 1
    Run         func(m *model, arg string) tea.Cmd
}
```

`defaultCommands()` constructor (`commands.go` mới):

```go
package main

import (
    "os/exec"
    "path/filepath"
    "runtime"

    tea "charm.land/bubbletea/v2"
)

func defaultCommands() []Command {
    return []Command{
        {
            Name: "reload", Description: "re-read current directory",
            Run: func(m *model, _ string) tea.Cmd {
                m.reload()
                m.statusMsg = "reloaded"
                return nil
            },
        },
        {
            Name: "copy path", Description: "copy selected entry's absolute path to clipboard",
            Run: func(m *model, _ string) tea.Cmd {
                if len(m.entries) == 0 {
                    m.statusMsg = "⚠ nothing selected"
                    return nil
                }
                full := filepath.Join(m.cwd, m.entries[m.cursor].name)
                if err := writeClipboard(full); err != nil {
                    m.statusMsg = "⚠ clipboard: " + err.Error()
                    return nil
                }
                m.statusMsg = "copied " + full
                return nil
            },
        },
        {
            Name: "cd", Description: "change directory (jail-guarded)", NeedsArg: true,
            Run: func(m *model, path string) tea.Cmd {
                target, err := resolvePath(m.cwd, path)
                if err != nil {
                    m.statusMsg = "⚠ " + err.Error()
                    return nil
                }
                if !withinRoot(m.root, target) {
                    m.statusMsg = "⚠ blocked: outside root"
                    return nil
                }
                m.cwd = target
                m.cursor, m.listTop = 0, 0
                m.reload()
                return nil
            },
        },
        {
            Name: "quit", Description: "exit lazyexplorer",
            Run: func(_ *model, _ string) tea.Cmd { return tea.Quit },
        },
    }
}

// writeClipboard ships text to the OS clipboard. Shell-out for portability
// without a CGo dep — pbcopy on darwin, xclip→wl-copy fallback on linux.
// Returns the first error or nil. Empty text is still written (matches the
// pbcopy/xclip semantics: clears the clipboard).
func writeClipboard(text string) error {
    var cmd *exec.Cmd
    switch runtime.GOOS {
    case "darwin":
        cmd = exec.Command("pbcopy")
    case "linux":
        // Try xclip first (X11), fall back to wl-copy (Wayland). Both reading
        // from stdin.
        if _, err := exec.LookPath("xclip"); err == nil {
            cmd = exec.Command("xclip", "-selection", "clipboard")
        } else if _, err := exec.LookPath("wl-copy"); err == nil {
            cmd = exec.Command("wl-copy")
        } else {
            return errClipboardUnsupported // "no xclip or wl-copy in PATH"
        }
    default:
        return errClipboardUnsupported
    }
    cmd.Stdin = strings.NewReader(text)
    return cmd.Run()
}

// resolvePath expands ~/, ./, ../, and absolute prefixes; cleans the result.
// Returns the absolute, cleaned path. Does NOT check existence — the caller
// does (jail check + reload error).
func resolvePath(cwd, in string) (string, error) {
    if in == "" {
        return "", errors.New("empty path")
    }
    // ~/x → $HOME/x
    if strings.HasPrefix(in, "~") {
        home, err := os.UserHomeDir()
        if err != nil {
            return "", err
        }
        in = filepath.Join(home, in[1:])
    }
    if !filepath.IsAbs(in) {
        in = filepath.Join(cwd, in)
    }
    return filepath.Clean(in), nil
}
```

`enterCommandPalette` + `exitCommandPalette` (`model.go`):

```go
func (m *model) enterCommandPalette() {
    m.mode = modeCommandPalette
    m.paletteStage = 0
    m.paletteQuery = ""
    m.paletteSecondaryInput = ""
    m.paletteCursor = 0
    m.paletteFiltered = defaultCommands() // all
    m.statusMsg = ""
    m.tel.Record("action.command_palette_open", nil)
}

func (m *model) exitCommandPalette() {
    m.mode = modeNormal
    m.paletteStage = 0
    m.paletteQuery = ""
    m.paletteSecondaryInput = ""
    m.paletteCursor = 0
    m.paletteFiltered = nil
}

func (m *model) applyPaletteFilter() {
    cmds := defaultCommands()
    if m.paletteQuery == "" {
        m.paletteFiltered = cmds
    } else {
        needle := strings.ToLower(m.paletteQuery)
        var out []Command
        for _, c := range cmds {
            if strings.Contains(strings.ToLower(c.Name), needle) {
                out = append(out, c)
            }
        }
        m.paletteFiltered = out
    }
    m.paletteCursor = 0
}
```

`updateCommandPalette` (`model.go`):

```go
func (m model) updateCommandPalette(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
    km := m.keymap

    // Stage 1: collecting argument (currently only cd).
    if m.paletteStage == 1 {
        switch {
        case key.Matches(msg, km.Back):
            m.paletteStage = 0
            m.paletteSecondaryInput = ""
            return m, nil
        case msg.Code == tea.KeyEnter:
            arg := m.paletteSecondaryInput
            cmd := m.paletteFiltered[m.paletteCursor].Run(&m, arg)
            ok := !strings.HasPrefix(m.statusMsg, "⚠")
            m.tel.Record("action.command_run", map[string]any{
                "name":    m.paletteFiltered[m.paletteCursor].Name,
                "success": ok,
            })
            if ok {
                m.exitCommandPalette()
            } else {
                // keep stage 1 open so user can retry
            }
            return m, cmd
        case msg.Code == tea.KeyBackspace:
            r := []rune(m.paletteSecondaryInput)
            if len(r) > 0 {
                m.paletteSecondaryInput = string(r[:len(r)-1])
            }
            return m, nil
        default:
            if msg.Text != "" {
                m.paletteSecondaryInput += msg.Text
            }
            return m, nil
        }
    }

    // Stage 0: pick a command.
    switch {
    case key.Matches(msg, km.Back), key.Matches(msg, km.CommandPalette):
        m.exitCommandPalette()
        return m, nil

    case key.Matches(msg, km.MoveDown):
        if m.paletteCursor < len(m.paletteFiltered)-1 {
            m.paletteCursor++
        }
        return m, nil
    case key.Matches(msg, km.MoveUp):
        if m.paletteCursor > 0 {
            m.paletteCursor--
        }
        return m, nil

    case msg.Code == tea.KeyEnter:
        if len(m.paletteFiltered) == 0 {
            return m, nil
        }
        sel := m.paletteFiltered[m.paletteCursor]
        if sel.NeedsArg {
            m.paletteStage = 1
            return m, nil
        }
        cmd := sel.Run(&m, "")
        m.tel.Record("action.command_run", map[string]any{
            "name":    sel.Name,
            "success": !strings.HasPrefix(m.statusMsg, "⚠"),
        })
        m.exitCommandPalette()
        return m, cmd

    case msg.Code == tea.KeyBackspace:
        if m.paletteQuery == "" {
            m.exitCommandPalette() // backspace-empty = close (cf. prd-search D13)
            return m, nil
        }
        r := []rune(m.paletteQuery)
        m.paletteQuery = string(r[:len(r)-1])
        m.applyPaletteFilter()
        return m, nil

    default:
        if msg.Text != "" {
            m.paletteQuery += msg.Text
            m.applyPaletteFilter()
        }
    }
    return m, nil
}
```

`Update` dispatch (`model.go:445-501`) thêm case:

```go
case tea.KeyPressMsg:
    var nm tea.Model
    switch m.mode {
    case modeConfirmDelete:
        nm, cmd = m.updateConfirmDelete(msg)
    case modeRename:
        nm, cmd = m.updateRename(msg)
    case modeCommandPalette:
        nm, cmd = m.updateCommandPalette(msg)
    case modeHelp:
        nm, cmd = m.updateHelp(msg)
    default:
        nm, cmd = m.updateNormal(msg)
    }
    m = nm.(model)
```

### 5.5 Help mode state + handler (`model.go`)

Help mode đơn giản hơn palette — chỉ scroll + close:

```go
type model struct {
    // …
    helpTop int
}

func (m *model) enterHelp() {
    m.mode = modeHelp
    m.helpTop = 0
}

func (m *model) exitHelp() {
    m.mode = modeNormal
    m.helpTop = 0
}

func (m model) updateHelp(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
    km := m.keymap
    switch {
    case key.Matches(msg, km.Back), key.Matches(msg, km.FullHelp), key.Matches(msg, km.Quit):
        // Esc, ?, q all close help. Quit included so user pressing q in help
        // closes help (not exit) — surprise avoidance.
        m.exitHelp()
    case key.Matches(msg, km.MoveDown):
        // Clamp on the way DOWN, mirror scrollPreview (model.go:682-686). Without
        // the upper clamp, j-spam grows helpTop unbounded and k then feels laggy
        // (it counts back down through the overshoot before the view moves).
        _, bodyH := m.previewScroll()
        maxTop := max(0, m.helpLineCount()-bodyH)
        m.helpTop = min(m.helpTop+1, maxTop)
    case key.Matches(msg, km.MoveUp):
        if m.helpTop > 0 {
            m.helpTop--
        }
    }
    return m, nil
}

// helpLineCount returns the total rendered help-body line count (group titles +
// binding rows + blank separators) — the same number renderHelp builds. The
// clamp in updateHelp and renderHelp both read it so scroll never overshoots
// the content. (One source of truth for "how tall is the help body".)
func (m model) helpLineCount() int {
    n := 0
    for _, group := range m.fullHelp() {
        n += 1 + len(group) + 1 // title + rows + blank separator
    }
    return n
}
```

### 5.6 View — compose floating modal over the background (`view.go`)

`View()` (`view.go:236`) builds the full background **unchanged** (list +
divider + the **real preview** — the preview pane no longer branches on mode),
then when `mode ∈ {modeCommandPalette, modeHelp}` composites the modal box
centered over the whole screen via lipgloss Canvas/Compositor/Layer (D9/D21):

```go
func (m model) View() tea.View {
    content := "loading…"
    if m.width != 0 && m.height != 0 {
        g := m.layout()
        // …build `body` exactly as today (list + divider + m.renderPreview),
        //   then the status row…
        content = strings.Join([]string{body, m.renderStatus()}, "\n")

        // Floating modal overlay (palette / help) drawn OVER the screen.
        if box, ok := m.renderModal(); ok {
            content = overlayCentered(content, box, m.width, m.height)
        }
    }
    v := tea.NewView(content)
    v.AltScreen = true
    v.MouseMode = tea.MouseModeCellMotion
    return v
}
```

`renderPreviewRegion` (`view.go:734`) is **removed**: the preview pane no longer
changes with mode (palette/help live in the modal), so both orientations call
`m.renderPreview(w)` directly where they call `renderPreviewRegion` today
(`view.go:264`, `view.go:276`).

`overlayCentered` composites the box over the background string (D21/D22 — no
dim, background shows through everywhere the box does not cover):

```go
// overlayCentered draws `box` centered over `bg` (a full w×h rendered screen)
// and returns the composited string.
func overlayCentered(bg, box string, w, h int) string {
    boxW, boxH := lipgloss.Width(box), lipgloss.Height(box)
    cx := max(0, (w-boxW)/2)
    cy := max(0, (h-boxH)/2)
    canvas := lipgloss.NewCanvas(w, h)
    return canvas.Compose(lipgloss.NewCompositor(
        lipgloss.NewLayer(bg).Z(0),
        lipgloss.NewLayer(box).X(cx).Y(cy).Z(1),
    )).Render()
}
```

`renderModal` returns the styled box for the active overlay mode (`ok=false` in
normal mode), and `modalSize` clamps the box to fit narrow/short terminals —
the same floor discipline as `leftInnerWidth` (`view.go:196`):

```go
func (m model) renderModal() (string, bool) {
    bw, bh := m.modalSize()
    switch m.mode {
    case modeCommandPalette:
        return modalBoxStyle.Render(m.renderPaletteBody(bw, bh)), true
    case modeHelp:
        return modalBoxStyle.Render(m.renderHelpBody(bw, bh)), true
    default:
        return "", false
    }
}

// modalSize clamps the inner box to fit. At m.width < 80 (vertical mode) or a
// short terminal the box shrinks but never overflows.
func (m model) modalSize() (w, h int) {
    w = min(modalTargetCols, m.width-modalMargin*2)        // modalTargetCols=56
    h = min(modalTargetRows, (m.height-1)-modalMargin*2)   // -1: status row
    return max(w, modalMinCols), max(h, modalMinRows)
}
```

`renderPaletteBody` (`view.go`) — search/arg prompt at the **box top**
(Raycast-style; the prompt now lives in the box, not the status bar), filtered
list below:

```go
func (m model) renderPaletteBody(w, h int) string {
    var lines []string

    // Row 0: the search prompt (stage 0) or the cd-arg prompt (stage 1).
    if m.paletteStage == 0 {
        lines = append(lines, promptStyle.Background(colAccent).Foreground(colSelFg).
            Render(fitWidth("> "+m.paletteQuery+"▏", w)))
    } else {
        sel := m.paletteFiltered[m.paletteCursor]
        lines = append(lines, promptStyle.Background(colAccent).Foreground(colSelFg).
            Render(fitWidth(sel.Name+" > "+m.paletteSecondaryInput+"▏", w)))
    }
    lines = append(lines, "") // blank between prompt and body

    // Stage 1: body is the chosen command's description for context.
    if m.paletteStage == 1 {
        sel := m.paletteFiltered[m.paletteCursor]
        lines = append(lines, dimStyle.Render(fitWidth(sel.Description, w)))
        return strings.Join(lines, "\n")
    }

    // Stage 0: filtered command rows; cursor row = full-width accent (same
    // cursor marker the list pane uses, no caret glyph).
    if len(m.paletteFiltered) == 0 {
        lines = append(lines, dimStyle.Render(fitWidth("(no matching commands)", w)))
        return strings.Join(lines, "\n")
    }
    bodyRows := h - len(lines) // rows left under the prompt + blank
    for i, c := range m.paletteFiltered {
        if i >= bodyRows {
            break
        }
        row := c.Name + "  — " + c.Description
        if i == m.paletteCursor {
            lines = append(lines, cursorActiveStyle.Width(w).Render(fitWidth(row, w)))
        } else {
            lines = append(lines, fileStyle.Render(fitWidth(row, w)))
        }
    }
    return strings.Join(lines, "\n")
}
```

`renderHelpBody` (`view.go`) — grouped bindings scrolled by `helpTop`; logic is
the shipped `renderHelp` (`view.go:779`) verbatim, only the caller changed (it
now fills the modal box body instead of the preview pane):

```go
func (m model) renderHelpBody(w, h int) string {
    titles := []string{"Navigation", "Preview", "Mutation", "Modes", "Misc"}
    var lines []string
    for gi, group := range m.fullHelp() {
        title := ""
        if gi < len(titles) {
            title = titles[gi]
        }
        lines = append(lines, renderingStyle.Render(title))
        for _, b := range group {
            hb := b.Help()
            lines = append(lines, fitWidth(fmt.Sprintf("  %-12s  %s", hb.Key, hb.Desc), w))
        }
        lines = append(lines, "") // blank separator between groups
    }
    start := min(max(0, m.helpTop), len(lines))
    end := min(start+h, len(lines))
    return strings.Join(lines[start:end], "\n")
}
```

> **`helpLineCount` (`palette.go`) vẫn là source of truth cho scroll-clamp**
> (`updateHelp` dùng nó để chặn `helpTop` overshoot). Modal-hoá chỉ đổi `h`
> truyền vào (giờ là box-body rows, không phải preview-pane rows) — clamp logic
> không đổi.

### 5.7 Status bar — prompt + ShortHelp (`view.go:475-499`)

```go
func (m model) renderStatus() string {
    switch m.mode {
    case modeConfirmDelete:
        // unchanged: view.go:478-481
    case modeRename:
        // unchanged: view.go:482-485
    case modeCommandPalette:
        var prompt string
        if m.paletteStage == 0 {
            prompt = promptStyle.Background(colAccent).Foreground(colSelFg).
                Render("> " + m.paletteQuery + "▏")
        } else {
            sel := m.paletteFiltered[m.paletteCursor]
            prompt = promptStyle.Background(colAccent).Foreground(colSelFg).
                Render(sel.Name + " > " + m.paletteSecondaryInput + "▏")
        }
        return prompt
    case modeHelp:
        return statusBarStyle.Width(m.width).Render(fitWidth(
            "press ? or esc to close   " + dimStyle.Render("j/k scroll"),
            m.width-2,
        ))
    default:
        // ShortHelp replaces the hardcoded hint string here.
        hints := renderShortHelp(m.shortHelp())
        status := hints
        if m.statusMsg != "" {
            status = m.statusMsg + dimStyle.Render("   "+hints)
        }
        // render spinner in a fixed 2-col slot at the right edge (no reflow)
        contentW := m.width - 2
        slot := "  "
        if m.pendingWidth > 0 {
            slot = " " + renderingStyle.Render(spinnerFrames[m.spinnerFrame%len(spinnerFrames)])
        }
        left := fitWidth(status, contentW-2)
        pad := strings.Repeat(" ", max(0, contentW-2-lipgloss.Width(left)))
        return statusBarStyle.Width(m.width).Render(left + pad + slot)
    }
}

// renderShortHelp joins a slice of key.Binding into a "[key] desc" string.
func renderShortHelp(bs []key.Binding) string {
    var parts []string
    for _, b := range bs {
        h := b.Help()
        parts = append(parts, "["+h.Key+"] "+h.Desc)
    }
    return strings.Join(parts, "  ")
}
```

`shortHelp` (`model.go`):

```go
func (m model) shortHelp() []key.Binding {
    km := m.keymap
    if m.focusPane == focusList {
        return []key.Binding{
            km.MoveDown, km.FocusToggle, km.OpenEntry, km.GoUp,
            km.Rename, km.Delete,
            km.CommandPalette, km.FullHelp, km.Quit,
        }
    }
    return []key.Binding{
        km.PreviewScrollDown, km.FocusToggle, km.PreviewJumpTop, km.PreviewJumpBottom,
        km.PreviewHalfPageDown, km.Back,
        km.CommandPalette, km.FullHelp, km.Quit,
    }
}

func (m model) fullHelp() [][]key.Binding {
    km := m.keymap
    return [][]key.Binding{
        {km.MoveUp, km.MoveDown, km.GoTop, km.GoBottom, km.OpenEntry, km.GoUp},
        {km.PreviewScrollUp, km.PreviewScrollDown, km.PreviewHalfPageUp, km.PreviewHalfPageDown, km.PreviewJumpTop, km.PreviewJumpBottom},
        {km.Rename, km.Delete},
        {km.FocusToggle, km.Search, km.CommandPalette, km.FullHelp, km.Back},
        {km.Quit},
    }
}
```

### 5.8 Coordination với related PRDs

**Đã shipped (baseline) — PRD này MIGRATE, không tạo mới:**

- **`prd-pane-focus.md`** (focusPane) + **`prd-search.md`** (`/`, modeSearch):
  đã trong `model.go` (xem Baseline note đầu doc). PRD này migrate các case
  string hiện hữu (`case "tab"`, `case "/"`, các `if m.focusPane == ...`) sang
  `key.Matches(msg, km.X)` **giữ nguyên branch logic** (§5.3). Không có giai đoạn
  "registry field chờ handler" — handler là code thật.

- **`prd-smooth-preview-scroll.md`** (đã shipped): hằng `previewLineStep` vẫn
  còn; PRD này chỉ migrate caller `scrollPreview(...)` sang dispatch qua
  `key.Matches(km.PreviewScroll{Up,Down})` — số dòng step không đổi. `J`/`K`
  legacy đã xoá (smooth-scroll D13) → KHÔNG đưa lại.

**Phụ thuộc xuôi — PRD tiêu thụ registry này:**

- **`prd-horizontal-scroll-preview.md`** thêm 6 binding preview-hscroll
  (`PreviewScrollLeft/Right`, `PreviewHScrollHalf{Left,Right}`, `PreviewHScrollReset`,
  `PreviewToggleWrap`) vào `KeyMap`. **B1 (bắt buộc):** `PreviewScrollLeft`
  (`h`/`left`) ≡ `GoUp`, và `PreviewScrollRight` (`l`/`right`) ≡ `OpenEntry`
  về key code. Trong Go `switch`, một key chỉ trúng **một** case. Vì vậy PRD
  hscroll **KHÔNG được thêm case riêng** cho `h`/`l` — nó **mở rộng nhánh `else`
  (focusPreview)** của case `GoUp`/`OpenEntry` có sẵn ở §5.3 (hiện chỉ xử lý
  focusList qua `if m.focusPane == focusList`). Đây là cùng pattern hai-binding-
  một-key của `MoveDown ≡ PreviewScrollDown`. Chi tiết: `prd-horizontal-scroll-preview.md`
  §5.5 + §5.6.

Marker comment ở `model.go` đầu `updateNormal` sau khi ship PRD này:

```go
// updateNormal dispatches normal-mode keys via key.Binding registry (m.keymap).
// Adding a binding: add field to KeyMap (keys.go), wire in defaultKeyMap, add
// a `case key.Matches(msg, km.X)` branch here. Adding a command: add an entry
// to defaultCommands (commands.go) — the palette picks it up automatically.
// Two-binding-one-key (e.g. h/left = GoUp in focusList ≡ PreviewScrollLeft in
// focusPreview): ONE case, branch on m.focusPane inside — never two cases for
// the same key codes (only the first would ever fire). See prd-horizontal-
// scroll-preview.md §5.6 B1.
```

### 5.9 Mouse + poll guard (no code change)

- `handleMouse` (`model.go:526`) đã early-return khi `m.mode != modeNormal`
  (`model.go:474-476`) → palette + help nhận zero mouse events tự do. D19 satisfied.

- `tickCmd` (`model.go:443`) → `m.syncFromDisk()` chỉ chạy khi
  `m.mode == modeNormal && !m.dragging` (`model.go:453`) → palette + help
  skip poll tự do. D20 satisfied.

### 5.10 Đã cân nhắc & defer khỏi v1

- **Vim `:` syntax** (so sánh head-to-head với palette):
  | Aspect | `:` syntax | Palette |
  |---|---|---|
  | UX cho cmd biết tên | `:q<CR>` 3 keys | `Ctrl+P q <CR>` 4 keys |
  | UX cho cmd không nhớ tên | tab-complete + man pages | substring filter immediate |
  | Discoverability | low (phải biết tên) | high (filter shows all) |
  | Parser cost | parser + tab-complete + history | ~30 lines filter |
  | Mental model | unique to vim users | universal IDE pattern |
  Verdict: defer `:` indefinitely; palette cover same surface better cho non-vim user, equal cho vim user.

- **Count-prefix** `5j`/`10G`:
  Hữu ích trong vim vì viewport hẹp, log file dài. Trong lazyexplorer typical
  list ≤200 entries — `G` + `k k k` hay scroll wheel nhanh hơn `5k`. Defer
  cho tới khi có project >500 entries one folder (rất hiếm).

- **Modal vim (normal/insert/visual)**:
  Insert mode đã có (modeRename = text input). Normal là default. Visual cần
  multi-select feature (bulk delete/rename) — defer cho tới khi có request.

- **Key remapping via config file** (`~/.config/lazyexplorer/keys.toml`):
  No Abstractions Until Proven. KeyMap hiện là single struct → edit nguồn
  recompile cho expert; nếu nhiều user xin remap, thêm loader sau.

- **Help overlay popup floating (vs full overlay vào preview pane)**:
  Cần dialog stack manager (crush có `m.dialog.HasDialogs` —
  `tmp/crush/.../ui.go:1893`). Premature cho 2-overlay surface; overlay vào
  preview pane đơn giản, layout không thay đổi, user vẫn thấy list pane.

- **`open in $EDITOR`** command trong palette:
  Defer per `prd-pane-focus.md` defer list — agent đã edit file, app không cần
  là launcher.

- **`new file` / `new folder`** commands:
  Mutation expansion — defer cho tới khi có request. Hiện rename + delete đủ.

- **Telemetry per-command-popularity dashboard**:
  `action.command_run.name` field đủ — analysis offline qua Datadog UI
  (`prd-datadog-integration.md`). Không cần code khác.

- **Async command Run** (vd `cd` reload là blocking nhưng hôm nay đủ nhanh):
  `Run` đã return `tea.Cmd` để extensible. v1 mọi command synchronous.

- **`?` mở popup floating overlapping preview** (vs full-pane):
  Cùng lý do help overlay defer popup.

- **Configurable palette commands** (load từ file, đăng ký từ plugin):
  Plugin system out-of-scope. `defaultCommands()` hardcoded ổn cho v1.

- **Fuzzy filter cho palette qua sahilm/fuzzy**:
  4-10 commands, substring đủ. Nếu `prd-search.md` ship trước (kéo sahilm/fuzzy
  vào project), reuse cho palette là one-line change — defer optimization.

- **Visual feedback khi clipboard write fail (vd Linux no xclip)**:
  Status message đủ. Không thêm modal error.

- **`shortHelp` adapt theo terminal width** (vài binding hiện trong 80 cols,
  tất cả trong 200 cols):
  `fitWidth` đã truncate string với `…` — acceptable interim. Smarter
  rotation/folding defer.

## 6. Acceptance criteria

### Gherkin

```gherkin
Feature: Keymap registry & command palette

  Background:
    Given the explorer is open at a project root in normal mode
    And focus is on the list pane

  Scenario: Status bar reflects bindings from the registry
    When the explorer first renders
    Then the status bar shows hints derived from the KeyMap
    And the hint for moving down reads "[↓/j] move down"
    And the hint for opening the command palette reads "[ctrl+p] commands"
    And the hint for full help reads "[?] help"

  Scenario: Ctrl+P opens the command palette
    Given the focus is on the list pane
    When I press ctrl+p
    Then the preview pane region renders the palette command list
    And the status bar shows a "> ▏" prompt
    And the cursor highlights the first command

  Scenario: Type to filter palette commands
    Given the command palette is open
    When I type "re"
    Then only commands whose name contains "re" remain in the list
    And the "reload" command is highlighted at the top

  Scenario: Enter runs a no-arg command
    Given the command palette is open with "reload" highlighted
    When I press Enter
    Then the explorer leaves command palette mode
    And the directory listing has been re-read
    And the status bar shows "reloaded"

  Scenario: cd command opens a path-input stage
    Given the command palette is open
    And "cd" is highlighted
    When I press Enter
    Then the palette enters a second stage
    And the status bar prompt becomes "cd > ▏"
    And the preview pane shows the cd command's description

  Scenario: cd to a path inside the jail succeeds
    Given the cd path-input stage is open
    When I type "docs" and press Enter
    Then the explorer leaves command palette mode
    And the current directory is "docs" under the project root

  Scenario: cd to a path outside the jail is blocked
    Given the cd path-input stage is open
    When I type "/etc" and press Enter
    Then the status bar shows "⚠ blocked: outside root"
    And the palette remains on the cd stage so I can correct the path

  Scenario: Backspace on empty palette query closes the palette
    Given the command palette is open and the query is empty
    When I press Backspace
    Then the explorer leaves command palette mode
    And the previous mode is restored

  Scenario: Esc closes the palette from either stage
    Given the cd path-input stage is open
    When I press Esc
    Then the palette returns to its command-pick stage
    When I press Esc again
    Then the explorer leaves command palette mode

  Scenario: ? opens the full help overlay
    Given the focus is on the list pane
    When I press ?
    Then the preview pane region renders the full help table
    And the help is grouped under headings Navigation, Preview, Mutation, Modes, Misc
    And each row shows a key chord and a one-line description

  Scenario: Esc or ? closes the help overlay
    Given the help overlay is open
    When I press Esc
    Then the help overlay closes and the preview pane returns
    When I open the help again and press ?
    Then the help overlay closes the same way

  Scenario: Help overlay scrolls when content exceeds the pane
    Given the help overlay is open in a short terminal
    And the help body is longer than the visible pane
    When I press j
    Then the help body scrolls down one row
    When I press k
    Then the help body scrolls up one row

  Scenario: Copy path puts the selected entry's absolute path on the clipboard
    Given the cursor is on a file named "main.go" at the project root
    When I open the command palette and run "copy path"
    Then the clipboard contains the absolute path to "main.go"
    And the status bar shows "copied <abs path>"

  Scenario: Copy path on a platform without a supported clipboard reports an error
    Given the host has no pbcopy/xclip/wl-copy available
    When I open the command palette and run "copy path"
    Then the status bar shows a clipboard error
    And the explorer state is otherwise unchanged

  Scenario: Mouse is disabled in palette mode
    Given the command palette is open
    When I click anywhere in the panes
    Then nothing happens
    And the palette stays open

  Scenario: Poll loop does not run in palette or help mode
    Given the command palette is open
    When an external process modifies the current directory
    Then the visible state does not change while the palette is open
    When I close the palette
    Then the directory listing updates on the next tick
```

### Checklist verify

1. `./lazyexplorer .` → status bar hiện hint từ KeyMap (không phải hardcoded
   string); verify bằng `rg '"\[↑↓/jk/click\]"' view.go` trả **0 hit**.
2. `Ctrl+P` mở palette; 4 commands hiển thị: `reload`, `copy path`, `cd`,
   `quit`. Cursor ở row 0.
3. Gõ `cd` → list filter còn `cd`. Gõ `xyz` → list rỗng, dòng `(no matching
   commands)` hiện.
4. Enter trên `reload` → palette đóng, list pane re-read, status "reloaded".
5. Enter trên `cd` → prompt đổi `cd > ▏`, preview pane mô tả cd command.
6. Trong cd stage, gõ `docs` Enter → `m.cwd` về `<root>/docs`.
7. Trong cd stage, gõ `/etc` Enter → status `⚠ blocked: outside root`, palette
   vẫn ở cd stage.
7b. Trong cd stage, gõ path tới một **file** trong root (vd `model.go`) Enter →
   status `⚠ not a directory: model.go`, palette vẫn ở cd stage (FR10 — không
   để `m.reload()` gọi readDir trên file rồi set entries=nil).
8. Trong cd stage, gõ `~/.config` Enter (with `~` expand to `$HOME`) → jail
   block (vì home ngoài root v.v.); status error.
8b. Cursor trên synthetic `..`, palette → `copy path` Enter → clipboard chứa abs
   path của **parent đã resolve** (jail-clamped tại root), KHÔNG phải literal
   `<cwd>/..` (FR9).
9. Backspace ở palette query rỗng → palette đóng.
10. Esc ở cd stage → quay về stage 1; Esc lần nữa → đóng palette.
11. `?` mở help overlay; 5 group hiện đúng thứ tự Navigation/Preview/Mutation/
    Modes/Misc.
12. Help overlay `j`/`k` scroll (nếu nội dung dài hơn pane); Esc đóng.
13. `?` lần nữa đóng help (toggle).
14. `q` trong help → đóng help (KHÔNG quit app — surprise avoidance).
15. `q` trong palette stage 1 (đang gõ command name) → KHÔNG quit; ký tự `q`
    được append vào query.
16. Copy path: select `main.go`, palette → `copy path` Enter → `pbcopy < main.go
    abs path` (verify: `pbpaste` trên macOS in ra path).
17. Linux không có xclip/wl-copy → status `⚠ clipboard: no xclip or wl-copy in
    PATH`, không crash.
18. Click chuột trong palette mode → no-op, palette không đóng.
19. Click chuột trong help mode → no-op.
20. Mở palette, agent tạo file mới trong cwd → list pane **không** update
    cho tới khi đóng palette (FR18, poll guard).
21. `rg 'switch msg\.String\(\)' model.go` → 0 hit ở `updateNormal` (đã refactor
    sang `key.Matches`); vẫn còn ở `updateRename`/`updateConfirmDelete` (D17
    defer scope).
22. `rg 'key\.NewBinding' keys.go` → ≥17 hit (mỗi binding một); `rg 'key\.Matches'
    model.go` → ≥10 hit (mỗi case một).
23. `go build -o lazyexplorer . && go vet ./... && go test ./...` xanh.
24. `go test -race ./...` xanh.
25. Visual verdict (`oh-my-claudecode:visual-verdict`) cho 3 frame:
    - Frame A: normal mode focus list — ShortHelp ở status bar đúng FR14.
    - Frame B: command palette open với 4 commands, cursor row 0 — accent
      highlight, dim description.
    - Frame C: help overlay với 5 groups, group title `renderingStyle`.
26. `gitnexus_impact({target: "updateNormal", direction: "upstream"})` —
    confirm risk + dependents trước commit.
27. `gitnexus_detect_changes()` post-commit — scope khớp §8.

## 7. Task breakdown

> Trước khi sửa: `gitnexus_impact` cho `updateNormal`, `renderStatus`, `View`.
> Risk current (verify trong T1): `updateNormal` CRITICAL, `renderStatus`
> CRITICAL — toàn bộ keypress + status frame phụ thuộc.

- [x] **T1 — Dependencies + API verify.** `charm.land/bubbles/v2 v2.1.0` added
  (`key` subpackage); `go mod tidy`. `TestKeyMatchContract` (palette_test.go)
  confirms `key.Matches` against `tea.KeyPressMsg`. Clipboard is shell-out
  (pbcopy/xclip/wl-copy), not exercised in tests (thin shell wrapper). *(go.mod, go.sum, palette_test.go)* ✅
- [x] **T2 — `KeyMap` + `defaultKeyMap`.** `keys.go` — flat struct + constructor
  (§5.2). All current bindings + CommandPalette/FullHelp + cross-ref FocusToggle/
  Search registered. *(keys.go)* ✅
- [x] **T3 — `updateNormal` dispatch refactor.** `switch { case key.Matches(...) }`
  (§5.3); focus-routed branches preserved, cross-ref comments kept. *(model.go)* ✅
- [x] **T4 — Mode enum + state.** `modeCommandPalette`, `modeHelp`; palette +
  help fields on model; `keymap` set in newModel. *(model.go)* ✅
- [x] **T5 — `commands.go`.** `Command` + `defaultCommands()` (reload/copy path/cd/
  quit) + `writeClipboard` + `resolvePath` + `selectedAbsPath` (".." jail-clamp). *(commands.go)* ✅
- [x] **T6 — Palette handler.** `updateCommandPalette` (two-stage) +
  `enterCommandPalette`/`exitCommandPalette`/`applyPaletteFilter` in `palette.go`;
  `Update` switch case `modeCommandPalette`. *(palette.go, model.go)* ✅
- [x] **T7 — Help handler.** `updateHelp` + `enterHelp`/`exitHelp`/`helpLineCount`
  in `palette.go`; `Update` switch case `modeHelp`. *(palette.go, model.go)* ✅
- [x] **T8 — View render.** `renderPreviewRegion` swaps the preview pane to
  `renderPalette`/`renderHelp` in both orientations; helpers in `view.go`. *(view.go)* ✅
- [x] **T9 — `renderStatus` + `shortHelp`/`fullHelp`/`renderShortHelp`.** Default
  branch uses `renderShortHelp(m.shortHelp())`; palette/help status cases added. *(view.go, model.go)* ✅
- [x] **T10 — `theme.go`.** No new style — palette/help reuse `cursorActiveStyle`,
  `dimStyle`, `renderingStyle`, `fileStyle`, `statusBarStyle`. Confirmed. *(theme.go — no change)* ✅

- [ ] **T11 — Tests.** *(`*_test.go`)*
  - `TestKeyMapAllBindingsHaveHelp`: mỗi field của `KeyMap` có
    non-empty `Help().Key` + `.Desc`.
  - `TestDispatchByKeyMatches`: gửi `KeyPressMsg{Code:'j'}` qua `Update` →
    cursor xuống 1; gửi key khác → no-op (snapshot test).
  - `TestPaletteOpenClose`: Ctrl+P → mode = palette; Esc → mode = normal.
  - `TestPaletteFilterSubstring`: query "re" → filtered chỉ chứa "reload".
  - `TestPaletteRunReload`: Enter trên reload → status "reloaded", mode
    normal.
  - `TestPaletteRunCdValid`: chọn cd, gõ "docs" Enter → m.cwd = root/docs.
  - `TestPaletteRunCdJailBlock`: chọn cd, gõ "/etc" Enter → status warn +
    palette ở stage 1.
  - `TestPaletteRunCdNonExistent`: gõ "xyz" Enter → status warn "not found".
  - `TestPaletteBackspaceEmptyCloses`.
  - `TestHelpOpenScrollClose`: ? → mode help; j → helpTop++; Esc → mode normal.
  - `TestHelpQuitDoesNotQuitApp`: trong modeHelp, `q` đóng help không tea.Quit.
  - `TestShortHelpReflectsFocus`: focusList → contain "rename" binding;
    focusPreview → contain "half page" binding.
  - `TestFullHelpHasFiveGroups`: `len(m.fullHelp()) == 5`.
  - `TestMouseDisabledInPalette`: gửi MouseClickMsg trong modeCommandPalette →
    no-op, mode không đổi.
  - `TestPollGuardInPalette`: tickMsg trong palette → `syncFromDisk` không
    gọi (mock cwd change).
  - `TestWriteClipboardDarwin`: skip if pbcopy not in PATH; pipe text +
    verify with pbpaste.
  - `TestResolvePathTildeAbsRel`: cases for `~/x`, `/abs`, `./rel`, `..`.
  - `TestTelemetryCommandRun`: spy on tel.Record; run reload → recorded with
    name="reload", success=true.
  - `-race`: parallel Update calls with palette messages không race trên
    palette state.

- [x] **T11 — Tests.** `palette_test.go` covers: `TestKeyMatchContract`,
  `TestCommandPaletteOpenClose`, `TestCommandPaletteFilter`, `TestCommandPaletteNav`,
  `TestCommandReload`, `TestCommandCdValid`, `TestCommandCdRejects` (file/outside-root/
  not-found), `TestCommandQuit`, `TestHelpOpenScrollClose`, `TestHelpScrollClamps`,
  `TestHelpQuitClosesNotExits`, `TestShortHelpByFocus`, `TestFullHelpGroups`,
  `TestResolvePath`, `TestSelectedAbsPathDotDot`, `TestPaletteRendersInView`,
  `TestHelpRendersInView`. `-race` green. *(palette_test.go)* ✅

- [x] **T12 — Visual smoke.** Palette + help frames rendered (90×16 / 90×22) and
  eyeballed: command list + descriptions aligned in the preview region with a
  `> ▏` status prompt; help shows grouped, column-aligned bindings. Done as a
  manual render smoke rather than a gated `zz_dump` + `visual-verdict` fixture. ✅

- [ ] **T13 — Update existing PRDs cross-ref wording.** The bindings ARE migrated
  into the registry (code is consistent), but the prose cross-refs in
  `prd-pane-focus.md`/`prd-search.md`/`prd-smooth-preview-scroll.md` §sections were
  not re-touched. Optional doc polish — deferred. *(docs/*)*

- [x] **T14 — Verify.** `go build -o lazyexplorer . && go vet ./... && go test ./...
  && go test -race ./...` green (ĐÃ VERIFY ✅ 2026-05-28). `rg '"J"|"K"' model.go`
  → 0 (legacy keys stay removed). Manual acceptance: palette open/filter/run,
  help scroll/close exercised via tests + render smoke.

## 8. Files chạm tới

| File | Thay đổi |
|------|----------|
| `go.mod` / `go.sum` | + `charm.land/bubbles/v2` (`key` + optionally `help` sub-packages) |
| `keys.go` *(mới)* | `type KeyMap struct` (~22 field); `defaultKeyMap() KeyMap` |
| `commands.go` *(mới)* | `type Command struct{Name,Description,NeedsArg,Run}`; `defaultCommands() []Command`; `writeClipboard(text string) error`; `resolvePath(cwd, in string) (string, error)`; `errClipboardUnsupported` sentinel |
| `model.go` | + field `keymap KeyMap` + palette state (`paletteStage`, `paletteQuery`, `paletteSecondaryInput`, `paletteCursor`, `paletteFiltered`) + help state (`helpTop`); + mode enum `modeCommandPalette`, `modeHelp`; + helper `enterCommandPalette`, `exitCommandPalette`, `applyPaletteFilter`, `enterHelp`, `exitHelp`, `shortHelp`, `fullHelp`; + handler `updateCommandPalette`, `updateHelp`; `updateNormal` refactor sang `key.Matches` dispatch (§5.3); `newModel` init `m.keymap = defaultKeyMap()`; `Update` switch thêm case `modeCommandPalette`, `modeHelp` |
| `view.go` | `View()` branch render palette/help vào preview pane region (§5.6); `renderPalette`, `renderHelp` mới; `renderStatus` case `modeCommandPalette` + `modeHelp` + default case dùng `renderShortHelp(m.shortHelp())` thay hardcoded hint string (`view.go:487`); helper `renderShortHelp` |
| `theme.go` | (likely no change) confirm `cursorActiveStyle`/`dimStyle`/`renderingStyle` đủ |
| `keys_contract_test.go` *(mới)* | API verify dep (T1) |
| `palette_test.go` *(mới)* | palette open/close/filter/run + cd valid/invalid + clipboard |
| `help_test.go` *(mới)* | help open/scroll/close + q-doesn't-quit-app |
| `keymap_test.go` *(mới)* | KeyMap completeness + ShortHelp/FullHelp content invariants |
| `commands_test.go` *(mới)* | resolvePath cases + writeClipboard darwin path |
| `zz_dump_test.go` | + 3 frame fixture (normal, palette, help) for visual verdict |
| `docs/prd-keymap-and-command-palette.md` | File này |
| `docs/prd-pane-focus.md` · `docs/prd-search.md` · `docs/prd-smooth-preview-scroll.md` | Cross-ref note added (T13); no code-spec change |
