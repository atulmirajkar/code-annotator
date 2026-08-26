// @vitest-environment happy-dom

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { bindKeyboardShortcuts } from "./keyboard-shortcuts.js";

interface ShortcutsView {
  testDocument: Document;
  status: HTMLElement;
  dialog: HTMLDialogElement;
  openButton: HTMLButtonElement;
  closeButton: HTMLButtonElement;
  enabledCheckbox: HTMLInputElement;
  documentsToggle: HTMLButtonElement;
  documentsSidebar: HTMLElement;
  reviewToggle: HTMLButtonElement | null;
  reviewSidebar: HTMLElement | null;
  press: (key: string, init?: KeyboardEventInit) => KeyboardEvent;
}

// Each keyboard shortcut context binds directly to a Document, so every test
// needs its own Document (as document-tree.test.ts does) rather than the
// shared global one — otherwise successive bindKeyboardShortcuts calls would
// stack keydown listeners on top of each other across tests.
function fixture(options: { review: boolean }): ShortcutsView {
  const testDocument = document.implementation.createHTMLDocument();
  testDocument.body.innerHTML = `
    <button type="button" class="documents-toggle" aria-controls="documents-sidebar" aria-expanded="true">Hide documents</button>
    ${options.review ? '<button type="button" class="review-toggle" aria-controls="annotation-sidebar" aria-expanded="true">Hide annotations</button>' : ""}
    <p class="shortcut-status" role="status"></p>
    <button type="button" class="shortcuts-open">Keyboard shortcuts</button>
    <dialog class="shortcuts-dialog">
      <button type="button" class="shortcuts-dialog-close">Close</button>
      <input type="checkbox" class="shortcuts-enabled-toggle">
    </dialog>
    <nav id="documents-sidebar"></nav>
    ${options.review ? '<aside id="annotation-sidebar"></aside>' : ""}
  `;
  const documentsToggle = testDocument.querySelector<HTMLButtonElement>(
    ".documents-toggle",
  );
  const documentsSidebar = testDocument.getElementById("documents-sidebar");
  const reviewToggle = testDocument.querySelector<HTMLButtonElement>(
    ".review-toggle",
  );
  const reviewSidebar = testDocument.getElementById("annotation-sidebar");
  // Real toggle behavior is owned by viewer-layout.ts; this fixture stands in
  // for it so activatePanelToggle's button.click() has something to observe.
  for (const button of [documentsToggle, reviewToggle]) {
    if (!button) continue;
    const panelID = button.getAttribute("aria-controls");
    const panel = panelID ? testDocument.getElementById(panelID) : null;
    button.addEventListener("click", () => {
      const expanded = button.getAttribute("aria-expanded") !== "true";
      button.setAttribute("aria-expanded", String(expanded));
      if (panel) panel.hidden = !expanded;
    });
  }
  const status = testDocument.querySelector<HTMLElement>(".shortcut-status");
  const dialog = testDocument.querySelector<HTMLDialogElement>(
    ".shortcuts-dialog",
  );
  const openButton = testDocument.querySelector<HTMLButtonElement>(
    ".shortcuts-open",
  );
  const closeButton = testDocument.querySelector<HTMLButtonElement>(
    ".shortcuts-dialog-close",
  );
  const enabledCheckbox = testDocument.querySelector<HTMLInputElement>(
    ".shortcuts-enabled-toggle",
  );
  if (
    !status ||
    !dialog ||
    !openButton ||
    !closeButton ||
    !enabledCheckbox ||
    !documentsToggle ||
    !documentsSidebar ||
    (options.review && (!reviewToggle || !reviewSidebar))
  ) {
    throw new Error("incomplete keyboard shortcuts fixture");
  }
  bindKeyboardShortcuts(testDocument, window, window.sessionStorage);
  return {
    testDocument,
    status,
    dialog,
    openButton,
    closeButton,
    enabledCheckbox,
    documentsToggle,
    documentsSidebar,
    reviewToggle,
    reviewSidebar,
    press: (key, init = {}) => keydown(testDocument, key, init),
  };
}

function keydown(
  target: EventTarget,
  key: string,
  init: KeyboardEventInit = {},
): KeyboardEvent {
  const event = new KeyboardEvent("keydown", {
    key,
    bubbles: true,
    cancelable: true,
    ...init,
  });
  target.dispatchEvent(event);
  return event;
}

