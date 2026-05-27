# PRD — Tích hợp Datadog để theo dõi toàn diện lazyexplorer

> Feature: gắn lazyexplorer vào ba sản phẩm Datadog — **Static Code Analysis**
> + **SAST** ở CI, và một lớp **RUM-equivalent** (session / view / action telemetry)
> ở runtime gửi qua Datadog Logs HTTP intake — để team có signal end-to-end về
> chất lượng code lúc merge và hành vi thực tế của user lúc chạy, **mà không
> thêm panel / mode / keybind nào vào TUI**.

Status: **accepted** · Author: PRD-drafting session · Ngày: 2026-05-27 · Shipped: 2026-05-28

---

## 1. Bối cảnh & vấn đề

Hôm nay lazyexplorer là một local TUI sống cạnh coding agent (xem root `CLAUDE.md`,
mục *Goal & Positioning*). Pipeline render preview đã đầy đủ engineering tốt —
markdown async (`docs/adr-async-markdown-render.md`), code highlight chroma
(`docs/prd-code-highlight.md`), poll-loop watch (`model.go:117`), drag-divider
defer (`model.go:378`). Nhưng **không có một mũi kim quan sát nào** xuyên qua
biên process:

- File `*_test.go` chứng minh code đúng theo *spec*, không chứng minh nó đúng
  *trên máy user thật*. Một preview render fail (`applyPreview` `model.go:299`,
  FR5 của markdown PRD) khi xảy ra ở user → status hint biến mất sau lần điều
  hướng kế, team không bao giờ biết.
- Khi cuộn qua nhiều `.md`, gen-counter (`renderGen` `model.go:73`) bỏ kết quả
  cũ — đúng theo D2 của `adr-async-markdown-render.md` — nhưng **mức** bỏ thật
  là bao nhiêu phần trăm goroutine bị wasted trên hardware/terminal user, ta
  chưa biết.
- Repo có TDD discipline (`CLAUDE.md` *Testing*), nhưng **không có security
  gate tự động** ở PR. Một dòng `os.Rename(old, dst)` (`model.go:640`) trong
  rename mode đã có guard `filepath.Base(newName) != newName` chống traversal;
  một SAST scanner sẽ phát hiện khi guard này bị tháo. Hiện tại không có
  scanner nào canh giữ.
- Một lỗi logic nhỏ trong `withinRoot` (`fs.go`) hoặc `recoverVanishedCwd`
  (`model.go:177`) có thể trượt qua review. Static Code Analysis (linter rules
  managed) bắt được các pattern Go đáng ngờ (`govet`, `staticcheck`,
  `errcheck`, nil-deref) trước khi merge.

**Giả định nền tảng (phải nêu thẳng):** PRD này cho rằng lazyexplorer **sẽ
được distribute** ra ≥ vài chục developer (open-source hoặc nội bộ team). Với
một user duy nhất, ROI của runtime telemetry âm — ship CI-only (SAST + Static
Code Analysis) là đủ. Reviewer chốt: nếu distribution chưa chắc, **defer §5.3
RUM-equivalent sang v2**, giữ §5.5–§5.6 (CI gates) ở v1. PRD này thiết kế hai
nhánh tách biệt được, không khoá nhau.

## 2. Goal (1 câu)

Khi v1 ship, mỗi PR vào `main` được Datadog Static Code Analysis + SAST quét
tự động (block merge nếu có HIGH/CRITICAL), và — khi user enable opt-in flag
— mỗi phiên chạy lazyexplorer phát ra một stream event RUM-equivalent
(`session.start`, `view.change`, `action.preview_rendered`, `error.render_fail`,
`session.end`) gửi qua Datadog Logs HTTP intake để team thấy được hành vi và
lỗi thực ở user thật, **mà không thêm panel / mode / keybind nào vào TUI**.

**Non-goal làm rõ:**

- KHÔNG dùng Datadog **RUM Browser/Mobile SDK** literal — chúng không hỗ trợ
  Go CLI/TUI (verify §5.1 D3). Lớp runtime đi qua **Logs HTTP intake** và
  Custom Metrics API, mô hình hoá khái niệm session/view/action của RUM.
- KHÔNG bật telemetry **default-on**. Local file explorer thấy filename/path
  user — đây là PII tiềm tàng → opt-in tường minh qua env var.
- KHÔNG **block startup** chờ network. Telemetry hỏng (offline, DNS, 5xx)
  không bao giờ làm app đơ, không bao giờ block một frame UI.
- KHÔNG thêm UI cho telemetry: không panel, không mode, không keybind, không
  chip status. Một dòng duy nhất ở `--help` (xem §5.7) là toàn bộ surface.
- KHÔNG implement **Datadog APM tracing** ở v1: lazyexplorer là single-process,
  không cross-service → APM ROI thấp. Custom Metrics đủ cho perf signal.

## 3. Quyết định đã chốt

