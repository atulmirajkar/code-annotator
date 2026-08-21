(() => {
  "use strict";

  const panel = document.querySelector(".review-panel");
  if (!panel) return;

  const list = panel.querySelector(".annotation-list");
  const count = panel.querySelector(".annotation-count");
  const preview = panel.querySelector(".selection-preview");
  const previewQuote = panel.querySelector(".selection-quote");
  const previewRange = panel.querySelector(".selection-range");
  const markdown = document.querySelector(".markdown-body");
  const documentPath = panel.dataset.document;
  if (!documentPath) {
    showMessage("Open a Markdown document to review annotations.");
    return;
  }

  fetch(`/api/annotations?document=${encodeURIComponent(documentPath)}`, {
    headers: { Accept: "application/json" },
  })
    .then((response) => {
      if (!response.ok) throw new Error(`annotation request failed: ${response.status}`);
      return response.json();
    })
    .then(renderAnnotations)
    .catch(() => showMessage("Could not load annotations. Refresh to try again."));

  if (markdown) {
    markdown.addEventListener("mouseup", updateSelectionPreview);
    markdown.addEventListener("keyup", updateSelectionPreview);
  }

  // Map a selection only when both endpoints belong to one annotated source
  // span. Crossing spans could include invisible Markdown delimiters and is
  // intentionally deferred until the mapping can remain exact.
  function updateSelectionPreview() {
    const selection = window.getSelection();
    if (!selection || selection.rangeCount !== 1 || selection.isCollapsed) {
      clearSelectionPreview();
      return;
    }
    const range = selection.getRangeAt(0);
    const startSpan = sourceSpan(range.startContainer);
    const endSpan = sourceSpan(range.endContainer);
    if (!startSpan || startSpan !== endSpan || !markdown.contains(startSpan)) {
      clearSelectionPreview();
      return;
    }

    const sourceStart = Number.parseInt(startSpan.dataset.sourceStart, 10);
    const sourceEnd = Number.parseInt(startSpan.dataset.sourceEnd, 10);
    const spanText = startSpan.textContent || "";
    const startOffset = textOffset(startSpan, range.startContainer, range.startOffset);
    const endOffset = textOffset(startSpan, range.endContainer, range.endOffset);
    const exact = range.toString();
    if (!Number.isInteger(sourceStart) || !Number.isInteger(sourceEnd) || startOffset < 0 || endOffset <= startOffset || !exact) {
      clearSelectionPreview();
      return;
    }

    const startByte = sourceStart + utf8Length(spanText.slice(0, startOffset));
    const endByte = sourceStart + utf8Length(spanText.slice(0, endOffset));
    if (endByte > sourceEnd) {
      clearSelectionPreview();
      return;
    }
    preview.dataset.startByte = String(startByte);
    preview.dataset.endByte = String(endByte);
    preview.dataset.exact = exact;
    previewQuote.textContent = exact;
    previewRange.textContent = `bytes ${startByte}–${endByte}`;
    preview.hidden = false;
  }

  function sourceSpan(node) {
    const element = node.nodeType === Node.ELEMENT_NODE ? node : node.parentElement;
    return element ? element.closest(".source-text") : null;
  }

  // Convert a DOM boundary into a UTF-16 text offset within its source span.
  function textOffset(span, boundaryNode, boundaryOffset) {
    const range = document.createRange();
    range.selectNodeContents(span);
    try {
      range.setEnd(boundaryNode, boundaryOffset);
    } catch (_) {
      return -1;
    }
    return range.toString().length;
  }

  function utf8Length(value) {
    return new TextEncoder().encode(value).length;
  }

  function clearSelectionPreview() {
    preview.hidden = true;
    delete preview.dataset.startByte;
    delete preview.dataset.endByte;
    delete preview.dataset.exact;
    previewQuote.textContent = "";
    previewRange.textContent = "";
  }

  // Render user-controlled content with textContent so comments and author
  // names can never become executable markup.
  function renderAnnotations(payload) {
    list.replaceChildren();
    const annotations = Array.isArray(payload.annotations) ? payload.annotations : [];
    count.textContent = String(annotations.length);
    if (annotations.length === 0) {
      showMessage("No annotations for this document.");
      return;
    }
    annotations.forEach((annotation) => list.append(createCard(annotation)));
  }

  function createCard(annotation) {
    const card = element("article", "annotation-card");
    card.dataset.annotationId = annotation.id || "";

    const meta = element("div", "annotation-meta");
    meta.append(badge(annotation.intent || "comment"), badge(annotation.status || "open"));
    if (annotation.anchor && annotation.anchor.state === "stale") {
      meta.append(badge("stale", "stale"));
    }
    card.append(meta);

    const comment = element("p", "annotation-comment");
    comment.textContent = annotation.comment || "";
    card.append(comment);

    const author = element("p", "annotation-author");
    author.textContent = annotation.author ? `By ${annotation.author}` : "Unknown author";
    card.append(author);

    if (Array.isArray(annotation.thread) && annotation.thread.length > 0) {
      const thread = element("ol", "annotation-thread");
      annotation.thread.forEach((entry) => {
        const item = document.createElement("li");
        item.textContent = `${entry.author || "Unknown"}: ${threadText(entry)}`;
        thread.append(item);
      });
      card.append(thread);
    }
    return card;
  }

  function threadText(entry) {
    return entry.message || entry.summary || `${entry.fromStatus || ""} → ${entry.toStatus || ""}`;
  }

  function badge(text, extraClass = "") {
    const item = element("span", `annotation-badge ${extraClass}`.trim());
    item.textContent = String(text).replaceAll("_", " ");
    return item;
  }

  function element(tag, className) {
    const item = document.createElement(tag);
    item.className = className;
    return item;
  }

  function showMessage(message) {
    list.replaceChildren();
    const item = element("p", "review-message");
    item.textContent = message;
    list.append(item);
    count.textContent = "";
  }
})();
