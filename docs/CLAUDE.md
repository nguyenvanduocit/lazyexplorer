# Quy ước viết tài liệu trong `docs/`

File này quy định cách viết mọi doc trong thư mục `docs/` của lazyexplorer: **PRD,
Acceptance Criteria (AC), Gherkin, ADR, Task, Bug report**. Mục tiêu: doc đọc-được-ngay,
trace thẳng về code, và nhất quán giữa các phiên — agent hay người viết đều ra cùng một format.

> Đây là spec hiện hành, không phải nhật ký. Lịch sử "tại sao đổi" thuộc về git / phần
> *Hệ quả* của ADR — không nhét vào thân doc đang sống.

## Nguyên tắc chung (áp cho mọi loại doc)

- **Ngôn ngữ**: Việt-Anh code-switching. Định danh code / thuật ngữ kỹ thuật giữ nguyên
  gốc (`previewPreStyled`, `tea.Tick`, lexer). Giữ đủ dấu tiếng Việt.
- **Citation `file:line`**: mọi tham chiếu code viết trong backtick kèm số dòng —
  `` `fs.go:107` ``, `` `model.go:70` ``. Không nói "trong file fs" chung chung.
- **Evidence over assertion**: claim kỹ thuật phải kèm bằng chứng. Khi đã chạy thử,
  ghi `ĐÃ VERIFY ✅` + **ngày** thực nghiệm (`2026-05-27`) + output cụ thể. Giả định
  chưa kiểm thì gọi thẳng là *giả định* và nêu cách falsify.
- **Reference clone**: khi mượn pattern từ `tmp/<clone>/…`, trích đường dẫn cụ thể và
  ghi rõ **"không copy"** — borrow idiom, không copy code (xem root `CLAUDE.md`).
- **Positive framing**: viết trạng thái đích affirmative. Không để lại "~~A~~ → B" hay
  "đừng dùng A" trong thân spec. Lý do đổi → phần *Hệ quả*/*Phương án đã cân nhắc* của ADR.
- **Defer rõ ràng**: thứ cố ý bỏ khỏi v1 → liệt kê trong mục *"Đã cân nhắc & defer khỏi
  v1"* để reviewer biết là chủ đích, không phải bỏ sót.
- **Verify gate**: doc nào mô tả thay đổi code đều chốt bằng lệnh chuẩn của repo:
  `go build -o lazyexplorer . && go vet ./... && go test ./...`.

## Đặt tên file

| Loại | Mẫu tên | Ví dụ |
|------|---------|-------|
| ADR | `adr-<slug>.md` | `adr-fs-refresh-polling.md` |
| PRD | `prd-<slug>.md` | `prd-consistent-file-listing.md` |
| Task (đứng riêng) | `task-<slug>.md` | `task-mouse-drag.md` |
| Bug report | `bug-<slug>.md` | `bug-poll-preview-rerender.md` |

`<slug>` là kebab-case, ngắn, mô tả tính năng (hoặc bug). AC và Gherkin **không** đứng file riêng
mặc định — chúng sống trong PRD (xem dưới). Tách `.feature` riêng chỉ khi bộ scenario
lớn tới mức làm PRD khó đọc.

> Skeleton sẵn-copy cho từng loại đặt ở `docs/templates/` (`prd-template.md`, `adr-template.md`,
> `bug-template.md`, `task-template.md` + `README.md`). Copy → đổi tên theo bảng trên → xoá comment
> `<!-- … -->` → điền. File này vẫn là source of truth cho quy ước; template chỉ là điểm khởi đầu.

## Header chung (mọi doc)

```markdown
# <TYPE> — <tiêu đề ngắn>     ← vd: "# PRD — …", "# ADR — …"

Status: **<status>** · Author: <ai/phiên nào> · Ngày: <YYYY-MM-DD>

---
```

`status` hợp lệ (PRD / ADR / Task): `draft / chờ review` · `accepted` · `superseded by <ref>`.
Bug report dùng lifecycle riêng — xem mục *Bug report*.

## PRD

Một file gói trọn **bối cảnh → quyết định → thiết kế → AC → task** (house style: PRD +
solution + task chung một file). Section đánh số:

```markdown
## 1. Bối cảnh & vấn đề      ← vì sao cần, ai đau, trích code hiện trạng (file:line)
## 2. Goal (1 câu)            ← + "Non-goal làm rõ:" để chặn scope creep
## 3. Quyết định đã chốt      ← bảng D1, D2… (xem mẫu bảng Quyết định)
## 4. Functional requirements ← FR1, FR2… mỗi FR một hành vi kiểm được
## 5. Technical design        ← kim chỉ nam + sub-section; kèm "Đã cân nhắc & defer khỏi v1"
## 6. Acceptance criteria     ← Gherkin (hành vi) + checklist verify (xem mục AC)
## 7. Task breakdown          ← T1, T2… (xem mục Task)
## 8. Files chạm tới          ← bảng | File | Thay đổi |
```

Bảng **Quyết định** (mục 3) — luôn có cột lý do:

```markdown
| # | Quyết định | Giá trị | Lý do |
|---|-----------|---------|-------|
| D1 | … | … | … |
```

## Acceptance Criteria (AC)

AC sống trong mục 6 của PRD, gồm **hai phần bổ trợ nhau**:

1. **Gherkin** — đặc tả hành vi quan sát được (xem mục Gherkin). Đây là phần chính.
2. **Checklist verify** — list đánh số những thứ kiểm tay/được tự động hoá mà Gherkin
   không tiện diễn đạt (regression, edge số học, perf). **Item cuối luôn là Verify gate**:

```markdown
N. `go build -o lazyexplorer . && go vet ./... && go test ./...` xanh.
```

Mỗi AC phải kiểm được (có thể trả lời pass/fail), không mơ hồ ("chạy mượt" → "không lỗi
index, số dòng = số dòng file").

## Gherkin

Dạng chuẩn cho behavioral AC. Nhúng fenced block ` ```gherkin ` trong PRD mục 6.

Quy tắc (theo skill `aio-gherkin-refine`):

- **Declarative, không imperative**: `When I open the folder "src"` — không phải
  `When I press Enter on row 3`. Mô tả ý định nghiệp vụ, không thao tác UI cụ thể.
- **Một rule / một scenario**: cần `And` nối hai hành động không liên quan → tách scenario.
- **Domain language**: dùng từ miền (list pane, folder preview, jail root), không phải
  tên hàm/biến.
- **Scenario độc lập**: mỗi scenario tự đứng, không share state với scenario khác.
- **Phủ cả happy path lẫn failure/edge**: tối thiểu một scenario thất bại/biên.
- **`Background`** chỉ khi MỌI scenario dùng chung precondition đó.

```gherkin
Feature: <cụm danh từ — năng lực được mô tả>

  Background:                         # chỉ khi mọi scenario dùng chung
    Given <precondition chung>

  Scenario: <câu mô tả rule đang test>
    Given <bối cảnh: ai, trạng thái gì>
    When  <hành động kích hoạt hành vi>
    Then  <kết quả quan sát được>

  Scenario: <case thất bại / thay thế>
    Given <bối cảnh khác>
    When  <hành động>
    Then  <kết quả khác>
```

## ADR

Ghi một quyết định kiến trúc + đánh đổi. Trạng thái thường `accepted`. Cấu trúc:

```markdown
## Bối cảnh                   ← vấn đề + ràng buộc, trích code (file:line)
## Quyết định                 ← bảng D1, D2…; kèm "Vì sao <điểm mấu chốt> bắt buộc"
## Các phương án đã cân nhắc   ← từng phương án + verdict **bác bỏ**/**chọn** + lý do;
                               #   so sánh reference clone (tmp/…) nếu có