| # | Quyết định | Giá trị | Lý do |
|---|-----------|---------|-------|
| D1 | Deliverable | **PRD + ADR (mục §5.3 sẽ tách ra `adr-telemetry-boundary.md` khi implement)** + task | house style: spec trước khi đụng code; lớp telemetry boundary đủ load-bearing cho riêng một ADR |
| D2 | Phạm vi 3 product | **Static Code Analysis (CI)** + **SAST (CI)** + **RUM-equivalent (runtime, opt-in)** | đúng scope user chọn qua interview; CI gates đứng độc lập, RUM-equivalent gate-bởi-flag |
| D3 | Cách "RUM" cho TUI | **Logs HTTP intake** + **Custom Metrics API** mô hình hoá session/view/action — KHÔNG dùng RUM SDK | Datadog RUM SDK chỉ có Browser/iOS/Android/Flutter/RN (verify §5.1); HTTP intake là API documented, dùng được từ bất kỳ Go binary |
| D4 | Mặc định opt-in | **OFF**. Bật bằng env var `LE_TELEMETRY=1` + một trong `DD_API_KEY` hoặc `DD_LOGS_URL` | local FS tool thấy path/filename user → PII; opt-in tường minh là hợp đồng đúng |
| D5 | Redaction | **Path → hash SHA-256 (8 bytes hex prefix)**; **filename → extension only** (`.md`, `.go`, `(noext)`). Không bao giờ gửi raw path / raw filename | đủ để cluster theo loại file mà không leak project structure |
| D6 | Vận chuyển | **Non-blocking buffered channel** (`chan event`, cap 256) drain bởi 1 goroutine gửi batch mỗi 5s hoặc khi đầy; flush ở `tea.Quit` với timeout 1s | render goroutine và `Update` không bao giờ block trên I/O mạng; lost-on-quit chấp nhận được — telemetry không phải audit log |
| D7 | Backend | **Datadog HTTP intake API** (`https://http-intake.logs.datadoghq.com/api/v2/logs`) với `DD-API-KEY` header. Site (US1/US3/EU…) qua `DD_SITE` env, default `datadoghq.com` | API thuần REST, không cần Datadog agent local — chạy được trên user laptop bất kỳ |
| D8 | Static Code Analysis runner | **GitHub Action `DataDog/datadog-static-analyzer-github-action@v1` (pin SHA lúc T7)** + config `static-analysis.datadog.yml` ở repo root | Datadog-managed action; SHA pin theo Github action security best practice (giả định verify @v1 còn maintained khi implement) |
| D9 | SAST runner | **`DataDog/datadog-ci`** chạy `datadog-ci sast` step trong workflow CI; ruleset `go-security` | cùng pipeline với Static Code Analysis, một secret bundle (`DD_API_KEY`, `DD_APP_KEY`) cho cả hai |
| D10 | Severity gate | **HIGH + CRITICAL block PR merge**; MEDIUM/LOW comment-only, không block | nhịp dev không bị chặn bởi nit; vấn đề thật chặn ngay |
| D11 | Test fixture | **Một file `testdata/sast_canary.go`** chứa pattern unsafe đã biết (vd `exec.Command(userInput)`) — bị Go build tag `//go:build sastcanary` cô lập khỏi prod build | hợp đồng "scanner thật sự chạy" được test bằng cách thấy canary bị flag |
| D12 | Telemetry init point | **`telemetry.Init(opts)` gọi ở `main.go` trước `tea.NewProgram`** (`main.go:49`); shutdown gọi sau `p.Run()` | một điểm bật/tắt; không rò goroutine khi tắt; song song với pattern `detectRenderStyle` resolve-một-lần (`main.go:18`) |

## 4. Functional requirements

### Nhánh CI (luôn-bật, không gate flag)

- **FR1** — Mọi PR target `main` chạy `datadog-static-analyzer-github-action`
  trên toàn repo Go files. Findings được post lên PR dạng GitHub Annotations
  ở dòng cụ thể.
- **FR2** — Mọi PR target `main` chạy `datadog-ci sast` với ruleset
  `go-security`. Findings cùng kênh annotation với FR1.
- **FR3** — Một finding **HIGH** hoặc **CRITICAL** từ FR1 hoặc FR2 → CI job
  fail → PR không merge được (branch protection rule, T8).
- **FR4** — Finding **MEDIUM** / **LOW** post comment lên PR nhưng KHÔNG fail
  job — không chặn nhịp dev.
- **FR5** — File `testdata/sast_canary.go` (D11) bị scanner flag MỖI lần CI
  chạy — nếu một ngày không bị flag nữa, có nghĩa scanner đang im lặng → CI
  job thứ ba `verify-sast-live` (T7) fail explicit.

### Nhánh runtime RUM-equivalent (opt-in, gate flag `LE_TELEMETRY=1`)

- **FR6** — Khi `LE_TELEMETRY=1` **vắng mặt** hoặc khác `1`/`true`/`yes`,
  lazyexplorer KHÔNG mở socket, KHÔNG đọc `DD_API_KEY`, KHÔNG khởi tạo
  goroutine telemetry. Hành vi bytes-for-bytes y hệt v0 hiện tại.
- **FR7** — Khi telemetry bật và `DD_API_KEY` thiếu/rỗng, lazyexplorer chạy
  bình thường, in **một dòng** ở stderr lúc startup: `lazyexplorer: telemetry
  enabled but DD_API_KEY missing — disabled`. Không retry, không crash.
