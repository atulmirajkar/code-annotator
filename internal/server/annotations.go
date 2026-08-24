package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"atulm/code-annotator/internal/annotation"
	annotationstore "atulm/code-annotator/internal/annotation/store"
	"atulm/code-annotator/internal/content"
)

// resolvedAnnotation combines persisted annotation data with the anchor
// location derived from the current document bytes. It is shared by API
// responses and presentation-model construction; it is not an HTML view model.
// Document-level annotations have no anchor. A selection saved after its
// document changed has a synthetic stale anchor until it is reattached.
type resolvedAnnotation struct {
	// Annotation is the validated record loaded from the document sidecar.
	annotation.Annotation
	// Anchor is resolved against current document bytes and is nil only for
	// document-level annotations.
	Anchor *annotation.AnchorResult `json:"anchor,omitempty"`
}

// annotationListResponse is the wire representation returned by the read API.
// Revision is also emitted as the HTTP ETag for later optimistic mutations.
type annotationListResponse struct {
	SchemaVersion int                  `json:"schemaVersion"`
	Document      string               `json:"document"`
	Kind          content.Kind         `json:"kind"`
	Language      string               `json:"language"`
	Revision      string               `json:"revision"`
	Annotations   []resolvedAnnotation `json:"annotations"`
}

// annotationQueueResponse groups actionable annotations by document. Each
// document retains its own revision because mutations use sidecar-scoped
// optimistic concurrency.
type annotationQueueResponse struct {
	SchemaVersion int                      `json:"schemaVersion"`
	Documents     []annotationListResponse `json:"documents"`
}

// createAnnotationRequest contains only reviewer-controlled fields. Lifecycle,
// identifiers, timestamps, and source hashes are assigned by the server.
type createAnnotationRequest struct {
	Document  string               `json:"document"`
	Intent    annotation.Intent    `json:"intent"`
	Comment   string               `json:"comment"`
	Role      annotation.Role      `json:"role"`
	Selection *annotationSelection `json:"selection,omitempty"`
}

// annotationSelection carries a document byte range bound to the exact source
// revision rendered by the browser. The server derives the quote itself.
type annotationSelection struct {
	StartByte      int    `json:"startByte"`
	EndByte        int    `json:"endByte"`
	DocumentSHA256 string `json:"documentSHA256"`
}

// createAnnotationResponse returns the created annotation and the sidecar
// revision required by the caller's next mutation.
type createAnnotationResponse struct {
	Annotation resolvedAnnotation `json:"annotation"`
	Revision   string             `json:"revision"`
}

// replyAnnotationRequest contains reviewer- or agent-attributed content for an
// ordinary discussion reply. Structured lifecycle events use transition APIs.
type replyAnnotationRequest struct {
	Document string          `json:"document"`
	Message  string          `json:"message"`
	Role     annotation.Role `json:"role"`
}

// replyAnnotationResponse returns the updated annotation and sidecar revision.
type replyAnnotationResponse struct {
	Annotation resolvedAnnotation `json:"annotation"`
	Revision   string             `json:"revision"`
}

// transitionAnnotationRequest describes one lifecycle transition and any
// activity content required for that transition.
type transitionAnnotationRequest struct {
	Document string            `json:"document"`
	Status   annotation.Status `json:"status"`
	Role     annotation.Role   `json:"role"`
	Message  string            `json:"message,omitempty"`
	Summary  string            `json:"summary,omitempty"`
	Commit   string            `json:"commit,omitempty"`
}

// transitionAnnotationResponse returns the transitioned annotation and the new
// sidecar revision.
type transitionAnnotationResponse struct {
	Annotation resolvedAnnotation `json:"annotation"`
	Revision   string             `json:"revision"`
}

// reattachAnnotationRequest identifies a new verified source range for an
// annotation whose previous source anchor is stale.
type reattachAnnotationRequest struct {
	Document  string              `json:"document"`
	Selection annotationSelection `json:"selection"`
}

