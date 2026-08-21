package gitdiff

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestChangedPaths(t *testing.T) {
	t.Parallel()
	requireGit(t)
	tests := []struct {
		name          string
		beforeBase    func(*testing.T, string)
		afterBase     func(*testing.T, string)
		contentPrefix string
		want          []string
	}{
		{name: "clean worktree", want: []string{}},
		{name: "modified tracked file", afterBase: func(t *testing.T, root string) { writeRepositoryFile(t, root, "README.md", "modified") }, want: []string{"README.md"}},
		{name: "staged file", afterBase: func(t *testing.T, root string) {
			writeRepositoryFile(t, root, "staged.go", "package staged")
			runRepositoryGit(t, root, "add", "staged.go")
		}, want: []string{"staged.go"}},
		{name: "committed after frozen base", afterBase: func(t *testing.T, root string) {
			writeRepositoryFile(t, root, "committed.go", "package committed")
			runRepositoryGit(t, root, "add", "committed.go")
			runRepositoryGit(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "later")
		}, want: []string{"committed.go"}},
		{name: "untracked excludes ignored", beforeBase: commitIgnoreRule, afterBase: func(t *testing.T, root string) {
			writeRepositoryFile(t, root, "new.go", "package current")
			writeRepositoryFile(t, root, "ignored.go", "package ignored")
		}, want: []string{"new.go"}},
		{name: "deleted tracked file", beforeBase: commitNestedFiles, afterBase: func(t *testing.T, root string) {
			if err := os.Remove(filepath.Join(root, "docs", "deleted.go")); err != nil {
				t.Fatalf("Remove() error = %v", err)
			}
		}, contentPrefix: "docs", want: []string{"deleted.go"}},
		{name: "nested content excludes sibling", beforeBase: commitNestedFiles, afterBase: func(t *testing.T, root string) {
			writeRepositoryFile(t, root, "docs/current.go", "package changed")
			writeRepositoryFile(t, root, "sibling.go", "package sibling")
		}, contentPrefix: "docs", want: []string{"current.go"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			repository := newRepository(t)
			if test.beforeBase != nil {
				test.beforeBase(t, repository)
			}
			contentRoot := repository
			if test.contentPrefix != "" {
				contentRoot = filepath.Join(repository, test.contentPrefix)
			}
			configuration, err := Open(context.Background(), contentRoot, "HEAD")
			if err != nil {
				t.Fatalf("Open() error = %v", err)
			}
			if test.afterBase != nil {
				test.afterBase(t, repository)
			}
			got, err := configuration.ChangedPaths(context.Background())
			if err != nil {
				t.Fatalf("ChangedPaths() error = %v", err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("ChangedPaths() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestParseNULPaths(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		want    []string
		wantErr bool
	}{
		{name: "empty", want: []string{}},
		{name: "paths", input: "one.go\x00nested/two.go\x00", want: []string{"one.go", "nested/two.go"}},
		{name: "missing terminator", input: "one.go", wantErr: true},
		{name: "empty path", input: "one.go\x00\x00", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseNULPaths([]byte(test.input))
			if (err != nil) != test.wantErr {
				t.Fatalf("parseNULPaths() error = %v, wantErr %t", err, test.wantErr)
			}
			if !test.wantErr && !reflect.DeepEqual(got, test.want) {
				t.Fatalf("parseNULPaths() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestChangedPathsRejectsInvalidConfig(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		configuration Config
	}{
		{name: "empty"},
		{name: "abbreviated commit", configuration: Config{RepositoryRoot: "/repository", BaseCommit: "abc1234"}},
		{name: "escaping prefix", configuration: Config{RepositoryRoot: "/repository", BaseCommit: strings.Repeat("a", 40), ContentPrefix: "../outside"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := test.configuration.ChangedPaths(context.Background()); err == nil {
				t.Fatal("ChangedPaths() error = nil, want invalid configuration")
			}
		})
	}
}

// commitIgnoreRule establishes ignored content before the frozen comparison
// base so only later untracked, non-ignored files appear as changes.
func commitIgnoreRule(t *testing.T, repository string) {
	t.Helper()
	writeRepositoryFile(t, repository, ".gitignore", "ignored.go\n")
	runRepositoryGit(t, repository, "add", ".gitignore")
	runRepositoryGit(t, repository, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "ignore rule")
}

// commitNestedFiles creates the tracked content subtree used by prefix and
// deletion cases before their immutable base is resolved.
func commitNestedFiles(t *testing.T, repository string) {
	t.Helper()
	if err := os.Mkdir(filepath.Join(repository, "docs"), 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	writeRepositoryFile(t, repository, "docs/current.go", "package current")
	writeRepositoryFile(t, repository, "docs/deleted.go", "package deleted")
	runRepositoryGit(t, repository, "add", "docs")
	runRepositoryGit(t, repository, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "nested files")
}
