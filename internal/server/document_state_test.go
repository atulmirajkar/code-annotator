package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"atulm/code-annotator/internal/annotation"
	annotationstore "atulm/code-annotator/internal/annotation/store"
	"atulm/code-annotator/internal/content"
	mdrender "atulm/code-annotator/internal/render"
)

func TestDocumentState(t *testing.T) {
	t.Parallel()

	rootPath := t.TempDir()
	writeTestFile(t, filepath.Join(rootPath, "README.md"), "# Home")
	writeTestFile(t, filepath.Join(rootPath, "src", "main.go"), "package main\n")
	root, err := content.Open(rootPath)
	if err != nil {
		t.Fatalf("content.Open() error = %v", err)
	}
	options, err := content.NewIndexOptions([]string{".go"}, nil)
	if err != nil {
		t.Fatalf("content.NewIndexOptions() error = %v", err)
	}
	store, err := annotationstore.Open(filepath.Join(t.TempDir(), "annotations"))
	if err != nil {
		t.Fatalf("annotationstore.Open() error = %v", err)
	}
	saveTransitionAnnotation(t, store, annotation.StatusOpen)
	viewer, err := New(root, mdrender.New(), WithIndexOptions(options), WithReviewSession(store, "http://127.0.0.1:8080", strings.Repeat("t", 32)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	response := getResponse(t, viewer.Handler(), "/ui/document-state?document=src%2Fmain.go")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q", got)
	}
	var state documentCatalogState
	if err := json.NewDecoder(response.Body).Decode(&state); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if state.SchemaVersion != 1 || state.SelectedPath == nil || *state.SelectedPath != "src/main.go" || state.Mode != "file" || !state.ReviewAvailable || state.ChangedAvailable || state.ChangedError {
		t.Fatalf("document state = %#v", state)
	}
	if len(state.Documents) != 2 {
		t.Fatalf("documents = %#v", state.Documents)
	}
	if state.Documents[0].Path != "README.md" || state.Documents[0].OpenCommentCount != 1 || state.Documents[0].Selected {
		t.Errorf("README state = %#v", state.Documents[0])
	}
	if state.Documents[1].Path != "src/main.go" || !state.Documents[1].Selected || state.Documents[1].URL != "/view/src/main.go" || state.Documents[1].Kind != content.KindCode {
		t.Errorf("source state = %#v", state.Documents[1])
	}
}

func TestDocumentStateDefaultsAndValidation(t *testing.T) {
	t.Parallel()

	viewer := newTestHandler(t, map[string]string{"README.md": "# Home"})
	response := getResponse(t, viewer, "/ui/document-state")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", response.Code, response.Body.String())
	}
	var state documentCatalogState
	if err := json.NewDecoder(response.Body).Decode(&state); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if state.SelectedPath == nil || *state.SelectedPath != "README.md" || state.ReviewAvailable || len(state.Documents) != 1 {
		t.Fatalf("default state = %#v", state)
	}

	tests := []struct {
		path       string
		wantStatus int
		contains   string
	}{
		{path: "/ui/document-state?document=missing.md", wantStatus: http.StatusNotFound, contains: "document not found"},
		{path: "/ui/document-state?mode=other", wantStatus: http.StatusBadRequest, contains: "unsupported document mode"},
		{path: "/ui/document-state?mode=diff", wantStatus: http.StatusNotFound, contains: "Changes view is unavailable"},
	}
	for _, test := range tests {
		request := httptest.NewRequest(http.MethodGet, test.path, nil)
		result := httptest.NewRecorder()
		viewer.ServeHTTP(result, request)
		if result.Code != test.wantStatus || !strings.Contains(result.Body.String(), test.contains) {
			t.Errorf("%s: status = %d, body = %q", test.path, result.Code, result.Body.String())
		}
	}
}

func TestNewDocumentCatalogStateUsesTypedInputs(t *testing.T) {
	t.Parallel()

	index := content.Index{Documents: []content.Document{
		{Path: "README.md", Name: "README.md", Kind: content.KindMarkdown},
		{Path: "src/main.go", Name: "main.go", Directory: "src", Kind: content.KindCode},
	}}
	state := newDocumentCatalogState(
		index, "src/main.go", "diff", map[string]struct{}{"src/main.go": {}}, true, false, true,
		map[string]int{"README.md": 2},
	)

	if state.SelectedPath == nil || *state.SelectedPath != "src/main.go" || !state.ChangedAvailable || state.ChangedError || !state.ReviewAvailable {
		t.Fatalf("state = %#v", state)
	}
	if state.Documents[0].URL != "/view/README.md?mode=diff" || state.Documents[0].OpenCommentCount != 2 {
		t.Errorf("README state = %#v", state.Documents[0])
	}
	if !state.Documents[1].Selected || !state.Documents[1].Changed || state.Documents[1].URL != "/view/src/main.go?mode=diff" {
		t.Errorf("source state = %#v", state.Documents[1])
	}
}
