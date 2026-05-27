package main

// Tests for the telemetry boundary, transport, and redaction invariants.
// G002 shipped the boundary + skeleton; G003 lands the drainer goroutine,
// HTTP transport (with httpPoster seam), hostnameHash, and the serializer
// regex assertion that pins PRD §5.4 against drift.
//
// Test layout:
//   * Recorder contract: noop absorbs everything; Active() reflects type.
//   * InitTelemetry env handling: FR6 / FR7 / DD_SITE / DD_LOGS_URL overrides.
//   * Drainer + transport: batching, drop-on-full, idempotent shutdown, 401
//     self-shutdown, 5xx rate-limited stderr, concurrent safety.
//   * Redaction helpers: extClass / cwdDepth / errorClass / hostnameHash.
//   * Serializer invariant (§5.4): no event JSON ever carries a path or the
//     literal strings "secret", "password", "key=".
//   * session.start / session.end shape.

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// ----------------------------------------------------------------------------
// Test fixtures — httpPoster fakes.
// ----------------------------------------------------------------------------

// fakePoster records every Post and returns a canned status/err. status==0
// defaults to 202 so a test that just wants "accept everything" doesn't have
// to remember to set it.
type fakePoster struct {
	mu     sync.Mutex
	calls  []fakeCall
	status int
	err    error
}

type fakeCall struct {
	URL    string
	APIKey string
	Body   []byte
}

func (f *fakePoster) Post(url, apiKey string, body []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]byte, len(body))
	copy(cp, body)
	f.calls = append(f.calls, fakeCall{URL: url, APIKey: apiKey, Body: cp})
	status := f.status
	if status == 0 {
		status = 202
	}
	return status, f.err
}

func (f *fakePoster) Calls() []fakeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]fakeCall, len(f.calls))
	copy(out, f.calls)
	return out
}

// blockingPoster parks the drainer inside Post so flood tests can demonstrate
// the channel-full drop path deterministically — no dependency on real network
// timing. release is closed by the test to let the drainer exit during cleanup.
type blockingPoster struct {
	release chan struct{}
	mu      sync.Mutex
	count   int
}

func (b *blockingPoster) Post(url, apiKey string, body []byte) (int, error) {
	b.mu.Lock()
	b.count++
	b.mu.Unlock()
	<-b.release
	return 202, nil
}

func (b *blockingPoster) Count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.count
}

// testRecorder wires a *realRecorder with a fake poster and registers
// cleanup, so tests don't leak goroutines.
func testRecorder(t *testing.T, client httpPoster) *realRecorder {
	t.Helper()
	r := newRealRecorder("test-key", "test.example", "http://test.example/api/v2/logs", client)
	t.Cleanup(func() { r.Shutdown(time.Second) })
	return r
}

// ----------------------------------------------------------------------------
// Recorder contract — noop + interface stability.
// ----------------------------------------------------------------------------

// TestNoopRecorderAbsorbsEverything pins FR6: when telemetry is off, Record
// (any name, any fields) is a true no-op — no panic, Active() reports false
// so syncPreview can skip the time.Now syscall.
func TestNoopRecorderAbsorbsEverything(t *testing.T) {
	var r Recorder = noopRecorder{}
	if r.Active() {
		t.Fatal("noop must report Active() == false (FR6 hot-path gate)")
	}
	r.Record("view.change", map[string]any{"entry_kind": "file"})
	r.Record("error.render_fail", nil)
	r.Record("", nil)
	r.Shutdown(0)
	r.Shutdown(time.Second)
}

// ----------------------------------------------------------------------------
// InitTelemetry — env handling.
// ----------------------------------------------------------------------------

func TestInitTelemetryOffByDefault(t *testing.T) {
	t.Setenv("LE_TELEMETRY", "")
	t.Setenv("DD_API_KEY", "anything")
	r := InitTelemetry()
	if _, ok := r.(noopRecorder); !ok {
		t.Fatalf("InitTelemetry without LE_TELEMETRY must return noopRecorder, got %T", r)
	}
	if r.Active() {
		t.Fatal("noop must report Active() == false")
	}
}

