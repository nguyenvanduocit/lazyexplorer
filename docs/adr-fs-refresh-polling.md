# ADR — Phản ánh filesystem change bằng polling (`tea.Tick`) có content-gate

Status: **accepted** · Author: feature-dev session · Ngày: 2026-05-27

---

## Bối cảnh

`reload()` (`model.go:70`) chỉ chạy khi user thao tác — `descend`/`ascend`/`jumpTo`/rename/delete. `Init()` trả `nil` nên **không có gì drive một refresh nền**. Hệ quả: khi một coding agent chạy cạnh lazyexplorer tạo / sửa / xóa file, cây thư mục trên panel **không cập nhật** cho tới khi user tự navigate.

Đây là vấn đề trực diện với positioning của tool (xem `CLAUDE.md` → "vibe-code workflow": liếc nhìn project tree bên cạnh agent đang sửa). Tree đứng yên trong khi agent đang thay đổi file = sai chức năng cốt lõi.

## Quyết định

Dùng **polling qua `tea.Tick`**, chu kỳ **cố định 1 giây**, có **content-gate bằng fnv64 signature**.

| # | Quyết định | Giá trị |
|---|-----------|---------|
| D1 | Cơ chế phát hiện | **Polling**, không fsnotify |
| D2 | Driver | `tea.Tick(1s)` **độc lập, tự reschedule** (`Init → tickCmd`, mỗi `tickMsg` lại `tickCmd`) — `model.go:16-24,166,170-179` |
| D3 | Chu kỳ | **1s cố định** (đủ nhanh cho "glance", đủ rẻ) |
| D4 | Gate | `dirSig([]entry) uint64` (fnv64a fold `name+isDir+size+modTime`) — `fs.go`. Signature trùng → return sớm, **không** re-read preview, **không** re-render markdown |
| D5 | Giữ selection | Theo **name**, không theo index — file thêm/xóa phía trên không làm cursor nhảy (`syncFromDisk`) |
| D6 | Giữ scroll | `previewTop` được giữ rồi clamp lại sau mỗi sync |
| D7 | cwd bị xóa | Tự leo lên ancestor sống gần nhất trong jail root (`recoverVanishedCwd`) |
| D8 | Tạm dừng poll | Bỏ qua khi `modeRename` / `modeConfirmDelete` / `dragging` — tránh giật selection khi user đang gõ/kéo |

### Vì sao gate hai tầng

Gate vận hành ở **hai tầng**, mỗi tầng chặn một loại churn:

1. **Dir-gate (`dirSig`, D4)** — quyết có rebuild **list** không. `dirSig(entries)` fold
   `name+isDir+size+modTime` toàn thư mục; signature trùng → return sớm, biến steady-state
   thành đúng **1 `os.ReadDir` + so hash → return**, không đụng list lẫn preview. Test
   `TestSyncGateNoChange` chốt (`previewTop` không bị reset khi không có thay đổi).

2. **Per-file gate (selected entry)** — quyết có refresh **preview** không.
   `refreshPreview()` (`model.go:334`) reset `srcWidth=0` + đặt placeholder mỗi lần chạy →
   re-render glamour/chroma. Khi dir-gate **đã** mở (một sibling đổi) nhưng entry đang chọn
   không đổi, gọi `refreshPreview` vô điều kiện gây re-render thừa + nháy 1 frame mỗi tick
   lúc agent ghi file cạnh. `syncFromDisk` so old-vs-new selected entry
   (`isDir`+`size`+`modTime`) và chỉ refresh khi nó thật sự đổi (xem
   `prd-fix-poll-preview-rerender`, `bug-poll-preview-rerender`). Test
   `TestSyncSkipsPreviewRefreshWhenSiblingChanges` chốt tầng này.

## Các phương án đã cân nhắc

### A. fsnotify / kernel watch — **bác bỏ**

- Thêm dependency + goroutine lifecycle + bridge channel→`tea.Msg`.
- `cwd` **di chuyển** khi user navigate → phải re-watch mỗi lần đổi thư mục (glow dùng fsnotify nhưng watch *một file cố định*, không gặp vấn đề này).
- Latency dưới giây là thứ "glance beside agent" **không cần**.
- Đi ngược simplicity ethos trong `CLAUDE.md` ("không thêm panel/mode/dep nếu chưa cần").

### B. Cách của superfile — tham khảo, **không copy**

Đọc `tmp/superfile/src/internal/...` (clone reference):

- Cũng **polling, cũng không fsnotify** (không có trong `go.mod`).
- Driver = **message stream**: `Update → updateModelStateAfterMsg → UpdateFilePanelsIfNeeded → UpdateElementsIfNeeded` (`model.go:105,123`; `filepanel/get_elements.go:89`). Heartbeat khi idle là `textinput.Blink` trả từ `Init()`.
- Gate = **time-throttle thích nghi**, KHÔNG phải content fingerprint: hết hạn throttle thì `os.ReadDir`+re-sort+rebuild **vô điều kiện**. Chu kỳ = `min(elemCount/100, 3)`s khi focused, `3s` khi unfocused (`filepanel/consts.go:21-24`, `get_elements.go:78-107`).

Khác biệt cốt lõi và lý do mình chọn hướng riêng:

| | superfile | lazyexplorer (chốt) |
|---|---|---|
| Driver | Ăn theo blink/message stream | `tea.Tick` độc lập → robust ở idle |
| Interval | Thích nghi theo số entry | Cố định 1s |
| Gate | Time-throttle, rebuild vô điều kiện | Content fingerprint, rebuild chỉ khi đổi |
| Re-render preview thừa | Có thể (họ dedup preview ở async cmd riêng, `filemodel/update.go:101`) | Không — gate hai tầng: dir-gate rebuild list, per-file gate (selected entry size+mtime) refresh preview |

Mình ưu tiên content-gate vì preview của lazyexplorer **render markdown inline** (đắt, có scroll state cần giữ); superfile tách preview thành cmd async riêng nên không chịu áp lực đó.

## Hệ quả

**Tích cực**
- Thay đổi hiện ra sau tối đa ~1s, không cần navigate.
- Steady-state cực rẻ nhờ gate; không flicker, không mất scroll/selection.
- Không thêm dependency; driver tự duy trì, không phụ thuộc activity khác.
- cwd bị xóa giữa chừng không làm kẹt UI.

**Đánh đổi / giới hạn hiện tại**
- **Latency ~1s**, không tức thì (chấp nhận được cho use case glance).
- **Idle cost** = 1 `os.ReadDir` + 1 lần duyệt `dirSig`/giây, chạy mãi kể cả khi không đổi. Rẻ với project tree thường; với thư mục **rất lớn** (chục nghìn file) thì 1 readDir/giây bắt đầu đáng kể.
- Interval **cố định**, chưa thích nghi theo kích thước như superfile.

**Phương án mở rộng nếu chạm giới hạn:** mượn ý *adaptive interval theo số entry* của superfile (dir lớn → giãn tick) — chưa làm vì chưa có nhu cầu thực (YAGNI).

## Phạm vi thay đổi

- `fs.go` — thêm `modTime` vào `entry`; thêm `dirSig`.
- `model.go` — `fsSig`; `tickMsg`/`tickCmd`/`pollInterval`; `Init`; case `tickMsg`; `syncFromDisk` + `recoverVanishedCwd`; `reload` set `fsSig`.
- `watch_test.go` — 6 test (add/delete/modify-qua-mtime, selection-by-name, gate no-op, recover cwd).

Verify: `go build` · `go vet ./...` · `go test ./...` · `go test -race ./...` đều pass.
