import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { once } from "node:events";
import { after, before, test } from "node:test";
import { setTimeout } from "node:timers/promises";
import { chromium, firefox } from "@playwright/test";

const origin = "http://127.0.0.1:4194";
let server, browser;
before(async () => {
  server = spawn(
    process.execPath,
    [
      "node_modules/vite/bin/vite.js",
      "preview",
      "--host",
      "127.0.0.1",
      "--port",
      "4194",
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
      "preview did not start",
    );
    await setTimeout(50);
  }
  browser = await (
    process.env.SELIWORKS_BROWSER === "firefox" ? firefox : chromium
  ).launch();
});
after(async () => {
  await browser?.close();
  if (server && server.exitCode === null) {
    const stopped = once(server, "exit");
    server.kill();
    await stopped;
  }
});

for (const width of [390, 1280])
  for (const mode of ["worker", "admin", "manual"]) {
    const admin = mode !== "worker";
    const manual = mode === "manual";
    test(`multiple activities and text round-trip (${mode}, ${width}px)`, async () => {
      const context = await browser.newContext({
        viewport: { width, height: 900 },
        serviceWorkers: "block",
      });
      try {
        const page = await context.newPage();
        const errors = [];
        page.on("pageerror", (error) => errors.push(error.message));
        let entry = {
          id: "work",
          version: 1,
          worker_id: "user",
          worker_name: "Testperson",
          farm_id: "farm",
          farm_name: "Testbetrieb",
          activity_id: "one",
          activity_ids: ["one"],
          activity_names: ["Fütterung"],
          activity_name: "Fütterung",
          additional_activities: "Vorheriger Text",
          description: "Arbeit erledigt",
          location: "",
          state: "STOPPED_PENDING_DETAILS",
          started_at: "2026-01-02T09:00:00Z",
          stopped_at: "2026-01-02T10:00:00Z",
          effective_start: "2026-01-02T09:00:00Z",
          effective_stop: "2026-01-02T10:00:00Z",
          net_us: 3600000000,
          gross_us: 3600000000,
          break_us: 0,
          breaks: [],
          day_type: "REGULAR",
          additional_deduction_us: 0,
        };
        const activities = [
          { id: "one", name: "Fütterung", active: true },
          { id: "two", name: "Stallarbeit", active: true },
        ];
        const writes = [];
        await page.route("**/api/v1/**", async (route) => {
          const path = new URL(route.request().url()).pathname;
          if (
            route.request().method() === "PATCH" ||
            path.endsWith("/manual")
          ) {
            const body = route.request().postDataJSON();
            writes.push(body);
            entry = {
              ...entry,
              ...body,
              activity_names: body.activity_ids.map(
                (id) => activities.find((a) => a.id === id).name,
              ),
              version: entry.version + 1,
            };
            return route.fulfill({ json: entry });
          }
          if (path.endsWith("/submit")) {
            entry.state = "SUBMITTED";
            return route.fulfill({ json: entry });
          }
          const value = path.endsWith("/me")
            ? {
                user: {
                  id: "user",
                  name: "Testperson",
                  is_admin: admin,
                  is_worker: true,
                  language: "de",
                  theme: "light",
                  passkey_count: 1,
                },
                organization: { timezone: "Europe/Vienna", currency: "EUR" },
                csrf_token: "fixture",
              }
            : path.endsWith("/activities")
              ? { items: activities, total: 2 }
              : path.endsWith("/farms")
                ? {
                    items: [{ id: "farm", name: "Testbetrieb", active: true }],
                    total: 1,
                  }
                : path.endsWith("/workers")
                  ? {
                      items: [
                        {
                          id: "user",
                          name: "Testperson",
                          active: true,
                          is_worker: true,
                        },
                      ],
                      total: 1,
                    }
                  : path.endsWith("/work-sessions")
                    ? { items: [entry], total: 1 }
                    : path.endsWith("/pricing")
                      ? { pricing: null, exclusion_reason: "DETAILS_REQUIRED" }
                      : { items: [], total: 0 };
          await route.fulfill({ json: value });
        });
        async function open() {
          if (admin)
            await page
              .getByRole("button", { name: "Details anzeigen", exact: true })
              .click();
          else await page.locator(".history-row").click();
        }
        await page.goto(origin + (admin ? "/#entries" : "/#history"));
        if (manual)
          await page
            .getByRole("button", {
              name: "Manuellen Eintrag erstellen",
              exact: true,
            })
            .click();
        else await open();
        if (admin && !manual)
          await page
            .getByRole("button", {
              name: "Eintrag vollständig bearbeiten",
              exact: true,
            })
            .click();
        const dialog = page.getByRole("dialog");
        if (manual) {
          await dialog.locator('[name="worker_id"]').selectOption("user");
          await dialog.locator('[name="farm_id"]').selectOption("farm");
          await dialog.locator('[name="started_at"]').fill("2026-01-03T09:00");
          await dialog.locator('[name="stopped_at"]').fill("2026-01-03T10:00");
          await dialog.locator('[name="description"]').fill("Manuelle Arbeit");
          await dialog.getByLabel("Fütterung", { exact: true }).check();
        }
        assert.equal(
          await dialog.getByLabel("Fütterung", { exact: true }).isChecked(),
          true,
        );
        assert.equal(
          await dialog
            .getByLabel("Weitere Tätigkeiten", { exact: true })
            .inputValue(),
          manual ? "" : "Vorheriger Text",
        );
        await dialog.getByLabel("Stallarbeit", { exact: true }).check();
        await dialog
          .getByLabel("Haupttätigkeit (Stundensatz)", { exact: true })
          .selectOption("two");
        await dialog
          .getByLabel("Weitere Tätigkeiten", { exact: true })
          .fill("Zaun repariert\nWasser geprüft <script>text</script>");
        if (admin)
          await dialog
            .locator('[name="reason"]')
            .fill("Weitere Arbeiten ergänzt");
        assert.equal(
          await page.evaluate(
            () => document.documentElement.scrollWidth > innerWidth,
          ),
          false,
        );
        if (process.env.SELIWORKS_SCREENSHOT_DIR) {
          await dialog.locator(".activity-selection").scrollIntoViewIfNeeded();
          await page.screenshot({
            path: `${process.env.SELIWORKS_SCREENSHOT_DIR}/activities-${mode}-${width}.png`,
            fullPage: true,
          });
        }
        await dialog
          .getByRole("button", {
            name: manual
              ? "Erstellen"
              : admin
                ? "Änderungen speichern"
                : "Speichern & zur Prüfung senden",
            exact: true,
          })
          .click();
        await dialog.waitFor({ state: "detached" });
        assert.equal(writes.length, 1);
        assert.deepEqual(writes[0].activity_ids, ["one", "two"]);
        assert.equal(writes[0].activity_id, "two");
        assert.equal(
          writes[0].additional_activities,
          "Zaun repariert\nWasser geprüft <script>text</script>",
        );
        await page.reload();
        await open();
        assert.match(
          await page.getByRole("dialog").innerText(),
          /Fütterung, Stallarbeit/,
        );
        assert.match(
          await page.getByRole("dialog").innerText(),
          /Wasser geprüft <script>text<\/script>/,
        );
        assert.deepEqual(errors, []);
      } finally {
        await context.close();
      }
    });
  }
