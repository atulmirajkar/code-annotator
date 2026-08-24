// Package server exposes reviewable local content through an HTTP viewer.
package server

import (
	"bytes"
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"atulm/code-annotator/internal/annotation"
	annotationstore "atulm/code-annotator/internal/annotation/store"
	"atulm/code-annotator/internal/content"
	"atulm/code-annotator/internal/gitdiff"
	mdrender "atulm/code-annotator/internal/render"
	"atulm/code-annotator/web"
)

const (
	maxDocumentBytes           int64 = 4 << 20
	maxAnnotationMutationBytes int64 = 64 << 10
	reviewTokenHeader                = "X-Code-Annotator-Token"
)

const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 15 * time.Second
	writeTimeout      = 30 * time.Second
	idleTimeout       = 60 * time.Second
)

const (
	baseContentSecurityPolicy    = "default-src 'none'; img-src 'self' data: http: https:; style-src 'self'; script-src 'self'; font-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"
	mermaidContentSecurityPolicy = "default-src 'none'; img-src 'self' data: http: https:; style-src 'self' 'unsafe-inline'; script-src 'self'; font-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"
)

// Server serves an index and rendered documents from a content root.
type Server struct {
	root            *content.Root
	indexOptions    content.IndexOptions
	renderer        *mdrender.Renderer
	annotations     *annotationstore.Store
	review          *reviewSession
	comparison      *comparisonController
	page            *template.Template
	styles          []byte
	reviewJS        []byte
	reviewActionsJS []byte
	reviewAPIJS     []byte
	reviewDOMJS     []byte
	reviewHLJS      []byte
	reviewNavJS     []byte
	reviewPanelJS   []byte
	reviewRenderJS  []byte
	reviewSelectJS  []byte
	reviewThreadJS  []byte
	documentTreeJS  []byte
	viewerJS        []byte
	htmxJS          []byte
	mermaidJS       []byte
	mermaidTiny     []byte
	handler         http.Handler
}

// reviewSession contains the browser-bound authority required for annotation
// mutations. The token is intentionally never exposed through logging APIs.
type reviewSession struct {
	origin string
	token  string
}

// Option configures an optional Server capability.
type Option func(*Server) error

// WithAnnotationStore enables read access to annotations. Write routes remain
// unavailable until the review-session security boundary is configured.
func WithAnnotationStore(store *annotationstore.Store) Option {
	return func(server *Server) error {
		if store == nil {
			return errors.New("configure annotation API: nil store")
		}
		server.annotations = store
		return nil
	}
}

// WithReviewSession enables annotation reads and configures the exact loopback
// origin and secret header value required by future mutation routes.
func WithReviewSession(store *annotationstore.Store, origin, token string) Option {
	return func(server *Server) error {
		if store == nil {
			return errors.New("configure review session: nil annotation store")
		}
		normalizedOrigin, err := validateReviewOrigin(origin)
		if err != nil {
			return err
		}
		if len(token) < 32 {
			return errors.New("configure review session: token must contain at least 32 characters")
		}
		server.annotations = store
		server.review = &reviewSession{origin: normalizedOrigin, token: token}
		return nil
	}
}

type pageData struct {
	Root            string
	Selected        string
	Documents       []documentView
	Content         template.HTML
	Empty           bool
	ReviewToken     string
	ComparisonToken string
	DocumentSHA256  string
	HasMermaid      bool
	IsCode          bool
	DiffBase        string
	DiffCommit      string
	DiffCommitShort string
	ChangedReady    bool
	ChangedError    bool
	DiffMode        bool
	DiffAvailable   bool
	FileURL         string
	ChangesURL      string
}

type documentView struct {
	Path      string
	Name      string
	Directory string
	URL       string
	Selected  bool
	Kind      string
	Changed   bool
}

// WithIndexOptions configures the reviewable content catalog.
func WithIndexOptions(options content.IndexOptions) Option {
	return func(server *Server) error {
		server.indexOptions = options
		return nil
	}
}

