"use strict";
// Apply saved layout preferences as soon as the layout element is parsed. The
// blocking script is its first child, so panels never paint in the server's
// default state before the interactive viewer controller takes over.
(() => {
    const layout = document.currentScript?.parentElement;
    if (!layout?.classList.contains("layout"))
        return;
    try {
        if (sessionStorage.getItem("code-annotator.panel-collapsed.documents") ===
            "true") {
            layout.classList.add("documents-collapsed");
        }
        const annotations = sessionStorage.getItem("code-annotator.panel-collapsed.annotations");
        if (layout.classList.contains("review-layout") &&
            (annotations === null || annotations === "true")) {
            layout.classList.add("review-collapsed");
        }
        const documentScope = sessionStorage.getItem("code-annotator.document-scope");
        if (documentScope === "all" ||
            documentScope === "changed" ||
            documentScope === "open-comments") {
            layout.classList.add("document-scope-restoring");
        }
    }
    catch (_) {
        // Storage can be unavailable in privacy-restricted contexts. The viewer
        // controller will retain the server defaults in that case.
    }
})();
