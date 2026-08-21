package server

import (
	"encoding/json"
	"io"
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

func TestCreateAnnotationAPI(t *testing.T) {
	t.Parallel()

	const (
		origin           = "http://127.0.0.1:8080"
		token            = "0123456789abcdef0123456789abcdef"
		selectedDocument = "Before **selected** after"
	)
	digest := annotation.DocumentSHA256([]byte(selectedDocument))
	selectedBody := `{"document":"README.md","intent":"change_request","comment":"Update this.","author":"reviewer","selection":{"startByte":9,"endByte":17,"documentSHA256":"` + digest + `"}}`
	crossTagBody := `{"document":"README.md","intent":"change_request","comment":"Update this.","author":"reviewer","selection":{"startByte":0,"endByte":19,"documentSHA256":"` + digest + `"}}`
	documentBody := `{"document":"README.md","intent":"question","comment":"Why this document?","author":"reviewer"}`
	tests := []struct {
		name         string
		body         string
		ifMatch      *string
		seedSidecar  bool
		omitToken    bool
		wantStatus   int
		wantExact    string
		wantConflict bool
	}{
		{name: "selected text", body: selectedBody, ifMatch: stringPointer(`""`), wantStatus: http.StatusCreated, wantExact: "selected"},
		{name: "selection across formatting", body: crossTagBody, ifMatch: stringPointer(`""`), wantStatus: http.StatusCreated, wantExact: "Before **selected**"},
		{name: "document level", body: documentBody, ifMatch: stringPointer(`""`), wantStatus: http.StatusCreated},
		{name: "missing review token", body: documentBody, ifMatch: stringPointer(`""`), omitToken: true, wantStatus: http.StatusForbidden},
		{name: "missing revision", body: documentBody, wantStatus: http.StatusPreconditionRequired},
		{name: "malformed revision", body: documentBody, ifMatch: stringPointer("unquoted"), wantStatus: http.StatusBadRequest},
		{name: "stale revision", body: documentBody, ifMatch: stringPointer(`""`), seedSidecar: true, wantStatus: http.StatusConflict, wantConflict: true},
		{name: "stale document digest", body: strings.Replace(selectedBody, digest, strings.Repeat("0", 64), 1), ifMatch: stringPointer(`""`), wantStatus: http.StatusConflict},
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
			if test.wantExact != "" {
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
			if len(stored.Annotations) != 1 || string(revision) != payload.Revision {
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
	validBody := `{"document":"README.md","message":"Please also update the example.","author":"reviewer"}`
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
		{name: "empty author", annotationID: "ann_api_test", body: strings.Replace(validBody, "reviewer", "", 1), useCurrent: true, wantStatus: http.StatusBadRequest},
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
		{name: "acknowledge open", from: annotation.StatusOpen, input: transitionAnnotationRequest{Status: annotation.StatusAcknowledged, ActorRole: annotation.RoleAgent, Author: "agent"}, wantKinds: []annotation.ThreadKind{annotation.ThreadAcknowledgement, annotation.ThreadStatusChange}},
		{name: "acknowledge retry", from: annotation.StatusNeedsChanges, input: transitionAnnotationRequest{Status: annotation.StatusAcknowledged, ActorRole: annotation.RoleAgent, Author: "agent"}, wantKinds: []annotation.ThreadKind{annotation.ThreadAcknowledgement, annotation.ThreadStatusChange}},
		{name: "report applied", from: annotation.StatusAcknowledged, input: transitionAnnotationRequest{Status: annotation.StatusApplied, ActorRole: annotation.RoleAgent, Author: "agent", Summary: "Implemented", Commit: "abc1234"}, wantKinds: []annotation.ThreadKind{annotation.ThreadResolution, annotation.ThreadStatusChange}},
		{name: "request changes", from: annotation.StatusApplied, input: transitionAnnotationRequest{Status: annotation.StatusNeedsChanges, ActorRole: annotation.RoleReviewer, Author: "reviewer", Message: "Keep the default."}, wantKinds: []annotation.ThreadKind{annotation.ThreadReview, annotation.ThreadStatusChange}},
		{name: "reject request", from: annotation.StatusOpen, input: transitionAnnotationRequest{Status: annotation.StatusRejected, ActorRole: annotation.RoleAgent, Author: "agent", Message: "Conflicts with policy."}, wantKinds: []annotation.ThreadKind{annotation.ThreadReply, annotation.ThreadStatusChange}},
		{name: "close applied", from: annotation.StatusApplied, input: transitionAnnotationRequest{Status: annotation.StatusClosed, ActorRole: annotation.RoleReviewer, Author: "reviewer"}, wantKinds: []annotation.ThreadKind{annotation.ThreadStatusChange}},
		{name: "reopen closed", from: annotation.StatusClosed, input: transitionAnnotationRequest{Status: annotation.StatusOpen, ActorRole: annotation.RoleReviewer, Author: "reviewer"}, wantKinds: []annotation.ThreadKind{annotation.ThreadStatusChange}},
		{name: "missing resolution summary", from: annotation.StatusAcknowledged, input: transitionAnnotationRequest{Status: annotation.StatusApplied, ActorRole: annotation.RoleAgent, Author: "agent"}, wantErr: "summary"},
		{name: "missing review message", from: annotation.StatusApplied, input: transitionAnnotationRequest{Status: annotation.StatusNeedsChanges, ActorRole: annotation.RoleReviewer, Author: "reviewer"}, wantErr: "message"},
		{name: "missing rejection reason", from: annotation.StatusOpen, input: transitionAnnotationRequest{Status: annotation.StatusRejected, ActorRole: annotation.RoleAgent, Author: "agent"}, wantErr: "message"},
		{name: "metadata on acknowledgement", from: annotation.StatusOpen, input: transitionAnnotationRequest{Status: annotation.StatusAcknowledged, ActorRole: annotation.RoleAgent, Author: "agent", Message: "unexpected"}, wantErr: "does not accept"},
		{name: "blank author", from: annotation.StatusOpen, input: transitionAnnotationRequest{Status: annotation.StatusAcknowledged, ActorRole: annotation.RoleAgent, Author: " "}, wantErr: "author"},
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
			if statusChange.FromStatus != test.from || statusChange.ToStatus != test.input.Status || statusChange.ActorRole != test.input.ActorRole {
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
	validBody := `{"document":"README.md","status":"needs_changes","actorRole":"reviewer","author":"reviewer","message":"Keep the loopback default."}`
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
			if stored.Annotations[0].Source.Selector.Exact != "new selection" || string(revision) != payload.Revision {
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
	default:
		t.Fatalf("unsupported source mode %q", sourceMode)
	}
	createdAt := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	sidecar := annotation.Sidecar{
		SchemaVersion: annotation.SchemaVersion,
		Document:      "README.md",
		Annotations: []annotation.Annotation{
			{
				ID:        "ann_reattach_test",
				Intent:    annotation.IntentChangeRequest,
				Status:    annotation.StatusOpen,
				Comment:   "Update this selection.",
				Author:    "reviewer",
				CreatedAt: createdAt,
				UpdatedAt: createdAt,
				Source:    source,
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
				Author:    "reviewer",
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
	}{
		{name: "read-only page omits token"},
		{name: "review page embeds controls", review: true, wantToken: true, wantPanel: true, wantSource: true, wantDigest: true},
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
			hasToken := strings.Contains(response.Body.String(), `name="md-viewer-review-token" content="`+token+`"`)
			if hasToken != test.wantToken {
				t.Fatalf("page contains review token = %t, want %t", hasToken, test.wantToken)
			}
			hasPanel := strings.Contains(response.Body.String(), `class="review-panel"`) && strings.Contains(response.Body.String(), `class="annotation-form"`) && strings.Contains(response.Body.String(), `class="show-inactive-annotations"`) && strings.Contains(response.Body.String(), `src="/static/review.js"`)
			if hasPanel != test.wantPanel {
				t.Fatalf("page contains review panel = %t, want %t", hasPanel, test.wantPanel)
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

func TestReviewScript(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		method       string
		wantStatus   int
		wantType     string
		wantContents []string
	}{
		{name: "get embedded script", method: http.MethodGet, wantStatus: http.StatusOK, wantType: "text/javascript; charset=utf-8", wantContents: []string{"submitAnnotation", "submitLifecycle"}},
		{name: "reject post", method: http.MethodPost, wantStatus: http.StatusMethodNotAllowed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			handler := newTestHandler(t, nil)
			request := httptest.NewRequest(test.method, "/static/review.js", nil)
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
					t.Errorf("script does not contain %q", wantContent)
				}
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
				Author:    "reviewer",
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
