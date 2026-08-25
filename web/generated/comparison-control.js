// Comparison choices are server-rendered and validated. The browser supplies
// submission feedback and the loopback-only mutation token.
export function bindComparisonControl(document) {
    const control = document.querySelector(".diff-comparison-control");
    const token = document.querySelector('meta[name="code-annotator-comparison-token"]')?.content ?? "";
    const selector = control?.querySelector(".revision-selector");
    const status = control?.querySelector(".diff-comparison-status");
    if (!control || !token || !selector || !status)
        return;
    const context = { control, status, token };
    selector.addEventListener("change", () => handleComparisonChange(context));
    document.body.addEventListener("htmx:configRequest", (event) => handleComparisonConfigRequest(context, event));
    document.body.addEventListener("htmx:responseError", (event) => handleComparisonResponseError(context, event));
}
function handleComparisonChange(context) {
    context.status.textContent = "Updating comparison base…";
    context.status.classList.remove("error");
    context.control.requestSubmit();
}
function handleComparisonConfigRequest(context, event) {
    const detail = customEventDetail(event);
    if (!detail || !eventTargetsControl(detail, context.control))
        return;
    const headers = Reflect.get(detail, "headers");
    if (typeof headers === "object" && headers !== null) {
        Reflect.set(headers, "X-Code-Annotator-Comparison-Token", context.token);
    }
}
function handleComparisonResponseError(context, event) {
    const detail = customEventDetail(event);
    if (!detail || !eventTargetsControl(detail, context.control))
        return;
    context.status.textContent = "The Git comparison could not be updated.";
    context.status.classList.add("error");
}
// HTMX event detail is untyped external input. Narrow it before inspecting the
// request source or headers.
function customEventDetail(event) {
    return event instanceof CustomEvent &&
        typeof event.detail === "object" &&
        event.detail !== null
        ? event.detail
        : null;
}
function eventTargetsControl(detail, control) {
    const source = Reflect.get(detail, "elt");
    return (source instanceof Element &&
        source.closest(".diff-comparison-control") === control);
}
