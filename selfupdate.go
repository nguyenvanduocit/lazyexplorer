package main

// Self-update from GitHub Releases — the `lazyexplorer update` command.
//
// Design (docs/prd-self-update.md): a Functional core of pure helpers
// (compareVersions/assetName/parseChecksums/extractBinary/brewManaged) that are
// unit-tested without I/O, wrapped by an imperative shell (runUpdate) whose
// HTTP client, target path and brew check are injected so the whole flow is
// exercised against an httptest server (selfupdate_test.go). The dangerous step
// — atomically replacing the running executable across platforms — is delegated
// to github.com/minio/selfupdate; we own only the GitHub API call, OS/arch asset
// selection and SHA256 verification, keeping go.mod lean and the logic testable.

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/minio/selfupdate"
)

const updateRepo = "nguyenvanduocit/lazyexplorer"

// --- Functional core (pure, no I/O) ------------------------------------------

// parseSemver reads a "vX.Y.Z" / "X.Y.Z" tag into its numeric parts, dropping
// any leading "v" and any -prerelease/+build metadata. ok is false for anything
// that is not a release version (e.g. the "dev" sentinel) — the gate for FR7.
func parseSemver(v string) (parts [3]int, ok bool) {
	v = strings.TrimPrefix(strings.TrimPrefix(v, "v"), "V")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	segs := strings.Split(v, ".")
	if len(segs) > 3 {
		return parts, false
	}
	for i, s := range segs {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			return [3]int{}, false
		}
		parts[i] = n
	}
	return parts, true
}

// compareVersions returns -1 / 0 / 1 for current <, ==, > latest, tolerating a
// "v" prefix on either side (git tags carry it, buildVersion does not).
func compareVersions(current, latest string) int {
	c, _ := parseSemver(current)
	l, _ := parseSemver(latest)
	for i := 0; i < 3; i++ {
		switch {
		case c[i] < l[i]:
			return -1
		case c[i] > l[i]:
			return 1
		}
	}
	return 0
}

// isReleaseVersion reports whether v is a stamped *release* (so self-update is
// allowed). A dev `go build` leaves buildVersion at the "dev" sentinel and a
// `goreleaser` snapshot stamps a "-snapshot" suffix (.goreleaser.yaml:58) —
// neither is a release, so both are refused before any network call. Only a
// clean "vX.Y.Z" with no prerelease/build metadata qualifies.
func isReleaseVersion(v string) bool {
	core := strings.TrimPrefix(strings.TrimPrefix(v, "v"), "V")
	if strings.ContainsAny(core, "-+") {
		return false
	}
	_, ok := parseSemver(core)
	return ok
}

// assetName rebuilds the goreleaser archive filename for a GOOS/GOARCH. The
// mapping (amd64→x86_64, windows→.zip) mirrors .goreleaser.yaml:46-49 and is
// pinned against the real v0.2.0 assets in TestAssetName. version is bare ("0.2.0").
func assetName(goos, goarch, version string) string {
	arch := goarch
	if goarch == "amd64" {
		arch = "x86_64"
	}
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("lazyexplorer_%s_%s_%s%s", version, goos, arch, ext)
}

// parseChecksums finds the SHA256 hex for filename in a checksums.txt body
// (lines of "<hex>  <name>").
func parseChecksums(text, filename string) (string, error) {
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) != 2 {
			continue
		}
		if f[1] == filename || filepath.Base(f[1]) == filename {
			return strings.ToLower(f[0]), nil
		}
	}
	return "", fmt.Errorf("checksum for %s not found", filename)
}

// extractBinary pulls the lazyexplorer executable out of a release archive,
// ignoring the LICENSE/README/docs that share it.
func extractBinary(name string, archive []byte) ([]byte, error) {
	if strings.HasSuffix(name, ".zip") {
		return extractFromZip(archive)
	}
	return extractFromTarGz(archive)
}

func isBinaryEntry(p string) bool {
	b := path.Base(p)
	return b == "lazyexplorer" || b == "lazyexplorer.exe"
}

