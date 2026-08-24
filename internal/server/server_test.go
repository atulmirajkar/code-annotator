package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"atulm/code-annotator/internal/annotation"
	annotationstore "atulm/code-annotator/internal/annotation/store"
	"atulm/code-annotator/internal/content"
	"atulm/code-annotator/internal/gitdiff"
	mdrender "atulm/code-annotator/internal/render"
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

func TestCodeDocumentRoute(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		path       string
		body       []byte
		wantStatus int
		contains   []string
	}{
		{name: "escaped Go source", path: "main.go", body: []byte("package main\nvar less = 1 < 2\n"), wantStatus: http.StatusOK, contains: []string{`class="source-view"`, `var less = 1 &lt; 2`, `class="document-kind">code`}},
		{name: "invalid UTF-8", path: "bad.go", body: []byte{0xff}, wantStatus: http.StatusUnsupportedMediaType},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			rootPath := t.TempDir()
			if err := os.WriteFile(filepath.Join(rootPath, test.path), test.body, 0o644); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
			root, err := content.Open(rootPath)
			if err != nil {
				t.Fatalf("content.Open() error = %v", err)
			}
			options, err := content.NewIndexOptions([]string{".go"}, nil)
			if err != nil {
				t.Fatalf("content.NewIndexOptions() error = %v", err)
			}
			viewer, err := New(root, mdrender.New(), WithIndexOptions(options))
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			response := httptest.NewRecorder()
			viewer.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/view/"+test.path, nil))
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", response.Code, test.wantStatus, response.Body.String())
			}
			for _, want := range test.contains {
				if !strings.Contains(response.Body.String(), want) {
					t.Errorf("response missing %q", want)
				}
			}
		})
	}
}

func TestCodeAnnotationCatalog(t *testing.T) {
	t.Parallel()

	const (
		origin = "http://127.0.0.1:8080"
		token  = "0123456789abcdef0123456789abcdef"
	)
	rootPath := t.TempDir()
	const sourceText = "package main\nvar less = 1 < 2\nvar selected = less\n"
	writeTestFile(t, filepath.Join(rootPath, "main.go"), sourceText)
	writeTestFile(t, filepath.Join(rootPath, "notes.txt"), "not cataloged")
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
	saveTestAnnotation(t, store, "main.go", sourceText)
	viewer, err := New(root, mdrender.New(), WithIndexOptions(options), WithReviewSession(store, origin, token))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tests := []struct {
		name       string
		path       string
		wantStatus int
		contains   []string
	}{
		{name: "source page has compact review layout", path: "/view/main.go", wantStatus: http.StatusOK, contains: []string{`class="document code-document"`, `class="source-text" data-source-start="0" data-source-end="12"`, `name="code-annotator-review-token"`, `id="annotation-sidebar"`}},
		{name: "cataloged source has annotation endpoint", path: "/api/annotations?document=main.go", wantStatus: http.StatusOK, contains: []string{`"document":"main.go"`, `"kind":"code"`, `"language":"go"`, `"annotations":[{`}},
		{name: "agent queue includes source metadata", path: "/api/annotations?status=open", wantStatus: http.StatusOK, contains: []string{`"document":"main.go"`, `"kind":"code"`, `"language":"go"`}},
		{name: "uncataloged asset has no annotation endpoint", path: "/api/annotations?document=notes.txt", wantStatus: http.StatusNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			response := httptest.NewRecorder()
			viewer.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", response.Code, test.wantStatus, response.Body.String())
			}
			for _, want := range test.contains {
				if !strings.Contains(response.Body.String(), want) {
					t.Errorf("response missing %q", want)
				}
			}
		})
	}
}

func TestGitComparisonMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		comparison gitdiff.Config
		wantErr    string
		contains   []string
	}{
		{
			name: "frozen base metadata",
			comparison: gitdiff.Config{
				RepositoryRoot: "/repository",
				RequestedBase:  "origin/main",
				BaseCommit:     strings.Repeat("a", 40),
			},
			contains: []string{`name="code-annotator-diff-base" content="origin/main"`, `name="code-annotator-diff-commit" content="` + strings.Repeat("a", 40) + `"`},
		},
		{name: "incomplete configuration", comparison: gitdiff.Config{RequestedBase: "HEAD"}, wantErr: "incomplete configuration"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			rootPath := t.TempDir()
			writeTestFile(t, filepath.Join(rootPath, "README.md"), "# Home")
			root, err := content.Open(rootPath)
			if err != nil {
				t.Fatalf("content.Open() error = %v", err)
			}
			viewer, err := New(root, mdrender.New(), WithGitComparison(test.comparison, "", ""))
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("New() error = %v, want containing %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			response := getResponse(t, viewer.Handler(), "/")
			for _, want := range test.contains {
				if !strings.Contains(response.Body.String(), want) {
					t.Errorf("response missing %q", want)
				}
			}
		})
	}
}

func TestAbbreviatedCommit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		commit string
		want   string
	}{
		{name: "full object ID", commit: strings.Repeat("a", 40), want: strings.Repeat("a", 12)},
		{name: "already short", commit: "abc123", want: "abc123"},
		{name: "empty", want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := abbreviatedCommit(test.commit); got != test.want {
				t.Fatalf("abbreviatedCommit(%q) = %q, want %q", test.commit, got, test.want)
			}
		})
	}
}

func TestCodeDiffRoute(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		path        string
		configure   func(*testing.T, string) *gitdiff.Config
		requiresGit bool
		wantStatus  int
		contains    []string
		excludes    []string
	}{
		{
			name: "side-by-side changes",
			path: "/view/main.go?mode=diff",
			configure: func(t *testing.T, rootPath string) *gitdiff.Config {
				comparison := changedCatalogRepository(t, rootPath)
				return &comparison
			},
			requiresGit: true,
			wantStatus:  http.StatusOK,
			contains: []string{
				`class="source-mode-tabs"`,
				`href="/view/main.go"`,
				`href="/view/main.go?mode=diff" aria-current="page"`,
				`class="diff-view"`,
				`class="diff-comparison"`,
				`<code>package main</code>`,
				`<code>package changed</code>`,
			},
			excludes: []string{`class="source-view"`},
		},
		{
			name: "File view hides the comparison base control",
			path: "/view/main.go",
			configure: func(t *testing.T, rootPath string) *gitdiff.Config {
				comparison := changedCatalogRepository(t, rootPath)
				return &comparison
			},
			requiresGit: true,
			wantStatus:  http.StatusOK,
			contains: []string{
				`class="source-mode-tabs"`,
				`href="/view/main.go"`,
				`href="/view/main.go?mode=diff"`,
			},
			excludes: []string{`class="diff-comparison"`, `class="diff-comparison-control"`},
		},
		{
			name:       "unconfigured changes view",
			path:       "/view/main.go?mode=diff",
			wantStatus: http.StatusNotFound,
			contains:   []string{"Changes view is unavailable"},
		},
		{
			name: "per-file Git failure preserves file navigation",
			path: "/view/main.go?mode=diff",
			configure: func(_ *testing.T, _ string) *gitdiff.Config {
				return &gitdiff.Config{RepositoryRoot: "/missing/repository", RequestedBase: "HEAD", BaseCommit: strings.Repeat("a", 40)}
			},
			wantStatus: http.StatusOK,
			contains:   []string{`href="/view/main.go"`, "Changes unavailable", "File view remains available."},
		},
		{
			name: "Markdown has changes view",
			path: "/view/README.md?mode=diff",
			configure: func(t *testing.T, rootPath string) *gitdiff.Config {
				comparison := changedCatalogRepository(t, rootPath)
				return &comparison
			},
			requiresGit: true,
			wantStatus:  http.StatusOK,
			contains: []string{
				`class="source-mode-tabs"`,
				`href="/view/README.md?mode=diff" aria-current="page"`,
				`class="diff-view"`,
				`<code># Home</code>`,
			},
			excludes: []string{`<h1>`, `class="source-view"`},
		},
		{name: "unknown view mode", path: "/view/main.go?mode=split", wantStatus: http.StatusBadRequest, contains: []string{"unsupported document mode"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if test.requiresGit {
				if _, err := exec.LookPath("git"); err != nil {
					t.Skip("git executable is unavailable")
				}
			}
			rootPath := t.TempDir()
			writeTestFile(t, filepath.Join(rootPath, "README.md"), "# Home")
			writeTestFile(t, filepath.Join(rootPath, "main.go"), "package main\n")
			root, err := content.Open(rootPath)
			if err != nil {
				t.Fatalf("content.Open() error = %v", err)
			}
			indexOptions, err := content.NewIndexOptions([]string{".go"}, nil)
			if err != nil {
				t.Fatalf("content.NewIndexOptions() error = %v", err)
			}
			serverOptions := []Option{WithIndexOptions(indexOptions)}
			if test.configure != nil {
				serverOptions = append(serverOptions, WithGitComparison(*test.configure(t, rootPath), "", ""))
			}
			viewer, err := New(root, mdrender.New(), serverOptions...)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			response := httptest.NewRecorder()
			viewer.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", response.Code, test.wantStatus, response.Body.String())
			}
			for _, want := range test.contains {
				if !strings.Contains(response.Body.String(), want) {
					t.Errorf("response missing %q: %s", want, response.Body.String())
				}
			}
			for _, unwanted := range test.excludes {
				if strings.Contains(response.Body.String(), unwanted) {
					t.Errorf("response unexpectedly contains %q", unwanted)
				}
			}
		})
	}
}