// WithGitComparison exposes one startup-resolved comparison base that the
// browser may re-pin to another local commit. A non-empty loopback origin and
// control token enable authenticated selection mutations; both empty leaves the
// comparison read-only.
func WithGitComparison(configuration gitdiff.Config, origin, token string) Option {
	return func(server *Server) error {
		normalizedOrigin := ""
		if origin != "" || token != "" {
			validated, err := validateLoopbackOrigin(origin)
			if err != nil {
				return fmt.Errorf("configure Git comparison control: %w", err)
			}
			if len(token) < 32 {
				return errors.New("configure Git comparison control: token must contain at least 32 characters")
			}
			normalizedOrigin = validated
		}
		controller, err := newComparisonController(configuration, normalizedOrigin, token)
		if err != nil {
			return err
		}
		server.comparison = controller
		return nil
	}
}

// activeComparison captures the current comparison base for the lifetime of a
// single request, so changed-path and file-diff work share a base that a
// concurrent selection cannot alter mid-request.
func (s *Server) activeComparison() *gitdiff.Config {
	if s.comparison == nil {
		return nil
	}
	active := s.comparison.active()
	return &active
}

// validateReviewOrigin accepts only an HTTP origin with a loopback IP host and
// no path, query, user information, or fragment.
func validateReviewOrigin(origin string) (string, error) {
	normalized, err := validateLoopbackOrigin(origin)
	if err != nil {
		return "", fmt.Errorf("configure review session: %w", err)
	}
	return normalized, nil
}

// validateLoopbackOrigin normalizes an HTTP origin to scheme://host and rejects
// anything that is not a bare loopback-IP origin, so browser mutation checks
// can compare the Origin header by exact string equality.
func validateLoopbackOrigin(origin string) (string, error) {
	parsed, err := url.Parse(origin)
	if err != nil {
		return "", errors.New("invalid origin")
	}
	if parsed.Scheme != "http" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", errors.New("invalid origin")
	}
	address := net.ParseIP(parsed.Hostname())
	if address == nil || !address.IsLoopback() {
		return "", errors.New("origin must use a loopback IP address")
	}
	return parsed.Scheme + "://" + parsed.Host, nil
}

// protectReviewMutation enforces the browser session boundary and JSON media
// type used by the stable automation API.
func (s *Server) protectReviewMutation(next http.Handler) http.Handler {
	return s.protectReviewMutationMediaType(next, "application/json")
}

func (s *Server) protectReviewFormMutation(next http.Handler) http.Handler {
	return s.protectReviewMutationMediaType(next, "application/x-www-form-urlencoded")
}

// protectReviewMutationMediaType centralizes origin, token, media-type, and
// body-size enforcement for JSON and inactive form mutation routes.
func (s *Server) protectReviewMutationMediaType(next http.Handler, requiredMediaType string) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if s.review == nil {
			http.NotFound(response, request)
			return
		}
		if request.Header.Get("Origin") != s.review.origin {
			http.Error(response, "forbidden review origin", http.StatusForbidden)
			return
		}
		providedToken := request.Header.Get(reviewTokenHeader)
		if subtle.ConstantTimeCompare([]byte(providedToken), []byte(s.review.token)) != 1 {
			http.Error(response, "invalid review token", http.StatusForbidden)
			return
		}
		mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
		if err != nil {
			http.Error(response, "Content-Type must be "+requiredMediaType, http.StatusUnsupportedMediaType)
			return
		}
		if mediaType != requiredMediaType {
			http.Error(response, "Content-Type must be "+requiredMediaType, http.StatusUnsupportedMediaType)
			return
		}
		request.Body = http.MaxBytesReader(response, request.Body, maxAnnotationMutationBytes)
		next.ServeHTTP(response, request)
	})
}

