const { writeFile } = require("node:fs/promises");
const path = require("node:path");
const { test, expect, openAnnotations } = require("./viewer");

// selectText asks the browser to create the same native range a reviewer would
// make with pointer or keyboard selection.
async function selectText(page, text) {
  await page.locator(".source-text", { hasText: text }).evaluate((span, selectedText) => {
    const start = span.textContent.indexOf(selectedText);
    if (start < 0 || !span.firstChild) throw new Error(`text not found: ${selectedText}`);
    const range = document.createRange();
    range.setStart(span.firstChild, start);
    range.setEnd(span.firstChild, start + selectedText.length);
    const selection = window.getSelection();
    selection.removeAllRanges();
    selection.addRange(range);
    document.dispatchEvent(new Event("selectionchange"));
  }, text);
}

async function createSelectionAnnotation(page, comment) {
  await selectText(page, "selected phrase");
  await expect(page.locator(".selection-preview")).toContainText("selected phrase");
  await page.locator('.annotation-form textarea[name="comment"]').fill(comment);
  await page.locator('.annotation-form button[type="submit"]').click();
  await expect(page.locator(".annotation-form-status")).toHaveText("Annotation added.");
  await expect(page.locator(".annotation-card")).toHaveCount(1);
}

// hasAnnotationHighlight inspects the browser-native highlight registry used
// by Chromium. The fallback DOM marks are covered by unit-level rendering.
async function hasAnnotationHighlight(page, text) {
  return page.evaluate((selectedText) => {
    const highlight = globalThis.CSS?.highlights?.get("code-annotator-annotations");
    return highlight ? Array.from(highlight).some((range) => range.toString() === selectedText) : false;
  }, text);
}