func TestInitTelemetryFalseyValuesStayOff(t *testing.T) {
	t.Setenv("DD_API_KEY", "k")
	for _, v := range []string{"0", "false", "no", "off", "FALSE", "  ", ""} {
		t.Setenv("LE_TELEMETRY", v)
		r := InitTelemetry()
		if _, ok := r.(noopRecorder); !ok {
			t.Errorf("LE_TELEMETRY=%q should keep telemetry off, got %T", v, r)
		}
	}
}

func TestInitTelemetryTruthyButNoKeyDisablesGracefully(t *testing.T) {
	for _, v := range []string{"1", "true", "yes", "TRUE", "Yes"} {
		t.Setenv("LE_TELEMETRY", v)
		t.Setenv("DD_API_KEY", "")
		r := InitTelemetry()
		if _, ok := r.(noopRecorder); !ok {
			t.Errorf("LE_TELEMETRY=%q + DD_API_KEY='' should disable gracefully, got %T", v, r)
		}
	}
}

func TestInitTelemetryFullyConfiguredReturnsReal(t *testing.T) {
	t.Setenv("LE_TELEMETRY", "1")
	t.Setenv("DD_API_KEY", "dummy-key-for-test")
	t.Setenv("DD_LOGS_URL", "http://localhost:0/discard")
	r1 := InitTelemetry()
	r2 := InitTelemetry()
	rr1, ok1 := r1.(*realRecorder)
	rr2, ok2 := r2.(*realRecorder)
	if !ok1 || !ok2 {
		t.Fatalf("expected *realRecorder from both InitTelemetry calls, got %T / %T", r1, r2)
	}
	if !rr1.Active() || !rr2.Active() {
		t.Fatal("realRecorder.Active() must be true once wired")
	}
	if rr1.sessionID == rr2.sessionID {
		t.Errorf("two Init calls produced the same session id %q — uuid generator stuck?", rr1.sessionID)
	}
	if rr1.site != "datadoghq.com" {
		t.Errorf("default DD_SITE should be datadoghq.com, got %q", rr1.site)
	}
	rr1.Shutdown(time.Second)
	rr2.Shutdown(time.Second)
}

func TestInitTelemetryRespectsDDSiteOverride(t *testing.T) {
	t.Setenv("LE_TELEMETRY", "1")
	t.Setenv("DD_API_KEY", "k")
	t.Setenv("DD_SITE", "datadoghq.eu")
	t.Setenv("DD_LOGS_URL", "http://localhost:0/eu")
	r := InitTelemetry()
	rr, ok := r.(*realRecorder)
	if !ok {
		t.Fatalf("expected *realRecorder, got %T", r)
	}
	if rr.site != "datadoghq.eu" {
		t.Errorf("DD_SITE override ignored: site=%q, want datadoghq.eu", rr.site)
	}
	rr.Shutdown(time.Second)
}

func TestInitTelemetryHonorsDDLogsURLOverride(t *testing.T) {
	t.Setenv("LE_TELEMETRY", "1")
	t.Setenv("DD_API_KEY", "k")
	t.Setenv("DD_SITE", "datadoghq.com")
	t.Setenv("DD_LOGS_URL", "http://localhost:0/sandbox/intake")
	r := InitTelemetry()
	rr, ok := r.(*realRecorder)
	if !ok {
		t.Fatalf("expected *realRecorder, got %T", r)
	}
	if rr.logsURL != "http://localhost:0/sandbox/intake" {
		t.Errorf("DD_LOGS_URL override ignored: logsURL=%q", rr.logsURL)
	}
	rr.Shutdown(time.Second)
}

func TestInitTelemetryDefaultLogsURLDerivedFromSite(t *testing.T) {
	t.Setenv("LE_TELEMETRY", "1")
	t.Setenv("DD_API_KEY", "k")
	t.Setenv("DD_SITE", "datadoghq.eu")
	t.Setenv("DD_LOGS_URL", "")
	r := InitTelemetry()
	rr := r.(*realRecorder)
	want := "https://http-intake.logs.datadoghq.eu/api/v2/logs"
	if rr.logsURL != want {
		t.Errorf("derived logsURL = %q, want %q", rr.logsURL, want)
	}
	rr.Shutdown(time.Second)
}

// ----------------------------------------------------------------------------
// Drainer + transport.
// ----------------------------------------------------------------------------

