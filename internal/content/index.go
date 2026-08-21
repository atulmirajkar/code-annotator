package content

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

const (
	maxCodeExtensions      = 64
	maxExcludedDirectories = 128
)

// Kind identifies how a reviewable document is rendered.
type Kind string

const (
	KindMarkdown Kind = "markdown"
	KindCode     Kind = "code"
)

var defaultCodeExtensions = []string{".go", ".cs", ".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".json", ".csproj"}
var defaultExcludedDirectories = []string{"node_modules", "vendor", "bin", "obj"}

// DefaultCodeExtensions returns a copy of the initial opt-in source catalog.
func DefaultCodeExtensions() []string { return append([]string(nil), defaultCodeExtensions...) }

// DefaultExcludedDirectories returns dependency and build-output defaults.
func DefaultExcludedDirectories() []string {
	return append([]string(nil), defaultExcludedDirectories...)
}

// IndexOptions configures additional reviewable files and directory pruning.
// Values must be normalized with NewIndexOptions before use.
type IndexOptions struct {
	CodeExtensions      []string
	ExcludedDirectories []string
}

// Document describes a reviewable file relative to a Root.
type Document struct {
	Path      string
	Name      string
	Directory string
	Kind      Kind
	Language  string
}

// Index is a snapshot of the reviewable documents currently present in a Root.
// Calling Root.Index or Root.IndexWithOptions again creates a fresh snapshot.
type Index struct {
	Documents   []Document
	DefaultPath string
}

// Index discovers Markdown files recursively. Hidden files and directories are
// omitted, symlinked directories are not traversed, and unsafe symlinked files
// are excluded.
func (r *Root) Index() (Index, error) {
	return r.IndexWithOptions(IndexOptions{})
}

// IndexWithOptions discovers Markdown plus configured source files. Hidden and
// explicitly excluded directories are pruned before their contents are read.
func (r *Root) IndexWithOptions(options IndexOptions) (Index, error) {
	var documents []Document
	extensions := stringSet(options.CodeExtensions)
	excluded := stringSet(options.ExcludedDirectories)

	err := filepath.WalkDir(r.path, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filePath == r.path {
			return nil
		}

		if isHidden(entry.Name()) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			if _, skip := excluded[strings.ToLower(entry.Name())]; skip {
				return filepath.SkipDir
			}
			return nil
		}

		extension := strings.ToLower(filepath.Ext(entry.Name()))
		kind := KindCode
		if extension == ".md" {
			kind = KindMarkdown
		} else if _, ok := extensions[extension]; !ok {
			return nil
		}

		relative, err := filepath.Rel(r.path, filePath)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)

		// ResolveFile applies the same regular-file and containment checks used
		// when the document is later requested. An unsafe or broken symlink is
		// not a document in this root.
		if _, err := r.ResolveFile(relative); err != nil {
			if entry.Type()&fs.ModeSymlink != 0 {
				return nil
			}
			return err
		}

		directory := filepath.ToSlash(filepath.Dir(relative))
		if directory == "." {
			directory = ""
		}
		documents = append(documents, Document{
			Path:      relative,
			Name:      entry.Name(),
			Directory: directory,
			Kind:      kind,
			Language:  languageForExtension(extension),
		})
		return nil
	})
	if err != nil {
		return Index{}, err
	}

	sort.Slice(documents, func(i, j int) bool {
		left := strings.ToLower(documents[i].Path)
		right := strings.ToLower(documents[j].Path)
		if left == right {
			return documents[i].Path < documents[j].Path
		}
		return left < right
	})

	index := Index{Documents: documents}
	for _, document := range documents {
		if document.Directory == "" && strings.EqualFold(document.Name, "README.md") {
			index.DefaultPath = document.Path
			break
		}
	}
	if index.DefaultPath == "" {
		for _, document := range documents {
			if document.Kind == KindMarkdown {
				index.DefaultPath = document.Path
				break
			}
		}
	}
	if index.DefaultPath == "" && len(documents) > 0 {
		index.DefaultPath = documents[0].Path
	}

	return index, nil
}

// NewIndexOptions validates and normalizes user-supplied catalog values.
func NewIndexOptions(codeExtensions, excludedDirectories []string) (IndexOptions, error) {
	extensions, err := normalizeNames(codeExtensions, maxCodeExtensions, true)
	if err != nil {
		return IndexOptions{}, err
	}
	excluded, err := normalizeNames(excludedDirectories, maxExcludedDirectories, false)
	if err != nil {
		return IndexOptions{}, err
	}
	return IndexOptions{CodeExtensions: extensions, ExcludedDirectories: excluded}, nil
}

// normalizeNames canonicalizes one ordered, case-insensitive configuration
// list while rejecting values that could be interpreted as paths.
func normalizeNames(values []string, limit int, extension bool) ([]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	if len(values) > limit {
		return nil, fmt.Errorf("at most %d values are allowed", limit)
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		invalid := value == "" || value == "." || value == ".." || strings.ContainsAny(value, `/\\`)
		if extension {
			invalid = invalid || !strings.HasPrefix(value, ".") || len(value) == 1
		}
		if invalid {
			return nil, errors.New("catalog values must be non-empty base names; extensions require a leading dot")
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result, nil
}

// stringSet converts normalized names into a case-insensitive lookup set.
func stringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[strings.ToLower(value)] = struct{}{}
	}
	return result
}

// languageForExtension returns stable display metadata without invoking any
// language parser or inspecting file contents.
func languageForExtension(extension string) string {
	languages := map[string]string{
		".md": "markdown", ".go": "go", ".cs": "csharp", ".js": "javascript",
		".jsx": "javascript", ".mjs": "javascript", ".cjs": "javascript",
		".ts": "typescript", ".tsx": "typescript", ".json": "json", ".csproj": "msbuild",
	}
	if language := languages[extension]; language != "" {
		return language
	}
	return strings.TrimPrefix(extension, ".")
}

// isHidden applies the catalog's basename-based hidden-file policy.
func isHidden(name string) bool {
	return strings.HasPrefix(name, ".")
}
