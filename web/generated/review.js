import { annotationLocation, annotationLocations, configureLifecycleForm } from "./review-fragments.js";
import { createAnnotationHighlighter } from "./review-highlights.js";
import { configureReviewHTMX } from "./review-htmx.js";
import { createAnnotationNavigator } from "./review-navigation.js";
import { createReviewPanelController } from "./review-panel.js";
import { createSelectionController } from "./review-selection.js";
function requiredElement(value, label) {
    if (!value)
        throw new Error(`Missing ${label} in review template`);
    return value;
}
function selectionInput(form, name) {
    const value = form.elements.namedItem(name);
    return value instanceof HTMLInputElement ? value : null;
}
function writeSelection(form, selection) {
    const values = {
        selection_start_byte: selection ? String(selection.startByte) : "",
        selection_end_byte: selection ? String(selection.endByte) : "",
        document_sha256: selection?.documentSHA256 || "",
    };
    Object.entries(values).forEach(([name, value]) => {
        const input = selectionInput(form, name);
        if (input)
            input.value = value;
    });
}
(() => {
    const panel = document.querySelector(".review-panel");
    if (!panel)
        return;
    const reviewPanel = panel;
    const documentPath = reviewPanel.dataset.document;
    if (!documentPath)
        return;
    const markdown = requiredElement(document.querySelector(".markdown-body"), "markdown body");
    const preview = requiredElement(panel.querySelector(".selection-preview"), "selection preview");
    const previewQuote = requiredElement(panel.querySelector(".selection-quote"), "selection quote");
    const previewRange = requiredElement(panel.querySelector(".selection-range"), "selection range");
    const form = requiredElement(panel.querySelector(".annotation-form"), "annotation form");
    const formStatus = requiredElement(panel.querySelector(".annotation-form-status"), "form status");
    const selectionScope = requiredElement(form.querySelector('input[name="scope"][value="selection"]'), "selection scope");
    const documentScope = requiredElement(form.querySelector('input[name="scope"][value="document"]'), "document scope");
    const showInactive = requiredElement(panel.querySelector(".show-inactive-annotations"), "inactive toggle");
    const reviewToken = document.querySelector('meta[name="code-annotator-review-token"]')?.content || "";
    const panelController = createReviewPanelController({
        panel: reviewPanel,
        form,
        formStatus,
        addAnnotationButton: panel.querySelector(".add-annotation-toggle"),
        closeAnnotationButton: panel.querySelector(".annotation-form-close"),
        layout: panel.closest(".layout"),
        resizeHandle: panel.querySelector(".review-panel-resize"),
        documentPath,
    });
    let updateSelectionFields = () => { };
    const selectionController = createSelectionController({
        panel: reviewPanel,
        markdown,
        preview,
        previewQuote,
        previewRange,
        selectionScope,
        documentScope,
        onSelectionChanged: () => updateSelectionFields(),
    });
    const { currentSelection, forceClearSelectionPreview, sourceSpan, sourceSpanRange, utf8Length } = selectionController;
    const { renderAnnotationHighlights, sourceRange } = createAnnotationHighlighter({ markdown, sourceSpan, sourceSpanRange, utf8Length });
    const { navigateFromAnnotation } = createAnnotationNavigator({ markdown, sourceRange, sourceSpan });
    updateSelectionFields = () => {
        writeSelection(form, selectionScope.checked ? currentSelection() : null);
        updateReattachForms();
    };
    function updateReattachForms() {
        reviewPanel.querySelectorAll(".annotation-reattach").forEach((reattachForm) => {
            const selection = currentSelection();
            if (selection)
                writeSelection(reattachForm, selection);
            const ready = ["selection_start_byte", "selection_end_byte", "document_sha256"]
                .every((name) => Boolean(selectionInput(reattachForm, name)?.value));
            const button = reattachForm.querySelector('button[type="submit"]');
            if (button)
                button.disabled = !ready;
            const help = reattachForm.querySelector(".reattach-help");
            if (help)
                help.textContent = ready
                    ? "The selected text will replace this stale source attachment."
                    : "Select replacement text in the document to enable reattachment.";
        });
    }
    function initializePanel() {
        const content = reviewPanel.querySelector("#annotation-panel-content");
        if (!content)
            return;
        showInactive.checked = content.dataset.showInactive === "true";
        content.querySelectorAll(".annotation-lifecycle").forEach((lifecycleForm) => {
            configureLifecycleForm(lifecycleForm, true);
        });
        updateReattachForms();
        renderAnnotationHighlights(annotationLocations(content));
    }
    reviewPanel.addEventListener("click", (event) => {
        const target = event.target instanceof Element ? event.target : null;
        const summary = target?.closest(".annotation-summary");
        const card = summary?.closest(".annotation-card");
        if (summary && card)
            navigateFromAnnotation(event, card, annotationLocation(card));
    });
    reviewPanel.addEventListener("change", (event) => {
        const target = event.target;
        if (target === selectionScope || target === documentScope) {
            updateSelectionFields();
            return;
        }
        if (target instanceof HTMLSelectElement && target.name === "status") {
            const lifecycleForm = target.closest(".annotation-lifecycle");
            if (lifecycleForm)
                configureLifecycleForm(lifecycleForm, false);
        }
    });
    form.addEventListener("submit", () => panelController.setFormStatus("Saving…"));
    configureReviewHTMX({
        panel: reviewPanel,
        token: reviewToken,
        onPanelChanged: (source, mutation, successful) => {
            initializePanel();
            if (!mutation)
                return;
            if (!successful) {
                const feedback = reviewPanel.querySelector(".annotation-panel-feedback");
                if (feedback?.textContent)
                    panelController.setFormStatus(feedback.textContent, true);
                return;
            }
            if (source?.classList.contains("annotation-form")) {
                const comment = form.elements.namedItem("comment");
                if (comment instanceof HTMLTextAreaElement)
                    comment.value = "";
                window.getSelection()?.removeAllRanges();
                forceClearSelectionPreview();
                panelController.setAnnotationFormVisible(false);
                panelController.setFormStatus("Annotation added.");
            }
            else if (source?.classList.contains("annotation-reattach")) {
                window.getSelection()?.removeAllRanges();
                forceClearSelectionPreview();
            }
            document.dispatchEvent(new CustomEvent("code-annotator:annotations-updated", {
                detail: { document: documentPath },
            }));
        },
        onRequestError: () => panelController.setFormStatus("Could not update annotations. Refresh to try again.", true),
    });
    selectionController.bind();
    initializePanel();
})();
