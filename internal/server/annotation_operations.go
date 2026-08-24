package server

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"atulm/code-annotator/internal/annotation"
	annotationstore "atulm/code-annotator/internal/annotation/store"
	"atulm/code-annotator/internal/content"
)

type annotationOperationErrorKind uint8

const (
	annotationOperationInvalid annotationOperationErrorKind = iota + 1
	annotationOperationNotFound
	annotationOperationConflict
	annotationOperationTooLarge
	annotationOperationInternal
)

// annotationOperationError describes a transport-neutral failure from shared
// annotation application logic. HTTP handlers decide how each kind is encoded.
type annotationOperationError struct {
	kind     annotationOperationErrorKind
	message  string
	revision annotationstore.Revision
	cause    error
}

func (e *annotationOperationError) Error() string {
	return e.message
}

func (e *annotationOperationError) Unwrap() error {
	return e.cause
}

type annotationDocumentResult struct {
	Document    content.Document
	Revision    annotationstore.Revision
	Annotations []resolvedAnnotation
}

type createAnnotationInput struct {
	Document         string
	Intent           annotation.Intent
	Comment          string
	Role             annotation.Role
	Selection        *annotationSelection
	ExpectedRevision annotationstore.Revision
}

type createAnnotationResult struct {
	Created  resolvedAnnotation
	Document annotationDocumentResult
}

// readAnnotationDocumentOperation returns the current resolved annotations for
// one cataloged document without knowing whether the caller needs JSON or HTML.
func (s *Server) readAnnotationDocumentOperation(documentPath string) (annotationDocumentResult, error) {
	document, catalogDocument, err := s.loadAnnotationSource(documentPath)
	if err != nil {
		return annotationDocumentResult{}, err
	}
	sidecar, revision, err := s.annotations.Load(documentPath)
	if err != nil {
		return annotationDocumentResult{}, operationError(annotationOperationInternal, "could not read annotations", err)
	}
	annotations, err := anchorAnnotations(document, sidecar.Annotations)
	if err != nil {
		return annotationDocumentResult{}, operationError(annotationOperationInternal, "could not resolve annotation anchor", err)
	}
	return annotationDocumentResult{Document: catalogDocument, Revision: revision, Annotations: annotations}, nil
}

// createAnnotationOperation validates browser-selected source bytes when the
// document revision matches. If it changed, the comment is still saved with a
// stale selection marker so the reviewer can reattach it without losing text.
func (s *Server) createAnnotationOperation(input createAnnotationInput) (createAnnotationResult, error) {
	document, catalogDocument, err := s.loadAnnotationSource(input.Document)
	if err != nil {
		return createAnnotationResult{}, err
	}

	var source *annotation.Source
	needsReattachment := false
	if input.Selection != nil {
		if !strings.EqualFold(input.Selection.DocumentSHA256, annotation.DocumentSHA256(document)) {
			needsReattachment = true
		} else {
			createdSource, err := annotation.NewSource(document, input.Selection.StartByte, input.Selection.EndByte)
			if err != nil {
				return createAnnotationResult{}, operationError(annotationOperationInvalid, err.Error(), err)
			}
			source = &createdSource
		}
	}

	now := time.Now().UTC()
	identifier, err := annotation.NewAnnotationID(now)
	if err != nil {
		return createAnnotationResult{}, operationError(annotationOperationInternal, "could not generate annotation identifier", err)
	}
	created := annotation.Annotation{
		ID: identifier, Intent: input.Intent, Status: annotation.StatusOpen,
		Comment: input.Comment, Role: input.Role, CreatedAt: now, UpdatedAt: now,
		Source: source, NeedsReattachment: needsReattachment,
		Thread: []annotation.ThreadEntry{},
	}
	if err := created.Validate(); err != nil {
		return createAnnotationResult{}, operationError(annotationOperationInvalid, err.Error(), err)
	}

	sidecar, _, err := s.annotations.Load(input.Document)
	if err != nil {
		return createAnnotationResult{}, operationError(annotationOperationInternal, "could not read annotations", err)
	}
	sidecar.Annotations = append(sidecar.Annotations, created)
	anchoredAnnotations, err := anchorAnnotations(document, sidecar.Annotations)
	if err != nil {
		return createAnnotationResult{}, operationError(annotationOperationInternal, "could not resolve annotation anchor", err)
	}
	revision, err := s.annotations.Save(sidecar, input.ExpectedRevision)
	if err != nil {
		if errors.Is(err, annotationstore.ErrConflict) {
			return createAnnotationResult{}, &annotationOperationError{kind: annotationOperationConflict, message: "annotation sidecar revision conflict", revision: revision, cause: err}
		}
		return createAnnotationResult{}, operationError(annotationOperationInternal, "could not save annotation", err)
	}

	return createAnnotationResult{
		Created: anchoredAnnotations[len(anchoredAnnotations)-1],
		Document: annotationDocumentResult{
			Document: catalogDocument, Revision: revision, Annotations: anchoredAnnotations,
		},
	}, nil
}

func (s *Server) loadAnnotationSource(documentPath string) ([]byte, content.Document, error) {
	if err := annotation.ValidateDocumentPath(documentPath); err != nil {
		return nil, content.Document{}, operationError(annotationOperationNotFound, "document not found", err)
	}
	index, err := s.root.IndexWithOptions(s.indexOptions)
	if err != nil {
		return nil, content.Document{}, operationError(annotationOperationInternal, "could not index documents", err)
	}
	catalogDocument, ok := findDocument(index, documentPath)
	if !ok {
		return nil, content.Document{}, operationError(annotationOperationNotFound, "document not found", nil)
	}
	document, err := s.root.ReadFile(documentPath, maxDocumentBytes)
	if err != nil {
		switch {
		case content.IsNotExist(err), errors.Is(err, content.ErrInvalidPath), errors.Is(err, content.ErrOutsideRoot), errors.Is(err, content.ErrNotRegular):
			return nil, content.Document{}, operationError(annotationOperationNotFound, "document not found", err)
		case errors.Is(err, content.ErrTooLarge):
			return nil, content.Document{}, operationError(annotationOperationTooLarge, "document is too large", err)
		default:
			return nil, content.Document{}, operationError(annotationOperationInternal, "could not read document", err)
		}
	}
	return document, catalogDocument, nil
}

func anchorAnnotations(document []byte, items []annotation.Annotation) ([]resolvedAnnotation, error) {
	result := make([]resolvedAnnotation, 0, len(items))
	for _, item := range items {
		view, err := resolveAnnotation(document, item)
		if err != nil {
			return nil, fmt.Errorf("annotation %q: %w", item.ID, err)
		}
		result = append(result, view)
	}
	return result, nil
}

func operationError(kind annotationOperationErrorKind, message string, cause error) error {
	return &annotationOperationError{kind: kind, message: message, cause: cause}
}
