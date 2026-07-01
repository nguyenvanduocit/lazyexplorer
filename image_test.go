package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestIsImage(t *testing.T) {
	cases := map[string]bool{
		"photo.png": true, "PIC.JPG": true, "a.jpeg": true,
		"anim.gif": true, "x.webp": true, "y.bmp": true,
		"main.go": false, "doc.md": false, "notes.txt": false, "noext": false,
	}
	for name, want := range cases {
		if got := isImage(name); got != want {
			t.Errorf("isImage(%q) = %v, want %v", name, got, want)
		}
	}
}

// writePNG writes a w×h PNG to path for tests.
func writePNG(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

// TestImageToHalfBlocks pins the half-block contract (prd-inline-image-view D1): each
// cell is a `▀` with foreground = upper pixel, background = lower pixel, so a 2×2 image
// at 2 cols is ONE cell row of two cells carrying the four pixels exactly.
func TestImageToHalfBlocks(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})         // top-left  red
	img.Set(1, 0, color.RGBA{G: 255, A: 255})         // top-right green
	img.Set(0, 1, color.RGBA{B: 255, A: 255})         // bot-left  blue
	img.Set(1, 1, color.RGBA{R: 255, G: 255, A: 255}) // bot-right yellow

	lines := imageToHalfBlocks(img, 2)
	if len(lines) != 1 {
		t.Fatalf("a 2×2 image at 2 cols is one cell row; got %d rows", len(lines))
	}
	want := lipgloss.NewStyle().Foreground(lipgloss.Color("#ff0000")).Background(lipgloss.Color("#0000ff")).Render("▀") +
		lipgloss.NewStyle().Foreground(lipgloss.Color("#00ff00")).Background(lipgloss.Color("#ffff00")).Render("▀")
	if lines[0] != want {
		t.Errorf("half-block row mismatch:\n got %q\nwant %q", lines[0], want)
	}

	// A portrait image yields more cell rows than it is wide (it scrolls vertically): an
	// 4×8 image at 4 cols → 8 px tall → 4 cell rows.
	tall := image.NewRGBA(image.Rect(0, 0, 4, 8))
	if n := len(imageToHalfBlocks(tall, 4)); n != 4 {
		t.Errorf("4×8 image at 4 cols → 4 cell rows; got %d", n)
	}
}

// TestRenderImagePreviewDraws proves the renderer DRAWS a decodable image as half-block
// ANSI (pre-styled), not a metadata placeholder: a 32×18 PNG at 80 cols renders as
// multiple `▀` rows scaled to its natural width.
func TestRenderImagePreviewDraws(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "pic.png")
	writePNG(t, p, 32, 18)

	lines, preStyled, err := renderImagePreview(p, nil, 80, "")
	if err != nil {
		t.Fatalf("image renderer must not error: %v", err)
	}
	if !preStyled {
		t.Error("a drawn image is verbatim ANSI → preStyled must be true")
	}
	if len(lines) < 2 {
		t.Fatalf("a 32×18 image should draw multiple half-block rows, got %d", len(lines))
	}
	if !strings.Contains(strings.Join(lines, "\n"), "▀") {
		t.Errorf("drawn image must use the half-block glyph ▀; got %q", lines)
	}
}

// TestRenderImagePreviewFallback pins D6: a file that cannot be decoded (here a .png
// that is not a real PNG) degrades to a dim metadata line, never an error or empty pane.
func TestRenderImagePreviewFallback(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "broken.png")
	if err := os.WriteFile(p, []byte("not a real png"), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, preStyled, err := renderImagePreview(p, nil, 80, "")
	if err != nil {
		t.Fatalf("undecodable image must degrade, not error: %v", err)
	}
	if preStyled {
		t.Error("the fallback metadata line is plain → preStyled must be false")
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "image") {
		t.Errorf("fallback must be a single '(image …)' line; got %q", lines)
	}
}

// TestImagePreviewThroughModel drives selection: an image file is a renderable (binary)
// selection — placeholder while pending, the DRAWN image (pre-styled half-blocks) after
// the render lands.
func TestImagePreviewThroughModel(t *testing.T) {
	dir := t.TempDir()
	writePNG(t, filepath.Join(dir, "pic.png"), 10, 10)

	m := modelAt(t, dir, 100, 30) // cursor on pic.png (only entry)
	if m.entries[m.cursor].name != "pic.png" {
		t.Fatalf("setup: cursor on %q, want pic.png", m.entries[m.cursor].name)
	}
	if m.srcPath == "" {
		t.Error("an image selection should be renderable (srcPath set for the binary renderer)")
	}
	if !strings.Contains(strings.Join(m.preview, "\n"), "rendering image") {
		t.Errorf("while pending, placeholder should announce the image render, got %q", m.preview)
	}

	m.renderNow()
	if !m.previewPreStyled {
		t.Error("a drawn image is verbatim ANSI → previewPreStyled must be true")
	}
	if !strings.Contains(strings.Join(m.preview, "\n"), "▀") {
		t.Errorf("after render, preview should be the drawn image (half-blocks), got %q", m.preview)
	}
}
