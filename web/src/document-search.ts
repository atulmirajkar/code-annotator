import { readPreference, writePreference } from "./browser-storage.js";
import { filterDocuments } from "./document-catalog.js";
import type { DocumentScope } from "./document-catalog.js";
import { fetchDocumentCatalogState } from "./document-state.js";
import type { DocumentCatalogState, DocumentMode } from "./document-state.js";
import type { ViewerEnvironment } from "./viewer-environment.js";
import { resolveDocumentScope } from "./viewer-preferences.js";

interface DocumentPanelRequest {
  query: string;
  scope: DocumentScope;
}

interface DocumentSearchContext {
  document: Document;
  window: Window;
  location: Location;
  storage: Storage;
  htmx: ViewerEnvironment["htmx"];
  path: string;
  mode: DocumentMode;
  // Typed catalog state supplies keyboard navigation order. Rendered list
  // elements are never inspected to reconstruct document data.
  state: DocumentCatalogState | null;
  scope: DocumentScope;
  searchTimer: number;
  requestRunning: boolean;
  queuedRequest: DocumentPanelRequest | null;
}

const changedOnlyStorageKey = "code-annotator.changed-only";
const documentScopeStorageKey = "code-annotator.document-scope";

// The server owns catalog rendering and filtering. This adapter owns only
// typed navigation state, preferences, keyboard behavior, and request timing.
export function bindDocumentSearch(environment: ViewerEnvironment): void {
  if (!environment.document.querySelector("#document-panel-content")) return;
  const context: DocumentSearchContext = {
    document: environment.document,
    window: environment.window,
    location: environment.location,
    storage: environment.storage,
    htmx: environment.htmx,
    path: decodeURIComponent(
      environment.location.pathname.startsWith("/view/")
        ? environment.location.pathname.slice(6)
        : "",
    ),
    mode:
      new URL(environment.location.href).searchParams.get("mode") === "diff"
        ? "diff"
        : "file",
    state: null,
    scope: "all",
    searchTimer: 0,
    requestRunning: false,
    queuedRequest: null,
  };

  refreshDocumentCatalog(context, true);
  environment.document.addEventListener(
    "change",
    (event) => handleDocumentScopeChange(context, event),
    true,
  );
  environment.document.addEventListener(
    "input",
    (event) => handleDocumentSearchInput(context, event),
    true,
  );
  environment.document.addEventListener(
    "search",
    (event) => handleDocumentSearch(context, event),
    true,
  );
  environment.document.addEventListener(
    "code-annotator:annotations-updated",
    () => handleAnnotationsUpdated(context),
  );
  environment.document.addEventListener("keydown", (event) =>
    handleDocumentSearchKeydown(context, event),
  );
  environment.document.addEventListener("keydown", (event) =>
    handleDocumentShortcutKeydown(context, event),
  );
  environment.document.addEventListener(
    "code-annotator:viewer-navigated",
    () => handleViewerNavigation(context),
  );
}

function handleViewerNavigation(context: DocumentSearchContext): void {
  context.path = decodeURIComponent(
    context.location.pathname.startsWith("/view/")
      ? context.location.pathname.slice(6)
      : "",
  );
  context.mode = new URL(context.location.href).searchParams.get("mode") === "diff"
    ? "diff"
    : "file";
  synchronizeDocumentLinks(context);
  void refreshDocumentCatalog(context, false);
}

// The document sidebar survives an HTMX document swap, so its existing links
// still describe the prior page's mode and selection. Rewrite every href to
// retain the active File/Changes mode, move aria-current to the new document,
// and reprocess the changed anchor so HTMX refreshes its boosted-link metadata.
function synchronizeDocumentLinks(context: DocumentSearchContext): void {
  for (const link of context.document.querySelectorAll<HTMLAnchorElement>(
    ".documents .document-file a",
  )) {
    const url = new URL(link.href);
    if (context.mode === "diff") url.searchParams.set("mode", "diff");
    else url.searchParams.delete("mode");
    const href = `${url.pathname}${url.search}`;
    const selected = decodeURIComponent(url.pathname.slice("/view/".length)) === context.path;
    link.href = href;
    link.classList.toggle("selected", selected);
    if (selected) link.setAttribute("aria-current", "page");
    else link.removeAttribute("aria-current");
    context.htmx?.process?.(link);
  }
}

// Runtime validation in document-state.ts keeps unknown server payloads out of
// the context. The rendered catalog remains usable if this fetch fails.
async function refreshDocumentCatalog(
  context: DocumentSearchContext,
  restoreScope: boolean,
): Promise<void> {
  try {
    const catalog = await fetchDocumentCatalogState(context.path, context.mode);
    context.state = catalog;
    if (!restoreScope) return;
    context.scope = resolveDocumentScope(
      readPreference(context.storage, documentScopeStorageKey),
      readPreference(context.storage, changedOnlyStorageKey),
      catalog.documents,
    );
    const inputs = context.document.querySelectorAll<HTMLInputElement>(
      '.document-filter-form input[name="scope"]',
    );
    const renderedScope = Array.from(inputs).find((input) => input.checked)
      ?.value ?? "all";
    if (context.scope !== renderedScope) {
      // HTMX preserves this sidebar across document navigation, so its radio
      // state can intentionally differ from the freshly fetched server default
      // (normally Changed only). Reapply the tab's saved scope and refresh the
      // panel only when those states differ; otherwise every navigation would
      // visibly reset the filter before restoring it.
      for (const input of inputs) input.checked = input.value === context.scope;
      await requestDocumentPanel(context, "", context.scope);
    }
  } catch (_) {
    // Server-rendered links and filters still work without typed state.
  } finally {
    if (restoreScope) {
      context.document
        .querySelector(".layout")
        ?.classList.remove("document-scope-restoring");
    }
  }
}

