package gitdiff

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

const maxRevisionOptions = 50

// ErrInvalidRevisionList reports Git log output that does not match the strict
// full-object-ID and NUL-framed subject contract requested by RecentCommits.
var ErrInvalidRevisionList = errors.New("invalid Git revision list")

// RevisionOption is one locally available commit that the browser may select.
// Subject is untrusted display text and must be escaped by its output format.
type RevisionOption struct {
	Commit  string `json:"commit"`
	Subject string `json:"subject"`
}

// RecentCommits returns at most 50 recent commits reachable from local refs.
// Explicit formatting and NUL framing avoid locale, decoration, and newline
// ambiguity; the shared runner bounds output, duration, and process behavior.
func (c Config) RecentCommits(ctx context.Context) ([]RevisionOption, error) {
	if c.RepositoryRoot == "" {
		return nil, errors.New("Git comparison is not configured")
	}
	output, err := runGit(ctx, c.RepositoryRoot,
		"--no-pager", "log", "-z", "--all", "--date-order",
		fmt.Sprintf("--max-count=%d", maxRevisionOptions), "--format=%H%x00%s")
	if err != nil {
		return nil, fmt.Errorf("list recent Git commits: %w", err)
	}
	options, err := parseRevisionOptions(output)
	if err != nil {
		return nil, err
	}
	return options, nil
}

// parseRevisionOptions decodes alternating full object IDs and subjects. Git
// `log -z` supplies the final record terminator, so every valid field sequence
// ends in NUL and contains an even number of non-terminal fields.
func parseRevisionOptions(output []byte) ([]RevisionOption, error) {
	if len(output) == 0 {
		return []RevisionOption{}, nil
	}
	if !utf8.Valid(output) || output[len(output)-1] != 0 {
		return nil, fmt.Errorf("%w: output is not terminated UTF-8", ErrInvalidRevisionList)
	}
	fields := bytes.Split(output[:len(output)-1], []byte{0})
	if len(fields)%2 != 0 || len(fields)/2 > maxRevisionOptions {
		return nil, fmt.Errorf("%w: unexpected field count", ErrInvalidRevisionList)
	}

	options := make([]RevisionOption, 0, len(fields)/2)
	seen := make(map[string]struct{}, len(fields)/2)
	for index := 0; index < len(fields); index += 2 {
		commit := strings.ToLower(string(fields[index]))
		if !validObjectID(commit) {
			return nil, fmt.Errorf("%w: invalid commit identifier", ErrInvalidRevisionList)
		}
		if _, duplicate := seen[commit]; duplicate {
			return nil, fmt.Errorf("%w: duplicate commit", ErrInvalidRevisionList)
		}
		seen[commit] = struct{}{}
		options = append(options, RevisionOption{Commit: commit, Subject: string(fields[index+1])})
	}
	return options, nil
}
