package gitdiff

import (
	"context"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
)

// ChangedPaths returns sorted, slash-separated paths relative to the viewer
// content root. It includes tracked differences from the frozen base and
// untracked non-ignored files, but does not mutate the index or worktree.
func (c Config) ChangedPaths(ctx context.Context) ([]string, error) {
	if c.RepositoryRoot == "" || !validObjectID(c.BaseCommit) || !validContentPrefix(c.ContentPrefix) {
		return nil, errors.New("Git comparison is not configured")
	}
	pathspec := c.contentPathspec()
	trackedArguments := []string{"diff", "--name-only", "-z", "--no-renames", "--no-ext-diff", "--no-textconv", c.BaseCommit, "--"}
	untrackedArguments := []string{"ls-files", "--others", "--exclude-standard", "-z", "--"}
	if pathspec != "" {
		trackedArguments = append(trackedArguments, pathspec)
		untrackedArguments = append(untrackedArguments, pathspec)
	}

	tracked, err := runGit(ctx, c.RepositoryRoot, trackedArguments...)
	if err != nil {
		return nil, fmt.Errorf("list tracked changes: %w", err)
	}
	untracked, err := runGit(ctx, c.RepositoryRoot, untrackedArguments...)
	if err != nil {
		return nil, fmt.Errorf("list untracked changes: %w", err)
	}

	changed := make(map[string]struct{})
	for _, output := range [][]byte{tracked, untracked} {
		paths, err := parseNULPaths(output)
		if err != nil {
			return nil, err
		}
		for _, repositoryPath := range paths {
			relative, ok := c.toContentPath(repositoryPath)
			if ok {
				changed[relative] = struct{}{}
			}
		}
	}
	result := make([]string, 0, len(changed))
	for changedPath := range changed {
		result = append(result, changedPath)
	}
	sort.Strings(result)
	return result, nil
}

// validContentPrefix accepts only the normalized repository-relative form
// produced by Open, even if a Config was manually constructed by another package.
func validContentPrefix(prefix string) bool {
	if prefix == "" {
		return true
	}
	cleaned := path.Clean(prefix)
	return cleaned == prefix && cleaned != "." && cleaned != ".." && !strings.HasPrefix(cleaned, "../") && !strings.HasPrefix(cleaned, "/")
}

// contentPathspec scopes Git enumeration to the configured content subtree.
// Literal pathspec magic prevents wildcard characters from being interpreted.
func (c Config) contentPathspec() string {
	if c.ContentPrefix == "" {
		return ""
	}
	return ":(literal)" + c.ContentPrefix
}

// toContentPath converts a Git repository-relative path to the viewer's path
// space and rejects malformed or out-of-scope command output.
func (c Config) toContentPath(repositoryPath string) (string, bool) {
	if repositoryPath == "" || strings.HasPrefix(repositoryPath, "/") {
		return "", false
	}
	cleaned := path.Clean(repositoryPath)
	if cleaned != repositoryPath || cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", false
	}
	if c.ContentPrefix == "" {
		return cleaned, true
	}
	prefix := strings.TrimSuffix(c.ContentPrefix, "/") + "/"
	if !strings.HasPrefix(cleaned, prefix) {
		return "", false
	}
	relative := strings.TrimPrefix(cleaned, prefix)
	return relative, relative != ""
}

// parseNULPaths decodes Git's unambiguous -z path format. A non-empty suffix
// without a terminator is treated as malformed rather than silently accepted.
func parseNULPaths(output []byte) ([]string, error) {
	if len(output) == 0 {
		return []string{}, nil
	}
	if output[len(output)-1] != 0 {
		return nil, errors.New("Git returned malformed path output")
	}
	values := strings.Split(string(output[:len(output)-1]), "\x00")
	for _, value := range values {
		if value == "" {
			return nil, errors.New("Git returned an empty changed path")
		}
	}
	return values, nil
}
