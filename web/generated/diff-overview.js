import { layoutDiffOverviewMarkers, layoutDiffOverviewViewport, selectDiffOverviewLocation, } from "./diff-overview-geometry.js";
const minimumMarkerHeight = 3;
const markerGap = 1;
const minimumViewportHeight = 4;
let cleanupActiveOverview = null;
// The server owns hunk identity and link order. This component validates that
// contract once, then owns only measurements and browser interaction state.
export function bindDiffOverview(environment) {
    cleanupActiveOverview?.();
    cleanupActiveOverview = null;
    const ruler = environment.document.querySelector(".diff-overview");
    if (!ruler)
        return;
    const view = ruler.closest(".diff-view");
    const panes = ruler.closest(".diff-panes");
    const currentPane = panes?.querySelector(".diff-current-pane");
    const headings = view?.querySelector(".diff-column-headings");
    const viewport = ruler.querySelector(".diff-overview-viewport");
    if (!view || !panes || !currentPane || !headings || !viewport)
        return;
    const items = resolveOverviewItems(environment.document, ruler, currentPane);
    if (!items || items.length === 0)
        return;
    const context = {
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
    const observer = new environment.resizeObserver(() => scheduleUpdate(context));
    context.observer = observer;
    context.scrollHandler = () => scheduleUpdate(context);
    observer.observe(view);
    observer.observe(ruler);
    const handleResize = () => scheduleUpdate(context);
    environment.window.addEventListener("resize", handleResize);
    for (const item of items) {
        item.marker.addEventListener("click", (event) => activateMarker(context, item, event));
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
function resolveOverviewItems(document, ruler, currentPane) {
    const containers = ruler.querySelectorAll(".diff-overview-item");
    const items = [];
    for (const container of containers) {
        const marker = container.querySelector(".diff-overview-marker");
        const endLink = container.querySelector(".diff-overview-end");
        if (!marker || !endLink)
            return null;
        const start = resolveUniqueTarget(document, currentPane, marker);
        const end = resolveUniqueTarget(document, currentPane, endLink);
        if (!start || !end)
            return null;
        items.push({ container, marker, start, end });
    }
    return items;
}
// Fragment links remain ordinary navigation when this validation fails. Exact
// ID matching avoids selector escaping and rejects duplicate server identities.
function resolveUniqueTarget(document, currentPane, link) {
    const href = link.getAttribute("href");
    if (!href?.startsWith("#") || href.length === 1)
        return null;
    let id;
    try {
        id = decodeURIComponent(href.slice(1));
    }
    catch {
        return null;
    }
    const matches = Array.from(document.querySelectorAll("[id]")).filter((element) => element.id === id);
    const target = matches[0];
    if (matches.length !== 1 || !target || !currentPane.contains(target)) {
        return null;
    }
    return target;
}
function scheduleUpdate(context) {
    if (context.framePending)
        return;
    context.framePending = true;
    context.requestAnimationFrame(() => {
        context.framePending = false;
        updateOverview(context);
    });
}
function updateOverview(context) {
    updateScrollOwner(context, findVerticalScrollOwner(context));
    const owner = context.scrollOwner;
    if (!owner)
        return;
    try {
        const paneRect = context.currentPane.getBoundingClientRect();
        const panesRect = context.panes.getBoundingClientRect();
        const headingsRect = context.headings.getBoundingClientRect();
        const ownerBounds = verticalBounds(context.window, owner);
        const headingTop = stickyHeadingTop(context, ownerBounds);
        const rulerTop = Math.max(panesRect.top, ownerBounds.top + headingTop + headingsRect.height);
        const rulerBottom = Math.min(panesRect.bottom, ownerBounds.bottom);
        const trackHeight = rulerBottom - rulerTop;
        const contentHeight = Math.max(context.currentPane.scrollHeight, paneRect.height);
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
        const visibleEnd = clamp(rulerBottom - paneRect.top, visibleStart, contentHeight);
        const viewport = layoutDiffOverviewViewport({
            contentHeight,
            trackHeight,
            visibleStart,
            visibleEnd,
            minimumHeight: minimumViewportHeight,
        });
        context.view.style.setProperty("--diff-heading-sticky-top", `${headingTop}px`);
        context.view.style.setProperty("--diff-overview-sticky-top", `${headingTop + headingsRect.height}px`);
        context.ruler.style.setProperty("--diff-overview-track-height", `${trackHeight}px`);
        context.viewport.style.setProperty("--diff-overview-viewport-top", `${viewport.top}px`);
        context.viewport.style.setProperty("--diff-overview-viewport-height", `${viewport.height}px`);
        renderMarkers(context.items, markers);
        renderLocation(context.items, selectDiffOverviewLocation(ranges, visibleStart, visibleEnd));
        context.ruler.classList.add("diff-overview-enhanced");
        context.enhanced = true;
    }
    catch {
        // A stale or unusable measurement disables interception but leaves every
        // server-rendered link available as ordinary same-page navigation.
        disableEnhancement(context);
    }
}
function findVerticalScrollOwner(context) {
    for (let candidate = context.view.parentElement; candidate; candidate = candidate.parentElement) {
        const overflowY = context.window.getComputedStyle(candidate).overflowY;
        if (/^(auto|scroll|overlay)$/.test(overflowY))
            return candidate;
    }
    return context.window;
}
// Responsive layout can transfer vertical scrolling between `.document` and
// Window. Keep exactly one scroll listener and observed element as ownership
// changes instead of duplicating the CSS breakpoint in TypeScript.
function updateScrollOwner(context, next) {
    if (context.scrollOwner === next)
        return;
    const scrollHandler = context.scrollHandler;
    const observer = context.observer;
    if (!scrollHandler || !observer)
        return;
    if (context.scrollOwner) {
        context.scrollOwner.removeEventListener("scroll", scrollHandler);
        if (!isWindow(context.scrollOwner, context.window)) {
            observer.unobserve(context.scrollOwner);
        }
    }
    context.scrollOwner = next;
    next.addEventListener("scroll", scrollHandler);
    if (!isWindow(next, context.window))
        observer.observe(next);
}
function verticalBounds(window, owner) {
    if (isWindow(owner, window)) {
        return { top: 0, bottom: window.innerHeight };
    }
    const rect = owner.getBoundingClientRect();
    return { top: rect.top, bottom: rect.bottom };
}
function isWindow(owner, target) {
    return owner === target;
}
function stickyHeadingTop(context, ownerBounds) {
    const tabs = context.document.querySelector(".source-mode-tabs");
    if (!tabs)
        return 0;
    return clamp(tabs.getBoundingClientRect().bottom - ownerBounds.top, 0, ownerBounds.bottom - ownerBounds.top);
}
function measureRanges(items, paneTop, contentHeight) {
    return items.map((item) => {
        const start = clamp(item.start.getBoundingClientRect().top - paneTop, 0, contentHeight);
        const end = clamp(item.end.getBoundingClientRect().bottom - paneTop, start, contentHeight);
        return { id: item.start.id, start, end };
    });
}
function renderMarkers(items, markers) {
    for (let index = 0; index < items.length; index += 1) {
        const item = requireAt(items, index);
        const marker = requireAt(markers, index);
        item.container.style.setProperty("--diff-overview-marker-top", `${marker.top}px`);
        item.container.style.setProperty("--diff-overview-marker-height", `${marker.height}px`);
        item.container.classList.toggle("diff-overview-density", marker.densityGroup !== null);
        if (marker.densityCount > 1) {
            item.container.setAttribute("title", `${marker.densityCount} changes in this area`);
        }
        else {
            item.container.removeAttribute("title");
        }
    }
}
function renderLocation(items, location) {
    for (const item of items) {
        const state = item.start.id === location?.id ? location.state : null;
        item.marker.classList.toggle("diff-overview-current", state === "current");
        item.marker.classList.toggle("diff-overview-next", state === "next");
        if (state)
            item.marker.setAttribute("aria-current", "location");
        else
            item.marker.removeAttribute("aria-current");
    }
}
function activateMarker(context, item, event) {
    if (!context.enhanced ||
        event.defaultPrevented ||
        event.button !== 0 ||
        event.metaKey ||
        event.ctrlKey ||
        event.shiftKey ||
        event.altKey) {
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
function disableEnhancement(context) {
    context.enhanced = false;
    context.ruler.classList.remove("diff-overview-enhanced");
    renderLocation(context.items, null);
}
function requireAt(values, index) {
    const value = values[index];
    if (value === undefined)
        throw new RangeError(`missing value at ${index}`);
    return value;
}
function clamp(value, minimum, maximum) {
    return Math.min(maximum, Math.max(minimum, value));
}
