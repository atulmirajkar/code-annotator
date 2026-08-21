const { test, expect } = require("./viewer");

// selectCurrentText creates a native range inside the source-backed current
// pane, matching mouse or keyboard selection without relying on coordinates.
async function selectCurrentText(page, text) {
  await page.locator(".diff-current-pane .source-text", { hasText: text }).evaluate((span, selectedText) => {
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

// selectBetween builds a range between text in two explicit panes. It supports
// both valid current-only coverage and intentionally invalid cross-pane cases.
async function selectBetween(page, startSelector, startText, endSelector, endText) {
  await page.evaluate(({ startSelector, startText, endSelector, endText }) => {
    const startNode = Array.from(document.querySelectorAll(startSelector)).find((node) => node.textContent.includes(startText));
    const endNode = Array.from(document.querySelectorAll(endSelector)).find((node) => node.textContent.includes(endText));
    if (!startNode?.firstChild || !endNode?.firstChild) throw new Error("selection endpoint not found");
    const start = startNode.textContent.indexOf(startText);
    const end = endNode.textContent.indexOf(endText) + endText.length;
    const range = document.createRange();
    range.setStart(startNode.firstChild, start);
    range.setEnd(endNode.firstChild, end);
    const selection = window.getSelection();
    selection.removeAllRanges();
    selection.addRange(range);
    document.dispatchEvent(new Event("selectionchange"));
  }, { startSelector, startText, endSelector, endText });
}

// hasAnnotationHighlight inspects Chromium's native highlight registry after
// annotations are loaded or restored.
async function hasAnnotationHighlight(page, text) {
  return page.evaluate((selectedText) => {
    const highlight = globalThis.CSS?.highlights?.get("md-viewer-annotations");
    return highlight ? Array.from(highlight).some((range) => range.toString() === selectedText) : false;
  }, text);
}

test.describe("side-by-side diff", () => {
  test("keeps long lines inside independently scrollable panes", async ({ page, viewerURL }) => {
    await page.goto(`${viewerURL}view/diff-layout.go?mode=diff`);

    const basePane = page.locator(".diff-base-pane");
    const currentPane = page.locator(".diff-current-pane");
    await expect(basePane).toBeVisible();
    await expect(currentPane).toBeVisible();

    const geometry = await page.locator(".diff-panes").evaluate((panes) => {
      const base = panes.querySelector(".diff-base-pane");
      const current = panes.querySelector(".diff-current-pane");
      const baseBox = base.getBoundingClientRect();
      const currentBox = current.getBoundingClientRect();
      return {
        baseRight: baseBox.right,
        currentLeft: currentBox.left,
        baseScrollable: base.scrollWidth > base.clientWidth,
        currentScrollable: current.scrollWidth > current.clientWidth,
      };
    });
    expect(geometry.baseRight).toBeLessThanOrEqual(geometry.currentLeft + 1);
    expect(geometry.baseScrollable).toBe(true);
    expect(geometry.currentScrollable).toBe(true);

    await basePane.evaluate((pane) => { pane.scrollLeft = 80; });
    await expect.poll(() => basePane.evaluate((pane) => pane.scrollLeft)).toBeGreaterThan(0);
    await expect(currentPane.evaluate((pane) => pane.scrollLeft)).resolves.toBe(0);

    const highlight = await page.locator(".diff-current.diff-modified").evaluate((cell) => {
      const text = cell.querySelector("code");
      return {
        color: getComputedStyle(cell).backgroundColor,
        cellRight: cell.getBoundingClientRect().right,
        textRight: text.getBoundingClientRect().right,
      };
    });
    expect(highlight.color).not.toBe("rgba(0, 0, 0, 0)");
    expect(highlight.cellRight).toBeGreaterThanOrEqual(highlight.textRight);
  });

  test("creates, restores, and navigates a current-side annotation", async ({ page, viewerURL }) => {
    const selectedText = "current-side replacement";
    await page.goto(`${viewerURL}view/diff-layout.go?mode=diff`);
    await selectCurrentText(page, selectedText);
    await expect(page.locator(".selection-preview")).toContainText(selectedText);
    await page.locator('.annotation-form textarea[name="comment"]').fill("Review the replacement in Changes view.");
    await page.locator('.annotation-form button[type="submit"]').click();

    const card = page.locator(".annotation-card", { hasText: "Review the replacement in Changes view." });
    await expect(card).toHaveCount(1);
    await card.locator(".annotation-summary").click();
    await expect(card.locator(".annotation-source")).toContainText(selectedText);
    await expect.poll(() => hasAnnotationHighlight(page, selectedText)).toBe(true);

    await page.reload();
    const restoredCard = page.locator(".annotation-card", { hasText: "Review the replacement in Changes view." });
    await expect(restoredCard).toHaveCount(1);
    await expect.poll(() => hasAnnotationHighlight(page, selectedText)).toBe(true);
    await restoredCard.locator(".annotation-summary").click();
    await expect(page.locator(".diff-current-pane .annotation-navigation-target")).toContainText(selectedText);
    await expect(restoredCard.locator(".annotation-navigation-status")).toBeEmpty();
  });

  test("creates an annotation across multiple current rows", async ({ page, viewerURL }) => {
    await page.goto(`${viewerURL}view/diff-layout.go?mode=diff`);
    await selectBetween(
      page,
      ".diff-current-pane .source-text",
      "fixtures",
      ".diff-current-pane .source-text",
      "current-side replacement",
    );

    const preview = page.locator(".selection-preview");
    await expect(preview).toBeVisible();
    await expect(preview).toContainText("fixtures");
    await expect(preview).toContainText("current-side replacement");
    await page.locator('.annotation-form textarea[name="comment"]').fill("Review this multi-line replacement context.");
    await page.locator('.annotation-form button[type="submit"]').click();

    const card = page.locator(".annotation-card", { hasText: "Review this multi-line replacement context." });
    await expect(card).toHaveCount(1);
    await card.locator(".annotation-summary").click();
    await expect(card.locator(".annotation-source")).toContainText("fixtures");
    await expect(card.locator(".annotation-source")).toContainText("current-side replacement");
  });

  test("rejects base-side and cross-pane selections", async ({ page, viewerURL }) => {
    await page.goto(`${viewerURL}view/diff-layout.go?mode=diff`);
    const preview = page.locator(".selection-preview");
    const selectionScope = page.locator('input[name="scope"][value="selection"]');
    const cases = [
      {
        name: "base only",
        startSelector: ".diff-base-pane code",
        startText: "base-side line",
        endSelector: ".diff-base-pane code",
        endText: "horizontal scrolling",
      },
      {
        name: "base to current",
        startSelector: ".diff-base-pane code",
        startText: "base-side line",
        endSelector: ".diff-current-pane .source-text",
        endText: "current-side replacement",
      },
    ];

    for (const selectionCase of cases) {
      await test.step(selectionCase.name, async () => {
        await selectBetween(
          page,
          selectionCase.startSelector,
          selectionCase.startText,
          selectionCase.endSelector,
          selectionCase.endText,
        );
        await expect(preview).toBeHidden();
        await expect(selectionScope).toBeDisabled();
      });
    }
  });
});
