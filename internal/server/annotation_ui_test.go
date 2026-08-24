package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"atulm/code-annotator/internal/annotation"
	annotationstore "atulm/code-annotator/internal/annotation/store"
	"atulm/code-annotator/internal/content"
	mdrender "atulm/code-annotator/internal/render"
)

func TestAnnotationPanelUI(t *testing.T) {
	t.Parallel()

	const (
		origin = "http://127.0.0.1:8080"
		token  = "0123456789abcdef0123456789abcdef"
	)
	rootPath := t.TempDir()
	writeTestFile(t, filepath.Join(rootPath, "README.md"), "Review this document")
	root, err := content.Open(rootPath)
	if err != nil {
		t.Fatalf("content.Open() error = %v", err)
	}
	store, err := annotationstore.Open(filepath.Join(t.TempDir(), "annotations"))
	if err != nil {
		t.Fatalf("annotationstore.Open() error = %v", err)
	}
	saveTransitionAnnotation(t, store, annotation.StatusClosed)
	viewer, err := New(root, mdrender.New(), WithReviewSession(store, origin, token))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tests := []struct {
		name       string
		query      string
		wantStatus int
		contains   []string
	}{
		{name: "active only", query: "document=README.md", wantStatus: http.StatusOK, contains: []string{`id="annotation-panel-content"`, `data-document="README.md"`, "No active annotations."}},
		{name: "show inactive", query: "document=README.md&show_inactive=true", wantStatus: http.StatusOK, contains: []string{`data-show-inactive="true"`, `data-annotation-id="ann_transition_test"`, `data-inactive="true"`}},
		{name: "invalid filter", query: "document=README.md&show_inactive=yes", wantStatus: http.StatusBadRequest, contains: []string{"invalid show_inactive"}},
		{name: "missing document", query: "document=missing.md", wantStatus: http.StatusNotFound, contains: []string{"document not found"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/ui/review/annotations?"+test.query, nil)
			response := httptest.NewRecorder()
			viewer.Handler().ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", response.Code, test.wantStatus, response.Body.String())
			}
			for _, want := range test.contains {
				if !strings.Contains(response.Body.String(), want) {
					t.Errorf("body missing %q:\n%s", want, response.Body.String())
				}
			}
			if test.wantStatus == http.StatusOK && response.Header().Get("Content-Type") != "text/html; charset=utf-8" {
				t.Errorf("Content-Type = %q", response.Header().Get("Content-Type"))
			}
			if test.wantStatus == http.StatusOK && response.Header().Get("ETag") == "" {
				t.Error("successful panel response is missing ETag")
			}
		})
	}
}

