import { copyFile, mkdir, readFile, realpath, rm, writeFile } from "node:fs/promises";
import { basename, isAbsolute, join, relative } from "node:path";
import {
  checksums,
  compileServer,
  copyDocs,
  files,
  hash,
  hostPlatform,
  manifest,
  options,
  platforms,
  pnpm,
  releases,
  root,
  run,
  stage,
  validateServerTarget,
  version,
} from "./package-lib.mjs";
import { notices } from "./package-notices.mjs";
import { windowsTools } from "./package-windows-tools.mjs";
import { installerTemplate, windowsInstaller } from "./package-windows-installer.mjs";

const args = options(["server", "target", "dm"]);
const mode = args.server;
if (!["none", "bundled"].includes(mode))
  throw new Error("Select --server=none or --server=bundled explicitly.");
const target =
  args.target ?? /^host: (.+)$/m.exec(await run("rustc", ["-vV"], { quiet: true }))?.[1];
if (!platforms[target]) throw new Error(`Unsupported desktop target: ${target}`);
const [platform, arch] = platforms[target];
if (platform !== hostPlatform)
  throw new Error("Build Tauri on its native OS; a cross-compiled Server is packaged separately.");
const dm = args.dm ?? "omit";
validateServerTarget(platform, arch, dm);
if (mode === "none" && dm !== "omit")
  throw new Error("A pure client cannot contain a DM driver or Server.");
const dir = await stage(`client-${mode}-${target}${dm === "native" ? "-dm-local" : ""}`);
const payload = join(dir, "payload");
await mkdir(payload, { recursive: true });
let server;
let serverExecutable;
if (mode === "bundled") {
  console.log("Compiling the bundled Server.");
  const sidecar = join(
    root,
    "client/src-tauri/binaries",
    `dbmcp-${target}${platform === "windows" ? ".exe" : ""}`,
  );
  server = await compileServer(sidecar, platform, arch, dm);
  serverExecutable = sidecar;
  server.sha256 = await hash(sidecar);
} else {
  console.log("Building a pure client: Go compilation and Server resources are disabled.");
}
const license = await notices(join(payload, "licenses"), { client: true, target, server, dm });
if (platform === "windows") {
  const tools = await windowsTools();
  await mkdir(join(payload, "licenses/build-tools"), { recursive: true });
  await copyFile(join(tools, "COPYING"), join(payload, "licenses/build-tools/NSIS-COPYING"));
  const utilities = JSON.parse(await readFile(join(root, "third-party/notices.json"), "utf8")).find(
    (v) => v.id === "tooling:nsis-tauri-utils@0.5.3",
  );
  for (const file of utilities.files) {
    const source = join(root, "third-party", file.path);
    if ((await hash(source)) !== file.sha256)
      throw new Error("Installer utility license checksum mismatch");
    await copyFile(source, join(payload, "licenses/build-tools", basename(source)));
  }
  await writeFile(
    join(payload, "licenses/build-tools/provenance.json"),
    JSON.stringify(utilities, null, 2) + "\n",
  );
  await writeFile(
    join(payload, "licenses/build-tools/README.md"),
    "# Windows installer tools\n\nNSIS 3.13 COPYING and the exact release-tag licenses for Tauri utilities 0.5.3 are included. Provenance is recorded separately. Build tool assets are checked against hashes pinned by Tauri CLI 2.12.4. The official CLI 2.12.4 NSIS template is adapted under MIT; its source and checksum are in template-provenance.json.\n",
  );
  await copyFile(
    join(root, "third-party/upstream/tauri/LICENSE-MIT"),
    join(payload, "licenses/build-tools/TAURI-LICENSE-MIT"),
  );
  await writeFile(
    join(payload, "licenses/build-tools/template-provenance.json"),
    JSON.stringify(installerTemplate, null, 2) + "\n",
  );
}
await copyDocs(payload);
await writeFile(
  join(payload, "README.md"),
  `# DB MCP Tools ${version}\n\nThis is the ${mode === "bundled" ? "client with a bundled Server" : "pure client (no Go executable)"}. See docs/user-guide.md. The build is unsigned.\n`,
);
await manifest(payload, {
  kind: "client-resources",
  platform,
  arch,
  server_mode: mode,
  server: server
    ? {
        version: server.version,
        drivers: server.drivers,
        sha256: server.sha256,
        execution_validation: server.execution,
      }
    : null,
  licenses: license,
  distribution: dm === "native" ? "local-use-only-dm-license-unconfirmed" : "unsigned-local-build",
});
const bundles =
  platform === "windows" ? ["nsis"] : platform === "darwin" ? ["dmg"] : ["deb", "appimage"];
