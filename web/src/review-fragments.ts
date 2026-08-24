export type AnchorState = "exact" | "moved" | "stale";

export interface AnnotationLocation {
  anchorState: AnchorState | null;
  anchorStartByte: number | null;
  anchorEndByte: number | null;
  sourceStartByte: number | null;
  documentLevel: boolean;
  needsReattachment: boolean;
}

function integer(value: string | undefined): number | null {
  if (!value) return null;
  const parsed = Number.parseInt(value, 10);
  return Number.isInteger(parsed) ? parsed : null;
}

function anchorState(value: string | undefined): AnchorState | null {
  return value === "exact" || value === "moved" || value === "stale" ? value : null;
}

export function annotationLocation(card: HTMLElement): AnnotationLocation {
  return {
    anchorState: anchorState(card.dataset.anchorState),
    anchorStartByte: integer(card.dataset.anchorStartByte),
    anchorEndByte: integer(card.dataset.anchorEndByte),
    sourceStartByte: integer(card.dataset.sourceStartByte),
    documentLevel: card.dataset.documentLevel === "true",
    needsReattachment: card.dataset.needsReattachment === "true",
  };
}

export function annotationLocations(root: ParentNode): AnnotationLocation[] {
  return Array.from(root.querySelectorAll<HTMLElement>(".annotation-card")).map(annotationLocation);
}

export function configureLifecycleForm(form: HTMLFormElement, preserveValues: boolean): void {
  const status = form.elements.namedItem("status");
  const role = form.elements.namedItem("role");
  if (!(status instanceof HTMLSelectElement) || !(role instanceof HTMLSelectElement)) return;
  const selected = status.selectedOptions[0];
  if (!selected) return;

  const selectedRole = selected.dataset.role || "";
  const roleOption = document.createElement("option");
  roleOption.value = selectedRole;
  roleOption.textContent = selectedRole === "agent" ? "Agent" : "Reviewer";
  role.replaceChildren(roleOption);
  const activity = form.querySelector<HTMLElement>(".lifecycle-activity");
  const activityInput = activity?.querySelector<HTMLTextAreaElement>('textarea[name="activity"]');
  const commit = form.querySelector<HTMLElement>(".lifecycle-commit");
  const commitInput = commit?.querySelector<HTMLInputElement>('input[name="commit"]');
  const activityKind = selected.dataset.activity || "";
  if (activity) activity.hidden = !activityKind;
  if (activityInput) {
    activityInput.required = activityKind === "summary";
    const label = activity?.querySelector<HTMLElement>("span");
    if (label) label.textContent = selected.dataset.activityLabel || "Message";
    if (!preserveValues && !activityKind) activityInput.value = "";
  }
  if (commit) commit.hidden = activityKind !== "summary";
  if (commitInput && !preserveValues && activityKind !== "summary") commitInput.value = "";
}
