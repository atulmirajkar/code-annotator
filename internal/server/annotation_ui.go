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
	s.renderAnnotationPanel(response, newAnnotationPanelView(document, result.Annotations, showInactive), http.StatusOK)
}

func (s *Server) handleCreateAnnotationForm(response http.ResponseWriter, request *http.Request) {
	expected, status, err := parseIfMatch(request)
	if err != nil {
		http.Error(response, err.Error(), status)
		return
	}
	input, status, err := decodeCreateAnnotationForm(request)
	if err != nil {
		if status == http.StatusUnprocessableEntity && input.Document != "" {
			s.renderAnnotationPanelFeedback(response, input.Document, "", annotationMutationDraft{}, err.Error(), "validation", status)
			return
		}
		http.Error(response, err.Error(), status)
		return
	}
	result, err := s.createAnnotationOperation(createAnnotationInput{
		Document: input.Document, Intent: input.Intent, Comment: input.Comment,
		Role: input.Role, Selection: input.Selection, ExpectedRevision: expected,
	})
	if err != nil {
		s.renderAnnotationMutationError(response, input.Document, "", annotationMutationDraft{}, err)
		return
	}
	response.Header().Set("ETag", strconv.Quote(string(result.Document.Revision)))
	s.renderAnnotationPanel(response, newAnnotationPanelView(input.Document, result.Document.Annotations, false), http.StatusOK)
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
	s.renderAnnotationPanel(response, newAnnotationPanelView(input.Document, result.Document.Annotations, false), http.StatusOK)
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

type reattachAnnotationForm struct {
	Document  string
	StartByte string
	EndByte   string
	Digest    string
	Selection annotationSelection
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
	s.renderAnnotationPanel(response, newAnnotationPanelView(input.Document, result.Document.Annotations, false), http.StatusOK)
}

func (s *Server) handleReattachAnnotationForm(response http.ResponseWriter, request *http.Request) {
	expected, status, err := parseIfMatch(request)
	if err != nil {
		http.Error(response, err.Error(), status)
		return
	}
	input, status, err := decodeReattachAnnotationForm(request)
	annotationID := request.PathValue("id")
	draft := annotationMutationDraft{Reattach: &input}
	if err != nil {
		if status == http.StatusUnprocessableEntity && input.Document != "" {
			s.renderAnnotationPanelFeedback(response, input.Document, annotationID, draft, err.Error(), "validation", status)
			return
		}
		http.Error(response, err.Error(), status)
		return
	}
	result, err := s.reattachAnnotationOperation(reattachAnnotationInput{
		Document: input.Document, AnnotationID: annotationID, Selection: input.Selection,
		ExpectedRevision: expected,
	})
	if err != nil {
		var operationErr *annotationOperationError
		if errors.As(err, &operationErr) && operationErr.kind == annotationOperationConflict && operationErr.revision == "" {
			draft.Reattach = nil
		}
		s.renderAnnotationMutationError(response, input.Document, annotationID, draft, err)
		return
	}
	response.Header().Set("ETag", strconv.Quote(string(result.Document.Revision)))
	s.renderAnnotationPanel(response, newAnnotationPanelView(input.Document, result.Document.Annotations, false), http.StatusOK)
}

type annotationMutationDraft struct {
	Reply      *replyAnnotationForm
	Reattach   *reattachAnnotationForm
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

func decodeReattachAnnotationForm(request *http.Request) (reattachAnnotationForm, int, error) {
	if status, err := parseAnnotationForm(request); err != nil {
		return reattachAnnotationForm{}, status, err
	}
	input := reattachAnnotationForm{
		Document:  request.PostForm.Get("document"),
		StartByte: request.PostForm.Get("selection_start_byte"),
		EndByte:   request.PostForm.Get("selection_end_byte"),
		Digest:    request.PostForm.Get("document_sha256"),
	}
	selection, status, err := decodeAnnotationSelection(input.StartByte, input.EndByte, input.Digest, true)
	if err != nil {
		return input, status, err
	}
	input.Selection = *selection
	return input, 0, nil
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
	selection, status, err := decodeAnnotationSelection(
		request.PostForm.Get("selection_start_byte"),
		request.PostForm.Get("selection_end_byte"),
		request.PostForm.Get("document_sha256"), false,
	)
	if err != nil {
		return input, status, err
	}
	input.Selection = selection
	return input, 0, nil
}

func decodeAnnotationSelection(start, end, digest string, required bool) (*annotationSelection, int, error) {
	if start == "" && end == "" && digest == "" {
		if required {
			return nil, http.StatusUnprocessableEntity, errors.New("selection fields are required")
		}
		return nil, 0, nil
	}
	if start == "" || end == "" || digest == "" {
		return nil, http.StatusUnprocessableEntity, errors.New("selection fields must be provided together")
	}
	startByte, err := strconv.Atoi(start)
	if err != nil {
		return nil, http.StatusUnprocessableEntity, errors.New("selection start byte must be an integer")
	}
	endByte, err := strconv.Atoi(end)
	if err != nil {
		return nil, http.StatusUnprocessableEntity, errors.New("selection end byte must be an integer")
	}
	return &annotationSelection{StartByte: startByte, EndByte: endByte, DocumentSHA256: digest}, 0, nil
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
	status := http.StatusUnprocessableEntity
	message := operationErr.message
	kind := "validation"
	if operationErr.kind == annotationOperationConflict {
		status = http.StatusConflict
		kind = "conflict"
		if operationErr.revision != "" {
			message = "Annotations changed; review the latest state before retrying."
		}
	}
	s.renderAnnotationPanelFeedback(response, document, annotationID, draft, message, kind, status)
}

func (s *Server) renderAnnotationPanelFeedback(response http.ResponseWriter, document, annotationID string, draft annotationMutationDraft, message, kind string, status int) {
	result, err := s.readAnnotationDocumentOperation(document)
	if err != nil {
		writeAnnotationOperationError(response, err, true)
		return
	}
	view := newAnnotationPanelView(document, result.Annotations, false)
	view.Feedback = message
	view.FeedbackKind = kind
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
		if draft.Reattach != nil {
			actions.ReattachStartByte = draft.Reattach.StartByte
			actions.ReattachEndByte = draft.Reattach.EndByte
			actions.ReattachDigest = draft.Reattach.Digest
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
	if err := s.page.ExecuteTemplate(&output, "annotation-panel-response", view); err != nil {
		http.Error(response, "could not render annotations", http.StatusInternalServerError)
		return
	}
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.WriteHeader(status)
	_, _ = response.Write(output.Bytes())
}