// New creates the Markdown viewer handler with optional capabilities.
func New(root *content.Root, renderer *mdrender.Renderer, options ...Option) (*Server, error) {
	if root == nil {
		return nil, errors.New("create server: nil content root")
	}
	if renderer == nil {
		return nil, errors.New("create server: nil renderer")
	}

	page, err := parseViewerTemplates()
	if err != nil {
		return nil, fmt.Errorf("parse viewer templates: %w", err)
	}
	styles, err := fs.ReadFile(web.Files, "generated/styles.css")
	if err != nil {
		return nil, fmt.Errorf("read viewer styles: %w", err)
	}
	reviewJS, err := fs.ReadFile(web.Files, "generated/review.js")
	if err != nil {
		return nil, fmt.Errorf("read review script: %w", err)
	}
	reviewActionsJS, err := fs.ReadFile(web.Files, "generated/review-actions.js")
	if err != nil {
		return nil, fmt.Errorf("read review actions script: %w", err)
	}
	reviewAPIJS, err := fs.ReadFile(web.Files, "generated/review-api.js")
	if err != nil {
		return nil, fmt.Errorf("read review API script: %w", err)
	}
	reviewDOMJS, err := fs.ReadFile(web.Files, "generated/review-dom.js")
	if err != nil {
		return nil, fmt.Errorf("read review DOM script: %w", err)
	}
	reviewHLJS, err := fs.ReadFile(web.Files, "generated/review-highlights.js")
	if err != nil {
		return nil, fmt.Errorf("read review highlights script: %w", err)
	}
	reviewNavJS, err := fs.ReadFile(web.Files, "generated/review-navigation.js")
	if err != nil {
		return nil, fmt.Errorf("read review navigation script: %w", err)
	}
	reviewPanelJS, err := fs.ReadFile(web.Files, "generated/review-panel.js")
	if err != nil {
		return nil, fmt.Errorf("read review panel script: %w", err)
	}
	reviewRenderJS, err := fs.ReadFile(web.Files, "generated/review-render.js")
	if err != nil {
		return nil, fmt.Errorf("read review render script: %w", err)
	}
	reviewSelectJS, err := fs.ReadFile(web.Files, "generated/review-selection.js")
	if err != nil {
		return nil, fmt.Errorf("read review selection script: %w", err)
	}
	reviewThreadJS, err := fs.ReadFile(web.Files, "generated/review-thread.js")
	if err != nil {
		return nil, fmt.Errorf("read review thread script: %w", err)
	}
	viewerJS, err := fs.ReadFile(web.Files, "generated/viewer.js")
	if err != nil {
		return nil, fmt.Errorf("read viewer script: %w", err)
	}
	documentTreeJS, err := fs.ReadFile(web.Files, "generated/document-tree.js")
	if err != nil {
		return nil, fmt.Errorf("read document tree script: %w", err)
	}
	mermaidJS, err := fs.ReadFile(web.Files, "generated/mermaid.js")
	if err != nil {
		return nil, fmt.Errorf("read Mermaid integration script: %w", err)
	}
	mermaidTiny, err := fs.ReadFile(web.Files, "vendor/mermaid/mermaid.tiny.js")
	if err != nil {
		return nil, fmt.Errorf("read Mermaid library: %w", err)
	}
	htmxJS, err := fs.ReadFile(web.Files, "vendor/htmx/htmx.min.js")
	if err != nil {
		return nil, fmt.Errorf("read HTMX library: %w", err)
	}

	server := &Server{
		root:            root,
		renderer:        renderer,
		page:            page,
		styles:          styles,
		reviewJS:        reviewJS,
		reviewActionsJS: reviewActionsJS,
		reviewAPIJS:     reviewAPIJS,
		reviewDOMJS:     reviewDOMJS,
		reviewHLJS:      reviewHLJS,
		reviewNavJS:     reviewNavJS,
		reviewPanelJS:   reviewPanelJS,
		reviewRenderJS:  reviewRenderJS,
		reviewSelectJS:  reviewSelectJS,
		reviewThreadJS:  reviewThreadJS,
		documentTreeJS:  documentTreeJS,
		viewerJS:        viewerJS,
		htmxJS:          htmxJS,
		mermaidJS:       mermaidJS,
		mermaidTiny:     mermaidTiny,
	}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("create server: nil option")
		}
		if err := option(server); err != nil {
			return nil, err
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", server.handleIndex)
	mux.HandleFunc("GET /view/{path...}", server.handleDocument)
	mux.HandleFunc("GET /asset/{path...}", server.handleAsset)
	mux.HandleFunc("GET /healthz", server.handleHealth)
	mux.HandleFunc("GET /static/review.js", server.handleReviewScript)
	mux.HandleFunc("GET /static/review-actions.js", server.handleReviewActionsScript)
	mux.HandleFunc("GET /static/review-api.js", server.handleReviewAPIScript)
	mux.HandleFunc("GET /static/review-dom.js", server.handleReviewDOMScript)
	mux.HandleFunc("GET /static/review-highlights.js", server.handleReviewHighlightsScript)
	mux.HandleFunc("GET /static/review-navigation.js", server.handleReviewNavigationScript)
	mux.HandleFunc("GET /static/review-panel.js", server.handleReviewPanelScript)
	mux.HandleFunc("GET /static/review-render.js", server.handleReviewRenderScript)
	mux.HandleFunc("GET /static/review-selection.js", server.handleReviewSelectionScript)
	mux.HandleFunc("GET /static/review-thread.js", server.handleReviewThreadScript)
	mux.HandleFunc("GET /static/viewer.js", server.handleViewerScript)
	mux.HandleFunc("GET /static/document-tree.js", server.handleDocumentTreeScript)
	mux.HandleFunc("GET /static/styles.css", server.handleStyles)
	mux.HandleFunc("GET /static/htmx.min.js", server.handleHTMXLibrary)
	mux.HandleFunc("GET /static/mermaid.js", server.handleMermaidScript)
	mux.HandleFunc("GET /static/mermaid.tiny.js", server.handleMermaidLibrary)
	if server.annotations != nil {
		mux.HandleFunc("GET /api/annotations", server.handleAnnotations)
	}
	if server.review != nil {
		mux.HandleFunc("GET /ui/review/annotations", server.handleAnnotationPanel)
		mux.Handle("POST /ui/review/annotations", server.protectReviewFormMutation(http.HandlerFunc(server.handleCreateAnnotationForm)))
		mux.Handle("POST /ui/review/annotations/{id}/replies", server.protectReviewFormMutation(http.HandlerFunc(server.handleReplyAnnotationForm)))
		mux.Handle("POST /ui/review/annotations/{id}/transition", server.protectReviewFormMutation(http.HandlerFunc(server.handleTransitionAnnotationForm)))
		mux.Handle("POST /ui/review/annotations/{id}/reattach", server.protectReviewFormMutation(http.HandlerFunc(server.handleReattachAnnotationForm)))
		mux.Handle("POST /api/annotations", server.protectReviewMutation(http.HandlerFunc(server.handleCreateAnnotation)))
		mux.Handle("PATCH /api/annotations/{id}", server.protectReviewMutation(http.HandlerFunc(server.handleTransitionAnnotation)))
		mux.Handle("POST /api/annotations/{id}/replies", server.protectReviewMutation(http.HandlerFunc(server.handleReplyAnnotation)))
		mux.Handle("POST /api/annotations/{id}/reattach", server.protectReviewMutation(http.HandlerFunc(server.handleReattachAnnotation)))
	}
	if server.comparison != nil {
		mux.HandleFunc("GET /api/git-comparison", server.handleComparisonState)
	}
	if server.comparisonControlEnabled() {
		mux.Handle("POST /api/git-comparison", server.protectComparisonMutation(http.HandlerFunc(server.handleComparisonSelect)))
	}
	server.handler = securityHeaders(mux)

	return server, nil
}

