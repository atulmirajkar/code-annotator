// Package store persists annotation sidecars beneath a constrained writable root.
package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"atulm/code-annotator/internal/annotation"
)

var (
	// ErrConflict indicates that the expected revision is stale.
	ErrConflict = errors.New("annotation sidecar revision conflict")
	// ErrUnsafePath indicates that a sidecar path escapes through traversal,
	// symlinks, or a non-directory path component.
	ErrUnsafePath = errors.New("unsafe annotation sidecar path")
)

// Revision is the SHA-256 fingerprint of the exact persisted sidecar bytes. An
// empty revision represents a sidecar that does not yet exist.
type Revision string

// Store serializes access to versioned JSON sidecars under one resolved root.
type Store struct {
	root string
	mu   sync.Mutex
}

// Open creates the annotation directory when needed and resolves it as the
// immutable writable root for a Store.
func Open(rootPath string) (*Store, error) {
	if strings.TrimSpace(rootPath) == "" {
		return nil, errors.New("open annotation store: root path is required")
	}
	abs, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, fmt.Errorf("make annotation root absolute: %w", err)
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, fmt.Errorf("create annotation root: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("resolve annotation root: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, fmt.Errorf("stat annotation root: %w", err)
	}
	if !info.IsDir() {
		return nil, errors.New("open annotation store: root is not a directory")
	}
	return &Store{root: filepath.Clean(resolved)}, nil
}

// Root returns the absolute, symlink-resolved annotation storage root.
func (s *Store) Root() string {
	return s.root
}

// Load reads a document sidecar and its revision. A missing sidecar is returned
// as a valid empty schema with an empty revision.
func (s *Store) Load(document string) (annotation.Sidecar, Revision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load(document)
}

// Save atomically persists a complete sidecar only if expected matches the
// current revision. Use an empty expected revision to create a new sidecar.
func (s *Store) Save(sidecar annotation.Sidecar, expected Revision) (Revision, error) {
	if err := sidecar.Validate(); err != nil {
		return "", fmt.Errorf("save annotation sidecar: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	target, err := s.targetPath(sidecar.Document, true)
	if err != nil {
		return "", err
	}
	existingData, current, err := readSidecarBytes(target)
	if err != nil {
		return "", err
	}
	if current != expected {
		return current, fmt.Errorf("%w: expected %q, current %q", ErrConflict, expected, current)
	}

	encoded, err := json.MarshalIndent(sidecar, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode annotation sidecar: %w", err)
	}
	if existingData != nil {
		encoded, err = mergeUnknownJSON(existingData, encoded)
		if err != nil {
			return "", fmt.Errorf("preserve annotation sidecar fields: %w", err)
		}
	}
	encoded = append(encoded, '\n')

	if err := atomicWrite(target, encoded); err != nil {
		return "", fmt.Errorf("write annotation sidecar: %w", err)
	}
	return revisionOf(encoded), nil
}

// load performs the locked portion of Load. It validates both the requested
// document path and the document identity stored inside an existing sidecar.
func (s *Store) load(document string) (annotation.Sidecar, Revision, error) {
	if err := annotation.ValidateDocumentPath(document); err != nil {
		return annotation.Sidecar{}, "", fmt.Errorf("load annotation sidecar: %w", err)
	}
	target, err := s.targetPath(document, false)
	if err != nil {
		return annotation.Sidecar{}, "", err
	}
	data, revision, err := readSidecarBytes(target)
	if err != nil {
		return annotation.Sidecar{}, "", err
	}
	if data == nil {
		return annotation.Sidecar{
			SchemaVersion: annotation.SchemaVersion,
			Document:      document,
			Annotations:   []annotation.Annotation{},
		}, "", nil
	}

	var sidecar annotation.Sidecar
	if err := json.Unmarshal(data, &sidecar); err != nil {
		return annotation.Sidecar{}, "", fmt.Errorf("decode annotation sidecar: %w", err)
	}
	if err := sidecar.Validate(); err != nil {
		return annotation.Sidecar{}, "", fmt.Errorf("validate annotation sidecar: %w", err)
	}
	if sidecar.Document != document {
		return annotation.Sidecar{}, "", fmt.Errorf("annotation sidecar document %q does not match requested %q", sidecar.Document, document)
	}
	return sidecar, revision, nil
}

// targetPath maps a canonical document path to its mirrored sidecar path. It
// optionally creates safe parent directories and rejects symlinks or special
// files at the final target.
func (s *Store) targetPath(document string, createParent bool) (string, error) {
	if err := annotation.ValidateDocumentPath(document); err != nil {
		return "", fmt.Errorf("annotation sidecar path: %w", err)
	}
	target := filepath.Join(s.root, filepath.FromSlash(document)+".json")
	if !within(s.root, target) {
		return "", ErrUnsafePath
	}
	parent := filepath.Dir(target)
	if createParent {
		if err := s.ensureDirectories(parent); err != nil {
			return "", err
		}
	} else if err := s.verifyExistingParents(parent); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return target, nil
		}
		return "", err
	}

	info, err := os.Lstat(target)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return target, nil
		}
		return "", fmt.Errorf("inspect annotation sidecar: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", ErrUnsafePath
	}
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", fmt.Errorf("resolve annotation sidecar: %w", err)
	}
	if !within(s.root, resolved) {
		return "", ErrUnsafePath
	}
	return target, nil
}

