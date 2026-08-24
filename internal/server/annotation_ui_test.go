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
		{name: "active only", query: "document=README.md", wantStatus: http.StatusOK, contains: []string{`id="annotation-panel-content"`, "No active annotations."}},
		{name: "show inactive", query: "document=README.md&show_inactive=true", wantStatus: http.StatusOK, contains: []string{`id="annotation-ann_transition_test"`, `class="annotation-badge">closed`}},
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
		if response.Header().Get("Content-Type") != "text/html; charset=utf-8" || !strings.Contains(response.Body.String(), "Annotations changed; review the latest state before retrying.") {
			t.Fatalf("conflict did not return authoritative HTML panel: %s", response.Body.String())
		}
	})

	t.Run("preserves comment when selection revision changed", func(t *testing.T) {
		t.Parallel()
		viewer, store := newViewer(t)
		form := url.Values{
			"document": {"README.md"}, "intent": {"question"},
			"comment": {"Keep my draft."}, "role": {"reviewer"},
			"selection_start_byte": {"7"}, "selection_end_byte": {"15"},
			"document_sha256": {strings.Repeat("0", 64)},
		}
		request := httptest.NewRequest(http.MethodPost, "/ui/review/annotations", strings.NewReader(form.Encode()))
		request.Header.Set("Origin", origin)
		request.Header.Set(reviewTokenHeader, token)
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("If-Match", `""`)
		response := httptest.NewRecorder()
		viewer.Handler().ServeHTTP(response, request)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Original selection unavailable; reattach required.") || !strings.Contains(response.Body.String(), "Keep my draft.") {
			t.Fatalf("status = %d; body: %s", response.Code, response.Body.String())
		}
		stored, _, err := store.Load("README.md")
		if err != nil {
			t.Fatalf("Store.Load() error = %v", err)
		}
		if len(stored.Annotations) != 1 || !stored.Annotations[0].NeedsReattachment || stored.Annotations[0].Comment != "Keep my draft." {
			t.Fatalf("stored sidecar = %#v", stored)
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
			if test.wantStatus == http.StatusUnprocessableEntity && (response.Header().Get("Content-Type") != "text/html; charset=utf-8" || !strings.Contains(response.Body.String(), `id="annotation-panel-content"`)) {
				t.Errorf("validation did not return authoritative HTML panel: %s", response.Body.String())
			}
		})
	}
}

func TestReplyAnnotationUI(t *testing.T) {
	t.Parallel()

	const (
		origin = "http://127.0.0.1:8080"
		token  = "0123456789abcdef0123456789abcdef"
	)
	newViewer := func(t *testing.T) (*Server, *annotationstore.Store, annotationstore.Revision) {
		t.Helper()
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
		return viewer, store, revision
	}
	post := func(t *testing.T, viewer *Server, revision annotationstore.Revision, form url.Values) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(http.MethodPost, "/ui/review/annotations/ann_transition_test/replies", strings.NewReader(form.Encode()))
		request.Header.Set("Origin", origin)
		request.Header.Set(reviewTokenHeader, token)
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("If-Match", strconv.Quote(string(revision)))
		response := httptest.NewRecorder()
		viewer.Handler().ServeHTTP(response, request)
		return response
	}

	t.Run("appends reply and returns authoritative panel", func(t *testing.T) {
		t.Parallel()
		viewer, store, revision := newViewer(t)
		response := post(t, viewer, revision, url.Values{"document": {"README.md"}, "role": {"agent"}, "message": {"Checked <this>."}})
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Checked &lt;this&gt;.") {
			t.Fatalf("status = %d; body: %s", response.Code, response.Body.String())
		}
		stored, savedRevision, err := store.Load("README.md")
		if err != nil {
			t.Fatalf("Store.Load() error = %v", err)
		}
		if len(stored.Annotations[0].Thread) != 1 || stored.Annotations[0].Thread[0].Role != annotation.RoleAgent || response.Header().Get("ETag") != strconv.Quote(string(savedRevision)) {
			t.Fatalf("stored = %#v, ETag = %q", stored, response.Header().Get("ETag"))
		}
	})

	t.Run("validation returns HTML and preserves escaped draft", func(t *testing.T) {
		t.Parallel()
		viewer, _, revision := newViewer(t)
		response := post(t, viewer, revision, url.Values{"document": {"README.md"}, "role": {"owner"}, "message": {"Retry <script>alert(1)</script>"}})
		body := response.Body.String()
		if response.Code != http.StatusUnprocessableEntity || response.Header().Get("Content-Type") != "text/html; charset=utf-8" {
			t.Fatalf("status = %d, Content-Type = %q; body: %s", response.Code, response.Header().Get("Content-Type"), body)
		}
		for _, want := range []string{`id="annotation-panel-content"`, `annotation-panel-feedback validation`, `Retry &lt;script&gt;alert(1)&lt;/script&gt;`} {
			if !strings.Contains(body, want) {
				t.Errorf("body missing %q:\n%s", want, body)
			}
		}
		if strings.Contains(body, "<script>alert(1)</script>") {
			t.Errorf("body contains unescaped draft:\n%s", body)
		}
	})

	t.Run("conflict returns latest panel and preserves draft", func(t *testing.T) {
		t.Parallel()
		viewer, _, currentRevision := newViewer(t)
		response := post(t, viewer, "", url.Values{"document": {"README.md"}, "role": {"reviewer"}, "message": {"Retry this reply."}})
		body := response.Body.String()
		if response.Code != http.StatusConflict || response.Header().Get("ETag") != strconv.Quote(string(currentRevision)) {
			t.Fatalf("status = %d, ETag = %q; body: %s", response.Code, response.Header().Get("ETag"), body)
		}
		for _, want := range []string{"Annotations changed; review the latest state before retrying.", "Retry this reply."} {
			if !strings.Contains(body, want) {
				t.Errorf("body missing %q:\n%s", want, body)
			}
		}
	})
}

