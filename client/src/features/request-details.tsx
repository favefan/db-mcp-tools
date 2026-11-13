import { useEffect, useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowLeft, ArrowRight, Copy, Download, RefreshCw } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage, type Audit, type AuditDetail } from "../lib/api";
import { Badge, Button, Drawer, Empty, Stack } from "../components/ui";
import { statusNames } from "../lib/status";

const eventNames: Record<string, string> = {
  "request.received": "收到请求",
  "policy.checked": "权限检查",
  "execution.start": "开始执行",
  "execution.recheck": "执行前权限重检",
  execute: "执行结束",
  approval: "审批决定",
  "approval.remember": "保存审批规则",
  db_query: "查询请求",
  db_execute: "写入请求",
  db_explain: "执行计划请求",
};
const maskModes: Record<string, string> = {
  fixed: "固定替换",
  partial: "部分保留",
  null: "设为 NULL",
  regex: "正则替换",
  deny: "禁止访问",
};
const decisionNames: Record<string, string> = {
  ...statusNames,
  received: "已接收",
  allowed: "通过",
};
const displayTime = (value?: string) => (value ? new Date(value).toLocaleString() : "未记录");
const tone = (status: string) =>
  status === "succeeded"
    ? "green"
    : ["failed", "denied", "result_unknown"].includes(status)
      ? "red"
      : status === "pending"
        ? "orange"
        : "neutral";

function Fields({ values }: { values: [string, string | number | undefined][] }) {
  return (
    <dl className="detail-fields">
      {values.map(([name, value]) => (
        <div key={name}>
          <dt>{name}</dt>
          <dd>{value === undefined || value === "" ? "未记录" : value}</dd>
        </div>
      ))}
    </dl>
  );
}

