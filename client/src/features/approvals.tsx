import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, X, Clock3, RefreshCw, ClipboardCheck } from "lucide-react";
import { toast } from "sonner";
import {
  api,
  errorMessage,
  type Approval,
  type Entity,
  type Instance,
  type Registration,
} from "../lib/api";
import { Badge, Empty, Modal, Button, ViewTabs, Card } from "../components/ui";
import { Page } from "../components/page-header";
import { statusNames } from "../lib/status";
import { Registrations } from "./registrations";
export function Approvals({
  initialTab = "operations",
  focusEntity,
}: {
  initialTab?: "operations" | "registrations";
  focusEntity?: string;
}) {
  const q = useQuery({ queryKey: ["approvals"], queryFn: () => api<Approval[]>("approvals") });
  const registrations = useQuery({
    queryKey: ["registrations"],
    queryFn: () => api<Registration[]>("registrations"),
  });
  const [tab, setTab] = useState(initialTab);
  const registrationCount = registrations.data?.filter((r) => r.status === "pending").length ?? 0;
  const operationCount = q.data?.filter((r) => r.status === "pending").length ?? 0;
  const entities = useQuery({ queryKey: ["entities"], queryFn: () => api<Entity[]>("entities") });
  const instances = useQuery({
    queryKey: ["instances"],
    queryFn: () => api<Instance[]>("instances"),
  });
  const [detail, setDetail] = useState<Approval>();
  const [busy, setBusy] = useState(false);
  const [history, setHistory] = useState(false);
  const client = useQueryClient();
  const decide = async (id: string, action: string) => {
    setBusy(true);
    try {
      await api(`approvals/${id}/${action}`, "POST");
      await client.invalidateQueries();
      setDetail(undefined);
      toast.success(
        action === "deny"
          ? "已拒绝请求"
          : action === "remember"
            ? "已保存限定审批规则，等待 Agent 恢复执行"
            : "已记录批准，等待 Agent 恢复执行",
      );
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };
  const records = q.data?.filter(
    (v) => history || ["pending", "approved", "executing"].includes(v.status),
  );
  return (
    <Page
      title="待审批"
      icon={ClipboardCheck}
      count={operationCount + registrationCount}
      actions={
        <>
          <Button
            onClick={() => {
              void q.refetch();
              void registrations.refetch();
            }}
          >
            <RefreshCw size={15} />
            刷新
          </Button>
        </>
      }
    >
      <ViewTabs
        label="审批类型"
        value={tab}
        onValueChange={setTab}
        items={[
          {
            value: "operations",
            label: (
              <>数据库操作 {operationCount > 0 && <Badge tone="orange">{operationCount}</Badge>}</>
            ),
          },
          {
            value: "registrations",
            label: (
              <>
                实体登记 {registrationCount > 0 && <Badge tone="orange">{registrationCount}</Badge>}
              </>
            ),
          },
        ]}
      />
      <div hidden={tab !== "registrations"}>
        <Registrations focusEntity={focusEntity} />
      </div>
      <div hidden={tab !== "operations"}>
        <p className="page-description">批准只对原始请求生效，执行前会重新检查权限。</p>
        <label
          className="check toolbar"
          title="勾选后包含成功、失败、拒绝、过期、取消及结果未知的请求；最近最多 200 条。"
        >
          <input type="checkbox" checked={history} onChange={(e) => setHistory(e.target.checked)} />
          显示近期已处理请求
          <span className="muted">（{records?.length ?? 0} 条）</span>
        </label>
        {q.error ? (
          <Empty title="审批读取失败">{errorMessage(q.error)}</Empty>
        ) : !records?.length ? (
          <Empty title="当前没有待审批操作">Agent 发起需要审批的请求后会显示在这里。</Empty>
        ) : (
          <div className="cards">
            {records.map((v) => (
              <Card kind="approval" key={v.id}>
                <div className="card-title">
                  <Badge tone={v.status === "pending" ? "orange" : "neutral"}>
                    {statusNames[v.status] ?? v.status}
                  </Badge>
                  <time>{new Date(v.created_at).toLocaleString()}</time>
                </div>
                <h2>
                  {v.operation.toUpperCase()}{" "}
                  <span>
                    {v.tables?.map((t) => `${t.schema}.${t.table}`).join(", ") || "无表查询"}
                  </span>
                </h2>
                <p>
                  {entities.data?.find((e) => e.id === v.entity_id)?.name ?? v.entity_id}
                  <span className="dot">→</span>
                  {instances.data?.find((i) => i.id === v.instance_id)?.name ?? v.instance_id}
                  {v.database && (
                    <>
                      <span className="dot">·</span>
                      {v.database}
                    </>
                  )}
                </p>
                {v.agent_reason && <p>请求说明（Agent 提供）：{v.agent_reason}</p>}
                <div className="approval-footer">
                  <small>
                    {["pending", "approved", "executing"].includes(v.status) ? (
                      <>
                        <Clock3 size={13} />
                        <Remaining expires={v.expires_at} />
                      </>
                    ) : (
                      "请求已结束"
                    )}
                  </small>
                  <div className="actions">
                    <Button
                      onClick={async () => {
                        try {
                          setDetail(await api<Approval>(`approvals/${v.id}`));
                        } catch (e) {
                          toast.error(errorMessage(e));
                        }
                      }}
                    >
                      查看请求
                    </Button>
                    {v.status === "pending" && (
                      <>
                        <Button danger disabled={busy} onClick={() => void decide(v.id, "deny")}>
                          <X size={15} />
                          拒绝
                        </Button>
                        <Button
                          variant="primary"
                          disabled={busy}
                          onClick={() => void decide(v.id, "approve")}
                        >
                          <Check size={15} />
                          允许一次
                        </Button>
                      </>
                    )}
                  </div>
                </div>
              </Card>
            ))}
          </div>
        )}
        {detail && (
          <Modal title="审批请求详情" onClose={() => setDetail(undefined)} wide>
            <Badge tone="orange">{statusNames[detail.status]}</Badge>
            <p className="muted">
              实体：
              {entities.data?.find((e) => e.id === detail.entity_id)?.name ?? detail.entity_id}
              {" · "}实例：
              {instances.data?.find((i) => i.id === detail.instance_id)?.name ?? detail.instance_id}
              {" · "}数据库：{detail.database || "未记录"}
              {" · "}来源：{detail.source || "未记录"}
            </p>
            {detail.status === "pending" &&
              ["insert", "update", "delete"].includes(detail.operation) && (
                <p>写入提醒：成功执行后不会自动撤销，请在批准前核对 SQL、参数和目标范围。</p>
              )}
            {(detail.agent_reason || detail.query?.agent_reason) && (
              <p>请求说明（Agent 提供）：{detail.agent_reason || detail.query?.agent_reason}</p>
            )}
            <pre className="sql">{detail.query?.sql ?? "原始载荷已清理"}</pre>
            {detail.query?.params && (
              <details>
                <summary>查看绑定参数</summary>
                <pre>{detail.parameter_json ?? JSON.stringify(detail.query.params, null, 2)}</pre>
              </details>
            )}
            <p className="muted">请求 {detail.id}</p>
            {detail.result && <pre>{JSON.stringify(detail.result, null, 2)}</pre>}
            {detail.status === "pending" && (
              <p className="muted">
                记住规则后，此实体在相同实例的 {detail.database} 数据库上对{" "}
                {detail.tables?.map((t) => `${t.schema}.${t.table}`).join(", ")} 的{" "}
                {detail.operation.toUpperCase()} 操作可按限定范围自动放行，仍受当前资源权限与 Mask
                约束。
              </p>
            )}
            {detail.status === "pending" && (
              <div className="form-actions">
                <Button
                  disabled={busy || !detail.tables?.length}
                  onClick={() => void decide(detail.id, "remember")}
                >
                  记住此表与操作范围
                </Button>
                <Button danger disabled={busy} onClick={() => void decide(detail.id, "deny")}>
                  拒绝
                </Button>
                <Button
                  variant="primary"
                  disabled={busy}
                  onClick={() => void decide(detail.id, "approve")}
                >
                  允许一次
                </Button>
              </div>
            )}
          </Modal>
        )}
      </div>
    </Page>
  );
}
function Remaining({ expires }: { expires: string }) {
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);
  const seconds = Math.max(0, Math.ceil((Date.parse(expires) - now) / 1000));
  return (
    <>{seconds > 0 ? `剩余 ${Math.floor(seconds / 60)} 分 ${seconds % 60} 秒` : "有效期已结束"}</>
  );
}