func TestTransitionAnnotationUI(t *testing.T) {
	t.Parallel()

	const (
		origin = "http://127.0.0.1:8080"
		token  = "0123456789abcdef0123456789abcdef"
	)
	newViewer := func(t *testing.T) (*Server, *annotationstore.Store, annotationstore.Revision) {
		t.Helper()
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
		revision := saveTransitionAnnotation(t, store, annotation.StatusApplied)
		viewer, err := New(root, mdrender.New(), WithReviewSession(store, origin, token))
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		return viewer, store, revision
	}
	post := func(t *testing.T, viewer *Server, revision annotationstore.Revision, form url.Values) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(http.MethodPost, "/ui/review/annotations/ann_transition_test/transition", strings.NewReader(form.Encode()))
		request.Header.Set("Origin", origin)
		request.Header.Set(reviewTokenHeader, token)
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("If-Match", strconv.Quote(string(revision)))
		response := httptest.NewRecorder()
		viewer.Handler().ServeHTTP(response, request)
		return response
	}

	t.Run("records lifecycle activity and returns authoritative panel", func(t *testing.T) {
		t.Parallel()
		viewer, store, revision := newViewer(t)
		response := post(t, viewer, revision, url.Values{
			"document": {"README.md"}, "status": {"needs_changes"}, "role": {"reviewer"},
			"activity": {"Keep the loopback default."},
		})
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "needs_changes") || !strings.Contains(response.Body.String(), "Keep the loopback default.") {
			t.Fatalf("status = %d; body: %s", response.Code, response.Body.String())
		}
		stored, _, err := store.Load("README.md")
		if err != nil {
			t.Fatalf("Store.Load() error = %v", err)
		}
		if stored.Annotations[0].Status != annotation.StatusNeedsChanges || len(stored.Annotations[0].Thread) != 2 {
			t.Fatalf("stored = %#v", stored)
		}
	})

	t.Run("validation returns HTML with attempted transition", func(t *testing.T) {
		t.Parallel()
		viewer, _, revision := newViewer(t)
		response := post(t, viewer, revision, url.Values{
			"document": {"README.md"}, "status": {"needs_changes"}, "role": {"reviewer"},
			"activity": {"   "}, "commit": {"draft123"},
		})
		body := response.Body.String()
		if response.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d; body: %s", response.Code, body)
		}
		for _, want := range []string{`value="needs_changes"`, `value="reviewer" selected`, `value="draft123"`, `annotation-panel-feedback validation`} {
			if !strings.Contains(body, want) {
				t.Errorf("body missing %q:\n%s", want, body)
			}
		}
	})

	t.Run("quick close succeeds", func(t *testing.T) {
		t.Parallel()
		viewer, store, revision := newViewer(t)
		response := post(t, viewer, revision, url.Values{"document": {"README.md"}, "status": {"closed"}, "role": {"reviewer"}})
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "No active annotations.") {
			t.Fatalf("status = %d; body: %s", response.Code, response.Body.String())
		}
		stored, _, err := store.Load("README.md")
		if err != nil {
			t.Fatalf("Store.Load() error = %v", err)
		}
		if stored.Annotations[0].Status != annotation.StatusClosed {
			t.Fatalf("stored status = %q", stored.Annotations[0].Status)
		}
	})
}

