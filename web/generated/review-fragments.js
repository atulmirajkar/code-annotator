export function configureLifecycleForm(form, transitions, preserveValues) {
    const status = form.elements.namedItem("status");
    const role = form.elements.namedItem("role");
    if (!(status instanceof HTMLSelectElement) || !(role instanceof HTMLSelectElement))
        return;
    const behavior = transitions.find((candidate) => candidate.status === status.value);
    if (!behavior)
        return;
    const roleOption = document.createElement("option");
    roleOption.value = behavior.role;
    roleOption.textContent = behavior.role === "agent" ? "Agent" : "Reviewer";
    role.replaceChildren(roleOption);
    const activity = form.querySelector(".lifecycle-activity");
    const activityInput = activity?.querySelector('textarea[name="activity"]');
    const commit = form.querySelector(".lifecycle-commit");
    const commitInput = commit?.querySelector('input[name="commit"]');
    if (activity)
        activity.hidden = !behavior.activity;
    if (activityInput) {
        activityInput.required = behavior.activity === "summary";
        const label = activity?.querySelector("span");
        if (label)
            label.textContent = behavior.activityLabel || "Message";
        if (!preserveValues && !behavior.activity)
            activityInput.value = "";
    }
    if (commit)
        commit.hidden = behavior.activity !== "summary";
    if (commitInput && !preserveValues && behavior.activity !== "summary")
        commitInput.value = "";
}
