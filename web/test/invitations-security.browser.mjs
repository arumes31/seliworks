import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { once } from "node:events";
import { after, before, test } from "node:test";
import { setTimeout } from "node:timers/promises";
import { chromium } from "@playwright/test";

const origin = "http://127.0.0.1:4179";
let server;
let browser;

before(async () => {
  server = spawn(
    process.execPath,
    [
      "node_modules/vite/bin/vite.js",
      "preview",
      "--host",
      "127.0.0.1",
      "--port",
      "4179",
      "--strictPort",
    ],
    { cwd: new URL("../", import.meta.url), stdio: "ignore" },
  );
  const deadline = Date.now() + 15000;
  for (;;) {
    try {
      if ((await fetch(origin)).ok) break;
    } catch {}
    assert.ok(
      server.exitCode === null && Date.now() < deadline,
      "preview server failed to start",
    );
    await setTimeout(50);
  }
  browser = await chromium.launch();
});

after(async () => {
  await browser?.close();
  if (server && server.exitCode === null) {
    const stopped = once(server, "exit");
    server.kill();
    await stopped;
  }
});

for (const [name, original, next] of [
  ["pending onboarding name correction", false, false],
  ["active worker name correction", true, true],
  ["explicit deactivation", true, false],
  ["explicit activation", false, true],
]) {
  test(name, async () => {
    const context = await browser.newContext();
    try {
      const page = await context.newPage();
      page.setDefaultTimeout(10000);
      const user = {
        id: "admin-fixture",
        name: "Admin",
        email: "admin@example.test",
        is_admin: true,
        is_worker: true,
        active: true,
        language: "de",
        theme: "light",
        version: 1,
      };
      const worker = {
        id: "pending-fixture",
        name: "Pending worker",
        email: "pending@example.test",
        is_admin: false,
        is_worker: true,
        active: original,
        version: 1,
      };
      await page.route("**/api/v1/**", async (route) => {
        const path = new URL(route.request().url()).pathname;
        const value =
          path === "/api/v1/me"
            ? {
                user,
                csrf_token: "isolated-ui-fixture",
                organization: {
                  name: "Security test",
                  timezone: "Europe/Vienna",
                  currency: "EUR",
                },
              }
            : path === "/api/v1/admin/workers"
              ? { items: [worker], total: 1 }
              : { items: [], total: 0 };
        await route.fulfill({ json: value });
      });
      await page.goto(origin + "/#workers");
      await page
        .getByRole("row")
        .filter({ hasText: "Pending worker" })
        .getByRole("button", { name: "Bearbeiten", exact: true })
        .click();
      await page.locator('[name="name"]').fill("Corrected worker");
      await page.locator('[name="active"]').setChecked(next);
      const request = page.waitForRequest(
        (r) =>
          r.method() === "PATCH" &&
          r.url().endsWith("/api/v1/admin/workers/pending-fixture"),
      );
      await page
        .getByRole("button", { name: "Änderungen speichern", exact: true })
        .click();
      const payload = (await request).postDataJSON();
      assert.equal(payload.name, "Corrected worker");
      assert.equal(Object.hasOwn(payload, "active"), original !== next);
      if (original !== next) assert.equal(payload.active, next);
    } finally {
      await context.close();
    }
  });
}
