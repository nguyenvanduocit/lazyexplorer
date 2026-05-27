package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWithinRoot(t *testing.T) {
	root := "/home/user/proj"
	cases := []struct {
		target string
		want   bool
	}{
		{"/home/user/proj", true},
		{"/home/user/proj/sub", true},
		{"/home/user/proj/a/b/c", true},
		{"/home/user", false},       // parent — must be blocked
		{"/home/user/other", false}, // sibling
		{"/", false},
		{"/home/user/proj-evil", false}, // prefix trap, not a child
	}
	for _, c := range cases {
		if got := withinRoot(root, filepath.Clean(c.target)); got != c.want {
			t.Errorf("withinRoot(%q, %q) = %v, want %v", root, c.target, got, c.want)
		}
	}
}

func TestReadDirOrdersDirsFirst(t *testing.T) {
	dir := t.TempDir()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.WriteFile(filepath.Join(dir, "zebra.txt"), []byte("x"), 0o644))
	must(os.WriteFile(filepath.Join(dir, "apple.txt"), []byte("x"), 0o644))
	must(os.Mkdir(filepath.Join(dir, "src"), 0o755))
	must(os.Mkdir(filepath.Join(dir, "Build"), 0o755))

	entries, err := readDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Build", "src", "apple.txt", "zebra.txt"} // dirs first, then files; case-insensitive alpha
	if len(entries) != len(want) {
		t.Fatalf("got %d entries, want %d", len(entries), len(want))
	}
	for i, w := range want {
		if entries[i].name != w {
			t.Errorf("entry[%d] = %q, want %q", i, entries[i].name, w)
		}
	}
}

func TestIsBinary(t *testing.T) {
	if !isBinary([]byte{0x7f, 0x45, 0x4c, 0x00}) {
		t.Error("NUL byte should mark binary")
	}
	if isBinary([]byte("hello world\nutf8 café")) {
		t.Error("valid utf8 text wrongly flagged binary")
	}
}

func TestFitWidth(t *testing.T) {
	if got := fitWidth("hello", 10); got != "hello" {
		t.Errorf("no truncation expected, got %q", got)
	}
	got := fitWidth("hello world", 6)
	if []rune(got)[len([]rune(got))-1] != '…' {
		t.Errorf("expected ellipsis suffix, got %q", got)
	}
}
