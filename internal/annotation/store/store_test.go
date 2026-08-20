package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"atulm/md-viewer/internal/annotation"
)

func TestStore(t *testing.T) {
	tests := []struct {
		name string
		run  func(*testing.T)
	}{
		{name: "load missing sidecar", run: testLoadMissingReturnsEmptySidecar},
		{name: "save and load", run: testSaveAndLoad},
		{name: "reject stale revision", run: testSaveRejectsStaleRevision},
		{name: "preserve unknown fields", run: testSavePreservesUnknownFields},
		{name: "reject unsafe paths", run: testStoreRejectsUnsafePaths},
		{name: "reject escaping symlink", run: testStoreRejectsEscapingSymlink},
		{name: "serialize concurrent saves", run: testConcurrentSaveAllowsOneRevisionWinner},
		{name: "reject malformed sidecar", run: testLoadRejectsMalformedSidecar},
	}

	for _, test := range tests {
		t.Run(test.name, test.run)
	}
}

func testLoadMissingReturnsEmptySidecar(t *testing.T) {
	t.Parallel()

	storage := openTestStore(t)
	sidecar, revision, err := storage.Load("designs/missing.md")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if revision != "" || sidecar.SchemaVersion != annotation.SchemaVersion || sidecar.Document != "designs/missing.md" || len(sidecar.Annotations) != 0 {
		t.Fatalf("Load() = %#v, revision %q", sidecar, revision)
	}
}

func testSaveAndLoad(t *testing.T) {
	t.Parallel()

	storage := openTestStore(t)
	want := testSidecar("designs/architecture.md")
	revision, err := storage.Save(want, "")
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if len(revision) != 64 {
		t.Fatalf("revision = %q, want SHA-256", revision)
	}

	path := filepath.Join(storage.Root(), "designs", "architecture.md.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("sidecar mode = %o, want 600", info.Mode().Perm())
	}

	got, loadedRevision, err := storage.Load(want.Document)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loadedRevision != revision || got.Annotations[0].ID != want.Annotations[0].ID {
		t.Fatalf("Load() = %#v, revision %q; want revision %q", got, loadedRevision, revision)
	}
}

func testSaveRejectsStaleRevision(t *testing.T) {
	t.Parallel()

	storage := openTestStore(t)
	sidecar := testSidecar("README.md")
	firstRevision, err := storage.Save(sidecar, "")
	if err != nil {
		t.Fatalf("first Save() error = %v", err)
	}
	sidecar.Annotations[0].Comment = "updated"
	sidecar.Annotations[0].UpdatedAt = sidecar.Annotations[0].UpdatedAt.Add(time.Minute)
	secondRevision, err := storage.Save(sidecar, firstRevision)
	if err != nil {
		t.Fatalf("second Save() error = %v", err)
	}

	sidecar.Annotations[0].Comment = "stale writer"
	current, err := storage.Save(sidecar, firstRevision)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("Save() error = %v, want ErrConflict", err)
	}
	if current != secondRevision {
		t.Fatalf("conflict revision = %q, want %q", current, secondRevision)
	}
}

