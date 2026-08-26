import { bindAnnotationCardDisclosures } from "./annotation-card-disclosures.js";
import type { AnnotationOperationFocus } from "./annotation-card-disclosures.js";
import { fetchDocumentCatalogState } from "./document-state.js";
import { configureLifecycleForm } from "./review-fragments.js";
import { createAnnotationHighlighter } from "./review-highlights.js";
import { configureReviewHTMX } from "./review-htmx.js";
import type { ReviewHtmxAPI, ReviewMutationKind } from "./review-htmx.js";
import { createAnnotationNavigator } from "./review-navigation.js";
import { createReviewPanelController } from "./review-panel.js";
import { createSelectionController } from "./review-selection.js";
import type { SelectionPayload } from "./review-selection.js";
import { fetchViewerState } from "./viewer-state.js";
import type { AnnotationBrowserState, ViewerState } from "./viewer-state.js";

interface ReviewElements {
  panel: HTMLElement;
  markdown: HTMLElement;
  preview: HTMLElement;
  previewQuote: HTMLElement;
  previewRange: HTMLElement;
  form: HTMLFormElement;
  formStatus: HTMLElement;
  selectionScope: HTMLInputElement;
  documentScope: HTMLInputElement;
  showInactive: HTMLInputElement;
  reviewToken: string;
}

interface ReviewContext extends ReviewElements {
  document: Document;
  window: Window;
  documentPath: string;
  mode: "file" | "diff";
  viewerState: ViewerState;
  panelController: ReturnType<typeof createReviewPanelController>;
  selectionController: ReturnType<typeof createSelectionController>;
  cardOperationFocus: AnnotationOperationFocus;
  renderAnnotationHighlights: ReturnType<
    typeof createAnnotationHighlighter
  >["renderAnnotationHighlights"];
  navigateFromAnnotation: ReturnType<
    typeof createAnnotationNavigator
  >["navigateFromAnnotation"];
}

export interface ReviewEnvironment {
  document: Document;
  window: Window;
  htmx: ReviewHtmxAPI | null;
}

const selectionFieldNames = [
  "selection_start_byte",
  "selection_end_byte",
  "document_sha256",
] as const;
let activeReviewCleanup: (() => void) | null = null;
let reviewGeneration = 0;

// initializeReview is a composition root: it resolves required elements,
// loads typed state, creates focused controllers, and wires module-level event
// handlers. Shared mutable state is explicit in ReviewContext.
export async function initializeReview(
  environment: ReviewEnvironment = defaultReviewEnvironment(),
): Promise<void> {
  const generation = ++reviewGeneration;
  activeReviewCleanup?.();
  activeReviewCleanup = null;
  const panel =
    environment.document.querySelector<HTMLElement>(".review-panel");
  if (!panel) return;

  const elements = reviewElements(environment.document, panel);
  const documentPath = await currentDocumentPath(environment.window.location);
  const mode = reviewMode(environment.window.location);
  const panelController = createReviewPanelController({
    panel,
    form: elements.form,
    formStatus: elements.formStatus,
    addAnnotationButton: panel.querySelector(".add-annotation-toggle"),
    closeAnnotationButton: panel.querySelector(".annotation-form-close"),
    layout: panel.closest(".layout"),
    resizeHandle: panel.querySelector(".review-panel-resize"),
    documentPath,
    storage: environment.window.localStorage,
  });

  const viewerState = await loadInitialViewerState(
    documentPath,
    mode,
    panelController,
  );
  if (!viewerState || generation !== reviewGeneration || !panel.isConnected) return;

  prepareDiagramSelection(environment.document, elements.markdown, viewerState);

  // Selection events can fire only after start(), which runs after context is
  // assigned. The nullable relay breaks the controller construction cycle.
  let context: ReviewContext | null = null;
  const bindings = new AbortController();
  // Bound ahead of context construction so its returned handle can be stored
  // on ReviewContext; the panel it delegates on outlives every fragment swap.
  const cardOperationFocus = bindAnnotationCardDisclosures(
    panel,
    bindings.signal,
  );
  const selectionController = createSelectionController({
    document: environment.document,
    window: environment.window,
    panel,
    markdown: elements.markdown,
    preview: elements.preview,
    previewQuote: elements.previewQuote,
    previewRange: elements.previewRange,
    selectionScope: elements.selectionScope,
    documentScope: elements.documentScope,
    documentSHA256: viewerState.document.sha256,
    sourceNodes: viewerState.document.sourceNodes,
    diagrams: viewerState.document.diagrams,
    onSelectionChanged: () => {
      if (context) updateSelectionFields(context);
    },
  });
  const highlighter = createAnnotationHighlighter({
    document: environment.document,
    markdown: elements.markdown,
    sourceSpan: selectionController.sourceSpan,
    sourceSpanRange: selectionController.sourceSpanRange,
    utf8Length: selectionController.utf8Length,
    sourceNodes: viewerState.document.sourceNodes,
    diagrams: viewerState.document.diagrams,
  });
  const navigator = createAnnotationNavigator({
    document: environment.document,
    window: environment.window,
    markdown: elements.markdown,
    sourceRange: highlighter.sourceRange,
    sourceSpan: selectionController.sourceSpan,
    sourceNodes: viewerState.document.sourceNodes,
    diagrams: viewerState.document.diagrams,
  });

  context = {
    ...elements,
    document: environment.document,
    window: environment.window,
    documentPath,
    mode,
    viewerState,
    panelController,
    selectionController,
    cardOperationFocus,
    renderAnnotationHighlights: highlighter.renderAnnotationHighlights,
    navigateFromAnnotation: navigator.navigateFromAnnotation,
  };

  bindReviewEvents(context, environment.htmx, bindings);
  selectionController.start();
  activeReviewCleanup = () => {
    bindings.abort();
    selectionController.stop();
  };
  initializePanel(context);
}

