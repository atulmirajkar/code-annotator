// @vitest-environment happy-dom

import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  bindDiffOverview,
  type DiffOverviewEnvironment,
} from "./diff-overview.js";

interface OverviewFixture {
  readonly environment: DiffOverviewEnvironment;
  readonly frames: Array<FrameRequestCallback>;
  readonly observer: FakeResizeObserver;
  readonly owner: HTMLElement;
  readonly ruler: HTMLElement;
  readonly markers: ReadonlyArray<HTMLAnchorElement>;
  readonly starts: ReadonlyArray<HTMLElement>;
  readonly setElementScrollOwner: (enabled: boolean) => void;
}

class FakeResizeObserver implements ResizeObserver {
  readonly observe = vi.fn();
  readonly unobserve = vi.fn();
  readonly disconnect = vi.fn();
}

class FixtureResizeObserver implements ResizeObserver {
  static next: FakeResizeObserver | null = null;

  readonly observe: ResizeObserver["observe"];
  readonly unobserve: ResizeObserver["unobserve"];
  readonly disconnect: ResizeObserver["disconnect"];

  constructor(_callback: ResizeObserverCallback) {
    const next = FixtureResizeObserver.next;
    if (!next) throw new Error("missing fixture ResizeObserver");
    this.observe = next.observe;
    this.unobserve = next.unobserve;
    this.disconnect = next.disconnect;
  }
}

describe("diff overview browser adapter", () => {
  beforeEach(() => {
    document.body.innerHTML = "";
    vi.restoreAllMocks();
  });

  it("is a no-op when the page has no diff overview", () => {
    const fixture = createFixture(false);

    expect(() => bindDiffOverview(fixture.environment)).not.toThrow();
    expect(fixture.frames).toHaveLength(0);
  });

  it("projects validated targets and marks the visible hunk current", () => {
    const fixture = createFixture();

    bindDiffOverview(fixture.environment);
    flushFrame(fixture.frames);

    expect(fixture.ruler.classList.contains("diff-overview-enhanced")).toBe(
      true,
    );
    expect(
      fixture.ruler.style.getPropertyValue("--diff-overview-track-height"),
    ).toBe("540px");
    const first = requireAt(fixture.markers, 0);
    const second = requireAt(fixture.markers, 1);
    expect(first.getAttribute("aria-current")).toBe("location");
    expect(first.classList.contains("diff-overview-current")).toBe(true);
    expect(second.hasAttribute("aria-current")).toBe(false);
    expect(fixture.observer.observe).toHaveBeenCalledWith(fixture.owner);
  });

  it("coalesces repeated owner scroll and window resize events", () => {
    const fixture = createFixture();
    bindDiffOverview(fixture.environment);
    flushFrame(fixture.frames);

    fixture.owner.dispatchEvent(new Event("scroll"));
    fixture.owner.dispatchEvent(new Event("scroll"));
    window.dispatchEvent(new Event("resize"));

    expect(fixture.frames).toHaveLength(1);
  });

  it("moves observation and scrolling to Window when responsive ownership changes", () => {
    const fixture = createFixture();
    bindDiffOverview(fixture.environment);
    flushFrame(fixture.frames);

    fixture.setElementScrollOwner(false);
    window.dispatchEvent(new Event("resize"));
    flushFrame(fixture.frames);
    expect(fixture.observer.unobserve).toHaveBeenCalledWith(fixture.owner);

    fixture.owner.dispatchEvent(new Event("scroll"));
    expect(fixture.frames).toHaveLength(0);
    window.dispatchEvent(new Event("scroll"));
    expect(fixture.frames).toHaveLength(1);
  });

  it("scrolls a marker target while retaining the focused link", () => {
    const fixture = createFixture();
    const scrollIntoView = vi.fn();
    requireAt(fixture.starts, 0).scrollIntoView = scrollIntoView;
    Object.defineProperty(window, "matchMedia", {
      configurable: true,
      value: vi.fn(() => ({ matches: true })),
    });
    bindDiffOverview(fixture.environment);
    flushFrame(fixture.frames);

    const marker = requireAt(fixture.markers, 0);
    marker.focus();
    const followed = marker.dispatchEvent(
      new MouseEvent("click", { bubbles: true, button: 0, cancelable: true }),
    );

    expect(followed).toBe(false);
    expect(scrollIntoView).toHaveBeenCalledWith({
      block: "center",
      inline: "nearest",
      behavior: "auto",
    });
    expect(document.activeElement).toBe(marker);
  });

  it("leaves native navigation untouched when a target is missing", () => {
    const fixture = createFixture();
    requireAt(fixture.starts, 0).remove();

    bindDiffOverview(fixture.environment);

    expect(fixture.frames).toHaveLength(0);
    expect(fixture.ruler.classList.contains("diff-overview-enhanced")).toBe(
      false,
    );
  });
});

