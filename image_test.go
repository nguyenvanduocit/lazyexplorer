package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

// TestRenderImagePreviewMetadata proves the scaffold renderer reads real header
// metadata (format + dimensions) and returns it as a plain placeholder line —
// the binary-renderer path, distinct from the text renderers.
func TestRenderImagePreviewMetadata(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "pic.png")
	writePNG(t, p, 32, 18)

	lines, preStyled, err := renderImagePreview(p, nil, 80, "")
	if err != nil {
		t.Fatalf("scaffold renderer must not error: %v", err)
	}
	if preStyled {
		t.Error("image placeholder is plain text → preStyled must be false (so fitWidth still applies)")
	}
	if len(lines) != 1 {
		t.Fatalf("want a single placeholder line, got %d: %q", len(lines), lines)
	}
	if !strings.Contains(lines[0], "32×18") {
		t.Errorf("metadata line missing dimensions: %q", lines[0])
	}
	if !strings.Contains(strings.ToUpper(lines[0]), "PNG") {
		t.Errorf("metadata line missing format: %q", lines[0])
	}
}

// TestImagePreviewThroughModel drives selection: an image file is a renderable
// (binary) selection — placeholder while pending, metadata after the render —
// and stays non-pre-styled so the plain line is width-fit normally.
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
	if m.previewPreStyled {
		t.Error("image metadata is a plain line → previewPreStyled must be false")
	}
	if !strings.Contains(strings.Join(m.preview, "\n"), "10×10") {
		t.Errorf("after render, preview should show 10×10 metadata, got %q", m.preview)
	}
}
