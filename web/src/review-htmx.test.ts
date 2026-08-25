// @vitest-environment happy-dom

import { describe, expect, it, vi } from "vitest";

import { configureReviewHTMX } from "./review-htmx.js";

describe("review HTMX adapter", () => {
  it("configures an injected API and adds mutation authority headers", () => {
    document.body.innerHTML = `<section id="annotation-panel-content">
      <form id="annotation-form" action="/api/annotations"></form>
    </section>`;
    const panel = document.querySelector<HTMLElement>(
      "#annotation-panel-content",
    );
    const form = document.querySelector<HTMLFormElement>("#annotation-form");
    if (!panel || !form) throw new Error("expected review fixture elements");
    const config = {
      allowEval: true,
      allowNestedOobSwaps: true,
      allowScriptTags: true,
      historyCacheSize: 10,
      selfRequestsOnly: false,
    };
    const headers: Record<string, string> = {};

    configureReviewHTMX({
      document,
      api: { config },
      panel,
      token: "review-token",
      getRevision: () => "revision-1",
      onPanelChanged: vi.fn(),
      onRequestError: vi.fn(),
    });
    form.dispatchEvent(
      new CustomEvent("htmx:configRequest", {
        bubbles: true,
        detail: { elt: form, headers, verb: "post" },
      }),
    );

    expect(config).toEqual({
      allowEval: false,
      allowNestedOobSwaps: false,
      allowScriptTags: false,
      historyCacheSize: 0,
      selfRequestsOnly: true,
    });
    expect(headers).toEqual({
      "X-Code-Annotator-Token": "review-token",
      "If-Match": JSON.stringify("revision-1"),
    });
  });
});
