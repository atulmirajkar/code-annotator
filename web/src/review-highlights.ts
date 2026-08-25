import type { AnnotationBrowserState, SourcePosition } from "./viewer-state.js";

interface AnnotationHighlighterOptions {
  document: Document;
  markdown: HTMLElement;
  sourceSpan: (node: Node) => HTMLElement | null;
  sourceSpanRange: (
    startSpan: HTMLElement,
    endSpan: HTMLElement,
  ) => HTMLElement[] | null;
  utf8Length: (value: string) => number;
  sourceNodes: ReadonlyMap<string, SourcePosition>;
  diagrams: ReadonlyMap<string, SourcePosition>;
}

interface TextPoint {
  node: Text;
  offset: number;
}

interface AnnotationHighlighter {
  renderAnnotationHighlights(
    annotations: ReadonlyArray<AnnotationBrowserState>,
  ): void;
  sourceRange(startByte: number, endByte: number): Range | null;
}

export function mergeIntervals(
  values: ReadonlyArray<readonly [number, number]>,
): Array<[number, number]> {
  const sorted = values
    .map(([start, end]): [number, number] => [start, end])
    .sort((left, right) => left[0] - right[0]);
  return sorted.reduce(
    (merged, current) => {
      const previous = merged[merged.length - 1];
      if (previous && current[0] <= previous[1])
        previous[1] = Math.max(previous[1], current[1]);
      else merged.push(current);
      return merged;
    },
    [] as Array<[number, number]>,
  );
}

export function createAnnotationHighlighter(
  options: AnnotationHighlighterOptions,
): AnnotationHighlighter {
  return new AnnotationHighlighterController(options);
}

class AnnotationHighlighterController implements AnnotationHighlighter {
  constructor(private readonly options: AnnotationHighlighterOptions) {}

  readonly renderAnnotationHighlights = (
    annotations: ReadonlyArray<AnnotationBrowserState>,
  ): void => {
    renderAnnotationHighlights(this.options, annotations);
  };

  readonly sourceRange = (startByte: number, endByte: number): Range | null =>
    sourceRange(this.options, startByte, endByte);
}

// Highlight only anchors resolved against the current document.
function renderAnnotationHighlights(
  options: AnnotationHighlighterOptions,
  annotations: ReadonlyArray<AnnotationBrowserState>,
): void {
  clearFallbackHighlights(options);
  renderDiagramHighlights(options, annotations);
  const ranges = annotations
    .filter(hasResolvedAnchor)
    .map((annotation) =>
      sourceRange(
        options,
        annotation.anchor.startByte,
        annotation.anchor.endByte,
      ),
    )
    .filter((range): range is Range => range !== null);
  if (globalThis.CSS && CSS.highlights && typeof Highlight !== "undefined") {
    CSS.highlights.delete("code-annotator-annotations");
    if (ranges.length > 0)
      CSS.highlights.set(
        "code-annotator-annotations",
        new Highlight(...ranges),
      );
    return;
  }
  renderFallbackHighlights(options, ranges);
}

// Diagram annotations highlight the rendered region as a whole.
function renderDiagramHighlights(
  { document, markdown, diagrams }: AnnotationHighlighterOptions,
  annotations: ReadonlyArray<AnnotationBrowserState>,
): void {
  const activeRanges = annotations
    .filter(hasResolvedAnchor)
    .map((annotation): [number, number] => [
      annotation.anchor.startByte,
      annotation.anchor.endByte,
    ]);
  diagrams.forEach((position) => {
    const diagram = document.getElementById(position.elementId);
    if (!diagram || !markdown.contains(diagram)) return;
    diagram.classList.toggle(
      "annotation-highlight-region",
      activeRanges.some(
        (range) =>
          range[0] === position.startByte && range[1] === position.endByte,
      ),
    );
  });
}

// Convert source bytes into a DOM Range using typed source spans.
function sourceRange(
  options: AnnotationHighlighterOptions,
  startByte: number,
  endByte: number,
): Range | null {
  const { document, markdown, sourceNodes } = options;
  const spans = Array.from(sourceNodes.keys())
    .map((elementId) => document.getElementById(elementId))
    .filter(
      (element): element is HTMLElement =>
        element instanceof HTMLElement && markdown.contains(element),
    );
  const startSpan = spans.find((span) =>
    containsSourceOffset(sourceNodes, span, startByte, false),
  );
  const endSpan = spans
    .slice()
    .reverse()
    .find((span) => containsSourceOffset(sourceNodes, span, endByte, true));
  if (!startSpan || !endSpan) return null;
  const startPoint = sourceByteToTextPoint(
    options,
    startSpan,
    startByte,
    false,
  );
  const endPoint = sourceByteToTextPoint(options, endSpan, endByte, true);
  if (!startPoint || !endPoint) return null;
  const range = document.createRange();
  try {
    range.setStart(startPoint.node, startPoint.offset);
    range.setEnd(endPoint.node, endPoint.offset);
  } catch (_) {
    return null;
  }
  return range.collapsed ? null : range;
}

// Opposite endpoint affinity keeps cross-line ranges on adjacent spans.
function containsSourceOffset(
  sourceNodes: ReadonlyMap<string, SourcePosition>,
  span: HTMLElement,
  offset: number,
  endBoundary: boolean,
): boolean {
  const position = sourceNodes.get(span.id);
  return (
    Boolean(position) &&
    (endBoundary
      ? position!.startByte < offset && offset <= position!.endByte
      : position!.startByte <= offset && offset < position!.endByte)
  );
}

