package server

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"atulm/code-annotator/internal/annotation"
)

func TestAnnotationPanelViewFiltersInactiveCards(t *testing.T) {
	t.Parallel()

	items := []resolvedAnnotation{
		{Annotation: annotation.Annotation{ID: "ann_open", Status: annotation.StatusOpen}},
		{Annotation: annotation.Annotation{ID: "ann_closed", Status: annotation.StatusClosed}},
		{Annotation: annotation.Annotation{ID: "ann_rejected", Status: annotation.StatusRejected}},
	}

	active := newAnnotationPanelView("README.md", "revision", items, false)
	if got, want := active.CountLabel, "1 active · 3 total"; got != want {
		t.Fatalf("CountLabel = %q, want %q", got, want)
	}
	if got, want := cardIDs(active.Cards), []string{"ann_open"}; !slices.Equal(got, want) {
		t.Fatalf("active card IDs = %v, want %v", got, want)
	}

	all := newAnnotationPanelView("README.md", "revision", items, true)
	if got, want := cardIDs(all.Cards), []string{"ann_open", "ann_closed", "ann_rejected"}; !slices.Equal(got, want) {
		t.Fatalf("all card IDs = %v, want %v", got, want)
	}
	if !all.Cards[1].Inactive || !all.Cards[2].Inactive {
		t.Fatalf("closed and rejected cards are not marked inactive: %#v", all.Cards)
	}

	empty := newAnnotationPanelView("README.md", "revision", nil, false)
	if got, want := empty.EmptyMessage, "No annotations for this document."; got != want {
		t.Fatalf("empty message = %q, want %q", got, want)
	}
	inactiveOnly := newAnnotationPanelView("README.md", "revision", items[1:], false)
	if got, want := inactiveOnly.EmptyMessage, "No active annotations."; got != want {
		t.Fatalf("inactive-only message = %q, want %q", got, want)
	}
}

func TestAnnotationActionAvailabilityUsesLifecycleRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status         annotation.Status
		wantStatuses   []annotation.Status
		wantRoles      []annotation.Role
		wantQuickClose bool
	}{
		{status: annotation.StatusOpen, wantStatuses: []annotation.Status{annotation.StatusAcknowledged, annotation.StatusRejected, annotation.StatusClosed}, wantRoles: []annotation.Role{annotation.RoleAgent, annotation.RoleAgent, annotation.RoleReviewer}},
		{status: annotation.StatusAcknowledged, wantStatuses: []annotation.Status{annotation.StatusApplied, annotation.StatusRejected}, wantRoles: []annotation.Role{annotation.RoleAgent, annotation.RoleAgent}},
		{status: annotation.StatusNeedsChanges, wantStatuses: []annotation.Status{annotation.StatusAcknowledged, annotation.StatusRejected}, wantRoles: []annotation.Role{annotation.RoleAgent, annotation.RoleAgent}},
		{status: annotation.StatusApplied, wantStatuses: []annotation.Status{annotation.StatusNeedsChanges}, wantRoles: []annotation.Role{annotation.RoleReviewer}, wantQuickClose: true},
		{status: annotation.StatusClosed, wantStatuses: []annotation.Status{annotation.StatusOpen}, wantRoles: []annotation.Role{annotation.RoleReviewer}},
		{status: annotation.StatusRejected, wantStatuses: []annotation.Status{annotation.StatusOpen}, wantRoles: []annotation.Role{annotation.RoleReviewer}},
	}

	for _, test := range tests {
		t.Run(string(test.status), func(t *testing.T) {
			t.Parallel()
			item := resolvedAnnotation{Annotation: annotation.Annotation{ID: "ann_actions", Status: test.status}}
			view := newAnnotationActionsView("README.md", item, false)
			if got := transitionStatuses(view.Transitions); !slices.Equal(got, test.wantStatuses) {
				t.Fatalf("transition statuses = %v, want %v", got, test.wantStatuses)
			}
			if got := transitionRoles(view.Transitions); !slices.Equal(got, test.wantRoles) {
				t.Fatalf("transition roles = %v, want %v", got, test.wantRoles)
			}
			if view.CanQuickClose != test.wantQuickClose {
				t.Fatalf("CanQuickClose = %t, want %t", view.CanQuickClose, test.wantQuickClose)
			}
		})
	}
}

