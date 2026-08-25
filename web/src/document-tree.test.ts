// @vitest-environment happy-dom

import { beforeEach, describe, expect, it } from "vitest";

import { bindDocumentTree } from "./document-tree.js";

const directory = (id: string, name: string) =>
  `<li id="${id}" class="document-directory">
  <button class="document-directory-toggle" aria-expanded="true">${name}</button>
  <ul class="document-tree-children"></ul>
</li>`;

function treeDocument(contents: string): Document {
  const treeDocument = document.implementation.createHTMLDocument();
  treeDocument.body.innerHTML = `<section id="document-panel-content">${contents}</section>`;
  return treeDocument;
}

describe("document tree", () => {
  beforeEach(() => {
    window.sessionStorage.clear();
  });

  it("preserves explicit collapses while newly swapped directories default expanded", () => {
    const testDocument = treeDocument(
      directory("document-directory-guides", "guides"),
    );
    bindDocumentTree(testDocument, window.sessionStorage);

    testDocument
      .querySelector<HTMLButtonElement>(".document-directory-toggle")
      ?.click();
    expect(
      window.sessionStorage.getItem("code-annotator.document-tree-collapsed"),
    ).toBe('["document-directory-guides"]');

    const panel = testDocument.querySelector<HTMLElement>(
      "#document-panel-content",
    );
    if (!panel) throw new Error("expected document panel");
    panel.innerHTML =
      directory("document-directory-guides", "guides") +
      directory("document-directory-new", "new");
    testDocument.dispatchEvent(new Event("htmx:afterSwap"));

    // HTMX restores the server-rendered class during its settle phase.
    testDocument
      .querySelector("#document-directory-guides")
      ?.classList.remove("collapsed");
    testDocument.dispatchEvent(new Event("htmx:afterSettle"));

    expect(
      testDocument
        .querySelector("#document-directory-guides")
        ?.classList.contains("collapsed"),
    ).toBe(true);
    expect(
      testDocument
        .querySelector("#document-directory-new .document-directory-toggle")
        ?.getAttribute("aria-expanded"),
    ).toBe("true");
  });

  it("migrates visible directories from the legacy expanded preference", () => {
    const testDocument = treeDocument(
      directory("document-directory-expanded", "expanded") +
        directory("document-directory-collapsed", "collapsed"),
    );
    window.sessionStorage.setItem(
      "code-annotator.document-tree-expanded",
      '["document-directory-expanded"]',
    );

    bindDocumentTree(testDocument, window.sessionStorage);

    expect(
      testDocument
        .querySelector(
          "#document-directory-expanded .document-directory-toggle",
        )
        ?.getAttribute("aria-expanded"),
    ).toBe("true");
    expect(
      testDocument
        .querySelector(
          "#document-directory-collapsed .document-directory-toggle",
        )
        ?.getAttribute("aria-expanded"),
    ).toBe("false");
    expect(
      window.sessionStorage.getItem("code-annotator.document-tree-collapsed"),
    ).toBe('["document-directory-collapsed"]');
  });
});
