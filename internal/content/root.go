// Package content provides filesystem access constrained to a configured root.
package content

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

var (
	// ErrInvalidPath indicates that a requested path is malformed or absolute.
	ErrInvalidPath = errors.New("invalid content path")
	// ErrOutsideRoot indicates that a requested path resolves outside the root.
	ErrOutsideRoot = errors.New("content path is outside root")
	// ErrNotRegular indicates that a requested path is not a regular file.
	ErrNotRegular = errors.New("content path is not a regular file")
	// ErrTooLarge indicates that a file exceeds the configured read limit.
	ErrTooLarge = errors.New("content file is too large")
)

// Root provides access to regular files contained by a directory. The root is
// converted to an absolute, symlink-resolved path when it is opened.
type Root struct {
	path string
}

// Open validates path as a readable directory and returns a constrained Root.
func Open(rootPath string) (*Root, error) {
	if strings.TrimSpace(rootPath) == "" {
		return nil, fmt.Errorf("open content root: %w", ErrInvalidPath)
	}

	abs, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, fmt.Errorf("make content root absolute: %w", err)
	}

	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("resolve content root: %w", err)
	}

	info, err := os.Stat(resolved)
	if err != nil {
		return nil, fmt.Errorf("stat content root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("open content root: %w", ErrInvalidPath)
	}

	return &Root{path: filepath.Clean(resolved)}, nil
}

// Path returns the absolute, symlink-resolved content root.
func (r *Root) Path() string {
	return r.path
}

// ResolveFile resolves a slash-separated relative path to an existing regular file
// inside the root. It rejects lexical traversal and symlinks escaping the root.
func (r *Root) ResolveFile(requestPath string) (string, error) {
	relative, err := cleanRequestPath(requestPath)
	if err != nil {
		return "", err
	}

	candidate := filepath.Join(r.path, filepath.FromSlash(relative))
	if !isWithin(r.path, candidate) {
		return "", ErrOutsideRoot
	}

	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", err
	}
	if !isWithin(r.path, resolved) {
		return "", ErrOutsideRoot
	}

	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", ErrNotRegular
	}

	return resolved, nil
}

// ReadFile reads a root-contained regular file, rejecting files larger than
// maxBytes. A non-positive limit is invalid rather than meaning unlimited.
func (r *Root) ReadFile(requestPath string, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("read content file: invalid size limit %d", maxBytes)
	}

	resolved, err := r.ResolveFile(requestPath)
	if err != nil {
		return nil, err
	}

	file, err := os.Open(resolved)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > maxBytes {
		return nil, ErrTooLarge
	}

	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, ErrTooLarge
	}

	return data, nil
}

func cleanRequestPath(requestPath string) (string, error) {
	if requestPath == "" || strings.ContainsRune(requestPath, '\x00') {
		return "", ErrInvalidPath
	}

	// Treat backslashes as separators on every platform so a path accepted on
	// Unix cannot become traversal when the same binary is built for Windows.
	normalized := strings.ReplaceAll(requestPath, `\`, "/")
	if strings.HasPrefix(normalized, "/") {
		return "", ErrInvalidPath
	}

	cleaned := path.Clean(normalized)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", ErrOutsideRoot
	}

	return cleaned, nil
}

func isWithin(rootPath, candidate string) bool {
	relative, err := filepath.Rel(rootPath, candidate)
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

// IsNotExist reports whether err was caused by a missing content path.
func IsNotExist(err error) bool {
	return errors.Is(err, fs.ErrNotExist)
}
