# BUG — footer "nháy" mỗi lần preview (re)render: chip "• rendering…" prepend làm reflow cả status bar

Status: **fixed by prd-preview-render-stability** · Author: bug-report session · Ngày: 2026-05-28

---

## Triệu chứng

Footer (status bar) nháy/giật **một cái** mỗi khi: chuyển cursor sang một file có
renderer (markdown / code / image), preview render xong, hoặc poll refresh re-render.
Trong khoảnh khắc đó toàn bộ chip `[ list ]`/`[ preview ]` + dải hint nhảy sang **phải**
rồi nhảy về **trái**, và phần hint ở đuôi bị **cắt cụt** rồi hiện lại. Mắt đọc thành
"footer bị glitch" chứ không phải "chip vừa di chuyển".

## Repro

Precondition: `modeNormal`, terminal đủ rộng cho layout 2-col (`m.width >= widthBreakpoint`,
`view.go:117`). Footer ở nhánh default của `renderStatus` (`view.go:689`).

1. Mở một thư mục có ít nhất một file renderable, ví dụ `doc.md` (markdown) hoặc `main.go` (code).
2. Đặt cursor lên `doc.md`.
3. Quan sát footer ngay tại thời điểm chuyển vào file: chip + hint dịch phải 13 cột, hint
   đuôi mất; vài ms sau (render land) nó snap về vị trí cũ. Đó là cú "nháy".

Điều kiện kích hoạt chính xác: selection phải khớp một renderer trong registry — chỉ khi đó
`syncPreview` mới dispatch một render và set `pendingWidth > 0` (`model.go:617`), tức điều
kiện hiện chip (`view.go:707`). File plain-text/thư mục/empty **không** kích hoạt (xem *Phạm vi*).

> Trạng thái evidence: **ĐÃ VERIFY ✅ 2026-05-28** — repro ở mức unit qua một throwaway probe
> gọi thẳng `renderStatus()` hai lần (cùng model, chỉ đổi `pendingWidth`) và đo vị trí chip
> sau `ansi.Strip`. Output:
>
> ```
> pendingWidth=0    footer="  list  [↓/j] move down  …  [d] delete  [ctrl+p] commands… "
> pendingWidth=100  footer=" • rendering…  list  [↓/j] move down  …  [d] delete  [ctr…"
> chip dịch phải 13 DISPLAY columns (= bề rộng "• rendering… ")
> trailing-clip = true   (đuôi "[ctrl+p] commands…" bị fitWidth cắt còn "[ctr…")
> ```
>
> Boundary cùng probe: `doc.md` (markdown) → `syncPreview() != nil`, `pendingWidth` lên 72;
> `notes.txt` (plain, không renderer) → `syncPreview() == nil`, `pendingWidth` giữ 0.

## Root cause

Chip "• rendering…" được **prepend** vào chuỗi status, nên khi nó bật/tắt thì **mọi thứ
phía sau nó dịch ngang** — và vì chuỗi dài thêm bị `fitWidth` cắt theo `m.width-2`, đuôi
hint biến mất cùng lúc. Indicator nhằm trấn an "đang format" lại tự trở thành cú giật.

Chuỗi nhân quả (mỗi bước một `file:line`):

1. Một event đổi selection / re-read file gọi `refreshPreview()` → set `m.pendingWidth = 0`
   và `m.srcWidth = 0`, đồng thời stash `srcPath` cho file renderable — `model.go:371`,
   `model.go:370`, `model.go:425`.
2. Đuôi `Update` gọi `syncPreview()`; với file renderable, `srcWidth (0) != w` nên nó
   dispatch render async và set `m.pendingWidth = w` — `model.go:605-617`.
3. View vẽ **trước** khi render land. Nhánh default của `renderStatus` thấy `m.pendingWidth > 0`
   nên **prepend** `renderingStyle.Render("• rendering… ")` vào `status` — `view.go:707-709`.
   Prepend ⇒ chip + hint dịch phải đúng 13 cột (bề rộng hiển thị của tiền tố).
4. Cùng dòng đó, `fitWidth(status, m.width-2)` cắt chuỗi đã dài thêm 13 cột → hint đuôi rụng
   — `view.go:710`.
