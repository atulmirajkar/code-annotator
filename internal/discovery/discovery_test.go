package discovery

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRegisterListCleanupRoundTrip(t *testing.T) {
	t.Setenv("CODE_ANNOTATOR_STATE_DIR", t.TempDir())

	cleanup, err := Register("/content/root", "http://127.0.0.1:12345/")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	entries, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("List returned %d entries, want 1", len(entries))
	}
	entry := entries[0]
	if entry.URL != "http://127.0.0.1:12345/" {
		t.Errorf("URL = %q, want the registered URL", entry.URL)
	}
	if entry.Root != "/content/root" {
		t.Errorf("Root = %q, want /content/root", entry.Root)
	}
	if entry.PID != os.Getpid() {
		t.Errorf("PID = %d, want %d", entry.PID, os.Getpid())
	}
	if entry.SchemaVersion != schemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", entry.SchemaVersion, schemaVersion)
	}

	cleanup()

	entries, err = List()
	if err != nil {
		t.Fatalf("List after cleanup: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("List after cleanup returned %d entries, want 0", len(entries))
	}
}

func TestListSkipsMalformedAndForeignFiles(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("CODE_ANNOTATOR_STATE_DIR", stateDir)

	serversDir, err := StateDir()
	if err != nil {
		t.Fatalf("StateDir: %v", err)
	}
	if err := os.MkdirAll(serversDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(serversDir, "not-json.json"), []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("write malformed file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(serversDir, "future-schema.json"), []byte(`{"schemaVersion":99,"url":"http://127.0.0.1:1/"}`), 0o600); err != nil {
		t.Fatalf("write future-schema file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(serversDir, "notes.txt"), []byte("ignore me"), 0o600); err != nil {
		t.Fatalf("write non-json file: %v", err)
	}

	cleanup, err := Register("/content/root", "http://127.0.0.1:54321/")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	t.Cleanup(cleanup)

	entries, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("List returned %d entries, want 1 (malformed/foreign files should be skipped)", len(entries))
	}
	if entries[0].URL != "http://127.0.0.1:54321/" {
		t.Errorf("URL = %q, want the registered URL", entries[0].URL)
	}
}

func TestListReturnsNilWhenStateDirMissing(t *testing.T) {
	t.Setenv("CODE_ANNOTATOR_STATE_DIR", filepath.Join(t.TempDir(), "does-not-exist"))

	entries, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if entries != nil {
		t.Errorf("List = %v, want nil", entries)
	}
}

func TestRemoveIsIdempotent(t *testing.T) {
	t.Setenv("CODE_ANNOTATOR_STATE_DIR", t.TempDir())

	if err := Remove(999999); err != nil {
		t.Fatalf("Remove of a nonexistent entry: %v", err)
	}

	cleanup, err := Register("/content/root", "http://127.0.0.1:1/")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	defer cleanup()

	if err := Remove(os.Getpid()); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	entries, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("List after Remove returned %d entries, want 0", len(entries))
	}
}