func TestChangedCatalogMetadata(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git executable is unavailable")
	}

	tests := []struct {
		name        string
		configure   func(*testing.T, string) gitdiff.Config
		wantReady   bool
		wantError   bool
		wantChanged int
	}{
		{name: "catalog intersects changed paths", configure: changedCatalogRepository, wantReady: true, wantChanged: 2},
		{name: "failed lookup remains distinct", configure: func(_ *testing.T, _ string) gitdiff.Config {
			return gitdiff.Config{RepositoryRoot: "/missing/repository", RequestedBase: "HEAD", BaseCommit: strings.Repeat("a", 40)}
		}, wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			rootPath := t.TempDir()
			writeTestFile(t, filepath.Join(rootPath, "README.md"), "# Home")
			comparison := test.configure(t, rootPath)
			root, err := content.Open(rootPath)
			if err != nil {
				t.Fatalf("content.Open() error = %v", err)
			}
			options, err := content.NewIndexOptions([]string{".go"}, nil)
			if err != nil {
				t.Fatalf("content.NewIndexOptions() error = %v", err)
			}
			viewer, err := New(root, mdrender.New(), WithIndexOptions(options), WithGitComparison(comparison, "", ""))
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			response := getResponse(t, viewer.Handler(), "/")
			body := response.Body.String()
			if got := strings.Contains(body, `class="document-changed-filter"`); got != test.wantReady {
				t.Errorf("changed filter present = %t, want %t", got, test.wantReady)
			}
			if got := strings.Contains(body, "Changed-file lookup unavailable."); got != test.wantError {
				t.Errorf("lookup error present = %t, want %t", got, test.wantError)
			}
			if got := strings.Count(body, `data-changed="true"`); got != test.wantChanged {
				t.Errorf("changed document count = %d, want %d", got, test.wantChanged)
			}
		})
	}
}

// changedCatalogRepository freezes a small worktree, then creates supported
// tracked and untracked changes plus one unsupported file for intersection.
func changedCatalogRepository(t *testing.T, repository string) gitdiff.Config {
	t.Helper()
	writeTestFile(t, filepath.Join(repository, "README.md"), "# Home")
	writeTestFile(t, filepath.Join(repository, "main.go"), "package main\n")
	runServerTestGit(t, repository, "init", "-b", "main")
	runServerTestGit(t, repository, "add", "README.md", "main.go")
	runServerTestGit(t, repository, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "initial")
	comparison, err := gitdiff.Open(context.Background(), repository, "HEAD")
	if err != nil {
		t.Fatalf("gitdiff.Open() error = %v", err)
	}
	writeTestFile(t, filepath.Join(repository, "main.go"), "package changed\n")
	writeTestFile(t, filepath.Join(repository, "new.go"), "package added\n")
	writeTestFile(t, filepath.Join(repository, "unsupported.txt"), "not reviewable")
	return comparison
}

// runServerTestGit executes fixture setup without involving production command
// execution, which remains owned and bounded by internal/gitdiff.
func runServerTestGit(t *testing.T, repository string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repository}, arguments...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v error = %v: %s", arguments, err, output)
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
	if !strings.Contains(response.Body.String(), "No reviewable documents found") {
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

func TestAnnotationQueueAPI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		status        string
		seed          bool
		wantStatus    int
		wantDocuments int
		wantErr       string
	}{
		{name: "all annotations", seed: true, wantStatus: http.StatusOK, wantDocuments: 1},
		{name: "actionable filter", status: "open,needs_changes", seed: true, wantStatus: http.StatusOK, wantDocuments: 1},
		{name: "no matching status", status: "closed", seed: true, wantStatus: http.StatusOK},
		{name: "empty queue", wantStatus: http.StatusOK},
		{name: "invalid status", status: "pending", wantStatus: http.StatusBadRequest, wantErr: "invalid annotation status"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
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
			if test.seed {
				saveTestAnnotation(t, store, "README.md", "Before selected after")
			}
			viewer, err := New(root, mdrender.New(), WithAnnotationStore(store))
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}

			requestPath := "/api/annotations"
			if test.status != "" {
				requestPath += "?status=" + url.QueryEscape(test.status)
			}
			request := httptest.NewRequest(http.MethodGet, requestPath, nil)
			response := httptest.NewRecorder()
			viewer.Handler().ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", response.Code, test.wantStatus, response.Body.String())
			}
			if test.wantErr != "" {
				if !strings.Contains(response.Body.String(), test.wantErr) {
					t.Fatalf("body = %q, want containing %q", response.Body.String(), test.wantErr)
				}
				return
			}
			var payload annotationQueueResponse
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatalf("json.Unmarshal() error = %v; body: %s", err, response.Body.String())
			}
			if payload.SchemaVersion != annotation.SchemaVersion || len(payload.Documents) != test.wantDocuments {
				t.Fatalf("queue = %#v, want %d documents", payload, test.wantDocuments)
			}
			if test.wantDocuments > 0 {
				document := payload.Documents[0]
				if document.Document != "README.md" || document.Revision == "" || len(document.Annotations) != 1 {
					t.Fatalf("queued document = %#v", document)
				}
			}
		})
	}
}

func TestAnnotationQueueETag(t *testing.T) {
	t.Parallel()

	newViewer := func(t *testing.T) (*Server, *annotationstore.Store) {
		t.Helper()
		rootPath := t.TempDir()
		writeTestFile(t, filepath.Join(rootPath, "a.md"), "Before selected after")
		writeTestFile(t, filepath.Join(rootPath, "b.md"), "Before selected after")
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
		return viewer, store
	}

	get := func(t *testing.T, viewer *Server, status, ifNoneMatch string) *httptest.ResponseRecorder {
		t.Helper()
		requestPath := "/api/annotations"
		if status != "" {
			requestPath += "?status=" + url.QueryEscape(status)
		}
		request := httptest.NewRequest(http.MethodGet, requestPath, nil)
		if ifNoneMatch != "" {
			request.Header.Set("If-None-Match", ifNoneMatch)
		}
		response := httptest.NewRecorder()
		viewer.Handler().ServeHTTP(response, request)
		return response
	}

	t.Run("matching If-None-Match returns 304 with no body", func(t *testing.T) {
		t.Parallel()
		viewer, store := newViewer(t)
		saveTestAnnotation(t, store, "a.md", "Before selected after")

		first := get(t, viewer, "", "")
		etag := first.Header().Get("ETag")
		if etag == "" {
			t.Fatal("first response has no ETag")
		}

		second := get(t, viewer, "", etag)
		if second.Code != http.StatusNotModified {
			t.Fatalf("status = %d, want %d; body: %s", second.Code, http.StatusNotModified, second.Body.String())
		}
		if second.Body.Len() != 0 {
			t.Fatalf("body = %q, want empty", second.Body.String())
		}
		if got := second.Header().Get("ETag"); got != etag {
			t.Fatalf("ETag = %q, want %q", got, etag)
		}
		if got := second.Header().Get("Cache-Control"); got != "no-store" {
			t.Fatalf("Cache-Control = %q, want no-store", got)
		}
	})

	t.Run("stale If-None-Match returns a fresh body and ETag", func(t *testing.T) {
		t.Parallel()
		viewer, store := newViewer(t)
		saveTestAnnotation(t, store, "a.md", "Before selected after")

		stale := `"0000000000000000000000000000000000000000000000000000000000000000"`
		response := get(t, viewer, "", stale)
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body: %s", response.Code, http.StatusOK, response.Body.String())
		}
		var payload annotationQueueResponse
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
			t.Fatalf("json.Unmarshal() error = %v; body: %s", err, response.Body.String())
		}
		if len(payload.Documents) != 1 {
			t.Fatalf("queue = %#v, want 1 document", payload)
		}
		if response.Header().Get("ETag") == "" {
			t.Fatal("response has no ETag")
		}
	})

	t.Run("malformed If-None-Match falls through to a normal response", func(t *testing.T) {
		t.Parallel()
		viewer, store := newViewer(t)
		saveTestAnnotation(t, store, "a.md", "Before selected after")

		response := get(t, viewer, "", "not-a-valid-etag")
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body: %s", response.Code, http.StatusOK, response.Body.String())
		}
	})

	t.Run("ETag changes when a matching annotation's status changes", func(t *testing.T) {
		t.Parallel()
		viewer, store := newViewer(t)
		revision := saveTestAnnotation(t, store, "a.md", "Before selected after")

		first := get(t, viewer, "open,needs_changes", "")
		etag := first.Header().Get("ETag")

		sidecar, _, err := store.Load("a.md")
		if err != nil {
			t.Fatalf("Store.Load() error = %v", err)
		}
		sidecar.Annotations[0].Status = annotation.StatusNeedsChanges
		sidecar.Annotations[0].UpdatedAt = time.Now().UTC().Truncate(time.Second)
		if _, err := store.Save(sidecar, revision); err != nil {
			t.Fatalf("Store.Save() error = %v", err)
		}

		second := get(t, viewer, "open,needs_changes", etag)
		if second.Code != http.StatusOK {
			t.Fatalf("stale-etag status = %d, want %d (annotation changed)", second.Code, http.StatusOK)
		}
		if got := second.Header().Get("ETag"); got == etag {
			t.Fatalf("ETag = %q, want different from %q after a matching status change", got, etag)
		}
	})

	t.Run("ETag is unchanged by an unrelated document outside the filter", func(t *testing.T) {
		t.Parallel()
		viewer, store := newViewer(t)
		saveTestAnnotation(t, store, "a.md", "Before selected after")
		bRevision := saveTestAnnotation(t, store, "b.md", "Before selected after")
		bSidecar, _, err := store.Load("b.md")
		if err != nil {
			t.Fatalf("Store.Load() error = %v", err)
		}
		bSidecar.Annotations[0].Status = annotation.StatusClosed
		if _, err := store.Save(bSidecar, bRevision); err != nil {
			t.Fatalf("Store.Save() error = %v", err)
		}

		first := get(t, viewer, "open", "")
		etag := first.Header().Get("ETag")

		bSidecar, currentRevision, err := store.Load("b.md")
		if err != nil {
			t.Fatalf("Store.Load() error = %v", err)
		}
		bSidecar.Annotations[0].Status = annotation.StatusRejected
		if _, err := store.Save(bSidecar, currentRevision); err != nil {
			t.Fatalf("Store.Save() error = %v", err)
		}

		second := get(t, viewer, "open", etag)
		if second.Code != http.StatusNotModified {
			t.Fatalf("status = %d, want %d (b.md is outside the open filter)", second.Code, http.StatusNotModified)
		}
	})
}

