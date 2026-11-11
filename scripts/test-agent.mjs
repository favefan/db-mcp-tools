import { spawn, spawnSync } from "node:child_process";
import { mkdtemp, mkdir, readFile, writeFile, rm } from "node:fs/promises";
import { resolve, join } from "node:path";
import { createServer } from "node:net";
import { setTimeout as delay } from "node:timers/promises";

// Real Agent acceptance uses a dedicated temporary container and isolated cwd.
// No user MCP configuration is loaded or modified. Authentication stays in the
// Agent's own credential store. Never print or persist the issued MCP token.
const root = resolve(import.meta.dirname, "..");
const executable = resolve(
  process.env.DBMCP_TEST_SERVER ??
    join(root, "server/bin", process.platform === "win32" ? "dbmcp.exe" : "dbmcp"),
);
const runtime = process.env.DBMCP_TEST_RUNTIME ?? "docker";
const codexCLI = process.env.DBMCP_TEST_CODEX_JS;
const agentKind = process.env.DBMCP_TEST_AGENT ?? "codex";
const claudeCLI = process.env.DBMCP_TEST_CLAUDE;
if (!["codex", "claude"].includes(agentKind)) throw new Error("Unsupported test Agent");
if (agentKind === "codex" && !codexCLI)
  throw new Error("Set DBMCP_TEST_CODEX_JS to the installed Codex bin/codex.js");
if (agentKind === "claude" && !claudeCLI)
  throw new Error("Set DBMCP_TEST_CLAUDE to the installed Claude executable");
const tempRoot = join(root, ".tmp");
await mkdir(tempRoot, { recursive: true });
const temp = await mkdtemp(join(tempRoot, "agent-"));
const data = join(temp, "Server 中文 data");
const container = `dbmcp-agent-${process.pid}-${Date.now()}`;
let server,
  agent,
  secret = "",
  watching = true,
  watcher,
  watchError;
const evidence = { at: new Date().toISOString(), agent: agentKind, status: "failed", approved: [] };
function stopAgent() {
  if (!agent || agent.exitCode !== null) return;
  if (process.platform === "win32")
    spawnSync("taskkill.exe", ["/PID", String(agent.pid), "/T", "/F"], {
      windowsHide: true,
      stdio: "ignore",
    });
  else agent.kill();
}
for (const signal of ["SIGINT", "SIGTERM"])
  process.on(signal, () => {
    watching = false;
    stopAgent();
  });
