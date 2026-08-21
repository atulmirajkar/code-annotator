package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"atulm/md-viewer/internal/annotation"
	annotationstore "atulm/md-viewer/internal/annotation/store"
	"atulm/md-viewer/internal/content"
	mdrender "atulm/md-viewer/internal/render"
)

func TestIndexRendersReadmeAndNavigation(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, map[string]string{
		"README.md":      "# Home",
		"guide/intro.md": "# Introduction",
	})
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if got, want := response.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body: %s", got, want, response.Body.String())
	}
	for _, want := range []string{
		"<h1>Home</h1>",
		`href="/view/README.md"`,
		`href="/view/guide/intro.md"`,
		`aria-current="page"`,
	} {
		if !strings.Contains(response.Body.String(), want) {
			t.Errorf("response missing %q", want)
		}
	}
}

func TestDocumentRoute(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, map[string]string{
		"guide/my notes.md": "# Notes",
	})
	request := httptest.NewRequest(http.MethodGet, "/view/guide/my%20notes.md", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if got, want := response.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body: %s", got, want, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "<h1>Notes</h1>") {
		t.Fatalf("response does not contain rendered document: %s", response.Body.String())
	}
}

func TestDocumentRouteDecodesPercentOnce(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, map[string]string{
		"100%.md":  "[asset](100%25.txt)",
		"100%.txt": "complete",
	})
	request := httptest.NewRequest(http.MethodGet, "/view/100%25.md", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if got, want := response.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body: %s", got, want, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `href="/asset/100%25.txt"`) {
		t.Fatalf("response has incorrectly encoded link: %s", response.Body.String())
	}
}

func TestIndexEmptyState(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, nil)
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if got, want := response.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if !strings.Contains(response.Body.String(), "No Markdown files found") {
		t.Fatalf("response does not contain empty state: %s", response.Body.String())
	}
}

func TestDocumentErrors(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, map[string]string{"README.md": "home"})
	tests := []struct {
		name   string
		method string
		path   string
		status int
	}{
		{name: "missing", method: http.MethodGet, path: "/view/missing.md", status: http.StatusNotFound},
		{name: "non markdown", method: http.MethodGet, path: "/view/image.png", status: http.StatusNotFound},
		{name: "method", method: http.MethodPost, path: "/view/README.md", status: http.StatusMethodNotAllowed},
		{name: "unknown", method: http.MethodGet, path: "/unknown", status: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(tt.method, tt.path, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if got := response.Code; got != tt.status {
				t.Fatalf("status = %d, want %d; body: %s", got, tt.status, response.Body.String())
			}
		})
	}
}

func TestHealth(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, nil)
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if got, want := response.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if got, want := response.Body.String(), "ok\n"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func TestAnnotationAPI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		review          bool
		method          string
		document        string
		storedSource    string
		currentSource   string
		wantStatus      int
		wantAnnotations int
		wantAnchor      annotation.AnchorState
		wantReason      annotation.StaleReason
		wantRevision    bool
	}{
		{name: "route absent outside review mode", method: http.MethodGet, document: "README.md", currentSource: "# Home", wantStatus: http.StatusNotFound},
		{name: "missing document query", review: true, method: http.MethodGet, currentSource: "# Home", wantStatus: http.StatusNotFound},
		{name: "unknown document", review: true, method: http.MethodGet, document: "missing.md", currentSource: "# Home", wantStatus: http.StatusNotFound},
		{name: "empty sidecar", review: true, method: http.MethodGet, document: "README.md", currentSource: "# Home", wantStatus: http.StatusOK},
		{name: "exact anchor", review: true, method: http.MethodGet, document: "README.md", storedSource: "Before selected after", currentSource: "Before selected after", wantStatus: http.StatusOK, wantAnnotations: 1, wantAnchor: annotation.AnchorExact, wantRevision: true},
		{name: "stale anchor", review: true, method: http.MethodGet, document: "README.md", storedSource: "Before selected after", currentSource: "Selection was removed", wantStatus: http.StatusOK, wantAnnotations: 1, wantAnchor: annotation.AnchorStale, wantReason: annotation.StaleNotFound, wantRevision: true},
		{name: "write route unavailable", review: true, method: http.MethodPost, document: "README.md", currentSource: "# Home", wantStatus: http.StatusMethodNotAllowed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			rootPath := t.TempDir()
			writeTestFile(t, filepath.Join(rootPath, "README.md"), test.currentSource)
			root, err := content.Open(rootPath)
			if err != nil {
				t.Fatalf("content.Open() error = %v", err)
			}

			var options []Option
			if test.review {
				store, err := annotationstore.Open(filepath.Join(t.TempDir(), "annotations"))
				if err != nil {
					t.Fatalf("annotationstore.Open() error = %v", err)
				}
				if test.storedSource != "" {
					saveTestAnnotation(t, store, test.document, test.storedSource)
				}
				options = append(options, WithAnnotationStore(store))
			}
			viewer, err := New(root, mdrender.New(), options...)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}

			requestPath := "/api/annotations"
			if test.document != "" {
				requestPath += "?document=" + url.QueryEscape(test.document)
			}
			request := httptest.NewRequest(test.method, requestPath, nil)
			response := httptest.NewRecorder()
			viewer.Handler().ServeHTTP(response, request)
			if got := response.Code; got != test.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", got, test.wantStatus, response.Body.String())
			}
			if test.wantStatus != http.StatusOK {
				return
			}

			var payload annotationListResponse
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatalf("json.Unmarshal() error = %v; body: %s", err, response.Body.String())
			}
			if payload.Document != test.document || len(payload.Annotations) != test.wantAnnotations {
				t.Fatalf("payload = %#v, want document %q and %d annotations", payload, test.document, test.wantAnnotations)
			}
			if test.wantRevision != (payload.Revision != "") {
				t.Fatalf("revision = %q, want non-empty %t", payload.Revision, test.wantRevision)
			}
			if got, want := response.Header().Get("ETag"), strconv.Quote(payload.Revision); got != want {
				t.Fatalf("ETag = %q, want %q", got, want)
			}
			if test.wantAnnotations > 0 {
				anchor := payload.Annotations[0].Anchor
				if anchor == nil || anchor.State != test.wantAnchor || anchor.Reason != test.wantReason {
					t.Fatalf("anchor = %#v, want state %q and reason %q", anchor, test.wantAnchor, test.wantReason)
				}
			}
		})
	}
}

