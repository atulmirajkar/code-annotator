(() => {
  "use strict";

  interface PanelToggleOptions {
    button: HTMLButtonElement | null;
    panel: HTMLElement | null;
    collapsedClass: string;
    name: string;
    defaultCollapsed?: boolean;
  }

  interface ComparisonOption {
    commit: string;
    commitShort?: string;
    subject?: string;
  }

  interface ComparisonState {
    activeCommit: string;
    activeShort?: string;
    options: ComparisonOption[];
  }

  function requiredElement<T extends Element>(value: T | null, label: string): T {
    if (!value) throw new Error(`Missing ${label} in viewer template`);
    return value;
  }

  const changedOnlyStorageKey = "code-annotator.changed-only";
  const sourceModeStorageKey = "code-annotator.source-mode";
  const diffSplitStorageKey = "code-annotator.diff-split";
  const panelStoragePrefix = "code-annotator.panel-collapsed.";
  const diffSplitMin = 20;
  const diffSplitMax = 80;
  const diffSplitStep = 2;
  const layout = document.querySelector<HTMLElement>(".layout");
  if (!layout) return;

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

  // Source mode is a reviewer preference across document navigation. It
  // changes only when a File or Changes tab is activated, then rewrites
  // sidebar links to match, for any document kind, since Changes view is no
  // longer code-only.
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

    if (readPreference(sourceModeStorageKey) !== "diff") return;
    document.querySelectorAll<HTMLAnchorElement>('.documents li a').forEach((link) => {
      const target = new URL(link.href);
      target.searchParams.set("mode", "diff");
      link.href = target.pathname + target.search;
    });
  }

  // bindDocumentSearch filters by the displayed slash-separated relative path.
  // Enter opens the first match, while slash focuses lookup from document view.
  function bindDocumentSearch() {
    const input = document.querySelector<HTMLInputElement>(".document-search input");
    const changedOnly = document.querySelector<HTMLInputElement>(".document-changed-filter input");
    const status = document.querySelector<HTMLElement>(".document-search-status");
    const items = Array.from(document.querySelectorAll<HTMLElement>(".documents li"));
    if (!input || !status || items.length === 0) return;
    if (changedOnly) changedOnly.checked = readChangedOnlyPreference();

    const visibleLinks = (): HTMLAnchorElement[] => items.filter((item) => !item.hidden).map((item) => item.querySelector<HTMLAnchorElement>("a")).filter((link): link is HTMLAnchorElement => link !== null);
    const filter = (): void => {
      const query = input.value.trim().toLocaleLowerCase();
      const changed = Boolean(changedOnly?.checked);
      let matches = 0;
      items.forEach((item) => {
        const path = (item.textContent || "").trim().toLocaleLowerCase();
        const pathMatches = !query || path.includes(query);
        const changeMatches = !changed || item.dataset.changed === "true";
        item.hidden = !pathMatches || !changeMatches;
        if (!item.hidden) matches++;
      });
      status.hidden = !query && !changed;
      const qualifier = changed ? (query ? "matching changed" : "changed") : "matching";
      status.textContent = matches === 0 ? `No ${qualifier} documents.` : `${matches} ${qualifier} document${matches === 1 ? "" : "s"}.`;
    };

    input.addEventListener("input", filter);
    changedOnly?.addEventListener("change", () => {
      writeChangedOnlyPreference(changedOnly.checked);
      filter();
    });
    filter();
    input.addEventListener("keydown", (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        input.value = "";
        filter();
      } else if (event.key === "Enter") {
        visibleLinks()[0]?.click();
      } else if (event.key === "ArrowDown") {
        event.preventDefault();
        visibleLinks()[0]?.focus();
      }
    });
    document.addEventListener("keydown", (event: KeyboardEvent) => {
      const target = event.target;
      const editing = target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement || target instanceof HTMLSelectElement || (target instanceof HTMLElement && target.isContentEditable);
      if (event.key === "/" && !editing && !event.metaKey && !event.ctrlKey && !event.altKey) {
        event.preventDefault();
        input.focus();
      }
    });
  }

  // bindComparisonControl turns the static base label into a bounded revision
  // selector backed by the server comparison API. The base is always one
  // explicit commit; selecting another re-pins it server-wide and reloads the
  // page in its existing File/Changes mode so the diff recomputes.
  function bindComparisonControl() {
    const control = document.querySelector<HTMLElement>(".diff-comparison-control");
    const token = document.querySelector<HTMLMetaElement>('meta[name="code-annotator-comparison-token"]')?.content || "";
    if (!control || !token) return;
    const selector = requiredElement(control.querySelector<HTMLSelectElement>(".revision-selector"), "revision selector");
    const status = requiredElement(control.querySelector<HTMLElement>(".diff-comparison-status"), "comparison status");

    selector.addEventListener("change", () => selectBase(selector.value));
    load();

    async function load(): Promise<void> {
      try {
        const response = await fetch("/api/git-comparison", { headers: { Accept: "application/json" } });
        if (!response.ok) throw new Error();
        render(await response.json() as ComparisonState);
      } catch (_) {
        setStatus("Revision list unavailable.", true);
      }
    }

    // render rebuilds the selector from server state. An active commit that is
    // no longer among the options, such as a pinned commit dropped from the
    // bounded list, is preserved as a leading selected entry.
    function render(state: ComparisonState): void {
      const options = Array.isArray(state.options) ? state.options : [];
      selector.replaceChildren();
      if (!options.some((option) => option.commit === state.activeCommit)) {
        selector.append(buildOption({ commit: state.activeCommit, ...(state.activeShort ? { commitShort: state.activeShort } : {}) }, state.activeCommit));
      }
      options.forEach((option) => selector.append(buildOption(option, state.activeCommit)));
      selector.disabled = false;
      setStatus("");
    }

    function buildOption(option: ComparisonOption, activeCommit: string): HTMLOptionElement {
      const element = document.createElement("option");
      element.value = option.commit;
      element.textContent = optionLabel(option);
      element.title = option.subject ? `${option.commit} ${option.subject}` : option.commit;
      element.selected = option.commit === activeCommit;
      return element;
    }

    function optionLabel(option: ComparisonOption): string {
      const subject = option.subject ? ` ${truncate(option.subject, 72)}` : "";
      return `${option.commitShort || ""}${subject}`;
    }

    async function selectBase(commit: string): Promise<void> {
      selector.disabled = true;
      setStatus("Updating comparison base…");
      try {
        const response = await fetch("/api/git-comparison", {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            "X-Code-Annotator-Comparison-Token": token,
          },
          body: JSON.stringify({ commit }),
        });
        if (!response.ok) throw new Error();
        // The server re-pinned the base; reload keeps the current mode and URL
        // so diffs, highlights, and the changed-only filter recompute together.
        window.location.reload();
      } catch (_) {
        setStatus("The Git comparison could not be updated.", true);
        selector.disabled = false;
      }
    }

    function setStatus(message: string, isError = false): void {
      status.textContent = message || "";
      status.classList.toggle("error", Boolean(isError));
    }

    function truncate(value: string, limit: number): string {
      return value.length > limit ? `${value.slice(0, limit - 1)}…` : value;
    }
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

  // Session storage keeps an explicit reviewer choice across document
  // navigation in one tab without turning it into a server-wide preference.
  // Before any explicit choice, a configured Git base with at least one
  // changed document defaults the filter on: that is exactly the moment a
  // reviewer wants the sidebar scoped to changed files, independent of which
  // document happens to be open first (often a Markdown file with no diff).
  // A clean worktree with nothing changed leaves the default off, since an
  // always-on default would otherwise open to an empty filtered list.
  function readChangedOnlyPreference() {
    const stored = readPreference(changedOnlyStorageKey);
    if (stored !== null) return stored === "true";
    return hasChangedDocuments();
  }

  function writeChangedOnlyPreference(enabled: boolean): void {
    writeBooleanPreference(changedOnlyStorageKey, enabled);
  }

  function hasChangedDocuments() {
    return document.querySelector('.documents li[data-changed="true"]') !== null;
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
