export function mergeIntervals(values) {
    const sorted = values
        .map(([start, end]) => [start, end])
        .sort((left, right) => left[0] - right[0]);
    return sorted.reduce((merged, current) => {
        const previous = merged[merged.length - 1];
        if (previous && current[0] <= previous[1])
            previous[1] = Math.max(previous[1], current[1]);
        else
            merged.push(current);
        return merged;
    }, []);
}
export function createAnnotationHighlighter({ markdown, sourceSpan, sourceSpanRange, utf8Length, sourceNodes, diagrams }) {
    // Highlight only anchors resolved against the current document. Stale and
    // document-level annotations remain visible in the panel without a range.
    function renderAnnotationHighlights(annotations) {
        clearFallbackHighlights();
        renderDiagramHighlights(annotations);
        const ranges = annotations
            .filter(hasResolvedAnchor)
            .map((annotation) => sourceRange(annotation.anchor.startByte, annotation.anchor.endByte))
            .filter((range) => range !== null);
        if (globalThis.CSS && CSS.highlights && typeof Highlight !== "undefined") {
            CSS.highlights.delete("code-annotator-annotations");
            if (ranges.length > 0)
                CSS.highlights.set("code-annotator-annotations", new Highlight(...ranges));
            return;
        }
        renderFallbackHighlights(ranges);
    }
    // Diagram annotations highlight the rendered region as a whole; their
    // hidden source ranges remain available for quote previews and fallback APIs.
    function renderDiagramHighlights(annotations) {
        const activeRanges = annotations
            .filter(hasResolvedAnchor)
            .map((annotation) => [annotation.anchor.startByte, annotation.anchor.endByte]);
        diagrams.forEach((position) => {
            const diagram = document.getElementById(position.elementId);
            if (!diagram || !markdown.contains(diagram))
                return;
            diagram.classList.toggle("annotation-highlight-region", activeRanges.some((range) => range[0] === position.startByte && range[1] === position.endByte));
        });
    }
    function sourceRange(startByte, endByte) {
        const spans = Array.from(markdown.querySelectorAll(".source-text"));
        const startSpan = spans.find((span) => containsSourceOffset(span, startByte, false));
        const endSpan = spans.slice().reverse().find((span) => containsSourceOffset(span, endByte, true));
        if (!startSpan || !endSpan)
            return null;
        const startNode = sourceTextNode(startSpan);
        const endNode = sourceTextNode(endSpan);
        const startOffset = byteOffsetToTextOffset(startSpan, startByte);
        const endOffset = byteOffsetToTextOffset(endSpan, endByte);
        if (!startNode || !endNode || startOffset < 0 || endOffset < 0)
            return null;
        const range = document.createRange();
        try {
            range.setStart(startNode, startOffset);
            range.setEnd(endNode, endOffset);
        }
        catch (_) {
            return null;
        }
        return range.collapsed ? null : range;
    }
    function containsSourceOffset(span, offset, endBoundary) {
        const position = sourceNodes.get(span.id);
        return Boolean(position) && (endBoundary
            ? position.startByte < offset && offset <= position.endByte
            : position.startByte <= offset && offset < position.endByte);
    }
    function byteOffsetToTextOffset(span, sourceOffset) {
        const position = sourceNodes.get(span.id);
        if (!position)
            return -1;
        const target = sourceOffset - position.startByte;
        let bytes = 0;
        let textOffset = 0;
        for (const character of span.textContent || "") {
            if (bytes === target)
                return textOffset;
            bytes += utf8Length(character);
            textOffset += character.length;
            if (bytes > target)
                return -1;
        }
        return bytes === target ? textOffset : -1;
    }
    function sourceTextNode(span) {
        span.normalize();
        return span.firstChild instanceof Text ? span.firstChild : null;
    }
    // The fallback merges overlapping intervals within each source span before
    // wrapping them, avoiding invalid nested or crossing mark elements.
    function renderFallbackHighlights(ranges) {
        const intervals = new Map();
        ranges.forEach((range) => {
            const startSpan = sourceSpan(range.startContainer);
            const endSpan = sourceSpan(range.endContainer);
            if (!startSpan || !endSpan)
                return;
            const spans = sourceSpanRange(startSpan, endSpan) || [];
            spans.forEach((span) => {
                const length = (span.textContent || "").length;
                const start = span === startSpan ? range.startOffset : 0;
                const end = span === endSpan ? range.endOffset : length;
                if (end > start)
                    intervals.set(span, [...(intervals.get(span) || []), [start, end]]);
            });
        });
        intervals.forEach((values, span) => {
            const merged = mergeIntervals(values);
            const textNode = sourceTextNode(span);
            if (!textNode)
                return;
            merged.reverse().forEach(([start, end]) => {
                const range = document.createRange();
                range.setStart(textNode, start);
                range.setEnd(textNode, end);
                const mark = document.createElement("mark");
                mark.className = "annotation-highlight-fallback";
                range.surroundContents(mark);
            });
        });
    }
    function clearFallbackHighlights() {
        markdown.querySelectorAll("mark.annotation-highlight-fallback").forEach((mark) => {
            mark.replaceWith(document.createTextNode(mark.textContent || ""));
        });
        markdown.querySelectorAll(".source-text").forEach((span) => span.normalize());
    }
    return {
        renderAnnotationHighlights,
        sourceRange,
    };
}
function hasResolvedAnchor(annotation) {
    return annotation.anchor !== null && annotation.anchor.state !== "stale";
}
