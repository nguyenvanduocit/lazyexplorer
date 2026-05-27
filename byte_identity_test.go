package main

// PRD FR6 invariant: when telemetry is wired and active, the rendered TUI is
// byte-for-byte identical to a build with telemetry off. View() never touches
// the Recorder; render goroutines never block on it; the only model field
// telemetry writes (renderStartedAt) is internal and not surfaced anywhere in
// the frame.
//
// This test pins the invariant against drift — any future code change that
// makes telemetry visible in View() output fails here.

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// TestFrameByteIdenticalAcrossTelemetryModes captures the initial frame in
// three telemetry configurations and asserts all three are byte-identical:
//
//   - off: explicit noopRecorder (FR6 baseline)
//   - on-noop-via-init: LE_TELEMETRY=1 with no DD_API_KEY → InitTelemetry's
//     FR7 graceful-disable path; the model still sees a noopRecorder
//   - on-real-offline: fully-wired *realRecorder with an offline/blocked
//     fakePoster — proves the active path's syscalls (time.Now in syncPreview,
//     counter atomics, channel send) leave View() untouched
func TestFrameByteIdenticalAcrossTelemetryModes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("plain content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("more plain text\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	captureFrame := func(tel Recorder) string {
		var m tea.Model = newModel(dir, tel)
		m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		// One nav step so refreshPreview fires (view.change recorded) and the
		// frame reflects a real user interaction, not just construction state.
		m, _ = m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
		return m.(model).View().Content
	}

	// Mode A: explicit noopRecorder.
	frameOff := captureFrame(noopRecorder{})

	// Mode B: InitTelemetry's FR7 graceful-disable path.
	t.Setenv("LE_TELEMETRY", "1")
	t.Setenv("DD_API_KEY", "")
	telB := InitTelemetry()
	if _, ok := telB.(noopRecorder); !ok {
		t.Fatalf("FR7: LE_TELEMETRY=1 + DD_API_KEY='' should yield noopRecorder, got %T", telB)
	}
	frameInitNoop := captureFrame(telB)

	// Mode C: fully-wired real recorder with blocked offline transport. Drainer
	// runs and stays parked on blockingPoster forever; View() must still
	// produce the same bytes as the off path.
	bp := &blockingPoster{release: make(chan struct{})}
	telC := newRealRecorder("k", "site", "http://offline.example/api/v2/logs", bp)
	t.Cleanup(func() {
		close(bp.release)
		telC.Shutdown(time.Second)
	})
	frameOn := captureFrame(telC)

	if frameOff != frameInitNoop {
		t.Errorf("FR6 violation: noopRecorder vs Init-noop produced different frames\nlen(off)=%d len(init)=%d",
			len(frameOff), len(frameInitNoop))
	}
	if frameOff != frameOn {
		t.Errorf("FR6 violation: noopRecorder vs active realRecorder produced different frames.\n"+
			"len(off)=%d len(on)=%d", len(frameOff), len(frameOn))
		// Reveal the first diverging byte to make root-causing easier.
		for i := 0; i < len(frameOff) && i < len(frameOn); i++ {
			if frameOff[i] != frameOn[i] {
				lo := i - 30
				if lo < 0 {
					lo = 0
				}
				hi := i + 30
				if hi > len(frameOff) {
					hi = len(frameOff)
				}
				t.Logf("first diff at byte %d:\noff: %q\non:  %q", i, frameOff[lo:hi], frameOn[lo:hi])
				break
			}
		}
	}
}

// TestModelTelemetryNeverAffectsViewWithMarkdownRender extends the
// byte-identity invariant to the async-render path: a markdown file selected,
// the render dispatched and applied via renderNow, and the post-render frame
// captured in both modes. The renderStartedAt field is written only in the
// active path; this test pins that "internal book-keeping never bleeds into
// the visible frame".
func TestModelTelemetryNeverAffectsViewWithMarkdownRender(t *testing.T) {
	dir := t.TempDir()
	src := "# Title\n\nbody **text**\n"
	if err := os.WriteFile(filepath.Join(dir, "doc.md"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	capture := func(tel Recorder) string {
		m := newModel(dir, tel)
		m.renderStyle = "notty" // deterministic palette (no terminal detect)
		m.width, m.height = 80, 24
		m.refreshPreview()
		m.renderNow()
		return m.View().Content
	}

	off := capture(noopRecorder{})

	bp := &blockingPoster{release: make(chan struct{})}
	on := newRealRecorder("k", "site", "http://offline.example/api/v2/logs", bp)
	t.Cleanup(func() {
		close(bp.release)
		on.Shutdown(time.Second)
	})
	onFrame := capture(on)

	if off != onFrame {
		t.Errorf("FR6 violation: post-markdown-render frame differs between off and on modes (len off=%d on=%d)",
			len(off), len(onFrame))
	}
}
