// Package commands implements offline annotation workflows that do not start
// the Markdown web server.
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

	"atulm/md-viewer/internal/annotation"
	annotationstore "atulm/md-viewer/internal/annotation/store"
	"atulm/md-viewer/internal/content"
)

const maxListDocumentBytes int64 = 4 << 20

type listConfig struct {
	rootPath       string
	annotationsDir string
	statuses       map[annotation.Status]struct{}
	format         string
}

type listOutput struct {
	SchemaVersion int            `json:"schemaVersion"`
	Documents     []listDocument `json:"documents"`
}

type listDocument struct {
	Document    string           `json:"document"`
	Annotations []listAnnotation `json:"annotations"`
}

type listAnnotation struct {
	annotation.Annotation
	Anchor *annotation.AnchorResult `json:"anchor,omitempty"`
}

// RunAnnotations executes an offline annotations subcommand.
func RunAnnotations(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("annotations subcommand is required")
	}
	switch args[0] {
	case "list":
		configuration, err := parseListConfig(args[1:], stderr)
		if err != nil {
			return err
		}
		return runList(configuration, stdout)
	default:
		return fmt.Errorf("unknown annotations subcommand %q", args[0])
	}
}

// parseListConfig validates list flags without opening either filesystem root.
func parseListConfig(args []string, stderr io.Writer) (listConfig, error) {
	flags := flag.NewFlagSet("md-viewer annotations list", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", "", "Markdown content root")
	annotationsDir := flags.String("annotations-dir", "", "annotation storage directory")
	statuses := flags.String("status", "", "comma-separated annotation statuses")
	format := flags.String("format", "json", "output format (json)")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: md-viewer annotations list --root <directory> [options]")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return listConfig{}, err
	}
	if flags.NArg() != 0 {
		flags.Usage()
		return listConfig{}, errors.New("annotations list does not accept positional arguments")
	}
	if strings.TrimSpace(*root) == "" {
		flags.Usage()
		return listConfig{}, errors.New("--root is required")
	}
	if *format != "json" {
		return listConfig{}, fmt.Errorf("unsupported list format %q", *format)
	}
	statusFilter, err := parseStatuses(*statuses)
	if err != nil {
		return listConfig{}, err
	}
	return listConfig{rootPath: *root, annotationsDir: *annotationsDir, statuses: statusFilter, format: *format}, nil
}

func parseStatuses(value string) (map[annotation.Status]struct{}, error) {
	result := make(map[annotation.Status]struct{})
	if strings.TrimSpace(value) == "" {
		return result, nil
	}
	for _, raw := range strings.Split(value, ",") {
		status := annotation.Status(strings.TrimSpace(raw))
		if !status.Valid() {
			return nil, fmt.Errorf("invalid annotation status %q", raw)
		}
		result[status] = struct{}{}
	}
	return result, nil
}

// runList loads annotations in the content index's stable document order and
// emits only documents containing at least one matching annotation.
func runList(configuration listConfig, output io.Writer) error {
	root, err := content.Open(configuration.rootPath)
	if err != nil {
		return fmt.Errorf("open Markdown directory: %w", err)
	}
	index, err := root.Index()
	if err != nil {
		return fmt.Errorf("index Markdown directory: %w", err)
	}

	directory := configuration.annotationsDir
	if directory == "" {
		directory = filepath.Join(root.Path(), ".md-viewer", "annotations")
	}
	info, err := os.Stat(directory)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return writeListOutput(output, listOutput{SchemaVersion: annotation.SchemaVersion, Documents: []listDocument{}})
		}
		return fmt.Errorf("inspect annotation directory: %w", err)
	}
	if !info.IsDir() {
		return errors.New("annotation storage path is not a directory")
	}
	store, err := annotationstore.Open(directory)
	if err != nil {
		return fmt.Errorf("open annotation directory: %w", err)
	}

	result := listOutput{SchemaVersion: annotation.SchemaVersion, Documents: []listDocument{}}
	for _, document := range index.Documents {
		sidecar, _, err := store.Load(document.Path)
		if err != nil {
			return fmt.Errorf("load annotations for %q: %w", document.Path, err)
		}
		if len(sidecar.Annotations) == 0 {
			continue
		}
		source, err := root.ReadFile(document.Path, maxListDocumentBytes)
		if err != nil {
			return fmt.Errorf("read Markdown document %q: %w", document.Path, err)
		}
		listed := listDocument{Document: document.Path, Annotations: []listAnnotation{}}
		for _, item := range sidecar.Annotations {
			if len(configuration.statuses) > 0 {
				if _, wanted := configuration.statuses[item.Status]; !wanted {
					continue
				}
			}
			view := listAnnotation{Annotation: item}
			if item.Source != nil {
				anchor, err := annotation.ResolveAnchor(source, *item.Source)
				if err != nil {
					return fmt.Errorf("resolve annotation %q: %w", item.ID, err)
				}
				view.Anchor = &anchor
			}
			listed.Annotations = append(listed.Annotations, view)
		}
		if len(listed.Annotations) > 0 {
			result.Documents = append(result.Documents, listed)
		}
	}
	return writeListOutput(output, result)
}

func writeListOutput(output io.Writer, result listOutput) error {
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		return fmt.Errorf("write annotation list: %w", err)
	}
	return nil
}
