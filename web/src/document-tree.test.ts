// @vitest-environment happy-dom

import { beforeEach, describe, expect, it } from "vitest";

import { bindDocumentTree } from "./document-tree.js";

const directory = `<li id="document-directory-guides" class="document-directory">
  <button class="document-directory-toggle" aria-expanded="true">guides</button>
  <ul class="document-tree-children"></ul>
</li>`;

describe("document tree", () => {
  beforeEach(() => {
    document.body.innerHTML = `<section id="document-panel-content">${directory}</section>`;
    window.sessionStorage.clear();
  });

  it("restores typed expansion state after an HTMX swap", () => {
    bindDocumentTree(document, window.sessionStorage);

    document
      .querySelector<HTMLButtonElement>(".document-directory-toggle")
      ?.click();
    expect(
      window.sessionStorage.getItem("code-annotator.document-tree-expanded"),
    ).toBe("[]");

    const panel = document.querySelector<HTMLElement>(
      "#document-panel-content",
    );
    if (!panel) throw new Error("expected document panel");
    panel.innerHTML = directory;
    document.dispatchEvent(new Event("htmx:afterSwap"));

    expect(
      document
        .querySelector(".document-directory")
        ?.classList.contains("collapsed"),
    ).toBe(true);
    expect(
      document
        .querySelector(".document-directory-toggle")
        ?.getAttribute("aria-expanded"),
    ).toBe("false");
  });
});