func TestLiveAgentHandoffAPI(t *testing.T) {
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
	revision := saveTransitionAnnotation(t, store, annotation.StatusOpen)
	viewer, err := New(root, mdrender.New(), WithReviewSession(store, origin, token))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	queueRequest := httptest.NewRequest(http.MethodGet, "/api/annotations?status=open,needs_changes", nil)
	queueResponse := httptest.NewRecorder()
	viewer.Handler().ServeHTTP(queueResponse, queueRequest)
	if queueResponse.Code != http.StatusOK {
		t.Fatalf("queue status = %d; body: %s", queueResponse.Code, queueResponse.Body.String())
	}
	var queue annotationQueueResponse
	if err := json.Unmarshal(queueResponse.Body.Bytes(), &queue); err != nil {
		t.Fatalf("json.Unmarshal(queue) error = %v", err)
	}
	if len(queue.Documents) != 1 || queue.Documents[0].Revision != string(revision) || len(queue.Documents[0].Annotations) != 1 {
		t.Fatalf("queue = %#v", queue)
	}

	initialRevision := string(revision)
	steps := []struct {
		name         string
		method       string
		path         string
		body         string
		stale        bool
		wantStatus   int
		wantState    annotation.Status
		keepRevision bool
	}{
		{name: "agent acknowledges", method: http.MethodPatch, path: "/api/annotations/ann_transition_test", body: `{"document":"README.md","status":"acknowledged","role":"agent"}`, wantStatus: http.StatusOK, wantState: annotation.StatusAcknowledged},
		{name: "stale browser revision conflicts", method: http.MethodPost, path: "/api/annotations/ann_transition_test/replies", body: `{"document":"README.md","role":"reviewer","message":"Concurrent note"}`, stale: true, wantStatus: http.StatusConflict, keepRevision: true},
		{name: "agent replies with current revision", method: http.MethodPost, path: "/api/annotations/ann_transition_test/replies", body: `{"document":"README.md","role":"agent","message":"I retained the existing behavior."}`, wantStatus: http.StatusCreated, wantState: annotation.StatusAcknowledged},
		{name: "agent applies", method: http.MethodPatch, path: "/api/annotations/ann_transition_test", body: `{"document":"README.md","status":"applied","role":"agent","summary":"Updated the documentation.","commit":"abc1234"}`, wantStatus: http.StatusOK, wantState: annotation.StatusApplied},
		{name: "agent cannot close", method: http.MethodPatch, path: "/api/annotations/ann_transition_test", body: `{"document":"README.md","status":"closed","role":"agent"}`, wantStatus: http.StatusBadRequest, keepRevision: true},
		{name: "reviewer requests changes", method: http.MethodPatch, path: "/api/annotations/ann_transition_test", body: `{"document":"README.md","status":"needs_changes","role":"reviewer","message":"Keep the compatibility note."}`, wantStatus: http.StatusOK, wantState: annotation.StatusNeedsChanges},
		{name: "agent acknowledges retry", method: http.MethodPatch, path: "/api/annotations/ann_transition_test", body: `{"document":"README.md","status":"acknowledged","role":"agent"}`, wantStatus: http.StatusOK, wantState: annotation.StatusAcknowledged},
		{name: "agent reapplies", method: http.MethodPatch, path: "/api/annotations/ann_transition_test", body: `{"document":"README.md","status":"applied","role":"agent","summary":"Restored the compatibility note."}`, wantStatus: http.StatusOK, wantState: annotation.StatusApplied},
		{name: "reviewer closes", method: http.MethodPatch, path: "/api/annotations/ann_transition_test", body: `{"document":"README.md","status":"closed","role":"reviewer"}`, wantStatus: http.StatusOK, wantState: annotation.StatusClosed},
	}

	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			request := httptest.NewRequest(step.method, step.path, strings.NewReader(step.body))
			request.Header.Set("Origin", origin)
			request.Header.Set(reviewTokenHeader, token)
			request.Header.Set("Content-Type", "application/json")
			if step.stale {
				request.Header.Set("If-Match", strconv.Quote(initialRevision))
			} else {
				request.Header.Set("If-Match", strconv.Quote(string(revision)))
			}
			response := httptest.NewRecorder()
			viewer.Handler().ServeHTTP(response, request)
			if response.Code != step.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", response.Code, step.wantStatus, response.Body.String())
			}
			if step.keepRevision {
				return
			}
			var payload transitionAnnotationResponse
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatalf("json.Unmarshal() error = %v; body: %s", err, response.Body.String())
			}
			if payload.Annotation.Status != step.wantState || payload.Revision == "" {
				t.Fatalf("mutation payload = %#v", payload)
			}
			revision = annotationstore.Revision(payload.Revision)
		})
	}

	finalQueue := httptest.NewRequest(http.MethodGet, "/api/annotations?status=open,needs_changes", nil)
	finalResponse := httptest.NewRecorder()
	viewer.Handler().ServeHTTP(finalResponse, finalQueue)
	if finalResponse.Code != http.StatusOK {
		t.Fatalf("final queue status = %d; body: %s", finalResponse.Code, finalResponse.Body.String())
	}
	if err := json.Unmarshal(finalResponse.Body.Bytes(), &queue); err != nil {
		t.Fatalf("json.Unmarshal(final queue) error = %v", err)
	}
	if len(queue.Documents) != 0 {
		t.Fatalf("final actionable queue = %#v, want empty", queue)
	}
}

