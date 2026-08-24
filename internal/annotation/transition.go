package annotation

import (
	"errors"
	"fmt"
	"time"
)

// ErrTransitionIdentifier marks an internal failure to generate an immutable
// transition event identifier.
var ErrTransitionIdentifier = errors.New("generate transition identifier")

// TransitionInput contains the participant role and target-specific lifecycle data.
type TransitionInput struct {
	Status  Status
	Role    Role
	Message string
	Summary string
	Commit  string
}

// TransitionEntries validates one lifecycle change and creates its immutable
// activity followed by a status-change event.
func TransitionEntries(current Annotation, input TransitionInput, now time.Time) ([]ThreadEntry, error) {
	if err := ValidateTransition(current.Status, input.Status, input.Role); err != nil {
		return nil, err
	}
	entries := make([]ThreadEntry, 0, 2)
	activity := ThreadEntry{Role: input.Role, CreatedAt: now}
	switch input.Status {
	case StatusAcknowledged:
		if input.Message != "" || input.Summary != "" || input.Commit != "" {
			return nil, errors.New("acknowledgement does not accept message, summary, or commit")
		}
		activity.Kind = ThreadAcknowledgement
	case StatusApplied:
		if input.Message != "" {
			return nil, errors.New("applied transition does not accept message")
		}
		activity.Kind = ThreadResolution
		activity.Summary = input.Summary
		activity.Commit = input.Commit
	case StatusNeedsChanges:
		if input.Summary != "" || input.Commit != "" {
			return nil, errors.New("needs_changes transition does not accept summary or commit")
		}
		activity.Kind = ThreadReview
		activity.Message = input.Message
	case StatusRejected:
		if input.Summary != "" || input.Commit != "" {
			return nil, errors.New("rejected transition does not accept summary or commit")
		}
		activity.Kind = ThreadReply
		activity.Message = input.Message
	case StatusClosed, StatusOpen:
		if input.Message != "" || input.Summary != "" || input.Commit != "" {
			return nil, errors.New("status transition does not accept message, summary, or commit")
		}
	default:
		return nil, fmt.Errorf("unsupported transition target %q", input.Status)
	}

	if activity.Kind != "" {
		identifier, err := NewThreadID(now)
		if err != nil {
			return nil, fmt.Errorf("%w: activity: %v", ErrTransitionIdentifier, err)
		}
		activity.ID = identifier
		if err := activity.Validate(); err != nil {
			return nil, err
		}
		entries = append(entries, activity)
	}
	statusIdentifier, err := NewThreadID(now)
	if err != nil {
		return nil, fmt.Errorf("%w: status change: %v", ErrTransitionIdentifier, err)
	}
	statusChange := ThreadEntry{ID: statusIdentifier, Kind: ThreadStatusChange, Role: input.Role, FromStatus: current.Status, ToStatus: input.Status, CreatedAt: now}
	if err := statusChange.Validate(); err != nil {
		return nil, err
	}
	return append(entries, statusChange), nil
}
