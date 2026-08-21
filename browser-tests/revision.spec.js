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
  test("lists bounded options with abbreviated commit and subject", async ({ page, viewerURL }) => {
    await page.goto(changesURL(viewerURL));
    await waitForEnhancedSelector(page);

    const options = await optionList(page);
    expect(options.length).toBeGreaterThan(1);
    // The active base starts as the tip commit shown on the diff toolbar.
    const fullCommit = await page.locator('meta[name="code-annotator-diff-commit"]').getAttribute("content");
    const active = options.find((option) => option.selected);
    expect(active.value).toBe(fullCommit);
    expect(active.text).toContain(fullCommit.slice(0, 12));
  });

  test("pins a selected commit and can return to the original base", async ({ page, viewerURL }) => {
    await page.goto(changesURL(viewerURL));
    await waitForEnhancedSelector(page);

    const initial = (await optionList(page)).find((option) => option.selected);
    const other = (await optionList(page)).find((option) => !option.selected);
    expect(other).toBeTruthy();

    await reloadAfter(page, () => page.locator(".revision-selector").selectOption(other.value));
    await waitForEnhancedSelector(page);
    await expect(page.locator(".revision-selector")).toHaveValue(other.value);

    // Selecting the original base restores server-wide state so later
    // shared-server tests still compare against the same commit.
    await reloadAfter(page, () => page.locator(".revision-selector").selectOption(initial.value));
    await waitForEnhancedSelector(page);
    await expect(page.locator(".revision-selector")).toHaveValue(initial.value);
  });

  test("changing the base in one tab is visible to another after reload", async ({ page, viewerURL, context }) => {
    await page.goto(changesURL(viewerURL));
    await waitForEnhancedSelector(page);
    const initial = (await optionList(page)).find((option) => option.selected);
    const other = (await optionList(page)).find((option) => !option.selected);

    await reloadAfter(page, () => page.locator(".revision-selector").selectOption(other.value));
    await waitForEnhancedSelector(page);

    // A second tab loads the server-wide base that the first tab just pinned.
    const second = await context.newPage();
    await second.goto(changesURL(viewerURL));
    await waitForEnhancedSelector(second);
    await expect(second.locator(".revision-selector")).toHaveValue(other.value);
    await second.close();

    // Restore the original base for later shared-server tests.
    await reloadAfter(page, () => page.locator(".revision-selector").selectOption(initial.value));
    await waitForEnhancedSelector(page);
  });
});
