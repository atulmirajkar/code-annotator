import {
  layoutDiffOverviewMarkers,
  layoutDiffOverviewViewport,
  selectDiffOverviewLocation,
  type DiffOverviewMarkerLayout,
  type DiffOverviewRange,
} from "./diff-overview-geometry.js";

export interface DiffOverviewEnvironment {
  readonly document: Document;
  readonly window: Window;
  readonly resizeObserver: typeof ResizeObserver;
  readonly requestAnimationFrame: (callback: FrameRequestCallback) => number;
}

interface DiffOverviewItem {
  readonly container: HTMLElement;
  readonly marker: HTMLAnchorElement;
  readonly start: HTMLElement;
  readonly end: HTMLElement;
}

interface DiffOverviewContext extends DiffOverviewEnvironment {
  readonly view: HTMLElement;
  readonly panes: HTMLElement;
  readonly currentPane: HTMLElement;
  readonly headings: HTMLElement;
  readonly ruler: HTMLElement;
  readonly viewport: HTMLElement;
  readonly items: ReadonlyArray<DiffOverviewItem>;
  observer: ResizeObserver | null;
  scrollOwner: HTMLElement | Window | null;
  scrollHandler: (() => void) | null;
  framePending: boolean;
  enhanced: boolean;
}

interface VerticalBounds {
  readonly top: number;
  readonly bottom: number;
}

const minimumMarkerHeight = 3;
const markerGap = 1;
const minimumViewportHeight = 4;
let cleanupActiveOverview: (() => void) | null = null;

// The server owns hunk identity and link order. This component validates that
// contract once, then owns only measurements and browser interaction state.
export function bindDiffOverview(environment: DiffOverviewEnvironment): void {
  cleanupActiveOverview?.();
  cleanupActiveOverview = null;
  const ruler =
    environment.document.querySelector<HTMLElement>(".diff-overview");
  if (!ruler) return;

  const view = ruler.closest<HTMLElement>(".diff-view");
  const panes = ruler.closest<HTMLElement>(".diff-panes");
  const currentPane = panes?.querySelector<HTMLElement>(".diff-current-pane");
  const headings = view?.querySelector<HTMLElement>(".diff-column-headings");
  const viewport = ruler.querySelector<HTMLElement>(".diff-overview-viewport");
  if (!view || !panes || !currentPane || !headings || !viewport) return;

  const items = resolveOverviewItems(environment.document, ruler, currentPane);
  if (!items || items.length === 0) return;

  const context: DiffOverviewContext = {
    ...environment,
    view,
    panes,
    currentPane,
    headings,
    ruler,
    viewport,
    items,
    observer: null,
    scrollOwner: null,
    scrollHandler: null,
    framePending: false,
    enhanced: false,
  };

  // These callbacks need the completed context, so their stable identities are
  // assigned immediately after construction and retained for listener removal.
  const observer = new environment.resizeObserver(() =>
    scheduleUpdate(context),
  );
  context.observer = observer;
  context.scrollHandler = () => scheduleUpdate(context);
  observer.observe(view);
  observer.observe(ruler);
  const handleResize = () => scheduleUpdate(context);
  environment.window.addEventListener("resize", handleResize);
  for (const item of items) {
    item.marker.addEventListener("click", (event) =>
      activateMarker(context, item, event),
    );
  }
  cleanupActiveOverview = () => {
    observer.disconnect();
    environment.window.removeEventListener("resize", handleResize);
    if (context.scrollOwner && context.scrollHandler) {
      context.scrollOwner.removeEventListener("scroll", context.scrollHandler);
    }
  };
  scheduleUpdate(context);
}

function resolveOverviewItems(
  document: Document,
  ruler: HTMLElement,
  currentPane: HTMLElement,
): ReadonlyArray<DiffOverviewItem> | null {
  const containers = ruler.querySelectorAll<HTMLElement>(".diff-overview-item");
  const items: Array<DiffOverviewItem> = [];
  for (const container of containers) {
    const marker = container.querySelector<HTMLAnchorElement>(
      ".diff-overview-marker",
    );
    const endLink =
      container.querySelector<HTMLAnchorElement>(".diff-overview-end");
    if (!marker || !endLink) return null;

    const start = resolveUniqueTarget(document, currentPane, marker);
    const end = resolveUniqueTarget(document, currentPane, endLink);
    if (!start || !end) return null;
    items.push({ container, marker, start, end });
  }
  return items;
}

