import { describe, expect, it } from "vitest";

import { parseViewerState } from "./viewer-state.js";

function validState(): unknown {
  return {
    schemaVersion: 1,
    document: { path: "README.md", kind: "markdown", sha256: "a".repeat(64) },
    review: {
      revision: "revision-1",
      annotations: [{
        id: "ann_1",
        documentLevel: false,
        needsReattachment: false,
        sourceStartByte: 8,
        anchor: { state: "exact", startByte: 8, endByte: 16 },
        transitions: [{ status: "acknowledged", role: "agent", activity: "", activityLabel: "" }],
      }],
    },
  };
}

describe("parseViewerState", () => {
  it("validates wire data and indexes annotations by semantic ID", () => {
    const state = parseViewerState(validState());

    expect(state.document.path).toBe("README.md");
    expect(state.review?.annotations.get("ann_1")?.anchor).toEqual({ state: "exact", startByte: 8, endByte: 16 });
    expect(state.review?.annotations.get("ann_1")?.transitions[0]?.role).toBe("agent");
  });

  it.each([
    ["unknown schema", { ...validState() as object, schemaVersion: 2 }],
    ["invalid digest", { schemaVersion: 1, document: { path: "README.md", kind: "markdown", sha256: "bad" }, review: null }],
    ["untyped annotations", { schemaVersion: 1, document: { path: "README.md", kind: "markdown", sha256: "a".repeat(64) }, review: { revision: "", annotations: {} } }],
  ])("rejects %s", (_name, value) => {
    expect(() => parseViewerState(value)).toThrow();
  });

  it("rejects duplicate annotation identities", () => {
    const value = validState() as { review: { annotations: unknown[] } };
    value.review.annotations.push(value.review.annotations[0]);

    expect(() => parseViewerState(value)).toThrow(/unique/u);
  });
});
