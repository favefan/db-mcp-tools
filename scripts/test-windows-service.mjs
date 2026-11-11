import { parseArgs } from "node:util";
import { access } from "node:fs/promises";
import { spawnSync } from "node:child_process";
import { createHash, randomUUID } from "node:crypto";
import { mkdir, readFile, realpath, rm, writeFile } from "node:fs/promises";
import { createServer } from "node:net";
import { isAbsolute, join, relative, resolve } from "node:path";

// Run from an administrator terminal after reviewing this file. This script
// creates only uniquely named acceptance services/directories, and cleans those
// resources in finally. It does not touch a user's installed Server or profile.
const { values: args } = parseArgs({
  options: {
    original: { type: "string" },
    candidate: { type: "string" },
    help: { type: "boolean" },
  },
});
if (args.help) {
  console.log(
    "node scripts/test-windows-service.mjs --original=<path> --candidate=<different-build>",
  );
  process.exit(0);
}
if (!args.original || !args.candidate)
  throw new Error(
    "node scripts/test-windows-service.mjs --original=<path> --candidate=<different-build>",
  );
const original = resolve(args.original);
const candidate = resolve(args.candidate);
await access(original);
await access(candidate);
if (process.platform !== "win32") throw new Error("Windows acceptance only");
const root = resolve(import.meta.dirname, "..");
const recordPath = join(root, "artifacts/validation/windows-service.json");
const id = randomUUID();
const systemRoot = await realpath(process.env.ProgramData);
const dirs = ["source", "migrated"].map((name) =>
  join(systemRoot, `dbmcp-v1-acceptance-${id}-${name}`),
);
let activeDir = dirs[0],
  activeExe = original;
const evidence = { at: new Date().toISOString(), status: "failed", steps: [], cleanup: [] };
const invoke = (exe, data, args, required = true) => {
  const r = spawnSync(exe, ["--data-dir", data, ...args], {
    windowsHide: true,
    encoding: "utf8",
    timeout: 90000,
  });
  if (required && (r.error || r.status !== 0))
    throw new Error(`${args.slice(0, 2).join(" ")} failed: ${r.stderr?.trim() || r.error}`);
  return required ? JSON.parse(r.stdout) : r;
};
const run = (...args) => invoke(activeExe, activeDir, args);
const sha = async (path) =>
  createHash("sha256")
    .update(await readFile(path))
    .digest("hex");
const port = async () => {
  const s = createServer();
  await new Promise((r) => s.listen(0, "127.0.0.1", r));
  const p = s.address().port;
  await new Promise((r) => s.close(r));
  return p;
};
try {
  const groups = spawnSync("whoami.exe", ["/groups"], { encoding: "utf8", windowsHide: true });
  if (!groups.stdout?.includes("S-1-16-12288"))
    throw new Error("Run this acceptance script in an elevated administrator terminal");
  evidence.original_sha256 = await sha(original);
  evidence.candidate_sha256 = await sha(candidate);
  if (evidence.original_sha256 === evidence.candidate_sha256)
    throw new Error("Service upgrade needs two different builds");
  for (const dir of dirs) await mkdir(dir); // Non-recursive: never reuse an existing folder.
  const init = run("init");
  const [adminPort, mcpPort] = await Promise.all([port(), port()]);
  await writeFile(
    join(activeDir, "deployment.toml"),
    `admin='127.0.0.1:${adminPort}'\nmcp='127.0.0.1:${mcpPort}'\n`,
  );
  run("runtime", "deploy");
  run("service", "install");
  const started = run("service", "start");
  if (!started.status?.running || started.status.mode !== "service")
    throw new Error("SCM did not reach Server readiness");
  evidence.steps.push("LocalService install and start");
  const adminToken = (await readFile(join(activeDir, "admin.token"))).toString("base64url");
  const admin = async (path, body) => {
    const r = await fetch(`http://127.0.0.1:${adminPort}/admin/v1/${path}`, {
      method: body === undefined ? "GET" : "POST",
      headers: { Authorization: `Bearer ${adminToken}`, "Content-Type": "application/json" },
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: AbortSignal.timeout(10000),
    });
    if (!r.ok) throw new Error(`Admin ${path}: ${r.status}`);
    return r.json();
  };
  const entity = await admin("entities", { name: "service acceptance fixture", enabled: true });
  const duplicate = invoke(original, activeDir, ["run", "start"], false);
  if (duplicate.status === 0) throw new Error("Background start duplicated a registered service");
  run("service", "restart");
  if ((await admin("health")).server_id !== init.server_id)
    throw new Error("Restart changed identity");
  evidence.steps.push("restart, identity and background exclusion");
  activeExe = candidate;
  const upgraded = run("runtime", "upgrade", "--service", "--start");
  if (!upgraded.status?.running || upgraded.status.server_id !== init.server_id)
    throw new Error("Service upgrade did not preserve a ready Server");
  if (!(await admin("entities")).some((e) => e.id === entity.id))
    throw new Error("Service upgrade lost state");
  evidence.steps.push("upgrade to different executable with persisted state");
  const moved = run("runtime", "migrate", "--to", dirs[1], "--service", "--start");
  activeDir = dirs[1];
  activeExe = moved.launcher;
  if (!moved.status?.running || moved.status.server_id !== init.server_id)
    throw new Error("Service migration did not preserve identity");
  if (!(await admin("entities")).some((e) => e.id === entity.id))
    throw new Error("Service migration lost state");
  evidence.steps.push("service path migration");
  run("service", "stop");
  run("service", "uninstall");
  if (run("service", "status").installed !== false)
    throw new Error("Service registration retained after uninstall");
  await readFile(join(activeDir, "state.sqlite"));
  await readFile(join(activeDir, "key"));
  evidence.steps.push("stop, uninstall and data retention");
  evidence.status = "passed";
  console.log("Passed Windows SCM lifecycle, upgrade, migration and uninstall retention.");
} catch (e) {
  evidence.reason = e.message;
  process.exitCode = 1;
  console.error(e.message);
} finally {
  for (const dir of dirs) {
    try {
      const actual = await realpath(dir);
      const within = relative(systemRoot, actual);
      if (
        !within.startsWith(`dbmcp-v1-acceptance-${id}-`) ||
        within.includes("\\") ||
        isAbsolute(within)
      )
        throw new Error("Unsafe cleanup path");
      const state = invoke(original, actual, ["service", "status"]);
      if (state.installed === true) invoke(original, actual, ["service", "uninstall"]);
      if (invoke(original, actual, ["service", "status"]).installed !== false)
        throw new Error("Cannot confirm service removal; keeping files");
      if (invoke(original, actual, ["run", "status"]).running === true)
        throw new Error("Server still running; keeping files");
      await rm(actual, { recursive: true, force: true });
      evidence.cleanup.push({ directory: actual, status: "removed" });
    } catch (e) {
      if (e.code !== "ENOENT") {
        evidence.cleanup.push({ directory: dir, status: "retained", reason: e.message });
        evidence.status = "failed";
        process.exitCode = 1;
      }
    }
  }
  await mkdir(join(root, "artifacts/validation"), { recursive: true });
  await writeFile(recordPath, JSON.stringify(evidence, null, 2) + "\n");
}
