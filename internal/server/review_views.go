package server

import (
	"fmt"
	"net/url"
	"strings"

	"atulm/code-annotator/internal/annotation"
)

// annotationPanelView is the presentation-only model for the replaceable
// annotation panel fragment. It contains no request or storage dependencies.
type annotationPanelView struct {
	// Document is the canonical path submitted by every mutation form.
	Document string
	// Revision is the sidecar revision used for optimistic concurrency.
	Revision string
	// ShowInactive records whether closed and rejected cards are included.
	ShowInactive bool
	// CountLabel is the ready-to-display active and total count summary.
	CountLabel string
	// EmptyMessage distinguishes an empty document from an active-only filter.
	EmptyMessage string
	// Feedback is an escaped mutation result shown above the authoritative list.
	Feedback     string
	FeedbackKind string
	// Cards contains only annotations visible under ShowInactive.
	Cards []annotationCardView
}

// annotationCardView contains the display values for one annotation card.
type annotationCardView struct {
	// ID is the stable annotation identifier used by browser interaction hooks.
	ID string
	// Intent and Status are validated domain values rendered as badges.
	Intent      annotation.Intent
	Status      annotation.Status
	IntentLabel string
	StatusLabel string
	// Comment is untrusted text escaped by html/template. Role is a validated
	// domain value used for both attribution and permissions.
	Comment string
	Role    annotation.Role
	// Inactive marks closed and rejected annotations when they are visible.
	Inactive bool
	// SourceQuote and SourceLines describe a source-level annotation.
	SourceQuote string
	SourceLines string
	// DocumentLevel selects the whole-document label instead of a source quote.
	DocumentLevel bool
	// SelectionUnavailable marks a selection that could not be verified after
	// the document changed and must be reattached.
	SelectionUnavailable bool
	// AnchorStale adds the stale badge and permits a reattachment form.
	AnchorStale bool
	// Browser-only highlighting and navigation consume these validated values
	// from data attributes instead of reconstructing annotation presentation.
	AnchorState     annotation.AnchorState
	AnchorStartByte int
	AnchorEndByte   int
	HasAnchor       bool
	SourceStartByte int
	HasSource       bool
	NeedsReattach   bool
	// Turn is the optional "waiting for" badge derived from recent activity.
	Turn *annotationTurnView
	// Thread omits redundant acknowledgement entries from the visible history.
	Thread []annotationThreadView
	// Actions contains only lifecycle operations allowed from the current state.
	Actions annotationActionsView
}

// annotationTurnView describes who is expected to act next on active work.
type annotationTurnView struct {
	// Label is user-facing text; Class selects the existing status-badge style.
	Label string
	Class string
}

// annotationThreadView is one visible, presentation-ready thread entry.
type annotationThreadView struct {
	// Kind remains available as a stable data attribute for browser adapters.
	Kind annotation.ThreadKind
	// KindLabel and Class are the readable label and CSS presentation hook.
	KindLabel string
	Class     string
	// Role is the attribution and authorization identity (agent or reviewer).
	Role annotation.Role
	// Text is untrusted display content escaped by html/template.
	Text string
}

// annotationActionsView supplies URLs and state for forms on one card.
type annotationActionsView struct {
	// AnnotationID is used in accessible labels and browser interaction hooks.
	AnnotationID string
	// Document is submitted so handlers can authorize catalog membership.
	Document string
	// The URLs are derived only from the validated annotation identifier.
	ReplyURL      string
	ReattachURL   string
	TransitionURL string
	// CanReattach requires both a source selector and a currently stale anchor.
	CanReattach bool
	// CanQuickClose promotes the common applied-to-closed reviewer action.
	CanQuickClose bool
	// Transitions contains the remaining domain-authorized lifecycle actions.
	Transitions []annotationTransitionView
	// Draft values preserve user input in expected validation/conflict fragments.
	ReplyRole          annotation.Role
	ReplyMessage       string
	ReattachStartByte  string
	ReattachEndByte    string
	ReattachDigest     string
	TransitionRole     annotation.Role
	TransitionActivity string
	TransitionCommit   string
}

