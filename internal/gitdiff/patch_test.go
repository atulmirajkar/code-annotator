package gitdiff

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestParsePatch(t *testing.T) {
	t.Parallel()
	commit := strings.Repeat("a", 40)
	tests := []struct {
		name    string
		base    string
		current string
		patch   string
		want    []Row
	}{
		{name: "unchanged", base: "one\ntwo\n", current: "one\ntwo\n", want: []Row{
			{Kind: RowUnchanged, OldLine: 1, NewLine: 1, CurrentStart: 0, CurrentEnd: 3, BaseText: "one"},
			{Kind: RowUnchanged, OldLine: 2, NewLine: 2, CurrentStart: 4, CurrentEnd: 7, BaseText: "two"},
		}},
		{name: "replacement", base: "one\nold\nthree\n", current: "one\nnew\nthree\n", patch: "@@ -2 +2 @@\n-old\n+new\n", want: []Row{
			{Kind: RowUnchanged, OldLine: 1, NewLine: 1, CurrentStart: 0, CurrentEnd: 3, BaseText: "one"},
			{Kind: RowModified, OldLine: 2, NewLine: 2, CurrentStart: 4, CurrentEnd: 7, BaseText: "old"},
			{Kind: RowUnchanged, OldLine: 3, NewLine: 3, CurrentStart: 8, CurrentEnd: 13, BaseText: "three"},
		}},
		{name: "addition at start", base: "old\n", current: "new\nold\n", patch: "@@ -0,0 +1 @@\n+new\n", want: []Row{
			{Kind: RowAdded, NewLine: 1, CurrentStart: 0, CurrentEnd: 3},
			{Kind: RowUnchanged, OldLine: 1, NewLine: 2, CurrentStart: 4, CurrentEnd: 7, BaseText: "old"},
		}},
		{name: "uneven replacement", base: "a\nb\nc\n", current: "z\nc\n", patch: "@@ -1,2 +1 @@\n-a\n-b\n+z\n", want: []Row{
			{Kind: RowModified, OldLine: 1, NewLine: 1, CurrentStart: 0, CurrentEnd: 1, BaseText: "a"},
			{Kind: RowDeleted, OldLine: 2, BaseText: "b"},
			{Kind: RowUnchanged, OldLine: 3, NewLine: 2, CurrentStart: 2, CurrentEnd: 3, BaseText: "c"},
		}},
		{name: "CRLF offsets", base: "a\r\nold\r\n", current: "a\r\nnew\r\n", patch: "@@ -2 +2 @@\n-old\r\n+new\r\n", want: []Row{
			{Kind: RowUnchanged, OldLine: 1, NewLine: 1, CurrentStart: 0, CurrentEnd: 1, BaseText: "a"},
			{Kind: RowModified, OldLine: 2, NewLine: 2, CurrentStart: 3, CurrentEnd: 6, BaseText: "old"},
		}},
		{name: "multiple hunks and UTF-8 offsets", base: "α\nold\nmiddle\nremove\n", current: "α\nnew\nmiddle\n", patch: "@@ -2 +2 @@\n-old\n+new\n@@ -4 +3,0 @@\n-remove\n", want: []Row{
			{Kind: RowUnchanged, OldLine: 1, NewLine: 1, CurrentStart: 0, CurrentEnd: 2, BaseText: "α"},
			{Kind: RowModified, OldLine: 2, NewLine: 2, CurrentStart: 3, CurrentEnd: 6, BaseText: "old"},
			{Kind: RowUnchanged, OldLine: 3, NewLine: 3, CurrentStart: 7, CurrentEnd: 13, BaseText: "middle"},
			{Kind: RowDeleted, OldLine: 4, BaseText: "remove"},
		}},
		{name: "no newline marker", base: "old", current: "new", patch: "@@ -1 +1 @@\n-old\n\\ No newline at end of file\n+new\n\\ No newline at end of file\n", want: []Row{
			{Kind: RowModified, OldLine: 1, NewLine: 1, CurrentStart: 0, CurrentEnd: 3, BaseText: "old"},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParsePatch("main.go", commit, []byte(test.base), []byte(test.current), []byte(test.patch))
			if err != nil {
				t.Fatalf("ParsePatch() error = %v", err)
			}
			if got.Path != "main.go" || got.BasePath != "main.go" || got.BaseCommit != commit || !reflect.DeepEqual(got.Rows, test.want) {
				t.Fatalf("ParsePatch() = %#v, want rows %#v", got, test.want)
			}
		})
	}
}

func TestParsePatchRejectsInvalidInput(t *testing.T) {
	t.Parallel()
	commit := strings.Repeat("a", 40)
	tests := []struct {
		name    string
		commit  string
		base    []byte
		current []byte
		patch   []byte
	}{
		{name: "invalid commit", commit: "abc", base: []byte("a\n"), current: []byte("a\n")},
		{name: "binary patch", commit: commit, patch: []byte("GIT binary patch\n")},
		{name: "invalid UTF-8", commit: commit, base: []byte{0xff}},
		{name: "oversized", commit: commit, patch: make([]byte, maxPatchBytes+1)},
		{name: "malformed header", commit: commit, base: []byte("a\n"), current: []byte("b\n"), patch: []byte("@@ invalid @@\n-a\n+b\n")},
		{name: "truncated hunk", commit: commit, base: []byte("a\n"), current: []byte("b\n"), patch: []byte("@@ -1 +1 @@\n-a\n")},
		{name: "base mismatch", commit: commit, base: []byte("a\n"), current: []byte("b\n"), patch: []byte("@@ -1 +1 @@\n-wrong\n+b\n")},
		{name: "current mismatch", commit: commit, base: []byte("a\n"), current: []byte("b\n"), patch: []byte("@@ -1 +1 @@\n-a\n+wrong\n")},
		{name: "overlapping hunks", commit: commit, base: []byte("a\nb\n"), current: []byte("x\ny\n"), patch: []byte("@@ -1 +1 @@\n-a\n+x\n@@ -1 +1 @@\n-a\n+y\n")},
		{name: "count exceeds header", commit: commit, base: []byte("a\nb\n"), current: []byte("x\n"), patch: []byte("@@ -1 +1 @@\n-a\n-b\n+x\n")},
		{name: "difference without hunk", commit: commit, base: []byte("a\n"), current: []byte("b\n"), patch: []byte("diff --git a/main.go b/main.go\n")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParsePatch("main.go", test.commit, test.base, test.current, test.patch)
			if !errors.Is(err, ErrUnsupportedPatch) {
				t.Fatalf("ParsePatch() error = %v, want ErrUnsupportedPatch", err)
			}
		})
	}
}
