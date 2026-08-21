package server

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"atulm/md-viewer/internal/annotation"
	annotationstore "atulm/md-viewer/internal/annotation/store"
	"atulm/md-viewer/internal/content"
)

var errTransitionIdentifier = errors.New("generate transition identifier")

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

// createAnnotationRequest contains only reviewer-controlled fields. Lifecycle,
// identifiers, timestamps, and source hashes are assigned by the server.
type createAnnotationRequest struct {
	Document  string               `json:"document"`
	Intent    annotation.Intent    `json:"intent"`
	Comment   string               `json:"comment"`
	Author    string               `json:"author"`
	Selection *annotationSelection `json:"selection,omitempty"`
}

// annotationSelection carries the browser's Markdown byte range and quote. The
// server recreates the complete source selector from current document bytes.
type annotationSelection struct {
	StartByte int    `json:"startByte"`
	EndByte   int    `json:"endByte"`
	Exact     string `json:"exact"`
}

// createAnnotationResponse returns the created annotation and the sidecar
// revision required by the caller's next mutation.
type createAnnotationResponse struct {
	Annotation annotationView `json:"annotation"`
	Revision   string         `json:"revision"`
}

// replyAnnotationRequest contains the reviewer or agent-authored content for an
// ordinary discussion reply. Structured lifecycle events use transition APIs.
type replyAnnotationRequest struct {
	Document string `json:"document"`
	Message  string `json:"message"`
	Author   string `json:"author"`
}

// replyAnnotationResponse returns the updated annotation and sidecar revision.
type replyAnnotationResponse struct {
	Annotation annotationView `json:"annotation"`
	Revision   string         `json:"revision"`
}

// transitionAnnotationRequest describes one lifecycle transition and any
// activity content required for that transition.
type transitionAnnotationRequest struct {
	Document  string               `json:"document"`
	Status    annotation.Status    `json:"status"`
	ActorRole annotation.ActorRole `json:"actorRole"`
	Author    string               `json:"author"`
	Message   string               `json:"message,omitempty"`
	Summary   string               `json:"summary,omitempty"`
	Commit    string               `json:"commit,omitempty"`
}

// transitionAnnotationResponse returns the transitioned annotation and the new
// sidecar revision.
type transitionAnnotationResponse struct {
	Annotation annotationView `json:"annotation"`
	Revision   string         `json:"revision"`
}

