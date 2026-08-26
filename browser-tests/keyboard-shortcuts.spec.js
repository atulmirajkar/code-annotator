const { test, expect } = require("./viewer");

// selectText asks the browser to create the same native range a reviewer
// would make with pointer or keyboard selection. Duplicated from
// annotation.spec.js, which does not export it.
async function selectText(page, text) {
  await page.locator(".source-text", { hasText: text }).evaluate((span, selectedText) => {
    const start = span.textContent.indexOf(selectedText);
    if (start < 0) throw new Error(`text not found: ${selectedText}`);
    const point = (offset) => {
      const walker = document.createTreeWalker(span, NodeFilter.SHOW_TEXT);
      let node;
      let remaining = offset;
      let last = null;
      while ((node = walker.nextNode())) {
        last = node;
        if (remaining <= node.data.length) return [node, remaining];
        remaining -= node.data.length;
      }
      return last ? [last, last.data.length] : [span, 0];
    };
    const range = document.createRange();
    range.setStart(...point(start));
    range.setEnd(...point(start + selectedText.length));
    const selection = window.getSelection();
    selection.removeAllRanges();
    selection.addRange(range);
    document.dispatchEvent(new Event("selectionchange"));
  }, text);
}

test.describe("keyboard shortcuts", () => {
  test("opens the reference with ? and closes it on Escape", async ({ page, viewerURL }) => {
    await page.goto(`${viewerURL}view/lifecycle.md`);
    await page.locator("h1").click();

    await page.keyboard.press("?");
    const dialog = page.locator(".shortcuts-dialog");
    await expect(dialog).toBeVisible();
    await expect(dialog.locator("h2")).toHaveText("Keyboard shortcuts");
    await expect(dialog.getByText("Toggle documents sidebar")).toBeVisible();
    await expect(dialog.getByText("Toggle review sidebar")).toBeVisible();
    await expect(dialog.getByText("Add a new comment")).toBeVisible();

    await page.keyboard.press("Escape");
    await expect(dialog).toBeHidden();
  });

  test("opens the reference from the top-bar button and restores focus to it on close", async ({ page, viewerURL }) => {
    await page.goto(`${viewerURL}view/lifecycle.md`);
    const openButton = page.getByRole("button", { name: "Keyboard shortcuts" });

    await openButton.click();
    const dialog = page.locator(".shortcuts-dialog");
    await expect(dialog).toBeVisible();

    await page.keyboard.press("Escape");
    await expect(dialog).toBeHidden();
    await expect(openButton).toBeFocused();
  });

  test("completes Space then E by toggling the documents sidebar", async ({ page, viewerURL }) => {
    await page.goto(`${viewerURL}view/lifecycle.md`);
    await page.locator("h1").click();
    const toggle = page.getByRole("button", { name: /documents$/ });
    await expect(page.locator("#documents-sidebar")).toBeVisible();

    await page.keyboard.press("Space");
    await page.keyboard.press("e");
    await expect(page.locator("#documents-sidebar")).toBeHidden();
    await expect(toggle).toHaveAttribute("aria-expanded", "false");
    await expect(page.locator(".shortcut-status")).toHaveText("Documents sidebar hidden.");

    await page.keyboard.press("Space");
    await page.keyboard.press("E");
    await expect(page.locator("#documents-sidebar")).toBeVisible();
    await expect(toggle).toHaveAttribute("aria-expanded", "true");
  });

  test("completes Space then R by toggling the review sidebar, which starts hidden", async ({ page, viewerURL }) => {
    await page.goto(`${viewerURL}view/lifecycle.md`);
    await page.locator("h1").click();
    await expect(page.locator("#annotation-sidebar")).toBeHidden();

    await page.keyboard.press("Space");
    await page.keyboard.press("r");

    await expect(page.locator("#annotation-sidebar")).toBeVisible();
    await expect(page.getByRole("button", { name: "Hide annotations" })).toBeVisible();
    await expect(page.locator(".shortcut-status")).toHaveText("Review sidebar shown.");
  });

  test("completes Space then C by revealing the review sidebar, opening the comment form, and keeping the selection", async ({ page, viewerURL }) => {
    await page.goto(`${viewerURL}view/lifecycle.md`);
    await selectText(page, "selected phrase");
    await expect(page.locator(".selection-preview")).toContainText("selected phrase");
    await expect(page.locator("#annotation-sidebar")).toBeHidden();

    await page.keyboard.press("Space");
    await page.keyboard.press("c");

    await expect(page.locator("#annotation-sidebar")).toBeVisible();
    const form = page.locator(".annotation-form");
    await expect(form).toBeVisible();
    await expect(form.locator('textarea[name="comment"]')).toBeFocused();
    await expect(page.locator(".selection-preview")).toContainText("selected phrase");
  });

  test("suppresses every shortcut while typing in a form field", async ({ page, viewerURL }) => {
    await page.goto(`${viewerURL}view/lifecycle.md`);
    const search = page.getByRole("searchbox", { name: "Find document" });
    await search.click();

    await search.pressSequentially("? e r c");

    await expect(page.locator(".shortcuts-dialog")).toBeHidden();
    await expect(page.locator("#documents-sidebar")).toBeVisible();
    await expect(page.locator("#annotation-sidebar")).toBeHidden();
    await expect(search).toHaveValue("? e r c");
  });

  test("lets the enable/disable preference persist and keeps the top-bar button working while disabled", async ({ page, viewerURL }) => {
    await page.goto(`${viewerURL}view/lifecycle.md`);
    await page.locator("h1").click();
    await page.getByRole("button", { name: "Keyboard shortcuts" }).click();
    const dialog = page.locator(".shortcuts-dialog");
    await expect(dialog).toBeVisible();

    const enabledToggle = page.getByRole("checkbox", { name: "Enable global shortcuts" });
    await expect(enabledToggle).toBeChecked();
    await enabledToggle.uncheck();
    await dialog.getByRole("button", { name: "Close" }).click();
    await expect(dialog).toBeHidden();

    await page.keyboard.press("Space");
    await page.keyboard.press("e");
    await expect(page.locator("#documents-sidebar")).toBeVisible();
    await expect(page.locator(".shortcut-status")).toBeEmpty();

    await page.reload();
    await page.getByRole("button", { name: "Keyboard shortcuts" }).click();
    await expect(page.getByRole("checkbox", { name: "Enable global shortcuts" })).not.toBeChecked();

    await page.keyboard.press("Escape");
    await expect(dialog).toBeHidden();
  });
});
