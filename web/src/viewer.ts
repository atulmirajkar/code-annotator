import { bindComparisonControl } from "./comparison-control.js";
import { bindDiffDivider } from "./diff-divider.js";
import { bindDocumentSearch } from "./document-search.js";
import { bindDocumentTree } from "./document-tree.js";
import {
  defaultViewerEnvironment,
  type HtmxAPI,
  type ViewerEnvironment,
} from "./viewer-environment.js";
import { bindViewerLayout } from "./viewer-layout.js";

export type { ViewerEnvironment } from "./viewer-environment.js";

// viewer.ts is the browser composition root. Each imported module owns one
// interaction area and exposes a single binding entrypoint.
export function initializeViewer(
  environment: ViewerEnvironment = defaultViewerEnvironment(),
): void {
  const layout = environment.document.querySelector<HTMLElement>(".layout");
  if (!layout) return;

  configureHTMX(environment.htmx);
  bindViewerLayout(
    environment.document,
    environment.storage,
    environment.resizeObserver,
    layout,
  );
  bindDocumentTree(environment.document, environment.storage);
  bindDocumentSearch(environment);
  bindComparisonControl(environment.document);
  bindDiffDivider(environment.document, environment.storage);
}

// Harden HTMX before any component can issue a request.
function configureHTMX(api: HtmxAPI | null): void {
  if (!api) return;
  api.config.allowEval = false;
  api.config.allowNestedOobSwaps = false;
  api.config.allowScriptTags = false;
  api.config.historyCacheSize = 0;
  api.config.selfRequestsOnly = true;
}

initializeViewer();
