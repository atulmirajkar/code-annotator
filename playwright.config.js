const { defineConfig } = require("@playwright/test");

module.exports = defineConfig({
  testDir: "./browser-tests",
  fullyParallel: false,
  workers: 1,
  reporter: "line",
  timeout: 30000,
  use: {
    channel: process.env.CODE_ANNOTATOR_BROWSER_CHANNEL || "msedge",
    headless: true,
    trace: "retain-on-failure",
  },
});
