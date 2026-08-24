function parseSourcePositions(value, label) {
    if (!Array.isArray(value))
        throw new Error(`${label} must be an array`);
    const result = new Map();
    value.forEach((positionValue, index) => {
        const position = requireRecord(positionValue, `${label}[${index}]`);
        const elementId = requireString(position.elementId, `${label}[${index}].elementId`);
        const startByte = requireInteger(position.startByte, `${label}[${index}].startByte`);
        const endByte = requireInteger(position.endByte, `${label}[${index}].endByte`);
        if (!elementId || result.has(elementId))
            throw new Error(`${label} element IDs must be non-empty and unique`);
        if (endByte < startByte)
            throw new Error(`${label}[${index}] range is reversed`);
        result.set(elementId, { elementId, startByte, endByte });
    });
    return result;
}
function requireRecord(value, label) {
    if (typeof value !== "object" || value === null || Array.isArray(value))
        throw new Error(`${label} must be an object`);
    return value;
}
function requireString(value, label) {
    if (typeof value !== "string")
        throw new Error(`${label} must be a string`);
    return value;
}
function requireBoolean(value, label) {
    if (typeof value !== "boolean")
        throw new Error(`${label} must be a boolean`);
    return value;
}
function requireInteger(value, label) {
    if (typeof value !== "number" || !Number.isInteger(value) || value < 0)
        throw new Error(`${label} must be a non-negative integer`);
    return value;
}
function requireMember(value, allowed, label) {
    const matched = allowed.find((candidate) => candidate === value);
    if (matched === undefined)
        throw new Error(`${label} is invalid`);
    return matched;
}
function nullableInteger(value, label) {
    return value === null ? null : requireInteger(value, label);
}
function parseTransition(value, index) {
    const item = requireRecord(value, `review.annotations.transitions[${index}]`);
    return {
        status: requireMember(item.status, ["open", "acknowledged", "applied", "needs_changes", "closed", "rejected"], "transition.status"),
        role: requireMember(item.role, ["agent", "reviewer"], "transition.role"),
        activity: requireMember(item.activity, ["", "message", "summary"], "transition.activity"),
        activityLabel: requireString(item.activityLabel, "transition.activityLabel"),
    };
}
function parseAnnotation(value, index) {
    const item = requireRecord(value, `review.annotations[${index}]`);
    const anchorValue = item.anchor;
    let anchor = null;
    if (anchorValue !== null) {
        const parsed = requireRecord(anchorValue, `review.annotations[${index}].anchor`);
        anchor = {
            state: requireMember(parsed.state, ["exact", "moved", "stale"], "annotation.anchor.state"),
            startByte: requireInteger(parsed.startByte, "annotation.anchor.startByte"),
            endByte: requireInteger(parsed.endByte, "annotation.anchor.endByte"),
        };
        if (anchor.endByte < anchor.startByte)
            throw new Error("annotation anchor range is reversed");
    }
    if (!Array.isArray(item.transitions))
        throw new Error("annotation.transitions must be an array");
    return {
        id: requireString(item.id, "annotation.id"),
        elementId: requireString(item.elementId, "annotation.elementId"),
        lifecycleFormId: requireString(item.lifecycleFormId, "annotation.lifecycleFormId"),
        documentLevel: requireBoolean(item.documentLevel, "annotation.documentLevel"),
        needsReattachment: requireBoolean(item.needsReattachment, "annotation.needsReattachment"),
        sourceStartByte: nullableInteger(item.sourceStartByte, "annotation.sourceStartByte"),
        anchor,
        transitions: item.transitions.map(parseTransition),
    };
}
export function parseViewerState(value) {
    const root = requireRecord(value, "viewer state");
    if (root.schemaVersion !== 1)
        throw new Error("viewer state schemaVersion is unsupported");
    const documentValue = requireRecord(root.document, "viewer state document");
    const path = requireString(documentValue.path, "document.path");
    const sha256 = requireString(documentValue.sha256, "document.sha256");
    if (!path)
        throw new Error("document.path must not be empty");
    if (!/^[a-f0-9]{64}$/u.test(sha256))
        throw new Error("document.sha256 is invalid");
    let review = null;
    if (root.review !== null) {
        const reviewValue = requireRecord(root.review, "viewer state review");
        if (!Array.isArray(reviewValue.annotations))
            throw new Error("review.annotations must be an array");
        const annotations = new Map();
        const annotationsByElementId = new Map();
        const annotationsByLifecycleFormId = new Map();
        reviewValue.annotations.forEach((annotationValue, index) => {
            const annotation = parseAnnotation(annotationValue, index);
            if (!annotation.id || annotations.has(annotation.id))
                throw new Error("annotation IDs must be non-empty and unique");
            if (!annotation.elementId || annotationsByElementId.has(annotation.elementId))
                throw new Error("annotation element IDs must be non-empty and unique");
            if (!annotation.lifecycleFormId || annotationsByLifecycleFormId.has(annotation.lifecycleFormId))
                throw new Error("annotation lifecycle form IDs must be non-empty and unique");
            annotations.set(annotation.id, annotation);
            annotationsByElementId.set(annotation.elementId, annotation);
            annotationsByLifecycleFormId.set(annotation.lifecycleFormId, annotation);
        });
        review = {
            revision: requireString(reviewValue.revision, "review.revision"),
            annotations,
            annotationsByElementId,
            annotationsByLifecycleFormId,
        };
    }
    return {
        schemaVersion: 1,
        document: {
            path,
            kind: requireMember(documentValue.kind, ["markdown", "code"], "document.kind"),
            sha256,
            sourceNodes: parseSourcePositions(documentValue.sourceNodes, "document.sourceNodes"),
            diagrams: parseSourcePositions(documentValue.diagrams, "document.diagrams"),
        },
        review,
    };
}
export async function fetchViewerState(documentPath, mode = "file") {
    const query = new URLSearchParams({ document: documentPath });
    if (mode === "diff")
        query.set("mode", mode);
    const response = await fetch(`/ui/viewer-state?${query.toString()}`, { headers: { Accept: "application/json" } });
    if (!response.ok)
        throw new Error(`viewer state request failed: ${response.status}`);
    const payload = await response.json();
    return parseViewerState(payload);
}
