package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// --- Pure core ---------------------------------------------------------------

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		current, latest string
		want            int
	}{
		{"0.1.0", "0.2.0", -1},
		{"0.1.0", "v0.2.0", -1}, // v-prefix on the latest tag, bare on current
		{"v0.2.0", "0.2.0", 0},  // equal across the prefix gap
		{"0.2.0", "0.2.0", 0},
		{"0.3.0", "0.2.0", 1}, // current is newer
		{"0.2.0", "0.2.1", -1},
		{"0.2.0", "1.0.0", -1},
		{"0.2", "0.2.0", 0}, // missing patch treated as 0
		{"1.0.0", "0.9.9", 1},
	}
	for _, c := range cases {
		if got := compareVersions(c.current, c.latest); got != c.want {
			t.Errorf("compareVersions(%q,%q)=%d want %d", c.current, c.latest, got, c.want)
		}
	}
}

func TestIsReleaseVersion(t *testing.T) {
	releases := []string{"0.2.0", "v0.2.0", "1.10.3", "0.2"}
	for _, v := range releases {
		if !isReleaseVersion(v) {
			t.Errorf("isReleaseVersion(%q)=false want true", v)
		}
	}
	dev := []string{"dev", "", "snapshot", "0.2.0-dev", "unknown"}
	for _, v := range dev {
		if isReleaseVersion(v) {
			t.Errorf("isReleaseVersion(%q)=true want false", v)
		}
	}
}

func TestAssetName(t *testing.T) {
	// These are the exact asset names published on the real v0.2.0 release
	// (gh release view v0.2.0 --json assets, VERIFIED 2026-06-04).
	cases := []struct {
		goos, goarch, want string
	}{
		{"darwin", "arm64", "lazyexplorer_0.2.0_darwin_arm64.tar.gz"},
		{"darwin", "amd64", "lazyexplorer_0.2.0_darwin_x86_64.tar.gz"},
		{"linux", "arm64", "lazyexplorer_0.2.0_linux_arm64.tar.gz"},
		{"linux", "amd64", "lazyexplorer_0.2.0_linux_x86_64.tar.gz"},
		{"windows", "amd64", "lazyexplorer_0.2.0_windows_x86_64.zip"},
	}
	for _, c := range cases {
		if got := assetName(c.goos, c.goarch, "0.2.0"); got != c.want {
			t.Errorf("assetName(%q,%q)=%q want %q", c.goos, c.goarch, got, c.want)
		}
	}
}

func TestParseChecksums(t *testing.T) {
	text := "abc123  lazyexplorer_0.2.0_darwin_arm64.tar.gz\n" +
		"def456  lazyexplorer_0.2.0_linux_amd64.tar.gz\n"
	got, err := parseChecksums(text, "lazyexplorer_0.2.0_darwin_arm64.tar.gz")
	if err != nil || got != "abc123" {
		t.Fatalf("parseChecksums got %q,%v want abc123,nil", got, err)
	}
	if _, err := parseChecksums(text, "missing.tar.gz"); err == nil {
		t.Error("parseChecksums: expected error for missing file")
	}
}

func TestExtractBinary(t *testing.T) {
	want := []byte("#!/fake/lazyexplorer binary payload")

	tgz := buildTarGz(t, want)
	got, err := extractBinary("lazyexplorer_0.2.0_linux_amd64.tar.gz", tgz)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("extractBinary(tar.gz): got %q,%v", got, err)
	}

	z := buildZip(t, want)
	got, err = extractBinary("lazyexplorer_0.2.0_windows_x86_64.zip", z)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("extractBinary(zip): got %q,%v", got, err)
	}

	if _, err := extractBinary("x.tar.gz", []byte("not a gzip")); err == nil {
		t.Error("extractBinary: expected error on garbage archive")
	}
}

func TestBrewManaged(t *testing.T) {
	managed := []string{
		"/opt/homebrew/Cellar/lazyexplorer/0.2.0/bin/lazyexplorer",
		"/usr/local/Cellar/lazyexplorer/0.2.0/bin/lazyexplorer",
		"/home/linuxbrew/.linuxbrew/Cellar/lazyexplorer/0.2.0/bin/lazyexplorer",
	}
	for _, p := range managed {
		if !brewManaged(p) {
			t.Errorf("brewManaged(%q)=false want true", p)
		}
	}
	free := []string{
		"/usr/local/bin/lazyexplorer", // a manual /usr/local install is not brew-managed
		"/home/user/go/bin/lazyexplorer",
		"/tmp/lazyexplorer",
	}
	for _, p := range free {
		if brewManaged(p) {
			t.Errorf("brewManaged(%q)=true want false", p)
		}
	}
}

// --- Imperative shell (injected config) --------------------------------------

