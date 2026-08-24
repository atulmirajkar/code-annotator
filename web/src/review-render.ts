import { badge, element } from "./review-dom.js";
import { annotationTurnBadge, showThreadEntry, threadKind, threadText } from "./review-thread.js";
import type { Annotation, AnnotationPayload } from "./types.js";

interface AnnotationRendererOptions {
  list: HTMLElement;
  count: HTMLElement;
  showInactive: HTMLInputElement;
  renderAnnotationHighlights: (annotations: Annotation[]) => void;
  navigateFromAnnotation: (event: MouseEvent, card: HTMLDetailsElement, annotation: Annotation) => void;
  createQuickClose: (annotation: Annotation) => HTMLButtonElement;
  createReattachForm: (annotation: Annotation) => HTMLFormElement;
  createReplyForm: (annotation: Annotation) => HTMLFormElement;
  createLifecycleForm: (annotation: Annotation) => HTMLFormElement | null;
}

export function createAnnotationRenderer({
  list,
  count,
  showInactive,
  renderAnnotationHighlights,
  navigateFromAnnotation,
  createQuickClose,
  createReattachForm,
  createReplyForm,
  createLifecycleForm,
}: AnnotationRendererOptions) {
  let annotationPayload: AnnotationPayload | null = null;

  // Render user-controlled content with textContent so comments can never
  // become executable markup.
  function renderAnnotations(payload: AnnotationPayload): void {
    annotationPayload = payload;
    list.replaceChildren();
    const annotations = Array.isArray(payload.annotations) ? payload.annotations : [];
    const active = annotations.filter((annotation) => !isInactive(annotation));
    const visible = showInactive.checked ? annotations : active;
    renderAnnotationHighlights(visible);
    count.textContent = annotations.length === active.length
      ? String(active.length)
      : `${active.length} active · ${annotations.length} total`;
    if (visible.length === 0) {
      showMessage(annotations.length === 0 ? "No annotations for this document." : "No active annotations.");
      return;
    }
    visible.forEach((annotation) => list.append(createCard(annotation)));
  }

  // Closed and rejected annotations retain their history in storage but are
  // inactive for the current review, so they do not appear or highlight text
  // unless the reviewer explicitly asks to inspect them.
  function isInactive(annotation: Annotation): boolean {
    return annotation.status === "closed" || annotation.status === "rejected";
  }

  function createCard(annotation: Annotation): HTMLDetailsElement {
    const card = element("details", "annotation-card");
    card.dataset.annotationId = annotation.id || "";

    const summary = element("summary", "annotation-summary");
    summary.title = "Show this annotation in the document";

    const meta = element("span", "annotation-meta");
    meta.append(badge(annotation.intent || "comment"), badge(annotation.status || "open"));
    const turnBadge = annotationTurnBadge(annotation);
    if (turnBadge) meta.append(badge(turnBadge.label, turnBadge.className));
    if (annotation.anchor && annotation.anchor.state === "stale") {
      meta.append(badge("stale", "stale"));
    }
    const summaryComment = element("span", "annotation-summary-comment");
    summaryComment.textContent = annotation.comment || "Annotation";
    summary.append(meta, summaryComment);
    summary.addEventListener("click", (event) => navigateFromAnnotation(event, card, annotation));
    card.append(summary);

    const body = element("div", "annotation-card-body");

    const navigationStatus = element("p", "annotation-navigation-status");
    navigationStatus.setAttribute("role", "status");
    body.append(navigationStatus);

    const source = element("div", "annotation-source");
    if (annotation.needsReattachment) {
      source.textContent = "Original selection unavailable; reattach required.";
      source.classList.add("stale-selection");
    } else if (annotation.source && annotation.source.selector) {
      const quote = document.createElement("q");
      quote.textContent = annotation.source.selector.exact || "";
      source.append(quote);
      const lines = element("span", "annotation-source-lines");
      const startLine = annotation.source.selector.startLine;
      const endLine = annotation.source.selector.endLine;
      lines.textContent = startLine === endLine ? `Line ${startLine}` : `Lines ${startLine}–${endLine}`;
      source.append(lines);
    } else {
      source.textContent = "Whole document";
      source.classList.add("document-level");
    }
    body.append(source);

    const comment = element("p", "annotation-comment");
    comment.textContent = annotation.comment || "";
    body.append(comment);

    const role = element("p", "annotation-author");
    role.textContent = `As ${annotation.role}`;
    body.append(role);

    const visibleThread = Array.isArray(annotation.thread) ? annotation.thread.filter(showThreadEntry) : [];
    if (visibleThread.length > 0) {
      const thread = element("ol", "annotation-thread");
      visibleThread.forEach((entry) => {
        const item = document.createElement("li");
        const kind = threadKind(entry);
        item.className = `annotation-thread-entry ${kind.className}`;
        item.dataset.kind = entry.kind || "";
        item.dataset.role = entry.role;

        const header = element("div", "annotation-thread-header");
        const kindBadge = element("span", "annotation-thread-kind");
        kindBadge.textContent = kind.label;
        const role = element("span", "annotation-thread-author");
        role.textContent = entry.role;
        header.append(kindBadge, role);

        const body = element("p", "annotation-thread-body");
        body.textContent = threadText(entry);
        item.append(header, body);
        thread.append(item);
      });
      body.append(thread);
    }

    const actionBar = element("div", "annotation-action-bar");
    const actions = element("details", "annotation-actions");
    const actionsSummary = document.createElement("summary");
    actionsSummary.textContent = "Actions";
    actions.append(actionsSummary);
    if (annotation.anchor && annotation.anchor.state === "stale") {
      actions.append(createReattachForm(annotation));
    }
    actions.append(createReplyForm(annotation));
    const lifecycle = createLifecycleForm(annotation);
    if (lifecycle) actions.append(lifecycle);
    actionBar.append(actions);
    if (annotation.status === "applied") {
      actionBar.append(createQuickClose(annotation));
    }
    body.append(actionBar);
    card.append(body);
    return card;
  }

  function showMessage(message: string): void {
    list.replaceChildren();
    const item = element("p", "review-message");
    item.textContent = message;
    list.append(item);
    count.textContent = "";
  }

  function currentPayload(): AnnotationPayload | null {
    return annotationPayload;
  }

  return {
    currentPayload,
    renderAnnotations,
    showMessage,
  };
}
