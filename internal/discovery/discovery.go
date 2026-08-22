// Package discovery lets a review-mode server advertise its loopback URL to
// agent tooling without a human copying it over, and lets that tooling find
// it again. The registry never carries the review mutation token; a
// discovering client still has to fetch that from the loopback page itself,
// exactly as when the URL is supplied directly.
package discovery

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const schemaVersion = 1

const stateDirEnv = "CODE_ANNOTATOR_STATE_DIR"

// Entry describes one running review-mode server. It intentionally excludes
// the review mutation token.
type Entry struct {
	SchemaVersion int       `json:"schemaVersion"`
	PID           int       `json:"pid"`
	URL           string    `json:"url"`
	Root          string    `json:"root"`
	StartedAt     time.Time `json:"startedAt"`
}

// StateDir returns the directory holding one registry file per running
// review-mode server. CODE_ANNOTATOR_STATE_DIR overrides the default
// per-user location, which keeps tests hermetic and lets a user relocate
// state explicitly.
func StateDir() (string, error) {
	if override := os.Getenv(stateDirEnv); override != "" {
		return filepath.Join(override, "servers"), nil
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve discovery state directory: %w", err)
	}
	return filepath.Join(configDir, "code-annotator", "servers"), nil
}

// Register advertises a running review-mode server at url for the given
// content root, returning a cleanup func that removes the registry entry.
// The caller should defer cleanup immediately so every exit path retracts
// the advertisement.
func Register(root, url string) (cleanup func(), err error) {
	directory, err := StateDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create discovery state directory: %w", err)
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve discovery content root: %w", err)
	}
	entry := Entry{
		SchemaVersion: schemaVersion,
		PID:           os.Getpid(),
		URL:           url,
		Root:          absoluteRoot,
		StartedAt:     time.Now().UTC(),
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return nil, fmt.Errorf("encode discovery entry: %w", err)
	}
	target := entryPath(directory, entry.PID)
	if err := atomicWrite(target, data); err != nil {
		return nil, fmt.Errorf("write discovery entry: %w", err)
	}
	return func() { _ = os.Remove(target) }, nil
}

// List returns every parseable registry entry in the state directory.
// Malformed or foreign files are skipped rather than treated as an error, so
// one broken entry never breaks discovery for the rest.
func List() ([]Entry, error) {
	directory, err := StateDir()
	if err != nil {
		return nil, err
	}
	files, err := os.ReadDir(directory)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read discovery state directory: %w", err)
	}
	var entries []Entry
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(directory, file.Name()))
		if err != nil {
			continue
		}
		var entry Entry
		if err := json.Unmarshal(data, &entry); err != nil {
			continue
		}
		if entry.SchemaVersion != schemaVersion || entry.URL == "" {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// Remove deletes the registry entry for pid, if present. Callers use this to
// self-heal after finding a dead entry (for example, one left behind by a
// server that was killed instead of shut down cleanly).
func Remove(pid int) error {
	directory, err := StateDir()
	if err != nil {
		return err
	}
	if err := os.Remove(entryPath(directory, pid)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove discovery entry: %w", err)
	}
	return nil
}

func entryPath(directory string, pid int) string {
	return filepath.Join(directory, strconv.Itoa(pid)+".json")
}

// atomicWrite durably writes data to a temporary file in the target
// directory before renaming it over the destination, so a discovering
// reader never observes a partially written entry.
func atomicWrite(target string, data []byte) (returnErr error) {
	directory := filepath.Dir(target)
	temporary, err := os.CreateTemp(directory, ".code-annotator-discovery-*.tmp")
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
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, target)
}
