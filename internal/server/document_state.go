package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"atulm/code-annotator/internal/annotation"
	"atulm/code-annotator/internal/content"
)

const documentStateSchemaVersion = 1

var (
	errDocumentStateNotFound        = errors.New("document not found")
	errDocumentStateMode            = errors.New("unsupported document mode")
	errDocumentStateDiffUnavailable = errors.New("Changes view is unavailable")
)

// documentCatalogState is the inactive typed browser boundary for commit 8A.
// Commit 8B will consume it instead of reconstructing catalog state from HTML.
type documentCatalogState struct {
	SchemaVersion    int                   `json:"schemaVersion"`
	SelectedPath     *string               `json:"selectedPath"`
	Mode             string                `json:"mode"`
	ChangedAvailable bool                  `json:"changedAvailable"`
	ChangedError     bool                  `json:"changedError"`
	ReviewAvailable  bool                  `json:"reviewAvailable"`
	Documents        []documentCatalogItem `json:"documents"`
}

type documentCatalogItem struct {
	Path             string       `json:"path"`
	Name             string       `json:"name"`
	Directory        string       `json:"directory"`
	Kind             content.Kind `json:"kind"`
	URL              string       `json:"url"`
	Selected         bool         `json:"selected"`
	Changed          bool         `json:"changed"`
	OpenCommentCount int          `json:"openCommentCount"`
}

func (s *Server) handleDocumentState(response http.ResponseWriter, request *http.Request) {
	state, err := s.readDocumentCatalogState(request.Context(), request.URL.Query().Get("document"), request.URL.Query().Get("mode"))
	if err != nil {
		s.writeDocumentStateError(response, err)
		return
	}
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(response).Encode(state)
}

func (s *Server) readDocumentCatalogState(ctx context.Context, selected, mode string) (documentCatalogState, error) {
	index, err := s.root.IndexWithOptions(s.indexOptions)
	if err != nil {
		return documentCatalogState{}, err
	}

	if selected == "" {
		selected = index.DefaultPath
	} else if _, ok := findDocument(index, selected); !ok {
		return documentCatalogState{}, errDocumentStateNotFound
	}
	if mode == "" {
		mode = "file"
	}
	if mode != "file" && mode != "diff" {
		return documentCatalogState{}, errDocumentStateMode
	}

	active := s.activeComparison()
	if mode == "diff" && active == nil {
		return documentCatalogState{}, errDocumentStateDiffUnavailable
	}
	changed := make(map[string]struct{})
	changedAvailable := false
	changedError := false
	if active != nil {
		paths, changedErr := active.ChangedPaths(ctx)
		if changedErr != nil {
			changedError = true
		} else {
			changedAvailable = true
			for _, path := range paths {
				changed[path] = struct{}{}
			}
		}
	}

	openCommentCounts, err := s.documentOpenCommentCounts(index)
	if err != nil {
		return documentCatalogState{}, err
	}
	return newDocumentCatalogState(index, selected, mode, changed, changedAvailable, changedError, s.annotations != nil, openCommentCounts), nil
}

func (s *Server) writeDocumentStateError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errDocumentStateNotFound), errors.Is(err, errDocumentStateDiffUnavailable):
		http.Error(response, err.Error(), http.StatusNotFound)
	case errors.Is(err, errDocumentStateMode):
		http.Error(response, err.Error(), http.StatusBadRequest)
	default:
		http.Error(response, "could not load document state", http.StatusInternalServerError)
	}
}

func (s *Server) documentOpenCommentCounts(index content.Index) (map[string]int, error) {
	counts := make(map[string]int)
	if s.annotations == nil {
		return counts, nil
	}
	for _, document := range index.Documents {
		sidecar, _, err := s.annotations.Load(document.Path)
		if err != nil {
			return nil, err
		}
		counts[document.Path] = activeAnnotationCount(sidecar.Annotations)
	}
	return counts, nil
}

func newDocumentCatalogState(
	index content.Index,
	selected string,
	mode string,
	changed map[string]struct{},
	changedAvailable bool,
	changedError bool,
	reviewAvailable bool,
	openCommentCounts map[string]int,
) documentCatalogState {
	state := documentCatalogState{
		SchemaVersion:    documentStateSchemaVersion,
		Mode:             mode,
		ChangedAvailable: changedAvailable,
		ChangedError:     changedError,
		ReviewAvailable:  reviewAvailable,
		Documents:        make([]documentCatalogItem, 0, len(index.Documents)),
	}
	if selected != "" {
		state.SelectedPath = &selected
	}
	for _, document := range index.Documents {
		url := routeURL("/view/", document.Path)
		if mode == "diff" {
			url += "?mode=diff"
		}
		state.Documents = append(state.Documents, documentCatalogItem{
			Path: document.Path, Name: document.Name, Directory: document.Directory,
			Kind: document.Kind, URL: url, Selected: document.Path == selected,
			Changed: containsPath(changed, document.Path), OpenCommentCount: openCommentCounts[document.Path],
		})
	}
	return state
}

func activeAnnotationCount(items []annotation.Annotation) int {
	count := 0
	for _, item := range items {
		if !isInactiveAnnotation(item.Status) {
			count++
		}
	}
	return count
}
