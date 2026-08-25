import { describe, expect, it } from "vitest";

import {
  layoutDiffOverviewMarkers,
  layoutDiffOverviewViewport,
  selectDiffOverviewLocation,
  type DiffOverviewGeometry,
  type DiffOverviewMarkerLayout,
  type DiffOverviewRange,
} from "./diff-overview-geometry.js";

const geometry: DiffOverviewGeometry = {
  contentHeight: 100,
  trackHeight: 100,
  minimumMarkerHeight: 3,
  markerGap: 1,
  devicePixelRatio: 1,
};

describe("diff overview marker geometry", () => {
  it("maps hunk position and length across the complete track", () => {
    const ranges: ReadonlyArray<DiffOverviewRange> = [
      { id: "first", start: 0, end: 10 },
      { id: "middle", start: 50, end: 70 },
      { id: "last", start: 90, end: 100 },
    ];

    expect(layoutDiffOverviewMarkers(ranges, geometry)).toEqual([
      marker("first", 0, 10),
      marker("middle", 50, 20),
      marker("last", 90, 10),
    ]);
  });

  it("keeps a minimum-height marker inside the bottom edge", () => {
    expect(
      layoutDiffOverviewMarkers([{ id: "last", start: 99, end: 100 }], {
        ...geometry,
        trackHeight: 50,
      }),
    ).toEqual([marker("last", 47, 3)]);
  });

  it("packs colliding markers forward while preserving a gap", () => {
    const layouts = layoutDiffOverviewMarkers(
      [
        { id: "one", start: 0, end: 1 },
        { id: "two", start: 2, end: 3 },
        { id: "three", start: 4, end: 5 },
      ],
      {
        ...geometry,
        trackHeight: 20,
        minimumMarkerHeight: 5,
      },
    );

    expect(layouts).toEqual([
      marker("one", 0, 5),
      marker("two", 6, 5),
      marker("three", 12, 5),
    ]);
  });

  it("pulls a packed end group back inside the track", () => {
    const layouts = layoutDiffOverviewMarkers(
      [
        { id: "one", start: 90, end: 91 },
        { id: "two", start: 92, end: 93 },
        { id: "three", start: 94, end: 95 },
      ],
      {
        ...geometry,
        trackHeight: 20,
        minimumMarkerHeight: 5,
      },
    );

    expect(layouts).toEqual([
      marker("one", 3, 5),
      marker("two", 9, 5),
      marker("three", 15, 5),
    ]);
  });

  it("groups every hunk into device-pixel density slots when packing is impossible", () => {
    const layouts = layoutDiffOverviewMarkers(
      [
        { id: "one", start: 0, end: 1 },
        { id: "two", start: 1, end: 2 },
        { id: "three", start: 2, end: 3 },
      ],
      {
        contentHeight: 3,
        trackHeight: 2,
        minimumMarkerHeight: 1,
        markerGap: 1,
        devicePixelRatio: 1,
      },
    );

    expect(layouts).toEqual([
      marker("one", 0, 1),
      densityMarker("two", 1, "density-1", 2),
      densityMarker("three", 1, "density-1", 2),
    ]);
  });

  it("rejects duplicate, unordered, and out-of-bounds ranges", () => {
    expect(() =>
      layoutDiffOverviewMarkers(
        [
          { id: "same", start: 0, end: 10 },
          { id: "same", start: 20, end: 30 },
        ],
        geometry,
      ),
    ).toThrow("duplicate overview range id");
    expect(() =>
      layoutDiffOverviewMarkers(
        [
          { id: "later", start: 20, end: 30 },
          { id: "overlap", start: 25, end: 40 },
        ],
        geometry,
      ),
    ).toThrow("invalid overview range");
    expect(() =>
      layoutDiffOverviewMarkers(
        [{ id: "outside", start: 90, end: 101 }],
        geometry,
      ),
    ).toThrow("invalid overview range");
  });
});

describe("diff overview viewport geometry", () => {
  it("projects middle and clipped viewport ranges onto the track", () => {
    expect(
      layoutDiffOverviewViewport({
        contentHeight: 1000,
        trackHeight: 100,
        visibleStart: 250,
        visibleEnd: 500,
        minimumHeight: 4,
      }),
    ).toEqual({ top: 25, height: 25 });
    expect(
      layoutDiffOverviewViewport({
        contentHeight: 1000,
        trackHeight: 100,
        visibleStart: -100,
        visibleEnd: 2000,
        minimumHeight: 4,
      }),
    ).toEqual({ top: 0, height: 100 });
  });

  it("centers a minimum-height indicator at the bottom edge", () => {
    expect(
      layoutDiffOverviewViewport({
        contentHeight: 1000,
        trackHeight: 100,
        visibleStart: 990,
        visibleEnd: 1000,
        minimumHeight: 4,
      }),
    ).toEqual({ top: 96, height: 4 });
  });
});

describe("diff overview current and next location", () => {
  const ranges: ReadonlyArray<DiffOverviewRange> = [
    { id: "first", start: 10, end: 20 },
    { id: "second", start: 40, end: 50 },
  ];

  it("prefers the first hunk intersecting the viewport", () => {
    expect(selectDiffOverviewLocation(ranges, 15, 45)).toEqual({
      id: "first",
      state: "current",
    });
  });

  it("selects the next lower hunk when none is visible", () => {
    expect(selectDiffOverviewLocation(ranges, 25, 35)).toEqual({
      id: "second",
      state: "next",
    });
  });

  it("returns no location after the final hunk", () => {
    expect(selectDiffOverviewLocation(ranges, 55, 70)).toBeNull();
  });
});

function marker(
  id: string,
  top: number,
  height: number,
): DiffOverviewMarkerLayout {
  return { id, top, height, densityGroup: null, densityCount: 1 };
}

function densityMarker(
  id: string,
  top: number,
  densityGroup: string,
  densityCount: number,
): DiffOverviewMarkerLayout {
  return { id, top, height: 1, densityGroup, densityCount };
}
