import { cp, mkdir, readFile, realpath, rm, writeFile } from "node:fs/promises";
import { createHash } from "node:crypto";
import { isAbsolute, join, relative } from "node:path";
import { root, run, stage } from "./package-lib.mjs";

const nsisHash = "ba63dffc4410ee89193e1cb5a41989991bd77c61068da17e3156d136b7b0b3d8";
const nsisExe = "6d45d4e8ccf18fe1fdac9164058f356102a2ecf2";
const utilitiesHash = "75197fee3c6a814fe035788d1c34ead39349b860";
async function matches(path, expected, algorithm = "sha1") {
  try {
    return (
      createHash(algorithm)
        .update(await readFile(path))
        .digest("hex") === expected
    );
  } catch {
    return false;
  }
}
async function asset(path, expected, algorithm, url) {
  if (await matches(path, expected, algorithm)) return;
  const response = await fetch(url, {
    headers: { "User-Agent": "db-mcp-tools", Accept: "application/octet-stream" },
    signal: AbortSignal.timeout(120000),
  });
  if (!response.ok || response.headers.get("content-type")?.includes("json"))
    throw new Error(`Official build-tool download failed: HTTP ${response.status}`);
  const bytes = Buffer.from(await response.arrayBuffer());
  if (createHash(algorithm).update(bytes).digest("hex") !== expected)
    throw new Error("Official build-tool checksum mismatch");
  await writeFile(path, bytes);
}

// Versions and hashes match the pinned Tauri CLI 2.12.4 bundler. The official
// asset API avoids this environment's 504 response on browser download URLs.
export async function windowsTools() {
  const tools = join(root, "client/src-tauri/target/.tauri/NSIS");
  const plugin = join(tools, "Plugins/x86-unicode/additional/nsis_tauri_utils.dll");
  if ((await matches(join(tools, "NSIS.exe"), nsisExe)) && (await matches(plugin, utilitiesHash)))
    return tools;
  const cache = join(root, ".cache/build-tools");
  await mkdir(cache, { recursive: true });
  const zip = join(cache, "nsis-3.13.zip");
  await asset(
    zip,
    nsisHash,
    "sha256",
    "https://api.github.com/repos/tauri-apps/binary-releases/releases/assets/623832696",
  );
  const metadata = await fetch(
    "https://api.github.com/repos/tauri-apps/nsis-tauri-utils/releases/tags/nsis_tauri_utils-v0.5.3",
    { headers: { "User-Agent": "db-mcp-tools" }, signal: AbortSignal.timeout(30000) },
  );
  if (!metadata.ok)
    throw new Error(`Official NSIS utility metadata failed: HTTP ${metadata.status}`);
  const info = (await metadata.json()).assets.find((v) => v.name === "nsis_tauri_utils.dll");
  if (!info) throw new Error("Pinned NSIS utility asset was not found");
  const dll = join(cache, "nsis-tauri-utils-0.5.3.dll");
  await asset(dll, utilitiesHash, "sha1", info.url);
  const unpacked = await stage("windows-nsis-tools");
  await run("tar", ["-xf", zip, "-C", unpacked], { quiet: true });
  try {
    const actual = await realpath(tools);
    const inside = relative(root, actual);
    if (!inside || inside.startsWith("..") || isAbsolute(inside))
      throw new Error("Unsafe build-tool cache replacement");
    await rm(actual, { recursive: true });
  } catch (e) {
    if (e.code !== "ENOENT") throw e;
  }
  await cp(join(unpacked, "nsis-3.13"), tools, { recursive: true });
  await mkdir(join(tools, "Plugins/x86-unicode/additional"), { recursive: true });
  await cp(dll, plugin);
  if (!(await matches(join(tools, "NSIS.exe"), nsisExe)) || !(await matches(plugin, utilitiesHash)))
    throw new Error("Extracted build tools failed verification");
  console.log("Prepared official NSIS 3.13 and Tauri utilities 0.5.3 with verified hashes.");
  return tools;
}
