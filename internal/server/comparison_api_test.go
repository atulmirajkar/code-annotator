package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"atulm/code-annotator/internal/content"
	"atulm/code-annotator/internal/gitdiff"
	mdrender "atulm/code-annotator/internal/render"
)

const (
	testComparisonOrigin = "http://127.0.0.1:9000"
	testComparisonToken  = "comparison-control-token-abcdefghijklmnop"
)

// newComparisonAPIServer builds a viewer whose content root is a two-commit
// worktree with the base pinned at the tip and comparison control enabled. It
// returns the server plus the initial and tip commit IDs.
func newComparisonAPIServer(t *testing.T, origin, token string) (*Server, string, string) {
	t.Helper()
	requireComparisonGit(t)
	repository := t.TempDir()
	runServerTestGit(t, repository, "init", "-b", "main")
	writeTestFile(t, repository+"/main.go", "package main\n")
	runServerTestGit(t, repository, "add", "main.go")
	runServerTestGit(t, repository, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "initial")
	initial := revParse(t, repository, "HEAD")
	writeTestFile(t, repository+"/main.go", "package changed\n")
	runServerTestGit(t, repository, "add", "main.go")
	runServerTestGit(t, repository, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "second")
	tip := revParse(t, repository, "HEAD")

	configuration, err := gitdiff.Open(context.Background(), repository, "HEAD")
	if err != nil {
		t.Fatalf("gitdiff.Open() error = %v", err)
	}
	root, err := content.Open(repository)
	if err != nil {
		t.Fatalf("content.Open() error = %v", err)
	}
	indexOptions, err := content.NewIndexOptions([]string{".go"}, nil)
	if err != nil {
		t.Fatalf("content.NewIndexOptions() error = %v", err)
	}
	viewer, err := New(root, mdrender.New(), WithIndexOptions(indexOptions), WithGitComparison(configuration, origin, token))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return viewer, initial, tip
}

func getComparisonState(t *testing.T, handler http.Handler) comparisonStateView {
	t.Helper()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/git-comparison", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200; body: %s", response.Code, response.Body.String())
	}
	var view comparisonStateView
	if err := json.Unmarshal(response.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode state error = %v; body: %s", err, response.Body.String())
	}
	return view
}

func postComparison(t *testing.T, handler http.Handler, body string, decorate func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/git-comparison", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", testComparisonOrigin)
	request.Header.Set(comparisonTokenHeader, testComparisonToken)
	if decorate != nil {
		decorate(request)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestComparisonStateReportsOptions(t *testing.T) {
	t.Parallel()
	viewer, initial, tip := newComparisonAPIServer(t, testComparisonOrigin, testComparisonToken)
	view := getComparisonState(t, viewer.Handler())

	if view.ActiveCommit != tip {
		t.Fatalf("state active = %s, want tip %s", view.ActiveCommit, tip)
	}
	commits := map[string]bool{}
	for _, option := range view.Options {
		commits[option.Commit] = true
	}
	if !commits[tip] || !commits[initial] {
		t.Fatalf("options missing initial or tip: %#v", view.Options)
	}
}

func TestComparisonSelectEndpointPinsCommit(t *testing.T) {
	t.Parallel()
	viewer, initial, _ := newComparisonAPIServer(t, testComparisonOrigin, testComparisonToken)

	response := postComparison(t, viewer.Handler(), `{"commit":"`+initial+`"}`, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("select status = %d, want 200; body: %s", response.Code, response.Body.String())
	}
	var view comparisonStateView
	if err := json.Unmarshal(response.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode error = %v", err)
	}
	if view.ActiveCommit != initial {
		t.Fatalf("select active = %s, want pinned %s", view.ActiveCommit, initial)
	}
	// The change is server-wide: a fresh read reports the pinned base.
	if getComparisonState(t, viewer.Handler()).ActiveCommit != initial {
		t.Fatalf("state after select did not persist the pinned base")
	}
}

func TestComparisonSelectFormPinsCommitAndRefreshesPage(t *testing.T) {
	t.Parallel()
	viewer, initial, _ := newComparisonAPIServer(t, testComparisonOrigin, testComparisonToken)
	request := httptest.NewRequest(http.MethodPost, "/ui/review/git-comparison", strings.NewReader("commit="+initial))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", testComparisonOrigin)
	request.Header.Set(comparisonTokenHeader, testComparisonToken)
	response := httptest.NewRecorder()
	viewer.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusNoContent || response.Header().Get("HX-Refresh") != "true" {
		t.Fatalf("form response = %d, HX-Refresh %q; body: %s", response.Code, response.Header().Get("HX-Refresh"), response.Body.String())
	}
	if got := getComparisonState(t, viewer.Handler()).ActiveCommit; got != initial {
		t.Fatalf("active commit = %s, want %s", got, initial)
	}
}

func TestComparisonControlIsServerRenderedWithoutCustomData(t *testing.T) {
	t.Parallel()
	viewer, initial, tip := newComparisonAPIServer(t, testComparisonOrigin, testComparisonToken)
	response := getResponse(t, viewer.Handler(), "/view/main.go?mode=diff")
	body := response.Body.String()
	for _, expected := range []string{`action="/ui/review/git-comparison"`, `name="commit"`, `value="` + initial + `"`, `value="` + tip + `"`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("comparison control missing %q:\n%s", expected, body)
		}
	}
	if strings.Contains(body, "data-active-commit") {
		t.Fatal("comparison state leaked into a custom data attribute")
	}
}

func TestComparisonSelectRejections(t *testing.T) {
	t.Parallel()
	viewer, initial, _ := newComparisonAPIServer(t, testComparisonOrigin, testComparisonToken)
	handler := viewer.Handler()

	tests := []struct {
		name     string
		body     string
		decorate func(*http.Request)
		want     int
	}{
		{name: "wrong origin", body: `{"commit":"` + initial + `"}`, want: http.StatusForbidden, decorate: func(r *http.Request) { r.Header.Set("Origin", "http://127.0.0.1:1") }},
		{name: "wrong token", body: `{"commit":"` + initial + `"}`, want: http.StatusForbidden, decorate: func(r *http.Request) { r.Header.Set(comparisonTokenHeader, "wrong") }},
		{name: "wrong content type", body: `{"commit":"` + initial + `"}`, want: http.StatusUnsupportedMediaType, decorate: func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }},
		{name: "unknown field", body: `{"commit":"` + initial + `","extra":1}`, want: http.StatusBadRequest},
		{name: "unknown commit", body: `{"commit":"` + strings.Repeat("f", 40) + `"}`, want: http.StatusBadRequest},
		{name: "malformed commit", body: `{"commit":"` + initial[:8] + `"}`, want: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			response := postComparison(t, handler, test.body, test.decorate)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d; body: %s", response.Code, test.want, response.Body.String())
			}
		})
	}
}

func TestComparisonReadOnlyWithoutControl(t *testing.T) {
	t.Parallel()
	viewer, initial, _ := newComparisonAPIServer(t, "", "")
	handler := viewer.Handler()

	if getComparisonState(t, handler).ActiveCommit == "" {
		t.Fatal("read-only GET should still report the active base")
	}
	// With no control token the selection route is never registered, so the
	// shared GET pattern answers POST with 405 rather than exposing a handler.
	response := postComparison(t, handler, `{"commit":"`+initial+`"}`, nil)
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("selection without control = %d, want 405", response.Code)
	}
}
