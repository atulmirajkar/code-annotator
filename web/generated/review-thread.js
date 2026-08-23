export function replyActors() {
    return [
        { value: "reviewer", label: "Reviewer" },
        { value: "author", label: "Author" },
        { value: "agent", label: "Agent" },
    ];
}
export function replyActorValue(author) {
    const preferred = String(author || "").trim().toLowerCase();
    return replyActors().some((actor) => actor.value === preferred)
        ? preferred
        : "reviewer";
}
export function transitionOptions(status) {
    const transitions = {
        open: [
            { status: "acknowledged", label: "Acknowledge", role: "agent" },
            { status: "rejected", label: "Reject", role: "agent", activity: "message" },
        ],
        acknowledged: [
            { status: "applied", label: "Mark applied", role: "agent", activity: "summary" },
            { status: "rejected", label: "Reject", role: "agent", activity: "message" },
        ],
        needs_changes: [
            { status: "acknowledged", label: "Acknowledge retry", role: "agent" },
            { status: "rejected", label: "Reject", role: "agent", activity: "message" },
        ],
        applied: [
            { status: "closed", label: "Close", role: "reviewer" },
            { status: "needs_changes", label: "Needs changes", role: "reviewer", activity: "message" },
        ],
        closed: [{ status: "open", label: "Reopen", role: "reviewer" }],
        rejected: [{ status: "open", label: "Reopen", role: "reviewer" }],
    };
    return transitions[status] || [];
}
export function threadText(entry) {
    return entry.message || entry.summary || `${entry.fromStatus || ""} → ${entry.toStatus || ""}`;
}
export function showThreadEntry(entry) {
    return entry.kind !== "acknowledgement";
}
export function threadKind(entry) {
    const kinds = {
        reply: { label: "Reply", className: "reply" },
        acknowledgement: { label: "Acknowledgement", className: "acknowledgement" },
        resolution: { label: "Resolution", className: "resolution" },
        review: { label: "Review note", className: "review" },
        status_change: { label: "Status change", className: "status-change" },
    };
    return kinds[entry.kind] || { label: "Update", className: "update" };
}
export function annotationTurnBadge(annotation) {
    if (annotation.status !== "open" && annotation.status !== "needs_changes")
        return null;
    const role = latestThreadActorRole(annotation);
    if (role === "agent") {
        return { label: "waiting for reviewer", className: "pending-review" };
    }
    if (role === "reviewer") {
        return { label: "waiting for agent", className: "pending-agent" };
    }
    return null;
}
function latestThreadActorRole(annotation) {
    if (!Array.isArray(annotation.thread))
        return null;
    for (let index = annotation.thread.length - 1; index >= 0; index -= 1) {
        const entry = annotation.thread[index];
        if (!entry)
            continue;
        const role = threadActorRole(entry, annotation);
        if (role)
            return role;
    }
    return null;
}
function threadActorRole(entry, annotation) {
    if (entry.actorRole === "agent" || entry.actorRole === "reviewer")
        return entry.actorRole;
    const author = normalizeThreadAuthor(entry.author);
    if (!author)
        return null;
    const reviewerAuthor = normalizeThreadAuthor(annotation.author);
    if (author === reviewerAuthor || author === "reviewer")
        return "reviewer";
    if (author === "author")
        return "reviewer";
    if (author === "agent" || author === "codex" || author === "claude")
        return "agent";
    return null;
}
function normalizeThreadAuthor(author) {
    return String(author || "").trim().toLowerCase();
}
