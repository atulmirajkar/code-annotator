export interface DiffOverviewRange {
  readonly id: string;
  readonly start: number;
  readonly end: number;
}

export interface DiffOverviewGeometry {
  readonly contentHeight: number;
  readonly trackHeight: number;
  readonly minimumMarkerHeight: number;
  readonly markerGap: number;
  readonly devicePixelRatio: number;
}

export interface DiffOverviewMarkerLayout {
  readonly id: string;
  readonly top: number;
  readonly height: number;
  readonly densityGroup: string | null;
  readonly densityCount: number;
}

export interface DiffOverviewViewportGeometry {
  readonly contentHeight: number;
  readonly trackHeight: number;
  readonly visibleStart: number;
  readonly visibleEnd: number;
  readonly minimumHeight: number;
}

export interface DiffOverviewViewportLayout {
  readonly top: number;
  readonly height: number;
}

export interface DiffOverviewLocation {
  readonly id: string;
  readonly state: "current" | "next";
}

interface MutableMarkerLayout {
  readonly id: string;
  top: number;
  readonly height: number;
}

export function layoutDiffOverviewMarkers(
  ranges: ReadonlyArray<DiffOverviewRange>,
  geometry: DiffOverviewGeometry,
): ReadonlyArray<DiffOverviewMarkerLayout> {
  validateGeometry(geometry);
  validateRanges(ranges, geometry.contentHeight);
  if (ranges.length === 0) return [];

  const markers = ranges.map((range) => idealMarkerLayout(range, geometry));
  const requiredHeight = markers.reduce(
    (total, marker) => total + marker.height,
    Math.max(0, ranges.length - 1) * geometry.markerGap,
  );
  if (requiredHeight > geometry.trackHeight) {
    return layoutDensityMarkers(ranges, geometry);
  }

  packMarkers(markers, geometry.trackHeight, geometry.markerGap);
  return markers.map((marker) => ({
    ...marker,
    densityGroup: null,
    densityCount: 1,
  }));
}

export function layoutDiffOverviewViewport(
  geometry: DiffOverviewViewportGeometry,
): DiffOverviewViewportLayout {
  requirePositive(geometry.contentHeight, "contentHeight");
  requirePositive(geometry.trackHeight, "trackHeight");
  requirePositive(geometry.minimumHeight, "minimumHeight");
  requireFinite(geometry.visibleStart, "visibleStart");
  requireFinite(geometry.visibleEnd, "visibleEnd");
  if (geometry.visibleEnd < geometry.visibleStart) {
    throw new RangeError("visibleEnd must not precede visibleStart");
  }

  const visibleStart = clamp(geometry.visibleStart, 0, geometry.contentHeight);
  const visibleEnd = clamp(
    geometry.visibleEnd,
    visibleStart,
    geometry.contentHeight,
  );
  const rawTop = (visibleStart / geometry.contentHeight) * geometry.trackHeight;
  const rawHeight =
    ((visibleEnd - visibleStart) / geometry.contentHeight) *
    geometry.trackHeight;
  const height = Math.min(
    geometry.trackHeight,
    Math.max(geometry.minimumHeight, rawHeight),
  );
  const centeredTop = rawTop - (height - rawHeight) / 2;
  return {
    top: clamp(centeredTop, 0, geometry.trackHeight - height),
    height,
  };
}

export function selectDiffOverviewLocation(
  ranges: ReadonlyArray<DiffOverviewRange>,
  visibleStart: number,
  visibleEnd: number,
): DiffOverviewLocation | null {
  requireFinite(visibleStart, "visibleStart");
  requireFinite(visibleEnd, "visibleEnd");
  if (visibleEnd < visibleStart) {
    throw new RangeError("visibleEnd must not precede visibleStart");
  }
  validateRanges(ranges, rangeContentHeight(ranges));

  const current = ranges.find(
    (range) => range.start < visibleEnd && range.end > visibleStart,
  );
  if (current) return { id: current.id, state: "current" };

  const next = ranges.find((range) => range.start >= visibleStart);
  return next ? { id: next.id, state: "next" } : null;
}

function idealMarkerLayout(
  range: DiffOverviewRange,
  geometry: DiffOverviewGeometry,
): MutableMarkerLayout {
  const top = (range.start / geometry.contentHeight) * geometry.trackHeight;
  const proportionalHeight =
    ((range.end - range.start) / geometry.contentHeight) * geometry.trackHeight;
  const height = Math.min(
    geometry.trackHeight,
    Math.max(geometry.minimumMarkerHeight, proportionalHeight),
  );
  return {
    id: range.id,
    top: clamp(top, 0, geometry.trackHeight - height),
    height,
  };
}