// annotationTransitionView describes one option rendered in the lifecycle
// form. Role authorizes and attributes the state change.
type annotationTransitionView struct {
	// Status is the requested target lifecycle state.
	Status annotation.Status
	// Label is the option text shown to the reviewer or agent.
	Label string
	// Role identifies which domain role may perform this transition.
	Role annotation.Role
	// Activity and ActivityLabel describe optional message/summary input.
	Activity      string
	ActivityLabel string
	// Selected preserves the attempted action when a form is returned with feedback.
	Selected bool
}

// lifecycleActionDefinition is presentation metadata for a possible domain
// transition. The complete list below is only a display catalog: each card
// still calls annotation.ValidateTransition and exposes authorized entries.
type lifecycleActionDefinition struct {
	status        annotation.Status
	label         string
	role          annotation.Role
	activity      string
	activityLabel string
}

var lifecycleActionDefinitions = [...]lifecycleActionDefinition{
	{status: annotation.StatusAcknowledged, label: "Acknowledge", role: annotation.RoleAgent},
	{status: annotation.StatusApplied, label: "Mark applied", role: annotation.RoleAgent, activity: "summary", activityLabel: "Summary"},
	{status: annotation.StatusRejected, label: "Reject", role: annotation.RoleAgent, activity: "message", activityLabel: "Message"},
	{status: annotation.StatusClosed, label: "Close", role: annotation.RoleReviewer},
	{status: annotation.StatusNeedsChanges, label: "Needs changes", role: annotation.RoleReviewer, activity: "message", activityLabel: "Message"},
	{status: annotation.StatusOpen, label: "Reopen", role: annotation.RoleReviewer},
}

// newAnnotationPanelView applies active/inactive filtering once on the server
// so templates only iterate presentation-ready cards.
func newAnnotationPanelView(document, revision string, annotations []resolvedAnnotation, showInactive bool) annotationPanelView {
	activeCount := 0
	for _, item := range annotations {
		if !isInactiveAnnotation(item.Status) {
			activeCount++
		}
	}

	view := annotationPanelView{
		Document:     document,
		Revision:     revision,
		ShowInactive: showInactive,
		CountLabel:   annotationCountLabel(activeCount, len(annotations)),
		Cards:        make([]annotationCardView, 0, len(annotations)),
	}
	for _, item := range annotations {
		if !showInactive && isInactiveAnnotation(item.Status) {
			continue
		}
		view.Cards = append(view.Cards, newAnnotationCardView(document, item))
	}
	if len(view.Cards) == 0 {
		if len(annotations) == 0 {
			view.EmptyMessage = "No annotations for this document."
		} else {
			view.EmptyMessage = "No active annotations."
		}
	}
	return view
}

// newAnnotationCardView converts validated annotation and anchor data into the
// labels and nested models consumed by annotation-card.html.
func newAnnotationCardView(document string, item resolvedAnnotation) annotationCardView {
	inactive := isInactiveAnnotation(item.Status)
	view := annotationCardView{
		ID:            item.ID,
		Intent:        item.Intent,
		Status:        item.Status,
		IntentLabel:   humanizeAnnotationValue(string(item.Intent)),
		StatusLabel:   humanizeAnnotationValue(string(item.Status)),
		Comment:       item.Comment,
		Role:          item.Role,
		Inactive:      inactive,
		Turn:          pendingTurnBadge(item.Annotation),
		Thread:        annotationThread(item.Thread),
		NeedsReattach: item.NeedsReattachment,
	}
	if item.NeedsReattachment {
		view.SelectionUnavailable = true
	} else if item.Source == nil {
		view.DocumentLevel = true
	} else {
		view.SourceQuote = item.Source.Selector.Exact
		view.SourceLines = lineRangeLabel(item.Source.Selector.StartLine, item.Source.Selector.EndLine)
	}
	view.AnchorStale = item.Anchor != nil && item.Anchor.State == annotation.AnchorStale
	if item.Anchor != nil {
		view.AnchorState = item.Anchor.State
		view.AnchorStartByte = item.Anchor.StartByte
		view.AnchorEndByte = item.Anchor.EndByte
		view.HasAnchor = true
	}
	if item.Source != nil {
		view.SourceStartByte = item.Source.Selector.StartByte
		view.HasSource = true
	}
	view.Actions = newAnnotationActionsView(document, item, view.AnchorStale)
	return view
}

func humanizeAnnotationValue(value string) string {
	return strings.ReplaceAll(value, "_", " ")
}

