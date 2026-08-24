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
	Document     string
	Revision     string
	ShowInactive bool
	CountLabel   string
	EmptyMessage string
	Cards        []annotationCardView
}

type annotationCardView struct {
	ID            string
	Intent        annotation.Intent
	Status        annotation.Status
	Comment       string
	Author        string
	Inactive      bool
	SourceQuote   string
	SourceLines   string
	DocumentLevel bool
	AnchorStale   bool
	Turn          *annotationTurnView
	Thread        []annotationThreadView
	Actions       annotationActionsView
}

type annotationTurnView struct {
	Label string
	Class string
}

type annotationThreadView struct {
	Kind      annotation.ThreadKind
	KindLabel string
	Class     string
	ActorRole annotation.ActorRole
	Author    string
	Text      string
}

type annotationActionsView struct {
	AnnotationID  string
	Document      string
	ReplyURL      string
	ReattachURL   string
	TransitionURL string
	CanReattach   bool
	CanQuickClose bool
	Transitions   []annotationTransitionView
}

type annotationTransitionView struct {
	Status        annotation.Status
	Label         string
	ActorRole     annotation.ActorRole
	Activity      string
	ActivityLabel string
}

type transitionCandidate struct {
	status        annotation.Status
	label         string
	actorRole     annotation.ActorRole
	activity      string
	activityLabel string
}

var annotationTransitionCandidates = [...]transitionCandidate{
	{status: annotation.StatusAcknowledged, label: "Acknowledge", actorRole: annotation.RoleAgent},
	{status: annotation.StatusApplied, label: "Mark applied", actorRole: annotation.RoleAgent, activity: "summary", activityLabel: "Summary"},
	{status: annotation.StatusRejected, label: "Reject", actorRole: annotation.RoleAgent, activity: "message", activityLabel: "Message"},
	{status: annotation.StatusClosed, label: "Close", actorRole: annotation.RoleReviewer},
	{status: annotation.StatusNeedsChanges, label: "Needs changes", actorRole: annotation.RoleReviewer, activity: "message", activityLabel: "Message"},
	{status: annotation.StatusOpen, label: "Reopen", actorRole: annotation.RoleReviewer},
}

func newAnnotationPanelView(document, revision string, annotations []annotationView, showInactive bool) annotationPanelView {
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

func newAnnotationCardView(document string, item annotationView) annotationCardView {
	inactive := isInactiveAnnotation(item.Status)
	view := annotationCardView{
		ID:       item.ID,
		Intent:   item.Intent,
		Status:   item.Status,
		Comment:  item.Comment,
		Author:   item.Author,
		Inactive: inactive,
		Turn:     annotationTurn(item.Annotation),
		Thread:   annotationThread(item.Thread),
	}
	if item.Source == nil {
		view.DocumentLevel = true
	} else {
		view.SourceQuote = item.Source.Selector.Exact
		view.SourceLines = lineRangeLabel(item.Source.Selector.StartLine, item.Source.Selector.EndLine)
	}
	view.AnchorStale = item.Anchor != nil && item.Anchor.State == annotation.AnchorStale
	view.Actions = newAnnotationActionsView(document, item, view.AnchorStale)
	return view
}

func newAnnotationActionsView(document string, item annotationView, anchorStale bool) annotationActionsView {
	escapedID := url.PathEscape(item.ID)
	baseURL := "/ui/review/annotations/" + escapedID
	view := annotationActionsView{
		AnnotationID:  item.ID,
		Document:      document,
		ReplyURL:      baseURL + "/replies",
		ReattachURL:   baseURL + "/reattach",
		TransitionURL: baseURL + "/transition",
		CanReattach:   item.Source != nil && anchorStale,
		CanQuickClose: item.Status == annotation.StatusApplied,
	}
	for _, candidate := range annotationTransitionCandidates {
		if err := annotation.ValidateTransition(item.Status, candidate.status, candidate.actorRole); err != nil {
			continue
		}
		if view.CanQuickClose && candidate.status == annotation.StatusClosed {
			continue
		}
		label := candidate.label
		if item.Status == annotation.StatusNeedsChanges && candidate.status == annotation.StatusAcknowledged {
			label = "Acknowledge retry"
		}
		view.Transitions = append(view.Transitions, annotationTransitionView{
			Status:        candidate.status,
			Label:         label,
			ActorRole:     candidate.actorRole,
			Activity:      candidate.activity,
			ActivityLabel: candidate.activityLabel,
		})
	}
	return view
}

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
			ActorRole: entry.ActorRole,
			Author:    entry.Author,
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

func annotationTurn(item annotation.Annotation) *annotationTurnView {
	if item.Status != annotation.StatusOpen && item.Status != annotation.StatusNeedsChanges {
		return nil
	}
	for index := len(item.Thread) - 1; index >= 0; index-- {
		role := threadActorRole(item.Thread[index], item.Author)
		switch role {
		case annotation.RoleAgent:
			return &annotationTurnView{Label: "waiting for reviewer", Class: "pending-review"}
		case annotation.RoleReviewer:
			return &annotationTurnView{Label: "waiting for agent", Class: "pending-agent"}
		}
	}
	return nil
}

func threadActorRole(entry annotation.ThreadEntry, reviewer string) annotation.ActorRole {
	if entry.ActorRole == annotation.RoleAgent || entry.ActorRole == annotation.RoleReviewer {
		return entry.ActorRole
	}
	author := strings.ToLower(strings.TrimSpace(entry.Author))
	switch {
	case author == "":
		return ""
	case author == strings.ToLower(strings.TrimSpace(reviewer)), author == "reviewer", author == "author":
		return annotation.RoleReviewer
	case author == "agent", author == "codex", author == "claude":
		return annotation.RoleAgent
	default:
		return ""
	}
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
