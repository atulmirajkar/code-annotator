import { describe, expect, it } from "vitest";

import { transitionOptions } from "./review-thread.js";

describe("transitionOptions", () => {
  it("lets reviewers close an irrelevant or accidental open annotation", () => {
    expect(transitionOptions("open")).toContainEqual({
      status: "closed",
      label: "Close",
      role: "reviewer",
    });
  });

  it("keeps close unavailable to agents", () => {
    const close = transitionOptions("open").find((option) => option.status === "closed");

    expect(close?.role).toBe("reviewer");
  });
});
