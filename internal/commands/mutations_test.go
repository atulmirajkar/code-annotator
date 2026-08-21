package commands

import (
	"bytes"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"

	annotationstore "atulm/md-viewer/internal/annotation/store"
)

func TestParseReplyConfig(t *testing.T) {
	t.Parallel()

	valid := []string{"--root", "./docs", "--id", "ann_test", "--author", "reviewer", "--message", "More detail"}
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "valid", args: valid},
		{name: "missing root", args: valid[2:], wantErr: "--root is required"},
		{name: "missing id", args: []string{"--root", "./docs", "--author", "reviewer", "--message", "More detail"}, wantErr: "--id is required"},
		{name: "blank author", args: []string{"--root", "./docs", "--id", "ann_test", "--author", " ", "--message", "More detail"}, wantErr: "--author is required"},
		{name: "missing message", args: []string{"--root", "./docs", "--id", "ann_test", "--author", "reviewer"}, wantErr: "--message is required"},
		{name: "positional", args: append(append([]string{}, valid...), "extra"), wantErr: "does not accept positional"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseReplyConfig(test.args, io.Discard)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("parseReplyConfig() error = %v, want containing %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseReplyConfig() error = %v", err)
			}
		})
	}
}

func TestRunAnnotationReply(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		identifier   string
		missingStore bool
		wantErr      string
	}{
		{name: "append reply", identifier: "ann_readme"},
		{name: "annotation missing", identifier: "ann_missing", wantErr: "not found"},
		{name: "store missing", identifier: "ann_readme", missingStore: true, wantErr: "does not exist"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			rootPath := t.TempDir()
			writeCommandFile(t, filepath.Join(rootPath, "README.md"), "Before selected after")
			writeCommandFile(t, filepath.Join(rootPath, "guide.md"), "Guide text")
			annotationsDir := filepath.Join(t.TempDir(), "annotations")
			if !test.missingStore {
				seedCommandAnnotations(t, annotationsDir)
			}

			args := []string{"reply", "--root", rootPath, "--annotations-dir", annotationsDir, "--id", test.identifier, "--author", "agent", "--message", "Additional context"}
			var output bytes.Buffer
			err := RunAnnotations(args, &output, io.Discard)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("RunAnnotations() error = %v, want containing %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("RunAnnotations() error = %v", err)
			}

			var result mutationOutput
			if err := json.Unmarshal(output.Bytes(), &result); err != nil {
				t.Fatalf("json.Unmarshal() error = %v; output: %s", err, output.String())
			}
			if result.Document != "README.md" || result.Annotation.ID != test.identifier || result.Revision == "" {
				t.Fatalf("mutation output = %#v", result)
			}
			thread := result.Annotation.Thread
			if len(thread) != 2 || thread[1].Message != "Additional context" || thread[1].Author != "agent" || !strings.HasPrefix(thread[1].ID, "msg_") {
				t.Fatalf("thread = %#v", thread)
			}

			store, err := annotationstore.Open(annotationsDir)
			if err != nil {
				t.Fatalf("annotationstore.Open() error = %v", err)
			}
			stored, revision, err := store.Load("README.md")
			if err != nil {
				t.Fatalf("Store.Load() error = %v", err)
			}
			if len(stored.Annotations[0].Thread) != 2 || string(revision) != result.Revision {
				t.Fatalf("stored sidecar = %#v, revision %q", stored, revision)
			}
		})
	}
}
