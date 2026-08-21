(() => {
  "use strict";

  const panel = document.querySelector(".review-panel");
  if (!panel) return;

  const list = panel.querySelector(".annotation-list");
  const count = panel.querySelector(".annotation-count");
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
