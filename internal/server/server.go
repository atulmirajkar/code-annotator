// Package server exposes Markdown content through an HTTP viewer.
package server

import (
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

	annotationstore "atulm/md-viewer/internal/annotation/store"
	"atulm/md-viewer/internal/content"
	mdrender "atulm/md-viewer/internal/render"
	"atulm/md-viewer/web"
)

const (
	maxDocumentBytes           int64 = 4 << 20
	maxAnnotationMutationBytes int64 = 64 << 10
	reviewTokenHeader                = "X-MD-Viewer-Token"
)

const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 15 * time.Second
	writeTimeout      = 30 * time.Second
	idleTimeout       = 60 * time.Second
)

// Server serves an index and rendered Markdown documents from a content root.
type Server struct {
	root        *content.Root
	renderer    *mdrender.Renderer
	annotations *annotationstore.Store
	review      *reviewSession
	page        *template.Template
	styles      template.CSS
	handler     http.Handler
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
	Root        string
	Selected    string
	Documents   []documentView
	Content     template.HTML
	Styles      template.CSS
	Empty       bool
	ReviewToken string
}

type documentView struct {
	Name      string
	Directory string
	URL       string
	Selected  bool
}

// validateReviewOrigin accepts only an HTTP origin with a loopback IP host and
// no path, query, user information, or fragment.
func validateReviewOrigin(origin string) (string, error) {
	parsed, err := url.Parse(origin)
	if err != nil {
		return "", errors.New("configure review session: invalid origin")
	}
	if parsed.Scheme != "http" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", errors.New("configure review session: invalid origin")
	}
	address := net.ParseIP(parsed.Hostname())
	if address == nil || !address.IsLoopback() {
		return "", errors.New("configure review session: origin must use a loopback IP address")
	}
	return parsed.Scheme + "://" + parsed.Host, nil
}

// protectReviewMutation enforces the browser session boundary before invoking
// an annotation mutation handler. The wrapped handler remains responsible for
// decoding JSON and reporting an oversized body as a client error.
func (s *Server) protectReviewMutation(next http.Handler) http.Handler {
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
			http.Error(response, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
			return
		}
		if mediaType != "application/json" {
			http.Error(response, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
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

	pageSource, err := fs.ReadFile(web.Files, "page.html")
	if err != nil {
		return nil, fmt.Errorf("read page template: %w", err)
	}
	page, err := template.New("page").Parse(string(pageSource))
	if err != nil {
		return nil, fmt.Errorf("parse page template: %w", err)
	}
	styles, err := fs.ReadFile(web.Files, "styles.css")
	if err != nil {
		return nil, fmt.Errorf("read viewer styles: %w", err)
	}

	server := &Server{
		root:     root,
		renderer: renderer,
		page:     page,
		styles:   template.CSS(styles), // Embedded, application-owned CSS.
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
	if server.annotations != nil {
		mux.HandleFunc("GET /api/annotations", server.handleAnnotations)
	}
	if server.review != nil {
		mux.Handle("POST /api/annotations", server.protectReviewMutation(http.HandlerFunc(server.handleCreateAnnotation)))
		mux.Handle("PATCH /api/annotations/{id}", server.protectReviewMutation(http.HandlerFunc(server.handleTransitionAnnotation)))
		mux.Handle("POST /api/annotations/{id}/replies", server.protectReviewMutation(http.HandlerFunc(server.handleReplyAnnotation)))
	}
	server.handler = securityHeaders(mux)

	return server, nil
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
	index, err := s.root.Index()
	if err != nil {
		http.Error(response, "could not index Markdown documents", http.StatusInternalServerError)
		return
	}
	if index.DefaultPath == "" {
		s.renderPage(response, index, "", nil)
		return
	}

	s.renderDocument(response, index, index.DefaultPath)
}

func (s *Server) handleDocument(response http.ResponseWriter, request *http.Request) {
	requestPath := escapedRoutePath(request, "/view/")
	documentPath, err := url.PathUnescape(requestPath)
	if err != nil {
		http.NotFound(response, request)
		return
	}
	if !strings.EqualFold(filepath.Ext(documentPath), ".md") {
		http.NotFound(response, request)
		return
	}

	index, err := s.root.Index()
	if err != nil {
		http.Error(response, "could not index Markdown documents", http.StatusInternalServerError)
		return
	}
	s.renderDocument(response, index, documentPath)
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

func (s *Server) renderDocument(response http.ResponseWriter, index content.Index, documentPath string) {
	source, err := s.root.ReadFile(documentPath, maxDocumentBytes)
	if err != nil {
		s.writeContentError(response, err)
		return
	}

	fragment, err := s.renderer.Render(source, documentPath)
	if err != nil {
		http.Error(response, "could not render Markdown document", http.StatusInternalServerError)
		return
	}
	s.renderPage(response, index, documentPath, fragment)
}

func (s *Server) renderPage(response http.ResponseWriter, index content.Index, selected string, fragment []byte) {
	documents := make([]documentView, 0, len(index.Documents))
	for _, document := range index.Documents {
		documents = append(documents, documentView{
			Name:      document.Name,
			Directory: document.Directory,
			URL:       routeURL("/view/", document.Path),
			Selected:  document.Path == selected,
		})
	}

	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := pageData{
		Root:      s.root.Path(),
		Selected:  selected,
		Documents: documents,
		Content:   template.HTML(fragment), // goldmark output with safe defaults.
		Styles:    s.styles,
		Empty:     len(index.Documents) == 0,
	}
	if s.review != nil {
		data.ReviewToken = s.review.token
	}
	if err := s.page.Execute(response, data); err != nil {
		// Headers may already be written; this message is primarily useful in
		// tests and terminal diagnostics until structured logging is added.
		http.Error(response, "could not render viewer page", http.StatusInternalServerError)
	}
}

func (s *Server) writeContentError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, content.ErrTooLarge):
		http.Error(response, "Markdown document is too large", http.StatusRequestEntityTooLarge)
	case content.IsNotExist(err), errors.Is(err, content.ErrInvalidPath), errors.Is(err, content.ErrOutsideRoot), errors.Is(err, content.ErrNotRegular):
		http.Error(response, "Markdown document not found", http.StatusNotFound)
	default:
		http.Error(response, "could not read Markdown document", http.StatusInternalServerError)
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
		response.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self' data: http: https:; style-src 'unsafe-inline'; font-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
		response.Header().Set("Referrer-Policy", "no-referrer")
		response.Header().Set("X-Content-Type-Options", "nosniff")
		response.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(response, request)
	})
}
