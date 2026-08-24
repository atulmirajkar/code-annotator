import type { TransitionBehavior } from "./viewer-state.js";

export function configureLifecycleForm(
  form: HTMLFormElement,
  transitions: ReadonlyArray<TransitionBehavior>,
  preserveValues: boolean,
): void {
  const status = form.elements.namedItem("status");
  const role = form.elements.namedItem("role");
  if (!(status instanceof HTMLSelectElement) || !(role instanceof HTMLSelectElement)) return;
  const behavior = transitions.find((candidate) => candidate.status === status.value);
  if (!behavior) return;

  const roleOption = document.createElement("option");
  roleOption.value = behavior.role;
  roleOption.textContent = behavior.role === "agent" ? "Agent" : "Reviewer";
  role.replaceChildren(roleOption);
  const activity = form.querySelector<HTMLElement>(".lifecycle-activity");
  const activityInput = activity?.querySelector<HTMLTextAreaElement>('textarea[name="activity"]');
  const commit = form.querySelector<HTMLElement>(".lifecycle-commit");
  const commitInput = commit?.querySelector<HTMLInputElement>('input[name="commit"]');
  if (activity) activity.hidden = !behavior.activity;
  if (activityInput) {
    activityInput.required = behavior.activity === "summary";
    const label = activity?.querySelector<HTMLElement>("span");
    if (label) label.textContent = behavior.activityLabel || "Message";
    if (!preserveValues && !behavior.activity) activityInput.value = "";
  }
  if (commit) commit.hidden = behavior.activity !== "summary";
  if (commitInput && !preserveValues && behavior.activity !== "summary") commitInput.value = "";
}
