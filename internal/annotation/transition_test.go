package annotation

import (
	"strings"
	"testing"
	"time"
)

func TestTransitionEntries(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		from      Status
		input     TransitionInput
		wantKinds []ThreadKind
		wantErr   string
	}{
		{name: "acknowledge", from: StatusOpen, input: TransitionInput{Status: StatusAcknowledged, ActorRole: RoleAgent, Author: "codex"}, wantKinds: []ThreadKind{ThreadAcknowledgement, ThreadStatusChange}},
		{name: "apply", from: StatusAcknowledged, input: TransitionInput{Status: StatusApplied, ActorRole: RoleAgent, Author: "codex", Summary: "Implemented", Commit: "abc1234"}, wantKinds: []ThreadKind{ThreadResolution, ThreadStatusChange}},
		{name: "review changes", from: StatusApplied, input: TransitionInput{Status: StatusNeedsChanges, ActorRole: RoleReviewer, Author: "reviewer", Message: "Keep the default"}, wantKinds: []ThreadKind{ThreadReview, ThreadStatusChange}},
		{name: "reject changes", from: StatusNeedsChanges, input: TransitionInput{Status: StatusRejected, ActorRole: RoleAgent, Author: "codex", Message: "This change is not applicable"}, wantKinds: []ThreadKind{ThreadReply, ThreadStatusChange}},
		{name: "reviewer dismisses open", from: StatusOpen, input: TransitionInput{Status: StatusClosed, ActorRole: RoleReviewer, Author: "reviewer"}, wantKinds: []ThreadKind{ThreadStatusChange}},
		{name: "agent cannot close", from: StatusApplied, input: TransitionInput{Status: StatusClosed, ActorRole: RoleAgent, Author: "codex"}, wantErr: "cannot transition"},
		{name: "summary required", from: StatusAcknowledged, input: TransitionInput{Status: StatusApplied, ActorRole: RoleAgent, Author: "codex"}, wantErr: "summary"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			entries, err := TransitionEntries(Annotation{Status: test.from}, test.input, now)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("TransitionEntries() error = %v, want containing %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("TransitionEntries() error = %v", err)
			}
			if len(entries) != len(test.wantKinds) {
				t.Fatalf("entries = %#v, want %d", entries, len(test.wantKinds))
			}
			for index, kind := range test.wantKinds {
				if entries[index].Kind != kind || !strings.HasPrefix(entries[index].ID, "msg_") {
					t.Errorf("entry %d = %#v, want kind %q and msg_ ID", index, entries[index], kind)
				}
			}
		})
	}
}
