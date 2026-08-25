import { filterDocuments } from "./document-catalog.js";
import type { DocumentScope } from "./document-catalog.js";
import { fetchDocumentCatalogState } from "./document-state.js";
import type { DocumentCatalogState, DocumentMode } from "./document-state.js";
import { clampInteger, resolveDocumentScope } from "./viewer-preferences.js";

interface HtmxConfig {
  allowEval: boolean;
  allowNestedOobSwaps: boolean;
  allowScriptTags: boolean;
  historyCacheSize: number;
  selfRequestsOnly: boolean;
}

interface HtmxAPI {
  config: HtmxConfig;
  ajax(
    verb: "GET",
    path: string,
    options: { target: string; swap: "outerHTML" },
  ): Promise<void>;
}

interface PanelToggleContext {
  button: HTMLButtonElement;
  panel: HTMLElement;
  layout: HTMLElement;
  storage: Storage;
  collapsedClass: string;
  name: string;
}

interface PanelToggleOptions
  extends Omit<PanelToggleContext, "button" | "panel"> {
  button: HTMLButtonElement | null;
  panel: HTMLElement | null;
  defaultCollapsed?: boolean;
}

interface DocumentPanelRequest {
  query: string;
  scope: DocumentScope;
}

interface DocumentSearchContext {
  document: Document;
  window: Window;
  location: Location;
  storage: Storage;
  htmx: HtmxAPI | null;
  path: string;
  mode: DocumentMode;
  state: DocumentCatalogState | null;
  scope: DocumentScope;
  searchTimer: number;
  requestRunning: boolean;
  queuedRequest: DocumentPanelRequest | null;
}

interface ComparisonControlContext {
  control: HTMLFormElement;
  status: HTMLElement;
  token: string;
}

interface DiffDividerContext {
  document: Document;
  storage: Storage;
  view: HTMLElement;
  divider: HTMLElement;
  percent: number;
  dragRect: DOMRect | null;
  pointerMoveHandler: ((event: PointerEvent) => void) | null;
}

export interface ViewerEnvironment {
  document: Document;
  window: Window;
  location: Location;
  storage: Storage;
  resizeObserver: typeof ResizeObserver;
  htmx: HtmxAPI | null;
}

const changedOnlyStorageKey = "code-annotator.changed-only";
const documentScopeStorageKey = "code-annotator.document-scope";
const documentTreeStorageKey = "code-annotator.document-tree-expanded";
const sourceModeStorageKey = "code-annotator.source-mode";
const diffSplitStorageKey = "code-annotator.diff-split";
const panelStoragePrefix = "code-annotator.panel-collapsed.";
const diffSplitMin = 20;
const diffSplitMax = 80;
const diffSplitStep = 2;

function browserHTMX(): HtmxAPI | null {
  return (Reflect.get(globalThis, "htmx") as HtmxAPI | undefined) ?? null;
}

function defaultViewerEnvironment(): ViewerEnvironment {
  return {
    document,
    window,
    location,
    storage: sessionStorage,
    resizeObserver: ResizeObserver,
    htmx: browserHTMX(),
  };
}

export function initializeViewer(
  environment: ViewerEnvironment = defaultViewerEnvironment(),
): void {
  const layout = environment.document.querySelector<HTMLElement>(".layout");
  if (!layout) return;
  configureHTMX(environment.htmx);
  bindTopbarHeight(environment.document, environment.resizeObserver);
  bindPanelToggle({
    button: environment.document.querySelector(".documents-toggle"),
    panel: environment.document.querySelector("#documents-sidebar"),
    layout,
    storage: environment.storage,
    collapsedClass: "documents-collapsed",
    name: "documents",
  });
  bindPanelToggle({
    button: environment.document.querySelector(".review-toggle"),
    panel: environment.document.querySelector("#annotation-sidebar"),
    layout,
    storage: environment.storage,
    collapsedClass: "review-collapsed",
    name: "annotations",
    defaultCollapsed: true,
  });
  bindSourceModePreference(environment.document, environment.storage);
  bindDocumentSearch(environment);
  bindComparisonControl(environment.document);
  bindDiffDivider(environment.document, environment.storage);
}

function configureHTMX(api: HtmxAPI | null): void {
  if (!api) return;
  api.config.allowEval = false;
  api.config.allowNestedOobSwaps = false;
  api.config.allowScriptTags = false;
  api.config.historyCacheSize = 0;
  api.config.selfRequestsOnly = true;
}

function bindTopbarHeight(
  document: Document,
  ResizeObserver: typeof globalThis.ResizeObserver,
): void {
  const topbar = document.querySelector<HTMLElement>(".topbar");
  if (!topbar) return;
  updateTopbarHeight(document, topbar);
  new ResizeObserver(updateTopbarHeight.bind(null, document, topbar)).observe(
    topbar,
  );
}