// saveTestAnnotation persists one selected-text annotation for API test setup.
func saveTestAnnotation(t *testing.T, store *annotationstore.Store, document, sourceText string) {
	t.Helper()
	start := strings.Index(sourceText, "selected")
	if start < 0 {
		t.Fatal("test source does not contain selected text")
	}
	source, err := annotation.NewSource([]byte(sourceText), start, start+len("selected"))
	if err != nil {
		t.Fatalf("annotation.NewSource() error = %v", err)
	}
	now := time.Date(2026, 8, 20, 20, 0, 0, 0, time.UTC)
	sidecar := annotation.Sidecar{
		SchemaVersion: annotation.SchemaVersion,
		Document:      document,
		Annotations: []annotation.Annotation{
			{
				ID:        "ann_api_test",
				Intent:    annotation.IntentChangeRequest,
				Status:    annotation.StatusOpen,
				Comment:   "Update this selection.",
				Author:    "reviewer",
				CreatedAt: now,
				UpdatedAt: now,
				Source:    &source,
				Thread:    []annotation.ThreadEntry{},
			},
		},
	}
	if _, err := store.Save(sidecar, ""); err != nil {
		t.Fatalf("Store.Save() error = %v", err)
	}
}

func TestAssetRoute(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, map[string]string{
		"README.md":        "![pixel](images/pixel.svg)",
		"images/pixel.svg": `<svg xmlns="http://www.w3.org/2000/svg"></svg>`,
	})
	request := httptest.NewRequest(http.MethodGet, "/asset/images/pixel.svg", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if got, want := response.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body: %s", got, want, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); !strings.HasPrefix(got, "image/svg+xml") {
		t.Fatalf("Content-Type = %q, want image/svg+xml", got)
	}
	if !strings.Contains(response.Body.String(), "<svg") {
		t.Fatalf("asset body = %q", response.Body.String())
	}
}

func TestAssetRouteDecodesPercentOnce(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, map[string]string{"images/100%.txt": "complete"})
	request := httptest.NewRequest(http.MethodGet, "/asset/images/100%25.txt", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if got, want := response.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body: %s", got, want, response.Body.String())
	}
	if got, want := response.Body.String(), "complete"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func TestAssetErrors(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, map[string]string{"README.md": "home"})
	for _, requestPath := range []string{
		"/asset/missing.png",
		"/asset/%2e%2e%2fsecret.txt",
	} {
		request := httptest.NewRequest(http.MethodGet, requestPath, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if got, want := response.Code, http.StatusNotFound; got != want {
			t.Errorf("GET %q status = %d, want %d; body: %s", requestPath, got, want, response.Body.String())
		}
	}
}

func TestAssetRejectsEscapingSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks may require elevated privileges on Windows")
	}
	t.Parallel()

	rootPath := t.TempDir()
	outsidePath := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outsidePath, []byte("secret"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.Symlink(outsidePath, filepath.Join(rootPath, "escape.txt")); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}
	handler := handlerForRoot(t, rootPath)

	request := httptest.NewRequest(http.MethodGet, "/asset/escape.txt", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if got, want := response.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body: %s", got, want, response.Body.String())
	}
}

