import type { DocumentCatalogItem } from "./document-state.js";

export type DocumentScope = "all" | "changed" | "open-comments";

export interface DocumentTreeNode {
  key: string;
  name: string;
  directory: boolean;
  children: ReadonlyArray<DocumentTreeNode>;
  document: DocumentCatalogItem | null;
}

export interface DocumentFilterResult {
  documents: ReadonlyArray<DocumentCatalogItem>;
  paths: ReadonlySet<string>;
  status: string;
}

interface MutableTreeNode {
  key: string;
  name: string;
  directory: boolean;
  children: MutableTreeNode[];
  document: DocumentCatalogItem | null;
}

export function buildDocumentTree(documents: ReadonlyArray<DocumentCatalogItem>): ReadonlyArray<DocumentTreeNode> {
  const roots: MutableTreeNode[] = [];
  documents.forEach((document) => {
    const segments = document.path.split("/");
    let siblings = roots;
    let parentKey = "";
    segments.forEach((segment, index) => {
      const directory = index !== segments.length - 1;
      const key = directory ? (parentKey ? `${parentKey}/${segment}` : segment) : document.path;
      let node = siblings.find((candidate) => candidate.key === key);
      if (!node) {
        node = { key, name: segment, directory, children: [], document: null };
        siblings.push(node);
      }
      if (directory) siblings = node.children;
      else node.document = document;
      parentKey = key;
    });
  });
  return roots;
}

export function filterDocuments(
  documents: ReadonlyArray<DocumentCatalogItem>,
  queryValue: string,
  scope: DocumentScope,
): DocumentFilterResult {
  const query = queryValue.trim().toLocaleLowerCase();
  const matches = documents.filter((document) => {
    const pathMatches = !query || document.path.toLocaleLowerCase().includes(query);
    const scopeMatches = scope === "all"
      || (scope === "changed" && document.changed)
      || (scope === "open-comments" && document.openCommentCount > 0);
    return pathMatches && scopeMatches;
  });
  return {
    documents: matches,
    paths: new Set(matches.map((document) => document.path)),
    status: documentFilterStatus(query, scope, matches.length),
  };
}

export function documentFilterStatus(query: string, scope: DocumentScope, count: number): string {
  if (!query && scope === "all") return "";
  const descriptor = scope === "changed"
    ? (query ? "matching changed document" : "changed document")
    : scope === "open-comments"
      ? (query ? "matching document with open comments" : "document with open comments")
      : "matching document";
  const plural = descriptor.replace("document", "documents");
  return count === 0 ? `No ${plural}.` : `${count} ${count === 1 ? descriptor : plural}.`;
}

export function countDocumentsWithOpenComments(documents: ReadonlyArray<DocumentCatalogItem>): number {
  return documents.filter((document) => document.openCommentCount > 0).length;
}

export function hasChangedDocuments(documents: ReadonlyArray<DocumentCatalogItem>): boolean {
  return documents.some((document) => document.changed);
}
