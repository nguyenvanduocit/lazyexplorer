package main

// telemetry.go is the ONLY boundary between the rest of the app and Datadog.
// Update / refreshPreview / applyPreview / syncPreview never import net/http
// (or any wire format) — they call Record on a Recorder. The default is the
// no-op recorder; InitTelemetry upgrades to the real one when LE_TELEMETRY=1
// and DD_API_KEY is set (PRD §5.2, FR6/FR7).
//
// Concurrency invariant: Record runs on the Update goroutine and MUST NOT
// block. The drainer goroutine is the SOLE reader of r.ch and the SOLE writer
// to the network. Lifecycle is fenced by r.quit (signal to exit) + r.drained
// (signal that drainer has flushed and returned). r.quit is closed exactly
// once via sync.Once so Shutdown and the 401/403 self-shutdown can both reach
// for it safely.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// buildVersion is the lazyexplorer version stamped into every telemetry event
// (ddtags). Override at release time via: -ldflags="-X main.buildVersion=v0.2.0".
var buildVersion = "0.1.0"

// Tunables. All const so the drainer's contract is statically inspectable.
const (
	// chanCap is the event channel buffer size. PRD §5.8/FR14 — 256 absorbs
	// bursty navigation while bounding memory; overflow is dropped, not grown.
	chanCap = 256
	// batchMax flushes the in-flight batch early once accumulated, so a busy
	// session does not wait the full batchInterval to send. ≤ chanCap so the
	// drainer never starves the producer.
	batchMax = 100
	// batchInterval is the cadence of the flush ticker — bounded latency for a
	// steady-state session that never reaches batchMax. PRD D6 = 5s.
	batchInterval = 5 * time.Second
	// httpTimeout is the per-request deadline. Keep well above batchInterval so
	// a single slow POST does not double up against the next flush.
	httpTimeout = 10 * time.Second
	// warnInterval rate-limits the stderr "post failed" line per FR15 — at
	// most one line per minute regardless of how many batches fail.
	warnInterval = time.Minute
	// hostHashBytes is how many bytes of the sha256(hostname) hex we keep —
	// PRD §5.4 calls for "8 bytes hex prefix" so backends can cluster sessions
	// from the same machine without learning the hostname.
	hostHashBytes = 8
)

// Recorder is the one telemetry surface the rest of the app talks to. The
// implementation is either noopRecorder (default; absorbs every call with zero
// side effect) or *realRecorder (enqueues for the drainer goroutine). Active()
// exists so hot paths can skip a syscall (time.Now in syncPreview, PRD §5.3)
// when the no-op is in place — without leaking the implementation type through
// the model.
type Recorder interface {
	Record(name string, fields map[string]any)
	Shutdown(timeout time.Duration)
	Active() bool
}

// event is the in-memory shape of one telemetry event before serialization.
// serializeBatch maps it into the Datadog Logs HTTP intake JSON shape per
// PRD §5.1; keeping the in-memory form transport-agnostic means a future
// transport (DogStatsD, OTLP) can drop in without touching call sites.
type event struct {
	name      string
	sessionID string
	timeMS    int64
	fields    map[string]any
}

// httpPoster is the transport seam — the real client is httpClientPoster; tests
// inject a fake that asserts request shape and controls status codes. Returning
// (statusCode, error) keeps 5xx vs network-failure distinct so post() can route
// 401/403 to self-shutdown and others to FR15's rate-limited stderr.
type httpPoster interface {
	Post(url, apiKey string, body []byte) (statusCode int, err error)
}

type httpClientPoster struct {
	client *http.Client
}

func newHTTPClientPoster() *httpClientPoster {
	return &httpClientPoster{client: &http.Client{Timeout: httpTimeout}}
}

func (h *httpClientPoster) Post(url, apiKey string, body []byte) (int, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("DD-API-KEY", apiKey)
	resp, err := h.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body) // free the connection promptly
	return resp.StatusCode, nil
}

// noopRecorder is the zero-overhead default: when telemetry is off the rest of
// the app still calls Record everywhere, but every call lands here and returns
// immediately. FR6 mandates "bytes-for-bytes identical TUI when off" — that
// invariant lives in this struct having no fields, no goroutine, no syscall.
type noopRecorder struct{}

func (noopRecorder) Record(string, map[string]any) {}
func (noopRecorder) Shutdown(time.Duration)        {}
func (noopRecorder) Active() bool                  { return false }

