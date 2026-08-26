import { readPreference, writePreference } from "./browser-storage.js";
const shortcutsEnabledStorageKey = "code-annotator.global-shortcuts-enabled";
const leaderTimeoutMs = 1000;
// Elements a click, focus, or contenteditable state on which must suppress
// every global shortcut, per WCAG 2.2 SC 2.1.4: native form controls, links,
// disclosure summaries, and widgets carrying an interactive ARIA role.
const interactiveTargetSelector = [
    "input",
    "textarea",
    "select",
    "button",
    "a[href]",
    "summary",
    '[role="button"]',
    '[role="link"]',
    '[role="checkbox"]',
    '[role="radio"]',
    '[role="switch"]',
    '[role="combobox"]',
    '[role="listbox"]',
    '[role="menu"]',
    '[role="menuitem"]',
    '[role="menuitemcheckbox"]',
    '[role="menuitemradio"]',
    '[role="option"]',
    '[role="slider"]',
    '[role="spinbutton"]',
    '[role="tab"]',
    '[role="textbox"]',
    '[role="searchbox"]',
    '[role="treeitem"]',
].join(", ");
// Binds the `?` shortcut reference dialog, its persistent enable/disable
// preference, and the Space-leader command sequence (Space then E/R/C) to one
// page. Safe to call once per page load; the dialog, its controls, and the
// status region are all server-rendered and outlive HTMX fragment swaps.
export function bindKeyboardShortcuts(document, window, storage) {
    const dialog = requiredElement(document.querySelector(".shortcuts-dialog"), "shortcuts dialog");
    const openButton = requiredElement(document.querySelector(".shortcuts-open"), "shortcuts open button");
    const closeButton = requiredElement(document.querySelector(".shortcuts-dialog-close"), "shortcuts close button");
    const enabledCheckbox = requiredElement(document.querySelector(".shortcuts-enabled-toggle"), "shortcuts enabled checkbox");
    const status = requiredElement(document.querySelector(".shortcut-status"), "shortcut status region");
    const stored = readPreference(storage, shortcutsEnabledStorageKey);
    const context = {
        document,
        window,
        storage,
        dialog,
        status,
        reviewAvailable: document.querySelector(".review-toggle") !== null,
        enabled: stored === null ? true : stored === "true",
        leaderTimer: null,
        invoker: null,
    };
    enabledCheckbox.checked = context.enabled;
    openButton.addEventListener("click", () => openShortcutsDialog(context));
    closeButton.addEventListener("click", () => dialog.close());
    dialog.addEventListener("close", () => {
        context.invoker?.focus();
        context.invoker = null;
    });
    enabledCheckbox.addEventListener("change", () => {
        context.enabled = enabledCheckbox.checked;
        writePreference(storage, shortcutsEnabledStorageKey, String(context.enabled));
        if (!context.enabled)
            cancelLeader(context);
    });
    document.addEventListener("keydown", (event) => handleKeydown(context, event));
    document.addEventListener("visibilitychange", () => {
        if (document.hidden)
            cancelLeader(context);
    });
}
function requiredElement(value, label) {
    if (!value)
        throw new Error(`keyboard shortcuts: missing ${label}`);
    return value;
}
function openShortcutsDialog(context) {
    if (context.dialog.open)
        return;
    context.invoker =
        context.document.activeElement instanceof HTMLElement
            ? context.document.activeElement
            : null;
    context.dialog.showModal();
}
// Routes a keydown to the idle or leader-armed handler. Autorepeat is
// ignored uniformly so holding Space or a leader key cannot re-arm or
// re-cancel the sequence on every repeated event.
function handleKeydown(context, event) {
    if (event.repeat)
        return;
    if (context.leaderTimer !== null) {
        handleLeaderKey(context, event);
        return;
    }
    handleIdleKey(context, event);
}
// From idle, `?` opens the reference dialog and a bare Space arms the
// leader. Both require global shortcuts enabled and an event target outside
// every editable or interactive control.
function handleIdleKey(context, event) {
    if (!context.enabled)
        return;
    if (isSuppressedTarget(context, event.target))
        return;
    if (event.key === "?" && !hasBlockingModifier(event)) {
        openShortcutsDialog(context);
        return;
    }
    if (event.key === " " && !hasAnyModifier(event)) {
        event.preventDefault();
        armLeader(context);
    }
}
// From the armed state, Escape or any unsupported/modified/composed key
// cancels the sequence; a recognized, available command key executes it.
// Suppression is re-checked here because focus can move into an editable or
// interactive control between the leader key and its follow-up.
function handleLeaderKey(context, event) {
    if (event.key === "Escape") {
        cancelLeader(context);
        return;
    }
    if (
    // Mid-IME composition: the key is part of composing a character, not a
    // real command keystroke.
    event.isComposing ||
        // A dead key (e.g. an accent key awaiting its base letter) has no
        // meaningful .key value to match against E/R/C.
        event.key === "Dead" ||
        // Any modifier turns this into a different, unrelated keyboard command.
        hasAnyModifier(event) ||
        // Focus moved into an editable or interactive control between the
        // leader key and this one.
        isSuppressedTarget(context, event.target)) {
        cancelLeader(context);
        return;
    }
    const command = availableCommand(context, event.key.toLowerCase());
    cancelLeader(context);
    if (!command)
        return;
    event.preventDefault();
    performCommand(context, command);
}
function isSuppressedTarget(context, target) {
    if (!(target instanceof HTMLElement))
        return false;
    if (context.dialog.contains(target))
        return true;
    if (target.isContentEditable)
        return true;
    return target.closest(interactiveTargetSelector) !== null;
}
function hasAnyModifier(event) {
    return event.ctrlKey || event.altKey || event.metaKey || event.shiftKey;
}
// `?` requires Shift on most layouts, so only Ctrl/Alt/Meta block it.
function hasBlockingModifier(event) {
    return event.ctrlKey || event.altKey || event.metaKey;
}
function armLeader(context) {
    context.leaderTimer = context.window.setTimeout(() => cancelLeader(context), leaderTimeoutMs);
    context.status.textContent = "Shortcut: Space …";
}
// Clears the armed state and any pending status text. Used both for a timed
// out/cancelled sequence (stays visually quiet) and as the first step of
// completing a matched command (whose own message replaces this immediately
// after).
function cancelLeader(context) {
    if (context.leaderTimer !== null) {
        context.window.clearTimeout(context.leaderTimer);
        context.leaderTimer = null;
    }
    context.status.textContent = "";
}
function availableCommand(context, key) {
    switch (key) {
        case "e":
            return "documents";
        case "r":
            return context.reviewAvailable ? "annotations" : null;
        case "c":
            return context.reviewAvailable ? "add-comment" : null;
        default:
            return null;
    }
}
function performCommand(context, command) {
    switch (command) {
        case "documents":
            announcePanelToggle(context, ".documents-toggle", "Documents");
            return;
        case "annotations":
            announcePanelToggle(context, ".review-toggle", "Review");
            return;
        case "add-comment":
            context.document.dispatchEvent(new CustomEvent("code-annotator:add-comment"));
            return;
    }
}
function announcePanelToggle(context, toggleSelector, label) {
    const expanded = activatePanelToggle(context.document, toggleSelector);
    if (expanded === null)
        return;
    context.status.textContent = `${label} sidebar ${expanded ? "shown" : "hidden"}.`;
}
// Activates the existing panel-toggle button rather than editing classes,
// hidden state, or storage directly, so pointer and keyboard activation share
// the one toggle implementation and persisted preference. If the panel about
// to be hidden currently holds focus, focus moves to the toggle first so it
// never strands inside content about to become hidden.
function activatePanelToggle(document, toggleSelector) {
    const button = document.querySelector(toggleSelector);
    if (!button)
        return null;
    const panelID = button.getAttribute("aria-controls");
    const panel = panelID ? document.getElementById(panelID) : null;
    const hidingFocusedPanel = panel !== null && !panel.hidden && panel.contains(document.activeElement);
    button.click();
    if (hidingFocusedPanel)
        button.focus();
    return button.getAttribute("aria-expanded") === "true";
}
