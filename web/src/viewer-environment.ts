export interface HtmxConfig {
  allowEval: boolean;
  allowNestedOobSwaps: boolean;
  allowScriptTags: boolean;
  historyCacheSize: number;
  selfRequestsOnly: boolean;
}

export interface HtmxAPI {
  config: HtmxConfig;
  ajax(
    verb: "GET",
    path: string,
    options: { target: string; swap: "outerHTML" },
  ): Promise<void>;
}

export interface ViewerEnvironment {
  document: Document;
  window: Window;
  location: Location;
  storage: Storage;
  resizeObserver: typeof ResizeObserver;
  requestAnimationFrame: (callback: FrameRequestCallback) => number;
  htmx: HtmxAPI | null;
}

function browserHTMX(): HtmxAPI | null {
  return (Reflect.get(globalThis, "htmx") as HtmxAPI | undefined) ?? null;
}

export function defaultViewerEnvironment(): ViewerEnvironment {
  return {
    document,
    window,
    location,
    storage: sessionStorage,
    resizeObserver: ResizeObserver,
    requestAnimationFrame: (callback) => window.requestAnimationFrame(callback),
    htmx: browserHTMX(),
  };
}