const command = (exe, args) => {
  const result = spawnSync(exe, args, {
    cwd: temp,
    windowsHide: true,
    encoding: "utf8",
    timeout: 60000,
  });
  if (result.error || result.status !== 0)
    throw new Error(`${exe} failed: ${result.stderr?.slice(-1000)}`);
  return result.stdout;
};
const freePort = async () => {
  const listener = createServer();
  await new Promise((resolve) => listener.listen(0, "127.0.0.1", resolve));
  const port = listener.address().port;
  await new Promise((resolve) => listener.close(resolve));
  return port;
};
try {
  const [pgPort, adminPort, mcpPort] = await Promise.all([freePort(), freePort(), freePort()]);
  command(runtime, [
    "run",
    "--detach",
    "--rm",
    "--name",
    container,
    "--publish",
    `127.0.0.1:${pgPort}:5432`,
    "--tmpfs",
    "/var/lib/postgresql/data",
    "-e",
    "POSTGRES_PASSWORD=temporary-agent-test",
    "-e",
    "POSTGRES_DB=dbmcp_test",
    "postgres:17.6-alpine",
  ]);
  let ready = false;
  for (let i = 0; i < 100; i++) {
    const check = spawnSync(
      runtime,
      ["exec", container, "pg_isready", "-h", "127.0.0.1", "-U", "postgres", "-d", "dbmcp_test"],
      { windowsHide: true, timeout: 3000 },
    );
    if (check.status === 0) {
      ready = true;
      break;
    }
    await delay(300);
  }
  if (!ready) throw new Error("PG container not ready");
  const sql = (query) =>
    command(runtime, [
      "exec",
      container,
      "psql",
      "-U",
      "postgres",
      "-d",
      "dbmcp_test",
      "-At",
      "-v",
      "ON_ERROR_STOP=1",
      "-c",
      query,
    ]).trim();
  sql(
    "CREATE TABLE public.users(id BIGINT PRIMARY KEY,email TEXT,password TEXT,counter INTEGER); INSERT INTO public.users VALUES(1,'person@example.com','private-fixture-value',0)",
  );
  command(executable, ["--data-dir", data, "init"]);
  await writeFile(
    join(data, "deployment.toml"),
    `admin='127.0.0.1:${adminPort}'\nmcp='127.0.0.1:${mcpPort}'\n`,
  );
  secret = (await readFile(join(data, "admin.token"))).toString("base64url");
  server = spawn(executable, ["--data-dir", data, "serve"], { windowsHide: true, stdio: "ignore" });
  const admin = async (path, body, method = body === undefined ? "GET" : "POST") => {
    const response = await fetch(`http://127.0.0.1:${adminPort}/admin/v1/${path}`, {
      method,
      headers: { Authorization: `Bearer ${secret}`, "Content-Type": "application/json" },
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: AbortSignal.timeout(10000),
    });
    if (!response.ok) throw new Error(`Admin ${path} failed: ${response.status}`);
    return response.json();
  };
  ready = false;
  for (let i = 0; i < 100; i++) {
    try {
      await admin("health");
      ready = true;
      break;
    } catch {
      await delay(100);
    }
  }
  if (!ready) throw new Error("Server not ready");
  const policy = await admin("default-policy");
  policy.masks = [
    { id: "fixture-email", target: "email", mode: "fixed", value: "[EMAIL]" },
    { id: "fixture-password", target: "password", mode: "fixed", value: "[REDACTED]" },
  ];
  const instance = await admin("instances", {
    name: "Temporary Agent acceptance",
    driver: "postgres",
    enabled: true,
    connection: {
      host: "127.0.0.1",
      port: pgPort,
      database: "dbmcp_test",
      username: "postgres",
      password: "temporary-agent-test",
      tls: "disable",
    },
    policy,
  });
  const entity = await admin("entities", { name: "Temporary Codex acceptance", enabled: true });
  await admin(
    `entities/${entity.id}/bindings/${instance.id}`,
    { policy: await admin("default-policy?kind=binding") },
    "PUT",
  );
  const issued = await admin(`entities/${entity.id}/credentials`, {});
  const credentials = join(temp, "private.credentials.json");
  await writeFile(
    credentials,
    JSON.stringify({ url: `http://127.0.0.1:${mcpPort}/mcp`, token: issued.token }),
    { mode: 0o600 },
  );
  if (process.platform === "win32") {
    const who = command("whoami.exe", ["/user", "/fo", "csv", "/nh"])
      .trim()
      .split(",")
      .at(-1)
      .replaceAll('"', "");
    command("icacls.exe", [
      credentials,
      "/inheritance:r",
      "/grant:r",
      `*${who}:F`,
      "*S-1-5-18:F",
      "*S-1-5-32-544:F",
    ]);
  }
  const updateSQL = "UPDATE public.users SET counter=counter+1 WHERE id=1";
  watcher = (async () => {
    while (watching) {
      for (const request of await admin("approvals")) {
        if (request.status !== "pending" || evidence.approved.includes(request.id)) continue;
        const detail = await admin(`approvals/${request.id}`);
        if (
          detail.instance_id !== instance.id ||
          detail.entity_id !== entity.id ||
          detail.query?.sql !== updateSQL
        )
          throw new Error("Unexpected approval payload; refusing fixture approval");
        if (sql("SELECT counter FROM public.users WHERE id=1") !== "0")
          throw new Error("Write occurred before approval");
        // Leave pending observable before the narrowly authorized test decision.
        await delay(1500);
        await admin(`approvals/${request.id}/approve`, {});
        evidence.approved.push(request.id);
      }
      await delay(250);
    }
  })().catch((e) => {
    watchError = e;
  });
  const prompt = `This is a local product acceptance test against a disposable PostgreSQL container. Use only dbmcp MCP tools; do not use shell or any other tools. Call list_instances, list_schemas and describe_table for public.users on instance ${instance.id}. Query SELECT id,email,password,counter FROM public.users WHERE id=1 with client_request_id codex-read. Verify email and password are masked. Call db_execute with SQL exactly ${JSON.stringify(updateSQL)}, params [], and client_request_id codex-write. The test harness approves that exact temporary request after observing pending; if pending, call approval_status and resume_request with its original request_id until complete. Call resume_request again to verify it does not repeat the write. Finally query SELECT counter FROM public.users WHERE id=1 with client_request_id codex-verify and report the counter. Do not request any other changes.`;
  const config = `mcp_servers.dbmcp={command=${JSON.stringify(executable)},args=["bridge","--credentials",${JSON.stringify(credentials)},"--wait","0s"],startup_timeout_sec=20,tool_timeout_sec=60}`;
  const agentArgs =
    agentKind === "codex"
      ? [
          codexCLI,
          "exec",
          "--ignore-user-config",
          "--ephemeral",
          "--skip-git-repo-check",
          "--sandbox",
          "read-only",
          "--json",
          "-c",
          config,
          prompt,
        ]
      : [
          "--print",
          "--no-session-persistence",
          "--setting-sources",
          "",
          "--strict-mcp-config",
          "--mcp-config",
          JSON.stringify({
            mcpServers: {
              dbmcp: {
                command: executable,
                args: ["bridge", "--credentials", credentials, "--wait", "0s"],
              },
            },
          }),
          "--tools",
          "",
          "--allowedTools",
          "mcp__dbmcp__*",
          "--output-format",
          "stream-json",
          "--verbose",
          prompt,
        ];
  agent = spawn(agentKind === "codex" ? process.execPath : claudeCLI, agentArgs, {
    cwd: temp,
    windowsHide: true,
    stdio: ["ignore", "pipe", "pipe"],
  });
  let output = "",
    stderr = "";
  agent.stdout.on("data", (b) => {
    output += b;
  });
  agent.stderr.on("data", (b) => {
    stderr += b;
  });
  const deadline = setTimeout(stopAgent, 240000);
  const code = await new Promise((resolve, reject) => {
    agent.on("close", resolve);
    agent.on("error", reject);
  });
  clearTimeout(deadline);
  watching = false;
  await watcher;
  if (watchError) throw watchError;
  evidence.agent_exit = code;
  evidence.counter = sql("SELECT counter FROM public.users WHERE id=1");
  const sanitize = (s) => s.replaceAll(issued.token, "<redacted>").replaceAll(secret, "<redacted>");
  await mkdir(join(root, "artifacts/validation"), { recursive: true });
  await writeFile(join(root, `artifacts/validation/${agentKind}-agent.jsonl`), sanitize(output));
  await writeFile(
    join(root, `artifacts/validation/${agentKind}-agent.stderr.txt`),
    sanitize(stderr),
  );
  const calls = output.split("\n").flatMap((line) => {
    try {
      const e = JSON.parse(line);
      if (e.item?.type === "mcp_tool_call") return [e.item];
      return (e.message?.content ?? [])
        .filter((c) => c.type === "tool_use" && c.name?.startsWith("mcp__dbmcp__"))
        .map((c) => ({ tool: c.name.replace("mcp__dbmcp__", ""), status: "requested" }));
    } catch {
      return [];
    }
  });
  evidence.tools = calls.map((c) => ({ tool: c.tool, status: c.status }));
  if (
    calls.length === 0 &&
    /401 Unauthorized|not logged in|please.*log.?in|authentication.*required/i.test(output + stderr)
  ) {
    evidence.status = "blocked";
    throw new Error(
      `${agentKind} CLI authentication is unavailable; log in with the Agent's official CLI and rerun`,
    );
  }
  if (
    code !== 0 ||
    evidence.counter !== "1" ||
    evidence.approved.length !== 1 ||
    ![
      "list_instances",
      "list_schemas",
      "describe_table",
      "db_query",
      "db_execute",
      "approval_status",
    ].every((tool) => calls.some((c) => c.tool === tool)) ||
    calls.filter((c) => c.tool === "resume_request").length < 2 ||
    !output.includes("[EMAIL]") ||
    output.includes("private-fixture-value")
  )
    throw new Error(
      "Real Agent did not complete the required guarded workflow; inspect sanitized evidence",
    );
  evidence.status = "passed";
  evidence.server_version = (await admin("health")).version;
  console.log(
    `Passed real ${agentKind} discovery, masked query, approval/resume and exactly-once write.`,
  );
} catch (error) {
  evidence.reason = error.message;
  process.exitCode = 1;
  console.error(error.message);
} finally {
  watching = false;
  await watcher;
  stopAgent();
  server?.kill();
  if (server && server.exitCode === null)
    await new Promise((resolve) => server.once("close", resolve));
  spawnSync(runtime, ["remove", "--force", container], {
    windowsHide: true,
    stdio: "ignore",
    timeout: 30000,
  });
  await mkdir(join(root, "artifacts/validation"), { recursive: true });
  await writeFile(
    join(root, `artifacts/validation/${agentKind}-agent.json`),
    JSON.stringify(evidence, null, 2) + "\n",
  );
  await rm(temp, { recursive: true, force: true });
}
