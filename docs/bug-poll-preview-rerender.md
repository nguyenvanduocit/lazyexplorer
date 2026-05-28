# BUG — poll loop re-render preview của file đang mở khi file *khác* trong folder đổi

Status: **fixed by prd-fix-poll-preview-rerender** · Author: bug-report session · Ngày: 2026-05-28

---

## Triệu chứng

Khi một file trong cwd thay đổi (agent tạo/sửa/xóa file bên cạnh), poll loop re-read
và **re-render lại preview của file đang mở** — kể cả khi nội dung file đó **không
đổi một byte**. Với markdown/code, glamour/chroma chạy lại mỗi lần có thay đổi trong
folder, gây tốn CPU và **nháy 1 frame** (markdown flash về raw source, code flash về
uncolored, rồi mới render lại) mỗi ~1s trong lúc agent đang ghi file cạnh.

Đây nghịch với chính positioning "glance beside agent" (`CLAUDE.md`): use case điển
hình là agent **liên tục** ghi file trong project tree → mỗi lần ghi một sibling là
một lần preview bị churn vô ích.

## Repro

Precondition: lazyexplorer ở `modeNormal`, không drag divider (poll chỉ chạy khi
`m.mode == modeNormal && !m.dragging` — `model.go:705`).

1. Mở một thư mục có ≥2 file, ví dụ `bar.md` (markdown) và `foo.go`.
2. Đặt cursor lên `bar.md` → preview hiển thị markdown đã render bằng glamour.
3. Ở terminal khác, `touch foo.go` (hoặc agent ghi vào `foo.go`) — **không** chạm `bar.md`.
4. Trong ≤1s: preview của `bar.md` nháy về raw markdown source rồi render lại — dù
   `bar.md` không đổi.

Điều kiện kích hoạt chính xác: sibling phải nằm **cùng cwd** đang xem (poll chỉ watch
cwd, không đệ quy — xem `adr-fs-refresh-polling.md`). `dirSig` đổi qua `size` **hoặc**
`modTime` của bất kỳ entry nào (`fs.go:79-82`), nên cả tạo/xóa sibling lẫn bump mtime
đều trigger.

> Trạng thái evidence: **đã trace bằng đọc code (static)** ngày 2026-05-28 — chuỗi
> `file:line` dưới đây khép kín. Chưa chạy live repro / failing test (xem *Defer*).

## Root cause

Change-detection gate ở **mức directory**, nhưng preview phụ thuộc nội dung **một
file đơn**. Hai khái niệm "directory listing đổi" và "selected file đổi" bị gộp làm một,
nên sibling đổi cũng kéo theo re-render selected file.

Chuỗi nhân quả (mỗi bước có evidence):

1. `tickMsg` mỗi 1s → `syncFromDisk()` — `model.go:705-706`.
2. `dirSig(entries)` fold `name+isDir+size+modTime` của **toàn bộ** entries thành một
   hash — granularity là cả thư mục, không phải từng file — `fs.go:69-85`.
3. Gate: `if sig == m.fsSig { return }` chỉ return sớm khi **toàn bộ listing** không
   đổi — `model.go:281-285`. Sibling đổi ⇒ `sig != m.fsSig` ⇒ gate **không** chặn.
4. `m.refreshPreview()` được gọi **vô điều kiện**, key theo cursor — không kiểm tra
   selected file có đổi hay không — `model.go:312`.
5. `refreshPreview` reset sạch preview state: `srcWidth=0`, `pendingWidth=0`,
   `preview=nil`, `previewPreStyled=false`, `srcPath=""`, `srcRaw=nil` —
   `model.go:365-386` — rồi re-read selected file (`readPreviewBytes`, `model.go:418`)
   và set lại placeholder + `srcPath`/`srcRaw` (`model.go:424-435`).
6. Tail `syncPreview()`: kiểm cache-hit `if m.srcWidth == w { return nil }` —
   `model.go:605`. Vì bước 5 vừa reset `srcWidth=0`, điều kiện sai ⇒ **re-dispatch**
   render async (glamour/chroma chạy lại) — `model.go:616-628`.

Nguồn nháy: bước 5 đặt placeholder (markdown/code → `plainLines(content)` raw source,
`model.go:434`; image → `"(rendering …)"`, `model.go:431`) **trước** khi render async
land qua `previewRenderedMsg`. View vẽ ít nhất một frame với placeholder ⇒ flash.

## Phạm vi & impact

| Loại preview đang mở | Re-render thừa | Impact |
|---|---|---|
| markdown | glamour chạy lại | nháy raw-source → rendered + CPU |
| code | chroma highlight lại | nháy uncolored → colored + CPU |
| image | "(rendering …)" lại | nháy placeholder + re-decode metadata |
| plain text | `plainLines` lại | nhẹ (không renderer nặng) |
| dir / empty | rebuild `previewEntries` | nhẹ |

- **Scroll KHÔNG mất**: `syncFromDisk` lưu `prevTop` (`model.go:292`) và khôi phục sau
  `refreshPreview` (`model.go:313`). Impact thực = **CPU churn + flicker**, không phải
  mất state.
- **Tần suất**: trong vibe-code workflow, agent ghi file cạnh là chuyện thường xuyên →
  bug kích hoạt gần như liên tục, đúng kịch bản tool được thiết kế để phục vụ.
- Khi selected file **chính nó** đổi thì re-render là **đúng và mong muốn** — bug chỉ
  là phần re-render khi selected file **không** đổi mà sibling đổi.

## Tài liệu cần reconcile khi có fix

> **Đã reconcile** trong `prd-fix-poll-preview-rerender` (fix land 2026-05-28): cả hai chỗ
> dưới đã cập nhật sang mô tả **gate hai tầng** (dir-gate rebuild list + per-file gate
> refresh preview). Danh sách giữ lại làm bản ghi những gì đã đối soát.

`adr-fs-refresh-polling.md` đang **accepted** — không sửa ngay (đổi quyết định kiến trúc
là việc riêng), nhưng khi fix land thì hai chỗ này sai/thiếu cần đối soát:

- `adr-fs-refresh-polling.md:56` — bảng so sánh với superfile, cột *"Re-render preview
  thừa"* ghi *"Không — gate chặn"*. Claim này chỉ đúng ở case whole-directory unchanged;
  nó **bỏ sót** case "dir đổi nhưng selected file không đổi" — chính là chỗ bug sống.
- `adr-fs-refresh-polling.md:28-30` (mục *"Vì sao content-gate (D4) là bắt buộc"*) —
  lập luận gate bảo vệ glamour render khỏi chạy mỗi giây; đúng cho steady-state, nhưng
  không phủ trường hợp sibling churn.

## Defer khỏi bug report này

Cố ý **không** làm trong doc này (chờ review rồi mở doc riêng):

- **Thiết kế fix** → PRD/ADR riêng. Có nhiều phương án (per-file content gate trước
  `refreshPreview` trong poll path; so mtime/hash của selected file ở `syncPreview`
  trước khi re-dispatch; tách preview dedup khỏi dir-gate). Chọn phương án là quyết định
  cần review — không nhét vào bug report.
- **Failing test reproducer** → thêm khi fix (TDD, theo `CLAUDE.md` project): touch một
  sibling rồi assert `renderGen` của selected file's preview **không** tăng. Chưa viết
  bây giờ vì sẽ là bước implement đầu tiên của fix.
