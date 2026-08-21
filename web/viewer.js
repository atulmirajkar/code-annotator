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
})();