func TestReattachAnnotationUI(t *testing.T) {
	t.Parallel()

	const (
		origin          = "http://127.0.0.1:8080"
		token           = "0123456789abcdef0123456789abcdef"
		currentDocument = "Before new selection after"
	)
	digest := annotation.DocumentSHA256([]byte(currentDocument))
	tests := []struct {
		name          string
		sourceMode    string
		endByte       string
		digest        string
		staleRevision bool
		wantStatus    int
		wantText      string
		preserveDraft bool
		wantSuccess   bool
	}{
		{name: "reattaches stale selection", sourceMode: "stale", endByte: "20", digest: digest, wantStatus: http.StatusOK, wantText: "new selection", wantSuccess: true},
		{name: "reattaches selection lost during creation", sourceMode: "pending", endByte: "20", digest: digest, wantStatus: http.StatusOK, wantText: "new selection", wantSuccess: true},
		{name: "invalid range returns draft", sourceMode: "stale", endByte: "200", digest: digest, wantStatus: http.StatusUnprocessableEntity, wantText: "annotation source byte range is invalid", preserveDraft: true},
		{name: "invalid hidden integer is escaped", sourceMode: "stale", endByte: `<bad>`, digest: digest, wantStatus: http.StatusUnprocessableEntity, wantText: "selection end byte must be an integer", preserveDraft: true},
		{name: "document change clears stale selection", sourceMode: "stale", endByte: "20", digest: strings.Repeat("0", 64), wantStatus: http.StatusConflict, wantText: "document changed; refresh and select again"},
		{name: "resolved anchor reports semantic conflict", sourceMode: "exact", endByte: "20", digest: digest, wantStatus: http.StatusConflict, wantText: "annotation anchor is not stale"},
		{name: "revision conflict retains verified selection", sourceMode: "stale", endByte: "20", digest: digest, staleRevision: true, wantStatus: http.StatusConflict, wantText: "Annotations changed; review the latest state before retrying.", preserveDraft: true},
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
			form := url.Values{
				"document": {"README.md"}, "selection_start_byte": {"7"},
				"selection_end_byte": {test.endByte}, "document_sha256": {test.digest},
			}
			request := httptest.NewRequest(http.MethodPost, "/ui/review/annotations/ann_reattach_test/reattach", strings.NewReader(form.Encode()))
			request.Header.Set("Origin", origin)
			request.Header.Set(reviewTokenHeader, token)
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			revision := currentRevision
			if test.staleRevision {
				revision = ""
			}
			request.Header.Set("If-Match", strconv.Quote(string(revision)))
			response := httptest.NewRecorder()
			viewer.Handler().ServeHTTP(response, request)
			body := response.Body.String()
			if response.Code != test.wantStatus || !strings.Contains(body, test.wantText) {
				t.Fatalf("status = %d, want %d and %q; body: %s", response.Code, test.wantStatus, test.wantText, body)
			}
			if response.Header().Get("Content-Type") != "text/html; charset=utf-8" {
				t.Errorf("Content-Type = %q", response.Header().Get("Content-Type"))
			}
			if test.wantSuccess {
				stored, savedRevision, err := store.Load("README.md")
				if err != nil {
					t.Fatalf("Store.Load() error = %v", err)
				}
				if stored.Annotations[0].Source == nil || stored.Annotations[0].Source.Selector.Exact != "new selection" || stored.Annotations[0].NeedsReattachment || response.Header().Get("ETag") != strconv.Quote(string(savedRevision)) {
					t.Fatalf("stored = %#v, ETag = %q", stored, response.Header().Get("ETag"))
				}
				return
			}
			if response.Header().Get("ETag") != strconv.Quote(string(currentRevision)) {
				t.Errorf("ETag = %q, want %q", response.Header().Get("ETag"), strconv.Quote(string(currentRevision)))
			}
			if test.sourceMode == "exact" {
				return
			}
			wantEndValue := `name="selection_end_byte" value=""`
			if test.preserveDraft {
				wantEndValue = `name="selection_end_byte" value="` + test.endByte + `"`
				wantEndValue = strings.ReplaceAll(wantEndValue, "<", "&lt;")
				wantEndValue = strings.ReplaceAll(wantEndValue, ">", "&gt;")
			}
			if !strings.Contains(body, wantEndValue) {
				t.Errorf("body missing draft field %q:\n%s", wantEndValue, body)
			}
			if strings.Contains(body, `<bad>`) {
				t.Errorf("body contains unescaped hidden value:\n%s", body)
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
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/ui/review/annotations?document=README.md"},
		{http.MethodPost, "/ui/review/annotations"},
		{http.MethodPost, "/ui/review/annotations/ann_test/replies"},
		{http.MethodPost, "/ui/review/annotations/ann_test/transition"},
		{http.MethodPost, "/ui/review/annotations/ann_test/reattach"},
	} {
		request := httptest.NewRequest(route.method, route.path, nil)
		response := httptest.NewRecorder()
		viewer.Handler().ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Errorf("%s %s status = %d, want 404", route.method, route.path, response.Code)
		}
	}
}
