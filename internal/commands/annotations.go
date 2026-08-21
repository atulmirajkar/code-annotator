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
	case "export":
		configuration, err := parseExportConfig(args[1:], stderr)
		if err != nil {
			return err
		}
		return runExport(configuration, stdout)
	default:
		return fmt.Errorf("unknown annotations subcommand %q", args[0])
	}
}

// parseExportConfig reuses list collection flags while requiring Markdown
// output so future formats can be added without changing command semantics.
func parseExportConfig(args []string, stderr io.Writer) (listConfig, error) {
	flags := flag.NewFlagSet("md-viewer annotations export", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", "", "Markdown content root")
	annotationsDir := flags.String("annotations-dir", "", "annotation storage directory")
	statuses := flags.String("status", "", "comma-separated annotation statuses")
	format := flags.String("format", "markdown", "output format (markdown)")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: md-viewer annotations export --root <directory> [options]")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return listConfig{}, err
	}
	if flags.NArg() != 0 {
		flags.Usage()
		return listConfig{}, errors.New("annotations export does not accept positional arguments")
	}
	if strings.TrimSpace(*root) == "" {
		flags.Usage()
		return listConfig{}, errors.New("--root is required")
	}
	if *format != "markdown" {
		return listConfig{}, fmt.Errorf("unsupported export format %q", *format)
	}
	statusFilter, err := parseStatuses(*statuses)
	if err != nil {
		return listConfig{}, err
	}
	return listConfig{rootPath: *root, annotationsDir: *annotationsDir, statuses: statusFilter, format: *format}, nil
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
	result, err := collectList(configuration)
	if err != nil {
		return err
	}
	return writeListOutput(output, result)
}

// collectList performs the shared read-only traversal used by JSON listing and
// human-readable export.
func collectList(configuration listConfig) (listOutput, error) {
	root, err := content.Open(configuration.rootPath)
	if err != nil {
		return listOutput{}, fmt.Errorf("open Markdown directory: %w", err)
	}
	index, err := root.Index()
	if err != nil {
		return listOutput{}, fmt.Errorf("index Markdown directory: %w", err)
	}

	directory := configuration.annotationsDir
	if directory == "" {
		directory = filepath.Join(root.Path(), ".md-viewer", "annotations")
	}
	info, err := os.Stat(directory)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return listOutput{SchemaVersion: annotation.SchemaVersion, Documents: []listDocument{}}, nil
		}
		return listOutput{}, fmt.Errorf("inspect annotation directory: %w", err)
	}
	if !info.IsDir() {
		return listOutput{}, errors.New("annotation storage path is not a directory")
	}
	store, err := annotationstore.Open(directory)
	if err != nil {
		return listOutput{}, fmt.Errorf("open annotation directory: %w", err)
	}

	result := listOutput{SchemaVersion: annotation.SchemaVersion, Documents: []listDocument{}}
	for _, document := range index.Documents {
		sidecar, _, err := store.Load(document.Path)
		if err != nil {
			return listOutput{}, fmt.Errorf("load annotations for %q: %w", document.Path, err)
		}
		if len(sidecar.Annotations) == 0 {
			continue
		}
		source, err := root.ReadFile(document.Path, maxListDocumentBytes)
		if err != nil {
			return listOutput{}, fmt.Errorf("read Markdown document %q: %w", document.Path, err)
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
					return listOutput{}, fmt.Errorf("resolve annotation %q: %w", item.ID, err)
				}
				view.Anchor = &anchor
			}
			listed.Annotations = append(listed.Annotations, view)
		}
		if len(listed.Annotations) > 0 {
			result.Documents = append(result.Documents, listed)
		}
	}
	return result, nil
}

func writeListOutput(output io.Writer, result listOutput) error {
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		return fmt.Errorf("write annotation list: %w", err)
	}
	return nil
}

