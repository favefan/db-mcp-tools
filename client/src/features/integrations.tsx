import { useState } from "react";
import { invoke } from "../lib/desktop";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Cable, Plus } from "lucide-react";
import { toast } from "sonner";
import { errorMessage, type Health, type Profile } from "../lib/api";
import { Badge, Field, Modal, Button, DataList, DataListItem } from "../components/ui";

interface Target {
  client: string;
  scope: string;
  project_dir: string;
  entry: string;
  transport: string;
  url: string;
  executable: string;
}
interface Installation {
  id: string;
  client: string;
  path: string;
  entry: string;
  transport: string;
  server_id: string;
  entity_id: string;
  backup: string | null;
}
interface Preview {
  path: string;
  fingerprint: string;
  state: string;
  entry_preview: unknown;
}
interface Removal {
  id: string;
  path: string;
  fingerprint: string;
  remove_entry: boolean;
  message: string;
}

export function Integrations({ profile, health }: { profile: Profile | null; health?: Health }) {
  const client = useQueryClient();
  const list = useQuery({
    queryKey: ["integrations"],
    queryFn: () => invoke<Installation[]>("integration_list"),
  });
  const [editing, setEditing] = useState(false);
  const [removal, setRemoval] = useState<Removal>();
  const [busy, setBusy] = useState(false);
  return (
    <section className="panel">
      <div className="section-title">
        <h2>
          <Cable size={18} />
          已安装的 MCP 配置
        </h2>
        <Button disabled={!health} onClick={() => setEditing(true)}>
          <Plus size={16} />
          安装 MCP 配置
        </Button>
      </div>
      {list.error && <p className="error">{errorMessage(list.error)}</p>}
      {!list.data?.length && (
        <p className="muted">
          尚未安装配置。连接 MCP Server 后即可准备接入，无需预先创建实体或绑定实例。
        </p>
      )}
      <DataList>
        {list.data?.map((r) => (
          <DataListItem key={r.id}>
            <div>
              <strong>
                {r.client === "codex" ? "Codex" : "Claude Code"} · {r.entry}
              </strong>
              <p className="muted">{r.path}</p>
              {r.backup && <small>备份：{r.backup}</small>}
            </div>
            <Badge>{r.transport.toUpperCase()}</Badge>
            <Button
              variant="text"
              danger
              disabled={busy || health?.server_id !== r.server_id}
              title={
                health?.server_id !== r.server_id ? "连接原 MCP Server 后撤销凭据" : "预览移除"
              }
              onClick={async () => {
                setBusy(true);
                try {
                  const p = await invoke<Omit<Removal, "id">>("integration_remove_preview", {
                    id: r.id,
                  });
                  setRemoval({ ...p, id: r.id });
                } catch (e) {
                  toast.error(errorMessage(e));
                } finally {
                  setBusy(false);
                }
              }}
            >
              移除 / 撤销
            </Button>
          </DataListItem>
        ))}
      </DataList>
      {editing && <Installer profile={profile} onClose={() => setEditing(false)} />}
      {removal && (
        <Modal title="移除 Agent 接入" onClose={() => setRemoval(undefined)}>
          <p>{removal.message}</p>
          <p className="muted">{removal.path}</p>
          <p>本次接入凭据会被撤销；已经登记的实体会停用。其他 Agent 的凭据继续保留。</p>
          <div className="form-actions">
            <Button onClick={() => setRemoval(undefined)}>取消</Button>
            <Button
              variant="primary"
              danger
              disabled={busy}
              onClick={async () => {
                setBusy(true);
                try {
                  await invoke("integration_remove", {
                    id: removal.id,
                    expected: removal.fingerprint,
                  });
                  await client.invalidateQueries();
                  setRemoval(undefined);
                  toast.success("安装凭据已撤销");
                } catch (e) {
                  toast.error(errorMessage(e));
                } finally {
                  setBusy(false);
                }
              }}
            >
              确认移除并撤销
            </Button>
          </div>
        </Modal>
      )}
    </section>
  );
}

