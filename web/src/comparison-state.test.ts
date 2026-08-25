import { describe, expect, it } from "vitest";

import { parseComparisonState } from "./comparison-state.js";

const commit = "0123456789abcdef0123456789abcdef01234567";

describe("parseComparisonState", () => {
  it("validates comparison JSON before returning typed state", () => {
    expect(parseComparisonState({
      activeCommit: commit,
      activeShort: commit.slice(0, 12),
      requestedBase: "HEAD",
      options: [{ commit, commitShort: commit.slice(0, 12), subject: "baseline" }],
    }).options[0]?.subject).toBe("baseline");
  });

  it("rejects malformed and duplicate commit identities", () => {
    const option = { commit, commitShort: commit.slice(0, 12) };
    expect(() => parseComparisonState({ activeCommit: "HEAD", activeShort: "HEAD", requestedBase: "HEAD", options: [] })).toThrow(/full lowercase commit/);
    expect(() => parseComparisonState({ activeCommit: commit, activeShort: commit.slice(0, 12), requestedBase: "HEAD", options: [option, option] })).toThrow(/unique/);
  });
});
