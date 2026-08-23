export function createAnnotationNavigator({ markdown, sourceRange, sourceSpan }) {
  let navigationTargetTimer = 0;

  function navigateFromAnnotation(event, card, annotation) {
    if (window.getSelection()?.toString()) return;
    event.preventDefault();
    if (card.open) {
      card.open = false;
      return;
    }
    card.open = true;

    const result = annotationNavigationTarget(annotation);
    const status = card.querySelector(".annotation-navigation-status");
    if (!result.target) {
      status.textContent = "No approximate source location is available.";
      status.classList.add("approximate");
      return;
    }

    status.textContent = result.approximate ? "Showing the approximate original location." : "";
    status.classList.toggle("approximate", result.approximate);
    emphasizeNavigationTarget(result.target);
  }

  function annotationNavigationTarget(annotation) {
    if (!annotation.source || !annotation.source.selector) {
      return { target: markdown.querySelector("h1, h2, h3, h4, h5, h6") || markdown, approximate: false };
    }

    if (annotation.anchor && annotation.anchor.state !== "stale") {
      const diagram = diagramForRange(annotation.anchor.startByte, annotation.anchor.endByte);
      if (diagram) return { target: diagram, approximate: false };
      const range = sourceRange(annotation.anchor.startByte, annotation.anchor.endByte);
      if (range) return { target: sourceSpan(range.startContainer), approximate: false };
    }

    const originalOffset = Number.parseInt(annotation.source.selector.startByte, 10);
    return { target: nearestSourceTarget(originalOffset), approximate: true };
  }

  function diagramForRange(startByte, endByte) {
    return Array.from(markdown.querySelectorAll(".mermaid-diagram[data-source-start][data-source-end]"))
      .find((diagram) => Number.parseInt(diagram.dataset.sourceStart, 10) === startByte
        && Number.parseInt(diagram.dataset.sourceEnd, 10) === endByte) || null;
  }

  // Choose the source-backed element closest to the old byte offset. A span in
  // collapsed Mermaid source maps to its visible diagram container.
  function nearestSourceTarget(sourceOffset) {
    if (!Number.isInteger(sourceOffset)) return null;
    const candidates = Array.from(markdown.querySelectorAll(".source-text"))
      .map((span) => {
        const start = Number.parseInt(span.dataset.sourceStart, 10);
        const end = Number.parseInt(span.dataset.sourceEnd, 10);
        if (!Number.isInteger(start) || !Number.isInteger(end)) return null;
        const distance = sourceOffset < start ? start - sourceOffset : sourceOffset > end ? sourceOffset - end : 0;
        return { span, distance };
      })
      .filter(Boolean)
      .sort((left, right) => left.distance - right.distance)[0];
    if (!candidates) return null;
    return candidates.span.closest(".mermaid-diagram") || candidates.span;
  }

  function emphasizeNavigationTarget(target) {
    window.clearTimeout(navigationTargetTimer);
    markdown.querySelectorAll(".annotation-navigation-target").forEach((item) => {
      item.classList.remove("annotation-navigation-target");
      if (item.dataset.annotationNavigationTabindex === "added") {
        item.removeAttribute("tabindex");
        delete item.dataset.annotationNavigationTabindex;
      }
    });

    if (!target.hasAttribute("tabindex")) {
      target.tabIndex = -1;
      target.dataset.annotationNavigationTabindex = "added";
    }
    target.classList.add("annotation-navigation-target");
    target.focus({ preventScroll: true });
    const reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    target.scrollIntoView({ behavior: reducedMotion ? "auto" : "smooth", block: "center", inline: "nearest" });
    navigationTargetTimer = window.setTimeout(() => {
      target.classList.remove("annotation-navigation-target");
      if (target.dataset.annotationNavigationTabindex === "added") {
        target.removeAttribute("tabindex");
        delete target.dataset.annotationNavigationTabindex;
      }
    }, 1800);
  }

  return { navigateFromAnnotation };
}
