import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import {
  access,
  copyFile,
  mkdir,
  mkdtemp,
  readFile,
  readdir,
  realpath,
  rename,
  rm,
  writeFile,
} from "node:fs/promises";
import { dirname, isAbsolute, join, relative, resolve } from "node:path";
import { hash, options, root, run, version, serverVersion } from "./package-lib.mjs";

// Execute the real generated NSIS code with separate registry, shortcut,
// profile and installation identities. Never install over the user's app.
if (process.platform !== "win32") throw new Error("Windows acceptance only");
const args = options(["previous-script", "current-script", "previous-server", "report"]);
if (!args["previous-script"]) throw new Error("Supply a historical generated --previous-script");
const generated = join(root, "client/src-tauri/target/x86_64-pc-windows-msvc/release/nsis/x64");
const parent = join(root, ".cache/validation");
await mkdir(parent, { recursive: true });
const folder = await mkdtemp(join(parent, "installer-native-"));
const id = folder.split("-").at(-1);
const product = `DBMCP Installer Validation ${id}`;
const publisher = `DBMCP Installer Validation ${id}`;
const bundle = `local.dbmcp.installer-validation-${id}`;
const oldDir = join(folder, "迁移验收", "DB MCP Tools");
const defaultDir = join(dirname(oldDir), "db-mcp-tools");
const customDir = join(folder, "custom-installation");
const profile = join(process.env.APPDATA, bundle);
const local = join(process.env.LOCALAPPDATA, bundle);
const uninstallKey = `HKCU:\\Software\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\${product}`;
const productKey = `HKCU:\\Software\\${publisher}\\${product}`;
const runKey = "HKCU:\\Software\\Microsoft\\Windows\\CurrentVersion\\Run";
const quote = (s) => `'${s.replaceAll("'", "''")}'`;
const ps = (script) =>
  run(
    "powershell.exe",
    [
      "-NoProfile",
      "-NonInteractive",
      "-EncodedCommand",
      Buffer.from(
        "[Console]::OutputEncoding=[Text.UTF8Encoding]::new($false);$ProgressPreference='SilentlyContinue';" +
          script,
        "utf16le",
      ).toString("base64"),
    ],
    { quiet: true },
  );
const reg = async (key, name) =>
  JSON.parse(
    await ps(
      `$v=[Microsoft.Win32.Registry]::GetValue(${quote(key.replace("HKCU:\\", "HKEY_CURRENT_USER\\"))},${quote(name)},$null); if($null -eq $v){'null'}else{ConvertTo-Json -InputObject $v -Compress}`,
    ),
  );
const exists = (path) =>
  access(path).then(
    () => true,
    () => false,
  );
