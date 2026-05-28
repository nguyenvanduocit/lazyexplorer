# PRD — Flag `--split`: respawn vào split pane theo terminal

Status: **accepted** · Author: opus 4.7 session · Ngày: 2026-05-28

---

## 1. Bối cảnh & vấn đề

lazyexplorer sinh ra cho **vibe-code workflow**: user giữ nó mở trong một pane *cạnh* agent
(Claude Code) để liếc nhìn và điều hướng cây dự án trong lúc agent làm việc (xem `CLAUDE.md`
§"Goal & Positioning"). Để có layout đó hôm nay, user phải **tự tay**: split terminal → focus
pane mới → gõ `lazyexplorer`. Thao tác lặp mỗi session, và mỗi terminal split một kiểu khác
nhau (tmux `split-window`, Ghostty `cmd+d`, wezterm CLI…), nên không có một phím tắt cơ bắp
chung.

Entry point hiện tại (`main.go:37-100`) chỉ nhận một positional `DIR` cộng `--version`/`--help`
(`main.go:46-61`), rồi vào thẳng Bubbletea (`main.go:95`). Không có đường nào để lazyexplorer
tự đặt mình vào một split pane.

Mong muốn: **một lệnh duy nhất**, gọi ngay từ pane đang chạy agent, tự nhận diện terminal,
mở split pane, chạy lazyexplorer ở pane đó — còn pane gọi giữ nguyên (agent vẫn chạy).

## 2. Goal (1 câu)

Thêm flag `--split[=right|below]`: khi chạy, lazyexplorer detect terminal hiện tại, dùng
API/CLI của terminal đó để mở một split pane chạy `lazyexplorer <root>`, rồi process `--split`
thoát ngay — pane gọi không bị đụng.

**Non-goal làm rõ (chặn scope creep):**

- **Không** đóng/quản lý pane gọi (caller pane). `--split` chỉ *thêm* một pane (xem D10).
- **Không** thêm UI/keybind trong TUI để split — giữ surface tối giản (`CLAUDE.md`
  §"Design Principles"). `--split` là **launcher-mode**, không vào alt-screen.
- **Không** hỗ trợ split đa cấp, layout phức tạp, hay chọn size pane ở v1.
- **Không** tự bật/cài remote-control cho terminal (kitty/wezterm) — đó là config của user.

## 3. Quyết định đã chốt

| # | Quyết định | Giá trị | Lý do |
|---|-----------|---------|-------|
| D1 | Terminal support v1 | tmux · zellij · WezTerm · Kitty · Ghostty (macOS) · iTerm2 (macOS) | Phủ cả multiplexer lẫn emulator phổ biến; user chốt cả 6 |
| D2 | Cú pháp hướng split | `--split` (=right) · `--split=right` · `--split=below` | Right (vertical divider) hợp file-explorer-cạnh-agent; below là tùy chọn |
| D3 | Dispatch theo terminal | **Registry `[]splitEnv`** `{name, detected, buildCmd}`, mirror `previewRenderers` (`fs.go:230`) | Thêm terminal = một entry; nhất quán với pattern đã có (consistency is kindness) |
| D4 | Thứ tự detect | **Multiplexer (tmux, zellij) TRƯỚC emulator (wezterm, kitty, ghostty, iterm2)** | Khi user đang *trong* tmux/zellij, intent là chia pane của multiplexer — không phải mở window emulator thứ hai. Ưu tiên multiplexer đúng **dù env var emulator có persist hay không** khi nested (priority-as-safety-net, không lệ thuộc claim persistence). Invariant load-bearing |
| D5 | Re-exec dùng path nào | `os.Executable()` (absolute) + absolute `root` | Chạy được cả khi binary không trong `$PATH`; không lệ thuộc cwd của pane mới |
| D6 | Tránh đệ quy split | Spawn `lazyexplorer <root>` **không** kèm `--split` | Pane mới chạy bình thường, không split tiếp vô hạn |
| D7 | Telemetry khi `--split` thành công | Short-circuit **TRƯỚC** `InitTelemetry()` (`main.go:78`) | Spawn-rồi-exit không phải session thực; tránh phantom `session.start`/`session.end` (~ms) |
| D8 | Không detect được env | **Warn stderr + chạy bình thường ở pane hiện tại** | Graceful degrade — không bao giờ để user tay trắng |
| D9 | Detect được nhưng spawn lỗi | **Warn stderr (kèm lý do) + chạy bình thường** — mở rộng D8 | `--split` là best-effort; nhất quán với D8. Lý do nêu rõ (vd kitty remote-control off, macOS Accessibility) để user fix |
| D10 | Pane gọi | **Không tắt** | Use case chính: gọi từ pane agent; tắt nó = giết agent |
| D11 | Mechanism mỗi terminal | tmux/zellij/wezterm/kitty: CLI native · Ghostty: AppleScript keystroke · iTerm2: AppleScript `write text` | Ghostty **chưa có CLI split** → buộc keystroke; iTerm2 có scripting API sạch hơn keystroke |
| D12 | `parseArgs` strictness | Unknown flag (`-`-prefix lạ) → lỗi parse + exit 2 | Explicit > implicit; báo lỗi sớm thay vì stat `--foo` như thư mục. Đã verify không script/test nào dựa hành vi cũ (`rg "lazyexplorer (--\|-)"` chỉ ra `--version` ở goreleaser + `docs/index.html`) |

