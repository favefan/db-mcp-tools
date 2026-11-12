// In-memory visual-review fixtures, loaded only by Vite's explicit UI preview.
// No credentials, native integration, database execution or policy decisions.
import type {
  Policy,
  Instance,
  Entity,
  Template,
  Column,
  Registration,
  Audit,
  AuditDetail,
  Profile,
  BackupInfo,
} from "./api";
import { databaseResource } from "./resource-scope";

const policy: Policy = {
  resources: ["*"],
  operations: ["select", "insert", "update", "delete"],
  cidrs: ["127.0.0.0/8", "::1/128"],
  approval: "writes",
  max_rows: 5000,
  max_bytes: 10485760,
  timeout_seconds: 15,
  concurrency: 4,
  approval_seconds: 300,
  masks: [],
};
const instances: Instance[] = [
  ["postgres", "开发数据库", "workspace", 5432],
  ["mysql", "业务数据 · 只读", "analytics", 3306],
  ["sqlserver", "SQL Server 测试", "sandbox", 1433],
  ["dm", "达梦开发库", "DAMENG", 5236],
].map(([driver, name, database, port], i) => ({
  id: `preview-instance-${i}`,
  name: String(name),
  driver: String(driver),
  enabled: i !== 3,
  revision: 1,
  password_set: true,
  connection: {
    host: "127.0.0.1",
    port: Number(port),
    database: String(database),
    schema: driver === "postgres" ? "public" : "",
    username: "demo_reader",
    tls: "require",
  },
  policy: structuredClone(policy),
}));
const entities: Entity[] = [
  {
    id: "preview-codex",
    name: "Codex · 开发助手",
    notes: "开发项目的独立访问身份",
    enabled: true,
    revision: 1,
  },
  {
    id: "preview-claude",
    name: "Claude Code · 数据分析",
    notes: "分析工作区，只读访问",
    enabled: true,
    revision: 1,
  },
  {
    id: "preview-pending-entity",
    name: "Codex · 新项目",
    notes: "首次接入，等待授权",
    enabled: false,
    revision: 1,
    client: "Codex MCP client",
    last_source: "127.0.0.1",
    last_seen: new Date().toISOString(),
  },
];
const registrations: Registration[] = [
  {
    id: "preview-registration",
    entity_id: "preview-pending-entity",
    name: "Codex · 新项目",
    prefix: "demo-only",
    status: "pending",
    created_at: new Date().toISOString(),
    requested_at: new Date().toISOString(),
    source: "127.0.0.1",
  },
];
const templates: Template[] = [
  { id: "preview-template", name: "默认访问策略", revision: 1, policy: structuredClone(policy) },
];
let profile: Profile = {
  id: "preview-local",
  name: "default",
  mode: "local",
  preferences: {
    theme: "system",
    notifications: false,
    close_to_tray: false,
    close_behavior: "ask",
    lightweight_mode: false,
  },
  endpoint: "http://127.0.0.1:19876",
  data_dir: "D:/DBMCP/server",
  executable: "D:/DBMCP/dbmcp.exe",
  server_id: "preview-server",
};
let preferences: Profile["preferences"] = profile.preferences;
const profiles: Profile[] = [
  profile,
  {
    ...structuredClone(profile),
    id: "preview-existing",
    name: "同一 MCP Server · 深色配置",
    mode: "existing",
    data_dir: "",
    executable: "",
    endpoint: "http://127.0.0.1:19876",
    server_id: "preview-server",
    preferences: { ...profile.preferences, theme: "dark" },
  },
];
let generation = 1;
let connected = true;
const session = () => ({
  current: profiles.find((p) => p.id === profile.id) ?? null,
  profiles,
  generation,
  connected,
  busy: false,
  preferences,
});
const health = {
  server_id: "preview-server",
  version: "1.0.13",
  api_version: 1,
  mode: "background",
  data_dir: profile.data_dir,
  drivers: { postgres: true, mysql: true, sqlserver: true, dm: true },
};
let backups: BackupInfo[] = [];
const logs: Audit[] = instances.map((i, n) => ({
  id: 205 - n,
  time: new Date(Date.now() - n * 900000).toISOString(),
  entity_id: entities[n % 2].id,
  instance_id: i.id,
  action: "instance.save",
  decision: "saved",
  detail: "界面演示记录",
  duration_ms: 0,
  request_id: "",
}));
logs[0] = {
  ...logs[0],
  action: "db_query",
  decision: "succeeded",
  request_id: "preview-query",
  duration_ms: 28,
};
logs[1] = {
  ...logs[1],
  entity_id: logs[0].entity_id,
  instance_id: logs[0].instance_id,
  action: "execute",
  decision: "succeeded",
  request_id: "preview-query",
  duration_ms: 21,
};
logs[2] = {
  ...logs[2],
  action: "db_execute",
  decision: "denied",
  request_id: "preview-denied",
  duration_ms: 4,
};
logs[3] = { ...logs[3], action: "db_query", decision: "succeeded", request_id: "preview-legacy" };
for (let n = 1; n <= 201; n++)
  logs.push({
    ...logs[0],
    id: n,
    time: new Date(Date.now() - n * 900000).toISOString(),
    action: "instance.save",
    decision: "saved",
    request_id: "",
    detail: "管理配置已保存",
  });

