package commands

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"atulm/md-viewer/internal/annotation"
	annotationstore "atulm/md-viewer/internal/annotation/store"
	"atulm/md-viewer/internal/content"
)

type replyConfig struct {
	rootPath       string
	annotationsDir string
	annotationID   string
	author         string
	message        string
}

type resolveConfig struct {
	rootPath       string
	annotationsDir string
	annotationID   string
	input          annotation.TransitionInput
}

type mutationTarget struct {
	document string
	sidecar  annotation.Sidecar
	revision annotationstore.Revision
	index    int
}

type mutationOutput struct {
	Document   string                `json:"document"`
	Annotation annotation.Annotation `json:"annotation"`
	Revision   string                `json:"revision"`
}

func parseReplyConfig(args []string, stderr io.Writer) (replyConfig, error) {
	flags := flag.NewFlagSet("md-viewer annotations reply", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", "", "Markdown content root")
	annotationsDir := flags.String("annotations-dir", "", "annotation storage directory")
	identifier := flags.String("id", "", "annotation identifier")
	author := flags.String("author", "", "reply author")
	message := flags.String("message", "", "reply message")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: md-viewer annotations reply --root <directory> --id <annotation> --author <name> --message <text>")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return replyConfig{}, err
	}
	if flags.NArg() != 0 {
		flags.Usage()
		return replyConfig{}, errors.New("annotations reply does not accept positional arguments")
	}
	missing := ""
	required := []struct{ name, value string }{
		{name: "--root", value: *root},
		{name: "--id", value: *identifier},
		{name: "--author", value: *author},
		{name: "--message", value: *message},
	}
	for _, field := range required {
		if strings.TrimSpace(field.value) == "" {
			missing = field.name
			break
		}
	}
	if missing != "" {
		flags.Usage()
		return replyConfig{}, fmt.Errorf("%s is required", missing)
	}
	return replyConfig{rootPath: *root, annotationsDir: *annotationsDir, annotationID: *identifier, author: *author, message: *message}, nil
}

func parseResolveConfig(args []string, stderr io.Writer) (resolveConfig, error) {
	flags := flag.NewFlagSet("md-viewer annotations resolve", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", "", "Markdown content root")
	annotationsDir := flags.String("annotations-dir", "", "annotation storage directory")
	identifier := flags.String("id", "", "annotation identifier")
	status := flags.String("status", "", "target lifecycle status")
	role := flags.String("role", "", "actor role (agent or reviewer)")
	author := flags.String("author", "", "actor name")
	message := flags.String("message", "", "review or rejection message")
	summary := flags.String("summary", "", "applied-work summary")
	commit := flags.String("commit", "", "optional applied-work commit")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: md-viewer annotations resolve --root <directory> --id <annotation> --status <status> --role <role> --author <name> [options]")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return resolveConfig{}, err
	}
	if flags.NArg() != 0 {
		flags.Usage()
		return resolveConfig{}, errors.New("annotations resolve does not accept positional arguments")
	}
	required := []struct{ name, value string }{
		{name: "--root", value: *root},
		{name: "--id", value: *identifier},
		{name: "--status", value: *status},
		{name: "--role", value: *role},
		{name: "--author", value: *author},
	}
	for _, field := range required {
		if strings.TrimSpace(field.value) == "" {
			flags.Usage()
			return resolveConfig{}, fmt.Errorf("%s is required", field.name)
		}
	}
	input := annotation.TransitionInput{Status: annotation.Status(*status), ActorRole: annotation.ActorRole(*role), Author: *author, Message: *message, Summary: *summary, Commit: *commit}
	if !input.Status.Valid() {
		return resolveConfig{}, fmt.Errorf("invalid annotation status %q", input.Status)
	}
	if input.ActorRole != annotation.RoleAgent && input.ActorRole != annotation.RoleReviewer {
		return resolveConfig{}, fmt.Errorf("invalid annotation actor role %q", input.ActorRole)
	}
	return resolveConfig{rootPath: *root, annotationsDir: *annotationsDir, annotationID: *identifier, input: input}, nil
}

