# ADR — Git indicator: untracked-count cache + muted delta

Status: **accepted** · Author: phiên optimize git-indicator · Ngày: 2026-05-29

---

## Bối cảnh

Git change indicator (`prd-git-change-indicator.md`, shipped 2026-05-29) chạy refresh
**async mỗi poll tick** (`pollInterval=1s`, `model.go:17`), độc lập `dirSig` (D9). Mỗi refresh,
`collectGitState` (`git.go`) thực thi:

1. `git status --porcelain=v1 -z -uall` — drive badge.
2. `git rev-parse --verify -q HEAD` + `git diff … --numstat -z` — line delta tracked.
3. `countUntracked` — với **mỗi** untracked file, đọc tới `maxPreviewBytes=256KB` (`fs.go:224`)
   đếm dòng, cap `maxUntrackedScan=2000` file (`git.go`).

Hai vấn đề user nêu khi dùng cạnh agent:

- **Hiệu suất.** Bước 3 đọc tới `2000 × 256KB` **mỗi giây**, kể cả khi user không tương tác và
  không file untracked nào đổi. Trên repo có nhiều untracked (build output chưa gitignore), đây
  là phần I/O đắt nhất của vòng refresh — lặp lại vô ích mỗi tick.
- **Hiển thị.** Delta diffstat tô `+N` xanh sáng (`colGitNew #3FB950`) / `-N` đỏ sáng
  (`colDanger #DC3545`) — cặp màu bão hoà này kéo mắt mạnh ngang badge, đọc "nặng" trong list
  pane hẹp; badge (loại thay đổi) mới là tín hiệu glance chính.

## Quyết định

| # | Quyết định | Giá trị | Lý do |
|---|-----------|---------|-------|
| A1 | Cache đếm dòng untracked | `untrackedCache map[path]{mtime,size,lines,ok}` thread qua msg/cmd; cache hit (mtime+size khớp) tái dùng count, **không** đọc lại file | Steady-state untracked I/O từ "đọc 256KB/file" về "một `stat`/file". Chỉ file mới/đổi mới đọc → đếm vào read-budget |
| A2 | Race-safety của cache | Goroutine của `tea.Cmd` **chỉ đọc** `prev`, build `next` mới; main loop **chỉ reassign** `m.gitUntrackedCache` lúc apply (gen-gated), không mutate in-place | `gitInFlight` guard đảm bảo chuỗi tuần tự (không có dispatch song song); không cell nào bị hai goroutine chạm → race-free by construction (verify: `go test -race` + grep không có `gitUntrackedCache[`) |
| A3 | Read-budget tính theo **reads thật** | Cache hit **không** tính vào `maxUntrackedScan`; chỉ fresh read mới tính | Repo nhiều untracked: sau lần scan đầu, các tick sau gần như free (toàn hit) → budget dành cho file mới, hội tụ phủ hết qua vài tick |
| A4 | Degrade giữ cache | `git status` fail / không repo → trả `prev` nguyên vẹn cho lần sau | Một blip git không xoá công đã cache (FR10 per-command independence, mở rộng cho cache) |
| A5 | Delta màu **mờ** | `+N`/`-N` render `dimStyle` (`colDim #6C757D`); badge giữ màu trạng thái | Badge là tín hiệu glance chính → giữ màu; delta là phụ → lùi về xám. Dấu `+/-` vẫn phân biệt thêm/bớt nên không mất thông tin (D3 "đọc được khi mất màu" vẫn giữ) |

## Các phương án đã cân nhắc

