package annotation

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSidecarValidate(t *testing.T) {
	t.Parallel()

	valid := validSidecar()
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	tests := []struct {
		name    string
		mutate  func(*Sidecar)
		wantErr string
	}{
		{name: "schema", mutate: func(sidecar *Sidecar) { sidecar.SchemaVersion = 2 }, wantErr: "unsupported"},
		{name: "document traversal", mutate: func(sidecar *Sidecar) { sidecar.Document = "../secret.md" }, wantErr: "escapes"},
		{name: "duplicate annotation", mutate: func(sidecar *Sidecar) { sidecar.Annotations = append(sidecar.Annotations, sidecar.Annotations[0]) }, wantErr: "duplicate id"},
		{name: "duplicate thread id", mutate: func(sidecar *Sidecar) {
			copy := sidecar.Annotations[0]
			copy.ID = "ann_second"
			sidecar.Annotations = append(sidecar.Annotations, copy)
		}, wantErr: "duplicate thread id"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sidecar := validSidecar()
			tt.mutate(&sidecar)
			if err := sidecar.Validate(); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateDocumentPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		document string
		wantErr  bool
	}{
		{name: "Markdown", document: "docs/design.md"},
		{name: "Go source", document: "internal/server/server.go"},
		{name: "TypeScript source", document: "web/viewer.ts"},
		{name: "empty", wantErr: true},
		{name: "traversal", document: "../secret.go", wantErr: true},
		{name: "absolute", document: "/tmp/source.go", wantErr: true},
		{name: "backslash", document: `internal\\source.go`, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateDocumentPath(test.document)
			if (err != nil) != test.wantErr {
				t.Fatalf("ValidateDocumentPath(%q) error = %v, wantErr %t", test.document, err, test.wantErr)
			}
		})
	}
}

func TestAnnotationValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*Annotation)
		wantErr string
	}{
		{name: "invalid id", mutate: func(item *Annotation) { item.ID = "bad/id" }, wantErr: "must start"},
		{name: "intent", mutate: func(item *Annotation) { item.Intent = "todo" }, wantErr: "intent"},
		{name: "status", mutate: func(item *Annotation) { item.Status = "done" }, wantErr: "status"},
		{name: "comment", mutate: func(item *Annotation) { item.Comment = " " }, wantErr: "comment"},
		{name: "author", mutate: func(item *Annotation) { item.Author = "" }, wantErr: "author"},
		{name: "timestamps", mutate: func(item *Annotation) { item.UpdatedAt = item.CreatedAt.Add(-time.Second) }, wantErr: "precede"},
		{name: "digest", mutate: func(item *Annotation) { item.Source.SHA256 = "short" }, wantErr: "sha256"},
		{name: "byte range", mutate: func(item *Annotation) { item.Source.Selector.EndByte = item.Source.Selector.StartByte }, wantErr: "byte range"},
		{name: "line range", mutate: func(item *Annotation) { item.Source.Selector.StartLine = 0 }, wantErr: "line range"},
		{name: "thread message", mutate: func(item *Annotation) { item.Thread[0].Message = "" }, wantErr: "message"},
		{name: "thread timestamp", mutate: func(item *Annotation) { item.Thread[0].CreatedAt = item.CreatedAt.Add(-time.Second) }, wantErr: "chronological"},
		{name: "thread after update", mutate: func(item *Annotation) { item.Thread[0].CreatedAt = item.UpdatedAt.Add(time.Second) }, wantErr: "updatedAt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := validSidecar().Annotations[0]
			tt.mutate(&item)
			if err := item.Validate(); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestDocumentLevelAnnotationIsValid(t *testing.T) {
	t.Parallel()

	item := validSidecar().Annotations[0]
	item.Source = nil
	if err := item.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestThreadEntryValidate(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 20, 20, 15, 0, 0, time.UTC)
	tests := []struct {
		name    string
		entry   ThreadEntry
		wantErr string
	}{
		{name: "reply", entry: ThreadEntry{ID: "msg_reply", Kind: ThreadReply, Message: "More context", Author: "atul", CreatedAt: now}},
		{name: "review", entry: ThreadEntry{ID: "msg_review", Kind: ThreadReview, Message: "Needs changes", Author: "atul", CreatedAt: now}},
		{name: "resolution", entry: ThreadEntry{ID: "msg_resolution", Kind: ThreadResolution, Summary: "Implemented", Commit: "abc1234", Author: "codex", CreatedAt: now}},
		{name: "acknowledgement", entry: ThreadEntry{ID: "msg_ack", Kind: ThreadAcknowledgement, Author: "codex", CreatedAt: now}},
		{name: "status change", entry: ThreadEntry{ID: "msg_status", Kind: ThreadStatusChange, Author: "atul", ActorRole: RoleReviewer, FromStatus: StatusApplied, ToStatus: StatusNeedsChanges, CreatedAt: now}},
		{name: "missing reply", entry: ThreadEntry{ID: "msg_reply", Kind: ThreadReply, Author: "atul", CreatedAt: now}, wantErr: "message"},
		{name: "missing resolution", entry: ThreadEntry{ID: "msg_resolution", Kind: ThreadResolution, Author: "codex", CreatedAt: now}, wantErr: "summary"},
		{name: "invalid id", entry: ThreadEntry{ID: "message", Kind: ThreadReply, Message: "text", Author: "atul", CreatedAt: now}, wantErr: "must start"},
		{name: "invalid status change", entry: ThreadEntry{ID: "msg_status", Kind: ThreadStatusChange, Author: "codex", ActorRole: RoleAgent, FromStatus: StatusApplied, ToStatus: StatusClosed, CreatedAt: now}, wantErr: "cannot transition"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.entry.Validate()
			if tt.wantErr == "" && err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("Validate() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateTransition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		from    Status
		to      Status
		actor   ActorRole
		allowed bool
	}{
		{name: "agent acknowledges", from: StatusOpen, to: StatusAcknowledged, actor: RoleAgent, allowed: true},
		{name: "agent applies", from: StatusAcknowledged, to: StatusApplied, actor: RoleAgent, allowed: true},
		{name: "agent retries", from: StatusNeedsChanges, to: StatusAcknowledged, actor: RoleAgent, allowed: true},
		{name: "agent rejects", from: StatusOpen, to: StatusRejected, actor: RoleAgent, allowed: true},
		{name: "reviewer requests changes", from: StatusApplied, to: StatusNeedsChanges, actor: RoleReviewer, allowed: true},
		{name: "reviewer closes", from: StatusApplied, to: StatusClosed, actor: RoleReviewer, allowed: true},
		{name: "reviewer reopens", from: StatusClosed, to: StatusOpen, actor: RoleReviewer, allowed: true},
		{name: "agent cannot close", from: StatusApplied, to: StatusClosed, actor: RoleAgent},
		{name: "reviewer cannot apply", from: StatusAcknowledged, to: StatusApplied, actor: RoleReviewer},
		{name: "cannot skip acknowledgement", from: StatusOpen, to: StatusApplied, actor: RoleAgent},
		{name: "cannot repeat", from: StatusOpen, to: StatusOpen, actor: RoleReviewer},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTransition(tt.from, tt.to, tt.actor)
			if tt.allowed && err != nil {
				t.Fatalf("ValidateTransition() error = %v", err)
			}
			if !tt.allowed && err == nil {
				t.Fatal("ValidateTransition() error = nil, want rejection")
			}
		})
	}
}

func TestSidecarJSONRoundTrip(t *testing.T) {
	t.Parallel()

	want := validSidecar()
	encoded, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var got Sidecar
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("round-trip Validate() error = %v", err)
	}
	if got.Document != want.Document || len(got.Annotations) != 1 || got.Annotations[0].Thread[0].Message != "Keep the default" {
		t.Fatalf("round trip = %#v, want key fields from %#v", got, want)
	}
}

func validSidecar() Sidecar {
	created := time.Date(2026, 8, 20, 19, 30, 0, 0, time.UTC)
	return Sidecar{
		SchemaVersion: SchemaVersion,
		Document:      "designs/architecture.md",
		Annotations: []Annotation{
			{
				ID:        "ann_01J7Y8Y4T9J2YQ8M5CQ6E3K2P1",
				Intent:    IntentChangeRequest,
				Status:    StatusNeedsChanges,
				Comment:   "Make the listen address configurable.",
				Author:    "atul",
				CreatedAt: created,
				UpdatedAt: created.Add(time.Hour),
				Source: &Source{
					SHA256: strings.Repeat("a", 64),
					Selector: Selector{
						Exact:     "Bind to 127.0.0.1 by default",
						Prefix:    "Network and lifecycle ",
						Suffix:    " never all interfaces implicitly.",
						StartByte: 2410,
						EndByte:   2444,
						StartLine: 104,
						EndLine:   104,
					},
				},
				Thread: []ThreadEntry{
					{
						ID:        "msg_01J7Z0N4S8B6H3Q2C9D5F7K1M0",
						Kind:      ThreadReview,
						Message:   "Keep the default",
						Author:    "atul",
						CreatedAt: created.Add(time.Hour),
					},
				},
			},
		},
	}
}
