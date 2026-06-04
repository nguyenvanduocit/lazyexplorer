# PRD — Self-update from GitHub (`lazyexplorer update`)

Status: **accepted** · Author: ai/phiên auto-update-goal · Ngày: 2026-06-04

> Đã implement & verify (xem mục 6 — checklist có `ĐÃ VERIFY ✅ 2026-06-04`). Spec này
> khớp code đang ship (`selfupdate.go`, wiring trong `main.go`/`telemetry.go`).

---

## 1. Bối cảnh & vấn đề

lazyexplorer phát hành binary qua goreleaser → GitHub Releases: mỗi tag `v*` đẩy
archive đa-nền (`lazyexplorer_<ver>_<os>_<arch>.tar.gz`, windows `.zip`) kèm
`checksums.txt` (SHA256), và publish Homebrew formula
(`.goreleaser.yaml:40-97`). Người cài qua Homebrew nâng cấp bằng `brew upgrade`;
nhưng người tải binary thẳng từ Releases hoặc `go install` **không có đường nâng
cấp tại chỗ** — phải tự vào trang Releases, tải lại, giải nén, thay file.

Phiên bản đang chạy đã được nhúng sẵn: `buildVersion` stamp qua ldflags lúc release
(`telemetry.go:37-38`, `main.go:119-122` in ra `lazyexplorer <buildVersion>`).
Repo GitHub là `nguyenvanduocit/lazyexplorer`, API Releases public — đủ dữ kiện để
một lệnh tự-cập-nhật so phiên bản, tải đúng asset, verify checksum, rồi thay binary.

Đây là mối lo **CLI-level**, không phải TUI: nó giải quyết được trọn vẹn bằng một
subcommand chạy trước khi alt-screen mở — **không thêm panel/mode/keybind nào**, đúng
 trần đơn-giản của UI (`CLAUDE.md` Design Principles).

## 2. Goal (1 câu)

`lazyexplorer update` tự tải bản phát hành mới nhất từ GitHub Releases, **verify SHA256**,
rồi thay binary đang chạy tại chỗ — an toàn, đa nền, không cần quyền root.

