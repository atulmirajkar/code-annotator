package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