function defaultReviewEnvironment(): ReviewEnvironment {
  return {
    document,
    window,
    htmx:
      (Reflect.get(globalThis, "htmx") as ReviewHtmxAPI | undefined) ?? null,
  };
}

function reviewElements(
  document: Document,
  panel: HTMLElement,
): ReviewElements {
  const form = requiredElement(
    panel.querySelector<HTMLFormElement>(".annotation-form"),
    "annotation form",
  );
  return {
    panel,
    markdown: requiredElement(
      document.querySelector<HTMLElement>(".markdown-body"),
      "markdown body",
    ),
    preview: requiredElement(
      panel.querySelector<HTMLElement>(".selection-preview"),
      "selection preview",
    ),
    previewQuote: requiredElement(
      panel.querySelector<HTMLElement>(".selection-quote"),
      "selection quote",
    ),
    previewRange: requiredElement(
      panel.querySelector<HTMLElement>(".selection-range"),
      "selection range",
    ),
    form,
    formStatus: requiredElement(
      panel.querySelector<HTMLElement>(".annotation-form-status"),
      "form status",
    ),
    selectionScope: requiredElement(
      form.querySelector<HTMLInputElement>(
        'input[name="scope"][value="selection"]',
      ),
      "selection scope",
    ),
    documentScope: requiredElement(
      form.querySelector<HTMLInputElement>(
        'input[name="scope"][value="document"]',
      ),
      "document scope",
    ),
    showInactive: requiredElement(
      panel.querySelector<HTMLInputElement>(".show-inactive-annotations"),
      "inactive toggle",
    ),
    reviewToken:
      document.querySelector<HTMLMetaElement>(
        'meta[name="code-annotator-review-token"]',
      )?.content ?? "",
  };
}

function requiredElement<T extends Element>(value: T | null, label: string): T {
  if (!value) throw new Error(`Missing ${label} in review template`);
  return value;
}

async function currentDocumentPath(location: Location): Promise<string> {
  const prefix = "/view/";
  if (location.pathname.startsWith(prefix)) {
    return decodeURIComponent(location.pathname.slice(prefix.length));
  }
  const state = await fetchDocumentCatalogState();
  if (!state.selectedPath) {
    throw new Error("Review page has no selected document");
  }
  return state.selectedPath;
}

function reviewMode(location: Location): "file" | "diff" {
  return new URLSearchParams(location.search).get("mode") === "diff"
    ? "diff"
    : "file";
}

