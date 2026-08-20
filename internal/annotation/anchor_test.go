package annotation

import (
	"strings"
	"testing"
)

func TestNewSource(t *testing.T) {
	t.Parallel()

	document := []byte("# Network\n\nThe server binds to 127.0.0.1 by default.\nNext line.\n")
	exact := "binds to 127.0.0.1"
	start := strings.Index(string(document), exact)
	source, err := NewSource(document, start, start+len(exact))
	if err != nil {
		t.Fatalf("NewSource() error = %v", err)
	}

	if got, want := source.Selector.Exact, exact; got != want {
		t.Errorf("Exact = %q, want %q", got, want)
	}
	if source.Selector.StartLine != 3 || source.Selector.EndLine != 3 {
		t.Errorf("lines = %d-%d, want 3-3", source.Selector.StartLine, source.Selector.EndLine)
	}
	if len(source.SHA256) != 64 {
		t.Errorf("SHA256 length = %d, want 64", len(source.SHA256))
	}
	if !strings.HasSuffix(source.Selector.Prefix, "The server ") {
		t.Errorf("Prefix = %q", source.Selector.Prefix)
	}
	if !strings.HasPrefix(source.Selector.Suffix, " by default.") {
		t.Errorf("Suffix = %q", source.Selector.Suffix)
	}
}

func TestNewSourceRejectsInvalidRanges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		document []byte
		start    int
		end      int
	}{
		{name: "negative", document: []byte("hello"), start: -1, end: 2},
		{name: "empty", document: []byte("hello"), start: 2, end: 2},
		{name: "past end", document: []byte("hello"), start: 2, end: 6},
		{name: "invalid utf8", document: []byte{0xff}, start: 0, end: 1},
		{name: "split rune", document: []byte("aéb"), start: 2, end: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewSource(tt.document, tt.start, tt.end); err == nil {
				t.Fatal("NewSource() error = nil, want rejection")
			}
		})
	}
}

func TestResolveAnchorExact(t *testing.T) {
	t.Parallel()

	document := []byte("first line\nselected text\nlast line\n")
	start := strings.Index(string(document), "selected text")
	source, err := NewSource(document, start, start+len("selected text"))
	if err != nil {
		t.Fatalf("NewSource() error = %v", err)
	}

	result, err := ResolveAnchor(document, source)
	if err != nil {
		t.Fatalf("ResolveAnchor() error = %v", err)
	}
	if result.State != AnchorExact || result.StartByte != start || result.EndByte != start+len("selected text") {
		t.Fatalf("ResolveAnchor() = %#v, want exact at %d", result, start)
	}
	if result.StartLine != 2 || result.EndLine != 2 {
		t.Fatalf("lines = %d-%d, want 2-2", result.StartLine, result.EndLine)
	}
}

func TestResolveAnchorMovedAfterEdit(t *testing.T) {
	t.Parallel()

	original := []byte("# Title\n\nKeep this selected sentence.\n")
	exact := "selected sentence"
	start := strings.Index(string(original), exact)
	source, err := NewSource(original, start, start+len(exact))
	if err != nil {
		t.Fatalf("NewSource() error = %v", err)
	}
	updated := []byte("New introduction.\n\n# Title\n\nKeep this selected sentence.\n")

	result, err := ResolveAnchor(updated, source)
	if err != nil {
		t.Fatalf("ResolveAnchor() error = %v", err)
	}
	wantStart := strings.Index(string(updated), exact)
	if result.State != AnchorMoved || result.StartByte != wantStart {
		t.Fatalf("ResolveAnchor() = %#v, want moved to %d", result, wantStart)
	}
}

func TestResolveAnchorUsesContextForRepeatedText(t *testing.T) {
	t.Parallel()

	original := []byte("Alpha shared words ending.\nBeta shared words ending.\n")
	exact := "shared words"
	start := strings.LastIndex(string(original), exact)
	source, err := NewSource(original, start, start+len(exact))
	if err != nil {
		t.Fatalf("NewSource() error = %v", err)
	}
	updated := append([]byte("Introduction.\n"), original...)

	result, err := ResolveAnchor(updated, source)
	if err != nil {
		t.Fatalf("ResolveAnchor() error = %v", err)
	}
	wantStart := strings.LastIndex(string(updated), exact)
	if result.State != AnchorMoved || result.StartByte != wantStart {
		t.Fatalf("ResolveAnchor() = %#v, want contextual match at %d", result, wantStart)
	}
}

func TestResolveAnchorStale(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		original   string
		updated    string
		exact      string
		wantReason StaleReason
		candidates int
	}{
		{name: "removed", original: "before unique phrase after", updated: "before replacement after", exact: "unique phrase", wantReason: StaleNotFound},
		{name: "ambiguous", original: "prefix unique phrase suffix", updated: "unique phrase and unique phrase", exact: "unique phrase", wantReason: StaleAmbiguous, candidates: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start := strings.Index(tt.original, tt.exact)
			source, err := NewSource([]byte(tt.original), start, start+len(tt.exact))
			if err != nil {
				t.Fatalf("NewSource() error = %v", err)
			}
			result, err := ResolveAnchor([]byte(tt.updated), source)
			if err != nil {
				t.Fatalf("ResolveAnchor() error = %v", err)
			}
			if result.State != AnchorStale || result.Reason != tt.wantReason || result.Candidates != tt.candidates {
				t.Fatalf("ResolveAnchor() = %#v, want stale %q with %d candidates", result, tt.wantReason, tt.candidates)
			}
		})
	}
}

func TestResolveAnchorRepairsBadOffsets(t *testing.T) {
	t.Parallel()

	document := []byte("prefix selected suffix")
	start := strings.Index(string(document), "selected")
	source, err := NewSource(document, start, start+len("selected"))
	if err != nil {
		t.Fatalf("NewSource() error = %v", err)
	}
	source.Selector.StartByte = 0
	source.Selector.EndByte = len("selected")

	result, err := ResolveAnchor(document, source)
	if err != nil {
		t.Fatalf("ResolveAnchor() error = %v", err)
	}
	if result.State != AnchorMoved || result.StartByte != start {
		t.Fatalf("ResolveAnchor() = %#v, want repaired position %d", result, start)
	}
}

func TestResolveAnchorDetectsOverlappingAmbiguity(t *testing.T) {
	t.Parallel()

	original := []byte("prefix aaa suffix")
	start := strings.Index(string(original), "aaa")
	source, err := NewSource(original, start, start+len("aaa"))
	if err != nil {
		t.Fatalf("NewSource() error = %v", err)
	}

	result, err := ResolveAnchor([]byte("aaaa"), source)
	if err != nil {
		t.Fatalf("ResolveAnchor() error = %v", err)
	}
	if result.State != AnchorStale || result.Reason != StaleAmbiguous || result.Candidates != 2 {
		t.Fatalf("ResolveAnchor() = %#v, want two ambiguous overlapping matches", result)
	}
}

func TestNewSourceUnicodeLineAndContext(t *testing.T) {
	t.Parallel()

	document := []byte("héading\n🙂 selected café text\n")
	exact := "selected café"
	start := strings.Index(string(document), exact)
	source, err := NewSource(document, start, start+len(exact))
	if err != nil {
		t.Fatalf("NewSource() error = %v", err)
	}
	result, err := ResolveAnchor(document, source)
	if err != nil {
		t.Fatalf("ResolveAnchor() error = %v", err)
	}
	if result.State != AnchorExact || result.StartLine != 2 || result.EndLine != 2 {
		t.Fatalf("ResolveAnchor() = %#v", result)
	}
}