## 4. Functional requirements

- **FR1** — `lazyexplorer --split` parse thành công: `split=true`, `direction=right` (default).
- **FR2** — `--split=right` / `--split=below` set direction tương ứng; `--split=<khác>` → lỗi
  parse, exit 2, message rõ ("invalid --split value …, want right or below").
- **FR3** — `lazyexplorer <dir> --split` **và** `lazyexplorer --split <dir>` đều resolve
  `<dir>` thành root.
- **FR4** — Khi split thành công: spawn pane mới chạy `lazyexplorer <abs-root>` (không
  `--split`); process `--split` exit 0; **không** vào alt-screen; **không** init telemetry (D7).
- **FR5** — Detect theo thứ tự D4; multiplexer thắng emulator khi cả hai env var cùng có.
- **FR6** — Map hướng đúng mỗi terminal: `right` = pane bên phải (vertical divider),
  `below` = pane bên dưới (horizontal divider).
- **FR7** — Không detect được env hợp lệ → in warning stderr + chạy TUI bình thường ở pane
  hiện tại (exit code như chạy thường) (D8).
- **FR8** — Detect được nhưng lệnh spawn lỗi → in warning stderr **kèm lý do lỗi** + chạy TUI
  bình thường (D9).
- **FR9** — Pane gọi **không** bị đóng/đụng trong mọi nhánh (D10).
- **FR10** — `--help` liệt kê `--split[=right|below]`.
- **FR11** — Ghostty/iTerm2 chỉ chạy trên macOS (`runtime.GOOS == "darwin"`); ngoài macOS →
  coi như spawn lỗi → degrade theo FR8.
- **FR12** — `showVersion`/`showHelp` short-circuit **thắng** nhánh split: `lazyexplorer
  --split --help` in help rồi exit 0, **không** spawn (precedence explicit, theo thứ tự §5.1).

## 5. Technical design

**Kim chỉ nam:** `--split` là **launcher-mode thuần** — detect → spawn → exit, không bao giờ
chạm Bubbletea. Toàn bộ logic split đóng gói trong `split.go`; `main.go` chỉ thêm một nhánh sớm.

### 5.1 Arg parsing (`main.go`)

Tách `parseArgs([]string) (cliArgs, error)` — hàm **pure**, test được không cần I/O.

```go
type cliArgs struct {
    start       string // dir để explore (default ".")
    showVersion bool
    showHelp    bool
    split       bool
    splitDir    string // "right" | "below" (default "right")
}
```

Thứ tự trong `main()` (thay khối `main.go:41-95` hiện tại):

