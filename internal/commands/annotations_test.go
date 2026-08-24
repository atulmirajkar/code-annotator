package commands

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"atulm/code-annotator/internal/annotation"
	annotationstore "atulm/code-annotator/internal/annotation/store"
)

func TestParseListConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		args       []string
		wantRoot   string
		wantStatus annotation.Status
		wantCode   bool
		wantErr    string
	}{
		{name: "defaults", args: []string{"--root", "./docs"}, wantRoot: "./docs"},
		{name: "status filter", args: []string{"--root", "./docs", "--status", "open,needs_changes"}, wantRoot: "./docs", wantStatus: annotation.StatusNeedsChanges},
		{name: "include default code", args: []string{"--root", "./docs", "--include-code"}, wantRoot: "./docs", wantCode: true},
		{name: "custom code implies inclusion", args: []string{"--root", "./docs", "--code-extensions", ".go,.cs"}, wantRoot: "./docs", wantCode: true},
		{name: "missing root", wantErr: "--root is required"},
		{name: "invalid status", args: []string{"--root", "./docs", "--status", "unknown"}, wantErr: "invalid annotation status"},
		{name: "invalid format", args: []string{"--root", "./docs", "--format", "yaml"}, wantErr: "unsupported list format"},
		{name: "positional argument", args: []string{"--root", "./docs", "extra"}, wantErr: "does not accept positional"},
		{name: "invalid extension", args: []string{"--root", "./docs", "--code-extensions", "go"}, wantErr: "configure content catalog"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			configuration, err := parseListConfig(test.args, io.Discard)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("parseListConfig() error = %v, want containing %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseListConfig() error = %v", err)
			}
			if configuration.rootPath != test.wantRoot {
				t.Errorf("rootPath = %q, want %q", configuration.rootPath, test.wantRoot)
			}
			if test.wantStatus != "" {
				if _, ok := configuration.statuses[test.wantStatus]; !ok {
					t.Errorf("statuses = %#v, want %q", configuration.statuses, test.wantStatus)
				}
			}
			if test.wantCode && len(configuration.indexOptions.CodeExtensions) == 0 {
				t.Error("CodeExtensions is empty, want source catalog")
			}
		})
	}
}

func TestRunAnnotationsIncludesCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		command  string
		contains []string
	}{
		{name: "JSON list", command: "list", contains: []string{`"document": "main.go"`, `"kind": "code"`, `"language": "go"`}},
		{name: "Markdown export", command: "export", contains: []string{"## main.go", "- Kind: `code`", "- Language: `go`", "#### Selected source"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			rootPath := t.TempDir()
			annotationsDir := filepath.Join(t.TempDir(), "annotations")
			seedCodeCommandAnnotation(t, rootPath, annotationsDir)

			args := []string{test.command, "--root", rootPath, "--annotations-dir", annotationsDir, "--include-code"}
			var output bytes.Buffer
			if err := RunAnnotations(args, &output, io.Discard); err != nil {
				t.Fatalf("RunAnnotations() error = %v", err)
			}
			for _, want := range test.contains {
				if !strings.Contains(output.String(), want) {
					t.Errorf("output missing %q:\n%s", want, output.String())
				}
			}
		})
	}
}

// seedCodeCommandAnnotation creates one source-file sidecar for offline handoff
// and mutation tests that opt into the same code catalog.
func seedCodeCommandAnnotation(t *testing.T, rootPath, annotationsDir string) {
	t.Helper()
	const sourceText = "package main\nvar less = 1 < 2\n"
	writeCommandFile(t, filepath.Join(rootPath, "main.go"), sourceText)
	store, err := annotationstore.Open(annotationsDir)
	if err != nil {
		t.Fatalf("annotationstore.Open() error = %v", err)
	}
	now := time.Date(2026, time.August, 21, 12, 0, 0, 0, time.UTC)
	selectionStart := strings.Index(sourceText, "less")
	selected, err := annotation.NewSource([]byte(sourceText), selectionStart, selectionStart+len("less"))
	if err != nil {
		t.Fatalf("annotation.NewSource() error = %v", err)
	}
	sidecar := annotation.Sidecar{SchemaVersion: annotation.SchemaVersion, Document: "main.go", Annotations: []annotation.Annotation{{ID: "ann_code", Intent: annotation.IntentChangeRequest, Status: annotation.StatusOpen, Comment: "Check comparison", Role: "reviewer", CreatedAt: now, UpdatedAt: now, Source: &selected, Thread: []annotation.ThreadEntry{}}}}
	if _, err := store.Save(sidecar, ""); err != nil {
		t.Fatalf("Store.Save() error = %v", err)
	}
}

