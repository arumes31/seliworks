import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { once } from "node:events";
import { before, after, test } from "node:test";
import { setTimeout } from "node:timers/promises";
import { chromium, expect } from "@playwright/test";
const origin = "http://127.0.0.1:4191";
let browser, server;
before(async () => {
  server = spawn(
    process.execPath,
    [
      "node_modules/vite/bin/vite.js",
      "preview",
      "--host",
      "127.0.0.1",
      "--port",
      "4191",
      "--strictPort",
    ],
    { cwd: new URL("../", import.meta.url), stdio: "ignore" },
  );
  const deadline = Date.now() + 15000;
  for (;;) {
    try {
      if ((await fetch(origin)).ok) break;
    } catch {}
    assert.ok(server.exitCode === null && Date.now() < deadline);
    await setTimeout(50);
  }
  browser = await chromium.launch();
});
after(async () => {
  await browser?.close();
  if (server && server.exitCode === null) {
    const done = once(server, "exit");
    server.kill();
    await done;
  }
});
for (const lang of ["de", "en"])
  for (const width of [390, 1280])
    test(`admin employee PDF ${lang} width=${width}`, async () => {
      const context = await browser.newContext({
        viewport: { width, height: 900 },
        serviceWorkers: "block",
      });
      try {
        const page = await context.newPage();
        let query;
        const workers = [
          {
            id: "00000000-0000-0000-0000-000000000001",
            name: "Alice",
            email: "alice@example.test",
            is_worker: true,
            active: true,
          },
          {
            id: "00000000-0000-0000-0000-000000000002",
            name: "Bob",
            email: "bob@example.test",
            is_worker: false,
            timesheet_eligible: true,
            active: false,
          },
          {
            id: "00000000-0000-0000-0000-000000000003",
            name: "No work administrator",
            is_worker: false,
            active: true,
          },
        ];
        await page.route("**/api/v1/**", async (route) => {
          const url = new URL(route.request().url());
          if (url.pathname.endsWith("/admin/timesheets.pdf")) {
            query = url.searchParams;
            return route.fulfill({
              contentType: "application/pdf",
              body: "%PDF-1.4\n%%EOF\n",
            });
          }
          const data = url.pathname.endsWith("/me")
            ? {
                user: {
                  id: "self",
                  name: "Admin",
                  is_admin: true,
                  is_worker: true,
                  language: lang,
                  theme: "light",
                  passkey_count: 1,
                },
                organization: { timezone: "Europe/Vienna", currency: "EUR" },
                csrf_token: "fixture",
              }
            : {
                items: url.pathname.endsWith("/admin/workers") ? workers : [],
                total: 0,
              };
          return route.fulfill({ json: data });
        });
        await page.goto(origin + "/#entries");
        await page
          .getByRole("button", {
            name:
              lang === "de"
                ? "Zeitnachweise Mitarbeitende (PDF)"
                : "Employee timesheets (PDF)",
            exact: true,
          })
          .click();
        const dialog = page.getByRole("dialog");
        await dialog.locator("[name=from]").fill("2026-10-05");
        await dialog.locator("[name=to]").fill("2026-10-14");
        const button = dialog.getByRole("button", {
          name: lang === "de" ? "PDF herunterladen" : "Download PDF",
          exact: true,
        });
        async function download() {
          const [file] = await Promise.all([
            page.waitForEvent("download"),
            button.click(),
          ]);
          assert.equal(await file.failure(), null);
          await expect(button).toBeEnabled();
        }
        await download();
        assert.equal(query.get("all"), "true");
        assert.deepEqual(query.getAll("worker_id"), []);
        await dialog
          .getByRole("checkbox", {
            name:
              lang === "de"
                ? "Alle Mitarbeitenden (auch inaktive)"
                : "All employees (including inactive)",
            exact: true,
          })
          .uncheck();
        await button.click();
        await expect(dialog.getByRole("alert")).toBeVisible();
        await expect(
          dialog.getByRole("checkbox", { name: /No work administrator/ }),
        ).toHaveCount(0);
        await dialog.getByRole("checkbox", { name: /Alice/ }).check();
        await download();
        assert.equal(query.has("all"), false);
        assert.deepEqual(query.getAll("worker_id"), [workers[0].id]);
        await dialog.getByRole("checkbox", { name: /Bob/ }).check();
        await download();
        assert.deepEqual(
          query.getAll("worker_id"),
          workers.slice(0, 2).map((w) => w.id),
        );
        assert.equal(query.get("from"), "2026-10-05");
        assert.equal(query.get("to"), "2026-10-14");
        assert.equal(query.get("lang"), lang);
        assert.ok(
          await page.evaluate(
            () => document.documentElement.scrollWidth <= innerWidth,
          ),
        );
      } finally {
        await context.close();
      }
    });
