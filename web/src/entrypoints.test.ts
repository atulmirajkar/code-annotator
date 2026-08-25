// @vitest-environment happy-dom

import { describe, expect, it, vi } from "vitest";

import { initializeMermaid } from "./mermaid.js";
import { initializeReview } from "./review.js";
import { initializeViewer } from "./viewer.js";

describe("explicit browser initializers", () => {
  it("lets the viewer be instantiated against an injected empty root", () => {
    document.body.innerHTML = "";
    expect(() =>
      initializeViewer({
        document,
        window,
        location: window.location,
        storage: window.sessionStorage,
        resizeObserver: window.ResizeObserver,
        htmx: null,
      }),
    ).not.toThrow();
  });

  it("configures an explicitly injected HTMX API", () => {
    document.body.innerHTML = `<main class="layout"></main>`;
    const config = {
      allowEval: true,
      allowNestedOobSwaps: true,
      allowScriptTags: true,
      historyCacheSize: 10,
      selfRequestsOnly: false,
    };

    initializeViewer({
      document,
      window,
      location: window.location,
      storage: window.sessionStorage,
      resizeObserver: window.ResizeObserver,
      htmx: { config, ajax: vi.fn() },
    });

    expect(config).toEqual({
      allowEval: false,
      allowNestedOobSwaps: false,
      allowScriptTags: false,
      historyCacheSize: 0,
      selfRequestsOnly: true,
    });
  });

  it("lets review initialization use an injected empty root", async () => {
    document.body.innerHTML = "";
    await expect(
      initializeReview({ document, window, htmx: null }),
    ).resolves.toBeUndefined();
  });

  it("renders Mermaid through injected ports", async () => {
    document.body.innerHTML = `<div id="diagram-test" class="mermaid-diagram">
      <details class="mermaid-source"><code>graph TD; A--&gt;B</code></details>
      <div class="mermaid-output"></div><p class="mermaid-error" hidden></p>
    </div>`;
    const initialize = vi.fn();
    const render = vi.fn().mockResolvedValue({ svg: "<svg></svg>" });

    await initializeMermaid({
      document,
      api: { initialize, render },
      prefersDark: true,
      definitions: new Map([["diagram-test", "graph TD; A-->B"]]),
    });

    expect(initialize).toHaveBeenCalledWith(
      expect.objectContaining({ securityLevel: "strict", theme: "dark" }),
    );
    expect(render).toHaveBeenCalledWith(
      "code-annotator-mermaid-0",
      "graph TD; A-->B",
    );
    expect(document.querySelector(".mermaid-output")?.innerHTML).toBe(
      "<svg></svg>",
    );
  });
});