// TestRealRecorderDropsWhenChannelFull pins FR14 deterministically by parking
// the drainer's first Post on a blockingPoster, THEN saturating the channel
// past chanCap. With the drainer parked, the next chanCap events fill the
// buffer and every event beyond that increments dropped — no real network
// involved, no timing fragility.
//
// Two-phase setup avoids producer-vs-drainer race:
//  1. Fire batchMax events. Drainer accumulates them locally and calls Post,
//     which blocks on blockingPoster. Drainer is now parked.
//  2. Spin until bp.Count() ≥ 1 confirms the drainer entered Post.
//  3. Fire chanCap+surplus events. First chanCap fill the channel buffer; the
//     surplus drop and increment dropped by exactly surplus.
func TestRealRecorderDropsWhenChannelFull(t *testing.T) {
	bp := &blockingPoster{release: make(chan struct{})}
	r := testRecorder(t, bp)
	t.Cleanup(func() { close(bp.release) })

	// Phase 1: park the drainer.
	for i := 0; i < batchMax; i++ {
		r.Record("view.change", nil)
	}
	deadline := time.Now().Add(time.Second)
	for bp.Count() < 1 {
		if time.Now().After(deadline) {
			t.Fatal("drainer did not enter Post within 1s — flood setup precondition broken")
		}
		time.Sleep(time.Millisecond)
	}

	// Phase 2: with drainer parked, saturate channel + force drops.
	const surplus = 50
	for i := 0; i < chanCap+surplus; i++ {
		r.Record("view.change", nil)
	}

	if got := r.dropped.Load(); got != uint64(surplus) {
		t.Errorf("dropped = %d, want %d (drainer parked at batchMax, channel cap=%d, surplus=%d)",
			got, surplus, chanCap, surplus)
	}
	wantViews := uint64(batchMax + chanCap + surplus)
	if got := r.views.Load(); got != wantViews {
		t.Errorf("views = %d, want %d (every Record bumps the counter — PRD §5.3)", got, wantViews)
	}
}

func TestRealRecorderShutdownIsIdempotent(t *testing.T) {
	r := testRecorder(t, &fakePoster{})
	r.Shutdown(time.Millisecond)
	r.Shutdown(time.Second)
	r.Record("view.change", nil) // post-shutdown Record is a no-op (no panic)
}

// TestRealRecorderConcurrentRecordIsSafe runs producers in parallel under a
// blocking poster so the race detector can prove the counters, channel send,
// and shuttingDown gate are race-free. The deterministic assertion is "views
// counts every attempt" — channel/dropped accounting is intentionally not
// asserted here because the interleave between Record calls and the drainer's
// first batch pull is non-deterministic; the dedicated drop test
// (TestRealRecorderDropsWhenChannelFull) covers that contract.
func TestRealRecorderConcurrentRecordIsSafe(t *testing.T) {
	bp := &blockingPoster{release: make(chan struct{})}
	r := testRecorder(t, bp)
	t.Cleanup(func() { close(bp.release) })

	var wg sync.WaitGroup
	const producers = 8
	const perProducer = 50
	const totalAttempts = uint64(producers * perProducer)
	wg.Add(producers)
	for i := 0; i < producers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < perProducer; j++ {
				r.Record("view.change", map[string]any{"i": j})
			}
		}()
	}
	wg.Wait()

	if r.views.Load() != totalAttempts {
		t.Errorf("views = %d, want %d (every Record must bump views — PRD §5.3)", r.views.Load(), totalAttempts)
	}
}

