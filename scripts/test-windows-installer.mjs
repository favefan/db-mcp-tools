import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import {
  access,
  copyFile,
  mkdir,
  mkdtemp,
  readFile,
  realpath,
  rename,
  rm,
  writeFile,
} from "node:fs/promises";
import { dirname, isAbsolute, join, relative, resolve } from "node:path";
import { hash, options, root, run, version } from "./package-lib.mjs";

// Execute the real generated NSIS code with separate registry, shortcut,
// profile and installation identities. Never install over the user's app.
if (process.platform !== "win32") throw new Error("Windows acceptance only");
const args = options(["previous-script", "current-script", "report"]);
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
      .replace('"$LOCALAPPDATA\\${PRODUCTNAME}"', `"${oldDir}"`);
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
try {
  const previous = await fixture(resolve(args["previous-script"]), "previous", true);
  const current = await fixture(
    resolve(args["current-script"] ?? join(generated, "installer.nsi")),
    "current",
    false,
  );
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
  await writeFile(join(profile, "profile.json"), '{"preserved":true}');
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
  assert.equal(await readFile(join(profile, "profile.json"), "utf8"), '{"preserved":true}');
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
  assert.ok(records.every((v) => !Number.isNaN(Date.parse(v.time_utc))));
  report.migration_log = records;
  const payload = JSON.parse(await readFile(join(defaultDir, "dbmcp-docs/manifest.json"), "utf8"));
  for (const [name, expected] of Object.entries(payload.files))
    assert.equal(await hash(join(defaultDir, "dbmcp-docs", name)), expected, name);
  assert.equal(await exists(join(defaultDir, "dbmcp.exe")), payload.server_mode === "bundled");
  if (payload.server)
    assert.equal(await hash(join(defaultDir, "dbmcp.exe")), payload.server.sha256);
  report.resource_files = Object.keys(payload.files).length;
  report.server_mode = payload.server_mode;
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
