# ADR — `--split` respawn vào split pane theo terminal

Status: **accepted** · Author: opus 4.7 session · Ngày: 2026-05-28

---

## Bối cảnh

lazyexplorer sống cạnh agent trong một pane terminal (`CLAUDE.md` §"Goal & Positioning").
Để có layout đó, user phải tự tay split terminal → focus pane mới → gõ `lazyexplorer` mỗi
session, và mỗi terminal split một kiểu (tmux `split-window`, Ghostty `cmd+d`, wezterm CLI…).
Entry point cũ (`main.go`) chỉ nhận một positional `DIR` + `--version`/`--help` rồi vào thẳng
Bubbletea — không có đường nào để lazyexplorer tự đặt mình vào split pane.

Ràng buộc định hình thiết kế:

- **Sáu terminal khác nhau** (tmux · zellij · WezTerm · Kitty · Ghostty · iTerm2), hai họ:
  multiplexer (chia pane logic) và emulator (chia window OS-level). Cú pháp/cơ chế mỗi cái khác.
- **Không bao giờ để user tay trắng** — `--split` là tiện ích, fail của nó không được chặn việc
  mở lazyexplorer bình thường.
- **Không phantom telemetry** — `--split` spawn-rồi-thoát không phải một session thực; nếu init
  telemetry thì sẽ ghi `session.start`/`session.end` rỗng (~ms).

## Quyết định

| # | Quyết định | Vì sao mấu chốt |
|---|-----------|-----------------|
| D1 | **Registry `[]splitEnv{name, detected, buildCmd}`** (`split.go:29`), mirror `previewRenderers` (`fs.go`) | Thêm terminal = một entry; nhất quán với pattern dispatch đã có. `buildCmd` trả `*exec.Cmd` **không tự chạy** → unit-test argv/AppleScript mà không exec terminal thật. |
| D2 | **Detect order: multiplexer (tmux, zellij) TRƯỚC emulator** (`split.go:29-36`) | **Invariant load-bearing.** Khi user *trong* tmux/zellij, intent là chia pane multiplexer — không mở window emulator thứ hai. Emulator env var (`GHOSTTY_RESOURCES_DIR`, `WEZTERM_PANE`) có thể vẫn set khi nested → ưu tiên multiplexer là đúng *bất kể* persistence của env var đó (priority-as-safety-net, không lệ thuộc một claim persistence cụ thể). |
| D3 | **Launcher-mode thuần**: `main.go` short-circuit `spawnSplit` → `return` TRƯỚC `InitTelemetry()`; không bao giờ chạm Bubbletea | Spawn-rồi-exit không phải session thực (no phantom telemetry). Logic split đóng gói trong `split.go`; `main.go` chỉ thêm một nhánh sớm. |
| D4 | **Graceful degrade**: không detect được env, hoặc spawn lỗi → warn stderr (kèm lý do) + fall through chạy TUI bình thường ở pane hiện tại | `--split` best-effort. Nhánh fall-through đi tiếp tới `InitTelemetry()` → đó là session thực, init đúng. |
| D5 | **Re-exec qua `os.Executable()` + absolute root**, spawn `lazyexplorer <root>` **không** kèm `--split` | Chạy được cả khi binary ngoài `$PATH`; pane mới không split đệ quy. |
| D6 | **CLI native** cho tmux/zellij/wezterm/kitty; **AppleScript** cho Ghostty (keystroke) + iTerm2 (`write text`) | Ghostty chưa có CLI split → buộc keystroke `cmd+d`/`cmd+shift+d`. iTerm2 có scripting API sạch hơn keystroke. |
| D7 | **`parseArgs` pure + strict**: unknown `-`-flag → lỗi parse + exit 2 | Explicit > implicit; báo lỗi sớm thay vì `os.Stat("--foo")` như thư mục. Test được không cần I/O. |

## Các phương án đã cân nhắc

- **Detect bằng process-tree** thay vì env var — **bác bỏ** (v1): env var đủ và đơn giản hơn
  nhiều; process-tree walk thêm phức tạp không tương xứng.
- **Một Go clipboard/terminal lib** thay vì shell/AppleScript — **bác bỏ**: kéo CGo/X11 deps;
  `exec.Command` native CLI + `osascript` zero-dep, fail-soft, test-friendly.
- **Tự bật remote-control cho kitty/wezterm** — **bác bỏ** (non-goal): đó là config của user;
  nếu off, D4 degrade + message nêu lý do để user tự fix.
- **Tắt pane gọi sau khi spawn** — **bác bỏ** (D-non-goal): use case chính gọi từ pane agent;
  tắt nó = giết agent.
- **Nest detection phức tạp** (wezterm-trong-tmux-trong-ghostty…) — **bác bỏ**: D2 thứ-tự-ưu-tiên
  xử đủ; không thêm logic phát hiện nesting.

## Hệ quả

**Tích cực:**
- Một lệnh `lazyexplorer --split` dựng layout vibe-code từ pane agent, không thao tác tay.
- Thêm terminal mới = một entry registry + một `buildCmd` + test argv — không đụng `main.go`.
- `buildCmd`-trả-`*exec.Cmd` cho phép test toàn bộ matrix (6 terminal × 2 hướng) không cần
  terminal thật (`split_test.go`: `TestParseArgs`, `TestDetectSplitEnvPriority`,
  `TestBuildCLISplitArgs`, `TestBuildAppleScriptSplit`).

**Đánh đổi / giới hạn:**
- **Ghostty fragile**: keystroke giả định (a) user chưa remap `cmd+d`/`cmd+shift+d`; (b) macOS
  Accessibility permission đã cấp cho process cha. Cả hai fail thầm → `runSpawn` gom stderr +
  message nêu **cả hai** gợi ý. AppleScript path là best-effort, không robust như CLI.
- **Cú pháp CLI của zellij/wezterm/kitty là doc-based, chưa run-verified local** (các tool đó
  không cài trên máy build). tmux đã verify (`tmux split-window` usage khớp `-h`/`-v`/`-c` +
  `shell-command [args]`, 2026-05-28). Nếu một flag sai, D4 degrade + stderr nêu lý do; sửa =
  một dòng trong `buildCmd` tương ứng.
- **Ghostty/iTerm2 chỉ macOS** (`runtime.GOOS == "darwin"`); ngoài macOS → spawn lỗi → D4 degrade.

**Hướng mở rộng:** chọn size pane (`-l`/`--percent`), Ghostty trên Linux (khi có CLI/scriptable
action), tự-detect-và-bật remote-control — tất cả defer (PRD §5.4).

## Phạm vi thay đổi

| File | Thay đổi |
|------|----------|
| `main.go` | `parseArgs` (pure) + `printHelp` (liệt kê `--split`); nhánh `spawnSplit` đặt **trước** `InitTelemetry()`; version/help short-circuit thắng split |
| `split.go` | **mới** — `splitEnv` registry + `detectSplitEnv` + `spawnSplit` + `runSpawn` + 6 `buildCmd` + `shellJoin`/`shellQuote`/`appleScriptString` |
| `split_test.go` | **mới** — `parseArgs` table, detection priority qua `t.Setenv`, argv builders (tmux/zellij/wezterm/kitty), AppleScript builders (ghostty/iterm2) |

Verify gate: `go build -o lazyexplorer . && go vet ./... && go test ./...` xanh
(ĐÃ VERIFY ✅ 2026-05-28).
