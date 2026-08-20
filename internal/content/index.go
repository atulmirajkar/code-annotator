package content

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

// Document describes a Markdown file relative to a Root.
type Document struct {
	Path      string
	Name      string
	Directory string
}

// Index is a snapshot of the Markdown documents currently present in a Root.
// Calling Root.Index again creates a fresh snapshot from disk.
type Index struct {
	Documents   []Document
	DefaultPath string
}

// Index discovers Markdown files recursively. Hidden files and directories are
// omitted, symlinked directories are not traversed, and unsafe symlinked files
// are excluded.
func (r *Root) Index() (Index, error) {
	var documents []Document

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
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
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
	if index.DefaultPath == "" && len(documents) > 0 {
		index.DefaultPath = documents[0].Path
	}

	return index, nil
}

func isHidden(name string) bool {
	return strings.HasPrefix(name, ".")
}
