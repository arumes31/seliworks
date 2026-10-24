import assert from "node:assert/strict";
import { chromium, expect } from "@playwright/test";

// Only the isolated Go integration fixture supplies this URL and account.
const origin = process.env.SELIWORKS_TEST_URL;
assert.equal(new URL(origin).hostname, "localhost");
const browser = await chromium.launch();
try {
  const context = await browser.newContext({
    serviceWorkers: "block",
    viewport: { width: 390, height: 844 },
  });
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
  let authenticator = await client.send("WebAuthn.addVirtualAuthenticator", {
    options,
  });
  await page.goto(origin);
  await page
    .locator('input[name="email"]')
    .fill(process.env.SELIWORKS_TEST_EMAIL);
  await page
    .locator('input[name="password"]')
    .fill(process.env.SELIWORKS_TEST_PASSWORD);
  await page.getByRole("button", { name: "Anmelden", exact: true }).click();
  await page
    .getByRole("button", { name: "Passkey einrichten", exact: true })
    .click();
  for (let i = 1; i <= 2; i++) {
    if (i === 2) {
      await client.send("WebAuthn.removeVirtualAuthenticator", authenticator);
      authenticator = await client.send("WebAuthn.addVirtualAuthenticator", {
        options,
      });
    }
    await page
      .getByRole("button", { name: "Passkey hinzufügen", exact: true })
      .click();
    const modal = page.getByRole("dialog", {
      name: "Passkey hinzufügen",
      exact: true,
    });
    await modal
      .getByLabel("Passkey-Name", { exact: true })
      .fill("Testgerät " + i);
    await modal
      .locator('input[name="password"]')
      .fill(process.env.SELIWORKS_TEST_PASSWORD);
    await modal
      .getByRole("button", { name: "Passkey hinzufügen", exact: true })
      .click();
    await modal.waitFor({ state: "detached" });
  }
  await expect(
    page.getByRole("button", { name: "Passkey entfernen", exact: true }),
  ).toHaveCount(2);
  await page.getByRole("button", { name: "Abmelden", exact: true }).click();
  const signedIn = page.waitForResponse((r) =>
    r.url().endsWith("/auth/passkeys/finish"),
  );
  await page
    .getByRole("button", { name: "Mit Passkey anmelden", exact: true })
    .click();
  assert.equal((await signedIn).status(), 200);
  await page.goto(origin + "/#profile");
  await page.getByRole("button", { name: "Abmelden", exact: true }).waitFor();
  const result = await page.evaluate(async () => {
    const r = await fetch("/api/v1/me/passkeys");
    return r.json();
  });
  assert.equal(result.total, 2);
  assert.ok(result.items.some((k) => k.last_used_at));
  assert.equal(
    await page
      .getByRole("button", { name: "Passkey einrichten", exact: true })
      .count(),
    0,
  );
  await page
    .getByRole("button", { name: "Passkey entfernen", exact: true })
    .last()
    .click();
  const modal = page.getByRole("dialog", {
    name: "Passkey entfernen",
    exact: true,
  });
  await modal
    .locator('input[name="password"]')
    .fill(process.env.SELIWORKS_TEST_PASSWORD);
  await modal
    .getByRole("button", { name: "Passkey entfernen", exact: true })
    .click();
  await modal.waitFor({ state: "detached" });
  await expect(
    page.getByRole("button", { name: "Passkey entfernen", exact: true }),
  ).toHaveCount(1);
  console.log(
    "Real browser + Go + PostgreSQL: two registrations, passkey login, prompt suppression and individual revocation passed.",
  );
} finally {
  await browser.close();
}