func TestCreateAnnotationAPI(t *testing.T) {
	t.Parallel()

	const (
		origin           = "http://127.0.0.1:8080"
		token            = "0123456789abcdef0123456789abcdef"
		selectedDocument = "Before **selected** after"
	)
	digest := annotation.DocumentSHA256([]byte(selectedDocument))
	selectedBody := `{"document":"README.md","intent":"change_request","comment":"Update this.","role":"reviewer","selection":{"startByte":9,"endByte":17,"documentSHA256":"` + digest + `"}}`
	crossTagBody := `{"document":"README.md","intent":"change_request","comment":"Update this.","role":"reviewer","selection":{"startByte":0,"endByte":19,"documentSHA256":"` + digest + `"}}`
	documentBody := `{"document":"README.md","intent":"question","comment":"Why this document?","role":"reviewer"}`
	tests := []struct {
		name         string
		body         string
		ifMatch      *string
		seedSidecar  bool
		omitToken    bool
		wantStatus   int
		wantExact    string
		wantConflict bool
		wantReattach bool
	}{
		{name: "selected text", body: selectedBody, ifMatch: stringPointer(`""`), wantStatus: http.StatusCreated, wantExact: "selected"},
		{name: "selection across formatting", body: crossTagBody, ifMatch: stringPointer(`""`), wantStatus: http.StatusCreated, wantExact: "Before **selected**"},
		{name: "document level", body: documentBody, ifMatch: stringPointer(`""`), wantStatus: http.StatusCreated},
		{name: "missing review token", body: documentBody, ifMatch: stringPointer(`""`), omitToken: true, wantStatus: http.StatusForbidden},
		{name: "missing revision", body: documentBody, wantStatus: http.StatusPreconditionRequired},
		{name: "malformed revision", body: documentBody, ifMatch: stringPointer("unquoted"), wantStatus: http.StatusBadRequest},
		{name: "stale revision", body: documentBody, ifMatch: stringPointer(`""`), seedSidecar: true, wantStatus: http.StatusConflict, wantConflict: true},
		{name: "stale document digest preserves annotation", body: strings.Replace(selectedBody, digest, strings.Repeat("0", 64), 1), ifMatch: stringPointer(`""`), wantStatus: http.StatusCreated, wantReattach: true},
		{name: "selection range invalid", body: strings.Replace(selectedBody, `"endByte":17`, `"endByte":100`, 1), ifMatch: stringPointer(`""`), wantStatus: http.StatusBadRequest},
		{name: "invalid intent", body: strings.Replace(documentBody, `"question"`, `"unsupported"`, 1), ifMatch: stringPointer(`""`), wantStatus: http.StatusBadRequest},
		{name: "unknown field", body: strings.TrimSuffix(documentBody, "}") + `,"status":"closed"}`, ifMatch: stringPointer(`""`), wantStatus: http.StatusBadRequest},
		{name: "multiple JSON values", body: documentBody + `{}`, ifMatch: stringPointer(`""`), wantStatus: http.StatusBadRequest},
		{name: "oversized body", body: `"` + strings.Repeat("x", int(maxAnnotationMutationBytes)+1) + `"`, ifMatch: stringPointer(`""`), wantStatus: http.StatusRequestEntityTooLarge},
		{name: "non-Markdown document", body: strings.Replace(documentBody, "README.md", "image.png", 1), ifMatch: stringPointer(`""`), wantStatus: http.StatusNotFound},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			rootPath := t.TempDir()
			writeTestFile(t, filepath.Join(rootPath, "README.md"), selectedDocument)
			writeTestFile(t, filepath.Join(rootPath, "image.png"), "not Markdown")
			root, err := content.Open(rootPath)
			if err != nil {
				t.Fatalf("content.Open() error = %v", err)
			}
			store, err := annotationstore.Open(filepath.Join(t.TempDir(), "annotations"))
			if err != nil {
				t.Fatalf("annotationstore.Open() error = %v", err)
			}
			if test.seedSidecar {
				seed := annotation.Sidecar{SchemaVersion: annotation.SchemaVersion, Document: "README.md", Annotations: []annotation.Annotation{}}
				if _, err := store.Save(seed, ""); err != nil {
					t.Fatalf("seed Store.Save() error = %v", err)
				}
			}
			viewer, err := New(root, mdrender.New(), WithReviewSession(store, origin, token))
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}

			request := httptest.NewRequest(http.MethodPost, "/api/annotations", strings.NewReader(test.body))
			request.Header.Set("Origin", origin)
			request.Header.Set("Content-Type", "application/json")
			if !test.omitToken {
				request.Header.Set(reviewTokenHeader, token)
			}
			if test.ifMatch != nil {
				request.Header.Set("If-Match", *test.ifMatch)
			}
			response := httptest.NewRecorder()
			viewer.Handler().ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", response.Code, test.wantStatus, response.Body.String())
			}
			if test.wantConflict && response.Header().Get("ETag") == "" {
				t.Fatal("conflict response is missing current ETag")
			}
			if test.wantStatus != http.StatusCreated {
				return
			}

			var payload createAnnotationResponse
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatalf("json.Unmarshal() error = %v; body: %s", err, response.Body.String())
			}
			if !strings.HasPrefix(payload.Annotation.ID, "ann_") || payload.Revision == "" {
				t.Fatalf("created payload = %#v", payload)
			}
			if got := response.Header().Get("ETag"); got != strconv.Quote(payload.Revision) {
				t.Fatalf("ETag = %q, want %q", got, strconv.Quote(payload.Revision))
			}
			if got := response.Header().Get("Location"); got != "/api/annotations/"+payload.Annotation.ID {
				t.Fatalf("Location = %q, want created annotation location", got)
			}
			if test.wantReattach {
				if payload.Annotation.Source != nil || !payload.Annotation.NeedsReattachment || payload.Annotation.Anchor == nil || payload.Annotation.Anchor.State != annotation.AnchorStale || payload.Annotation.Anchor.Reason != annotation.StaleDocumentChanged {
					t.Fatalf("annotation awaiting reattachment = %#v", payload.Annotation)
				}
			} else if test.wantExact != "" {
				if payload.Annotation.Source == nil || payload.Annotation.Source.Selector.Exact != test.wantExact || payload.Annotation.Anchor == nil || payload.Annotation.Anchor.State != annotation.AnchorExact {
					t.Fatalf("selected annotation = %#v", payload.Annotation)
				}
			} else if payload.Annotation.Source != nil || payload.Annotation.Anchor != nil {
				t.Fatalf("document annotation has source or anchor: %#v", payload.Annotation)
			}
			stored, revision, err := store.Load("README.md")
			if err != nil {
				t.Fatalf("Store.Load() error = %v", err)
			}
			if len(stored.Annotations) != 1 || stored.Annotations[0].NeedsReattachment != test.wantReattach || string(revision) != payload.Revision {
				t.Fatalf("stored sidecar = %#v, revision %q", stored, revision)
			}
		})
	}
}

func TestParseIfMatch(t *testing.T) {
	t.Parallel()

	digest := strings.Repeat("a", 64)
	tests := []struct {
		name       string
		values     []string
		want       annotationstore.Revision
		wantStatus int
	}{
		{name: "missing", wantStatus: http.StatusPreconditionRequired},
		{name: "empty revision", values: []string{`""`}},
		{name: "digest", values: []string{strconv.Quote(digest)}, want: annotationstore.Revision(digest)},
		{name: "unquoted", values: []string{digest}, wantStatus: http.StatusBadRequest},
		{name: "weak", values: []string{`W/""`}, wantStatus: http.StatusBadRequest},
		{name: "multiple", values: []string{`""`, strconv.Quote(digest)}, wantStatus: http.StatusBadRequest},
		{name: "comma list", values: []string{`"", "` + digest + `"`}, wantStatus: http.StatusBadRequest},
		{name: "short digest", values: []string{`"aa"`}, wantStatus: http.StatusBadRequest},
		{name: "uppercase digest", values: []string{strconv.Quote(strings.ToUpper(digest))}, wantStatus: http.StatusBadRequest},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequest(http.MethodPost, "/api/annotations", nil)
			for _, value := range test.values {
				request.Header.Add("If-Match", value)
			}
			got, status, err := parseIfMatch(request)
			if test.wantStatus != 0 {
				if err == nil || status != test.wantStatus {
					t.Fatalf("parseIfMatch() = %q, status %d, error %v; want status %d", got, status, err, test.wantStatus)
				}
				return
			}
			if err != nil || status != 0 || got != test.want {
				t.Fatalf("parseIfMatch() = %q, status %d, error %v; want %q", got, status, err, test.want)
			}
		})
	}
}

// stringPointer returns a stable pointer for optional string table fields.
func stringPointer(value string) *string {
	return &value
}