async function removeTree(path, boundary = folder) {
  if (!(await exists(path))) return;
  const actual = await realpath(path);
  const boundaries = [await realpath(boundary)];
  // A packaged Codex process can redirect an AppData child to its MSIX cache
  // while realpath(AppData) itself remains unchanged.
  const family = process.env.CODEX_WINDOWS_SANDBOX_PACKAGE_FAMILY;
  if (
    family &&
    /^[\w.-]+$/.test(family) &&
    [process.env.APPDATA, process.env.LOCALAPPDATA].includes(boundary)
  )
    boundaries.push(
      join(
        process.env.LOCALAPPDATA,
        "Packages",
        family,
        "LocalCache",
        boundary === process.env.APPDATA ? "Roaming" : "Local",
      ),
    );
  if (
    !boundaries.some((base) => {
      const inside = relative(base, actual);
      return inside && !inside.startsWith("..") && !isAbsolute(inside);
    })
  )
    throw new Error(`Unsafe fixture cleanup path: ${actual}; boundary=${boundary}`);
  await rm(path, { recursive: true, force: true });
}
const report = {
  at: new Date().toISOString(),
  version,
  status: "failed",
  checks: [],
  cleanup: "pending",
};
const testServers = [];
const legacyProfile = JSON.stringify({
  endpoint: "http://127.0.0.1:19876",
  server_id: "preserved",
});
const check = (name) => {
  report.checks.push(name);
  console.log(name);
};
async function install(file, flags = []) {
  const exit = await new Promise((done, fail) => {
    const child = spawn(file, ["/S", ...flags], {
      windowsHide: true,
      windowsVerbatimArguments: true,
      stdio: "ignore",
    });
    const timer = setTimeout(() => {
      child.kill();
      fail(new Error("Installer timed out"));
    }, 60000);
    child.once("error", fail);
    child.once("close", (code) => {
      clearTimeout(timer);
      done(code);
    });
  });
  return exit;
}
async function fixture(input, name, legacy) {
  let source = await readFile(input, "utf8");
  source = source
    .replace(/!define PRODUCTNAME "[^"]*"/, `!define PRODUCTNAME "${product}"`)
    .replace(/!define MANUFACTURER "[^"]*"/, `!define MANUFACTURER "${publisher}"`)
    .replace(/!define BUNDLEID "[^"]*"/, `!define BUNDLEID "${bundle}"`)
    .replace(/!define STARTMENUFOLDER "[^"]*"/, `!define STARTMENUFOLDER "${product}"`)
    .replace(/!define OUTFILE "[^"]*"/, `!define OUTFILE "${join(folder, name + ".exe")}"`)
    .replace('SetCompressor /SOLID "lzma"', "SetCompress off");
  if (legacy) {
    source = source
      .replace(/!include "[^"\r\n]*installer-hooks\.nsh"\r?\n/, "")
      .replace(/^\s*Call Dbmcp\w+\r?\n/gm, "")
      .replace('"$LOCALAPPDATA\\${PRODUCTNAME}"', `"${oldDir}"`)
      .replace('"$LOCALAPPDATA\\db-mcp-tools"', `"${oldDir}"`);
  } else {
    source = source.replace('"$LOCALAPPDATA\\db-mcp-tools"', `"${defaultDir}"`);
    const match = /!include "([^"\r\n]*installer-hooks\.nsh)"/.exec(source);
    assert.ok(match, "Current installer must use the production hooks");
    const hook = join(folder, "current-hooks.nsh");
    await copyFile(match[1], hook);
    source = source.replace(match[0], `!include "${hook}"`);
  }
  const path = join(folder, `${name}.nsi`);
  await writeFile(path, source);
  await run(
    join(root, "client/src-tauri/target/.tauri/NSIS/makensis.exe"),
    ["/V2", "/NOCD", "/INPUTCHARSET", "UTF8", path],
    { cwd: generated, quiet: true },
  );
  return join(folder, `${name}.exe`);
}
const paths = JSON.parse(
  await ps(
    "[pscustomobject]@{programs=[Environment]::GetFolderPath('Programs');desktop=[Environment]::GetFolderPath('DesktopDirectory')} | ConvertTo-Json -Compress",
  ),
);
const menu = join(paths.programs, product, `${product}.lnk`);
const desktop = join(paths.desktop, `${product}.lnk`);
const shortcutTarget = async (path) =>
  (
    await ps(`(New-Object -ComObject WScript.Shell).CreateShortcut(${quote(path)}).TargetPath`)
  ).trim();
