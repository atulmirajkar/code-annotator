const { test, expect } = require("./viewer");

const changesURL = (viewerURL) => `${viewerURL}view/diff-layout.go?mode=diff`;

// waitForEnhancedSelector waits until the client has replaced the server's
// single fallback option with the full bounded list fetched from the API.
async function waitForEnhancedSelector(page) {
  await expect(page.locator(".revision-selector")).toBeEnabled();
  await expect
    .poll(async () => page.locator(".revision-selector option").count())
    .toBeGreaterThan(1);
}

// reloadAfter runs an action that triggers a full page reload and resolves once
// the marker planted on the previous document is gone.
async function reloadAfter(page, action) {
  await page.evaluate(() => { window.__beforeReload = true; });
  await action();
  await expect
    .poll(async () => {
      try {
        return await page.evaluate(() => window.__beforeReload === undefined);
      } catch (_) {
        // The document is navigating away; keep polling until it settles.
        return false;
      }
    })
    .toBe(true);
}

async function optionList(page) {
  return page.locator(".revision-selector option").evaluateAll((options) =>
    options.map((option) => ({ value: option.value, text: option.textContent, selected: option.selected })));
}

test.describe("revision selector", () => {
  test("lists bounded options labeled by distance from HEAD", async ({ page, viewerURL }) => {
    await page.goto(changesURL(viewerURL));
    await waitForEnhancedSelector(page);

    const options = await optionList(page);
    expect(options.length).toBeGreaterThan(1);
    // The tip is labeled (HEAD) and the previous first-parent commit (HEAD~1).
    expect(options.filter((option) => /\(HEAD\)$/.test(option.text))).toHaveLength(1);
    expect(options.some((option) => /\(HEAD~1\)$/.test(option.text))).toBe(true);
    // The active base starts at the tip commit.
    expect(options.find((option) => option.selected).text).toMatch(/\(HEAD\)$/);
  });

  test("pins a selected commit and can return to the tip", async ({ page, viewerURL }) => {
    await page.goto(changesURL(viewerURL));
    await waitForEnhancedSelector(page);

    const pinned = (await optionList(page)).find((option) => /\(HEAD~1\)$/.test(option.text));
    expect(pinned).toBeTruthy();

    await reloadAfter(page, () => page.locator(".revision-selector").selectOption(pinned.value));
    await waitForEnhancedSelector(page);
    await expect(page.locator(".revision-selector")).toHaveValue(pinned.value);
    await expect(page.locator(".revision-selector option:checked")).toHaveText(/\(HEAD~1\)$/);

    // Selecting the tip restores server-wide state so later shared-server tests
    // still compare against HEAD.
    const tip = (await optionList(page)).find((option) => /\(HEAD\)$/.test(option.text));
    await reloadAfter(page, () => page.locator(".revision-selector").selectOption(tip.value));
    await waitForEnhancedSelector(page);
    await expect(page.locator(".revision-selector option:checked")).toHaveText(/\(HEAD\)$/);
  });

  test("changing the base in one tab is visible to another after reload", async ({ page, viewerURL, context }) => {
    await page.goto(changesURL(viewerURL));
    await waitForEnhancedSelector(page);
    const pinned = (await optionList(page)).find((option) => /\(HEAD~1\)$/.test(option.text));

    await reloadAfter(page, () => page.locator(".revision-selector").selectOption(pinned.value));
    await waitForEnhancedSelector(page);

    // A second tab loads the server-wide base that the first tab just pinned.
    const second = await context.newPage();
    await second.goto(changesURL(viewerURL));
    await waitForEnhancedSelector(second);
    await expect(second.locator(".revision-selector")).toHaveValue(pinned.value);
    await second.close();

    // Restore the tip base for later shared-server tests.
    const tip = (await optionList(page)).find((option) => /\(HEAD\)$/.test(option.text));
    await reloadAfter(page, () => page.locator(".revision-selector").selectOption(tip.value));
    await waitForEnhancedSelector(page);
  });
});
