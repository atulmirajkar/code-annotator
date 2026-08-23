import { createAnnotation, fetchAnnotations } from "./review-api.js";
import { createAnnotationActions } from "./review-actions.js";
import { createAnnotationHighlighter } from "./review-highlights.js";
import { createAnnotationNavigator } from "./review-navigation.js";
import { createReviewPanelController } from "./review-panel.js";
import { createAnnotationRenderer } from "./review-render.js";
import { createSelectionController } from "./review-selection.js";
function requiredElement(value, label) {
    if (!value)
        throw new Error(`Missing ${label} in review template`);
    return value;
}
function errorMessage(error, fallback) {
    return error instanceof Error && error.message ? error.message : fallback;
}
(() => {
    const panel = document.querySelector(".review-panel");
    if (!panel)
        return;
    const list = requiredElement(panel.querySelector(".annotation-list"), "annotation list");
    const count = requiredElement(panel.querySelector(".annotation-count"), "annotation count");
    const showInactive = requiredElement(panel.querySelector(".show-inactive-annotations"), "inactive toggle");
    const preview = requiredElement(panel.querySelector(".selection-preview"), "selection preview");
    const previewQuote = requiredElement(panel.querySelector(".selection-quote"), "selection quote");
    const previewRange = requiredElement(panel.querySelector(".selection-range"), "selection range");
    const markdown = requiredElement(document.querySelector(".markdown-body"), "markdown body");
    const form = requiredElement(panel.querySelector(".annotation-form"), "annotation form");
    const addAnnotationButton = panel.querySelector(".add-annotation-toggle");
    const closeAnnotationButton = panel.querySelector(".annotation-form-close");
    const formStatus = requiredElement(panel.querySelector(".annotation-form-status"), "form status");
    const submitButton = requiredElement(form.querySelector('button[type="submit"]'), "submit button");
    const selectionScope = requiredElement(form.querySelector('input[name="scope"][value="selection"]'), "selection scope");
    const documentScope = requiredElement(form.querySelector('input[name="scope"][value="document"]'), "document scope");
    const resizeHandle = panel.querySelector(".review-panel-resize");
    const layout = panel.closest(".layout");
    const reviewToken = document.querySelector('meta[name="code-annotator-review-token"]')?.content || "";
    const documentPath = panel.dataset.document;
    let currentRevision = "";
    let updateReattachControls = () => { };
    let renderAnnotations = (_payload) => { };
    let showMessage = (_message) => { };
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
    const { currentSelection, forceClearSelectionPreview, sourceSpan, sourceSpanRange, utf8Length, } = selectionController;
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
        reviewerAuthor: () => String(form.elements.namedItem("author")?.value || ""),
        list,
    });
    const { createQuickClose, createReattachForm, createReplyForm, createLifecycleForm, } = actionController;
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
        if (payload)
            renderAnnotations(payload);
    });
    selectionController.bind();
    async function loadAnnotations() {
        try {
            const response = await fetchAnnotations(reviewDocumentPath);
            if (!response.ok)
                throw new Error(`annotation request failed: ${response.status}`);
            const payload = await response.json();
            currentRevision = typeof payload.revision === "string" ? payload.revision : "";
            renderAnnotations(payload);
            document.dispatchEvent(new CustomEvent("code-annotator:annotations-updated", {
                detail: { document: reviewDocumentPath },
            }));
        }
        catch (_) {
            showMessage("Could not load annotations. Refresh to try again.");
        }
    }
    async function submitAnnotation(event) {
        event.preventDefault();
        setFormStatus("Saving…");
        submitButton.disabled = true;
        const fields = new FormData(form);
        const payload = {
            document: reviewDocumentPath,
            intent: String(fields.get("intent") || ""),
            comment: String(fields.get("comment") || ""),
            author: String(fields.get("author") || ""),
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
            form.elements.namedItem("comment").value = "";
            window.getSelection()?.removeAllRanges();
            forceClearSelectionPreview();
            await loadAnnotations();
            setAnnotationFormVisible(false);
            setFormStatus("Annotation added.");
        }
        catch (error) {
            setFormStatus(errorMessage(error, "Could not save annotation."), true);
        }
        finally {
            submitButton.disabled = false;
        }
    }
})();
