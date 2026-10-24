import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { once } from "node:events";
import { after, before, test } from "node:test";
import { setTimeout } from "node:timers/promises";
import { chromium } from "@playwright/test";

const origin = "http://127.0.0.1:4185";
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
      "4185",
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

const auth = {
  user: {
    id: "worker",
    name: "Person",
    email: "worker@example.test",
    is_admin: false,
    is_worker: true,
    language: "de",
    theme: "light",
    passkey_count: 0,
  },
  organization: { timezone: "Europe/Vienna", currency: "EUR" },
  csrf_token: "test",
};
const b64 = (value) => Buffer.from(value).toString("base64url");
for (const width of [390, 1280]) {
  test(`passkey prompt, multiple authenticators and removal modal (${width}px)`, async () => {
    const context = await browser.newContext({
      viewport: { width, height: 900 },
      serviceWorkers: "block",
    });
    try {
      const page = await context.newPage();
      const client = await context.newCDPSession(page);
      await client.send("WebAuthn.enable");
      const options = {
        protocol: "ctap2",
        transport: "internal",
        hasResidentKey: true,
        hasUserVerification: true,
        isUserVerified: true,
        automaticPresenceSimulation: true,
      };
      let authenticator = await client.send(
        "WebAuthn.addVirtualAuthenticator",
        { options },
      );
      let keys = [];
      let loggedIn = true;
      let additions = 0;
      let deletions = 0;
      await page.route("**/api/v1/**", async (route) => {
        const path = new URL(route.request().url()).pathname;
        const method = route.request().method();
        if (path.endsWith("/register/begin"))
          return route.fulfill({
            json: {
              publicKey: {
                challenge: b64(
                  "registration-challenge-with-enough-entropy" + additions,
                ),
                rp: { id: "localhost", name: "SeliWorks" },
                user: {
                  id: b64("worker"),
                  name: "worker@example.test",
                  displayName: "Person",
                },
                pubKeyCredParams: [{ type: "public-key", alg: -7 }],
                authenticatorSelection: {
                  residentKey: "required",
                  userVerification: "required",
                },
                attestation: "none",
              },
            },
          });
        if (path.endsWith("/register/finish")) {
          const payload = route.request().postDataJSON();
          assert.ok(payload.response.attestationObject);
          assert.ok(payload.rawId);
          keys.push({
            id: String(++additions),
            name: "Phone " + additions,
            created_at: new Date().toISOString(),
          });
          return route.fulfill({ status: 201, json: {} });
        }
        if (path.startsWith("/api/v1/me/passkeys/") && method === "DELETE") {
          keys = keys.filter((k) => !path.endsWith("/" + k.id));
          deletions++;
          return route.fulfill({ status: 204 });
        }
        if (path === "/api/v1/me/passkeys")
          return route.fulfill({ json: { items: keys, total: keys.length } });
        if (path === "/api/v1/auth/passkeys/begin")
          return route.fulfill({
            json: {
              publicKey: {
                challenge: b64("login-challenge-with-enough-entropy"),
                rpId: "localhost",
                userVerification: "required",
              },
            },
          });
        if (path === "/api/v1/auth/passkeys/finish") {
          const payload = route.request().postDataJSON();
          assert.ok(payload.response.signature);
          assert.ok(payload.response.userHandle);
          loggedIn = true;
          return route.fulfill({
            json: {
              ...auth,
              user: { ...auth.user, passkey_count: keys.length },
            },
          });
        }
        if (path === "/api/v1/me")
          return route.fulfill(
            loggedIn
              ? {
                  json: {
                    ...auth,
                    user: { ...auth.user, passkey_count: keys.length },
                  },
                }
              : { status: 401, json: { error: { code: "UNAUTHENTICATED" } } },
          );
        return route.fulfill({
          json: path.endsWith("/active-work-session")
            ? null
            : { items: [], total: 0 },
        });
      });
      await page.goto(origin.replace("127.0.0.1", "localhost") + "/#work");
      await page
        .getByRole("button", { name: "Passkey einrichten", exact: true })
        .click();
      for (let i = 0; i < 2; i++) {
        if (i) {
          await client.send(
            "WebAuthn.removeVirtualAuthenticator",
            authenticator,
          );
          authenticator = await client.send(
            "WebAuthn.addVirtualAuthenticator",
            { options },
          );
        }
        await page
          .getByRole("button", { name: "Passkey hinzufügen", exact: true })
          .click();
        const dialog = page.getByRole("dialog", {
          name: "Passkey hinzufügen",
          exact: true,
        });
        await dialog
          .getByLabel("Passkey-Name", { exact: true })
          .fill("Phone " + (i + 1));
        await dialog
          .locator('input[name="password"]')
          .fill("isolated-test-password");
        await dialog
          .getByRole("button", { name: "Passkey hinzufügen", exact: true })
          .click();
        await dialog.waitFor({ state: "detached" });
      }
      assert.equal(additions, 2);
      await page
        .getByRole("button", { name: "Passkey entfernen", exact: true })
        .first()
        .click();
      let dialog = page.getByRole("dialog", {
        name: "Passkey entfernen",
        exact: true,
      });
      await dialog
        .getByRole("button", { name: "Abbrechen", exact: true })
        .click();
      assert.equal(deletions, 0);
      await page
        .getByRole("button", { name: "Passkey entfernen", exact: true })
        .first()
        .click();
      await dialog
        .locator('input[name="password"]')
        .fill("isolated-test-password");
      await dialog
        .getByRole("button", { name: "Passkey entfernen", exact: true })
        .click();
      await dialog.waitFor({ state: "detached" });
      assert.equal(deletions, 1);
      loggedIn = false;
      await page.reload();
      await page
        .getByRole("button", { name: "Mit Passkey anmelden", exact: true })
        .click();
      await page
        .getByRole("heading", { name: "Passkeys", exact: true })
        .waitFor();
      assert.equal(loggedIn, true);
      assert.equal(
        await page
          .getByRole("button", { name: "Passkey einrichten", exact: true })
          .count(),
        0,
      );
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
for (const count of [0, 3]) {
  test(`retention preview includes ${count} expired passkey requests`, async () => {
    const context = await browser.newContext({ serviceWorkers: "block" });
    try {
      const page = await context.newPage();
      await page.route("**/api/v1/**", (route) => {
        const path = new URL(route.request().url()).pathname;
        const value = path.endsWith("/me")
          ? {
              ...auth,
              user: { ...auth.user, is_admin: true, passkey_count: 1 },
            }
          : path.endsWith("/retention")
            ? {
                version: 1,
                security_retention_days: 30,
                cutoff: "2026-01-01T00:00:00Z",
                candidates: {
                  auth_sessions: 0,
                  invitations: 0,
                  auth_limits: 0,
                  passkey_ceremonies: count,
                },
              }
            : path.endsWith("/settings")
              ? { ...auth.organization, version: 1, tax_percent: "0" }
              : { items: [], total: 0 };
        return route.fulfill({ json: value });
      });
      await page.goto(origin + "/#settings");
      const label = page.getByText("Abgelaufene Passkey-Anfragen", {
        exact: true,
      });
      await label.waitFor();
      assert.equal(
        await label.locator("..").locator("dd").innerText(),
        String(count),
      );
      const button = page.getByRole("button", {
        name: "Abgelaufene Sicherheitsdaten bereinigen",
        exact: true,
      });
      assert.equal(await button.isEnabled(), count > 0);
    } finally {
      await context.close();
    }
  });
}

for (const [width, days] of [
  [390, 3],
  [1280, 7],
]) {
  test(`passkey reminder persists, expires and reports save errors (${days} days)`, async () => {
    const context = await browser.newContext({
      viewport: { width, height: 900 },
      serviceWorkers: "block",
    });
    try {
      const page = await context.newPage();
      let until = null;
      let failSave = true;
      const writes = [];
      await page.route("**/api/v1/**", async (route) => {
        const path = new URL(route.request().url()).pathname;
        const currentAuth = () => ({
          ...auth,
          user: { ...auth.user, passkey_remind_after: until },
        });
        if (path.endsWith("/me/passkeys/reminder")) {
          const data = route.request().postDataJSON();
          writes.push(data);
          assert.equal(route.request().headers()["x-csrf-token"], "test");
          if (failSave)
            return route.fulfill({
              status: 503,
              json: { error: { code: "UNAVAILABLE", message: "Try again" } },
            });
          until = new Date(Date.now() + data.days * 86400000).toISOString();
          return route.fulfill({ json: currentAuth() });
        }
        return route.fulfill({
          json: path.endsWith("/me") ? currentAuth() : { items: [], total: 0 },
        });
      });
      await page.goto(origin + "/#history");
      const prompt = page.locator('aside[aria-label="Passkeys"]');
      await prompt.waitFor();
      const select = prompt.getByLabel("Erneut erinnern in", { exact: true });
      assert.deepEqual(
        await select
          .locator("option")
          .evaluateAll((nodes) => nodes.map((node) => node.value)),
        ["3", "4", "5", "6", "7"],
      );
      await select.selectOption(String(days));
      await prompt
        .getByRole("button", { name: "Später erinnern", exact: true })
        .click();
      await prompt.getByRole("alert").waitFor();
      assert.equal(await prompt.isVisible(), true);
      failSave = false;
      await prompt
        .getByRole("button", { name: "Später erinnern", exact: true })
        .click();
      await prompt.waitFor({ state: "detached" });
      assert.deepEqual(writes, [{ days }, { days }]);
      await page.reload();
      await page
        .getByRole("heading", { name: "Verlauf", exact: true })
        .waitFor();
      assert.equal(await prompt.count(), 0);
      until = new Date(Date.now() - 1000).toISOString();
      await page.reload();
      await prompt.waitFor();
      assert.equal(
        await page.evaluate(
          () => document.documentElement.scrollWidth > innerWidth,
        ),
        false,
      );
      await prompt
        .getByRole("button", { name: "Passkey einrichten", exact: true })
        .click();
      await page
        .getByRole("heading", { name: "Passkeys", exact: true })
        .waitFor();
    } finally {
      await context.close();
    }
  });
}

test("a delayed reminder response cannot restore a signed-out user", async () => {
  const context = await browser.newContext({ serviceWorkers: "block" });
  try {
    const page = await context.newPage();
    let loggedIn = true;
    let release;
    const delayed = new Promise((resolve) => {
      release = resolve;
    });
    let received;
    const started = new Promise((resolve) => {
      received = resolve;
    });
    await page.route("**/api/v1/**", async (route) => {
      const path = new URL(route.request().url()).pathname;
      if (path.endsWith("/me/passkeys/reminder")) {
        received();
        await delayed;
        return route.fulfill({
          json: {
            ...auth,
            user: {
              ...auth.user,
              passkey_remind_after: new Date(
                Date.now() + 3 * 86400000,
              ).toISOString(),
            },
          },
        });
      }
      if (path.endsWith("/auth/logout")) {
        loggedIn = false;
        return route.fulfill({ json: { ok: true } });
      }
      if (path.endsWith("/me"))
        return route.fulfill(
          loggedIn
            ? { json: auth }
            : { status: 401, json: { error: { code: "UNAUTHENTICATED" } } },
        );
      return route.fulfill({ json: { items: [], total: 0 } });
    });
    await page.goto(origin + "/#history");
    await page
      .getByRole("button", { name: "Später erinnern", exact: true })
      .click();
    await started;
    await page.evaluate(() => {
      location.hash = "profile";
    });
    await page.getByRole("button", { name: "Abmelden", exact: true }).click();
    await page.getByRole("button", { name: "Anmelden", exact: true }).waitFor();
    const responded = page.waitForResponse((response) =>
      response.url().endsWith("/me/passkeys/reminder"),
    );
    release();
    await responded;
    // Allow the resolved fetch and React state update to run.
    await page.waitForTimeout(100);
    assert.equal(
      await page
        .getByRole("button", { name: "Anmelden", exact: true })
        .isVisible(),
      true,
    );
    assert.equal(
      await page
        .getByRole("heading", { name: "Passkeys", exact: true })
        .count(),
      0,
    );
  } finally {
    await context.close();
  }
});
