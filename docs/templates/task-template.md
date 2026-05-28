<!--
TEMPLATE — Task breakdown đứng riêng. Copy file này → `docs/task-<slug>.md`.
Xoá mọi dòng comment hướng dẫn (HTML comment) khi điền. Spec đầy đủ: docs/CLAUDE.md §Task.
LƯU Ý: mặc định task sống trong PRD §7 (house style: PRD + solution + task chung một file).
Chỉ tách `task-<slug>.md` riêng khi KHÔNG gắn với một PRD (vd chore/refactor đứng một mình)
hoặc khi bộ task lớn tới mức làm PRD khó đọc.
Quy tắc: ID T1, T2…; checkbox; map 1-1 với file (đuôi mỗi task italic *(file.go)*); mỗi task
trích §design liên quan nếu có. Task cuối LUÔN là Verify.
-->
# TASK — <tiêu đề ngắn>

Status: **draft / chờ review** · Author: <ai/phiên nào> · Ngày: <YYYY-MM-DD>
<!-- status: "draft / chờ review" · "accepted" · "superseded by <ref>". -->

---

<!-- (optional) 1–2 câu bối cảnh: bộ task này phục vụ việc gì, link PRD/ADR/bug liên quan. -->

- [ ] **T1 — <việc>.** <mô tả cụ thể + tham chiếu §/doc nếu có>. *(file.go)*
- [ ] **T2 — <việc>.** <…>. *(file.go)*
- [ ] **Tn — Verify.** `go build -o lazyexplorer . && go vet ./... && go test ./...` xanh;
  chạy tay kiểm acceptance liên quan.