// reattachAnnotationResponse returns the newly anchored annotation and sidecar
// revision.
type reattachAnnotationResponse struct {
	Annotation resolvedAnnotation `json:"annotation"`
	Revision   string             `json:"revision"`
}

// handleAnnotations returns persisted annotations plus anchor locations derived
// from the current document bytes. It never mutates either root.
func (s *Server) handleAnnotations(response http.ResponseWriter, request *http.Request) {
	document := request.URL.Query().Get("document")
	if document == "" {
		s.handleAnnotationQueue(response, request)
		return
	}
	result, err := s.readAnnotationDocumentOperation(document)
	if err != nil {
		writeAnnotationOperationError(response, err, false)
		return
	}

	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("ETag", strconv.Quote(string(result.Revision)))
	payload := annotationListResponse{
		SchemaVersion: annotation.SchemaVersion,
		Document:      document,
		Kind:          result.Document.Kind,
		Language:      result.Document.Language,
		Revision:      string(result.Revision),
		Annotations:   result.Annotations,
	}
	if err := json.NewEncoder(response).Encode(payload); err != nil {
		// The response may already be committed; structured request logging will
		// report this error when it is introduced.
		return
	}
}

// queueCandidate is a document with at least one status-matching annotation,
// known cheaply from its sidecar alone before paying for a source read and
// anchor resolution.
type queueCandidate struct {
	document content.Document
	sidecar  annotation.Sidecar
	revision annotationstore.Revision
}

// handleAnnotationQueue returns annotations across the stable content index.
// It supports conditional GET: the ETag is derived from the cheap candidate
// list below, so a matching If-None-Match short-circuits before any document
// source is read or any anchor is resolved, not just before the response is
// written.
func (s *Server) handleAnnotationQueue(response http.ResponseWriter, request *http.Request) {
	rawStatus := request.URL.Query().Get("status")
	statuses, err := parseAnnotationStatuses(rawStatus)
	if err != nil {
		http.Error(response, err.Error(), http.StatusBadRequest)
		return
	}
	index, err := s.root.IndexWithOptions(s.indexOptions)
	if err != nil {
		http.Error(response, "could not index documents", http.StatusInternalServerError)
		return
	}

	var candidates []queueCandidate
	for _, document := range index.Documents {
		sidecar, revision, err := s.annotations.Load(document.Path)
		if err != nil {
			http.Error(response, "could not read annotations", http.StatusInternalServerError)
			return
		}
		if !hasMatchingStatus(sidecar.Annotations, statuses) {
			continue
		}
		candidates = append(candidates, queueCandidate{document: document, sidecar: sidecar, revision: revision})
	}

	etag := queueETag(rawStatus, candidates)
	response.Header().Set("ETag", strconv.Quote(etag))
	if matched, ok := parseIfNoneMatch(request); ok && matched == etag {
		response.WriteHeader(http.StatusNotModified)
		return
	}

	payload := annotationQueueResponse{SchemaVersion: annotation.SchemaVersion, Documents: []annotationListResponse{}}
	for _, candidate := range candidates {
		source, err := s.root.ReadFile(candidate.document.Path, maxDocumentBytes)
		if err != nil {
			s.writeAnnotationReadError(response, err)
			return
		}
		views := make([]resolvedAnnotation, 0, len(candidate.sidecar.Annotations))
		for _, item := range candidate.sidecar.Annotations {
			if len(statuses) > 0 {
				if _, wanted := statuses[item.Status]; !wanted {
					continue
				}
			}
			view, err := resolveAnnotation(source, item)
			if err != nil {
				http.Error(response, "could not resolve annotation anchor", http.StatusInternalServerError)
				return
			}
			views = append(views, view)
		}
		if len(views) == 0 {
			continue
		}
		payload.Documents = append(payload.Documents, annotationListResponse{
			SchemaVersion: candidate.sidecar.SchemaVersion,
			Document:      candidate.sidecar.Document,
			Kind:          candidate.document.Kind,
			Language:      candidate.document.Language,
			Revision:      string(candidate.revision),
			Annotations:   views,
		})
	}

	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(response).Encode(payload)
}

