interface HtmxConfig {
  allowEval: boolean;
  allowNestedOobSwaps: boolean;
  allowScriptTags: boolean;
  historyCacheSize: number;
  selfRequestsOnly: boolean;
}

interface HtmxAPI {
  config: HtmxConfig;
}

declare const htmx: HtmxAPI;

interface ReviewHTMXOptions {
  panel: HTMLElement;
  token: string;
  getRevision: () => string;
  onPanelChanged: (mutationKind: ReviewMutationKind, mutation: boolean, successful: boolean) => void | Promise<void>;
  onRequestError: () => void;
}

export type ReviewMutationKind = "create" | "reattach" | "other" | null;

function reviewMutationKind(source: Element | null): ReviewMutationKind {
  const form = source instanceof HTMLFormElement ? source : source?.closest<HTMLFormElement>("form") || null;
  if (!form) return null;
  if (form.id === "annotation-form") return "create";
  if (new URL(form.action).pathname.endsWith("/reattach")) return "reattach";
  return "other";
}

interface HtmxRequestDetail {
  elt?: unknown;
  headers?: unknown;
  requestConfig?: unknown;
  shouldSwap?: unknown;
  isError?: unknown;
  target?: unknown;
  verb?: unknown;
  xhr?: unknown;
}

function detail(event: Event): HtmxRequestDetail | null {
  if (!(event instanceof CustomEvent) || typeof event.detail !== "object" || event.detail === null) return null;
  return event.detail as HtmxRequestDetail;
}

function status(detailValue: HtmxRequestDetail): number {
  if (typeof detailValue.xhr !== "object" || detailValue.xhr === null) return 0;
  const value = Reflect.get(detailValue.xhr, "status");
  return typeof value === "number" ? value : 0;
}

function requestVerb(detailValue: HtmxRequestDetail): string {
  if (typeof detailValue.verb === "string") return detailValue.verb.toLowerCase();
  if (typeof detailValue.requestConfig !== "object" || detailValue.requestConfig === null) return "";
  const value = Reflect.get(detailValue.requestConfig, "verb");
  return typeof value === "string" ? value.toLowerCase() : "";
}

function targetsPanel(event: Event, detailValue: HtmxRequestDetail): boolean {
  return (detailValue.target instanceof HTMLElement && detailValue.target.id === "annotation-panel-content")
    || (detailValue.elt instanceof HTMLElement && detailValue.elt.id === "annotation-panel-content")
    || (event.target instanceof HTMLElement && event.target.id === "annotation-panel-content");
}

export function configureReviewHTMX({ panel, token, getRevision, onPanelChanged, onRequestError }: ReviewHTMXOptions): void {
  if (typeof htmx === "undefined") throw new Error("HTMX is unavailable on a review page");
  htmx.config.allowEval = false;
  htmx.config.allowNestedOobSwaps = false;
  htmx.config.allowScriptTags = false;
  htmx.config.historyCacheSize = 0;
  htmx.config.selfRequestsOnly = true;
  let requestMutationKind: ReviewMutationKind = null;
  let requestMethod = "";

  document.body.addEventListener("htmx:configRequest", (event) => {
    const value = detail(event);
    if (!value) return;
    const requestSource = value.elt instanceof Element ? value.elt : null;
    requestMutationKind = reviewMutationKind(requestSource);
    requestMethod = requestVerb(value);
    if (requestMethod !== "post" || typeof value.headers !== "object" || value.headers === null) return;
    Reflect.set(value.headers, "X-Code-Annotator-Token", token);
    Reflect.set(value.headers, "If-Match", JSON.stringify(getRevision()));
  });

  document.body.addEventListener("htmx:beforeSwap", (event) => {
    const value = detail(event);
    if (!value || !targetsPanel(event, value) || (status(value) !== 409 && status(value) !== 422)) return;
    value.shouldSwap = true;
    value.isError = false;
  });

  document.body.addEventListener("htmx:afterSwap", (event) => {
    const value = detail(event);
    if (!value || !targetsPanel(event, value)) return;
    const responseStatus = status(value);
    void onPanelChanged(requestMutationKind, requestMethod === "post", responseStatus >= 200 && responseStatus < 300);
    requestMutationKind = null;
    requestMethod = "";
  });

  document.body.addEventListener("htmx:responseError", (event) => {
    const value = detail(event);
    if (value && targetsPanel(event, value)) onRequestError();
  });
}