- **FR8** — Khi telemetry bật và init thành công, lần đầu `tea.NewProgram`
  được gọi, một event `session.start` được enqueue, chứa: `session_id`
  (uuidv4 sinh runtime), `version` (build-time), `go_version`, `os`,
  `arch`, `term` (giá trị `$TERM`, không phải nội dung terminal), `color_profile`
  (giá trị từ `detectRenderStyle` `main.go:18`).
- **FR9** — Mỗi lần `m.cursor` đổi (`refreshPreview` `model.go:189` chạy), một
  event `view.change` được enqueue, chứa: `session_id`, `entry_kind`
  (`dir`/`file`/`parent`), `ext_class` (xem D5: extension hoặc `(noext)` hoặc
  `(binary)`), `cwd_depth` (số segment dưới jail root — `0` cho `m.root`),
  KHÔNG có path/filename.
- **FR10** — Mỗi `previewRenderedMsg` được apply (`applyPreview` `model.go:294`),
  một event `action.preview_rendered` enqueue với: `session_id`, `renderer`
  (`markdown`/`code`/`image`/`plain`; lấy từ registry — xem `fs.go` D1 của
  `adr-preview-renderer-registry.md`), `width`, `lines`, `success` (bool tương
  ứng `msg.err == nil`), `duration_ms` (đo từ lúc `syncPreview` dispatch tới
  lúc `applyPreview` được gọi — xem §5.4).
- **FR11** — Mỗi `applyPreview` thấy `msg.err != nil` (FR5 của markdown PRD,
  FR6 của code PRD), một event `error.render_fail` enqueue: `session_id`,
  `renderer`, `error_class` (loại lỗi — `glamour`/`chroma`/`io`/`other`),
  KHÔNG có error message raw (có thể chứa path).
- **FR12** — Mỗi tick `tickMsg` (`model.go:321`, mặc định 1s) mà `m.cursor`
  không đổi và không có `previewRenderedMsg` đến, KHÔNG enqueue event nào —
  steady-state tick im lặng. Bảo toàn lý do "dirt cheap" của
  `pollInterval = time.Second` (`main.go:15`).
- **FR13** — Khi user gõ `q`/`ctrl+c` (`model.go:505`), trước khi `tea.Quit`
  trả về, telemetry **flush** event đang queue với timeout cứng 1s. Quá 1s
  → drop phần còn lại, exit. Không bao giờ kéo dài quit > 1s vì telemetry.
- **FR14** — Telemetry channel đầy (cap 256) → **drop event mới** (counter
  `dropped` được attach vào event kế nếu có chỗ). KHÔNG block `Update`, KHÔNG
  block render goroutine, KHÔNG grow channel.
- **FR15** — Network gửi thất bại (timeout, DNS, 5xx, 4xx) → log một dòng
  stderr **một lần mỗi 60s** ở dạng `lazyexplorer: telemetry post failed
  (will retry silently)`; bỏ batch hiện tại, tiếp tục batch kế. KHÔNG retry
  cùng batch (event đã cũ; drop là đúng).
- **FR16** — User unset `LE_TELEMETRY` giữa chừng (env không re-read) → state
  giữ theo lúc startup; chấp nhận giới hạn này (consistent với
  `detectRenderStyle` cũng resolve một lần, `main.go:18`).

## 5. Technical design

> Kim chỉ nam: **giữ TUI bytes-for-bytes không đổi khi telemetry off** và
> **một boundary duy nhất** (`telemetry.go`) tách network/serialization ra
> khỏi `model.go`/`view.go`/`fs.go`. `Update` không bao giờ import package
> `net/http`. Mọi gọi telemetry từ render path đi qua một interface 1-method
> (`Record(event)`) — no-op stub khi off, real client khi on. Đây là Functional
> Core / Imperative Shell (root `CLAUDE.md` §4): logic preview thuần, side
> effect đẩy ra biên.

### 5.1 Dependency

Verify trạng thái Datadog SDK cho Go runtime — **giả định, cần xác nhận lúc T1**:

- `github.com/DataDog/datadog-api-client-go/v2` — generated client cho REST API,
  có `LogsApi.SubmitLog` và `MetricsApi.SubmitMetrics`. Đây là path em chọn.
- `github.com/DataDog/datadog-go/v5` (DogStatsD UDP client) — yêu cầu Datadog
  agent local → KHÔNG fit user laptop bất kỳ (D7), defer khỏi v1.
- Datadog không publish **RUM SDK cho Go** (verify pkg.go.dev lúc T1, kiểm
  `https://docs.datadoghq.com/real_user_monitoring/`). Đây là gốc của D3.
- `github.com/google/uuid` v1.x cho `session_id` (FR8) — dep nhỏ, ổn định.

Nếu T1 phát hiện `datadog-api-client-go` quá nặng (sinh từ OpenAPI, ~1MB sau
strip), fallback **một file `client.go` thuần `net/http` ~80 dòng** post JSON
trực tiếp tới `https://http-intake.logs.datadoghq.com/api/v2/logs` với
`DD-API-KEY` header. Sample request shape (đã xác nhận từ docs Datadog
*"HTTP API → Logs → Send logs"*, **giả định format ổn định lúc implement**):

