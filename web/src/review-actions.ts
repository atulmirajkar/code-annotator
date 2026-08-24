import { reattachAnnotation, replyToAnnotation, updateAnnotation } from "./review-api.js";
import { element } from "./review-dom.js";
import { replyRoles, transitionOptions } from "./review-thread.js";
import type {
  Annotation,
  ReattachRequest,
  SelectionPayload,
  TransitionRequest,
} from "./types.js";

interface AnnotationActionsOptions {
  documentPath: string;
  reviewToken: string;
  getCurrentRevision: () => string;
  currentSelection: () => SelectionPayload | null;
  forceClearSelectionPreview: () => void;
  loadAnnotations: () => Promise<void>;
  setFormStatus: (message: string, isError?: boolean) => void;
  list: HTMLElement;
}

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error && error.message ? error.message : fallback;
}

function requiredElement<T extends Element>(value: T | null, label: string): T {
  if (!value) throw new Error(`Missing ${label} in review template`);
  return value;
}

export function createAnnotationActions({
  documentPath,
  reviewToken,
  getCurrentRevision,
  currentSelection,
  forceClearSelectionPreview,
  loadAnnotations,
  setFormStatus,
  list,
}: AnnotationActionsOptions) {
  // Closing is the common reviewer response to an applied annotation, so keep
  // it available without requiring the less-frequent Actions panel to open.
  function createQuickClose(annotation: Annotation): HTMLButtonElement {
    const button = element("button", "annotation-quick-close");
    button.type = "button";
    button.textContent = "Close";
    button.setAttribute("aria-label", `Close annotation: ${annotation.comment || annotation.id}`);
    button.addEventListener("click", async () => {
      await updateLifecycle(annotation.id, {
        document: documentPath,
        status: "closed",
        role: "reviewer",
      }, button, null);
    });
    return button;
  }

  // A stale annotation can be rebound only to a currently verified selection.
  // The API rebuilds the selector from source bytes and preserves all review
  // content, so the browser sends no replacement quote or thread data.
  function createReattachForm(annotation: Annotation): HTMLFormElement {
    const reattach = element("form", "annotation-reattach");
    const help = element("p", "reattach-help");
    const selectedRange = currentSelection();
    help.textContent = selectedRange
      ? "Use the current selection as the replacement anchor."
      : "Select replacement text in the document to enable reattachment.";
    const status = element("p", "reattach-status");
    status.setAttribute("role", "status");
    const button = document.createElement("button");
    button.type = "submit";
    button.textContent = "Reattach selection";
    button.disabled = !selectedRange;
    reattach.append(help, status, button);
    reattach.addEventListener("submit", (event) => submitReattach(event, annotation.id));
    return reattach;
  }

  // Keep every visible stale card synchronized with the one shared document
  // selection. The user chooses the target annotation by its card button.
  function updateReattachControls() {
    list.querySelectorAll(".annotation-reattach").forEach((reattach) => {
      const button = requiredElement(reattach.querySelector<HTMLButtonElement>('button[type="submit"]'), "reattach button");
      const help = requiredElement(reattach.querySelector<HTMLElement>(".reattach-help"), "reattach help");
      const selectedRange = currentSelection();
      button.disabled = !selectedRange;
      help.textContent = selectedRange
        ? "Use the current selection as the replacement anchor."
        : "Select replacement text in the document to enable reattachment.";
    });
  }

  async function submitReattach(event: Event, annotationID: string): Promise<void> {
    event.preventDefault();
    const reattach = requiredElement(event.currentTarget as HTMLFormElement | null, "reattach form");
    const button = requiredElement(reattach.querySelector<HTMLButtonElement>('button[type="submit"]'), "reattach button");
    const status = requiredElement(reattach.querySelector<HTMLElement>(".reattach-status"), "reattach status");
    const selectedRange = currentSelection();
    if (!selectedRange) {
      status.textContent = "Select replacement text first.";
      status.classList.add("error");
      return;
    }

    const selection: SelectionPayload = { ...selectedRange };
    status.textContent = "Saving…";
    status.classList.remove("error");
    button.disabled = true;
    try {
      const payload: ReattachRequest = { document: documentPath, selection };
      const response = await reattachAnnotation(reviewToken, getCurrentRevision(), annotationID, payload);
      if (!response.ok) {
        if (response.status === 409) {
          await loadAnnotations();
          setFormStatus("The document or annotations changed. Refresh and select again.", true);
          return;
        }
        throw new Error((await response.text()).trim() || `Could not reattach annotation (${response.status}).`);
      }
      window.getSelection()?.removeAllRanges();
      forceClearSelectionPreview();
      await loadAnnotations();
    } catch (error) {
      status.textContent = errorMessage(error, "Could not reattach annotation.");
      status.classList.add("error");
      button.disabled = false;
    }
  }

  // Ordinary replies extend the discussion thread without implying that the
  // annotation has advanced through its lifecycle.
  function createReplyForm(annotation: Annotation): HTMLFormElement {
    const reply = element("form", "annotation-reply");
    const roleLabel = document.createElement("label");
    roleLabel.append(document.createTextNode("Reply as"));
    const role = document.createElement("select");
    role.name = "role";
    role.required = true;
    replyRoles().forEach((replyRole) => {
      const option = document.createElement("option");
      option.value = replyRole.value;
      option.textContent = replyRole.label;
      role.append(option);
    });
    roleLabel.append(role);

    const messageLabel = document.createElement("label");
    messageLabel.append(document.createTextNode("Reply"));
    const message = document.createElement("textarea");
    message.name = "message";
    message.rows = 3;
    message.required = true;
    messageLabel.append(message);

    const status = element("p", "reply-status");
    status.setAttribute("role", "status");
    const button = document.createElement("button");
    button.type = "submit";
    button.textContent = "Add reply";
    reply.append(roleLabel, messageLabel, status, button);
    reply.addEventListener("submit", (event) => submitReply(event, annotation.id));
    return reply;
  }

  async function submitReply(event: Event, annotationID: string): Promise<void> {
    event.preventDefault();
    const reply = requiredElement(event.currentTarget as HTMLFormElement | null, "reply form");
    const button = requiredElement(reply.querySelector<HTMLButtonElement>('button[type="submit"]'), "reply button");
    const status = requiredElement(reply.querySelector<HTMLElement>(".reply-status"), "reply status");
    const fields = new FormData(reply);
    status.textContent = "Saving…";
    status.classList.remove("error");
    button.disabled = true;
    try {
      const response = await replyToAnnotation(reviewToken, getCurrentRevision(), annotationID, {
        document: documentPath,
        role: String(fields.get("role") || "") as "agent" | "reviewer",
        message: String(fields.get("message") || ""),
      });
      if (!response.ok) {
        if (response.status === 409) {
          await loadAnnotations();
          setFormStatus("Annotations changed. Review the latest thread and try again.", true);
          return;
        }
        throw new Error((await response.text()).trim() || `Could not add reply (${response.status}).`);
      }
      await loadAnnotations();
    } catch (error) {
      status.textContent = errorMessage(error, "Could not add reply.");
      status.classList.add("error");
      button.disabled = false;
    }
  }

  // Build only the lifecycle actions valid from the annotation's current
  // state. Roles and required activity are derived from that action so
  // the browser cannot accidentally submit an invalid transition shape.
  function createLifecycleForm(annotation: Annotation): HTMLFormElement | null {
    // Applied annotations expose Close as a quick action beside this panel.
    // Keep only Needs changes here so the same transition is not duplicated.
    const options = transitionOptions(annotation.status)
      .filter((option) => !(annotation.status === "applied" && option.status === "closed"));
    if (options.length === 0) return null;

    const lifecycle = element("form", "annotation-lifecycle");
    const actionLabel = document.createElement("label");
    actionLabel.append(document.createTextNode("Action"));
    const action = document.createElement("select");
    action.name = "status";
    options.forEach((option) => {
      const item = document.createElement("option");
      item.value = option.status;
      item.textContent = option.label;
      item.dataset.role = option.role;
      item.dataset.activity = option.activity || "";
      action.append(item);
    });
    actionLabel.append(action);

    const roleLabel = document.createElement("label");
    roleLabel.append(document.createTextNode("Role"));
    const role = document.createElement("select");
    role.name = "role";
    role.required = true;
    roleLabel.append(role);

    const activityLabel = document.createElement("label");
    activityLabel.className = "lifecycle-activity";
    const activityTitle = document.createElement("span");
    const activity = document.createElement("textarea");
    activity.name = "activity";
    activity.rows = 3;
    activityLabel.append(activityTitle, activity);

    const commitLabel = document.createElement("label");
    commitLabel.className = "lifecycle-commit";
    commitLabel.append(document.createTextNode("Commit (optional)"));
    const commit = document.createElement("input");
    commit.name = "commit";
    commitLabel.append(commit);

    const status = element("p", "lifecycle-status");
    status.setAttribute("role", "status");
    const button = document.createElement("button");
    button.type = "submit";
    button.textContent = "Update status";

    lifecycle.append(actionLabel, roleLabel, activityLabel, commitLabel, status, button);
    const updateFields = () => {
      const selected = requiredElement(action.selectedOptions[0] || null, "lifecycle action");
      const activityKind = selected.dataset.activity;
      const selectedRole = selected.dataset.role || "reviewer";
      role.replaceChildren(new Option(selectedRole === "agent" ? "Agent" : "Reviewer", selectedRole));
      activityLabel.hidden = !activityKind;
      activity.required = Boolean(activityKind);
      activityTitle.textContent = activityKind === "summary" ? "Summary" : "Message";
      commitLabel.hidden = activityKind !== "summary";
      if (!activityKind) activity.value = "";
      if (activityKind !== "summary") commit.value = "";
    };
    action.addEventListener("change", updateFields);
    lifecycle.addEventListener("submit", (event) => submitLifecycle(event, annotation.id));
    updateFields();
    return lifecycle;
  }

  async function submitLifecycle(event: Event, annotationID: string): Promise<void> {
    event.preventDefault();
    const lifecycle = requiredElement(event.currentTarget as HTMLFormElement | null, "lifecycle form");
    const button = requiredElement(lifecycle.querySelector<HTMLButtonElement>('button[type="submit"]'), "lifecycle button");
    const status = requiredElement(lifecycle.querySelector<HTMLElement>(".lifecycle-status"), "lifecycle status");
    const fields = new FormData(lifecycle);
    const selected = requiredElement((lifecycle.elements.namedItem("status") as HTMLSelectElement | null)?.selectedOptions[0] || null, "lifecycle action");
    const activityKind = selected.dataset.activity;
    const payload: TransitionRequest = {
      document: documentPath,
      status: selected.value as TransitionRequest["status"],
      role: String(fields.get("role") || "") as TransitionRequest["role"],
    };
    if (activityKind === "message") payload.message = String(fields.get("activity") || "");
    if (activityKind === "summary") {
      payload.summary = String(fields.get("activity") || "");
      const commit = fields.get("commit");
      if (commit) payload.commit = String(commit);
    }

    await updateLifecycle(annotationID, payload, button, status);
  }

  // Both the expanded lifecycle form and Quick Close use this mutation path so
  // token, revision-conflict, reload, and error behavior cannot drift apart.
  async function updateLifecycle(annotationID: string, payload: TransitionRequest, button: HTMLButtonElement, status: HTMLElement | null): Promise<void> {
    if (status) {
      status.textContent = "Saving…";
      status.classList.remove("error");
    }
    button.disabled = true;
    try {
      const response = await updateAnnotation(reviewToken, getCurrentRevision(), annotationID, payload);
      if (!response.ok) {
        if (response.status === 409) {
          await loadAnnotations();
          setFormStatus("Annotations changed. Review the latest status and try again.", true);
          return;
        }
        throw new Error((await response.text()).trim() || `Could not update annotation (${response.status}).`);
      }
      await loadAnnotations();
    } catch (error) {
      if (status) {
        status.textContent = errorMessage(error, "Could not update annotation.");
        status.classList.add("error");
      } else {
        setFormStatus(errorMessage(error, "Could not close annotation."), true);
      }
      button.disabled = false;
    }
  }

  return {
    createQuickClose,
    createReattachForm,
    createReplyForm,
    createLifecycleForm,
    updateReattachControls,
  };
}
