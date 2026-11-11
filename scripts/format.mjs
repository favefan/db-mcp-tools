import { spawnSync } from "node:child_process";
import { readdirSync } from "node:fs";
import { resolve } from "node:path";

const check = process.argv.includes("--check");
function run(command, args, options = {}) {
  const r = spawnSync(command, args, { encoding: "utf8", windowsHide: true, ...options });
  if (r.error) throw r.error;
  if (r.stdout) process.stdout.write(r.stdout);
  if (r.stderr) process.stderr.write(r.stderr);
  if (r.status !== 0) process.exit(r.status ?? 1);
  return r.stdout;
}
const goFiles = [];
function walk(dir) {
  for (const f of readdirSync(dir, { withFileTypes: true })) {
    const path = resolve(dir, f.name);
    if (f.isDirectory() && f.name !== "bin") walk(path);
    else if (f.name.endsWith(".go")) goFiles.push(path);
  }
}
walk("server");
const changed = run("gofmt", [check ? "-l" : "-w", ...goFiles]);
if (check && changed.trim()) process.exit(1);
run("cargo", [
  "fmt",
  "--manifest-path",
  "client/src-tauri/Cargo.toml",
  ...(check ? ["--", "--check"] : []),
]);
run(process.execPath, [
  "node_modules/prettier/bin/prettier.cjs",
  check ? "--check" : "--write",
  ".",
]);
run(process.execPath, [
  "node_modules/@taplo/cli/dist/cli.js",
  "format",
  ...(check ? ["--check"] : []),
]);