```json
POST /api/v2/logs HTTP/1.1
Host: http-intake.logs.datadoghq.com
DD-API-KEY: ...
Content-Type: application/json

[{
  "ddsource": "lazyexplorer",
  "ddtags":   "env:user,version:0.1.0",
  "hostname": "<redacted-host-hash>",
  "service":  "lazyexplorer-tui",
  "message":  "view.change",
  "session_id": "uuid…",
  "entry_kind": "file",
  "ext_class":  ".go"
}]
```

`go mod tidy` sau T1.

### 5.2 Telemetry boundary (`telemetry.go`)

Một file mới, một interface, một implementation thật + một no-op:

```go
package main

// Recorder is the only telemetry surface the rest of the app talks to.
// noopRecorder is the default; realRecorder is wired by Init() when
// LE_TELEMETRY=1 and DD_API_KEY is present. Update / refreshPreview /
// applyPreview never import net/http — they call Record on a Recorder.
type Recorder interface {
    Record(name string, fields map[string]any)
    Shutdown(timeout time.Duration) // flushes; safe to call on noop
}

type noopRecorder struct{}
func (noopRecorder) Record(string, map[string]any)        {}
func (noopRecorder) Shutdown(time.Duration)               {}

// realRecorder owns the buffered channel and the drainer goroutine.
// Record is non-blocking: full channel → counted into dropped, dropped is
// attached to the next event that DOES fit.
type realRecorder struct {
    ch      chan event
    dropped atomic.Uint64
    sess    string // uuid; set once in Init
    base    map[string]any // base fields merged into every Record
    client  httpPoster
    done    chan struct{}
}
```

Init returns the concrete recorder; caller stores it on `model.tel`:

```go
func InitTelemetry() Recorder {
    if !envFlag("LE_TELEMETRY") { return noopRecorder{} }
    key := os.Getenv("DD_API_KEY")
    if key == "" {
        fmt.Fprintln(os.Stderr, "lazyexplorer: telemetry enabled but DD_API_KEY missing — disabled")
        return noopRecorder{}
    }
    return newRealRecorder(key, datadogSite(), buildVersion)
}
```

**Vì sao interface trên `model` chứ không global var:** test inject mock dễ,
race-detector clean (mỗi `model` instance giữ recorder của nó), không state
global ngầm. Đúng tinh thần root `CLAUDE.md` §5 *Explicit over implicit*.

### 5.3 Event surface — instrumentation chỗ nào

Bảng dưới là **mọi chỗ** có call `m.tel.Record(...)`. Không có chỗ nào khác.
Mỗi call đứng tự nó, no-op khi telemetry off (FR6).

| Event | Surface (file:line dự kiến) | Trigger | Fields ngoài base |
|-------|----------------------------|---------|-------------------|
| `session.start` | `main.go` sau `InitTelemetry`, trước `p.Run()` | một lần lúc khởi động | `version`, `go_version`, `os`, `arch`, `term`, `color_profile` |
| `view.change` | `model.go` đuôi `refreshPreview` (`model.go:189`) | cursor đổi → preview reload | `entry_kind`, `ext_class`, `cwd_depth` |
| `action.preview_rendered` | `model.go` cuối `applyPreview` (`model.go:294`) nhánh `err == nil` | render xong | `renderer`, `width`, `lines`, `duration_ms` |
| `error.render_fail` | `model.go` `applyPreview` nhánh `err != nil` (`model.go:299`) | render lỗi (FR5/FR6 markdown/code PRD) | `renderer`, `error_class` |
| `session.end` | `main.go` defer sau `p.Run()` | luôn — bao gồm cả crash đường happy | `duration_ms`, `views_total`, `renders_total`, `errors_total` |

**Câu hỏi quan trọng — vì sao `view.change` chạy trong `refreshPreview` an
toàn:** `refreshPreview` chạy **trong Update goroutine** (`model.go:189` được
gọi từ `updateNormal` `model.go:511`, `descend` `model.go:573`, `ascend`
`model.go:598`, …). `Record` là non-blocking (channel send no-block khi đầy
→ drop, FR14), nên không kéo Update ra khỏi single-goroutine invariant của
Bubbletea. Cùng lý do `Record` từ `applyPreview` an toàn.

**Câu hỏi quan trọng — `duration_ms` của `action.preview_rendered`:** đo từ
lúc `syncPreview` (`model.go:257`) bump `renderGen` tới lúc `applyPreview`
được gọi. Để bắt đo, thêm field `renderStartedAt time.Time` trên `model`
(set ở `syncPreview` khi quyết định dispatch, `model.go:279`), `applyPreview`
đọc và clear. **Chỉ đo khi `tel != noopRecorder`** — bằng cách `syncPreview`
hỏi `m.tel != nil && !isNoop(m.tel)`; với noop, field giữ zero, không tốn
1 syscall `time.Now()`. (`isNoop` là type assertion đơn giản hoặc bool flag
trên model `telemetryActive bool` set ở Init — implementer chọn lúc T3,
đo perf trước nếu nghi.)

### 5.4 Privacy & redaction (D5 chi tiết)

**Bất biến cứng:** không event nào leak ra ngoài chứa:

