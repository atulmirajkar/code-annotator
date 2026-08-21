const { writeFile } = require("node:fs/promises");
const path = require("node:path");
const { test, expect } = require("./viewer");

test.describe("viewer navigation", () => {
  test("collapses both sidebars and gives their space to the document", async ({ page, viewerURL }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto(`${viewerURL}view/valid.md`);

    const document = page.locator("main.document");
    // Annotations start collapsed by default; documents start visible.
    await expect(page.locator("#annotation-sidebar")).toBeHidden();
    await expect(page.getByRole("button", { name: "Show annotations" })).toHaveAttribute("aria-expanded", "false");
    const initialWidth = await document.evaluate((element) => element.getBoundingClientRect().width);

    await page.getByRole("button", { name: "Hide documents" }).click();
    await expect(page.locator("#documents-sidebar")).toBeHidden();
    await expect(page.getByRole("button", { name: "Show documents" })).toHaveAttribute("aria-expanded", "false");
    const withoutDocuments = await document.evaluate((element) => element.getBoundingClientRect().width);
    expect(withoutDocuments).toBeGreaterThan(initialWidth);

    await page.getByRole("button", { name: "Show annotations" }).click();
    await expect(page.locator("#annotation-sidebar")).toBeVisible();
    const withAnnotations = await document.evaluate((element) => element.getBoundingClientRect().width);
    expect(withAnnotations).toBeLessThan(withoutDocuments);

    await page.getByRole("button", { name: "Hide annotations" }).click();
    await expect(page.locator("#annotation-sidebar")).toBeHidden();
    const documentOnly = await document.evaluate((element) => element.getBoundingClientRect().width);
    expect(documentOnly).toBeGreaterThan(withAnnotations);
  });

  test("filters paths case-insensitively and opens the first result", async ({ page, viewerURL }) => {
    await page.goto(viewerURL);
    // A configured base with a changed fixture file defaults this on; uncheck
    // it since this test exercises path lookup across the full catalog.
    await page.getByRole("checkbox", { name: "Changed only" }).uncheck();
    const search = page.getByRole("searchbox", { name: "Find document" });
    await search.fill("LIFECYCLE");

    await expect(page.locator(".document-search-status")).toHaveText("1 matching document.");
    await expect(page.locator(".documents li:not([hidden]) a")).toHaveText("lifecycle.md");
    await search.press("Enter");
    await expect(page).toHaveURL(/\/view\/lifecycle\.md$/);
  });

  test("supports lookup focus, empty results, clearing, and result focus", async ({ page, viewerURL }) => {
    await page.goto(viewerURL);
    // A configured base with a changed fixture file defaults this on; uncheck
    // it since this test exercises path lookup across the full catalog. Move
    // focus off the checkbox afterward so the "/" shortcut below still fires.
    await page.getByRole("checkbox", { name: "Changed only" }).uncheck();
    await page.locator("#documents-sidebar h2").click();
    const search = page.getByRole("searchbox", { name: "Find document" });
    const initialDocumentCount = await page.locator(".documents li:not([hidden])").count();
    await page.keyboard.press("/");
    await expect(search).toBeFocused();

    await search.fill("missing-document-name");
    await expect(page.locator(".document-search-status")).toHaveText("No matching documents.");
    await expect(page.locator(".documents li:not([hidden])")).toHaveCount(0);
    await search.press("Escape");
    await expect(search).toHaveValue("");
    await expect(page.locator(".documents li:not([hidden])")).toHaveCount(initialDocumentCount);

    await search.fill("stale");
    await search.press("ArrowDown");
    await expect(page.locator(".documents li:not([hidden]) a")).toBeFocused();
  });

  test("composes Changed only with path lookup", async ({ page, viewerURL }) => {
    await page.goto(viewerURL);
    const changedOnly = page.getByRole("checkbox", { name: "Changed only" });
    await expect(changedOnly).toBeVisible();
    await changedOnly.check();
    await expect(page.locator(".documents li:not([hidden]) a", { hasText: "changed-only.go" })).toBeVisible();
    await expect(page.locator(".documents li:not([hidden]) a", { hasText: "valid.md" })).toHaveCount(0);

    const search = page.getByRole("searchbox", { name: "Find document" });
    await search.fill("missing-change");
    await expect(page.locator(".document-search-status")).toHaveText("No matching changed documents.");
    await expect(page.locator(".documents li:not([hidden])")).toHaveCount(0);

    await search.fill("changed-only");
    await expect(page.locator(".document-search-status")).toHaveText("1 matching changed document.");
    await search.press("Enter");
    await expect(page).toHaveURL(/\/view\/changed-only\.go$/);
    await expect(page.getByRole("checkbox", { name: "Changed only" })).toBeChecked();
    await expect(page.locator(".document-search-status")).toHaveText(/changed document/);
  });

  test("preserves diff mode and collapsed annotations across code navigation", async ({ page, viewerURL }) => {
    await page.goto(`${viewerURL}view/diff-layout.go?mode=diff`);
    await expect(page.getByRole("link", { name: "Changes" })).toHaveAttribute("aria-current", "page");
    // Annotations start collapsed by default.
    await expect(page.locator("#annotation-sidebar")).toBeHidden();
    // Changes view with a configured base defaults the sidebar to changed-only;
    // uncheck it since this test browses code documents regardless of status.
    await page.getByRole("checkbox", { name: "Changed only" }).uncheck();

    const nextCode = page.locator('.documents li[data-kind="code"] a', { hasText: "code-annotation.go" });
    await expect(nextCode).toHaveAttribute("href", "/view/code-annotation.go?mode=diff");
    await nextCode.click();
    await expect(page).toHaveURL(/\/view\/code-annotation\.go\?mode=diff$/);
    await expect(page.getByRole("link", { name: "Changes" })).toHaveAttribute("aria-current", "page");
    await expect(page.locator("#annotation-sidebar")).toBeHidden();
    await expect(page.getByRole("button", { name: "Show annotations" })).toHaveAttribute("aria-expanded", "false");

    await page.getByRole("link", { name: "File" }).click();
    await expect(page).toHaveURL(/\/view\/code-annotation\.go$/);
    const otherCode = page.locator('.documents li[data-kind="code"] a', { hasText: "diff-layout.go" });
    await expect(otherCode).toHaveAttribute("href", "/view/diff-layout.go");
    await otherCode.click();
    await expect(page).toHaveURL(/\/view\/diff-layout\.go$/);
    await expect(page.getByRole("link", { name: "File" })).toHaveAttribute("aria-current", "page");
    await expect(page.locator("#annotation-sidebar")).toBeHidden();
  });

  test("keeps panel controls usable without horizontal page overflow on mobile", async ({ page, viewerURL }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto(`${viewerURL}view/valid.md`);
    await page.getByRole("button", { name: "Hide documents" }).click();
    // Annotations start collapsed by default.
    await expect(page.locator("#annotation-sidebar")).toBeHidden();

    const viewport = await page.evaluate(() => ({ clientWidth: document.documentElement.clientWidth, scrollWidth: document.documentElement.scrollWidth }));
    expect(viewport.scrollWidth).toBe(viewport.clientWidth);
    await expect(page.getByRole("button", { name: "Show documents" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Show annotations" })).toBeVisible();
  });

  test("keeps the File/Changes toolbar visible while scrolling a long file", async ({ page, viewer }) => {
    const documentPath = path.join(viewer.contentRoot, "tall.go");
    const lines = Array.from({ length: 150 }, (_, index) => `// line ${index + 1} of filler content to force page scrolling.`).join("\n");
    await writeFile(documentPath, `package fixture\n\n${lines}\n`);
    await page.goto(`${viewer.url}view/tall.go`);

    const tabs = page.locator(".source-mode-tabs");
    const topbarHeight = await page.locator(".topbar").evaluate((element) => element.getBoundingClientRect().height);
    await expect(tabs).toBeVisible();

    await page.mouse.wheel(0, 3000);
    await expect.poll(() => page.evaluate(() => window.scrollY)).toBeGreaterThan(200);
    // The sticky toolbar pins directly beneath the sticky topbar, so it never
    // scrolls out of view while reviewing a long file.
    const scrolledTop = await tabs.evaluate((element) => element.getBoundingClientRect().top);
    expect(Math.abs(scrolledTop - topbarHeight)).toBeLessThan(2);
  });
});
