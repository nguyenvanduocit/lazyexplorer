package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"image"
	_ "image/gif"  // register GIF decoder for image.DecodeConfig
	_ "image/jpeg" // register JPEG decoder
	_ "image/png"  // register PNG decoder
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/x/ansi"
	"github.com/nguyenvanduocit/glamour/v2"
	glamourstyles "github.com/nguyenvanduocit/glamour/v2/styles"
)

// entry is one item shown in the left panel.
type entry struct {
	name    string
	isDir   bool
	size    int64
	modTime time.Time // carried only for the poll loop's change-detection (dirSig)
}

// readDir lists a directory: dirs first (alpha), then files (alpha).
// Hidden entries are included — this is a file manager, the user wants to see them.
func readDir(dir string) ([]entry, error) {
	items, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]entry, 0, len(items))
	for _, it := range items {
		e := entry{name: it.Name(), isDir: it.IsDir()}
		if info, err := it.Info(); err == nil {
			e.size = info.Size()
			e.modTime = info.ModTime()
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].isDir != out[j].isDir {
			return out[i].isDir // dirs before files
		}
		return strings.ToLower(out[i].name) < strings.ToLower(out[j].name)
	})
	return out, nil
}

// dirSig is a cheap content-fingerprint of a directory listing: it folds each
// entry's name, kind, size and mtime into one hash. The poll loop compares it
// tick-to-tick and rebuilds the view only when it changes, so an unchanged
// directory costs one os.ReadDir and nothing else — no preview re-read, no
// markdown re-render. mtime is what lets it notice an in-place edit that keeps
// the file's size unchanged.
func dirSig(entries []entry) uint64 {
	h := fnv.New64a()
	var num [8]byte
	for _, e := range entries {
		h.Write([]byte(e.name))
		if e.isDir {
			h.Write([]byte{1})
		} else {
			h.Write([]byte{0})
		}
		binary.LittleEndian.PutUint64(num[:], uint64(e.size))
		h.Write(num[:])
		binary.LittleEndian.PutUint64(num[:], uint64(e.modTime.UnixNano()))
		h.Write(num[:])
	}
	return h.Sum64()
}