func TestRunUpdate_DevBuildRefuses(t *testing.T) {
	hits := 0
	cfg := updateConfig{
		currentVersion: "dev",
		httpClient:     &http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) { hits++; return nil, fmt.Errorf("no net") })},
		targetPath:     filepath.Join(t.TempDir(), "lazyexplorer"),
		brewCheck:      func(string) bool { return false },
	}
	var out, errOut bytes.Buffer
	cfg.out, cfg.errOut = &out, &errOut
	if code := runUpdate(cfg); code == 0 {
		t.Error("dev build: expected non-zero exit")
	}
	if hits != 0 {
		t.Errorf("dev build made %d network calls, want 0", hits)
	}
	if !strings.Contains(errOut.String(), "development build") {
		t.Errorf("dev build message = %q", errOut.String())
	}
}

func TestRunUpdate_BrewRefuses(t *testing.T) {
	hits := 0
	cfg := updateConfig{
		currentVersion: "0.1.0",
		httpClient:     &http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) { hits++; return nil, fmt.Errorf("no net") })},
		targetPath:     filepath.Join(t.TempDir(), "lazyexplorer"),
		brewCheck:      func(string) bool { return true },
	}
	var out, errOut bytes.Buffer
	cfg.out, cfg.errOut = &out, &errOut
	if code := runUpdate(cfg); code == 0 {
		t.Error("brew install: expected non-zero exit")
	}
	if hits != 0 {
		t.Errorf("brew install made %d network calls, want 0", hits)
	}
	if !strings.Contains(errOut.String(), "brew upgrade") {
		t.Errorf("brew message = %q", errOut.String())
	}
}

