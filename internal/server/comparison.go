package server

import (
	"context"
	"errors"
	"sync"

	"atulm/code-annotator/internal/gitdiff"
)

// errUnknownCommit reports a selection whose commit is absent from the current
// bounded option listing. Selection never accepts an arbitrary revision.
var errUnknownCommit = errors.New("commit is not a selectable comparison option")

// comparisonController owns the server-wide comparison base. The base is always
// one explicit commit: startup pins the configured revision, and each selection
// pins a commit chosen from a freshly listed bounded option set. Reads take a
// value copy under the read lock so a concurrent selection cannot alter a base
// mid-request.
type comparisonController struct {
	configured gitdiff.Config
	origin     string
	token      string
	mu         sync.RWMutex
	base       gitdiff.Config
}

// newComparisonController pins the initial base to the startup-resolved commit.
// A non-empty loopback origin and control token enable selection mutations.
func newComparisonController(configured gitdiff.Config, origin, token string) (*comparisonController, error) {
	if configured.RepositoryRoot == "" || configured.RequestedBase == "" || configured.BaseCommit == "" {
		return nil, errors.New("configure Git comparison: incomplete configuration")
	}
	return &comparisonController{configured: configured, origin: origin, token: token, base: configured}, nil
}

// active returns the current comparison base used for diff and change queries.
func (c *comparisonController) active() gitdiff.Config {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.base
}

// options lists the bounded selectable commits.
func (c *comparisonController) options(ctx context.Context) ([]gitdiff.RevisionOption, error) {
	return c.configured.RecentCommits(ctx)
}

// selectCommit pins the comparison base to a commit present in a freshly listed
// bounded option set. Validating against a live listing keeps the endpoint from
// accepting an arbitrary revision without caching a server-side option list.
func (c *comparisonController) selectCommit(ctx context.Context, commit string) (gitdiff.Config, error) {
	options, err := c.options(ctx)
	if err != nil {
		return gitdiff.Config{}, err
	}
	if !containsCommit(options, commit) {
		return gitdiff.Config{}, errUnknownCommit
	}
	base := c.configured
	base.BaseCommit = commit
	base.RequestedBase = abbreviatedCommit(commit)
	c.mu.Lock()
	c.base = base
	c.mu.Unlock()
	return base, nil
}

// containsCommit reports whether commit appears in the bounded option listing.
func containsCommit(options []gitdiff.RevisionOption, commit string) bool {
	for _, option := range options {
		if option.Commit == commit {
			return true
		}
	}
	return false
}