// withinRoot reports whether target is root or a descendant of root.
// Both paths must be absolute & cleaned. This is the jail guard: navigation
// must never produce a cwd outside root.
func withinRoot(root, target string) bool {
	if target == root {
		return true
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

const (
	maxPreviewBytes = 256 * 1024 // don't read more than 256KB for a preview
	maxPreviewLines = 2000       // cap rendered lines to keep View cheap

	// previewTabWidth is how many spaces a tab expands to in file previews.
	// Tabs MUST be expanded at load, because lipgloss/ansi width measurement
	// counts a tab as 0 columns while a terminal renders it as a jump to the
	// next tab stop. An unexpanded tab therefore makes a line measure narrower
	// than it actually draws: fitWidth leaves it untruncated, the terminal
	// hard-wraps the overflow onto a second row, the preview panel outgrows its
	// declared height, and the whole frame spills past the screen so it scrolls.
	// 8 matches the terminal default (and `cat`), so previewed indentation reads
	// the same as the file does elsewhere. (Pattern mirrors charmbracelet/crush.)
	previewTabWidth = 8
)

// readPreviewBytes reads up to maxPreviewBytes of path and classifies what it
// found: kind is "text" / "binary" / "empty" / "error". The returned bytes are
// the raw file content for "text"/"binary"; for "error" they hold the error
// message (callers turn it into a placeholder); for "empty" they are nil. This
// is the shared read step: plain preview, the renderers (markdown/code consume
// the text, image reads the path), and the kind gate all build on it.
func readPreviewBytes(path string, size int64) ([]byte, string) {
	f, err := os.Open(path)
	if err != nil {
		return []byte(err.Error()), "error"
	}
	defer f.Close()

	buf := make([]byte, min64(size, maxPreviewBytes))
	n, _ := f.Read(buf)
	buf = buf[:n]

	switch {
	case isBinary(buf):
		return buf, "binary"
	case n == 0:
		return nil, "empty"
	default:
		return buf, "text"
	}
}

// normalizeText makes raw text display-ready: normalize line endings and expand
// tabs so a line's measured width equals the width the terminal draws (see
// previewTabWidth). Rendering stays a pure width-fitter that never has to know
// about tabs or CRLF — and code highlighting (FR8) gets the same expanded source
// so chroma's output measures right too.
func normalizeText(content []byte) string {
	s := string(content)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "") // stray lone CR (old-Mac endings)
	s = strings.ReplaceAll(s, "\t", strings.Repeat(" ", previewTabWidth))
	return s
}

// plainLines turns raw text bytes into display-ready preview lines (normalized,
// capped). This is the plain-text preview body and the instant placeholder shown
// while a renderer works.
func plainLines(content []byte) []string {
	lines := strings.Split(normalizeText(content), "\n")
	if len(lines) > maxPreviewLines {
		lines = lines[:maxPreviewLines] // cap rendered lines to keep View cheap
	}
	return lines
}

// placeholderLines is the preview body for a non-text file (binary/empty/error).
// For "error", content holds the error message from readPreviewBytes.
func placeholderLines(kind string, content []byte, size int64) []string {
	switch kind {
	case "binary":
		return []string{"(binary file — " + humanSize(size) + ", preview skipped)"}
	case "empty":
		return []string{"(empty file)"}
	case "error":
		return []string{"⚠ " + string(content)}
	}
	return nil
}

// previewFile reads a file for the right panel: display-ready lines + a kind
// marker ("text"/"binary"/"empty"/"error"). It composes the read + classify +
// plain/placeholder steps; refreshPreview uses those steps directly when a
// renderer is involved, but plain callers (and tests) keep this one-shot form.
func previewFile(path string, size int64) ([]string, string) {
	content, kind := readPreviewBytes(path, size)
	if kind == "text" {
		return plainLines(content), kind
	}
	return placeholderLines(kind, content, size), kind
}

// previewDir returns the raw entries shown when the right panel previews a
// folder. The fs layer hands data back; the view layer (renderEntryRow + the
// renderPreview folder branch) decides how each row looks, so the list pane
// and the folder preview never drift in format (PRD §5.3). An empty folder
// returns (nil, nil) — the view emits the placeholder. An unreadable folder
// returns the readDir error; the caller (refreshPreview) decides how to
// surface it (kept out of previewEntries so an error never reads as "empty").
func previewDir(path string) ([]entry, error) {
	return readDir(path)
}

// previewRenderer turns a recognized file into preview lines. The async preview
// machinery (model.syncPreview/applyPreview) is entirely type-agnostic — a
// renderer is the only type-specific piece, so supporting a new file type means
// registering one here, nothing else.
//
//   - matches decides, by filename, whether this renderer handles the file.
//   - binary is false for renderers that need decoded UTF-8 text (markdown, code):
//     they are skipped on a binary file. It is true for renderers that work on
//     raw bytes / the path itself (image), which run regardless of the kind gate.
//   - render receives the file's path and up-to-maxPreviewBytes content (text
//     renderers use the normalized content; image reads the path), the current
//     preview body width, and the app-level style hint ("dark"/"light"/"notty");
//     each renderer maps that hint to its own engine (markdown→glamour style;
//     code/image ignore it). It returns the lines, preStyled (whether they carry
//     verbatim ANSI / are pre-fit to width, so renderPreview skips fitWidth — a
//     plain placeholder returns false), and an error (→ fallback to plain source).
type previewRenderer struct {
	name    string
	matches func(name string) bool
	binary  bool
	render  func(path string, content []byte, width int, style string) (lines []string, preStyled bool, err error)
}

// previewRenderers is the registry, tried in order. Markdown is before code so a
// .md file (which chroma's lexer would also match) renders via glamour, not as
// highlighted source. Append-only at init; never mutated at runtime, so looking a
// renderer up by filename each render is the source of truth (no stored pointer).
var previewRenderers = []previewRenderer{
	{name: "markdown", matches: isMarkdown, render: renderMarkdownPreview},
	{name: "code", matches: isCode, render: renderCodePreview},
	{name: "image", matches: isImage, binary: true, render: renderImagePreview},
}

// rendererFor returns the first registered renderer that matches name.
func rendererFor(name string) (previewRenderer, bool) {
	for _, r := range previewRenderers {
		if r.matches(name) {
			return r, true
		}
	}
	return previewRenderer{}, false
}

// isMarkdown reports whether name is a markdown file by extension (case-insensitive).
func isMarkdown(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".md", ".markdown":
		return true
	}
	return false
}

