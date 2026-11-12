import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { join } from "node:path";

export async function readServerVersion(root) {
  const source = await readFile(join(root, "server/internal/model/model.go"), "utf8");
  const version = source.match(/^const Version = "([^"]+)"$/m)?.[1];
  assert.match(
    version ?? "",
    /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/,
    "MCP Server must have a stable version",
  );
  return version;
}