test.describe("annotation review interactions", () => {
  test("creates and restores an annotation on source code", async ({ page, viewerURL }) => {
    await page.goto(`${viewerURL}view/code-annotation.go`);
    await openAnnotations(page);
    await expect(page.locator(".source-view")).toBeVisible();
    await expect(page.locator("#annotation-sidebar")).toBeVisible();
    await expect.poll(() => page.locator(".source-view").evaluate((source) => {
      const documentBounds = source.closest("main").getBoundingClientRect();
      const sourceBounds = source.getBoundingClientRect();
      return Math.max(sourceBounds.left - documentBounds.left, documentBounds.right - sourceBounds.right);
    })).toBeLessThanOrEqual(9);

    await selectText(page, "left < right");
    await expect(page.locator(".selection-preview")).toContainText("left < right");
    await page.locator('.annotation-form textarea[name="comment"]').fill("Check the comparison direction.");
    await page.locator('.annotation-form button[type="submit"]').click();

    const card = page.locator(".annotation-card");
    await expect(card).toHaveCount(1);
    await card.locator(".annotation-summary").click();
    await expect(card.locator(".annotation-source")).toContainText("left < right");
    await expect.poll(() => hasAnnotationHighlight(page, "left < right")).toBe(true);

    await page.reload();
    await expect(page.locator(".annotation-card")).toHaveCount(1);
    await expect.poll(() => hasAnnotationHighlight(page, "left < right")).toBe(true);
  });

  test("navigates annotation cards to resolved and approximate source", async ({ page, viewer }) => {
    const documentPath = path.join(viewer.contentRoot, "source-navigation.md");
    const filler = Array.from({ length: 45 }, (_, index) => `Paragraph ${index + 1} keeps the target below the fold.`).join("\n\n");
    await writeFile(documentPath, `# Navigation fixture\n\n${filler}\n\nReview this selected phrase before release.\n`);
    await page.emulateMedia({ reducedMotion: "reduce" });
    await page.goto(`${viewer.url}view/source-navigation.md`);
    await openAnnotations(page);
    await createSelectionAnnotation(page, "Navigate to this text.");

    await page.evaluate(() => window.scrollTo(0, 0));
    await page.getByRole("button", { name: "Hide annotations" }).click();
    await page.getByRole("button", { name: "Show annotations" }).click();
    const card = page.locator(".annotation-card");
    await card.locator(".annotation-summary").press("Enter");
    const exactTarget = page.locator(".source-text", { hasText: "selected phrase" });
    await expect(exactTarget).toHaveClass(/annotation-navigation-target/);
    await expect(exactTarget).toHaveCSS("animation-name", "none");
    await expect.poll(() => exactTarget.evaluate((target) => {
      const bounds = target.getBoundingClientRect();
      return bounds.top >= 0 && bounds.bottom <= window.innerHeight;
    })).toBe(true);
    await expect(card.locator(".annotation-navigation-status")).toBeEmpty();

    await writeFile(documentPath, `# Navigation fixture\n\nInserted before the original content.\n\n${filler}\n\nReview this selected phrase before release.\n`);
    await page.reload();
    const movedCard = page.locator(".annotation-card");
    await movedCard.locator(".annotation-summary").click();
    await expect(page.locator(".source-text", { hasText: "selected phrase" })).toHaveClass(/annotation-navigation-target/);
    await expect(movedCard.locator(".annotation-navigation-status")).toBeEmpty();

    await writeFile(documentPath, `# Navigation fixture\n\n${filler}\n\nThe reviewed wording was removed.\n`);
    await page.reload();
    const staleCard = page.locator(".annotation-card");
    await expect(staleCard.locator(".annotation-badge.stale")).toBeVisible();
    await staleCard.locator(".annotation-summary").click();
    await expect(staleCard.locator(".annotation-navigation-status")).toContainText("approximate original location");
    await expect(page.locator(".annotation-navigation-target")).toHaveCount(1);
  });

  test("navigates a document annotation to the heading", async ({ page, viewerURL }) => {
    await page.goto(`${viewerURL}view/document-navigation.md`);
    await openAnnotations(page);
    await page.locator('.annotation-form textarea[name="comment"]').fill("Review the whole document.");
    await page.locator('.annotation-form button[type="submit"]').click();

    const card = page.locator(".annotation-card");
    await card.locator(".annotation-summary").click();
    await expect(page.locator(".markdown-body h1")).toHaveClass(/annotation-navigation-target/);
    await expect(card.locator(".annotation-navigation-status")).toBeEmpty();
  });

  test("creates, replies to, transitions, and filters an annotation", async ({ page, viewerURL }) => {
    await page.goto(`${viewerURL}view/lifecycle.md`);
    await openAnnotations(page);
    await createSelectionAnnotation(page, "Clarify this wording.");

    const card = page.locator(".annotation-card");
    await card.locator(".annotation-summary").click();
    await expect(card.locator(".annotation-source")).toContainText("selected phrase");
    await card.locator(".annotation-actions > summary").click();

    await card.locator('.annotation-reply textarea[name="message"]').fill("I will update it.");
    await card.locator('.annotation-reply button[type="submit"]').click();
    await expect(card.locator(".annotation-thread")).toContainText("I will update it.");

    await card.locator(".annotation-summary").click();
    await card.locator('.annotation-actions > summary').click();
    await card.locator('.annotation-lifecycle select[name="status"]').selectOption("acknowledged");
    await card.locator('.annotation-lifecycle button[type="submit"]').click();
    await expect(card.locator(".annotation-badge")).toContainText(["change request", "acknowledged"]);

    await card.locator(".annotation-summary").click();
    await card.locator(".annotation-actions > summary").click();
    await card.locator('.annotation-lifecycle select[name="status"]').selectOption("applied");
    await card.locator('.annotation-lifecycle textarea[name="activity"]').fill("Updated the wording.");
    await card.locator('.annotation-lifecycle button[type="submit"]').click();
    await expect(card.locator(".annotation-badge")).toContainText(["change request", "applied"]);

    await card.locator(".annotation-summary").click();
    await card.locator(".annotation-actions > summary").click();
    await expect(card.locator('.annotation-lifecycle option[value="closed"]')).toHaveCount(0);
    await expect(card.locator('.annotation-lifecycle option[value="needs_changes"]')).toHaveCount(1);
    await expect(card.locator(".annotation-quick-close")).toBeVisible();
    await card.locator(".annotation-quick-close").click();
    await expect(page.locator(".annotation-card")).toHaveCount(0);
    await page.locator(".show-inactive-annotations").check();
    await expect(page.locator(".annotation-card")).toHaveCount(1);
    await expect(page.locator(".annotation-badge")).toContainText(["change request", "closed"]);
  });

  test("reattaches an annotation after its original text becomes stale", async ({ page, viewer }) => {
    const documentPath = path.join(viewer.contentRoot, "stale.md");
    await writeFile(documentPath, "# Stale-anchor fixture\n\nReview this selected phrase before release.\n\nReplacement anchor lives here.\n");
    await page.goto(`${viewer.url}view/stale.md`);
    await openAnnotations(page);
    await createSelectionAnnotation(page, "Track this anchor.");

    await writeFile(documentPath, "# Stale-anchor fixture\n\nOriginal wording was removed.\n\nReplacement anchor lives here.\n");
    await page.reload();
    const card = page.locator(".annotation-card");
    await expect(card.locator(".annotation-badge.stale")).toBeVisible();

    await selectText(page, "Replacement anchor");
    await card.locator(".annotation-summary").click();
    await card.locator(".annotation-actions > summary").click();
    await card.locator('.annotation-reattach button[type="submit"]').click();
    await expect(card.locator(".annotation-badge.stale")).toHaveCount(0);
    await expect(card.locator(".annotation-source")).toContainText("Replacement anchor");
  });

  test("reloads authoritative annotations after a revision conflict", async ({ page, viewerURL }) => {
    await page.goto(`${viewerURL}view/conflict.md`);
    await openAnnotations(page);
    await createSelectionAnnotation(page, "Keep my draft visible.");

    const token = await page.locator('meta[name="code-annotator-review-token"]').getAttribute("content");
    const current = await page.request.get(`${viewerURL}api/annotations?document=conflict.md`);
    const payload = await current.json();
    const external = await page.request.post(`${viewerURL}api/annotations`, {
      headers: {
        "Content-Type": "application/json",
        "If-Match": JSON.stringify(payload.revision),
        "Origin": new URL(viewerURL).origin,
        "X-Code-Annotator-Token": token,
      },
      data: {
        document: "conflict.md",
        intent: "question",
        comment: "Concurrent annotation",
        author: "agent",
      },
    });
    expect(external.ok()).toBeTruthy();

    const firstCard = page.locator(".annotation-card").first();
    await firstCard.locator(".annotation-summary").click();
    await firstCard.locator(".annotation-actions > summary").click();
    await firstCard.locator('.annotation-reply textarea[name="message"]').fill("Unsaved reply");
    await firstCard.locator('.annotation-reply button[type="submit"]').click();

    await expect(page.locator(".annotation-form-status")).toContainText("Annotations changed");
    await expect(page.locator(".annotation-card")).toHaveCount(2);
    await expect(page.locator(".annotation-list")).toContainText("Concurrent annotation");
  });

  test("reloads authoritative status when Quick Close conflicts", async ({ page, viewerURL }) => {
    await page.goto(`${viewerURL}view/quick-close.md`);
    await openAnnotations(page);
    await page.locator('.annotation-form textarea[name="comment"]').fill("Quick close conflict.");
    await page.locator('.annotation-form button[type="submit"]').click();

    const card = page.locator(".annotation-card", { hasText: "Quick close conflict." });
    await card.locator(".annotation-summary").click();
    await card.locator(".annotation-actions > summary").click();
    await card.locator('.annotation-lifecycle select[name="status"]').selectOption("acknowledged");
    await card.locator('.annotation-lifecycle button[type="submit"]').click();

    await card.locator(".annotation-summary").click();
    await card.locator(".annotation-actions > summary").click();
    await card.locator('.annotation-lifecycle select[name="status"]').selectOption("applied");
    await card.locator('.annotation-lifecycle textarea[name="activity"]').fill("Applied for conflict coverage.");
    await card.locator('.annotation-lifecycle button[type="submit"]').click();
    await card.locator(".annotation-summary").click();
    await expect(card.locator(".annotation-quick-close")).toBeVisible();

    const token = await page.locator('meta[name="code-annotator-review-token"]').getAttribute("content");
    const current = await page.request.get(`${viewerURL}api/annotations?document=quick-close.md`);
    const payload = await current.json();
    const external = await page.request.post(`${viewerURL}api/annotations`, {
      headers: {
        "Content-Type": "application/json",
        "If-Match": JSON.stringify(payload.revision),
        "Origin": new URL(viewerURL).origin,
        "X-Code-Annotator-Token": token,
      },
      data: {
        document: "quick-close.md",
        intent: "question",
        comment: "Concurrent quick-close annotation",
        author: "agent",
      },
    });
    expect(external.ok()).toBeTruthy();

    await card.locator(".annotation-quick-close").click();
    await expect(page.locator(".annotation-form-status")).toContainText("Annotations changed");
    await expect(page.locator(".annotation-card")).toHaveCount(2);
    await expect(card.locator(".annotation-badge")).toContainText(["change request", "applied"]);
    await card.locator(".annotation-summary").click();
    await expect(card.locator(".annotation-quick-close")).toBeVisible();
  });
});
