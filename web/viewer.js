(() => {
  "use strict";

  const layout = document.querySelector(".layout");
  if (!layout) return;

  bindPanelToggle({
    button: document.querySelector(".documents-toggle"),
    panel: document.querySelector("#documents-sidebar"),
    collapsedClass: "documents-collapsed",
    name: "documents",
  });
  bindPanelToggle({
    button: document.querySelector(".review-toggle"),
    panel: document.querySelector("#annotation-sidebar"),
    collapsedClass: "review-collapsed",
    name: "annotations",
  });
  bindDocumentSearch();

  // bindPanelToggle keeps the visual state, accessible state, and grid layout
  // synchronized for one optional viewer panel.
  function bindPanelToggle({ button, panel, collapsedClass, name }) {
    if (!button || !panel) return;
    button.addEventListener("click", () => {
      const collapsed = !panel.hidden;
      panel.hidden = collapsed;
      layout.classList.toggle(collapsedClass, collapsed);
      button.setAttribute("aria-expanded", String(!collapsed));
      button.textContent = `${collapsed ? "Show" : "Hide"} ${name}`;
    });
  }

  // bindDocumentSearch filters by the displayed slash-separated relative path.
  // Enter opens the first match, while slash focuses lookup from document view.
  function bindDocumentSearch() {
    const input = document.querySelector(".document-search input");
    const status = document.querySelector(".document-search-status");
    const items = Array.from(document.querySelectorAll(".documents li"));
    if (!input || !status || items.length === 0) return;

    const visibleLinks = () => items.filter((item) => !item.hidden).map((item) => item.querySelector("a"));
    const filter = () => {
      const query = input.value.trim().toLocaleLowerCase();
      let matches = 0;
      items.forEach((item) => {
        const path = (item.textContent || "").trim().toLocaleLowerCase();
        item.hidden = Boolean(query) && !path.includes(query);
        if (!item.hidden) matches++;
      });
      status.hidden = !query;
      status.textContent = matches === 0 ? "No matching documents." : `${matches} matching document${matches === 1 ? "" : "s"}.`;
    };

    input.addEventListener("input", filter);
    input.addEventListener("keydown", (event) => {
      if (event.key === "Escape") {
        input.value = "";
        filter();
      } else if (event.key === "Enter") {
        visibleLinks()[0]?.click();
      } else if (event.key === "ArrowDown") {
        event.preventDefault();
        visibleLinks()[0]?.focus();
      }
    });
    document.addEventListener("keydown", (event) => {
      const target = event.target;
      const editing = target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement || target instanceof HTMLSelectElement || target?.isContentEditable;
      if (event.key === "/" && !editing && !event.metaKey && !event.ctrlKey && !event.altKey) {
        event.preventDefault();
        input.focus();
      }
    });
  }
})();
