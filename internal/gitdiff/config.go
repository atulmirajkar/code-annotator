// Package gitdiff provides bounded, read-only Git comparisons for reviewable
// files. It owns process execution so HTTP and rendering code never invoke Git.
package gitdiff

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	commandTimeout  = 3 * time.Second
	maxCommandBytes = 64 << 10
)

// ErrOutputTooLarge reports that a Git process exceeded its bounded output.
var ErrOutputTooLarge = errors.New("git command output exceeded limit")

// Config freezes one locally resolved base commit and its relationship to the
// viewer content root for the lifetime of a server.
type Config struct {
	// RepositoryRoot is the canonical absolute worktree root returned by Git.
	RepositoryRoot string
	// ContentPrefix is the slash-separated content path within the worktree.
	ContentPrefix string
	// RequestedBase preserves the user-entered local revision for display.
	RequestedBase string
	// BaseCommit is the immutable full commit ID resolved at startup.
	BaseCommit string
}

// Open discovers the containing worktree and resolves requestedBase to an
// immutable commit without invoking a shell or contacting a remote.
func Open(ctx context.Context, contentRoot, requestedBase string) (Config, error) {
	if err := validateRevision(requestedBase); err != nil {
		return Config{}, err
	}
	root, err := canonicalDirectory(contentRoot)
	if err != nil {
		return Config{}, fmt.Errorf("resolve content root: %w", err)
	}
	repositoryOutput, err := runGit(ctx, root, "rev-parse", "--show-toplevel")
	if err != nil {
		return Config{}, errors.New("content root is not inside a Git worktree")
	}
	repositoryRoot, err := canonicalDirectory(strings.TrimSpace(string(repositoryOutput)))
	if err != nil {
		return Config{}, errors.New("Git returned an invalid worktree root")
	}
	prefix, err := contentPrefix(repositoryRoot, root)
	if err != nil {
		return Config{}, err
	}
	commitOutput, err := runGit(ctx, repositoryRoot, "rev-parse", "--verify", "--end-of-options", requestedBase+"^{commit}")
	if err != nil {
		return Config{}, fmt.Errorf("resolve Git base %q: revision is not a local commit", requestedBase)
	}
	commit := strings.TrimSpace(string(commitOutput))
	if !validObjectID(commit) {
		return Config{}, errors.New("Git returned an invalid commit identifier")
	}
	return Config{
		RepositoryRoot: repositoryRoot,
		ContentPrefix:  prefix,
		RequestedBase:  requestedBase,
		BaseCommit:     strings.ToLower(commit),
	}, nil
}

// validateRevision rejects values that Git could interpret as options or that
// would make diagnostics and command arguments ambiguous.
func validateRevision(revision string) error {
	if strings.TrimSpace(revision) == "" {
		return errors.New("Git base revision is required")
	}
	if strings.HasPrefix(revision, "-") || strings.ContainsAny(revision, "\x00\r\n") {
		return errors.New("Git base revision contains unsafe characters")
	}
	return nil
}

// canonicalDirectory resolves symlinks before containment checks so a viewer
// path cannot appear lexically inside a repository while resolving elsewhere.
func canonicalDirectory(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("path is not a directory")
	}
	return filepath.Clean(resolved), nil
}

// contentPrefix returns the slash-separated Git path from repository root to
// content root and rejects every path outside the discovered worktree.
func contentPrefix(repositoryRoot, contentRoot string) (string, error) {
	relative, err := filepath.Rel(repositoryRoot, contentRoot)
	if err != nil {
		return "", errors.New("content root is outside the Git worktree")
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", errors.New("content root is outside the Git worktree")
	}
	if relative == "." {
		return "", nil
	}
	return filepath.ToSlash(relative), nil
}

// runGit executes one bounded local Git command. The child inherits the normal
// environment while prompts and opportunistic repository writes are disabled.
func runGit(parent context.Context, directory string, arguments ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, commandTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, "git", append([]string{"-C", directory}, arguments...)...)
	command.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
	var stdout boundedBuffer
	var stderr boundedBuffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	if ctx.Err() != nil {
		return nil, errors.New("git command timed out")
	}
	if errors.Is(stdout.err, ErrOutputTooLarge) || errors.Is(stderr.err, ErrOutputTooLarge) {
		return nil, ErrOutputTooLarge
	}
	if err != nil {
		return nil, err
	}
	return stdout.Bytes(), nil
}

// boundedBuffer retains at most maxCommandBytes and then fails the command's
// output pipe, preventing untrusted repository output from growing memory use.
type boundedBuffer struct {
	buffer bytes.Buffer
	err    error
}

func (b *boundedBuffer) Write(value []byte) (int, error) {
	remaining := maxCommandBytes - b.buffer.Len()
	if len(value) > remaining {
		if remaining > 0 {
			_, _ = b.buffer.Write(value[:remaining])
		}
		b.err = ErrOutputTooLarge
		return len(value), ErrOutputTooLarge
	}
	return b.buffer.Write(value)
}

func (b *boundedBuffer) Bytes() []byte { return b.buffer.Bytes() }

// validObjectID accepts the full SHA-1 and SHA-256 object formats supported by
// Git repositories while rejecting abbreviated or decorated output.
func validObjectID(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, character := range value {
		if !strings.ContainsRune("0123456789abcdefABCDEF", character) {
			return false
		}
	}
	return true
}
