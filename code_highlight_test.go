package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// TestRendererRouting locks which renderer the registry picks per filename — the
// dispatch that makes the pipeline generic. Markdown must win over code for .md
// (registry order), source files route to code, html is covered by code for free
// (chroma has an html lexer), and plain/unknown files get no renderer.
func TestRendererRouting(t *testing.T) {
	cases := []struct {
		name       string
		renderable bool
		want       string // renderer name when renderable
	}{
		{"main.go", true, "code"},
		{"data.json", true, "code"},
		{"Dockerfile", true, "code"},
		{"index.html", true, "code"}, // html auto-covered, no dedicated renderer
		{"README.md", true, "markdown"},
		{"NOTES.MD", true, "markdown"},
		{"notes.txt", false, ""},
		{"plain", false, ""}, // no extension, no lexer
	}
	for _, c := range cases {
		r, ok := rendererFor(c.name)
		if ok != c.renderable {
			t.Errorf("rendererFor(%q): renderable=%v, want %v", c.name, ok, c.renderable)
			continue
		}
		if ok && r.name != c.want {
			t.Errorf("rendererFor(%q): renderer %q, want %q", c.name, r.name, c.want)
		}
	}
}

// TestMatchLang guards the code-detection gate: real languages map to a name,
// while plaintext/unknown map to "" so .txt/unknown files stay plain text.
func TestMatchLang(t *testing.T) {
	cases := map[string]bool{ // name → expect a non-empty language
		"main.go":    true,
		"x.json":     true,
		"Dockerfile": true,
		"a.txt":      false,
		"plain":      false,
	}
	for name, wantCode := range cases {
		if got := matchLang(name) != ""; got != wantCode {
			t.Errorf("matchLang(%q) recognized-as-code=%v, want %v (lang=%q)", name, got, wantCode, matchLang(name))
		}
	}
}

// TestHighlightCodeStyled proves chroma highlighting runs: a recognized source
// file comes back as pre-styled ANSI lines that differ from the raw text.
func TestHighlightCodeStyled(t *testing.T) {
	src := "package main\n\nfunc main() {\n\tprintln(\"hi\")\n}\n"
	lines, err := highlightCode(src, "main.go")
	if err != nil {
		t.Fatal(err)
	}
	if lines == nil {
		t.Fatal("highlightCode returned nil for a .go file — should be recognized as code")
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "\x1b[") {
		t.Error("highlighted output carries no ANSI escapes — chroma did not color it")
	}
	// Background must be cleared (Transform) so code inherits the panel background:
	// no 48;-prefixed (background) SGR should appear.
	if strings.Contains(joined, "\x1b[48;") || strings.Contains(joined, "48;2;") {
		t.Error("highlighted output sets a background color — Transform should clear it (no color stripes)")
	}
}

// TestHighlightUnrecognizedIsPlain guards no-regression: a file chroma does not
// recognize as code returns (nil, nil) so the caller keeps it as plain text.
func TestHighlightUnrecognizedIsPlain(t *testing.T) {
	lines, err := highlightCode("just some prose\nwith two lines\n", "notes.txt")
	if err != nil {
		t.Fatal(err)
	}
	if lines != nil {
		t.Errorf("highlightCode(.txt) returned %d lines, want nil (stay plain)", len(lines))
	}
}

// TestCodeMultiLineTokenKeepsColor locks PRD §5.5: chroma self-closes SGR per
// line, so every line of a multi-line block comment carries its own color — none
// drops from line 2 onward when we later split + truncate per line.
func TestCodeMultiLineTokenKeepsColor(t *testing.T) {
	src := "package main\n\n/*\nblock comment line ALPHA\nblock comment line BRAVO\n*/\nfunc main() {}\n"
	lines, err := highlightCode(src, "main.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ALPHA", "BRAVO"} {
		var found bool
		for _, ln := range lines {
			if strings.Contains(ln, want) {
				found = true
				if !strings.Contains(ln, "\x1b[") {
					t.Errorf("comment line %q carries no ANSI color — multi-line token dropped its color", want)
				}
			}
		}
		if !found {
			t.Errorf("comment line %q missing from highlighted output", want)
		}
	}
}

// TestRenderCodePreviewKeepsFullWidth proves the horizontal-scroll precondition
// (prd-horizontal-scroll-preview): a code line wider than the panel is returned
// FULL (no truncation), so the preview pane can pan across it. The width-fitting
// happens later in renderPreview's horizontal window, not in the renderer.
func TestRenderCodePreviewKeepsFullWidth(t *testing.T) {
	long := "var x = \"" + strings.Repeat("A", 400) + "\"\n"
	src := "package main\n\n" + long
	lines, preStyled, err := renderCodePreview("/tmp/main.go", []byte(src), 60, "")
	if err != nil {
		t.Fatal(err)
	}
	if !preStyled {
		t.Error("code preview must report preStyled=true (verbatim ANSI)")
	}
	widest := 0
	for _, ln := range lines {
		if w := lipgloss.Width(ln); w > widest {
			widest = w
		}
	}
	if widest <= 60 {
		t.Errorf("widest code line = %d cols, want > 60 (full width, not truncated — hscroll needs the overflow to pan)", widest)
	}
}

// TestCodePreviewThroughModel drives the real selection flow: navigating onto a
// .go file makes it a renderable selection and renderNow produces styled code;
// navigating to a .txt resets previewPreStyled so plain text gets fitWidth.
func TestCodePreviewThroughModel(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	longText := strings.Repeat("z", 300)
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte(longText+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := modelAt(t, dir, 100, 30) // a.go (0), b.txt (1); cursor on a.go
	if m.entries[m.cursor].name != "a.go" {
		t.Fatalf("setup: cursor on %q, want a.go", m.entries[m.cursor].name)
	}
	m.renderNow()
	if !m.previewPreStyled {
		t.Fatal("a.go should be pre-styled (chroma) after the render lands")
	}
	if strings.Join(m.preview, "\n") == "package main\n\nfunc main() {}\n" {
		t.Error("preview equals raw source — chroma did not run")
	}

	// Navigate to the plain .txt → previewPreStyled must reset, fitWidth applies.
	m.cursor = 1
	m.refreshPreview()
	if m.previewPreStyled {
		t.Fatal("after .go → .txt, previewPreStyled must reset to false")
	}
	w := m.previewBodyWidth()
	for _, ln := range strings.Split(m.renderPreview(w), "\n") {
		if lipgloss.Width(ln) > w {
			t.Errorf("plain .txt line width %d exceeds panel %d — fitWidth skipped after viewing code", lipgloss.Width(ln), w)
		}
	}
}
