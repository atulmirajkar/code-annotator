const { test, expect } = require("./viewer");

const overviewPath = "view/diff-overview.go?mode=diff";

test.describe("diff overview ruler", () => {
  test.beforeEach(async ({ page }) => {
    await page.setViewportSize({ width: 1400, height: 720 });
    await page.emulateMedia({ reducedMotion: "reduce" });
  });

  test("maps every long-file hunk into an ordered sticky track", async ({
    page,
    viewerURL,
  }) => {
    await page.goto(`${viewerURL}${overviewPath}`);

    const ruler = page.locator(".diff-overview");
    const items = ruler.locator(".diff-overview-item");
    const markers = ruler.locator(".diff-overview-marker");
    await expect(ruler).toHaveClass(/diff-overview-enhanced/);
    await expect(markers).toHaveCount(5);
    await expect(markers.nth(0)).toHaveAttribute("aria-current", "location");
    await expect(markers.nth(0)).toHaveClass(/diff-overview-next/);

    const kinds = await markers.evaluateAll((links) =>
      links.map((link) =>
        ["modified", "deleted", "added"].find((kind) =>
          link.classList.contains(`diff-overview-${kind}`),
        ),
      ),
    );
    expect(kinds).toEqual([
      "modified",
      "deleted",
      "added",
      "modified",
      "modified",
    ]);
    await expect(markers.nth(4)).toHaveAttribute(
      "aria-label",
      /^Change 5 of 5, modified near current line /,
    );

    const rulerBox = await requiredBox(ruler);
    const itemBoxes = await Promise.all(
      Array.from({ length: 5 }, (_, index) => requiredBox(items.nth(index))),
    );
    for (let index = 1; index < itemBoxes.length; index += 1) {
      expect(itemBoxes[index].y).toBeGreaterThan(itemBoxes[index - 1].y);
    }
    expect((itemBoxes[0].y - rulerBox.y) / rulerBox.height).toBeGreaterThan(
      0.1,
    );
    expect((itemBoxes[4].y - rulerBox.y) / rulerBox.height).toBeGreaterThan(
      0.85,
    );
    expect(itemBoxes[4].y + itemBoxes[4].height).toBeLessThanOrEqual(
      rulerBox.y + rulerBox.height + 1,
    );

    const rulerX = rulerBox.x;
    const currentPane = page.locator(".diff-current-pane");
    await currentPane.evaluate((pane) => {
      pane.scrollLeft = 240;
    });
    await expect
      .poll(() => currentPane.evaluate((pane) => pane.scrollLeft))
      .toBeGreaterThan(0);
    expect((await requiredBox(ruler)).x).toBeCloseTo(rulerX, 0);
  });

  test("moves the viewport and centers pointer and keyboard destinations", async ({
    page,
    viewerURL,
  }) => {
    await page.goto(`${viewerURL}${overviewPath}`);

    const owner = page.locator(".document");
    const ruler = page.locator(".diff-overview");
    const viewport = ruler.locator(".diff-overview-viewport");
    const markers = ruler.locator(".diff-overview-marker");
    await expect(ruler).toHaveClass(/diff-overview-enhanced/);
    const initialViewportTop = await viewport.evaluate((element) =>
      Number.parseFloat(getComputedStyle(element).top),
    );

    await markers.nth(3).click();
    await expect
      .poll(() => owner.evaluate((element) => element.scrollTop))
      .toBeGreaterThan(1000);
    await expect(markers.nth(3)).toHaveAttribute("aria-current", "location");
    await expect
      .poll(() => markerTargetCenterOffset(markers.nth(3), owner))
      .toBeLessThan(3);
    await expect
      .poll(() =>
        viewport.evaluate((element) =>
          Number.parseFloat(getComputedStyle(element).top),
        ),
      )
      .toBeGreaterThan(initialViewportTop);
    expect(
      await markers.nth(3).evaluate((link) => document.activeElement === link),
    ).toBe(true);

    await markers.nth(1).focus();
    await page.keyboard.press("Enter");
    await expect
      .poll(() => markerTargetCenterOffset(markers.nth(1), owner))
      .toBeLessThan(3);
    expect(
      await markers.nth(1).evaluate((link) => document.activeElement === link),
    ).toBe(true);

    const ownerBox = await requiredBox(owner);
    const rulerBox = await requiredBox(ruler);
    const panesBox = await requiredBox(page.locator(".diff-panes"));
    expect(rulerBox.y).toBeGreaterThanOrEqual(ownerBox.y);
    expect(rulerBox.y + rulerBox.height).toBeLessThanOrEqual(
      Math.min(ownerBox.y + ownerBox.height, panesBox.y + panesBox.height) + 1,
    );
  });

  test("uses page scrolling without narrow-layout overflow", async ({
    page,
    viewerURL,
  }) => {
    await page.setViewportSize({ width: 700, height: 700 });
    await page.goto(`${viewerURL}${overviewPath}`);

    const ruler = page.locator(".diff-overview");
    const marker = ruler.locator(".diff-overview-marker").nth(2);
    await expect(ruler).toHaveClass(/diff-overview-enhanced/);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);

    await marker.click();
    await expect
      .poll(() => page.evaluate(() => window.scrollY))
      .toBeGreaterThan(500);
    expect(
      await page.locator(".document").evaluate((element) => element.scrollTop),
    ).toBe(0);
    await expect.poll(() => windowTargetCenterOffset(marker)).toBeLessThan(3);
    const rulerBox = await requiredBox(ruler);
    const viewBox = await requiredBox(page.locator(".diff-view"));
    expect(rulerBox.x + rulerBox.width).toBeLessThanOrEqual(
      viewBox.x + viewBox.width + 1,
    );
  });

  for (const colorScheme of ["light", "dark"]) {
    test(`keeps marker kinds and focus distinguishable in ${colorScheme} mode`, async ({
      page,
      viewerURL,
    }) => {
      await page.emulateMedia({ colorScheme, reducedMotion: "reduce" });
      await page.goto(`${viewerURL}${overviewPath}`);

      const markers = page.locator(".diff-overview-marker");
      await expect(page.locator(".diff-overview")).toHaveClass(
        /diff-overview-enhanced/,
      );
      const colors = await markers.evaluateAll((links) =>
        links.slice(0, 3).map((link) => {
          const style = getComputedStyle(link, "::before");
          return {
            color: style.backgroundColor,
            image: style.backgroundImage,
          };
        }),
      );
      expect(colors[1].color).not.toBe(colors[2].color);
      expect(colors[0].image).toContain("linear-gradient");

      await markers.nth(2).focus();
      const focus = await markers.nth(2).evaluate((link) => {
        const style = getComputedStyle(link);
        return { style: style.outlineStyle, width: style.outlineWidth };
      });
      expect(focus.style).toBe("solid");
      expect(Number.parseFloat(focus.width)).toBeGreaterThan(0);
    });
  }

  test("retains every link when hunks collapse into density slots", async ({
    page,
    viewerURL,
  }) => {
    await page.goto(`${viewerURL}${overviewPath}`);
    await installSyntheticDensityDiff(page, 800);

    const ruler = page.locator(".diff-overview");
    const markers = ruler.locator(".diff-overview-marker");
    await expect(ruler).toHaveClass(/diff-overview-enhanced/);
    await expect(markers).toHaveCount(800);
    expect(
      await markers.evaluateAll((links) =>
        links.every(
          (link, index) =>
            link.getAttribute("href") === `#density-change-${index + 1}`,
        ),
      ),
    ).toBe(true);

    const densityItems = ruler.locator(".diff-overview-density");
    expect(await densityItems.count()).toBeGreaterThan(0);
    await expect(densityItems.first()).toHaveAttribute(
      "title",
      /^[2-9][0-9]* changes in this area$/,
    );
    const densityMarker = densityItems.first().locator(".diff-overview-marker");
    await densityMarker.focus();
    expect(
      await densityItems
        .first()
        .evaluate((item) => getComputedStyle(item).transform),
    ).not.toBe("none");
  });
});

