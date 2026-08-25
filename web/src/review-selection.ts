import type { DiagramPosition, SourcePosition } from "./viewer-state.js";

export interface SelectionPayload {
  startByte: number;
  endByte: number;
  documentSHA256: string;
}

interface SelectionControllerOptions {
  document: Document;
  window: Window;
  panel: HTMLElement;
  markdown: HTMLElement;
  preview: HTMLElement;
  previewQuote: HTMLElement;
  previewRange: HTMLElement;
  selectionScope: HTMLInputElement;
  documentScope: HTMLInputElement;
  documentSHA256: string;
  sourceNodes: ReadonlyMap<string, SourcePosition>;
  diagrams: ReadonlyMap<string, DiagramPosition>;
  onSelectionChanged: () => void;
}

interface SelectionState {
  pendingSelection: SelectionPayload | null;
  diagramSelectionActive: boolean;
  preserveSelection: boolean;
}

export function createSelectionController(
  options: SelectionControllerOptions,
): SelectionController {
  return new SelectionController(options);
}

class SelectionController {
  private readonly state: SelectionState = {
    pendingSelection: null,
    diagramSelectionActive: false,
    preserveSelection: false,
  };

  constructor(private readonly options: SelectionControllerOptions) {}

  start(): void {
    // Bind at the controller boundary: `.markdown` owns source selection and
    // `.panel` owns the form interaction that must preserve a pending range.
    const { document, markdown, panel } = this.options;
    panel.addEventListener("pointerdown", this.handlePanelPointerDown);
    document.addEventListener("pointerup", this.handleDocumentPointerUp);
    document.addEventListener("selectionchange", this.handleSelectionChange);
    markdown.addEventListener("pointerdown", this.handleMarkdownPointerDown);
    markdown.addEventListener("click", this.handleDiagramClick);
    markdown.addEventListener("keydown", this.handleDiagramKeydown);
    // Reconcile a native range created before the async viewer state loaded.
    this.updateSelectionPreview();
  }

  readonly currentSelection = (): SelectionPayload | null => {
    // The review form reads this typed payload; it never reads DOM IDs or
    // `data-*` attributes to reconstruct source bytes.
    return this.state.pendingSelection;
  };

  readonly forceClearSelectionPreview = (): void => {
    // Used when a new non-diagram pointer interaction invalidates the synthetic
    // `.mermaid-diagram.annotation-selection` presentation marker.
    clearSelectionState(this.options, this.state);
  };

  readonly sourceSpan = (node: Node): HTMLElement | null => {
    return findSourceSpan(node);
  };

  readonly sourceSpanRange = (
    startSpan: HTMLElement,
    endSpan: HTMLElement,
  ): HTMLElement[] | null => {
    return findSourceSpanRange(this.options, startSpan, endSpan);
  };

  readonly utf8Length = (value: string): number => {
    return utf8Length(value);
  };

  private readonly handlePanelPointerDown = (): void => {
    this.state.preserveSelection = true;
  };

  private readonly handleDocumentPointerUp = (): void => {
    this.options.window.setTimeout(() => {
      this.state.preserveSelection = false;
    }, 0);
  };

  private readonly handleSelectionChange = (): void => {
    this.updateSelectionPreview();
  };

  private readonly handleMarkdownPointerDown = (event: PointerEvent): void => {
    const target = event.target instanceof Element ? event.target : null;
    if (
      this.state.diagramSelectionActive &&
      !target?.closest(".mermaid-output")
    )
      this.forceClearSelectionPreview();
  };

  private readonly handleDiagramClick = (event: MouseEvent): void => {
    const target = event.target instanceof Element ? event.target : null;
    const output = target?.closest(".mermaid-output");
    if (output)
      this.captureDiagramSelection(output.closest(".mermaid-diagram"));
  };

  private readonly handleDiagramKeydown = (event: KeyboardEvent): void => {
    if (event.key !== "Enter" && event.key !== " ") return;
    const target = event.target instanceof Element ? event.target : null;
    const output = target?.closest(".mermaid-output");
    if (!output) return;
    event.preventDefault();
    this.captureDiagramSelection(output.closest(".mermaid-diagram"));
  };