1. Raw path tuyệt đối (vd `/Users/firegroup/projects/.../README.md`).
2. Raw filename (vd `secrets.env`).
3. Nội dung file (preview bytes).
4. Username / hostname raw.

Cách enforce:

- **Path → `cwd_depth`** (`int`, FR9). Không gửi path. Implementation:
  `strings.Count(strings.TrimPrefix(m.cwd, m.root), string(os.PathSeparator))`.
- **Filename → `ext_class`** (`string`, FR9). Pure function `extClass(name)`:
  `strings.ToLower(filepath.Ext(name))` nếu non-empty, `"(noext)"` cho file
  không đuôi, `"(parent)"` cho synthetic `..` entry (`model.go:108`).
- **Hostname → `hostname_hash`** trong `base` fields: SHA-256 của
  `os.Hostname()`, lấy 8 byte hex đầu. Đủ để cluster session từ cùng máy mà
  không leak ai. Khi `os.Hostname()` lỗi → `"unknown"`.
- **Error → `error_class` enum**, không phải `err.Error()`. Trong
  `applyPreview` (`model.go:299`), một `switch` map từ kiểu lỗi sang
  `"glamour"|"chroma"|"io"|"other"`. Chuỗi error gốc giữ trong status bar
  cho user (đúng UX hiện tại) nhưng KHÔNG ra mạng.

**Tự test bất biến (T9):** unit test feed một event qua serializer và
`assert` JSON kết quả KHÔNG match regex `^/|^[A-Z]:\\|secret|password|key=`.
Đây là cách bất biến không drift theo thời gian.

### 5.5 Static Code Analysis trên CI (D8)

Workflow file mới `.github/workflows/datadog-static-analysis.yml`:

```yaml
name: Datadog Static Analysis
on:
  pull_request:
    branches: [main]
jobs:
  static-analysis:
    runs-on: ubuntu-latest
    permissions: { contents: read, pull-requests: write }
    steps:
      - uses: actions/checkout@<sha-pin>
      - uses: DataDog/datadog-static-analyzer-github-action@<sha-pin> # T7 pin SHA
        with:
          dd_api_key: ${{ secrets.DD_API_KEY }}
          dd_app_key: ${{ secrets.DD_APP_KEY }}
          dd_site:    datadoghq.com
          cpu_count:  2
          enable_performance_statistics: false
```

Config repo-level `static-analysis.datadog.yml`:

```yaml
rulesets:
  - go-best-practices
  - go-security
  - go-inclusive
ignore:
  - tmp/                 # reference clones (root CLAUDE.md §Reference Code)
  - testdata/sast_canary.go  # intentional unsafe pattern (D11)
```

**`tmp/` ignore là load-bearing:** mục *Reference Code* của root `CLAUDE.md`
nói rõ `tmp/` là clone đọc-tham-khảo, KHÔNG compile vào binary. Scanner
quét nó sẽ tạo noise hàng nghìn finding không actionable cho ta.

### 5.6 SAST trên CI (D9)

Cùng workflow hoặc một workflow song song
`.github/workflows/datadog-sast.yml`:

```yaml
name: Datadog SAST
on:
  pull_request:
    branches: [main]
jobs:
  sast:
    runs-on: ubuntu-latest
    permissions: { contents: read, pull-requests: write, security-events: write }
    steps:
      - uses: actions/checkout@<sha-pin>
      - uses: DataDog/datadog-ci@<sha-pin>  # T7 pin SHA (giả định action tồn tại; fallback npm install -g @datadog/datadog-ci nếu không có composite action)
      - run: datadog-ci sast upload --service lazyexplorer .
        env:
          DD_API_KEY: ${{ secrets.DD_API_KEY }}
          DD_APP_KEY: ${{ secrets.DD_APP_KEY }}
          DD_SITE:    datadoghq.com
```

**`verify-sast-live` job thứ ba (FR5/T7):** chạy ngay sau SAST,
`grep -c 'sast_canary' <(curl …)` qua Datadog Findings API — nếu kết quả < 1
fail. Đảm bảo scanner không silently broken.

### 5.7 Config & secrets

| Env var | Bắt buộc khi telemetry on? | Default | Tác dụng |
|---------|---------------------------|---------|---------|
| `LE_TELEMETRY` | — | unset (off) | `1`/`true`/`yes` (case-insens.) → on |
| `DD_API_KEY` | có | — | thiếu → off + stderr 1 dòng (FR7) |
| `DD_SITE` | không | `datadoghq.com` | `us3.datadoghq.com`, `datadoghq.eu`, … |
| `DD_LOGS_URL` | không | derived từ `DD_SITE` | full URL override (cho sandbox) |

**CI secrets** ở GitHub repo settings: `DD_API_KEY` + `DD_APP_KEY` (write).
Doc onboard contributor để clone fork hiểu rằng CI gates sẽ skip trên fork
PR không có secret access (graceful: action exit 0 với warning, không fail
PR). Datadog action mặc định behavior này — **giả định, T7 verify**.

### 5.8 Failure modes — bảng đầy đủ