// realRecorder owns the buffered channel, the session id, the running totals,
// and the drainer goroutine. Record IS non-blocking — a full channel
// increments dropped and bails (FR14). Counters live here, not on model, so
// session.end's automatic snapshot reflects the cumulative totals regardless
// of which goroutine emitted the increments.
type realRecorder struct {
	ch chan event

	// Counters — atomic so Record (Update goroutine) and serializeBatch
	// (drainer goroutine) can both read/write without locking.
	dropped atomic.Uint64
	views   atomic.Uint64
	renders atomic.Uint64
	errors  atomic.Uint64

	// Identity, set once at construction; safe for concurrent read.
	sessionID string
	site      string
	apiKey    string
	logsURL   string
	hostHash  string

	// Lifecycle. quit is closed (exactly once, via quitOnce) by Shutdown OR by
	// post() on a 401/403; drained is closed by drain() on exit so Shutdown can
	// wait up to its timeout for a clean flush. shuttingDown gates Record so
	// producers stop trying to send immediately after quit closes.
	quit         chan struct{}
	drained      chan struct{}
	quitOnce     sync.Once
	shuttingDown atomic.Bool

	// Transport.
	client httpPoster

	// FR15: lastWarnNanos holds the unix nano of the last "post failed" stderr
	// line. notePostFailure compares-and-swaps against it so the cadence is
	// at most once per warnInterval regardless of how many batches fail.
	lastWarnNanos atomic.Int64

	// keyRejected is set once on the first 401/403 response so the rejection
	// stderr line prints exactly once (FR15 spec carve-out for auth failures
	// — they aren't transient, retrying would just spam).
	keyRejected atomic.Bool
}

// InitTelemetry resolves env vars per PRD §5.7 and returns the matching
// Recorder. FR6: when LE_TELEMETRY is unset / not in {1,true,yes}, return
// noopRecorder — no goroutine, no network, no extra syscall on the hot path.
// FR7: when LE_TELEMETRY is set but DD_API_KEY is missing, print one stderr
// line and return noopRecorder; the explorer keeps working unchanged.
func InitTelemetry() Recorder {
	if !envTruthy(os.Getenv("LE_TELEMETRY")) {
		return noopRecorder{}
	}
	apiKey := os.Getenv("DD_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "lazyexplorer: telemetry enabled but DD_API_KEY missing — disabled")
		return noopRecorder{}
	}
	site := os.Getenv("DD_SITE")
	if site == "" {
		site = "datadoghq.com" // PRD D7 default
	}
	logsURL := os.Getenv("DD_LOGS_URL")
	if logsURL == "" {
		logsURL = "https://http-intake.logs." + site + "/api/v2/logs"
	}
	return newRealRecorder(apiKey, site, logsURL, newHTTPClientPoster())
}

// newRealRecorder constructs the recorder and starts the drainer goroutine.
// The httpPoster seam lets tests inject a fake that asserts request shape
// without standing up an httptest.Server in every test (those that need
// per-status-code assertions still spin a server through the seam).
func newRealRecorder(apiKey, site, logsURL string, client httpPoster) *realRecorder {
	r := &realRecorder{
		ch:        make(chan event, chanCap),
		sessionID: uuid.NewString(),
		site:      site,
		apiKey:    apiKey,
		logsURL:   logsURL,
		hostHash:  hostnameHash(),
		quit:      make(chan struct{}),
		drained:   make(chan struct{}),
		client:    client,
	}
	go r.drain()
	return r
}

// Active reports whether telemetry is wired to the real recorder, so the model
// can skip a time.Now syscall in syncPreview when the no-op is in place
// (PRD §5.3 "Chỉ đo khi tel != noop"). The interface-level off path is
// noopRecorder.Active() → false; *realRecorder is always wired.
func (r *realRecorder) Active() bool { return true }

// Record enqueues without blocking. Callers run on the Update goroutine —
// Record MUST NOT block any of them. A full channel drops the event and bumps
// dropped; session.end auto-attaches the running totals so a backend query
// can see how much was lost.
func (r *realRecorder) Record(name string, fields map[string]any) {
	if r.shuttingDown.Load() {
		return
	}

	// Update per-event counters BEFORE constructing the event so session.end
	// — which auto-attaches them — sees its own contribution in the snapshot.
	switch name {
	case "view.change":
		r.views.Add(1)
	case "action.preview_rendered":
		r.renders.Add(1)
	case "error.render_fail":
		r.errors.Add(1)
	}

	if name == "session.end" {
		if fields == nil {
			fields = map[string]any{}
		}
		fields["views_total"] = r.views.Load()
		fields["renders_total"] = r.renders.Load()
		fields["errors_total"] = r.errors.Load()
		fields["dropped"] = r.dropped.Load()
	}

	ev := event{
		name:      name,
		sessionID: r.sessionID,
		timeMS:    time.Now().UnixMilli(),
		fields:    fields,
	}

	// Two-step send so a Shutdown that races against Record never tries to
	// push to a closed channel. The first select skips if quit is already
	// closed; the second prefers an immediate enqueue OR a quit signal OR a
	// drop — never blocks.
	select {
	case <-r.quit:
		return
	default:
	}
	select {
	case r.ch <- ev:
	case <-r.quit:
		return
	default:
		r.dropped.Add(1)
	}
}

