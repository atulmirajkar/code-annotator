import { filterDocuments, hasChangedDocuments } from "./document-catalog.js";
import { fetchDocumentCatalogState } from "./document-state.js";
import type { DocumentCatalogState, DocumentMode } from "./document-state.js";

(() => {
  "use strict";

  interface PanelToggleOptions {
    button: HTMLButtonElement | null;
    panel: HTMLElement | null;
    collapsedClass: string;
    name: string;
    defaultCollapsed?: boolean;
  }

  interface HtmxConfig {
    allowEval: boolean;
    allowNestedOobSwaps: boolean;
    allowScriptTags: boolean;
    historyCacheSize: number;
    selfRequestsOnly: boolean;
  }

  interface HtmxAPI {
    config: HtmxConfig;
    ajax(verb: "GET", path: string, options: { target: string; swap: "outerHTML" }): Promise<void>;
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
  const layout = document.querySelector<HTMLElement>(".layout");
  if (!layout) return;

  configureHTMX();

  bindTopbarHeight();

  bindPanelToggle({
    button: document.querySelector<HTMLButtonElement>(".documents-toggle"),
    panel: document.querySelector<HTMLElement>("#documents-sidebar"),
    collapsedClass: "documents-collapsed",
    name: "documents",
  });
  bindPanelToggle({
    button: document.querySelector<HTMLButtonElement>(".review-toggle"),
    panel: document.querySelector<HTMLElement>("#annotation-sidebar"),
    collapsedClass: "review-collapsed",
    name: "annotations",
    defaultCollapsed: true,
  });
  bindSourceModePreference();
  bindDocumentSearch();
  bindComparisonControl();
  bindDiffDivider();

  function configureHTMX(): void {
    const api = Reflect.get(globalThis, "htmx") as HtmxAPI | undefined;
    if (!api) return;
    api.config.allowEval = false;
    api.config.allowNestedOobSwaps = false;
    api.config.allowScriptTags = false;
    api.config.historyCacheSize = 0;
    api.config.selfRequestsOnly = true;
  }

  function bindTopbarHeight(): void {
    const topbar = document.querySelector<HTMLElement>(".topbar");
    if (!topbar) return;
    const update = (): void => {
      document.documentElement.style.setProperty("--topbar-height", `${topbar.getBoundingClientRect().height}px`);
    };
    update();
    new ResizeObserver(update).observe(topbar);
  }

  // bindPanelToggle keeps the visual state, accessible state, and grid layout
  // synchronized for one optional viewer panel. defaultCollapsed applies only
  // on the panel's first use in a tab; an explicit prior choice always wins.
  function bindPanelToggle({ button, panel, collapsedClass, name, defaultCollapsed = false }: PanelToggleOptions): void {
    if (!button || !panel) return;
    const toggleButton = button;
    const togglePanel = panel;
    setPanelCollapsed(readPanelCollapsedPreference(name, defaultCollapsed));
    toggleButton.addEventListener("click", () => {
      const collapsed = !togglePanel.hidden;
      setPanelCollapsed(collapsed);
      writeBooleanPreference(`${panelStoragePrefix}${name}`, collapsed);
    });

    // setPanelCollapsed restores and updates all representations of one panel
    // choice so navigation never briefly leaves the grid in a stale state.
    function setPanelCollapsed(collapsed: boolean): void {
      togglePanel.hidden = collapsed;
      layout!.classList.toggle(collapsedClass, collapsed);
      toggleButton.setAttribute("aria-expanded", String(!collapsed));
      toggleButton.textContent = `${collapsed ? "Show" : "Hide"} ${name}`;
    }
  }

  // Document links are rendered in the active mode by the server. TypeScript
  // only remembers an explicit tab choice; it never rewrites the catalog DOM.
  function bindSourceModePreference() {
    const tabs = document.querySelector<HTMLElement>(".source-mode-tabs");
    const activeTab = tabs?.querySelector<HTMLAnchorElement>('a[aria-current="page"]');
    if (activeTab) {
      const activeMode = new URL(activeTab.href).searchParams.get("mode") === "diff" ? "diff" : "file";
      writePreference(sourceModeStorageKey, activeMode);
      tabs!.querySelectorAll<HTMLAnchorElement>("a").forEach((tab) => {
        tab.addEventListener("click", () => {
          const mode = new URL(tab.href).searchParams.get("mode") === "diff" ? "diff" : "file";
          writePreference(sourceModeStorageKey, mode);
        });
      });
    }

  }

  type DocumentScope = "all" | "changed" | "open-comments";

  // The server owns tree construction, filtering, counts, and links. This
  // adapter retains only keyboard behavior and tab-local view preferences,
  // deriving navigation choices from the validated typed catalog.
  function bindDocumentSearch() {
    const panel = document.querySelector<HTMLElement>("#document-panel-content");
    if (!panel) return;
    let state: DocumentCatalogState | null = null;
    let scope: DocumentScope = "all";
    let searchTimer = 0;
    let documentRequestRunning = false;
    let queuedDocumentRequest: { query: string; scope: DocumentScope } | null = null;
    const mode: DocumentMode = new URL(location.href).searchParams.get("mode") === "diff" ? "diff" : "file";
    const path = decodeURIComponent(location.pathname.startsWith("/view/") ? location.pathname.slice(6) : "");

    fetchDocumentCatalogState(path, mode).then((catalog) => {
      state = catalog;
      scope = readDocumentScope(catalog);
      const checkbox = document.querySelector<HTMLInputElement>(`.document-filter-form input[value="${scope}"]`);
      if (scope !== "all" && checkbox && !checkbox.checked) {
        checkbox.checked = true;
        requestDocumentPanel("", scope);
      }
    }).catch(() => undefined);

    document.addEventListener("change", (event) => {
      const input = event.target;
      if (!(input instanceof HTMLInputElement) || input.form?.classList.contains("document-filter-form") !== true || input.name !== "scope") return;
      scope = input.checked && (input.value === "changed" || input.value === "open-comments") ? input.value : "all";
      input.form.querySelectorAll<HTMLInputElement>('input[name="scope"]').forEach((candidate) => {
        if (candidate !== input) candidate.checked = false;
      });
      writePreference(documentScopeStorageKey, scope);
      requestDocumentPanel(input.form.querySelector<HTMLInputElement>("#document-search-input")?.value || "", scope);
    }, true);

    document.addEventListener("input", (event) => {
      const input = event.target;
      if (!(input instanceof HTMLInputElement) || input.id !== "document-search-input") return;
      window.clearTimeout(searchTimer);
      searchTimer = window.setTimeout(() => requestDocumentPanel(input.value, scope), 150);
    }, true);

    document.addEventListener("search", (event) => {
      const input = event.target;
      if (!(input instanceof HTMLInputElement) || input.id !== "document-search-input") return;
      window.clearTimeout(searchTimer);
      requestDocumentPanel(input.value, scope);
    }, true);

    document.addEventListener("click", (event) => {
      const button = event.target instanceof Element ? event.target.closest<HTMLButtonElement>(".document-directory-toggle") : null;
      if (!button) return;
      const item = button.closest<HTMLLIElement>(".document-directory");
      if (!item) return;
      const expanded = button.getAttribute("aria-expanded") !== "true";
      button.setAttribute("aria-expanded", String(expanded));
      item.classList.toggle("collapsed", !expanded);
      writeExpandedDirectories();
    });

    document.addEventListener("htmx:afterSwap", () => {
      restoreExpandedDirectories();
    });
    restoreExpandedDirectories();

    document.addEventListener("code-annotator:annotations-updated", () => {
      fetchDocumentCatalogState(path, mode).then((catalog) => {
        state = catalog;
        document.querySelector<HTMLInputElement>("#document-search-input")?.dispatchEvent(new Event("search", { bubbles: true }));
      }).catch(() => undefined);
    });

    document.addEventListener("keydown", (event: KeyboardEvent) => {
      const input = document.querySelector<HTMLInputElement>("#document-search-input");
      if (!input) return;
      if (event.target === input && event.key === "Escape") {
        event.preventDefault();
        input.value = "";
        input.dispatchEvent(new Event("search", { bubbles: true }));
      } else if (event.target === input && event.key === "Enter") {
        event.preventDefault();
        const destination = state ? filterDocuments(state.documents, input.value, scope).documents[0] : undefined;
        if (destination) location.assign(destination.url);
      } else if (event.target === input && event.key === "ArrowDown") {
        event.preventDefault();
        document.querySelector<HTMLAnchorElement>(".documents .document-file a")?.focus();
      }
    });
    document.addEventListener("keydown", (event: KeyboardEvent) => {
      const target = event.target;
      const editing = target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement || target instanceof HTMLSelectElement || (target instanceof HTMLElement && target.isContentEditable);
      if (event.key === "/" && !editing && !event.metaKey && !event.ctrlKey && !event.altKey) {
        event.preventDefault();
        document.querySelector<HTMLInputElement>("#document-search-input")?.focus();
      }
    });

    function restoreExpandedDirectories(): void {
      const stored = readPreference(documentTreeStorageKey);
      if (stored === null) return;
      const expanded = readStringSet(stored);
      document.querySelectorAll<HTMLLIElement>(".document-directory[id]").forEach((item) => {
        const isExpanded = expanded.has(item.id);
        item.classList.toggle("collapsed", !isExpanded);
        item.querySelector<HTMLButtonElement>(":scope > .document-directory-toggle")?.setAttribute("aria-expanded", String(isExpanded));
      });
    }

    function writeExpandedDirectories(): void {
      const expanded = Array.from(document.querySelectorAll<HTMLLIElement>(".document-directory[id]"))
        .filter((item) => !item.classList.contains("collapsed"))
        .map((item) => item.id)
        .sort();
      writePreference(documentTreeStorageKey, JSON.stringify(expanded));
    }

    function requestDocumentPanel(queryValue: string, nextScope: DocumentScope): void {
      queuedDocumentRequest = { query: queryValue, scope: nextScope };
      if (documentRequestRunning) return;
      void sendNextDocumentRequest();
    }

    async function sendNextDocumentRequest(): Promise<void> {
      const api = Reflect.get(globalThis, "htmx") as HtmxAPI | undefined;
      const request = queuedDocumentRequest;
      if (!api || !request) return;
      queuedDocumentRequest = null;
      documentRequestRunning = true;
      const parameters = new URLSearchParams({ document: path, mode });
      if (request.query) parameters.set("query", request.query);
      if (request.scope !== "all") parameters.set("scope", request.scope);
      try {
        await api.ajax("GET", `/ui/review/documents?${parameters.toString()}`, { target: "#document-panel-content", swap: "outerHTML" });
      } finally {
        documentRequestRunning = false;
        if (queuedDocumentRequest) void sendNextDocumentRequest();
      }
    }
  }

  function readDocumentScope(state: DocumentCatalogState): DocumentScope {
    const stored = readPreference(documentScopeStorageKey);
    if (stored === "all" || stored === "changed" || stored === "open-comments") return stored;
    if (readPreference(changedOnlyStorageKey) === "true") return "changed";
    return hasChangedDocuments(state.documents) ? "changed" : "all";
  }

  function readStringSet(value: string): Set<string> {
    try {
      const parsed: unknown = JSON.parse(value);
      return new Set(Array.isArray(parsed) ? parsed.filter((item): item is string => typeof item === "string") : []);
    } catch (_) {
      return new Set();
    }
  }

  // The server renders comparison state and validates the selected commit.
  // This adapter only adds the secret transport header and submits on change.
  function bindComparisonControl() {
    const control = document.querySelector<HTMLFormElement>(".diff-comparison-control");
    const token = document.querySelector<HTMLMetaElement>('meta[name="code-annotator-comparison-token"]')?.content || "";
    if (!control || !token) return;
    const selector = control.querySelector<HTMLSelectElement>(".revision-selector");
    const status = control.querySelector<HTMLElement>(".diff-comparison-status");
    if (!selector || !status) return;
    selector.addEventListener("change", () => {
      status.textContent = "Updating comparison base…";
      status.classList.remove("error");
      control.requestSubmit();
    });
    document.body.addEventListener("htmx:configRequest", (event) => {
      if (!(event instanceof CustomEvent) || typeof event.detail !== "object" || event.detail === null) return;
      const source = Reflect.get(event.detail, "elt");
      if (!(source instanceof Element) || source.closest(".diff-comparison-control") !== control) return;
      const headers = Reflect.get(event.detail, "headers");
      if (typeof headers === "object" && headers !== null) Reflect.set(headers, "X-Code-Annotator-Comparison-Token", token);
    });
    document.body.addEventListener("htmx:responseError", (event) => {
      if (!(event instanceof CustomEvent) || typeof event.detail !== "object" || event.detail === null) return;
      const source = Reflect.get(event.detail, "elt");
      if (!(source instanceof Element) || source.closest(".diff-comparison-control") !== control) return;
      status.textContent = "The Git comparison could not be updated.";
      status.classList.add("error");
    });
  }

  // bindDiffDivider lets the reviewer drag or use the keyboard to resize the
  // base and current diff panes. The column headings share the same CSS grid
  // template as the panes, so setting one custom property keeps both aligned.
  // The chosen split is a tab-scoped preference restored across navigation,
  // matching the other reviewer preferences on this page.
  function bindDiffDivider() {
    const view = document.querySelector<HTMLElement>(".diff-view");
    const divider = view?.querySelector<HTMLElement>(".diff-divider");
    if (!view || !divider) return;
    let percent = clampDiffSplit(readDiffSplitPreference());
    applyDiffSplit(view, divider, percent);

    divider.addEventListener("keydown", (event: KeyboardEvent) => {
      if (event.key === "ArrowLeft") setSplit(percent - diffSplitStep);
      else if (event.key === "ArrowRight") setSplit(percent + diffSplitStep);
      else if (event.key === "Home") setSplit(diffSplitMin);
      else if (event.key === "End") setSplit(diffSplitMax);
      else return;
      event.preventDefault();
    });

    divider.addEventListener("pointerdown", (event: PointerEvent) => {
      if (event.button !== 0) return;
      event.preventDefault();
      const rect = view.getBoundingClientRect();
      const onMove = (moveEvent: PointerEvent): void => setSplit(((moveEvent.clientX - rect.left) / rect.width) * 100);
      const onUp = () => {
        document.removeEventListener("pointermove", onMove);
        document.removeEventListener("pointerup", onUp);
      };
      document.addEventListener("pointermove", onMove);
      document.addEventListener("pointerup", onUp);
    });

    function setSplit(value: number): void {
      percent = clampDiffSplit(value);
      applyDiffSplit(view!, divider!, percent);
      writeDiffSplitPreference(percent);
    }
  }

  function applyDiffSplit(view: HTMLElement, divider: HTMLElement, percent: number): void {
    view.style.setProperty("--diff-split", `${percent}%`);
    divider.setAttribute("aria-valuenow", String(percent));
  }

  function clampDiffSplit(value: number): number {
    return Math.min(diffSplitMax, Math.max(diffSplitMin, Math.round(value)));
  }

  function readDiffSplitPreference() {
    const stored = Number.parseFloat(readPreference(diffSplitStorageKey) || "");
    return Number.isFinite(stored) ? stored : 50;
  }

  function writeDiffSplitPreference(percent: number): void {
    writePreference(diffSplitStorageKey, String(percent));
  }

  // readPanelCollapsedPreference falls back to defaultCollapsed only when the
  // panel has never been toggled in this tab, so an explicit "false" (shown)
  // choice is never overridden by a panel's own default.
  function readPanelCollapsedPreference(name: string, defaultCollapsed: boolean): boolean {
    const stored = readPreference(`${panelStoragePrefix}${name}`);
    return stored === null ? defaultCollapsed : stored === "true";
  }

  function readBooleanPreference(key: string): boolean {
    return readPreference(key) === "true";
  }

  function writeBooleanPreference(key: string, enabled: boolean): void {
    writePreference(key, String(enabled));
  }

  function readPreference(key: string): string | null {
    try {
      return sessionStorage.getItem(key);
    } catch (_) {
      return null;
    }
  }

  function writePreference(key: string, value: string): void {
    try {
      sessionStorage.setItem(key, value);
    } catch (_) {
      // The current-page interaction still works when storage is unavailable.
    }
  }
})();