## Hệ quả                     ← **Tích cực** / **Đánh đổi / giới hạn** / hướng mở rộng
## Phạm vi thay đổi           ← file nào đổi gì + dòng Verify đã pass
```

ADR là nơi **đúng** để ghi lịch sử "đổi từ A sang B vì…" (phần *Hệ quả* / *Phương án*).
Khi một ADR bị thay thế: đặt status `superseded by adr-<slug>`, không xoá file.

## Bug report

Ghi lại một **bug đã quan sát** + root cause có evidence — *không* design fix. Tách
report khỏi solution để spec-first không bị nhảy cóc: report **mô tả & chẩn đoán** vấn
đề, còn *sửa thế nào* thuộc PRD/ADR mở **sau** khi report được review.

`status` cho bug report (lifecycle riêng, không dùng set PRD/ADR):

- `open / chờ review` — vừa report, chưa chốt hướng xử lý.
- `fixed by <ref>` — đã sửa; `<ref>` trỏ commit / PRD / ADR thực hiện fix.
- `wontfix: <lý do>` — quyết định không sửa, kèm lý do.
- `duplicate of bug-<slug>` — trùng một report khác.

Cấu trúc các section:

```markdown
## Triệu chứng        ← 1–2 câu, plain language, hành vi QUAN SÁT được (không phải lý thuyết)
## Repro              ← precondition + steps cụ thể + điều kiện kích hoạt chính xác
## Root cause         ← chuỗi nhân quả, mỗi bước một file:line. Chưa ra → ghi *giả thuyết* + cách falsify
## Phạm vi & impact   ← ai/cái gì bị ảnh hưởng, mức độ; nêu cả cái KHÔNG bị ảnh hưởng để chặn hiểu lầm
## Tài liệu cần reconcile   ← (optional) doc nào bị bug làm lộ là sai/thiếu — trích file:line; KHÔNG sửa doc accepted tại đây
## Defer khỏi bug report    ← thứ cố ý để lại: thiết kế fix → PRD/ADR riêng; failing-test reproducer → khi implement
```

- **Evidence**: chạy được repro/test rồi → `ĐÃ VERIFY ✅` + **ngày**. Mới trace tĩnh bằng
  đọc code, chưa chạy → nói thẳng *"trace tĩnh, chưa live repro"* và đưa repro vào *Defer*.
  Root cause là **chuỗi `file:line`**, không phải mô tả chung chung.
- **Không Verify gate cuối**: bug report không prescribe code change (đó là việc của PRD/ADR
  fix), nên không chốt bằng dòng `go build … && go test …` như các doc khác.
- Khi fix land: đổi `status` thành `fixed by <ref>`, **không** xoá file — report thành
  lịch sử tra cứu được. Phần *Tài liệu cần reconcile* là to-do cho pass sửa doc kèm fix.

## Task

Task breakdown (PRD mục 7) hoặc file `task-<slug>.md` đứng riêng:

- ID **T1, T2…**, checkbox `- [ ]`, **map 1-1 với file** — đuôi mỗi task ghi file trong
  italic: `*(model.go)*`.
- Mỗi task nêu việc cụ thể + trích section thiết kế liên quan (`§5.4`).
- **Task cuối luôn là Verify**: chạy build/vet/test + kiểm tay các AC.

```markdown
- [ ] **T1 — <việc>.** <mô tả + tham chiếu §>. *(file.go)*
- [ ] **Tn — Verify.** `go build -o lazyexplorer . && go vet ./... && go test ./...` xanh;
  chạy tay kiểm acceptance §6.
```
