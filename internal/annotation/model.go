// Package annotation defines the persistent review annotation domain model.
package annotation

import (
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"
)

// SchemaVersion is the sidecar schema version supported by this package.
const SchemaVersion = 1

// Intent describes what kind of response a reviewer expects.
type Intent string

const (
	IntentQuestion      Intent = "question"
	IntentSuggestion    Intent = "suggestion"
	IntentChangeRequest Intent = "change_request"
	IntentApproval      Intent = "approval"
)

// Status describes the current annotation lifecycle state.
type Status string

const (
	StatusOpen         Status = "open"
	StatusAcknowledged Status = "acknowledged"
	StatusApplied      Status = "applied"
	StatusNeedsChanges Status = "needs_changes"
	StatusClosed       Status = "closed"
	StatusRejected     Status = "rejected"
)

// ActorRole controls which lifecycle transitions an actor may perform.
type ActorRole string

const (
	RoleAgent    ActorRole = "agent"
	RoleReviewer ActorRole = "reviewer"
)

// ThreadKind describes an append-only event in an annotation discussion.
type ThreadKind string

const (
	ThreadReply           ThreadKind = "reply"
	ThreadAcknowledgement ThreadKind = "acknowledgement"
	ThreadResolution      ThreadKind = "resolution"
	ThreadReview          ThreadKind = "review"
	ThreadStatusChange    ThreadKind = "status_change"
)

// Sidecar contains all annotations for one Markdown document.
type Sidecar struct {
	SchemaVersion int          `json:"schemaVersion"`
	Document      string       `json:"document"`
	Annotations   []Annotation `json:"annotations"`
}

// Annotation is a review request and its durable discussion thread. Source is
// nil for a document-level annotation.
type Annotation struct {
	ID        string        `json:"id"`
	Intent    Intent        `json:"intent"`
	Status    Status        `json:"status"`
	Comment   string        `json:"comment"`
	Author    string        `json:"author"`
	CreatedAt time.Time     `json:"createdAt"`
	UpdatedAt time.Time     `json:"updatedAt"`
	Source    *Source       `json:"source,omitempty"`
	Thread    []ThreadEntry `json:"thread"`
}

// Source identifies the document revision and selected Markdown source text.
type Source struct {
	SHA256   string   `json:"sha256"`
	Selector Selector `json:"selector"`
}

// Selector combines quote, byte-position, and line-position selectors. Byte
// positions are offsets into the UTF-8 Markdown source and EndByte is exclusive.
type Selector struct {
	Exact     string `json:"exact"`
	Prefix    string `json:"prefix,omitempty"`
	Suffix    string `json:"suffix,omitempty"`
	StartByte int    `json:"startByte"`
	EndByte   int    `json:"endByte"`
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
}

