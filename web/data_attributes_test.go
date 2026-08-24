package web

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

var dataAttributePattern = regexp.MustCompile(`data-([a-z][a-z0-9-]*)`)
var datasetPropertyPattern = regexp.MustCompile(`\.dataset\.([A-Za-z][A-Za-z0-9]*)`)
var camelBoundaryPattern = regexp.MustCompile(`([a-z0-9])([A-Z])`)

// TestAuthoredDataAttributesAreExplicitBaseline turns the current custom-data
// debt into a shrinking allowlist. Migration commits remove entries; any new
// attribute name or dataset consumer fails until its architecture is reviewed.
func TestAuthoredDataAttributesAreExplicitBaseline(t *testing.T) {
	t.Parallel()

	allowed := map[string][]string{
		"internal/render/renderer.go":           {"line", "source-end", "source-start"},
		"web/src/document-tree.ts":              {"document-path", "filter-match"},
		"web/src/mermaid.ts":                    {"source-end", "source-start"},
		"web/src/review-fragments.ts":           {"activity", "activity-label", "anchor-end-byte", "anchor-start-byte", "anchor-state", "document-level", "needs-reattachment", "role", "source-start-byte"},
		"web/src/review-highlights.ts":          {"source-end", "source-start"},
		"web/src/review-htmx.ts":                {"revision"},
		"web/src/review-navigation.ts":          {"annotation-navigation-tabindex", "source-end", "source-start"},
		"web/src/review-selection.ts":           {"document-sha256", "end-byte", "exact", "source-end", "source-start", "start-byte"},
		"web/src/review.ts":                     {"document", "show-inactive"},
		"web/src/styles/_content.scss":          {"source-start"},
		"web/src/viewer.ts":                     {"changed", "document-path", "filter-match"},
		"web/templates/annotation-actions.html": {"activity", "activity-label", "role"},
		"web/templates/annotation-card.html":    {"anchor-end-byte", "anchor-start-byte", "anchor-state", "annotation-id", "document-level", "inactive", "kind", "needs-reattachment", "role", "source-start-byte"},
		"web/templates/annotation-panel.html":   {"document", "revision", "show-inactive"},
		"web/templates/page.html":               {"active-commit", "changed", "document", "document-path", "document-sha256", "kind"},
	}

	repository := repositoryRoot(t)
	found := make(map[string]map[string]struct{})
	for _, directory := range []string{"internal/render", "web/src", "web/templates"} {
		err := filepath.WalkDir(filepath.Join(repository, directory), func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !authoredBrowserFile(path) {
				return nil
			}
			relative, err := filepath.Rel(repository, path)
			if err != nil {
				return err
			}
			relative = filepath.ToSlash(relative)
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			attributes := dataAttributes(string(body))
			if len(attributes) > 0 {
				found[relative] = attributes
			}
			return nil
		})
		if err != nil {
			t.Fatalf("scan authored browser files: %v", err)
		}
	}

	for path, attributes := range found {
		want, ok := allowed[path]
		if !ok {
			t.Errorf("%s introduces custom data attributes %v; use typed browser state and semantic IDs", path, sortedKeys(attributes))
			continue
		}
		if got := sortedKeys(attributes); strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s data attributes = %v, reviewed baseline = %v", path, got, want)
		}
	}
	for path, attributes := range allowed {
		if _, ok := found[path]; !ok {
			t.Errorf("%s no longer uses its reviewed data-attribute baseline %v; remove the stale allowance", path, attributes)
		}
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() could not locate test source")
	}
	return filepath.Dir(filepath.Dir(file))
}

func authoredBrowserFile(path string) bool {
	if strings.HasSuffix(path, "_test.go") || strings.HasSuffix(path, ".test.ts") {
		return false
	}
	return strings.HasSuffix(path, ".go") || strings.HasSuffix(path, ".ts") || strings.HasSuffix(path, ".scss") || strings.HasSuffix(path, ".html")
}

func dataAttributes(body string) map[string]struct{} {
	result := make(map[string]struct{})
	for _, match := range dataAttributePattern.FindAllStringSubmatch(body, -1) {
		result[match[1]] = struct{}{}
	}
	for _, match := range datasetPropertyPattern.FindAllStringSubmatch(body, -1) {
		name := camelBoundaryPattern.ReplaceAllString(match[1], `${1}-${2}`)
		result[strings.ToLower(name)] = struct{}{}
	}
	return result
}

func sortedKeys(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
