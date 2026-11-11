import { spawn } from "node:child_process";
import { mkdir, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
import { parseArgs } from "node:util";

const { values: args } = parseArgs({
  options: {
    runtime: { type: "string", default: "docker" },
    drivers: { type: "string", default: "postgres,mysql,sqlserver,dm" },
    help: { type: "boolean" },
  },
});
if (args.help) {
  console.log(
    "pnpm test:databases --runtime=docker|podman|wslc --drivers=postgres,mysql,sqlserver,dm,postgis,timescaledb",
  );
  console.log(
    "Exit: 0 passed (see report for skips), 1 failed/interrupted, 2 all skipped. Apple Container is not adapted.",
  );
  process.exit(0);
}
const runtime = args.runtime;
if (!["wslc", "docker", "podman"].includes(runtime)) throw new Error("unsupported container CLI");
const selected = [...new Set(args.drivers.split(","))];
const definitions = {
  postgis: {
    driver: "postgres",
    extension: "postgis",
    image: "docker.io/postgis/postgis:17-3.5",
    containerPort: 5432,
    args: [
      "--tmpfs",
      "/var/lib/postgresql/data",
      "-e",
      "POSTGRES_PASSWORD=dbmcp-stage1-temporary",
      "-e",
      "POSTGRES_DB=dbmcp_test",
    ],
  },
  timescaledb: {
    driver: "postgres",
    extension: "timescaledb",
    image: "docker.io/timescale/timescaledb:latest-pg17",
    containerPort: 5432,
    args: [
      "--tmpfs",
      "/var/lib/postgresql/data",
      "-e",
      "POSTGRES_PASSWORD=dbmcp-stage1-temporary",
      "-e",
      "POSTGRES_DB=dbmcp_test",
    ],
  },
  postgres: {
    image: "docker.io/library/postgres:17.6-alpine",
    containerPort: 5432,
    args: [
      "--tmpfs",
      "/var/lib/postgresql/data",
      "-e",
      "POSTGRES_PASSWORD=dbmcp-stage1-temporary",
      "-e",
      "POSTGRES_DB=dbmcp_test",
    ],
  },
  mysql: {
    image: "docker.io/library/mysql:8.4.7",
    containerPort: 3306,
    args: [
      "--tmpfs",
      "/var/lib/mysql",
      "-e",
      "MYSQL_ROOT_PASSWORD=dbmcp-stage1-temporary",
      "-e",
      "MYSQL_ROOT_HOST=%",
      "-e",
      "MYSQL_DATABASE=dbmcp_test",
    ],
  },
  sqlserver: {
    image: "mcr.microsoft.com/mssql/server:2022-latest",
    containerPort: 1433,
    args: [
      "-e",
      "ACCEPT_EULA=Y",
      "-e",
      "MSSQL_SA_PASSWORD=Dbmcp!Test_2026Only",
      "-e",
      "MSSQL_PID=Developer",
    ],
  },
  dm: {
    image: "docker.io/sizx/dm8:1-2-128-22.08.04-166351-20005-CTM",
    containerPort: 5236,
    args: [
      "--tmpfs",
      "/opt/dmdbms/data",
      "-e",
      "PAGE_SIZE=16",
      "-e",
      "UNICODE_FLAG=1",
      "-e",
      "LENGTH_IN_CHAR=1",
      "-e",
      "CASE_SENSITIVE=0",
      "-e",
      "SYSDBA_PWD=Dbmcp!Test_2026Only",
      "-e",
      "LD_LIBRARY_PATH=/opt/dmdbms/bin",
      "-e",
      "INSTANCE_NAME=dbmcp_test",
    ],
  },
};
for (const driver of selected)
  if (!definitions[driver]) throw new Error(`Unknown database: ${driver}`);
const records = [];
let fatalReason;
let currentName;
let child;
let interrupted = false;
async function command(exe, args, { env = process.env, quiet = false, timeoutMs = 300000 } = {}) {
  return new Promise((resolvePromise, reject) => {
    child = spawn(exe, args, { env, windowsHide: true, stdio: ["ignore", "pipe", "pipe"] });
    const taskChild = child;
    const deadline = setTimeout(() => taskChild.kill(), timeoutMs);
    let text = "";
    child.stdout.on("data", (b) => {
      text += b;
      if (!quiet) process.stdout.write(b);
    });
    child.stderr.on("data", (b) => {
      text += b;
      if (!quiet) process.stderr.write(b);
    });
    child.on("error", (error) => {
      clearTimeout(deadline);
      child = undefined;
      reject(error);
    });
    child.on("close", (code) => {
      clearTimeout(deadline);
      child = undefined;
      resolvePromise({ code, text });
    });
  });
}
for (const sig of ["SIGINT", "SIGTERM"]) {
  process.on(sig, () => {
    interrupted = true;
    child?.kill();
  });
}
async function cleanup(name, record) {
  const removed = await command(runtime, ["remove", "--force", name], { quiet: true });
  const remaining = await command(runtime, ["container", "inspect", name], { quiet: true });
  if (removed.code !== 0 || remaining.code === 0) {
    record.status = "failed";
    record.cleanup = "failed";
    throw new Error("Cannot confirm test container removal: " + name);
  }
  record.cleanup = "removed";
}
try {
  const available = await command(runtime, ["info"], { quiet: true, timeoutMs: 30000 });
  if (available.code !== 0)
    throw new Error(
      runtime + " is not available; select a running Docker, Podman or WSLC explicitly",
    );
  for (const driver of selected) {
    if (interrupted) break;
    const spec = definitions[driver];
    if (!spec)
      throw new Error(
        `No verified image definition for ${driver}; supply a documented image before adding this database.`,
      );
    const record = { driver, image: spec.image, status: "failed" };
    records.push(record);
    const image = await command(runtime, ["image", "inspect", spec.image], { quiet: true });
    if (image.code !== 0) {
      const pull = await command(runtime, ["pull", spec.image], { quiet: true });
      if (pull.code !== 0) {
        record.status = "skipped";
        record.reason = "image retrieval failed or exceeded the download deadline";
        console.log(`${driver}: skipped (image unavailable)`);
        continue;
      }
    }
    const inspected = await command(runtime, ["image", "inspect", spec.image], { quiet: true });
    try {
      const meta = JSON.parse(inspected.text)[0];
      record.image_id = meta.Id;
      record.digests = meta.RepoDigests;
      record.architecture = meta.Architecture;
    } catch {
      record.image_metadata = "unavailable";
    }
    if (interrupted) break;
    currentName = `dbmcp-test-${driver}-${process.pid}-${Date.now()}`;
    try {
      const create = await command(
        runtime,
        [
          "run",
          "--detach",
          "--rm",
          "--name",
          currentName,
          "--publish",
          `127.0.0.1::${spec.containerPort}`,
          ...spec.args,
          spec.image,
        ],
        { quiet: true },
      );
      if (create.code !== 0) {
        record.reason = "container startup failed";
        throw new Error(create.text);
      }
      const inspectedContainer = await command(runtime, ["container", "inspect", currentName], {
        quiet: true,
      });
      if (inspectedContainer.code !== 0) throw new Error("Cannot inspect test container");
      const container = JSON.parse(inspectedContainer.text)[0];
      const ports = container.NetworkSettings?.Ports ?? container.Ports;
      const binding = ports?.[String(spec.containerPort) + "/tcp"]?.find(
        (p) => p.HostIp === "127.0.0.1",
      );
      const port = binding?.HostPort;
      if (!port || !/^\d+$/.test(port) || Number(port) < 1 || Number(port) > 65535)
        throw new Error("Cannot discover isolated container port");
      record.port = Number(port);
      const test = await command(
        "go",
        [
          "-C",
          "server",
          "test",
          ...(driver === "dm" ? ["-tags", "dm"] : []),
          "-count=1",
          "-v",
          "./tests/integration",
        ],
        {
          env: {
            ...process.env,
            DBMCP_TEST_CONTAINER: "1",
            DBMCP_TEST_DRIVER: spec.driver ?? driver,
            DBMCP_TEST_EXTENSIONS: spec.extension ?? "",
            DBMCP_TEST_PORT: port,
          },
        },
      );
      record.status = test.code === 0 ? "passed" : "failed";
      record.database_version = /database_version=(.*)/.exec(test.text)?.[1]?.trim();
      if (test.code !== 0) record.reason = "integration test failed";
    } catch (error) {
      record.reason ??= error.message;
      record.status = "failed";
    } finally {
      const name = currentName;
      currentName = undefined;
      await cleanup(name, record);
    }
  }
} catch (error) {
  fatalReason = error.message;
  console.error(fatalReason);
} finally {
  await mkdir("artifacts", { recursive: true });
  await writeFile(
    resolve(`artifacts/database-validation-${selected.join("-")}.json`),
    JSON.stringify(
      {
        at: new Date().toISOString(),
        runtime,
        interrupted,
        reason: fatalReason,
        summary: Object.fromEntries(
          ["passed", "failed", "skipped"].map((status) => [
            status,
            records.filter((r) => r.status === status).length,
          ]),
        ),
        records,
      },
      null,
      2,
    ) + "\n",
  );
}
if (fatalReason || interrupted || records.some((r) => r.status === "failed")) process.exitCode = 1;
else if (!records.some((r) => r.status === "passed")) process.exitCode = 2;
console.log(
  `Database tests: ${records.filter((r) => r.status === "passed").length} passed, ${records.filter((r) => r.status === "failed").length} failed, ${records.filter((r) => r.status === "skipped").length} skipped${interrupted ? "; interrupted" : ""}.`,
);