func TestReplyAnnotationAPI(t *testing.T) {
	t.Parallel()

	const (
		origin = "http://127.0.0.1:8080"
		token  = "0123456789abcdef0123456789abcdef"
	)
	validBody := `{"document":"README.md","message":"Please also update the example.","role":"reviewer"}`
	tests := []struct {
		name         string
		annotationID string
		body         string
		useCurrent   bool
		omitIfMatch  bool
		wantStatus   int
		wantConflict bool
	}{
		{name: "append reply", annotationID: "ann_api_test", body: validBody, useCurrent: true, wantStatus: http.StatusCreated},
		{name: "annotation missing", annotationID: "ann_missing", body: validBody, useCurrent: true, wantStatus: http.StatusNotFound},
		{name: "empty message", annotationID: "ann_api_test", body: strings.Replace(validBody, "Please also update the example.", "", 1), useCurrent: true, wantStatus: http.StatusBadRequest},
		{name: "empty role", annotationID: "ann_api_test", body: strings.Replace(validBody, "reviewer", "", 1), useCurrent: true, wantStatus: http.StatusBadRequest},
		{name: "unknown field", annotationID: "ann_api_test", body: strings.TrimSuffix(validBody, "}") + `,"kind":"resolution"}`, useCurrent: true, wantStatus: http.StatusBadRequest},
		{name: "missing revision", annotationID: "ann_api_test", body: validBody, omitIfMatch: true, wantStatus: http.StatusPreconditionRequired},
		{name: "stale revision", annotationID: "ann_api_test", body: validBody, wantStatus: http.StatusConflict, wantConflict: true},
		{name: "non-Markdown document", annotationID: "ann_api_test", body: strings.Replace(validBody, "README.md", "image.png", 1), useCurrent: true, wantStatus: http.StatusNotFound},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			rootPath := t.TempDir()
			writeTestFile(t, filepath.Join(rootPath, "README.md"), "Before selected after")
			writeTestFile(t, filepath.Join(rootPath, "image.png"), "not Markdown")
			root, err := content.Open(rootPath)
			if err != nil {
				t.Fatalf("content.Open() error = %v", err)
			}
			store, err := annotationstore.Open(filepath.Join(t.TempDir(), "annotations"))
			if err != nil {
				t.Fatalf("annotationstore.Open() error = %v", err)
			}
			currentRevision := saveTestAnnotation(t, store, "README.md", "Before selected after")
			viewer, err := New(root, mdrender.New(), WithReviewSession(store, origin, token))
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}

			requestPath := "/api/annotations/" + test.annotationID + "/replies"
			request := httptest.NewRequest(http.MethodPost, requestPath, strings.NewReader(test.body))
			request.Header.Set("Origin", origin)
			request.Header.Set(reviewTokenHeader, token)
			request.Header.Set("Content-Type", "application/json")
			if !test.omitIfMatch {
				revision := annotationstore.Revision("")
				if test.useCurrent {
					revision = currentRevision
				}
				request.Header.Set("If-Match", strconv.Quote(string(revision)))
			}
			response := httptest.NewRecorder()
			viewer.Handler().ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", response.Code, test.wantStatus, response.Body.String())
			}
			if test.wantConflict && response.Header().Get("ETag") != strconv.Quote(string(currentRevision)) {
				t.Fatalf("conflict ETag = %q, want current revision", response.Header().Get("ETag"))
			}
			if test.wantStatus != http.StatusCreated {
				return
			}

			var payload replyAnnotationResponse
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatalf("json.Unmarshal() error = %v; body: %s", err, response.Body.String())
			}
			thread := payload.Annotation.Thread
			if len(thread) != 1 || thread[0].Kind != annotation.ThreadReply || thread[0].Message != "Please also update the example." || !strings.HasPrefix(thread[0].ID, "msg_") {
				t.Fatalf("updated thread = %#v", thread)
			}
			if payload.Annotation.Status != annotation.StatusOpen {
				t.Fatalf("status = %q, want unchanged open status", payload.Annotation.Status)
			}
			if got := response.Header().Get("Location"); got != requestPath+"/"+thread[0].ID {
				t.Fatalf("Location = %q, want created reply location", got)
			}
			stored, revision, err := store.Load("README.md")
			if err != nil {
				t.Fatalf("Store.Load() error = %v", err)
			}
			if len(stored.Annotations[0].Thread) != 1 || string(revision) != payload.Revision {
				t.Fatalf("stored sidecar = %#v, revision %q", stored, revision)
			}
		})
	}
}

func TestTransitionEntries(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)
	tests := []struct {
		name      string
		from      annotation.Status
		input     transitionAnnotationRequest
		wantKinds []annotation.ThreadKind
		wantErr   string
	}{
		{name: "acknowledge open", from: annotation.StatusOpen, input: transitionAnnotationRequest{Status: annotation.StatusAcknowledged, Role: annotation.RoleAgent}, wantKinds: []annotation.ThreadKind{annotation.ThreadAcknowledgement, annotation.ThreadStatusChange}},
		{name: "acknowledge retry", from: annotation.StatusNeedsChanges, input: transitionAnnotationRequest{Status: annotation.StatusAcknowledged, Role: annotation.RoleAgent}, wantKinds: []annotation.ThreadKind{annotation.ThreadAcknowledgement, annotation.ThreadStatusChange}},
		{name: "report applied", from: annotation.StatusAcknowledged, input: transitionAnnotationRequest{Status: annotation.StatusApplied, Role: annotation.RoleAgent, Summary: "Implemented", Commit: "abc1234"}, wantKinds: []annotation.ThreadKind{annotation.ThreadResolution, annotation.ThreadStatusChange}},
		{name: "request changes", from: annotation.StatusApplied, input: transitionAnnotationRequest{Status: annotation.StatusNeedsChanges, Role: annotation.RoleReviewer, Message: "Keep the default."}, wantKinds: []annotation.ThreadKind{annotation.ThreadReview, annotation.ThreadStatusChange}},
		{name: "reject request", from: annotation.StatusOpen, input: transitionAnnotationRequest{Status: annotation.StatusRejected, Role: annotation.RoleAgent, Message: "Conflicts with policy."}, wantKinds: []annotation.ThreadKind{annotation.ThreadReply, annotation.ThreadStatusChange}},
		{name: "reviewer dismisses open", from: annotation.StatusOpen, input: transitionAnnotationRequest{Status: annotation.StatusClosed, Role: annotation.RoleReviewer}, wantKinds: []annotation.ThreadKind{annotation.ThreadStatusChange}},
		{name: "close applied", from: annotation.StatusApplied, input: transitionAnnotationRequest{Status: annotation.StatusClosed, Role: annotation.RoleReviewer}, wantKinds: []annotation.ThreadKind{annotation.ThreadStatusChange}},
		{name: "reopen closed", from: annotation.StatusClosed, input: transitionAnnotationRequest{Status: annotation.StatusOpen, Role: annotation.RoleReviewer}, wantKinds: []annotation.ThreadKind{annotation.ThreadStatusChange}},
		{name: "missing resolution summary", from: annotation.StatusAcknowledged, input: transitionAnnotationRequest{Status: annotation.StatusApplied, Role: annotation.RoleAgent}, wantErr: "summary"},
		{name: "missing review message", from: annotation.StatusApplied, input: transitionAnnotationRequest{Status: annotation.StatusNeedsChanges, Role: annotation.RoleReviewer}, wantErr: "message"},
		{name: "missing rejection reason", from: annotation.StatusOpen, input: transitionAnnotationRequest{Status: annotation.StatusRejected, Role: annotation.RoleAgent}, wantErr: "message"},
		{name: "metadata on acknowledgement", from: annotation.StatusOpen, input: transitionAnnotationRequest{Status: annotation.StatusAcknowledged, Role: annotation.RoleAgent, Message: "unexpected"}, wantErr: "does not accept"},
		{name: "blank role", from: annotation.StatusOpen, input: transitionAnnotationRequest{Status: annotation.StatusAcknowledged, Role: " "}, wantErr: "role"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			current := annotation.Annotation{Status: test.from}
			entries, err := transitionEntries(current, test.input, now)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("transitionEntries() error = %v, want containing %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("transitionEntries() error = %v", err)
			}
			if len(entries) != len(test.wantKinds) {
				t.Fatalf("entry count = %d, want %d: %#v", len(entries), len(test.wantKinds), entries)
			}
			for index, wantKind := range test.wantKinds {
				if entries[index].Kind != wantKind || !strings.HasPrefix(entries[index].ID, "msg_") {
					t.Errorf("entry %d = %#v, want kind %q and msg_ ID", index, entries[index], wantKind)
				}
			}
			statusChange := entries[len(entries)-1]
			if statusChange.FromStatus != test.from || statusChange.ToStatus != test.input.Status || statusChange.Role != test.input.Role {
				t.Fatalf("status change = %#v", statusChange)
			}
		})
	}
}

