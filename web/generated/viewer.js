import { buildDocumentTree, updateTreeVisibility } from "./document-tree.js";
(() => {
    "use strict";
    function requiredElement(value, label) {
        if (!value)
            throw new Error(`Missing ${label} in viewer template`);
        return value;
    }
    const changedOnlyStorageKey = "code-annotator.changed-only";
    const documentScopeStorageKey = "code-annotator.document-scope";
    const sourceModeStorageKey = "code-annotator.source-mode";
    const diffSplitStorageKey = "code-annotator.diff-split";
    const panelStoragePrefix = "code-annotator.panel-collapsed.";
    const diffSplitMin = 20;
    const diffSplitMax = 80;
    const diffSplitStep = 2;
    const layout = document.querySelector(".layout");
    if (!layout)
        return;
    bindTopbarHeight();
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
        defaultCollapsed: true,
    });
    bindSourceModePreference();
    bindDocumentSearch();
    bindComparisonControl();
    bindDiffDivider();
    function bindTopbarHeight() {
        const topbar = document.querySelector(".topbar");
        if (!topbar)
            return;
        const update = () => {
            document.documentElement.style.setProperty("--topbar-height", `${topbar.getBoundingClientRect().height}px`);
        };
        update();
        new ResizeObserver(update).observe(topbar);
    }
    // bindPanelToggle keeps the visual state, accessible state, and grid layout
    // synchronized for one optional viewer panel. defaultCollapsed applies only
    // on the panel's first use in a tab; an explicit prior choice always wins.
    function bindPanelToggle({ button, panel, collapsedClass, name, defaultCollapsed = false }) {
        if (!button || !panel)
            return;
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
        function setPanelCollapsed(collapsed) {
            togglePanel.hidden = collapsed;
            layout.classList.toggle(collapsedClass, collapsed);
            toggleButton.setAttribute("aria-expanded", String(!collapsed));
            toggleButton.textContent = `${collapsed ? "Show" : "Hide"} ${name}`;
        }
    }
    // Source mode is a reviewer preference across document navigation. It
    // changes only when a File or Changes tab is activated, then rewrites
    // sidebar links to match, for any document kind, since Changes view is no
    // longer code-only.
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
        if (readPreference(sourceModeStorageKey) !== "diff")
            return;
        document.querySelectorAll('.documents li a').forEach((link) => {
            const target = new URL(link.href);
            target.searchParams.set("mode", "diff");
            link.href = target.pathname + target.search;
        });
    }
    // bindDocumentSearch builds the real file tree from the stable flat catalog
    // emitted by the server, then applies path lookup and one mutually exclusive
    // document scope. Enter opens the first match, while slash focuses lookup.
    function bindDocumentSearch() {
        const input = document.querySelector(".document-search input");
        const changedOnly = document.querySelector(".document-changed-filter input");
        const openComments = document.querySelector(".document-open-filter input");
        const openCommentsTotal = document.querySelector(".document-open-total");
        const status = document.querySelector(".document-search-status");
        const list = document.querySelector(".documents");
        if (!input || !status || !list)
            return;
        const fileItems = Array.from(list.children).filter((item) => item instanceof HTMLLIElement);
        if (fileItems.length === 0)
            return;
        const tree = buildDocumentTree(list, fileItems, readPreference, writePreference);
        // refreshAnnotationSummary owns this map: it replaces the counts from the
        // server's annotation queue. applyDocumentFilters consumes those counts
        // when the reviewer scopes the tree to documents with open comments.
        let openCommentCounts = new Map();
        let scope = readDocumentScope();
        setScope(scope, false);
        const visibleLinks = () => fileItems
            .filter((item) => !item.hidden)
            .map((item) => item.querySelector("a"))
            .filter((link) => link !== null);
        // applyDocumentFilters marks each file match, then updateTreeVisibility
        // propagates those marks up through directory nodes so empty directories
        // disappear while a search or document scope is active.
        const applyDocumentFilters = () => {
            const query = input.value.trim().toLocaleLowerCase();
            let matches = 0;
            fileItems.forEach((item) => {
                const path = item.dataset.documentPath || "";
                const pathMatches = !query || path.toLocaleLowerCase().includes(query);
                const scopeMatches = scope === "all"
                    || (scope === "changed" && item.dataset.changed === "true")
                    || (scope === "open-comments" && (openCommentCounts.get(path) || 0) > 0);
                item.dataset.filterMatch = String(pathMatches && scopeMatches);
                if (pathMatches && scopeMatches)
                    matches++;
            });
            updateTreeVisibility(tree, scope !== "all");
            const descriptor = scope === "changed"
                ? (query ? "matching changed document" : "changed document")
                : scope === "open-comments"
                    ? (query ? "matching document with open comments" : "document with open comments")
                    : "matching document";
            const pluralDescriptor = descriptor.replace("document", "documents");
            const hasFilter = Boolean(query) || scope !== "all";
            status.hidden = !hasFilter;
            status.textContent = matches === 0
                ? `No ${pluralDescriptor}.`
                : `${matches} ${matches === 1 ? descriptor : pluralDescriptor}.`;
        };
        input.addEventListener("input", applyDocumentFilters);
        changedOnly?.addEventListener("change", () => {
            if (changedOnly.checked)
                setScope("changed");
            else
                setScope("all");
            applyDocumentFilters();
        });
        openComments?.addEventListener("change", () => {
            if (openComments.checked)
                setScope("open-comments");
            else
                setScope("all");
            applyDocumentFilters();
        });
        document.addEventListener("code-annotator:annotations-updated", () => {
            if (!openComments)
                return;
            refreshAnnotationSummary().then(applyDocumentFilters).catch(() => undefined);
        });
        if (openComments)
            refreshAnnotationSummary().then(applyDocumentFilters).catch(() => {
                openComments.disabled = true;
                applyDocumentFilters();
            });
        applyDocumentFilters();
        input.addEventListener("keydown", (event) => {
            if (event.key === "Escape") {
                input.value = "";
                applyDocumentFilters();
            }
            else if (event.key === "Enter") {
                visibleLinks()[0]?.click();
            }
            else if (event.key === "ArrowDown") {
                event.preventDefault();
                visibleLinks()[0]?.focus();
            }
        });
        document.addEventListener("keydown", (event) => {
            const target = event.target;
            const editing = target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement || target instanceof HTMLSelectElement || (target instanceof HTMLElement && target.isContentEditable);
            if (event.key === "/" && !editing && !event.metaKey && !event.ctrlKey && !event.altKey) {
                event.preventDefault();
                input.focus();
            }
        });
        function setScope(next, persist = true) {
            scope = next;
            if (changedOnly)
                changedOnly.checked = next === "changed";
            if (openComments)
                openComments.checked = next === "open-comments";
            if (persist)
                writePreference(documentScopeStorageKey, next);
        }
        async function refreshAnnotationSummary() {
            if (!openComments)
                return;
            const response = await fetch("/api/annotations?status=open,acknowledged,needs_changes,applied", {
                headers: { Accept: "application/json" },
            });
            if (!response.ok)
                throw new Error(`annotation queue request failed: ${response.status}`);
            const payload = await response.json();
            openCommentCounts = new Map((payload.documents || []).map((item) => [item.document || "", Array.isArray(item.annotations) ? item.annotations.length : 0]));
            const matchingDocuments = Array.from(openCommentCounts.values()).filter((count) => count > 0).length;
            if (openCommentsTotal) {
                openCommentsTotal.textContent = `${matchingDocuments} document${matchingDocuments === 1 ? "" : "s"}`;
            }
            fileItems.forEach((item) => {
                const path = item.dataset.documentPath || "";
                const count = openCommentCounts.get(path) || 0;
                const link = item.querySelector("a");
                if (!link)
                    return;
                let badge = link.querySelector(".document-open-count");
                if (count > 0) {
                    if (!badge) {
                        badge = document.createElement("span");
                        badge.className = "document-open-count";
                        link.append(badge);
                    }
                    badge.textContent = String(count);
                    badge.setAttribute("aria-label", `${count} open comment${count === 1 ? "" : "s"}`);
                }
                else {
                    badge?.remove();
                }
            });
        }
    }
    function readDocumentScope() {
        const stored = readPreference(documentScopeStorageKey);
        if (stored === "all" || stored === "changed" || stored === "open-comments")
            return stored;
        if (readPreference(changedOnlyStorageKey) === "true")
            return "changed";
        return hasChangedDocuments() ? "changed" : "all";
    }
    // bindComparisonControl turns the static base label into a bounded revision
    // selector backed by the server comparison API. The base is always one
    // explicit commit; selecting another re-pins it server-wide and reloads the
    // page in its existing File/Changes mode so the diff recomputes.
    function bindComparisonControl() {
        const control = document.querySelector(".diff-comparison-control");
        const token = document.querySelector('meta[name="code-annotator-comparison-token"]')?.content || "";
        if (!control || !token)
            return;
        const selector = requiredElement(control.querySelector(".revision-selector"), "revision selector");
        const status = requiredElement(control.querySelector(".diff-comparison-status"), "comparison status");
        selector.addEventListener("change", () => selectBase(selector.value));
        load();
        async function load() {
            try {
                const response = await fetch("/api/git-comparison", { headers: { Accept: "application/json" } });
                if (!response.ok)
                    throw new Error();
                render(await response.json());
            }
            catch (_) {
                setStatus("Revision list unavailable.", true);
            }
        }
        // render rebuilds the selector from server state. An active commit that is
        // no longer among the options, such as a pinned commit dropped from the
        // bounded list, is preserved as a leading selected entry.
        function render(state) {
            const options = Array.isArray(state.options) ? state.options : [];
            selector.replaceChildren();
            if (!options.some((option) => option.commit === state.activeCommit)) {
                selector.append(buildOption({ commit: state.activeCommit, ...(state.activeShort ? { commitShort: state.activeShort } : {}) }, state.activeCommit));
            }
            options.forEach((option) => selector.append(buildOption(option, state.activeCommit)));
            selector.disabled = false;
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
            const subject = option.subject ? ` ${truncate(option.subject, 72)}` : "";
            return `${option.commitShort || ""}${subject}`;
        }
        async function selectBase(commit) {
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
                if (!response.ok)
                    throw new Error();
                // The server re-pinned the base; reload keeps the current mode and URL
                // so diffs, highlights, and the changed-only filter recompute together.
                window.location.reload();
            }
            catch (_) {
                setStatus("The Git comparison could not be updated.", true);
                selector.disabled = false;
            }
        }
        function setStatus(message, isError = false) {
            status.textContent = message || "";
            status.classList.toggle("error", Boolean(isError));
        }
        function truncate(value, limit) {
            return value.length > limit ? `${value.slice(0, limit - 1)}…` : value;
        }
    }
    // bindDiffDivider lets the reviewer drag or use the keyboard to resize the
    // base and current diff panes. The column headings share the same CSS grid
    // template as the panes, so setting one custom property keeps both aligned.
    // The chosen split is a tab-scoped preference restored across navigation,
    // matching the other reviewer preferences on this page.
    function bindDiffDivider() {
        const view = document.querySelector(".diff-view");
        const divider = view?.querySelector(".diff-divider");
        if (!view || !divider)
            return;
        let percent = clampDiffSplit(readDiffSplitPreference());
        applyDiffSplit(view, divider, percent);
        divider.addEventListener("keydown", (event) => {
            if (event.key === "ArrowLeft")
                setSplit(percent - diffSplitStep);
            else if (event.key === "ArrowRight")
                setSplit(percent + diffSplitStep);
            else if (event.key === "Home")
                setSplit(diffSplitMin);
            else if (event.key === "End")
                setSplit(diffSplitMax);
            else
                return;
            event.preventDefault();
        });
        divider.addEventListener("pointerdown", (event) => {
            if (event.button !== 0)
                return;
            event.preventDefault();
            const rect = view.getBoundingClientRect();
            const onMove = (moveEvent) => setSplit(((moveEvent.clientX - rect.left) / rect.width) * 100);
            const onUp = () => {
                document.removeEventListener("pointermove", onMove);
                document.removeEventListener("pointerup", onUp);
            };
            document.addEventListener("pointermove", onMove);
            document.addEventListener("pointerup", onUp);
        });
        function setSplit(value) {
            percent = clampDiffSplit(value);
            applyDiffSplit(view, divider, percent);
            writeDiffSplitPreference(percent);
        }
    }
    function applyDiffSplit(view, divider, percent) {
        view.style.setProperty("--diff-split", `${percent}%`);
        divider.setAttribute("aria-valuenow", String(percent));
    }
    function clampDiffSplit(value) {
        return Math.min(diffSplitMax, Math.max(diffSplitMin, Math.round(value)));
    }
    function readDiffSplitPreference() {
        const stored = Number.parseFloat(readPreference(diffSplitStorageKey) || "");
        return Number.isFinite(stored) ? stored : 50;
    }
    function writeDiffSplitPreference(percent) {
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
        if (stored !== null)
            return stored === "true";
        return hasChangedDocuments();
    }
    function writeChangedOnlyPreference(enabled) {
        writeBooleanPreference(changedOnlyStorageKey, enabled);
    }
    function hasChangedDocuments() {
        return document.querySelector('.documents li[data-changed="true"]') !== null;
    }
    // readPanelCollapsedPreference falls back to defaultCollapsed only when the
    // panel has never been toggled in this tab, so an explicit "false" (shown)
    // choice is never overridden by a panel's own default.
    function readPanelCollapsedPreference(name, defaultCollapsed) {
        const stored = readPreference(`${panelStoragePrefix}${name}`);
        return stored === null ? defaultCollapsed : stored === "true";
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
        }
        catch (_) {
            return null;
        }
    }
    function writePreference(key, value) {
        try {
            sessionStorage.setItem(key, value);
        }
        catch (_) {
            // The current-page interaction still works when storage is unavailable.
        }
    }
})();
