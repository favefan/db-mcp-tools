import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  api,
  errorMessage,
  type Registration,
  type Instance,
  type Entity,
  type Policy,
} from "../lib/api";
import { Badge, Empty, Field, Modal, Button, Card } from "../components/ui";
import { PolicyEditor } from "./policy";
import { ScopePicker } from "./scope-picker";

export function Registrations({ focusEntity }: { focusEntity?: string }) {
  const q = useQuery({
    queryKey: ["registrations"],
    queryFn: () => api<Registration[]>("registrations"),
  });
  const entities = useQuery({ queryKey: ["entities"], queryFn: () => api<Entity[]>("entities") });
  const [selected, setSelected] = useState<Registration>();
  const [busy, setBusy] = useState(false);
  const target = useRef<HTMLElement>(null);
  const located = useRef(false);
  useEffect(() => {
    if (focusEntity && target.current && !located.current) {
      target.current.scrollIntoView({ block: "nearest" });
      target.current.focus({ preventScroll: true });
      located.current = true;
    }
  }, [focusEntity, q.data]);
  const client = useQueryClient();
  const pending = q.data?.filter((r) => r.status === "pending") ?? [];
  return (
    <>
      <p className="muted">
        Agent
        首次使用独立接入凭据时自动登记。确认实例和权限前，不能发现或访问任何数据库。名称与来源仅供辨认，身份由接入凭据稳定识别。
      </p>
      {q.error ? (
        <Empty title="实体登记读取失败">{errorMessage(q.error)}</Empty>
      ) : !pending.length ? (
        <Empty title="没有待授权的实体">
          在设置 → Agent 接入安装配置，Agent 首次连接后会出现在这里。
        </Empty>
      ) : (
        <div className="cards">
          {pending.map((r) => (
            <Card
              kind="approval"
              className={focusEntity === r.entity_id ? "focused-registration" : ""}
              key={r.id}
              ref={focusEntity === r.entity_id ? target : undefined}
              tabIndex={-1}
            >
              <div className="card-title">
                <h2>{r.name}</h2>
                <Badge tone="orange">首次接入 · 待授权</Badge>
              </div>
              <p>
                来源：{r.source} · 客户端：
                {entities.data?.find((e) => e.id === r.entity_id)?.client || "未提供"}
              </p>
              <div className="approval-footer">
                <time>{r.requested_at && new Date(r.requested_at).toLocaleString()}</time>
                <div className="actions">
                  <Button
                    danger
                    disabled={busy}
                    onClick={async () => {
                      setBusy(true);
                      try {
                        await api(`registrations/${r.id}/deny`, "POST");
                        await client.invalidateQueries();
                        toast.success("已拒绝登记，接入凭据已撤销");
                      } catch (e) {
                        toast.error(errorMessage(e));
                      } finally {
                        setBusy(false);
                      }
                    }}
                  >
                    拒绝
                  </Button>
                  <Button variant="primary" onClick={() => setSelected(r)}>
                    确认实例与权限
                  </Button>
                </div>
              </div>
            </Card>
          ))}
        </div>
      )}
      {selected && <Authorize registration={selected} onClose={() => setSelected(undefined)} />}
    </>
  );
}

function Authorize({ registration, onClose }: { registration: Registration; onClose: () => void }) {
  const instances = useQuery({
    queryKey: ["instances"],
    queryFn: () => api<Instance[]>("instances"),
  });
  const defaults = useQuery({
    queryKey: ["default-binding-policy"],
    queryFn: () => api<Policy>("default-policy?kind=binding"),
  });
  const [instance, setInstance] = useState("");
  const [edited, setEdited] = useState<Policy>();
  const [busy, setBusy] = useState(false);
  const policy = edited ?? defaults.data;
  const client = useQueryClient();
  return (
    <Modal title={`授权实体 · ${registration.name}`} onClose={onClose} wide stable>
      <div className="registration-form form-stack">
        <p className="muted">
          批准后启用此实体，Agent 继续使用原接入配置。以后可在访问实体中调整或增加实例授权。
        </p>
        <Field label="允许访问的实例">
          <select value={instance} onChange={(e) => setInstance(e.target.value)}>
            <option value="">选择实例</option>
            {instances.data
              ?.filter((i) => i.enabled)
              .map((i) => (
                <option key={i.id} value={i.id}>
                  {i.name}
                </option>
              ))}
          </select>
        </Field>
        {instance && policy && (
          <ScopePicker
            instanceId={instance}
            resources={policy.resources ?? []}
            onChange={(resources) => setEdited({ ...policy, resources })}
          />
        )}
        {policy && <PolicyEditor value={policy} onChange={setEdited} />}
        {(instances.error || defaults.error) && (
          <p className="error">{errorMessage(instances.error ?? defaults.error)}</p>
        )}
      </div>
      <div className="form-actions">
        <Button onClick={onClose}>取消</Button>
        <Button
          variant="primary"
          disabled={busy || !instance || !policy}
          onClick={async () => {
            setBusy(true);
            try {
              await api(`registrations/${registration.id}/approve`, "POST", {
                bindings: [
                  { entity_id: registration.entity_id, instance_id: instance, revision: 0, policy },
                ],
              });
              await client.invalidateQueries();
              toast.success("已授权，请让 Agent 重新连接 MCP");
              onClose();
            } catch (e) {
              toast.error(errorMessage(e));
            } finally {
              setBusy(false);
            }
          }}
        >
          {busy ? "正在授权…" : "批准登记并授权"}
        </Button>
      </div>
    </Modal>
  );
}