  private captureDiagramSelection(diagram: HTMLElement | null): void {
    // Mermaid SVG text has no source mapping, so selecting its `.mermaid-diagram`
    // element uses the complete fenced definition from typed viewer state.
    const {
      documentSHA256,
      diagrams,
      markdown,
      onSelectionChanged,
      preview,
      previewQuote,
      previewRange,
      selectionScope,
      window,
    } = this.options;
    if (!diagram) return;
    const position = diagrams.get(diagram.id);
    const exact = position?.text || "";
    if (
      !position ||
      position.endByte <= position.startByte ||
      !documentSHA256 ||
      !exact
    )
      return;
    this.state.diagramSelectionActive = true;
    window.getSelection()?.removeAllRanges();
    markdown
      .querySelectorAll(".mermaid-diagram.annotation-selection")
      .forEach((item) => item.classList.remove("annotation-selection"));
    diagram.classList.add("annotation-selection");
    this.state.pendingSelection = {
      startByte: position.startByte,
      endByte: position.endByte,
      documentSHA256,
    };
    selectionScope.disabled = false;
    selectionScope.checked = true;
    previewQuote.textContent = exact;
    previewRange.textContent = `bytes ${position.startByte}–${position.endByte} · complete diagram`;
    preview.hidden = false;
    onSelectionChanged();
  }

  private updateSelectionPreview(): void {
    updateSelectionPreview(this.options, this.state);
  }
}

function updateSelectionPreview(
  options: SelectionControllerOptions,
  state: SelectionState,
): void {
  // Native selection endpoints may be inside nested `.source-text` descendants;
  // convert them to bytes while excluding line numbers and diff markers.
  const {
    document,
    documentSHA256,
    markdown,
    onSelectionChanged,
    preview,
    previewQuote,
    previewRange,
    selectionScope,
    window,
  } = options;
  const selection = window.getSelection();
  if (!selection || selection.rangeCount !== 1 || selection.isCollapsed) {
    if (state.diagramSelectionActive && state.pendingSelection) return;
    clearSelectionPreview(options, state);
    return;
  }
  state.diagramSelectionActive = false;
  markdown
    .querySelectorAll(".mermaid-diagram.annotation-selection")
    .forEach((diagram) => diagram.classList.remove("annotation-selection"));
  const range = selection.getRangeAt(0);
  const startSpan = findSourceSpan(range.startContainer);
  const endSpan = findSourceSpan(range.endContainer);
  if (
    !startSpan ||
    !endSpan ||
    !markdown.contains(startSpan) ||
    !markdown.contains(endSpan)
  ) {
    clearSelectionPreview(options, state);
    return;
  }
  const startPosition = options.sourceNodes.get(startSpan.id);
  const endPosition = options.sourceNodes.get(endSpan.id);
  const spans = findSourceSpanRange(options, startSpan, endSpan);
  const startOffset = textOffset(
    document,
    startSpan,
    range.startContainer,
    range.startOffset,
  );
  const endOffset = textOffset(
    document,
    endSpan,
    range.endContainer,
    range.endOffset,
  );
  const exact = selectionPreviewText(
    range,
    startSpan,
    endSpan,
    startOffset,
    endOffset,
  );
  if (
    !startPosition ||
    !endPosition ||
    !spans ||
    !documentSHA256 ||
    startOffset < 0 ||
    endOffset < 0 ||
    !exact
  ) {
    clearSelectionPreview(options, state);
    return;
  }
  const startByte =
    startPosition.startByte +
    utf8Length((startSpan.textContent || "").slice(0, startOffset));
  const endByte =
    endPosition.startByte +
    utf8Length((endSpan.textContent || "").slice(0, endOffset));
  if (endByte > endPosition.endByte) {
    clearSelectionPreview(options, state);
    return;
  }
  state.pendingSelection = { startByte, endByte, documentSHA256 };
  selectionScope.disabled = false;
  selectionScope.checked = true;
  previewQuote.textContent = exact;
  previewRange.textContent = `bytes ${startByte}–${endByte}`;
  preview.hidden = false;
  onSelectionChanged();
}

function findSourceSpan(node: Node): HTMLElement | null {
  // Normal source selection lands in `#source-* .source-text`. Empty lines have
  // no text node, so their row fallback finds the row's empty source span.
  const element = node instanceof HTMLElement ? node : node.parentElement;
  if (!element) return null;
  const direct = element.closest<HTMLElement>(".source-text");
  if (direct) return direct;
  return (
    element
      .closest<HTMLElement>(".source-line, .diff-current")
      ?.querySelector<HTMLElement>(".source-text") || null
  );
}