async function removeInstalled() {
  const path = await reg(productKey, "");
  if (path && (await exists(join(path, "uninstall.exe"))))
    assert.equal(await install(join(path, "uninstall.exe"), [`_?=${path}`]), 0);
}
async function testBundledUpgrade() {
  const previous = resolve(args["previous-server"]);
  const current = join(defaultDir, "dbmcp.exe");
  const call = async (exe, data, ...args) =>
    JSON.parse(await run(exe, ["--data-dir", data, ...args], { quiet: true }));
  const oldBuild = JSON.parse(await run(previous, ["version"], { quiet: true }));
  assert.notEqual(oldBuild.version, serverVersion, "Supply an older server build");
  const fixtures = [];
  for (const [name, running] of [
    ["managed-running", true],
    ["managed-stopped", false],
    ["http", true],
    ["external", true],
  ]) {
    const data = join(folder, "server-data", name);
    testServers.push({ executable: previous, data });
    const initialized = await call(current, data, "init", "--allocate-ports");
    await call(previous, data, "runtime", "deploy");
    if (running) await call(previous, data, "run", "start");
    const before = await call(previous, data, "run", "status");
    fixtures.push({
      name,
      data,
      before,
      initialized,
      credential: await hash(join(data, "admin.token")),
      backups: (await readdir(join(data, "backups"))).length,
    });
  }
  const store = {
    version: 1,
    active_id: "managed-running",
    profiles: fixtures.map((item) => ({
      id: item.name,
      name: item.name,
      mode: item.name === "http" ? "existing" : "local",
      endpoint: `http://${item.initialized.admin}`,
      executable:
        item.name === "http"
          ? ""
          : item.name === "external"
            ? previous
            : join(item.data, "runtime/dbmcp.exe"),
      data_dir: item.name === "http" ? "" : item.data,
      server_id: item.initialized.server_id,
      preferences: { theme: "dark" },
    })),
  };
  await writeFile(join(profile, "profiles.json"), JSON.stringify(store));
  assert.equal(await install(currentInstaller, ["/UPDATE", "/NS"]), 0);
  for (const item of fixtures) {
    const status = await call(current, item.data, "run", "status");
    const managed = item.name.startsWith("managed");
    if (managed) {
      const build = JSON.parse(
        await run(join(item.data, "runtime/dbmcp.exe"), ["version"], { quiet: true }),
      );
      assert.equal(build.version, serverVersion);
      assert.equal(status.running, item.before.running);
      assert.equal((await readdir(join(item.data, "backups"))).length, item.backups + 1);
      if (status.running) assert.equal(status.server_id, item.before.server_id);
    } else {
      assert.equal(status.version, oldBuild.version);
      assert.equal(status.instance_id, item.before.instance_id);
      assert.equal((await readdir(join(item.data, "backups"))).length, item.backups);
    }
    assert.equal(await hash(join(item.data, "admin.token")), item.credential);
  }
  assert.deepEqual(JSON.parse(await readFile(join(profile, "profiles.json"), "utf8")), store);
  const running = await call(current, fixtures[0].data, "run", "status");
  assert.equal(await install(currentInstaller, ["/UPDATE", "/NS"]), 0);
  assert.equal(
    (await call(current, fixtures[0].data, "run", "status")).instance_id,
    running.instance_id,
  );
  for (const item of fixtures)
    assert.equal(
      (await readdir(join(item.data, "backups"))).length,
      item.backups + (item.name.startsWith("managed") ? 1 : 0),
    );
  check(
    "bundled installer upgrades running and stopped managed servers, retains HTTP/external processes, credentials and profiles, and skips repeat upgrades",
  );
  // An older client can have launched the bundled executable directly. It
  // must be moved out of the install directory before NSIS overwrites it.
  const direct = join(folder, "server-data", "bundled-direct");
  testServers.push({ executable: previous, data: direct });
  const initialized = await call(current, direct, "init", "--allocate-ports");
  await call(current, direct, "run", "start");
  store.profiles.push({
    id: "bundled-direct",
    name: "bundled-direct",
    mode: "local",
    endpoint: `http://${initialized.admin}`,
    executable: current,
    data_dir: direct,
    server_id: initialized.server_id,
    preferences: { theme: "dark" },
  });
  await writeFile(join(profile, "profiles.json"), JSON.stringify(store));
  assert.equal(await install(currentInstaller, ["/UPDATE", "/NS"]), 0);
  const directStatus = await call(current, direct, "run", "status");
  assert.equal(directStatus.running, true);
  assert.equal(directStatus.server_id, initialized.server_id);
  assert.equal(directStatus.version, serverVersion);
  assert.equal(await exists(join(direct, "runtime/current.json")), true);
  assert.equal(await exists(join(direct, "backups")), false);
  const updated = JSON.parse(await readFile(join(profile, "profiles.json"), "utf8"));
  assert.equal(
    resolve(updated.profiles.find((p) => p.id === "bundled-direct").executable),
    resolve(direct, "runtime/dbmcp.exe"),
  );
  check(
    "directly running bundled executable moves to stable runtime before replacement without a same-version backup",
  );
  const manifestPath = join(fixtures[1].data, "runtime/current.json");
  const manifest = await readFile(manifestPath, "utf8");
  await writeFile(manifestPath, "invalid");
  assert.equal(await install(currentInstaller, ["/UPDATE", "/NS"]), 2);
  await writeFile(manifestPath, manifest);
  assert.equal(await install(currentInstaller, ["/UPDATE", "/NS"]), 0);
  check(
    "failed server preflight returns installer failure and supports retry without replacing data",
  );
  await rm(join(profile, "profiles.json"));
}
let currentInstaller;
try {
  const previous = await fixture(resolve(args["previous-script"]), "previous", true);
  const current = await fixture(
    resolve(args["current-script"] ?? join(generated, "installer.nsi")),
    "current",
    false,
  );
  currentInstaller = current;
  assert.equal(await reg(uninstallKey, "DisplayVersion"), null);
  assert.equal(await install(current, ["/NS"]), 0);
  assert.equal(await reg(productKey, ""), defaultDir);
  assert.equal(await reg(uninstallKey, "DisplayVersion"), version);
  assert.equal(await exists(menu), false);
  assert.equal(await exists(desktop), false);
  check("fresh default directory and /NS");
  await removeInstalled();
  // Removing in place leaves the uninstaller itself; remove only this fixture.
  await removeTree(defaultDir);
  assert.equal(await install(previous, [`/D=${oldDir}`]), 0);
  assert.equal(await shortcutTarget(menu), join(oldDir, "dbmcp-client.exe"));
  assert.equal(await shortcutTarget(desktop), join(oldDir, "dbmcp-client.exe"));
  await mkdir(profile, { recursive: true });
  await mkdir(local, { recursive: true });
  await writeFile(join(profile, "profile.json"), legacyProfile);
  await writeFile(join(local, "webview-sentinel"), "preserved");
  await writeFile(join(oldDir, "user-file.txt"), "preserved");
  await ps(
    `New-ItemProperty -LiteralPath ${quote(runKey)} -Name ${quote(product)} -Value ${quote(`"${join(oldDir, "dbmcp-client.exe")}" --tray`)} -PropertyType String -Force | Out-Null`,
  );
  await mkdir(defaultDir, { recursive: true });
  await writeFile(join(defaultDir, "unrelated.txt"), "do not overwrite");
  assert.equal(await install(current), 2);
  assert.equal(await reg(productKey, ""), oldDir);
  assert.equal(await exists(join(oldDir, "dbmcp-client.exe")), true);
  assert.equal(await readFile(join(defaultDir, "unrelated.txt"), "utf8"), "do not overwrite");
  check("nonempty target rejected before removing old app");
  await removeTree(defaultDir);
  const oldUninstaller = join(oldDir, "uninstall.exe");
  await rename(oldUninstaller, oldUninstaller + ".saved");
  assert.equal(await install(current), 2);
  assert.equal(await exists(join(oldDir, "dbmcp-client.exe")), true);
  await rename(oldUninstaller + ".saved", oldUninstaller);
  check("missing uninstaller rejected without removing old app");
  assert.equal(await install(current, [`/D=${join(oldDir, "child")}`]), 2);
  check("nested migration target rejected");
  // Simulate a copy interrupted before committing the old uninstaller.
  await writeFile(join(defaultDir, ".dbmcp-install-migration"), oldDir, "utf16le");
  await writeFile(join(defaultDir, "partial-copy.txt"), "staged");
  assert.equal(await install(current, ["/UPDATE", "/NS"]), 0);
  assert.equal(await reg(productKey, ""), defaultDir);
  assert.equal(await reg(uninstallKey, "DisplayVersion"), version);
  assert.equal(
    await reg(uninstallKey, "UninstallString"),
    `"${join(defaultDir, "uninstall.exe")}"`,
  );
  assert.equal(await exists(join(oldDir, "dbmcp-client.exe")), false);
  assert.equal(await exists(oldUninstaller), false);
  assert.equal(await readFile(join(oldDir, "user-file.txt"), "utf8"), "preserved");
  assert.equal(await readFile(join(profile, "profile.json"), "utf8"), legacyProfile);
  assert.equal(await readFile(join(local, "webview-sentinel"), "utf8"), "preserved");
  assert.equal(await shortcutTarget(menu), join(defaultDir, "dbmcp-client.exe"));
  assert.equal(await shortcutTarget(desktop), join(defaultDir, "dbmcp-client.exe"));
  assert.equal(await reg(runKey, product), `"${join(defaultDir, "dbmcp-client.exe")}" --tray`);
  assert.equal(await exists(join(defaultDir, ".dbmcp-install-migration")), false);
  const records = (await readFile(join(local, "logs/installer-migration.log"), "utf8"))
    .trim()
    .split(/\r?\n/)
    .map(JSON.parse);
  assert.ok(
    records.some((v) => v.component === "installer" && v.msg.includes("migration completed")),
  );
  assert.ok(records.some((v) => v.msg.includes("迁移验收")));
  assert.ok(
    records.every((v) => Number.isFinite(v.time_unix_ms) || !Number.isNaN(Date.parse(v.time_utc))),
  );
  report.migration_log = records;
  const payload = JSON.parse(await readFile(join(defaultDir, "dbmcp-docs/manifest.json"), "utf8"));
  for (const [name, expected] of Object.entries(payload.files))
    assert.equal(await hash(join(defaultDir, "dbmcp-docs", name)), expected, name);
  assert.equal(await exists(join(defaultDir, "dbmcp.exe")), payload.server_mode === "bundled");
  if (payload.server)
    assert.equal(await hash(join(defaultDir, "dbmcp.exe")), payload.server.sha256);
  report.resource_files = Object.keys(payload.files).length;
  report.server_mode = payload.server_mode;
  if (payload.server_mode === "bundled" && args["previous-server"]) await testBundledUpgrade();
  check("migration retry, data preservation, shortcuts, quoted autostart and UTF-8 diagnostics");
  assert.equal(await install(current, ["/UPDATE", "/NS"]), 0);
  assert.equal(await reg(productKey, ""), defaultDir);
  check("repeat upgrade keeps new directory");
  await removeInstalled();
  await removeTree(defaultDir);
  assert.equal(await install(previous, [`/D=${oldDir}`]), 0);
  await ps(
    `New-ItemProperty -LiteralPath ${quote(runKey)} -Name ${quote(product)} -Value ${quote(`${join(oldDir, "dbmcp-client.exe")} --tray`)} -PropertyType String -Force | Out-Null`,
  );
  assert.equal(await install(current, [`/D=${customDir}`]), 0);
  assert.equal(await reg(productKey, ""), customDir);
  assert.equal(await reg(runKey, product), `"${join(customDir, "dbmcp-client.exe")}" --tray`);
  check("explicit /D migration and unquoted legacy autostart");
  assert.equal(await install(current, ["/UPDATE"]), 0);
  assert.equal(await reg(productKey, ""), customDir);
  check("custom installation directory retained");
  report.status = "passed";
} catch (error) {
  report.error = String(error);
  report.migration_log_raw = await readFile(
    join(local, "logs/installer-migration.log"),
    "utf8",
  ).catch(() => null);
  console.error(error);
  throw error;
} finally {
  for (const target of testServers) {
    await run(target.executable, ["--data-dir", target.data, "run", "stop"], { quiet: true });
  }
  await removeInstalled();
  await ps(
    `Remove-Item -LiteralPath ${quote(uninstallKey)},${quote(`HKCU:\\Software\\${publisher}`)} -Recurse -Force -ErrorAction SilentlyContinue; Remove-ItemProperty -LiteralPath ${quote(runKey)} -Name ${quote(product)} -ErrorAction SilentlyContinue; exit 0`,
  );
  for (const path of [folder, profile, local, dirname(menu)]) {
    if (!(await exists(path))) continue;
    const expected =
      path === folder
        ? parent
        : path === profile
          ? process.env.APPDATA
          : path === local
            ? process.env.LOCALAPPDATA
            : paths.programs;
    await removeTree(path, expected);
  }
  await rm(desktop, { force: true });
  report.cleanup = "completed";
  if (args.report) await writeFile(resolve(args.report), JSON.stringify(report, null, 2) + "\n");
}
