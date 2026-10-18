// Builds ../static/vendor/codemirror.js from entry.js, with a comment of the
// bundled packages and their versions at its top, and
// ../static/vendor/codemirror.LICENSE with their licenses.
//
//   npm ci && npm run build

import { build } from "esbuild";
import { readFileSync, readdirSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";

const vendor = "../static/vendor";
const out = join(vendor, "codemirror.js");

const result = await build({
  entryPoints: ["entry.js"],
  bundle: true,
  minify: true,
  format: "iife",
  globalName: "CM",
  target: "es2020",
  legalComments: "none", // the licenses go to codemirror.LICENSE
  metafile: true,
  write: false,
});

// The packages bundled, from the paths of the inputs.
const pkgs = new Map();
for (const input of Object.keys(result.metafile.inputs)) {
  const m = input.match(/^node_modules\/((?:@[^/]+\/)?[^/]+)\//);
  if (!m || pkgs.has(m[1])) continue;
  const dir = join("node_modules", m[1]);
  const pkg = JSON.parse(readFileSync(join(dir, "package.json"), "utf8"));
  const license = readdirSync(dir).find((f) => /^licen[sc]e/i.test(f));
  pkgs.set(m[1], { version: pkg.version, license: pkg.license, text: license && readFileSync(join(dir, license), "utf8").trim() });
}
const names = [...pkgs.keys()].sort();

const banner = "/* CodeMirror 6 for ebira, built by internal/ui/web/codemirror. Packages:\n" +
  names.map((n) => ` * ${n} ${pkgs.get(n).version}\n`).join("") +
  " * Licenses: codemirror.LICENSE */\n";
writeFileSync(out, banner + result.outputFiles[0].text);

const licenses = names.map((n) => {
  const p = pkgs.get(n);
  return `${n} ${p.version} (${p.license})\n\n${p.text || "No license file in the package."}\n`;
});
writeFileSync(join(vendor, "codemirror.LICENSE"), licenses.join("\n" + "-".repeat(72) + "\n\n"));
console.log(`${out}: ${names.length} packages`);