// ensureDirectories creates each missing directory between the store root and
// directory. Walking one component at a time lets the store reject an existing
// symlink or non-directory before descending through it.
func (s *Store) ensureDirectories(directory string) error {
	relative, err := filepath.Rel(s.root, directory)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return ErrUnsafePath
	}

	// Begin at the already-resolved store root and validate every component.
	// MkdirAll cannot be used here because it may traverse an existing symlink.
	current := s.root
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		info, statErr := os.Lstat(current)
		if statErr != nil {
			if !errors.Is(statErr, fs.ErrNotExist) {
				return fmt.Errorf("inspect annotation directory: %w", statErr)
			}
			if err := os.Mkdir(current, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
				return fmt.Errorf("create annotation directory: %w", err)
			}
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return ErrUnsafePath
		}
	}

	// Resolve the completed path once more to catch a component replaced during
	// the walk and ensure the result remains confined to the store root.
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil || !within(s.root, resolved) {
		return ErrUnsafePath
	}
	return nil
}

// verifyExistingParents checks that a read-only lookup's existing parent path
// resolves beneath the store root. A missing parent is returned to the caller,
// which treats it as a sidecar that has not been created yet.
func (s *Store) verifyExistingParents(directory string) error {
	if !within(s.root, directory) {
		return ErrUnsafePath
	}
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return err
	}
	if !within(s.root, resolved) {
		return ErrUnsafePath
	}
	return nil
}

// readSidecarBytes returns the exact stored bytes and their revision. A missing
// sidecar is represented by nil bytes and an empty revision rather than an error.
func readSidecarBytes(target string) ([]byte, Revision, error) {
	data, err := os.ReadFile(target)
	if err != nil {
		// Absence is a normal state rather than an error.
		if errors.Is(err, fs.ErrNotExist) {
			return nil, "", nil
		}
		return nil, "", fmt.Errorf("read annotation sidecar: %w", err)
	}
	return data, revisionOf(data), nil
}

