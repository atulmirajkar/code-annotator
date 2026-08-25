const reviewPanelMinWidth = 320;
const reviewPanelMaxWidth = 640;
const reviewPanelWidthStep = 24;

interface ReviewPanelOptions {
  panel: HTMLElement;
  form: HTMLFormElement;
  formStatus: HTMLElement;
  addAnnotationButton: HTMLButtonElement | null;
  closeAnnotationButton: HTMLButtonElement | null;
  layout: HTMLElement | null;
  resizeHandle: HTMLElement | null;
  documentPath: string;
  storage: Storage;
}

export function createReviewPanelController({
  panel,
  form,
  formStatus,
  addAnnotationButton,
  closeAnnotationButton,
  layout,
  resizeHandle,
  documentPath,
  storage,
}: ReviewPanelOptions) {
  const reviewPanelWidthKey = `code-annotator-review-panel-width:${documentPath}`;
  let reviewPanelWidth = restoreReviewPanelWidth() || 368;

  applyReviewPanelWidth(reviewPanelWidth);
  if (resizeHandle) {
    resizeHandle.setAttribute("aria-valuemin", String(reviewPanelMinWidth));
    resizeHandle.setAttribute("aria-valuemax", String(reviewPanelMaxWidth));
    resizeHandle.setAttribute("aria-valuenow", String(reviewPanelWidth));
    resizeHandle.addEventListener("pointerdown", startReviewPanelResize);
    resizeHandle.addEventListener("keydown", handleReviewPanelResizeKeydown);
  }

  syncAnnotationFormToggle();
  if (addAnnotationButton) {
    addAnnotationButton.addEventListener("click", () => {
      setAnnotationFormVisible(true);
      setFormStatus("");
    });
  }
  closeAnnotationButton?.addEventListener("click", () => {
    setAnnotationFormVisible(false);
    setFormStatus("");
  });

  function setFormStatus(message: string, error = false): void {
    formStatus.textContent = message;
    formStatus.classList.toggle("error", error);
  }

  function setAnnotationFormVisible(visible: boolean): void {
    form.hidden = !visible;
    syncAnnotationFormToggle();
    if (visible) {
      (form.elements.namedItem("comment") as HTMLTextAreaElement | null)?.focus(
        { preventScroll: true },
      );
    }
  }

  function syncAnnotationFormToggle() {
    if (!addAnnotationButton) return;
    const visible = !form.hidden;
    addAnnotationButton.textContent = "Add comment";
    addAnnotationButton.hidden = visible;
    addAnnotationButton.setAttribute("aria-expanded", String(visible));
  }

  function restoreReviewPanelWidth() {
    try {
      const stored = storage.getItem(reviewPanelWidthKey);
      const width = Number.parseInt(stored || "", 10);
      return Number.isInteger(width) ? clampReviewPanelWidth(width) : null;
    } catch (_) {
      return null;
    }
  }

  function persistReviewPanelWidth(width: number): void {
    try {
      storage.setItem(reviewPanelWidthKey, String(width));
    } catch (_) {
      // Ignore storage failures; the resize still applies for this session.
    }
  }

  function clampReviewPanelWidth(width: number): number {
    return Math.max(
      reviewPanelMinWidth,
      Math.min(reviewPanelMaxWidth, Math.round(width)),
    );
  }

  function applyReviewPanelWidth(width: number): void {
    reviewPanelWidth = clampReviewPanelWidth(width);
    if (layout)
      layout.style.setProperty("--review-panel-width", `${reviewPanelWidth}px`);
    if (resizeHandle)
      resizeHandle.setAttribute("aria-valuenow", String(reviewPanelWidth));
  }

  function startReviewPanelResize(event: PointerEvent): void {
    if (!resizeHandle) return;
    if (event.button !== 0) return;
    event.preventDefault();
    resizeHandle.setPointerCapture(event.pointerId);
    const startX = event.clientX;
    const startWidth = panel.getBoundingClientRect().width;

    const track = (moveEvent: PointerEvent): void => {
      applyReviewPanelWidth(startWidth + (startX - moveEvent.clientX));
    };
    const finish = () => {
      resizeHandle.removeEventListener("pointermove", track);
      persistReviewPanelWidth(reviewPanelWidth);
    };

    resizeHandle.addEventListener("pointermove", track);
    resizeHandle.addEventListener("pointerup", finish, { once: true });
    resizeHandle.addEventListener("pointercancel", finish, { once: true });
  }

  function handleReviewPanelResizeKeydown(event: KeyboardEvent): void {
    let delta = 0;
    if (event.key === "ArrowLeft") delta = -reviewPanelWidthStep;
    else if (event.key === "ArrowRight") delta = reviewPanelWidthStep;
    else if (event.key === "Home")
      delta = reviewPanelMinWidth - reviewPanelWidth;
    else if (event.key === "End")
      delta = reviewPanelMaxWidth - reviewPanelWidth;
    else return;
    event.preventDefault();
    applyReviewPanelWidth(reviewPanelWidth + delta);
    persistReviewPanelWidth(reviewPanelWidth);
  }

  return {
    setAnnotationFormVisible,
    setFormStatus,
  };
}
