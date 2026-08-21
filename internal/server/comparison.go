package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"

	"atulm/md-viewer/internal/gitdiff"
)

// errStaleComparison reports an If-Match revision that no longer matches the
// active comparison snapshot, meaning another tab already changed the base.
var errStaleComparison = errors.New("comparison state revision is stale")

// errUnknownCommit reports a selection whose commit is absent from the current
// bounded option snapshot. Selection never accepts an arbitrary revision.
var errUnknownCommit = errors.New("commit is not a selectable comparison option")

// comparisonSnapshot is an immutable view of the active Git comparison base and
// the bounded options offered to the browser when it was built. A snapshot is
// never mutated after publication, so every request reads a consistent state.
type comparisonSnapshot struct {
	// config is the active base identity used for all diff and change queries.
	config gitdiff.Config
	// configuredName is the startup revision expression, such as HEAD.
	configuredName string
	// configuredCommit is the latest resolved commit of configuredName.
	configuredCommit string
	// explicit reports that the active base is a pinned commit chosen from the
	// option list rather than the moving configured revision.
	explicit bool
	// options holds the recent local commits offered alongside configuredName.
	options []gitdiff.RevisionOption
	// revision is an opaque token that changes on every successful swap.
	revision string
}

// comparisonController owns concurrency-safe comparison state. Diff requests
// read an immutable snapshot; refresh and selection build a validated
// replacement before atomically swapping it under the write lock, so a failure
// always leaves the previous snapshot usable.
type comparisonController struct {
	configured gitdiff.Config
	token      string
	mu         sync.RWMutex
	current    comparisonSnapshot
}

// newComparisonController seeds the initial snapshot from the startup-resolved
// base. Options may be empty until the first refresh; diffs use only the base.
func newComparisonController(configured gitdiff.Config, options []gitdiff.RevisionOption, token string) (*comparisonController, error) {
	if configured.RepositoryRoot == "" || configured.RequestedBase == "" || configured.BaseCommit == "" {
		return nil, errors.New("configure Git comparison: incomplete configuration")
	}
	revision, err := newStateRevision()
	if err != nil {
		return nil, err
	}
	return &comparisonController{
		configured: configured,
		token:      token,
		current: comparisonSnapshot{
			config:           configured,
			configuredName:   configured.RequestedBase,
			configuredCommit: configured.BaseCommit,
			explicit:         false,
			options:          options,
			revision:         revision,
		},
	}, nil
}

// snapshot returns the active immutable comparison state. Callers treat the
// returned slices as read-only and reuse one snapshot for a whole request.
func (c *comparisonController) snapshot() comparisonSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.current
}

// refresh performs a fresh bounded option lookup. When the moving configured
// revision is active it also re-resolves and adopts that revision's new tip;
// when a pinned commit is active it keeps that commit and only updates options.
// The previous snapshot remains active if any Git step fails.
func (c *comparisonController) refresh(ctx context.Context) (comparisonSnapshot, error) {
	previous := c.snapshot()
	resolved, options, err := c.rebuild(ctx)
	if err != nil {
		return comparisonSnapshot{}, err
	}
	replacement := comparisonSnapshot{
		configuredName:   c.configured.RequestedBase,
		configuredCommit: resolved.BaseCommit,
		options:          options,
	}
	if previous.explicit {
		replacement.config = previous.config
		replacement.explicit = true
	} else {
		replacement.config = resolved
		replacement.explicit = false
	}
	return c.commit(previous.revision, replacement)
}

// selectCommit adopts a commit from the current option snapshot. Selecting the
// configured revision's current commit returns to moving mode; any other listed
// commit pins the base. ifMatch guards against overwriting another tab's change.
func (c *comparisonController) selectCommit(ctx context.Context, ifMatch, commit string) (comparisonSnapshot, error) {
	previous := c.snapshot()
	if ifMatch != previous.revision {
		return comparisonSnapshot{}, errStaleComparison
	}
	moving := commit == previous.configuredCommit
	if !moving && !containsCommit(previous.options, commit) {
		return comparisonSnapshot{}, errUnknownCommit
	}
	resolved, options, err := c.rebuild(ctx)
	if err != nil {
		return comparisonSnapshot{}, err
	}
	replacement := comparisonSnapshot{
		configuredName:   c.configured.RequestedBase,
		configuredCommit: resolved.BaseCommit,
		options:          options,
	}
	if moving {
		replacement.config = resolved
		replacement.explicit = false
	} else {
		pinned := c.configured
		pinned.BaseCommit = commit
		pinned.RequestedBase = abbreviatedCommit(commit)
		replacement.config = pinned
		replacement.explicit = true
	}
	return c.commit(previous.revision, replacement)
}

// rebuild performs the Git work shared by refresh and selection: re-resolving
// the configured revision and re-listing bounded recent commits. It runs
// outside the write lock so slow Git calls never block concurrent readers.
func (c *comparisonController) rebuild(ctx context.Context) (gitdiff.Config, []gitdiff.RevisionOption, error) {
	resolved, err := c.configured.Reresolve(ctx)
	if err != nil {
		return gitdiff.Config{}, nil, err
	}
	options, err := c.configured.RecentCommits(ctx)
	if err != nil {
		return gitdiff.Config{}, nil, err
	}
	return resolved, options, nil
}

// commit atomically publishes replacement when expected still matches the
// active revision, assigning a new opaque state revision. A concurrent change
// between the caller's read and this swap returns errStaleComparison.
func (c *comparisonController) commit(expected string, replacement comparisonSnapshot) (comparisonSnapshot, error) {
	revision, err := newStateRevision()
	if err != nil {
		return comparisonSnapshot{}, err
	}
	replacement.revision = revision
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.current.revision != expected {
		return comparisonSnapshot{}, errStaleComparison
	}
	c.current = replacement
	return replacement, nil
}

// containsCommit reports whether commit appears in the bounded option list.
func containsCommit(options []gitdiff.RevisionOption, commit string) bool {
	for _, option := range options {
		if option.Commit == commit {
			return true
		}
	}
	return false
}

// newStateRevision returns 128 bits of opaque, unpredictable state identity.
// A fresh value on every swap lets stale browser tabs detect a changed base.
func newStateRevision() (string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate comparison state revision: %w", err)
	}
	return hex.EncodeToString(random), nil
}
