import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { once } from "node:events";
import { after, before, test } from "node:test";
import { setTimeout } from "node:timers/promises";
import { chromium } from "@playwright/test";

const origin = "http://127.0.0.1:4184";
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
      "4184",
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

const entry = {
  id: "work",
  version: 3,
  worker_name: "Testperson",
  farm_name: "Testbetrieb",
  state: "SUBMITTED",
  started_at: "2026-01-02T09:00:00Z",
  stopped_at: "2026-01-02T10:00:00Z",
  effective_start: "2026-01-02T09:00:00Z",
  effective_stop: "2026-01-02T10:00:00Z",
  net_us: 3600000000,
  gross_us: 3600000000,
  break_us: 0,
};
for (const width of [390, 1280]) {
  test(`delete requires explicit modal confirmation (${width}px)`, async () => {
    const context = await browser.newContext({
      viewport: { width, height: 900 },
      serviceWorkers: "block",
    });
    try {
      const page = await context.newPage();
      let deleted = false;
      const writes = [];
      await page.route("**/api/v1/**", async (route) => {
        const path = new URL(route.request().url()).pathname;
        if (route.request().method() === "DELETE") {
          writes.push(route.request().postDataJSON());
          deleted = true;
          return route.fulfill({ status: 204 });
        }
        const value = path.endsWith("/me")
          ? {
              user: {
                id: "admin",
                name: "Admin",
                is_admin: true,
                is_worker: true,
                language: "de",
                theme: "light",
              },
              organization: { timezone: "Europe/Vienna", currency: "EUR" },
              csrf_token: "test",
            }
          : path.endsWith("/work-sessions")
            ? { items: deleted ? [] : [entry], total: deleted ? 0 : 1 }
            : path.endsWith("/pricing")
              ? { pricing: null, exclusion_reason: "DETAILS_REQUIRED" }
              : { items: [], total: 0 };
        await route.fulfill({ json: value });
      });
      await page.goto(origin + "/#entries");
      await page
        .getByRole("button", { name: "Details anzeigen", exact: true })
        .click();
      await page
        .getByRole("button", { name: "Arbeitseintrag löschen", exact: true })
        .click();
      assert.equal(await page.locator("dialog[open]").count(), 1);
      const dialog = page.getByRole("dialog", {
        name: "Arbeitseintrag löschen",
        exact: true,
      });
      await dialog.waitFor();
      assert.match(await dialog.innerText(), /Testperson/);
      assert.match(await dialog.innerText(), /Testbetrieb/);
      await dialog
        .getByRole("button", { name: "Abbrechen", exact: true })
        .click();
      assert.equal(writes.length, 0);
      await page
        .getByRole("button", { name: "Arbeitseintrag löschen", exact: true })
        .click();
      await page.keyboard.press("Escape");
      assert.equal(writes.length, 0);
      await page
        .getByRole("button", { name: "Arbeitseintrag löschen", exact: true })
        .click();
      await dialog
        .getByRole("button", { name: "Endgültig löschen", exact: true })
        .click();
      assert.equal(writes.length, 0);
      await dialog
        .getByLabel("Begründung", { exact: true })
        .fill("Doppelt erfasst");
      await dialog
        .getByRole("button", { name: "Endgültig löschen", exact: true })
        .click();
      await dialog.waitFor({ state: "detached" });
      assert.deepEqual(writes, [{ version: 3, reason: "Doppelt erfasst" }]);
      await page
        .getByRole("button", { name: "Details anzeigen", exact: true })
        .waitFor({ state: "detached" });
      assert.equal(
        await page.evaluate(
          () => document.documentElement.scrollWidth > innerWidth,
        ),
        false,
      );
    } finally {
      await context.close();
    }
  });
}
