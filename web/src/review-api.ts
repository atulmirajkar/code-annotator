import type {
  CreateAnnotationRequest,
  ReattachRequest,
  ReplyRequest,
  TransitionRequest,
} from "./types.js";

function mutationHeaders(reviewToken: string, currentRevision: string): Record<string, string> {
  return {
    "Content-Type": "application/json",
    "If-Match": JSON.stringify(currentRevision),
    "X-Code-Annotator-Token": reviewToken,
  };
}

export async function fetchAnnotations(documentPath: string): Promise<Response> {
  return fetch(`/api/annotations?document=${encodeURIComponent(documentPath)}`, {
    headers: { Accept: "application/json" },
  });
}

export async function createAnnotation(reviewToken: string, currentRevision: string, payload: CreateAnnotationRequest): Promise<Response> {
  return fetch("/api/annotations", {
    method: "POST",
    headers: mutationHeaders(reviewToken, currentRevision),
    body: JSON.stringify(payload),
  });
}

export async function reattachAnnotation(reviewToken: string, currentRevision: string, annotationID: string, payload: ReattachRequest): Promise<Response> {
  return fetch(`/api/annotations/${encodeURIComponent(annotationID)}/reattach`, {
    method: "POST",
    headers: mutationHeaders(reviewToken, currentRevision),
    body: JSON.stringify(payload),
  });
}

export async function replyToAnnotation(reviewToken: string, currentRevision: string, annotationID: string, payload: ReplyRequest): Promise<Response> {
  return fetch(`/api/annotations/${encodeURIComponent(annotationID)}/replies`, {
    method: "POST",
    headers: mutationHeaders(reviewToken, currentRevision),
    body: JSON.stringify(payload),
  });
}

export async function updateAnnotation(reviewToken: string, currentRevision: string, annotationID: string, payload: TransitionRequest): Promise<Response> {
  return fetch(`/api/annotations/${encodeURIComponent(annotationID)}`, {
    method: "PATCH",
    headers: mutationHeaders(reviewToken, currentRevision),
    body: JSON.stringify(payload),
  });
}
