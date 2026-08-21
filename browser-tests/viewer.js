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
    const annotationRoot = await mkdtemp(path.join(tmpdir(), "md-viewer-browser-annotations-"));
    const contentRoot = await mkdtemp(path.join(tmpdir(), "md-viewer-browser-content-"));
    await cp(path.join(__dirname, "fixtures"), contentRoot, { recursive: true });
    await initializeGitFixture(contentRoot);
    const server = spawn("go", [
      "run", "./cmd/md-viewer",
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
// untracked source document for deterministic Changed-only browser coverage.
async function initializeGitFixture(contentRoot) {
  await runFile("git", ["-C", contentRoot, "init", "-b", "main"]);
  await runFile("git", ["-C", contentRoot, "add", "."]);
  await runFile("git", ["-C", contentRoot, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "fixtures"]);
  await writeFile(path.join(contentRoot, "changed-only.go"), "package changed\n");
  await writeFile(path.join(contentRoot, "diff-layout.go"), `package fixture

const layoutMessage = "This current-side replacement is also deliberately long so its highlight stays inside the independently scrollable current pane."

var afterSelection = true
`);
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

module.exports = { test, expect };