| Tình huống | Hành vi |
|------------|--------|
| `LE_TELEMETRY` unset/0 | noop recorder, 0 syscall thêm, 0 goroutine thêm (FR6) |
| `LE_TELEMETRY=1` + `DD_API_KEY` thiếu | noop + stderr 1 dòng (FR7) |
| Network offline lúc startup | realRecorder vẫn khởi tạo; batch đầu fail → FR15; in-app không bị ảnh hưởng |
| Network gián đoạn giữa session | batch trong lúc đứt rớt; recover khi online; rate-limited stderr (FR15) |
| Datadog 4xx (key sai) | realRecorder thấy 401/403 → tự `Shutdown` + stderr 1 dòng *"telemetry api key rejected — disabled"* (T5). Tránh spam log mỗi 5s |
| Channel đầy 256 (user di cursor cực nhanh) | drop event mới, tăng `dropped` counter, gắn vào event kế (FR14) |
| User `kill -9` lazyexplorer | session.end mất; chấp nhận được. `session.start` đã gửi → Datadog query có thể detect missing-end làm proxy crash signal (post-v1 dashboard) |
| `tea.Quit` → flush 1s timeout vượt | drop phần còn queue, exit (FR13) |
| SAST scanner downtime ở CI | action fail → PR không merge được → engineer report; đây là correct fail-safe (D10/FR3) |
| CI fork PR không có DD secret | action skip với warning, không fail (D8/§5.7 giả định) |

### 5.9 Đã cân nhắc & **defer khỏi v1**

- **Datadog APM (distributed tracing):** ROI thấp với single-process TUI (Non-goal). Khi lazyexplorer có future RPC/IPC component → reconsider.
- **DogStatsD UDP cho metrics:** yêu cầu Datadog agent local — không fit "user laptop bất kỳ" (D7). HTTP intake đủ cho v1.
- **Datadog Continuous Profiler:** Go profiler có giá trị khi có hot loop server-side. lazyexplorer hot loop là render goroutine — đã được async, không phải bottleneck production. Defer.
- **Datadog RUM Browser SDK qua một WASM bridge:** phi lý cho TUI. Defer vĩnh viễn.
- **Datadog Synthetic CLI test:** một robot chạy lazyexplorer headless và assert frame output → giá trị thật nhưng infra phức tạp (cần PTY emulator). Sau khi project có CI runner ổn → reconsider v2.
- **Sampling client-side:** hôm nay drop chỉ-khi-đầy (FR14). Sampling theo % chỉ cần khi event volume > 100/s/user — chưa có signal. Defer.
- **`LE_TELEMETRY_VERBOSE` debug payload echo:** stderr-print mỗi batch trước send để soi shape lúc dev. Giá trị thật khi ops báo "không thấy event" và muốn xác nhận client-side đang gửi gì. Defer — debug path chưa có pain point, thêm code-surface không chính đáng cho v1.
- **`mtime` invalidation event:** không liên quan telemetry scope; out-of-scope.
- **Per-event encryption above TLS:** Datadog API đã TLS; over-engineer với content đã redact ở §5.4. Defer.
- **Distribution: ship binary signed cho macOS/Windows:** out-of-scope, là build/release concern; chỉ ảnh hưởng PRD này gián tiếp qua giả định "lazyexplorer được distribute".
- **Realtime Datadog dashboard JSON-as-code (dashboard repo):** ship sau khi production stable >1 tuần để biết shape thực của data; defer.

## 6. Acceptance criteria

### Gherkin

```gherkin
Feature: Datadog static analysis and SAST guard every PR into main

  Scenario: A clean PR passes both scanners
    Given the working tree has no HIGH or CRITICAL findings
    When I open a pull request targeting main
    Then the Datadog Static Analysis job succeeds
    And the Datadog SAST job succeeds
    And the PR is mergeable

  Scenario: A PR introducing a HIGH severity issue is blocked
    Given a pull request adds code flagged HIGH by Datadog SAST
    When CI runs
    Then the SAST job fails
    And the PR cannot be merged until the finding is fixed or downgraded by Datadog config

  Scenario: A PR with only MEDIUM findings still merges
    Given a pull request adds code flagged MEDIUM only
    When CI runs
    Then findings appear as PR comments
    And the SAST job still succeeds
    And the PR is mergeable

  Scenario: The SAST canary keeps the scanner honest
    Given the repo contains the SAST canary fixture file
    When CI runs Datadog SAST
    Then the canary is flagged at least once
    And the live-scanner verification job succeeds

Feature: Opt-in runtime telemetry mirrors Datadog RUM semantics

  Background:
    Given lazyexplorer is launched in a project directory

  Scenario: Telemetry is off by default
    Given the LE_TELEMETRY environment variable is unset
    When the user runs lazyexplorer normally
    Then no network connection to Datadog is opened
    And no extra goroutine for telemetry is spawned
    And the on-screen behaviour is byte-for-byte identical to a build without telemetry

  Scenario: Telemetry enabled without an API key gracefully disables itself
    Given LE_TELEMETRY is "1"
    And DD_API_KEY is unset
    When the user runs lazyexplorer
    Then a single stderr line reports telemetry is disabled
    And the explorer keeps working unchanged
    And no further telemetry message appears

  Scenario: Each user session emits a start and an end event
    Given telemetry is enabled with a valid key
    When the user launches lazyexplorer and quits with q
    Then a "session.start" event was sent with version, OS, arch and a session id
    And a "session.end" event was sent with duration and counters before the process exits
    And the quit took no longer than the configured flush timeout plus normal teardown

  Scenario: Navigating files emits redacted view events
    Given telemetry is enabled
    When I move the cursor across several files in the listing
    Then each cursor change produces one "view.change" event
    And no event contains the raw filename
    And no event contains the raw path
    And the event reports only an extension class and a depth under the jail root

  Scenario: A successful preview render is reported with timing
    Given telemetry is enabled
    When I select a markdown file the preview successfully renders
    Then one "action.preview_rendered" event is sent with renderer "markdown" and a positive duration
    And the user-visible preview is unchanged from a no-telemetry build

  Scenario: A failed render is reported without leaking error text
    Given telemetry is enabled
    And a renderer would fail on the next selected file
    When I select that file
    Then an "error.render_fail" event is sent with an error class enum
    And no event contains the raw error message
    And the in-app status hint still shows the human-readable fallback message

  Scenario: Telemetry never blocks the UI
    Given telemetry is enabled
    And the Datadog endpoint is unreachable
    When I navigate, scroll and quit
    Then every keystroke is acknowledged within the normal frame budget
    And the explorer quits within the flush timeout
    And a single stderr line reports the post failure at most once per minute

  Scenario: A flood of events drops the surplus instead of blocking
    Given telemetry is enabled
    When the user moves the cursor faster than the drainer can send
    Then the explorer stays responsive
    And surplus events are counted as dropped and reported on the next event that fits
    And no event is silently lost without being counted
```

