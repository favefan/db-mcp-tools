import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import * as Tabs from "@radix-ui/react-tabs";
import { Database, Plus, PlugZap, ShieldCheck } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage, type Instance, type Policy, type Template } from "../lib/api";
import { Badge, Empty, Field, Modal, Button, DataTable, Card } from "../components/ui";
import { Page } from "../components/page-header";
import { PolicyEditor, MaskEditor } from "./policy";
import { ScopePicker } from "./scope-picker";
import { defaultDatabases, qualifyResources, scopeConnection } from "../lib/resource-scope";
import { statusNames } from "../lib/status";

const names: Record<string, string> = {
  postgres: "PostgreSQL",
  mysql: "MySQL",
  sqlserver: "SQL Server",
  dm: "DM8",
};
const ports: Record<string, number> = { postgres: 5432, mysql: 3306, sqlserver: 1433, dm: 5236 };
export function Instances({ drivers }: { drivers: Record<string, boolean> }) {
  const query = useQuery({ queryKey: ["instances"], queryFn: () => api<Instance[]>("instances") });
  const [editing, setEditing] = useState<Instance>();
  const create = async () => {
    try {
      const policy = await api<Policy>("default-policy");
      setEditing({
        id: "",
        name: "",
        driver: "postgres",
        connection: {
          host: "127.0.0.1",
          port: 5432,
          database: "postgres",
          database_mode: "all",
          databases: [],
          schema: "",
          username: "",
          tls: "disable",
        },
        password_set: false,
        enabled: true,
        revision: 0,
        policy: { ...policy, resources: [] },
      });
    } catch (e) {
      toast.error(errorMessage(e));
    }
  };
  return (
    <Page
      title="数据库实例"
      icon={Database}
      count={query.data?.length ?? 0}
      actions={
        <>
          <Button variant="primary" onClick={create}>
            <Plus size={16} />
            新建实例
          </Button>
        </>
      }
    >
      {query.isPending ? (
        <Empty title="正在读取实例…" />
      ) : query.error ? (
        <Empty title="无法读取实例">{errorMessage(query.error)}</Empty>
      ) : query.data?.length === 0 ? (
        <Empty title="添加第一个数据库实例">连接数据库后，为 Agent 创建独立实体并授权。</Empty>
      ) : (
        <div className="cards">
          {query.data?.map((i) => (
            <Card key={i.id}>
              <div className={`database-icon ${i.driver}`}>
                <Database size={19} />
              </div>
              <div className="card-content">
                <div className="card-title">
                  <h2>{i.name}</h2>
                  <Badge tone={i.enabled ? "green" : "neutral"}>
                    {i.enabled ? "已启用" : "已停用"}
                  </Badge>
                </div>
                <div className="card-details">
                  <p>
                    {names[i.driver]}
                    <span className="dot">·</span>
                    {i.connection.host}:{i.connection.port}
                    <span className="dot">·</span>
                    {i.connection.database_mode === "all"
                      ? "全部数据库"
                      : i.connection.database_mode === "selected"
                        ? `${i.connection.databases?.length ?? 0} 个数据库`
                        : i.connection.database}
                  </p>
                  <div className="card-meta">
                    <ShieldCheck size={13} />
                    {i.policy.approval === "writes"
                      ? "写入需审批"
                      : i.policy.approval === "always"
                        ? "每次审批"
                        : "按策略放行"}
                    <span className="dot">·</span>最多 {i.policy.max_rows} 行
                  </div>
                </div>
              </div>
              <Button onClick={() => setEditing(i)}>管理实例</Button>
            </Card>
          ))}
        </div>
      )}
      {editing && (
        <InstanceForm initial={editing} drivers={drivers} onClose={() => setEditing(undefined)} />
      )}
    </Page>
  );
}
function InstanceForm({
  initial,
  onClose,
  drivers,
}: {
  initial: Instance;
  onClose: () => void;
  drivers: Record<string, boolean>;
}) {
  const [value, setValue] = useState<Instance>(() => ({
    ...initial,
    connection: {
      ...initial.connection,
      database: initial.connection.database || defaultDatabases[initial.driver],
    },
    policy: {
      ...initial.policy,
      resources: qualifyResources(initial.policy.resources ?? [], initial),
    },
  }));
  const payload = {
    ...value,
    connection: scopeConnection(value.connection, value.policy.resources ?? []),
  };
  const [tab, setTab] = useState("连接");
  const client = useQueryClient();
  const templates = useQuery({
    queryKey: ["templates"],
    queryFn: () => api<Template[]>("templates"),
  });
  const save = useMutation({
    mutationFn: () =>
      api<Instance>(
        value.id ? `instances/${value.id}` : "instances",
        value.id ? "PUT" : "POST",
        payload,
      ),
    onSuccess: () => {
      void client.invalidateQueries();
      toast.success("实例已保存");
      onClose();
    },
    onError: (e) => toast.error(errorMessage(e)),
  });
  const test = useMutation({
    mutationFn: () => api("instances/test", "POST", payload),
    onSuccess: () => toast.success("数据库连接成功"),
    onError: (e) => toast.error(errorMessage(e)),
  });
  const connection = <K extends keyof Instance["connection"]>(
    key: K,
    v: Instance["connection"][K],
  ) => {
    setValue({ ...value, connection: { ...value.connection, [key]: v } });
  };
  return (
    <Modal title={initial.id ? initial.name : "新建数据库实例"} onClose={onClose} wide stable>
      <Tabs.Root className="instance-editor" value={tab} onValueChange={setTab}>
        <Tabs.List className="tabs">
          {["连接", "范围", "权限", "Mask", "实体", "记录", "高级"].map((t) => (
            <Tabs.Trigger value={t} key={t}>
              {t}
            </Tabs.Trigger>
          ))}
        </Tabs.List>
        <Tabs.Content value="连接" className="tab-content">
          <div className="form-stack">
            <div className="form-grid">
              <Field label="实例名称">
                <input
                  value={value.name}
                  onChange={(e) => setValue({ ...value, name: e.target.value })}
                />
              </Field>
              <Field label="数据库类型">
                <select
                  value={value.driver}
                  onChange={(e) => {
                    setValue({
                      ...value,
                      driver: e.target.value,
                      connection: {
                        ...value.connection,
                        port: ports[e.target.value],
                        database: defaultDatabases[e.target.value],
                        schema: "",
                      },
                      policy: { ...value.policy, resources: [] },
                    });
                  }}
                >
                  {Object.entries(names).map(([key, name]) => (
                    <option value={key} key={key}>
                      {name}
                      {drivers[key] ? "" : "（此 MCP Server 未编译）"}
                    </option>
                  ))}
                </select>
              </Field>
              <Field label="主机">
                <input
                  value={value.connection.host}
                  onChange={(e) => connection("host", e.target.value)}
                />
              </Field>
              <Field label="端口">
                <input
                  type="number"
                  value={value.connection.port}
                  onChange={(e) => connection("port", Number(e.target.value))}
                />
              </Field>
              <Field
                label="默认数据库"
                hint={
                  value.driver === "mysql"
                    ? "MySQL 没有固定默认库，可留空；访问对象在范围页选择。"
                    : value.driver === "dm"
                      ? "DM8 数据库由连接端点决定，范围页会读取实际库名。"
                      : "用于建立连接和发现数据库，不会自动授予该库的访问权限。"
                }
              >
                <input
                  value={value.connection.database}
                  disabled={value.driver === "dm"}
                  placeholder={value.driver === "dm" ? "从当前端点识别" : "可留空"}
                  onChange={(e) => connection("database", e.target.value)}
                />
              </Field>
              {value.driver !== "mysql" && (
                <Field label="默认 Schema（可选）">
                  <input
                    value={value.connection.schema}
                    onChange={(e) => connection("schema", e.target.value)}
                  />
                </Field>
              )}
              <Field label="用户名">
                <input
                  autoComplete="off"
                  value={value.connection.username}
                  onChange={(e) => connection("username", e.target.value)}
                />
              </Field>
              <Field
                label="密码"
                hint={value.password_set ? "已设置；留空保留现有密码" : undefined}
              >
                <input
                  type="password"
                  autoComplete="new-password"
                  placeholder={value.password_set ? "●●●●●●" : ""}
                  value={value.connection.password ?? ""}
                  onChange={(e) => connection("password", e.target.value)}
                />
              </Field>
              <Field
                label="TLS"
                hint={
                  value.driver === "dm"
                    ? "当前 DM 驱动不提供可验证 TLS；仅使用本机或受保护隧道并显式关闭 TLS。"
                    : undefined
                }
              >
                <select
                  value={value.connection.tls}
                  onChange={(e) => connection("tls", e.target.value)}
                >
                  <option value="require">加密连接</option>
                  <option value="verify-full">加密并验证证书</option>
                  <option value="disable">关闭</option>
                </select>
              </Field>
              {!value.id && (
                <Field label="从模板复制策略">
                  <select
                    defaultValue=""
                    onChange={(e) => {
                      const t = templates.data?.find((x) => x.id === e.target.value);
                      setValue({
                        ...value,
                        policy: t
                          ? {
                              ...structuredClone(t.policy),
                              resources: qualifyResources(t.policy.resources ?? [], {
                                ...value,
                                connection: payload.connection,
                              }),
                            }
                          : structuredClone(initial.policy),
                      });
                    }}
                  >
                    <option value="">不使用模板</option>
                    {templates.data?.map((t) => (
                      <option key={t.id} value={t.id}>
                        {t.name}
                      </option>
                    ))}
                  </select>
                </Field>
              )}
            </div>
            <div className="actions">
              <Button disabled={test.isPending} onClick={() => test.mutate()}>
                <PlugZap size={16} />
                {test.isPending ? "正在测试…" : "测试连接"}
              </Button>
            </div>
          </div>
        </Tabs.Content>
        <Tabs.Content value="范围" className="tab-content">
          <ScopePicker
            instance={value}
            resources={value.policy.resources ?? []}
            onChange={(resources) => setValue({ ...value, policy: { ...value.policy, resources } })}
          />
        </Tabs.Content>
        <Tabs.Content value="权限" className="tab-content">
          <PolicyEditor
            value={value.policy}
            onChange={(policy) => setValue({ ...value, policy })}
          />
        </Tabs.Content>
        <Tabs.Content value="Mask" className="tab-content">
          <MaskEditor
            rules={value.policy.masks ?? []}
            onChange={(masks) => setValue({ ...value, policy: { ...value.policy, masks } })}
          />
        </Tabs.Content>
        <Tabs.Content value="实体" className="tab-content">
          <InstanceEntities id={value.id} />
        </Tabs.Content>
        <Tabs.Content value="记录" className="tab-content">
          <InstanceAudit id={value.id} />
        </Tabs.Content>
        <Tabs.Content value="高级" className="tab-content">
          <label className="check">
            <input
              type="checkbox"
              checked={value.enabled}
              onChange={(e) => setValue({ ...value, enabled: e.target.checked })}
            />
            启用此实例
          </label>
          <Field label="实例 ID">
            <input readOnly value={value.id || "保存后生成"} />
          </Field>
          <p className="muted">
            配置版本 {value.revision}。并发编辑会拒绝覆盖，请重新加载最新配置。
          </p>
        </Tabs.Content>
      </Tabs.Root>
      <div className="form-actions">
        <Button onClick={onClose}>取消</Button>
        <Button variant="primary" disabled={save.isPending} onClick={() => save.mutate()}>
          {save.isPending ? "正在保存…" : "保存实例"}
        </Button>
      </div>
    </Modal>
  );
}
function InstanceEntities({ id }: { id: string }) {
  const q = useQuery({
    queryKey: ["instance-entities", id],
    queryFn: () => api<EntitySummary[]>(`instances/${id}/entities`),
    enabled: !!id,
  });
  return (
    <div className="form-stack">
      {q.error ? (
        <Empty title="实体读取失败">{errorMessage(q.error)}</Empty>
      ) : q.isPending && id ? (
        <Empty title="正在读取实体…" />
      ) : !q.data?.length ? (
        <Empty title="尚未绑定实体" />
      ) : (
        <DataTable label="已绑定实体" columns={["实体名称", "状态"]}>
          {q.data.map((e) => (
            <tr key={e.id}>
              <td>{e.name}</td>
              <td>
                <Badge tone={e.enabled ? "green" : "neutral"}>
                  {e.enabled ? "已启用" : "已停用"}
                </Badge>
              </td>
            </tr>
          ))}
        </DataTable>
      )}
    </div>
  );
}
type EntitySummary = { id: string; name: string; enabled: boolean };
function InstanceAudit({ id }: { id: string }) {
  const q = useQuery({
    queryKey: ["audit", id],
    queryFn: () =>
      api<{ id: number; time: string; action: string; decision: string }[]>(
        `audit-events?instance_id=${encodeURIComponent(id)}`,
      ),
    enabled: !!id,
  });
  return (
    <div className="form-stack">
      {q.error ? (
        <Empty title="记录读取失败">{errorMessage(q.error)}</Empty>
      ) : q.isPending && id ? (
        <Empty title="正在读取记录…" />
      ) : !q.data?.length ? (
        <Empty title="暂无请求记录" />
      ) : (
        <DataTable label="实例访问记录" columns={["时间", "操作", "结果"]}>
          {q.data.map((e) => (
            <tr key={e.id}>
              <td>
                <time>{new Date(e.time).toLocaleString()}</time>
              </td>
              <td>
                <code>{e.action}</code>
              </td>
              <td>
                <Badge
                  tone={
                    e.decision === "succeeded"
                      ? "green"
                      : e.decision === "failed" || e.decision === "denied"
                        ? "red"
                        : "neutral"
                  }
                >
                  {statusNames[e.decision] ?? e.decision}
                </Badge>
              </td>
            </tr>
          ))}
        </DataTable>
      )}
    </div>
  );
}