// Fragment links remain ordinary navigation when this validation fails. Exact
// ID matching avoids selector escaping and rejects duplicate server identities.
function resolveUniqueTarget(
  document: Document,
  currentPane: HTMLElement,
  link: HTMLAnchorElement,
): HTMLElement | null {
  const href = link.getAttribute("href");
  if (!href?.startsWith("#") || href.length === 1) return null;

  let id: string;
  try {
    id = decodeURIComponent(href.slice(1));
  } catch {
    return null;
  }
  const matches = Array.from(
    document.querySelectorAll<HTMLElement>("[id]"),
  ).filter((element) => element.id === id);
  const target = matches[0];
  if (matches.length !== 1 || !target || !currentPane.contains(target)) {
    return null;
  }
  return target;
}

function scheduleUpdate(context: DiffOverviewContext): void {
  if (context.framePending) return;
  context.framePending = true;
  context.requestAnimationFrame(() => {
    context.framePending = false;
    updateOverview(context);
  });
}

function updateOverview(context: DiffOverviewContext): void {
  updateScrollOwner(context, findVerticalScrollOwner(context));
  const owner = context.scrollOwner;
  if (!owner) return;

  try {
    const paneRect = context.currentPane.getBoundingClientRect();
    const panesRect = context.panes.getBoundingClientRect();
    const headingsRect = context.headings.getBoundingClientRect();
    const ownerBounds = verticalBounds(context.window, owner);
    const headingTop = stickyHeadingTop(context, ownerBounds);
    const rulerTop = Math.max(
      panesRect.top,
      ownerBounds.top + headingTop + headingsRect.height,
    );
    const rulerBottom = Math.min(panesRect.bottom, ownerBounds.bottom);
    const trackHeight = rulerBottom - rulerTop;
    const contentHeight = Math.max(
      context.currentPane.scrollHeight,
      paneRect.height,
    );
    if (trackHeight <= 0 || contentHeight <= 0) {
      disableEnhancement(context);
      return;
    }

    const ranges = measureRanges(context.items, paneRect.top, contentHeight);
    const markers = layoutDiffOverviewMarkers(ranges, {
      contentHeight,
      trackHeight,
      minimumMarkerHeight,
      markerGap,
      devicePixelRatio: Math.max(1, context.window.devicePixelRatio),
    });
    const visibleStart = clamp(rulerTop - paneRect.top, 0, contentHeight);
    const visibleEnd = clamp(
      rulerBottom - paneRect.top,
      visibleStart,
      contentHeight,
    );
    const viewport = layoutDiffOverviewViewport({
      contentHeight,
      trackHeight,
      visibleStart,
      visibleEnd,
      minimumHeight: minimumViewportHeight,
    });

    context.view.style.setProperty(
      "--diff-heading-sticky-top",
      `${headingTop}px`,
    );
    context.view.style.setProperty(
      "--diff-overview-sticky-top",
      `${headingTop + headingsRect.height}px`,
    );
    context.ruler.style.setProperty(
      "--diff-overview-track-height",
      `${trackHeight}px`,
    );
    context.viewport.style.setProperty(
      "--diff-overview-viewport-top",
      `${viewport.top}px`,
    );
    context.viewport.style.setProperty(
      "--diff-overview-viewport-height",
      `${viewport.height}px`,
    );
    renderMarkers(context.items, markers);
    renderLocation(
      context.items,
      selectDiffOverviewLocation(ranges, visibleStart, visibleEnd),
    );
    context.ruler.classList.add("diff-overview-enhanced");
    context.enhanced = true;
  } catch {
    // A stale or unusable measurement disables interception but leaves every
    // server-rendered link available as ordinary same-page navigation.
    disableEnhancement(context);
  }
}

function findVerticalScrollOwner(
  context: DiffOverviewContext,
): HTMLElement | Window {
  for (
    let candidate = context.view.parentElement;
    candidate;
    candidate = candidate.parentElement
  ) {
    const overflowY = context.window.getComputedStyle(candidate).overflowY;
    if (/^(auto|scroll|overlay)$/.test(overflowY)) return candidate;
  }
  return context.window;
}

