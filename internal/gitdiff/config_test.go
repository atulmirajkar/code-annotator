package gitdiff

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpen(t *testing.T) {
	t.Parallel()
	requireGit(t)
	tests := []struct {
		name       string
		base       string
		content    func(*testing.T, string) string
		wantPrefix string
		wantErr    string
	}{
		{name: "repository root", base: "HEAD", content: func(_ *testing.T, root string) string { return root }},
		{name: "nested content root", base: "HEAD~0", content: nestedContentRoot, wantPrefix: "docs"},
		{name: "branch", base: "main", content: func(_ *testing.T, root string) string { return root }},
		{name: "missing revision", base: "missing", content: func(_ *testing.T, root string) string { return root }, wantErr: "not a local commit"},
		{name: "leading option", base: "--all", content: func(_ *testing.T, root string) string { return root }, wantErr: "unsafe characters"},
		{name: "newline", base: "HEAD\nmain", content: func(_ *testing.T, root string) string { return root }, wantErr: "unsafe characters"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			repository := newRepository(t)
			contentRoot := test.content(t, repository)
			configuration, err := Open(context.Background(), contentRoot, test.base)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("Open() error = %v, want containing %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Open() error = %v", err)
			}
			if configuration.RepositoryRoot != repository || configuration.ContentPrefix != test.wantPrefix || configuration.RequestedBase != test.base || !validObjectID(configuration.BaseCommit) {
				t.Fatalf("Open() = %#v, want repository %q, prefix %q, base %q", configuration, repository, test.wantPrefix, test.base)
			}
		})
	}
}

func TestOpenOutsideWorktree(t *testing.T) {
	t.Parallel()
	requireGit(t)
	tests := []struct {
		name string
		path func(*testing.T) string
	}{
		{name: "ordinary directory", path: func(t *testing.T) string { return t.TempDir() }},
		{name: "file path", path: func(t *testing.T) string {
			path := filepath.Join(t.TempDir(), "file")
			if err := os.WriteFile(path, []byte("content"), 0o644); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
			return path
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := Open(context.Background(), test.path(t), "HEAD")
			if err == nil {
				t.Fatal("Open() error = nil, want failure")
			}
		})
	}
}

func TestOpenFreezesCommit(t *testing.T) {
	t.Parallel()
	requireGit(t)
	tests := []struct {
		name string
		base string
	}{
		{name: "branch name", base: "main"},
		{name: "HEAD", base: "HEAD"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			repository := newRepository(t)
			configuration, err := Open(context.Background(), repository, test.base)
			if err != nil {
				t.Fatalf("Open() error = %v", err)
			}
			writeRepositoryFile(t, repository, "second.txt", "second")
			runRepositoryGit(t, repository, "add", "second.txt")
			runRepositoryGit(t, repository, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "second")
			current := strings.TrimSpace(runRepositoryGit(t, repository, "rev-parse", "HEAD"))
			if configuration.BaseCommit == current {
				t.Fatalf("BaseCommit moved to %q after repository changed", current)
			}
		})
	}
}

func TestBoundedBuffer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		size      int
		wantError bool
	}{
		{name: "within limit", size: maxCommandBytes},
		{name: "over limit", size: maxCommandBytes + 1, wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var buffer boundedBuffer
			_, err := buffer.Write(make([]byte, test.size))
			if errors.Is(err, ErrOutputTooLarge) != test.wantError {
				t.Fatalf("Write() error = %v, wantError %t", err, test.wantError)
			}
			if buffer.buffer.Len() > maxCommandBytes {
				t.Fatalf("buffer length = %d, max %d", buffer.buffer.Len(), maxCommandBytes)
			}
		})
	}
}

// newRepository creates one committed main branch without relying on global
// user configuration, making revision tests deterministic on developer hosts.
func newRepository(t *testing.T) string {
	t.Helper()
	repository := t.TempDir()
	runRepositoryGit(t, repository, "init", "-b", "main")
	writeRepositoryFile(t, repository, "README.md", "initial")
	runRepositoryGit(t, repository, "add", "README.md")
	runRepositoryGit(t, repository, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "initial")
	resolved, err := filepath.EvalSymlinks(repository)
	if err != nil {
		t.Fatalf("EvalSymlinks() error = %v", err)
	}
	return resolved
}

// nestedContentRoot adds the content directory used to verify Git path-prefix
// normalization beneath a temporary repository.
func nestedContentRoot(t *testing.T, repository string) string {
	t.Helper()
	path := filepath.Join(repository, "docs")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	return path
}

// writeRepositoryFile creates a test worktree file relative to its repository.
func writeRepositoryFile(t *testing.T, repository, path, value string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repository, path), []byte(value), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

// runRepositoryGit executes test-fixture Git and returns trimmed diagnostics on
// failure. Production Git execution remains isolated in runGit.
func runRepositoryGit(t *testing.T, repository string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repository}, arguments...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v error = %v: %s", arguments, err, output)
	}
	return string(output)
}

// requireGit skips integration fixtures only when the Git executable is not
// available on the test host.
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git executable is unavailable")
	}
}
