// @vitest-environment happy-dom

import { beforeEach, describe, expect, it } from "vitest";

import { annotationLocation, configureLifecycleForm } from "./review-fragments.js";

describe("annotationLocation", () => {
  it("parses validated navigation data from a server-rendered card", () => {
    const card = document.createElement("details");
    card.dataset.anchorState = "moved";
    card.dataset.anchorStartByte = "12";
    card.dataset.anchorEndByte = "19";
    card.dataset.sourceStartByte = "4";
    card.dataset.documentLevel = "false";
    card.dataset.needsReattachment = "false";

    expect(annotationLocation(card)).toEqual({
      anchorState: "moved",
      anchorStartByte: 12,
      anchorEndByte: 19,
      sourceStartByte: 4,
      documentLevel: false,
      needsReattachment: false,
    });
  });

  it("rejects malformed optional offsets", () => {
    const card = document.createElement("details");
    card.dataset.anchorState = "unknown";
    card.dataset.anchorStartByte = "nope";
    card.dataset.documentLevel = "true";
    card.dataset.needsReattachment = "true";

    expect(annotationLocation(card)).toEqual({
      anchorState: null,
      anchorStartByte: null,
      anchorEndByte: null,
      sourceStartByte: null,
      documentLevel: true,
      needsReattachment: true,
    });
  });
});

describe("configureLifecycleForm", () => {
  beforeEach(() => {
    document.body.innerHTML = `<form>
      <select name="status"><option value="applied" data-role="agent" data-activity="summary" data-activity-label="Summary" selected>Apply</option></select>
      <select name="role"><option value="reviewer">Reviewer</option><option value="agent">Agent</option></select>
      <label class="lifecycle-activity" hidden><span>Message</span><textarea name="activity"></textarea></label>
      <label class="lifecycle-commit" hidden><input name="commit"></label>
    </form>`;
  });

  it("derives role and fields from the server-authorized transition", () => {
    const form = document.querySelector<HTMLFormElement>("form")!;

    configureLifecycleForm(form, true);

    expect((form.elements.namedItem("role") as HTMLSelectElement).value).toBe("agent");
    expect(form.querySelector<HTMLElement>(".lifecycle-activity")!.hidden).toBe(false);
    expect(form.querySelector<HTMLTextAreaElement>("textarea")!.required).toBe(true);
    expect(form.querySelector<HTMLElement>(".lifecycle-commit")!.hidden).toBe(false);
  });
});
