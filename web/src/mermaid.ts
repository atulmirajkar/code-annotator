interface MermaidRenderResult {
  svg: string;
  bindFunctions?: (element: HTMLElement) => void;
}

interface MermaidApi {
  initialize(options: Record<string, unknown>): void;
  render(id: string, definition: string): Promise<MermaidRenderResult>;
}

declare const mermaid: MermaidApi;

(() => {
  "use strict";

  function requiredElement<T extends Element>(value: T | null, label: string): T {
    if (!value) throw new Error(`Missing ${label} in Mermaid template`);
    return value;
  }

  const maxDiagramCharacters = 100000;
  const diagrams = document.querySelectorAll<HTMLElement>(".mermaid-diagram");
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
    const source = requiredElement(diagram.querySelector<HTMLElement>(".mermaid-source code"), "Mermaid source");
    const output = requiredElement(diagram.querySelector<HTMLElement>(".mermaid-output"), "Mermaid output");
    const error = requiredElement(diagram.querySelector<HTMLElement>(".mermaid-error"), "Mermaid error");
    const definition = source.textContent;

    try {
      if (definition.length > maxDiagramCharacters) {
        throw new Error(`diagram exceeds ${maxDiagramCharacters} characters`);
      }
      const rendered = await mermaid.render(`code-annotator-mermaid-${index}`, definition);
      output.innerHTML = rendered.svg;
      rendered.bindFunctions?.(output);
    } catch (cause) {
      output.hidden = true;
      error.textContent = `Could not render diagram: ${cause instanceof Error ? cause.message : "unknown error"}`;
      error.hidden = false;
      const sourceDetails = diagram.querySelector<HTMLDetailsElement>(".mermaid-source");
      if (sourceDetails) sourceDetails.open = true;
    }
  });
})();
