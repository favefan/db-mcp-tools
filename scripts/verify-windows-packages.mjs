import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { isAbsolute, join, relative, resolve } from "node:path";
import { hash, releases, root, version, serverVersion } from "./package-lib.mjs";

// Verify fresh default Windows outputs. Historical DM-local releases are not inputs.
const server = JSON.parse(
  await readFile(
    join(root, ".cache/package", `dbmcp-server-${serverVersion}-windows-amd64`, "manifest.json"),
    "utf8",
  ),
);
assert.equal(server.version, serverVersion);
assert.equal(server.drivers.dm, false);
for (const mode of ["none", "bundled"]) {
  const name = `db-mcp-tools-client-${mode}-${version}-windows-amd64`;
  const manifest = JSON.parse(await readFile(join(releases, `${name}.manifest.json`), "utf8"));
  assert.equal(manifest.version, version);
  assert.equal(manifest.server_mode, mode);
  assert.equal(manifest.signed, false);
  if (mode === "none") assert.equal(manifest.server, null);
  else {
    assert.equal(manifest.server.version, serverVersion);
    assert.equal(manifest.server.drivers.dm, false);
  }
  const config = JSON.parse(
    await readFile(
      join(root, ".cache/package", `client-${mode}-x86_64-pc-windows-msvc`, "tauri.package.json"),
      "utf8",
    ),
  );
  assert.deepEqual(config.bundle.externalBin, mode === "none" ? [] : ["binaries/dbmcp"]);
  assert.equal(Object.keys(manifest.installer_files).length, 1);
  for (const [file, expected] of Object.entries(manifest.installer_files))
    assert.equal(await hash(join(releases, file)), expected, file);
}
const sums = await readFile(join(releases, "SHA256SUMS"), "utf8");
for (const line of sums.trim().split(/\r?\n/)) {
  const match = /^([a-f0-9]{64})  (.+)$/.exec(line);
  assert.ok(match, "Invalid SHA256SUMS entry");
  const path = resolve(releases, match[2]);
  const within = relative(releases, path);
  assert.ok(within && !within.startsWith("..") && !isAbsolute(within));
  assert.equal(await hash(path), match[1], match[2]);
}
console.log(
  "Verified Windows default package versions, declared Server boundaries and SHA256SUMS.",
);
