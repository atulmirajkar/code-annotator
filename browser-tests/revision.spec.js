const { test, expect } = require("./viewer");

const changesURL = (viewerURL) => `${viewerURL}view/diff-layout.go?mode=diff`;

// waitForEnhancedSelector waits until the client has replaced the server's
// single fallback option with the full bounded list fetched from the API.
async function waitForEnhancedSelector(page) {
  await expect(page.locator(".refresh-git-diff")).toBeEnabled();
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
  test("lists bounded options and refreshes without error", async ({ page, viewerURL }) => {
    await page.goto(changesURL(viewerURL));
    await waitForEnhancedSelector(page);

    const options = await optionList(page);
    expect(options.length).toBeGreaterThan(1);
    expect(options.filter((option) => option.text.startsWith("HEAD:"))).toHaveLength(1);
    const active = await page.locator(".revision-selector").inputValue();

    await reloadAfter(page, () => page.locator(".refresh-git-diff").click());
    await waitForEnhancedSelector(page);

    // Refreshing the moving base against a clean worktree keeps HEAD active and
    // never surfaces an error.
    await expect(page.locator(".revision-selector")).toHaveValue(active);
    await expect(page.locator(".diff-comparison-status")).toHaveText("");
    await expect(page.locator(".revision-selector option:checked")).toHaveText(/^HEAD:/);
  });

  test("pins a selected commit and returns to the moving base", async ({ page, viewerURL }) => {
    await page.goto(changesURL(viewerURL));
    await waitForEnhancedSelector(page);

    const options = await optionList(page);
    const pinned = options.find((option) => !option.text.startsWith("HEAD:"));
    expect(pinned).toBeTruthy();

    await reloadAfter(page, () => page.locator(".revision-selector").selectOption(pinned.value));
    await waitForEnhancedSelector(page);
    await expect(page.locator(".revision-selector")).toHaveValue(pinned.value);
    await expect(page.locator(".revision-selector option:checked")).not.toHaveText(/^HEAD:/);

    // Selecting the configured HEAD option restores server-wide moving state so
    // later shared-server tests still compare against HEAD.
    const headValue = options.find((option) => option.text.startsWith("HEAD:")).value;
    await reloadAfter(page, () => page.locator(".revision-selector").selectOption(headValue));
    await waitForEnhancedSelector(page);
    await expect(page.locator(".revision-selector option:checked")).toHaveText(/^HEAD:/);
  });

  test("reports a conflict when another tab already changed the base", async ({ page, viewerURL, context }) => {
    await page.goto(changesURL(viewerURL));
    await waitForEnhancedSelector(page);

    const second = await context.newPage();
    await second.goto(changesURL(viewerURL));
    await waitForEnhancedSelector(second);

    // The second tab refreshes first, advancing the server state revision.
    await reloadAfter(second, () => second.locator(".refresh-git-diff").click());
    await waitForEnhancedSelector(second);

    // The first tab still holds the stale revision; its refresh must be rejected
    // in place with a conflict message rather than reloading.
    await page.evaluate(() => { window.__beforeReload = true; });
    await page.locator(".refresh-git-diff").click();
    await expect(page.locator(".diff-comparison-status.error")).toContainText("another tab");
    expect(await page.evaluate(() => window.__beforeReload === true)).toBe(true);

    await second.close();
  });
});
