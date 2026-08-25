package web

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// TestPureStateModulesDoNotDependOnDOM protects the architectural boundary:
// these modules calculate application state and may not inspect browser views.
func TestPureStateModulesDoNotDependOnDOM(t *testing.T) {
	t.Parallel()

	forbidden := regexp.MustCompile(`(?m)\b(?:window|HTMLElement|HTML[A-Za-z]+Element|Element|Node|Document|querySelector|querySelectorAll|classList|dataset|textContent)\b|\bdocument\s*\.\s*(?:body|documentElement|querySelector|querySelectorAll|createElement|createTextNode|createRange|addEventListener)`)
	for _, relative := range []string{"web/src/document-catalog.ts", "web/src/viewer-preferences.ts"} {
		body, err := os.ReadFile(filepath.Join(repositoryRoot(t), relative))
		if err != nil {
			t.Fatalf("read %s: %v", relative, err)
		}
		if match := forbidden.Find(body); match != nil {
			t.Errorf("%s uses DOM token %q; pure state modules accept typed values only", relative, match)
		}
	}
}
