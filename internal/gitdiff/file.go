package gitdiff

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"
)

const (
	maxDiffSourceBytes = 4 << 20
	diffCommandTimeout = 5 * time.Second
)

// BuildFileDiff retrieves one frozen base blob and current-worktree patch, then
// validates and aligns them with the already containment-checked current bytes.
func (c Config) BuildFileDiff(ctx context.Context, documentPath string, current []byte) (FileDiff, error) {
	if c.RepositoryRoot == "" || !validObjectID(c.BaseCommit) || !validContentPrefix(c.ContentPrefix) {
		return FileDiff{}, errors.New("Git comparison is not configured")
	}
	if !validDocumentPath(documentPath) {
		return FileDiff{}, errors.New("invalid diff document path")
	}
	if len(current) > maxDiffSourceBytes {
		return FileDiff{}, fmt.Errorf("%w: current source exceeds %d bytes", ErrUnsupportedPatch, maxDiffSourceBytes)
	}

	repositoryPath := documentPath
	if c.ContentPrefix != "" {
		repositoryPath = path.Join(c.ContentPrefix, documentPath)
	}
	base, baseExists, err := c.readBaseBlob(ctx, repositoryPath)
	if err != nil {
		return FileDiff{}, err
	}
	patchBytes, err := runGitBounded(ctx, c.RepositoryRoot, diffCommandTimeout, maxPatchBytes,
		"diff", "--no-color", "--no-ext-diff", "--no-textconv", "--unified=0", "--no-renames",
		c.BaseCommit, "--", ":(literal)"+repositoryPath)
	if err != nil {
		return FileDiff{}, fmt.Errorf("read file patch: %w", err)
	}
	if !baseExists && len(patchBytes) == 0 && len(current) > 0 {
		patchBytes, err = addedFilePatch(current)
		if err != nil {
			return FileDiff{}, err
		}
	}
	return ParsePatch(documentPath, c.BaseCommit, base, current, patchBytes)
}

// readBaseBlob distinguishes a path absent from the frozen commit from a blob
// that exists but cannot be read within the source-size boundary.
func (c Config) readBaseBlob(ctx context.Context, repositoryPath string) ([]byte, bool, error) {
	object := c.BaseCommit + ":" + repositoryPath
	if _, err := runGitBounded(ctx, c.RepositoryRoot, diffCommandTimeout, maxCommandBytes, "cat-file", "-e", object); err != nil {
		if errors.Is(err, ErrOutputTooLarge) || strings.Contains(err.Error(), "timed out") {
			return nil, false, fmt.Errorf("inspect base blob: %w", err)
		}
		return []byte{}, false, nil
	}
	base, err := runGitBounded(ctx, c.RepositoryRoot, diffCommandTimeout, maxDiffSourceBytes, "cat-file", "blob", object)
	if err != nil {
		return nil, false, fmt.Errorf("read base blob: %w", err)
	}
	return base, true, nil
}

// addedFilePatch supplies the one case omitted by ordinary `git diff <base>`:
// an untracked current file. It uses the same zero-context hunk format as Git.
func addedFilePatch(current []byte) ([]byte, error) {
	lines := splitSourceLines(current)
	if len(lines) == 0 {
		return []byte{}, nil
	}
	var patch strings.Builder
	fmt.Fprintf(&patch, "@@ -0,0 +1,%d @@\n", len(lines))
	for _, line := range lines {
		patch.WriteByte('+')
		patch.WriteString(line.text)
		patch.WriteByte('\n')
		if patch.Len() > maxPatchBytes {
			return nil, fmt.Errorf("%w: synthesized patch exceeds %d bytes", ErrUnsupportedPatch, maxPatchBytes)
		}
	}
	return []byte(patch.String()), nil
}

// validDocumentPath accepts only the normalized slash-separated paths supplied
// by the safe content catalog before constructing a literal Git pathspec.
func validDocumentPath(documentPath string) bool {
	if documentPath == "" || strings.ContainsRune(documentPath, '\x00') || strings.Contains(documentPath, `\`) || strings.HasPrefix(documentPath, "/") {
		return false
	}
	cleaned := path.Clean(documentPath)
	return cleaned == documentPath && cleaned != "." && cleaned != ".." && !strings.HasPrefix(cleaned, "../")
}
