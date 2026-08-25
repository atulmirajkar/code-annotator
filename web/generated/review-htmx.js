function reviewMutationKind(source) {
    const form = source instanceof HTMLFormElement
        ? source
        : source?.closest("form") || null;
    if (!form)
        return null;
    if (form.id === "annotation-form")
        return "create";
    if (new URL(form.action).pathname.endsWith("/reattach"))
        return "reattach";
    return "other";
}
function detail(event) {
    if (!(event instanceof CustomEvent) ||
        typeof event.detail !== "object" ||
        event.detail === null)
        return null;
    return event.detail;
}
function status(detailValue) {
    if (typeof detailValue.xhr !== "object" || detailValue.xhr === null)
        return 0;
    const value = Reflect.get(detailValue.xhr, "status");
    return typeof value === "number" ? value : 0;
}
function requestVerb(detailValue) {
    if (typeof detailValue.verb === "string")
        return detailValue.verb.toLowerCase();
    if (typeof detailValue.requestConfig !== "object" ||
        detailValue.requestConfig === null)
        return "";
    const value = Reflect.get(detailValue.requestConfig, "verb");
    return typeof value === "string" ? value.toLowerCase() : "";
}
function targetsPanel(event, detailValue) {
    return ((detailValue.target instanceof HTMLElement &&
        detailValue.target.id === "annotation-panel-content") ||
        (detailValue.elt instanceof HTMLElement &&
            detailValue.elt.id === "annotation-panel-content") ||
        (event.target instanceof HTMLElement &&
            event.target.id === "annotation-panel-content"));
}
export function configureReviewHTMX({ document, api, panel, token, getRevision, onPanelChanged, onRequestError, }) {
    if (!api)
        throw new Error("HTMX is unavailable on a review page");
    api.config.allowEval = false;
    api.config.allowNestedOobSwaps = false;
    api.config.allowScriptTags = false;
    api.config.historyCacheSize = 0;
    api.config.selfRequestsOnly = true;
    let requestMutationKind = null;
    let requestMethod = "";
    document.body.addEventListener("htmx:configRequest", (event) => {
        const value = detail(event);
        if (!value)
            return;
        const requestSource = value.elt instanceof Element ? value.elt : null;
        requestMutationKind = reviewMutationKind(requestSource);
        requestMethod = requestVerb(value);
        if (requestMethod !== "post" ||
            typeof value.headers !== "object" ||
            value.headers === null)
            return;
        Reflect.set(value.headers, "X-Code-Annotator-Token", token);
        Reflect.set(value.headers, "If-Match", JSON.stringify(getRevision()));
    });
    document.body.addEventListener("htmx:beforeSwap", (event) => {
        const value = detail(event);
        if (!value ||
            !targetsPanel(event, value) ||
            (status(value) !== 409 && status(value) !== 422))
            return;
        value.shouldSwap = true;
        value.isError = false;
    });
    document.body.addEventListener("htmx:afterSwap", (event) => {
        const value = detail(event);
        if (!value || !targetsPanel(event, value))
            return;
        const responseStatus = status(value);
        void onPanelChanged(requestMutationKind, requestMethod === "post", responseStatus >= 200 && responseStatus < 300);
        requestMutationKind = null;
        requestMethod = "";
    });
    document.body.addEventListener("htmx:responseError", (event) => {
        const value = detail(event);
        if (value && targetsPanel(event, value))
            onRequestError();
    });
}
