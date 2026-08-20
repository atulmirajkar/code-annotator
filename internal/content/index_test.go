package content

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestIndex(t *testing.T) {
	t.Parallel()

	rootPath := t.TempDir()
	mustWriteFile(t, filepath.Join(rootPath, "README.md"), []byte("home"))
	mustWriteFile(t, filepath.Join(rootPath, "alpha.MD"), []byte("alpha"))
	mustWriteFile(t, filepath.Join(rootPath, "guide", "Intro.md"), []byte("intro"))
	mustWriteFile(t, filepath.Join(rootPath, "guide", "zebra.md"), []byte("zebra"))
	mustWriteFile(t, filepath.Join(rootPath, "guide", "image.png"), []byte("png"))
	mustWriteFile(t, filepath.Join(rootPath, ".private.md"), []byte("private"))
	mustWriteFile(t, filepath.Join(rootPath, ".git", "internal.md"), []byte("git"))

	root, err := Open(rootPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	index, err := root.Index()
	if err != nil {
		t.Fatalf("Index() error = %v", err)
	}

	want := []Document{
		{Path: "alpha.MD", Name: "alpha.MD", Directory: ""},
		{Path: "guide/Intro.md", Name: "Intro.md", Directory: "guide"},
		{Path: "guide/zebra.md", Name: "zebra.md", Directory: "guide"},
		{Path: "README.md", Name: "README.md", Directory: ""},
	}
	if !reflect.DeepEqual(index.Documents, want) {
		t.Fatalf("Index().Documents = %#v, want %#v", index.Documents, want)
	}
	if got, want := index.DefaultPath, "README.md"; got != want {
		t.Fatalf("Index().DefaultPath = %q, want %q", got, want)
	}
}

func TestIndexUsesFirstDocumentWithoutReadme(t *testing.T) {
	t.Parallel()

	rootPath := t.TempDir()
	mustWriteFile(t, filepath.Join(rootPath, "z.md"), []byte("z"))
	mustWriteFile(t, filepath.Join(rootPath, "A.md"), []byte("a"))
	root, err := Open(rootPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	index, err := root.Index()
	if err != nil {
		t.Fatalf("Index() error = %v", err)
	}
	if got, want := index.DefaultPath, "A.md"; got != want {
		t.Fatalf("Index().DefaultPath = %q, want %q", got, want)
	}
}

func TestIndexEmpty(t *testing.T) {
	t.Parallel()

	root, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	index, err := root.Index()
	if err != nil {
		t.Fatalf("Index() error = %v", err)
	}
	if len(index.Documents) != 0 || index.DefaultPath != "" {
		t.Fatalf("Index() = %#v, want empty index", index)
	}
}

func TestIndexRefreshesFromDisk(t *testing.T) {
	t.Parallel()

	rootPath := t.TempDir()
	mustWriteFile(t, filepath.Join(rootPath, "first.md"), []byte("first"))
	root, err := Open(rootPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	first, err := root.Index()
	if err != nil {
		t.Fatalf("first Index() error = %v", err)
	}
	if got := documentPaths(first); !reflect.DeepEqual(got, []string{"first.md"}) {
		t.Fatalf("first Index() paths = %v", got)
	}

	if err := os.Remove(filepath.Join(rootPath, "first.md")); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	mustWriteFile(t, filepath.Join(rootPath, "second.md"), []byte("second"))

	second, err := root.Index()
	if err != nil {
		t.Fatalf("second Index() error = %v", err)
	}
	if got := documentPaths(second); !reflect.DeepEqual(got, []string{"second.md"}) {
		t.Fatalf("second Index() paths = %v", got)
	}
}

func TestIndexExcludesEscapingSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks may require elevated privileges on Windows")
	}
	t.Parallel()

	rootPath := t.TempDir()
	outsidePath := filepath.Join(t.TempDir(), "outside.md")
	mustWriteFile(t, outsidePath, []byte("outside"))
	if err := os.Symlink(outsidePath, filepath.Join(rootPath, "escape.md")); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}
	mustWriteFile(t, filepath.Join(rootPath, "inside.md"), []byte("inside"))

	root, err := Open(rootPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	index, err := root.Index()
	if err != nil {
		t.Fatalf("Index() error = %v", err)
	}
	if got := documentPaths(index); !reflect.DeepEqual(got, []string{"inside.md"}) {
		t.Fatalf("Index() paths = %v, want [inside.md]", got)
	}
}

func documentPaths(index Index) []string {
	paths := make([]string, len(index.Documents))
	for i, document := range index.Documents {
		paths[i] = document.Path
	}
	return paths
}
