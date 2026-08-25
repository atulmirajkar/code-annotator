import { hasChangedDocuments } from "./document-catalog.js";
import type { DocumentScope } from "./document-catalog.js";
import type { DocumentCatalogItem } from "./document-state.js";

export function resolveDocumentScope(
  stored: string | null,
  legacyChangedOnly: string | null,
  documents: ReadonlyArray<DocumentCatalogItem>,
): DocumentScope {
  if (stored === "all" || stored === "changed" || stored === "open-comments") return stored;
  if (legacyChangedOnly === "true") return "changed";
  return hasChangedDocuments(documents) ? "changed" : "all";
}

export function clampInteger(value: number, minimum: number, maximum: number): number {
  return Math.min(maximum, Math.max(minimum, Math.round(value)));
}