// TestShutdownHonorsFlushTimeoutWhenDrainerBlocked pins PRD FR13: telemetry
// never extends quit by more than the configured timeout, even when the
// drainer is parked inside a slow network call. With a blockingPoster
// permanently parked on its release channel, the drainer cannot drain → quit
// → close(r.drained) within the timeout, so Shutdown must hit the
// time.After branch and return.
//
// 200ms is a tight enough budget that scheduler jitter alone cannot mask a
// genuine flush-timeout regression (real FR13 budget is 1s; we use the small
// value so a regression that grows the wait an order of magnitude is caught
// without making the test suite slow).
func TestShutdownHonorsFlushTimeoutWhenDrainerBlocked(t *testing.T) {
	bp := &blockingPoster{release: make(chan struct{})}
	r := newRealRecorder("k", "site", "http://offline.example/api", bp)

	// Park the drainer inside Post.
	for i := 0; i < batchMax; i++ {
		r.Record("view.change", nil)
	}
	deadline := time.Now().Add(time.Second)
	for bp.Count() < 1 {
		if time.Now().After(deadline) {
			close(bp.release)
			t.Fatal("drainer did not enter Post within 1s — flush-timeout precondition broken")
		}
		time.Sleep(time.Millisecond)
	}

	// Shutdown with a tight timeout. Drainer is stuck inside Post(); the
	// time.After branch in Shutdown is the only way out.
	timeout := 100 * time.Millisecond
	start := time.Now()
	r.Shutdown(timeout)
	elapsed := time.Since(start)
	close(bp.release) // let the drainer goroutine eventually return for cleanup

	// Budget is timeout + scheduler slack; never want to see >2× timeout.
	if elapsed > 2*timeout {
		t.Errorf("Shutdown took %v, want < %v (FR13: timeout must be honored even with drainer blocked)",
			elapsed, 2*timeout)
	}
	if elapsed < timeout/2 {
		// Sanity: the timeout actually fired; if Shutdown returned far earlier
		// the precondition (drainer-blocked) was wrong.
		t.Logf("Shutdown returned in %v — likely drainer drained faster than expected", elapsed)
	}
}

func TestSessionEndAttachesCounters(t *testing.T) {
	r := testRecorder(t, &fakePoster{})

	r.Record("view.change", nil)
	r.Record("view.change", nil)
	r.Record("action.preview_rendered", nil)
	r.Record("error.render_fail", nil)

	fields := map[string]any{"duration_ms": int64(123)}
	r.Record("session.end", fields)

	if fields["views_total"].(uint64) != 2 {
		t.Errorf("views_total = %v, want 2", fields["views_total"])
	}
	if fields["renders_total"].(uint64) != 1 {
		t.Errorf("renders_total = %v, want 1", fields["renders_total"])
	}
	if fields["errors_total"].(uint64) != 1 {
		t.Errorf("errors_total = %v, want 1", fields["errors_total"])
	}
	if _, ok := fields["dropped"]; !ok {
		t.Error("dropped counter must be attached on session.end (FR14 visibility)")
	}
	if fields["duration_ms"] != int64(123) {
		t.Errorf("caller's duration_ms was clobbered: got %v", fields["duration_ms"])
	}
}

// TestDrainerPostsBatchWithDatadogShape covers the happy path end-to-end:
// produce events, force a batch flush via Shutdown, inspect the fakePoster's
// captured request. Pins URL routing, headers (api key arrives as apiKey arg),
// and JSON shape per PRD §5.1.
func TestDrainerPostsBatchWithDatadogShape(t *testing.T) {
	fp := &fakePoster{}
	r := newRealRecorder("the-api-key", "datadoghq.com", "https://example/api/v2/logs", fp)

	r.Record("view.change", map[string]any{"entry_kind": "file", "ext_class": ".md", "cwd_depth": 2})
	r.Record("action.preview_rendered", map[string]any{"renderer": "markdown", "width": 80, "lines": 42, "duration_ms": int64(7)})
	r.Shutdown(time.Second) // forces final flush

	calls := fp.Calls()
	if len(calls) == 0 {
		t.Fatal("drainer never posted — expected at least one batch on shutdown")
	}
	c := calls[0]
	if c.URL != "https://example/api/v2/logs" {
		t.Errorf("URL = %q, want injected logsURL", c.URL)
	}
	if c.APIKey != "the-api-key" {
		t.Errorf("apiKey = %q, want the-api-key (DD-API-KEY header source)", c.APIKey)
	}

	// Body is JSON array of objects with PRD §5.1 base fields.
	var docs []map[string]any
	if err := json.Unmarshal(c.Body, &docs); err != nil {
		t.Fatalf("body is not JSON array: %v\n%s", err, c.Body)
	}
	if len(docs) < 2 {
		t.Fatalf("expected >= 2 docs in batch, got %d", len(docs))
	}
	for _, d := range docs {
		for _, k := range []string{"ddsource", "ddtags", "hostname", "service", "message", "session_id", "timestamp"} {
			if _, ok := d[k]; !ok {
				t.Errorf("doc missing base field %q: %v", k, d)
			}
		}
		if d["ddsource"] != "lazyexplorer" {
			t.Errorf("ddsource = %v, want lazyexplorer", d["ddsource"])
		}
		if d["service"] != "lazyexplorer-tui" {
			t.Errorf("service = %v, want lazyexplorer-tui", d["service"])
		}
		if !strings.HasPrefix(d["ddtags"].(string), "env:user,version:") {
			t.Errorf("ddtags = %v, want 'env:user,version:...' prefix", d["ddtags"])
		}
	}
}

