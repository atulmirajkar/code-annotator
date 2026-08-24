import type { AnnotationLocation } from "./review-fragments.js";

interface AnnotationNavigatorOptions {
  markdown: HTMLElement;
  sourceRange: (startByte: number, endByte: number) => Range | null;
  sourceSpan: (node: Node) => HTMLElement | null;
}

export function createAnnotationNavigator({ markdown, sourceRange, sourceSpan }: AnnotationNavigatorOptions) {
  let navigationTargetTimer = 0;

  function navigateFromAnnotation(event: MouseEvent, card: HTMLDetailsElement, annotation: AnnotationLocation): void {
    if (window.getSelection()?.toString()) return;
    event.preventDefault();
    if (card.open) {
      card.open = false;
      return;
    }
    card.open = true;

    const result = annotationNavigationTarget(annotation);
    const status = card.querySelector<HTMLElement>(".annotation-navigation-status");
    if (!status) return;
    if (!result.target) {
      status.textContent = "No approximate source location is available.";
      status.classList.add("approximate");
      return;
    }

    status.textContent = result.approximate ? "Showing the approximate original location." : "";
    status.classList.toggle("approximate", result.approximate);
    emphasizeNavigationTarget(result.target);
  }

  function annotationNavigationTarget(annotation: AnnotationLocation): { target: HTMLElement | null; approximate: boolean } {
    if (annotation.needsReattachment) {
      return { target: null, approximate: true };
    }
    if (annotation.documentLevel) {
      return { target: markdown.querySelector<HTMLElement>("h1, h2, h3, h4, h5, h6") || markdown, approximate: false };
    }

    if (annotation.anchorState !== null && annotation.anchorState !== "stale" && annotation.anchorStartByte !== null && annotation.anchorEndByte !== null) {
      const diagram = diagramForRange(annotation.anchorStartByte, annotation.anchorEndByte);
      if (diagram) return { target: diagram, approximate: false };
      const range = sourceRange(annotation.anchorStartByte, annotation.anchorEndByte);
      if (range) return { target: sourceSpan(range.startContainer), approximate: false };
    }

    return { target: annotation.sourceStartByte === null ? null : nearestSourceTarget(annotation.sourceStartByte), approximate: true };
  }

  function diagramForRange(startByte: number, endByte: number): HTMLElement | null {
    return Array.from(markdown.querySelectorAll<HTMLElement>(".mermaid-diagram[data-source-start][data-source-end]"))
      .find((diagram) => Number.parseInt(diagram.dataset.sourceStart || "", 10) === startByte
        && Number.parseInt(diagram.dataset.sourceEnd || "", 10) === endByte) || null;
  }

  // Choose the source-backed element closest to the old byte offset. A span in
  // collapsed Mermaid source maps to its visible diagram container.
  function nearestSourceTarget(sourceOffset: number): HTMLElement | null {
    if (!Number.isInteger(sourceOffset)) return null;
    const candidates = Array.from(markdown.querySelectorAll<HTMLElement>(".source-text"))
      .map((span): { span: HTMLElement; distance: number } | null => {
        const start = Number.parseInt(span.dataset.sourceStart || "", 10);
        const end = Number.parseInt(span.dataset.sourceEnd || "", 10);
        if (!Number.isInteger(start) || !Number.isInteger(end)) return null;
        const distance = sourceOffset < start ? start - sourceOffset : sourceOffset > end ? sourceOffset - end : 0;
        return { span, distance };
      })
      .filter((candidate): candidate is { span: HTMLElement; distance: number } => candidate !== null)
      .sort((left, right) => left.distance - right.distance)[0];
    if (!candidates) return null;
    return candidates.span.closest<HTMLElement>(".mermaid-diagram") || candidates.span;
  }

  function emphasizeNavigationTarget(target: HTMLElement): void {
    window.clearTimeout(navigationTargetTimer);
    markdown.querySelectorAll<HTMLElement>(".annotation-navigation-target").forEach((item) => {
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
