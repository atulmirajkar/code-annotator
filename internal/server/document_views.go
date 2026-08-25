package server

import (
	"cmp"
	"crypto/sha256"
	"fmt"
	"net/http"
	"slices"
	"strings"
)

type documentPanelView struct {
	Query             string
	Scope             string
	Selected          string
	Mode              string
	ChangedAvailable  bool
	ChangedError      bool
	ReviewAvailable   bool
	OpenDocumentCount int
	Status            string
	Empty             bool
	Tree              []documentTreeNodeView
}

type documentTreeNodeView struct {
	Key        string
	Name       string
	Directory  bool
	ElementID  string
	ChildrenID string
	Children   []documentTreeNodeView
	Document   *documentCatalogItem
}

type mutableDocumentTreeNode struct {
	key       string
	name      string
	directory bool
	children  []*mutableDocumentTreeNode
	document  *documentCatalogItem
}

func newDocumentPanelView(state documentCatalogState, query, scope string) documentPanelView {
	if scope != "changed" && scope != "open-comments" {
		scope = "all"
	}
	query = strings.TrimSpace(query)
	filtered := make([]documentCatalogItem, 0, len(state.Documents))
	openDocuments := 0
	for _, document := range state.Documents {
		if document.OpenCommentCount > 0 {
			openDocuments++
		}
		pathMatches := query == "" || strings.Contains(strings.ToLower(document.Path), strings.ToLower(query))
		scopeMatches := scope == "all" || scope == "changed" && document.Changed || scope == "open-comments" && document.OpenCommentCount > 0
		if pathMatches && scopeMatches {
			filtered = append(filtered, document)
		}
	}
	selected := ""
	if state.SelectedPath != nil {
		selected = *state.SelectedPath
	}
	return documentPanelView{
		Query: query, Scope: scope, Selected: selected, Mode: state.Mode,
		ChangedAvailable: state.ChangedAvailable, ChangedError: state.ChangedError,
		ReviewAvailable: state.ReviewAvailable, OpenDocumentCount: openDocuments,
		Status: documentFilterStatus(query, scope, len(filtered)),
		Empty:  len(state.Documents) == 0, Tree: buildDocumentTreeView(filtered),
	}
}

func (s *Server) handleDocumentPanel(response http.ResponseWriter, request *http.Request) {
	state, err := s.readDocumentCatalogState(request.Context(), request.URL.Query().Get("document"), request.URL.Query().Get("mode"))
	if err != nil {
		s.writeDocumentStateError(response, err)
		return
	}
	view := newDocumentPanelView(state, request.URL.Query().Get("query"), request.URL.Query().Get("scope"))
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	if err := s.page.ExecuteTemplate(response, "document-panel", view); err != nil {
		http.Error(response, "could not render document panel", http.StatusInternalServerError)
	}
}

func documentFilterStatus(query, scope string, count int) string {
	if query == "" && scope == "all" {
		return ""
	}
	descriptor := "matching document"
	switch scope {
	case "changed":
		if query == "" {
			descriptor = "changed document"
		} else {
			descriptor = "matching changed document"
		}
	case "open-comments":
		if query == "" {
			descriptor = "document with open comments"
		} else {
			descriptor = "matching document with open comments"
		}
	}
	plural := strings.Replace(descriptor, "document", "documents", 1)
	if count == 0 {
		return "No " + plural + "."
	}
	if count == 1 {
		return "1 " + descriptor + "."
	}
	return fmt.Sprintf("%d %s.", count, plural)
}

func buildDocumentTreeView(documents []documentCatalogItem) []documentTreeNodeView {
	roots := []*mutableDocumentTreeNode{}
	for index := range documents {
		document := &documents[index]
		segments := strings.Split(document.Path, "/")
		siblings := &roots
		parentKey := ""
		for segmentIndex, segment := range segments {
			directory := segmentIndex != len(segments)-1
			key := document.Path
			if directory {
				key = segment
				if parentKey != "" {
					key = parentKey + "/" + segment
				}
			}
			var node *mutableDocumentTreeNode
			for _, candidate := range *siblings {
				if candidate.key == key {
					node = candidate
					break
				}
			}
			if node == nil {
				node = &mutableDocumentTreeNode{key: key, name: segment, directory: directory}
				*siblings = append(*siblings, node)
			}
			if directory {
				siblings = &node.children
			} else {
				node.document = document
			}
			parentKey = key
		}
	}
	return freezeDocumentTree(roots)
}

func freezeDocumentTree(nodes []*mutableDocumentTreeNode) []documentTreeNodeView {
	// Sort every sibling group independently so directories precede files at
	// each depth while names remain deterministic for navigation and tests.
	slices.SortFunc(nodes, compareDocumentTreeNodes)
	result := make([]documentTreeNodeView, 0, len(nodes))
	for _, node := range nodes {
		view := documentTreeNodeView{Key: node.key, Name: node.name, Directory: node.directory, Document: node.document}
		if node.directory {
			identity := documentElementIdentity(node.key)
			view.ElementID = "document-directory-" + identity
			view.ChildrenID = "document-directory-children-" + identity
			view.Children = freezeDocumentTree(node.children)
		}
		result = append(result, view)
	}
	return result
}

func compareDocumentTreeNodes(left, right *mutableDocumentTreeNode) int {
	if left.directory != right.directory {
		if left.directory {
			return -1
		}
		return 1
	}
	if order := cmp.Compare(strings.ToLower(left.name), strings.ToLower(right.name)); order != 0 {
		return order
	}
	if order := cmp.Compare(left.name, right.name); order != 0 {
		return order
	}
	return cmp.Compare(left.key, right.key)
}

func documentElementIdentity(value string) string {
	digest := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", digest[:8])
}
