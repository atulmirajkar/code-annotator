import { hasChangedDocuments } from "./document-catalog.js";
export function resolveDocumentScope(stored, legacyChangedOnly, documents) {
    if (stored === "all" || stored === "changed" || stored === "open-comments")
        return stored;
    if (legacyChangedOnly === "true")
        return "changed";
    return hasChangedDocuments(documents) ? "changed" : "all";
}
export function clampInteger(value, minimum, maximum) {
    return Math.min(maximum, Math.max(minimum, Math.round(value)));
}
