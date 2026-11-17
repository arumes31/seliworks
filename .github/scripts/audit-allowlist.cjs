// Frontend npm audit gate with a surgical, single-advisory allowlist.
//
// Allowlist GHSA-fc5q-39xc-vjpf: picomatch ReDoS, dev/build-time only (vite's
// internal glob machinery: picomatch -> fdir -> tinyglobby -> vite ->
// @vitejs/plugin-react, all devDependencies). No upstream patch exists as of
// 2026-11-17 (latest picomatch 4.0.7 is still in the vulnerable `<=4.0.7`
// range; advisory firstPatchedVersion = None). Remove this allowlist once
// picomatch publishes a version > 4.0.7.
// Tracked: https://github.com/advisories/GHSA-fc5q-39xc-vjpf
//
// Usage: node .github/scripts/audit-allowlist.cjs <audit-json-file>
// Exits 0 only when every high/critical advisory present is allowlisted;
// exits 1 (failing the gate) on any other high/critical advisory.
"use strict";

const allow = new Set(["GHSA-fc5q-39xc-vjpf"]);
const blocking = new Set(["high", "critical"]);

const file = process.argv[2];
if (!file) {
  console.error("usage: audit-allowlist.cjs <audit-json-file>");
  process.exit(2);
}

const report = JSON.parse(require("fs").readFileSync(file, "utf8"));
const offenders = new Set();
const seen = new Set();

for (const vuln of Object.values(report.vulnerabilities || {})) {
  for (const via of vuln.via || []) {
    if (via && typeof via === "object" && via.url) {
      const id = via.url.split("/").pop();
      seen.add(id + " (" + via.severity + ")");
      if (blocking.has(via.severity) && !allow.has(id)) {
        offenders.add(id);
      }
    }
  }
}

if (offenders.size) {
  console.error(
    "npm audit gate failed; disallowed high/critical advisories present: " +
      [...offenders].join(", ")
  );
  process.exit(1);
}

console.log(
  "npm audit gate OK; only allowlisted advisories present: " +
    ([...seen].join(", ") || "none")
);
