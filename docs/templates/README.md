# `docs/templates/` — scaffold cho doc trong `docs/`

Skeleton sẵn sàng copy cho mỗi loại doc của lazyexplorer. Mỗi template encode đúng house style;
**`docs/CLAUDE.md` là source of truth** cho quy ước đầy đủ — template chỉ là điểm khởi đầu để
khỏi dựng cấu trúc từ đầu mỗi lần.

## Cách dùng

1. Copy template tương ứng sang `docs/` với tên theo convention (bảng dưới).
2. Xoá hết comment hướng dẫn `<!-- … -->` khi điền nội dung.
3. Điền theo nguyên tắc xuyên suốt: citation `file:line` trong backtick · evidence-over-assertion
   (claim kèm bằng chứng; đã chạy thì `ĐÃ VERIFY ✅` + ngày) · positive framing (viết trạng thái
   đích, không để lại "~~A~~ → B" trong thân spec) · defer rõ ràng thứ bỏ khỏi v1.

## Loại doc & tên file

| Loại | Template | Đổi tên thành | Khi nào dùng |
|------|----------|---------------|--------------|
| PRD | `prd-template.md` | `prd-<slug>.md` | Feature mới — gói trọn bối cảnh → quyết định → design → AC → task |
| ADR | `adr-template.md` | `adr-<slug>.md` | Một quyết định kiến trúc + đánh đổi; nơi ghi lịch sử "đổi từ A sang B vì…" |
| Bug | `bug-template.md` | `bug-<slug>.md` | Một bug đã quan sát + root cause có evidence; *không* design fix (fix → PRD/ADR riêng) |
| Task | `task-template.md` | `task-<slug>.md` | Task đứng riêng (mặc định task sống trong PRD §7; tách riêng khi không gắn PRD) |

`<slug>` = kebab-case, ngắn, mô tả feature/bug. AC + Gherkin sống trong PRD §6, không file riêng
(trừ khi bộ scenario lớn tới mức làm PRD khó đọc).

> Verify gate chuẩn của repo (mọi doc mô tả thay đổi code chốt bằng dòng này; bug report là ngoại
> lệ — không prescribe code change):
> `go build -o lazyexplorer . && go vet ./... && go test ./...`
