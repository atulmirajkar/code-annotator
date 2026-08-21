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
  bindComparisonControl();

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

  // bindComparisonControl turns the static base label into a bounded revision
  // selector and Refresh Git diff button backed by the server comparison API.
  // Selection and refresh reload the page in its existing File/Changes mode so
  // the server recomputes the diff against the newly adopted base.
  function bindComparisonControl() {
    const control = document.querySelector(".diff-comparison-control");
    const token = document.querySelector('meta[name="md-viewer-comparison-token"]')?.content || "";
    if (!control || !token) return;
    const selector = control.querySelector(".revision-selector");
    const refreshButton = control.querySelector(".refresh-git-diff");
    const status = control.querySelector(".diff-comparison-status");
    let currentRevision = "";

    selector.addEventListener("change", () => {
      mutate({ action: "select", commit: selector.value }, "Updating comparison base…");
    });
    refreshButton.addEventListener("click", () => {
      mutate({ action: "refresh" }, "Refreshing Git diff…");
    });
    load();

    async function load() {
      try {
        const response = await fetch("/api/git-comparison", { headers: { Accept: "application/json" } });
        if (!response.ok) throw new Error();
        render(await response.json());
      } catch (_) {
        setStatus("Revision list unavailable.", true);
      }
    }

    // render rebuilds the selector from server state. An active commit that is
    // no longer among the options, such as a pinned commit dropped from the
    // bounded list, is preserved as a leading selected entry.
    function render(state) {
      currentRevision = state.revision;
      const options = Array.isArray(state.options) ? state.options : [];
      selector.replaceChildren();
      if (!options.some((option) => option.commit === state.activeCommit)) {
        selector.append(buildOption({ commit: state.activeCommit, commitShort: state.activeShort, name: state.requestedBase, configured: !state.explicit }, state.activeCommit));
      }
      options.forEach((option) => selector.append(buildOption(option, state.activeCommit)));
      selector.disabled = false;
      refreshButton.disabled = false;
      setStatus("");
    }

    function buildOption(option, activeCommit) {
      const element = document.createElement("option");
      element.value = option.commit;
      element.textContent = optionLabel(option);
      element.title = option.subject ? `${option.commit} ${option.subject}` : option.commit;
      element.selected = option.commit === activeCommit;
      return element;
    }

    function optionLabel(option) {
      const name = option.configured && option.name ? `${option.name}: ` : "";
      const subject = option.subject ? ` ${truncate(option.subject, 72)}` : "";
      return `${name}${option.commitShort || ""}${subject}`;
    }

    async function mutate(payload, pending) {
      selector.disabled = true;
      refreshButton.disabled = true;
      setStatus(pending);
      try {
        const response = await fetch("/api/git-comparison", {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            "If-Match": JSON.stringify(currentRevision),
            "X-MD-Viewer-Comparison-Token": token,
          },
          body: JSON.stringify(payload),
        });
        if (response.status === 409) {
          render(await response.json());
          setStatus("The comparison base changed in another tab. Try again.", true);
          return;
        }
        if (!response.ok) throw new Error();
        // The server adopted the new base; reload keeps the current mode and URL
        // so diffs, highlights, and the changed-only filter recompute together.
        window.location.reload();
      } catch (_) {
        setStatus("The Git comparison could not be updated.", true);
        selector.disabled = false;
        refreshButton.disabled = false;
      }
    }

    function setStatus(message, isError) {
      status.textContent = message || "";
      status.classList.toggle("error", Boolean(isError));
    }

    function truncate(value, limit) {
      return value.length > limit ? `${value.slice(0, limit - 1)}…` : value;
    }
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
