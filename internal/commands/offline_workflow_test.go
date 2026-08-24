package commands

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"atulm/code-annotator/internal/annotation"
	annotationstore "atulm/code-annotator/internal/annotation/store"
)

// TestOfflineAnnotationWorkflow exercises the direct-store CLI contract from
// initial discovery through a reviewer-requested retry and final closure. Live
// agents use the HTTP API so browser and agent writes share one coordinator.
func TestOfflineAnnotationWorkflow(t *testing.T) {
	t.Parallel()

	rootPath := t.TempDir()
	writeCommandFile(t, filepath.Join(rootPath, "README.md"), "Before replacement after")
	annotationsDir := filepath.Join(t.TempDir(), "annotations")
	seedHandoffAnnotation(t, annotationsDir)

	common := []string{"--root", rootPath, "--annotations-dir", annotationsDir}
	steps := []struct {
		name        string
		args        []string
		contains    []string
		notContains []string
		wantErr     string
	}{
		{
			name:     "discover stale actionable annotation",
			args:     append([]string{"export"}, append(common, "--status", "open,needs_changes")...),
			contains: []string{"### ann_handoff", "- Status: `open`", "- Anchor: `stale` (`not_found`", "Update the documented behavior."},
		},
		{name: "acknowledge work", args: append([]string{"resolve"}, append(common, "--id", "ann_handoff", "--status", "acknowledged", "--role", "agent")...)},
		{name: "ask a question", args: append([]string{"reply"}, append(common, "--id", "ann_handoff", "--role", "agent", "--message", "Should the example retain compatibility wording?")...)},
		{name: "report first attempt", args: append([]string{"resolve"}, append(common, "--id", "ann_handoff", "--status", "applied", "--role", "agent", "--summary", "Updated the behavior and example.", "--commit", "abc1234")...)},
		{name: "reviewer requests changes", args: append([]string{"resolve"}, append(common, "--id", "ann_handoff", "--status", "needs_changes", "--role", "reviewer", "--message", "Retain the compatibility wording.")...)},
		{
			name: "rediscover same annotation and complete history",
			args: append([]string{"export"}, append(common, "--status", "open,needs_changes")...),
			contains: []string{
				"### ann_handoff", "- Status: `needs_changes`",
				"Should the example retain compatibility wording?",
				"Updated the behavior and example.", "Retain the compatibility wording.",
			},
		},
		{name: "acknowledge retry", args: append([]string{"resolve"}, append(common, "--id", "ann_handoff", "--status", "acknowledged", "--role", "agent")...)},
		{name: "report second attempt", args: append([]string{"resolve"}, append(common, "--id", "ann_handoff", "--status", "applied", "--role", "agent", "--summary", "Restored compatibility wording.", "--commit", "def5678")...)},
		{name: "agent cannot close", args: append([]string{"resolve"}, append(common, "--id", "ann_handoff", "--status", "closed", "--role", "agent")...), wantErr: "cannot transition"},
		{name: "reviewer closes", args: append([]string{"resolve"}, append(common, "--id", "ann_handoff", "--status", "closed", "--role", "reviewer")...)},
		{name: "actionable queue is empty", args: append([]string{"export"}, append(common, "--status", "open,needs_changes")...), contains: []string{"No matching annotations."}, notContains: []string{"ann_handoff"}},
	}

	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			var output bytes.Buffer
			err := RunAnnotations(step.args, &output, io.Discard)
			if step.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), step.wantErr) {
					t.Fatalf("RunAnnotations() error = %v, want containing %q", err, step.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("RunAnnotations() error = %v", err)
			}
			for _, wanted := range step.contains {
				if !strings.Contains(output.String(), wanted) {
					t.Errorf("output missing %q:\n%s", wanted, output.String())
				}
			}
			for _, unwanted := range step.notContains {
				if strings.Contains(output.String(), unwanted) {
					t.Errorf("output contains %q:\n%s", unwanted, output.String())
				}
			}
		})
	}

	store, err := annotationstore.Open(annotationsDir)
	if err != nil {
		t.Fatalf("annotationstore.Open() error = %v", err)
	}
	sidecar, _, err := store.Load("README.md")
	if err != nil {
		t.Fatalf("Store.Load() error = %v", err)
	}
	if len(sidecar.Annotations) != 1 {
		t.Fatalf("annotations = %#v, want one", sidecar.Annotations)
	}
	item := sidecar.Annotations[0]
	if item.ID != "ann_handoff" || item.Status != annotation.StatusClosed {
		t.Fatalf("final annotation = %#v", item)
	}
	if len(item.Thread) != 12 {
		t.Fatalf("thread entries = %d, want 12: %#v", len(item.Thread), item.Thread)
	}
	if item.Thread[2].Kind != annotation.ThreadReply || item.Thread[2].Message != "Should the example retain compatibility wording?" {
		t.Errorf("discussion entry = %#v", item.Thread[2])
	}
	if item.Thread[3].Kind != annotation.ThreadResolution || item.Thread[3].Commit != "abc1234" {
		t.Errorf("first resolution = %#v", item.Thread[3])
	}
	if item.Thread[9].Kind != annotation.ThreadResolution || item.Thread[9].Commit != "def5678" {
		t.Errorf("second resolution = %#v", item.Thread[9])
	}
}

// seedHandoffAnnotation stores a selector whose exact text is absent from the
// current fixture, allowing the discovery step to verify stale-anchor export.
func seedHandoffAnnotation(t *testing.T, directory string) {
	t.Helper()
	store, err := annotationstore.Open(directory)
	if err != nil {
		t.Fatalf("annotationstore.Open() error = %v", err)
	}
	original := []byte("Before selected after")
	source, err := annotation.NewSource(original, len("Before "), len("Before selected"))
	if err != nil {
		t.Fatalf("annotation.NewSource() error = %v", err)
	}
	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	sidecar := annotation.Sidecar{
		SchemaVersion: annotation.SchemaVersion,
		Document:      "README.md",
		Annotations: []annotation.Annotation{{
			ID:        "ann_handoff",
			Intent:    annotation.IntentChangeRequest,
			Status:    annotation.StatusOpen,
			Comment:   "Update the documented behavior.",
			Role:      "reviewer",
			CreatedAt: now,
			UpdatedAt: now,
			Source:    &source,
			Thread:    []annotation.ThreadEntry{},
		}},
	}
	if _, err := store.Save(sidecar, ""); err != nil {
		t.Fatalf("Store.Save() error = %v", err)
	}
}
