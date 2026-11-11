import { spawnSync } from "node:child_process";
import {
  mkdirSync,
  mkdtempSync,
  realpathSync,
  rmSync,
  cpSync,
  copyFileSync,
  writeFileSync,
} from "node:fs";
import { createServer } from "node:net";
import { resolve, join, sep } from "node:path";

if (Number(process.versions.node.split(".")[0]) !== 24)
  throw new Error("Runtime smoke requires the repository's Node 24 toolchain.");
const root = realpathSync(new URL("..", import.meta.url));
const tempRoot = join(root, ".tmp");
mkdirSync(tempRoot, { recursive: true });
const dir = mkdtempSync(join(tempRoot, "运行 smoke "));
const migratedDir = mkdtempSync(join(tempRoot, "迁移 smoke "));
const executable = resolve(
  root,
  "server/bin",
  process.platform === "win32" ? "dbmcp.exe" : "dbmcp",
);
let activeExecutable = executable;
let activeDir = dir;
function run(...args) {
  const result = spawnSync(activeExecutable, ["--data-dir", activeDir, ...args], {
    encoding: "utf8",
    windowsHide: true,
  });
  if (result.error || result.status !== 0) throw new Error(result.stderr || String(result.error));
  return JSON.parse(result.stdout);
}
try {
  const init = run("init");
  const listeners = [createServer(), createServer()];
  try {
    for (const listener of listeners)
      await new Promise((resolve, reject) => {
        listener.once("error", reject);
        listener.listen(0, "127.0.0.1", resolve);
      });
    writeFileSync(
      join(dir, "deployment.toml"),
      `admin='127.0.0.1:${listeners[0].address().port}'\nmcp='127.0.0.1:${listeners[1].address().port}'\n`,
    );
  } finally {
    for (const listener of listeners)
      if (listener.listening) await new Promise((resolve) => listener.close(resolve));
  }
  const deployment = run("runtime", "deploy");
  activeExecutable = deployment.launcher;
  const version = run("version");
  if (version.api_version !== 1)
    throw new Error("stable launcher failed to call the selected executable");
  const started = run("run", "start");
  if (!started.running || started.mode !== "background" || started.server_id !== init.server_id)
    throw new Error("runtime identity mismatch");
  const duplicate = spawnSync(executable, ["--data-dir", dir, "run", "start"], {
    windowsHide: true,
  });
  if (duplicate.status === 0) throw new Error("duplicate Server accepted");
  const clone = join(dir, "cloned data");
  cpSync(deployment.backup, clone, { recursive: true });
  copyFileSync(join(dir, "deployment.toml"), join(clone, "deployment.toml"));
  const foreign = spawnSync(activeExecutable, ["--data-dir", clone, "run", "stop"], {
    encoding: "utf8",
    windowsHide: true,
  });
  if (foreign.status === 0 || !foreign.stderr.includes("different data directory"))
    throw new Error("copied credentials controlled another data directory");
  if (!run("run", "status").running) throw new Error("ownership check stopped the original Server");
  run("run", "stop");
  const unchanged = run("runtime", "deploy");
  if (!unchanged.unchanged) throw new Error("same build redeployed unexpectedly");
  const restarted = run("run", "start");
  if (restarted.server_id !== init.server_id) throw new Error("server identity lost after restart");
  const candidate = join(dir, "candidate", process.platform === "win32" ? "dbmcp.exe" : "dbmcp");
  mkdirSync(join(dir, "candidate"));
  const build = spawnSync(
    "go",
    [
      "-C",
      "server",
      "build",
      "-ldflags=-buildid=dbmcp-smoke-upgrade",
      "-o",
      candidate,
      "./cmd/dbmcp",
    ],
    { cwd: root, encoding: "utf8", windowsHide: true },
  );
  if (build.error || build.status !== 0) throw new Error(build.stderr || String(build.error));
  activeExecutable = candidate;
  const upgraded = run("runtime", "upgrade");
  if (
    !upgraded.status?.running ||
    upgraded.status.server_id !== init.server_id ||
    upgraded.executable === deployment.executable
  )
    throw new Error("upgrade did not switch the executable and preserve state");
  activeExecutable = deployment.launcher;
  run("run", "stop");
  run("runtime", "rollback-program");
  if (!run("run", "start").running) throw new Error("previous executable could not resume");
  const migrated = run("runtime", "migrate", "--to", migratedDir, "--start");
  activeExecutable = migrated.launcher;
  activeDir = migratedDir;
  if (
    !migrated.status?.running ||
    migrated.status.server_id !== init.server_id ||
    !migrated.source_backup
  )
    throw new Error("migration lost the source snapshot or Server identity");
  run("run", "stop");
  console.log(
    "Passed stable launcher, ownership checks, upgrade to a different build, program rollback, state migration and background restart with Chinese/spaced paths.",
  );
} finally {
  for (const dataDir of [dir, migratedDir]) {
    const stop = spawnSync(executable, ["--data-dir", dataDir, "run", "stop"], {
      encoding: "utf8",
      windowsHide: true,
    });
    if (stop.status !== 0 && !stop.stderr.includes("different data directory"))
      throw new Error(stop.stderr || "test Server cleanup failed");
  }
  for (const dataDir of [dir, migratedDir]) {
    const target = realpathSync(dataDir);
    if (!target.startsWith(realpathSync(tempRoot) + sep))
      throw new Error("unsafe temporary cleanup target");
    rmSync(target, { recursive: true, force: true });
  }
}