func TestTransitionAnnotationAPI(t *testing.T) {
	t.Parallel()

	const (
		origin = "http://127.0.0.1:8080"
		token  = "0123456789abcdef0123456789abcdef"
	)
	validBody := `{"document":"README.md","status":"needs_changes","role":"reviewer","message":"Keep the loopback default."}`
	tests := []struct {
		name         string
		annotationID string
		body         string
		useCurrent   bool
		omitIfMatch  bool
		wantStatus   int
		wantConflict bool
	}{
		{name: "request changes", annotationID: "ann_transition_test", body: validBody, useCurrent: true, wantStatus: http.StatusOK},
		{name: "agent cannot request changes", annotationID: "ann_transition_test", body: strings.Replace(validBody, `"reviewer"`, `"agent"`, 1), useCurrent: true, wantStatus: http.StatusBadRequest},
		{name: "review message required", annotationID: "ann_transition_test", body: strings.Replace(validBody, "Keep the loopback default.", "", 1), useCurrent: true, wantStatus: http.StatusBadRequest},
		{name: "annotation missing", annotationID: "ann_missing", body: validBody, useCurrent: true, wantStatus: http.StatusNotFound},
		{name: "missing revision", annotationID: "ann_transition_test", body: validBody, omitIfMatch: true, wantStatus: http.StatusPreconditionRequired},
		{name: "stale revision", annotationID: "ann_transition_test", body: validBody, wantStatus: http.StatusConflict, wantConflict: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			rootPath := t.TempDir()
			writeTestFile(t, filepath.Join(rootPath, "README.md"), "# Review")
			root, err := content.Open(rootPath)
			if err != nil {
				t.Fatalf("content.Open() error = %v", err)
			}
			store, err := annotationstore.Open(filepath.Join(t.TempDir(), "annotations"))
			if err != nil {
				t.Fatalf("annotationstore.Open() error = %v", err)
			}
			currentRevision := saveTransitionAnnotation(t, store, annotation.StatusApplied)
			viewer, err := New(root, mdrender.New(), WithReviewSession(store, origin, token))
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}

			requestPath := "/api/annotations/" + test.annotationID
			request := httptest.NewRequest(http.MethodPatch, requestPath, strings.NewReader(test.body))
			request.Header.Set("Origin", origin)
			request.Header.Set(reviewTokenHeader, token)
			request.Header.Set("Content-Type", "application/json")
			if !test.omitIfMatch {
				revision := annotationstore.Revision("")
				if test.useCurrent {
					revision = currentRevision
				}
				request.Header.Set("If-Match", strconv.Quote(string(revision)))
			}
			response := httptest.NewRecorder()
			viewer.Handler().ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", response.Code, test.wantStatus, response.Body.String())
			}
			if test.wantConflict && response.Header().Get("ETag") != strconv.Quote(string(currentRevision)) {
				t.Fatalf("conflict ETag = %q, want current revision", response.Header().Get("ETag"))
			}
			if test.wantStatus != http.StatusOK {
				return
			}

			var payload transitionAnnotationResponse
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatalf("json.Unmarshal() error = %v; body: %s", err, response.Body.String())
			}
			thread := payload.Annotation.Thread
			if payload.Annotation.Status != annotation.StatusNeedsChanges || len(thread) != 2 || thread[0].Kind != annotation.ThreadReview || thread[0].Message != "Keep the loopback default." || thread[1].Kind != annotation.ThreadStatusChange {
				t.Fatalf("transitioned annotation = %#v", payload.Annotation)
			}
			stored, revision, err := store.Load("README.md")
			if err != nil {
				t.Fatalf("Store.Load() error = %v", err)
			}
			if stored.Annotations[0].Status != annotation.StatusNeedsChanges || string(revision) != payload.Revision {
				t.Fatalf("stored sidecar = %#v, revision %q", stored, revision)
			}
		})
	}
}

func TestReattachAnnotationAPI(t *testing.T) {
	t.Parallel()

	const (
		origin          = "http://127.0.0.1:8080"
		token           = "0123456789abcdef0123456789abcdef"
		currentDocument = "Before new selection after"
	)
	digest := annotation.DocumentSHA256([]byte(currentDocument))
	validBody := `{"document":"README.md","selection":{"startByte":7,"endByte":20,"documentSHA256":"` + digest + `"}}`
	tests := []struct {
		name         string
		annotationID string
		body         string
		sourceMode   string
		useCurrent   bool
		omitIfMatch  bool
		wantStatus   int
		wantConflict bool
	}{
		{name: "reattach stale anchor", annotationID: "ann_reattach_test", body: validBody, sourceMode: "stale", useCurrent: true, wantStatus: http.StatusOK},
		{name: "reattach selection lost during creation", annotationID: "ann_reattach_test", body: validBody, sourceMode: "pending", useCurrent: true, wantStatus: http.StatusOK},
		{name: "resolved anchor", annotationID: "ann_reattach_test", body: validBody, sourceMode: "exact", useCurrent: true, wantStatus: http.StatusConflict},
		{name: "document annotation", annotationID: "ann_reattach_test", body: validBody, sourceMode: "document", useCurrent: true, wantStatus: http.StatusConflict},
		{name: "stale document digest", annotationID: "ann_reattach_test", body: strings.Replace(validBody, digest, strings.Repeat("0", 64), 1), sourceMode: "stale", useCurrent: true, wantStatus: http.StatusConflict},
		{name: "invalid range", annotationID: "ann_reattach_test", body: strings.Replace(validBody, `"endByte":20`, `"endByte":200`, 1), sourceMode: "stale", useCurrent: true, wantStatus: http.StatusBadRequest},
		{name: "annotation missing", annotationID: "ann_missing", body: validBody, sourceMode: "stale", useCurrent: true, wantStatus: http.StatusNotFound},
		{name: "unknown field", annotationID: "ann_reattach_test", body: strings.TrimSuffix(validBody, "}") + `,"reason":"moved"}`, sourceMode: "stale", useCurrent: true, wantStatus: http.StatusBadRequest},
		{name: "missing revision", annotationID: "ann_reattach_test", body: validBody, sourceMode: "stale", omitIfMatch: true, wantStatus: http.StatusPreconditionRequired},
		{name: "stale revision", annotationID: "ann_reattach_test", body: validBody, sourceMode: "stale", wantStatus: http.StatusConflict, wantConflict: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			rootPath := t.TempDir()
			writeTestFile(t, filepath.Join(rootPath, "README.md"), currentDocument)
			root, err := content.Open(rootPath)
			if err != nil {
				t.Fatalf("content.Open() error = %v", err)
			}
			store, err := annotationstore.Open(filepath.Join(t.TempDir(), "annotations"))
			if err != nil {
				t.Fatalf("annotationstore.Open() error = %v", err)
			}
			currentRevision := saveReattachAnnotation(t, store, test.sourceMode)
			viewer, err := New(root, mdrender.New(), WithReviewSession(store, origin, token))
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}

			requestPath := "/api/annotations/" + test.annotationID + "/reattach"
			request := httptest.NewRequest(http.MethodPost, requestPath, strings.NewReader(test.body))
			request.Header.Set("Origin", origin)
			request.Header.Set(reviewTokenHeader, token)
			request.Header.Set("Content-Type", "application/json")
			if !test.omitIfMatch {
				revision := annotationstore.Revision("")
				if test.useCurrent {
					revision = currentRevision
				}
				request.Header.Set("If-Match", strconv.Quote(string(revision)))
			}
			response := httptest.NewRecorder()
			viewer.Handler().ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", response.Code, test.wantStatus, response.Body.String())
			}
			if test.wantConflict && response.Header().Get("ETag") != strconv.Quote(string(currentRevision)) {
				t.Fatalf("conflict ETag = %q, want current revision", response.Header().Get("ETag"))
			}
			if test.wantStatus != http.StatusOK {
				return
			}

			var payload reattachAnnotationResponse
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatalf("json.Unmarshal() error = %v; body: %s", err, response.Body.String())
			}
			if payload.Annotation.Source == nil || payload.Annotation.Source.Selector.Exact != "new selection" || payload.Annotation.Anchor == nil || payload.Annotation.Anchor.State != annotation.AnchorExact {
				t.Fatalf("reattached annotation = %#v", payload.Annotation)
			}
			stored, revision, err := store.Load("README.md")
			if err != nil {
				t.Fatalf("Store.Load() error = %v", err)
			}
			if stored.Annotations[0].Source.Selector.Exact != "new selection" || stored.Annotations[0].NeedsReattachment || string(revision) != payload.Revision {
				t.Fatalf("stored sidecar = %#v, revision %q", stored, revision)
			}
		})
	}
}

// saveReattachAnnotation persists a text or document annotation for
// reattachment API tests.
func saveReattachAnnotation(t *testing.T, store *annotationstore.Store, sourceMode string) annotationstore.Revision {
	t.Helper()
	var source *annotation.Source
	needsReattachment := false
	switch sourceMode {
	case "stale":
		created, err := annotation.NewSource([]byte("Before old selection after"), 7, 20)
		if err != nil {
			t.Fatalf("annotation.NewSource() error = %v", err)
		}
		source = &created
	case "exact":
		created, err := annotation.NewSource([]byte("Before new selection after"), 7, 20)
		if err != nil {
			t.Fatalf("annotation.NewSource() error = %v", err)
		}
		source = &created
	case "document":
	case "pending":
		needsReattachment = true
	default:
		t.Fatalf("unsupported source mode %q", sourceMode)
	}
	createdAt := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	sidecar := annotation.Sidecar{
		SchemaVersion: annotation.SchemaVersion,
		Document:      "README.md",
		Annotations: []annotation.Annotation{
			{
				ID:                "ann_reattach_test",
				Intent:            annotation.IntentChangeRequest,
				Status:            annotation.StatusOpen,
				Comment:           "Update this selection.",
				Role:              "reviewer",
				CreatedAt:         createdAt,
				UpdatedAt:         createdAt,
				Source:            source,
				NeedsReattachment: needsReattachment,
				Thread:            []annotation.ThreadEntry{},
			},
		},
	}
	revision, err := store.Save(sidecar, "")
	if err != nil {
		t.Fatalf("Store.Save() error = %v", err)
	}
	return revision
}

