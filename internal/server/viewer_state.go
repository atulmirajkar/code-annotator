package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"atulm/code-annotator/internal/annotation"
	"atulm/code-annotator/internal/content"
	mdrender "atulm/code-annotator/internal/render"
)

const viewerStateSchemaVersion = 1

// viewerStateResponse is the typed JSON boundary for browser-only behavior.
// HTML templates remain responsible for presentation and semantic element
// identity; later migration slices add source, document-tree, and comparison
// state here instead of introducing new custom data attributes.
type viewerStateResponse struct {
	SchemaVersion int                 `json:"schemaVersion"`
	Document      viewerDocumentState `json:"document"`
	Review        *viewerReviewState  `json:"review"`
}

type viewerDocumentState struct {
	Path        string                 `json:"path"`
	Kind        content.Kind           `json:"kind"`
	SHA256      string                 `json:"sha256"`
	SourceNodes []viewerSourcePosition `json:"sourceNodes"`
	Diagrams    []viewerSourcePosition `json:"diagrams"`
}

type viewerSourcePosition struct {
	ElementID string `json:"elementId"`
	StartByte int    `json:"startByte"`
	EndByte   int    `json:"endByte"`
}

type viewerReviewState struct {
	Revision    string                  `json:"revision"`
	Annotations []viewerAnnotationState `json:"annotations"`
}

type viewerAnnotationState struct {
	ID                string                     `json:"id"`
	ElementID         string                     `json:"elementId"`
	LifecycleFormID   string                     `json:"lifecycleFormId"`
	DocumentLevel     bool                       `json:"documentLevel"`
	NeedsReattachment bool                       `json:"needsReattachment"`
	SourceStartByte   *int                       `json:"sourceStartByte"`
	Anchor            *viewerAnchorState         `json:"anchor"`
	Transitions       []viewerTransitionBehavior `json:"transitions"`
}

type viewerAnchorState struct {
	State     annotation.AnchorState `json:"state"`
	StartByte int                    `json:"startByte"`
	EndByte   int                    `json:"endByte"`
}

type viewerTransitionBehavior struct {
	Status        annotation.Status `json:"status"`
	Role          annotation.Role   `json:"role"`
	Activity      string            `json:"activity"`
	ActivityLabel string            `json:"activityLabel"`
}

func (s *Server) handleViewerState(response http.ResponseWriter, request *http.Request) {
	documentPath := request.URL.Query().Get("document")
	document, catalogDocument, err := s.loadAnnotationSource(documentPath)
	if err != nil {
		writeAnnotationOperationError(response, err, false)
		return
	}

	state := viewerStateResponse{
		SchemaVersion: viewerStateSchemaVersion,
		Document: viewerDocumentState{
			Path:   documentPath,
			Kind:   catalogDocument.Kind,
			SHA256: annotation.DocumentSHA256(document),
		},
	}
	sourceMap, err := s.viewerSourceMap(request, document, catalogDocument)
	if err != nil {
		s.writeContentError(response, err)
		return
	}
	state.Document.SourceNodes = viewerSourcePositions(sourceMap.Nodes)
	state.Document.Diagrams = viewerSourcePositions(sourceMap.Diagrams)
	if s.annotations != nil {
		result, operationErr := s.readAnnotationDocumentOperation(documentPath)
		if operationErr != nil {
			writeAnnotationOperationError(response, operationErr, false)
			return
		}
		state.Review = newViewerReviewState(result)
		response.Header().Set("ETag", strconv.Quote(string(result.Revision)))
	}

	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(response).Encode(state)
}

func (s *Server) viewerSourceMap(request *http.Request, document []byte, catalogDocument content.Document) (mdrender.SourceMap, error) {
	mode := request.URL.Query().Get("mode")
	if mode != "" && mode != "file" && mode != "diff" {
		return mdrender.SourceMap{}, content.ErrInvalidPath
	}
	if mode == "diff" {
		active := s.activeComparison()
		if active == nil {
			return mdrender.SourceMap{}, content.ErrInvalidPath
		}
		diff, err := active.BuildFileDiff(request.Context(), catalogDocument.Path, document)
		if err != nil {
			// The page renders a Changes-unavailable message with no selectable
			// source in this case, so its typed state is intentionally empty.
			return mdrender.SourceMap{Nodes: []mdrender.SourcePosition{}, Diagrams: []mdrender.SourcePosition{}}, nil
		}
		return s.renderer.DiffSourceMap(document, diff)
	}
	if catalogDocument.Kind == content.KindCode {
		return s.renderer.CodeSourceMap(document)
	}
	return s.renderer.MarkdownSourceMap(document), nil
}

func viewerSourcePositions(values []mdrender.SourcePosition) []viewerSourcePosition {
	result := make([]viewerSourcePosition, 0, len(values))
	for _, value := range values {
		result = append(result, viewerSourcePosition{ElementID: value.ElementID, StartByte: value.StartByte, EndByte: value.EndByte})
	}
	return result
}

func newViewerReviewState(result annotationDocumentResult) *viewerReviewState {
	state := &viewerReviewState{
		Revision:    string(result.Revision),
		Annotations: make([]viewerAnnotationState, 0, len(result.Annotations)),
	}
	for _, item := range result.Annotations {
		view := viewerAnnotationState{
			ID:                item.ID,
			ElementID:         annotationElementID(item.ID),
			LifecycleFormID:   lifecycleFormElementID(item.ID),
			DocumentLevel:     item.Source == nil && !item.NeedsReattachment,
			NeedsReattachment: item.NeedsReattachment,
			Transitions:       []viewerTransitionBehavior{},
		}
		if item.Source != nil {
			start := item.Source.Selector.StartByte
			view.SourceStartByte = &start
		}
		if item.Anchor != nil {
			view.Anchor = &viewerAnchorState{
				State: item.Anchor.State, StartByte: item.Anchor.StartByte, EndByte: item.Anchor.EndByte,
			}
		}
		for _, transition := range newAnnotationActionsView(result.Document.Path, item, item.Anchor != nil && item.Anchor.State == annotation.AnchorStale).Transitions {
			view.Transitions = append(view.Transitions, viewerTransitionBehavior{
				Status: transition.Status, Role: transition.Role,
				Activity: transition.Activity, ActivityLabel: transition.ActivityLabel,
			})
		}
		state.Annotations = append(state.Annotations, view)
	}
	return state
}