func TestRunAnnotations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		statuses      string
		missingStore  bool
		wantDocuments int
		wantFirst     string
		wantStatus    annotation.Status
		wantAnchor    annotation.AnchorState
	}{
		{name: "list all", wantDocuments: 2, wantFirst: "guide.md", wantStatus: annotation.StatusOpen, wantAnchor: annotation.AnchorExact},
		{name: "filter status", statuses: "needs_changes", wantDocuments: 1, wantFirst: "guide.md", wantStatus: annotation.StatusNeedsChanges},
		{name: "missing store is empty", missingStore: true},
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
			} else {
				annotationsDir = filepath.Join(t.TempDir(), "missing")
			}

			args := []string{"list", "--root", rootPath, "--annotations-dir", annotationsDir}
			if test.statuses != "" {
				args = append(args, "--status", test.statuses)
			}
			var output bytes.Buffer
			if err := RunAnnotations(args, &output, io.Discard); err != nil {
				t.Fatalf("RunAnnotations() error = %v", err)
			}
			var result listOutput
			if err := json.Unmarshal(output.Bytes(), &result); err != nil {
				t.Fatalf("json.Unmarshal() error = %v; output: %s", err, output.String())
			}
			if len(result.Documents) != test.wantDocuments {
				t.Fatalf("documents = %#v, want count %d", result.Documents, test.wantDocuments)
			}
			if test.wantDocuments == 0 {
				if _, err := os.Stat(annotationsDir); !os.IsNotExist(err) {
					t.Fatalf("missing annotation directory was created: %v", err)
				}
				return
			}
			if result.Documents[0].Document != test.wantFirst {
				t.Errorf("first document = %q, want %q", result.Documents[0].Document, test.wantFirst)
			}
			var item *listAnnotation
			for documentIndex := range result.Documents {
				for annotationIndex := range result.Documents[documentIndex].Annotations {
					candidate := &result.Documents[documentIndex].Annotations[annotationIndex]
					if candidate.Status == test.wantStatus {
						item = candidate
					}
				}
			}
			if item == nil {
				t.Fatalf("annotations = %#v, want status %q", result.Documents, test.wantStatus)
			}
			if test.wantAnchor != "" && (item.Anchor == nil || item.Anchor.State != test.wantAnchor) {
				t.Errorf("anchor = %#v, want %q", item.Anchor, test.wantAnchor)
			}
		})
	}
}

func TestParseExportConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "defaults", args: []string{"--root", "./docs"}},
		{name: "explicit markdown", args: []string{"--root", "./docs", "--format", "markdown"}},
		{name: "missing root", wantErr: "--root is required"},
		{name: "invalid format", args: []string{"--root", "./docs", "--format", "json"}, wantErr: "unsupported export format"},
		{name: "invalid status", args: []string{"--root", "./docs", "--status", "pending"}, wantErr: "invalid annotation status"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseExportConfig(test.args, io.Discard)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("parseExportConfig() error = %v, want containing %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseExportConfig() error = %v", err)
			}
		})
	}
}

func TestRunAnnotationExport(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		status     string
		contains   []string
		notContain string
	}{
		{name: "agent handoff", status: "open", contains: []string{"# Annotation review", "## README.md", "- Kind: `markdown`", "- Language: `markdown`", "### ann_readme", "- Anchor: `exact` at lines 1–1", "#### Selected source", "selected", "````text\nUpdate ``` example\n````", "`reply` by reviewer", "First line second line"}},
		{name: "no matches", status: "applied", contains: []string{"No matching annotations."}, notContain: "## README.md"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			rootPath := t.TempDir()
			writeCommandFile(t, filepath.Join(rootPath, "README.md"), "Before selected after")
			writeCommandFile(t, filepath.Join(rootPath, "guide.md"), "Guide text")
			annotationsDir := filepath.Join(t.TempDir(), "annotations")
			seedCommandAnnotations(t, annotationsDir)

			args := []string{"export", "--root", rootPath, "--annotations-dir", annotationsDir, "--status", test.status}
			var output bytes.Buffer
			if err := RunAnnotations(args, &output, io.Discard); err != nil {
				t.Fatalf("RunAnnotations() error = %v", err)
			}
			for _, wanted := range test.contains {
				if !strings.Contains(output.String(), wanted) {
					t.Errorf("export missing %q:\n%s", wanted, output.String())
				}
			}
			if test.notContain != "" && strings.Contains(output.String(), test.notContain) {
				t.Errorf("export contains %q:\n%s", test.notContain, output.String())
			}
		})
	}
}

// seedCommandAnnotations creates two valid sidecars in deliberately reversed
// save order so the list test verifies content-index ordering.
func seedCommandAnnotations(t *testing.T, directory string) {
	t.Helper()
	store, err := annotationstore.Open(directory)
	if err != nil {
		t.Fatalf("annotationstore.Open() error = %v", err)
	}
	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	source, err := annotation.NewSource([]byte("Before selected after"), 7, 15)
	if err != nil {
		t.Fatalf("annotation.NewSource() error = %v", err)
	}
	sidecars := []annotation.Sidecar{
		{SchemaVersion: annotation.SchemaVersion, Document: "guide.md", Annotations: []annotation.Annotation{{ID: "ann_guide", Intent: annotation.IntentQuestion, Status: annotation.StatusNeedsChanges, Comment: "Clarify", Role: "reviewer", CreatedAt: now, UpdatedAt: now, Thread: []annotation.ThreadEntry{}}}},
		{SchemaVersion: annotation.SchemaVersion, Document: "README.md", Annotations: []annotation.Annotation{{ID: "ann_readme", Intent: annotation.IntentChangeRequest, Status: annotation.StatusOpen, Comment: "Update ``` example", Role: "reviewer", CreatedAt: now, UpdatedAt: now, Source: &source, Thread: []annotation.ThreadEntry{{ID: "msg_reply", Kind: annotation.ThreadReply, Message: "First line\nsecond line", Role: "reviewer", CreatedAt: now}}}}},
	}
	for _, sidecar := range sidecars {
		if _, err := store.Save(sidecar, ""); err != nil {
			t.Fatalf("Store.Save(%q) error = %v", sidecar.Document, err)
		}
	}
}

// writeCommandFile creates one fixture beneath an existing temporary root.
func writeCommandFile(t *testing.T, path, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", path, err)
	}
}
