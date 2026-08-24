export type AnnotationIntent =
  | "question"
  | "suggestion"
  | "change_request"
  | "approval";

export type AnnotationStatus =
  | "open"
  | "acknowledged"
  | "applied"
  | "needs_changes"
  | "closed"
  | "rejected";

export type Role = "agent" | "reviewer";

export type ThreadKind =
  | "reply"
  | "acknowledgement"
  | "resolution"
  | "review"
  | "status_change";

export interface Selector {
  exact: string;
  startByte: number;
  endByte: number;
  startLine: number;
  endLine: number;
}

export interface Source {
  sha256: string;
  selector: Selector;
}

export interface AnchorResult {
  state: "resolved" | "stale";
  startByte: number;
  endByte: number;
}

export interface ThreadEntry {
  id: string;
  kind: ThreadKind;
  message?: string;
  summary?: string;
  commit?: string;
  role: Role;
  fromStatus?: AnnotationStatus;
  toStatus?: AnnotationStatus;
  createdAt: string;
}

export interface Annotation {
  id: string;
  intent: AnnotationIntent;
  status: AnnotationStatus;
  comment: string;
  role: Role;
  source?: Source;
  anchor?: AnchorResult;
  thread: ThreadEntry[];
}

export interface AnnotationPayload {
  document: string;
  revision: string;
  annotations: Annotation[];
}

export interface SelectionPayload {
  startByte: number;
  endByte: number;
  documentSHA256: string;
}

export interface CreateAnnotationRequest {
  document: string;
  intent: AnnotationIntent;
  comment: string;
  role: Role;
  selection?: SelectionPayload;
}

export interface ReplyRequest {
  document: string;
  role: Role;
  message: string;
}

export interface TransitionRequest {
  document: string;
  status: AnnotationStatus;
  role: Role;
  activity?: string;
  commit?: string;
  message?: string;
  summary?: string;
}

export interface ReattachRequest {
  document: string;
  selection: SelectionPayload;
}

export interface ReplyRole {
  value: Role;
  label: string;
}

export interface TransitionOption {
  status: AnnotationStatus;
  label: string;
  role: Role;
  activity?: "message" | "summary";
}

export interface ThreadKindDisplay {
  label: string;
  className: string;
}

export interface AnnotationTurnBadge {
  label: string;
  className: string;
}
