const { writeFile } = require("node:fs/promises");
const path = require("node:path");
const { test, expect } = require("./viewer");

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

test.describe("annotation review interactions", () => {
  test("creates, replies to, transitions, and filters an annotation", async ({ page, viewerURL }) => {
    await page.goto(`${viewerURL}view/lifecycle.md`);
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
    await card.locator('.annotation-lifecycle select[name="status"]').selectOption("closed");
    await card.locator('.annotation-lifecycle button[type="submit"]').click();
    await expect(page.locator(".annotation-card")).toHaveCount(0);
    await page.locator(".show-inactive-annotations").check();
    await expect(page.locator(".annotation-card")).toHaveCount(1);
    await expect(page.locator(".annotation-badge")).toContainText(["change request", "closed"]);
  });

  test("reattaches an annotation after its original text becomes stale", async ({ page, viewer }) => {
    const documentPath = path.join(viewer.contentRoot, "stale.md");
    await writeFile(documentPath, "# Stale-anchor fixture\n\nReview this selected phrase before release.\n\nReplacement anchor lives here.\n");
    await page.goto(`${viewer.url}view/stale.md`);
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
    await createSelectionAnnotation(page, "Keep my draft visible.");

    const token = await page.locator('meta[name="md-viewer-review-token"]').getAttribute("content");
    const current = await page.request.get(`${viewerURL}api/annotations?document=conflict.md`);
    const payload = await current.json();
    const external = await page.request.post(`${viewerURL}api/annotations`, {
      headers: {
        "Content-Type": "application/json",
        "If-Match": JSON.stringify(payload.revision),
        "Origin": new URL(viewerURL).origin,
        "X-MD-Viewer-Token": token,
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
});
