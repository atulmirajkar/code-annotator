// @vitest-environment happy-dom

import { describe, expect, it } from "vitest";

import { mergeIntervals } from "./review-highlights.js";
import { createAnnotationHighlighter } from "./review-highlights.js";
import type { AnnotationBrowserState, SourcePosition } from "./viewer-state.js";

describe("mergeIntervals", () => {
  it("returns no ranges for no input", () => {
    expect(mergeIntervals([])).toEqual([]);
  });

  it("sorts and merges overlapping, adjacent, and contained ranges", () => {
    expect(mergeIntervals([
      [12, 15],
      [2, 5],
      [4, 8],
      [10, 12],
      [3, 4],
    ])).toEqual([
      [2, 8],
      [10, 15],
    ]);
  });

  it("keeps disjoint ranges separate", () => {
    expect(mergeIntervals([[1, 3], [5, 7], [9, 11]])).toEqual([
      [1, 3],
      [5, 7],
      [9, 11],
    ]);
  });

  it("does not mutate caller-owned ranges", () => {
    const ranges = [[8, 10], [2, 4]] as const;

    mergeIntervals(ranges);

    expect(ranges).toEqual([[8, 10], [2, 4]]);
  });
});

function fixture() {
  document.body.innerHTML =
    '<div class="markdown-body"><span id="source-0-15" class="source-text"><span class="syntax-keyword">const</span> value = <span class="syntax-number">1</span></span></div>';
  const markdown = document.querySelector<HTMLElement>(".markdown-body")!;
  const span = markdown.querySelector<HTMLElement>(".source-text")!;
  const sourceNodes = new Map<string, SourcePosition>([
    [span.id, { elementId: span.id, startByte: 0, endByte: 15 }],
  ]);
  const highlighter = createAnnotationHighlighter({
    document,
    markdown,
    sourceSpan: (node) =>
      (node instanceof HTMLElement ? node : node.parentElement)?.closest(
        ".source-text",
      ) || null,
    sourceSpanRange: () => [span],
    utf8Length: (value) => new TextEncoder().encode(value).length,
    sourceNodes,
    diagrams: new Map(),
  });
  return { markdown, span, highlighter };
}

function annotation(startByte: number, endByte: number): AnnotationBrowserState {
  return {
    id: "ann_test",
    status: "open",
    elementId: "source-0-14",
    lifecycleFormId: "",
    documentLevel: false,
    needsReattachment: false,
    sourceStartByte: startByte,
    anchor: { startByte, endByte, state: "exact" },
    transitions: [],
  };
}

describe("nested source ranges", () => {
  it("maps byte boundaries to descendant text nodes", () => {
    const { highlighter } = fixture();

    const keyword = highlighter.sourceRange(0, 5)!;
    expect(keyword.startContainer.textContent).toBe("const");
    expect(keyword.startOffset).toBe(0);
    expect(keyword.endContainer.textContent).toBe("const");
    expect(keyword.endOffset).toBe(5);

    const number = highlighter.sourceRange(14, 15)!;
    expect(number.startContainer.textContent).toBe("1");
    expect(number.startOffset).toBe(0);
    expect(number.endContainer.textContent).toBe("1");
    expect(number.endOffset).toBe(1);
  });

  it("wraps fallback marks inside token elements without flattening them", () => {
    const { highlighter, span } = fixture();

    highlighter.renderAnnotationHighlights([annotation(0, 5)]);

    const token = span.querySelector(".syntax-keyword")!;
    expect(token.querySelector("mark.annotation-highlight-fallback")?.textContent).toBe("const");
    expect(span.querySelector(".syntax-keyword")).toBe(token);
  });
});