// runReply appends one ordinary discussion entry and saves against the exact
// revision loaded during annotation lookup.
func runReply(configuration replyConfig, output io.Writer) error {
	root, store, err := openMutationRoots(configuration.rootPath, configuration.annotationsDir)
	if err != nil {
		return err
	}
	target, err := findMutationTarget(root, store, configuration.annotationID)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	updated := &target.sidecar.Annotations[target.index]
	if now.Before(updated.UpdatedAt) {
		now = updated.UpdatedAt
	}
	identifier, err := annotation.NewThreadID(now)
	if err != nil {
		return fmt.Errorf("generate reply identifier: %w", err)
	}
	reply := annotation.ThreadEntry{ID: identifier, Kind: annotation.ThreadReply, Message: configuration.message, Author: configuration.author, CreatedAt: now}
	if err := reply.Validate(); err != nil {
		return fmt.Errorf("validate reply: %w", err)
	}
	updated.Thread = append(updated.Thread, reply)
	updated.UpdatedAt = now
	if err := target.sidecar.Validate(); err != nil {
		return fmt.Errorf("validate updated annotations: %w", err)
	}
	revision, err := store.Save(target.sidecar, target.revision)
	if err != nil {
		if errors.Is(err, annotationstore.ErrConflict) {
			return errors.New("annotations changed concurrently; retry the reply")
		}
		return fmt.Errorf("save annotation reply: %w", err)
	}
	return writeMutationOutput(output, mutationOutput{Document: target.document, Annotation: *updated, Revision: string(revision)})
}

// runResolve applies an actor-validated lifecycle transition and persists its
// activity plus status-change entries in one optimistic sidecar save.
func runResolve(configuration resolveConfig, output io.Writer) error {
	root, store, err := openMutationRoots(configuration.rootPath, configuration.annotationsDir)
	if err != nil {
		return err
	}
	target, err := findMutationTarget(root, store, configuration.annotationID)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	updated := &target.sidecar.Annotations[target.index]
	if now.Before(updated.UpdatedAt) {
		now = updated.UpdatedAt
	}
	entries, err := annotation.TransitionEntries(*updated, configuration.input, now)
	if err != nil {
		return fmt.Errorf("transition annotation: %w", err)
	}
	updated.Status = configuration.input.Status
	updated.Thread = append(updated.Thread, entries...)
	updated.UpdatedAt = now
	if err := target.sidecar.Validate(); err != nil {
		return fmt.Errorf("validate transitioned annotations: %w", err)
	}
	revision, err := store.Save(target.sidecar, target.revision)
	if err != nil {
		if errors.Is(err, annotationstore.ErrConflict) {
			return errors.New("annotations changed concurrently; retry the transition")
		}
		return fmt.Errorf("save annotation transition: %w", err)
	}
	return writeMutationOutput(output, mutationOutput{Document: target.document, Annotation: *updated, Revision: string(revision)})
}

// openMutationRoots requires existing annotation storage so a misspelled path
// cannot silently create a second sidecar root for a mutation.
func openMutationRoots(rootPath, annotationsDir string) (*content.Root, *annotationstore.Store, error) {
	root, err := content.Open(rootPath)
	if err != nil {
		return nil, nil, fmt.Errorf("open Markdown directory: %w", err)
	}
	directory := annotationsDir
	if directory == "" {
		directory = filepath.Join(root.Path(), ".md-viewer", "annotations")
	}
	info, err := os.Stat(directory)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, errors.New("annotation directory does not exist")
		}
		return nil, nil, fmt.Errorf("inspect annotation directory: %w", err)
	}
	if !info.IsDir() {
		return nil, nil, errors.New("annotation storage path is not a directory")
	}
	store, err := annotationstore.Open(directory)
	if err != nil {
		return nil, nil, fmt.Errorf("open annotation directory: %w", err)
	}
	return root, store, nil
}

// findMutationTarget locates a globally unique stable annotation ID among the
// current content index and retains the sidecar revision for optimistic save.
func findMutationTarget(root *content.Root, store *annotationstore.Store, identifier string) (mutationTarget, error) {
	index, err := root.Index()
	if err != nil {
		return mutationTarget{}, fmt.Errorf("index Markdown directory: %w", err)
	}
	var found *mutationTarget
	for _, document := range index.Documents {
		sidecar, revision, err := store.Load(document.Path)
		if err != nil {
			return mutationTarget{}, fmt.Errorf("load annotations for %q: %w", document.Path, err)
		}
		for index := range sidecar.Annotations {
			if sidecar.Annotations[index].ID != identifier {
				continue
			}
			if found != nil {
				return mutationTarget{}, fmt.Errorf("annotation id %q is ambiguous", identifier)
			}
			found = &mutationTarget{document: document.Path, sidecar: sidecar, revision: revision, index: index}
		}
	}
	if found == nil {
		return mutationTarget{}, fmt.Errorf("annotation %q not found", identifier)
	}
	return *found, nil
}

func writeMutationOutput(output io.Writer, result mutationOutput) error {
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		return fmt.Errorf("write annotation mutation: %w", err)
	}
	return nil
}
