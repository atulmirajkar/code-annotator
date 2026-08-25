/**
 * Projects ordered, non-overlapping hunk ranges onto the ruler track.
 *
 * The normal path preserves a visible minimum marker size and separates close
 * markers by packing them. When that is mathematically impossible, the density
 * path reduces markers to device-pixel slots while retaining every hunk ID.
 */
export function layoutDiffOverviewMarkers(ranges, geometry) {
    validateGeometry(geometry);
    validateRanges(ranges, geometry.contentHeight);
    if (ranges.length === 0)
        return [];
    const markers = ranges.map((range) => idealMarkerLayout(range, geometry));
    // Decide whether every minimum-sized marker plus its gap can fit before
    // packing. This prevents the packing pass from manufacturing negative tops.
    const requiredHeight = markers.reduce((total, marker) => total + marker.height, Math.max(0, ranges.length - 1) * geometry.markerGap);
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
/** Projects the visible portion of the diff onto the overview track. */
export function layoutDiffOverviewViewport(geometry) {
    requirePositive(geometry.contentHeight, "contentHeight");
    requirePositive(geometry.trackHeight, "trackHeight");
    requirePositive(geometry.minimumHeight, "minimumHeight");
    requireFinite(geometry.visibleStart, "visibleStart");
    requireFinite(geometry.visibleEnd, "visibleEnd");
    if (geometry.visibleEnd < geometry.visibleStart) {
        throw new RangeError("visibleEnd must not precede visibleStart");
    }
    const visibleStart = clamp(geometry.visibleStart, 0, geometry.contentHeight);
    const visibleEnd = clamp(geometry.visibleEnd, visibleStart, geometry.contentHeight);
    const rawTop = (visibleStart / geometry.contentHeight) * geometry.trackHeight;
    const rawHeight = ((visibleEnd - visibleStart) / geometry.contentHeight) *
        geometry.trackHeight;
    // Grow a very short viewport indicator around its proportional center. Near
    // either track edge the final clamp moves the whole indicator back in bounds.
    const height = Math.min(geometry.trackHeight, Math.max(geometry.minimumHeight, rawHeight));
    const centeredTop = rawTop - (height - rawHeight) / 2;
    return {
        top: clamp(centeredTop, 0, geometry.trackHeight - height),
        height,
    };
}
/**
 * Finds the hunk the ruler should emphasize for the current viewport.
 * Intersections win over a later hunk, and document order breaks ties when a
 * tall viewport intersects more than one range.
 */
export function selectDiffOverviewLocation(ranges, visibleStart, visibleEnd) {
    requireFinite(visibleStart, "visibleStart");
    requireFinite(visibleEnd, "visibleEnd");
    if (visibleEnd < visibleStart) {
        throw new RangeError("visibleEnd must not precede visibleStart");
    }
    validateRanges(ranges, rangeContentHeight(ranges));
    const current = ranges.find((range) => range.start < visibleEnd && range.end > visibleStart);
    if (current)
        return { id: current.id, state: "current" };
    const next = ranges.find((range) => range.start >= visibleStart);
    return next ? { id: next.id, state: "next" } : null;
}
function idealMarkerLayout(range, geometry) {
    const top = (range.start / geometry.contentHeight) * geometry.trackHeight;
    const proportionalHeight = ((range.end - range.start) / geometry.contentHeight) * geometry.trackHeight;
    // Clamp after applying the minimum so a marker at the end of the document
    // grows upward rather than extending beyond the track.
    const height = Math.min(geometry.trackHeight, Math.max(geometry.minimumMarkerHeight, proportionalHeight));
    return {
        id: range.id,
        top: clamp(top, 0, geometry.trackHeight - height),
        height,
    };
}
// The forward pass resolves collisions in document order. If the final group
// crosses the track boundary, the backward pass pulls that group into view
// while retaining marker order and the requested gap.
function packMarkers(markers, trackHeight, markerGap) {
    for (let index = 1; index < markers.length; index += 1) {
        const previous = requireAt(markers, index - 1);
        const marker = requireAt(markers, index);
        marker.top = Math.max(marker.top, previous.top + previous.height + markerGap);
    }
    const last = requireAt(markers, markers.length - 1);
    if (last.top + last.height <= trackHeight)
        return;
    last.top = trackHeight - last.height;
    for (let index = markers.length - 2; index >= 0; index -= 1) {
        const marker = requireAt(markers, index);
        const next = requireAt(markers, index + 1);
        marker.top = Math.min(marker.top, next.top - markerGap - marker.height);
    }
}
function layoutDensityMarkers(ranges, geometry) {
    // One slot corresponds to one physical device pixel. Higher-density displays
    // therefore gain more independent slots without introducing sub-pixel gaps
    // that the browser cannot render distinctly.
    const slotCount = Math.max(1, Math.floor(geometry.trackHeight * geometry.devicePixelRatio));
    const slotHeight = geometry.trackHeight / slotCount;
    const slots = ranges.map((range) => {
        // The midpoint best represents a hunk after its height has collapsed to a
        // single slot; using only the start would bias long hunks toward the top.
        const center = (range.start + range.end) / 2;
        return Math.min(slotCount - 1, Math.floor((center / geometry.contentHeight) * slotCount));
    });
    const counts = new Map();
    for (const slot of slots)
        counts.set(slot, (counts.get(slot) ?? 0) + 1);
    // Emit one layout per input range even when layouts overlap. The controller
    // can make a shared slot clickable as a group while retaining the count and
    // each hunk's stable ID for navigation and accessibility.
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
function validateGeometry(geometry) {
    requirePositive(geometry.contentHeight, "contentHeight");
    requirePositive(geometry.trackHeight, "trackHeight");
    requirePositive(geometry.minimumMarkerHeight, "minimumMarkerHeight");
    requireNonNegative(geometry.markerGap, "markerGap");
    requirePositive(geometry.devicePixelRatio, "devicePixelRatio");
}
function validateRanges(ranges, contentHeight) {
    const identifiers = new Set();
    let previousEnd = 0;
    for (const range of ranges) {
        if (range.id === "")
            throw new Error("overview range id is required");
        if (identifiers.has(range.id)) {
            throw new Error(`duplicate overview range id: ${range.id}`);
        }
        identifiers.add(range.id);
        requireFinite(range.start, `${range.id}.start`);
        requireFinite(range.end, `${range.id}.end`);
        // Requiring document order and non-overlap keeps current/next selection and
        // both packing passes deterministic; callers must normalize DOM ranges.
        if (range.start < previousEnd ||
            range.start < 0 ||
            range.end <= range.start ||
            range.end > contentHeight) {
            throw new RangeError(`invalid overview range: ${range.id}`);
        }
        previousEnd = range.end;
    }
}
function rangeContentHeight(ranges) {
    // Selection only compares relative range positions, so the final end is a
    // sufficient validation boundary. Empty input still needs a positive bound.
    return ranges.length === 0 ? 1 : requireAt(ranges, ranges.length - 1).end;
}
function requireAt(values, index) {
    const value = values[index];
    if (value === undefined)
        throw new RangeError(`missing value at ${index}`);
    return value;
}
function requireFinite(value, name) {
    if (!Number.isFinite(value))
        throw new RangeError(`${name} must be finite`);
}
function requirePositive(value, name) {
    requireFinite(value, name);
    if (value <= 0)
        throw new RangeError(`${name} must be positive`);
}
function requireNonNegative(value, name) {
    requireFinite(value, name);
    if (value < 0)
        throw new RangeError(`${name} must not be negative`);
}
function clamp(value, minimum, maximum) {
    return Math.min(maximum, Math.max(minimum, value));
}
