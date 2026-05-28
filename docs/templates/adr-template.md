<!--
TEMPLATE — ADR (Architecture Decision Record). Copy file này → `docs/adr-<slug>.md`.
Xoá mọi dòng comment hướng dẫn (HTML comment) khi điền. Spec đầy đủ: docs/CLAUDE.md §ADR. Ví dụ tốt:
adr-fs-refresh-polling.md, adr-async-markdown-render.md.
ADR ghi MỘT quyết định kiến trúc + đánh đổi. Đây là nơi ĐÚNG để ghi lịch sử "đổi từ A sang B
vì…" (phần Hệ quả / Phương án) — khác spec đang sống (PRD) vốn chỉ mô tả trạng thái đích.
Khi một ADR bị thay thế: đặt status `superseded by adr-<slug>`, KHÔNG xoá file.
-->
# ADR — <tiêu đề ngắn của quyết định>

Status: **accepted** · Author: <ai/phiên nào> · Ngày: <YYYY-MM-DD>
<!-- status: "draft / chờ review" · "accepted" · "superseded by adr-<slug>". -->

---

## Bối cảnh

<!-- Vấn đề + ràng buộc dẫn tới quyết định. Trích code hiện trạng (`file.go:line`). Vì sao
     status quo không đủ. -->

## Quyết định

<!-- Câu chốt: ta chọn gì. Rồi bảng D1, D2… (cùng dạng decision table của PRD). -->

| # | Quyết định | Giá trị |
|---|-----------|---------|
| D1 | <khía cạnh> | <giá trị chốt> |
| D2 | … | … |

### Vì sao <điểm mấu chốt> bắt buộc

<!-- Giải thích cốt lõi: vì sao quyết định mấu chốt nhất KHÔNG thể bỏ. Trích test/code chốt
     invariant nếu có (`TestXxx`). -->

## Các phương án đã cân nhắc

<!-- Mỗi phương án + verdict **bác bỏ**/**chọn** + lý do. So sánh reference clone (tmp/…) nếu
     có — borrow idiom, ghi rõ "không copy". -->

### A. <phương án> — **bác bỏ**

- <lý do bác bỏ>.

### B. <phương án> — **chọn** / **tham khảo, không copy**

- <lý do>.

## Hệ quả

**Tích cực**
- <lợi ích>.

**Đánh đổi / giới hạn hiện tại**
- <chi phí / giới hạn chấp nhận>.

<!-- Phương án mở rộng nếu chạm giới hạn (YAGNI cho tới khi có nhu cầu thật): <…> -->

## Phạm vi thay đổi

<!-- File nào đổi gì + dòng Verify đã pass. -->

- `<file.go>` — <thay đổi>.

Verify: `go build -o lazyexplorer . && go vet ./... && go test ./...` (+ `go test -race ./...` nếu chạm concurrency) đều pass.
