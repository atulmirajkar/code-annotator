interface MermaidRenderResult {
  svg: string;
  bindFunctions?: (element: HTMLElement) => void;
}

interface MermaidApi {
  initialize(options: Record<string, unknown>): void;
  render(id: string, definition: string): Promise<MermaidRenderResult>;
}

declare const mermaid: MermaidApi;

export interface MermaidEnvironment {
  document: Document;
  api: MermaidApi;
  prefersDark: boolean;
  definitions: ReadonlyMap<string, string>;
}

export async function initializeMermaid(environment: MermaidEnvironment): Promise<void> {
  const { document } = environment;

  function requiredElement<T extends Element>(value: T | null, label: string): T {
    if (!value) throw new Error(`Missing ${label} in Mermaid template`);
    return value;
  }

  const maxDiagramCharacters = 100000;
  const diagrams = document.querySelectorAll<HTMLElement>(".mermaid-diagram");
  if (diagrams.length === 0) {
    return;
  }

  environment.api.initialize({
    startOnLoad: false,
    securityLevel: "strict",
    suppressErrorRendering: true,
    theme: environment.prefersDark ? "dark" : "default",
  });

  await Promise.all(Array.from(diagrams).map(async (diagram, index) => {
    const output = requiredElement(diagram.querySelector<HTMLElement>(".mermaid-output"), "Mermaid output");
    const error = requiredElement(diagram.querySelector<HTMLElement>(".mermaid-error"), "Mermaid error");
    const definition = environment.definitions.get(diagram.id);

    try {
      if (definition === undefined) throw new Error("typed diagram source is unavailable");
      if (definition.length > maxDiagramCharacters) {
        throw new Error(`diagram exceeds ${maxDiagramCharacters} characters`);
      }
      const rendered = await environment.api.render(`code-annotator-mermaid-${index}`, definition);
      output.innerHTML = rendered.svg;
      rendered.bindFunctions?.(output);
    } catch (cause) {
      output.hidden = true;
      error.textContent = `Could not render diagram: ${cause instanceof Error ? cause.message : "unknown error"}`;
      error.hidden = false;
      const sourceDetails = diagram.querySelector<HTMLDetailsElement>(".mermaid-source");
      if (sourceDetails) sourceDetails.open = true;
    }
  }));
}

export async function initializeMermaidPage(): Promise<void> {
  if (typeof mermaid === "undefined") return;
  const prefix = "/view/";
  const mode = new URLSearchParams(window.location.search).get("mode") === "diff" ? "diff" : "file";
  const documentPath = window.location.pathname.startsWith(prefix)
    ? decodeURIComponent(window.location.pathname.slice(prefix.length))
    : (await fetchDocumentCatalogState()).selectedPath;
  if (!documentPath) return;
  const state = await fetchViewerState(documentPath, mode);
  await initializeMermaid({
    document,
    api: mermaid,
    prefersDark: window.matchMedia("(prefers-color-scheme: dark)").matches,
    definitions: new Map(Array.from(state.document.diagrams.values()).map((position) => [position.elementId, position.text])),
  });
}

if (typeof mermaid !== "undefined") void initializeMermaidPage();
import { fetchDocumentCatalogState } from "./document-state.js";
import { fetchViewerState } from "./viewer-state.js";
