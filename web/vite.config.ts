import { defineConfig, minify } from "vite";
import react from "@vitejs/plugin-react";
import { createHash } from "node:crypto";
import { readFile, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
const publicServiceWorker = () => ({
  name: "version-public-service-worker",
  async writeBundle(
    options: { dir?: string },
    bundle: Record<
      string,
      { type: string; code?: string; source?: string | Uint8Array }
    >,
  ) {
    const hash = createHash("sha256");
    for (const key of Object.keys(bundle).sort()) {
      const output = bundle[key]!;
      hash.update(key);
      if (output.type === "chunk") hash.update(output.code ?? "");
      else hash.update(output.source ?? "");
    }
    const source = await readFile(resolve("public/sw.js"), "utf8");
    const worker = await minify(
      "sw.js",
      source.replaceAll("__BUILD_ID__", hash.digest("hex").slice(0, 20)),
      { module: false, sourcemap: false },
    );
    if (worker.errors.length) {
      throw new Error(
        `Service worker minification failed: ${worker.errors.map((error) => error.message).join("; ")}`,
      );
    }
    await writeFile(resolve(options.dir ?? "dist", "sw.js"), worker.code);
  },
});
export default defineConfig({
  plugins: [react(), publicServiceWorker()],
  server: { host: "127.0.0.1", proxy: { "/api": "http://127.0.0.1:16540" } },
  build: {
    outDir: "dist",
    minify: "oxc",
    sourcemap: false,
    assetsInlineLimit: 0,
  },
});