// The forward pass resolves collisions in document order. If the final group
// crosses the track boundary, the backward pass pulls that group into view
// while retaining marker order and the requested gap.
function packMarkers(
  markers: Array<MutableMarkerLayout>,
  trackHeight: number,
  markerGap: number,
): void {
  for (let index = 1; index < markers.length; index += 1) {
    const previous = requireAt(markers, index - 1);
    const marker = requireAt(markers, index);
    marker.top = Math.max(
      marker.top,
      previous.top + previous.height + markerGap,
    );
  }

  const last = requireAt(markers, markers.length - 1);
  if (last.top + last.height <= trackHeight) return;
  last.top = trackHeight - last.height;
  for (let index = markers.length - 2; index >= 0; index -= 1) {
    const marker = requireAt(markers, index);
    const next = requireAt(markers, index + 1);
    marker.top = Math.min(marker.top, next.top - markerGap - marker.height);
  }
}

function layoutDensityMarkers(
  ranges: ReadonlyArray<DiffOverviewRange>,
  geometry: DiffOverviewGeometry,
): ReadonlyArray<DiffOverviewMarkerLayout> {
  const slotCount = Math.max(
    1,
    Math.floor(geometry.trackHeight * geometry.devicePixelRatio),
  );
  const slotHeight = geometry.trackHeight / slotCount;
  const slots = ranges.map((range) => {
    const center = (range.start + range.end) / 2;
    return Math.min(
      slotCount - 1,
      Math.floor((center / geometry.contentHeight) * slotCount),
    );
  });
  const counts = new Map<number, number>();
  for (const slot of slots) counts.set(slot, (counts.get(slot) ?? 0) + 1);

  return ranges.map((range, index) => {
    const slot = requireAt(slots, index);
    const densityCount = counts.get(slot) ?? 1;
    return {
      id: range.id,
      top: slot * slotHeight,
      height: slotHeight,
      densityGroup: densityCount > 1 ? `density-${slot}` : null,
      densityCount,
    };
  });
}

function validateGeometry(geometry: DiffOverviewGeometry): void {
  requirePositive(geometry.contentHeight, "contentHeight");
  requirePositive(geometry.trackHeight, "trackHeight");
  requirePositive(geometry.minimumMarkerHeight, "minimumMarkerHeight");
  requireNonNegative(geometry.markerGap, "markerGap");
  requirePositive(geometry.devicePixelRatio, "devicePixelRatio");
}

function validateRanges(
  ranges: ReadonlyArray<DiffOverviewRange>,
  contentHeight: number,
): void {
  const identifiers = new Set<string>();
  let previousEnd = 0;
  for (const range of ranges) {
    if (range.id === "") throw new Error("overview range id is required");
    if (identifiers.has(range.id)) {
      throw new Error(`duplicate overview range id: ${range.id}`);
    }
    identifiers.add(range.id);
    requireFinite(range.start, `${range.id}.start`);
    requireFinite(range.end, `${range.id}.end`);
    if (
      range.start < previousEnd ||
      range.start < 0 ||
      range.end <= range.start ||
      range.end > contentHeight
    ) {
      throw new RangeError(`invalid overview range: ${range.id}`);
    }
    previousEnd = range.end;
  }
}

function rangeContentHeight(ranges: ReadonlyArray<DiffOverviewRange>): number {
  return ranges.length === 0 ? 1 : requireAt(ranges, ranges.length - 1).end;
}

function requireAt<T>(values: ReadonlyArray<T>, index: number): T {
  const value = values[index];
  if (value === undefined) throw new RangeError(`missing value at ${index}`);
  return value;
}

function requireFinite(value: number, name: string): void {
  if (!Number.isFinite(value)) throw new RangeError(`${name} must be finite`);
}

function requirePositive(value: number, name: string): void {
  requireFinite(value, name);
  if (value <= 0) throw new RangeError(`${name} must be positive`);
}

function requireNonNegative(value: number, name: string): void {
  requireFinite(value, name);
  if (value < 0) throw new RangeError(`${name} must not be negative`);
}

function clamp(value: number, minimum: number, maximum: number): number {
  return Math.min(maximum, Math.max(minimum, value));
}
