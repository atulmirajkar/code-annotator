import { filterDocuments } from "./document-catalog.js";
import { fetchDocumentCatalogState } from "./document-state.js";
import { clampInteger, resolveDocumentScope } from "./viewer-preferences.js";
// All preferences are tab-scoped through the injected Storage instance.
const changedOnlyStorageKey = "code-annotator.changed-only";
const documentScopeStorageKey = "code-annotator.document-scope";
const documentTreeStorageKey = "code-annotator.document-tree-expanded";
const sourceModeStorageKey = "code-annotator.source-mode";
const diffSplitStorageKey = "code-annotator.diff-split";
const panelStoragePrefix = "code-annotator.panel-collapsed.";
const diffSplitMin = 20;
const diffSplitMax = 80;
const diffSplitStep = 2;
// Production initialization reads the optional HTMX global once, then passes
// the resulting port through the same path used by tests.
function browserHTMX() {
    return Reflect.get(globalThis, "htmx") ?? null;
}
function defaultViewerEnvironment() {
    return {
        document,
        window,
        location,
        storage: sessionStorage,
        resizeObserver: ResizeObserver,
        htmx: browserHTMX(),
    };
}
// initializeViewer is intentionally limited to element lookup and dependency
// wiring. Interaction behavior belongs to the module-level helpers below.
export function initializeViewer(environment = defaultViewerEnvironment()) {
    const layout = environment.document.querySelector(".layout");
    if (!layout)
        return;
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
// Harden the HTMX defaults before any viewer interaction can issue a request.
function configureHTMX(api) {
    if (!api)
        return;
    api.config.allowEval = false;
    api.config.allowNestedOobSwaps = false;
    api.config.allowScriptTags = false;
    api.config.historyCacheSize = 0;
    api.config.selfRequestsOnly = true;
}
// CSS uses the measured topbar height to keep sticky content below a topbar
// whose height can change with viewport width or wrapped controls.
function bindTopbarHeight(document, ResizeObserver) {
    const topbar = document.querySelector(".topbar");
    if (!topbar)
        return;
    updateTopbarHeight(document, topbar);
    new ResizeObserver(updateTopbarHeight.bind(null, document, topbar)).observe(topbar);
}
function updateTopbarHeight(document, topbar) {
    document.documentElement.style.setProperty("--topbar-height", `${topbar.getBoundingClientRect().height}px`);
}
// One panel context keeps the DOM's hidden state, layout class, accessible
// state, button label, and stored preference synchronized.
function bindPanelToggle(options) {
    if (!options.button || !options.panel)
        return;
    const context = {
        button: options.button,
        panel: options.panel,
        layout: options.layout,
        storage: options.storage,
        collapsedClass: options.collapsedClass,
        name: options.name,
    };
    setPanelCollapsed(context, readPanelCollapsedPreference(options.storage, options.name, options.defaultCollapsed ?? false));
    options.button.addEventListener("click", handlePanelToggle.bind(null, context));
}
function handlePanelToggle(context) {
    const collapsed = !context.panel.hidden;
    setPanelCollapsed(context, collapsed);
    writePreference(context.storage, `${panelStoragePrefix}${context.name}`, String(collapsed));
}
function setPanelCollapsed(context, collapsed) {
    context.panel.hidden = collapsed;
    context.layout.classList.toggle(context.collapsedClass, collapsed);
    context.button.setAttribute("aria-expanded", String(!collapsed));
    context.button.textContent = `${collapsed ? "Show" : "Hide"} ${context.name}`;
}
// Source links are already rendered in the correct mode by Go. The browser
// remembers only an explicit user choice for subsequent navigation.
function bindSourceModePreference(document, storage) {
    const tabs = document.querySelector(".source-mode-tabs");
    const activeTab = tabs?.querySelector('a[aria-current="page"]');
    if (!tabs || !activeTab)
        return;
    persistSourceMode(storage, activeTab);
    for (const tab of tabs.querySelectorAll("a")) {
        tab.addEventListener("click", persistSourceMode.bind(null, storage, tab));
    }
}
function persistSourceMode(storage, tab) {
    writePreference(storage, sourceModeStorageKey, new URL(tab.href).searchParams.get("mode") === "diff" ? "diff" : "file");
}
// The server owns catalog rendering and filtering. This adapter adds tab-local
// preferences, keyboard behavior, debouncing, and HTMX fragment replacement.
function bindDocumentSearch(environment) {
    if (!environment.document.querySelector("#document-panel-content"))
        return;
    const context = {
        document: environment.document,
        window: environment.window,
        location: environment.location,
        storage: environment.storage,
        htmx: environment.htmx,
        path: decodeURIComponent(environment.location.pathname.startsWith("/view/")
            ? environment.location.pathname.slice(6)
            : ""),
        mode: new URL(environment.location.href).searchParams.get("mode") === "diff"
            ? "diff"
            : "file",
        state: null,
        scope: "all",
        searchTimer: 0,
        requestRunning: false,
        queuedRequest: null,
    };
    void refreshDocumentCatalog(context, true);
    environment.document.addEventListener("change", handleDocumentScopeChange.bind(null, context), true);
    environment.document.addEventListener("input", handleDocumentSearchInput.bind(null, context), true);
    environment.document.addEventListener("search", handleDocumentSearch.bind(null, context), true);
    environment.document.addEventListener("click", handleDocumentDirectoryClick.bind(null, context));
    environment.document.addEventListener("htmx:afterSwap", restoreExpandedDirectories.bind(null, context));
    environment.document.addEventListener("code-annotator:annotations-updated", handleAnnotationsUpdated.bind(null, context));
    environment.document.addEventListener("keydown", handleDocumentSearchKeydown.bind(null, context));
    environment.document.addEventListener("keydown", handleDocumentShortcutKeydown.bind(null, context));
    restoreExpandedDirectories(context);
}
// Catalog state is fetched separately from rendered HTML and runtime-validated
// by document-state.ts before it reaches this module.
async function refreshDocumentCatalog(context, restoreScope) {
    try {
        const catalog = await fetchDocumentCatalogState(context.path, context.mode);
        context.state = catalog;
        if (!restoreScope)
            return;
        context.scope = resolveDocumentScope(readPreference(context.storage, documentScopeStorageKey), readPreference(context.storage, changedOnlyStorageKey), catalog.documents);
        const checkbox = context.document.querySelector(`.document-filter-form input[value="${context.scope}"]`);
        if (context.scope !== "all" && checkbox && !checkbox.checked) {
            checkbox.checked = true;
            requestDocumentPanel(context, "", context.scope);
        }
    }
    catch (_) {
        // The server-rendered catalog remains usable if typed state is unavailable.
    }
}
// Scope checkboxes behave as an exclusive filter even though the server sends
// checkboxes to allow an active option to be toggled back to "all".
function handleDocumentScopeChange(context, event) {
    const input = event.target;
    if (!(input instanceof HTMLInputElement) ||
        input.form?.classList.contains("document-filter-form") !== true ||
        input.name !== "scope")
        return;
    context.scope =
        input.checked && isFilteredDocumentScope(input.value) ? input.value : "all";
    for (const candidate of input.form.querySelectorAll('input[name="scope"]')) {
        if (candidate !== input)
            candidate.checked = false;
    }
    writePreference(context.storage, documentScopeStorageKey, context.scope);
    requestDocumentPanel(context, input.form.querySelector("#document-search-input")
        ?.value ?? "", context.scope);
}
function isFilteredDocumentScope(value) {
    return value === "changed" || value === "open-comments";
}
// Input events are delegated because HTMX replaces the complete panel,
// including the search input, after each server-rendered result.
function handleDocumentSearchInput(context, event) {
    const input = event.target;
    if (!(input instanceof HTMLInputElement) ||
        input.id !== "document-search-input")
        return;
    context.window.clearTimeout(context.searchTimer);
    context.searchTimer = context.window.setTimeout(requestDocumentPanel.bind(null, context, input.value, context.scope), 150);
}
function handleDocumentSearch(context, event) {
    const input = event.target;
    if (!(input instanceof HTMLInputElement) ||
        input.id !== "document-search-input")
        return;
    context.window.clearTimeout(context.searchTimer);
    requestDocumentPanel(context, input.value, context.scope);
}
// Directory expansion is presentation state keyed by semantic element IDs;
// document identity and filtering continue to come from typed catalog state.
function handleDocumentDirectoryClick(context, event) {
    const button = event.target instanceof Element
        ? event.target.closest(".document-directory-toggle")
        : null;
    const item = button?.closest(".document-directory");
    if (!button || !item)
        return;
    const expanded = button.getAttribute("aria-expanded") !== "true";
    button.setAttribute("aria-expanded", String(expanded));
    item.classList.toggle("collapsed", !expanded);
    writeExpandedDirectories(context);
}
// Reapply expansion after every HTMX swap because the replacement fragment is
// authoritative server HTML and starts with its default tree state.
function restoreExpandedDirectories(context) {
    const stored = readPreference(context.storage, documentTreeStorageKey);
    if (stored === null)
        return;
    const expanded = readStringSet(stored);
    for (const item of context.document.querySelectorAll(".document-directory[id]")) {
        const isExpanded = expanded.has(item.id);
        item.classList.toggle("collapsed", !isExpanded);
        item
            .querySelector(":scope > .document-directory-toggle")
            ?.setAttribute("aria-expanded", String(isExpanded));
    }
}
function writeExpandedDirectories(context) {
    const expanded = [];
    for (const item of context.document.querySelectorAll(".document-directory[id]")) {
        if (!item.classList.contains("collapsed"))
            expanded.push(item.id);
    }
    expanded.sort();
    writePreference(context.storage, documentTreeStorageKey, JSON.stringify(expanded));
}
function readStringSet(value) {
    try {
        const parsed = JSON.parse(value);
        return new Set(Array.isArray(parsed) ? parsed.filter(isString) : []);
    }
    catch (_) {
        return new Set();
    }
}
function isString(value) {
    return typeof value === "string";
}
// Annotation mutations can change open-comment counts and scope membership, so
// refresh typed state before asking the server to rerender the active filter.
function handleAnnotationsUpdated(context) {
    void refreshDocumentCatalog(context, false).then(dispatchDocumentSearch.bind(null, context));
}
function dispatchDocumentSearch(context) {
    context.document
        .querySelector("#document-search-input")
        ?.dispatchEvent(new Event("search", { bubbles: true }));
}
// Search-field keys use typed catalog order for navigation and DOM references
// only for focus management and dispatching the normal search interaction.
function handleDocumentSearchKeydown(context, event) {
    const input = context.document.querySelector("#document-search-input");
    if (!input || event.target !== input)
        return;
    if (event.key === "Escape") {
        event.preventDefault();
        input.value = "";
        input.dispatchEvent(new Event("search", { bubbles: true }));
    }
    else if (event.key === "Enter") {
        event.preventDefault();
        const destination = context.state
            ? filterDocuments(context.state.documents, input.value, context.scope)
                .documents[0]
            : undefined;
        if (destination)
            context.location.assign(destination.url);
    }
    else if (event.key === "ArrowDown") {
        event.preventDefault();
        context.document
            .querySelector(".documents .document-file a")
            ?.focus();
    }
}
function handleDocumentShortcutKeydown(context, event) {
    const target = event.target;
    const editing = target instanceof HTMLInputElement ||
        target instanceof HTMLTextAreaElement ||
        target instanceof HTMLSelectElement ||
        (target instanceof HTMLElement && target.isContentEditable);
    if (event.key !== "/" ||
        editing ||
        event.metaKey ||
        event.ctrlKey ||
        event.altKey)
        return;
    event.preventDefault();
    context.document
        .querySelector("#document-search-input")
        ?.focus();
}
// Keep only the newest requested query while a prior HTMX request is running.
// sendNextDocumentRequest drains that single-slot queue after each response.
function requestDocumentPanel(context, query, scope) {
    context.queuedRequest = { query, scope };
    if (!context.requestRunning)
        void sendNextDocumentRequest(context);
}
async function sendNextDocumentRequest(context) {
    const request = context.queuedRequest;
    if (!context.htmx || !request)
        return;
    context.queuedRequest = null;
    context.requestRunning = true;
    const parameters = new URLSearchParams({
        document: context.path,
        mode: context.mode,
    });
    if (request.query)
        parameters.set("query", request.query);
    if (request.scope !== "all")
        parameters.set("scope", request.scope);
    try {
        await context.htmx.ajax("GET", `/ui/review/documents?${parameters}`, {
            target: "#document-panel-content",
            swap: "outerHTML",
        });
    }
    finally {
        context.requestRunning = false;
        if (context.queuedRequest)
            void sendNextDocumentRequest(context);
    }
}
// Comparison choices are server-rendered and server-validated. The browser is
// responsible only for submission feedback and the loopback request token.
function bindComparisonControl(document) {
    const control = document.querySelector(".diff-comparison-control");
    const token = document.querySelector('meta[name="code-annotator-comparison-token"]')?.content ?? "";
    const selector = control?.querySelector(".revision-selector");
    const status = control?.querySelector(".diff-comparison-status");
    if (!control || !token || !selector || !status)
        return;
    const context = { control, status, token };
    selector.addEventListener("change", handleComparisonChange.bind(null, context));
    document.body.addEventListener("htmx:configRequest", handleComparisonConfigRequest.bind(null, context));
    document.body.addEventListener("htmx:responseError", handleComparisonResponseError.bind(null, context));
}
function handleComparisonChange(context) {
    context.status.textContent = "Updating comparison base…";
    context.status.classList.remove("error");
    context.control.requestSubmit();
}
function handleComparisonConfigRequest(context, event) {
    const detail = customEventDetail(event);
    if (!detail || !comparisonEventTargetsControl(detail, context.control))
        return;
    const headers = Reflect.get(detail, "headers");
    if (typeof headers === "object" && headers !== null)
        Reflect.set(headers, "X-Code-Annotator-Comparison-Token", context.token);
}
function handleComparisonResponseError(context, event) {
    const detail = customEventDetail(event);
    if (!detail || !comparisonEventTargetsControl(detail, context.control))
        return;
    context.status.textContent = "The Git comparison could not be updated.";
    context.status.classList.add("error");
}
// HTMX event detail is untyped external input, so narrow it before reading the
// request source or headers object.
function customEventDetail(event) {
    return event instanceof CustomEvent &&
        typeof event.detail === "object" &&
        event.detail !== null
        ? event.detail
        : null;
}
function comparisonEventTargetsControl(detail, control) {
    const source = Reflect.get(detail, "elt");
    return (source instanceof Element &&
        source.closest(".diff-comparison-control") === control);
}
// The divider context owns one drag gesture and the persisted split. Both the
// headings and panes consume the same CSS custom property set by render.
function bindDiffDivider(document, storage) {
    const view = document.querySelector(".diff-view");
    const divider = view?.querySelector(".diff-divider");
    if (!view || !divider)
        return;
    const context = {
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
    divider.addEventListener("keydown", handleDiffDividerKeydown.bind(null, context));
    divider.addEventListener("pointerdown", handleDiffPointerDown.bind(null, context));
}
function handleDiffDividerKeydown(context, event) {
    let next;
    if (event.key === "ArrowLeft")
        next = context.percent - diffSplitStep;
    else if (event.key === "ArrowRight")
        next = context.percent + diffSplitStep;
    else if (event.key === "Home")
        next = diffSplitMin;
    else if (event.key === "End")
        next = diffSplitMax;
    else
        return;
    event.preventDefault();
    setDiffSplit(context, next);
}
// Capture the view bounds once per gesture. A stable bound move handler is
// stored in the context so pointerup can reliably unregister it.
function handleDiffPointerDown(context, event) {
    if (event.button !== 0)
        return;
    event.preventDefault();
    context.dragRect = context.view.getBoundingClientRect();
    if (context.pointerMoveHandler)
        context.document.addEventListener("pointermove", context.pointerMoveHandler);
    context.document.addEventListener("pointerup", handleDiffPointerUp.bind(null, context), { once: true });
}
function handleDiffPointerMove(context, event) {
    if (context.dragRect)
        setDiffSplit(context, ((event.clientX - context.dragRect.left) / context.dragRect.width) * 100);
}
function handleDiffPointerUp(context) {
    context.dragRect = null;
    if (context.pointerMoveHandler)
        context.document.removeEventListener("pointermove", context.pointerMoveHandler);
}
// Centralizing the clamp, render, and persistence prevents keyboard and
// pointer interactions from drifting into different behavior.
function setDiffSplit(context, value) {
    context.percent = clampDiffSplit(value);
    renderDiffSplit(context);
    writePreference(context.storage, diffSplitStorageKey, String(context.percent));
}
function renderDiffSplit(context) {
    context.view.style.setProperty("--diff-split", `${context.percent}%`);
    context.divider.setAttribute("aria-valuenow", String(context.percent));
}
function clampDiffSplit(value) {
    return clampInteger(value, diffSplitMin, diffSplitMax);
}
function readDiffSplitPreference(storage) {
    const stored = Number.parseFloat(readPreference(storage, diffSplitStorageKey) ?? "");
    return Number.isFinite(stored) ? stored : 50;
}
function readPanelCollapsedPreference(storage, name, defaultCollapsed) {
    const stored = readPreference(storage, `${panelStoragePrefix}${name}`);
    return stored === null ? defaultCollapsed : stored === "true";
}
// Storage can be unavailable in privacy-restricted browser contexts. Viewer
// interactions must remain functional even when preferences cannot persist.
function readPreference(storage, key) {
    try {
        return storage.getItem(key);
    }
    catch (_) {
        return null;
    }
}
function writePreference(storage, key, value) {
    try {
        storage.setItem(key, value);
    }
    catch (_) {
        // The current-page interaction still works when storage is unavailable.
    }
}
initializeViewer();