for (const lang of ["de", "en"])
  for (const admin of [false, true])
    for (const width of [390, 1280])
      test(`personal PDF ${lang} admin=${admin} width=${width}`, async () => {
        const context = await browser.newContext({
          viewport: { width, height: 900 },
          serviceWorkers: "block",
        });
        try {
          const page = await context.newPage();
          const errors = [];
          page.on("pageerror", (e) => errors.push(e.message));
          let query;
          let empty = false;
          await page.route("**/api/v1/**", async (route) => {
            const url = new URL(route.request().url());
            if (url.pathname.endsWith("/me/timesheet.pdf")) {
              query = Object.fromEntries(url.searchParams);
              return empty
                ? route.fulfill({
                    status: 422,
                    json: {
                      error: {
                        code: "TIMESHEET_EMPTY",
                        message: "No completed work entries in this period",
                      },
                    },
                  })
                : route.fulfill({
                    contentType: "application/pdf",
                    body: "%PDF-1.4\n%%EOF\n",
                  });
            }
            const data = url.pathname.endsWith("/me")
              ? {
                  user: {
                    id: "self",
                    name: "Own user",
                    is_admin: admin,
                    is_worker: true,
                    language: lang,
                    theme: "light",
                    passkey_count: 1,
                  },
                  organization: { timezone: "Europe/Vienna", currency: "EUR" },
                  csrf_token: "fixture",
                }
              : { items: [], total: 0 };
            return route.fulfill({ json: data });
          });
          await page.goto(origin + "/#history");
          await page
            .getByRole("button", {
              name:
                lang === "de"
                  ? "Persönlicher Zeitnachweis (PDF)"
                  : "Personal timesheet (PDF)",
              exact: true,
            })
            .click();
          const dialog = page.getByRole("dialog");
          await expect(dialog).toHaveCount(1);
          await dialog.locator("[name=from]").fill("2026-10-25");
          await dialog
            .getByLabel(
              lang === "de" ? "Bis (einschließlich)" : "Through (included)",
              { exact: true },
            )
            .fill("2026-10-25");
          const button = dialog.getByRole("button", {
            name: lang === "de" ? "PDF herunterladen" : "Download PDF",
            exact: true,
          });
          const [download] = await Promise.all([
            page.waitForEvent("download"),
            button.click(),
          ]);
          assert.equal(await download.failure(), null);
          assert.equal(
            download.suggestedFilename(),
            "timesheet-2026-10-25-2026-10-25.pdf",
          );
          assert.deepEqual(query, {
            from: "2026-10-25",
            to: "2026-10-25",
            lang,
          });
          await expect(button).toBeEnabled();
          empty = true;
          await button.click();
          await expect(
            dialog.getByText(
              lang === "de"
                ? "In diesem Zeitraum gibt es keine beendeten Arbeitseinträge."
                : "No completed work entries in this period",
              { exact: true },
            ),
          ).toBeVisible();
          assert.equal(
            await dialog.locator("[name=from]").inputValue(),
            "2026-10-25",
          );
          assert.ok(
            await page.evaluate(
              () => document.documentElement.scrollWidth <= window.innerWidth,
            ),
            "horizontal overflow",
          );
          assert.deepEqual(errors, []);
        } finally {
          await context.close();
        }
      });
