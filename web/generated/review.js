import { configureLifecycleForm } from "./review-fragments.js";
import { fetchDocumentCatalogState } from "./document-state.js";
import { createAnnotationHighlighter } from "./review-highlights.js";
import { configureReviewHTMX } from "./review-htmx.js";
import { createAnnotationNavigator } from "./review-navigation.js";
import { createReviewPanelController } from "./review-panel.js";
import { createSelectionController } from "./review-selection.js";
import { fetchViewerState } from "./viewer-state.js";
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
async function currentDocumentPath(location) {
    const prefix = "/view/";
    if (location.pathname.startsWith(prefix)) {
        return decodeURIComponent(location.pathname.slice(prefix.length));
    }
    const state = await fetchDocumentCatalogState();
    if (!state.selectedPath)
        throw new Error("Review page has no selected document");
    return state.selectedPath;
}
export async function initializeReview(environment = { document, window }) {
    const { document, window } = environment;
    const panel = document.querySelector(".review-panel");
    if (!panel)
        return;
    const reviewPanel = panel;
    const documentPath = await currentDocumentPath(window.location);
    const mode = new URLSearchParams(window.location.search).get("mode") === "diff" ? "diff" : "file";
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
    let viewerState;
    try {
        viewerState = await fetchViewerState(documentPath, mode);
    }
    catch (_) {
        panelController.setFormStatus("Could not load typed viewer state. Refresh to try again.", true);
        return;
    }
    if (!viewerState.review) {
        panelController.setFormStatus("Review state is unavailable for this document.", true);
        return;
    }
    viewerState.document.diagrams.forEach((position) => {
        const diagram = document.getElementById(position.elementId);
        if (!diagram || !markdown.contains(diagram))
            return;
        diagram.classList.add("annotation-selectable");
        const output = diagram.querySelector(".mermaid-output");
        if (output) {
            output.tabIndex = 0;
            output.setAttribute("aria-label", "Rendered Mermaid diagram. Select the complete diagram for annotation.");
        }
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
        documentSHA256: viewerState.document.sha256,
        sourceNodes: viewerState.document.sourceNodes,
        diagrams: viewerState.document.diagrams,
        onSelectionChanged: () => updateSelectionFields(),
    });
    const { currentSelection, forceClearSelectionPreview, sourceSpan, sourceSpanRange, utf8Length } = selectionController;
    const { renderAnnotationHighlights, sourceRange } = createAnnotationHighlighter({
        markdown,
        sourceSpan,
        sourceSpanRange,
        utf8Length,
        sourceNodes: viewerState.document.sourceNodes,
        diagrams: viewerState.document.diagrams,
    });
    const { navigateFromAnnotation } = createAnnotationNavigator({
        markdown,
        sourceRange,
        sourceSpan,
        sourceNodes: viewerState.document.sourceNodes,
        diagrams: viewerState.document.diagrams,
    });
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
    function annotationByElementId(elementId) {
        return viewerState.review?.annotationsByElementId.get(elementId);
    }
    function displayedAnnotations() {
        const annotations = Array.from(viewerState.review?.annotations.values() || []);
        return showInactive.checked
            ? annotations
            : annotations.filter((annotation) => annotation.status !== "closed" && annotation.status !== "rejected");
    }
    function initializePanel() {
        const content = reviewPanel.querySelector("#annotation-panel-content");
        if (!content)
            return;
        displayedAnnotations().forEach((annotation) => {
            const lifecycleForm = document.getElementById(annotation.lifecycleFormId);
            if (lifecycleForm instanceof HTMLFormElement && content.contains(lifecycleForm)) {
                configureLifecycleForm(lifecycleForm, annotation.transitions, true);
            }
        });
        updateReattachForms();
        renderAnnotationHighlights(displayedAnnotations());
    }
    reviewPanel.addEventListener("click", (event) => {
        const target = event.target instanceof Element ? event.target : null;
        const summary = target?.closest(".annotation-summary");
        const card = summary?.closest(".annotation-card");
        const annotation = card ? annotationByElementId(card.id) : undefined;
        if (summary && card && annotation)
            navigateFromAnnotation(event, card, annotation);
    });
    reviewPanel.addEventListener("change", (event) => {
        const target = event.target;
        if (target === selectionScope || target === documentScope) {
            updateSelectionFields();
            return;
        }
        if (target instanceof HTMLSelectElement && target.name === "status") {
            const lifecycleForm = target.closest(".annotation-lifecycle");
            const annotation = lifecycleForm ? viewerState.review?.annotationsByLifecycleFormId.get(lifecycleForm.id) : undefined;
            if (lifecycleForm && annotation)
                configureLifecycleForm(lifecycleForm, annotation.transitions, false);
        }
    });
    form.addEventListener("submit", () => panelController.setFormStatus("Saving…"));
    configureReviewHTMX({
        panel: reviewPanel,
        token: reviewToken,
        getRevision: () => viewerState.review?.revision || "",
        onPanelChanged: async (mutationKind, mutation, successful) => {
            if (mutation) {
                try {
                    viewerState = await fetchViewerState(documentPath, mode);
                }
                catch (_) {
                    panelController.setFormStatus("Annotations changed, but viewer state could not be refreshed.", true);
                    return;
                }
                showInactive.checked = false;
            }
            initializePanel();
            if (!mutation)
                return;
            if (!successful) {
                panelController.setFormStatus("Annotations changed elsewhere. Review the refreshed panel and retry.", true);
                return;
            }
            if (mutationKind === "create") {
                const comment = form.elements.namedItem("comment");
                if (comment instanceof HTMLTextAreaElement)
                    comment.value = "";
                window.getSelection()?.removeAllRanges();
                forceClearSelectionPreview();
                panelController.setAnnotationFormVisible(false);
                panelController.setFormStatus("Annotation added.");
            }
            else if (mutationKind === "reattach") {
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
}
void initializeReview();
