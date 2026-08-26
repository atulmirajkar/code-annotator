// @vitest-environment happy-dom

import { beforeEach, describe, expect, it } from "vitest";

import { bindAnnotationCardDisclosures } from "./annotation-card-disclosures.js";

function fixture(): {
  panel: HTMLElement;
  replyButton: HTMLButtonElement;
  actionsButton: HTMLButtonElement;
  replyPanel: HTMLElement;
  actionsPanel: HTMLElement;
  message: HTMLTextAreaElement;
  action: HTMLSelectElement;
  bindings: AbortController;
} {
  document.body.innerHTML = `<aside id="annotation-sidebar">
    <button type="button" class="annotation-disclosure-toggle annotation-reply-toggle"
      aria-expanded="false" aria-controls="reply-panel">Reply</button>
    <button type="button" class="annotation-disclosure-toggle annotation-actions-toggle"
      aria-expanded="false" aria-controls="actions-panel">Actions</button>
    <div id="reply-panel" class="annotation-reply-panel" hidden>
      <textarea name="message"></textarea>
    </div>
    <div id="actions-panel" class="annotation-actions-panel" hidden>
      <select name="status"><option>Close</option></select>
    </div>
  </aside>`;
  const panel = document.querySelector<HTMLElement>("#annotation-sidebar");
  const replyButton = document.querySelector<HTMLButtonElement>(
    ".annotation-reply-toggle",
  );
  const actionsButton = document.querySelector<HTMLButtonElement>(
    ".annotation-actions-toggle",
  );
  const replyPanel = document.querySelector<HTMLElement>("#reply-panel");
  const actionsPanel = document.querySelector<HTMLElement>("#actions-panel");
  const message = document.querySelector<HTMLTextAreaElement>(
    'textarea[name="message"]',
  );
  const action = document.querySelector<HTMLSelectElement>(
    'select[name="status"]',
  );
  if (
    !panel ||
    !replyButton ||
    !actionsButton ||
    !replyPanel ||
    !actionsPanel ||
    !message ||
    !action
  ) {
    throw new Error("incomplete annotation disclosure fixture");
  }
  const bindings = new AbortController();
  bindAnnotationCardDisclosures(panel, bindings.signal);
  return {
    panel,
    replyButton,
    actionsButton,
    replyPanel,
    actionsPanel,
    message,
    action,
    bindings,
  };
}

function operationFixture(options: { withFeedback: boolean }): {
  panel: HTMLElement;
  card: HTMLElement;
  replyButton: HTMLButtonElement;
  actionsButton: HTMLButtonElement;
  replyForm: HTMLFormElement;
  lifecycleForm: HTMLFormElement;
  quickCloseForm: HTMLFormElement;
  operationFocus: ReturnType<typeof bindAnnotationCardDisclosures>;
} {
  document.body.innerHTML = `<h2 id="review-heading" tabindex="-1">Annotations</h2>
  <aside id="annotation-sidebar">
    <div id="annotation-panel-content">
      ${options.withFeedback ? '<p class="annotation-panel-feedback" role="status" tabindex="-1">Annotations changed elsewhere.</p>' : ""}
      <details id="annotation-ann_1" class="annotation-card">
        <summary class="annotation-summary">Comment</summary>
        <button type="button" class="annotation-disclosure-toggle annotation-reply-toggle"
          aria-expanded="false" aria-controls="reply-panel">Reply</button>
        <button type="button" class="annotation-disclosure-toggle annotation-actions-toggle"
          aria-expanded="false" aria-controls="actions-panel">Actions</button>
        <div id="reply-panel" class="annotation-reply-panel" hidden>
          <form class="annotation-reply"><textarea name="message"></textarea></form>
        </div>
        <div id="actions-panel" class="annotation-actions-panel" hidden>
          <form class="annotation-lifecycle"><select name="status"></select></form>
        </div>
        <form class="annotation-quick-close-form"><button type="submit">Close</button></form>
      </details>
    </div>
  </aside>`;
  const panel = document.querySelector<HTMLElement>("#annotation-sidebar");
  const card = document.querySelector<HTMLElement>("#annotation-ann_1");
  const replyButton = document.querySelector<HTMLButtonElement>(
    ".annotation-reply-toggle",
  );
  const actionsButton = document.querySelector<HTMLButtonElement>(
    ".annotation-actions-toggle",
  );
  const replyForm = document.querySelector<HTMLFormElement>(".annotation-reply");
  const lifecycleForm = document.querySelector<HTMLFormElement>(
    ".annotation-lifecycle",
  );
  const quickCloseForm = document.querySelector<HTMLFormElement>(
    ".annotation-quick-close-form",
  );
  if (
    !panel ||
    !card ||
    !replyButton ||
    !actionsButton ||
    !replyForm ||
    !lifecycleForm ||
    !quickCloseForm
  ) {
    throw new Error("incomplete annotation operation fixture");
  }
  const bindings = new AbortController();
  const operationFocus = bindAnnotationCardDisclosures(panel, bindings.signal);
  return {
    panel,
    card,
    replyButton,
    actionsButton,
    replyForm,
    lifecycleForm,
    quickCloseForm,
    operationFocus,
  };
}

