import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { once } from "node:events";
import { after, before, test } from "node:test";
import { setTimeout } from "node:timers/promises";
import { chromium, expect } from "@playwright/test";

const origin = "http://127.0.0.1:4180";
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
      "4180",
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

for (const event of ["online", "focus"]) {
  test(`failed shared data recovers on ${event}`, async () => {
    const context = await browser.newContext();
    try {
      const page = await context.newPage();
      let farms = 0;
      await page.route("**/api/v1/**", async (route) => {
        const path = new URL(route.request().url()).pathname;
        if (path === "/api/v1/farms" && ++farms === 1)
          return route.abort("connectionreset");
        const value =
          path === "/api/v1/me"
            ? {
                user: {
                  id: "admin",
                  name: "Admin",
                  is_admin: true,
                  is_worker: true,
                  language: "de",
                  theme: "light",
                },
                organization: {
                  name: "Test",
                  timezone: "Europe/Vienna",
                  currency: "EUR",
                },
                csrf_token: "test-only",
              }
            : { items: [], total: 0 };
        return route.fulfill({ json: value });
      });
      await page.goto(origin + "/#activities");
      await page.getByRole("alert").waitFor();
      await page.evaluate(
        (name) => window.dispatchEvent(new Event(name)),
        event,
      );
      await page
        .getByRole("alert")
        .waitFor({ state: "detached", timeout: 4000 });
      assert.equal(farms, 2);
    } finally {
      await context.close();
    }
  });
}

for (const outcome of ["disconnect", "null", "empty", "no-content"]) {
  test(`${outcome}: uncertain start retains its command and compact clock fits`, async () => {
    const context = await browser.newContext();
    try {
      const page = await context.newPage();
      const starts = [];
      await page.clock.install({ time: new Date("2026-10-24T08:15:00Z") });
      await page.route("**/api/v1/**", async (route) => {
        const path = new URL(route.request().url()).pathname;
        if (route.request().method() === "POST") {
          starts.push(route.request().headers()["idempotency-key"]);
          if (outcome === "null") return route.fulfill({ json: null });
          if (outcome === "empty") return route.fulfill({ json: {} });
          if (outcome === "no-content") return route.fulfill({ status: 204 });
          return route.abort("connectionreset");
        }
        const value =
          path === "/api/v1/me"
            ? {
                user: {
                  id: "admin",
                  name: "Admin",
                  is_admin: true,
                  is_worker: true,
                  language: "de",
                  theme: "light",
                },
                organization: {
                  name: "Test",
                  timezone: "Europe/Vienna",
                  currency: "EUR",
                },
                csrf_token: "test-only",
              }
            : path === "/api/v1/me/active-work-session"
              ? null
              : path === "/api/v1/farms"
                ? {
                    items: [{ id: "farm", name: "Testbetrieb", active: true }],
                    total: 1,
                  }
                : { items: [], total: 0 };
        return route.fulfill({ json: value });
      });
      await page.goto(origin);
      const clock = page.locator(".timer-clock");
      await clock.waitFor();
      assert.match(await clock.innerText(), /24\.10\.2026, 10:15/);
      await page.clock.runFor(60000);
      assert.match(await clock.innerText(), /10:16/);
      for (const width of [1280, 390, 320]) {
        await page.setViewportSize({ width, height: 844 });
        assert.equal(
          await page.evaluate(
            () => document.documentElement.scrollWidth <= innerWidth,
          ),
          true,
        );
        assert.equal(
          await clock.evaluate((el) => getComputedStyle(el).fontSize),
          "11px",
        );
      }
      await page
        .getByRole("button", { name: "Arbeit starten", exact: true })
        .click();
      await page.getByRole("alert").waitFor();
      const saved = await page.evaluate(() =>
        JSON.parse(sessionStorage.getItem("seliworks:admin:pending-command")),
      );
      assert.equal(saved.key, starts[0]);
      await page.evaluate(() => window.dispatchEvent(new Event("online")));
      await page.clock.runFor(30000);
      assert.equal(starts.length, 1, "reconnection must not replay a write");
    } finally {
      await context.close();
    }
  });
}

test("online before the old request rejects supersedes the failed load", async () => {
  const context = await browser.newContext();
  try {
    const page = await context.newPage();
    let pending;
    let farmCalls = 0;
    await page.route("**/api/v1/**", async (route) => {
      const path = new URL(route.request().url()).pathname;
      if (path === "/api/v1/farms" && ++farmCalls === 1) {
        pending = route;
        return;
      }
      return route.fulfill({
        json: path === "/api/v1/me" ? authFixture() : { items: [], total: 0 },
      });
    });
    await page.goto(origin + "/#activities");
    await page
      .getByRole("heading", { name: "Tätigkeiten", exact: true })
      .waitFor();
    await expect.poll(() => Boolean(pending)).toBe(true);
    const recovered = page.waitForResponse((r) =>
      r.url().endsWith("/api/v1/farms"),
    );
    await page.evaluate(() => window.dispatchEvent(new Event("online")));
    await recovered;
    await pending.abort("connectionreset");
    await page
      .getByRole("heading", { name: "Tätigkeiten", exact: true })
      .waitFor();
    await page.waitForTimeout(100);
    assert.equal(await page.getByRole("alert").count(), 0);
    assert.equal(farmCalls, 2);
  } finally {
    await context.close();
  }
});

test("shared reads cannot dismiss an unconfirmed language change", async () => {
  const context = await browser.newContext();
  try {
    const page = await context.newPage();
    let reads = 0;
    await page.route("**/api/v1/**", async (route) => {
      if (route.request().method() === "PATCH")
        return route.abort("connectionreset");
      const path = new URL(route.request().url()).pathname;
      if (path === "/api/v1/me") reads++;
      return route.fulfill({
        json: path === "/api/v1/me" ? authFixture() : { items: [], total: 0 },
      });
    });
    await page.goto(origin + "/#activities");
    await page.locator(".language-button").click();
    await page.getByRole("alert").waitFor();
    const recovered = page.waitForResponse((r) =>
      r.url().endsWith("/api/v1/farms"),
    );
    await page.evaluate(() => window.dispatchEvent(new Event("online")));
    await recovered;
    assert.match(
      await page.getByRole("alert").innerText(),
      /Erfolg ist nicht bestätigt/,
    );
    await page
      .getByRole("alert")
      .getByRole("button", { name: "Aktualisieren", exact: true })
      .click();
    await page.getByRole("alert").waitFor({ state: "detached" });
    assert.equal(
      reads,
      2,
      "reconcile the actual profile rather than repeating PATCH",
    );
  } finally {
    await context.close();
  }
});

function authFixture() {
  return {
    user: {
      id: "admin",
      name: "Admin",
      is_admin: true,
      is_worker: true,
      language: "de",
      theme: "light",
    },
    organization: { name: "Test", timezone: "Europe/Vienna", currency: "EUR" },
    csrf_token: "test-only",
  };
}