// Responsive layout can transfer vertical scrolling between `.document` and
// Window. Keep exactly one scroll listener and observed element as ownership
// changes instead of duplicating the CSS breakpoint in TypeScript.
function updateScrollOwner(
  context: DiffOverviewContext,
  next: HTMLElement | Window,
): void {
  if (context.scrollOwner === next) return;
  const scrollHandler = context.scrollHandler;
  const observer = context.observer;
  if (!scrollHandler || !observer) return;
  if (context.scrollOwner) {
    context.scrollOwner.removeEventListener("scroll", scrollHandler);
    if (!isWindow(context.scrollOwner, context.window)) {
      observer.unobserve(context.scrollOwner);
    }
  }
  context.scrollOwner = next;
  next.addEventListener("scroll", scrollHandler);
  if (!isWindow(next, context.window)) observer.observe(next);
}

function verticalBounds(
  window: Window,
  owner: HTMLElement | Window,
): VerticalBounds {
  if (isWindow(owner, window)) {
    return { top: 0, bottom: window.innerHeight };
  }
  const rect = owner.getBoundingClientRect();
  return { top: rect.top, bottom: rect.bottom };
}

function isWindow(
  owner: HTMLElement | Window,
  target: Window,
): owner is Window {
  return owner === target;
}

function stickyHeadingTop(
  context: DiffOverviewContext,
  ownerBounds: VerticalBounds,
): number {
  const tabs = context.document.querySelector<HTMLElement>(".source-mode-tabs");
  if (!tabs) return 0;
  return clamp(
    tabs.getBoundingClientRect().bottom - ownerBounds.top,
    0,
    ownerBounds.bottom - ownerBounds.top,
  );
}

function measureRanges(
  items: ReadonlyArray<DiffOverviewItem>,
  paneTop: number,
  contentHeight: number,
): ReadonlyArray<DiffOverviewRange> {
  return items.map((item) => {
    const start = clamp(
      item.start.getBoundingClientRect().top - paneTop,
      0,
      contentHeight,
    );
    const end = clamp(
      item.end.getBoundingClientRect().bottom - paneTop,
      start,
      contentHeight,
    );
    return { id: item.start.id, start, end };
  });
}

function renderMarkers(
  items: ReadonlyArray<DiffOverviewItem>,
  markers: ReadonlyArray<DiffOverviewMarkerLayout>,
): void {
  for (let index = 0; index < items.length; index += 1) {
    const item = requireAt(items, index);
    const marker = requireAt(markers, index);
    item.container.style.setProperty(
      "--diff-overview-marker-top",
      `${marker.top}px`,
    );
    item.container.style.setProperty(
      "--diff-overview-marker-height",
      `${marker.height}px`,
    );
    item.container.classList.toggle(
      "diff-overview-density",
      marker.densityGroup !== null,
    );
    if (marker.densityCount > 1) {
      item.container.setAttribute(
        "title",
        `${marker.densityCount} changes in this area`,
      );
    } else {
      item.container.removeAttribute("title");
    }
  }
}

function renderLocation(
  items: ReadonlyArray<DiffOverviewItem>,
  location: ReturnType<typeof selectDiffOverviewLocation>,
): void {
  for (const item of items) {
    const state = item.start.id === location?.id ? location.state : null;
    item.marker.classList.toggle("diff-overview-current", state === "current");
    item.marker.classList.toggle("diff-overview-next", state === "next");
    if (state) item.marker.setAttribute("aria-current", "location");
    else item.marker.removeAttribute("aria-current");
  }
}

function activateMarker(
  context: DiffOverviewContext,
  item: DiffOverviewItem,
  event: MouseEvent,
): void {
  if (
    !context.enhanced ||
    event.defaultPrevented ||
    event.button !== 0 ||
    event.metaKey ||
    event.ctrlKey ||
    event.shiftKey ||
    event.altKey
  ) {
    return;
  }
  event.preventDefault();
  item.start.scrollIntoView({
    block: "center",
    inline: "nearest",
    behavior: context.window.matchMedia("(prefers-reduced-motion: reduce)")
      .matches
      ? "auto"
      : "smooth",
  });
}

function disableEnhancement(context: DiffOverviewContext): void {
  context.enhanced = false;
  context.ruler.classList.remove("diff-overview-enhanced");
  renderLocation(context.items, null);
}

function requireAt<T>(values: ReadonlyArray<T>, index: number): T {
  const value = values[index];
  if (value === undefined) throw new RangeError(`missing value at ${index}`);
  return value;
}

function clamp(value: number, minimum: number, maximum: number): number {
  return Math.min(maximum, Math.max(minimum, value));
}
