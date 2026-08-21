package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"atulm/md-viewer/internal/content"
	"atulm/md-viewer/internal/gitdiff"
	mdrender "atulm/md-viewer/internal/render"
)

const (
	testComparisonOrigin = "http://127.0.0.1:9000"
	testComparisonToken  = "comparison-control-token-abcdefghijklmnop"
)

// newComparisonAPIServer builds a viewer whose content root is a two-commit
// worktree with the moving base frozen at the tip and comparison control
// enabled. It returns the server plus the initial and tip commit IDs.
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
	options, err := configuration.RecentCommits(context.Background())
	if err != nil {
		t.Fatalf("RecentCommits() error = %v", err)
	}
	root, err := content.Open(repository)
	if err != nil {
		t.Fatalf("content.Open() error = %v", err)
	}
	indexOptions, err := content.NewIndexOptions([]string{".go"}, nil)
	if err != nil {
		t.Fatalf("content.NewIndexOptions() error = %v", err)
	}
	viewer, err := New(root, mdrender.New(), WithIndexOptions(indexOptions), WithGitComparison(configuration, options, origin, token))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return viewer, initial, tip
}

func getComparisonState(t *testing.T, handler http.Handler) (comparisonStateView, string) {
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
	return view, response.Header().Get("ETag")
}

func postComparison(t *testing.T, handler http.Handler, body, ifMatch string, decorate func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/git-comparison", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", testComparisonOrigin)
	request.Header.Set(comparisonTokenHeader, testComparisonToken)
	if ifMatch != "" {
		request.Header.Set("If-Match", strconv.Quote(ifMatch))
	}
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
	view, etag := getComparisonState(t, viewer.Handler())

	if view.ActiveCommit != tip || view.Explicit {
		t.Fatalf("state active = %s explicit = %t, want moving tip %s", view.ActiveCommit, view.Explicit, tip)
	}
	if etag != strconv.Quote(view.Revision) {
		t.Fatalf("ETag = %s, want %s", etag, strconv.Quote(view.Revision))
	}
	if !hasOption(view.Options, initial) || !hasOption(view.Options, tip) {
		t.Fatalf("options missing initial or tip: %#v", view.Options)
	}
	configured := 0
	for _, option := range view.Options {
		if option.Configured {
			configured++
			if option.Commit != tip || option.Name != "HEAD" {
				t.Errorf("configured option = %#v, want tip %s named HEAD", option, tip)
			}
		}
	}
	if configured != 1 {
		t.Fatalf("configured option count = %d, want 1", configured)
	}
}

func TestComparisonRefreshAdoptsNewTip(t *testing.T) {
	t.Parallel()
	viewer, _, _ := newComparisonAPIServer(t, testComparisonOrigin, testComparisonToken)
	repository := viewer.comparison.configured.RepositoryRoot
	writeTestFile(t, repository+"/main.go", "package refreshed\n")
	runServerTestGit(t, repository, "add", "main.go")
	runServerTestGit(t, repository, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "third")
	newTip := revParse(t, repository, "HEAD")

	before, _ := getComparisonState(t, viewer.Handler())
	response := postComparison(t, viewer.Handler(), `{"action":"refresh"}`, before.Revision, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("refresh status = %d, want 200; body: %s", response.Code, response.Body.String())
	}
	var view comparisonStateView
	if err := json.Unmarshal(response.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode error = %v", err)
	}
	if view.ActiveCommit != newTip || view.Explicit {
		t.Fatalf("refresh active = %s explicit = %t, want moving tip %s", view.ActiveCommit, view.Explicit, newTip)
	}
	if view.Revision == before.Revision {
		t.Fatal("refresh reused the previous revision")
	}
}

func TestComparisonSelectPinsCommit(t *testing.T) {
	t.Parallel()
	viewer, initial, _ := newComparisonAPIServer(t, testComparisonOrigin, testComparisonToken)
	before, _ := getComparisonState(t, viewer.Handler())

	response := postComparison(t, viewer.Handler(), `{"action":"select","commit":"`+initial+`"}`, before.Revision, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("select status = %d, want 200; body: %s", response.Code, response.Body.String())
	}
	var view comparisonStateView
	if err := json.Unmarshal(response.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode error = %v", err)
	}
	if view.ActiveCommit != initial || !view.Explicit {
		t.Fatalf("select active = %s explicit = %t, want pinned %s", view.ActiveCommit, view.Explicit, initial)
	}
}

func TestComparisonMutationRejections(t *testing.T) {
	t.Parallel()
	viewer, initial, _ := newComparisonAPIServer(t, testComparisonOrigin, testComparisonToken)
	handler := viewer.Handler()
	state, _ := getComparisonState(t, handler)

	tests := []struct {
		name     string
		body     string
		ifMatch  string
		decorate func(*http.Request)
		want     int
	}{
		{name: "wrong origin", body: `{"action":"refresh"}`, ifMatch: state.Revision, want: http.StatusForbidden, decorate: func(r *http.Request) { r.Header.Set("Origin", "http://127.0.0.1:1") }},
		{name: "wrong token", body: `{"action":"refresh"}`, ifMatch: state.Revision, want: http.StatusForbidden, decorate: func(r *http.Request) { r.Header.Set(comparisonTokenHeader, "wrong") }},
		{name: "wrong content type", body: `{"action":"refresh"}`, ifMatch: state.Revision, want: http.StatusUnsupportedMediaType, decorate: func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }},
		{name: "missing if-match", body: `{"action":"refresh"}`, want: http.StatusPreconditionRequired},
		{name: "stale if-match", body: `{"action":"refresh"}`, ifMatch: "0000", want: http.StatusConflict},
		{name: "unknown action", body: `{"action":"rebase"}`, ifMatch: state.Revision, want: http.StatusBadRequest},
		{name: "unknown field", body: `{"action":"refresh","extra":1}`, ifMatch: state.Revision, want: http.StatusBadRequest},
		{name: "unknown commit", body: `{"action":"select","commit":"` + strings.Repeat("f", 40) + `"}`, ifMatch: state.Revision, want: http.StatusBadRequest},
		{name: "malformed commit", body: `{"action":"select","commit":"` + initial[:8] + `"}`, ifMatch: state.Revision, want: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			response := postComparison(t, handler, test.body, test.ifMatch, test.decorate)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d; body: %s", response.Code, test.want, response.Body.String())
			}
			if test.want == http.StatusConflict {
				var view comparisonStateView
				if err := json.Unmarshal(response.Body.Bytes(), &view); err != nil || view.Revision == "" {
					t.Fatalf("conflict body missing current state: %s", response.Body.String())
				}
			}
		})
	}
}

func TestComparisonReadOnlyWithoutControl(t *testing.T) {
	t.Parallel()
	viewer, _, _ := newComparisonAPIServer(t, "", "")
	handler := viewer.Handler()

	if _, etag := getComparisonState(t, handler); etag == "" {
		t.Fatal("read-only GET should still return an ETag")
	}
	// With no control token the mutation route is never registered, so the
	// shared GET pattern answers POST with 405 rather than exposing a handler.
	response := postComparison(t, handler, `{"action":"refresh"}`, "any", nil)
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("mutation without control = %d, want 405", response.Code)
	}
}

func hasOption(options []comparisonOptionView, commit string) bool {
	for _, option := range options {
		if option.Commit == commit {
			return true
		}
	}
	return false
}
