import assert from "node:assert/strict";
import { appendFile, readFile } from "node:fs/promises";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";

export async function releaseVersion(root, tag) {
  assert.match(
    tag ?? "",
    /^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/,
    "Use a stable vMAJOR.MINOR.PATCH tag",
  );
  const version = tag.slice(1);
  const read = (path) => readFile(resolve(root, path), "utf8");
  for (const path of ["package.json", "client/package.json", "client/src-tauri/tauri.conf.json"])
    assert.equal(JSON.parse(await read(path)).version, version, `${path} does not match ${tag}`);
  const cargo = (await read("client/src-tauri/Cargo.toml"))
    .split(/^\[package\]\s*$/m)[1]
    ?.split(/^\[/m)[0];
  assert.equal(
    cargo?.match(/^version\s*=\s*"([^"]+)"\s*$/m)?.[1],
    version,
    "Cargo.toml version mismatch",
  );
  const lock = await read("client/src-tauri/Cargo.lock");
  assert.equal(
    lock.match(/^name = "dbmcp-client"\r?\nversion = "([^"]+)"$/m)?.[1],
    version,
    "Cargo.lock version mismatch",
  );
  const go = await read("server/internal/model/model.go");
  assert.equal(go.match(/^const Version = "([^"]+)"$/m)?.[1], version, "Go version mismatch");
  assert.equal(
    (await read("RELEASE.md")).split(/\r?\n/)[0],
    `# DB MCP Tools ${tag}`,
    "Update RELEASE.md for this tag",
  );
  assert.ok(
    (await read("CHANGELOG.md"))
      .split(/\r?\n/)
      .some((line) => line.startsWith(`## [${version}] - `)),
    "Missing changelog section",
  );
  return version;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const root = fileURLToPath(new URL("..", import.meta.url));
  const version = await releaseVersion(root, process.env.RELEASE_TAG);
  if (process.env.GITHUB_OUTPUT)
    await appendFile(process.env.GITHUB_OUTPUT, `version=${version}\n`);
  console.log(`Release tag and source versions agree: v${version}`);
}