// renderMarkdownPreview adapts glamour markdown rendering to the previewRenderer
// contract: content is already normalized text, output is pre-styled ANSI.
func renderMarkdownPreview(_ string, content []byte, width int, style string) ([]string, bool, error) {
	lines, err := renderMarkdown(string(content), width, style)
	if err != nil {
		return nil, false, err
	}
	return lines, true, nil
}

// matchLang returns the chroma lexer's language name for a file, or "" when the
// file is not recognized as source code: a nil match, or the plaintext/fallback
// lexer, count as "not code" so .txt/.log and unknown files stay plain text.
func matchLang(name string) string {
	l := lexers.Match(name)
	if l == nil {
		return ""
	}
	switch l.Config().Name {
	case "plaintext", "fallback":
		return ""
	}
	return l.Config().Name
}

// isCode reports whether name is a source file chroma can syntax-highlight. It is
// the code renderer's matcher — registered after markdown so a .md file (which
// chroma's markdown lexer would also match) renders via glamour, not as code.
func isCode(name string) bool { return matchLang(name) != "" }

// highlightCode syntax-colors source by the language inferred from name, returning
// ANSI lines. chroma emits a full open+close SGR run per line, so each line is
// self-contained — a later per-line truncate (renderCodePreview) cannot drop the
// color of a multi-line token. It does NOT wrap to width (that is width-dependent).
// Returns (nil, nil) when name is not recognized as code (caller falls back).
//
// The truecolor formatter prints exact hex colors; the Transform clears each
// token's background so code inherits the panel background instead of painting
// style-defined background stripes — mirrors charmbracelet/crush's highlighter.
func highlightCode(source, name string) ([]string, error) {
	l := lexers.Match(name)
	if l == nil {
		return nil, nil
	}
	switch l.Config().Name {
	case "plaintext", "fallback":
		return nil, nil
	}
	l = chroma.Coalesce(l)

	it, err := l.Tokenise(nil, source)
	if err != nil {
		return nil, err
	}
	style, err := styles.Get(codeHighlightStyle).Builder().Transform(
		func(e chroma.StyleEntry) chroma.StyleEntry { e.Background = 0; return e },
	).Build()
	if err != nil {
		style = styles.Get(codeHighlightStyle) // rare: use the style as-is
	}
	var buf bytes.Buffer
	if err := formatters.Get("terminal16m").Format(&buf, style, it); err != nil {
		return nil, err
	}
	out := strings.Split(buf.String(), "\n")
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1] // drop trailing blank line (mirrors renderMarkdown)
	}
	return out, nil
}

// renderCodePreview adapts chroma highlighting to the previewRenderer contract:
// content is normalized source text; chroma does not wrap, so each highlighted
// line is truncated ANSI-aware to the panel width (the escape-safe Truncate keeps
// the trailing "…" and re-closes the SGR). Output is pre-styled ANSI. style is
// unused — code uses codeHighlightStyle (the app hint drives only markdown).
func renderCodePreview(path string, content []byte, width int, _ string) ([]string, bool, error) {
	lines, err := highlightCode(string(content), filepath.Base(path))
	if err != nil {
		return nil, false, err
	}
	if lines == nil { // defensive: matches() already gated this to real code
		return plainLines(content), false, nil
	}
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], width, "…")
	}
	return lines, true, nil
}