// happy-dom does not run real form submission navigation; dispatching a
// cancelable "submit" event on the form is the supported way to trigger the
// same delegated listener bindAnnotationCardDisclosures installs on panel.
function submit(form: HTMLFormElement): void {
  form.dispatchEvent(
    new Event("submit", { bubbles: true, cancelable: true }),
  );
}

describe("annotation card disclosures", () => {
  beforeEach(() => {
    document.body.innerHTML = "";
  });

  it("opens Reply independently and focuses its message", () => {
    const view = fixture();

    view.replyButton.click();

    expect(view.replyPanel.hidden).toBe(false);
    expect(view.replyButton.getAttribute("aria-expanded")).toBe("true");
    expect(view.actionsPanel.hidden).toBe(true);
    expect(document.activeElement).toBe(view.message);
  });

  it("allows Reply and Actions to be open together", () => {
    const view = fixture();

    view.replyButton.click();
    view.actionsButton.click();

    expect(view.replyPanel.hidden).toBe(false);
    expect(view.actionsPanel.hidden).toBe(false);
    expect(view.actionsButton.getAttribute("aria-expanded")).toBe("true");
  });

  it("closes the containing region with Escape and restores trigger focus", () => {
    const view = fixture();
    view.actionsButton.click();
    view.action.focus();

    view.action.dispatchEvent(
      new KeyboardEvent("keydown", { key: "Escape", bubbles: true }),
    );

    expect(view.actionsPanel.hidden).toBe(true);
    expect(view.actionsButton.getAttribute("aria-expanded")).toBe("false");
    expect(document.activeElement).toBe(view.actionsButton);
  });

  it("stops handling controls after cleanup", () => {
    const view = fixture();
    view.bindings.abort();

    view.replyButton.click();

    expect(view.replyPanel.hidden).toBe(true);
  });

  it("focuses the card summary after a successful reply", () => {
    const view = operationFixture({ withFeedback: false });

    submit(view.replyForm);
    view.operationFocus.restoreAfterSwap(true);

    expect(document.activeElement).toBe(
      view.card.querySelector(".annotation-summary"),
    );
  });

  it("reopens Reply and focuses the panel feedback after a failed reply", () => {
    const view = operationFixture({ withFeedback: true });

    submit(view.replyForm);
    view.operationFocus.restoreAfterSwap(false);

    expect(view.replyButton.getAttribute("aria-expanded")).toBe("true");
    expect(document.activeElement).toBe(
      document.querySelector(".annotation-panel-feedback"),
    );
  });

  it("reopens Actions and falls back to the card summary when no feedback is rendered", () => {
    const view = operationFixture({ withFeedback: false });

    submit(view.lifecycleForm);
    view.operationFocus.restoreAfterSwap(false);

    expect(view.actionsButton.getAttribute("aria-expanded")).toBe("true");
    expect(document.activeElement).toBe(
      view.card.querySelector(".annotation-summary"),
    );
  });

  it("does not reopen a disclosure for a failed quick close", () => {
    const view = operationFixture({ withFeedback: true });

    submit(view.quickCloseForm);
    view.operationFocus.restoreAfterSwap(false);

    expect(view.replyButton.getAttribute("aria-expanded")).toBe("false");
    expect(view.actionsButton.getAttribute("aria-expanded")).toBe("false");
    expect(document.activeElement).toBe(
      document.querySelector(".annotation-panel-feedback"),
    );
  });

  it("falls back to the panel heading when the card no longer exists", () => {
    const view = operationFixture({ withFeedback: false });

    submit(view.lifecycleForm);
    view.card.remove();
    view.operationFocus.restoreAfterSwap(true);

    expect(document.activeElement).toBe(
      document.getElementById("review-heading"),
    );
  });

  it("does nothing when no operation was captured before the swap", () => {
    const view = operationFixture({ withFeedback: false });

    view.operationFocus.restoreAfterSwap(true);

    expect(document.activeElement).not.toBe(
      view.card.querySelector(".annotation-summary"),
    );
  });
});