// newAnnotationActionsView derives form availability from domain lifecycle
// validation. Quick Close is separated from the less-common lifecycle menu,
// which avoids rendering the same applied-to-closed action twice.
func newAnnotationActionsView(document string, item resolvedAnnotation, anchorStale bool) annotationActionsView {
	escapedID := url.PathEscape(item.ID)
	baseURL := "/ui/review/annotations/" + escapedID
	view := annotationActionsView{
		AnnotationID:  item.ID,
		Document:      document,
		ReplyURL:      baseURL + "/replies",
		ReattachURL:   baseURL + "/reattach",
		TransitionURL: baseURL + "/transition",
		CanReattach:   (item.Source != nil || item.NeedsReattachment) && anchorStale,
		CanQuickClose: item.Status == annotation.StatusApplied,
		ReplyRole:     annotation.RoleReviewer,
	}
	for _, definition := range lifecycleActionDefinitions {
		if err := annotation.ValidateTransition(item.Status, definition.status, definition.role); err != nil {
			continue
		}
		if view.CanQuickClose && definition.status == annotation.StatusClosed {
			continue
		}
		label := definition.label
		if item.Status == annotation.StatusNeedsChanges && definition.status == annotation.StatusAcknowledged {
			label = "Acknowledge retry"
		}
		view.Transitions = append(view.Transitions, annotationTransitionView{
			Status:        definition.status,
			Label:         label,
			Role:          definition.role,
			Activity:      definition.activity,
			ActivityLabel: definition.activityLabel,
		})
	}
	if len(view.Transitions) > 0 {
		view.Transitions[0].Selected = true
		view.TransitionRole = view.Transitions[0].Role
	}
	return view
}

// annotationThread removes acknowledgement events because the following status
// change already communicates the same fact in the visible history.
func annotationThread(entries []annotation.ThreadEntry) []annotationThreadView {
	result := make([]annotationThreadView, 0, len(entries))
	for _, entry := range entries {
		if entry.Kind == annotation.ThreadAcknowledgement {
			continue
		}
		kindLabel, class := threadKindPresentation(entry.Kind)
		result = append(result, annotationThreadView{
			Kind:      entry.Kind,
			KindLabel: kindLabel,
			Class:     class,
			Role:      entry.Role,
			Text:      threadEntryText(entry),
		})
	}
	return result
}

func threadKindPresentation(kind annotation.ThreadKind) (string, string) {
	switch kind {
	case annotation.ThreadReply:
		return "Reply", "reply"
	case annotation.ThreadResolution:
		return "Resolution", "resolution"
	case annotation.ThreadReview:
		return "Review note", "review"
	case annotation.ThreadStatusChange:
		return "Status change", "status-change"
	default:
		return "Update", "update"
	}
}

func threadEntryText(entry annotation.ThreadEntry) string {
	switch {
	case entry.Message != "":
		return entry.Message
	case entry.Summary != "":
		return entry.Summary
	default:
		return fmt.Sprintf("%s → %s", entry.FromStatus, entry.ToStatus)
	}
}

// pendingTurnBadge infers who should respond next for statuses that represent
// active work. The most recent classifiable entry wins: agent activity waits
// for reviewer feedback, while reviewer activity waits for an agent response.
// Completed or inactive states do not display a turn badge.
func pendingTurnBadge(item annotation.Annotation) *annotationTurnView {
	if item.Status != annotation.StatusOpen && item.Status != annotation.StatusNeedsChanges {
		return nil
	}
	for index := len(item.Thread) - 1; index >= 0; index-- {
		role := item.Thread[index].Role
		switch role {
		case annotation.RoleAgent:
			return &annotationTurnView{Label: "waiting for reviewer", Class: "pending-review"}
		case annotation.RoleReviewer:
			return &annotationTurnView{Label: "waiting for agent", Class: "pending-agent"}
		}
	}
	return nil
}

func annotationCountLabel(active, total int) string {
	if active == total {
		return fmt.Sprintf("%d", active)
	}
	return fmt.Sprintf("%d active · %d total", active, total)
}

func lineRangeLabel(start, end int) string {
	if start == end {
		return fmt.Sprintf("Line %d", start)
	}
	return fmt.Sprintf("Lines %d–%d", start, end)
}

func isInactiveAnnotation(status annotation.Status) bool {
	return status == annotation.StatusClosed || status == annotation.StatusRejected
}