// handleAnnotations returns persisted annotations plus anchor locations derived
// from the current Markdown bytes. It never mutates either root.
func (s *Server) handleAnnotations(response http.ResponseWriter, request *http.Request) {
	document := request.URL.Query().Get("document")
	if err := annotation.ValidateDocumentPath(document); err != nil {
		http.Error(response, "Markdown document not found", http.StatusNotFound)
		return
	}
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
		view, err := resolveAnnotationView(source, item)
		if err != nil {
			http.Error(response, "could not resolve annotation anchor", http.StatusInternalServerError)
			return
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

// handleCreateAnnotation verifies the selected Markdown bytes, appends one open
// annotation, and saves it using the revision supplied in If-Match.
func (s *Server) handleCreateAnnotation(response http.ResponseWriter, request *http.Request) {
	expected, status, err := parseIfMatch(request)
	if err != nil {
		http.Error(response, err.Error(), status)
		return
	}

	var input createAnnotationRequest
	if status, err := decodeMutationJSON(request, &input); err != nil {
		http.Error(response, err.Error(), status)
		return
	}
	if err := annotation.ValidateDocumentPath(input.Document); err != nil {
		http.Error(response, "Markdown document not found", http.StatusNotFound)
		return
	}
	document, err := s.root.ReadFile(input.Document, maxDocumentBytes)
	if err != nil {
		s.writeAnnotationReadError(response, err)
		return
	}

	var source *annotation.Source
	var anchor *annotation.AnchorResult
	if input.Selection != nil {
		created, err := annotation.NewSource(document, input.Selection.StartByte, input.Selection.EndByte)
		if err != nil {
			http.Error(response, err.Error(), http.StatusBadRequest)
			return
		}
		if created.Selector.Exact != input.Selection.Exact {
			http.Error(response, "selected text no longer matches the Markdown source", http.StatusConflict)
			return
		}
		source = &created
		resolved, err := annotation.ResolveAnchor(document, created)
		if err != nil {
			http.Error(response, "could not resolve selected text", http.StatusInternalServerError)
			return
		}
		anchor = &resolved
	}

	now := time.Now().UTC()
	identifier, err := annotation.NewAnnotationID(now)
	if err != nil {
		http.Error(response, "could not generate annotation identifier", http.StatusInternalServerError)
		return
	}
	created := annotation.Annotation{
		ID:        identifier,
		Intent:    input.Intent,
		Status:    annotation.StatusOpen,
		Comment:   input.Comment,
		Author:    input.Author,
		CreatedAt: now,
		UpdatedAt: now,
		Source:    source,
		Thread:    []annotation.ThreadEntry{},
	}
	if err := created.Validate(); err != nil {
		http.Error(response, err.Error(), http.StatusBadRequest)
		return
	}

	sidecar, _, err := s.annotations.Load(input.Document)
	if err != nil {
		http.Error(response, "could not read annotations", http.StatusInternalServerError)
		return
	}
	sidecar.Annotations = append(sidecar.Annotations, created)
	revision, err := s.annotations.Save(sidecar, expected)
	if err != nil {
		if errors.Is(err, annotationstore.ErrConflict) {
			response.Header().Set("ETag", strconv.Quote(string(revision)))
			http.Error(response, "annotation sidecar revision conflict", http.StatusConflict)
			return
		}
		http.Error(response, "could not save annotation", http.StatusInternalServerError)
		return
	}

	view := annotationView{Annotation: created, Anchor: anchor}
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("ETag", strconv.Quote(string(revision)))
	response.Header().Set("Location", "/api/annotations/"+created.ID)
	response.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(response).Encode(createAnnotationResponse{
		Annotation: view,
		Revision:   string(revision),
	})
}

// handleReplyAnnotation appends one server-identified ordinary reply while
// preserving every existing thread entry and annotation lifecycle field.
func (s *Server) handleReplyAnnotation(response http.ResponseWriter, request *http.Request) {
	expected, status, err := parseIfMatch(request)
	if err != nil {
		http.Error(response, err.Error(), status)
		return
	}

	var input replyAnnotationRequest
	if status, err := decodeMutationJSON(request, &input); err != nil {
		http.Error(response, err.Error(), status)
		return
	}
	if err := annotation.ValidateDocumentPath(input.Document); err != nil {
		http.Error(response, "Markdown document not found", http.StatusNotFound)
		return
	}
	document, err := s.root.ReadFile(input.Document, maxDocumentBytes)
	if err != nil {
		s.writeAnnotationReadError(response, err)
		return
	}
	sidecar, _, err := s.annotations.Load(input.Document)
	if err != nil {
		http.Error(response, "could not read annotations", http.StatusInternalServerError)
		return
	}

	annotationIndex := findAnnotation(sidecar, request.PathValue("id"))
	if annotationIndex < 0 {
		http.Error(response, "annotation not found", http.StatusNotFound)
		return
	}
	now := time.Now().UTC()
	identifier, err := annotation.NewThreadID(now)
	if err != nil {
		http.Error(response, "could not generate reply identifier", http.StatusInternalServerError)
		return
	}
	reply := annotation.ThreadEntry{
		ID:        identifier,
		Kind:      annotation.ThreadReply,
		Message:   input.Message,
		Author:    input.Author,
		CreatedAt: now,
	}
	if err := reply.Validate(); err != nil {
		http.Error(response, err.Error(), http.StatusBadRequest)
		return
	}

	updated := &sidecar.Annotations[annotationIndex]
	updated.Thread = append(updated.Thread, reply)
	updated.UpdatedAt = now
	if err := sidecar.Validate(); err != nil {
		http.Error(response, "could not validate updated annotation", http.StatusInternalServerError)
		return
	}
	view, err := resolveAnnotationView(document, *updated)
	if err != nil {
		http.Error(response, "could not resolve annotation anchor", http.StatusInternalServerError)
		return
	}
	revision, err := s.annotations.Save(sidecar, expected)
	if err != nil {
		if errors.Is(err, annotationstore.ErrConflict) {
			response.Header().Set("ETag", strconv.Quote(string(revision)))
			http.Error(response, "annotation sidecar revision conflict", http.StatusConflict)
			return
		}
		http.Error(response, "could not save annotation reply", http.StatusInternalServerError)
		return
	}

	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("ETag", strconv.Quote(string(revision)))
	response.Header().Set("Location", "/api/annotations/"+updated.ID+"/replies/"+reply.ID)
	response.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(response).Encode(replyAnnotationResponse{
		Annotation: view,
		Revision:   string(revision),
	})
}

// handleTransitionAnnotation validates one actor-controlled lifecycle change
// and records its activity and status events in the same atomic sidecar save.
func (s *Server) handleTransitionAnnotation(response http.ResponseWriter, request *http.Request) {
	expected, status, err := parseIfMatch(request)
	if err != nil {
		http.Error(response, err.Error(), status)
		return
	}

	var input transitionAnnotationRequest
	if status, err := decodeMutationJSON(request, &input); err != nil {
		http.Error(response, err.Error(), status)
		return
	}
	if err := annotation.ValidateDocumentPath(input.Document); err != nil {
		http.Error(response, "Markdown document not found", http.StatusNotFound)
		return
	}
	document, err := s.root.ReadFile(input.Document, maxDocumentBytes)
	if err != nil {
		s.writeAnnotationReadError(response, err)
		return
	}
	sidecar, _, err := s.annotations.Load(input.Document)
	if err != nil {
		http.Error(response, "could not read annotations", http.StatusInternalServerError)
		return
	}

	annotationIndex := findAnnotation(sidecar, request.PathValue("id"))
	if annotationIndex < 0 {
		http.Error(response, "annotation not found", http.StatusNotFound)
		return
	}
	updated := &sidecar.Annotations[annotationIndex]
	if err := annotation.ValidateTransition(updated.Status, input.Status, input.ActorRole); err != nil {
		http.Error(response, err.Error(), http.StatusBadRequest)
		return
	}

	now := time.Now().UTC()
	if now.Before(updated.UpdatedAt) {
		// Preserve chronological thread ordering if the system clock moves
		// backwards or the sidecar came from a slightly faster clock.
		now = updated.UpdatedAt
	}
	entries, err := transitionEntries(*updated, input, now)
	if err != nil {
		if errors.Is(err, errTransitionIdentifier) {
			http.Error(response, "could not generate transition identifier", http.StatusInternalServerError)
			return
		}
		http.Error(response, err.Error(), http.StatusBadRequest)
		return
	}
	updated.Status = input.Status
	updated.Thread = append(updated.Thread, entries...)
	updated.UpdatedAt = now
	if err := sidecar.Validate(); err != nil {
		http.Error(response, "could not validate transitioned annotation", http.StatusInternalServerError)
		return
	}
	view, err := resolveAnnotationView(document, *updated)
	if err != nil {
		http.Error(response, "could not resolve annotation anchor", http.StatusInternalServerError)
		return
	}
	revision, err := s.annotations.Save(sidecar, expected)
	if err != nil {
		if errors.Is(err, annotationstore.ErrConflict) {
			response.Header().Set("ETag", strconv.Quote(string(revision)))
			http.Error(response, "annotation sidecar revision conflict", http.StatusConflict)
			return
		}
		http.Error(response, "could not save annotation transition", http.StatusInternalServerError)
		return
	}

	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("ETag", strconv.Quote(string(revision)))
	_ = json.NewEncoder(response).Encode(transitionAnnotationResponse{
		Annotation: view,
		Revision:   string(revision),
	})
}

// transitionEntries builds the immutable activity history required by one
// already-validated status transition, followed by its status-change event.
func transitionEntries(current annotation.Annotation, input transitionAnnotationRequest, now time.Time) ([]annotation.ThreadEntry, error) {
	if strings.TrimSpace(input.Author) == "" {
		return nil, errors.New("author is required")
	}
	entries := make([]annotation.ThreadEntry, 0, 2)
	activity := annotation.ThreadEntry{Author: input.Author, CreatedAt: now}
	switch input.Status {
	case annotation.StatusAcknowledged:
		if input.Message != "" || input.Summary != "" || input.Commit != "" {
			return nil, errors.New("acknowledgement does not accept message, summary, or commit")
		}
		activity.Kind = annotation.ThreadAcknowledgement
	case annotation.StatusApplied:
		if input.Message != "" {
			return nil, errors.New("applied transition does not accept message")
		}
		activity.Kind = annotation.ThreadResolution
		activity.Summary = input.Summary
		activity.Commit = input.Commit
	case annotation.StatusNeedsChanges:
		if input.Summary != "" || input.Commit != "" {
			return nil, errors.New("needs_changes transition does not accept summary or commit")
		}
		activity.Kind = annotation.ThreadReview
		activity.Message = input.Message
	case annotation.StatusRejected:
		if input.Summary != "" || input.Commit != "" {
			return nil, errors.New("rejected transition does not accept summary or commit")
		}
		activity.Kind = annotation.ThreadReply
		activity.Message = input.Message
	case annotation.StatusClosed, annotation.StatusOpen:
		if input.Message != "" || input.Summary != "" || input.Commit != "" {
			return nil, errors.New("status transition does not accept message, summary, or commit")
		}
	default:
		return nil, fmt.Errorf("unsupported transition target %q", input.Status)
	}

	if activity.Kind != "" {
		identifier, err := annotation.NewThreadID(now)
		if err != nil {
			return nil, fmt.Errorf("%w: activity: %v", errTransitionIdentifier, err)
		}
		activity.ID = identifier
		if err := activity.Validate(); err != nil {
			return nil, err
		}
		entries = append(entries, activity)
	}
	statusIdentifier, err := annotation.NewThreadID(now)
	if err != nil {
		return nil, fmt.Errorf("%w: status change: %v", errTransitionIdentifier, err)
	}
	statusChange := annotation.ThreadEntry{
		ID:         statusIdentifier,
		Kind:       annotation.ThreadStatusChange,
		Author:     input.Author,
		ActorRole:  input.ActorRole,
		FromStatus: current.Status,
		ToStatus:   input.Status,
		CreatedAt:  now,
	}
	if err := statusChange.Validate(); err != nil {
		return nil, err
	}
	return append(entries, statusChange), nil
}

// findAnnotation returns the index of identifier or -1 when the sidecar does
// not contain it.
func findAnnotation(sidecar annotation.Sidecar, identifier string) int {
	for index := range sidecar.Annotations {
		if sidecar.Annotations[index].ID == identifier {
			return index
		}
	}
	return -1
}

// resolveAnnotationView derives current anchor state without changing the
// persisted annotation. Document-level annotations have no derived anchor.
func resolveAnnotationView(document []byte, item annotation.Annotation) (annotationView, error) {
	view := annotationView{Annotation: item}
	if item.Source == nil {
		return view, nil
	}
	anchor, err := annotation.ResolveAnchor(document, *item.Source)
	if err != nil {
		return annotationView{}, err
	}
	view.Anchor = &anchor
	return view, nil
}

// parseIfMatch returns the lowercase sidecar revision carried by one strong
// If-Match entity tag. The empty entity tag represents a missing sidecar.
func parseIfMatch(request *http.Request) (annotationstore.Revision, int, error) {
	values := request.Header.Values("If-Match")
	if len(values) == 0 {
		return "", http.StatusPreconditionRequired, errors.New("If-Match is required")
	}
	if len(values) != 1 || strings.Contains(values[0], ",") || strings.HasPrefix(strings.TrimSpace(values[0]), "W/") {
		return "", http.StatusBadRequest, errors.New("If-Match must contain one strong revision ETag")
	}
	decoded, err := strconv.Unquote(strings.TrimSpace(values[0]))
	if err != nil {
		return "", http.StatusBadRequest, errors.New("If-Match must be a quoted revision ETag")
	}
	if decoded == "" {
		return "", 0, nil
	}
	digest, err := hex.DecodeString(decoded)
	if err != nil {
		return "", http.StatusBadRequest, errors.New("If-Match revision must be hexadecimal")
	}
	if len(digest) != 32 || decoded != strings.ToLower(decoded) {
		return "", http.StatusBadRequest, errors.New("If-Match revision must be 64 lowercase hexadecimal characters")
	}
	return annotationstore.Revision(decoded), 0, nil
}

// decodeMutationJSON decodes exactly one JSON value, rejects unknown fields,
// and translates the mutation middleware's body limit into HTTP status 413.
func decodeMutationJSON(request *http.Request, destination any) (int, error) {
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return http.StatusRequestEntityTooLarge, errors.New("request body is too large")
		}
		return http.StatusBadRequest, errors.New("request body must contain valid JSON")
	}
	err := decoder.Decode(&struct{}{})
	if err != nil {
		if errors.Is(err, io.EOF) {
			return 0, nil
		}
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return http.StatusRequestEntityTooLarge, errors.New("request body is too large")
		}
		return http.StatusBadRequest, errors.New("request body must contain one JSON value")
	}
	return http.StatusBadRequest, errors.New("request body must contain one JSON value")
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