**Non-goal làm rõ:**
- **Không** auto-check lúc khởi động, **không** notification trong TUI, **không** thêm
  network call vào đường chạy thường. "auto" ở đây = tự-động-hoá khâu *fetch → verify →
  replace*, không phải tự chạy khi chưa được yêu cầu (đối lập với cam kết "zero overhead
  when off, no socket" của telemetry — `README.md:78-81`).
- **Không** quản lý nhiều kênh (stable/beta), không rollback UI, không downgrade tuỳ ý.
- **Không** tự nâng cấp bản cài qua Homebrew (đó là việc của `brew upgrade`).
- **Không** thêm secret/biến môi trường mới; dùng GitHub API ẩn danh.

## 3. Quyết định đã chốt

| # | Quyết định | Giá trị | Lý do |
|---|-----------|---------|-------|
| D1 | "auto update" nghĩa là gì | Subcommand `update` (+ `--update`), chạy thủ công | "auto" = tự-động-hoá cơ chế tải/verify/thay, không phải chạy lén lúc launch. Startup-check sẽ thêm network mỗi lần chạy + bề mặt notify trong TUI → vi phạm ethos đơn-giản & cam kết zero-overhead. |
| D2 | Mức độ | Tải + thay binary thật (không chỉ "check & báo") | "update" nghĩa là update. Bản chỉ-báo-có-bản-mới được liệt kê ở §5 *defer*. |
| D3 | Thay binary atomically | Lib `github.com/minio/selfupdate` cho khâu apply; tự viết phần GitHub API + chọn asset + parse checksum | Thay file thực thi đang chạy đa-nền (Windows không ghi đè `.exe` đang chạy; cần rename-current-to-`.old`, rollback khi lỗi) là phần *nguy hiểm* — giao cho lib đã được kiểm chứng (minio dùng prod). Phần thuần-logic giữ tự viết để go.mod gọn & test được. "Uncompromising in engineering" + "consult docs before SDK". |
| D4 | Toàn vẹn | Verify SHA256 archive đã tải với `checksums.txt` **trước khi** giải nén/apply | Đang thay một executable — bắt buộc. Mismatch → huỷ, không đụng binary. |
| D5 | Bản cài qua package-manager | Phát hiện install do brew/được-quản-lý → **từ chối** + in `brew upgrade nguyenvanduocit/tap/lazyexplorer`; không bao giờ `sudo` | Self-update đè lên file do brew quản lý sẽ làm hỏng trạng thái brew. Tham chiếu pattern `gh` CLI. Phát hiện: `os.Executable` → `EvalSymlinks` nằm dưới brew prefix, hoặc target không ghi được. |
| D6 | Dev/unstamped build | Đổi sentinel mặc định `buildVersion` `"0.1.0"` → `"dev"`; từ chối self-update khi version không phải semver release | Binary `go build` cục bộ trước đây mặc định `"0.1.0"` — **không phân biệt được** với bản release v0.1.0 thật, nên có thể tự "update" đè bản dev. Sentinel `"dev"` cho tín hiệu rõ và cũng trung thực hơn cho telemetry (bản dev không giả danh một release). |
| D7 | Nguồn | GitHub API `/repos/nguyenvanduocit/lazyexplorer/releases/latest`, ẩn danh | 60 req/giờ không-auth là quá đủ cho một lệnh thủ công. Không thêm token/secret. |
| D8 | Vị trí trong main() | Short-circuit **trước** `InitTelemetry` (như `--version`) | Một lần chạy update không được ghi nhận thành session telemetry ma (giống lý do của `--split`, `main.go:140-152`). |

## 4. Functional requirements

- **FR1** — `lazyexplorer update` và `lazyexplorer --update` kích hoạt đường self-update;
  được giải quyết **trước** khi mở TUI (và trước `--split`/telemetry). Thắng các cờ khác.
- **FR2** — Truy vấn `/releases/latest`, đọc `tag_name`, so với version hiện tại sau khi
  **chuẩn hoá tiền tố `v`** (tag `v0.2.0` ⇄ buildVersion `0.2.0`).
- **FR3** — Đã mới nhất → in `lazyexplorer is already up to date (vX.Y.Z)`, exit 0, **không tải gì**.
- **FR4** — Có bản mới hơn → tải đúng archive theo `runtime.GOOS`/`GOARCH`, verify SHA256 với
  `checksums.txt`, giải nén binary, thay executable đang chạy atomically, in
  `updated lazyexplorer X.Y.Z → A.B.C`.
- **FR5** — Checksum lệch → **huỷ, không sửa binary**; in một dòng lỗi stderr; exit ≠ 0.
- **FR6** — Lỗi mạng/offline/API/asset-not-found → một dòng lỗi stderr, exit ≠ 0, **binary nguyên vẹn** (không ghi nửa chừng, không panic).
- **FR7** — Bản dev/unstamped → từ chối với thông điệp rõ (`this is a development build …`),
  exit ≠ 0, **không** gọi mạng.
- **FR8** — Bản cài brew/được-quản-lý → từ chối + in lệnh `brew upgrade …`, **không** tải.
- **FR9** — `helpText()` liệt kê lệnh `update`; `README.md` có mục "Updating".

## 5. Technical design

**Kim chỉ nam:** *Functional core, imperative shell* (`CLAUDE.md` Engineering Principle #4).
Mọi logic thuần (so version, dựng tên asset, parse checksum, nhận diện brew, giải nén) là
hàm pure — test trực tiếp, không I/O. Vỏ mệnh lệnh chỉ nối HTTP + filesystem + `selfupdate.Apply`.

### 5.1 Functional core (pure — `selfupdate.go`, test trong `selfupdate_test.go`)

- `compareVersions(current, latest string) int` — chuẩn hoá tiền tố `v`, parse
  `major.minor.patch`, trả -1/0/1. Xử lý `0.1.0` vs `v0.2.0` → `-1`.
- `isReleaseVersion(v string) bool` — `true` chỉ khi parse được thành semver release
  (sentinel `"dev"` và chuỗi không-semver → `false`). Cổng cho FR7.
- `assetName(goos, goarch, version string) string` — dựng đúng tên file đã quan sát trên
  release thật (`gh release view v0.2.0`, ĐÃ VERIFY ✅ 2026-06-04):
  `lazyexplorer_<version>_<goos>_<arch><ext>`, với `amd64→x86_64`, `arm64→arm64`,
  `ext=.zip` khi windows ngược lại `.tar.gz`. `version` là dạng trần (`0.2.0`, không `v`).
- `parseChecksums(text, filename string) (string, error)` — tìm dòng `<hex>  <filename>`
  trong `checksums.txt`, trả hex SHA256; không thấy → lỗi.
- `extractBinary(assetName string, archive []byte) ([]byte, error)` — rút entry binary
  (`lazyexplorer`/`lazyexplorer.exe`) từ tar.gz (hoặc zip nếu windows). Bỏ qua
  `LICENSE`/`README.md`/`docs/**` cùng archive.
- `brewManaged(execPath string) bool` — `execPath` (đã EvalSymlinks) nằm dưới prefix brew
  điển hình (`/opt/homebrew`, `/usr/local`, `/home/linuxbrew/.linuxbrew`) hoặc Cellar.

### 5.2 Imperative shell (`selfupdate.go`)

`runUpdate(cfg updateConfig) int` — trả exit code, nhận `updateConfig{ currentVersion,
apiBaseURL, downloadBaseURL, httpClient, targetPath, out, errOut }` để **test bơm được**
httptest server + temp target (không bao giờ đè binary test). Mặc định production:
`apiBaseURL=https://api.github.com`, `downloadBaseURL` lấy từ
`assets[].browser_download_url`, `targetPath=os.Executable()` (đã EvalSymlinks).

Luồng: resolve exe → dev-guard (FR7) → brew-guard (FR8) → GET latest JSON →
`compareVersions` (FR2/FR3) → tải archive + `checksums.txt` → verify SHA256 (FR4/FR5) →
`extractBinary` → `selfupdate.Apply(bytes.NewReader(bin), selfupdate.Options{TargetPath: …})`
→ in kết quả. Mọi lỗi → một dòng + exit ≠ 0, binary nguyên (FR6).

`selfupdate.Options.Checksum` **không** dùng (nó verify binary-bên-trong, mà checksums.txt
chứa hash của *archive*); toàn vẹn đã đảm bảo bằng verify archive trước khi giải nén (D4).

### 5.3 Wiring (`main.go`, `telemetry.go`)

- `parseArgs`: thêm `case a == "update" || a == "--update": out.update = true` (bare-word
  `update` xử lý như `version`/`help` đã có — `main.go:54-57`).
- `main()`: sau `showVersion`/`showHelp`, **trước** `--split`/`InitTelemetry` (D8):
  `if args.update { os.Exit(runUpdate(defaultUpdateConfig())) }`.
- `helpText()`: thêm dòng `update` vào danh sách lệnh (FR9).
- `telemetry.go:38`: `var buildVersion = "dev"` + cập nhật comment (D6).

### 5.4 Reference clone

Không clone nào trong `tmp/` làm self-update; pattern apply tham chiếu doc của
`github.com/minio/selfupdate` (đọc doc, **không copy code**). Pattern brew-defer tham chiếu
hành vi `gh` CLI (mô tả, không copy).

### Đã cân nhắc & defer khỏi v1

- **Auto-check lúc khởi động + banner "có bản mới"** — thêm network mỗi lần chạy + bề mặt UI;
  trái ethos. Defer; nếu cần, làm opt-in qua biến môi trường sau.
- **Chỉ-check (`update --check`)** — báo có bản mới mà không thay. Có ích nhưng D2 chọn thay thật trước.
- **Signature/cosign verify** — checksums.txt (SHA256) đủ cho v1; ký số defer.
- **Chọn version tuỳ ý / downgrade** — chỉ theo `/latest`.

## 6. Acceptance criteria

```gherkin
Feature: Self-update lazyexplorer from GitHub Releases

  Scenario: Update an outdated release binary to the latest
    Given a stamped release build older than the latest GitHub release
    And the binary lives in a user-writable location
    When the user runs the update command
    Then the latest release archive for this OS and architecture is downloaded
    And its SHA256 is verified against the published checksums
    And the running binary is replaced atomically with the new version
    And the user is told the old and new versions

  Scenario: Already on the latest version
    Given a stamped release build equal to the latest GitHub release
    When the user runs the update command
    Then nothing is downloaded
    And the user is told it is already up to date

  Scenario: Corrupted download is rejected
    Given the downloaded archive does not match the published checksum
    When the user runs the update command
    Then the update is aborted
    And the existing binary is left untouched
    And the user sees an integrity error

  Scenario: Development build refuses to self-update
    Given an unstamped development build
    When the user runs the update command
    Then no network request is made
    And the user is told to install a release build

  Scenario: Homebrew-managed install defers to brew
    Given the binary is installed under a Homebrew prefix
    When the user runs the update command
    Then nothing is downloaded
    And the user is told to run brew upgrade
```

**Checklist verify:** *(ĐÃ VERIFY ✅ 2026-06-04)*

1. `TestAssetName` — `assetName` khớp **chính xác** 5 tên asset của release thật (bảng §5.1)
   cho mọi GOOS/GOARCH hỗ trợ (darwin/linux/windows × amd64/arm64, trừ windows/arm64). ✅
2. `TestCompareVersions` — đúng cho: bằng nhau, current cũ hơn, current mới hơn, lệch
   v-prefix, khác số chữ số. ✅
3. `TestParseChecksums` — lấy đúng hex cho asset đích, lỗi khi thiếu. ✅
4. `TestRunUpdate_Integration` + `TestRunUpdate_ChecksumMismatch` (httptest): release giả +
   archive giả → flow ghi binary mới ra **temp targetPath**, không đụng binary test;
   checksum lệch → giữ nguyên binary cũ. ✅
5. `TestRunUpdate_E2E_RealGitHub` (env-gated `LE_E2E_UPDATE=1`, off khỏi gate hermetic): tải
   release **thật** mới nhất, verify SHA256 thật, apply ra temp target, exec `--version` →
   in `lazyexplorer 0.2.0`. ✅ (chạy tay 2026-06-04, PASS 3.53s)
6. `TestRunUpdate_DevBuildRefuses` + binary thật `./lazyexplorer update` → in
   `this is a development build; install a release to self-update`, exit 1, **không** gọi
   mạng. ✅
7. `go build -o lazyexplorer . && go vet ./... && go test ./...` — **xanh** (test 10.4s). ✅

## 7. Task breakdown

- [x] **T1 — Pure core + test (TDD).** `compareVersions`/`isReleaseVersion`/`assetName`/
  `parseChecksums`/`extractBinary`/`brewManaged` theo §5.1; test đỏ trước (undefined → green). *(selfupdate.go, selfupdate_test.go)*
- [x] **T2 — Imperative shell.** `runUpdate`/`updateConfig`/`defaultUpdateConfig` theo §5.2;
  thêm dep `github.com/minio/selfupdate` v0.6.0. *(selfupdate.go)*
- [x] **T3 — Integration + e2e test.** httptest flow (AC#4) + e2e mạng env-gated (AC#5). *(selfupdate_test.go)*
- [x] **T4 — Wiring.** `parseArgs`+`main()`+`helpText()` (§5.3); đổi sentinel `buildVersion="dev"`. *(main.go, telemetry.go)*
- [x] **T5 — Docs sync.** README mục "Updating". *(README.md)*
- [x] **T6 — Verify.** `go build -o lazyexplorer . && go vet ./... && go test ./...` xanh; chạy tay
  `./lazyexplorer update` dev-guard + live e2e PASS; code-review pass riêng (không self-approve).

## 8. Files chạm tới

| File | Thay đổi |
|------|----------|
| `selfupdate.go` | **Mới** — pure core + shell self-update. |
| `selfupdate_test.go` | **Mới** — unit + integration + e2e. |
| `main.go` | `parseArgs` (`update`/`--update`), short-circuit trong `main()`, dòng help. |
| `telemetry.go` | `buildVersion` sentinel `"dev"` + comment. |
| `README.md` | Mục "Updating". |
| `go.mod` / `go.sum` | `github.com/minio/selfupdate`. |
| `docs/prd-self-update.md` | PRD này. |