function updateTopbarHeight(document: Document, topbar: HTMLElement): void {
  document.documentElement.style.setProperty(
    "--topbar-height",
    `${topbar.getBoundingClientRect().height}px`,
  );
}

function bindPanelToggle(options: PanelToggleOptions): void {
  if (!options.button || !options.panel) return;
  const context: PanelToggleContext = {
    button: options.button,
    panel: options.panel,
    layout: options.layout,
    storage: options.storage,
    collapsedClass: options.collapsedClass,
    name: options.name,
  };
  setPanelCollapsed(
    context,
    readPanelCollapsedPreference(
      options.storage,
      options.name,
      options.defaultCollapsed ?? false,
    ),
  );
  options.button.addEventListener(
    "click",
    handlePanelToggle.bind(null, context),
  );
}

function handlePanelToggle(context: PanelToggleContext): void {
  const collapsed = !context.panel.hidden;
  setPanelCollapsed(context, collapsed);
  writePreference(
    context.storage,
    `${panelStoragePrefix}${context.name}`,
    String(collapsed),
  );
}

function setPanelCollapsed(
  context: PanelToggleContext,
  collapsed: boolean,
): void {
  context.panel.hidden = collapsed;
  context.layout.classList.toggle(context.collapsedClass, collapsed);
  context.button.setAttribute("aria-expanded", String(!collapsed));
  context.button.textContent = `${collapsed ? "Show" : "Hide"} ${context.name}`;
}

function bindSourceModePreference(document: Document, storage: Storage): void {
  const tabs = document.querySelector<HTMLElement>(".source-mode-tabs");
  const activeTab = tabs?.querySelector<HTMLAnchorElement>(
    'a[aria-current="page"]',
  );
  if (!tabs || !activeTab) return;
  persistSourceMode(storage, activeTab);
  for (const tab of tabs.querySelectorAll<HTMLAnchorElement>("a")) {
    tab.addEventListener("click", persistSourceMode.bind(null, storage, tab));
  }
}

function persistSourceMode(storage: Storage, tab: HTMLAnchorElement): void {
  writePreference(
    storage,
    sourceModeStorageKey,
    new URL(tab.href).searchParams.get("mode") === "diff" ? "diff" : "file",
  );
}

function bindDocumentSearch(environment: ViewerEnvironment): void {
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
  void refreshDocumentCatalog(context, true);
  environment.document.addEventListener(
    "change",
    handleDocumentScopeChange.bind(null, context),
    true,
  );
  environment.document.addEventListener(
    "input",
    handleDocumentSearchInput.bind(null, context),
    true,
  );
  environment.document.addEventListener(
    "search",
    handleDocumentSearch.bind(null, context),
    true,
  );
  environment.document.addEventListener(
    "click",
    handleDocumentDirectoryClick.bind(null, context),
  );
  environment.document.addEventListener(
    "htmx:afterSwap",
    restoreExpandedDirectories.bind(null, context),
  );
  environment.document.addEventListener(
    "code-annotator:annotations-updated",
    handleAnnotationsUpdated.bind(null, context),
  );
  environment.document.addEventListener(
    "keydown",
    handleDocumentSearchKeydown.bind(null, context),
  );
  environment.document.addEventListener(
    "keydown",
    handleDocumentShortcutKeydown.bind(null, context),
  );
  restoreExpandedDirectories(context);
}

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
    const checkbox = context.document.querySelector<HTMLInputElement>(
      `.document-filter-form input[value="${context.scope}"]`,
    );
    if (context.scope !== "all" && checkbox && !checkbox.checked) {
      checkbox.checked = true;
      requestDocumentPanel(context, "", context.scope);
    }
  } catch (_) {
    // The server-rendered catalog remains usable if typed state is unavailable.
  }
}

function handleDocumentScopeChange(
  context: DocumentSearchContext,
  event: Event,
): void {
  const input = event.target;
  if (
    !(input instanceof HTMLInputElement) ||
    input.form?.classList.contains("document-filter-form") !== true ||
    input.name !== "scope"
  )
    return;
  context.scope =
    input.checked && isFilteredDocumentScope(input.value) ? input.value : "all";
  for (const candidate of input.form.querySelectorAll<HTMLInputElement>(
    'input[name="scope"]',
  )) {
    if (candidate !== input) candidate.checked = false;
  }
  writePreference(context.storage, documentScopeStorageKey, context.scope);
  requestDocumentPanel(
    context,
    input.form.querySelector<HTMLInputElement>("#document-search-input")
      ?.value ?? "",
    context.scope,
  );
}

