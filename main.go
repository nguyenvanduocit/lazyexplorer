package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// cliArgs is the parsed command line. parseArgs is kept pure (no I/O) so the flag
// grammar can be unit-tested directly (see split_test.go).
type cliArgs struct {
	start       string // directory to explore (default ".")
	showVersion bool
	showHelp    bool
	split       bool
	splitDir    string // "right" | "below" (default "right")
}

// parseArgs interprets os.Args[1:]. Unknown flags are a hard error rather than
// being treated as a directory — explicit beats a late "not a directory" stat.
func parseArgs(args []string) (cliArgs, error) {
	out := cliArgs{start: ".", splitDir: "right"}
	sawPositional := false
	for _, a := range args {
		switch {
		case a == "--version" || a == "-v" || a == "version":
			out.showVersion = true
		case a == "--help" || a == "-h" || a == "help":
			out.showHelp = true
		case a == "--split":
			out.split = true
		case strings.HasPrefix(a, "--split="):
			out.split = true
			dir := strings.TrimPrefix(a, "--split=")
			if dir != "right" && dir != "below" {
				return out, fmt.Errorf("invalid --split value %q (want right or below)", dir)
			}
			out.splitDir = dir
		case strings.HasPrefix(a, "-"):
			return out, fmt.Errorf("unknown flag %q", a)
		default:
			if sawPositional {
				return out, fmt.Errorf("unexpected argument %q (only one DIR allowed)", a)
			}
			out.start = a
			sawPositional = true
		}
	}
	return out, nil
}

// printHelp documents the CLI. buildVersion is the ldflags-stamped release version
// (see telemetry.go).
func printHelp() {
	fmt.Println("Usage: lazyexplorer [DIR] [--split[=right|below]]")
	fmt.Println("  DIR                   directory to explore (defaults to current working directory)")
	fmt.Println("  --split[=right|below] open a split pane in the current terminal and run there,")
	fmt.Println("                        leaving this pane untouched (tmux, zellij, WezTerm, Kitty,")
	fmt.Println("                        Ghostty, iTerm2; default right)")
	fmt.Println("  --version             print version and exit")
	fmt.Println("  --help                print this help and exit")
	fmt.Println()
	fmt.Println("Keys: q/ctrl+c quit · j/k or ↑↓ move · l/enter open · h/← up")
	fmt.Println("      d delete · r rename · e open in editor · J/ctrl+d page-down · K/ctrl+u page-up")
	fmt.Println()
	fmt.Println("Telemetry (opt-in): LE_TELEMETRY=1 DD_API_KEY=… — see README.md.")
}

func main() {
	args, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "lazyexplorer:", err)
		os.Exit(2)
	}

	// Short non-TUI commands resolved before anything else so packagers (Homebrew
	// formula test do block, debian postinst, etc.) can probe the binary without
	// driving the alt-screen. version/help win over --split.
	if args.showVersion {
		fmt.Println("lazyexplorer", buildVersion)
		return
	}
	if args.showHelp {
		printHelp()
		return
	}

	// Root = the directory lazyexplorer is launched in. It is the jail:
	// navigation can descend below it but never above.
	root, err := filepath.Abs(args.start)
	if err != nil {
		fmt.Fprintln(os.Stderr, "lazyexplorer:", err)
		os.Exit(1)
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		fmt.Fprintln(os.Stderr, "lazyexplorer: not a directory:", root)
		os.Exit(1)
	}

	// --split is a launcher mode: detect the terminal, open a split pane running
	// lazyexplorer there, then exit — leaving the calling pane (often an agent)
	// untouched. It short-circuits BEFORE InitTelemetry so a spawn-and-exit is not
	// recorded as a phantom session (D7). On any failure (no supported terminal,
	// spawn errored) we warn and fall through to a normal run in the current pane,
	// so --split never leaves the user empty-handed (docs/prd-split-respawn.md D8/D9).
	if args.split {
		if err := spawnSplit(args.splitDir, root); err != nil {
			fmt.Fprintln(os.Stderr, "lazyexplorer --split:", err)
		} else {
			return
		}
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
