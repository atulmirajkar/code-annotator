import { readPreference, writePreference } from "./browser-storage.js";
const collapsedDirectoriesStorageKey = "code-annotator.document-tree-collapsed";
const legacyExpandedDirectoriesStorageKey = "code-annotator.document-tree-expanded";
// Directory expansion is browser-owned presentation state. Semantic element
// IDs connect that typed state to server-rendered directories without reading
// classes or ARIA attributes back from the DOM.
export function bindDocumentTree(document, storage) {
    if (!document.querySelector("#document-panel-content"))
        return;
    const context = {
        document,
        storage,
        collapsedIds: readCollapsedIds(document, storage),
    };
    document.addEventListener("click", (event) => handleDirectoryClick(context, event));
    document.addEventListener("htmx:afterSwap", () => renderTree(context));
    // HTMX settles classes for same-ID elements after the swap event. Reapply
    // presentation state afterward so its class and ARIA state cannot diverge.
    document.addEventListener("htmx:afterSettle", () => renderTree(context));
    renderTree(context);
}
function handleDirectoryClick(context, event) {
    const button = event.target instanceof Element
        ? event.target.closest(".document-directory-toggle")
        : null;
    const item = button?.closest(".document-directory[id]");
    if (!button || !item)
        return;
    const collapsed = !context.collapsedIds.has(item.id);
    if (collapsed)
        context.collapsedIds.add(item.id);
    else
        context.collapsedIds.delete(item.id);
    renderDirectory(item, button, !collapsed);
    writeCollapsedIds(context.storage, context.collapsedIds);
}
// HTMX replaces the entire document panel. Project the existing state onto
// each replacement fragment so the server does not need tab-local preferences.
function renderTree(context) {
    for (const item of context.document.querySelectorAll(".document-directory[id]")) {
        const button = item.querySelector(":scope > .document-directory-toggle");
        if (button) {
            renderDirectory(item, button, !context.collapsedIds.has(item.id));
        }
    }
}
function renderDirectory(item, button, expanded) {
    item.classList.toggle("collapsed", !expanded);
    button.setAttribute("aria-expanded", String(expanded));
}
function readCollapsedIds(document, storage) {
    const stored = readPreference(storage, collapsedDirectoriesStorageKey);
    if (stored !== null)
        return parseDirectoryIds(stored) ?? new Set();
    // Migrate only directories visible in the initial fragment. A directory
    // absent from that filtered fragment has no known legacy preference and
    // therefore keeps the new default-expanded behavior when later revealed.
    const legacyStored = readPreference(storage, legacyExpandedDirectoriesStorageKey);
    const legacyExpandedIds = legacyStored
        ? parseDirectoryIds(legacyStored)
        : null;
    if (!legacyExpandedIds)
        return new Set();
    const collapsedIds = new Set();
    for (const item of document.querySelectorAll(".document-directory[id]")) {
        if (!legacyExpandedIds.has(item.id))
            collapsedIds.add(item.id);
    }
    writeCollapsedIds(storage, collapsedIds);
    return collapsedIds;
}
function parseDirectoryIds(value) {
    try {
        const parsed = JSON.parse(value);
        if (!Array.isArray(parsed))
            return null;
        const ids = new Set();
        for (const item of parsed) {
            if (typeof item === "string")
                ids.add(item);
        }
        return ids;
    }
    catch (_) {
        return null;
    }
}
function writeCollapsedIds(storage, collapsedIds) {
    writePreference(storage, collapsedDirectoriesStorageKey, JSON.stringify(Array.from(collapsedIds).sort()));
}