function Installer({ profile, onClose }: { profile: Profile | null; onClose: () => void }) {
  const client = useQueryClient();
  const [target, setTarget] = useState<Target>({
    client: "codex",
    scope: "user",
    project_dir: "",
    entry: "dbmcp",
    transport: "http",
    url: "http://127.0.0.1:19877/mcp",
    executable: profile?.executable ?? "",
  });
  const [plan, setPlan] = useState<{
    preview: Preview;
    target: Target;
  }>();
  const [busy, setBusy] = useState(false);
  const update = (key: keyof Target, value: string) => {
    setTarget({ ...target, [key]: value });
    setPlan(undefined);
  };
  return (
    <Modal title="安装 Agent MCP 配置" onClose={onClose} wide>
      <div className="form-stack">
        <div className="form-grid">
          <Field label="客户端">
            <select value={target.client} onChange={(e) => update("client", e.target.value)}>
              <option value="codex">Codex</option>
              <option value="claude">Claude Code</option>
            </select>
          </Field>
          <Field label="作用域">
            <select value={target.scope} onChange={(e) => update("scope", e.target.value)}>
              <option value="user">当前用户</option>
              <option value="project">指定项目</option>
            </select>
          </Field>
          {target.scope === "project" && (
            <Field label="项目目录（绝对路径）">
              <input
                value={target.project_dir}
                onChange={(e) => update("project_dir", e.target.value)}
              />
            </Field>
          )}
          <Field label="MCP 条目名称">
            <input value={target.entry} onChange={(e) => update("entry", e.target.value)} />
          </Field>
          <Field label="接入方式">
            <select value={target.transport} onChange={(e) => update("transport", e.target.value)}>
              <option value="http">Streamable HTTP</option>
              <option value="stdio">STDIO Bridge</option>
            </select>
          </Field>
          <Field label="MCP 地址" hint="使用 Agent 所在电脑可访问的地址；远程连接需要 HTTPS。">
            <input value={target.url} onChange={(e) => update("url", e.target.value)} />
          </Field>
          {target.transport === "stdio" && (
            <Field
              label="已安装的 dbmcp 程序"
              hint="纯客户端可选择独立安装的程序；没有程序时使用 HTTP。"
            >
              <input
                value={target.executable}
                onChange={(e) => update("executable", e.target.value)}
              />
            </Field>
          )}
        </div>
        {target.client === "codex" && target.scope === "project" && (
          <p className="muted">Codex 只加载已信任项目的 MCP 配置；请在 Codex 中完成项目信任。</p>
        )}
        {target.client === "claude" && target.scope === "project" && (
          <p className="muted">Claude Code 首次加载项目 .mcp.json 时会要求确认。</p>
        )}
        <p className="muted">
          此步骤只准备接入凭据。Agent 首次连接后才创建待授权实体，不会自动获得数据库权限。
        </p>
        <div className="form-actions">
          <Button onClick={onClose}>取消</Button>
          <Button
            variant="primary"
            disabled={busy}
            onClick={async () => {
              setBusy(true);
              try {
                const preview = await invoke<Preview>("integration_preview", { target });
                setPlan({
                  preview,
                  target: structuredClone(target),
                });
              } catch (e) {
                toast.error(errorMessage(e));
              } finally {
                setBusy(false);
              }
            }}
          >
            预览安装
          </Button>
        </div>
      </div>
      {plan && (
        <Modal title="确认 MCP 配置变更" onClose={() => setPlan(undefined)} wide>
          <p>{plan.preview.path}</p>
          <p>
            {plan.preview.state === "conflict"
              ? "同名条目属于其他配置，无法覆盖。请返回并更换名称。"
              : plan.preview.state === "managed"
                ? "替换本工具此前安装的条目，签发新凭据并撤销旧凭据。"
                : "增加一个 MCP 条目和独立接入凭据。首次连接后自动登记，等待管理员确认实例和权限。"}
          </p>
          <pre>{JSON.stringify(plan.preview.entry_preview, null, 2)}</pre>
          <p className="muted">
            已有配置将备份到同一目录。安装后重启 Agent 或重新连接 MCP；HTTP
            凭据保存在目标文件，Bridge 凭据保存在受限本机文件。
          </p>
          <div className="form-actions">
            <Button onClick={() => setPlan(undefined)}>返回修改</Button>
            <Button
              variant="primary"
              disabled={busy || plan.preview.state === "conflict"}
              onClick={async () => {
                setBusy(true);
                try {
                  const result = await invoke<{ record: Installation; warnings: string[] }>(
                    "integration_install",
                    {
                      target: plan.target,
                      expected: plan.preview.fingerprint,
                    },
                  );
                  await client.invalidateQueries();
                  toast.success("MCP 配置已安装");
                  result.warnings.forEach((w) => toast.warning(w));
                  onClose();
                } catch (e) {
                  toast.error(errorMessage(e));
                  setPlan(undefined);
                } finally {
                  setBusy(false);
                }
              }}
            >
              备份并安装
            </Button>
          </div>
        </Modal>
      )}
    </Modal>
  );
}
