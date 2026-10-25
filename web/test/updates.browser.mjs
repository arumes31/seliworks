import assert from "node:assert/strict";
import { createServer } from "node:http";
import { readFile } from "node:fs/promises";
import { extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { before, after, test } from "node:test";
import { chromium, firefox, expect } from "@playwright/test";
const root = fileURLToPath(new URL("../dist/", import.meta.url));
let revision = 1,
  server,
  browser,
  origin;
before(async () => {
  server = createServer(async (req, res) => {
    try {
      const path = resolve(
        root,
        "." + (req.url === "/" ? "/index.html" : req.url.split("?")[0]),
      );
      if (!path.startsWith(root)) {
        res.writeHead(403).end();
        return;
      }
      let body = await readFile(path);
      if (req.url === "/sw.js")
        body = Buffer.from(
          body.toString() + `\n// test revision ${revision}\n`,
        );
      res.setHeader("Cache-Control", "no-store");
      res.setHeader(
        "Content-Type",
        {
          ".js": "text/javascript",
          ".html": "text/html",
          ".css": "text/css",
          ".svg": "image/svg+xml",
          ".webmanifest": "application/manifest+json",
          ".woff": "font/woff",
          ".woff2": "font/woff2",
        }[extname(path)] || "application/octet-stream",
      );
      res.end(body);
    } catch {
      res.writeHead(404).end();
    }
  });
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  origin = `http://127.0.0.1:${server.address().port}`;
  browser = await (
    process.env.SELIWORKS_BROWSER === "firefox" ? firefox : chromium
  ).launch();
});
after(async () => {
  await browser?.close();
  await new Promise((resolve) => server?.close(resolve));
});

test("production service worker is minified and versioned", async () => {
  const source = await readFile(
    new URL("../public/sw.js", import.meta.url),
    "utf8",
  );
  const built = await readFile(resolve(root, "sw.js"), "utf8");
  assert.ok(
    Buffer.byteLength(built) < Buffer.byteLength(source) * 0.75,
    "the published service worker should be substantially smaller than its source",
  );
  assert.match(built, /seliworks-shell-[a-f0-9]{20}/);
  assert.doesNotMatch(built, /__BUILD_ID__|sourceMappingURL/);
});

test("a new worker activates even when the existing client cannot request it", async () => {
  const context = await browser.newContext();
  try {
    const page = await context.newPage();
    await page.goto(origin + "/offline.html");
    await page.evaluate(() => navigator.serviceWorker.register("/sw.js"));
    await page.waitForFunction(() => !!navigator.serviceWorker.controller);
    revision++;
    await page.evaluate(async () => {
      const changed = new Promise((resolve, reject) => {
        const timeout = setTimeout(
          () =>
            reject(
              new Error("worker still requires a client activation message"),
            ),
          7000,
        );
        navigator.serviceWorker.addEventListener(
          "controllerchange",
          () => {
            clearTimeout(timeout);
            resolve(true);
          },
          { once: true },
        );
      });
      const registration = await navigator.serviceWorker.getRegistration();
      await registration.update();
      await changed;
    });
    assert.equal(page.url(), origin + "/offline.html");
  } finally {
    await context.close();
  }
});

for (const blocker of ["none", "dirty form", "pending write", "offline"]) {
  test(`real service worker update: ${blocker}`, async () => {
    const context = await browser.newContext();
    try {
      const page = await context.newPage();
      let write;
      const auth = {
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
        csrf_token: "fixture",
      };
      await page.route("**/api/v1/**", (route) => {
        if (route.request().method() === "PATCH") {
          write = route;
          return;
        }
        return route.fulfill({
          json:
            new URL(route.request().url()).pathname === "/api/v1/me"
              ? auth
              : { items: [], total: 0 },
        });
      });
      await page.goto(origin + "/#activities");
      await page.evaluate(() => navigator.serviceWorker.ready.then(() => true));
      await page.waitForFunction(
        () => navigator.serviceWorker.controller !== null,
      );
      await expect(page.locator(".update-notice")).toHaveCount(0);
      await page
        .getByRole("heading", { name: "Tätigkeiten", exact: true })
        .waitFor();
      if (blocker === "dirty form" || blocker === "offline") {
        await page
          .getByRole("button", { name: "Erstellen", exact: true })
          .click();
        await page.locator("[name=name]").fill("Nicht verlieren");
      }
      if (blocker === "pending write") {
        await page.locator(".language-button").click();
        await expect.poll(() => !!write).toBe(true);
      }
      let navigations = 0;
      page.on("framenavigated", (frame) => {
        if (frame === page.mainFrame()) navigations++;
      });
      revision++;
      await page.evaluate(() => {
        void navigator.serviceWorker
          .getRegistration()
          .then((reg) => reg.update());
      });
      if (blocker !== "none") {
        await expect(page.locator(".update-notice")).toBeVisible();
        await page.waitForTimeout(1500);
        assert.equal(navigations, 0);
        if (blocker === "dirty form" || blocker === "offline") {
          await expect(page.locator("[name=name]")).toHaveValue(
            "Nicht verlieren",
          );
          if (blocker === "offline") await context.setOffline(true);
          await page
            .getByRole("button", { name: "Schließen", exact: true })
            .click();
          if (blocker === "offline") {
            await page.waitForFunction(async () => {
              const reg = await navigator.serviceWorker.getRegistration();
              return !reg?.waiting && reg?.active?.state === "activated";
            });
            await page.waitForTimeout(1500);
            assert.equal(
              navigations,
              0,
              "the worker activates without an offline page reload",
            );
            await context.setOffline(false);
          }
        } else await write.fulfill({ json: auth.user });
      }
      await expect.poll(() => navigations, { timeout: 7000 }).toBe(1);
      await page
        .getByRole("heading", { name: "Tätigkeiten", exact: true })
        .waitFor();
      await page.waitForTimeout(1200);
      assert.equal(navigations, 1, "no reload loop");
    } finally {
      await context.close();
    }
  });
}

test("a clean tab updates while another tab preserves its unsaved form", async () => {
  const context = await browser.newContext();
  try {
    const auth = {
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
      csrf_token: "fixture",
    };
    await context.route("**/api/v1/**", (route) =>
      route.fulfill({
        json:
          new URL(route.request().url()).pathname === "/api/v1/me"
            ? auth
            : { items: [], total: 0 },
      }),
    );
    const first = await context.newPage();
    await first.goto(origin + "/#activities");
    await first.evaluate(() => navigator.serviceWorker.ready.then(() => true));
    await first.waitForFunction(() => !!navigator.serviceWorker.controller);
    await expect(first.locator(".update-notice")).toHaveCount(0);
    const second = await context.newPage();
    await second.goto(origin + "/#activities");
    await second
      .getByRole("button", { name: "Erstellen", exact: true })
      .click();
    await second.locator("[name=name]").fill("Bleibt erhalten");
    let firstNavigations = 0,
      secondNavigations = 0;
    first.on("framenavigated", (f) => {
      if (f === first.mainFrame()) firstNavigations++;
    });
    second.on("framenavigated", (f) => {
      if (f === second.mainFrame()) secondNavigations++;
    });
    revision++;
    await first.evaluate(() => {
      void navigator.serviceWorker
        .getRegistration()
        .then((reg) => reg.update());
    });
    await expect.poll(() => firstNavigations, { timeout: 7000 }).toBe(1);
    await second.waitForTimeout(1200);
    assert.equal(secondNavigations, 0);
    await expect(second.locator("[name=name]")).toHaveValue("Bleibt erhalten");
    await second
      .getByRole("button", { name: "Schließen", exact: true })
      .click();
    await expect.poll(() => secondNavigations, { timeout: 7000 }).toBe(1);
  } finally {
    await context.close();
  }
});