async function loadInitialViewerState(
  documentPath: string,
  mode: "file" | "diff",
  panelController: ReturnType<typeof createReviewPanelController>,
): Promise<ViewerState | null> {
  let viewerState: ViewerState;
  try {
    viewerState = await fetchViewerState(documentPath, mode);
  } catch (_) {
    panelController.setFormStatus(
      "Could not load typed viewer state. Refresh to try again.",
      true,
    );
    return null;
  }
  if (!viewerState.review) {
    panelController.setFormStatus(
      "Review state is unavailable for this document.",
      true,
    );
    return null;
  }
  return viewerState;
}

// Diagram IDs come from validated viewer state. The DOM is used only to apply
// focus and presentation behavior to the matching rendered element.
function prepareDiagramSelection(
  document: Document,
  markdown: HTMLElement,
  viewerState: ViewerState,
): void {
  for (const position of viewerState.document.diagrams.values()) {
    const diagram = document.getElementById(position.elementId);
    if (!diagram || !markdown.contains(diagram)) continue;
    diagram.classList.add("annotation-selectable");
    const output = diagram.querySelector<HTMLElement>(".mermaid-output");
    if (!output) continue;
    output.tabIndex = 0;
    output.setAttribute(
      "aria-label",
      "Rendered Mermaid diagram. Select the complete diagram for annotation.",
    );
  }
}

function bindReviewEvents(
  context: ReviewContext,
  htmx: ReviewHtmxAPI | null,
  bindings: AbortController,
): void {
  context.panel.addEventListener("click", (event) =>
    handleReviewPanelClick(context, event), { signal: bindings.signal },
  );
  context.panel.addEventListener("change", (event) =>
    handleReviewPanelChange(context, event), { signal: bindings.signal },
  );
  context.form.addEventListener("submit", () =>
    context.panelController.setFormStatus("Saving…"), { signal: bindings.signal },
  );
  configureReviewHTMX({
    document: context.document,
    api: htmx,
    panel: context.panel,
    token: context.reviewToken,
    getRevision: () => context.viewerState.review?.revision ?? "",
    onPanelChanged: (mutationKind, mutation, successful) =>
      handlePanelChanged(context, mutationKind, mutation, successful),
    onRequestError: () =>
      context.panelController.setFormStatus(
        "Could not update annotations. Refresh to try again.",
        true,
      ),
    signal: bindings.signal,
  });
}

function handleReviewPanelClick(
  context: ReviewContext,
  event: MouseEvent,
): void {
  const target = event.target instanceof Element ? event.target : null;
  const summary = target?.closest<HTMLElement>(".annotation-summary");
  const card = summary?.closest<HTMLDetailsElement>(".annotation-card");
  const annotation = card
    ? context.viewerState.review?.annotationsByElementId.get(card.id)
    : undefined;
  if (summary && card && annotation) {
    context.navigateFromAnnotation(event, card, annotation);
  }
}

function handleReviewPanelChange(context: ReviewContext, event: Event): void {
  const target = event.target;
  if (target === context.selectionScope || target === context.documentScope) {
    updateSelectionFields(context);
    return;
  }
  if (!(target instanceof HTMLSelectElement) || target.name !== "status")
    return;
  const lifecycleForm = target.closest<HTMLFormElement>(
    ".annotation-lifecycle",
  );
  const annotation = lifecycleForm
    ? context.viewerState.review?.annotationsByLifecycleFormId.get(
        lifecycleForm.id,
      )
    : undefined;
  if (lifecycleForm && annotation) {
    configureLifecycleForm(lifecycleForm, annotation.transitions, false);
  }
}

async function handlePanelChanged(
  context: ReviewContext,
  mutationKind: ReviewMutationKind,
  mutation: boolean,
  successful: boolean,
): Promise<void> {
  if (mutation) {
    const refreshed = await refreshViewerState(context);
    if (!refreshed) return;
    context.showInactive.checked = false;
  }
  initializePanel(context);
  if (!mutation) return;
  context.cardOperationFocus.restoreAfterSwap(successful);
  if (!successful) {
    context.panelController.setFormStatus(
      "Annotations changed elsewhere. Review the refreshed panel and retry.",
      true,
    );
    return;
  }
  if (mutationKind === "create") finishCreate(context);
  else if (mutationKind === "reattach") clearNativeSelection(context);

  context.document.dispatchEvent(
    new CustomEvent("code-annotator:annotations-updated", {
      detail: { document: context.documentPath },
    }),
  );
}