func parseViewerTemplates() (*template.Template, error) {
	return template.ParseFS(web.Files, "templates/*.html")
}

// handleReviewScript serves the embedded annotation UI without requiring a
// runtime asset directory. The script is inert on pages outside review mode.
func (s *Server) handleReviewScript(response http.ResponseWriter, _ *http.Request) {
	response.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	_, _ = response.Write(s.reviewJS)
}

func (s *Server) handleReviewActionsScript(response http.ResponseWriter, _ *http.Request) {
	response.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	_, _ = response.Write(s.reviewActionsJS)
}

func (s *Server) handleReviewAPIScript(response http.ResponseWriter, _ *http.Request) {
	response.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	_, _ = response.Write(s.reviewAPIJS)
}

func (s *Server) handleReviewDOMScript(response http.ResponseWriter, _ *http.Request) {
	response.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	_, _ = response.Write(s.reviewDOMJS)
}

func (s *Server) handleReviewHighlightsScript(response http.ResponseWriter, _ *http.Request) {
	response.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	_, _ = response.Write(s.reviewHLJS)
}

func (s *Server) handleReviewNavigationScript(response http.ResponseWriter, _ *http.Request) {
	response.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	_, _ = response.Write(s.reviewNavJS)
}

func (s *Server) handleReviewPanelScript(response http.ResponseWriter, _ *http.Request) {
	response.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	_, _ = response.Write(s.reviewPanelJS)
}

