package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
)

// telemetryFlushTimeout caps how long Shutdown waits for the drainer to flush
// the pending event batch when the user quits (PRD FR13). Past this the
// remaining batch is dropped and the process exits — telemetry must never
// extend quit by more than one second.
const telemetryFlushTimeout = time.Second

// detectRenderStyle resolves the preview color palette ONCE, here at startup
// while we still own the terminal in normal mode — before tea.NewProgram takes
// it over. The chosen style hint ("dark"/"light"/"notty") is handed to the model
// and reused by every async render, so a renderer never re-queries the terminal
// background from a render goroutine (which would race Bubbletea's stdin reader
// and frame writer, corrupting output). See renderMarkdown for why this matters.
func detectRenderStyle() string {
	switch colorprofile.Detect(os.Stdout, os.Environ()) {
	case colorprofile.NoTTY, colorprofile.Ascii:
		return "notty" // not a color terminal (e.g. piped) — glamour's plain style
	}
	if lipgloss.HasDarkBackground(os.Stdin, os.Stdout) {
		return "dark"
	}
	return "light"
}

func main() {
	// Root = the directory lazyexplorer is launched in. It is the jail:
	// navigation can descend below it but never above.
	start := "."
	if len(os.Args) > 1 {
		// Short non-TUI commands resolved here so packagers (Homebrew formula
		// test do block, debian postinst, etc.) can probe the binary without
		// driving the alt-screen. buildVersion is the ldflags-stamped release
		// version (see telemetry.go).
		switch os.Args[1] {
		case "--version", "-v", "version":
			fmt.Println("lazyexplorer", buildVersion)
			return
		case "--help", "-h", "help":
			fmt.Println("Usage: lazyexplorer [DIR]")
			fmt.Println("  DIR       directory to explore (defaults to current working directory)")
			fmt.Println("  --version print version and exit")
			fmt.Println("  --help    print this help and exit")
			fmt.Println()
			fmt.Println("Keys: q/ctrl+c quit · j/k or ↑↓ move · l/enter open · h/← up")
			fmt.Println("      d delete · r rename · J/ctrl+d page-down · K/ctrl+u page-up")
			fmt.Println()
			fmt.Println("Telemetry (opt-in): LE_TELEMETRY=1 DD_API_KEY=… — see README.md.")
			return
		}
		start = os.Args[1]
	}
	root, err := filepath.Abs(start)
	if err != nil {
		fmt.Fprintln(os.Stderr, "lazyexplorer:", err)
		os.Exit(1)
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		fmt.Fprintln(os.Stderr, "lazyexplorer: not a directory:", root)
		os.Exit(1)
	}

	// Telemetry is wired BEFORE newModel so the model's initial reload (which
	// fires the first refreshPreview → first view.change) is observed by the
	// real recorder when LE_TELEMETRY=1. When telemetry is off (FR6) tel is
	// the no-op and every call site is a zero-cost return.
	tel := InitTelemetry()
	startedAt := time.Now()
	defer func() {
		tel.Record("session.end", map[string]any{
			"duration_ms": time.Since(startedAt).Milliseconds(),
		})
		tel.Shutdown(telemetryFlushTimeout)
	}()

	renderStyle := detectRenderStyle()
	tel.Record("session.start", sessionStartFields(renderStyle))

	m := newModel(root, tel)
	m.renderStyle = renderStyle

	// Alt-screen and mouse mode are declared on the model's View (bubbletea v2),
	// not as program options.
	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "lazyexplorer:", err)
		os.Exit(1)
	}
}
