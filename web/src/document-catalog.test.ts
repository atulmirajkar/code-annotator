import { describe, expect, it } from "vitest";

import {
  buildDocumentTree,
  countDocumentsWithOpenComments,
  filterDocuments,
  hasChangedDocuments,
} from "./document-catalog.js";
import type { DocumentCatalogItem } from "./document-state.js";

const documents: DocumentCatalogItem[] = [
  { path: "README.md", name: "README.md", directory: "", kind: "markdown", url: "/view/README.md", selected: true, changed: false, openCommentCount: 0 },
  { path: "src/main.go", name: "main.go", directory: "src", kind: "code", url: "/view/src/main.go", selected: false, changed: true, openCommentCount: 2 },
  { path: "src/nested/view.ts", name: "view.ts", directory: "src/nested", kind: "code", url: "/view/src/nested/view.ts", selected: false, changed: true, openCommentCount: 0 },
];

describe("document catalog rules", () => {
  it("builds a pure nested tree without mutating the catalog", () => {
    const before = structuredClone(documents);
    const tree = buildDocumentTree(documents);

    expect(tree.map((node) => node.key)).toEqual(["README.md", "src"]);
    expect(tree[1]?.children.map((node) => node.key)).toEqual(["src/main.go", "src/nested"]);
    expect(tree[1]?.children[1]?.children[0]?.document?.path).toBe("src/nested/view.ts");
    expect(documents).toEqual(before);
  });

  it("filters paths case-insensitively and preserves catalog order", () => {
    const result = filterDocuments(documents, "SRC/", "all");

    expect(result.documents.map((item) => item.path)).toEqual(["src/main.go", "src/nested/view.ts"]);
    expect(result.status).toBe("2 matching documents.");
  });

  it("composes query and mutually exclusive scopes", () => {
    expect(filterDocuments(documents, "main", "changed").documents.map((item) => item.path)).toEqual(["src/main.go"]);
    expect(filterDocuments(documents, "src", "open-comments").documents.map((item) => item.path)).toEqual(["src/main.go"]);
    expect(filterDocuments(documents, "missing", "changed").status).toBe("No matching changed documents.");
    expect(filterDocuments(documents, "", "all").status).toBe("");
  });

  it("derives catalog summaries from typed state", () => {
    expect(countDocumentsWithOpenComments(documents)).toBe(1);
    expect(hasChangedDocuments(documents)).toBe(true);
    expect(hasChangedDocuments([documents[0]!])).toBe(false);
  });
});
