import { createAnnotation, fetchAnnotations } from "./review-api.js";
import { createAnnotationActions } from "./review-actions.js";
import { createAnnotationHighlighter } from "./review-highlights.js";
import { createAnnotationNavigator } from "./review-navigation.js";
import { createReviewPanelController } from "./review-panel.js";
import { createAnnotationRenderer } from "./review-render.js";
import { createSelectionController } from "./review-selection.js";
import type { AnnotationPayload, CreateAnnotationRequest, AnnotationIntent } from "./types.js";

function requiredElement<T extends Element>(value: T | null, label: string): T {
  if (!value) throw new Error(`Missing ${label} in review template`);
  return value;
}

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error && error.message ? error.message : fallback;
}

(() => {
  const panel = document.querySelector<HTMLElement>(".review-panel");
  if (!panel) return;

  const list = requiredElement(panel.querySelector<HTMLElement>(".annotation-list"), "annotation list");
  const count = requiredElement(panel.querySelector<HTMLElement>(".annotation-count"), "annotation count");
  const showInactive = requiredElement(panel.querySelector<HTMLInputElement>(".show-inactive-annotations"), "inactive toggle");
  const preview = requiredElement(panel.querySelector<HTMLElement>(".selection-preview"), "selection preview");
  const previewQuote = requiredElement(panel.querySelector<HTMLElement>(".selection-quote"), "selection quote");
  const previewRange = requiredElement(panel.querySelector<HTMLElement>(".selection-range"), "selection range");
  const markdown = requiredElement(document.querySelector<HTMLElement>(".markdown-body"), "markdown body");
  const form = requiredElement(panel.querySelector<HTMLFormElement>(".annotation-form"), "annotation form");
  const addAnnotationButton = panel.querySelector<HTMLButtonElement>(".add-annotation-toggle");
  const closeAnnotationButton = panel.querySelector<HTMLButtonElement>(".annotation-form-close");
  const formStatus = requiredElement(panel.querySelector<HTMLElement>(".annotation-form-status"), "form status");
  const submitButton = requiredElement(form.querySelector<HTMLButtonElement>('button[type="submit"]'), "submit button");
  const selectionScope = requiredElement(form.querySelector<HTMLInputElement>('input[name="scope"][value="selection"]'), "selection scope");
  const documentScope = requiredElement(form.querySelector<HTMLInputElement>('input[name="scope"][value="document"]'), "document scope");
  const resizeHandle = panel.querySelector<HTMLElement>(".review-panel-resize");
  const layout = panel.closest<HTMLElement>(".layout");
  const reviewToken = document.querySelector<HTMLMetaElement>('meta[name="code-annotator-review-token"]')?.content || "";
  const documentPath = panel.dataset.document;
  let currentRevision = "";
  let updateReattachControls = () => {};
  let renderAnnotations = (_payload: AnnotationPayload): void => {};
  let showMessage = (_message: string): void => {};
  if (!documentPath) {
    list.replaceChildren();
    const item = document.createElement("p");
    item.className = "review-message";
    item.textContent = "Open a Markdown document to review annotations.";
    list.append(item);
    count.textContent = "";
    return;
  }
  const reviewDocumentPath = documentPath;

  const panelController = createReviewPanelController({
    panel,
    form,
    formStatus,
    addAnnotationButton,
    closeAnnotationButton,
    layout,
    resizeHandle,
    documentPath: reviewDocumentPath,
  });
  const { setAnnotationFormVisible, setFormStatus } = panelController;
  const selectionController = createSelectionController({
    panel,
    markdown,
    preview,
    previewQuote,
    previewRange,
    selectionScope,
    documentScope,
    onSelectionChanged: () => updateReattachControls(),
  });
  const {
    currentSelection,
    forceClearSelectionPreview,
    sourceSpan,
    sourceSpanRange,
    utf8Length,
  } = selectionController;
  const { renderAnnotationHighlights, sourceRange } = createAnnotationHighlighter({ markdown, sourceSpan, sourceSpanRange, utf8Length });
  const { navigateFromAnnotation } = createAnnotationNavigator({ markdown, sourceRange, sourceSpan });
  const actionController = createAnnotationActions({
    documentPath,
    reviewToken,
    getCurrentRevision: () => currentRevision,
    currentSelection,
    forceClearSelectionPreview,
    loadAnnotations,
    setFormStatus,
    list,
  });
  const {
    createQuickClose,
    createReattachForm,
    createReplyForm,
    createLifecycleForm,
  } = actionController;
  updateReattachControls = actionController.updateReattachControls;
  const renderer = createAnnotationRenderer({
    list,
    count,
    showInactive,
    renderAnnotationHighlights,
    navigateFromAnnotation,
    createQuickClose,
    createReattachForm,
    createReplyForm,
    createLifecycleForm,
  });
  renderAnnotations = renderer.renderAnnotations;
  showMessage = renderer.showMessage;

  loadAnnotations();
  form.addEventListener("submit", submitAnnotation);
  showInactive.addEventListener("change", () => {
    const payload = renderer.currentPayload();
    if (payload) renderAnnotations(payload);
  });
  selectionController.bind();

  async function loadAnnotations(): Promise<void> {
    try {
      const response = await fetchAnnotations(reviewDocumentPath);
      if (!response.ok) throw new Error(`annotation request failed: ${response.status}`);
      const payload = await response.json() as AnnotationPayload;
      currentRevision = typeof payload.revision === "string" ? payload.revision : "";
      renderAnnotations(payload);
      document.dispatchEvent(new CustomEvent("code-annotator:annotations-updated", {
        detail: { document: reviewDocumentPath },
      }));
    } catch (_) {
      showMessage("Could not load annotations. Refresh to try again.");
    }
  }

  async function submitAnnotation(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    setFormStatus("Saving…");
    submitButton.disabled = true;

    const fields = new FormData(form);
    const payload: CreateAnnotationRequest = {
      document: reviewDocumentPath,
      intent: String(fields.get("intent") || "") as AnnotationIntent,
      comment: String(fields.get("comment") || ""),
      role: String(fields.get("role") || "") as CreateAnnotationRequest["role"],
    };
    const selectedRange = currentSelection();
    if (fields.get("scope") === "selection" && selectedRange) {
      payload.selection = selectedRange;
    }

    try {
      const response = await createAnnotation(reviewToken, currentRevision, payload);
      if (!response.ok) {
        if (response.status === 409) {
          await loadAnnotations();
          throw new Error("The document or annotations changed. Refresh and select again.");
        }
        throw new Error((await response.text()).trim() || `Could not save annotation (${response.status}).`);
      }

      (form.elements.namedItem("comment") as HTMLTextAreaElement).value = "";
      window.getSelection()?.removeAllRanges();
      forceClearSelectionPreview();
      await loadAnnotations();
      setAnnotationFormVisible(false);
      setFormStatus("Annotation added.");
    } catch (error) {
      setFormStatus(errorMessage(error, "Could not save annotation."), true);
    } finally {
      submitButton.disabled = false;
    }
  }

})();
