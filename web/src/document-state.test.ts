import { describe, expect, it } from "vitest";

import { parseDocumentCatalogState } from "./document-state.js";

function validState(): Record<string, unknown> {
  return {
    schemaVersion: 1,
    selectedPath: "src/main.go",
    mode: "diff",
    changedAvailable: true,
    changedError: false,
    reviewAvailable: true,
    documents: [
      { path: "README.md", name: "README.md", directory: "", kind: "markdown", url: "/view/README.md?mode=diff", selected: false, changed: false, openCommentCount: 0 },
      { path: "src/main.go", name: "main.go", directory: "src", kind: "code", url: "/view/src/main.go?mode=diff", selected: true, changed: true, openCommentCount: 2 },
    ],
  };
}

describe("parseDocumentCatalogState", () => {
  it("validates wire data and indexes documents by path", () => {
    const state = parseDocumentCatalogState(validState());

    expect(state.selectedPath).toBe("src/main.go");
    expect(state.documentsByPath.get("src/main.go")?.openCommentCount).toBe(2);
    expect(state.documents[1]?.kind).toBe("code");
  });

  it.each([
    ["unknown schema", { ...validState(), schemaVersion: 2 }],
    ["invalid documents", { ...validState(), documents: {} }],
    ["inconsistent selected path", { ...validState(), selectedPath: "README.md" }],
    ["changed item without capability", { ...validState(), changedAvailable: false }],
    ["conflicting changed result", { ...validState(), changedError: true }],
    ["invalid path", { ...validState(), documents: [{ path: "../bad.md", name: "bad.md", directory: "..", kind: "markdown", url: "/view/bad.md", selected: true, changed: false, openCommentCount: 0 }], selectedPath: "../bad.md" }],
  ])("rejects %s", (_name, value) => {
    expect(() => parseDocumentCatalogState(value)).toThrow();
  });

  it("rejects duplicate paths", () => {
    const state = validState();
    const documents = state.documents as unknown[];
    documents.push(documents[0]);

    expect(() => parseDocumentCatalogState(state)).toThrow(/unique/u);
  });

  it("accepts an empty catalog with no selected document", () => {
    const state = parseDocumentCatalogState({
      schemaVersion: 1, selectedPath: null, mode: "file",
      changedAvailable: false, changedError: false, reviewAvailable: false, documents: [],
    });

    expect(state.documents).toEqual([]);
    expect(state.documentsByPath.size).toBe(0);
  });
});
