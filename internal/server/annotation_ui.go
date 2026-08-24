package server

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"atulm/code-annotator/internal/annotation"
)

func (s *Server) handleAnnotationPanel(response http.ResponseWriter, request *http.Request) {
	document := request.URL.Query().Get("document")
	showInactive, err := parseShowInactive(request.URL.Query().Get("show_inactive"))
	if err != nil {
		http.Error(response, err.Error(), http.StatusBadRequest)
		return
	}
	result, err := s.readAnnotationDocumentOperation(document)
	if err != nil {
		writeAnnotationOperationError(response, err, true)
		return
	}
	response.Header().Set("ETag", strconv.Quote(string(result.Revision)))
	s.renderAnnotationPanel(response, newAnnotationPanelView(document, string(result.Revision), result.Annotations, showInactive), http.StatusOK)
}

func (s *Server) handleCreateAnnotationForm(response http.ResponseWriter, request *http.Request) {
	expected, status, err := parseIfMatch(request)
	if err != nil {
		http.Error(response, err.Error(), status)
		return
	}
	input, status, err := decodeCreateAnnotationForm(request)
	if err != nil {
		http.Error(response, err.Error(), status)
		return
	}
	result, err := s.createAnnotationOperation(createAnnotationInput{
		Document: input.Document, Intent: input.Intent, Comment: input.Comment,
		Role: input.Role, Selection: input.Selection, ExpectedRevision: expected,
	})
	if err != nil {
		writeAnnotationOperationError(response, err, true)
		return
	}
	response.Header().Set("ETag", strconv.Quote(string(result.Document.Revision)))
	s.renderAnnotationPanel(response, newAnnotationPanelView(input.Document, string(result.Document.Revision), result.Document.Annotations, false), http.StatusOK)
}

func decodeCreateAnnotationForm(request *http.Request) (createAnnotationRequest, int, error) {
	if err := request.ParseForm(); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return createAnnotationRequest{}, http.StatusRequestEntityTooLarge, errors.New("request body is too large")
		}
		return createAnnotationRequest{}, http.StatusBadRequest, errors.New("request body must contain valid form data")
	}
	input := createAnnotationRequest{
		Document: request.PostForm.Get("document"),
		Intent:   annotation.Intent(request.PostForm.Get("intent")),
		Comment:  request.PostForm.Get("comment"),
		Role:     annotation.Role(request.PostForm.Get("role")),
	}
	start := request.PostForm.Get("selection_start_byte")
	end := request.PostForm.Get("selection_end_byte")
	digest := request.PostForm.Get("document_sha256")
	if start == "" && end == "" && digest == "" {
		return input, 0, nil
	}
	if start == "" || end == "" || digest == "" {
		return createAnnotationRequest{}, http.StatusUnprocessableEntity, errors.New("selection fields must be provided together")
	}
	startByte, err := strconv.Atoi(start)
	if err != nil {
		return createAnnotationRequest{}, http.StatusUnprocessableEntity, errors.New("selection start byte must be an integer")
	}
	endByte, err := strconv.Atoi(end)
	if err != nil {
		return createAnnotationRequest{}, http.StatusUnprocessableEntity, errors.New("selection end byte must be an integer")
	}
	input.Selection = &annotationSelection{StartByte: startByte, EndByte: endByte, DocumentSHA256: digest}
	return input, 0, nil
}

func parseShowInactive(value string) (bool, error) {
	switch value {
	case "", "false":
		return false, nil
	case "true":
		return true, nil
	default:
		return false, fmt.Errorf("invalid show_inactive value %q", value)
	}
}

func (s *Server) renderAnnotationPanel(response http.ResponseWriter, view annotationPanelView, status int) {
	var output bytes.Buffer
	if err := s.page.ExecuteTemplate(&output, "annotation-panel", view); err != nil {
		http.Error(response, "could not render annotations", http.StatusInternalServerError)
		return
	}
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.WriteHeader(status)
	_, _ = response.Write(output.Bytes())
}