- **Visible-only (chỉ tính delta/untracked cho hàng đang hiển thị):** **bác**. Tầng git
  (`git.go`) cố ý độc-lập-viewport (PRD kim chỉ nam: "git là tầng dữ liệu độc lập; view chỉ tra
  map"). Folder roll-up `●` cần `git status` **toàn repo** nên status không thể visible-only;
  và tính delta theo viewport làm indicator "nhấp nháy hiện ra" khi scroll. Cache cho cùng cái
  lợi steady-state mà **không** coupling tầng và **không** vỡ roll-up.
- **Status+mtime signature để skip cả `numstat` lúc idle ("Level 2"):** **defer**. Lợi ích biên
  (bỏ một `git diff` rẻ mỗi giây) so với độ phức tạp signature. `git status`/`numstat` mỗi giây
  là giá của "glance tươi ~1s" (FR7), đã rẻ sau khi cache cắt phần đọc file.
- **`core.untrackedCache=true` (git tự cache untracked walk):** **bác**. Mutate git config của
  user — xâm lấn, ngoài phạm vi một file explorer read-only.
- **Tăng poll cadence (vd 2s) để giảm idle exec:** **bác**. Đánh đổi độ tươi FR7 lấy lợi ích nhỏ;
  cache đã giải quyết phần nặng (I/O).
- **Delta xanh/đỏ desaturated (giữ cue màu, chỉ nhạt hơn):** **bác** cho v1. Xám hẳn cho "nhẹ"
  tối đa; badge đã encode loại thay đổi nên cue màu trên delta là thừa. Để mở nếu user muốn lại.
- **Nén delta thành churn một số / delta chỉ ở hàng active (giảm **cột**):** **defer**. User chọn
  giữ nguyên cột, chỉ giảm độ đậm. Giữ sẵn nếu dim chưa đủ "gọn".

## Hệ quả

**Tích cực:**
- Untracked scan steady-state: `stat` thay vì đọc 256KB/file → idle I/O sụp gần như bằng 0 khi
  không file nào đổi. Repo build-output-untracked không còn đọc hàng nghìn file mỗi giây.
- Delta lùi về xám, badge màu dẫn glance — list pane đọc nhẹ hơn, đúng "companion cạnh agent".
- Cache race-free by construction, không thêm lock; tái dùng đúng pattern gen-counter async sẵn có.

**Đánh đổi / giới hạn:**
- **`git status -uall` vẫn là idle floor.** Cache cắt phần *đọc nội dung* file, nhưng `-uall`
  buộc git **walk + stat mọi untracked file mỗi tick** — phần này cache không đụng tới. Trên repo
  gitignore hỏng (nhiều untracked), git walk vẫn là cost mỗi giây. Đây là giá của badge+roll-up
  đúng cho file mới; chấp nhận, không "idle solved".
- Cache key `(mtime,size)`: một sửa-đổi giữ-nguyên-size về lý thuyết trượt, nhưng đổi nội dung
  hầu như luôn đổi size, và save luôn bump mtime (APFS ns) → edge không thực tế, chấp nhận.
- Delta mất cue màu xanh/đỏ — magnitude đọc qua dấu `+/-` + số; loại thay đổi đọc qua badge.

**Hướng mở rộng:** nếu repo khổng lồ vẫn nặng vì git walk → cân nhắc Level-2 signature hoặc cap
số untracked stat; nếu dim chưa đủ gọn → nén cột (churn / active-only) đã thiết kế sẵn ở trên.

## Phạm vi thay đổi

| File | Thay đổi |
|------|----------|
| `git.go` | `+ untrackedStat`/`untrackedCache`; `collectGitState(repoRoot, prev) (gitState, untrackedCache)`; `countUntracked` nhận `prev` + trả `next`, cache hit skip read, read-budget theo reads thật |
| `git_test.go` | `+ TestCountUntrackedCacheHitSkipsReread` (hit tái dùng count sai-cố-ý → proof không re-read), `+ TestCountUntrackedCacheMissRereads` (stale mtime → re-read); cập nhật call sites `collectGitState(root, nil)` |
| `model.go` | `+ gitUntrackedCache` field; `gitRefreshedMsg.cache`; `gitRefreshCmd(…, prev)`; dispatch pass cache; apply reassign cache cùng gen-gate với `m.git` |
| `view.go` | `fullStyled` delta → `dimStyle`; gỡ `styleDelta` |
| `theme.go` | gỡ `gitAddStyle`/`gitDelStyle` (delta không còn xanh/đỏ); cập nhật comment màu |
| `entryrow_test.go` / `model_git_test.go` | assert delta `dimStyle` thay green/red; active-plain assert không có `dimStyle` SGR |
| `prd-git-change-indicator.md` | reconcile D12 (delta mờ), `+` D13 cache; positive framing |

Verify: `go build -o lazyexplorer . && go vet ./... && go test ./... -race` xanh (2026-05-29);
visual verdict trên render thật (delta xám, badge màu) — pass.
