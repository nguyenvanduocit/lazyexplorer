<!--
TEMPLATE — Bug report. Copy file này → `docs/bug-<slug>.md`.
Xoá mọi dòng comment hướng dẫn (HTML comment) khi điền. Spec đầy đủ: docs/CLAUDE.md §Bug report. Ví dụ tốt:
bug-poll-preview-rerender.md, bug-footer-flicker.md.
Bug report ghi MỘT bug đã quan sát + root cause có evidence — *không* design fix. Tách report
khỏi solution để spec-first không nhảy cóc: report mô tả & chẩn đoán; *sửa thế nào* thuộc
PRD/ADR mở SAU khi report được review. KHÔNG có dòng Verify gate cuối (report không prescribe
code change). Khi fix land: đổi status → `fixed by <ref>`, KHÔNG xoá file.
-->
# BUG — <triệu chứng ngắn>

Status: **open / chờ review** · Author: <ai/phiên nào> · Ngày: <YYYY-MM-DD>
<!-- lifecycle bug (KHÔNG dùng set PRD/ADR):
     "open / chờ review" · "fixed by <ref>" (commit/PRD/ADR) · "wontfix: <lý do>" ·
     "duplicate of bug-<slug>". -->

---

## Triệu chứng

<!-- 1–2 câu, plain language, hành vi QUAN SÁT được (không phải lý thuyết). Cái gì sai dưới
     mắt user. -->

## Repro

<!-- Precondition + steps cụ thể + điều kiện kích hoạt CHÍNH XÁC (cái gì bật/tắt bug). -->

1. <bước>.
2. <bước>.
3. <kết quả sai quan sát được>.

<!-- Trạng thái evidence: đã chạy live repro → "ĐÃ VERIFY ✅ <YYYY-MM-DD>" + output. Mới trace
     tĩnh bằng đọc code → nói thẳng "trace tĩnh, chưa live repro" và đưa repro vào Defer. -->

## Root cause

<!-- Chuỗi nhân quả, MỖI bước một `file.go:line`. Chưa ra root cause → ghi *giả thuyết* + cách
     falsify, đừng đoán bừa. -->

1. <bước nhân quả> — `file.go:line`.
2. <…> — `file.go:line`.

## Phạm vi & impact

<!-- Ai/cái gì bị ảnh hưởng, mức độ. Nêu CẢ cái KHÔNG bị ảnh hưởng để chặn hiểu lầm. Tần suất
     kích hoạt trong use case thật. -->

## Tài liệu cần reconcile khi có fix

<!-- (optional) Doc nào bị bug làm lộ là sai/thiếu — trích `file:line`. KHÔNG sửa doc accepted
     tại đây; đây là to-do cho pass sửa doc KÈM fix. -->

- `<doc.md>:line` — <chỗ sai/thiếu>.

## Defer khỏi bug report này

<!-- Thứ CỐ Ý để lại: thiết kế fix → PRD/ADR riêng; failing-test reproducer → khi implement. -->

- **Thiết kế fix** → PRD/ADR riêng (chọn phương án là quyết định cần review).
- **Failing test reproducer** → thêm khi fix (TDD).