// hasMatchingStatus reports whether any annotation qualifies for the queue
// filter, without touching document source or anchors.
func hasMatchingStatus(annotations []annotation.Annotation, statuses map[annotation.Status]struct{}) bool {
	if len(statuses) == 0 {
		return len(annotations) > 0
	}
	for _, item := range annotations {
		if _, wanted := statuses[item.Status]; wanted {
			return true
		}
	}
	return false
}

// queueETag summarizes exactly the state a matching If-None-Match needs to
// reproduce: the status filter plus every candidate document's own revision.
// Sidecar content and anchors never factor in directly, since a per-document
// revision already changes whenever its sidecar does.
func queueETag(rawStatus string, candidates []queueCandidate) string {
	hash := sha256.New()
	hash.Write([]byte(rawStatus))
	hash.Write([]byte{0})
	for _, candidate := range candidates {
		hash.Write([]byte(candidate.document.Path))
		hash.Write([]byte{0})
		hash.Write([]byte(candidate.revision))
		hash.Write([]byte{'\n'})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// parseIfNoneMatch reads an optional single strong ETag for conditional GET.
// Unlike parseIfMatch, malformed input is not an error: If-None-Match is an
// optimization, not a required mutation precondition, so anything that isn't
// a clean match just falls through to an ordinary response.
func parseIfNoneMatch(request *http.Request) (string, bool) {
	values := request.Header.Values("If-None-Match")
	if len(values) != 1 || strings.Contains(values[0], ",") || strings.HasPrefix(strings.TrimSpace(values[0]), "W/") {
		return "", false
	}
	decoded, err := strconv.Unquote(strings.TrimSpace(values[0]))
	if err != nil {
		return "", false
	}
	digest, err := hex.DecodeString(decoded)
	if err != nil || len(digest) != sha256.Size || decoded != strings.ToLower(decoded) {
		return "", false
	}
	return decoded, true
}

// parseAnnotationStatuses validates the optional comma-separated queue filter.
func parseAnnotationStatuses(value string) (map[annotation.Status]struct{}, error) {
	result := make(map[annotation.Status]struct{})
	if strings.TrimSpace(value) == "" {
		return result, nil
	}
	for _, raw := range strings.Split(value, ",") {
		status := annotation.Status(strings.TrimSpace(raw))
		if !status.Valid() {
			return nil, fmt.Errorf("invalid annotation status %q", raw)
		}
		result[status] = struct{}{}
	}
	return result, nil
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
	result, err := s.createAnnotationOperation(createAnnotationInput{
		Document: input.Document, Intent: input.Intent, Comment: input.Comment,
		Role: input.Role, Selection: input.Selection, ExpectedRevision: expected,
	})
	if err != nil {
		writeAnnotationOperationError(response, err, false)
		return
	}

	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("ETag", strconv.Quote(string(result.Document.Revision)))
	response.Header().Set("Location", "/api/annotations/"+result.Created.ID)
	response.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(response).Encode(createAnnotationResponse{
		Annotation: result.Created,
		Revision:   string(result.Document.Revision),
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
	result, err := s.replyAnnotationOperation(replyAnnotationInput{
		Document: input.Document, AnnotationID: request.PathValue("id"), Message: input.Message,
		Role: input.Role, ExpectedRevision: expected,
	})
	if err != nil {
		writeAnnotationOperationError(response, err, false)
		return
	}

	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("ETag", strconv.Quote(string(result.Document.Revision)))
	response.Header().Set("Location", "/api/annotations/"+result.Updated.ID+"/replies/"+result.Reply.ID)
	response.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(response).Encode(replyAnnotationResponse{
		Annotation: result.Updated,
		Revision:   string(result.Document.Revision),
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
	result, err := s.transitionAnnotationOperation(transitionAnnotationInput{
		Document: input.Document, AnnotationID: request.PathValue("id"), Status: input.Status,
		Role: input.Role, Message: input.Message, Summary: input.Summary, Commit: input.Commit,
		ExpectedRevision: expected,
	})
	if err != nil {
		writeAnnotationOperationError(response, err, false)
		return
	}

	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("ETag", strconv.Quote(string(result.Document.Revision)))
	_ = json.NewEncoder(response).Encode(transitionAnnotationResponse{
		Annotation: result.Updated,
		Revision:   string(result.Document.Revision),
	})
}

// handleReattachAnnotation replaces a stale text selector with a range verified
// against the current document source. It cannot convert document annotations.
func (s *Server) handleReattachAnnotation(response http.ResponseWriter, request *http.Request) {
	expected, status, err := parseIfMatch(request)
	if err != nil {
		http.Error(response, err.Error(), status)
		return
	}

	var input reattachAnnotationRequest
	if status, err := decodeMutationJSON(request, &input); err != nil {
		http.Error(response, err.Error(), status)
		return
	}
	result, err := s.reattachAnnotationOperation(reattachAnnotationInput{
		Document: input.Document, AnnotationID: request.PathValue("id"), Selection: input.Selection,
		ExpectedRevision: expected,
	})
	if err != nil {
		writeAnnotationOperationError(response, err, false)
		return
	}

	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("ETag", strconv.Quote(string(result.Document.Revision)))
	_ = json.NewEncoder(response).Encode(reattachAnnotationResponse{
		Annotation: result.Updated,
		Revision:   string(result.Document.Revision),
	})
}

// transitionEntries builds the immutable activity history required by one
// already-validated status transition, followed by its status-change event.
func transitionEntries(current annotation.Annotation, input transitionAnnotationRequest, now time.Time) ([]annotation.ThreadEntry, error) {
	return annotation.TransitionEntries(current, annotation.TransitionInput{Status: input.Status, Role: input.Role, Message: input.Message, Summary: input.Summary, Commit: input.Commit}, now)
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

// resolveAnnotation derives current anchor state without changing the
// persisted annotation. Document-level annotations have no derived anchor;
// selections awaiting reattachment expose a synthetic stale anchor.
func resolveAnnotation(document []byte, item annotation.Annotation) (resolvedAnnotation, error) {
	view := resolvedAnnotation{Annotation: item}
	if item.Source == nil {
		if item.NeedsReattachment {
			view.Anchor = &annotation.AnchorResult{State: annotation.AnchorStale, Reason: annotation.StaleDocumentChanged}
		}
		return view, nil
	}
	anchor, err := annotation.ResolveAnchor(document, *item.Source)
	if err != nil {
		return resolvedAnnotation{}, err
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

func writeAnnotationOperationError(response http.ResponseWriter, err error, formRequest bool) {
	var operationErr *annotationOperationError
	if !errors.As(err, &operationErr) {
		http.Error(response, "could not process annotation", http.StatusInternalServerError)
		return
	}
	status := http.StatusInternalServerError
	switch operationErr.kind {
	case annotationOperationInvalid:
		status = http.StatusBadRequest
		if formRequest {
			status = http.StatusUnprocessableEntity
		}
	case annotationOperationNotFound:
		status = http.StatusNotFound
	case annotationOperationConflict:
		status = http.StatusConflict
	case annotationOperationTooLarge:
		status = http.StatusRequestEntityTooLarge
	case annotationOperationInternal:
		status = http.StatusInternalServerError
	}
	if operationErr.revision != "" {
		response.Header().Set("ETag", strconv.Quote(string(operationErr.revision)))
	}
	http.Error(response, operationErr.message, status)
}

// writeAnnotationReadError hides filesystem details and treats invalid or
// unavailable document paths as missing resources.
func (s *Server) writeAnnotationReadError(response http.ResponseWriter, err error) {
	if content.IsNotExist(err) || errors.Is(err, content.ErrInvalidPath) || errors.Is(err, content.ErrOutsideRoot) || errors.Is(err, content.ErrNotRegular) {
		http.Error(response, "document not found", http.StatusNotFound)
		return
	}
	if errors.Is(err, content.ErrTooLarge) {
		http.Error(response, "document is too large", http.StatusRequestEntityTooLarge)
		return
	}
	http.Error(response, "could not read document", http.StatusInternalServerError)
}
