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

func (s *Server) handleReplyAnnotationForm(response http.ResponseWriter, request *http.Request) {
	expected, status, err := parseIfMatch(request)
	if err != nil {
		http.Error(response, err.Error(), status)
		return
	}
	input, status, err := decodeReplyAnnotationForm(request)
	if err != nil {
		http.Error(response, err.Error(), status)
		return
	}
	annotationID := request.PathValue("id")
	result, err := s.replyAnnotationOperation(replyAnnotationInput{
		Document: input.Document, AnnotationID: annotationID, Message: input.Message,
		Role: input.Role, ExpectedRevision: expected,
	})
	if err != nil {
		s.renderAnnotationMutationError(response, input.Document, annotationID, annotationMutationDraft{Reply: &input}, err)
		return
	}
	response.Header().Set("ETag", strconv.Quote(string(result.Document.Revision)))
	s.renderAnnotationPanel(response, newAnnotationPanelView(input.Document, string(result.Document.Revision), result.Document.Annotations, false), http.StatusOK)
}

type replyAnnotationForm struct {
	Document string
	Message  string
	Role     annotation.Role
}

type transitionAnnotationForm struct {
	Document string
	Status   annotation.Status
	Role     annotation.Role
	Activity string
	Commit   string
}

func (s *Server) handleTransitionAnnotationForm(response http.ResponseWriter, request *http.Request) {
	expected, status, err := parseIfMatch(request)
	if err != nil {
		http.Error(response, err.Error(), status)
		return
	}
	input, status, err := decodeTransitionAnnotationForm(request)
	if err != nil {
		http.Error(response, err.Error(), status)
		return
	}
	annotationID := request.PathValue("id")
	operationInput := transitionAnnotationInput{
		Document: input.Document, AnnotationID: annotationID, Status: input.Status,
		Role: input.Role, Commit: input.Commit, ExpectedRevision: expected,
	}
	if input.Status == annotation.StatusApplied {
		operationInput.Summary = input.Activity
	} else {
		operationInput.Message = input.Activity
	}
	result, err := s.transitionAnnotationOperation(operationInput)
	if err != nil {
		s.renderAnnotationMutationError(response, input.Document, annotationID, annotationMutationDraft{Transition: &input}, err)
		return
	}
	response.Header().Set("ETag", strconv.Quote(string(result.Document.Revision)))
	s.renderAnnotationPanel(response, newAnnotationPanelView(input.Document, string(result.Document.Revision), result.Document.Annotations, false), http.StatusOK)
}

type annotationMutationDraft struct {
	Reply      *replyAnnotationForm
	Transition *transitionAnnotationForm
}

func decodeReplyAnnotationForm(request *http.Request) (replyAnnotationForm, int, error) {
	if status, err := parseAnnotationForm(request); err != nil {
		return replyAnnotationForm{}, status, err
	}
	return replyAnnotationForm{
		Document: request.PostForm.Get("document"),
		Message:  request.PostForm.Get("message"),
		Role:     annotation.Role(request.PostForm.Get("role")),
	}, 0, nil
}

func decodeTransitionAnnotationForm(request *http.Request) (transitionAnnotationForm, int, error) {
	if status, err := parseAnnotationForm(request); err != nil {
		return transitionAnnotationForm{}, status, err
	}
	return transitionAnnotationForm{
		Document: request.PostForm.Get("document"),
		Status:   annotation.Status(request.PostForm.Get("status")),
		Role:     annotation.Role(request.PostForm.Get("role")),
		Activity: request.PostForm.Get("activity"),
		Commit:   request.PostForm.Get("commit"),
	}, 0, nil
}

func decodeCreateAnnotationForm(request *http.Request) (createAnnotationRequest, int, error) {
	if status, err := parseAnnotationForm(request); err != nil {
		return createAnnotationRequest{}, status, err
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

func parseAnnotationForm(request *http.Request) (int, error) {
	if err := request.ParseForm(); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return http.StatusRequestEntityTooLarge, errors.New("request body is too large")
		}
		return http.StatusBadRequest, errors.New("request body must contain valid form data")
	}
	return 0, nil
}

func (s *Server) renderAnnotationMutationError(response http.ResponseWriter, document, annotationID string, draft annotationMutationDraft, err error) {
	var operationErr *annotationOperationError
	if !errors.As(err, &operationErr) || (operationErr.kind != annotationOperationInvalid && operationErr.kind != annotationOperationConflict) {
		writeAnnotationOperationError(response, err, true)
		return
	}
	result, readErr := s.readAnnotationDocumentOperation(document)
	if readErr != nil {
		writeAnnotationOperationError(response, readErr, true)
		return
	}
	view := newAnnotationPanelView(document, string(result.Revision), result.Annotations, false)
	status := http.StatusUnprocessableEntity
	view.Feedback = operationErr.message
	view.FeedbackKind = "validation"
	if operationErr.kind == annotationOperationConflict {
		status = http.StatusConflict
		view.Feedback = "Annotations changed; review the latest state before retrying."
		view.FeedbackKind = "conflict"
	}
	applyAnnotationMutationDraft(&view, annotationID, draft)
	response.Header().Set("ETag", strconv.Quote(string(result.Revision)))
	s.renderAnnotationPanel(response, view, status)
}

func applyAnnotationMutationDraft(view *annotationPanelView, annotationID string, draft annotationMutationDraft) {
	for index := range view.Cards {
		if view.Cards[index].ID != annotationID {
			continue
		}
		actions := &view.Cards[index].Actions
		if draft.Reply != nil {
			actions.ReplyRole = draft.Reply.Role
			actions.ReplyMessage = draft.Reply.Message
		}
		if draft.Transition != nil {
			actions.TransitionRole = draft.Transition.Role
			actions.TransitionActivity = draft.Transition.Activity
			actions.TransitionCommit = draft.Transition.Commit
			for transitionIndex := range actions.Transitions {
				actions.Transitions[transitionIndex].Selected = actions.Transitions[transitionIndex].Status == draft.Transition.Status
			}
		}
		return
	}
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
