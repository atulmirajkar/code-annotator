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
// Document-level annotations have no anchor.
type resolvedAnnotation struct {
	// Annotation is the validated record loaded from the document sidecar.
	annotation.Annotation
	// Anchor is resolved against current document bytes and is nil for
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
	source, catalogDocument, ok := s.readAnnotationDocument(response, document)
	if !ok {
		return
	}
	sidecar, revision, err := s.annotations.Load(document)
	if err != nil {
		http.Error(response, "could not read annotations", http.StatusInternalServerError)
		return
	}

	annotations := make([]resolvedAnnotation, 0, len(sidecar.Annotations))
	for _, item := range sidecar.Annotations {
		view, err := resolveAnnotation(source, item)
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
		Kind:          catalogDocument.Kind,
		Language:      catalogDocument.Language,
		Revision:      string(revision),
		Annotations:   annotations,
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
	document, _, ok := s.readAnnotationDocument(response, input.Document)
	if !ok {
		return
	}

	var source *annotation.Source
	var anchor *annotation.AnchorResult
	if input.Selection != nil {
		if !strings.EqualFold(input.Selection.DocumentSHA256, annotation.DocumentSHA256(document)) {
			http.Error(response, "document changed; refresh and select again", http.StatusConflict)
			return
		}
		created, err := annotation.NewSource(document, input.Selection.StartByte, input.Selection.EndByte)
		if err != nil {
			http.Error(response, err.Error(), http.StatusBadRequest)
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
		Role:      input.Role,
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

	view := resolvedAnnotation{Annotation: created, Anchor: anchor}
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
	document, _, ok := s.readAnnotationDocument(response, input.Document)
	if !ok {
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
		Role:      input.Role,
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
	view, err := resolveAnnotation(document, *updated)
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
	document, _, ok := s.readAnnotationDocument(response, input.Document)
	if !ok {
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
	if err := annotation.ValidateTransition(updated.Status, input.Status, input.Role); err != nil {
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
		if errors.Is(err, annotation.ErrTransitionIdentifier) {
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
	view, err := resolveAnnotation(document, *updated)
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
	document, _, ok := s.readAnnotationDocument(response, input.Document)
	if !ok {
		return
	}
	if !strings.EqualFold(input.Selection.DocumentSHA256, annotation.DocumentSHA256(document)) {
		http.Error(response, "document changed; refresh and select again", http.StatusConflict)
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
	if updated.Source == nil {
		http.Error(response, "document-level annotation cannot be reattached", http.StatusConflict)
		return
	}
	oldAnchor, err := annotation.ResolveAnchor(document, *updated.Source)
	if err != nil {
		http.Error(response, "could not resolve annotation anchor", http.StatusInternalServerError)
		return
	}
	if oldAnchor.State != annotation.AnchorStale {
		http.Error(response, "annotation anchor is not stale", http.StatusConflict)
		return
	}

	replacement, err := annotation.NewSource(document, input.Selection.StartByte, input.Selection.EndByte)
	if err != nil {
		http.Error(response, err.Error(), http.StatusBadRequest)
		return
	}
	newAnchor, err := annotation.ResolveAnchor(document, replacement)
	if err != nil {
		http.Error(response, "could not resolve replacement anchor", http.StatusInternalServerError)
		return
	}

	now := time.Now().UTC()
	if now.Before(updated.UpdatedAt) {
		now = updated.UpdatedAt
	}
	updated.Source = &replacement
	updated.UpdatedAt = now
	if err := sidecar.Validate(); err != nil {
		http.Error(response, "could not validate reattached annotation", http.StatusInternalServerError)
		return
	}
	revision, err := s.annotations.Save(sidecar, expected)
	if err != nil {
		if errors.Is(err, annotationstore.ErrConflict) {
			response.Header().Set("ETag", strconv.Quote(string(revision)))
			http.Error(response, "annotation sidecar revision conflict", http.StatusConflict)
			return
		}
		http.Error(response, "could not save annotation reattachment", http.StatusInternalServerError)
		return
	}

	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("ETag", strconv.Quote(string(revision)))
	_ = json.NewEncoder(response).Encode(reattachAnnotationResponse{
		Annotation: resolvedAnnotation{Annotation: *updated, Anchor: &newAnchor},
		Revision:   string(revision),
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
// persisted annotation. Document-level annotations have no derived anchor.
func resolveAnnotation(document []byte, item annotation.Annotation) (resolvedAnnotation, error) {
	view := resolvedAnnotation{Annotation: item}
	if item.Source == nil {
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

// readAnnotationDocument requires both a safe annotation path and membership
// in the configured reviewable catalog before reading current bytes.
func (s *Server) readAnnotationDocument(response http.ResponseWriter, documentPath string) ([]byte, content.Document, bool) {
	if err := annotation.ValidateDocumentPath(documentPath); err != nil {
		http.Error(response, "document not found", http.StatusNotFound)
		return nil, content.Document{}, false
	}
	index, err := s.root.IndexWithOptions(s.indexOptions)
	if err != nil {
		http.Error(response, "could not index documents", http.StatusInternalServerError)
		return nil, content.Document{}, false
	}
	catalogDocument, ok := findDocument(index, documentPath)
	if !ok {
		http.Error(response, "document not found", http.StatusNotFound)
		return nil, content.Document{}, false
	}
	document, err := s.root.ReadFile(documentPath, maxDocumentBytes)
	if err != nil {
		s.writeAnnotationReadError(response, err)
		return nil, content.Document{}, false
	}
	return document, catalogDocument, true
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
