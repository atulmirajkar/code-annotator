const { test, expect } = require("./viewer");

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
});