func TestRunUpdate_AlreadyLatest(t *testing.T) {
	srv := httptest.NewServer(releaseHandler(t, "v0.2.0", nil))
	defer srv.Close()
	target := writeTarget(t, "old binary")
	cfg := testConfig("0.2.0", srv.URL, target)
	var out, errOut bytes.Buffer
	cfg.out, cfg.errOut = &out, &errOut
	if code := runUpdate(cfg); code != 0 {
		t.Fatalf("already-latest exit=%d stderr=%q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "up to date") {
		t.Errorf("already-latest stdout=%q", out.String())
	}
	if b, _ := os.ReadFile(target); string(b) != "old binary" {
		t.Errorf("already-latest must not touch the binary, got %q", b)
	}
}

func TestRunUpdate_Integration(t *testing.T) {
	newBin := []byte("NEW lazyexplorer 0.2.0 payload")
	want := assetName(runtime.GOOS, runtime.GOARCH, "0.2.0")
	archive := buildArchive(t, want, newBin)
	sum := sha256.Sum256(archive)
	checksums := hex.EncodeToString(sum[:]) + "  " + want + "\n"

	srv := httptest.NewServer(releaseHandler(t, "v0.2.0",
		map[string][]byte{want: archive, "checksums.txt": []byte(checksums)}))
	defer srv.Close()

	target := writeTarget(t, "OLD binary")
	cfg := testConfig("0.1.0", srv.URL, target)
	var out, errOut bytes.Buffer
	cfg.out, cfg.errOut = &out, &errOut

	if code := runUpdate(cfg); code != 0 {
		t.Fatalf("integration exit=%d stderr=%q", code, errOut.String())
	}
	got, _ := os.ReadFile(target)
	if !bytes.Equal(got, newBin) {
		t.Fatalf("target not replaced: got %q want %q", got, newBin)
	}
	if !strings.Contains(out.String(), "0.1.0") || !strings.Contains(out.String(), "0.2.0") {
		t.Errorf("integration stdout=%q (want old→new versions)", out.String())
	}
}

func TestRunUpdate_ChecksumMismatch(t *testing.T) {
	want := assetName(runtime.GOOS, runtime.GOARCH, "0.2.0")
	archive := buildArchive(t, want, []byte("real payload"))
	// Publish a checksum that does NOT match the served archive.
	checksums := strings.Repeat("0", 64) + "  " + want + "\n"

	srv := httptest.NewServer(releaseHandler(t, "v0.2.0",
		map[string][]byte{want: archive, "checksums.txt": []byte(checksums)}))
	defer srv.Close()

	target := writeTarget(t, "ORIGINAL")
	cfg := testConfig("0.1.0", srv.URL, target)
	var out, errOut bytes.Buffer
	cfg.out, cfg.errOut = &out, &errOut

	if code := runUpdate(cfg); code == 0 {
		t.Fatal("checksum mismatch: expected non-zero exit")
	}
	if b, _ := os.ReadFile(target); string(b) != "ORIGINAL" {
		t.Errorf("checksum mismatch must leave binary untouched, got %q", b)
	}
	if !strings.Contains(strings.ToLower(errOut.String()), "checksum") {
		t.Errorf("checksum mismatch message=%q", errOut.String())
	}
}

func TestRunUpdate_NonSemverTagRejected(t *testing.T) {
	// GitHub should only ever surface a clean vX.Y.Z on /releases/latest, but a
	// malformed tag must be an explicit error, not a silent "already up to date".
	srv := httptest.NewServer(releaseHandler(t, "nightly-2026-06-04", nil))
	defer srv.Close()
	target := writeTarget(t, "ORIGINAL")
	cfg := testConfig("0.1.0", srv.URL, target)
	var out, errOut bytes.Buffer
	cfg.out, cfg.errOut = &out, &errOut

	if code := runUpdate(cfg); code == 0 {
		t.Fatalf("non-semver tag: expected non-zero exit, stdout=%q", out.String())
	}
	if strings.Contains(out.String(), "up to date") {
		t.Errorf("non-semver tag must not report up to date, stdout=%q", out.String())
	}
	if b, _ := os.ReadFile(target); string(b) != "ORIGINAL" {
		t.Errorf("non-semver tag must leave binary untouched, got %q", b)
	}
}

// TestRunUpdate_E2E_RealGitHub exercises the whole pipeline against the live
// GitHub release (download → SHA256 verify → extract → atomic apply → exec the
// replaced binary). Network + opt-in gated: it touches the internet, so it is
// off the hermetic `go test ./...` gate. Run it for real-world evidence with:
//
//	LE_E2E_UPDATE=1 go test -run E2E_RealGitHub -v .
func TestRunUpdate_E2E_RealGitHub(t *testing.T) {
	if os.Getenv("LE_E2E_UPDATE") == "" || testing.Short() {
		t.Skip("set LE_E2E_UPDATE=1 to run the live-GitHub e2e")
	}
	target := writeTarget(t, "placeholder")
	cfg := defaultUpdateConfig()
	cfg.currentVersion = "0.0.1" // pretend we are old so it actually updates
	cfg.targetPath = target
	cfg.brewCheck = func(string) bool { return false }
	var out, errOut bytes.Buffer
	cfg.out, cfg.errOut = &out, &errOut

	if code := runUpdate(cfg); code != 0 {
		t.Fatalf("e2e exit=%d stderr=%q", code, errOut.String())
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(target, 0o755)
	}
	v, err := exec.Command(target, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("exec replaced binary: %v (%s)", err, v)
	}
	if !strings.Contains(string(v), "lazyexplorer") {
		t.Errorf("replaced binary --version=%q", v)
	}
	t.Logf("e2e ok: %s", strings.TrimSpace(string(v)))
}

// --- helpers -----------------------------------------------------------------

type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// testConfig wires runUpdate at a test server with a writable temp target and
// the brew guard disabled.
func testConfig(current, baseURL, target string) updateConfig {
	cfg := defaultUpdateConfig()
	cfg.currentVersion = current
	cfg.apiBaseURL = baseURL
	cfg.targetPath = target
	cfg.brewCheck = func(string) bool { return false }
	return cfg
}

func writeTarget(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "lazyexplorer")
	if err := os.WriteFile(p, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// releaseHandler serves /repos/.../releases/latest plus each asset at
// /download/<name>, with browser_download_url pointing back at this server.
func releaseHandler(t *testing.T, tag string, assets map[string][]byte) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/nguyenvanduocit/lazyexplorer/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		var b strings.Builder
		b.WriteString(`{"tag_name":"` + tag + `","assets":[`)
		first := true
		for name := range assets {
			if !first {
				b.WriteString(",")
			}
			first = false
			fmt.Fprintf(&b, `{"name":%q,"browser_download_url":%q}`, name, "http://"+r.Host+"/download/"+name)
		}
		b.WriteString("]}")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, b.String())
	})
	mux.HandleFunc("/download/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/download/")
		body, ok := assets[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(body)
	})
	return mux
}

func buildArchive(t *testing.T, name string, bin []byte) []byte {
	if strings.HasSuffix(name, ".zip") {
		return buildZip(t, bin)
	}
	return buildTarGz(t, bin)
}

func buildTarGz(t *testing.T, bin []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	// extra files that share the archive, like the real goreleaser output
	files := []struct {
		name string
		body []byte
	}{
		{"LICENSE", []byte("MIT")},
		{"README.md", []byte("# lazyexplorer")},
		{"lazyexplorer", bin},
	}
	for _, f := range files {
		if err := tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0o755, Size: int64(len(f.body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(f.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func buildZip(t *testing.T, bin []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range []struct {
		name string
		body []byte
	}{
		{"LICENSE", []byte("MIT")},
		{"lazyexplorer.exe", bin},
	} {
		w, err := zw.Create(f.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(f.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
