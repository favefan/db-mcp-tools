import { useState } from "react";
import { invoke } from "../lib/desktop";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { KeyRound, Plus, Users, Copy } from "lucide-react";
import { toast } from "sonner";
import {
  api,
  errorMessage,
  type Entity,
  type Instance,
  type Policy,
  type Binding,
  type Credential,
  type Effective,
  type Registration,
} from "../lib/api";
import {
  Badge,
  Empty,
  Field,
  Modal,
  Button,
  DataList,
  DataListItem,
  DataTable,
  Card,
} from "../components/ui";
import { Page } from "../components/page-header";
import { MaskEditor, PolicyEditor } from "./policy";
import { Simulator } from "./simulator";
import { ScopePicker } from "./scope-picker";

export function Entities({ onAuthorize }: { onAuthorize: (entity: string) => void }) {
  const q = useQuery({ queryKey: ["entities"], queryFn: () => api<Entity[]>("entities") });
  const registrations = useQuery({
    queryKey: ["registrations"],
    queryFn: () => api<Registration[]>("registrations"),
  });
  const [editing, setEditing] = useState<Entity>();
  return (
    <Page
      title="访问实体"
      icon={Users}
      count={q.data?.length ?? 0}
      actions={
        <>
          <Button
            variant="primary"
            onClick={() => setEditing({ id: "", name: "", notes: "", enabled: true, revision: 0 })}
          >
            <Plus size={16} />
            新建实体
          </Button>
        </>
      }
    >
      {q.error ? (
        <Empty title="实体读取失败">{errorMessage(q.error)}</Empty>
      ) : !q.data?.length ? (
        <Empty title="创建第一个访问实体">新实体默认没有数据库访问权限。</Empty>
      ) : (
        <div className="cards">
          {q.data.map((e) => (
            <Card key={e.id}>
              <div className="database-icon entity">
                <Users size={19} />
              </div>
              <div className="card-content">
                <div className="card-title">
                  <h2>{e.name}</h2>
                  <Badge
                    tone={
                      registrations.data?.some(
                        (r) => r.entity_id === e.id && r.status === "pending",
                      )
                        ? "orange"
                        : e.enabled
                          ? "green"
                          : "neutral"
                    }
                  >
                    {registrations.data?.some((r) => r.entity_id === e.id && r.status === "pending")
                      ? "待授权"
                      : e.enabled
                        ? "已启用"
                        : "已禁用"}
                  </Badge>
                </div>
                <p>{e.notes || "独立 Agent 身份"}</p>
                <code>{e.id}</code>
                {e.last_seen && (
                  <p>
                    最近访问 {new Date(e.last_seen).toLocaleString()} · {e.last_source}{" "}
                    {e.client && `· ${e.client}`}
                  </p>
                )}
              </div>
              <div className="actions">
                {registrations.data?.some(
                  (r) => r.entity_id === e.id && r.status === "pending",
                ) && (
                  <Button variant="primary" onClick={() => onAuthorize(e.id)}>
                    去授权
                  </Button>
                )}
                <Button onClick={() => setEditing(e)}>管理权限</Button>
              </div>
            </Card>
          ))}
        </div>
      )}
      {editing && (
        <EntityForm
          initial={editing}
          pending={
            registrations.data?.some((r) => r.entity_id === editing.id && r.status === "pending") ??
            false
          }
          onClose={() => setEditing(undefined)}
        />
      )}
    </Page>
  );
}
function EntityForm({
  initial,
  onClose,
  pending,
}: {
  initial: Entity;
  onClose: () => void;
  pending: boolean;
}) {
  const [value, setValue] = useState(initial);
  const [busy, setBusy] = useState(false);
  const [token, setToken] = useState("");
  const [binding, setBinding] = useState<Binding>();
  const client = useQueryClient();
  const instances = useQuery({
    queryKey: ["instances"],
    queryFn: () => api<Instance[]>("instances"),
  });
  const bindings = useQuery({
    queryKey: ["bindings", value.id],
    queryFn: () => api<Binding[]>(`entities/${value.id}/bindings`),
    enabled: !!value.id,
  });
  const credentials = useQuery({
    queryKey: ["credentials", value.id],
    queryFn: () => api<Credential[]>(`entities/${value.id}/credentials`),
    enabled: !!value.id,
  });
  const effective = useQuery({
    queryKey: ["effective", value.id, binding?.instance_id],
    queryFn: () => api<Effective>(`entities/${value.id}/effective/${binding!.instance_id}`),
    enabled: !!binding?.revision,
  });
  const perform = async (f: () => Promise<unknown>, message: string) => {
    setBusy(true);
    try {
      await f();
      await client.invalidateQueries();
      toast.success(message);
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };
  const editBinding = async (instanceID: string) => {
    const old = bindings.data?.find((b) => b.instance_id === instanceID);
    if (old) {
      setBinding(structuredClone(old));
      return;
    }
    try {
      const p = await api<Policy>("default-policy?kind=binding");
      setBinding({ entity_id: value.id, instance_id: instanceID, revision: 0, policy: p });
    } catch (e) {
      toast.error(errorMessage(e));
    }
  };
  return (
    <Modal title={initial.id ? initial.name : "新建访问实体"} onClose={onClose} wide>
      {pending && (
        <p className="connection-banner">
          此实体首次接入，等待授权。请在“待审批 → 实体登记”确认实例与权限。
        </p>
      )}
      <div className="form-stack">
        <div className="form-grid">
          <Field label="实体名称">
            <input
              value={value.name}
              onChange={(e) => setValue({ ...value, name: e.target.value })}
            />
          </Field>
          <Field label="备注">
            <input
              value={value.notes}
              onChange={(e) => setValue({ ...value, notes: e.target.value })}
            />
          </Field>
        </div>
        <label className="check">
          <input
            type="checkbox"
            checked={value.enabled}
            disabled={pending}
            onChange={(e) => setValue({ ...value, enabled: e.target.checked })}
          />
          启用实体
        </label>
        <Button
          variant="primary"
          disabled={busy}
          onClick={() =>
            void perform(
              async () =>
                setValue(
                  await api<Entity>(
                    value.id ? `entities/${value.id}` : "entities",
                    value.id ? "PUT" : "POST",
                    value,
                  ),
                ),
              "实体已保存",
            )
          }
        >
          保存实体
        </Button>
        {value.id && (
          <>
            <div className="section-title">
              <h3>实例授权</h3>
              <Badge>实例与绑定策略共同生效</Badge>
            </div>
            <DataList>
              {instances.data?.map((i) => (
                <DataListItem key={i.id}>
                  <div>
                    <strong>{i.name}</strong>
                    <p className="muted">
                      {bindings.data?.some((b) => b.instance_id === i.id)
                        ? "已绑定 · 点击查看与调整策略"
                        : "尚未授予权限"}
                    </p>
                  </div>
                  <Button disabled={pending} onClick={() => void editBinding(i.id)}>
                    {bindings.data?.some((b) => b.instance_id === i.id)
                      ? "有效权限 / 编辑"
                      : "授权实例"}
                  </Button>
                </DataListItem>
              ))}
            </DataList>
            <div className="section-title">
              <h3>访问凭据</h3>
              <Button
                disabled={busy || pending}
                onClick={() =>
                  void perform(async () => {
                    const out = await api<{ credential: Credential; token: string }>(
                      `entities/${value.id}/credentials`,
                      "POST",
                      {},
                    );
                    setToken(out.token);
                  }, "新凭据已签发")
                }
              >
                <KeyRound size={15} />
                签发独立凭据
              </Button>
            </div>
            <DataTable label="访问凭据" columns={["凭据前缀", "状态", "操作"]}>
              {credentials.data?.map((c) => (
                <tr key={c.id}>
                  <td>
                    <code>{c.prefix}…</code>
                  </td>
                  <td>
                    <Badge tone={c.revoked ? "neutral" : "green"}>
                      {c.revoked ? "已撤销" : "有效"}
                    </Badge>
                  </td>
                  <td>
                    <div className="actions">
                      {!c.revoked && (
                        <Button
                          variant="text"
                          disabled={busy || pending}
                          onClick={() =>
                            void perform(async () => {
                              const out = await api<{ credential: Credential; token: string }>(
                                `credentials/${c.id}/rotate`,
                                "POST",
                              );
                              setToken(out.token);
                            }, "凭据已轮换，请更新 Agent 配置")
                          }
                        >
                          轮换
                        </Button>
                      )}
                      {!c.revoked && (
                        <Button
                          variant="text"
                          danger
                          disabled={busy}
                          onClick={() =>
                            void perform(
                              () => api(`credentials/${c.id}/revoke`, "POST"),
                              "凭据已撤销",
                            )
                          }
                        >
                          撤销
                        </Button>
                      )}
                      {c.revoked && <span className="muted">—</span>}
                    </div>
                  </td>
                </tr>
              ))}
            </DataTable>
            {token && (
              <div className="secret-panel">
                <strong>凭据仅在本次签发后显示</strong>
                <p>保存到受限配置文件，或用于安装 MCP 配置。</p>
                <code>{token}</code>
                <GenericExport token={token} />
                <Button
                  onClick={() =>
                    void navigator.clipboard
                      .writeText(token)
                      .then(() => toast.success("凭据已复制"))
                  }
                >
                  <Copy size={15} />
                  复制凭据
                </Button>
                <Button variant="text" onClick={() => setToken("")}>
                  隐藏
                </Button>
              </div>
            )}
            {!!bindings.data?.length && !!credentials.data?.some((c) => !c.revoked) && (
              <Simulator
                entity={value.id}
                bindings={bindings.data}
                credentials={credentials.data}
                instances={instances.data ?? []}
              />
            )}
          </>
        )}
      </div>
      {binding && (
        <Modal
          title={`实例授权 · ${instances.data?.find((i) => i.id === binding.instance_id)?.name ?? ""}`}
          onClose={() => setBinding(undefined)}
          wide
        >
          {effective.data && (
            <section className="panel">
              <h3>当前有效权限</h3>
              <p>
                {effective.data.operations?.join(" / ").toUpperCase() || "无 SQL 操作权限"} ·{" "}
                {effective.data.max_rows} 行 · {effective.data.timeout_seconds} 秒 ·{" "}
                {effective.data.concurrency} 并发
              </p>
              <p className="muted">
                实例范围：{effective.data.instance_policy.resources?.join(", ") || "无"}
                <br />
                实体范围：{effective.data.binding.policy.resources?.join(", ") || "无"}
                <br />
                两层范围与来源限制共同生效；实例强制 Mask 在实体追加规则之前处理。
              </p>
            </section>
          )}
          <ScopePicker
            instanceId={binding.instance_id}
            resources={binding.policy.resources ?? []}
            onChange={(resources) =>
              setBinding({ ...binding, policy: { ...binding.policy, resources } })
            }
          />
          <PolicyEditor
            value={binding.policy}
            onChange={(policy) => setBinding({ ...binding, policy })}
          />
          <h3>实体追加 Mask</h3>
          <MaskEditor
            rules={binding.policy.masks ?? []}
            onChange={(masks) => setBinding({ ...binding, policy: { ...binding.policy, masks } })}
          />
          <div className="form-actions">
            {binding.revision > 0 && (
              <Button
                danger
                disabled={busy}
                onClick={() =>
                  void perform(async () => {
                    await api(`entities/${value.id}/bindings/${binding.instance_id}`, "PUT", {
                      ...binding,
                      policy: { ...binding.policy, resources: [], operations: [] },
                      approval_rules: [],
                    });
                    setBinding(undefined);
                  }, "实例访问已撤销")
                }
              >
                撤销实例访问
              </Button>
            )}
            <Button onClick={() => setBinding(undefined)}>取消</Button>
            <Button
              variant="primary"
              disabled={busy}
              onClick={() =>
                void perform(async () => {
                  await api(`entities/${value.id}/bindings/${binding.instance_id}`, "PUT", binding);
                  setBinding(undefined);
                }, "实例授权已保存")
              }
            >
              保存授权
            </Button>
          </div>
          {!!binding.approval_rules?.length && (
            <section className="panel">
              <h3>已记住的审批范围</h3>
              <DataList>
                {binding.approval_rules.map((rule) => (
                  <DataListItem key={rule.id}>
                    <span>
                      {rule.operation.toUpperCase()} · {rule.database || "旧规则（需重新审批）"} ·{" "}
                      {rule.tables?.map((t) => `${t.schema}.${t.table}`).join(", ")}
                    </span>
                    <Button
                      variant="text"
                      danger
                      onClick={() =>
                        setBinding({
                          ...binding,
                          approval_rules: binding.approval_rules?.filter((r) => r.id !== rule.id),
                        })
                      }
                    >
                      移除
                    </Button>
                  </DataListItem>
                ))}
              </DataList>
            </section>
          )}
        </Modal>
      )}
    </Modal>
  );
}

function GenericExport({ token }: { token: string }) {
  const [url, setUrl] = useState("http://127.0.0.1:19877/mcp");
  const [busy, setBusy] = useState(false);
  return (
    <div className="form-stack">
      <Field label="通用 HTTP MCP 地址" hint="填写此 MCP Server 的 MCP 地址；远程连接使用 HTTPS。">
        <input value={url} onChange={(e) => setUrl(e.target.value)} />
      </Field>
      <p className="muted">
        导出 mcpServers 格式的 HTTP
        配置供其他客户端导入，包含当前实体凭据。配置文件限制为私密权限；替换已有文件时先备份。
      </p>
      <Button
        disabled={busy || !url.trim()}
        onClick={async () => {
          setBusy(true);
          try {
            const result = await invoke<{ path: string; backup: string | null } | null>(
              "integration_export",
              { url, token },
            );
            if (result) toast.success(`MCP 配置已导出：${result.path}`);
          } catch (e) {
            toast.error(errorMessage(e));
          } finally {
            setBusy(false);
          }
        }}
      >
        导出通用 MCP 配置…
      </Button>
    </div>
  );
}
