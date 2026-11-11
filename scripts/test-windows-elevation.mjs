import { parseArgs } from "node:util";
import { access } from "node:fs/promises";
import { spawnSync } from "node:child_process";
import { mkdir, mkdtemp, realpath, rm, writeFile } from "node:fs/promises";
import { isAbsolute, join, relative, resolve } from "node:path";

// Native UAC remains a manual Windows interaction. Run from the original,
// non-elevated user to verify the product's own result and manager-SID path.
const { values: args } = parseArgs({
  options: { server: { type: "string" }, help: { type: "boolean" } },
});
if (args.help) {
  console.log("node scripts/test-windows-elevation.mjs --server=<path>");
  process.exit(0);
}
if (!args.server) throw new Error("node scripts/test-windows-elevation.mjs --server=<path>");
const executable = resolve(args.server);
await access(executable);
if (process.platform !== "win32") throw new Error("Windows acceptance only");
const root = resolve(import.meta.dirname, "..");
const scratch = join(root, ".tmp");
await mkdir(scratch, { recursive: true });
const folder = await mkdtemp(join(scratch, "授权 smoke "));
const data = join(folder, "Server 中文 data");
const evidence = { at: new Date().toISOString(), status: "failed", cleanup: "pending" };
function run(...args) {
  const r = spawnSync(executable, ["--data-dir", data, ...args], {
    encoding: "utf8",
    windowsHide: true,
    timeout: 180000,
  });
  if (r.error || r.status !== 0) throw new Error(r.stderr?.trim() || String(r.error));
  return JSON.parse(r.stdout);
}
try {
  const deployed = run("--elevate", "runtime", "deploy");
  if (!deployed.launcher) throw new Error("Native authorized worker returned no deployment");
  const started = run("run", "start");
  if (!started.running || started.mode !== "background")
    throw new Error("Original user could not run the authorized deployment");
  run("run", "stop");
  evidence.status = "passed";
  evidence.steps = [
    "native ShellExecuteExW authorization",
    "private result roundtrip",
    "original manager access after deployment",
    "background start and stop",
  ];
  console.log("Passed native UAC result roundtrip and original-user runtime access.");
} catch (e) {
  evidence.reason = e.message;
  process.exitCode = 1;
  console.error(e.message);
} finally {
  try {
    const actual = await realpath(folder);
    const boundary = await realpath(scratch);
    const within = relative(boundary, actual);
    if (!within || within.startsWith("..") || isAbsolute(within))
      throw new Error("Unsafe cleanup path");
    if (run("run", "status").running) run("run", "stop");
    await rm(actual, { recursive: true, force: true });
    evidence.cleanup = "removed";
  } catch (e) {
    evidence.cleanup = e.message;
    evidence.status = "failed";
    process.exitCode = 1;
  }
  await mkdir(join(root, "artifacts/validation"), { recursive: true });
  await writeFile(
    join(root, "artifacts/validation/windows-elevation.json"),
    JSON.stringify(evidence, null, 2) + "\n",
  );
}
