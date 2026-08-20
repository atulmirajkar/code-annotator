// Package server exposes Markdown content through an HTTP viewer.
package server

import (
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"atulm/md-viewer/internal/content"
	mdrender "atulm/md-viewer/internal/render"
	"atulm/md-viewer/web"
)

const maxDocumentBytes int64 = 4 << 20

// Server serves an index and rendered Markdown documents from a content root.
type Server struct {
	root     *content.Root
	renderer *mdrender.Renderer
	page     *template.Template
	styles   template.CSS
	handler  http.Handler
}

type pageData struct {
	Root      string
	Selected  string
	Documents []documentView
	Content   template.HTML
	Styles    template.CSS
	Empty     bool
}

type documentView struct {
	Name      string
	Directory string
	URL       string
	Selected  bool
}

// New creates the Markdown viewer handler.
func New(root *content.Root, renderer *mdrender.Renderer) (*Server, error) {
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
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", server.handleIndex)
	mux.HandleFunc("GET /view/{path...}", server.handleDocument)
	mux.HandleFunc("GET /healthz", server.handleHealth)
	server.handler = mux

	return server, nil
}

// Handler returns the complete HTTP handler for the viewer.
func (s *Server) Handler() http.Handler {
	return s.handler
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
	documentPath := request.PathValue("path")
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

func (s *Server) handleHealth(response http.ResponseWriter, _ *http.Request) {
	response.Header().Set("Content-Type", "text/plain; charset=utf-8")
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write([]byte("ok\n"))
}

func routeURL(prefix, relative string) string {
	return (&url.URL{Path: prefix + relative}).String()
}