// atomicWrite durably writes data to a temporary file in the target directory
// before renaming it over the destination. Using the same directory keeps the
// rename on one filesystem, which is required for atomic replacement.
func atomicWrite(target string, data []byte) (returnErr error) {
	directory := filepath.Dir(target)
	temporary, err := os.CreateTemp(directory, ".code-annotator-annotations-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() {
		if returnErr != nil {
			_ = os.Remove(temporaryPath)
		}
	}()

	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	// The fully written temporary file becomes visible at the destination in a
	// single rename, so readers never observe a partially written JSON document.
	if err := os.Rename(temporaryPath, target); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		// Syncing the directory persists the rename itself on systems that support
		// directory fsync. Windows does not allow directories to be synced this way.
		directoryFile, err := os.Open(directory)
		if err != nil {
			return err
		}
		syncErr := directoryFile.Sync()
		closeErr := directoryFile.Close()
		if syncErr != nil {
			return syncErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

// revisionOf fingerprints the exact persisted representation, including its
// formatting and trailing newline, for optimistic concurrency checks.
func revisionOf(data []byte) Revision {
	digest := sha256.Sum256(data)
	return Revision(hex.EncodeToString(digest[:]))
}

// within reports whether candidate is root itself or one of its descendants.
// Both paths must already be absolute and cleaned by their callers.
func within(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

// mergeUnknownJSON carries forward fields from a newer schema while allowing
// all fields understood by this version to be changed or cleared normally.
//
// Example:
//
//	existing: {"document":"README.md","futureRoot":true}
//	updated:  {"document":"guide.md"}
//	result:   {"document":"guide.md","futureRoot":true}
func mergeUnknownJSON(existing, updated []byte) ([]byte, error) {
	var oldRoot map[string]any
	if err := json.Unmarshal(existing, &oldRoot); err != nil {
		return nil, err
	}
	var newRoot map[string]any
	if err := json.Unmarshal(updated, &newRoot); err != nil {
		return nil, err
	}

	mergeUnknownObject(oldRoot, newRoot, knownRootFields)
	mergeObjectList(oldRoot["annotations"], newRoot["annotations"], mergeAnnotationUnknown)
	return json.MarshalIndent(newRoot, "", "  ")
}

// These sets distinguish schema fields owned by this version from future fields
// that must survive a read-modify-write cycle.
var (
	knownRootFields       = fieldSet("schemaVersion", "document", "annotations")
	knownAnnotationFields = fieldSet(
		"id", "intent", "status", "comment", "author", "createdAt", "updatedAt", "source", "thread",
	)
	knownSourceFields   = fieldSet("sha256", "selector")
	knownSelectorFields = fieldSet("exact", "prefix", "suffix", "startByte", "endByte", "startLine", "endLine")
	knownThreadFields   = fieldSet(
		"id", "kind", "message", "summary", "commit", "author", "actorRole", "fromStatus", "toStatus", "createdAt",
	)
)

// fieldSet builds a membership set used to identify known schema fields.
func fieldSet(fields ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		set[field] = struct{}{}
	}
	return set
}

// mergeAnnotationUnknown preserves unknown fields throughout one matched
// annotation, including its source selector and thread entries.
//
// Example:
//
//	existing: {"id":"ann_1","comment":"old","futureFlag":true}
//	updated:  {"id":"ann_1","comment":"new"}
//	result:   {"id":"ann_1","comment":"new","futureFlag":true}
func mergeAnnotationUnknown(oldAnnotation, newAnnotation map[string]any) {
	mergeUnknownObject(oldAnnotation, newAnnotation, knownAnnotationFields)
	mergeNestedObject(oldAnnotation["source"], newAnnotation["source"], knownSourceFields)

	oldSource, oldOK := oldAnnotation["source"].(map[string]any)
	newSource, newOK := newAnnotation["source"].(map[string]any)
	if oldOK && newOK {
		mergeNestedObject(oldSource["selector"], newSource["selector"], knownSelectorFields)
	}
	mergeObjectList(oldAnnotation["thread"], newAnnotation["thread"], func(oldEntry, newEntry map[string]any) {
		mergeUnknownObject(oldEntry, newEntry, knownThreadFields)
	})
}

// mergeNestedObject merges unknown fields only when both schema versions retain
// the nested object. This prevents a removed known object from being restored.
//
// Example:
//
//	existing: {"sha256":"old","futureSource":"keep"}
//	updated:  {"sha256":"new"}
//	result:   {"sha256":"new","futureSource":"keep"}
//
// If updated has no source object, the result remains absent.
func mergeNestedObject(oldValue, newValue any, known map[string]struct{}) {
	oldObject, oldOK := oldValue.(map[string]any)
	newObject, newOK := newValue.(map[string]any)
	if oldOK && newOK {
		mergeUnknownObject(oldObject, newObject, known)
	}
}

// mergeUnknownObject copies fields not owned by the current schema without
// overwriting values already supplied by the updated representation.
//
// Example with "comment" identified as a known field:
//
//	existing: {"comment":"old","futureColor":"blue"}
//	updated:  {"comment":"new","futureColor":"green"}
//	result:   {"comment":"new","futureColor":"green"}
func mergeUnknownObject(oldObject, newObject map[string]any, known map[string]struct{}) {
	for key, value := range oldObject {
		if _, isKnown := known[key]; !isKnown {
			if _, alreadySet := newObject[key]; !alreadySet {
				newObject[key] = value
			}
		}
	}
}

// mergeObjectList pairs old and updated list entries by stable ID before
// delegating their field-level merge. Removed entries are not resurrected.
//
// Example:
//
//	existing: [{"id":"a","futureFlag":true},{"id":"removed"}]
//	updated:  [{"id":"a","comment":"new"},{"id":"added"}]
//	result:   [{"id":"a","comment":"new","futureFlag":true},{"id":"added"}]
func mergeObjectList(oldValue, newValue any, merge func(map[string]any, map[string]any)) {
	oldList, oldOK := oldValue.([]any)
	newList, newOK := newValue.([]any)
	if !oldOK || !newOK {
		return
	}
	oldByID := make(map[string]map[string]any, len(oldList))
	// Indexing avoids relying on list order, which can change during an update.
	for _, item := range oldList {
		if object, ok := item.(map[string]any); ok {
			if id, ok := object["id"].(string); ok {
				oldByID[id] = object
			}
		}
	}
	for _, item := range newList {
		if object, ok := item.(map[string]any); ok {
			if id, ok := object["id"].(string); ok {
				if oldObject, exists := oldByID[id]; exists {
					merge(oldObject, object)
				}
			}
		}
	}
}