// Walk token-containing text leaves and translate UTF-8 bytes to UTF-16.
function sourceByteToTextPoint(
  { sourceNodes, utf8Length, document }: AnnotationHighlighterOptions,
  span: HTMLElement,
  sourceOffset: number,
  endBoundary: boolean,
): TextPoint | null {
  const position = sourceNodes.get(span.id);
  if (!position) return null;
  const target = sourceOffset - position.startByte;
  const nodes = descendantTextNodes(document, span);
  let bytes = 0;
  for (let index = 0; index < nodes.length; index += 1) {
    const node = nodes[index]!;
    const length = utf8Length(node.data);
    if (target < bytes + length) {
      const offset = utf16OffsetAtByte(utf8Length, node.data, target - bytes);
      return offset < 0 ? null : { node, offset };
    }
    if (target === bytes + length) {
      const next = nodes[index + 1];
      if (!endBoundary && next) return { node: next, offset: 0 };
      return { node, offset: node.length };
    }
    bytes += length;
  }
  return null;
}

// TreeWalker preserves rendered order while ignoring token element nodes.
function descendantTextNodes(document: Document, span: HTMLElement): Text[] {
  const walker = document.createTreeWalker(span, NodeFilter.SHOW_TEXT);
  const nodes: Text[] = [];
  let node = walker.nextNode();
  while (node) {
    if (node instanceof Text) nodes.push(node);
    node = walker.nextNode();
  }
  return nodes;
}

// Reject boundaries that split a multi-byte code point.
function utf16OffsetAtByte(
  utf8Length: (value: string) => number,
  value: string,
  target: number,
): number {
  if (target === 0) return 0;
  let bytes = 0;
  let offset = 0;
  for (const character of value) {
    bytes += utf8Length(character);
    offset += character.length;
    if (bytes === target) return offset;
    if (bytes > target) return -1;
  }
  return -1;
}

// Split leaves independently so fallback marks never cross token elements.
function renderFallbackHighlights(
  options: AnnotationHighlighterOptions,
  ranges: Range[],
): void {
  const intervals = new Map<HTMLElement, Array<[number, number]>>();
  ranges.forEach((range) => {
    const startSpan = options.sourceSpan(range.startContainer);
    const endSpan = options.sourceSpan(range.endContainer);
    if (!startSpan || !endSpan) return;
    const spans = options.sourceSpanRange(startSpan, endSpan) || [];
    spans.forEach((span) => {
      const length = (span.textContent || "").length;
      const start =
        span === startSpan
          ? textOffsetWithinSpan(
              options.document,
              span,
              range.startContainer,
              range.startOffset,
            )
          : 0;
      const end =
        span === endSpan
          ? textOffsetWithinSpan(
              options.document,
              span,
              range.endContainer,
              range.endOffset,
            )
          : length;
      if (start >= 0 && end >= 0 && end > start)
        intervals.set(span, [...(intervals.get(span) || []), [start, end]]);
    });
  });
  intervals.forEach((values, span) => {
    const merged = mergeIntervals(values);
    const nodes = descendantTextNodes(options.document, span);
    const nodeIntervals: Array<[Text, number, number]> = [];
    let textOffset = 0;
    nodes.forEach((node) => {
      const nodeEnd = textOffset + node.length;
      merged.forEach(([start, end]) => {
        const overlapStart = Math.max(start, textOffset);
        const overlapEnd = Math.min(end, nodeEnd);
        if (overlapEnd > overlapStart)
          nodeIntervals.push([
            node,
            overlapStart - textOffset,
            overlapEnd - textOffset,
          ]);
      });
      textOffset = nodeEnd;
    });
    nodeIntervals.reverse().forEach(([node, start, end]) => {
      node.splitText(end);
      const target = start === 0 ? node : node.splitText(start);
      const mark = options.document.createElement("mark");
      mark.className = "annotation-highlight-fallback";
      target.parentNode?.replaceChild(mark, target);
      mark.appendChild(target);
    });
  });
}

// Measure the text prefix across arbitrary descendant token nodes.
function textOffsetWithinSpan(
  document: Document,
  span: HTMLElement,
  boundaryNode: Node,
  boundaryOffset: number,
): number {
  const range = document.createRange();
  range.selectNodeContents(span);
  try {
    range.setEnd(boundaryNode, boundaryOffset);
  } catch (_) {
    return -1;
  }
  return range.toString().length;
}

function clearFallbackHighlights({
  document,
  markdown,
}: AnnotationHighlighterOptions): void {
  markdown
    .querySelectorAll("mark.annotation-highlight-fallback")
    .forEach((mark) =>
      mark.replaceWith(document.createTextNode(mark.textContent || "")),
    );
  markdown.querySelectorAll(".source-text").forEach((span) => span.normalize());
}

function hasResolvedAnchor(
  annotation: AnnotationBrowserState,
): annotation is AnnotationBrowserState & {
  anchor: NonNullable<AnnotationBrowserState["anchor"]>;
} {
  return annotation.anchor !== null && annotation.anchor.state !== "stale";
}