// Shutdown signals the drainer to exit, then waits up to timeout for a clean
// flush. Idempotent — Shutdown from main.go's defer and from a 401-triggered
// self-shutdown can both call this without panic. FR13: never extends quit
// past timeout, even if the network hangs.
func (r *realRecorder) Shutdown(timeout time.Duration) {
	r.quitOnce.Do(func() {
		r.shuttingDown.Store(true)
		close(r.quit)
	})
	if timeout <= 0 {
		// Even with no timeout the drainer should exit promptly because quit is
		// closed; give it one short tick to drain remaining events before
		// returning so a test that calls Shutdown(0) immediately after Record
		// still sees the post happen when the fake transport is fast.
		select {
		case <-r.drained:
		case <-time.After(10 * time.Millisecond):
		}
		return
	}
	select {
	case <-r.drained:
	case <-time.After(timeout):
	}
}

// drain is the sole reader of r.ch. It batches events up to batchMax (early
// flush) or batchInterval (steady-state cadence) and POSTs each batch through
// r.client. On quit it does one final non-blocking sweep of any remaining
// events, posts them, and returns — close(r.drained) signals Shutdown.
func (r *realRecorder) drain() {
	defer close(r.drained)
	ticker := time.NewTicker(batchInterval)
	defer ticker.Stop()

	batch := make([]event, 0, batchMax)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		r.post(batch)
		batch = batch[:0]
	}

	for {
		select {
		case ev, ok := <-r.ch:
			if !ok {
				flush()
				return
			}
			batch = append(batch, ev)
			if len(batch) >= batchMax {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-r.quit:
			// Final drain: pull anything left in the channel without blocking,
			// then post it as one last batch. The producer has already stopped
			// (shuttingDown gate in Record) so ch will not grow further.
			for {
				select {
				case ev := <-r.ch:
					batch = append(batch, ev)
				default:
					flush()
					return
				}
			}
		}
	}
}

// post serializes a batch and sends it to Datadog Logs HTTP intake. Status
// routing per PRD §5.8:
//   - 2xx → silently OK (the success path is invisible to the UI by design)
//   - 401/403 → keyRejected flag + one stderr line + self-shutdown via r.quit
//     (auth failures are persistent; retry would just spam)
//   - everything else (5xx, network error, timeout) → FR15 rate-limited stderr
//     line, drop this batch, keep draining
func (r *realRecorder) post(batch []event) {
	if r.keyRejected.Load() {
		return
	}

	body, err := r.serializeBatch(batch)
	if err != nil {
		// A serialize failure means a programmer error somewhere upstream
		// (a non-JSON-encodable value made it into ev.fields). Treat it the
		// same as a network failure — rate-limited stderr, drop the batch.
		r.notePostFailure()
		return
	}

	status, err := r.client.Post(r.logsURL, r.apiKey, body)
	if err != nil {
		r.notePostFailure()
		return
	}

	switch {
	case status >= 200 && status < 300:
		return
	case status == 401 || status == 403:
		if r.keyRejected.CompareAndSwap(false, true) {
			fmt.Fprintln(os.Stderr, "lazyexplorer: telemetry api key rejected — disabled")
		}
		// Self-shutdown — close quit so drain() exits on its next loop. quitOnce
		// makes this safe alongside main.go's defer Shutdown.
		r.quitOnce.Do(func() {
			r.shuttingDown.Store(true)
			close(r.quit)
		})
	default:
		r.notePostFailure()
	}
}

// notePostFailure prints PRD FR15's stderr line at most once per warnInterval.
// The compare-and-swap on lastWarnNanos races cleanly: at most one caller
// observes (now-prev >= warnInterval) AND wins the CAS, so the message is
// printed exactly once per window even under burst failures.
func (r *realRecorder) notePostFailure() {
	now := time.Now().UnixNano()
	prev := r.lastWarnNanos.Load()
	if now-prev < int64(warnInterval) {
		return
	}
	if !r.lastWarnNanos.CompareAndSwap(prev, now) {
		return
	}
	fmt.Fprintln(os.Stderr, "lazyexplorer: telemetry post failed (will retry silently)")
}