func TestSecurityHeaders(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, nil)
	for _, requestPath := range []string{"/", "/healthz", "/missing"} {
		request := httptest.NewRequest(http.MethodGet, requestPath, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)

		for name, want := range map[string]string{
			"Cache-Control":          "no-store",
			"Referrer-Policy":        "no-referrer",
			"X-Content-Type-Options": "nosniff",
			"X-Frame-Options":        "DENY",
		} {
			if got := response.Header().Get(name); got != want {
				t.Errorf("GET %q %s = %q, want %q", requestPath, name, got, want)
			}
		}
		if got := response.Header().Get("Content-Security-Policy"); !strings.Contains(got, "default-src 'none'") {
			t.Errorf("GET %q Content-Security-Policy = %q", requestPath, got)
		}
	}
}

func TestHTTPServerConfiguration(t *testing.T) {
	t.Parallel()

	rootPath := t.TempDir()
	root, err := content.Open(rootPath)
	if err != nil {
		t.Fatalf("content.Open() error = %v", err)
	}
	viewer, err := New(root, mdrender.New())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	httpServer := viewer.HTTPServer("127.0.0.1:0")

	if got, want := httpServer.Addr, "127.0.0.1:0"; got != want {
		t.Errorf("Addr = %q, want %q", got, want)
	}
	for name, values := range map[string][2]time.Duration{
		"ReadHeaderTimeout": {httpServer.ReadHeaderTimeout, readHeaderTimeout},
		"ReadTimeout":       {httpServer.ReadTimeout, readTimeout},
		"WriteTimeout":      {httpServer.WriteTimeout, writeTimeout},
		"IdleTimeout":       {httpServer.IdleTimeout, idleTimeout},
	} {
		if got, want := values[0], values[1]; got != want {
			t.Errorf("%s = %s, want %s", name, got, want)
		}
	}
}

func TestViewerRefreshReflectsDiskChanges(t *testing.T) {
	t.Parallel()

	rootPath := t.TempDir()
	readmePath := filepath.Join(rootPath, "README.md")
	assetPath := filepath.Join(rootPath, "images", "status.txt")
	writeTestFile(t, readmePath, "# First version\n\n[Guide](guide/intro.md)\n")
	writeTestFile(t, assetPath, "first asset")
	handler := handlerForRoot(t, rootPath)

	firstPage := getResponse(t, handler, "/")
	if !strings.Contains(firstPage.Body.String(), "<h1>First version</h1>") {
		t.Fatalf("first page does not contain original Markdown: %s", firstPage.Body.String())
	}

	writeTestFile(t, readmePath, "# Saved update\n\n[Guide](guide/intro.md)\n")
	writeTestFile(t, filepath.Join(rootPath, "guide", "intro.md"), "# New guide")
	writeTestFile(t, assetPath, "updated asset")

	refreshedPage := getResponse(t, handler, "/")
	for _, want := range []string{
		"<h1>Saved update</h1>",
		`href="/view/guide/intro.md"`,
	} {
		if !strings.Contains(refreshedPage.Body.String(), want) {
			t.Errorf("refreshed page missing %q: %s", want, refreshedPage.Body.String())
		}
	}
	if strings.Contains(refreshedPage.Body.String(), "First version") {
		t.Errorf("refreshed page contains stale Markdown: %s", refreshedPage.Body.String())
	}

	guidePage := getResponse(t, handler, "/view/guide/intro.md")
	if !strings.Contains(guidePage.Body.String(), "<h1>New guide</h1>") {
		t.Fatalf("nested document was not discovered: %s", guidePage.Body.String())
	}
	asset := getResponse(t, handler, "/asset/images/status.txt")
	if got, want := asset.Body.String(), "updated asset"; got != want {
		t.Fatalf("refreshed asset = %q, want %q", got, want)
	}
}

func newTestHandler(t *testing.T, files map[string]string) http.Handler {
	t.Helper()
	rootPath := t.TempDir()
	for relative, body := range files {
		filePath := filepath.Join(rootPath, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}
		if err := os.WriteFile(filePath, []byte(body), 0o644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
	}

	return handlerForRoot(t, rootPath)
}

func writeTestFile(t *testing.T, filePath, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filePath, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func getResponse(t *testing.T, handler http.Handler, requestPath string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, requestPath, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if got, want := response.Code, http.StatusOK; got != want {
		t.Fatalf("GET %q status = %d, want %d; body: %s", requestPath, got, want, response.Body.String())
	}
	return response
}

func handlerForRoot(t *testing.T, rootPath string) http.Handler {
	t.Helper()
	root, err := content.Open(rootPath)
	if err != nil {
		t.Fatalf("content.Open() error = %v", err)
	}
	server, err := New(root, mdrender.New())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return server.Handler()
}