### Checklist verify

1. Đặt một PR introduce một dòng `exec.Command(os.Args[1])` (HIGH/CRITICAL pattern) → CI SAST job fail, PR merge button bị block.
2. Đặt một PR introduce một typo `for i := 0; i < len(s); i++` ở chỗ không cần (style MEDIUM) → CI pass, comment xuất hiện inline.
3. `testdata/sast_canary.go` luôn bị scanner flag mỗi CI run; nếu không, `verify-sast-live` job fail.
4. `LE_TELEMETRY=` unset → `strace`/`dtruss` lazyexplorer trên một thư mục thật KHÔNG thấy syscall `connect` tới `:443`. (Đặc thù platform; test tay là đủ.)
5. `LE_TELEMETRY=1 DD_API_KEY=fake-key` → stderr in **đúng một** dòng "*api key rejected — disabled*" sau lần post đầu thất bại, không spam.
6. `LE_TELEMETRY=1 DD_API_KEY=<valid sandbox key>` → trong Datadog Logs explorer, query `service:lazyexplorer-tui` thấy `session.start`, ≥1 `view.change`, ≥1 `action.preview_rendered`, `session.end` cùng `session_id`.
7. Trong cùng test ở (6), inspect raw log payload: KHÔNG có ký tự `/` trong field path/filename, KHÔNG có chữ "secret"/"password" trong payload.
8. Test unit `telemetry_test.go` feed 300 event vào channel cap 256, assert `dropped >= 44` và recorder không deadlock.
9. Test unit `payload_redaction_test.go` ép `extClass`, `cwdDepth`, `hostnameHash` qua bộ input đủ corner case (path không đuôi, path Windows, path UTF-8, hostname rỗng) — assert KHÔNG output nào leak raw input.
10. `go build -o lazyexplorer . && go vet ./... && go test ./...` xanh.
11. `go test -race ./...` xanh — recorder concurrent với render goroutine không có race.
12. Visual verdict (theo `oh-my-claudecode:visual-verdict` workflow trong root `CLAUDE.md` *Testing*) so 2 frame: telemetry off vs telemetry on + offline. **Frame byte-identical** (FR6/FR14).

## 7. Task breakdown