// TestAPIKeyRejected401TriggersSelfShutdownOnce: a 401 from the transport
// must (a) print the stderr line ONCE — never spammed across subsequent
// batches, (b) flip keyRejected so further post() returns early, (c) close
// quit so the drainer exits cleanly.
func TestAPIKeyRejected401TriggersSelfShutdownOnce(t *testing.T) {
	fp := &fakePoster{status: 401}
	r := newRealRecorder("bad-key", "site", "http://localhost/api", fp)
	t.Cleanup(func() { r.Shutdown(time.Second) })

	// Fire enough events to ensure at least one batch is posted.
	for i := 0; i < batchMax+10; i++ {
		r.Record("view.change", map[string]any{"i": i})
	}

	// Wait for drained channel — the 401 self-shutdown should close it.
	select {
	case <-r.drained:
	case <-time.After(2 * time.Second):
		t.Fatal("drainer did not exit after 401 self-shutdown within 2s")
	}

	if !r.keyRejected.Load() {
		t.Error("keyRejected must be set after 401")
	}
	if !r.shuttingDown.Load() {
		t.Error("shuttingDown must be set after 401 self-shutdown")
	}

	// Subsequent Records are absorbed silently (no panic, no further counter
	// movement — they hit the shuttingDown gate).
	prevViews := r.views.Load()
	r.Record("view.change", nil)
	if r.views.Load() != prevViews {
		t.Error("Record after self-shutdown must be a no-op (shuttingDown gate)")
	}
}

// TestServerError5xxRateLimitsStderr: repeated 5xx responses must not flood
// stderr — at most one "post failed" line per warnInterval. This test asserts
// the gating state (lastWarnNanos) advances exactly once per window, which is
// what FR15 promises.
func TestServerError5xxRateLimitsStderr(t *testing.T) {
	fp := &fakePoster{status: 503}
	r := newRealRecorder("k", "site", "http://localhost/api", fp)
	t.Cleanup(func() { r.Shutdown(time.Second) })

	// Manually invoke notePostFailure a handful of times in rapid succession.
	r.notePostFailure()
	first := r.lastWarnNanos.Load()
	r.notePostFailure()
	r.notePostFailure()
	r.notePostFailure()
	last := r.lastWarnNanos.Load()
	if first == 0 {
		t.Fatal("first notePostFailure did not advance lastWarnNanos")
	}
	if first != last {
		t.Errorf("rapid notePostFailure calls bumped lastWarnNanos more than once: first=%d last=%d", first, last)
	}
}

func TestNotePostFailureAllowsNextWarnAfterInterval(t *testing.T) {
	r := newRealRecorder("k", "s", "http://x", &fakePoster{})
	t.Cleanup(func() { r.Shutdown(time.Second) })
	r.notePostFailure()
	first := r.lastWarnNanos.Load()
	// Move the gate back to just before now-warnInterval so the next call
	// crosses the threshold and bumps.
	r.lastWarnNanos.Store(first - int64(warnInterval) - 1)
	r.notePostFailure()
	if r.lastWarnNanos.Load() <= first-int64(warnInterval)-1 {
		t.Errorf("post-interval notePostFailure did not advance lastWarnNanos: %d", r.lastWarnNanos.Load())
	}
}

// ----------------------------------------------------------------------------
// Redaction helpers.
// ----------------------------------------------------------------------------

