import { bindComparisonControl } from "./comparison-control.js";
import { bindDiffDivider } from "./diff-divider.js";
import { bindDiffOverview } from "./diff-overview.js";
import { bindDocumentSearch } from "./document-search.js";
import { bindDocumentTree } from "./document-tree.js";
import { bindThemeToggle } from "./theme-toggle.js";
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
  bindThemeToggle(environment.document, environment.window, environment.storage);
  const layout = environment.document.querySelector<HTMLElement>(".layout");
  if (!layout) return;

  configureHTMX(environment.htmx, environment.document);
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
  bindDiffOverview(environment);
  bindDocumentNavigationLifecycle(environment);
}

function bindDocumentNavigationLifecycle(environment: ViewerEnvironment): void {
  environment.document.addEventListener("htmx:afterSettle", (event) => {
    if (!documentSwapTarget(event)) return;
    bindComparisonControl(environment.document);
    bindDiffDivider(environment.document, environment.storage);
    bindDiffOverview(environment);
    environment.document.dispatchEvent(
      new CustomEvent("code-annotator:viewer-navigated"),
    );
  });
}

function documentSwapTarget(event: Event): boolean {
  if (!(event instanceof CustomEvent) || typeof event.detail !== "object" || event.detail === null) {
    return false;
  }
  const target = Reflect.get(event.detail, "target");
  return target instanceof HTMLElement && target.matches(".document");
}

// Harden HTMX before any component can issue a request.
function configureHTMX(api: HtmxAPI | null, document: Document): void {
  if (!api) return;
  api.config.allowEval = false;
  api.config.allowNestedOobSwaps = false;
  api.config.allowScriptTags = false;
  api.config.historyCacheSize = 0;
  api.config.includeIndicatorStyles = false;
  api.config.selfRequestsOnly = true;
  document.body.addEventListener("htmx:configRequest", (event) => {
    if (Reflect.get(globalThis, "mermaid") === undefined) return;
    if (!(event instanceof CustomEvent) || typeof event.detail !== "object" || event.detail === null) return;
    const headers = Reflect.get(event.detail, "headers");
    if (typeof headers === "object" && headers !== null) {
      Reflect.set(headers, "X-Code-Annotator-Mermaid", "true");
    }
  });
}

initializeViewer();
