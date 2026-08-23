import { createAnnotation, fetchAnnotations } from "./review-api.js";
import { createAnnotationActions } from "./review-actions.js";
import { createAnnotationHighlighter } from "./review-highlights.js";
import { createAnnotationNavigator } from "./review-navigation.js";
import { createReviewPanelController } from "./review-panel.js";
import { createAnnotationRenderer } from "./review-render.js";
import { createSelectionController } from "./review-selection.js";

(() => {
  const panel = document.querySelector(".review-panel");
  if (!panel) return;

  const list = panel.querySelector(".annotation-list");
  const count = panel.querySelector(".annotation-count");
  const showInactive = panel.querySelector(".show-inactive-annotations");
  const preview = panel.querySelector(".selection-preview");
  const previewQuote = panel.querySelector(".selection-quote");
  const previewRange = panel.querySelector(".selection-range");
  const markdown = document.querySelector(".markdown-body");
  const form = panel.querySelector(".annotation-form");
  const addAnnotationButton = panel.querySelector(".add-annotation-toggle");
  const closeAnnotationButton = panel.querySelector(".annotation-form-close");
  const formStatus = panel.querySelector(".annotation-form-status");
  const submitButton = form.querySelector('button[type="submit"]');
  const selectionScope = form.querySelector('input[name="scope"][value="selection"]');
  const documentScope = form.querySelector('input[name="scope"][value="document"]');
  const resizeHandle = panel.querySelector(".review-panel-resize");
  const layout = panel.closest(".layout");
  const reviewToken = document.querySelector('meta[name="code-annotator-review-token"]')?.content || "";
  const documentPath = panel.dataset.document;
  let currentRevision = "";
  let updateReattachControls = () => {};
  let renderAnnotations = () => {};
  let showMessage = () => {};
  if (!documentPath) {
    list.replaceChildren();
    const item = document.createElement("p");
    item.className = "review-message";
    item.textContent = "Open a Markdown document to review annotations.";
    list.append(item);
    count.textContent = "";
    return;
  }

  const panelController = createReviewPanelController({
    panel,
    form,
    formStatus,
    addAnnotationButton,
    closeAnnotationButton,
    layout,
    resizeHandle,
    documentPath,
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
    reviewerAuthor: () => form.elements.author.value,
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

  async function loadAnnotations() {
    try {
      const response = await fetchAnnotations(documentPath);
      if (!response.ok) throw new Error(`annotation request failed: ${response.status}`);
      const payload = await response.json();
      currentRevision = typeof payload.revision === "string" ? payload.revision : "";
      renderAnnotations(payload);
    } catch (_) {
      showMessage("Could not load annotations. Refresh to try again.");
    }
  }

  async function submitAnnotation(event) {
    event.preventDefault();
    setFormStatus("Saving…");
    submitButton.disabled = true;

    const fields = new FormData(form);
    const payload = {
      document: documentPath,
      intent: fields.get("intent"),
      comment: fields.get("comment"),
      author: fields.get("author"),
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

      form.elements.comment.value = "";
      window.getSelection()?.removeAllRanges();
      forceClearSelectionPreview();
      await loadAnnotations();
      setAnnotationFormVisible(false);
      setFormStatus("Annotation added.");
    } catch (error) {
      setFormStatus(error.message || "Could not save annotation.", true);
    } finally {
      submitButton.disabled = false;
    }
  }

})();