function isFilteredDocumentScope(
  value: string,
): value is Exclude<DocumentScope, "all"> {
  return value === "changed" || value === "open-comments";
}

function handleDocumentSearchInput(
  context: DocumentSearchContext,
  event: Event,
): void {
  const input = event.target;
  if (
    !(input instanceof HTMLInputElement) ||
    input.id !== "document-search-input"
  )
    return;
  context.window.clearTimeout(context.searchTimer);
  context.searchTimer = context.window.setTimeout(
    requestDocumentPanel.bind(null, context, input.value, context.scope),
    150,
  );
}

function handleDocumentSearch(
  context: DocumentSearchContext,
  event: Event,
): void {
  const input = event.target;
  if (
    !(input instanceof HTMLInputElement) ||
    input.id !== "document-search-input"
  )
    return;
  context.window.clearTimeout(context.searchTimer);
  requestDocumentPanel(context, input.value, context.scope);
}

function handleDocumentDirectoryClick(
  context: DocumentSearchContext,
  event: MouseEvent,
): void {
  const button =
    event.target instanceof Element
      ? event.target.closest<HTMLButtonElement>(".document-directory-toggle")
      : null;
  const item = button?.closest<HTMLLIElement>(".document-directory");
  if (!button || !item) return;
  const expanded = button.getAttribute("aria-expanded") !== "true";
  button.setAttribute("aria-expanded", String(expanded));
  item.classList.toggle("collapsed", !expanded);
  writeExpandedDirectories(context);
}

function restoreExpandedDirectories(context: DocumentSearchContext): void {
  const stored = readPreference(context.storage, documentTreeStorageKey);
  if (stored === null) return;
  const expanded = readStringSet(stored);
  for (const item of context.document.querySelectorAll<HTMLLIElement>(
    ".document-directory[id]",
  )) {
    const isExpanded = expanded.has(item.id);
    item.classList.toggle("collapsed", !isExpanded);
    item
      .querySelector<HTMLButtonElement>(":scope > .document-directory-toggle")
      ?.setAttribute("aria-expanded", String(isExpanded));
  }
}

function writeExpandedDirectories(context: DocumentSearchContext): void {
  const expanded: string[] = [];
  for (const item of context.document.querySelectorAll<HTMLLIElement>(
    ".document-directory[id]",
  )) {
    if (!item.classList.contains("collapsed")) expanded.push(item.id);
  }
  expanded.sort();
  writePreference(
    context.storage,
    documentTreeStorageKey,
    JSON.stringify(expanded),
  );
}

function readStringSet(value: string): Set<string> {
  try {
    const parsed: unknown = JSON.parse(value);
    return new Set(Array.isArray(parsed) ? parsed.filter(isString) : []);
  } catch (_) {
    return new Set();
  }
}

function isString(value: unknown): value is string {
  return typeof value === "string";
}

function handleAnnotationsUpdated(context: DocumentSearchContext): void {
  void refreshDocumentCatalog(context, false).then(
    dispatchDocumentSearch.bind(null, context),
  );
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
  )
    return;
  event.preventDefault();
  context.document
    .querySelector<HTMLInputElement>("#document-search-input")
    ?.focus();
}

function requestDocumentPanel(
  context: DocumentSearchContext,
  query: string,
  scope: DocumentScope,
): void {
  context.queuedRequest = { query, scope };
  if (!context.requestRunning) void sendNextDocumentRequest(context);
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
    if (context.queuedRequest) void sendNextDocumentRequest(context);
  }
}

function bindComparisonControl(document: Document): void {
  const control = document.querySelector<HTMLFormElement>(
    ".diff-comparison-control",
  );
  const token =
    document.querySelector<HTMLMetaElement>(
      'meta[name="code-annotator-comparison-token"]',
    )?.content ?? "";
  const selector =
    control?.querySelector<HTMLSelectElement>(".revision-selector");
  const status = control?.querySelector<HTMLElement>(".diff-comparison-status");
  if (!control || !token || !selector || !status) return;
  const context: ComparisonControlContext = { control, status, token };
  selector.addEventListener(
    "change",
    handleComparisonChange.bind(null, context),
  );
  document.body.addEventListener(
    "htmx:configRequest",
    handleComparisonConfigRequest.bind(null, context),
  );
  document.body.addEventListener(
    "htmx:responseError",
    handleComparisonResponseError.bind(null, context),
  );
}

function handleComparisonChange(context: ComparisonControlContext): void {
  context.status.textContent = "Updating comparison base…";
  context.status.classList.remove("error");
  context.control.requestSubmit();
}