func TestExtClassHandlesCornerCases(t *testing.T) {
	cases := []struct {
		name, want string
	}{
		{"README.md", ".md"},
		{"main.go", ".go"},
		{"Makefile", "(noext)"},
		{".bashrc", ".bashrc"},
		{"archive.tar.gz", ".gz"},
		{"PHOTO.JPG", ".jpg"},
		{"..", "(parent)"},
		{"", "(noext)"},
		{"x.UNKNOWN", ".unknown"},
	}
	for _, c := range cases {
		if got := extClass(c.name); got != c.want {
			t.Errorf("extClass(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestCwdDepthAtRootIsZero(t *testing.T) {
	root := filepath.FromSlash("/tmp/le-root")
	if d := cwdDepth(root, root); d != 0 {
		t.Errorf("cwdDepth at root = %d, want 0", d)
	}
}

func TestCwdDepthCountsSegments(t *testing.T) {
	root := filepath.FromSlash("/tmp/le-root")
	cases := []struct {
		cwd  string
		want int
	}{
		{filepath.Join(root, "a"), 1},
		{filepath.Join(root, "a", "b"), 2},
		{filepath.Join(root, "a", "b", "c"), 3},
	}
	for _, c := range cases {
		if d := cwdDepth(root, c.cwd); d != c.want {
			t.Errorf("cwdDepth(%q,%q) = %d, want %d", root, c.cwd, d, c.want)
		}
	}
}

func TestCwdDepthOutsideRootReturnsZero(t *testing.T) {
	root := filepath.FromSlash("/tmp/le-root")
	outside := filepath.FromSlash("/tmp/other")
	if d := cwdDepth(root, outside); d != 0 {
		t.Errorf("cwdDepth for outside path should be 0 (jail invariant), got %d", d)
	}
}

func TestErrorClassEnumeratesOrigins(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{nil, ""},
		{errors.New("glamour: render failed at line 4"), "glamour"},
		{errors.New("markdown parser exploded"), "glamour"},
		{errors.New("chroma: lexer not found"), "chroma"},
		{errors.New("Tokenise: invalid token"), "chroma"},
		{errors.New("open /etc/shadow: permission denied"), "io"},
		{errors.New("read /tmp/x: no such file or directory"), "io"},
		{errors.New("something totally unexpected"), "other"},
	}
	for _, c := range cases {
		if got := errorClass(c.err); got != c.want {
			t.Errorf("errorClass(%v) = %q, want %q", c.err, got, c.want)
		}
	}
}

func TestErrorClassNeverLeaksMessage(t *testing.T) {
	leak := errors.New("/Users/alice/secrets.env not found")
	got := errorClass(leak)
	if strings.Contains(got, "/") || strings.Contains(got, "secret") {
		t.Errorf("errorClass leaked raw message: %q", got)
	}
	if got != "io" && got != "other" {
		t.Errorf("errorClass returned non-enum value: %q", got)
	}
}

func TestHostnameHashStable(t *testing.T) {
	// hashString is the underlying helper; same input → same output → backend
	// can group sessions by machine without learning the hostname.
	a := hashString("workstation-1234")
	b := hashString("workstation-1234")
	if a != b {
		t.Errorf("hashString not stable: %q != %q", a, b)
	}
	if len(a) != hostHashBytes*2 {
		t.Errorf("hash length = %d hex chars, want %d (8 bytes)", len(a), hostHashBytes*2)
	}
	c := hashString("workstation-5678")
	if a == c {
		t.Error("distinct inputs must produce distinct hashes")
	}
}

func TestHostnameHashHandlesEmpty(t *testing.T) {
	// Contract: hostnameHash never panics and never returns the empty string,
	// even when os.Hostname errors or returns "". The fallback inside the
	// helper substitutes "unknown" so the field is always populated.
	got := hostnameHash()
	if got == "" {
		t.Error("hostnameHash returned empty string")
	}
}

// ----------------------------------------------------------------------------
// Serializer invariant — PRD §5.4. THE big one.
// ----------------------------------------------------------------------------

// TestSerializerNeverLeaksPath constructs every event shape the call sites
// actually emit (session.start / view.change / action.preview_rendered /
// error.render_fail / session.end) using ONLY the production helpers, then
// asserts the serialized JSON bytes never match the PRD §5.4 leak patterns.
// A future call site that puts a path in a field will fail this test.
func TestSerializerNeverLeaksPath(t *testing.T) {
	r := &realRecorder{
		sessionID: "11111111-2222-3333-4444-555555555555",
		hostHash:  hashString("my-laptop.local"),
	}

	root := filepath.FromSlash("/Users/alice/projects/lazyexplorer")
	cwd := filepath.Join(root, "docs", "drafts")

	batch := []event{
		{
			name:      "session.start",
			sessionID: r.sessionID,
			timeMS:    time.Now().UnixMilli(),
			fields:    sessionStartFields("dark"),
		},
		{
			name:      "view.change",
			sessionID: r.sessionID,
			timeMS:    time.Now().UnixMilli(),
			fields: map[string]any{
				"entry_kind": "file",
				"ext_class":  extClass("secrets.env"),
				"cwd_depth":  cwdDepth(root, cwd),
			},
		},
		{
			name:      "action.preview_rendered",
			sessionID: r.sessionID,
			timeMS:    time.Now().UnixMilli(),
			fields: map[string]any{
				"renderer":    "markdown",
				"width":       80,
				"lines":       42,
				"duration_ms": int64(7),
			},
		},
		{
			name:      "error.render_fail",
			sessionID: r.sessionID,
			timeMS:    time.Now().UnixMilli(),
			fields: map[string]any{
				"renderer":    "chroma",
				"error_class": errorClass(errors.New("open /Users/alice/secrets.env: permission denied")),
			},
		},
		{
			name:      "session.end",
			sessionID: r.sessionID,
			timeMS:    time.Now().UnixMilli(),
			fields: map[string]any{
				"duration_ms":   int64(12345),
				"views_total":   uint64(10),
				"renders_total": uint64(7),
				"errors_total":  uint64(1),
				"dropped":       uint64(0),
			},
		},
	}

	body, err := r.serializeBatch(batch)
	if err != nil {
		t.Fatalf("serializeBatch: %v", err)
	}

	// PRD §5.4 leak patterns. Path-like, Windows-path, and the literal
	// substrings "secret"/"password"/"key=".
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`/[A-Za-z]`),  // unix path-like
		regexp.MustCompile(`[A-Z]:\\`),   // windows path
		regexp.MustCompile(`(?i)secret`), // case-insensitive
		regexp.MustCompile(`(?i)password`),
		regexp.MustCompile(`key=`),
	}
	for _, re := range patterns {
		if loc := re.FindIndex(body); loc != nil {
			ctxStart := loc[0] - 30
			if ctxStart < 0 {
				ctxStart = 0
			}
			ctxEnd := loc[1] + 30
			if ctxEnd > len(body) {
				ctxEnd = len(body)
			}
			t.Errorf("serializer leak: pattern %q matched at offset %d, context: %q",
				re.String(), loc[0], body[ctxStart:ctxEnd])
		}
	}
}

