export function createAnnotationNavigator({ document, window, markdown, sourceRange, sourceSpan, sourceNodes, diagrams, }) {
    let navigationTargetTimer = 0;
    const temporaryTabIndex = new WeakSet();
    function navigateFromAnnotation(event, card, annotation) {
        if (window.getSelection()?.toString())
            return;
        event.preventDefault();
        if (card.open) {
            card.open = false;
            return;
        }
        card.open = true;
        const result = annotationNavigationTarget(annotation);
        const status = card.querySelector(".annotation-navigation-status");
        if (!status)
            return;
        if (!result.target) {
            status.textContent = "No approximate source location is available.";
            status.classList.add("approximate");
            return;
        }
        status.textContent = result.approximate
            ? "Showing the approximate original location."
            : "";
        status.classList.toggle("approximate", result.approximate);
        emphasizeNavigationTarget(result.target);
    }
    function annotationNavigationTarget(annotation) {
        if (annotation.needsReattachment) {
            return { target: null, approximate: true };
        }
        if (annotation.documentLevel) {
            return {
                target: markdown.querySelector("h1, h2, h3, h4, h5, h6") ||
                    markdown,
                approximate: false,
            };
        }
        if (annotation.anchor !== null && annotation.anchor.state !== "stale") {
            const diagram = diagramForRange(annotation.anchor.startByte, annotation.anchor.endByte);
            if (diagram)
                return { target: diagram, approximate: false };
            const range = sourceRange(annotation.anchor.startByte, annotation.anchor.endByte);
            if (range)
                return { target: sourceSpan(range.startContainer), approximate: false };
        }
        return {
            target: annotation.sourceStartByte === null
                ? null
                : nearestSourceTarget(annotation.sourceStartByte),
            approximate: true,
        };
    }
    function diagramForRange(startByte, endByte) {
        const position = Array.from(diagrams.values()).find((candidate) => candidate.startByte === startByte && candidate.endByte === endByte);
        const element = position
            ? document.getElementById(position.elementId)
            : null;
        return element instanceof HTMLElement && markdown.contains(element)
            ? element
            : null;
    }
    // Choose the source-backed element closest to the old byte offset. A span in
    // collapsed Mermaid source maps to its visible diagram container.
    function nearestSourceTarget(sourceOffset) {
        if (!Number.isInteger(sourceOffset))
            return null;
        const candidates = Array.from(sourceNodes.values())
            .map((position) => {
            const span = document.getElementById(position.elementId);
            if (!(span instanceof HTMLElement) || !markdown.contains(span))
                return null;
            const distance = sourceOffset < position.startByte
                ? position.startByte - sourceOffset
                : sourceOffset > position.endByte
                    ? sourceOffset - position.endByte
                    : 0;
            return { span, distance };
        })
            .filter((candidate) => candidate !== null)
            .sort((left, right) => left.distance - right.distance)[0];
        if (!candidates)
            return null;
        return (candidates.span.closest(".mermaid-diagram") ||
            candidates.span);
    }
    function emphasizeNavigationTarget(target) {
        window.clearTimeout(navigationTargetTimer);
        markdown
            .querySelectorAll(".annotation-navigation-target")
            .forEach((item) => {
            item.classList.remove("annotation-navigation-target");
            if (temporaryTabIndex.has(item)) {
                item.removeAttribute("tabindex");
                temporaryTabIndex.delete(item);
            }
        });
        if (!target.hasAttribute("tabindex")) {
            target.tabIndex = -1;
            temporaryTabIndex.add(target);
        }
        target.classList.add("annotation-navigation-target");
        target.focus({ preventScroll: true });
        const reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
        target.scrollIntoView({
            behavior: reducedMotion ? "auto" : "smooth",
            block: "center",
            inline: "nearest",
        });
        navigationTargetTimer = window.setTimeout(() => {
            target.classList.remove("annotation-navigation-target");
            if (temporaryTabIndex.has(target)) {
                target.removeAttribute("tabindex");
                temporaryTabIndex.delete(target);
            }
        }, 1800);
    }
    return { navigateFromAnnotation };
}
