import { readPreference, writePreference } from "./browser-storage.js";

const collapsedDirectoriesStorageKey =
  "code-annotator.document-tree-collapsed";
const legacyExpandedDirectoriesStorageKey =
  "code-annotator.document-tree-expanded";

interface DocumentTreeContext {
  document: Document;
  storage: Storage;
  collapsedIds: Set<string>;
}

// Directory expansion is browser-owned presentation state. Semantic element
// IDs connect that typed state to server-rendered directories without reading
// classes or ARIA attributes back from the DOM.
export function bindDocumentTree(document: Document, storage: Storage): void {
  if (!document.querySelector("#document-panel-content")) return;
  const context: DocumentTreeContext = {
    document,
    storage,
    collapsedIds: readCollapsedIds(document, storage),
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

  const collapsed = !context.collapsedIds.has(item.id);
  if (collapsed) context.collapsedIds.add(item.id);
  else context.collapsedIds.delete(item.id);

  renderDirectory(item, button, !collapsed);
  writeCollapsedIds(context.storage, context.collapsedIds);
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
    if (button) {
      renderDirectory(item, button, !context.collapsedIds.has(item.id));
    }
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

function readCollapsedIds(document: Document, storage: Storage): Set<string> {
  const stored = readPreference(storage, collapsedDirectoriesStorageKey);
  if (stored !== null) return parseDirectoryIds(stored) ?? new Set();

  // Migrate only directories visible in the initial fragment. A directory
  // absent from that filtered fragment has no known legacy preference and
  // therefore keeps the new default-expanded behavior when later revealed.
  const legacyStored = readPreference(
    storage,
    legacyExpandedDirectoriesStorageKey,
  );
  const legacyExpandedIds = legacyStored
    ? parseDirectoryIds(legacyStored)
    : null;
  if (!legacyExpandedIds) return new Set();

  const collapsedIds = new Set<string>();
  for (const item of document.querySelectorAll<HTMLLIElement>(
    ".document-directory[id]",
  )) {
    if (!legacyExpandedIds.has(item.id)) collapsedIds.add(item.id);
  }
  writeCollapsedIds(storage, collapsedIds);
  return collapsedIds;
}

function parseDirectoryIds(value: string): Set<string> | null {
  try {
    const parsed: unknown = JSON.parse(value);
    if (!Array.isArray(parsed)) return null;

    const ids = new Set<string>();
    for (const item of parsed) {
      if (typeof item === "string") ids.add(item);
    }
    return ids;
  } catch (_) {
    return null;
  }
}

function writeCollapsedIds(storage: Storage, collapsedIds: Set<string>): void {
  writePreference(
    storage,
    collapsedDirectoriesStorageKey,
    JSON.stringify(Array.from(collapsedIds).sort()),
  );
}
