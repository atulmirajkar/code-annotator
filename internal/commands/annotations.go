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

	"atulm/code-annotator/internal/annotation"
	annotationstore "atulm/code-annotator/internal/annotation/store"
	"atulm/code-annotator/internal/content"
)

const maxListDocumentBytes int64 = 4 << 20

type listConfig struct {
	rootPath       string
	annotationsDir string
	statuses       map[annotation.Status]struct{}
	format         string
	indexOptions   content.IndexOptions
}

type listOutput struct {
	SchemaVersion int            `json:"schemaVersion"`
	Documents     []listDocument `json:"documents"`
}

type listDocument struct {
	Document    string           `json:"document"`
	Kind        content.Kind     `json:"kind"`
	Language    string           `json:"language"`
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
	case "reply":
		configuration, err := parseReplyConfig(args[1:], stderr)
		if err != nil {
			return err
		}
		return runReply(configuration, stdout)
	case "resolve":
		configuration, err := parseResolveConfig(args[1:], stderr)
		if err != nil {
			return err
		}
		return runResolve(configuration, stdout)
	default:
		return fmt.Errorf("unknown annotations subcommand %q", args[0])
	}
}

// parseExportConfig reuses list collection flags while requiring Markdown
// output so future formats can be added without changing command semantics.
func parseExportConfig(args []string, stderr io.Writer) (listConfig, error) {
	flags := flag.NewFlagSet("code-annotator annotations export", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", "", "reviewable content root")
	annotationsDir := flags.String("annotations-dir", "", "annotation storage directory")
	statuses := flags.String("status", "", "comma-separated annotation statuses")
	format := flags.String("format", "markdown", "output format (markdown)")
	includeCode := flags.Bool("include-code", false, "include supported source files")
	codeExtensions := flags.String("code-extensions", "", "comma-separated source extensions (implies --include-code)")
	excludeDirs := flags.String("exclude-dirs", "", "comma-separated directory base names to exclude")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: code-annotator annotations export --root <directory> [options]")
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
	indexOptions, err := annotationCatalogOptions(*includeCode, *codeExtensions, *excludeDirs)
	if err != nil {
		return listConfig{}, err
	}
	return listConfig{rootPath: *root, annotationsDir: *annotationsDir, statuses: statusFilter, format: *format, indexOptions: indexOptions}, nil
}

// parseListConfig validates list flags without opening either filesystem root.
func parseListConfig(args []string, stderr io.Writer) (listConfig, error) {
	flags := flag.NewFlagSet("code-annotator annotations list", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", "", "reviewable content root")
	annotationsDir := flags.String("annotations-dir", "", "annotation storage directory")
	statuses := flags.String("status", "", "comma-separated annotation statuses")
	format := flags.String("format", "json", "output format (json)")
	includeCode := flags.Bool("include-code", false, "include supported source files")
	codeExtensions := flags.String("code-extensions", "", "comma-separated source extensions (implies --include-code)")
	excludeDirs := flags.String("exclude-dirs", "", "comma-separated directory base names to exclude")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: code-annotator annotations list --root <directory> [options]")
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
	indexOptions, err := annotationCatalogOptions(*includeCode, *codeExtensions, *excludeDirs)
	if err != nil {
		return listConfig{}, err
	}
	return listConfig{rootPath: *root, annotationsDir: *annotationsDir, statuses: statusFilter, format: *format, indexOptions: indexOptions}, nil
}

// annotationCatalogOptions mirrors viewer catalog semantics so offline output
// addresses the same opt-in source extensions and default excluded directories.
func annotationCatalogOptions(includeCode bool, codeExtensions, excludeDirs string) (content.IndexOptions, error) {
	extensions := splitAnnotationCSV(codeExtensions)
	includeCode = includeCode || len(extensions) > 0
	if includeCode && len(extensions) == 0 {
		extensions = content.DefaultCodeExtensions()
	}
	excluded := splitAnnotationCSV(excludeDirs)
	if includeCode {
		excluded = append(content.DefaultExcludedDirectories(), excluded...)
	}
	options, err := content.NewIndexOptions(extensions, excluded)
	if err != nil {
		return content.IndexOptions{}, fmt.Errorf("configure content catalog: %w", err)
	}
	return options, nil
}

// splitAnnotationCSV preserves invalid empty entries for catalog validation
// while treating an omitted flag as no values.
func splitAnnotationCSV(value string) []string {
	if value == "" {
		return nil
	}
	return strings.Split(value, ",")
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
		return listOutput{}, fmt.Errorf("open content directory: %w", err)
	}
	index, err := root.IndexWithOptions(configuration.indexOptions)
	if err != nil {
		return listOutput{}, fmt.Errorf("index content directory: %w", err)
	}

	directory := configuration.annotationsDir
	if directory == "" {
		directory = filepath.Join(root.Path(), ".code-annotator", "annotations")
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
			return listOutput{}, fmt.Errorf("read document %q: %w", document.Path, err)
		}
		listed := listDocument{Document: document.Path, Kind: document.Kind, Language: document.Language, Annotations: []listAnnotation{}}
		for _, item := range sidecar.Annotations {
			if len(configuration.statuses) > 0 {
				if _, wanted := configuration.statuses[item.Status]; !wanted {
					continue
				}
			}
			view := listAnnotation{Annotation: item}
			if item.NeedsReattachment {
				view.Anchor = &annotation.AnchorResult{State: annotation.AnchorStale, Reason: annotation.StaleDocumentChanged}
			} else if item.Source != nil {
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
		fmt.Fprintf(&markdown, "- Kind: `%s`\n", document.Kind)
		fmt.Fprintf(&markdown, "- Language: `%s`\n\n", document.Language)
		for _, item := range document.Annotations {
			fmt.Fprintf(&markdown, "### %s\n\n", item.ID)
			fmt.Fprintf(&markdown, "- Intent: `%s`\n", item.Intent)
			fmt.Fprintf(&markdown, "- Status: `%s`\n", item.Status)
			fmt.Fprintf(&markdown, "- Role: %s\n", item.Role)
			fmt.Fprintf(&markdown, "- Created: %s\n", item.CreatedAt.Format("2006-01-02T15:04:05.999999999Z07:00"))
			writeAnchorSummary(&markdown, item)
			if item.Source != nil {
				markdown.WriteString("\n#### Selected source\n\n")
				writeMarkdownCodeBlock(&markdown, item.Source.Selector.Exact)
			}
			markdown.WriteString("\n#### Comment\n\n")
			writeMarkdownCodeBlock(&markdown, item.Comment)
			if len(item.Thread) > 0 {
				markdown.WriteString("\n#### Thread\n\n")
				for _, entry := range item.Thread {
					fmt.Fprintf(&markdown, "- `%s` by %s at %s", entry.Kind, entry.Role, entry.CreatedAt.Format("2006-01-02T15:04:05.999999999Z07:00"))
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
	if item.NeedsReattachment {
		output.WriteString("- Original lines: unavailable\n")
		output.WriteString("- Anchor: `stale` (`document_changed`, candidates: 0)\n")
		return
	}
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