5. Render land qua `previewRenderedMsg` → `applyPreview()` set `m.pendingWidth = 0` —
   `model.go:641`. Frame kế tiếp `renderStatus` bỏ tiền tố ⇒ chip + hint snap **về trái**,
   đuôi hiện lại.

Hai lần dịch (bật ở bước 3, tắt ở bước 5) xảy ra trong đúng một khoảng render; với renderer
nhanh (file nhỏ) hai lần đó sát nhau nên mắt thấy **một cú nháy**.

Lưu ý: cú nháy này **độc lập** với poll — nó nổ ngay trên **một** lần chuyển file renderable
hợp lệ (không cần file nào khác đổi). Cùng cơ chế prepend còn nổ lặp lại ~mỗi giây khi poll
re-render thừa selected file lúc agent ghi file cạnh — đó là `bug-poll-preview-rerender.md`
(amplifier về **tần suất**, không phải nguồn gốc của cú dịch).

## Phạm vi & impact

| Trigger | `pendingWidth` bật? | Footer nháy? |
|---|---|---|
| Chuyển sang file markdown / code / image | có (render dispatch) | **có** |
| Chuyển sang file plain-text | không (`srcPath==""` → `syncPreview` nil, `model.go:595`) | không |
| Chuyển sang thư mục / empty | không (không renderer) | không |
| Poll refresh khi sibling đổi (xem `bug-poll-preview-rerender.md`) | có, lặp ~1s | **có**, lặp lại |
| `modeSearch` / `modeRename` / `modeConfirmDelete` / palette / help | n/a — các mode này có nhánh `renderStatus` riêng (`view.go:645-688`), không qua tiền tố này | không |

- Chỉ là vấn đề **thị giác** ở footer: không mất state, không sai chức năng. Nhưng đúng kịch
  bản "glance beside agent" (`CLAUDE.md`): mỗi lần liếc sang chuyển file/agent ghi file là một
  cú giật, ăn mòn cảm giác "mượt".
- Không ảnh hưởng layout body (list/preview): geometry trong `layout()` không phụ thuộc
  `pendingWidth`. Cú nháy khu trú hoàn toàn ở status row `m.height-1`.

## Tài liệu cần reconcile khi có fix

> **Đã reconcile ✅ 2026-05-28** kèm `prd-preview-render-stability` — hai doc dưới giờ tả
> indicator là spinner mép phải (không còn chip prepend).

Hai doc dưới mô tả chip như feedback "sạch", **bỏ sót** tác dụng phụ reflow/clip — cần đối
soát ở pass sửa kèm fix (KHÔNG sửa doc accepted tại đây):

- `adr-async-markdown-render.md:31` (D5) — claim chip báo "user thấy nội dung ngay và biết bản
  đẹp đang tới"; đúng về *ý định*, nhưng cách hiện (prepend) khiến chính chip đọc thành glitch
  — nghịch với mục tiêu của D5.
- `prd-markdown-view.md:59-61` — tả "status bar hiện chip … chip biến mất" như chuyển tiếp êm,
  không nêu rằng prepend làm dịch + cắt phần còn lại của footer.

## Defer khỏi bug report này

Cố ý **không** làm ở doc này (chờ review rồi mở doc riêng):

- **Thiết kế fix** → PRD/ADR riêng. Ràng buộc cần giữ: tiền tố là *chủ đích* (`view.go:704-706`
  ghi rõ muốn báo "đang format, không phải lỗi") — fix phải **giữ tín hiệu đó** mà **bỏ
  reflow** (ví dụ slot cố định / chỉ vị trí dành sẵn, chứ không prepend đẩy chuỗi). Chọn
  phương án là quyết định cần review — không nhét vào bug report.
- **Failing test reproducer** → thêm khi implement fix (TDD, theo `CLAUDE.md`): assert vị trí
  chip (sau `ansi.Strip`) **không đổi** giữa `pendingWidth == 0` và `> 0`, và đuôi hint không
  bị cắt thêm. Probe ở mục *Repro* là phác thảo của test này.