describe("keyboard shortcuts", () => {
  beforeEach(() => {
    window.sessionStorage.clear();
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("arms the leader on a bare Space and shows the pending status", () => {
    const view = fixture({ review: true });

    const event = view.press(" ");

    expect(event.defaultPrevented).toBe(true);
    expect(view.status.textContent).toBe("Shortcut: Space …");
  });

  it("completes Space then E by activating the documents toggle", () => {
    const view = fixture({ review: true });

    view.press(" ");
    view.press("e");

    expect(view.documentsToggle.getAttribute("aria-expanded")).toBe("false");
    expect(view.documentsSidebar.hidden).toBe(true);
    expect(view.status.textContent).toBe("Documents sidebar hidden.");
  });

  it("matches the second key case-insensitively", () => {
    const view = fixture({ review: true });

    view.press(" ");
    view.press("E");

    expect(view.documentsToggle.getAttribute("aria-expanded")).toBe("false");
  });

  it("completes Space then R by activating the review toggle when available", () => {
    const view = fixture({ review: true });

    view.press(" ");
    view.press("r");

    expect(view.reviewToggle?.getAttribute("aria-expanded")).toBe("false");
    expect(view.status.textContent).toBe("Review sidebar hidden.");
  });

  it("dispatches code-annotator:add-comment for Space then C when review is available", () => {
    const view = fixture({ review: true });
    const handler = vi.fn();
    view.testDocument.addEventListener("code-annotator:add-comment", handler);

    view.press(" ");
    view.press("c");

    expect(handler).toHaveBeenCalledTimes(1);
    expect(view.status.textContent).toBe("");
  });

  it("does not reserve R or C outside review mode", () => {
    const view = fixture({ review: false });
    const handler = vi.fn();
    view.testDocument.addEventListener("code-annotator:add-comment", handler);

    view.press(" ");
    view.press("r");

    expect(handler).not.toHaveBeenCalled();
    expect(view.status.textContent).toBe("");
  });

  it("cancels the leader on Escape without announcing anything", () => {
    const view = fixture({ review: true });

    view.press(" ");
    view.press("Escape");
    view.press("e");

    expect(view.status.textContent).toBe("");
    expect(view.documentsToggle.getAttribute("aria-expanded")).toBe("true");
  });

  it("cancels the leader after the timeout without announcing anything", () => {
    const view = fixture({ review: true });

    view.press(" ");
    vi.advanceTimersByTime(1000);
    view.press("e");

    expect(view.status.textContent).toBe("");
    expect(view.documentsToggle.getAttribute("aria-expanded")).toBe("true");
  });

  it("ignores a repeated Space keydown instead of arming or re-arming", () => {
    const view = fixture({ review: true });

    view.press(" ", { repeat: true });

    expect(view.status.textContent).toBe("");
  });

  it("does not arm the leader while focus is in an editable control", () => {
    const view = fixture({ review: true });
    const input = view.testDocument.createElement("input");
    view.testDocument.body.appendChild(input);
    input.focus();

    keydown(input, " ");

    expect(view.status.textContent).toBe("");
  });

  it("opens the dialog on ? and restores focus to the invoker on close", () => {
    const view = fixture({ review: true });
    view.openButton.focus();

    view.press("?", { shiftKey: true });

    expect(view.dialog.open).toBe(true);
    view.dialog.close();
    expect(view.testDocument.activeElement).toBe(view.openButton);
  });

  it("does not open the dialog with ? while shortcuts are disabled", () => {
    const view = fixture({ review: true });
    view.enabledCheckbox.click();

    view.press("?", { shiftKey: true });

    expect(view.dialog.open).toBe(false);
  });

  it("opens the dialog from the always-available top-bar button while disabled", () => {
    const view = fixture({ review: true });
    view.enabledCheckbox.click();

    view.openButton.click();

    expect(view.dialog.open).toBe(true);
  });

  it("cancels a pending leader immediately when shortcuts are disabled", () => {
    const view = fixture({ review: true });

    view.press(" ");
    view.enabledCheckbox.click();
    view.press("e");

    expect(view.status.textContent).toBe("");
    expect(view.documentsToggle.getAttribute("aria-expanded")).toBe("true");
  });

  it("persists the enabled preference across bindings", () => {
    const first = fixture({ review: true });
    first.enabledCheckbox.click();
    expect(
      window.sessionStorage.getItem("code-annotator.global-shortcuts-enabled"),
    ).toBe("false");

    const second = fixture({ review: true });
    expect(second.enabledCheckbox.checked).toBe(false);

    second.press("?", { shiftKey: true });
    expect(second.dialog.open).toBe(false);
  });
});
