(() => {
  "use strict";

  const changedOnlyStorageKey = "md-viewer.changed-only";
  const sourceModeStorageKey = "md-viewer.source-mode";
  const panelStoragePrefix = "md-viewer.panel-collapsed.";
  const layout = document.querySelector(".layout");
  if (!layout) return;

  bindPanelToggle({
    button: document.querySelector(".documents-toggle"),
    panel: document.querySelector("#documents-sidebar"),
    collapsedClass: "documents-collapsed",
    name: "documents",
  });
  bindPanelToggle({
    button: document.querySelector(".review-toggle"),
    panel: document.querySelector("#annotation-sidebar"),
    collapsedClass: "review-collapsed",
    name: "annotations",
  });
  bindSourceModePreference();
  bindDocumentSearch();

  // bindPanelToggle keeps the visual state, accessible state, and grid layout
  // synchronized for one optional viewer panel.
  function bindPanelToggle({ button, panel, collapsedClass, name }) {
    if (!button || !panel) return;
    setPanelCollapsed(readBooleanPreference(`${panelStoragePrefix}${name}`));
    button.addEventListener("click", () => {
      const collapsed = !panel.hidden;
      setPanelCollapsed(collapsed);
      writeBooleanPreference(`${panelStoragePrefix}${name}`, collapsed);
    });

    // setPanelCollapsed restores and updates all representations of one panel
    // choice so navigation never briefly leaves the grid in a stale state.
    function setPanelCollapsed(collapsed) {
      panel.hidden = collapsed;
      layout.classList.toggle(collapsedClass, collapsed);
      button.setAttribute("aria-expanded", String(!collapsed));
      button.textContent = `${collapsed ? "Show" : "Hide"} ${name}`;
    }
  }

  // Source mode is a reviewer preference across code-document navigation. It
  // changes only when a File or Changes tab is activated, then rewrites code
  // links without applying diff mode to Markdown documents.
  function bindSourceModePreference() {
    const tabs = document.querySelector(".source-mode-tabs");
    const activeTab = tabs?.querySelector('a[aria-current="page"]');
    if (activeTab) {
      const activeMode = new URL(activeTab.href).searchParams.get("mode") === "diff" ? "diff" : "file";
      writePreference(sourceModeStorageKey, activeMode);
      tabs.querySelectorAll("a").forEach((tab) => {
        tab.addEventListener("click", () => {
          const mode = new URL(tab.href).searchParams.get("mode") === "diff" ? "diff" : "file";
          writePreference(sourceModeStorageKey, mode);
        });
      });
    }

    if (readPreference(sourceModeStorageKey) !== "diff") return;
    document.querySelectorAll('.documents li[data-kind="code"] a').forEach((link) => {
      const target = new URL(link.href);
      target.searchParams.set("mode", "diff");
      link.href = target.pathname + target.search;
    });
  }

  // bindDocumentSearch filters by the displayed slash-separated relative path.
  // Enter opens the first match, while slash focuses lookup from document view.
  function bindDocumentSearch() {
    const input = document.querySelector(".document-search input");
    const changedOnly = document.querySelector(".document-changed-filter input");
    const status = document.querySelector(".document-search-status");
    const items = Array.from(document.querySelectorAll(".documents li"));
    if (!input || !status || items.length === 0) return;
    if (changedOnly) changedOnly.checked = readChangedOnlyPreference();

    const visibleLinks = () => items.filter((item) => !item.hidden).map((item) => item.querySelector("a"));
    const filter = () => {
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
    input.addEventListener("keydown", (event) => {
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
    document.addEventListener("keydown", (event) => {
      const target = event.target;
      const editing = target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement || target instanceof HTMLSelectElement || target?.isContentEditable;
      if (event.key === "/" && !editing && !event.metaKey && !event.ctrlKey && !event.altKey) {
        event.preventDefault();
        input.focus();
      }
    });
  }

  // Session storage keeps an explicit reviewer choice across document
  // navigation in one tab without turning it into a server-wide preference.
  function readChangedOnlyPreference() {
    return readBooleanPreference(changedOnlyStorageKey);
  }

  function writeChangedOnlyPreference(enabled) {
    writeBooleanPreference(changedOnlyStorageKey, enabled);
  }

  function readBooleanPreference(key) {
    return readPreference(key) === "true";
  }

  function writeBooleanPreference(key, enabled) {
    writePreference(key, String(enabled));
  }

  function readPreference(key) {
    try {
      return sessionStorage.getItem(key);
    } catch (_) {
      return null;
    }
  }

  function writePreference(key, value) {
    try {
      sessionStorage.setItem(key, value);
    } catch (_) {
      // The current-page interaction still works when storage is unavailable.
    }
  }
})();
