import { describe, expect, it } from "vitest";

import { annotationTurnBadge, replyRoles, transitionOptions } from "./review-thread.js";
import type { Annotation } from "./types.js";

describe("roles", () => {
  it("offers only the two permission roles for replies", () => {
    expect(replyRoles()).toEqual([
      { value: "reviewer", label: "Reviewer" },
      { value: "agent", label: "Agent" },
    ]);
  });

  it("derives the pending turn directly from the latest thread role", () => {
    const annotation = {
      id: "ann_test",
      intent: "question",
      status: "open",
      comment: "Clarify this.",
      role: "reviewer",
      thread: [{
        id: "msg_test",
        kind: "reply",
        role: "agent",
        message: "I will investigate.",
        createdAt: "2026-08-24T12:00:00Z",
      }],
    } satisfies Annotation;

    expect(annotationTurnBadge(annotation)).toEqual({
      label: "waiting for reviewer",
      className: "pending-review",
    });
  });
});

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
