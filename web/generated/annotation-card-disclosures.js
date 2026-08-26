const toggleSelector = ".annotation-disclosure-toggle";
const panelSelector = ".annotation-reply-panel, .annotation-actions-panel";
// The annotation panel survives fragment swaps. Event delegation keeps these
// disclosures functional without rebinding each server-rendered card.
//
// Binds click, keyboard, and submit handling for reply/actions disclosures
// within an annotation panel, and returns a handle for restoring focus after
// a server-driven fragment swap completes.
// - panel: the delegation root; only toggles, disclosure panels, and forms
//   inside it are handled.
// - signal: aborts all three listeners together when the panel is torn down.
export function bindAnnotationCardDisclosures(panel, signal) {
    panel.addEventListener("click", (event) => handleClick(panel, event), {
        signal,
    });
    panel.addEventListener("keydown", (event) => handleKeydown(panel, event), {
        signal,
    });
    let pendingOperation = null;
    panel.addEventListener("submit", (event) => {
        pendingOperation = pendingOperationFromSubmit(panel, event);
    }, { signal });
    return {
        restoreAfterSwap(successful) {
            const operation = pendingOperation;
            pendingOperation = null;
            if (operation)
                restoreOperationFocus(panel, operation, successful);
        },
    };
}
// Toggles the panel disclosed by a clicked toggle button, and focuses the
// reply textarea when a reply panel is being expanded.
function handleClick(panel, event) {
    const target = event.target instanceof Element ? event.target : null;
    const button = target?.closest(toggleSelector);
    if (!button || !panel.contains(button))
        return;
    const disclosurePanel = disclosurePanelFor(panel, button);
    if (!disclosurePanel)
        return;
    const expanding = disclosurePanel.hidden;
    setExpanded(button, disclosurePanel, expanding);
    if (expanding && button.classList.contains("annotation-reply-toggle")) {
        // Reply panels start with an empty draft, so autofocus the textarea
        // instead of the toggle button.
        disclosurePanel
            .querySelector('textarea[name="message"]')
            ?.focus();
    }
}
// Collapses an open panel on Escape and returns focus to its toggle button,
// wherever inside that panel the keypress originated.
function handleKeydown(panel, event) {
    if (event.key !== "Escape")
        return;
    const target = event.target instanceof Element ? event.target : null;
    const disclosurePanel = target?.closest(panelSelector);
    if (!disclosurePanel ||
        disclosurePanel.hidden ||
        !panel.contains(disclosurePanel)) {
        return;
    }
    const button = toggleForPanel(panel, disclosurePanel.id);
    if (!button)
        return;
    // Escape is also the browser's dialog-cancel key; prevent default so it
    // only closes this disclosure, not an ancestor dialog.
    event.preventDefault();
    setExpanded(button, disclosurePanel, false);
    button.focus();
}
// Resolves the panel a toggle button discloses via aria-controls, scoped to
// elements inside panel so a stale or foreign ID never matches.
function disclosurePanelFor(panel, button) {
    const id = button.getAttribute("aria-controls");
    if (!id)
        return null;
    const target = button.ownerDocument.getElementById(id);
    return target instanceof HTMLElement && panel.contains(target)
        ? target
        : null;
}
// Finds the toggle button whose aria-controls points at panelID, the inverse
// lookup of disclosurePanelFor, needed when Escape is pressed inside the
// panel rather than on the button itself.
function toggleForPanel(panel, panelID) {
    for (const button of panel.querySelectorAll(toggleSelector)) {
        if (button.getAttribute("aria-controls") === panelID)
            return button;
    }
    return null;
}
// Applies expanded/collapsed state to a toggle and the panel it discloses,
// moving focus to the toggle first when collapsing would otherwise strand
// focus inside a panel about to be hidden.
function setExpanded(button, panel, expanded) {
    if (!expanded && panel.contains(button.ownerDocument.activeElement)) {
        button.focus();
    }
    panel.hidden = !expanded;
    button.setAttribute("aria-expanded", String(expanded));
}
// Classifies a submitted form so restoreAfterSwap later knows which
// disclosure to reopen. Returns null for forms this module does not track,
// such as the top-level new-comment form, which also lives inside panel.
function operationKindForForm(form) {
    if (form.classList.contains("annotation-reply"))
        return "reply";
    if (form.classList.contains("annotation-reattach"))
        return "reattach";
    if (form.classList.contains("annotation-lifecycle"))
        return "lifecycle";
    if (form.classList.contains("annotation-quick-close-form")) {
        return "quickClose";
    }
    return null;
}
// Captures which card and operation a submit event belongs to, before the
// request goes out and the response fragment overwrites the card's disclosure
// state. Returns null for submits this module has no restoration behavior
// for, so restoreAfterSwap can safely no-op on the next completed request.
function pendingOperationFromSubmit(panel, event) {
    const form = event.target instanceof HTMLFormElement ? event.target : null;
    if (!form || !panel.contains(form))
        return null;
    const card = form.closest(".annotation-card");
    if (!card || !card.id)
        return null;
    const kind = operationKindForForm(form);
    return kind ? { cardID: card.id, kind } : null;
}
// Restores focus after a card operation's response fragment has replaced
// #annotation-panel-content. On success, focus goes to the card summary so
// the reviewer lands on updated state. On failure, the disclosure that owned
// the operation is reopened (there is none for quickClose) so the
// server-preserved draft stays visible, and focus moves to the panel-level
// feedback message carrying the error. If the card itself is gone, for
// example a quick close that made the annotation inactive under the current
// filter, focus falls back to the panel heading rather than <body>.
function restoreOperationFocus(panel, operation, successful) {
    const documentRef = panel.ownerDocument;
    const card = documentRef.getElementById(operation.cardID);
    if (!card) {
        documentRef.getElementById("review-heading")?.focus();
        return;
    }
    if (successful) {
        focusCardSummary(card);
        return;
    }
    reopenOperationDisclosure(panel, card, operation.kind);
    if (!focusPanelFeedback(documentRef))
        focusCardSummary(card);
}
function focusCardSummary(card) {
    card.querySelector(".annotation-summary")?.focus();
}
function focusPanelFeedback(documentRef) {
    const feedback = documentRef.querySelector(".annotation-panel-feedback");
    feedback?.focus();
    return feedback !== null;
}
// Reopens the disclosure a failed operation belongs to, using the same
// toggle/panel pairing as a pointer click, so a retried reply or lifecycle
// action starts from the state the reviewer left it in.
function reopenOperationDisclosure(panel, card, kind) {
    const toggleClass = kind === "reply"
        ? "annotation-reply-toggle"
        : kind === "reattach" || kind === "lifecycle"
            ? "annotation-actions-toggle"
            : null;
    if (!toggleClass)
        return;
    const button = card.querySelector(`.${toggleClass}`);
    if (!button)
        return;
    const disclosurePanel = disclosurePanelFor(panel, button);
    if (disclosurePanel)
        setExpanded(button, disclosurePanel, true);
}