async function refreshViewerState(context: ReviewContext): Promise<boolean> {
  try {
    const viewerState = await fetchViewerState(
      context.documentPath,
      context.mode,
    );
    if (!viewerState.review) {
      context.panelController.setFormStatus(
        "Annotations changed, but review state is unavailable.",
        true,
      );
      return false;
    }
    context.viewerState = viewerState;
    return true;
  } catch (_) {
    context.panelController.setFormStatus(
      "Annotations changed, but viewer state could not be refreshed.",
      true,
    );
    return false;
  }
}

function finishCreate(context: ReviewContext): void {
  const comment = context.form.elements.namedItem("comment");
  if (comment instanceof HTMLTextAreaElement) comment.value = "";
  clearNativeSelection(context);
  context.panelController.setAnnotationFormVisible(false);
  context.panelController.setFormStatus("Annotation added.");
}

function clearNativeSelection(context: ReviewContext): void {
  context.window.getSelection()?.removeAllRanges();
  context.selectionController.forceClearSelectionPreview();
}

function updateSelectionFields(context: ReviewContext): void {
  const selection = context.selectionScope.checked
    ? context.selectionController.currentSelection()
    : null;
  writeSelection(context.form, selection);
  updateReattachForms(context);
}

function updateReattachForms(context: ReviewContext): void {
  for (const form of context.panel.querySelectorAll<HTMLFormElement>(
    ".annotation-reattach",
  )) {
    const selection = context.selectionController.currentSelection();
    if (selection) writeSelection(form, selection);
    const ready = hasCompleteSelection(form);
    const button = form.querySelector<HTMLButtonElement>(
      'button[type="submit"]',
    );
    if (button) button.disabled = !ready;
    const help = form.querySelector<HTMLElement>(".reattach-help");
    if (help) {
      help.textContent = ready
        ? "The selected text will replace this stale source attachment."
        : "Select replacement text in the document to enable reattachment.";
    }
  }
}

function hasCompleteSelection(form: HTMLFormElement): boolean {
  for (const name of selectionFieldNames) {
    if (!selectionInput(form, name)?.value) return false;
  }
  return true;
}

function selectionInput(
  form: HTMLFormElement,
  name: string,
): HTMLInputElement | null {
  const value = form.elements.namedItem(name);
  return value instanceof HTMLInputElement ? value : null;
}

function writeSelection(
  form: HTMLFormElement,
  selection: SelectionPayload | null,
): void {
  const values: Record<(typeof selectionFieldNames)[number], string> = {
    selection_start_byte: selection ? String(selection.startByte) : "",
    selection_end_byte: selection ? String(selection.endByte) : "",
    document_sha256: selection?.documentSHA256 ?? "",
  };
  for (const name of selectionFieldNames) {
    const input = selectionInput(form, name);
    if (input) input.value = values[name];
  }
}

function displayedAnnotations(
  context: ReviewContext,
): AnnotationBrowserState[] {
  const displayed: AnnotationBrowserState[] = [];
  for (const annotation of context.viewerState.review?.annotations.values() ??
    []) {
    if (
      context.showInactive.checked ||
      (annotation.status !== "closed" && annotation.status !== "rejected")
    ) {
      displayed.push(annotation);
    }
  }
  return displayed;
}

function initializePanel(context: ReviewContext): void {
  const content = context.panel.querySelector<HTMLElement>(
    "#annotation-panel-content",
  );
  if (!content) return;
  const annotations = displayedAnnotations(context);
  for (const annotation of annotations) {
    const lifecycleForm = context.document.getElementById(
      annotation.lifecycleFormId,
    );
    if (
      lifecycleForm instanceof HTMLFormElement &&
      content.contains(lifecycleForm)
    ) {
      configureLifecycleForm(lifecycleForm, annotation.transitions, true);
    }
  }
  updateReattachForms(context);
  context.renderAnnotationHighlights(annotations);
}

document.addEventListener("code-annotator:viewer-navigated", () => {
  void initializeReview();
});
void initializeReview();
