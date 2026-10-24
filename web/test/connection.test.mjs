import assert from "node:assert/strict";
import test from "node:test";
import { api, APIError, errorText } from "../src/api.ts";

test("read failures explain loading, writes retain uncertain-outcome warning", async (t) => {
  let calls = 0;
  t.mock.method(globalThis, "fetch", async () => {
    calls++;
    throw new TypeError("Failed to fetch");
  });
  for (const method of ["GET", "POST"]) {
    await assert.rejects(api("/farms", method), (error) => {
      assert.ok(error instanceof APIError);
      assert.equal(
        error.code,
        method === "GET" ? "NETWORK_READ" : "NETWORK_WRITE",
      );
      assert.match(
        errorText(error, "de"),
        method === "GET"
          ? /Daten konnten nicht geladen/
          : /Erfolg ist nicht bestätigt/,
      );
      return true;
    });
  }
  assert.equal(calls, 2, "mutations must never be automatically replayed");
});

test("invalid successful JSON is rejected instead of becoming null data", async (t) => {
  t.mock.method(
    globalThis,
    "fetch",
    async () => new Response("<html>Proxy</html>"),
  );
  await assert.rejects(
    api("/farms"),
    (error) => error.code === "INVALID_RESPONSE",
  );
});

test("aborted response bodies remain transport failures", async (t) => {
  t.mock.method(globalThis, "fetch", async () => ({
    ok: true,
    status: 200,
    json: async () => {
      throw new DOMException("Aborted", "AbortError");
    },
  }));
  await assert.rejects(api("/farms"), (error) => error.code === "NETWORK_READ");
});

test("JSON null and no-content success remain valid; HTTP errors keep their status", async (t) => {
  t.mock.method(globalThis, "fetch", async () => new Response("null"));
  assert.equal(await api("/me/active-work-session"), null);
  globalThis.fetch = async () => new Response(null, { status: 204 });
  assert.equal(await api("/auth/logout", "POST", {}), null);
  globalThis.fetch = async () => new Response("Bad gateway", { status: 502 });
  await assert.rejects(api("/farms"), (error) => error.status === 502);
});

test("application TypeErrors are not mislabeled as connectivity failures", () => {
  assert.equal(
    errorText(new TypeError("invalid field"), "de"),
    "invalid field",
  );
});
