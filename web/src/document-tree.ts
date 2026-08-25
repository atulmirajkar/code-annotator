import { readPreference, writePreference } from "./browser-storage.js";

const expandedDirectoriesStorageKey = "code-annotator.document-tree-expanded";

interface DocumentTreeContext {
  document: Document;
  storage: Storage;
  expandedIds: Set<string>;
}

// Directory expansion is browser-owned presentation state. Semantic element
// IDs connect that typed state to server-rendered directories without reading
// classes or ARIA attributes back from the DOM.
export function bindDocumentTree(document: Document, storage: Storage): void {
  if (!document.querySelector("#document-panel-content")) return;
  const context: DocumentTreeContext = {
    document,
    storage,
    expandedIds: readExpandedIds(document, storage),
  };

  document.addEventListener("click", (event) =>
    handleDirectoryClick(context, event),
  );
  document.addEventListener("htmx:afterSwap", () => renderTree(context));
  renderTree(context);
}

function handleDirectoryClick(
  context: DocumentTreeContext,
  event: MouseEvent,
): void {
  const button =
    event.target instanceof Element
      ? event.target.closest<HTMLButtonElement>(".document-directory-toggle")
      : null;
  const item = button?.closest<HTMLLIElement>(".document-directory[id]");
  if (!button || !item) return;

  const expanded = !context.expandedIds.has(item.id);
  if (expanded) context.expandedIds.add(item.id);
  else context.expandedIds.delete(item.id);

  renderDirectory(item, button, expanded);
  writeExpandedIds(context.storage, context.expandedIds);
}

// HTMX replaces the entire document panel. Project the existing state onto
// each replacement fragment so the server does not need tab-local preferences.
function renderTree(context: DocumentTreeContext): void {
  for (const item of context.document.querySelectorAll<HTMLLIElement>(
    ".document-directory[id]",
  )) {
    const button = item.querySelector<HTMLButtonElement>(
      ":scope > .document-directory-toggle",
    );
    if (button) renderDirectory(item, button, context.expandedIds.has(item.id));
  }
}

function renderDirectory(
  item: HTMLLIElement,
  button: HTMLButtonElement,
  expanded: boolean,
): void {
  item.classList.toggle("collapsed", !expanded);
  button.setAttribute("aria-expanded", String(expanded));
}

function readExpandedIds(document: Document, storage: Storage): Set<string> {
  const stored = readPreference(storage, expandedDirectoriesStorageKey);
  if (stored !== null) return parseExpandedIds(stored);

  // The server's initial tree is fully expanded. Use its semantic IDs to seed
  // state without inspecting presentation classes or accessible attributes.
  const expandedIds = new Set<string>();
  for (const item of document.querySelectorAll<HTMLLIElement>(
    ".document-directory[id]",
  )) {
    expandedIds.add(item.id);
  }
  return expandedIds;
}

function parseExpandedIds(value: string): Set<string> {
  try {
    const parsed: unknown = JSON.parse(value);
    if (!Array.isArray(parsed)) return new Set();

    const ids = new Set<string>();
    for (const item of parsed) {
      if (typeof item === "string") ids.add(item);
    }
    return ids;
  } catch (_) {
    return new Set();
  }
}

function writeExpandedIds(storage: Storage, expandedIds: Set<string>): void {
  writePreference(
    storage,
    expandedDirectoriesStorageKey,
    JSON.stringify(Array.from(expandedIds).sort()),
  );
}
