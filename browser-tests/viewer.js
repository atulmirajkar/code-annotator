const { test: base, expect } = require("@playwright/test");
const { execFile, spawn } = require("node:child_process");
const { cp, mkdtemp, rm, writeFile } = require("node:fs/promises");
const { tmpdir } = require("node:os");
const path = require("node:path");
const { promisify } = require("node:util");

const runFile = promisify(execFile);

// viewerURL owns one isolated review server per Playwright worker. Content is
// read from immutable fixtures while annotation writes go to a temporary root.
const test = base.extend({
  viewer: [async ({}, use) => {
    const annotationRoot = await mkdtemp(path.join(tmpdir(), "code-annotator-browser-annotations-"));
    const contentRoot = await mkdtemp(path.join(tmpdir(), "code-annotator-browser-content-"));
    await cp(path.join(__dirname, "fixtures"), contentRoot, { recursive: true });
    await initializeGitFixture(contentRoot);
    const server = spawn("go", [
      "run", "./cmd/code-annotator",
      "-review",
      "-include-code",
	  "-diff-base", "HEAD",
      "-no-open",
      "-port", "0",
      "-annotations-dir", annotationRoot,
      contentRoot,
    ], {
      cwd: path.join(__dirname, ".."),
      stdio: ["ignore", "pipe", "pipe"],
    });

    let diagnostics = "";
    server.stderr.on("data", (chunk) => { diagnostics += chunk.toString(); });
    try {
      const viewerURL = await waitForViewerURL(server, () => diagnostics);
      await use({ url: viewerURL, contentRoot, annotationRoot });
    } finally {
      server.kill("SIGINT");
      await Promise.race([
        new Promise((resolve) => server.once("exit", resolve)),
        new Promise((resolve) => setTimeout(resolve, 3000)),
      ]);
      if (server.exitCode === null) server.kill("SIGKILL");
      await rm(annotationRoot, { recursive: true, force: true });
      await rm(contentRoot, { recursive: true, force: true });
    }
  }, { scope: "worker" }],
  viewerURL: [async ({ viewer }, use) => use(viewer.url), { scope: "worker" }],
});

// initializeGitFixture freezes all ordinary fixtures at HEAD, then adds one
// untracked source document for deterministic Changed-only browser coverage. A
// second non-catalog commit gives the revision selector more than one bounded
// option without altering any reviewable file's committed content.
async function initializeGitFixture(contentRoot) {
  await writeFile(
    path.join(contentRoot, "diff-overview.go"),
    diffOverviewFixture(false),
  );
  await runFile("git", ["-C", contentRoot, "init", "-b", "main"]);
  await runFile("git", ["-C", contentRoot, "add", "."]);
  await runFile("git", ["-C", contentRoot, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "fixtures"]);
  await writeFile(path.join(contentRoot, "git-history.txt"), "baseline\n");
  await runFile("git", ["-C", contentRoot, "add", "git-history.txt"]);
  await runFile("git", ["-C", contentRoot, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "add history marker"]);
  await writeFile(path.join(contentRoot, "changed-only.go"), "package changed\n");
  await writeFile(path.join(contentRoot, "diff-layout.go"), `package fixture

const layoutMessage = "This current-side replacement is also deliberately long so its highlight stays inside the independently scrollable current pane."

var afterSelection = true
`);
  await writeFile(path.join(contentRoot, "diff-layout.md"), `# Diff Layout Fixture

This paragraph describes the current line after edits were applied.
`);
  await writeFile(
    path.join(contentRoot, "diff-overview.go"),
    diffOverviewFixture(true),
  );
}

// Five separated edits across a 210-row source file give the overview ruler a
// stable long document, every marker kind, and a final hunk close to EOF.
function diffOverviewFixture(changed) {
  const rows = [];
  for (let row = 1; row <= 210; row += 1) {
    if (changed && row === 80) continue;
    if (changed && row === 120) {
      rows.push('    "overview row 119 added",');
    }
    const suffix =
      changed && (row === 40 || row === 160 || row === 205)
        ? " changed"
        : "";
    rows.push(
      `    "overview row ${String(row).padStart(3, "0")}${suffix} keeps a deliberately long stable value for independent horizontal diff scrolling",`,
    );
  }
  return `package fixture

var overviewRows = []string{
${rows.join("\n")}
}
`;
}

// waitForViewerURL resolves startup output while retaining stderr for useful
// failures when the Go process exits before binding its loopback listener.
function waitForViewerURL(server, readDiagnostics) {
  return new Promise((resolve, reject) => {
    let stdout = "";
    const timeout = setTimeout(() => reject(new Error(`viewer startup timed out: ${readDiagnostics()}`)), 15000);
    server.stdout.on("data", (chunk) => {
      stdout += chunk.toString();
      const match = stdout.match(/Serving .* at (http:\/\/127\.0\.0\.1:\d+\/)/);
      if (match) {
        clearTimeout(timeout);
        resolve(match[1]);
      }
    });
    server.once("exit", (code) => {
      clearTimeout(timeout);
      reject(new Error(`viewer exited with ${code}: ${readDiagnostics()}`));
    });
  });
}

// openAnnotations reveals the annotation sidebar. It starts collapsed by
// default, so tests that exercise it open it once after navigating.
async function openAnnotations(page) {
  await page.getByRole("button", { name: "Show annotations" }).click();
}

module.exports = { test, expect, openAnnotations };
