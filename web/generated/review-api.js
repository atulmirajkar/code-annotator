function mutationHeaders(reviewToken, currentRevision) {
    return {
        "Content-Type": "application/json",
        "If-Match": JSON.stringify(currentRevision),
        "X-Code-Annotator-Token": reviewToken,
    };
}
export async function fetchAnnotations(documentPath) {
    return fetch(`/api/annotations?document=${encodeURIComponent(documentPath)}`, {
        headers: { Accept: "application/json" },
    });
}
export async function createAnnotation(reviewToken, currentRevision, payload) {
    return fetch("/api/annotations", {
        method: "POST",
        headers: mutationHeaders(reviewToken, currentRevision),
        body: JSON.stringify(payload),
    });
}
export async function reattachAnnotation(reviewToken, currentRevision, annotationID, payload) {
    return fetch(`/api/annotations/${encodeURIComponent(annotationID)}/reattach`, {
        method: "POST",
        headers: mutationHeaders(reviewToken, currentRevision),
        body: JSON.stringify(payload),
    });
}
export async function replyToAnnotation(reviewToken, currentRevision, annotationID, payload) {
    return fetch(`/api/annotations/${encodeURIComponent(annotationID)}/replies`, {
        method: "POST",
        headers: mutationHeaders(reviewToken, currentRevision),
        body: JSON.stringify(payload),
    });
}
export async function updateAnnotation(reviewToken, currentRevision, annotationID, payload) {
    return fetch(`/api/annotations/${encodeURIComponent(annotationID)}`, {
        method: "PATCH",
        headers: mutationHeaders(reviewToken, currentRevision),
        body: JSON.stringify(payload),
    });
}
