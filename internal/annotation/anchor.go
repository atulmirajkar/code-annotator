package annotation

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

const selectorContextBytes = 64

// AnchorState describes whether a source selector still identifies text.
type AnchorState string

const (
	AnchorExact AnchorState = "exact"
	AnchorMoved AnchorState = "moved"
	AnchorStale AnchorState = "stale"
)

// StaleReason explains why a stale selector could not be attached safely.
type StaleReason string

const (
	StaleNotFound  StaleReason = "not_found"
	StaleAmbiguous StaleReason = "ambiguous"
)

// AnchorResult is the current location and derived state of a source selector.
// Byte offsets are valid only for exact and moved results.
type AnchorResult struct {
	State      AnchorState `json:"state"`
	Reason     StaleReason `json:"reason,omitempty"`
	StartByte  int         `json:"startByte,omitempty"`
	EndByte    int         `json:"endByte,omitempty"`
	StartLine  int         `json:"startLine,omitempty"`
	EndLine    int         `json:"endLine,omitempty"`
	Candidates int         `json:"candidates,omitempty"`
}

// NewSource creates a verified selector for the byte range [startByte, endByte)
// in UTF-8 Markdown source. It captures bounded adjacent quote context and the
// exact document revision digest.
func NewSource(document []byte, startByte, endByte int) (Source, error) {
	if !utf8.Valid(document) {
		return Source{}, errors.New("annotation source is not valid UTF-8")
	}
	if startByte < 0 || endByte <= startByte || endByte > len(document) {
		return Source{}, errors.New("annotation source byte range is invalid")
	}
	if !isRuneBoundary(document, startByte) || !isRuneBoundary(document, endByte) {
		return Source{}, errors.New("annotation source byte range splits a UTF-8 character")
	}

	prefixStart := runeBoundaryAtOrAfter(document, max(0, startByte-selectorContextBytes))
	suffixEnd := runeBoundaryAtOrBefore(document, min(len(document), endByte+selectorContextBytes))
	startLine, endLine := lineRange(document, startByte, endByte)

	result := Source{
		SHA256: documentSHA256(document),
		Selector: Selector{
			Exact:     string(document[startByte:endByte]),
			Prefix:    string(document[prefixStart:startByte]),
			Suffix:    string(document[endByte:suffixEnd]),
			StartByte: startByte,
			EndByte:   endByte,
			StartLine: startLine,
			EndLine:   endLine,
		},
	}
	if err := result.Validate(); err != nil {
		return Source{}, fmt.Errorf("create annotation source: %w", err)
	}
	return result, nil
}

// ResolveAnchor locates a selector in the current Markdown source. Staleness is
// returned as data rather than an error because it is an expected review state.
func ResolveAnchor(document []byte, source Source) (AnchorResult, error) {
	if !utf8.Valid(document) {
		return AnchorResult{}, errors.New("current Markdown source is not valid UTF-8")
	}
	if err := source.Validate(); err != nil {
		return AnchorResult{}, fmt.Errorf("resolve annotation source: %w", err)
	}

	selector := source.Selector
	if strings.EqualFold(documentSHA256(document), source.SHA256) &&
		selector.StartByte >= 0 && selector.EndByte <= len(document) &&
		bytes.Equal(document[selector.StartByte:selector.EndByte], []byte(selector.Exact)) {
		return resolvedResult(AnchorExact, document, selector.StartByte, selector.EndByte, 1), nil
	}

	exact := []byte(selector.Exact)
	allMatches := findMatches(document, exact)
	contextMatches := make([]int, 0, len(allMatches))
	for _, start := range allMatches {
		end := start + len(exact)
		if hasContext(document, start, end, selector.Prefix, selector.Suffix) {
			contextMatches = append(contextMatches, start)
		}
	}

	switch {
	case len(contextMatches) == 1:
		start := contextMatches[0]
		return resolvedResult(AnchorMoved, document, start, start+len(exact), 1), nil
	case len(contextMatches) > 1:
		return AnchorResult{State: AnchorStale, Reason: StaleAmbiguous, Candidates: len(contextMatches)}, nil
	case len(allMatches) == 1:
		start := allMatches[0]
		return resolvedResult(AnchorMoved, document, start, start+len(exact), 1), nil
	case len(allMatches) > 1:
		return AnchorResult{State: AnchorStale, Reason: StaleAmbiguous, Candidates: len(allMatches)}, nil
	default:
		return AnchorResult{State: AnchorStale, Reason: StaleNotFound}, nil
	}
}

func documentSHA256(document []byte) string {
	digest := sha256.Sum256(document)
	return hex.EncodeToString(digest[:])
}

func findMatches(document, exact []byte) []int {
	var matches []int
	for offset := 0; offset <= len(document)-len(exact); {
		index := bytes.Index(document[offset:], exact)
		if index < 0 {
			break
		}
		start := offset + index
		matches = append(matches, start)
		offset = start + 1
	}
	return matches
}

func hasContext(document []byte, start, end int, prefix, suffix string) bool {
	return (prefix == "" || bytes.HasSuffix(document[:start], []byte(prefix))) &&
		(suffix == "" || bytes.HasPrefix(document[end:], []byte(suffix)))
}

func resolvedResult(state AnchorState, document []byte, start, end, candidates int) AnchorResult {
	startLine, endLine := lineRange(document, start, end)
	return AnchorResult{
		State:      state,
		StartByte:  start,
		EndByte:    end,
		StartLine:  startLine,
		EndLine:    endLine,
		Candidates: candidates,
	}
}

func lineRange(document []byte, start, end int) (int, int) {
	startLine := bytes.Count(document[:start], []byte{'\n'}) + 1
	endLine := bytes.Count(document[:end-1], []byte{'\n'}) + 1
	return startLine, endLine
}

func isRuneBoundary(document []byte, offset int) bool {
	return offset == 0 || offset == len(document) || utf8.RuneStart(document[offset])
}

func runeBoundaryAtOrAfter(document []byte, offset int) int {
	for offset < len(document) && !utf8.RuneStart(document[offset]) {
		offset++
	}
	return offset
}

func runeBoundaryAtOrBefore(document []byte, offset int) int {
	for offset > 0 && offset < len(document) && !utf8.RuneStart(document[offset]) {
		offset--
	}
	return offset
}