func (s *Server) handleReviewRenderScript(response http.ResponseWriter, _ *http.Request) {
	response.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	_, _ = response.Write(s.reviewRenderJS)
}

func (s *Server) handleReviewSelectionScript(response http.ResponseWriter, _ *http.Request) {
	response.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	_, _ = response.Write(s.reviewSelectJS)
}

func (s *Server) handleReviewThreadScript(response http.ResponseWriter, _ *http.Request) {
	response.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	_, _ = response.Write(s.reviewThreadJS)
}

// handleViewerScript serves shared navigation behavior on every viewer page.
func (s *Server) handleViewerScript(response http.ResponseWriter, _ *http.Request) {
	response.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	_, _ = response.Write(s.viewerJS)
}

func (s *Server) handleDocumentTreeScript(response http.ResponseWriter, _ *http.Request) {
	response.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	_, _ = response.Write(s.documentTreeJS)
}

// handleStyles serves the embedded viewer stylesheet from the same origin so
// pages do not require CSP permission for inline styles.
func (s *Server) handleStyles(response http.ResponseWriter, _ *http.Request) {
	response.Header().Set("Content-Type", "text/css; charset=utf-8")
	_, _ = response.Write(s.styles)
}

// handleHTMXLibrary serves the pinned HTMX bundle. Pages do not load it until
// the server-rendered review UI is activated by a later migration gate.
func (s *Server) handleHTMXLibrary(response http.ResponseWriter, _ *http.Request) {
	response.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	_, _ = response.Write(s.htmxJS)
}

// handleMermaidScript serves the application-owned diagram integration.
func (s *Server) handleMermaidScript(response http.ResponseWriter, _ *http.Request) {
	response.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	_, _ = response.Write(s.mermaidJS)
}

// handleMermaidLibrary serves the pinned, self-contained Mermaid Tiny bundle.
func (s *Server) handleMermaidLibrary(response http.ResponseWriter, _ *http.Request) {
	response.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	_, _ = response.Write(s.mermaidTiny)
}

// Handler returns the complete HTTP handler for the viewer.
func (s *Server) Handler() http.Handler {
	return s.handler
}

// HTTPServer creates a configured net/http server for address. The caller owns
// the listener and graceful shutdown lifecycle.
func (s *Server) HTTPServer(address string) *http.Server {
	return &http.Server{
		Addr:              address,
		Handler:           s.Handler(),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}
}

func (s *Server) handleIndex(response http.ResponseWriter, request *http.Request) {
	index, err := s.root.IndexWithOptions(s.indexOptions)
	if err != nil {
		http.Error(response, "could not index documents", http.StatusInternalServerError)
		return
	}
	if index.DefaultPath == "" {
		s.renderPage(request.Context(), response, index, "", nil, "", false, s.activeComparison())
		return
	}

	s.renderDocument(request.Context(), response, index, index.DefaultPath, false)
}

