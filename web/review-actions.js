import { reattachAnnotation, replyToAnnotation, updateAnnotation } from "./review-api.js";
import { element } from "./review-dom.js";
import { replyActors, replyActorValue, transitionOptions } from "./review-thread.js";

export function createAnnotationActions({
  documentPath,
  reviewToken,
  getCurrentRevision,
  currentSelection,
  forceClearSelectionPreview,
  loadAnnotations,
  setFormStatus,
  reviewerAuthor,
  list,
}) {
  // Closing is the common reviewer response to an applied annotation, so keep
  // it available without requiring the less-frequent Actions panel to open.
  function createQuickClose(annotation) {
    const button = element("button", "annotation-quick-close");
    button.type = "button";
    button.textContent = "Close";
    button.setAttribute("aria-label", `Close annotation: ${annotation.comment || annotation.id}`);
    button.addEventListener("click", async () => {
      const author = reviewerAuthor() || "reviewer";
      await updateLifecycle(annotation.id, {
        document: documentPath,
        status: "closed",
        actorRole: "reviewer",
        author,
      }, button, null);
    });
    return button;
  }

  // A stale annotation can be rebound only to a currently verified selection.
  // The API rebuilds the selector from source bytes and preserves all review
  // content, so the browser sends no replacement quote or thread data.
  function createReattachForm(annotation) {
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
      const button = reattach.querySelector('button[type="submit"]');
      const help = reattach.querySelector(".reattach-help");
      const selectedRange = currentSelection();
      button.disabled = !selectedRange;
      help.textContent = selectedRange
        ? "Use the current selection as the replacement anchor."
        : "Select replacement text in the document to enable reattachment.";
    });
  }

  async function submitReattach(event, annotationID) {
    event.preventDefault();
    const reattach = event.currentTarget;
    const button = reattach.querySelector('button[type="submit"]');
    const status = reattach.querySelector(".reattach-status");
    const selectedRange = currentSelection();
    if (!selectedRange) {
      status.textContent = "Select replacement text first.";
      status.classList.add("error");
      return;
    }

    const selection = { ...selectedRange };
    status.textContent = "Saving…";
    status.classList.remove("error");
    button.disabled = true;
    try {
      const response = await reattachAnnotation(reviewToken, getCurrentRevision(), annotationID, { document: documentPath, selection });
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
      status.textContent = error.message || "Could not reattach annotation.";
      status.classList.add("error");
      button.disabled = false;
    }
  }

  // Ordinary replies extend the discussion thread without implying that the
  // annotation has advanced through its lifecycle.
  function createReplyForm(annotation) {
    const reply = element("form", "annotation-reply");
    const authorLabel = document.createElement("label");
    authorLabel.append(document.createTextNode("Reply as"));
    const author = document.createElement("select");
    author.name = "author";
    author.required = true;
    replyActors().forEach((actor) => {
      const option = document.createElement("option");
      option.value = actor.value;
      option.textContent = actor.label;
      author.append(option);
    });
    author.value = replyActorValue(reviewerAuthor());
    authorLabel.append(author);

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
    reply.append(authorLabel, messageLabel, status, button);
    reply.addEventListener("submit", (event) => submitReply(event, annotation.id));
    return reply;
  }

  async function submitReply(event, annotationID) {
    event.preventDefault();
    const reply = event.currentTarget;
    const button = reply.querySelector('button[type="submit"]');
    const status = reply.querySelector(".reply-status");
    const fields = new FormData(reply);
    status.textContent = "Saving…";
    status.classList.remove("error");
    button.disabled = true;
    try {
      const response = await replyToAnnotation(reviewToken, getCurrentRevision(), annotationID, {
        document: documentPath,
        author: fields.get("author"),
        message: fields.get("message"),
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
      status.textContent = error.message || "Could not add reply.";
      status.classList.add("error");
      button.disabled = false;
    }
  }

  // Build only the lifecycle actions valid from the annotation's current
  // state. Actor roles and required activity are derived from that action so
  // the browser cannot accidentally submit an invalid transition shape.
  function createLifecycleForm(annotation) {
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

    const authorLabel = document.createElement("label");
    authorLabel.append(document.createTextNode("Author"));
    const author = document.createElement("select");
    author.name = "author";
    author.required = true;
    authorLabel.append(author);

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

    lifecycle.append(actionLabel, authorLabel, activityLabel, commitLabel, status, button);
    const updateFields = () => {
      const selected = action.selectedOptions[0];
      const activityKind = selected.dataset.activity;
      updateLifecycleAuthorOptions(author, selected.dataset.role);
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

  function updateLifecycleAuthorOptions(author, role) {
    const preferred = replyActorValue(reviewerAuthor());
    const actors = role === "agent"
      ? replyActors().filter((actor) => actor.value === "agent")
      : replyActors().filter((actor) => actor.value !== "agent");
    author.replaceChildren();
    actors.forEach((actor) => {
      const option = document.createElement("option");
      option.value = actor.value;
      option.textContent = actor.label;
      author.append(option);
    });
    author.value = actors.some((actor) => actor.value === preferred) ? preferred : actors[0]?.value || "reviewer";
  }

  async function submitLifecycle(event, annotationID) {
    event.preventDefault();
    const lifecycle = event.currentTarget;
    const button = lifecycle.querySelector('button[type="submit"]');
    const status = lifecycle.querySelector(".lifecycle-status");
    const fields = new FormData(lifecycle);
    const selected = lifecycle.elements.status.selectedOptions[0];
    const activityKind = selected.dataset.activity;
    const payload = {
      document: documentPath,
      status: fields.get("status"),
      actorRole: selected.dataset.role,
      author: fields.get("author"),
    };
    if (activityKind === "message") payload.message = fields.get("activity");
    if (activityKind === "summary") {
      payload.summary = fields.get("activity");
      const commit = fields.get("commit");
      if (commit) payload.commit = commit;
    }

    await updateLifecycle(annotationID, payload, button, status);
  }

  // Both the expanded lifecycle form and Quick Close use this mutation path so
  // token, revision-conflict, reload, and error behavior cannot drift apart.
  async function updateLifecycle(annotationID, payload, button, status) {
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
        status.textContent = error.message || "Could not update annotation.";
        status.classList.add("error");
      } else {
        setFormStatus(error.message || "Could not close annotation.", true);
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