function fixtureAuditDetail(event: Audit): AuditDetail {
  const base: AuditDetail = {
    event,
    events: event.request_id
      ? logs.filter((e) => e.request_id === event.request_id).sort((a, b) => a.id - b.id)
      : [event],
    events_truncated: false,
    payload_state: event.request_id ? "available" : "not_recorded",
  };
  if (!event.request_id) return base;
  const legacy = event.request_id === "preview-legacy";
  const denied = event.decision === "denied";
  return {
    ...base,
    payload_state: legacy ? "cleared" : "available",
    request: {
      id: event.request_id,
      tool: event.action,
      client_request_id: "preview-client-request",
      entity_id: event.entity_id,
      entity_name: entities.find((e) => e.id === event.entity_id)?.name ?? "演示实体",
      client: "界面评审 Agent",
      instance_id: event.instance_id,
      instance_name: instances.find((i) => i.id === event.instance_id)?.name ?? "演示实例",
      database: "workspace",
      source: "127.0.0.1",
      trace_id: "preview-diagnostic-id",
      sql: legacy
        ? ""
        : denied
          ? "DELETE FROM public.orders WHERE id = $1"
          : "SELECT id, email FROM public.users WHERE id = $1",
      parameter_json: "[9007199254740993]",
      status: event.decision,
      operation: denied ? "delete" : "select",
      tables: [{ schema: "public", table: "users", column: "" }],
      created_at: event.time,
      finished_at: event.time,
      duration_ms: event.duration_ms,
      ...(legacy
        ? {}
        : {
            result: {
              columns: denied ? [] : ["id", "email"],
              row_count: denied ? 0 : 1,
              affected: 0,
              truncated: false,
              reason: denied ? "该实体无 DELETE 权限" : "",
              masks: denied
                ? []
                : [
                    {
                      id: "user-configured-preview-rule",
                      target: "email",
                      mode: "fixed",
                      layer: "instance",
                    },
                  ],
            },
          }),
    },
  };
}
const approvals = [
  {
    id: "preview-approval",
    entity_id: entities[0].id,
    credential_id: "preview-credential",
    instance_id: instances[0].id,
    client_request_id: "preview-request",
    digest: "preview",
    status: "pending",
    created_at: new Date().toISOString(),
    expires_at: new Date(Date.now() + 3600000).toISOString(),
    operation: "update",
    tables: [{ schema: "public", table: "orders", column: "" }],
    source: "127.0.0.1",
    query: {
      instance_id: instances[0].id,
      sql: "UPDATE public.orders SET status = $1 WHERE id = $2",
      params: ["confirmed", 42],
      client_request_id: "preview-request",
      tool: "db_execute",
    },
  },
];
for (const [index, status] of ["succeeded", "denied", "expired"].entries()) {
  approvals.push({
    ...structuredClone(approvals[0]),
    id: `preview-${status}`,
    status,
    client_request_id: `preview-${status}`,
    created_at: new Date(Date.now() - (index + 1) * 900000).toISOString(),
    expires_at: new Date(Date.now() - (index + 1) * 300000).toISOString(),
  });
}

function save<T extends { id: string; revision: number }>(list: T[], body: unknown): T {
  const value = structuredClone(body) as T;
  value.id ||= crypto.randomUUID();
  value.revision = (value.revision || 0) + 1;
  const index = list.findIndex((v) => v.id === value.id);
  if (index < 0) list.push(value);
  else list[index] = value;
  return value;
}