// ThreadEntry records a reply, acknowledgement, implementation attempt, review,
// or status event. Existing entries are immutable after persistence.
type ThreadEntry struct {
	ID         string     `json:"id"`
	Kind       ThreadKind `json:"kind"`
	Message    string     `json:"message,omitempty"`
	Summary    string     `json:"summary,omitempty"`
	Commit     string     `json:"commit,omitempty"`
	Author     string     `json:"author"`
	ActorRole  ActorRole  `json:"actorRole,omitempty"`
	FromStatus Status     `json:"fromStatus,omitempty"`
	ToStatus   Status     `json:"toStatus,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
}

// Validate checks a complete sidecar against schema version 1.
func (s Sidecar) Validate() error {
	if s.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported annotation schema version %d", s.SchemaVersion)
	}
	if err := validateDocumentPath(s.Document); err != nil {
		return err
	}

	annotationIDs := make(map[string]struct{}, len(s.Annotations))
	threadIDs := make(map[string]struct{})
	for index, item := range s.Annotations {
		if err := item.Validate(); err != nil {
			return fmt.Errorf("annotation %d: %w", index, err)
		}
		if _, exists := annotationIDs[item.ID]; exists {
			return fmt.Errorf("annotation %d: duplicate id %q", index, item.ID)
		}
		annotationIDs[item.ID] = struct{}{}
		for _, entry := range item.Thread {
			if _, exists := threadIDs[entry.ID]; exists {
				return fmt.Errorf("annotation %q: duplicate thread id %q", item.ID, entry.ID)
			}
			threadIDs[entry.ID] = struct{}{}
		}
	}
	return nil
}

// Validate checks an annotation independently of its containing sidecar.
func (a Annotation) Validate() error {
	if err := validateIdentifier(a.ID, "ann_"); err != nil {
		return fmt.Errorf("id: %w", err)
	}
	if !a.Intent.Valid() {
		return fmt.Errorf("invalid intent %q", a.Intent)
	}
	if !a.Status.Valid() {
		return fmt.Errorf("invalid status %q", a.Status)
	}
	if strings.TrimSpace(a.Comment) == "" {
		return errors.New("comment is required")
	}
	if strings.TrimSpace(a.Author) == "" {
		return errors.New("author is required")
	}
	if a.CreatedAt.IsZero() || a.UpdatedAt.IsZero() {
		return errors.New("createdAt and updatedAt are required")
	}
	if a.UpdatedAt.Before(a.CreatedAt) {
		return errors.New("updatedAt cannot precede createdAt")
	}
	if a.Source != nil {
		if err := a.Source.Validate(); err != nil {
			return fmt.Errorf("source: %w", err)
		}
	}
	previousTime := a.CreatedAt
	for index, entry := range a.Thread {
		if err := entry.Validate(); err != nil {
			return fmt.Errorf("thread %d: %w", index, err)
		}
		if entry.CreatedAt.Before(previousTime) {
			return fmt.Errorf("thread %d: entries are not in chronological order", index)
		}
		if entry.CreatedAt.After(a.UpdatedAt) {
			return fmt.Errorf("thread %d: createdAt cannot follow annotation updatedAt", index)
		}
		previousTime = entry.CreatedAt
	}
	return nil
}

// Validate checks a source selector and its SHA-256 revision digest.
func (s Source) Validate() error {
	digest, err := hex.DecodeString(s.SHA256)
	if err != nil || len(digest) != 32 {
		return errors.New("sha256 must be a 64-character hexadecimal digest")
	}
	if strings.TrimSpace(s.Selector.Exact) == "" {
		return errors.New("selector exact text is required")
	}
	if s.Selector.StartByte < 0 || s.Selector.EndByte <= s.Selector.StartByte {
		return errors.New("selector byte range is invalid")
	}
	if s.Selector.StartLine < 1 || s.Selector.EndLine < s.Selector.StartLine {
		return errors.New("selector line range is invalid")
	}
	return nil
}

// Validate checks a thread event independently of its annotation.
func (e ThreadEntry) Validate() error {
	if err := validateIdentifier(e.ID, "msg_"); err != nil {
		return fmt.Errorf("id: %w", err)
	}
	if !e.Kind.Valid() {
		return fmt.Errorf("invalid kind %q", e.Kind)
	}
	if strings.TrimSpace(e.Author) == "" {
		return errors.New("author is required")
	}
	if e.CreatedAt.IsZero() {
		return errors.New("createdAt is required")
	}
	switch e.Kind {
	case ThreadReply, ThreadReview:
		if strings.TrimSpace(e.Message) == "" {
			return errors.New("message is required")
		}
	case ThreadResolution:
		if strings.TrimSpace(e.Summary) == "" {
			return errors.New("summary is required")
		}
	case ThreadStatusChange:
		if err := ValidateTransition(e.FromStatus, e.ToStatus, e.ActorRole); err != nil {
			return fmt.Errorf("status change: %w", err)
		}
	}
	return nil
}

// Valid reports whether i is a supported intent.
func (i Intent) Valid() bool {
	switch i {
	case IntentQuestion, IntentSuggestion, IntentChangeRequest, IntentApproval:
		return true
	default:
		return false
	}
}

// Valid reports whether s is a supported status.
func (s Status) Valid() bool {
	switch s {
	case StatusOpen, StatusAcknowledged, StatusApplied, StatusNeedsChanges, StatusClosed, StatusRejected:
		return true
	default:
		return false
	}
}

// Valid reports whether k is a supported thread event kind.
func (k ThreadKind) Valid() bool {
	switch k {
	case ThreadReply, ThreadAcknowledgement, ThreadResolution, ThreadReview, ThreadStatusChange:
		return true
	default:
		return false
	}
}

// ValidateTransition checks whether actor may move an annotation between two
// lifecycle states. Only reviewers may close or return applied work for changes.
func ValidateTransition(from, to Status, actor ActorRole) error {
	if !from.Valid() || !to.Valid() {
		return fmt.Errorf("invalid annotation transition %q -> %q", from, to)
	}
	if actor != RoleAgent && actor != RoleReviewer {
		return fmt.Errorf("invalid annotation actor role %q", actor)
	}
	if from == to {
		return fmt.Errorf("annotation is already %q", to)
	}

	allowed := false
	switch actor {
	case RoleAgent:
		allowed = (from == StatusOpen && (to == StatusAcknowledged || to == StatusRejected)) ||
			(from == StatusAcknowledged && (to == StatusApplied || to == StatusRejected)) ||
			(from == StatusNeedsChanges && to == StatusAcknowledged)
	case RoleReviewer:
		allowed = (from == StatusApplied && (to == StatusClosed || to == StatusNeedsChanges)) ||
			((from == StatusClosed || from == StatusRejected) && to == StatusOpen)
	}
	if !allowed {
		return fmt.Errorf("role %q cannot transition annotation from %q to %q", actor, from, to)
	}
	return nil
}

func validateDocumentPath(document string) error {
	if document == "" || strings.ContainsRune(document, '\x00') || strings.Contains(document, `\`) || strings.HasPrefix(document, "/") {
		return errors.New("document must be a non-empty slash-separated relative path")
	}
	cleaned := path.Clean(document)
	if cleaned != document || cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return errors.New("document path is not clean or escapes the root")
	}
	if !strings.EqualFold(path.Ext(document), ".md") {
		return errors.New("document must have a .md extension")
	}
	return nil
}

func validateIdentifier(identifier, prefix string) error {
	if !strings.HasPrefix(identifier, prefix) || len(identifier) == len(prefix) {
		return fmt.Errorf("must start with %q and include a value", prefix)
	}
	for _, character := range identifier[len(prefix):] {
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') && character != '_' && character != '-' {
			return errors.New("contains unsupported characters")
		}
	}
	return nil
}