function createFixture(withOverview = true): OverviewFixture {
  document.body.innerHTML = `<main class="document"><nav class="source-mode-tabs"></nav>
    <div class="diff-view"><div class="diff-column-headings"></div>
      <div class="diff-panes"><div class="diff-pane diff-base-pane"></div>
        <div class="diff-pane diff-current-pane">
          <div id="diff-change-1"></div><div id="diff-change-1-end"></div>
          <div id="diff-change-2"></div>
        </div>
        ${withOverview ? overviewMarkup() : ""}
      </div>
    </div></main>`;

  const owner = requiredElement<HTMLElement>(".document");
  const tabs = requiredElement<HTMLElement>(".source-mode-tabs");
  const headings = requiredElement<HTMLElement>(".diff-column-headings");
  const panes = requiredElement<HTMLElement>(".diff-panes");
  const currentPane = requiredElement<HTMLElement>(".diff-current-pane");
  const ruler =
    document.querySelector<HTMLElement>(".diff-overview") ??
    document.createElement("nav");
  const starts = [
    requiredElement<HTMLElement>("#diff-change-1"),
    requiredElement<HTMLElement>("#diff-change-2"),
  ];
  const end = requiredElement<HTMLElement>("#diff-change-1-end");
  stubRect(owner, 0, 600);
  stubRect(tabs, 0, 40);
  stubRect(headings, 40, 20);
  stubRect(panes, 60, 1000);
  stubRect(currentPane, 60, 1000);
  stubRect(requireAt(starts, 0), 160, 20);
  stubRect(end, 260, 20);
  stubRect(requireAt(starts, 1), 760, 20);
  Object.defineProperty(currentPane, "scrollHeight", {
    configurable: true,
    value: 1000,
  });

  let elementOwnsScroll = true;
  vi.spyOn(window, "getComputedStyle").mockImplementation(
    (element) =>
      ({
        overflowY: element === owner && elementOwnsScroll ? "auto" : "visible",
      }) as CSSStyleDeclaration,
  );
  Object.defineProperty(window, "innerHeight", {
    configurable: true,
    value: 600,
  });
  Object.defineProperty(window, "devicePixelRatio", {
    configurable: true,
    value: 1,
  });
  const frames: Array<FrameRequestCallback> = [];
  const observer = new FakeResizeObserver();
  FixtureResizeObserver.next = observer;
  const requestAnimationFrame = vi.fn((callback: FrameRequestCallback) => {
    frames.push(callback);
    return frames.length;
  });
  return {
    environment: {
      document,
      window,
      resizeObserver: FixtureResizeObserver,
      requestAnimationFrame,
    },
    frames,
    observer,
    owner,
    ruler,
    markers: Array.from(
      document.querySelectorAll<HTMLAnchorElement>(".diff-overview-marker"),
    ),
    starts,
    setElementScrollOwner: (enabled) => {
      elementOwnsScroll = enabled;
    },
  };
}

function overviewMarkup(): string {
  return `<nav class="diff-overview" aria-label="Changes in this file">
    <span class="diff-overview-viewport" aria-hidden="true"></span>
    <span class="diff-overview-item">
      <a class="diff-overview-marker" href="#diff-change-1"></a>
      <a class="diff-overview-end" href="#diff-change-1-end"></a>
    </span>
    <span class="diff-overview-item">
      <a class="diff-overview-marker" href="#diff-change-2"></a>
      <a class="diff-overview-end" href="#diff-change-2"></a>
    </span>
  </nav>`;
}

function stubRect(element: HTMLElement, top: number, height: number): void {
  vi.spyOn(element, "getBoundingClientRect").mockReturnValue(
    new DOMRect(0, top, 100, height),
  );
}

function flushFrame(frames: Array<FrameRequestCallback>): void {
  const callback = frames.shift();
  if (!callback) throw new Error("expected a queued animation frame");
  callback(0);
}

function requiredElement<T extends Element>(selector: string): T {
  const element = document.querySelector<T>(selector);
  if (!element) throw new Error(`missing test element: ${selector}`);
  return element;
}

function requireAt<T>(values: ReadonlyArray<T>, index: number): T {
  const value = values[index];
  if (value === undefined) throw new Error(`missing test value at ${index}`);
  return value;
}