// isImage reports whether name is a raster image by extension (case-insensitive).
func isImage(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
		return true
	}
	return false
}

// renderImagePreview is a scaffold renderer (binary: true) — it does not draw the
// image (terminal-graphics is future work) but proves the binary-renderer path:
// it reads the file header for real metadata and shows it, an upgrade over the old
// "(binary file — skipped)". It never errors — a header it cannot decode (webp/bmp
// have no stdlib decoder) still reports the size. preStyled is false: the line is
// a plain placeholder, so renderPreview keeps fitWidth on it.
func renderImagePreview(path string, _ []byte, _ int, _ string) ([]string, bool, error) {
	var size int64
	if info, err := os.Stat(path); err == nil {
		size = info.Size()
	}
	f, err := os.Open(path)
	if err != nil {
		return []string{dimStyle.Render("(image — " + humanSize(size) + ", could not open)")}, false, nil
	}
	defer f.Close()
	cfg, format, err := image.DecodeConfig(f)
	if err != nil {
		return []string{dimStyle.Render("(image — " + humanSize(size) + ", inline preview not supported)")}, false, nil
	}
	line := fmt.Sprintf("(image %s — %d×%d, %s — inline preview not supported)",
		strings.ToUpper(format), cfg.Width, cfg.Height, humanSize(size))
	return []string{dimStyle.Render(line)}, false, nil
}

// renderMarkdown renders raw markdown to ANSI-styled, width-wrapped lines via
// glamour (the same engine glow uses). The returned lines carry ANSI codes, so
// the caller must NOT run them through fitWidth (rune-slicing would corrupt the
// escapes) — glamour has already wrapped to width.
//
// style is a glamour style name ("dark"/"light"/"notty"), resolved ONCE at
// startup (see detectRenderStyle in main.go). Resolving it to an explicit
// ansi.StyleConfig here is load-bearing for the async render: it never queries
// the terminal background (an OSC escape round-trip on stdin/stdout that a
// background render goroutine would race against Bubbletea's own stdin reader
// and frame writer, corrupting output or hanging). DefaultStyles holds the
// palette by name; an empty/unknown name falls back to the plain "notty" style
// so non-program callers (tests, pipes) stay deterministic.
//
// The resolved config is copied by value before mutation, then RowBorder is
// enabled so tables render a divider between every row (and not just under the
// header). Copying keeps glamour's package-level style config untouched.
func renderMarkdown(raw string, width int, style string) ([]string, error) {
	if style == "" {
		style = glamourstyles.NoTTYStyle
	}
	cfg, ok := glamourstyles.DefaultStyles[style]
	if !ok {
		cfg = glamourstyles.DefaultStyles[glamourstyles.NoTTYStyle]
	}
	styleCfg := *cfg
	rowBorder := true
	styleCfg.Table.RowBorder = &rowBorder

	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(styleCfg),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return nil, err
	}
	out, err := r.Render(raw)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(out, "\n")
	// glamour appends a trailing newline (and document-margin blank lines); drop
	// purely-empty trailing lines so the preview doesn't end in dead scroll space.
	// Styled-but-blank lines keep their ANSI, so TrimSpace won't strip those.
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines, nil
}

// isBinary uses the common heuristic: a NUL byte in the leading bytes means binary.
func isBinary(buf []byte) bool {
	if bytes.IndexByte(buf, 0) >= 0 {
		return true
	}
	// Also treat clearly-invalid UTF-8 as binary.
	return len(buf) > 0 && !utf8.Valid(buf)
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(n)/float64(div), "KMGTPE"[exp])
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