async function requiredBox(locator) {
  const box = await locator.boundingBox();
  if (!box) throw new Error("expected element bounding box");
  return box;
}

async function markerTargetCenterOffset(marker, owner) {
  return marker.evaluate(
    (link, ownerElement) => {
      const target = document.querySelector(link.getAttribute("href"));
      if (!target) throw new Error("marker target not found");
      const targetBox = target.getBoundingClientRect();
      const ownerBox = ownerElement.getBoundingClientRect();
      return Math.abs(
        targetBox.top +
          targetBox.height / 2 -
          (ownerBox.top + ownerBox.height / 2),
      );
    },
    await owner.elementHandle(),
  );
}

async function windowTargetCenterOffset(marker) {
  return marker.evaluate((link) => {
    const target = document.querySelector(link.getAttribute("href"));
    if (!target) throw new Error("marker target not found");
    const targetBox = target.getBoundingClientRect();
    return Math.abs(
      targetBox.top + targetBox.height / 2 - window.innerHeight / 2,
    );
  });
}

async function installSyntheticDensityDiff(page, hunkCount) {
  await page.locator(".document").evaluate((documentPane, count) => {
    const targets = Array.from({ length: count }, (_, index) => {
      return `<div id="density-change-${index + 1}"></div>`;
    }).join("");
    const items = Array.from({ length: count }, (_, index) => {
      const ordinal = index + 1;
      return `<span class="diff-overview-item"><a class="diff-overview-marker diff-overview-added" href="#density-change-${ordinal}" aria-label="Change ${ordinal} of ${count}, added"></a><a class="diff-overview-end" href="#density-change-${ordinal}" tabindex="-1" aria-hidden="true"></a></span>`;
    }).join("");
    documentPane.innerHTML = `<nav class="source-mode-tabs"></nav><div class="diff-view"><div class="diff-column-headings" aria-hidden="true"><span>Base</span><span></span><span>Current</span></div><div class="diff-panes"><div class="diff-pane diff-base-pane"></div><div class="diff-divider"></div><div class="diff-pane diff-current-pane">${targets}</div><nav class="diff-overview" aria-label="Changes in this file"><span class="diff-overview-viewport" aria-hidden="true"></span>${items}</nav></div></div>`;
    const currentPane = documentPane.querySelector(".diff-current-pane");
    if (!currentPane) throw new Error("synthetic current pane not found");
    currentPane.style.position = "relative";
    currentPane.style.height = `${count * 2}px`;
    for (let index = 0; index < count; index += 1) {
      const target = documentPane.querySelector(`#density-change-${index + 1}`);
      if (!target) throw new Error(`synthetic target ${index + 1} not found`);
      target.style.position = "absolute";
      target.style.top = `${index * 2}px`;
      target.style.height = "1px";
    }
  }, hunkCount);
  await page.evaluate(async () => {
    const module = await import("/static/diff-overview.js");
    module.bindDiffOverview({
      document,
      window,
      resizeObserver: ResizeObserver,
      requestAnimationFrame: (callback) =>
        window.requestAnimationFrame(callback),
    });
  });
}