1. `parseArgs(os.Args[1:])` → lỗi → stderr + `os.Exit(2)`.
2. `showVersion` / `showHelp` short-circuit (giữ logic in như `main.go:47-60`).
3. Resolve + validate root (`filepath.Abs` + `os.Stat` IsDir, như `main.go:64-72`).
4. **Nếu `split`**: gọi `spawnSplit(splitDir, root)`.
   - Thành công → `return` ngay (**TRƯỚC** `InitTelemetry()` — D7). Pane gọi nguyên vẹn.
   - Lỗi → `fmt.Fprintln(os.Stderr, "lazyexplorer --split:", err)` rồi **fall through** (D8/D9).
5. `InitTelemetry()` + chạy TUI bình thường (khối `main.go:78-99`).

Nhánh fall-through (4 lỗi) đi tiếp vào bước 5 → đó là session thực, init telemetry là đúng.

### 5.2 Registry (`split.go` — file mới)

```go
type splitEnv struct {
    name     string
    detected func() bool
    buildCmd func(direction, root, self string) (*exec.Cmd, error)
}
```

`var splitEnvs = []splitEnv{ tmux, zellij, wezterm, kitty, ghostty, iterm2 }` —
**append-only, thứ tự là invariant** (D4: multiplexer trước emulator), comment giải thích
ngay tại slice (mirror style `fs.go:226-229`).

- `detectSplitEnv() *splitEnv` — trả env đầu tiên có `detected()==true`, nil nếu không có.
- `spawnSplit(direction, root string) error`:
  1. `self, err := os.Executable()` — lỗi → wrap "cannot resolve own path".
  2. `env := detectSplitEnv()` — nil → `error` "no supported terminal detected
     (tmux/zellij/wezterm/kitty/ghostty/iterm2)".
  3. `cmd, err := env.buildCmd(direction, root, self)` → lỗi (vd non-darwin cho ghostty/iterm).
  4. `runSpawn(cmd)` — chạy, gom `cmd.Stderr` vào wrapped error để FR8 hiển thị lý do.

`buildCmd` **trả `*exec.Cmd` chứ không tự chạy** → test assert `cmd.Args`/script string mà
không exec terminal thật (xem §6).

### 5.3 Mechanism mỗi terminal

Flag user-facing chỉ có `right`/`below`; mỗi `buildCmd` tự map sang từ vựng riêng của terminal.

| Terminal | detect (env) | right | below |
|---|---|---|---|
| tmux | `$TMUX` | `tmux split-window -h -c <root> <self> <root>` | `-v` thay `-h` |
| zellij | `$ZELLIJ` | `zellij action new-pane --direction right --cwd <root> -- <self> <root>` | `down` thay `right` |
| wezterm | `$WEZTERM_PANE` ‖ `$TERM_PROGRAM=WezTerm` | `wezterm cli split-pane --right --cwd <root> -- <self> <root>` | `--bottom` thay `--right` |
| kitty | `$KITTY_WINDOW_ID` | `kitty @ launch --type=window --location=vsplit --cwd <root> <self> <root>` | `hsplit` thay `vsplit` |
| ghostty (darwin) | `$GHOSTTY_RESOURCES_DIR` ‖ `$TERM_PROGRAM=ghostty` | osascript: activate Ghostty → `keystroke "d" using command down` → delay → type `<self> <root>` → Return | `keystroke "d" using {command down, shift down}` |
| iterm2 (darwin) | `$TERM_PROGRAM=iTerm.app` | osascript: `split vertically with same profile` → `write text "<self> <root>"` | `split horizontally with same profile` |

