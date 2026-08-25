import { readPreference, writePreference } from "./browser-storage.js";
import { clampInteger } from "./viewer-preferences.js";
const diffSplitStorageKey = "code-annotator.diff-split";
const diffSplitMin = 20;
const diffSplitMax = 80;
const diffSplitStep = 2;
// One context owns the active drag and persisted split. Headings and panes use
// the same CSS custom property, keeping both grids aligned.
export function bindDiffDivider(document, storage) {
    const view = document.querySelector(".diff-view");
    const divider = view?.querySelector(".diff-divider");
    if (!view || !divider)
        return;
    const context = {
        document,
        storage,
        view,
        divider,
        percent: clampDiffSplit(readDiffSplitPreference(storage)),
        dragRect: null,
        pointerMoveHandler: null,
    };
    context.pointerMoveHandler = (event) => handleDiffPointerMove(context, event);
    renderDiffSplit(context);
    divider.addEventListener("keydown", (event) => handleDiffDividerKeydown(context, event));
    divider.addEventListener("pointerdown", (event) => handleDiffPointerDown(context, event));
}
function handleDiffDividerKeydown(context, event) {
    let next;
    if (event.key === "ArrowLeft")
        next = context.percent - diffSplitStep;
    else if (event.key === "ArrowRight")
        next = context.percent + diffSplitStep;
    else if (event.key === "Home")
        next = diffSplitMin;
    else if (event.key === "End")
        next = diffSplitMax;
    else
        return;
    event.preventDefault();
    setDiffSplit(context, next);
}
// Capture bounds once per drag and keep a stable move-handler reference so the
// pointer-up path can reliably unregister it.
function handleDiffPointerDown(context, event) {
    if (event.button !== 0)
        return;
    event.preventDefault();
    context.dragRect = context.view.getBoundingClientRect();
    if (context.pointerMoveHandler) {
        context.document.addEventListener("pointermove", context.pointerMoveHandler);
    }
    context.document.addEventListener("pointerup", () => handleDiffPointerUp(context), { once: true });
}
function handleDiffPointerMove(context, event) {
    if (!context.dragRect)
        return;
    const percent = ((event.clientX - context.dragRect.left) / context.dragRect.width) * 100;
    setDiffSplit(context, percent);
}
function handleDiffPointerUp(context) {
    context.dragRect = null;
    if (context.pointerMoveHandler) {
        context.document.removeEventListener("pointermove", context.pointerMoveHandler);
    }
}
function setDiffSplit(context, value) {
    context.percent = clampDiffSplit(value);
    renderDiffSplit(context);
    writePreference(context.storage, diffSplitStorageKey, String(context.percent));
}
function renderDiffSplit(context) {
    context.view.style.setProperty("--diff-split", `${context.percent}%`);
    context.divider.setAttribute("aria-valuenow", String(context.percent));
}
function clampDiffSplit(value) {
    return clampInteger(value, diffSplitMin, diffSplitMax);
}
function readDiffSplitPreference(storage) {
    const stored = Number.parseFloat(readPreference(storage, diffSplitStorageKey) ?? "");
    return Number.isFinite(stored) ? stored : 50;
}