const override = {
  bundle: {
    targets: bundles,
    externalBin: mode === "bundled" ? ["binaries/dbmcp"] : [],
    resources: { [payload]: "dbmcp-docs/" },
    licenseFile: join(root, "LICENSE"),
  },
};
if (platform === "windows") {
  override.bundle.windows = { nsis: await windowsInstaller(dir, serverExecutable) };
}
const config = join(dir, "tauri.package.json");
await writeFile(config, JSON.stringify(override, null, 2) + "\n");
// Tauri copies resources beside the binary. Clear this build-owned tree so a
// previous package mode cannot retain stale resources or read-only notices.
const resourceTarget = join(root, "client/src-tauri/target", target, "release/dbmcp-docs");
try {
  const actual = await realpath(resourceTarget);
  const within = relative(join(root, "client/src-tauri/target"), actual);
  if (!within || within.startsWith("..") || isAbsolute(within))
    throw new Error("Unsafe resource cleanup path");
  await rm(actual, { recursive: true, force: true });
} catch (e) {
  if (e.code !== "ENOENT") throw e;
}
await pnpm(
  [
    "--dir",
    "client",
    "tauri",
    "build",
    "--target",
    target,
    "--config",
    config,
    ...(mode === "bundled" ? ["--features", "bundled-server"] : []),
  ],
  { env: { ...process.env, CARGO_TARGET_DIR: join(root, "client/src-tauri/target") } },
);
const buildDir = join(root, "client/src-tauri/target", target, "release");
const app = join(buildDir, platform === "windows" ? "dbmcp-client.exe" : "dbmcp-client");
const expectedExt =
  platform === "windows" ? [".exe"] : platform === "darwin" ? [".dmg"] : [".deb", ".AppImage"];
const installers = (await files(join(buildDir, "bundle"))).filter(
  (path) => expectedExt.some((ext) => path.endsWith(ext)) && basename(path).includes(version),
);
if (installers.length !== bundles.length)
  throw new Error(`Expected ${bundles.join("/")} installers; found ${installers.length}.`);
const name = `db-mcp-tools-client-${mode}-${version}-${platform}-${arch}${dm === "native" ? "-dm-local" : ""}`;
const outputs = {};
const sourceMaps = {};
for (const source of (await files(join(root, "client/dist"))).filter((path) =>
  path.endsWith(".map"),
)) {
  const destination = join(releases, "source-maps", basename(source));
  await mkdir(join(releases, "source-maps"), { recursive: true });
  await copyFile(source, destination);
  sourceMaps[`source-maps/${basename(source)}`] = await hash(destination);
}
for (const source of installers) {
  const extension =
    platform === "windows"
      ? "setup.exe"
      : source.endsWith(".AppImage")
        ? "AppImage"
        : source.endsWith(".dmg")
          ? "dmg"
          : "deb";
  const destination = join(releases, `${name}.${extension}`);
  await copyFile(source, destination);
  outputs[basename(destination)] = await hash(destination);
  console.log(`Created ${destination}`);
}
const packaged = {
  product: "db-mcp-tools",
  version,
  api_version: 1,
  kind: "client",
  platform,
  arch,
  target,
  server_mode: mode,
  server: server
    ? { version: server.version, drivers: server.drivers, sha256: server.sha256 }
    : null,
  // Tauri restores this binary after adding per-installer bundle metadata.
  // It is not the byte hash of the executable extracted from an installer.
  unbundled_app_sha256: await hash(app),
  signed: false,
  licenses: license,
  installer_files: outputs,
  frontend_source_maps: sourceMaps,
  built_at: new Date().toISOString(),
  distribution: dm === "native" ? "local-use-only-dm-license-unconfirmed" : "unsigned-local-build",
};
await writeFile(join(releases, `${name}.manifest.json`), JSON.stringify(packaged, null, 2) + "\n");
await checksums();
