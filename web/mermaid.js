(() => {
  "use strict";

  const maxDiagramCharacters = 100000;
  const diagrams = document.querySelectorAll(".mermaid-diagram");
  if (diagrams.length === 0) {
    return;
  }

  mermaid.initialize({
    startOnLoad: false,
    securityLevel: "strict",
    suppressErrorRendering: true,
    theme: window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "default",
  });

  diagrams.forEach(async (diagram, index) => {
    const source = diagram.querySelector(".mermaid-source code");
    const output = diagram.querySelector(".mermaid-output");
    const error = diagram.querySelector(".mermaid-error");
    const definition = source.textContent;

    try {
      if (definition.length > maxDiagramCharacters) {
        throw new Error(`diagram exceeds ${maxDiagramCharacters} characters`);
      }
      const rendered = await mermaid.render(`md-viewer-mermaid-${index}`, definition);
      output.innerHTML = rendered.svg;
      if (diagram.dataset.sourceStart && diagram.dataset.sourceEnd) {
        output.tabIndex = 0;
        output.setAttribute("aria-label", "Rendered Mermaid diagram. Select the complete diagram for annotation.");
      }
      rendered.bindFunctions?.(output);
    } catch (cause) {
      output.hidden = true;
      error.textContent = `Could not render diagram: ${cause instanceof Error ? cause.message : "unknown error"}`;
      error.hidden = false;
      diagram.querySelector(".mermaid-source").open = true;
    }
  });
})();
