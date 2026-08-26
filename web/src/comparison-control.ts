interface ComparisonControlContext {
  control: HTMLFormElement;
  status: HTMLElement;
  token: string;
}

let activeBindings: AbortController | null = null;

// Comparison choices are server-rendered and validated. The browser supplies
// submission feedback and the loopback-only mutation token.
export function bindComparisonControl(document: Document): void {
  activeBindings?.abort();
  activeBindings = null;
  const control = document.querySelector<HTMLFormElement>(
    ".diff-comparison-control",
  );
  const token =
    document.querySelector<HTMLMetaElement>(
      'meta[name="code-annotator-comparison-token"]',
    )?.content ?? "";
  const selector =
    control?.querySelector<HTMLSelectElement>(".revision-selector");
  const status = control?.querySelector<HTMLElement>(".diff-comparison-status");
  if (!control || !token || !selector || !status) return;
  const context: ComparisonControlContext = { control, status, token };
  const bindings = new AbortController();
  activeBindings = bindings;

  selector.addEventListener("change", () => handleComparisonChange(context), {
    signal: bindings.signal,
  });
  document.body.addEventListener("htmx:configRequest", (event) =>
    handleComparisonConfigRequest(context, event), { signal: bindings.signal },
  );
  document.body.addEventListener("htmx:responseError", (event) =>
    handleComparisonResponseError(context, event), { signal: bindings.signal },
  );
}

function handleComparisonChange(context: ComparisonControlContext): void {
  context.status.textContent = "Updating comparison base…";
  context.status.classList.remove("error");
  context.control.requestSubmit();
}

function handleComparisonConfigRequest(
  context: ComparisonControlContext,
  event: Event,
): void {
  const detail = customEventDetail(event);
  if (!detail || !eventTargetsControl(detail, context.control)) return;
  const headers = Reflect.get(detail, "headers");
  if (typeof headers === "object" && headers !== null) {
    Reflect.set(headers, "X-Code-Annotator-Comparison-Token", context.token);
  }
}

function handleComparisonResponseError(
  context: ComparisonControlContext,
  event: Event,
): void {
  const detail = customEventDetail(event);
  if (!detail || !eventTargetsControl(detail, context.control)) return;
  context.status.textContent = "The Git comparison could not be updated.";
  context.status.classList.add("error");
}

// HTMX event detail is untyped external input. Narrow it before inspecting the
// request source or headers.
function customEventDetail(event: Event): object | null {
  return event instanceof CustomEvent &&
    typeof event.detail === "object" &&
    event.detail !== null
    ? event.detail
    : null;
}

function eventTargetsControl(
  detail: object,
  control: HTMLFormElement,
): boolean {
  const source = Reflect.get(detail, "elt");
  return (
    source instanceof Element &&
    source.closest(".diff-comparison-control") === control
  );
}
