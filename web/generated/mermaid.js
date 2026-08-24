"use strict";
(() => {
    "use strict";
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
    mermaid.initialize({
        startOnLoad: false,
        securityLevel: "strict",
        suppressErrorRendering: true,
        theme: window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "default",
    });
    diagrams.forEach(async (diagram, index) => {
        const source = requiredElement(diagram.querySelector(".mermaid-source code"), "Mermaid source");
        const output = requiredElement(diagram.querySelector(".mermaid-output"), "Mermaid output");
        const error = requiredElement(diagram.querySelector(".mermaid-error"), "Mermaid error");
        const definition = source.textContent;
        try {
            if (definition.length > maxDiagramCharacters) {
                throw new Error(`diagram exceeds ${maxDiagramCharacters} characters`);
            }
            const rendered = await mermaid.render(`code-annotator-mermaid-${index}`, definition);
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
    });
})();
