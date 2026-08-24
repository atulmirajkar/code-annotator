package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"atulm/code-annotator/internal/annotation"
	annotationstore "atulm/code-annotator/internal/annotation/store"
	"atulm/code-annotator/internal/content"
	mdrender "atulm/code-annotator/internal/render"
)

func TestViewerState(t *testing.T) {
	t.Parallel()

	rootPath := t.TempDir()
	const source = "Review this document"
	writeTestFile(t, filepath.Join(rootPath, "README.md"), source)
	root, err := content.Open(rootPath)
	if err != nil {
		t.Fatalf("content.Open() error = %v", err)
	}
	store, err := annotationstore.Open(filepath.Join(t.TempDir(), "annotations"))
	if err != nil {
		t.Fatalf("annotationstore.Open() error = %v", err)
	}
	revision := saveTransitionAnnotation(t, store, annotation.StatusClosed)
	viewer, err := New(root, mdrender.New(), WithReviewSession(store, "http://127.0.0.1:8080", strings.Repeat("t", 32)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/ui/viewer-state?document=README.md", nil)
	response := httptest.NewRecorder()
	viewer.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q", got)
	}
	if got := response.Header().Get("ETag"); got != strconv.Quote(string(revision)) {
		t.Errorf("ETag = %q, want %q", got, strconv.Quote(string(revision)))
	}

	var state viewerStateResponse
	if err := json.NewDecoder(response.Body).Decode(&state); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if state.SchemaVersion != viewerStateSchemaVersion || state.Document.Path != "README.md" || state.Document.Kind != content.KindMarkdown || state.Document.SHA256 != annotation.DocumentSHA256([]byte(source)) {
		t.Fatalf("document state = %#v", state)
	}
	if len(state.Document.SourceNodes) == 0 || len(state.Document.Diagrams) != 0 {
		t.Fatalf("source state = %#v", state.Document)
	}
	if state.Review == nil || state.Review.Revision != string(revision) || len(state.Review.Annotations) != 1 {
		t.Fatalf("review state = %#v", state.Review)
	}
	item := state.Review.Annotations[0]
	if item.ID != "ann_transition_test" || item.ElementID != "annotation-ann_transition_test" || item.LifecycleFormID != "annotation-lifecycle-ann_transition_test" || !item.DocumentLevel || item.Anchor != nil || item.SourceStartByte != nil || len(item.Transitions) != 1 || item.Transitions[0].Status != annotation.StatusOpen {
		t.Fatalf("annotation state = %#v", item)
	}
}

func TestViewerStateWithoutReview(t *testing.T) {
	t.Parallel()

	viewer := newTestHandler(t, map[string]string{"README.md": "# Home"})
	response := getResponse(t, viewer, "/ui/viewer-state?document=README.md")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", response.Code, response.Body.String())
	}
	var state viewerStateResponse
	if err := json.NewDecoder(response.Body).Decode(&state); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if state.Review != nil || response.Header().Get("ETag") != "" {
		t.Fatalf("read-only state = %#v, ETag = %q", state.Review, response.Header().Get("ETag"))
	}

	missingRequest := httptest.NewRequest(http.MethodGet, "/ui/viewer-state?document=missing.md", nil)
	missing := httptest.NewRecorder()
	viewer.ServeHTTP(missing, missingRequest)
	if missing.Code != http.StatusNotFound || !strings.Contains(missing.Body.String(), "document not found") {
		t.Fatalf("missing status = %d; body: %s", missing.Code, missing.Body.String())
	}
}
