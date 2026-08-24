import { describe, expect, it } from "vitest";

import { mergeIntervals } from "./review-highlights.js";

describe("mergeIntervals", () => {
  it("returns no ranges for no input", () => {
    expect(mergeIntervals([])).toEqual([]);
  });

  it("sorts and merges overlapping, adjacent, and contained ranges", () => {
    expect(mergeIntervals([
      [12, 15],
      [2, 5],
      [4, 8],
      [10, 12],
      [3, 4],
    ])).toEqual([
      [2, 8],
      [10, 15],
    ]);
  });

  it("keeps disjoint ranges separate", () => {
    expect(mergeIntervals([[1, 3], [5, 7], [9, 11]])).toEqual([
      [1, 3],
      [5, 7],
      [9, 11],
    ]);
  });

  it("does not mutate caller-owned ranges", () => {
    const ranges = [[8, 10], [2, 4]] as const;

    mergeIntervals(ranges);

    expect(ranges).toEqual([[8, 10], [2, 4]]);
  });
});