// Checkboxes allow an active scope to be toggled back to "all", while this
// handler ensures that no two filtered scopes remain active together.
function handleDocumentScopeChange(
  context: DocumentSearchContext,
  event: Event,
): void {
  const input = event.target;
  if (
    !(input instanceof HTMLInputElement) ||
    input.form?.classList.contains("document-filter-form") !== true ||
    input.name !== "scope"
  ) {
    return;
  }
  context.scope =
    input.checked && isFilteredDocumentScope(input.value) ? input.value : "all";
  for (const candidate of input.form.querySelectorAll<HTMLInputElement>(
    'input[name="scope"]',
  )) {
    if (candidate !== input) candidate.checked = false;
  }
  writePreference(context.storage, documentScopeStorageKey, context.scope);
  const query =
    input.form.querySelector<HTMLInputElement>("#document-search-input")
      ?.value ?? "";
  requestDocumentPanel(context, query, context.scope);
}

function isFilteredDocumentScope(
  value: string,
): value is Exclude<DocumentScope, "all"> {
  return value === "changed" || value === "open-comments";
}

// Events are delegated because HTMX replaces the input along with the panel.
function handleDocumentSearchInput(
  context: DocumentSearchContext,
  event: Event,
): void {
  const input = documentSearchInput(event);
  if (!input) return;
  context.window.clearTimeout(context.searchTimer);
  context.searchTimer = context.window.setTimeout(
    () => requestDocumentPanel(context, input.value, context.scope),
    150,
  );
}

function handleDocumentSearch(
  context: DocumentSearchContext,
  event: Event,
): void {
  const input = documentSearchInput(event);
  if (!input) return;
  context.window.clearTimeout(context.searchTimer);
  requestDocumentPanel(context, input.value, context.scope);
}

function documentSearchInput(event: Event): HTMLInputElement | null {
  const input = event.target;
  return input instanceof HTMLInputElement &&
    input.id === "document-search-input"
    ? input
    : null;
}

// Annotation changes can alter open-comment counts and scope membership.
async function handleAnnotationsUpdated(
  context: DocumentSearchContext,
): Promise<void> {
  await refreshDocumentCatalog(context, false);
  dispatchDocumentSearch(context);
}

function dispatchDocumentSearch(context: DocumentSearchContext): void {
  context.document
    .querySelector<HTMLInputElement>("#document-search-input")
    ?.dispatchEvent(new Event("search", { bubbles: true }));
}

function handleDocumentSearchKeydown(
  context: DocumentSearchContext,
  event: KeyboardEvent,
): void {
  const input = context.document.querySelector<HTMLInputElement>(
    "#document-search-input",
  );
  if (!input || event.target !== input) return;
  if (event.key === "Escape") {
    event.preventDefault();
    input.value = "";
    input.dispatchEvent(new Event("search", { bubbles: true }));
  } else if (event.key === "Enter") {
    event.preventDefault();
    const destination = context.state
      ? filterDocuments(context.state.documents, input.value, context.scope)
          .documents[0]
      : undefined;
    if (destination) context.location.assign(destination.url);
  } else if (event.key === "ArrowDown") {
    event.preventDefault();
    context.document
      .querySelector<HTMLAnchorElement>(".documents .document-file a")
      ?.focus();
  }
}

function handleDocumentShortcutKeydown(
  context: DocumentSearchContext,
  event: KeyboardEvent,
): void {
  const target = event.target;
  const editing =
    target instanceof HTMLInputElement ||
    target instanceof HTMLTextAreaElement ||
    target instanceof HTMLSelectElement ||
    (target instanceof HTMLElement && target.isContentEditable);
  if (
    event.key !== "/" ||
    editing ||
    event.metaKey ||
    event.ctrlKey ||
    event.altKey
  ) {
    return;
  }
  event.preventDefault();
  context.document
    .querySelector<HTMLInputElement>("#document-search-input")
    ?.focus();
}

// Keep only the newest request while an earlier HTMX swap is in flight.
function requestDocumentPanel(
  context: DocumentSearchContext,
  query: string,
  scope: DocumentScope,
): Promise<void> {
  context.queuedRequest = { query, scope };
  if (!context.requestRunning) return sendNextDocumentRequest(context);
  return Promise.resolve();
}

async function sendNextDocumentRequest(
  context: DocumentSearchContext,
): Promise<void> {
  const request = context.queuedRequest;
  if (!context.htmx || !request) return;
  context.queuedRequest = null;
  context.requestRunning = true;
  const parameters = new URLSearchParams({
    document: context.path,
    mode: context.mode,
  });
  if (request.query) parameters.set("query", request.query);
  if (request.scope !== "all") parameters.set("scope", request.scope);
  try {
    await context.htmx.ajax("GET", `/ui/review/documents?${parameters}`, {
      target: "#document-panel-content",
      swap: "outerHTML",
    });
  } finally {
    context.requestRunning = false;
    if (context.queuedRequest) sendNextDocumentRequest(context);
  }
}
