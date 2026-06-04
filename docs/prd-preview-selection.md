# PRD — `V`: chọn & copy một dải dòng trong preview (in-app line-visual selection)

Status: **accepted** · Author: preview-selection spec (goal "select and copy content in the file preview") · Ngày: 2026-06-04 · Shipped: 2026-06-04 (✅ `go build -o lazyexplorer . && go vet ./... && go test ./...` + `go test -race ./...` green)

> Amends `prd-preview-copy` (accepted 2026-06-02): bổ sung **in-app selection** mà PRD đó cố ý defer.
> Quan hệ chính xác ở §2 + bảng D2. `prd-preview-copy` KHÔNG bị superseded toàn phần — phím `Y`
> (copy cả file) và native-drag note vẫn nguyên giá trị; chỉ **D1/D2** ("native-drag là đủ cho chọn
> đoạn nhìn thấy") được **thu hẹp phạm vi**, không còn là câu trả lời đầy đủ.

---

## 1. Bối cảnh & vấn đề

lazyexplorer sống *cạnh* một coding agent (`CLAUDE.md` §"Goal & Positioning"): user liếc preview pane,
thấy nội dung file agent vừa sửa, rồi muốn **lấy MỘT ĐOẠN** dán vào chat của agent. Pain ghi nhận (lặp
lại): *"support to select and copy content in the file preview"* — **chính** câu pain mà
`prd-preview-copy` đã ghi (*"let me select and copy content"*, `prd-preview-copy.md:11`). Việc pain quay
lại sau khi `prd-preview-copy` đã ship (`Y` + native-drag note) là **bằng chứng** giải pháp cũ chưa đủ.

`prd-preview-copy` D1/D2 chốt: phần "chọn đoạn nhìn thấy" để **terminal native** lo (giữ Shift/Option rồi
kéo), app chỉ sở hữu "copy cả file" qua `Y`. Quyết định đó **đúng nhưng phụ thuộc layout và không đầy đủ** —
nó có ba lỗ hổng mà beside-an-agent workflow đụng phải, **xếp theo sức nặng bằng chứng**:

1. **Off-viewport — không với tới (lỗ hổng chính, HIGH, độc lập terminal).** Native-drag chỉ chọn được các
   dòng **đang hiện** trên viewport; một hàm 40 dòng tràn khỏi viewport thì **không terminal nào** kéo-chọn
   một nhịp được — đây là giới hạn vật lý của native-selection, đúng ở *mọi* terminal, *mọi* layout, không
   modifier nào lách được. `Y` (cả file) giải "off-viewport = toàn bộ", nhưng **một dải dòng cụ thể** tràn
   viewport thì không công cụ nào hiện có với tới. Đây là lý do **chắc nhất** để có `V`.
2. **Nhiễm chéo trong layout 2-cột (lỗ hổng phụ, MEDIUM, suy luận từ geometry).** Ở side-by-side (mặc định
   khi `m.width >= widthBreakpoint`), list pane chiếm cột `[0, dividerStart)` và preview chiếm
   `[dividerStart+dividerWidth, m.width)` (`view.go:90-95`). Native-selection **linewise toàn chiều rộng**
   (mặc định ở đa số terminal) — kéo qua dòng preview ôm luôn cột list + glyph divider. Native-drag chỉ sạch
   ở **1-cột stacked** (terminal hẹp) hoặc preview full-width. *Cảnh báo evidence*: vài terminal hỗ trợ
   **block/rectangular selection** (vd iTerm2 Option-drag — chính modifier `prd-preview-copy` khuyến nghị
   `prd-preview-copy.md:122`) **có thể** cô lập cột, làm yếu lỗ hổng này. Vì vậy nó là **phụ**, không phải
   trụ của reversal (cách falsify ở §5.1).
3. **Rendered ≠ raw + modifier ẩn.** Native copy đúng cái đang vẽ: markdown (glamour) ra prose reflow +
   bullet (không phải source); và modifier Shift/Option là tri-thức terminal-specific, không discoverable
   cho user keyboard-first kiểu lazygit.

`Y` (copy CẢ file, `model.go:1541`) giải quyết "off-viewport = cả file" nhưng **không** giải quyết "một
dải dòng cụ thể". Khoảng trống còn lại: **chọn một dải dòng (line range), kể cả tràn viewport, copy raw,
không nhiễm list pane** — đúng thứ pain mô tả.

## 2. Goal (1 câu)

Thêm **in-app line-visual selection** trong preview qua **hai lối vào dùng chung một state**:
- **Bàn phím**: `V` bắt đầu chọn từ dòng trên cùng đang hiện, mở rộng dải bằng đúng các phím cuộn
  (j/k/ctrl+d/u/g/G) với auto-scroll, rồi `y`/Enter copy.
- **Chuột**: **kéo** (press→drag→release) trong pane preview để chọn dải dòng, **thả là copy** — một cử chỉ.

Cả hai copy **raw text** của dải dòng lên clipboard. Là một **sub-state của focusPreview**, KHÔNG thêm
mode/panel mới; chuột tái dùng toàn bộ state + highlight + cancel của bàn phím (chỉ thêm press/motion/release).

**Quan hệ với `prd-preview-copy` (positive framing):**

- Phím `Y` = copy **cả file** (off-viewport, một keystroke) — **giữ nguyên**.
- Phím `V` = chọn & copy **một dải dòng** (range, kể cả tràn viewport, không nhiễm list pane) — **mới**.
- Native Shift/Option-drag = chọn **đoạn nhìn thấy ở 1-cột / full-width preview** — vẫn hợp lệ, vẫn ghi
  trong help/README; `V` phủ đúng phần native-drag KHÔNG với được (2-cột + off-viewport + raw).

Ba công cụ, ba phạm vi không chồng lấn. `prd-preview-copy` D1/D2 được **thu hẹp** từ "native-drag là đủ"
thành "native-drag đủ cho 1-cột/full-width nhìn-thấy; phần còn lại thuộc `V`".

**Non-goal làm rõ (chặn scope creep):**

- **KHÔNG** selection theo **ký tự / sub-line** (chọn nửa dòng, chọn cột) — line-granular là đủ cho
  "dán mấy dòng vào agent"; char-select là cả một subsystem (highlight per-cell, tương tác hSlice/wrap)
  phá ceiling đơn giản cho giá trị biên (§5.6).
- **CÓ** mouse drag-to-select trong preview (press→drag→release-copy, D13/§5.6) — bàn phím + chuột cùng
  một state. KHÔNG dùng cho selection theo ký tự (vẫn line-granular cả ở chuột) và KHÔNG edge-scroll phức
  tạp (best-effort một dòng/motion, §5.6).
- **KHÔNG** thêm một preview line-cursor **thường trực** (highlight dòng hiện tại mọi lúc ở focusPreview) —
  đó là thay đổi UX rộng + churn nhiều test prd-pane-focus; v1 cursor chỉ tồn tại **trong lúc đang chọn**
  (anchor = dòng trên cùng, D6).
- **KHÔNG** copy bản render ANSI (glamour/chroma/diff-color) — copy **raw de-colored text** (D5/D8).
- **KHÔNG** selection trên markdown/image/folder — preview không có ánh xạ dòng-nguồn sạch ở đó; markdown
  đã có `Y` cho raw source (D4).
- **KHÔNG** palette twin cho selection — selection là một sub-state tương tác, không phải one-shot chạy
  được từ palette khi chưa có dải đang chọn (D9).
- **KHÔNG** đụng `Y` (copy cả file), `y` (yank rel path), `v` (toggle diff) — phím mới là `V`, lane riêng.
- **KHÔNG** wiring trong modeSearch/modeChanges flat-list ở v1 — selection sống ở focusPreview của
  modeNormal; copy một hit/diff-row vẫn là `Y` (cả file) như `prd-preview-copy` D4 (§5.5).

## 3. Quyết định đã chốt

Headline (D1) là **hình dạng selection**; mọi D sau nằm dưới nó.

| # | Quyết định | Giá trị | Lý do |
|---|-----------|---------|-------|
| **D1** | **In-app line-visual selection, keyboard-driven, là SUB-STATE của focusPreview (không phải mode mới).** | `V` mở selection; phím cuộn mở rộng dải; `y`/Enter copy; Esc/`V` huỷ. State: `selecting bool`, `selAnchor int`, `selCursor int` (chỉ số **dòng nguồn** vào `m.preview`). Không thêm giá trị `mode` enum | Khoảng trống thật là "chọn một dải dòng, kể cả tràn viewport, không nhiễm list pane" (§1). Line-visual + scroll-follow giải đúng cái đó. **Sub-state, không phải mode**: gate bằng `m.selecting` trong `updateNormal` (như `focusPane` là sub-state của modeNormal), nên bộ máy `mode` (rename/delete/search/changes/palette/help) **không đụng tới** — ceiling đơn giản còn nguyên. Reuse đúng các phím focusPreview đã route theo pane (D7) |
| **D2** | **Thu hẹp `prd-preview-copy` D1/D2: native-drag KHÔNG đủ — trụ là off-viewport.** | `V` phủ off-viewport (trụ HIGH) + 2-cột (phụ MEDIUM) + raw; native-drag giữ giá trị ở 1-cột/full-width nhìn-thấy | **Trụ của reversal là off-viewport (HIGH, độc lập terminal)**: native-drag *vật lý* không với tới dòng đã cuộn khỏi viewport, ở mọi terminal/layout — không tranh cãi (§1.1). Nhiễm-chéo-2-cột là **lý do phụ MEDIUM** (geometry, có thể bị block-selection của iTerm2 làm yếu — §1.2/§5.1), KHÔNG được làm trụ. `prd-preview-copy` D1/D2 không *sai* — nó **scope-limited**: đúng cho nhìn-thấy/full-width, hở cho off-viewport (và 2-cột). Pain quay lại sau ship là evidence. Không xoá native-drag note (vẫn đúng phần của nó) — chỉ thôi tuyên bố "là đủ" |
| **D3** | **Granularity = LINE, không phải character** | Chọn theo dòng nguồn trọn vẹn; không chọn nửa dòng | Use case là "dán mấy dòng code/text vào agent" — line-granular khớp 100%. Char-select nổ scope: highlight per-cell, tương tác với hSlice nowrap (`view.go:671`) + wrap reflow (`model.go:771`), anchor 2 chiều. Giá trị biên cho một glance-companion → defer (§5.6) |
| **D4** | **Scope = mọi preview SCROLLABLE (plain text · code · diff); markdown/image/folder → `V` no-op** | Gate: `m.previewScrollable && !m.previewIsDir` (đã true cho plain/code `model.go:701,713,730`; false cho markdown/image/folder) | Cơ chế copy (de-ANSI dòng đang hiển thị, D5) **đồng nhất** cho plain/code/diff: với code nó trả source de-color; với diff nó trả text diff de-color (một hunk dán vào agent — hữu ích). Markdown glamour reflow → không có dòng-nguồn 1:1, và đã có `Y` cho raw source. **Bao gồm diff** (khác lưu ý "defer diff" ban đầu): vì D5 không cần ánh xạ file-line, diff vào **miễn phí** và đồng nhất (consistency-is-kindness) — chi tiết §5.4 |
| **D5** | **Copy = `ansi.Strip(m.preview[i])` cho i ∈ [min,max], join `"\n"` — dòng ĐANG HIỂN THỊ, de-color; KHÔNG đọc đĩa** | `strings.Join(stripEach(m.preview[lo:hi+1]), "\n")` qua `github.com/charmbracelet/x/ansi` (`ansi.Strip`, direct dep `go.mod:11`, đã dùng ở `changes_test.go:337`) | **WYSIWYG là invariant đúng (không phải line-count equality):** selection **định nghĩa trên `m.preview`** (buffer hiển thị) và `copySelection` đọc thẳng `m.preview[i]` → copy **tái tạo đúng cái đang hiển thị** ở các dòng được chọn. Không cần "số dòng preview = số dòng nguồn": ta chỉ chọn được dòng *đang hiện*, nên highlightCode bỏ trailing-blank (`fs.go:444`) hay cap lệch (plain line-cap 2000 `fs.go:284`, code byte-cap 256KB `fs.go:252`) **không ảnh hưởng** — chỉ giới hạn *tới đâu* chọn được, không sai *cái* chọn. **Race-free + đồng nhất**: không đọc đĩa lúc copy; plain/code/diff cùng một path (diff không có file-line map nhưng text de-color CHÍNH là nội dung hữu ích). Đọc-đĩa-theo-range đã cân nhắc, bác bỏ ở §5.4 |
| **D6** | **Anchor = dòng nguồn TRÊN CÙNG đang hiện lúc nhấn `V`; KHÔNG có preview cursor thường trực ở v1** | `selAnchor = selCursor = sourceLineAt(previewTop)` (`model.go:811`) | Preview hiện là viewport cuộn thuần, **không có** line-cursor (`previewTop` only). Thêm cursor thường trực = đổi UX rộng + churn test prd-pane-focus → defer (non-goal). Anchor-tại-đỉnh là default **trung thực duy nhất** khi chưa có cursor. Hệ quả (documented compromise): để chọn một dải *bên trong* viewport, **cuộn dòng-bắt-đầu lên đỉnh rồi `V`** rồi mở rộng xuống. Workflow "scroll-to-top → V → extend" tự nhiên cho selection tràn viewport |
| **D7** | **Phím: `V` mở/huỷ; trong lúc chọn các phím focusPreview được REPURPOSE** | `V` (free — mnemonic vim line-visual, cạnh `v`=diff). Đang chọn: j/k/↑/↓ ±1 dòng cursor; ctrl+d/u ±½ trang; g/G đầu/cuối; **copy = `y` HOẶC `enter`** (binding `CopySelection` = `WithKeys("y","enter")` — KHÔNG dùng `OpenEntry`); **huỷ = `Esc` HOẶC `V`**; **`Tab` huỷ rồi đổi focus**. Dispatch qua `updateSelecting(msg)` ở đầu `updateNormal` khi `m.selecting` | `V` uppercase free (`keys.go` không `WithKeys("V")`). **Copy KHÔNG match `OpenEntry`**: `OpenEntry`=`WithKeys("enter","l","right")` (`keys.go:72`) — match nó thì `l`/`→` (vốn pan-right ở focusPreview `model.go:1652`) sẽ vô tình copy-và-thoát. Nên `CopySelection` bind **riêng** `y`+`enter`; `l`/`→` trong lúc chọn → noop (bị switch nuốt). `updateSelecting` riêng (locality: toàn bộ phím selection một chỗ) **nuốt** mọi phím khác (Y/e/r/d…) → không mutation bất ngờ. `Tab` là **case tường minh** huỷ+đổi focus (không thì Tab thành dead key trong lúc chọn). Sub-state **sở hữu** phím cuộn như `focusPane` sở hữu j/k (`model.go:1602`). `y` (Yank) no-op ở focusPreview (`model.go:1735`) → free, không double-map với CopySelection (Yank chỉ chạy !selecting & focusList) |
| **D8** | **Copy raw de-colored text; status `copied N lines (B bytes)` / `⚠ clipboard: …`** | `writeClipboard(text)` (`commands.go:210`) đồng bộ, như yank/copyContent — không `tea.Cmd` | Mirror `prd-preview-copy` D7: clipboard nội dung phải sạch (không escape-code). `writeClipboard` đồng bộ đã là precedent (`model.go:1566`). Status clipboard-agnostic (CI không có pbcopy → `⚠ clipboard`) |
| **D9** | **Một code path `copySelection()`, KHÔNG palette twin** | `func (m *model) copySelection()`: clamp range → `ansi.Strip`+join → `tel.Record` một lần → `writeClipboard` → status → `selecting=false` | Selection là sub-state **tương tác** (cần dải đang chọn) — không one-shot-able từ palette như copyContent/yank. Bỏ twin tránh một command palette "copy selection" chạy được khi không có gì đang chọn (bẫy). Telemetry record đúng một lần trong helper |
| **D10** | **Telemetry `action.copy_selection {lines, bytes}` — meta, KHÔNG log content** | `m.tel.Record("action.copy_selection", {"lines": n, "bytes": len(text)})` một lần | Mirror copy_content D10: đo adoption, log số dòng + size; KHÔNG log nội dung (tránh rò file content) |
| **D11** | **Huỷ selection tại CHOKE POINT đổi buffer (`applyPreview`) — không chỉ gate poll** | **Chính**: `applyPreview` set `m.selecting=false` ở **cả** nhánh success (`model.go:1041`) lẫn error (`model.go:1026`) — mọi đường reassign `m.preview`. **Phụ (belt-and-suspenders)**: `startSelection` **từ chối** khi `m.pendingWidth > 0` (đừng anchor lên placeholder sắp bị thay); tickMsg guard `&& !m.selecting` (`model.go:1074`); `refreshPreview` reset hygiene; `Tab` huỷ tường minh (D7) | **CRITICAL race (review tìm ra):** `m.preview` là **buffer async khả biến**, không phải mảng dòng-nguồn ổn định. `V` KHÔNG bump `renderGen` nên một render đang bay (`syncPreview`) vẫn **đáp xuống** giữa lúc chọn và ghi đè `m.preview` (`applyPreview` `model.go:999-1041`). Tệ nhất là **diff**: placeholder = cả file (`plainLines` `model.go:703`), diff async đáp = vài dòng hunk (`model.go:949`) → `selAnchor/selCursor` chỉ vào buffer ngắn hơn → `copySelection` clamp **âm thầm copy SAI**. Ba vector cùng đổ về `applyPreview`: render-đáp, **resize** (`WindowSizeMsg` `model.go:1106`→tail re-render), **git refresh** ở modeChanges (`gitRefreshedMsg`→`refreshChanges` `model.go:1088`). Gate `!m.selecting` ở tickMsg là **cần nhưng KHÔNG đủ** — phải huỷ tại `applyPreview` (một điểm chặn phủ cả ba) |
| **D12** | **Highlight = nền selection trên các dòng VISUAL của dải, text de-color; style riêng khác cursor list** | `selectionStyle` (theme.go) `Background(<surface>)` ≠ `cursorActiveStyle.Background(colAccent)` (`theme.go:69`). Dòng visual `vi` được tô khi `sourceLineAt(vi) ∈ [min,max]` | Tô **nền lên dòng đã có ANSI (chroma)** dễ vỡ SGR → giải pháp robust: dòng được chọn render **de-color + nền selection** (khối highlight = đúng cái sẽ copy, WYSIWYG). Wrap: một dòng nguồn → nhiều dòng visual, ánh xạ qua `sourceLineAt`/`previewSrcStart` (`model.go:811,804`) đã có sẵn cho wrap-toggle. Màu nền khác accent để dải-preview ≠ cursor-list |
| **D13** | **Mouse: press→drag→release-copy; KÉO mới chọn (click trơn KHÔNG copy); tái dùng state bàn phím** | Trong `handleMouse` (`model.go:1248`): **press** trái trong pane preview (file scrollable) → arm (`mouseDragArmed=true`, anchor = dòng nguồn dưới con trỏ), set focus=preview, CHƯA `selecting`. **Motion** khi armed → `selecting=true`, `selCursor`=dòng nguồn dưới con trỏ (commit ở motion đầu). **Release** → nếu `selecting` thì `copySelection()` (thả-là-copy), luôn clear arm. Edge-scroll best-effort: motion qua mép trên/dưới → `scrollPreview(∓1)` rồi clamp | **Kéo mới chọn** (motion commit) tránh **click trơn copy bất ngờ** — press+release không motion = click thường (focus-set, không copy). **Thả-là-copy** = một cử chỉ liền mạch cho mouse-crowd (`CLAUDE.md` "click/drag/wheel"), khác bàn phím (incremental → `y` xác nhận) đúng bản chất hai modality. **KHÔNG đụng**: divider-drag (press trên divider hit-zone, vùng tách rời `model.go:1262`), native Shift-drag (terminal nuốt, app không thấy — bổ sung, không xung đột), wheel (message type khác). 2-cột: press ở **pane phải** chỉ chọn dòng preview → đúng cái fix nhiễm-chéo (§1.2). Toàn bộ `selAnchor/selCursor/selecting/copySelection/highlight/cancel` **dùng chung** với bàn phím — chuột chỉ thêm `mouseDragArmed` + 3 nhánh press/motion/release + helper row→dòng-nguồn |

## 4. Functional requirements

- **FR1** — Ở focusPreview trên một preview **scrollable** (plain/code/diff), nhấn `V` bắt đầu chọn dòng,
  anchor = dòng nguồn trên cùng đang hiện; dòng anchor được highlight; vào sub-state `selecting`.
- **FR2** — Đang chọn: các phím cuộn focusPreview (j/k/↑/↓ = ±1 dòng; ctrl+d/u = ±½ trang; g/G = đầu/cuối)
  **di chuyển đầu-cuối (selCursor) của dải**, auto-scroll để cursor luôn trong viewport; dải inclusive
  `[min(anchor,cursor), max(anchor,cursor)]` được highlight.
- **FR3** — Đang chọn: `y` (hoặc Enter) copy **raw de-colored text** của các dòng trong dải, join bằng
  `"\n"`, lên clipboard; status `copied N lines (B bytes)` (clipboard ok) hoặc `⚠ clipboard: …` (no
  helper); thoát `selecting`.
- **FR4** — Đang chọn: `Esc` (hoặc `V`) huỷ chọn, KHÔNG copy; preview trở lại cuộn thường. Sau khi huỷ,
  `Esc` lần nữa trả focus về list (hành vi prd-pane-focus cũ, không đổi).
- **FR5** — `V` là **no-op** trên preview không-scrollable (markdown/image/folder) và ở focusList. (Tuỳ
  chọn: status hint "selection works in the file preview".)
- **FR6** — Nội dung copy là **raw** (không ANSI): text đã-hiển-thị/đã-normalize của các dòng được chọn —
  KHÔNG phải bản tô màu (code highlight) hay bản diff đã màu, dù preview đang ở chế độ nào.
- **FR7** — Mọi đường **reassign `m.preview`** trong lúc đang chọn → huỷ dải (`selecting=false`): một
  async render đáp xuống (`applyPreview`), một **resize** (đổi width → re-render), một **git refresh ở
  modeChanges** (re-derive diff). `Tab` cũng huỷ rồi đổi focus. (Cursor-move/điều-hướng/poll KHÔNG cancel
  "giữa chừng" vì sub-state nuốt phím và poll bị gate — buffer **đứng yên** trong lúc chọn, FR8.)
- **FR8** — Trong lúc `selecting`: poll loop **không** `syncFromDisk` re-render (gate `!m.selecting`); và
  `startSelection` **từ chối** mở khi đang có render bay (`m.pendingWidth > 0`) — để không anchor lên một
  placeholder sắp bị thay (status hint, no-op). Hai điều này giữ chỉ số dòng ổn định khi đã vào được chọn.
- **FR9** — Chọn rồi copy record `action.copy_selection` **đúng một lần** với `{lines, bytes}`, KHÔNG log
  content.
- **FR10** — Discoverability: `V` reachable qua nhóm **Preview** của `?` fullHelp (glance bar tinh
  gọn không mang `V` — xem prd-keymap-and-command-palette FR14); trong lúc chọn, status bar hiện
  hint (`y` copy · `esc` cancel).
- **FR11** — Dải một dòng hợp lệ: `V` rồi `y` ngay (không di chuyển) copy đúng một dòng (dòng trên cùng).
- **FR12** — `l`/`→` trong lúc chọn là **no-op** (KHÔNG copy, KHÔNG thoát) — chỉ `y`/`enter` copy (D7).
- **FR13** — **Mouse kéo** (press trái trong pane preview của một file scrollable → di chuyển → thả): chọn
  dải dòng từ dòng nguồn dưới điểm press tới dòng nguồn dưới con trỏ; **thả là copy** (raw de-color, cùng
  `copySelection`/telemetry/status như bàn phím). Hoạt động ở cả 2-cột (pane phải) lẫn 1-cột (pane dưới).
- **FR14** — **Click trơn** (press+release KHÔNG di chuyển) trong preview **KHÔNG** copy và KHÔNG chọn —
  giữ nguyên hành vi click cũ (set focus=preview). Chỉ có **kéo** (motion) mới bắt đầu chọn.
- **FR15** — Kéo qua **mép trên/dưới** pane preview → edge-scroll best-effort (cuộn ∓1 dòng mỗi motion),
  `selCursor` theo dòng mới ở mép → cho chọn vượt viewport bằng chuột mà không cần bàn phím.
- **FR16** — Mouse selection **không** can thiệp: divider-drag (press trên divider vẫn kéo divider), native
  Shift/Option-drag (terminal nuốt, app không thấy), wheel (vẫn cuộn list/preview).

## 5. Technical design

Kim chỉ nam: **selection là sub-state của focusPreview** (như `focusPane` là sub-state của modeNormal),
**định nghĩa trên dòng nguồn `m.preview`**, copy = de-ANSI dòng hiển thị. Reuse máy móc đã có
(`sourceLineAt`/`previewSrcStart`/`previewScroll`), thêm tối thiểu state + một `updateSelecting` + một
nhánh highlight trong `renderPreview`.

### 5.1 Lỗ hổng native-drag ở 2-cột — bằng chứng (nền của D2)

`view.go:90-95`: ở layout 2-cột, `leftInner` cột list, `rightInner` cột preview, ngăn bởi `dividerWidth`
cột divider — ba khối **cùng hàng**. Native-selection của terminal (khi app bật mouse capture
`v.MouseMode = tea.MouseModeCellMotion` `view.go:381`, và user giữ Shift/Option để bypass về native theo
`prd-preview-copy` §5.1) ở đa số terminal là **linewise toàn chiều rộng**: kéo phủ các hàng preview sẽ
chọn cả ký tự cột list + glyph divider trên đúng hàng đó. Vậy **không** chọn sạch riêng preview ở 2-cột.

Mức: **suy luận từ geometry (MEDIUM)** — chưa live-drag-test trong phiên này (môi trường agent headless).
**Cách falsify**: chạy lazyexplorer ở terminal thật, width ≥ `widthBreakpoint` (2-cột), Shift/Option-drag
qua vài dòng preview, xem clipboard có lẫn text cột list không. Nếu terminal hỗ trợ **block/rectangular
selection** (vài terminal: thêm modifier nữa) thì có thể cô lập cột — nhưng đó là thao tác fiddly, không
phổ quát, và vẫn chỉ với tới dòng nhìn-thấy (không giải off-viewport). `V` không phụ thuộc điều này.

> Lưu ý: D2 chỉ **thu hẹp** `prd-preview-copy` D1/D2, không phủ nhận. Native-drag ở **1-cột stacked**
> (terminal hẹp, preview full-width) vẫn chọn sạch — note Shift/Option trong help/README giữ nguyên.

### 5.2 State + vòng đời (`model.go`)

Thêm vào `model` (cụm focusPreview):

```go
// In-app line selection (prd-preview-selection). A SUB-STATE of focusPreview, not a
// mode: gated by `selecting` inside updateNormal so the mode machinery is untouched.
// selAnchor/selCursor are SOURCE-line indices into m.preview (the displayed buffer);
// the inclusive range [min,max] is what highlights and what copySelection copies.
// Alive only while selecting — there is no persistent preview cursor (D6).
selecting bool
selAnchor int
selCursor int
```

Vòng đời:
- **Mở** (`startSelection`, gọi từ `V` ở focusPreview, scrollable): **từ chối** nếu `m.pendingWidth > 0`
  (render đang bay — đừng anchor lên placeholder sắp bị thay; status hint, no-op, FR8). Ngược lại
  `selecting=true`; `selAnchor=selCursor=sourceLineAt(previewTop)`.
- **Mở rộng** (`moveSelection(delta)` / `moveSelectionTo(i)`): `selCursor` clamp `[0, len(m.preview)-1]`;
  rồi `scrollSelectionIntoView()` đẩy `previewTop` để dòng **visual** của `selCursor` nằm trong
  `[top, top+bodyH)` (xem §5.5 cho công thức wrap/nowrap).
- **Copy** (`copySelection`): D5/D9 — clamp range, `ansi.Strip`+join, record, writeClipboard, status,
  `selecting=false`.
- **Huỷ** (`cancelSelection`): `selecting=false` (giữ nguyên `previewTop`). Gọi từ `Esc`/`V`/`Tab`.
- **Huỷ tại choke point đổi buffer (CRITICAL, D11)**: `applyPreview` set `selecting=false` ở **cả** nhánh
  success (`model.go:1041`) lẫn error (`model.go:1026`) — một điểm chặn phủ render-đáp + resize + git
  refresh (FR7). Đây là cơ chế freeze **chính**; gate poll `!m.selecting` chỉ là phụ.
- **Reset hygiene**: `refreshPreview` set `selecting=false` (cụm reset đầu hàm `model.go:624`); `FocusToggle`
  set `selecting=false`.

### 5.3 Dispatch (`updateNormal` + `updateSelecting`)

Đầu `updateNormal` (`model.go:1576`), trước `switch`:

```go
if m.selecting {
    return m.updateSelecting(msg)
}
```

`updateSelecting` (mới) — switch đóng, CHỈ các phím selection, mọi phím khác noop (không mutation giữa chừng):

```go
func (m model) updateSelecting(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
    km := m.keymap
    switch {
    case key.Matches(msg, km.SelectMode), key.Matches(msg, km.Back): // V / esc → huỷ
        m.cancelSelection()
    case key.Matches(msg, km.FocusToggle): // Tab → huỷ rồi đổi focus
        m.cancelSelection()
        m.focusPane = focusList
    case key.Matches(msg, km.CopySelection): // y / enter — KHÔNG match OpenEntry (l/right)
        m.copySelection()
    case key.Matches(msg, km.MoveDown):   m.moveSelection(1)
    case key.Matches(msg, km.MoveUp):     m.moveSelection(-1)
    case key.Matches(msg, km.PreviewHalfPageDown): _, h := m.previewScroll(); m.moveSelection(max(1, h/2))
    case key.Matches(msg, km.PreviewHalfPageUp):   _, h := m.previewScroll(); m.moveSelection(-max(1, h/2))
    case key.Matches(msg, km.GoBottom):   m.moveSelectionTo(len(m.preview) - 1)
    case key.Matches(msg, km.GoTop):      m.moveSelectionTo(0)
    // l/right (OpenEntry), Y/e/r/d, v, w, H/L/0… → rơi xuống đây = noop trong lúc chọn
    }
    return m, m.reconcilePreview(nil)
}
```

`V` ở focusPreview (trong `updateNormal`, KHI chưa `selecting`) gọi `startSelection()` — gọi này **từ chối**
khi `m.pendingWidth > 0` (FR8). `CopySelection` bind **riêng** `y`+`enter` (`keys.go`,
`WithKeys("y","enter")`) — **không** match `km.OpenEntry` (`enter/l/right`) để `l`/`→` không vô tình
copy-và-thoát (D7/FR12). `Back` (esc) huỷ — sub-state nuốt esc lần một; esc lần hai (đã thoát selecting)
mới về list (FR4). `Tab` huỷ + đổi focus (case tường minh, không để Tab thành dead key).

### 5.4 Copy = de-ANSI dòng hiển thị (`copySelection`) — và vì sao KHÔNG đọc đĩa

```go
func (m *model) copySelection() {
    lo, hi := min(m.selAnchor, m.selCursor), max(m.selAnchor, m.selCursor)
    hi = min(hi, len(m.preview)-1)
    if lo < 0 || lo > hi { m.selecting = false; return } // defensive
    raw := make([]string, 0, hi-lo+1)
    for i := lo; i <= hi; i++ {
        raw = append(raw, ansi.Strip(m.preview[i]))
    }
    text := strings.Join(raw, "\n")
    m.tel.Record("action.copy_selection", map[string]any{"lines": hi - lo + 1, "bytes": len(text)})
    m.selecting = false
    if err := writeClipboard(text); err != nil { m.statusMsg = "⚠ clipboard: " + err.Error(); return }
    m.statusMsg = "copied " + strconv.Itoa(hi-lo+1) + " lines (" + strconv.Itoa(len(text)) + " bytes)"
}
```

**Invariant nền (đã verify bằng đọc code):** `m.preview[i]` ↔ dòng nguồn `i` cho plain + code.
- plain: `plainLines(content) = strings.Split(normalizeText(content), "\n")` (`fs.go:282-283`); `normalizeText`
  chỉ CRLF→LF, bỏ lone-CR, tab→space (`fs.go:271-277`) — **không đổi line-count**. Nên `ansi.Strip` (no-op
  trên plain) trả đúng dòng hiển thị.
- code: `renderCodePreview` → `highlightCode`, chroma emit **một SGR-run đóng/mở mỗi dòng** (`fs.go:410`,
  `Coalesce` `fs.go:427`) — line-count = số dòng nguồn; `ansi.Strip` trả source de-color.
- diff: `m.preview` = output `diffHunks` (preStyled ANSI `git.go`), KHÔNG ánh xạ file-line — nhưng D5 copy
  **dòng hiển thị de-color**, nên trả đúng text diff (±/@@) sạch màu. Hữu ích (một hunk dán vào agent) và
  **không cần** file-line map → diff vào miễn phí, cùng một code path.

**Phương án đã cân nhắc — đọc đĩa theo line-number (BÁC BỎ):** `os.ReadFile`→`normalizeText`→`Split`→lấy
`[lo:hi]`. Hợp lý cho mệnh đề "fresh from disk" của `Y`, nhưng `Y` copy **cả file** (không cần ánh xạ
hiển-thị→nguồn) còn selection **bản chất là về dải đang hiển thị** → đọc buffer hiển thị trung thực hơn.
Đọc đĩa lại: (a) mở cửa sổ race file-đổi-giữa-chừng; (b) buộc re-derive normalize để giữ chỉ số khớp;
(c) **không phục vụ được diff** (không file-line map) → phải loại diff. `ansi.Strip(m.preview)` race-free,
index-honest, đồng nhất ba loại. Đánh đổi documented: code/diff có tab đã expand→space (preview normalize
trước, `fs.go:275`); đó là "copy-what-you-see", chấp nhận được cho dán-vào-agent.

### 5.5 Highlight render (`view.go renderPreview` + `theme.go`)

`renderPreview` (`view.go:610`) có ba nhánh: non-scrollable (markdown/placeholder), wrap, nowrap-hwindow.
Selection (D4) chỉ chạm **hai nhánh scrollable** — wrap (`view.go:648`) và nowrap (`renderHWindow`
`view.go:663-671`); nhánh markdown/placeholder bị D4 loại nên **không đụng**. Với mỗi dòng visual tuyệt đối
`vi = top+off` đang vẽ: nếu `m.selecting && sourceLineAt(vi) ∈ [min,max]` → render dòng đó **de-color +
`selectionStyle` nền** (`selectionStyle.Render(fitWidth(ansi.Strip(displayLine), w))`), ngược lại render như
cũ. Khối highlight do đó **bằng đúng** cái copySelection sẽ copy (WYSIWYG, D12).

- **nowrap** (`renderHWindow`): dòng được chọn render **de-color + nền, fit đúng `w` (full pane width),
  BỎ QUA hSlice-window + chỉ báo `‹/›`**. Đánh đổi cosmetic documented: dòng chọn **không** đi theo
  `previewHScroll` (đọc trọn dòng quan trọng hơn pan-ngang khi đang chọn) và rộng hơn dòng-không-chọn 1-2
  cột (dòng thường chừa cột chỉ báo `‹/›` `view.go:687-693`). Copy không bị ảnh hưởng (vẫn `ansi.Strip`
  dòng nguồn).
- **wrap** (`view.go:648`): mọi dòng visual của một dòng nguồn được chọn đều tô (ánh xạ qua `previewSrcStart`
  `model.go:804`).

`scrollSelectionIntoView()` (đặc tả): đẩy `previewTop` tối thiểu để **dòng visual** của `selCursor` lọt
`[previewTop, previewTop+bodyH)`. Visual line = `visualLineFor(selCursor)` ở wrap (`model.go:828`), = chính
`selCursor` ở nowrap. Nếu visual `< previewTop` → `previewTop = visual`; nếu `≥ previewTop+bodyH` →
`previewTop = visual - bodyH + 1`; rồi clamp `[0, max(0, previewLen()-bodyH)]` (như `scrollPreview`
`model.go:1446`). Bất biến kiểm được: sau mỗi `moveSelection`, visual của `selCursor` ∈ `[top, top+bodyH)`.

`theme.go`: `selectionStyle = lipgloss.NewStyle().Background(<surface color>)` — màu nền **khác**
`colAccent` (cursor list) để hai loại highlight phân biệt; foreground để mặc định/`colFg` đọc rõ trên nền.

### 5.6 Mouse drag-to-select (`handleMouse`, `model.go:1248`)

Chuột tái dùng **toàn bộ** state selection của bàn phím — chỉ thêm một cờ `mouseDragArmed bool` và ba nhánh
trong `handleMouse`. Helper chung:

```go
// srcLineAtRow maps a screen row y to the source-line index under it in the preview
// pane: visual line = previewTop + (y - g.previewFirstRow); source = sourceLineAt(visual),
// clamped to [0, len(m.preview)-1]. Mirrors previewClick's row math (model.go:1472).
func (m model) srcLineAtRow(y int, g geometry) int {
    top, bodyH := m.previewScroll()
    vrow := top + clamp(y-g.previewFirstRow, 0, max(0, bodyH-1))
    return m.sourceLineAt(vrow)
}
```

Ba nhánh trong `handleMouse` (chỉ khi preview là **file scrollable**, `m.previewScrollable && !m.previewIsDir`):

- **MouseClickMsg, left, trong pane preview** (sau các guard divider/header/non-left đã có): set
  `focusPane=preview` (như cũ) **và** `mouseDragArmed=true`, `selAnchor=selCursor=srcLineAtRow(e.Y, g)`,
  `selecting=false` (chưa commit — click trơn không được copy, FR14). KHÔNG gọi `previewClick` cho file
  (nó vốn noop trên file preview `model.go:1462`).
- **MouseMotionMsg khi `mouseDragArmed`**: nếu `e.Y < g.previewFirstRow` → `scrollPreview(-1)`; nếu
  `e.Y >= g.previewFirstRow+bodyH` → `scrollPreview(1)` (edge-scroll, FR15). Rồi `selecting=true`,
  `selCursor=srcLineAtRow(e.Y, g)` (commit ở motion đầu). (Nhánh `m.dragging` divider đứng trước, tách rời.)
- **MouseReleaseMsg khi `mouseDragArmed`**: nếu `selecting` → `m.copySelection()` (thả-là-copy, D13/FR13);
  `mouseDragArmed=false`. (Nhánh `m.dragging=false` divider vẫn chạy.)

**Không xung đột (FR16):** divider-drag bắt đầu khi press trên divider hit-zone (`overDivider`,
`model.go:1262`) — nhánh đó `return` trước khi tới nhánh preview, nên một press hoặc là divider hoặc là
preview, không cả hai. Native Shift/Option-drag bị **terminal** nuốt (bypass, `prd-preview-copy` §5.1) nên
app không bao giờ thấy event đó — bổ sung, không đè. Wheel là `MouseWheelMsg` (type khác) → vẫn cuộn.

**CRITICAL race vẫn được phủ:** mouse-selection cũng set `selecting=true`, nên `applyPreview` cancel
(D11) + poll gate `!m.selecting` áp dụng y hệt. Một mouse-drag thường rất ngắn nên hiếm đụng render-đáp,
nhưng cơ chế phủ sẵn.

### 5.7 Đã cân nhắc & defer khỏi v1

- **Char/sub-line selection** — REJECTED (D3): nổ scope (highlight per-cell, tương tác hSlice/wrap, anchor
  2 chiều) cho giá trị biên; line-granular đủ cho dán-vào-agent.
- **Mouse char/sub-line drag + edge-scroll mượt (timer-based auto-scroll)** — DEFER: v1 mouse là
  line-granular + edge-scroll best-effort (một dòng/motion, §5.6). Auto-scroll liên tục khi giữ chuột đứng
  yên qua mép (cần timer/tick) là polish v2; v1 chỉ cuộn khi chuột còn di chuyển.
- **Thả-không-copy (release chỉ finalize, copy bằng `y` sau)** — REJECTED (D13): thả-là-copy là một cử chỉ
  liền cho mouse-crowd; re-drag rẻ nếu chọn nhầm. Muốn tinh chỉnh thì dùng lối bàn phím (V + extend + `y`).
- **Preview line-cursor thường trực** — DEFER: đẹp hơn (anchor tại dòng bất kỳ) nhưng đổi UX cuộn hiện tại
  + churn test prd-pane-focus. v1 anchor-tại-đỉnh (D6) + workflow scroll-to-top.
- **Selection trong modeChanges/modeSearch flat-list** — DEFER (§2 non-goal): selection sống ở focusPreview
  modeNormal; copy một change/hit vẫn là `Y` cả-file (`prd-preview-copy` D4).
- **Char-accurate copy giữ tab thật (đọc đĩa)** — REJECTED (§5.4): race + loại diff; de-ANSI hiển thị
  đồng nhất hơn.

## 6. Acceptance criteria

```gherkin
Feature: Select and copy a range of lines in the file preview

  Background:
    Given the launch directory is the jail root
    And the preview pane is focused on a scrollable text file

  Scenario: Select a few lines and copy them
    Given I have started a line selection at the top visible line
    When I extend the selection down by several lines
    And I copy the selection
    Then the clipboard receives those lines' raw text joined by newlines
    And the status line reports the line count and byte count

  Scenario: The copied text is raw, not the colorized render
    Given the preview shows a syntax-highlighted code file
    When I select a line and copy it
    Then the clipboard receives the plain source line with no ANSI escape codes

  Scenario: Selection spans content scrolled off-screen
    Given I start a selection and keep extending past the bottom of the viewport
    Then the viewport follows the selection end
    And copying yields every line in the range, including those scrolled through

  Scenario: A single-line selection copies one line
    Given I have started a selection
    When I copy without extending it
    Then the clipboard receives exactly the top visible line

  Scenario: Cancel a selection without copying
    Given I have an active line selection
    When I cancel it
    Then nothing is copied to the clipboard
    And the preview returns to normal scrolling

  Scenario: Starting selection is unavailable on a rendered markdown preview
    Given the preview pane is focused on a markdown file
    When I ask to start a selection
    Then no selection begins

  Scenario: An async render landing during selection clears it
    Given the preview shows an instant placeholder while its real render is in flight
    And I have an active line selection on that placeholder
    When the real render lands and replaces the preview buffer
    Then the selection is cleared so no stale range can be copied

  Scenario: Resizing the window during selection clears it
    Given I have an active line selection
    When the terminal is resized so the preview re-renders to a new width
    Then the selection is cleared

  Scenario: Pressing right does not copy during selection
    Given I have an active line selection
    When I press the "move right" key
    Then nothing is copied and the selection stays active

  Scenario: No clipboard helper available
    Given the host has no clipboard helper
    And I have an active line selection
    When I copy the selection
    Then nothing reaches the clipboard
    And the status line reports a clipboard error

  Scenario: Dragging in the preview selects lines and copies on release
    Given the preview pane shows a scrollable text file
    When I press in the preview, drag across several lines, and release
    Then the dragged range of lines is copied to the clipboard as raw text
    And the status line reports the line count

  Scenario: A plain click in the preview copies nothing
    Given the preview pane shows a scrollable text file
    When I click in the preview without dragging
    Then nothing is copied to the clipboard
    And no selection remains active
```

Checklist verify:

1. `updateSelecting`: `V` ở focusPreview/scrollable → `selecting=true`, anchor=`sourceLineAt(previewTop)`;
   j×k mở rộng dải; `y` copy → recorder `action.copy_selection` `bytes`=len(join de-ANSI), `lines`=hi-lo+1;
   status `copied N lines (B bytes)` HOẶC `⚠ clipboard`. *(preview_selection_test.go)*
2. **Raw-not-ANSI**: trên code file, dòng copy = `ansi.Strip` của dòng preview (không chứa `\x1b`); assert
   `!strings.Contains(text, "\x1b")` và bằng `ansi.Strip(m.preview[i])`.
3. **Off-viewport**: mở rộng quá `bodyH` → `previewTop` đi theo (`scrollSelectionIntoView`); range copy gồm
   đủ dòng đã cuộn qua.
4. **Single-line**: `V` rồi `y` ngay → copy đúng 1 dòng (dòng đỉnh).
5. **Cancel**: `V`→`esc` (hoặc `V`) → `selecting=false`, KHÔNG record, clipboard không đổi; `esc` kế tiếp
   về focusList. `Tab` đang chọn → `selecting=false` + focus về list (KHÔNG copy).
6. **No-op**: `V` trên markdown/image/folder/focusList → `selecting` vẫn false, không record. `V` khi
   `m.pendingWidth > 0` (render đang bay) → no-op (FR8).
7. **CRITICAL race (review):** đang chọn mà một `previewRenderedMsg` đáp xuống `applyPreview` (gen khớp) →
   `selecting=false` ở **cả** nhánh success lẫn error → copy không thể grab range cũ trên buffer mới. Test
   trực diện ca diff: anchor trên placeholder cả-file, đẩy diff async đáp → assert `selecting=false`.
   Resize (`WindowSizeMsg` re-render) đang chọn → `selecting=false`. modeChanges git-refresh re-derive diff
   → `selecting=false`.
8. **Poll gate**: poll tick khi `selecting` KHÔNG gọi `syncFromDisk` re-render (FR8, gate `model.go:1074`).
   `refreshPreview`/`FocusToggle` reset hygiene → `selecting=false`.
9. **`l`/`→` no-op (FR12)**: đang chọn, `l`/`→` (OpenEntry) KHÔNG copy, KHÔNG thoát — `selecting` vẫn true,
   không record. Chỉ `y`/`enter` copy.
10. **scrollSelectionIntoView**: sau mỗi `moveSelection`, dòng visual của `selCursor` ∈ `[previewTop,
    previewTop+bodyH)` (cả wrap lẫn nowrap). Off-viewport extend đẩy `previewTop` theo.
11. **Telemetry** đúng một lần mỗi copy; log `lines`+`bytes`, KHÔNG content.
12. **Help**: `V` trong nhóm Preview của `fullHelp` (`view.go`); số nhóm fullHelp không đổi (5).
    While-selecting status hint hiện `y`/`esc`.
13. **Keybind**: `keyRune('V')` match `SelectMode`, không match binding khác; `CopySelection` bind `y`+`enter`
    riêng (KHÔNG `OpenEntry`), coexist với `Yank` (`y`) qua mode-lane (Yank chỉ !selecting & focusList;
    CopySelection chỉ khi selecting) — không double-map ngoài ý.
14. **Mouse (D13/FR13-16)**: press trong pane preview (file scrollable) + motion + release → `copySelection`
    record đúng một lần, range = dòng-press→dòng-release; **click trơn** (press+release không motion) KHÔNG
    record, `selecting` vẫn false (FR14); edge-scroll khi motion qua mép (FR15); press trên divider vẫn kéo
    divider, KHÔNG chọn (FR16). Test qua `handleMouse` với `MouseClickMsg`/`MouseMotionMsg`/`MouseReleaseMsg`
    (v2 mouse: action theo **message type**, `e.Mouse()` mang `X/Y/Button`).
15. **Visual verdict**: render preview đang chọn → highlight khối dải, de-color, nền ≠ cursor-list; hint
    bar `y copy · esc cancel`. (UI test — render-to-image + agent verdict.)
16. `go build -o lazyexplorer . && go vet ./... && go test ./...` xanh; `go test -race ./...` xanh.

## 7. Task breakdown

- [x] **T1 — Keybind.** Field `SelectMode` (`V`, "select lines") + `CopySelection` (`y`/`enter`, "copy selection")
  nhóm Preview/Misc trong `KeyMap`/`defaultKeyMap`. §5.3. *(keys.go)*
- [x] **T2 — State + vòng đời.** `selecting/selAnchor/selCursor` trên `model`; `startSelection` (từ chối
  khi `pendingWidth>0`), `moveSelection`/`moveSelectionTo`, `scrollSelectionIntoView`, `cancelSelection`,
  `copySelection`; reset trong `refreshPreview` + `FocusToggle`; poll gate `&& !m.selecting`. §5.2/§5.4. *(model.go)*
- [x] **T2b — CRITICAL race fix.** `applyPreview` set `selecting=false` trước **cả** nhánh success lẫn error
  — điểm chặn phủ render-đáp + resize + git-refresh (D11/FR7). *(model.go)*
- [x] **T3 — Dispatch.** Nhánh `if m.selecting { return m.updateSelecting(msg) }` đầu `updateNormal`;
  `updateSelecting` (copy = `y`/`enter` riêng, KHÔNG `OpenEntry`; `Tab` huỷ+đổi focus); `V`→`startSelection`
  ở focusPreview/scrollable khi chưa chọn. §5.3. *(model.go)*
- [x] **T4 — Highlight render + style.** `selectionStyle` (theme.go); nhánh tô dòng được chọn (de-color +
  nền) trong **hai** path scrollable (wrap + nowrap `renderHWindow`) của `renderPreview`. §5.5. *(view.go, theme.go)*
- [x] **T4b — Mouse drag-to-select.** `mouseDragArmed` + `srcLineAtRow` helper; ba nhánh press(arm)/motion
  (commit+edge-scroll)/release(copy) trong `handleMouse`; click-trơn không copy (FR14); không đụng
  divider/wheel (FR16). §5.6/D13. *(model.go)*
- [x] **T5 — Help surfaces + status hint.** `SelectMode` vào `fullHelp` Preview + `shortHelp` focusPreview;
  status bar hiện `y copy · esc cancel` khi `selecting`; dòng Keys `--help`. §5.3/FR10. *(view.go, main.go)*
- [x] **T6 — README note.** Một mục: `V` chọn dải dòng trong preview rồi `y` copy; giữ Shift/Option-drag
  vẫn dùng cho chọn-nhìn-thấy ở full-width. *(README.md)*
- [x] **T7 — Tests (TDD).** `preview_selection_test.go`: **keyboard** open/extend/copy (raw-not-ANSI,
  off-viewport, single-line, cancel, no-op, reset-on-change, **applyPreview-race**, resize-race,
  telemetry-once, `l`/`→`-noop); **mouse** press/motion/release copy + click-trơn-no-copy +
  edge-scroll + divider-not-disturbed (`handleMouse`); keybind test; poll-gate test; help-surface test;
  dogfood drive `V`+extend+`y` và một mouse drag, assert recorder. §6.
  *(preview_selection_test.go, keys/help tests, zz_dogfood_test.go, main_test.go)*
- [x] **T8 — Reconcile `prd-preview-copy`.** Thêm pointer "D1/D2 narrowed by prd-preview-selection" +
  thu hẹp non-goal đảo chiều (docs-sync, positive framing). *(docs/prd-preview-copy.md)*
- [x] **T9 — Verify.** `go build -o lazyexplorer . && go vet ./... && go test ./...` xanh; `-race` xanh.
  Visual verdict preview-đang-chọn để chủ dự án thực hiện. §6.

## 8. Files chạm tới

| File | Thay đổi |
|------|----------|
| `keys.go` | Field `SelectMode` (`V`) + `CopySelection` (`y`) + bindings |
| `model.go` | `selecting/selAnchor/selCursor` + `mouseDragArmed`; `startSelection`(refuse khi pendingWidth>0)/`moveSelection`/`moveSelectionTo`/`scrollSelectionIntoView`/`cancelSelection`/`copySelection`/`srcLineAtRow`; `updateSelecting`; nhánh dispatch đầu `updateNormal`; `V`→start; **`applyPreview` cancel (CRITICAL race)**; mouse press/motion/release trong `handleMouse`; reset ở `refreshPreview`+`FocusToggle`; poll gate `!m.selecting` |
| `view.go` | Nhánh highlight dòng được chọn trong `renderPreview`/`renderHWindow`; `V` vào `fullHelp`/`shortHelp`; status hint while-selecting |
| `theme.go` | `selectionStyle` (nền ≠ accent) |
| `main.go` | `helpText()` Keys line thêm `V select lines` |
| `README.md` | Note: `V` chọn dải + copy; native-drag vẫn cho full-width nhìn-thấy |
| `preview_selection_test.go` (mới) | TDD selection: open/extend/copy/cancel/no-op/reset/telemetry/no-helper |
| `zz_dogfood_test.go` | Dogfood: drive `V`+extend+`y`, assert clipboard == dải dòng |
| `docs/prd-preview-copy.md` | Reconcile: pointer amend D1/D2 + thu hẹp non-goal đảo chiều |
| `docs/prd-preview-selection.md` | PRD này |
