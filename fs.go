package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	_ "image/gif"  // register GIF decoder for image.DecodeConfig
	_ "image/jpeg" // register JPEG decoder
	_ "image/png"  // register PNG decoder
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/nguyenvanduocit/glamour/v2"
	glamourstyles "github.com/nguyenvanduocit/glamour/v2/styles"
	gitignore "github.com/sabhiram/go-gitignore"
	"github.com/sahilm/fuzzy"
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
	// maxWalkEntries caps walkTree defensively (PRD FR14). A project past this
	// size is well outside the vibe-code companion scope; capping keeps the walk
	// and the subsequent fuzzy filter sub-second instead of letting a pathological
	// tree (a node_modules someone forgot to .gitignore) balloon memory.
	maxWalkEntries = 100_000

	// maxSearchResults is how many filtered matches the list pane shows at once
	// (PRD D11). Past this the result is a scroll-fest, not a glance — the user
	// types another character to refine instead. 500 renders sub-millisecond.
	maxSearchResults = 500
)

// walkTree walks root recursively into a flat []entry whose names are relative
// to root (e.g. "docs/prd-search.md"), the source the fuzzy filter ranks over.
// It is the project-wide-search counterpart to readDir's single-directory listing.
//
// Exclusions (PRD §5.3, FR3, D7):
//   - .git/ is skipped unconditionally — even when the project's .gitignore does
//     not mention it (ripgrep-default; nobody wants object hashes in results).
//   - the root .gitignore is honored. A directory match is tested against both
//     rel and rel+"/" because a trailing-slash pattern ("node_modules/") matches
//     the dir's contents but NOT the bare dir name — testing rel+"/" lets us
//     SkipDir the whole subtree instead of descending and filtering file-by-file.
//   - symlinks are neither followed nor included: following risks an infinite
//     loop and, worse, leaking entries from outside the jail root.
//
// Resilience (FR13, FR14): a permission error on a sub-directory skips that
// subtree (fs.SkipDir) rather than aborting the walk; the entry count is capped
// at maxWalkEntries (fs.SkipAll). The result is sorted alpha by relPath so an
// empty query (FR4) shows a stable, predictable listing. The root itself is
// never emitted. ignore is nil when the project has no .gitignore — then only
// the hardcoded .git/ skip applies, which is a sane default.
func walkTree(root string) ([]entry, error) {
	// A missing or unparseable .gitignore degrades to ignore == nil — the walk
	// continues with only the hardcoded .git/ skip (FR13: never fail the walk
	// over a bad ignore file). The parse error is discarded rather than surfaced:
	// it is rare and recoverable, and the search status bar is already carrying
	// the prompt + indexing/0-results chips. The user-visible symptom of a missing
	// ignore is "too many results", which they refine away by typing.
	ignore, _ := gitignore.CompileIgnoreFile(filepath.Join(root, ".gitignore"))

	out := make([]entry, 0, 256)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// A read/permission error on a directory → skip its subtree and keep
			// going; on a file → just skip it. Never bubble the error up (FR13).
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if path == root {
			return nil // never emit the root itself
		}
		if d.IsDir() && d.Name() == ".git" {
			return fs.SkipDir // D7: always skip .git/, regardless of .gitignore
		}
		// Skip symlinks without following them (FR3): no loops, no jail leak.
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		if ignore != nil {
			if d.IsDir() {
				// A dir matches a trailing-slash pattern only WITH the slash, and a
				// bare-name pattern WITHOUT it — test both so either pattern shape
				// prunes the whole subtree.
				if ignore.MatchesPath(rel) || ignore.MatchesPath(rel+"/") {
					return fs.SkipDir
				}
			} else if ignore.MatchesPath(rel) {
				return nil
			}
		}
		e := entry{name: rel, isDir: d.IsDir()}
		if info, infoErr := d.Info(); infoErr == nil {
			e.size = info.Size()
			e.modTime = info.ModTime()
		}
		out = append(out, e)
		if len(out) >= maxWalkEntries {
			return fs.SkipAll // FR14: defensive cap on pathologically large trees
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out, err
}

// filterSearch returns up to `limit` entries matching query, fuzzy-ranked by
// score descending (PRD §5.4, FR4/FR5). An empty query returns the first `limit`
// entries unchanged — walkTree already alpha-sorted them, so this is the
// "browse everything" view. A non-empty query runs sahilm/fuzzy (case-
// insensitive, D5) over the entry names and maps each match back to its entry
// via Match.Index. The fuzzy result is already score-sorted, so the slice keeps
// the closest match on top.
func filterSearch(entries []entry, query string, limit int) []entry {
	if query == "" {
		if len(entries) > limit {
			return entries[:limit]
		}
		return entries
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.name
	}
	matches := fuzzy.Find(query, names)
	if len(matches) > limit {
		matches = matches[:limit]
	}
	out := make([]entry, 0, len(matches))
	for _, mt := range matches {
		out = append(out, entries[mt.Index])
	}
	return out
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
	// The width is 2, narrower than the terminal/`cat` default of 8: the preview
	// is a slim column beside the agent, where an 8-wide tab stop burns horizontal
	// room and pushes code off-screen. Two spaces keep nesting legible while
	// maximizing visible content. It is applied uniformly — plain text, code and
	// markdown (normalizeText) and the diff view (diffHunks) — so one file's
	// indentation reads identically in every preview mode.
	previewTabWidth = 2
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
	return highlightCodeBg(source, name, "")
}

// highlightCodeBg is highlightCode with an optional background WASH (bgHex != "")
// painted behind every token. The diff renderer uses it to tint an added line green and
// a removed line red (D11): because the wash is set on each chroma token, it survives
// chroma's per-token "\x1b[0m" resets — a whole-line lipgloss background would be
// clobbered mid-line. bgHex == "" clears the background (code inherits the panel
// background), the default for the plain code preview.
func highlightCodeBg(source, name, bgHex string) ([]string, error) {
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
	var bg chroma.Colour // zero clears the background (code inherits the panel bg)
	if bgHex != "" {
		bg = chroma.MustParseColour(bgHex)
	}
	style, err := styles.Get(codeHighlightStyle).Builder().Transform(
		func(e chroma.StyleEntry) chroma.StyleEntry { e.Background = bg; return e },
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
func renderCodePreview(path string, content []byte, _ int, _ string) ([]string, bool, error) {
	lines, err := highlightCode(string(content), filepath.Base(path))
	if err != nil {
		return nil, false, err
	}
	if lines == nil { // defensive: matches() already gated this to real code
		return plainLines(content), false, nil
	}
	// Lines are returned full-width (no truncation): the preview pane windows
	// them horizontally at render time (renderHWindow / hSlice), which is what
	// makes horizontal scroll possible (prd-horizontal-scroll-preview). At
	// hscroll 0 the window shows the same leading columns the old truncation did.
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

// renderImagePreview draws a raster image inline in the preview pane as half-block
// ANSI (prd-inline-image-view): it decodes the file (png/jpeg/gif via stdlib), scales
// to the pane width, and renders via imageToHalfBlocks. Returns pre-styled ANSI lines
// (preStyled=true) so renderPreview keeps the SGR verbatim. It never errors — a file
// it cannot decode (webp/bmp have no stdlib decoder, or a corrupt image) degrades to a
// dim metadata line (D6), so the pane is never empty. The decode + scale is heavy but
// runs off the Update goroutine (the registry render Cmd in syncPreview is async).
func renderImagePreview(path string, _ []byte, width int, _ string) ([]string, bool, error) {
	if width <= 0 {
		return imageFallback(path, "pane too narrow"), false, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return imageFallback(path, "could not open"), false, nil
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return imageFallback(path, "inline preview not supported"), false, nil
	}
	lines := imageToHalfBlocks(img, width)
	if len(lines) == 0 {
		return imageFallback(path, "inline preview not supported"), false, nil
	}
	return lines, true, nil
}

// imageFallback is the dim metadata line shown when an image cannot be drawn (D6):
// it reports format + dimensions (when DecodeConfig can read the header) and size, so
// even an undrawable image tells the user what it is. plain (preStyled=false caller).
func imageFallback(path, reason string) []string {
	var size int64
	if info, err := os.Stat(path); err == nil {
		size = info.Size()
	}
	f, err := os.Open(path)
	if err != nil {
		return []string{dimStyle.Render("(image — " + humanSize(size) + ", " + reason + ")")}
	}
	defer f.Close()
	cfg, format, derr := image.DecodeConfig(f)
	if derr != nil {
		return []string{dimStyle.Render("(image — " + humanSize(size) + ", " + reason + ")")}
	}
	return []string{dimStyle.Render(fmt.Sprintf("(image %s — %d×%d, %s — %s)",
		strings.ToUpper(format), cfg.Width, cfg.Height, humanSize(size), reason))}
}

// imageToHalfBlocks renders img as half-block ANSI lines fit to `cols` cells wide
// (prd-inline-image-view D1/D3). Each text cell is a `▀` (upper half block) whose
// FOREGROUND is the upper pixel and BACKGROUND the lower pixel, so one cell shows two
// vertically-stacked pixels — a `cols × rows` cell grid carries `cols × rows·2` pixels.
// The image is scaled (nearest-neighbor, no dep) to `cols` px wide — never UP past its
// natural width (a tiny icon stays crisp) — and a proportional pixel height, so aspect
// is preserved (the 1×2 cell shape falls out of the px math, no fudge factor). The last
// cell row of an odd-height image draws only its top pixel (no background). Pure +
// width-agnostic-of-height: a tall image yields more rows than the pane and scrolls
// vertically like any preview.
func imageToHalfBlocks(img image.Image, cols int) []string {
	b := img.Bounds()
	imgW, imgH := b.Dx(), b.Dy()
	if imgW <= 0 || imgH <= 0 || cols <= 0 {
		return nil
	}
	w := cols
	if w > imgW {
		w = imgW // do not upscale beyond the image's natural width
	}
	hpx := (w*imgH + imgW/2) / imgW // scaled pixel height, aspect-preserving (rounded)
	if hpx < 1 {
		hpx = 1
	}
	rows := (hpx + 1) / 2 // two pixel rows per cell
	lines := make([]string, rows)
	var sb strings.Builder
	for r := 0; r < rows; r++ {
		sb.Reset()
		topPy, botPy := 2*r, 2*r+1
		for x := 0; x < w; x++ {
			sx := b.Min.X + x*imgW/w
			top := lipglossColorAt(img, sx, b.Min.Y+topPy*imgH/hpx)
			st := lipgloss.NewStyle().Foreground(top)
			if botPy < hpx {
				st = st.Background(lipglossColorAt(img, sx, b.Min.Y+botPy*imgH/hpx))
			}
			sb.WriteString(st.Render("▀"))
		}
		lines[r] = sb.String()
	}
	return lines
}

// lipglossColorAt is the 8-bit hex lipgloss color of the pixel at (x, y). image RGBA()
// returns 16-bit alpha-premultiplied channels; the high byte is the 8-bit value (alpha
// is ignored — an image preview treats pixels as opaque).
func lipglossColorAt(img image.Image, x, y int) color.Color {
	r, g, bl, _ := img.At(x, y).RGBA()
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, bl>>8))
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
// The resolved config is copied by value before mutation, then tweaked so the
// rendered output reads as tight as the code preview (chroma emits flush-left
// content with no inter-block blank rows). Copying keeps glamour's
// package-level style config untouched.
//
// Spacing tweaks vs the upstream DarkStyleConfig:
//   - Document.BlockPrefix / BlockSuffix → "" so the document doesn't add a
//     leading or trailing blank row inside the preview pane.
//   - Heading.BlockSuffix → "" so a heading butts straight against the next
//     block instead of leaving a blank row below it.
//   - Document.Margin → 0 so content starts at col 0 of the preview pane,
//     matching chroma's flush-left code rendering (the two preview modes look
//     equally dense beside each other).
//
// RowBorder is enabled so tables render a divider between every row (and not
// just under the header).
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

	// Tighten vertical and horizontal spacing so markdown reads as tight as code.
	styleCfg.Document.BlockPrefix = ""
	styleCfg.Document.BlockSuffix = ""
	styleCfg.Heading.BlockSuffix = ""
	zero := uint(0)
	styleCfg.Document.Margin = &zero

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