// serializeBatch maps []event into the Datadog Logs HTTP intake JSON shape
// per PRD §5.1. Base fields are always merged in; event-specific fields layer
// on top, EXCEPT a key collision with a base field is silently dropped — this
// is the §5.4 invariant's last line of defense against a future call site
// that accidentally puts a path under "message" or similar. The same defense
// runs in tests via TestSerializerRefusesBaseKeyOverride.
func (r *realRecorder) serializeBatch(batch []event) ([]byte, error) {
	base := map[string]struct{}{
		"ddsource":   {},
		"ddtags":     {},
		"hostname":   {},
		"service":    {},
		"message":    {},
		"session_id": {},
		"timestamp":  {},
	}
	out := make([]map[string]any, 0, len(batch))
	for _, ev := range batch {
		m := map[string]any{
			"ddsource":   "lazyexplorer",
			"ddtags":     "env:user,version:" + buildVersion,
			"hostname":   r.hostHash,
			"service":    "lazyexplorer-tui",
			"message":    ev.name,
			"session_id": ev.sessionID,
			"timestamp":  ev.timeMS,
		}
		for k, v := range ev.fields {
			if _, isBase := base[k]; isBase {
				continue // §5.4 defense — base fields are invariant
			}
			m[k] = v
		}
		out = append(out, m)
	}
	return json.Marshal(out)
}

// envTruthy parses LE_TELEMETRY's truthiness per PRD FR6 — "1"/"true"/"yes"
// (case-insensitive). Anything else, including empty, leaves telemetry OFF.
func envTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes":
		return true
	}
	return false
}

// ============================================================================
// Redaction helpers (PRD §5.4 — D5). Pure functions; the serializer-level
// invariant test (TestSerializerNeverLeaksPath) pins the no-leak contract
// against drift.
// ============================================================================

// extClass returns the redacted file-type bucket used in view.change events
// (FR9). Path leakage is impossible: only the extension (lowercased) survives,
// and well-known sentinels stand in for "no extension" and the synthetic
// parent entry. Examples: "README.md" → ".md", "Makefile" → "(noext)",
// ".." → "(parent)".
func extClass(name string) string {
	if name == ".." {
		return "(parent)"
	}
	ext := strings.ToLower(filepath.Ext(name))
	if ext == "" {
		return "(noext)"
	}
	return ext
}

// cwdDepth reports how many segments below root the cwd sits — 0 for the
// jail root itself, 1 for an immediate child, etc. Used in view.change
// (FR9) so the backend can see "user is N levels deep" without learning
// any actual path component.
func cwdDepth(root, cwd string) int {
	if cwd == root {
		return 0
	}
	rel, err := filepath.Rel(root, cwd)
	if err != nil || rel == "" || rel == "." {
		return 0
	}
	// filepath.Rel on a cwd outside root could yield ".." segments; depth is
	// nonsensical in that case and we report 0 rather than a negative.
	if strings.HasPrefix(rel, "..") {
		return 0
	}
	return strings.Count(rel, string(filepath.Separator)) + 1
}

// errorClass enumerates renderer error origins (FR11) so the raw error
// message — which may carry a path or filename — never leaves the process.
// The classification is best-effort substring matching; a future refactor
// could swap to errors.As once each renderer's error type is settled. Empty
// string for nil — callers gate on that.
func errorClass(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "glamour") || strings.Contains(msg, "markdown"):
		return "glamour"
	case strings.Contains(msg, "chroma") || strings.Contains(msg, "lexer") ||
		strings.Contains(msg, "tokenise"):
		return "chroma"
	case strings.Contains(msg, "open ") || strings.Contains(msg, "read ") ||
		strings.Contains(msg, "no such file") || strings.Contains(msg, "permission denied"):
		return "io"
	}
	return "other"
}

// hostnameHash returns sha256(os.Hostname())[:hostHashBytes] hex so a backend
// can cluster sessions by machine without learning the hostname. On error or
// empty hostname → "unknown". Called once at recorder construction; the
// result lives in realRecorder.hostHash so the drainer hot path never re-runs
// the syscall.
func hostnameHash() string {
	name, err := os.Hostname()
	if err != nil || name == "" {
		return "unknown"
	}
	return hashString(name)
}

// hashString is the underlying sha256-prefix-hex helper, factored out so tests
// can verify stability (same input → same output) without monkeypatching
// os.Hostname.
func hashString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:hostHashBytes])
}

// sessionStartFields builds the FR8 payload for session.start. Pulled out so
// the test can verify the contract without spinning a real program.
func sessionStartFields(renderStyle string) map[string]any {
	return map[string]any{
		"version":       buildVersion,
		"go_version":    runtime.Version(),
		"os":            runtime.GOOS,
		"arch":          runtime.GOARCH,
		"term":          os.Getenv("TERM"),
		"color_profile": renderStyle,
	}
}