func TestAnnotationFragmentTemplatesRenderEscapedAuthoritativeState(t *testing.T) {
	t.Parallel()

	templates, err := parseViewerTemplates()
	if err != nil {
		t.Fatalf("parseViewerTemplates() error = %v", err)
	}
	for _, name := range []string{"page.html", "annotation-panel", "annotation-card", "annotation-actions"} {
		if templates.Lookup(name) == nil {
			t.Fatalf("template set does not contain %q", name)
		}
	}

	item := resolvedAnnotation{
		Annotation: annotation.Annotation{
			ID:      "ann_fragment",
			Intent:  annotation.IntentChangeRequest,
			Status:  annotation.StatusOpen,
			Comment: `<script>alert("comment")</script>`,
			Role:    `<img src=x onerror="role()">`,
			Source: &annotation.Source{Selector: annotation.Selector{
				Exact:     `<svg onload="source()">`,
				StartLine: 4,
				EndLine:   6,
			}},
			Thread: []annotation.ThreadEntry{
				{Kind: annotation.ThreadAcknowledgement, Role: "agent"},
				{Kind: annotation.ThreadReply, Role: "reviewer", Message: `<b onclick="reply()">reply</b>`},
				{Kind: annotation.ThreadStatusChange, Role: annotation.RoleAgent, FromStatus: annotation.StatusOpen, ToStatus: annotation.StatusAcknowledged},
			},
		},
		Anchor: &annotation.AnchorResult{State: annotation.AnchorStale, Reason: annotation.StaleNotFound},
	}
	view := newAnnotationPanelView("README.md", "revision-1", []resolvedAnnotation{item}, false)
	var output bytes.Buffer
	if err := templates.ExecuteTemplate(&output, "annotation-panel", view); err != nil {
		t.Fatalf("ExecuteTemplate() error = %v", err)
	}
	html := output.String()
	for _, unsafe := range []string{"<script>", "<img", "<svg", "<b onclick"} {
		if strings.Contains(html, unsafe) {
			t.Errorf("rendered fragment contains unescaped user markup %q:\n%s", unsafe, html)
		}
	}
	for _, want := range []string{
		`&lt;script&gt;alert(&#34;comment&#34;)&lt;/script&gt;`,
		`&lt;img src=x onerror=&#34;role()&#34;&gt;`,
		`&lt;svg onload=&#34;source()&#34;&gt;`,
		`&lt;b onclick=&#34;reply()&#34;&gt;reply&lt;/b&gt;`,
		`Lines 4–6`,
		`class="annotation-badge stale"`,
		`class="annotation-reattach"`,
		`open → acknowledged`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered fragment does not contain %q:\n%s", want, html)
		}
	}
	if strings.Contains(html, "Acknowledgement") {
		t.Errorf("rendered fragment exposes redundant acknowledgement entry:\n%s", html)
	}
}

func TestAnnotationReattachActionRequiresStaleSourceAnchor(t *testing.T) {
	t.Parallel()

	source := &annotation.Source{Selector: annotation.Selector{Exact: "selected", StartLine: 1, EndLine: 1}}
	tests := []struct {
		name              string
		source            *annotation.Source
		needsReattachment bool
		anchor            *annotation.AnchorResult
		want              bool
	}{
		{name: "document annotation", anchor: &annotation.AnchorResult{State: annotation.AnchorStale}},
		{name: "exact source", source: source, anchor: &annotation.AnchorResult{State: annotation.AnchorExact}},
		{name: "moved source", source: source, anchor: &annotation.AnchorResult{State: annotation.AnchorMoved}},
		{name: "stale source", source: source, anchor: &annotation.AnchorResult{State: annotation.AnchorStale}, want: true},
		{name: "selection lost during creation", needsReattachment: true, anchor: &annotation.AnchorResult{State: annotation.AnchorStale, Reason: annotation.StaleDocumentChanged}, want: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			item := resolvedAnnotation{Annotation: annotation.Annotation{ID: "ann_anchor", Status: annotation.StatusOpen, Source: test.source, NeedsReattachment: test.needsReattachment}, Anchor: test.anchor}
			view := newAnnotationCardView("README.md", item)
			if got := view.Actions.CanReattach; got != test.want {
				t.Fatalf("CanReattach = %t, want %t", got, test.want)
			}
			if view.SelectionUnavailable != test.needsReattachment {
				t.Fatalf("SelectionUnavailable = %t, want %t", view.SelectionUnavailable, test.needsReattachment)
			}
		})
	}
}

func cardIDs(cards []annotationCardView) []string {
	result := make([]string, 0, len(cards))
	for _, card := range cards {
		result = append(result, card.ID)
	}
	return result
}

func transitionStatuses(transitions []annotationTransitionView) []annotation.Status {
	result := make([]annotation.Status, 0, len(transitions))
	for _, transition := range transitions {
		result = append(result, transition.Status)
	}
	return result
}

func transitionRoles(transitions []annotationTransitionView) []annotation.Role {
	result := make([]annotation.Role, 0, len(transitions))
	for _, transition := range transitions {
		result = append(result, transition.Role)
	}
	return result
}