> **Trạng thái verify cú pháp (2026-05-28):**
> - **tmux — ĐÃ VERIFY ✅**: `tmux split-window` usage liệt kê `[-bdefhIPvZ] [-c start-directory]
>   … [shell-command [argument ...]]` → `-h` (side-by-side), `-v` (top/bottom), `-c <root>`, rồi
>   `self root` là argv khớp `buildTmux`.
> - **zellij · wezterm · kitty — doc-based**: các CLI không cài trên máy build nên cú pháp lấy
>   từ tài liệu công cụ, chưa run-verified local. Falsify khi có môi trường: `wezterm cli
>   split-pane --help` · `kitty @ launch --help` · `zellij action new-pane --help`.
> - **Ghostty · iTerm2 — doc-based (macOS)**: Ghostty default keybind `cmd+d`/`cmd+shift+d`
>   (Settings → Keybind); iTerm2 scripting dictionary `split vertically/horizontally with same
>   profile`. Chạy tay xác nhận trên macOS thật.
>
> Nếu một flag sai: D8/D9 degrade + `runSpawn` gom stderr nêu lý do → sửa một dòng trong
> `buildCmd` tương ứng, không ảnh hưởng phần còn lại.

**Argv-vs-shell (tmux/zellij/wezterm):** truyền `self` và `root` là **hai argv tách biệt**
(không nối chuỗi) → terminal exec trực tiếp, path có space vẫn an toàn, không cần shell-quote.

**Ghostty fragility (ghi rõ trong code + ADR):** keystroke giả định (a) user **chưa remap**
default keybind `cmd+d`/`cmd+shift+d`; (b) macOS **Accessibility permission** đã cấp cho
process cha (terminal/Claude Code). Cả hai fail **thầm** → message lỗi của `buildCmd`/`runSpawn`
phải nêu **cả hai** gợi ý, đừng để stderr chỉ có "osascript exit 1".

**AppleScript escaping:** chạy qua `exec.Command("osascript", "-e", script)` (không qua shell),
nên chỉ cần escape `"`/`\` ở mức AppleScript string literal khi nhúng `<self>`/`<root>`.

### 5.4 Đã cân nhắc & defer khỏi v1

- **Tắt pane gọi** (D10 = không) — defer; nguy hiểm khi pane đang chạy agent.
- **Chọn size pane** (`-l` / `--percent` / `-l size`) — defer; dùng mặc định 50/50 của terminal.
- **Ghostty trên Linux** — defer; chưa có CLI split lẫn AppleScript tương đương (GTK action
  chưa scriptable từ ngoài). Linux Ghostty rơi vào FR11 → degrade.
- **Detect bằng process-tree** thay vì env var — defer; env var đủ và đơn giản hơn nhiều.
- **Tự bật remote-control cho kitty/wezterm** — non-goal (config của user).
- **WezTerm/Kitty/Zellij khi nằm *trong* nhau hoặc trong tmux** — D4 đã xử qua thứ tự ưu tiên;
  không thêm logic phát hiện nested phức tạp.

## 6. Acceptance criteria

### Gherkin (hành vi)

```gherkin
Feature: Respawn lazyexplorer vào split pane theo terminal

  Scenario: Split mặc định bên phải trong multiplexer
    Given tôi đang ở trong một tmux session
    When tôi chạy lazyexplorer với cờ split
    Then một pane mới mở bên phải chạy lazyexplorer ở cùng thư mục gốc
    And pane gọi vẫn nguyên, không bị đóng

  Scenario: Split xuống dưới
    Given tôi đang ở trong một terminal được hỗ trợ
    When tôi chạy lazyexplorer với cờ split hướng dưới
    Then pane mới mở bên dưới chạy lazyexplorer

  Scenario: Multiplexer thắng emulator khi lồng nhau
    Given tôi chạy tmux bên trong Ghostty
    When tôi chạy lazyexplorer với cờ split
    Then pane mới là một tmux pane trong cùng session
    And số window Ghostty không thay đổi

  Scenario: Terminal không được hỗ trợ
    Given tôi đang ở một terminal không nhận dạng được
    When tôi chạy lazyexplorer với cờ split
    Then tôi thấy cảnh báo là không split được
    And lazyexplorer vẫn mở bình thường ở pane hiện tại

  Scenario: Terminal hỗ trợ nhưng lệnh split lỗi
    Given tôi đang ở một terminal được hỗ trợ nhưng remote-control bị tắt
    When tôi chạy lazyexplorer với cờ split
    Then tôi thấy cảnh báo nêu rõ lý do split lỗi
    And lazyexplorer vẫn mở bình thường ở pane hiện tại

  Scenario: Hướng split không hợp lệ
    Given bất kỳ terminal nào
    When tôi chạy lazyexplorer với cờ split kèm giá trị hướng sai
    Then chương trình báo lỗi và thoát mà không mở giao diện
