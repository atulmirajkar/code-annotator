package gitdiff

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestBuildFileDiff(t *testing.T) {
	t.Parallel()
	requireGit(t)
	tests := []struct {
		name          string
		beforeBase    func(*testing.T, string)
		afterBase     func(*testing.T, string) string
		contentPrefix string
		documentPath  string
		wantKinds     []RowKind
		wantBase      []string
	}{
		{name: "unchanged", afterBase: func(_ *testing.T, _ string) string { return "initial" }, wantKinds: []RowKind{RowUnchanged}, wantBase: []string{"initial"}},
		{name: "tracked replacement", afterBase: func(t *testing.T, root string) string {
			writeRepositoryFile(t, root, "README.md", "updated")
			return "updated"
		}, wantKinds: []RowKind{RowModified}, wantBase: []string{"initial"}},
		{name: "untracked file", afterBase: func(t *testing.T, root string) string {
			writeRepositoryFile(t, root, "new.go", "package added\n")
			return "package added\n"
		}, documentPath: "new.go", wantKinds: []RowKind{RowAdded}, wantBase: []string{""}},
		{name: "committed after base", afterBase: func(t *testing.T, root string) string {
			writeRepositoryFile(t, root, "new.go", "package committed\n")
			runRepositoryGit(t, root, "add", "new.go")
			runRepositoryGit(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "add source")
			return "package committed\n"
		}, documentPath: "new.go", wantKinds: []RowKind{RowAdded}, wantBase: []string{""}},
		{name: "nested content root", beforeBase: commitNestedDiffFile, afterBase: func(t *testing.T, root string) string {
			writeRepositoryFile(t, root, "docs/main.go", "package changed\n")
			return "package changed\n"
		}, contentPrefix: "docs", documentPath: "main.go", wantKinds: []RowKind{RowModified}, wantBase: []string{"package original"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			repository := newRepository(t)
			if test.beforeBase != nil {
				test.beforeBase(t, repository)
			}
			contentRoot := repository
			documentPath := test.documentPath
			if documentPath == "" {
				documentPath = "README.md"
			}
			if test.contentPrefix != "" {
				contentRoot = filepath.Join(repository, test.contentPrefix)
			}
			configuration, err := Open(context.Background(), contentRoot, "HEAD")
			if err != nil {
				t.Fatalf("Open() error = %v", err)
			}
			currentText := test.afterBase(t, repository)
			got, err := configuration.BuildFileDiff(context.Background(), documentPath, []byte(currentText))
			if err != nil {
				t.Fatalf("BuildFileDiff() error = %v", err)
			}
			kinds := make([]RowKind, 0, len(got.Rows))
			baseText := make([]string, 0, len(got.Rows))
			for _, row := range got.Rows {
				kinds = append(kinds, row.Kind)
				baseText = append(baseText, row.BaseText)
			}
			if !reflect.DeepEqual(kinds, test.wantKinds) || !reflect.DeepEqual(baseText, test.wantBase) {
				t.Fatalf("BuildFileDiff() rows = %#v, want kinds %#v, base %#v", got.Rows, test.wantKinds, test.wantBase)
			}
		})
	}
}

func TestBuildFileDiffRejectsInvalidInput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		configuration Config
		document      string
		current       []byte
	}{
		{name: "empty configuration", document: "main.go"},
		{name: "traversal", configuration: Config{RepositoryRoot: "/repository", BaseCommit: strings.Repeat("a", 40)}, document: "../main.go"},
		{name: "backslash", configuration: Config{RepositoryRoot: "/repository", BaseCommit: strings.Repeat("a", 40)}, document: `dir\main.go`},
		{name: "oversized current", configuration: Config{RepositoryRoot: "/repository", BaseCommit: strings.Repeat("a", 40)}, document: "main.go", current: make([]byte, maxDiffSourceBytes+1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := test.configuration.BuildFileDiff(context.Background(), test.document, test.current); err == nil {
				t.Fatal("BuildFileDiff() error = nil, want failure")
			}
		})
	}
}

func TestAddedFilePatch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		current string
		want    string
	}{
		{name: "empty"},
		{name: "one line", current: "package main", want: "@@ -0,0 +1,1 @@\n+package main\n"},
		{name: "empty line", current: "\n", want: "@@ -0,0 +1,1 @@\n+\n"},
		{name: "CRLF", current: "one\r\ntwo\r\n", want: "@@ -0,0 +1,2 @@\n+one\n+two\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := addedFilePatch([]byte(test.current))
			if err != nil {
				t.Fatalf("addedFilePatch() error = %v", err)
			}
			if string(got) != test.want {
				t.Fatalf("addedFilePatch() = %q, want %q", got, test.want)
			}
		})
	}
}

// commitNestedDiffFile creates one base blob below a nested viewer root.
func commitNestedDiffFile(t *testing.T, repository string) {
	t.Helper()
	if err := os.Mkdir(filepath.Join(repository, "docs"), 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	writeRepositoryFile(t, repository, "docs/main.go", "package original\n")
	runRepositoryGit(t, repository, "add", "docs/main.go")
	runRepositoryGit(t, repository, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "nested source")
}
