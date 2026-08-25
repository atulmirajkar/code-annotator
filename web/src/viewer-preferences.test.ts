import { describe, expect, it } from "vitest";

import { clampInteger, resolveDocumentScope } from "./viewer-preferences.js";
import type { DocumentCatalogItem } from "./document-state.js";

const document = (changed: boolean): DocumentCatalogItem => ({
  path: "README.md", name: "README.md", directory: "", kind: "markdown",
  url: "/view/README.md", selected: true, changed, openCommentCount: 0,
});

describe("viewer preferences", () => {
  it("resolves explicit, legacy, and catalog-derived document scopes", () => {
    expect(resolveDocumentScope("open-comments", null, [document(true)])).toBe("open-comments");
    expect(resolveDocumentScope(null, "true", [document(false)])).toBe("changed");
    expect(resolveDocumentScope(null, null, [document(true)])).toBe("changed");
    expect(resolveDocumentScope(null, null, [document(false)])).toBe("all");
  });

  it("rounds and bounds numeric view preferences", () => {
    expect(clampInteger(19.8, 20, 80)).toBe(20);
    expect(clampInteger(50.6, 20, 80)).toBe(51);
    expect(clampInteger(99, 20, 80)).toBe(80);
  });
});