func TestCreateAnnotationUI(t *testing.T) {
	t.Parallel()

	const (
		origin = "http://127.0.0.1:8080"
		token  = "0123456789abcdef0123456789abcdef"
	)
	newViewer := func(t *testing.T) (*Server, *annotationstore.Store) {
		t.Helper()
		rootPath := t.TempDir()
		writeTestFile(t, filepath.Join(rootPath, "README.md"), "Before selected after")
		root, err := content.Open(rootPath)
		if err != nil {
			t.Fatalf("content.Open() error = %v", err)
		}
		store, err := annotationstore.Open(filepath.Join(t.TempDir(), "annotations"))
		if err != nil {
			t.Fatalf("annotationstore.Open() error = %v", err)
		}
		viewer, err := New(root, mdrender.New(), WithReviewSession(store, origin, token))
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		return viewer, store
	}

	t.Run("creates and returns authoritative HTML panel", func(t *testing.T) {
		t.Parallel()
		viewer, store := newViewer(t)
		form := url.Values{
			"document": {"README.md"}, "intent": {"question"},
			"comment": {`Why <this>?`}, "role": {"reviewer"},
			"selection_start_byte": {"7"}, "selection_end_byte": {"15"},
			"document_sha256": {annotation.DocumentSHA256([]byte("Before selected after"))},
		}
		request := httptest.NewRequest(http.MethodPost, "/ui/review/annotations", strings.NewReader(form.Encode()))
		request.Header.Set("Origin", origin)
		request.Header.Set(reviewTokenHeader, token)
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("If-Match", `""`)
		response := httptest.NewRecorder()
		viewer.Handler().ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body: %s", response.Code, response.Body.String())
		}
		body := response.Body.String()
		for _, want := range []string{`id="annotation-panel-content"`, `Why &lt;this&gt;?`, `<q>selected</q>`, `As reviewer`} {
			if !strings.Contains(body, want) {
				t.Errorf("body missing %q:\n%s", want, body)
			}
		}
		stored, revision, err := store.Load("README.md")
		if err != nil {
			t.Fatalf("Store.Load() error = %v", err)
		}
		if len(stored.Annotations) != 1 || stored.Annotations[0].Role != annotation.RoleReviewer || response.Header().Get("ETag") != strconv.Quote(string(revision)) {
			t.Fatalf("stored = %#v, revision = %q, ETag = %q", stored, revision, response.Header().Get("ETag"))
		}
	})

	t.Run("returns current revision on conflict", func(t *testing.T) {
		t.Parallel()
		viewer, store := newViewer(t)
		currentRevision, err := store.Save(annotation.Sidecar{SchemaVersion: annotation.SchemaVersion, Document: "README.md", Annotations: []annotation.Annotation{}}, "")
		if err != nil {
			t.Fatalf("Store.Save() error = %v", err)
		}
		form := url.Values{"document": {"README.md"}, "intent": {"question"}, "comment": {"Why?"}, "role": {"reviewer"}}
		request := httptest.NewRequest(http.MethodPost, "/ui/review/annotations", strings.NewReader(form.Encode()))
		request.Header.Set("Origin", origin)
		request.Header.Set(reviewTokenHeader, token)
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("If-Match", `""`)
		response := httptest.NewRecorder()
		viewer.Handler().ServeHTTP(response, request)
		if response.Code != http.StatusConflict || response.Header().Get("ETag") != strconv.Quote(string(currentRevision)) {
			t.Fatalf("status = %d, ETag = %q; want 409 and %q", response.Code, response.Header().Get("ETag"), strconv.Quote(string(currentRevision)))
		}
	})

	tests := []struct {
		name        string
		contentType string
		origin      string
		token       string
		ifMatch     string
		body        string
		wantStatus  int
	}{
		{name: "requires form media type", contentType: "application/json", origin: origin, token: token, ifMatch: `""`, body: `{}`, wantStatus: http.StatusUnsupportedMediaType},
		{name: "requires origin", contentType: "application/x-www-form-urlencoded", token: token, ifMatch: `""`, wantStatus: http.StatusForbidden},
		{name: "requires token", contentType: "application/x-www-form-urlencoded", origin: origin, ifMatch: `""`, wantStatus: http.StatusForbidden},
		{name: "requires revision", contentType: "application/x-www-form-urlencoded", origin: origin, token: token, wantStatus: http.StatusPreconditionRequired},
		{name: "validates role", contentType: "application/x-www-form-urlencoded", origin: origin, token: token, ifMatch: `""`, body: "document=README.md&intent=question&comment=why&role=owner", wantStatus: http.StatusUnprocessableEntity},
		{name: "rejects partial selection", contentType: "application/x-www-form-urlencoded", origin: origin, token: token, ifMatch: `""`, body: "document=README.md&intent=question&comment=why&role=reviewer&selection_start_byte=1", wantStatus: http.StatusUnprocessableEntity},
		{name: "limits body", contentType: "application/x-www-form-urlencoded", origin: origin, token: token, ifMatch: `""`, body: "comment=" + strings.Repeat("x", int(maxAnnotationMutationBytes)+1), wantStatus: http.StatusRequestEntityTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			viewer, _ := newViewer(t)
			request := httptest.NewRequest(http.MethodPost, "/ui/review/annotations", strings.NewReader(test.body))
			if test.contentType != "" {
				request.Header.Set("Content-Type", test.contentType)
			}
			if test.origin != "" {
				request.Header.Set("Origin", test.origin)
			}
			if test.token != "" {
				request.Header.Set(reviewTokenHeader, test.token)
			}
			if test.ifMatch != "" {
				request.Header.Set("If-Match", test.ifMatch)
			}
			response := httptest.NewRecorder()
			viewer.Handler().ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", response.Code, test.wantStatus, response.Body.String())
			}
		})
	}
}

func TestAnnotationUIRoutesRequireReviewSession(t *testing.T) {
	t.Parallel()
	rootPath := t.TempDir()
	writeTestFile(t, filepath.Join(rootPath, "README.md"), "# Home")
	root, err := content.Open(rootPath)
	if err != nil {
		t.Fatalf("content.Open() error = %v", err)
	}
	store, err := annotationstore.Open(filepath.Join(t.TempDir(), "annotations"))
	if err != nil {
		t.Fatalf("annotationstore.Open() error = %v", err)
	}
	viewer, err := New(root, mdrender.New(), WithAnnotationStore(store))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		request := httptest.NewRequest(method, "/ui/review/annotations?document=README.md", nil)
		response := httptest.NewRecorder()
		viewer.Handler().ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Errorf("%s status = %d, want 404", method, response.Code)
		}
	}
}