export function RequestDetails({
  event,
  records,
  onSelect,
  onClose,
}: {
  event: Audit;
  records: Audit[];
  onSelect: (event: Audit) => void;
  onClose: () => void;
}) {
  const q = useQuery({
    queryKey: ["audit-detail", event.id],
    queryFn: () => api<AuditDetail>(`audit-events/${event.id}`),
    refetchInterval: (query) =>
      ["pending", "approved", "executing"].includes(query.state.data?.request?.status ?? "")
        ? 3000
        : false,
  });
  const body = useRef<HTMLDivElement>(null);
  useEffect(() => {
    body.current?.closest(".drawer-body")?.scrollTo(0, 0);
  }, [event.id]);
  const index = records.findIndex((v) => v.id === event.id);
  const data = q.data;
  const request = data?.request;
  const status = request?.status ?? event.request_status ?? event.decision;
  const exportRequest = () => {
    if (!data) return;
    const url = URL.createObjectURL(
      new Blob(
        [JSON.stringify({ version: 1, exported_at: new Date().toISOString(), ...data }, null, 2)],
        { type: "application/json" },
      ),
    );
    const a = document.createElement("a");
    a.href = url;
    a.download = `dbmcp-request-${request?.id ?? event.id}.json`;
    a.click();
    URL.revokeObjectURL(url);
  };
  return (
    <Drawer
      title="请求详情"
      description="查看请求内容、执行摘要和关联事件；导出包含已保存的 SQL 与参数。"
      onClose={onClose}
      actions={
        <>
          <div className="actions">
            <Button
              variant="icon"
              aria-label="上一条日志"
              disabled={index <= 0}
              onClick={() => onSelect(records[index - 1])}
            >
              <ArrowLeft size={16} />
            </Button>
            <Button
              variant="icon"
              aria-label="下一条日志"
              disabled={index < 0 || index >= records.length - 1}
              onClick={() => onSelect(records[index + 1])}
            >
              <ArrowRight size={16} />
            </Button>
            <Badge tone={tone(status)}>{decisionNames[status] ?? status}</Badge>
          </div>
          <div className="actions">
            <Button
              variant="icon"
              aria-label="刷新请求详情"
              disabled={q.isFetching}
              onClick={() => void q.refetch()}
            >
              <RefreshCw size={16} />
            </Button>
            <Button disabled={!data} onClick={exportRequest}>
              <Download size={15} />
              导出该请求
            </Button>
          </div>
        </>
      }
    >
      <div ref={body}>
        {q.isPending ? (
          <Empty title="正在读取请求详情…" />
        ) : q.error ? (
          <Empty title="请求详情读取失败">
            {errorMessage(q.error)}
            {" · 可使用上方刷新按钮重试；请确认 MCP Server 已升级至 1.0.8。"}
          </Empty>
        ) : (
          data && (
            <Stack>
              <section className="detail-section">
                <h3>概览</h3>
                <div className="request-identity">
                  <code>{request?.id ?? (event.request_id || `日志 ${event.id}`)}</code>
                  <Button
                    variant="icon"
                    aria-label="复制请求 ID"
                    disabled={!request?.id && !event.request_id}
                    onClick={() => {
                      void navigator.clipboard.writeText(request?.id || event.request_id).then(
                        () => toast.success("请求 ID 已复制"),
                        () => toast.error("复制失败，请手动选择请求 ID"),
                      );
                    }}
                  >
                    <Copy size={15} />
                  </Button>
                </div>
                <Fields
                  values={[
                    ["发起时间", displayTime(request?.created_at ?? event.time)],
                    ["结束时间", displayTime(request?.finished_at)],
                    ["执行耗时", `${request?.result ? request.duration_ms : event.duration_ms} ms`],
                    ["实体", request?.entity_name || event.entity_id || "管理员"],
                    ["客户端", request?.client],
                    ["来源", request?.source],
                    ["实例", request?.instance_name || event.instance_id],
                    ["数据库", request?.database],
                    ["工具／操作", request?.tool || event.action],
                    ["请求说明（Agent 提供）", request?.agent_reason],
                    ["诊断 ID", request?.trace_id],
                  ]}
                />
              </section>
              <section className="detail-section">
                <h3>请求内容</h3>
                {data.payload_state === "available" && request ? (
                  <>
                    <pre className="sql detail-code">{request.sql || "未记录 SQL"}</pre>
                    <h4>绑定参数</h4>
                    <pre className="detail-code">
                      {request.parameter_json === "null" || !request.parameter_json
                        ? "[]"
                        : request.parameter_json}
                    </pre>
                    <Fields
                      values={[
                        ["客户端请求 ID", request.client_request_id],
                        [
                          "目标对象",
                          request.tables?.map((t) => `${t.schema}.${t.table}`).join(", "),
                        ],
                      ]}
                    />
                  </>
                ) : (
                  <p className="muted">
                    {data.payload_state === "cleared"
                      ? "原始请求内容已清理，无法还原 SQL 和参数。"
                      : "这条记录未保存数据库请求内容。管理操作的记录见下方处理经过。"}
                  </p>
                )}
              </section>
              <section className="detail-section">
                <h3>执行情况</h3>
                {request?.result ? (
                  <>
                    <Fields
                      values={[
                        ["返回行数", request.result.row_count],
                        ["影响行数", request.result.affected],
                        ["返回字段", request.result.columns?.join(", ") || "无"],
                        ["结果截断", request.result.truncated ? "是" : "否"],
                        ["原因", request.result.reason || "无"],
                      ]}
                    />
                    <h4>匹配的 Mask 规则</h4>
                    {request.result.masks?.length ? (
                      <ul className="detail-rules">
                        {request.result.masks.map((r) => (
                          <li key={`${r.layer}:${r.id}`}>
                            <code>{r.target}</code>
                            <span>
                              {r.layer === "instance" ? "实例" : "实体追加"} ·{" "}
                              {maskModes[r.mode] ?? r.mode} · {r.id}
                            </span>
                          </li>
                        ))}
                      </ul>
                    ) : (
                      <p className="muted">没有匹配的 Mask 规则。</p>
                    )}
                    <p className="muted">此处保留执行摘要，不保存结果行内容。</p>
                  </>
                ) : (
                  <p className="muted">
                    {["pending", "approved", "executing"].includes(status)
                      ? "请求尚未结束，完成后显示执行摘要。"
                      : "未记录执行摘要。"}
                  </p>
                )}
              </section>
              <section className="detail-section">
                <h3>
                  处理经过 <span className="muted">{data.events?.length ?? 0} 条</span>
                </h3>
                <ol className="request-timeline">
                  {(data.events ?? []).map((item) => (
                    <li key={item.id}>
                      <div>
                        <strong>{eventNames[item.action] ?? item.action}</strong>
                        <Badge tone={tone(item.decision)}>
                          {decisionNames[item.decision] ?? item.decision}
                        </Badge>
                      </div>
                      <time>
                        {displayTime(item.time)} · {item.duration_ms} ms
                      </time>
                      {item.detail && <p>{item.detail}</p>}
                    </li>
                  ))}
                </ol>
                {data.events_truncated && (
                  <p className="muted">关联事件较多，当前展示最早的 1000 条。</p>
                )}
              </section>
            </Stack>
          )
        )}
      </div>
    </Drawer>
  );
}
