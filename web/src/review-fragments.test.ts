// @vitest-environment happy-dom

import { describe, expect, it } from "vitest";

import { configureLifecycleForm } from "./review-fragments.js";
import type { TransitionBehavior } from "./viewer-state.js";

const transitions: TransitionBehavior[] = [
  { status: "applied", role: "agent", activity: "summary", activityLabel: "Summary" },
  { status: "closed", role: "reviewer", activity: "", activityLabel: "" },
];

describe("configureLifecycleForm", () => {
  it("applies typed transition behavior without reading HTML metadata", () => {
    document.body.innerHTML = `<form>
      <select name="status"><option value="applied" selected>Apply</option><option value="closed">Close</option></select>
      <select name="role"></select>
      <label class="lifecycle-activity" hidden><span>Message</span><textarea name="activity"></textarea></label>
      <label class="lifecycle-commit" hidden><input name="commit"></label>
    </form>`;
    const form = document.querySelector<HTMLFormElement>("form");
    if (!form) throw new Error("missing test form");

    configureLifecycleForm(form, transitions, true);

    expect((form.elements.namedItem("role") as HTMLSelectElement).value).toBe("agent");
    expect(form.querySelector<HTMLElement>(".lifecycle-activity")?.hidden).toBe(false);
    expect(form.querySelector<HTMLTextAreaElement>('textarea[name="activity"]')?.required).toBe(true);
    expect(form.querySelector<HTMLElement>(".lifecycle-activity span")?.textContent).toBe("Summary");
    expect(form.querySelector<HTMLElement>(".lifecycle-commit")?.hidden).toBe(false);
  });
});
