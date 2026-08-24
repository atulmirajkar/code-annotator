import type { AnnotationLocation } from "./review-fragments.js";

interface AnnotationHighlighterOptions {
  markdown: HTMLElement;
  sourceSpan: (node: Node) => HTMLElement | null;
  sourceSpanRange: (startSpan: HTMLElement, endSpan: HTMLElement) => HTMLElement[] | null;
  utf8Length: (value: string) => number;
}

export function mergeIntervals(values: ReadonlyArray<readonly [number, number]>): Array<[number, number]> {
  const sorted = values
    .map(([start, end]): [number, number] => [start, end])
    .sort((left, right) => left[0] - right[0]);
  return sorted.reduce((merged, current) => {
    const previous = merged[merged.length - 1];
    if (previous && current[0] <= previous[1]) previous[1] = Math.max(previous[1], current[1]);
    else merged.push(current);
    return merged;
  }, [] as Array<[number, number]>);
}

export function createAnnotationHighlighter({ markdown, sourceSpan, sourceSpanRange, utf8Length }: AnnotationHighlighterOptions) {
  // Highlight only anchors resolved against the current document. Stale and
  // document-level annotations remain visible in the panel without a range.
  function renderAnnotationHighlights(annotations: AnnotationLocation[]): void {
    clearFallbackHighlights();
    renderDiagramHighlights(annotations);
    const ranges = annotations
      .filter(hasResolvedAnchor)
      .map((annotation) => sourceRange(annotation.anchorStartByte, annotation.anchorEndByte))
      .filter((range): range is Range => range !== null);

    if (globalThis.CSS && CSS.highlights && typeof Highlight !== "undefined") {
      CSS.highlights.delete("code-annotator-annotations");
      if (ranges.length > 0) CSS.highlights.set("code-annotator-annotations", new Highlight(...ranges));
      return;
    }
    renderFallbackHighlights(ranges);
  }

  // Diagram annotations highlight the rendered region as a whole; their
  // hidden source ranges remain available for quote previews and fallback APIs.
  function renderDiagramHighlights(annotations: AnnotationLocation[]): void {
    const activeRanges = annotations
      .filter(hasResolvedAnchor)
      .map((annotation): [number, number] => [annotation.anchorStartByte, annotation.anchorEndByte]);
    markdown.querySelectorAll<HTMLElement>(".mermaid-diagram[data-source-start][data-source-end]").forEach((diagram) => {
      const start = Number.parseInt(diagram.dataset.sourceStart || "", 10);
      const end = Number.parseInt(diagram.dataset.sourceEnd || "", 10);
      diagram.classList.toggle("annotation-highlight-region", activeRanges.some((range) => range[0] === start && range[1] === end));
    });
  }

  function sourceRange(startByte: number, endByte: number): Range | null {
    const spans = Array.from(markdown.querySelectorAll<HTMLElement>(".source-text"));
    const startSpan = spans.find((span) => containsSourceOffset(span, startByte, false));
    const endSpan = spans.slice().reverse().find((span) => containsSourceOffset(span, endByte, true));
    if (!startSpan || !endSpan) return null;

    const startNode = sourceTextNode(startSpan);
    const endNode = sourceTextNode(endSpan);
    const startOffset = byteOffsetToTextOffset(startSpan, startByte);
    const endOffset = byteOffsetToTextOffset(endSpan, endByte);
    if (!startNode || !endNode || startOffset < 0 || endOffset < 0) return null;

    const range = document.createRange();
    try {
      range.setStart(startNode, startOffset);
      range.setEnd(endNode, endOffset);
    } catch (_) {
      return null;
    }
    return range.collapsed ? null : range;
  }

  function containsSourceOffset(span: HTMLElement, offset: number, endBoundary: boolean): boolean {
    const start = Number.parseInt(span.dataset.sourceStart || "", 10);
    const end = Number.parseInt(span.dataset.sourceEnd || "", 10);
    return Number.isInteger(start) && Number.isInteger(end) && (endBoundary ? start < offset && offset <= end : start <= offset && offset < end);
  }

  function byteOffsetToTextOffset(span: HTMLElement, sourceOffset: number): number {
    const spanStart = Number.parseInt(span.dataset.sourceStart || "", 10);
    const target = sourceOffset - spanStart;
    if (!Number.isInteger(spanStart) || target < 0) return -1;

    let bytes = 0;
    let textOffset = 0;
    for (const character of span.textContent || "") {
      if (bytes === target) return textOffset;
      bytes += utf8Length(character);
      textOffset += character.length;
      if (bytes > target) return -1;
    }
    return bytes === target ? textOffset : -1;
  }

  function sourceTextNode(span: HTMLElement): Text | null {
    span.normalize();
    return span.firstChild instanceof Text ? span.firstChild : null;
  }

  // The fallback merges overlapping intervals within each source span before
  // wrapping them, avoiding invalid nested or crossing mark elements.
  function renderFallbackHighlights(ranges: Range[]): void {
    const intervals = new Map<HTMLElement, Array<[number, number]>>();
    ranges.forEach((range) => {
      const startSpan = sourceSpan(range.startContainer);
      const endSpan = sourceSpan(range.endContainer);
      if (!startSpan || !endSpan) return;
      const spans = sourceSpanRange(startSpan, endSpan) || [];
      spans.forEach((span) => {
        const length = (span.textContent || "").length;
        const start = span === startSpan ? range.startOffset : 0;
        const end = span === endSpan ? range.endOffset : length;
        if (end > start) intervals.set(span, [...(intervals.get(span) || []), [start, end]]);
      });
    });

    intervals.forEach((values, span) => {
      const merged = mergeIntervals(values);
      const textNode = sourceTextNode(span);
      if (!textNode) return;
      merged.reverse().forEach(([start, end]) => {
        const range = document.createRange();
        range.setStart(textNode, start);
        range.setEnd(textNode, end);
        const mark = document.createElement("mark");
        mark.className = "annotation-highlight-fallback";
        range.surroundContents(mark);
      });
    });
  }

  function clearFallbackHighlights() {
    markdown.querySelectorAll("mark.annotation-highlight-fallback").forEach((mark) => {
      mark.replaceWith(document.createTextNode(mark.textContent || ""));
    });
    markdown.querySelectorAll(".source-text").forEach((span) => span.normalize());
  }

  return {
    renderAnnotationHighlights,
    sourceRange,
  };
}

function hasResolvedAnchor(annotation: AnnotationLocation): annotation is AnnotationLocation & { anchorStartByte: number; anchorEndByte: number } {
  return annotation.anchorState !== null
    && annotation.anchorState !== "stale"
    && annotation.anchorStartByte !== null
    && annotation.anchorEndByte !== null;
}