func (s *Server) handleDocument(response http.ResponseWriter, request *http.Request) {
	requestPath := escapedRoutePath(request, "/view/")
	documentPath, err := url.PathUnescape(requestPath)
	if err != nil {
		http.NotFound(response, request)
		return
	}
	index, err := s.root.IndexWithOptions(s.indexOptions)
	if err != nil {
		http.Error(response, "could not index documents", http.StatusInternalServerError)
		return
	}
	mode := request.URL.Query().Get("mode")
	if mode != "" && mode != "diff" {
		http.Error(response, "unsupported document mode", http.StatusBadRequest)
		return
	}
	s.renderDocument(request.Context(), response, index, documentPath, mode == "diff")
}

func (s *Server) handleAsset(response http.ResponseWriter, request *http.Request) {
	assetPath := escapedRoutePath(request, "/asset/")
	assetPath, err := url.PathUnescape(assetPath)
	if err != nil {
		http.NotFound(response, request)
		return
	}
	resolved, err := s.root.ResolveFile(assetPath)
	if err != nil {
		s.writeAssetError(response, request, err)
		return
	}

	file, err := os.Open(resolved)
	if err != nil {
		s.writeAssetError(response, request, err)
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		http.Error(response, "could not read document asset", http.StatusInternalServerError)
		return
	}
	http.ServeContent(response, request, filepath.Base(resolved), info.ModTime(), file)
}

func (s *Server) renderDocument(ctx context.Context, response http.ResponseWriter, index content.Index, documentPath string, diffMode bool) {
	active := s.activeComparison()
	document, ok := findDocument(index, documentPath)
	if !ok {
		http.Error(response, "document not found", http.StatusNotFound)
		return
	}
	source, err := s.root.ReadFile(documentPath, maxDocumentBytes)
	if err != nil {
		s.writeContentError(response, err)
		return
	}

	if diffMode && active == nil {
		http.Error(response, "Changes view is unavailable", http.StatusNotFound)
		return
	}

	var fragment []byte
	if diffMode {
		diff, diffErr := active.BuildFileDiff(ctx, documentPath, source)
		if diffErr != nil {
			// File view remains usable when a per-file Git operation fails. Avoid
			// exposing command output or repository details in the browser.
			fragment = []byte(`<section class="diff-unavailable"><h1>Changes unavailable</h1><p>The Git comparison for this file could not be generated. File view remains available.</p></section>`)
		} else {
			fragment, err = s.renderer.RenderDiff(source, diff, s.review != nil)
		}
	} else if document.Kind == content.KindCode {
		fragment, err = s.renderer.RenderCode(source, s.review != nil)
	} else if s.review != nil {
		fragment, err = s.renderer.RenderWithSourcePositions(source, documentPath)
	} else {
		fragment, err = s.renderer.Render(source, documentPath)
	}
	if err != nil {
		if errors.Is(err, mdrender.ErrUnsupportedText) {
			http.Error(response, "source file is not valid UTF-8 text", http.StatusUnsupportedMediaType)
			return
		}
		http.Error(response, "could not render document", http.StatusInternalServerError)
		return
	}
	digest := ""
	if s.review != nil {
		digest = annotation.DocumentSHA256(source)
	}
	s.renderPage(ctx, response, index, documentPath, fragment, digest, diffMode, active)
}