function handleComparisonConfigRequest(
  context: ComparisonControlContext,
  event: Event,
): void {
  const detail = customEventDetail(event);
  if (!detail || !comparisonEventTargetsControl(detail, context.control))
    return;
  const headers = Reflect.get(detail, "headers");
  if (typeof headers === "object" && headers !== null)
    Reflect.set(headers, "X-Code-Annotator-Comparison-Token", context.token);
}

function handleComparisonResponseError(
  context: ComparisonControlContext,
  event: Event,
): void {
  const detail = customEventDetail(event);
  if (!detail || !comparisonEventTargetsControl(detail, context.control))
    return;
  context.status.textContent = "The Git comparison could not be updated.";
  context.status.classList.add("error");
}

function customEventDetail(event: Event): object | null {
  return event instanceof CustomEvent &&
    typeof event.detail === "object" &&
    event.detail !== null
    ? event.detail
    : null;
}

function comparisonEventTargetsControl(
  detail: object,
  control: HTMLFormElement,
): boolean {
  const source = Reflect.get(detail, "elt");
  return (
    source instanceof Element &&
    source.closest(".diff-comparison-control") === control
  );
}

function bindDiffDivider(document: Document, storage: Storage): void {
  const view = document.querySelector<HTMLElement>(".diff-view");
  const divider = view?.querySelector<HTMLElement>(".diff-divider");
  if (!view || !divider) return;
  const context: DiffDividerContext = {
    document,
    storage,
    view,
    divider,
    percent: clampDiffSplit(readDiffSplitPreference(storage)),
    dragRect: null,
    pointerMoveHandler: null,
  };
  context.pointerMoveHandler = handleDiffPointerMove.bind(null, context);
  renderDiffSplit(context);
  divider.addEventListener(
    "keydown",
    handleDiffDividerKeydown.bind(null, context),
  );
  divider.addEventListener(
    "pointerdown",
    handleDiffPointerDown.bind(null, context),
  );
}

function handleDiffDividerKeydown(
  context: DiffDividerContext,
  event: KeyboardEvent,
): void {
  let next: number;
  if (event.key === "ArrowLeft") next = context.percent - diffSplitStep;
  else if (event.key === "ArrowRight") next = context.percent + diffSplitStep;
  else if (event.key === "Home") next = diffSplitMin;
  else if (event.key === "End") next = diffSplitMax;
  else return;
  event.preventDefault();
  setDiffSplit(context, next);
}

function handleDiffPointerDown(
  context: DiffDividerContext,
  event: PointerEvent,
): void {
  if (event.button !== 0) return;
  event.preventDefault();
  context.dragRect = context.view.getBoundingClientRect();
  if (context.pointerMoveHandler)
    context.document.addEventListener(
      "pointermove",
      context.pointerMoveHandler,
    );
  context.document.addEventListener(
    "pointerup",
    handleDiffPointerUp.bind(null, context),
    { once: true },
  );
}

function handleDiffPointerMove(
  context: DiffDividerContext,
  event: PointerEvent,
): void {
  if (context.dragRect)
    setDiffSplit(
      context,
      ((event.clientX - context.dragRect.left) / context.dragRect.width) * 100,
    );
}

function handleDiffPointerUp(context: DiffDividerContext): void {
  context.dragRect = null;
  if (context.pointerMoveHandler)
    context.document.removeEventListener(
      "pointermove",
      context.pointerMoveHandler,
    );
}

function setDiffSplit(context: DiffDividerContext, value: number): void {
  context.percent = clampDiffSplit(value);
  renderDiffSplit(context);
  writePreference(
    context.storage,
    diffSplitStorageKey,
    String(context.percent),
  );
}

function renderDiffSplit(context: DiffDividerContext): void {
  context.view.style.setProperty("--diff-split", `${context.percent}%`);
  context.divider.setAttribute("aria-valuenow", String(context.percent));
}

function clampDiffSplit(value: number): number {
  return clampInteger(value, diffSplitMin, diffSplitMax);
}

function readDiffSplitPreference(storage: Storage): number {
  const stored = Number.parseFloat(
    readPreference(storage, diffSplitStorageKey) ?? "",
  );
  return Number.isFinite(stored) ? stored : 50;
}

function readPanelCollapsedPreference(
  storage: Storage,
  name: string,
  defaultCollapsed: boolean,
): boolean {
  const stored = readPreference(storage, `${panelStoragePrefix}${name}`);
  return stored === null ? defaultCollapsed : stored === "true";
}

function readPreference(storage: Storage, key: string): string | null {
  try {
    return storage.getItem(key);
  } catch (_) {
    return null;
  }
}

function writePreference(storage: Storage, key: string, value: string): void {
  try {
    storage.setItem(key, value);
  } catch (_) {
    // The current-page interaction still works when storage is unavailable.
  }
}

initializeViewer();
