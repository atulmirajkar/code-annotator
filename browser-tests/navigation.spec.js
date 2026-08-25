const { mkdir, writeFile } = require("node:fs/promises");
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
    await expect(page.locator(".document-search-status")).toBeHidden();
    await page.locator("#documents-sidebar h2").click();
    const search = page.getByRole("searchbox", { name: "Find document" });
    const initialDocumentCount = await page.locator(".documents .document-file:not([hidden])").count();
    await page.keyboard.press("/");
    await expect(search).toBeFocused();

    await search.fill("missing-document-name");
    await expect(page.locator(".document-search-status")).toHaveText("No matching documents.");
    await expect(page.locator(".documents .document-file:not([hidden])")).toHaveCount(0);
    await search.press("Escape");
    await expect(search).toHaveValue("");
    await expect(page.locator(".documents .document-file:not([hidden])")).toHaveCount(initialDocumentCount);

    await search.fill("stale");
    await expect(page.locator(".document-search-status")).toHaveText("1 matching document.");
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
    await expect(page.locator(".documents .document-file:not([hidden])")).toHaveCount(0);

    await search.fill("changed-only");
    await expect(page.locator(".document-search-status")).toHaveText("1 matching changed document.");
    await search.press("Enter");
    await expect(page).toHaveURL(/\/view\/changed-only\.go$/);
    await expect(page.getByRole("checkbox", { name: "Changed only" })).toBeChecked();
    await expect(page.locator(".document-search-status")).toHaveText(/changed document/);
  });

  test("renders a file tree and switches exclusively between changed and open-comment scopes", async ({ page, viewer, viewerURL }) => {
    await mkdir(path.join(viewer.contentRoot, "nested"));
    await writeFile(path.join(viewer.contentRoot, "nested", "reviewed.md"), "# Reviewed nested document\n");
    await writeFile(path.join(viewer.contentRoot, "nested", "other.md"), "# Other nested document\n");
    await page.goto(viewerURL);

    await expect(page.locator(".documents > li").first()).toHaveClass(/document-directory/);

    const token = await page.locator('meta[name="code-annotator-review-token"]').getAttribute("content");
    const current = await page.request.get(`${viewerURL}api/annotations?document=nested%2Freviewed.md`);
    const currentPayload = await current.json();
    const created = await page.request.post(`${viewerURL}api/annotations`, {
      headers: {
        "Content-Type": "application/json",
        "If-Match": JSON.stringify(currentPayload.revision),
        "Origin": new URL(viewerURL).origin,
        "X-Code-Annotator-Token": token,
      },
      data: {
        document: "nested/reviewed.md",
        intent: "question",
        comment: "Review this nested document.",
        role: "reviewer",
      },
    });
    expect(created.ok()).toBe(true);
    await page.reload();

    await expect(page.locator(".document-directory-toggle", { hasText: "nested" })).toBeVisible();
    await expect(page.locator(".document-directory-toggle", { hasText: "nested" })).toHaveAttribute("aria-expanded", "true");
    const openComments = page.getByRole("checkbox", { name: "Open comments" });
    const changedOnly = page.getByRole("checkbox", { name: "Changed only" });
    await expect(page.locator(".document-open-total")).toHaveText(/^\d+ documents?$/);
    await openComments.check();
    await expect(changedOnly).not.toBeChecked();
    await expect(page.locator(".document-file", { has: page.getByRole("link", { name: /reviewed\.md/ }) })).toBeVisible();
    await expect(page.getByRole("link", { name: /other\.md/ })).toHaveCount(0);
    await expect(page.getByRole("link", { name: /reviewed\.md/ }).locator(".document-open-count")).toHaveText("1");

    await changedOnly.check();
    await expect(openComments).not.toBeChecked();
    await expect(changedOnly).toBeChecked();
  });

  test("preserves collapsed directories while newly revealed directories default expanded", async ({ page, viewer }) => {
    const changedDirectory = path.join(viewer.contentRoot, "expansion-changed");
    await mkdir(changedDirectory, { recursive: true });
    await writeFile(path.join(changedDirectory, "changed.go"), "package changed\n");
    await page.goto(viewer.url);

    const changedOnly = page.getByRole("checkbox", { name: "Changed only" });
    await expect(changedOnly).toBeChecked();
    const changedToggle = page.locator(".document-directory-toggle", { hasText: "expansion-changed" });
    await expect(changedToggle).toHaveAttribute("aria-expanded", "true");
    await changedToggle.click();
    await expect(changedToggle).toHaveAttribute("aria-expanded", "false");

    await page.reload();
    await expect(changedOnly).toBeChecked();
    await expect(changedToggle).toHaveAttribute("aria-expanded", "false");

    const newDirectory = path.join(viewer.contentRoot, "expansion-new");
    await mkdir(newDirectory, { recursive: true });
    await writeFile(path.join(newDirectory, "new.go"), "package new\n");
    await changedOnly.uncheck();

    await expect(changedToggle).toHaveAttribute("aria-expanded", "false");
    await expect(
      page.locator(".document-directory-toggle", { hasText: "expansion-new" }),
    ).toHaveAttribute("aria-expanded", "true");
  });

  test("preserves diff mode and collapsed annotations across code navigation", async ({ page, viewerURL }) => {
    await page.goto(`${viewerURL}view/diff-layout.go?mode=diff`);
    await expect(page.getByRole("link", { name: "Changes" })).toHaveAttribute("aria-current", "page");
    // Annotations start collapsed by default.
    await expect(page.locator("#annotation-sidebar")).toBeHidden();
    // Changes view with a configured base defaults the sidebar to changed-only;
    // uncheck it since this test browses code documents regardless of status.
    await page.getByRole("checkbox", { name: "Changed only" }).uncheck();

    const nextCode = page.locator(".documents .document-file a", { hasText: "code-annotation.go" });
    await expect(nextCode).toHaveAttribute("href", "/view/code-annotation.go?mode=diff");
    await nextCode.click();
    await expect(page).toHaveURL(/\/view\/code-annotation\.go\?mode=diff$/);
    await expect(page.getByRole("link", { name: "Changes" })).toHaveAttribute("aria-current", "page");
    await expect(page.locator("#annotation-sidebar")).toBeHidden();
    await expect(page.getByRole("button", { name: "Show annotations" })).toHaveAttribute("aria-expanded", "false");

    await page.getByRole("link", { name: "File" }).click();
    await expect(page).toHaveURL(/\/view\/code-annotation\.go$/);
    const otherCode = page.locator(".documents .document-file a", { hasText: "diff-layout.go" });
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
    const documentPane = page.locator("main.document");
    const topbar = page.locator(".topbar");
    await expect(tabs).toBeVisible();
    await expect(topbar.evaluate((element) => getComputedStyle(element).backgroundColor)).resolves.not.toContain("/");

    await documentPane.evaluate((element) => element.scrollTo(0, 3000));
    await expect.poll(() => documentPane.evaluate((element) => element.scrollTop)).toBeGreaterThan(200);
    // The sticky toolbar stays pinned at the top of the document pane while
    // reviewing a long file.
    const scrolledTop = await tabs.evaluate((element) => element.getBoundingClientRect().top);
    const documentTop = await documentPane.evaluate((element) => element.getBoundingClientRect().top);
    expect(scrolledTop).toBeGreaterThanOrEqual(documentTop);
    expect(scrolledTop).toBeLessThan(documentTop + 20);
  });

  test("keeps the topbar and source tabs visible while scrolling at tablet width", async ({ page, viewer }) => {
    const documentPath = path.join(viewer.contentRoot, "tablet-tall.go");
    const lines = Array.from({ length: 150 }, (_, index) => `// line ${index + 1} of filler content to force page scrolling.`).join("\n");
    await writeFile(documentPath, `package fixture\n\n${lines}\n`);
    await page.setViewportSize({ width: 1000, height: 800 });
    await page.goto(`${viewer.url}view/tablet-tall.go`);

    const topbar = page.locator(".topbar");
    const tabs = page.locator(".source-mode-tabs");
    const topbarHeight = await topbar.evaluate((element) => element.getBoundingClientRect().height);
    await page.evaluate(() => window.scrollTo(0, 3000));
    await expect.poll(() => page.evaluate(() => window.scrollY)).toBeGreaterThan(200);
    const geometry = await page.evaluate(() => ({
      topbarTop: document.querySelector(".topbar").getBoundingClientRect().top,
      tabsTop: document.querySelector(".source-mode-tabs").getBoundingClientRect().top,
    }));
    expect(geometry.topbarTop).toBeGreaterThanOrEqual(0);
    expect(geometry.tabsTop).toBeGreaterThanOrEqual(topbarHeight - 1);
  });

  test("keeps the topbar and source tabs visible with long content on mobile", async ({ page, viewer }) => {
    const documentPath = path.join(viewer.contentRoot, "mobile-tall.go");
    const lines = Array.from({ length: 200 }, (_, index) => `// mobile filler line ${index + 1} with enough text to force scrolling.`).join("\n");
    await writeFile(documentPath, `package fixture\n\n${lines}\n`);
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto(`${viewer.url}view/mobile-tall.go`);

    await page.evaluate(() => window.scrollTo(0, 3000));
    await expect.poll(() => page.evaluate(() => window.scrollY)).toBeGreaterThan(200);
    const geometry = await page.evaluate(() => {
      const topbar = document.querySelector(".topbar");
      const tabs = document.querySelector(".source-mode-tabs");
      return {
        topbarTop: topbar.getBoundingClientRect().top,
        topbarBottom: topbar.getBoundingClientRect().bottom,
        tabsTop: tabs.getBoundingClientRect().top,
      };
    });
    expect(geometry.topbarTop).toBeGreaterThanOrEqual(0);
    expect(geometry.tabsTop).toBeGreaterThanOrEqual(geometry.topbarBottom - 1);
  });

  test("keeps source tabs above long Changes content", async ({ page, viewer }) => {
    const documentPath = path.join(viewer.contentRoot, "diff-layout.go");
    const lines = Array.from({ length: 200 }, (_, index) => `// diff filler line ${index + 1} with enough text to force scrolling.`).join("\n");
    await writeFile(documentPath, `package fixture\n\n${lines}\n`);
    await page.setViewportSize({ width: 1280, height: 800 });
    await page.goto(`${viewer.url}view/diff-layout.go?mode=diff`);

    const documentPane = page.locator("main.document");
    await documentPane.evaluate((element) => element.scrollTo(0, 3000));
    await expect.poll(() => documentPane.evaluate((element) => element.scrollTop)).toBeGreaterThan(200);

    const geometry = await page.evaluate(() => {
      const tabs = document.querySelector(".source-mode-tabs");
      const documentPane = document.querySelector("main.document");
      const point = tabs ? document.elementFromPoint(tabs.getBoundingClientRect().right - 20, tabs.getBoundingClientRect().top + 2) : null;
      return {
        tabsTop: tabs?.getBoundingClientRect().top ?? -1,
        documentTop: documentPane?.getBoundingClientRect().top ?? -1,
        documentPaddingTop: Number.parseFloat(getComputedStyle(documentPane).paddingTop),
        pointClass: point?.className ?? "",
      };
    });
    expect(geometry.documentPaddingTop).toBe(0);
    expect(geometry.tabsTop).toBeGreaterThanOrEqual(geometry.documentTop);
    expect(geometry.tabsTop).toBeLessThan(geometry.documentTop + 20);
    expect(geometry.pointClass).toContain("source-mode-tabs");
  });

});