function fixtureCatalog(driver: string, database: string) {
  const schema =
    driver === "mysql"
      ? database
      : driver === "sqlserver"
        ? "dbo"
        : driver === "dm"
          ? "DBMCP_TEST"
          : "public";
  const columns: Column[] = ["id", "name", "email", "status"].map((name, i) => ({
    database,
    database_resource: databaseResource(database) + "/*",
    resource: databaseResource(database, schema, "users", name),
    table_resource: databaseResource(database, schema, "users"),
    schema_resource: databaseResource(database, schema),
    schema,
    table: "users",
    name,
    type: i === 0 ? "bigint" : "text",
    ordinal: i + 1,
  }));
  return {
    database,
    columns,
    schemas: driver === "mysql" ? [database] : [schema, "empty_schema"],
    extensions: [],
  };
}

async function request(path: string, method: string, body: unknown): Promise<unknown> {
  const [route] = path.replace(/^\/admin\/v1\//, "").split("?");
  const parts = route.split("/");
  if (route === "health") return health;
  if (route === "default-policy") return structuredClone(policy);
  if (route === "instances") return method === "GET" ? instances : save(instances, body);
  if (route === "instances/databases")
    return (body as Instance).driver === "dm" ? ["DAMENG"] : ["app", "analytics", "archive"];
  if (
    parts[0] === "instances" &&
    parts.length === 2 &&
    method === "GET" &&
    parts[1] !== "databases"
  )
    return instances.find((i) => i.id === parts[1]);
  if (route === "instances/schema" && method === "POST") {
    const input = body as { instance: Instance; database: string };
    return fixtureCatalog(input.instance.driver, input.database);
  }
  if (parts[0] === "instances" && parts[2] === "databases") {
    const connection = instances.find((i) => i.id === parts[1])?.connection;
    return connection?.database_mode === "all"
      ? ["app", "analytics", "archive"]
      : connection?.database_mode === "selected"
        ? (connection.databases ?? [])
        : [connection?.database ?? "app"];
  }
  if (route === "entities") return method === "GET" ? entities : save(entities, body);
  if (route === "templates") return method === "GET" ? templates : save(templates, body);
  if (parts[0] === "instances" && parts.length === 2 && method === "PUT")
    return save(instances, body);
  if (parts[0] === "entities" && parts.length === 2 && method === "PUT")
    return save(entities, body);
  if (parts[0] === "instances" && parts[2] === "schema") {
    return fixtureCatalog(
      instances.find((i) => i.id === parts[1])?.driver ?? "postgres",
      new URLSearchParams(path.split("?")[1]).get("database") || "app",
    );
  }
  if (parts[0] === "instances" && parts[2] === "entities") return entities;
  if (parts[0] === "entities" && parts[2] === "credentials") return [];
  if (parts[0] === "entities" && parts[2] === "bindings") return method === "GET" ? [] : body;
  if (route === "audit-events") {
    const params = new URLSearchParams(path.split("?")[1]);
    return logs
      .filter(
        (e) =>
          (!params.get("entity_id") || e.entity_id === params.get("entity_id")) &&
          (!params.get("instance_id") || e.instance_id === params.get("instance_id")) &&
          (!params.get("decision") || e.decision === params.get("decision")) &&
          (!Number(params.get("before")) || e.id < Number(params.get("before"))) &&
          (!params.get("from") || e.time >= params.get("from")!) &&
          (!params.get("to") || e.time <= params.get("to")!),
      )
      .sort((a, b) => b.id - a.id)
      .slice(0, 200);
  }
  if (parts[0] === "audit-events" && parts.length === 2) {
    const event = logs.find((e) => e.id === Number(parts[1]));
    if (!event) throw new Error("日志已清理或不存在");
    return fixtureAuditDetail(event);
  }
  if (route === "approvals") return approvals;
  if (parts[0] === "approvals") {
    const item = approvals.find((v) => v.id === parts[1]);
    if (method === "GET") return item;
    if (item) item.status = parts[2] === "deny" ? "denied" : "approved";
    return { recorded: true };
  }
  if (route === "registrations") return registrations;
  if (parts[0] === "registrations" && method === "POST") {
    const registration = registrations.find((r) => r.id === parts[1]);
    if (!registration) throw new Error("演示登记不存在");
    registration.status = parts[2] === "approve" ? "approved" : "denied";
    const entity = entities.find((e) => e.id === registration.entity_id);
    if (entity) entity.enabled = registration.status === "approved";
    return registration;
  }
  if (route === "backups") {
    if (method === "GET") return backups;
    const id = `preview-backup-${Date.now()}`;
    const path = `${profile.data_dir}/backups/${id}`;
    backups = [
      {
        id,
        path,
        created_at: new Date().toISOString(),
        version: health.version,
        schema: 6,
        server_id: health.server_id,
        files: {},
      },
      ...backups,
    ];
    return { path };
  }
  if (parts[0] === "backups" && method === "DELETE") {
    backups = backups.filter((backup) => backup.id !== parts[1]);
    return { deleted: true };
  }
  if (route === "configuration")
    return { version: 1, instances, entities, templates, bindings: [] };
  throw new Error("这是界面演示数据；数据库、规则执行和本机操作请在桌面客户端验证。");
}

export async function previewInvoke(
  command: string,
  args: Record<string, unknown> = {},
): Promise<unknown> {
  let result: unknown;
  switch (command) {
    case "get_session":
      result = session();
      break;
    case "save_profile": {
      const p = structuredClone(args.profile as Profile);
      if (!p.id) p.id = crypto.randomUUID();
      const index = profiles.findIndex((x) => x.id === p.id);
      if (index < 0) profiles.push(p);
      else profiles[index] = p;
      if (profile.id === p.id || !profiles.some((current) => current.id === profile.id)) {
        profile = p;
        preferences = p.preferences;
        generation++;
        connected = false;
      }
      result = session();
      break;
    }
    case "delete_profile": {
      if (args.id === profile.id && connected) throw new Error("请先断开此配置");
      const i = profiles.findIndex((p) => p.id === args.id);
      if (i >= 0) profiles.splice(i, 1);
      if (args.id === profile.id) {
        generation++;
        preferences = {
          theme: "system",
          notifications: false,
          close_to_tray: false,
          close_behavior: "ask",
          lightweight_mode: false,
        };
      }
      result = session();
      break;
    }
    case "switch_profile": {
      const p = profiles.find((p) => p.id === args.id);
      if (!p) throw new Error("配置不存在");
      profile = p;
      preferences = p.preferences;
      generation++;
      connected = Boolean(args.connectNow);
      result = session();
      break;
    }
    case "disconnect_session":
      connected = false;
      generation++;
      result = session();
      break;
    case "get_profile":
      result = profile;
      break;
    case "get_preferences":
      result = preferences;
      break;
    case "set_desktop_preferences":
      preferences.close_behavior = args.closeBehavior as Profile["preferences"]["close_behavior"];
      if (args.theme) preferences.theme = args.theme as Profile["preferences"]["theme"];
      if (typeof args.notifications === "boolean") preferences.notifications = args.notifications;
      preferences.close_to_tray = args.closeBehavior === "tray";
      preferences.lightweight_mode = Boolean(args.lightweightMode);
      result = preferences;
      break;
    case "local_defaults":
      result = {
        executable: profile.executable,
        bundled: true,
        platform: "windows",
        build: {
          version: "1.0.8",
          drivers: health.drivers,
          default_data_dir: profile.data_dir,
          system_data_dir: "C:/ProgramData/DBMCP/server",
        },
      };
      break;
    case "local_command":
      if (args.action !== "status") throw new Error("网页用于界面评审，此操作需要桌面客户端。");
      result = { ...health, running: args.dataDir === profile.data_dir };
      break;
    case "integration_list":
      result = [];
      break;
    case "integration_preview": {
      const target = args.target as { client: string; entry: string; url: string };
      result = {
        path: `演示/${target.client}/config`,
        fingerprint: "preview-only",
        state: "new",
        entry_preview: { name: target.entry, url: target.url, authorization: "[接入凭据，仅演示]" },
      };
      break;
    }
    case "connect_profile":
      connected = true;
      generation++;
      result = { ...health, server_id: profile.server_id };
      break;
    case "admin_request":
      if (!connected) throw new Error("当前配置未连接");
      result = await request(String(args.path), String(args.method), args.body);
      if (args.path === "/admin/v1/health") result = { ...health, server_id: profile.server_id };
      break;
    default:
      throw new Error("网页用于界面评审，此操作需要桌面客户端。");
  }
  return structuredClone(result);
}