func (s *Server) renderPage(ctx context.Context, response http.ResponseWriter, index content.Index, selected string, fragment []byte, documentSHA256 string, diffMode bool, active *gitdiff.Config) {
	changed := make(map[string]struct{})
	changedReady := false
	changedError := false
	if active != nil {
		paths, err := active.ChangedPaths(ctx)
		if err != nil {
			changedError = true
		} else {
			changedReady = true
			for _, changedPath := range paths {
				changed[changedPath] = struct{}{}
			}
		}
	}
	documents := make([]documentView, 0, len(index.Documents))
	isCode := false
	for _, document := range index.Documents {
		if document.Path == selected && document.Kind == content.KindCode {
			isCode = true
		}
		documents = append(documents, documentView{
			Path:      document.Path,
			Name:      document.Name,
			Directory: document.Directory,
			URL:       routeURL("/view/", document.Path),
			Selected:  document.Path == selected,
			Kind:      string(document.Kind),
			Changed:   containsPath(changed, document.Path),
		})
	}

	hasMermaid := bytes.Contains(fragment, []byte(`class="mermaid-diagram"`))
	if hasMermaid {
		// Mermaid generates scoped SVG style elements and style attributes. Allow
		// inline CSS only for pages that render diagrams; script execution remains
		// restricted to application-owned, same-origin assets.
		response.Header().Set("Content-Security-Policy", mermaidContentSecurityPolicy)
	}
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := pageData{
		Root:           s.root.Path(),
		Selected:       selected,
		Documents:      documents,
		Content:        template.HTML(fragment), // goldmark output with safe defaults.
		Empty:          len(index.Documents) == 0,
		DocumentSHA256: documentSHA256,
		HasMermaid:     hasMermaid,
		IsCode:         isCode,
		ChangedReady:   changedReady,
		ChangedError:   changedError,
		DiffMode:       diffMode,
		DiffAvailable:  active != nil,
		FileURL:        routeURL("/view/", selected),
		ChangesURL:     routeURL("/view/", selected) + "?mode=diff",
	}
	if active != nil {
		data.DiffBase = active.RequestedBase
		data.DiffCommit = active.BaseCommit
		data.DiffCommitShort = abbreviatedCommit(active.BaseCommit)
	}
	if s.comparisonControlEnabled() {
		data.ComparisonToken = s.comparison.token
	}
	if s.review != nil {
		data.ReviewToken = s.review.token
	}
	if err := s.page.ExecuteTemplate(response, "page.html", data); err != nil {
		// Headers may already be written; this message is primarily useful in
		// tests and terminal diagnostics until structured logging is added.
		http.Error(response, "could not render viewer page", http.StatusInternalServerError)
	}
}

// abbreviatedCommit keeps the visible comparison identity compact while the
// complete immutable object ID remains available in metadata and hover text.
func abbreviatedCommit(commit string) string {
	const displayLength = 12
	if len(commit) <= displayLength {
		return commit
	}
	return commit[:displayLength]
}

// containsPath reports changed-set membership without exposing the mutable map
// to templates or duplicating lookup code in the render loop.
func containsPath(paths map[string]struct{}, documentPath string) bool {
	_, ok := paths[documentPath]
	return ok
}

// findDocument performs an exact membership check against the safe catalog.
func findDocument(index content.Index, documentPath string) (content.Document, bool) {
	for _, document := range index.Documents {
		if document.Path == documentPath {
			return document, true
		}
	}
	return content.Document{}, false
}

func (s *Server) writeContentError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, content.ErrTooLarge):
		http.Error(response, "document is too large", http.StatusRequestEntityTooLarge)
	case content.IsNotExist(err), errors.Is(err, content.ErrInvalidPath), errors.Is(err, content.ErrOutsideRoot), errors.Is(err, content.ErrNotRegular):
		http.Error(response, "document not found", http.StatusNotFound)
	default:
		http.Error(response, "could not read document", http.StatusInternalServerError)
	}
}

func (s *Server) writeAssetError(response http.ResponseWriter, request *http.Request, err error) {
	if content.IsNotExist(err) || errors.Is(err, content.ErrInvalidPath) || errors.Is(err, content.ErrOutsideRoot) || errors.Is(err, content.ErrNotRegular) {
		http.NotFound(response, request)
		return
	}
	http.Error(response, "could not read document asset", http.StatusInternalServerError)
}

func (s *Server) handleHealth(response http.ResponseWriter, _ *http.Request) {
	response.Header().Set("Content-Type", "text/plain; charset=utf-8")
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write([]byte("ok\n"))
}

func routeURL(prefix, relative string) string {
	return (&url.URL{Path: prefix + relative}).String()
}

func escapedRoutePath(request *http.Request, prefix string) string {
	return strings.TrimPrefix(request.URL.EscapedPath(), prefix)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Cache-Control", "no-store")
		response.Header().Set("Content-Security-Policy", baseContentSecurityPolicy)
		response.Header().Set("Referrer-Policy", "no-referrer")
		response.Header().Set("X-Content-Type-Options", "nosniff")
		response.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(response, request)
	})
}