func runExport(configuration listConfig, output io.Writer) error {
	result, err := collectList(configuration)
	if err != nil {
		return err
	}
	var markdown strings.Builder
	markdown.WriteString("# Annotation review\n\n")
	if len(result.Documents) == 0 {
		markdown.WriteString("No matching annotations.\n")
	}
	for _, document := range result.Documents {
		fmt.Fprintf(&markdown, "## %s\n\n", document.Document)
		for _, item := range document.Annotations {
			fmt.Fprintf(&markdown, "### %s\n\n", item.ID)
			fmt.Fprintf(&markdown, "- Intent: `%s`\n", item.Intent)
			fmt.Fprintf(&markdown, "- Status: `%s`\n", item.Status)
			fmt.Fprintf(&markdown, "- Author: %s\n", item.Author)
			fmt.Fprintf(&markdown, "- Created: %s\n", item.CreatedAt.Format("2006-01-02T15:04:05.999999999Z07:00"))
			writeAnchorSummary(&markdown, item)
			if item.Source != nil {
				markdown.WriteString("\n#### Selected Markdown\n\n")
				writeMarkdownCodeBlock(&markdown, item.Source.Selector.Exact)
			}
			markdown.WriteString("\n#### Comment\n\n")
			writeMarkdownCodeBlock(&markdown, item.Comment)
			if len(item.Thread) > 0 {
				markdown.WriteString("\n#### Thread\n\n")
				for _, entry := range item.Thread {
					fmt.Fprintf(&markdown, "- `%s` by %s at %s", entry.Kind, entry.Author, entry.CreatedAt.Format("2006-01-02T15:04:05.999999999Z07:00"))
					if entry.Message != "" {
						fmt.Fprintf(&markdown, ": %s", singleLine(entry.Message))
					} else if entry.Summary != "" {
						fmt.Fprintf(&markdown, ": %s", singleLine(entry.Summary))
					} else if entry.FromStatus != "" || entry.ToStatus != "" {
						fmt.Fprintf(&markdown, ": `%s` → `%s`", entry.FromStatus, entry.ToStatus)
					}
					if entry.Commit != "" {
						fmt.Fprintf(&markdown, " (commit `%s`)", entry.Commit)
					}
					markdown.WriteString("\n")
				}
			}
			markdown.WriteString("\n")
		}
	}
	if _, err := io.WriteString(output, markdown.String()); err != nil {
		return fmt.Errorf("write annotation export: %w", err)
	}
	return nil
}

// writeAnchorSummary distinguishes original selector lines from the current
// derived location so moved and stale annotations remain unambiguous to agents.
func writeAnchorSummary(output *strings.Builder, item listAnnotation) {
	if item.Source == nil {
		output.WriteString("- Anchor: document\n")
		return
	}
	selector := item.Source.Selector
	fmt.Fprintf(output, "- Original lines: %d–%d\n", selector.StartLine, selector.EndLine)
	if item.Anchor == nil {
		output.WriteString("- Anchor: unavailable\n")
		return
	}
	fmt.Fprintf(output, "- Anchor: `%s`", item.Anchor.State)
	if item.Anchor.State == annotation.AnchorStale {
		fmt.Fprintf(output, " (`%s`, candidates: %d)", item.Anchor.Reason, item.Anchor.Candidates)
	} else {
		fmt.Fprintf(output, " at lines %d–%d", item.Anchor.StartLine, item.Anchor.EndLine)
	}
	output.WriteString("\n")
}

// writeMarkdownCodeBlock chooses a fence longer than every backtick run in the
// value so arbitrary comments and selections cannot terminate the block.
func writeMarkdownCodeBlock(output *strings.Builder, value string) {
	fence := strings.Repeat("`", max(3, longestBacktickRun(value)+1))
	fmt.Fprintf(output, "%stext\n%s\n%s\n", fence, value, fence)
}

// longestBacktickRun returns the minimum fence hazard within arbitrary text.
func longestBacktickRun(value string) int {
	longest := 0
	current := 0
	for _, character := range value {
		if character == '`' {
			current++
			longest = max(longest, current)
		} else {
			current = 0
		}
	}
	return longest
}

// singleLine makes multiline thread activity safe and readable in a list item.
func singleLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
