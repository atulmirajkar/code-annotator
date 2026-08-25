export async function initializeMermaid(environment) {
    const { document } = environment;
    function requiredElement(value, label) {
        if (!value)
            throw new Error(`Missing ${label} in Mermaid template`);
        return value;
    }
    const maxDiagramCharacters = 100000;
    const diagrams = document.querySelectorAll(".mermaid-diagram");
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
        const output = requiredElement(diagram.querySelector(".mermaid-output"), "Mermaid output");
        const error = requiredElement(diagram.querySelector(".mermaid-error"), "Mermaid error");
        const definition = environment.definitions.get(diagram.id);
        try {
            if (definition === undefined)
                throw new Error("typed diagram source is unavailable");
            if (definition.length > maxDiagramCharacters) {
                throw new Error(`diagram exceeds ${maxDiagramCharacters} characters`);
            }
            const rendered = await environment.api.render(`code-annotator-mermaid-${index}`, definition);
            output.innerHTML = rendered.svg;
            rendered.bindFunctions?.(output);
        }
        catch (cause) {
            output.hidden = true;
            error.textContent = `Could not render diagram: ${cause instanceof Error ? cause.message : "unknown error"}`;
            error.hidden = false;
            const sourceDetails = diagram.querySelector(".mermaid-source");
            if (sourceDetails)
                sourceDetails.open = true;
        }
    }));
}
export async function initializeMermaidPage() {
    if (typeof mermaid === "undefined")
        return;
    const prefix = "/view/";
    const mode = new URLSearchParams(window.location.search).get("mode") === "diff" ? "diff" : "file";
    const documentPath = window.location.pathname.startsWith(prefix)
        ? decodeURIComponent(window.location.pathname.slice(prefix.length))
        : (await fetchDocumentCatalogState()).selectedPath;
    if (!documentPath)
        return;
    const state = await fetchViewerState(documentPath, mode);
    await initializeMermaid({
        document,
        api: mermaid,
        prefersDark: window.matchMedia("(prefers-color-scheme: dark)").matches,
        definitions: new Map(Array.from(state.document.diagrams.values()).map((position) => [position.elementId, position.text])),
    });
}
if (typeof mermaid !== "undefined")
    void initializeMermaidPage();
import { fetchDocumentCatalogState } from "./document-state.js";
import { fetchViewerState } from "./viewer-state.js";
