import { bindComparisonControl } from "./comparison-control.js";
import { bindDiffDivider } from "./diff-divider.js";
import { bindDiffOverview } from "./diff-overview.js";
import { bindDocumentSearch } from "./document-search.js";
import { bindDocumentTree } from "./document-tree.js";
import { defaultViewerEnvironment, } from "./viewer-environment.js";
import { bindViewerLayout } from "./viewer-layout.js";
// viewer.ts is the browser composition root. Each imported module owns one
// interaction area and exposes a single binding entrypoint.
export function initializeViewer(environment = defaultViewerEnvironment()) {
    const layout = environment.document.querySelector(".layout");
    if (!layout)
        return;
    configureHTMX(environment.htmx);
    bindViewerLayout(environment.document, environment.storage, environment.resizeObserver, layout);
    bindDocumentTree(environment.document, environment.storage);
    bindDocumentSearch(environment);
    bindComparisonControl(environment.document);
    bindDiffDivider(environment.document, environment.storage);
    bindDiffOverview(environment);
}
// Harden HTMX before any component can issue a request.
function configureHTMX(api) {
    if (!api)
        return;
    api.config.allowEval = false;
    api.config.allowNestedOobSwaps = false;
    api.config.allowScriptTags = false;
    api.config.historyCacheSize = 0;
    api.config.selfRequestsOnly = true;
}
initializeViewer();
