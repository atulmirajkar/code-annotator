package content

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestOpen(t *testing.T) {
	t.Parallel()

	t.Run("resolves directory", func(t *testing.T) {
		rootPath := t.TempDir()
		root, err := Open(rootPath)
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		if !filepath.IsAbs(root.Path()) {
			t.Fatalf("Path() = %q, want absolute path", root.Path())
		}
	})

	t.Run("rejects empty path", func(t *testing.T) {
		if _, err := Open(" "); !errors.Is(err, ErrInvalidPath) {
			t.Fatalf("Open() error = %v, want ErrInvalidPath", err)
		}
	})

	t.Run("rejects regular file", func(t *testing.T) {
		filePath := filepath.Join(t.TempDir(), "README.md")
		mustWriteFile(t, filePath, []byte("hello"))
		if _, err := Open(filePath); !errors.Is(err, ErrInvalidPath) {
			t.Fatalf("Open() error = %v, want ErrInvalidPath", err)
		}
	})

	t.Run("reports missing directory", func(t *testing.T) {
		_, err := Open(filepath.Join(t.TempDir(), "missing"))
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("Open() error = %v, want os.ErrNotExist", err)
		}
	})
}

func TestResolveFile(t *testing.T) {
	t.Parallel()

	rootPath := t.TempDir()
	mustWriteFile(t, filepath.Join(rootPath, "guide", "intro.md"), []byte("intro"))
	root, err := Open(rootPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	wantResolved, err := filepath.EvalSymlinks(filepath.Join(rootPath, "guide", "intro.md"))
	if err != nil {
		t.Fatalf("EvalSymlinks() error = %v", err)
	}

	tests := []struct {
		name    string
		path    string
		wantErr error
	}{
		{name: "nested file", path: "guide/intro.md"},
		{name: "clean relative path", path: "guide/./intro.md"},
		{name: "empty", path: "", wantErr: ErrInvalidPath},
		{name: "root", path: ".", wantErr: ErrOutsideRoot},
		{name: "absolute", path: "/etc/passwd", wantErr: ErrInvalidPath},
		{name: "lexical traversal", path: "../secret.md", wantErr: ErrOutsideRoot},
		{name: "encoded traversal", path: "%2e%2e/secret.md", wantErr: ErrOutsideRoot},
		{name: "encoded slash traversal", path: "%2e%2e%2fsecret.md", wantErr: ErrOutsideRoot},
		{name: "backslash traversal", path: `..\secret.md`, wantErr: ErrOutsideRoot},
		{name: "malformed escape", path: "%zz", wantErr: ErrInvalidPath},
		{name: "missing", path: "missing.md", wantErr: os.ErrNotExist},
		{name: "directory", path: "guide", wantErr: ErrNotRegular},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved, err := root.ResolveFile(tt.path)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("ResolveFile(%q) error = %v, want %v", tt.path, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveFile(%q) error = %v", tt.path, err)
			}
			if got, want := resolved, wantResolved; got != want {
				t.Fatalf("ResolveFile(%q) = %q, want %q", tt.path, got, want)
			}
		})
	}
}

func TestResolveFileRejectsEscapingSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks may require elevated privileges on Windows")
	}
	t.Parallel()

	rootPath := t.TempDir()
	outsidePath := filepath.Join(t.TempDir(), "secret.md")
	mustWriteFile(t, outsidePath, []byte("secret"))
	if err := os.Symlink(outsidePath, filepath.Join(rootPath, "escape.md")); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	root, err := Open(rootPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if _, err := root.ResolveFile("escape.md"); !errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("ResolveFile() error = %v, want ErrOutsideRoot", err)
	}
}

func TestReadFile(t *testing.T) {
	t.Parallel()

	rootPath := t.TempDir()
	mustWriteFile(t, filepath.Join(rootPath, "README.md"), []byte("hello"))
	root, err := Open(rootPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	data, err := root.ReadFile("README.md", 5)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if got, want := string(data), "hello"; got != want {
		t.Fatalf("ReadFile() = %q, want %q", got, want)
	}

	if _, err := root.ReadFile("README.md", 4); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("ReadFile() oversized error = %v, want ErrTooLarge", err)
	}
	if _, err := root.ReadFile("README.md", 0); err == nil {
		t.Fatal("ReadFile() with zero limit returned nil error")
	}
}

func mustWriteFile(t *testing.T, filePath string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filePath, data, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}
