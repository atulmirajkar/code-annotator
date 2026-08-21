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
  const form = panel.querySelector(".annotation-form");
  const formStatus = panel.querySelector(".annotation-form-status");
  const submitButton = form.querySelector('button[type="submit"]');
  const selectionScope = form.querySelector('input[name="scope"][value="selection"]');
  const documentScope = form.querySelector('input[name="scope"][value="document"]');
  const reviewToken = document.querySelector('meta[name="md-viewer-review-token"]')?.content || "";
  const documentPath = panel.dataset.document;
  let currentRevision = "";
  let pendingSelection = null;
  let preserveSelection = false;
  if (!documentPath) {
    showMessage("Open a Markdown document to review annotations.");
    return;
  }

  loadAnnotations();
  form.addEventListener("submit", submitAnnotation);
  panel.addEventListener("pointerdown", () => { preserveSelection = true; });
  document.addEventListener("pointerup", () => {
    window.setTimeout(() => { preserveSelection = false; }, 0);
  });

  if (markdown) {
    // selectionchange covers mouse, touch, and keyboard expansion regardless
    // of which element owns focus or where a drag gesture ends.
    document.addEventListener("selectionchange", updateSelectionPreview);
  }

  // Map any ordered pair of source-backed endpoints. Byte gaps may contain
  // Markdown delimiters; the server derives the exact source after verifying
  // the document digest instead of asking the browser to reconstruct them.
  function updateSelectionPreview() {
    const selection = window.getSelection();
    if (!selection || selection.rangeCount !== 1 || selection.isCollapsed) {
      clearSelectionPreview();
      return;
    }
    const range = selection.getRangeAt(0);
    const startSpan = sourceSpan(range.startContainer);
    const endSpan = sourceSpan(range.endContainer);
    if (!startSpan || !endSpan || !markdown.contains(startSpan) || !markdown.contains(endSpan)) {
      clearSelectionPreview();
      return;
    }

    const sourceStart = Number.parseInt(startSpan.dataset.sourceStart, 10);
    const sourceEnd = Number.parseInt(endSpan.dataset.sourceEnd, 10);
    const spans = sourceSpanRange(startSpan, endSpan);
    const documentSHA256 = markdown.dataset.documentSha256;
    const startOffset = textOffset(startSpan, range.startContainer, range.startOffset);
    const endOffset = textOffset(endSpan, range.endContainer, range.endOffset);
    const exact = range.toString();
    if (!Number.isInteger(sourceStart) || !Number.isInteger(sourceEnd) || !spans || !documentSHA256 || startOffset < 0 || endOffset < 0 || !exact) {
      clearSelectionPreview();
      return;
    }

    const startByte = sourceStart + utf8Length((startSpan.textContent || "").slice(0, startOffset));
    const endByte = Number.parseInt(endSpan.dataset.sourceStart, 10) + utf8Length((endSpan.textContent || "").slice(0, endOffset));
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
  }

  function sourceSpan(node) {
    const element = node.nodeType === Node.ELEMENT_NODE ? node : node.parentElement;
    return element ? element.closest(".source-text") : null;
  }

  // Confirm that the selection endpoints occur in document source order.
  function sourceSpanRange(startSpan, endSpan) {
    const spans = Array.from(markdown.querySelectorAll(".source-text"));
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

  function codeBlock(span) {
    const code = span.closest("code");
    return code && code.parentElement && code.parentElement.tagName === "PRE" ? code : null;
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
    if ((preserveSelection || panel.contains(document.activeElement)) && pendingSelection) return;
    preview.hidden = true;
    delete preview.dataset.startByte;
    delete preview.dataset.endByte;
    delete preview.dataset.exact;
    delete preview.dataset.documentSha256;
    previewQuote.textContent = "";
    previewRange.textContent = "";
    pendingSelection = null;
    selectionScope.disabled = true;
    documentScope.checked = true;
  }

  async function loadAnnotations() {
    try {
      const response = await fetch(`/api/annotations?document=${encodeURIComponent(documentPath)}`, {
        headers: { Accept: "application/json" },
      });
      if (!response.ok) throw new Error(`annotation request failed: ${response.status}`);
      const payload = await response.json();
      currentRevision = typeof payload.revision === "string" ? payload.revision : "";
      renderAnnotations(payload);
    } catch (_) {
      showMessage("Could not load annotations. Refresh to try again.");
    }
  }

  async function submitAnnotation(event) {
    event.preventDefault();
    setFormStatus("Saving…");
    submitButton.disabled = true;

    const fields = new FormData(form);
    const payload = {
      document: documentPath,
      intent: fields.get("intent"),
      comment: fields.get("comment"),
      author: fields.get("author"),
    };
    if (fields.get("scope") === "selection" && pendingSelection) {
      payload.selection = pendingSelection;
    }

    try {
      const response = await fetch("/api/annotations", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "If-Match": JSON.stringify(currentRevision),
          "X-MD-Viewer-Token": reviewToken,
        },
        body: JSON.stringify(payload),
      });
      if (!response.ok) {
        if (response.status === 409) {
          await loadAnnotations();
          throw new Error("The document or annotations changed. Refresh and select again.");
        }
        throw new Error((await response.text()).trim() || `Could not save annotation (${response.status}).`);
      }

      form.elements.comment.value = "";
      window.getSelection()?.removeAllRanges();
      forceClearSelectionPreview();
      await loadAnnotations();
      setFormStatus("Annotation added.");
    } catch (error) {
      setFormStatus(error.message || "Could not save annotation.", true);
    } finally {
      submitButton.disabled = false;
    }
  }

  function forceClearSelectionPreview() {
    pendingSelection = null;
    preview.hidden = true;
    previewQuote.textContent = "";
    previewRange.textContent = "";
    selectionScope.disabled = true;
    documentScope.checked = true;
  }

  function setFormStatus(message, error = false) {
    formStatus.textContent = message;
    formStatus.classList.toggle("error", error);
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