function selectionPreviewText(
  range: Range,
  startSpan: HTMLElement,
  endSpan: HTMLElement,
  startOffset: number,
  endOffset: number,
): string {
  // Rebuild line-oriented markup from `.source-line`/`.diff-current` rows so
  // `<span class="source-line-number">` and `.diff-marker` never enter quotes.
  const startRow = startSpan.closest<HTMLElement>(
    ".source-line, .diff-current",
  );
  const endRow = endSpan.closest<HTMLElement>(".source-line, .diff-current");
  if (!startRow || !endRow || startRow.parentElement !== endRow.parentElement)
    return range.toString();
  const parent = startRow.parentElement;
  if (!parent) return "";
  const rows = Array.from(parent.children).filter(
    (row): row is HTMLElement =>
      row instanceof HTMLElement && row.matches(".source-line, .diff-current"),
  );
  const startIndex = rows.indexOf(startRow);
  const endIndex = rows.indexOf(endRow);
  if (startIndex < 0 || endIndex < startIndex) return "";
  if (startIndex === endIndex)
    return (startSpan.textContent || "").slice(startOffset, endOffset);
  return rows
    .slice(startIndex, endIndex + 1)
    .map((row, index, selectedRows) => {
      const text = row.querySelector(".source-text")?.textContent || "";
      if (index === 0) return text.slice(startOffset);
      if (index === selectedRows.length - 1) return text.slice(0, endOffset);
      return text;
    })
    .join("\n");
}

// Confirm endpoints are ordered and do not mix a block-code region with prose.
function findSourceSpanRange(
  options: SelectionControllerOptions,
  startSpan: HTMLElement,
  endSpan: HTMLElement,
): HTMLElement[] | null {
  const { document, markdown, sourceNodes } = options;
  const spans = Array.from(sourceNodes.keys())
    .map((elementId) => document.getElementById(elementId))
    .filter(
      (element): element is HTMLElement =>
        element instanceof HTMLElement && markdown.contains(element),
    );
  const startIndex = spans.indexOf(startSpan);
  const endIndex = spans.indexOf(endSpan);
  if (startIndex < 0 || endIndex < startIndex) return null;
  const selected = spans.slice(startIndex, endIndex + 1);
  const startBlock = codeBlock(startSpan);
  const endBlock = codeBlock(endSpan);
  if (startBlock || endBlock)
    return startBlock && startBlock === endBlock ? selected : null;
  return selected.some((span) => codeBlock(span)) ? null : selected;
}

function codeBlock(span: HTMLElement): HTMLElement | null {
  const code = span.closest<HTMLElement>("code");
  return code && code.parentElement?.tagName === "PRE" ? code : null;
}

function textOffset(
  document: Document,
  span: HTMLElement,
  boundaryNode: Node,
  boundaryOffset: number,
): number {
  // Range text measures through nested token elements while ignoring their
  // presentation classes, yielding a UTF-16 offset within `.source-text`.
  if (
    !(span.textContent || "") &&
    span.closest(".source-line, .diff-current")?.contains(boundaryNode)
  )
    return 0;
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

function clearSelectionPreview(
  options: SelectionControllerOptions,
  state: SelectionState,
): void {
  // Preserve the preview while the annotation panel owns focus during a form
  // submission; otherwise remove the transient selection presentation.
  if (
    (state.preserveSelection ||
      options.panel.contains(options.document.activeElement)) &&
    state.pendingSelection
  )
    return;
  clearSelectionState(options, state);
}

function clearSelectionState(
  options: SelectionControllerOptions,
  state: SelectionState,
): void {
  // Reset both typed selection state and its visible preview/selection classes.
  state.pendingSelection = null;
  state.diagramSelectionActive = false;
  options.markdown
    .querySelectorAll(".mermaid-diagram.annotation-selection")
    .forEach((diagram) => diagram.classList.remove("annotation-selection"));
  options.preview.hidden = true;
  options.previewQuote.textContent = "";
  options.previewRange.textContent = "";
  options.selectionScope.disabled = true;
  options.documentScope.checked = true;
  options.onSelectionChanged();
}
