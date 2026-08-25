package server

import (
	"fmt"
	"io"
	"strings"
	"testing"

	"atulm/code-annotator/internal/content"
)

func TestNewDocumentPanelViewFiltersTypedCatalog(t *testing.T) {
	t.Parallel()
	selected := "README.md"
	state := documentCatalogState{
		SchemaVersion: 1, SelectedPath: &selected, Mode: "diff",
		ChangedAvailable: true, ReviewAvailable: true,
		Documents: []documentCatalogItem{
			{Path: "README.md", Name: "README.md", Kind: content.KindMarkdown, URL: "/view/README.md?mode=diff", Selected: true},
			{Path: "src/main.go", Name: "main.go", Directory: "src", Kind: content.KindCode, URL: "/view/src/main.go?mode=diff", Changed: true, OpenCommentCount: 2},
			{Path: "src/view.go", Name: "view.go", Directory: "src", Kind: content.KindCode, URL: "/view/src/view.go?mode=diff", Changed: true},
		},
	}

	view := newDocumentPanelView(state, "MAIN", "open-comments")
	if view.Scope != "open-comments" || view.Status != "1 matching document with open comments." {
		t.Fatalf("filtered view = scope %q, status %q", view.Scope, view.Status)
	}
	if len(view.Tree) != 1 || view.Tree[0].Key != "src" || len(view.Tree[0].Children) != 1 || view.Tree[0].Children[0].Document.Path != "src/main.go" {
		t.Fatalf("filtered tree = %#v", view.Tree)
	}
	if view.OpenDocumentCount != 1 || view.Mode != "diff" || view.Selected != selected {
		t.Fatalf("panel summary = %#v", view)
	}
}

func TestDocumentTreeUsesStableSemanticIDs(t *testing.T) {
	t.Parallel()
	documents := []documentCatalogItem{
		{Path: "src/nested/a.go", Name: "a.go"},
		{Path: "src/b.go", Name: "b.go"},
	}
	first := buildDocumentTreeView(documents)
	second := buildDocumentTreeView(documents)
	if len(first) != 1 || first[0].ElementID == "" || first[0].ChildrenID == "" {
		t.Fatalf("tree does not contain semantic directory IDs: %#v", first)
	}
	if first[0].ElementID != second[0].ElementID || !strings.HasPrefix(first[0].ElementID, "document-directory-") {
		t.Fatalf("directory ID is not stable: %q, %q", first[0].ElementID, second[0].ElementID)
	}
}

func BenchmarkDocumentPanel5000(b *testing.B) {
	documents := make([]documentCatalogItem, 0, 5000)
	for index := 0; index < 5000; index++ {
		path := fmt.Sprintf("group-%02d/package-%03d/file-%04d.go", index%25, index%250, index)
		documents = append(documents, documentCatalogItem{
			Path: path, Name: fmt.Sprintf("file-%04d.go", index), Directory: path[:strings.LastIndex(path, "/")],
			Kind: content.KindCode, URL: "/view/" + path, Changed: index%3 == 0, OpenCommentCount: index % 4,
		})
	}
	state := documentCatalogState{SchemaVersion: 1, Mode: "file", ChangedAvailable: true, ReviewAvailable: true, Documents: documents}
	templates, err := parseViewerTemplates()
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		view := newDocumentPanelView(state, "file-4", "changed")
		if err := templates.ExecuteTemplate(io.Discard, "document-panel", view); err != nil {
			b.Fatal(err)
		}
	}
}