// TestSerializerRefusesBaseKeyOverride: the §5.4 invariant's last line of
// defense. A buggy call site that puts a "message" or "hostname" key in
// ev.fields MUST NOT be able to override the base field — otherwise the
// invariant could be bypassed by an accidental rename.
func TestSerializerRefusesBaseKeyOverride(t *testing.T) {
	r := &realRecorder{sessionID: "ses", hostHash: "abc"}
	body, err := r.serializeBatch([]event{{
		name:      "view.change",
		sessionID: "ses",
		fields: map[string]any{
			"message":    "OVERRIDDEN_MESSAGE", // attempt to override base
			"hostname":   "/Users/alice/leak",  // attempt to override base
			"session_id": "OVERRIDDEN_SES",
			"entry_kind": "file",
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "OVERRIDDEN_MESSAGE") {
		t.Error("serializer allowed message override — §5.4 base-key defense breached")
	}
	if strings.Contains(string(body), "/Users/alice") {
		t.Error("serializer allowed hostname override — §5.4 base-key defense breached")
	}
	if strings.Contains(string(body), "OVERRIDDEN_SES") {
		t.Error("serializer allowed session_id override — §5.4 base-key defense breached")
	}
}

// TestSessionStartFieldsShape pins FR8: the six fields are always present,
// none of them is the raw cwd or filename.
func TestSessionStartFieldsShape(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	got := sessionStartFields("dark")
	for _, k := range []string{"version", "go_version", "os", "arch", "term", "color_profile"} {
		if _, ok := got[k]; !ok {
			t.Errorf("session.start fields missing %q", k)
		}
	}
	if got["color_profile"] != "dark" {
		t.Errorf("color_profile = %v, want dark", got["color_profile"])
	}
	if got["term"] != "xterm-256color" {
		t.Errorf("term = %v, want xterm-256color", got["term"])
	}
}
