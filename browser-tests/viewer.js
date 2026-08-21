const { test: base, expect } = require("@playwright/test");
const { spawn } = require("node:child_process");
const { mkdtemp, rm } = require("node:fs/promises");
const { tmpdir } = require("node:os");
const path = require("node:path");

// viewerURL owns one isolated review server per Playwright worker. Content is
// read from immutable fixtures while annotation writes go to a temporary root.
const test = base.extend({
  viewerURL: [async ({}, use) => {
    const annotationRoot = await mkdtemp(path.join(tmpdir(), "md-viewer-browser-annotations-"));
    const contentRoot = path.join(__dirname, "fixtures");
    const server = spawn("go", [
      "run", "./cmd/md-viewer",
      "-review",
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
      await use(viewerURL);
    } finally {
      server.kill("SIGINT");
      await Promise.race([
        new Promise((resolve) => server.once("exit", resolve)),
        new Promise((resolve) => setTimeout(resolve, 3000)),
      ]);
      if (server.exitCode === null) server.kill("SIGKILL");
      await rm(annotationRoot, { recursive: true, force: true });
    }
  }, { scope: "worker" }],
});

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
