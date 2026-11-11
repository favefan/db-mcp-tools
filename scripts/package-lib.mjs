import { spawn } from "node:child_process";
import { createReadStream, createWriteStream } from "node:fs";
import { mkdir, readFile, readdir, realpath, rm, stat, writeFile } from "node:fs/promises";
import { createHash } from "node:crypto";
import { basename, dirname, isAbsolute, join, relative, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";
import { ZipArchive, TarArchive } from "archiver";

export const root = await realpath(fileURLToPath(new URL("..", import.meta.url)));
export const version = JSON.parse(
  await readFile(join(root, "client/package.json"), "utf8"),
).version;
export const scratch = join(root, ".cache", "package");
export const releases = join(root, "artifacts", "releases", version);
export const platforms = {
  "x86_64-pc-windows-msvc": ["windows", "amd64"],
  "aarch64-pc-windows-msvc": ["windows", "arm64"],
  "x86_64-apple-darwin": ["darwin", "amd64"],
  "aarch64-apple-darwin": ["darwin", "arm64"],
  "x86_64-unknown-linux-gnu": ["linux", "amd64"],
  "aarch64-unknown-linux-gnu": ["linux", "arm64"],
};
export const hostPlatform = { win32: "windows", darwin: "darwin", linux: "linux" }[
  process.platform
];
export const hostArch = { x64: "amd64", arm64: "arm64" }[process.arch];

export function options(allowed) {
  const result = {};
  for (const arg of process.argv.slice(2)) {
    if (arg === "--") continue;
    const found = /^--([a-z-]+)=(.+)$/.exec(arg);
    if (!found || !allowed.includes(found[1]) || result[found[1]])
      throw new Error(`Invalid option: ${arg}`);
    result[found[1]] = found[2];
  }
  return result;
}
export function run(executable, args, { cwd = root, env = process.env, quiet = false } = {}) {
  return new Promise((resolvePromise, reject) => {
    const child = spawn(executable, args, {
      cwd,
      env,
      windowsHide: true,
      stdio: ["ignore", "pipe", "pipe"],
    });
    let stdout = "",
      stderr = "";
    child.stdout.on("data", (b) => {
      stdout += b;
      if (!quiet) process.stdout.write(b);
    });
    child.stderr.on("data", (b) => {
      stderr += b;
      if (!quiet) process.stderr.write(b);
    });
    child.on("error", reject);
    child.on("close", (code) =>
      code === 0
        ? resolvePromise(stdout)
        : reject(
            new Error(
              `${basename(executable)} failed (${code})${quiet ? `: ${(stderr.trim() || stdout.trim()).slice(-6000)}` : "; see the command output above"}`,
            ),
          ),
    );
  });
}
export const pnpm = (args, settings) => {
  const cli = process.env.npm_execpath;
  if (!cli || !/pnpm/i.test(cli))
    throw new Error("Run this build through the repository's pnpm package script.");
  return run(process.execPath, [cli, ...args], settings);
};
export async function stage(name) {
  if (!/^[a-z0-9][a-z0-9._-]*$/.test(name)) throw new Error("Invalid package staging name");
  await mkdir(scratch, { recursive: true });
  const path = join(scratch, name);
  try {
    const actual = await realpath(path);
    const boundary = await realpath(scratch);
    const within = relative(boundary, actual);
    if (!within || within.startsWith("..") || isAbsolute(within))
      throw new Error("Unsafe package cleanup path");
    await rm(actual, { recursive: true });
  } catch (e) {
    if (e.code !== "ENOENT") throw e;
  }
  await mkdir(path, { recursive: true });
  await mkdir(releases, { recursive: true });
  return path;
}
export async function hash(path) {
  const digest = createHash("sha256");
  for await (const chunk of createReadStream(path)) digest.update(chunk);
  return digest.digest("hex");
}
export async function files(dir) {
  const result = [];
  for (const item of await readdir(dir, { withFileTypes: true })) {
    const path = join(dir, item.name);
    if (item.isDirectory()) result.push(...(await files(path)));
    else if (item.isFile()) result.push(path);
  }
  return result.sort();
}
export async function manifest(dir, details) {
  const hashes = {};
  for (const path of await files(dir)) {
    if (basename(path) !== "manifest.json")
      hashes[relative(dir, path).split(sep).join("/")] = await hash(path);
  }
  const value = {
    product: "db-mcp-tools",
    version,
    api_version: 1,
    built_at: new Date().toISOString(),
    signed: false,
    ...details,
    files: hashes,
  };
  await writeFile(join(dir, "manifest.json"), JSON.stringify(value, null, 2) + "\n");
  return value;
}
export async function archive(dir, output, platform) {
  const compressor =
    platform === "windows"
      ? new ZipArchive({ zlib: { level: 9 } })
      : new TarArchive({ gzip: true, gzipOptions: { level: 9 } });
  const stream = createWriteStream(output);
  const complete = new Promise((resolvePromise, reject) => {
    stream.on("close", resolvePromise);
    stream.on("error", reject);
    compressor.on("error", reject);
    compressor.on("warning", reject);
  });
  compressor.pipe(stream);
  compressor.directory(dir, basename(output).replace(/\.(zip|tar\.gz)$/, ""));
  await compressor.finalize();
  await complete;
}
export async function checksums() {
  const lines = [];
  for (const path of await files(releases)) {
    if (basename(path) !== "SHA256SUMS")
      lines.push(`${await hash(path)}  ${relative(releases, path).split(sep).join("/")}`);
  }
  await writeFile(join(releases, "SHA256SUMS"), lines.join("\n") + "\n");
}
export function validateServerTarget(platform, arch, dm) {
  if (
    !["windows", "linux", "darwin"].includes(platform) ||
    !["amd64", "arm64"].includes(arch) ||
    !["omit", "native"].includes(dm)
  )
    throw new Error("Unsupported Server target or DM selection");
}
export async function compileServer(output, platform, arch, dm) {
  validateServerTarget(platform, arch, dm);
  const source = await readFile(join(root, "server/internal/model/model.go"), "utf8");
  if (!source.includes(`const Version = "${version}"`))
    throw new Error("Go and client versions differ");
  await mkdir(dirname(output), { recursive: true });
  const env = { ...process.env, GOOS: platform, GOARCH: arch, CGO_ENABLED: "0" };
  await run(
    "go",
    [
      "build",
      "-trimpath",
      "-ldflags=-s -w",
      ...(dm === "native" ? ["-tags", "dm"] : []),
      "-o",
      resolve(output),
      "./cmd/dbmcp",
    ],
    { cwd: join(root, "server"), env },
  );
  let execution = "cross-compiled-only";
  if (platform === hostPlatform && arch === hostArch) {
    const actual = JSON.parse(await run(output, ["version"], { quiet: true }));
    if (
      actual.version !== version ||
      actual.api_version !== 1 ||
      actual.drivers.dm !== (dm === "native")
    )
      throw new Error("Server build capabilities differ from requested package");
    execution = "native-version-command-passed";
  }
  return {
    env,
    execution,
    drivers: { postgres: true, mysql: true, sqlserver: true, dm: dm === "native" },
  };
}
export async function copyDocs(dir) {
  const { copyFile, cp } = await import("node:fs/promises");
  for (const name of ["LICENSE", "RELEASE.md", "CHANGELOG.md", "CONTRIBUTING.md", "SECURITY.md"])
    await copyFile(join(root, name), join(dir, name));
  await cp(join(root, "docs"), join(dir, "docs"), { recursive: true });
  await cp(join(root, "third-party"), join(dir, "third-party"), { recursive: true });
  for (const name of ["client", "server"]) {
    await mkdir(join(dir, name), { recursive: true });
    await copyFile(join(root, name, "README.md"), join(dir, name, "README.md"));
  }
}
export async function exists(path) {
  try {
    return (await stat(path)).isFile();
  } catch {
    return false;
  }
}