- [ ] **T1 — Dependency.** Thêm `github.com/DataDog/datadog-api-client-go/v2` + `github.com/google/uuid`; verify tại pkg.go.dev rằng không có RUM SDK cho Go (chốt D3); `go mod tidy`. Nếu client-go > 1MB strip → fallback `net/http` thuần (§5.1). *(go.mod, go.sum)*
- [ ] **T2 — Recorder boundary.** `telemetry.go`: interface `Recorder`, `noopRecorder`, `realRecorder` skeleton; `InitTelemetry()` đọc env (§5.7); `event` struct + serialization tới Datadog Logs JSON shape (§5.1). *(telemetry.go, telemetry_test.go)*
- [ ] **T3 — Wire model.** Thêm field `tel Recorder` lên `model` (`model.go:46`); `newModel` chấp nhận `Recorder` argument (test inject noop); `Update` không thay đổi signature. *(model.go, main.go)*
- [ ] **T4 — Instrumentation surface.** Đặt 4 call `m.tel.Record(...)` đúng vị trí §5.3: `session.start` ở `main.go`, `view.change` ở `refreshPreview` (`model.go:189`), `action.preview_rendered` + `error.render_fail` ở `applyPreview` (`model.go:294`); `session.end` ở defer của `main`. Cộng `renderStartedAt time.Time` trên `model` cho `duration_ms` (§5.3). *(main.go, model.go)*
- [ ] **T5 — Drainer + transport.** `realRecorder` goroutine: drain channel, batch 5s/đầy, POST tới `https://http-intake.<DD_SITE>/api/v2/logs` với header `DD-API-KEY`. 401/403 → self-shutdown + stderr 1 dòng (§5.8). 5xx/timeout → rate-limited stderr (FR15). Drop-on-full (FR14). *(telemetry.go, telemetry_test.go)*
- [ ] **T6 — Redaction helpers.** `extClass(name)`, `cwdDepth(root, cwd)`, `hostnameHash()`, `errorClass(err)`; unit test feed corner case (§5.4 self-test). *(telemetry.go, payload_redaction_test.go)*
- [ ] **T7 — CI workflows.** `.github/workflows/datadog-static-analysis.yml`, `.github/workflows/datadog-sast.yml`, `.github/workflows/verify-sast-live.yml`, `static-analysis.datadog.yml`. SHA pin mọi action ref (§5.5). Add `DD_API_KEY` + `DD_APP_KEY` secret ở repo settings (handoff item — ngoài code). *(.github/workflows/, static-analysis.datadog.yml)*
- [ ] **T8 — Branch protection.** Bật rule "Require status checks": chọn `Datadog Static Analysis`, `Datadog SAST`, `Verify SAST live` (handoff item — admin GitHub, ngoài code). *(repo settings — không phải file)*
- [ ] **T9 — Canary fixture.** `testdata/sast_canary.go` build-tag `sastcanary` chứa `exec.Command(os.Args[0])` + un-validated `os.Open(os.Getenv("X"))`; `static-analysis.datadog.yml` ignore (D11/§5.5). *(testdata/sast_canary.go, static-analysis.datadog.yml)*
- [ ] **T10 — Flush on quit.** Wire `defer m.tel.Shutdown(time.Second)` trong `main()` sau `p.Run()` (FR13). Verify với `time` command rằng `q` exit < 1.2s ngay cả khi DNS đen lỗ. *(main.go)*
- [ ] **T11 — Off-default verify.** Test bytes-identity: chạy `lazyexplorer` không telemetry, dump frame qua `zz_dump_test.go` harness (root `CLAUDE.md` *UI tests*); chạy lại với `LE_TELEMETRY=1 DD_API_KEY=` (key rỗng → fallback noop) → assert hai dump byte-identical. *(zz_dump_test.go hoặc test riêng)*
- [ ] **T12 — Doc hand-off.** README section *Telemetry (opt-in)* — không thêm UI doc, chỉ env var table (§5.7) + một câu "this respects D5 — paths/filenames never leave your machine". *(README.md)*
- [ ] **T13 — Verify.** `go build -o lazyexplorer . && go vet ./... && go test ./...` xanh; `go test -race ./...` xanh; visual verdict hai frame (off vs on-offline) byte-identical; chạy tay AC §6 (1–9); query Datadog Logs explorer xác nhận shape JSON khớp §5.1.

## 8. Files chạm tới

| File | Thay đổi |
|------|---------|
| `go.mod` / `go.sum` | + `github.com/DataDog/datadog-api-client-go/v2` (hoặc 0 dep nếu fallback `net/http`); + `github.com/google/uuid` |
| `telemetry.go` (mới) | `Recorder` interface; `noopRecorder`; `realRecorder` + drainer goroutine; `InitTelemetry`; `event` struct; redaction helpers (`extClass`, `cwdDepth`, `hostnameHash`, `errorClass`); HTTP poster |
| `main.go` | + `m.tel = InitTelemetry()` trước `tea.NewProgram` (`main.go:49`); `m.tel.Record("session.start", …)`; `defer m.tel.Record("session.end", …); m.tel.Shutdown(time.Second)` sau `p.Run()` |
| `model.go` | + field `tel Recorder` (`model.go:46`); + field `renderStartedAt time.Time` cho duration metric; `refreshPreview` cuối hàm `m.tel.Record("view.change", …)` (`model.go:189`); `applyPreview` cuối hàm `m.tel.Record("action.preview_rendered", …)` hoặc `error.render_fail` (`model.go:294`); `syncPreview` set `renderStartedAt = time.Now()` ngay trước `return` Cmd (`model.go:282`) |
| `.github/workflows/datadog-static-analysis.yml` (mới) | Workflow chạy Datadog Static Analysis trên PR (§5.5) |
| `.github/workflows/datadog-sast.yml` (mới) | Workflow chạy Datadog SAST trên PR (§5.6) |
| `.github/workflows/verify-sast-live.yml` (mới) | Job verify canary đã bị flag (§5.6/FR5) |
| `static-analysis.datadog.yml` (mới) | Config rulesets + ignore `tmp/` + `testdata/sast_canary.go` (§5.5) |
| `testdata/sast_canary.go` (mới) | Build-tag `sastcanary` fixture chứa unsafe pattern (D11) |
| `README.md` | + section *Telemetry (opt-in)* với bảng env var (§5.7) |
| `*_test.go` | `telemetry_test.go` (channel cap, drop counter, drainer batching, race); `payload_redaction_test.go` (extClass/cwdDepth/hostnameHash); cập nhật `zz_dump_test.go` cho off-vs-on-offline byte-identity (T11) |