// saveTransitionAnnotation persists one document annotation in the requested
// lifecycle state for transition API tests.
func saveTransitionAnnotation(t *testing.T, store *annotationstore.Store, status annotation.Status) annotationstore.Revision {
	t.Helper()
	created := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	sidecar := annotation.Sidecar{
		SchemaVersion: annotation.SchemaVersion,
		Document:      "README.md",
		Annotations: []annotation.Annotation{
			{
				ID:        "ann_transition_test",
				Intent:    annotation.IntentChangeRequest,
				Status:    status,
				Comment:   "Keep the default.",
				Role:      "reviewer",
				CreatedAt: created,
				UpdatedAt: created,
				Thread:    []annotation.ThreadEntry{},
			},
		},
	}
	revision, err := store.Save(sidecar, "")
	if err != nil {
		t.Fatalf("Store.Save() error = %v", err)
	}
	return revision
}

func TestReviewSessionConfiguration(t *testing.T) {
	t.Parallel()

	store, err := annotationstore.Open(filepath.Join(t.TempDir(), "annotations"))
	if err != nil {
		t.Fatalf("annotationstore.Open() error = %v", err)
	}
	validToken := strings.Repeat("t", 32)
	tests := []struct {
		name       string
		store      *annotationstore.Store
		origin     string
		token      string
		wantOrigin string
		wantErr    bool
	}{
		{name: "valid IPv4 loopback", store: store, origin: "http://127.0.0.1:8080/", token: validToken, wantOrigin: "http://127.0.0.1:8080"},
		{name: "valid IPv6 loopback", store: store, origin: "http://[::1]:8080", token: validToken, wantOrigin: "http://[::1]:8080"},
		{name: "nil store", origin: "http://127.0.0.1:8080", token: validToken, wantErr: true},
		{name: "non-loopback host", store: store, origin: "http://example.com:8080", token: validToken, wantErr: true},
		{name: "HTTPS origin", store: store, origin: "https://127.0.0.1:8080", token: validToken, wantErr: true},
		{name: "origin path", store: store, origin: "http://127.0.0.1:8080/view", token: validToken, wantErr: true},
		{name: "origin credentials", store: store, origin: "http://user@127.0.0.1:8080", token: validToken, wantErr: true},
		{name: "short token", store: store, origin: "http://127.0.0.1:8080", token: "short", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := &Server{}
			err := WithReviewSession(test.store, test.origin, test.token)(server)
			if test.wantErr {
				if err == nil {
					t.Fatal("WithReviewSession() error = nil, want rejection")
				}
				return
			}
			if err != nil {
				t.Fatalf("WithReviewSession() error = %v", err)
			}
			if server.annotations != test.store || server.review == nil || server.review.origin != test.wantOrigin || server.review.token != test.token {
				t.Fatalf("review session = %#v, annotation store = %#v", server.review, server.annotations)
			}
		})
	}
}

func TestReviewMutationProtection(t *testing.T) {
	t.Parallel()

	const origin = "http://127.0.0.1:8080"
	token := strings.Repeat("t", 32)
	tests := []struct {
		name        string
		review      *reviewSession
		origin      string
		token       string
		contentType string
		body        string
		wantStatus  int
		wantCalled  bool
	}{
		{name: "review disabled", contentType: "application/json", body: `{}`, wantStatus: http.StatusNotFound},
		{name: "missing origin", review: &reviewSession{origin: origin, token: token}, token: token, contentType: "application/json", body: `{}`, wantStatus: http.StatusForbidden},
		{name: "wrong origin", review: &reviewSession{origin: origin, token: token}, origin: "http://evil.example", token: token, contentType: "application/json", body: `{}`, wantStatus: http.StatusForbidden},
		{name: "missing token", review: &reviewSession{origin: origin, token: token}, origin: origin, contentType: "application/json", body: `{}`, wantStatus: http.StatusForbidden},
		{name: "wrong token", review: &reviewSession{origin: origin, token: token}, origin: origin, token: strings.Repeat("x", 32), contentType: "application/json", body: `{}`, wantStatus: http.StatusForbidden},
		{name: "missing content type", review: &reviewSession{origin: origin, token: token}, origin: origin, token: token, body: `{}`, wantStatus: http.StatusUnsupportedMediaType},
		{name: "non-JSON content type", review: &reviewSession{origin: origin, token: token}, origin: origin, token: token, contentType: "text/plain", body: `{}`, wantStatus: http.StatusUnsupportedMediaType},
		{name: "JSON with charset", review: &reviewSession{origin: origin, token: token}, origin: origin, token: token, contentType: "application/json; charset=utf-8", body: `{}`, wantStatus: http.StatusNoContent, wantCalled: true},
		{name: "oversized body", review: &reviewSession{origin: origin, token: token}, origin: origin, token: token, contentType: "application/json", body: strings.Repeat("x", int(maxAnnotationMutationBytes)+1), wantStatus: http.StatusRequestEntityTooLarge, wantCalled: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := &Server{review: test.review}
			called := false
			next := http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				called = true
				if _, err := io.ReadAll(request.Body); err != nil {
					http.Error(response, "request body is too large", http.StatusRequestEntityTooLarge)
					return
				}
				response.WriteHeader(http.StatusNoContent)
			})
			handler := server.protectReviewMutation(next)
			request := httptest.NewRequest(http.MethodPost, "/api/annotations", strings.NewReader(test.body))
			if test.origin != "" {
				request.Header.Set("Origin", test.origin)
			}
			if test.token != "" {
				request.Header.Set(reviewTokenHeader, test.token)
			}
			if test.contentType != "" {
				request.Header.Set("Content-Type", test.contentType)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.wantStatus || called != test.wantCalled {
				t.Fatalf("status = %d, called = %t; want %d, %t", response.Code, called, test.wantStatus, test.wantCalled)
			}
		})
	}
}

func TestReviewPageEmbedding(t *testing.T) {
	t.Parallel()

	token := strings.Repeat("t", 32)
	tests := []struct {
		name       string
		review     bool
		wantToken  bool
		wantPanel  bool
		wantSource bool
		wantDigest bool
		wantHTMX   bool
	}{
		{name: "read-only page omits token"},
		{name: "review page embeds controls", review: true, wantToken: true, wantPanel: true, wantSource: true, wantDigest: true, wantHTMX: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			rootPath := t.TempDir()
			writeTestFile(t, filepath.Join(rootPath, "README.md"), "# Home")
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
				options = append(options, WithReviewSession(store, "http://127.0.0.1:8080", token))
			}
			viewer, err := New(root, mdrender.New(), options...)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			response := getResponse(t, viewer.Handler(), "/")
			if body := response.Body.String(); !strings.Contains(body, `<link rel="stylesheet" href="/static/styles.css">`) || strings.Contains(body, "<style>") {
				t.Fatalf("page does not use only the external viewer stylesheet:\n%s", body)
			}
			if body := response.Body.String(); !strings.Contains(body, `class="panel-toggle documents-toggle"`) || !strings.Contains(body, `class="document-search"`) || !strings.Contains(body, `src="/static/viewer.js"`) {
				t.Fatalf("page does not contain shared document-panel controls:\n%s", body)
			}
			hasHTMX := strings.Contains(response.Body.String(), `src="/static/htmx.min.js"`)
			if hasHTMX != test.wantHTMX {
				t.Fatalf("page loads HTMX = %t, want %t", hasHTMX, test.wantHTMX)
			}
			hasToken := strings.Contains(response.Body.String(), `name="code-annotator-review-token" content="`+token+`"`)
			if hasToken != test.wantToken {
				t.Fatalf("page contains review token = %t, want %t", hasToken, test.wantToken)
			}
			hasPanel := strings.Contains(response.Body.String(), `class="review-panel"`) && strings.Contains(response.Body.String(), `class="annotation-form"`) && strings.Contains(response.Body.String(), `class="show-inactive-annotations"`) && strings.Contains(response.Body.String(), `id="annotation-panel-content"`) && strings.Contains(response.Body.String(), `hx-post="/ui/review/annotations"`) && strings.Contains(response.Body.String(), `src="/static/review.js"`)
			if hasPanel != test.wantPanel {
				t.Fatalf("page contains review panel = %t, want %t", hasPanel, test.wantPanel)
			}
			hasReviewToggle := strings.Contains(response.Body.String(), `class="panel-toggle review-toggle"`)
			if hasReviewToggle != test.wantPanel {
				t.Fatalf("page contains annotation-panel toggle = %t, want %t", hasReviewToggle, test.wantPanel)
			}
			hasOpenCommentsFilter := strings.Contains(response.Body.String(), `class="document-open-filter"`)
			if hasOpenCommentsFilter != test.wantPanel {
				t.Fatalf("page contains open-comments filter = %t, want %t", hasOpenCommentsFilter, test.wantPanel)
			}
			hasSource := strings.Contains(response.Body.String(), `class="source-text" data-source-start=`)
			if hasSource != test.wantSource {
				t.Fatalf("page contains source metadata = %t, want %t", hasSource, test.wantSource)
			}
			digest := annotation.DocumentSHA256([]byte("# Home"))
			hasDigest := strings.Contains(response.Body.String(), `data-document-sha256="`+digest+`"`)
			if hasDigest != test.wantDigest {
				t.Fatalf("page contains document digest = %t, want %t", hasDigest, test.wantDigest)
			}
		})
	}
}

