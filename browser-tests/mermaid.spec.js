const { test, expect, openAnnotations } = require("./viewer");

function captureBrowserSignals(page) {
  const consoleErrors = [];
  const externalRequests = [];
  page.on("console", (message) => {
    if (message.type() === "error") consoleErrors.push(message.text());
  });
  page.on("request", (request) => {
    const url = new URL(request.url());
    if (url.hostname !== "127.0.0.1") externalRequests.push(request.url());
  });
  return { consoleErrors, externalRequests };
}

async function openNewAnnotationForm(page) {
  const form = page.locator(".annotation-form");
  if (!(await form.isVisible())) {
    await page.locator(".add-annotation-toggle").click();
  }
  await expect(form).toBeVisible();
}

test.describe("Mermaid rendering", () => {
  test("renders after HTMX navigation from a document without diagrams", async ({ page, viewerURL }) => {
    const signals = captureBrowserSignals(page);
    await page.goto(`${viewerURL}view/lifecycle.md`);
    await page.getByRole("checkbox", { name: "Changed only" }).uncheck();
    await expect(page.locator(".mermaid-output")).toHaveCount(0);

    await page.getByRole("link", { name: "valid.md" }).click();
    await expect(page).toHaveURL(/\/view\/valid\.md$/);
    await expect(page.locator(".mermaid-output svg")).toBeVisible();
    expect(signals.consoleErrors.filter((message) => message.includes("Content Security Policy"))).toEqual([]);

    await page.getByRole("link", { name: "lifecycle.md" }).click();
    await expect(page.locator(".mermaid-output")).toHaveCount(0);
  });

  for (const colorScheme of ["light", "dark"]) {
    test(`renders a sequence diagram in ${colorScheme} mode without CSP or network errors`, async ({ page, viewerURL }) => {
      const signals = captureBrowserSignals(page);
      await page.emulateMedia({ colorScheme });
      await page.goto(`${viewerURL}view/valid.md`);

      const svg = page.locator(".mermaid-output svg");
      await expect(svg).toBeVisible();
      await expect(page.locator(".mermaid-error")).toBeHidden();
      const bodyColorScheme = await page.locator("body").evaluate((body) => getComputedStyle(body).colorScheme);
      expect(bodyColorScheme).toContain(colorScheme);
      expect(signals.consoleErrors.filter((message) => message.includes("Content Security Policy"))).toEqual([]);
      expect(signals.externalRequests).toEqual([]);
    });
  }

  test("keeps malformed source visible with a useful error", async ({ page, viewerURL }) => {
    await page.goto(`${viewerURL}view/malformed.md`);

    await expect(page.locator(".mermaid-error")).toBeVisible();
    await expect(page.locator(".mermaid-error")).toContainText("Could not render diagram");
    await expect(page.locator(".mermaid-source")).toHaveAttribute("open", "");
    await expect(page.locator(".mermaid-source code")).toContainText("sequenceDiagram");
  });

  test("selects the complete diagram by mouse and survives delayed selection changes", async ({ page, viewerURL }) => {
    await page.goto(`${viewerURL}view/valid.md`);
    await openAnnotations(page);
    await page.locator(".mermaid-output svg text").first().click();
    await page.waitForTimeout(500);
    await page.evaluate(() => document.dispatchEvent(new Event("selectionchange")));

    await expect(page.locator(".selection-preview")).toBeVisible();
    await expect(page.locator(".selection-preview")).toContainText("complete diagram");
    await expect(page.locator(".selection-quote")).toContainText("sequenceDiagram");
    await expect(page.locator(".mermaid-diagram")).toHaveClass(/annotation-selection/);
  });

  test("selects the complete diagram from the keyboard", async ({ page, viewerURL }) => {
    await page.goto(`${viewerURL}view/valid.md`);
    await openAnnotations(page);
    const output = page.locator(".mermaid-output");
    await output.focus();
    await output.press("Enter");

    await expect(page.locator(".selection-preview")).toBeVisible();
    await expect(page.locator(".selection-preview")).toContainText("complete diagram");
  });

  test("creates and highlights a whole-diagram annotation", async ({ page, viewerURL }) => {
    await page.goto(`${viewerURL}view/valid.md`);
    await openAnnotations(page);
    await page.locator(".mermaid-output svg text").first().click();
    await openNewAnnotationForm(page);
    await page.locator('.annotation-form textarea[name="comment"]').fill("Update this interaction.");
    await page.locator('.annotation-form button[type="submit"]').click();

    await expect(page.locator(".annotation-form-status")).toHaveText("Annotation added.");
    await expect(page.locator(".annotation-card")).toHaveCount(1);
    await expect(page.locator(".mermaid-diagram")).toHaveClass(/annotation-highlight-region/);
    await page.locator(".annotation-card .annotation-summary").click();
    await expect(page.locator(".annotation-source")).toContainText("sequenceDiagram");
  });

  test("keeps fenced text diagrams monospace and horizontally scrollable", async ({ page, viewerURL }) => {
    await page.goto(`${viewerURL}view/valid.md`);
    const code = page.locator("pre code.language-text");

    const styles = await code.evaluate((element) => {
      const computed = getComputedStyle(element);
      return {
        fontFamily: computed.fontFamily,
        ligatures: computed.fontVariantLigatures,
        whiteSpace: computed.whiteSpace,
      };
    });
    expect(styles.fontFamily).toContain("monospace");
    expect(styles.ligatures).toBe("none");
    expect(styles.whiteSpace).toBe("pre");
  });
});
