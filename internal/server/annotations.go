package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"atulm/md-viewer/internal/annotation"
	"atulm/md-viewer/internal/content"
)

// annotationView extends one persisted annotation with its location derived
// from the current Markdown source. Document-level annotations have no anchor.
type annotationView struct {
	annotation.Annotation
	Anchor *annotation.AnchorResult `json:"anchor,omitempty"`
}

// annotationListResponse is the wire representation returned by the read API.
// Revision is also emitted as the HTTP ETag for later optimistic mutations.
type annotationListResponse struct {
	SchemaVersion int              `json:"schemaVersion"`
	Document      string           `json:"document"`
	Revision      string           `json:"revision"`
	Annotations   []annotationView `json:"annotations"`
}

// handleAnnotations returns persisted annotations plus anchor locations derived
// from the current Markdown bytes. It never mutates either root.
func (s *Server) handleAnnotations(response http.ResponseWriter, request *http.Request) {
	document := request.URL.Query().Get("document")
	source, err := s.root.ReadFile(document, maxDocumentBytes)
	if err != nil {
		s.writeAnnotationReadError(response, err)
		return
	}
	sidecar, revision, err := s.annotations.Load(document)
	if err != nil {
		http.Error(response, "could not read annotations", http.StatusInternalServerError)
		return
	}

	annotations := make([]annotationView, 0, len(sidecar.Annotations))
	for _, item := range sidecar.Annotations {
		view := annotationView{Annotation: item}
		if item.Source != nil {
			anchor, err := annotation.ResolveAnchor(source, *item.Source)
			if err != nil {
				http.Error(response, "could not resolve annotation anchor", http.StatusInternalServerError)
				return
			}
			view.Anchor = &anchor
		}
		annotations = append(annotations, view)
	}

	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("ETag", strconv.Quote(string(revision)))
	payload := annotationListResponse{
		SchemaVersion: sidecar.SchemaVersion,
		Document:      sidecar.Document,
		Revision:      string(revision),
		Annotations:   annotations,
	}
	if err := json.NewEncoder(response).Encode(payload); err != nil {
		// The response may already be committed; structured request logging will
		// report this error when it is introduced.
		return
	}
}

// writeAnnotationReadError hides filesystem details and treats invalid or
// unavailable document paths as missing resources.
func (s *Server) writeAnnotationReadError(response http.ResponseWriter, err error) {
	if content.IsNotExist(err) || errors.Is(err, content.ErrInvalidPath) || errors.Is(err, content.ErrOutsideRoot) || errors.Is(err, content.ErrNotRegular) {
		http.Error(response, "Markdown document not found", http.StatusNotFound)
		return
	}
	if errors.Is(err, content.ErrTooLarge) {
		http.Error(response, "Markdown document is too large", http.StatusRequestEntityTooLarge)
		return
	}
	http.Error(response, "could not read Markdown document", http.StatusInternalServerError)
}
