<!--
TEMPLATE — PRD. Copy file này → `docs/prd-<slug>.md` (slug kebab-case, ngắn, mô tả feature).
Xoá mọi dòng comment hướng dẫn (HTML comment) khi điền. Spec đầy đủ: docs/CLAUDE.md §PRD. Ví dụ tốt: prd-pane-focus.md.
Nguyên tắc xuyên suốt: citation `file:line` trong backtick · evidence-over-assertion (claim kèm
bằng chứng; đã chạy → `ĐÃ VERIFY ✅` + ngày) · positive framing (viết trạng thái đích, không
"~~A~~ → B") · defer rõ ràng. PRD gói trọn: bối cảnh → quyết định → design → AC → task.
-->
# PRD — <tiêu đề ngắn>

Status: **draft / chờ review** · Author: <ai/phiên nào> · Ngày: <YYYY-MM-DD>
<!-- status hợp lệ: "draft / chờ review" · "accepted" · "superseded by prd-<slug>".
     Khi ship: thêm "· Shipped: <YYYY-MM-DD>". -->

---

## 1. Bối cảnh & vấn đề

<!-- Vì sao cần feature này, ai đau, đau thế nào. Trích code hiện trạng (`file.go:line`) để
     neo vấn đề vào thực tế, không nói chung chung. Nếu mượn pattern từ tmp/<clone>/… → trích
     đường dẫn cụ thể + ghi rõ "không copy". -->

## 2. Goal (1 câu)

<!-- Một câu, plain language: feature làm gì cho user. -->

**Non-goal làm rõ:**
<!-- Chặn scope creep: liệt kê thứ feature này CỐ Ý không làm (đặc biệt thứ dễ bị hiểu nhầm là
     có trong scope). Mỗi dòng một non-goal + lý do ngắn. -->
- KHÔNG <…> — <lý do>.

## 3. Quyết định đã chốt

<!-- Mỗi quyết định thiết kế quan trọng = một hàng. Cột "Lý do" BẮT BUỘC — quyết định không
     có lý do là quyết định chưa chín. -->

| # | Quyết định | Giá trị | Lý do |
|---|-----------|---------|-------|
| D1 | <vấn đề quyết> | <giá trị chốt> | <vì sao chọn giá trị này> |
| D2 | … | … | … |

## 4. Functional requirements

<!-- Mỗi FR = một hành vi quan sát/kiểm được. Đánh số FR1, FR2… để AC + task trace về được. -->

- **FR1** — <hành vi cụ thể, kiểm được>.
- **FR2** — <…>.

## 5. Technical design

> Kim chỉ nam: <một câu tóm tắt cách tiếp cận — vd "feature là sub-state nhỏ, không phá cấu trúc">.

### 5.1 <sub-section>

<!-- Chia design theo concern. Kèm sketch code trước/sau khi hữu ích. Trích `file.go:line`. -->

### 5.x Đã cân nhắc & **defer khỏi v1**

<!-- Thứ CỐ Ý bỏ khỏi v1 (để reviewer biết là chủ đích, không phải bỏ sót). Mỗi mục + lý do
     defer. Phương án bị bác bỏ + lý do bác bỏ cũng để ở đây. -->
- **<thứ defer>**: <lý do để lại sau / bác bỏ>.

## 6. Acceptance criteria

### Gherkin

<!-- Đặc tả hành vi quan sát được. Declarative không imperative ("When I open the folder",
     không "When I press Enter on row 3"). Một rule/scenario. Domain language. Scenario độc lập.
     Phủ cả happy path lẫn failure/edge. Quy tắc đầy đủ: docs/CLAUDE.md §Gherkin. -->

```gherkin
Feature: <cụm danh từ — năng lực được mô tả>

  Background:
    Given <precondition chung — chỉ khi MỌI scenario dùng chung>

  Scenario: <câu mô tả rule đang test>
    Given <bối cảnh: ai, trạng thái gì>
    When  <hành động kích hoạt hành vi>
    Then  <kết quả quan sát được>

  Scenario: <case thất bại / biên>
    Given <bối cảnh khác>
    When  <hành động>
    Then  <kết quả khác>
```

### Checklist verify

<!-- Những thứ kiểm tay / tự-động-hoá mà Gherkin không tiện diễn đạt (regression, edge số học,
     perf). Mỗi item pass/fail rõ ràng. Đã chạy → `ĐÃ VERIFY ✅ <YYYY-MM-DD>`. -->

1. <kiểm tra cụ thể>.
2. <…>.
N. `go build -o lazyexplorer . && go vet ./... && go test ./...` xanh.

## 7. Task breakdown

<!-- ID T1, T2…; checkbox; map 1-1 với file (đuôi mỗi task italic *(file.go)*); trích §design
     liên quan. Task cuối LUÔN là Verify. -->

- [ ] **T1 — <việc>.** <mô tả + tham chiếu §5.x>. *(file.go)*
- [ ] **T2 — <việc>.** <…>. *(file.go)*
- [ ] **Tn — Verify.** `go build -o lazyexplorer . && go vet ./... && go test ./...` xanh;
  chạy tay kiểm acceptance §6.

## 8. Files chạm tới

| File | Thay đổi |
|------|----------|
| `<file.go>` | <thay đổi gì> |
| `docs/prd-<slug>.md` | File này |