```

### Checklist verify

1. `--split` thành công **không** ghi `session.start`/`session.end`: bật `LE_TELEMETRY=1`,
   xác nhận không có phantom event từ nhánh split (D7/FR4).
2. Pane mới chạy `lazyexplorer <root>` **không** kèm `--split` (không đệ quy): kiểm `cmd.Args`
   của builder không chứa `--split` (FR4/D6).
3. `parseArgs` cover: `--split`, `--split=right`, `--split=below`, `--split=bad` (lỗi),
   `--foo` (lỗi), `dir --split`, `--split dir`, hai positional (lỗi) (FR1–FR3, D12).
4a. **argv** đúng cho tmux/zellij/wezterm/kitty (cả hai hướng) — assert `cmd.Args` (FR6).
4b. **script osascript** chứa verb đúng cho ghostty (`keystroke "d"`, biến thể shift) và
    iterm2 (`split vertically`/`horizontally`) — assert script string (FR6).
5. Path có space an toàn — self/root tách argv cho CLI, escape đúng cho AppleScript (§5.3).
6. `go build -o lazyexplorer . && go vet ./... && go test ./...` xanh.

## 7. Task breakdown

- [x] **T1 — parseArgs + wire nhánh split.** `parseArgs` pure (`main.go`); nhánh `--split`
  đặt **trước** `InitTelemetry()` (§5.1, D7); version/help short-circuit thắng split (FR12);
  `--help` liệt kê `--split[=right|below]` (FR10). *(main.go)* ✅
- [x] **T2 — Registry + spawnSplit + 6 buildCmd.** `splitEnvs` registry + invariant-order comment
  (D4) + Ghostty fragility comment (`split.go`); argv tách self/root (CLI); `appleScriptString`
  escape (Ghostty/iTerm2). *(split.go)* ✅
- [x] **T3 — Tests.** `TestParseArgs` (17 case), `TestDetectSplitEnvPriority` (tmux thắng ghostty
  nested qua `t.Setenv`), `TestBuildCLISplitArgs` (tmux/zellij/wezterm/kitty × 2 hướng),
  `TestBuildAppleScriptSplit` (ghostty/iterm2) — không exec terminal thật. *(split_test.go)* ✅
- [x] **Tn — Verify.** `go build -o lazyexplorer . && go vet ./... && go test ./...` xanh
  (ĐÃ VERIFY ✅ 2026-05-28). Bảng §5.3: **tmux verified** (`tmux split-window` usage khớp
  `-h`/`-v`/`-c` + `shell-command [args]`, 2026-05-28); zellij/wezterm/kitty/Ghostty/iTerm2 =
  doc-based, các tool không cài trên máy build — chạy tay xác nhận khi có môi trường thật.

## 8. Files chạm tới

| File | Thay đổi |
|------|----------|
| `main.go` | + `parseArgs` (pure); nhánh `spawnSplit` đặt **trước** `InitTelemetry()` (`main.go:78`); cập nhật `--help` text (`main.go:50-60`) |
| `split.go` | **mới** — `splitEnv` struct + `splitEnvs` registry + `detectSplitEnv` + `spawnSplit` + 6 `buildCmd*` (tmux/zellij/wezterm/kitty/ghostty/iterm2) + `runSpawn` |
| `split_test.go` | **mới** — table tests `parseArgs`, detection priority, argv/script builders |
| `docs/adr-split-respawn.md` | **mới** (sau khi PRD accepted) — quyết định registry + multiplexer-priority invariant + AppleScript fragility (đối ứng `adr-preview-renderer-registry.md`) |

---

Verify gate: `go build -o lazyexplorer . && go vet ./... && go test ./...` xanh (chạy khi
implement, không phải lúc viết PRD này).