func extractFromTarGz(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag == tar.TypeReg && isBinaryEntry(h.Name) {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("binary not found in archive")
}

func extractFromZip(archive []byte) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, err
	}
	for _, f := range zr.File {
		if isBinaryEntry(f.Name) {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, fmt.Errorf("binary not found in archive")
}

// brewPrefixes are the Homebrew-owned roots a binary resolves under once its bin
// symlink is followed. /usr/local/bin alone is deliberately absent — a manual
// install there is the user's to replace; only the Cellar is brew-managed.
var brewPrefixes = []string{
	"/opt/homebrew",
	"/usr/local/Cellar",
	"/home/linuxbrew/.linuxbrew",
}

// brewManaged reports whether execPath (already symlink-resolved) lives under a
// Homebrew prefix, in which case self-update defers to `brew upgrade` (D5/FR8).
func brewManaged(execPath string) bool {
	clean := filepath.Clean(execPath)
	for _, p := range brewPrefixes {
		if clean == p || strings.HasPrefix(clean, p+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// --- Imperative shell --------------------------------------------------------

// updateConfig is the injected environment for runUpdate; defaultUpdateConfig
// wires production values and tests substitute an httptest server + temp target.
type updateConfig struct {
	currentVersion string
	apiBaseURL     string // e.g. https://api.github.com
	httpClient     *http.Client
	targetPath     string // executable to replace (symlink-resolved)
	out            io.Writer
	errOut         io.Writer
	brewCheck      func(string) bool
}

func defaultUpdateConfig() updateConfig {
	exe, err := os.Executable()
	if err == nil {
		if resolved, e := filepath.EvalSymlinks(exe); e == nil {
			exe = resolved
		}
	}
	return updateConfig{
		currentVersion: buildVersion,
		apiBaseURL:     "https://api.github.com",
		httpClient:     &http.Client{Timeout: 30 * time.Second},
		targetPath:     exe,
		out:            os.Stdout,
		errOut:         os.Stderr,
		brewCheck:      brewManaged,
	}
}

type ghRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

func (r *ghRelease) assetURL(name string) string {
	for _, a := range r.Assets {
		if a.Name == name {
			return a.URL
		}
	}
	return ""
}

// runUpdate performs the whole fetch→verify→replace flow and returns a process
// exit code. Every failure prints a single line and leaves the binary untouched.
func runUpdate(cfg updateConfig) int {
	fail := func(format string, a ...any) int {
		fmt.Fprintf(cfg.errOut, "lazyexplorer update: "+format+"\n", a...)
		return 1
	}

	if !isReleaseVersion(cfg.currentVersion) {
		return fail("this is a development build; install a release to self-update")
	}
	if cfg.brewCheck(cfg.targetPath) {
		return fail("installed via Homebrew; run: brew upgrade %s", "nguyenvanduocit/tap/lazyexplorer")
	}
	// Preflight writability before touching the network so a non-writable
	// install fails fast instead of after a multi-MB download.
	if err := (&selfupdate.Options{TargetPath: cfg.targetPath}).CheckPermissions(); err != nil {
		return fail("cannot write to %s: %v", cfg.targetPath, err)
	}

	rel, err := fetchLatestRelease(cfg)
	if err != nil {
		return fail("%v", err)
	}
	// A non-semver tag would make compareVersions read it as 0.0.0 and silently
	// report "already up to date"; surface it as an explicit error instead.
	if !isReleaseVersion(rel.TagName) {
		return fail("latest GitHub release has a non-version tag %q", rel.TagName)
	}
	if compareVersions(cfg.currentVersion, rel.TagName) >= 0 {
		fmt.Fprintf(cfg.out, "lazyexplorer is already up to date (%s)\n", displayVersion(rel.TagName))
		return 0
	}

	bareVer := strings.TrimPrefix(rel.TagName, "v")
	want := assetName(runtime.GOOS, runtime.GOARCH, bareVer)
	archiveURL := rel.assetURL(want)
	checksumURL := rel.assetURL("checksums.txt")
	if archiveURL == "" || checksumURL == "" {
		return fail("no release asset for %s/%s in %s", runtime.GOOS, runtime.GOARCH, rel.TagName)
	}

	archive, err := download(cfg, archiveURL)
	if err != nil {
		return fail("download %s: %v", want, err)
	}
	checksumText, err := download(cfg, checksumURL)
	if err != nil {
		return fail("download checksums: %v", err)
	}
	wantSum, err := parseChecksums(string(checksumText), want)
	if err != nil {
		return fail("%v", err)
	}
	gotSum := sha256.Sum256(archive)
	if hex.EncodeToString(gotSum[:]) != wantSum {
		return fail("checksum mismatch for %s — aborting, binary left untouched", want)
	}

	bin, err := extractBinary(want, archive)
	if err != nil {
		return fail("%v", err)
	}
	if err := selfupdate.Apply(bytes.NewReader(bin), selfupdate.Options{TargetPath: cfg.targetPath}); err != nil {
		if rb := selfupdate.RollbackError(err); rb != nil {
			return fail("apply failed AND rollback failed: %v (your binary may be at %s.old)", rb, cfg.targetPath)
		}
		return fail("apply: %v", err)
	}
	fmt.Fprintf(cfg.out, "updated lazyexplorer %s → %s\n", strings.TrimPrefix(cfg.currentVersion, "v"), bareVer)
	return 0
}

func fetchLatestRelease(cfg updateConfig) (*ghRelease, error) {
	req, err := http.NewRequest(http.MethodGet, cfg.apiBaseURL+"/repos/"+updateRepo+"/releases/latest", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "lazyexplorer/"+cfg.currentVersion)
	resp, err := cfg.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api returned %s", resp.Status)
	}
	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	if rel.TagName == "" {
		return nil, fmt.Errorf("github api returned no release tag")
	}
	return &rel, nil
}

func download(cfg updateConfig, url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "lazyexplorer/"+cfg.currentVersion)
	resp, err := cfg.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s returned %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// displayVersion normalises a tag for user-facing output ("0.2.0" → "v0.2.0").
func displayVersion(tag string) string {
	if strings.HasPrefix(tag, "v") {
		return tag
	}
	return "v" + tag
}
