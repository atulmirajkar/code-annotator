import { readPreference, writePreference } from "./browser-storage.js";

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

const sourceModeStorageKey = "code-annotator.source-mode";
const panelStoragePrefix = "code-annotator.panel-collapsed.";

// Bind the viewer shell independently from document, comparison, and review
// behavior. The server still owns the shell's initial HTML.
export function bindViewerLayout(
  document: Document,
  storage: Storage,
  ResizeObserver: typeof globalThis.ResizeObserver,
  layout: HTMLElement,
): void {
  bindTopbarHeight(document, ResizeObserver);
  bindPanelToggle({
    button: document.querySelector(".documents-toggle"),
    panel: document.querySelector("#documents-sidebar"),
    layout,
    storage,
    collapsedClass: "documents-collapsed",
    name: "documents",
  });
  bindPanelToggle({
    button: document.querySelector(".review-toggle"),
    panel: document.querySelector("#annotation-sidebar"),
    layout,
    storage,
    collapsedClass: "review-collapsed",
    name: "annotations",
    defaultCollapsed: true,
  });
  bindSourceModePreference(document, storage);
}

// CSS uses the measured topbar height to keep sticky content below controls
// whose height changes when they wrap at narrower viewport widths.
function bindTopbarHeight(
  document: Document,
  ResizeObserver: typeof globalThis.ResizeObserver,
): void {
  const topbar = document.querySelector<HTMLElement>(".topbar");
  if (!topbar) return;
  updateTopbarHeight(document, topbar);
  const observer = new ResizeObserver(() =>
    updateTopbarHeight(document, topbar),
  );
  observer.observe(topbar);
}

function updateTopbarHeight(document: Document, topbar: HTMLElement): void {
  document.documentElement.style.setProperty(
    "--topbar-height",
    `${topbar.getBoundingClientRect().height}px`,
  );
}

// One context keeps the panel's visual, accessible, layout, and preference
// representations synchronized.
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
  options.button.addEventListener("click", () => {
    const panelID = context.button.getAttribute("aria-controls");
    const currentPanel = panelID
      ? context.button.ownerDocument.getElementById(panelID)
      : null;
    if (currentPanel) context.panel = currentPanel;
    handlePanelToggle(context);
  });
}

function handlePanelToggle(context: PanelToggleContext): void {
  const collapsed = !context.layout.classList.contains(context.collapsedClass);
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

// Source links are server-rendered in the correct mode. This adapter remembers
// only the user's explicit mode choice for later navigation.
function bindSourceModePreference(document: Document, storage: Storage): void {
  const tabs = document.querySelector<HTMLElement>(".source-mode-tabs");
  const activeTab = tabs?.querySelector<HTMLAnchorElement>(
    'a[aria-current="page"]',
  );
  if (activeTab) persistSourceMode(storage, activeTab);
  document.addEventListener("click", (event) => {
    const target = event.target instanceof Element ? event.target : null;
    const tab = target?.closest<HTMLAnchorElement>(".source-mode-tabs a");
    if (tab) persistSourceMode(storage, tab);
  });
}

function persistSourceMode(storage: Storage, tab: HTMLAnchorElement): void {
  const mode =
    new URL(tab.href).searchParams.get("mode") === "diff" ? "diff" : "file";
  writePreference(storage, sourceModeStorageKey, mode);
}

function readPanelCollapsedPreference(
  storage: Storage,
  name: string,
  defaultCollapsed: boolean,
): boolean {
  const stored = readPreference(storage, `${panelStoragePrefix}${name}`);
  return stored === null ? defaultCollapsed : stored === "true";
}