func testSavePreservesUnknownFields(t *testing.T) {
	t.Parallel()

	storage := openTestStore(t)
	sidecar := testSidecar("designs/future.md")
	revision, err := storage.Save(sidecar, "")
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	path := filepath.Join(storage.Root(), "designs", "future.md.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	raw["futureRoot"] = "keep"
	items := raw["annotations"].([]any)
	item := items[0].(map[string]any)
	item["futureAnnotation"] = map[string]any{"enabled": true}
	source := item["source"].(map[string]any)
	source["futureSource"] = float64(42)
	thread := item["thread"].([]any)[0].(map[string]any)
	thread["futureThread"] = "keep"
	thread["commit"] = "old-commit"
	data, err = json.MarshalIndent(raw, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent() error = %v", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	revision = revisionOf(data)

	loaded, loadedRevision, err := storage.Load(sidecar.Document)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loadedRevision != revision {
		t.Fatalf("Load() revision = %q, want %q", loadedRevision, revision)
	}
	loaded.Annotations[0].Comment = "known field changed"
	loaded.Annotations[0].Thread[0].Commit = ""
	loaded.Annotations[0].UpdatedAt = loaded.Annotations[0].UpdatedAt.Add(time.Minute)
	if _, err := storage.Save(loaded, loadedRevision); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	for _, want := range []string{"futureRoot", "futureAnnotation", "futureSource", "futureThread", "known field changed"} {
		if !strings.Contains(string(updated), want) {
			t.Errorf("updated sidecar missing %q:\n%s", want, updated)
		}
	}
	if strings.Contains(string(updated), "old-commit") {
		t.Errorf("updated sidecar restored a cleared known field:\n%s", updated)
	}
}

func testStoreRejectsUnsafePaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		document string
	}{
		{name: "parent traversal", document: "../secret.md"},
		{name: "absolute path", document: "/absolute.md"},
		{name: "backslash separator", document: `windows\\path.md`},
		{name: "non-Markdown extension", document: "notes.txt"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			storage := openTestStore(t)
			if _, _, err := storage.Load(test.document); err == nil {
				t.Errorf("Load(%q) error = nil, want rejection", test.document)
			}
		})
	}
}

func testStoreRejectsEscapingSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks may require elevated privileges on Windows")
	}
	t.Parallel()

	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "designs")); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}
	storage, err := Open(root)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if _, err := storage.Save(testSidecar("designs/escape.md"), ""); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("Save() error = %v, want ErrUnsafePath", err)
	}
}

func testConcurrentSaveAllowsOneRevisionWinner(t *testing.T) {
	t.Parallel()

	storage := openTestStore(t)
	sidecar := testSidecar("README.md")
	revision, err := storage.Save(sidecar, "")
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for index := 0; index < 2; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			candidate := testSidecar("README.md")
			candidate.Annotations[0].Comment = "writer " + string(rune('A'+index))
			candidate.Annotations[0].UpdatedAt = candidate.Annotations[0].UpdatedAt.Add(time.Duration(index+1) * time.Minute)
			<-start
			_, saveErr := storage.Save(candidate, revision)
			results <- saveErr
		}(index)
	}
	close(start)
	wait.Wait()
	close(results)

	var successes, conflicts int
	for result := range results {
		switch {
		case result == nil:
			successes++
		case errors.Is(result, ErrConflict):
			conflicts++
		default:
			t.Fatalf("Save() unexpected error = %v", result)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent results: %d successes, %d conflicts", successes, conflicts)
	}
}

func testLoadRejectsMalformedSidecar(t *testing.T) {
	t.Parallel()

	storage := openTestStore(t)
	path := filepath.Join(storage.Root(), "README.md.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, _, err := storage.Load("README.md"); err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("Load() error = %v, want decode error", err)
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	storage, err := Open(filepath.Join(t.TempDir(), "annotations"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	return storage
}

func testSidecar(document string) annotation.Sidecar {
	created := time.Date(2026, 8, 20, 19, 30, 0, 0, time.UTC)
	return annotation.Sidecar{
		SchemaVersion: annotation.SchemaVersion,
		Document:      document,
		Annotations: []annotation.Annotation{
			{
				ID:        "ann_test",
				Intent:    annotation.IntentChangeRequest,
				Status:    annotation.StatusApplied,
				Comment:   "Make the default explicit.",
				Author:    "atul",
				CreatedAt: created,
				UpdatedAt: created.Add(time.Minute),
				Source: &annotation.Source{
					SHA256: strings.Repeat("a", 64),
					Selector: annotation.Selector{
						Exact:     "default",
						StartByte: 4,
						EndByte:   11,
						StartLine: 1,
						EndLine:   1,
					},
				},
				Thread: []annotation.ThreadEntry{
					{
						ID:        "msg_resolution",
						Kind:      annotation.ThreadResolution,
						Summary:   "Implemented",
						Author:    "codex",
						CreatedAt: created.Add(time.Minute),
					},
				},
			},
		},
	}
}
