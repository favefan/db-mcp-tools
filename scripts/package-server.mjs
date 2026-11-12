import { writeFile } from "node:fs/promises";
import { join } from "node:path";
import {
  archive,
  checksums,
  compileServer,
  copyDocs,
  hostArch,
  hostPlatform,
  manifest,
  options,
  releases,
  stage,
  validateServerTarget,
  serverVersion as version,
} from "./package-lib.mjs";
import { notices } from "./package-notices.mjs";

const args = options(["platform", "arch", "dm"]);
const platform = args.platform ?? hostPlatform;
const arch = args.arch ?? hostArch;
const dm = args.dm ?? "omit";
validateServerTarget(platform, arch, dm);
const name = `dbmcp-server-${version}-${platform}-${arch}${dm === "native" ? "-dm-local" : ""}`;
const dir = await stage(name);
const binary = join(dir, platform === "windows" ? "dbmcp.exe" : "dbmcp");
const build = await compileServer(binary, platform, arch, dm);
const license = await notices(join(dir, "licenses"), { server: build, dm });
await copyDocs(dir);
await writeFile(
  join(dir, "README.md"),
  `# DB MCP Tools Server ${version}\n\nRun dbmcp version to inspect capabilities. See docs/user-guide.md for initialization, Agent setup, services and recovery. This package is unsigned.\n\nDrivers: ${Object.entries(
    build.drivers,
  )
    .filter(([, available]) => available)
    .map(([name]) => name)
    .join(
      ", ",
    )}. ${dm === "native" ? "Native DM is a local-use build with unconfirmed redistribution permission." : "Native DM is intentionally omitted; it is not a four-driver package."}\n`,
);
await manifest(dir, {
  version,
  kind: "server",
  platform,
  arch,
  drivers: build.drivers,
  execution_validation: build.execution,
  distribution: dm === "native" ? "local-use-only-dm-license-unconfirmed" : "unsigned-local-build",
  licenses: license,
});
const output = join(releases, `${name}.${platform === "windows" ? "zip" : "tar.gz"}`);
await archive(dir, output, platform);
await checksums();
console.log(`Created ${output}`);