func TestStaticAssets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		path         string
		method       string
		wantStatus   int
		wantType     string
		wantContents []string
	}{
		{name: "get review script", path: "/static/review.js", method: http.MethodGet, wantStatus: http.StatusOK, wantType: "text/javascript; charset=utf-8", wantContents: []string{"./review-fragments.js", "./review-highlights.js", "./review-htmx.js", "./review-navigation.js", "./review-panel.js", "./review-selection.js", "configureReviewHTMX"}},
		{name: "get review fragments module", path: "/static/review-fragments.js", method: http.MethodGet, wantStatus: http.StatusOK, wantType: "text/javascript; charset=utf-8", wantContents: []string{"annotationLocation", "annotationLocations", "configureLifecycleForm"}},
		{name: "get review HTMX module", path: "/static/review-htmx.js", method: http.MethodGet, wantStatus: http.StatusOK, wantType: "text/javascript; charset=utf-8", wantContents: []string{"configureReviewHTMX", "htmx:configRequest", "htmx:beforeSwap", "X-Code-Annotator-Token", "If-Match"}},
		{name: "get review highlights module", path: "/static/review-highlights.js", method: http.MethodGet, wantStatus: http.StatusOK, wantType: "text/javascript; charset=utf-8", wantContents: []string{"createAnnotationHighlighter", "renderAnnotationHighlights", "sourceRange"}},
		{name: "get review navigation module", path: "/static/review-navigation.js", method: http.MethodGet, wantStatus: http.StatusOK, wantType: "text/javascript; charset=utf-8", wantContents: []string{"createAnnotationNavigator", "navigateFromAnnotation", "emphasizeNavigationTarget"}},
		{name: "get review panel module", path: "/static/review-panel.js", method: http.MethodGet, wantStatus: http.StatusOK, wantType: "text/javascript; charset=utf-8", wantContents: []string{"createReviewPanelController", "setAnnotationFormVisible", "startReviewPanelResize"}},
		{name: "get review selection module", path: "/static/review-selection.js", method: http.MethodGet, wantStatus: http.StatusOK, wantType: "text/javascript; charset=utf-8", wantContents: []string{"createSelectionController", "captureDiagramSelection", "diagramSelectionActive", "currentSelection"}},
		{name: "get viewer script", path: "/static/viewer.js", method: http.MethodGet, wantStatus: http.StatusOK, wantType: "text/javascript; charset=utf-8", wantContents: []string{"./document-tree.js", "bindPanelToggle", "documents-collapsed", "review-collapsed", "bindDocumentSearch", "open-comments"}},
		{name: "get viewer state module", path: "/static/viewer-state.js", method: http.MethodGet, wantStatus: http.StatusOK, wantType: "text/javascript; charset=utf-8", wantContents: []string{"parseViewerState", "fetchViewerState", "viewer state schemaVersion is unsupported"}},
		{name: "get document tree module", path: "/static/document-tree.js", method: http.MethodGet, wantStatus: http.StatusOK, wantType: "text/javascript; charset=utf-8", wantContents: []string{"buildDocumentTree", "updateTreeVisibility", "document-tree-expanded"}},
		{name: "get viewer stylesheet", path: "/static/styles.css", method: http.MethodGet, wantStatus: http.StatusOK, wantType: "text/css; charset=utf-8", wantContents: []string{".markdown-body", ".mermaid-output", ".review-panel", "font-variant-ligatures: none", "min-width: max-content"}},
		{name: "get HTMX library", path: "/static/htmx.min.js", method: http.MethodGet, wantStatus: http.StatusOK, wantType: "text/javascript; charset=utf-8", wantContents: []string{"htmx", "2.0.10"}},
		{name: "get Mermaid integration", path: "/static/mermaid.js", method: http.MethodGet, wantStatus: http.StatusOK, wantType: "text/javascript; charset=utf-8", wantContents: []string{`securityLevel: "strict"`, "maxDiagramCharacters", "mermaid.render"}},
		{name: "get Mermaid library", path: "/static/mermaid.tiny.js", method: http.MethodGet, wantStatus: http.StatusOK, wantType: "text/javascript; charset=utf-8", wantContents: []string{"mermaid"}},
		{name: "reject review script post", path: "/static/review.js", method: http.MethodPost, wantStatus: http.StatusMethodNotAllowed},
		{name: "reject HTMX library post", path: "/static/htmx.min.js", method: http.MethodPost, wantStatus: http.StatusMethodNotAllowed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			handler := newTestHandler(t, nil)
			request := httptest.NewRequest(test.method, test.path, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", response.Code, test.wantStatus, response.Body.String())
			}
			if test.wantType != "" && response.Header().Get("Content-Type") != test.wantType {
				t.Errorf("Content-Type = %q, want %q", response.Header().Get("Content-Type"), test.wantType)
			}
			for _, wantContent := range test.wantContents {
				if !strings.Contains(response.Body.String(), wantContent) {
					t.Errorf("asset does not contain %q", wantContent)
				}
			}
		})
	}
}

func TestMermaidAssetsAreLoadedOnlyForDiagramPages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                  string
		source                string
		wantAssets            bool
		wantInlineStylePolicy bool
	}{
		{name: "ordinary Markdown omits Mermaid assets", source: "# Home\n"},
		{name: "Mermaid fence loads Mermaid assets and permits generated styles", source: "```mermaid\nsequenceDiagram\n  A->>B: Hello\n```\n", wantAssets: true, wantInlineStylePolicy: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			rootPath := t.TempDir()
			writeTestFile(t, filepath.Join(rootPath, "README.md"), test.source)
			root, err := content.Open(rootPath)
			if err != nil {
				t.Fatalf("content.Open() error = %v", err)
			}
			viewer, err := New(root, mdrender.New())
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			response := getResponse(t, viewer.Handler(), "/")
			body := response.Body.String()
			hasAssets := strings.Contains(body, `src="/static/mermaid.tiny.js"`) && strings.Contains(body, `src="/static/mermaid.js"`)
			if hasAssets != test.wantAssets {
				t.Fatalf("page contains Mermaid assets = %t, want %t", hasAssets, test.wantAssets)
			}
			hasInlineStylePolicy := strings.Contains(response.Header().Get("Content-Security-Policy"), "style-src 'self' 'unsafe-inline'")
			if hasInlineStylePolicy != test.wantInlineStylePolicy {
				t.Fatalf("page permits generated inline styles = %t, want %t; policy: %q", hasInlineStylePolicy, test.wantInlineStylePolicy, response.Header().Get("Content-Security-Policy"))
			}
		})
	}
}

// saveTestAnnotation persists one selected-text annotation for API test setup.
func saveTestAnnotation(t *testing.T, store *annotationstore.Store, document, sourceText string) annotationstore.Revision {
	t.Helper()
	start := strings.Index(sourceText, "selected")
	if start < 0 {
		t.Fatal("test source does not contain selected text")
	}
	source, err := annotation.NewSource([]byte(sourceText), start, start+len("selected"))
	if err != nil {
		t.Fatalf("annotation.NewSource() error = %v", err)
	}
	now := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	sidecar := annotation.Sidecar{
		SchemaVersion: annotation.SchemaVersion,
		Document:      document,
		Annotations: []annotation.Annotation{
			{
				ID:        "ann_api_test",
				Intent:    annotation.IntentChangeRequest,
				Status:    annotation.StatusOpen,
				Comment:   "Update this selection.",
				Role:      "reviewer",
				CreatedAt: now,
				UpdatedAt: now,
				Source:    &source,
				Thread:    []annotation.ThreadEntry{},
			},
		},
	}
	revision, err := store.Save(sidecar, "")
	if err != nil {
		t.Fatalf("Store.Save() error = %v", err)
	}
	return revision
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
	for _, requestPath := range []string{"/", "/healthz", "/static/htmx.min.js", "/missing"} {
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
		if got := response.Header().Get("Content-Security-Policy"); got != baseContentSecurityPolicy {
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
