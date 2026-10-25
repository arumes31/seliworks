import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { once } from "node:events";
import { after, before, test } from "node:test";
import { setTimeout } from "node:timers/promises";
import { chromium } from "@playwright/test";

const origin = "http://127.0.0.1:4186";
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
      "4186",
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

const workers = [
  {
    id: "one",
    name: "Anna",
    email: "anna@example.test",
    is_worker: true,
    active: true,
    version: 1,
    farm_ids: ["everyone"],
    assignment_revision: "fixture-revision",
  },
  {
    id: "two",
    name: "Ben",
    email: "ben@example.test",
    is_worker: true,
    active: false,
    version: 1,
    farm_ids: ["everyone"],
    assignment_revision: "fixture-revision",
  },
  {
    id: "admin",
    name: "Verwaltung",
    email: "admin@example.test",
    farm_ids: ["everyone"],
    assignment_revision: "admin-revision",
    is_worker: false,
    is_admin: true,
    active: true,
    version: 1,
  },
];
for (const width of [390, 1280]) {
  test(`farm assignment default, individual selection and editing (${width}px)`, async () => {
    const context = await browser.newContext({
      viewport: { width, height: 900 },
      serviceWorkers: "block",
    });
    try {
      const page = await context.newPage();
      const writes = [];
      const farms = [];
      await page.route("**/api/v1/**", async (route) => {
        const path = new URL(route.request().url()).pathname;
        if (["POST", "PATCH"].includes(route.request().method())) {
          const body = route.request().postDataJSON();
          writes.push(body);
          const farm = { ...body, id: "farm", active: true, version: 1 };
          farms.splice(0, farms.length, farm);
          return route.fulfill({ json: farm });
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
                passkey_count: 1,
              },
              organization: { timezone: "Europe/Vienna", currency: "EUR" },
              csrf_token: "test",
            }
          : path.endsWith("/workers")
            ? { items: workers, total: workers.length }
            : path.endsWith("/farms")
              ? { items: farms, total: farms.length }
              : { items: [], total: 0 };
        await route.fulfill({ json: value });
      });
      await page.goto(origin + "/#farms");
      await page
        .getByRole("button", { name: "Erstellen", exact: true })
        .click();
      let dialog = page.getByRole("dialog");
      assert.equal(
        await dialog
          .getByRole("radio", {
            name: "Alle Benutzer – auch zukünftige",
            exact: true,
          })
          .isChecked(),
        true,
      );
      await dialog.getByLabel("Name", { exact: true }).fill("Alle-Betrieb");
      await dialog
        .getByRole("button", { name: "Änderungen speichern", exact: true })
        .click();
      await dialog.waitFor({ state: "detached" });
      assert.equal(writes[0].assign_all_workers, true);
      assert.deepEqual(writes[0].worker_ids, []);
      await page
        .getByRole("button", { name: "Bearbeiten", exact: true })
        .click();
      dialog = page.getByRole("dialog");
      await dialog
        .getByRole("radio", { name: "Einzelne Benutzer", exact: true })
        .check();
      await dialog.getByRole("checkbox", { name: "Anna", exact: true }).check();
      await dialog
        .getByRole("checkbox", { name: "Verwaltung", exact: true })
        .check();
      assert.equal(
        await dialog.getByRole("checkbox", { name: /Ben/ }).count(),
        1,
      );
      // Switching modes must preserve an unsaved individual selection.
      await dialog
        .getByRole("radio", {
          name: "Alle Benutzer – auch zukünftige",
          exact: true,
        })
        .check();
      await dialog
        .getByRole("radio", { name: "Einzelne Benutzer", exact: true })
        .check();
      assert.equal(
        await dialog
          .getByRole("checkbox", { name: "Anna", exact: true })
          .isChecked(),
        true,
      );
      await dialog
        .getByRole("button", { name: "Änderungen speichern", exact: true })
        .click();
      await dialog.waitFor({ state: "detached" });
      assert.equal(writes[1].assign_all_workers, false);
      assert.deepEqual(writes[1].worker_ids, ["one", "admin"]);
      await page
        .getByRole("button", { name: "Bearbeiten", exact: true })
        .click();
      dialog = page.getByRole("dialog");
      assert.equal(
        await dialog
          .getByRole("radio", { name: "Einzelne Benutzer", exact: true })
          .isChecked(),
        true,
      );
      assert.equal(
        await dialog
          .getByRole("checkbox", { name: "Anna", exact: true })
          .isChecked(),
        true,
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

for (const target of ["anna@example.test", "admin@example.test"])
  test(`inherited farms and explicit assignment for ${target}`, async () => {
    const context = await browser.newContext({ serviceWorkers: "block" });
    try {
      const page = await context.newPage();
      const writes = [];
      await page.route("**/api/v1/**", async (route) => {
        const path = new URL(route.request().url()).pathname;
        if (route.request().method() === "PUT") {
          writes.push(route.request().postDataJSON());
          return route.fulfill({ json: { ok: true } });
        }
        const value = path.endsWith("/me")
          ? {
              user: {
                id: "admin",
                name: "Admin",
                is_admin: true,
                language: "de",
                passkey_count: 1,
              },
              organization: { timezone: "Europe/Vienna", currency: "EUR" },
              csrf_token: "test",
            }
          : path.endsWith("/workers")
            ? { items: workers, total: workers.length }
            : path.endsWith("/farms")
              ? {
                  items: [
                    {
                      id: "everyone",
                      name: "Alle-Betrieb",
                      assign_all_workers: true,
                      active: true,
                    },
                    {
                      id: "selected",
                      name: "Einzel-Betrieb",
                      assign_all_workers: false,
                      active: true,
                    },
                  ],
                  total: 2,
                }
              : { items: [], total: 0 };
        await route.fulfill({ json: value });
      });
      await page.goto(origin + "/#workers");
      await page
        .getByRole("row")
        .filter({ hasText: target })
        .getByRole("button", { name: "Zugewiesene Betriebe", exact: true })
        .click();
      const dialog = page.getByRole("dialog");
      const checkbox = dialog.getByRole("checkbox", { name: /Alle-Betrieb/ });
      assert.equal(await checkbox.isChecked(), true);
      assert.equal(await checkbox.isDisabled(), true);
      await dialog
        .getByRole("checkbox", { name: "Einzel-Betrieb", exact: true })
        .check();
      await dialog
        .getByRole("button", { name: "Änderungen speichern", exact: true })
        .click();
      await dialog.waitFor({ state: "detached" });
      assert.deepEqual(writes, [
        {
          farm_ids: ["selected"],
          expected_assignment_revision: target.startsWith("admin")
            ? "admin-revision"
            : "fixture-revision",
        },
      ]);
    } finally {
      await context.close();
    }
  });

for (const width of [390, 1280])
  test(`admin transfers work to another farm (${width}px)`, async () => {
    const context = await browser.newContext({
      viewport: { width, height: 900 },
      serviceWorkers: "block",
    });
    try {
      const page = await context.newPage();
      const writes = [];
      let entry = {
        id: "work",
        version: 3,
        worker_id: "former",
        worker_name: "Former worker",
        farm_id: "old",
        farm_name: "Originalbetrieb",
        recorded_farm_name: "Originalbetrieb",
        activity_id: "activity",
        description: "Recorded work",
        state: "APPROVED",
        started_at: "2026-01-02T09:00:00Z",
        stopped_at: "2026-01-02T10:00:00Z",
        effective_start: "2026-01-02T09:00:00Z",
        effective_stop: "2026-01-02T10:00:00Z",
        day_type: "REGULAR",
        additional_deduction_us: 0,
        net_us: 3600000000,
        gross_us: 3600000000,
        break_us: 0,
        breaks: [],
      };
      await page.route("**/api/v1/**", async (route) => {
        const path = new URL(route.request().url()).pathname;
        if (route.request().method() === "PATCH") {
          assert.equal(path, "/api/v1/admin/work-sessions/work");
          const body = route.request().postDataJSON();
          writes.push(body);
          entry = {
            ...entry,
            ...body,
            version: 4,
            farm_name: "Zielbetrieb",
            state: "SUBMITTED",
          };
          return route.fulfill({ json: entry });
        }
        const value = path.endsWith("/me")
          ? {
              user: {
                id: "admin",
                name: "Admin",
                is_admin: true,
                is_worker: false,
                language: "de",
                theme: "light",
                passkey_count: 1,
              },
              organization: { timezone: "Europe/Vienna", currency: "EUR" },
              csrf_token: "test",
            }
          : path.endsWith("/workers")
            ? {
                items: [
                  {
                    id: "former",
                    name: "Former worker",
                    is_worker: false,
                    is_admin: true,
                    active: true,
                  },
                ],
                total: 1,
              }
            : path.endsWith("/farms")
              ? {
                  items: [
                    { id: "old", name: "Originalbetrieb", active: true },
                    { id: "new", name: "Zielbetrieb", active: true },
                  ],
                  total: 2,
                }
              : path.endsWith("/activities")
                ? {
                    items: [{ id: "activity", name: "Arbeit", active: true }],
                    total: 1,
                  }
                : path.endsWith("/work-sessions")
                  ? { items: [entry], total: 1 }
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
        .getByRole("button", {
          name: "Eintrag vollständig bearbeiten",
          exact: true,
        })
        .click();
      const dialog = page.getByRole("dialog");
      await dialog
        .getByRole("combobox", { name: "Betrieb / Kunde", exact: true })
        .selectOption("new");
      await dialog
        .locator('[name="reason"]')
        .fill("Falscher Betrieb bei Erfassung");
      await dialog
        .getByRole("button", { name: "Änderungen speichern", exact: true })
        .click();
      await dialog.waitFor({ state: "detached" });
      assert.equal(writes.length, 1);
      assert.equal(writes[0].farm_id, "new");
      assert.equal(writes[0].worker_id, "former");
      assert.equal(writes[0].version, 3);
      assert.equal(writes[0].effective_start, "2026-01-02T09:00:00Z");
      await page
        .getByRole("button", { name: "Details anzeigen", exact: true })
        .click();
      await page
        .getByRole("dialog", { name: "Zielbetrieb", exact: true })
        .waitFor();
      assert.match(
        await page.getByRole("dialog").innerText(),
        /Originalbetrieb/,
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
