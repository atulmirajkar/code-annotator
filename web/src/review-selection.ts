import type { SelectionPayload } from "./types.js";

interface SelectionControllerOptions {
  panel: HTMLElement;
  markdown: HTMLElement;
  preview: HTMLElement;
  previewQuote: HTMLElement;
  previewRange: HTMLElement;
  selectionScope: HTMLInputElement;
  documentScope: HTMLInputElement;
  onSelectionChanged: () => void;
}

export function createSelectionController({
  panel,
  markdown,
  preview,
  previewQuote,
  previewRange,
  selectionScope,
  documentScope,
  onSelectionChanged,
}: SelectionControllerOptions) {
  let pendingSelection: SelectionPayload | null = null;
  let diagramSelectionActive = false;
  let preserveSelection = false;

  function bind() {
    panel.addEventListener("pointerdown", () => { preserveSelection = true; });
    document.addEventListener("pointerup", () => {
      window.setTimeout(() => { preserveSelection = false; }, 0);
    });

    // selectionchange covers mouse, touch, and keyboard expansion regardless
    // of which element owns focus or where a drag gesture ends.
    document.addEventListener("selectionchange", updateSelectionPreview);
    markdown.addEventListener("pointerdown", clearDiagramSelectionOnPointerdown);
    markdown.addEventListener("click", captureDiagramClick);
    markdown.addEventListener("keydown", captureDiagramKeydown);
  }

  // Beginning a different interaction explicitly releases a synthetic diagram
  // selection. Pointer events inside the diagram itself are handled on click.
  function clearDiagramSelectionOnPointerdown(event: PointerEvent): void {
    const target = event.target instanceof Element ? event.target : null;
    if (diagramSelectionActive && !target?.closest(".mermaid-output")) {
      forceClearSelectionPreview();
    }
  }

  // A rendered diagram has no stable label-to-Markdown mapping. Treat a click
  // anywhere on its SVG as a selection of the complete fenced definition.
  function captureDiagramClick(event: MouseEvent): void {
    const target = event.target instanceof Element ? event.target : null;
    const output = target?.closest(".mermaid-output");
    if (output) captureDiagramSelection(output.closest(".mermaid-diagram"));
  }

  function captureDiagramKeydown(event: KeyboardEvent): void {
    if (event.key !== "Enter" && event.key !== " ") return;
    const target = event.target instanceof Element ? event.target : null;
    const output = target?.closest(".mermaid-output");
    if (!output) return;
    event.preventDefault();
    captureDiagramSelection(output.closest(".mermaid-diagram"));
  }

  function captureDiagramSelection(diagram: HTMLElement | null): void {
    if (!diagram) return;
    const startByte = Number.parseInt(diagram.dataset.sourceStart || "", 10);
    const endByte = Number.parseInt(diagram.dataset.sourceEnd || "", 10);
    const documentSHA256 = markdown.dataset.documentSha256;
    const exact = diagram?.querySelector(".mermaid-source code")?.textContent || "";
    if (!Number.isInteger(startByte) || !Number.isInteger(endByte) || endByte <= startByte || !documentSHA256 || !exact) return;

    // Diagram selection is synthetic: the SVG has no DOM text range that maps
    // safely to Markdown. Keep it active across delayed collapsed
    // selectionchange events until the reviewer starts another interaction.
    diagramSelectionActive = true;
    window.getSelection()?.removeAllRanges();
    markdown.querySelectorAll(".mermaid-diagram.annotation-selection").forEach((item) => item.classList.remove("annotation-selection"));
    diagram.classList.add("annotation-selection");
    preview.dataset.startByte = String(startByte);
    preview.dataset.endByte = String(endByte);
    preview.dataset.exact = exact;
    preview.dataset.documentSha256 = documentSHA256;
    pendingSelection = { startByte, endByte, documentSHA256 };
    selectionScope.disabled = false;
    selectionScope.checked = true;
    previewQuote.textContent = exact;
    previewRange.textContent = `bytes ${startByte}–${endByte} · complete diagram`;
    preview.hidden = false;
    onSelectionChanged();
  }

  // Map any ordered pair of source-backed endpoints. Byte gaps may contain
  // Markdown delimiters; the server derives the exact source after verifying
  // the document digest instead of asking the browser to reconstruct them.
  function updateSelectionPreview(): void {
    const selection = window.getSelection();
    if (!selection || selection.rangeCount !== 1 || selection.isCollapsed) {
      if (diagramSelectionActive && pendingSelection) return;
      clearSelectionPreview();
      return;
    }
    diagramSelectionActive = false;
    markdown.querySelectorAll(".mermaid-diagram.annotation-selection").forEach((diagram) => diagram.classList.remove("annotation-selection"));
    const range = selection.getRangeAt(0);
    const startSpan = sourceSpan(range.startContainer);
    const endSpan = sourceSpan(range.endContainer);
    if (!startSpan || !endSpan || !markdown.contains(startSpan) || !markdown.contains(endSpan)) {
      clearSelectionPreview();
      return;
    }

    const sourceStart = Number.parseInt(startSpan.dataset.sourceStart || "", 10);
    const sourceEnd = Number.parseInt(endSpan.dataset.sourceEnd || "", 10);
    const spans = sourceSpanRange(startSpan, endSpan);
    const documentSHA256 = markdown.dataset.documentSha256;
    const startOffset = textOffset(startSpan, range.startContainer, range.startOffset);
    const endOffset = textOffset(endSpan, range.endContainer, range.endOffset);
    const exact = selectionPreviewText(range, startSpan, endSpan, startOffset, endOffset);
    if (!Number.isInteger(sourceStart) || !Number.isInteger(sourceEnd) || !spans || !documentSHA256 || startOffset < 0 || endOffset < 0 || !exact) {
      clearSelectionPreview();
      return;
    }

    const startByte = sourceStart + utf8Length((startSpan.textContent || "").slice(0, startOffset));
    const endByte = Number.parseInt(endSpan.dataset.sourceStart || "", 10) + utf8Length((endSpan.textContent || "").slice(0, endOffset));
    if (endByte > sourceEnd) {
      clearSelectionPreview();
      return;
    }
    preview.dataset.startByte = String(startByte);
    preview.dataset.endByte = String(endByte);
    preview.dataset.exact = exact;
    preview.dataset.documentSha256 = documentSHA256;
    pendingSelection = { startByte, endByte, documentSHA256 };
    selectionScope.disabled = false;
    selectionScope.checked = true;
    previewQuote.textContent = exact;
    previewRange.textContent = `bytes ${startByte}–${endByte}`;
    preview.hidden = false;
    onSelectionChanged();
  }

  function sourceSpan(node: Node): HTMLElement | null {
    const element = node instanceof HTMLElement ? node : node.parentElement;
    if (!element) return null;
    const direct = element.closest<HTMLElement>(".source-text");
    if (direct) return direct;
    // Empty source lines have a zero-length span with no text node of their
    // own, so a native selection boundary lands on the surrounding row/code.
    return element.closest<HTMLElement>(".source-line, .diff-current")?.querySelector<HTMLElement>(".source-text") || null;
  }

  // Browser Range text includes line-number and diff-marker siblings between
  // endpoints. For line-oriented code, rebuild the preview from source spans
  // only while preserving visually empty intervening rows as newlines.
  function selectionPreviewText(range: Range, startSpan: HTMLElement, endSpan: HTMLElement, startOffset: number, endOffset: number): string {
    const startRow = startSpan.closest<HTMLElement>(".source-line, .diff-current");
    const endRow = endSpan.closest<HTMLElement>(".source-line, .diff-current");
    if (!startRow || !endRow || startRow.parentElement !== endRow.parentElement) {
      return range.toString();
    }

    const parent = startRow.parentElement;
    if (!parent) return "";
    const rows = Array.from(parent.children).filter((row): row is HTMLElement => row instanceof HTMLElement && row.matches(".source-line, .diff-current"));
    const startIndex = rows.indexOf(startRow);
    const endIndex = rows.indexOf(endRow);
    if (startIndex < 0 || endIndex < startIndex) return "";
    if (startIndex === endIndex) {
      return (startSpan.textContent || "").slice(startOffset, endOffset);
    }

    return rows.slice(startIndex, endIndex + 1).map((row, index, selectedRows) => {
      const text = row.querySelector(".source-text")?.textContent || "";
      if (index === 0) return text.slice(startOffset);
      if (index === selectedRows.length - 1) return text.slice(0, endOffset);
      return text;
    }).join("\n");
  }

  // Confirm that the selection endpoints occur in document source order.
  function sourceSpanRange(startSpan: HTMLElement, endSpan: HTMLElement): HTMLElement[] | null {
    const spans = Array.from(markdown.querySelectorAll<HTMLElement>(".source-text"));
    const startIndex = spans.indexOf(startSpan);
    const endIndex = spans.indexOf(endSpan);
    if (startIndex < 0 || endIndex < startIndex) return null;

    const selected = spans.slice(startIndex, endIndex + 1);
    const startBlock = codeBlock(startSpan);
    const endBlock = codeBlock(endSpan);
    if (startBlock || endBlock) {
      return startBlock && startBlock === endBlock ? selected : null;
    }
    // Keep block-code selection pure even when both endpoints are outside it.
    if (selected.some((span) => codeBlock(span))) return null;
    return selected;
  }

  function codeBlock(span: HTMLElement): HTMLElement | null {
    const code = span.closest<HTMLElement>("code");
    return code && code.parentElement && code.parentElement.tagName === "PRE" ? code : null;
  }

  // Convert a DOM boundary into a UTF-16 text offset within its source span.
  function textOffset(span: HTMLElement, boundaryNode: Node, boundaryOffset: number): number {
    if (!(span.textContent || "") && span.closest(".source-line, .diff-current")?.contains(boundaryNode)) {
      return 0;
    }
    const range = document.createRange();
    range.selectNodeContents(span);
    try {
      range.setEnd(boundaryNode, boundaryOffset);
    } catch (_) {
      return -1;
    }
    return range.toString().length;
  }

  function utf8Length(value: string): number {
    return new TextEncoder().encode(value).length;
  }

  function clearSelectionPreview() {
    if ((preserveSelection || panel.contains(document.activeElement)) && pendingSelection) return;
    clearSelectionState();
  }

  function forceClearSelectionPreview() {
    clearSelectionState();
  }

  function clearSelectionState() {
    pendingSelection = null;
    diagramSelectionActive = false;
    markdown.querySelectorAll(".mermaid-diagram.annotation-selection").forEach((diagram) => diagram.classList.remove("annotation-selection"));
    preview.hidden = true;
    delete preview.dataset.startByte;
    delete preview.dataset.endByte;
    delete preview.dataset.exact;
    delete preview.dataset.documentSha256;
    previewQuote.textContent = "";
    previewRange.textContent = "";
    selectionScope.disabled = true;
    documentScope.checked = true;
    onSelectionChanged();
  }

  function currentSelection(): SelectionPayload | null {
    return pendingSelection;
  }

  return {
    bind,
    currentSelection,
    forceClearSelectionPreview,
    sourceSpan,
    sourceSpanRange,
    utf8Length,
  };
}
