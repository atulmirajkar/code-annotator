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
		{Path: "alpha.MD", Name: "alpha.MD", Directory: "", Kind: KindMarkdown, Language: "markdown"},
		{Path: "guide/Intro.md", Name: "Intro.md", Directory: "guide", Kind: KindMarkdown, Language: "markdown"},
		{Path: "guide/zebra.md", Name: "zebra.md", Directory: "guide", Kind: KindMarkdown, Language: "markdown"},
		{Path: "README.md", Name: "README.md", Directory: "", Kind: KindMarkdown, Language: "markdown"},
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

func TestIndexWithOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		extensions  []string
		excluded    []string
		wantPaths   []string
		wantDefault string
		wantKinds   []Kind
		wantLangs   []string
		noMarkdown  bool
	}{
		{name: "markdown only", wantPaths: []string{"README.md"}, wantDefault: "README.md", wantKinds: []Kind{KindMarkdown}, wantLangs: []string{"markdown"}},
		{name: "configured code", extensions: []string{".GO", ".cs"}, wantPaths: []string{"main.go", "obj/generated.go", "README.md", "src/App.cs", "vendor/dependency.go"}, wantDefault: "README.md", wantKinds: []Kind{KindCode, KindCode, KindMarkdown, KindCode, KindCode}, wantLangs: []string{"go", "go", "markdown", "csharp", "go"}},
		{name: "excluded base names", extensions: []string{".go"}, excluded: []string{"vendor", "OBJ"}, wantPaths: []string{"main.go", "README.md"}, wantDefault: "README.md", wantKinds: []Kind{KindCode, KindMarkdown}, wantLangs: []string{"go", "markdown"}},
		{name: "source fallback", extensions: []string{".go"}, excluded: []string{"obj"}, wantPaths: []string{"main.go", "vendor/dependency.go"}, wantDefault: "main.go", wantKinds: []Kind{KindCode, KindCode}, wantLangs: []string{"go", "go"}, noMarkdown: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			rootPath := t.TempDir()
			if !test.noMarkdown {
				mustWriteFile(t, filepath.Join(rootPath, "README.md"), []byte("readme"))
			}
			mustWriteFile(t, filepath.Join(rootPath, "main.go"), []byte("package main"))
			mustWriteFile(t, filepath.Join(rootPath, "src", "App.cs"), []byte("class App {}"))
			mustWriteFile(t, filepath.Join(rootPath, "vendor", "dependency.go"), []byte("package dependency"))
			mustWriteFile(t, filepath.Join(rootPath, "obj", "generated.go"), []byte("package generated"))
			root, err := Open(rootPath)
			if err != nil {
				t.Fatalf("Open() error = %v", err)
			}
			options, err := NewIndexOptions(test.extensions, test.excluded)
			if err != nil {
				t.Fatalf("NewIndexOptions() error = %v", err)
			}
			index, err := root.IndexWithOptions(options)
			if err != nil {
				t.Fatalf("IndexWithOptions() error = %v", err)
			}
			if got := documentPaths(index); !reflect.DeepEqual(got, test.wantPaths) {
				t.Fatalf("IndexWithOptions() paths = %v, want %v", got, test.wantPaths)
			}
			if index.DefaultPath != test.wantDefault {
				t.Fatalf("IndexWithOptions().DefaultPath = %q, want %q", index.DefaultPath, test.wantDefault)
			}
			for i, document := range index.Documents {
				if document.Kind != test.wantKinds[i] || document.Language != test.wantLangs[i] {
					t.Fatalf("IndexWithOptions().Documents[%d] = %#v, want kind %q language %q", i, document, test.wantKinds[i], test.wantLangs[i])
				}
			}
		})
	}
}

func TestNewIndexOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		extensions []string
		excluded   []string
		want       IndexOptions
		wantErr    bool
	}{
		{name: "normalizes and deduplicates", extensions: []string{" .GO ", ".go", ".TS"}, excluded: []string{" Vendor ", "vendor", "OBJ"}, want: IndexOptions{CodeExtensions: []string{".go", ".ts"}, ExcludedDirectories: []string{"vendor", "obj"}}},
		{name: "empty values", extensions: []string{""}, wantErr: true},
		{name: "extension without dot", extensions: []string{"go"}, wantErr: true},
		{name: "extension path", extensions: []string{"src/.go"}, wantErr: true},
		{name: "excluded path", excluded: []string{"build/generated"}, wantErr: true},
		{name: "parent directory", excluded: []string{".."}, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := NewIndexOptions(test.extensions, test.excluded)
			if test.wantErr {
				if err == nil {
					t.Fatalf("NewIndexOptions() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("NewIndexOptions() error = %v", err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("NewIndexOptions() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func documentPaths(index Index) []string {
	paths := make([]string, len(index.Documents))
	for i, document := range index.Documents {
		paths[i] = document.Path
	}
	return paths
}
