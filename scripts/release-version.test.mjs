import assert from "node:assert/strict";
import { copyFile, mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";
import { releaseVersion } from "./release-version.mjs";

const source = fileURLToPath(new URL("..", import.meta.url));
const paths = [
  "package.json",
  "client/package.json",
  "client/src-tauri/tauri.conf.json",
  "client/src-tauri/Cargo.toml",
  "client/src-tauri/Cargo.lock",
  "server/internal/model/model.go",
  "RELEASE.md",
  "CHANGELOG.md",
];
const version = JSON.parse(await readFile(join(source, "package.json"), "utf8")).version;

test("release accepts the current snapshot and rejects unsafe or nonstable tags", async () => {
  assert.equal(await releaseVersion(source, `v${version}`), version);
  for (const tag of [undefined, version, "v01.0.9", "v1.0.9-beta.1", "v1.0.9;echo bad"])
    await assert.rejects(releaseVersion(source, tag), /stable vMAJOR.MINOR.PATCH/);
});

test("any stale version or release document blocks publication", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "dbmcp-release-"));
  try {
    for (const path of paths) {
      await mkdir(dirname(join(root, path)), { recursive: true });
      await copyFile(join(source, path), join(root, path));
    }
    for (const path of paths)
      await t.test(path, async () => {
        const file = join(root, path);
        const original = await readFile(file, "utf8");
        await writeFile(file, original.replaceAll(version, "999.0.0"));
        try {
          await assert.rejects(releaseVersion(root, `v${version}`));
        } finally {
          await writeFile(file, original);
        }
      });
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});
