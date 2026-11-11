import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Download, ScrollText } from "lucide-react";
import { api, errorMessage, type Audit, type Entity, type Instance } from "../lib/api";
import { Badge, Empty, Button, DataTable } from "../components/ui";
import { Page } from "../components/page-header";
import { statusNames } from "../lib/status";
import { DiagnosticExport } from "../components/diagnostic-export";
import { RequestDetails } from "./request-details";

export function Logs() {
  const [entity, setEntity] = useState("");
  const [instance, setInstance] = useState("");
  const [decision, setDecision] = useState("");
  const [before, setBefore] = useState(0);
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [selected, setSelected] = useState<Audit>();
  const entities = useQuery({ queryKey: ["entities"], queryFn: () => api<Entity[]>("entities") });
  const instances = useQuery({
    queryKey: ["instances"],
    queryFn: () => api<Instance[]>("instances"),
  });
  const q = useQuery({
    queryKey: ["audit", entity, instance, decision, before, from, to],
    queryFn: () =>
      api<Audit[]>(
        `audit-events?${new URLSearchParams({ entity_id: entity, instance_id: instance, decision, before: String(before), from: from ? new Date(from).toISOString() : "", to: to ? new Date(to).toISOString() : "" })}`,
      ),
  });
  const exportLogs = () => {
    const url = URL.createObjectURL(
      new Blob([JSON.stringify(q.data, null, 2)], { type: "application/json" }),
    );
    const a = document.createElement("a");
    a.href = url;
    a.download = `dbmcp-audit-${new Date().toISOString().slice(0, 10)}.json`;
    a.click();
    URL.revokeObjectURL(url);
  };
  return (
    <Page
      title="访问日志"
      icon={ScrollText}
      description="查询、审批、拒绝与管理变更；默认保留 30 天。"
      actions={
        <>
          <DiagnosticExport />
          <Button disabled={!q.data?.length} onClick={exportLogs}>
            <Download size={15} />
            导出当前结果
          </Button>
        </>
      }
    >
      <div className="filters">
        <select
          aria-label="按实体筛选"
          value={entity}
          onChange={(e) => {
            setEntity(e.target.value);
            setBefore(0);
          }}
        >
          <option value="">全部实体</option>
          {entities.data?.map((e) => (
            <option value={e.id} key={e.id}>
              {e.name}
            </option>
          ))}
        </select>
        <select
          aria-label="按实例筛选"
          value={instance}
          onChange={(e) => {
            setInstance(e.target.value);
            setBefore(0);
          }}
        >
          <option value="">全部实例</option>
          {instances.data?.map((i) => (
            <option key={i.id} value={i.id}>
              {i.name}
            </option>
          ))}
        </select>
        <select
          aria-label="按结果筛选"
          value={decision}
          onChange={(e) => {
            setDecision(e.target.value);
            setBefore(0);
          }}
        >
          <option value="">全部结果</option>
          {Object.entries(statusNames).map(([key, name]) => (
            <option key={key} value={key}>
              {name}
            </option>
          ))}
        </select>
      </div>
      <div className="filters">
        <label className="field">
          开始时间
          <input
            type="datetime-local"
            value={from}
            onChange={(e) => {
              setFrom(e.target.value);
              setBefore(0);
            }}
          />
        </label>
        <label className="field">
          结束时间
          <input
            type="datetime-local"
            value={to}
            onChange={(e) => {
              setTo(e.target.value);
              setBefore(0);
            }}
          />
        </label>
      </div>
      {q.error ? (
        <Empty title="日志读取失败">{errorMessage(q.error)}</Empty>
      ) : !q.data?.length ? (
        <Empty title="没有匹配的日志" />
      ) : (
        <DataTable
          label="访问日志"
          columns={["时间", "实体 / 实例", "操作", "结果", "耗时", "详情"]}
        >
          {q.data.map((v) => (
            <tr
              key={v.id}
              className="audit-row"
              data-audit-id={v.id}
              tabIndex={0}
              aria-selected={selected?.id === v.id}
              onClick={() => setSelected(v)}
              onKeyDown={(e) => {
                if (e.target === e.currentTarget && (e.key === "Enter" || e.key === " ")) {
                  e.preventDefault();
                  setSelected(v);
                }
              }}
            >
              <td>{new Date(v.time).toLocaleString()}</td>
              <td>
                <div>
                  {entities.data?.find((e) => e.id === v.entity_id)?.name ??
                    (v.entity_id ? "历史实体" : "管理员")}
                </div>
                <small>{instances.data?.find((i) => i.id === v.instance_id)?.name ?? "—"}</small>
              </td>
              <td>
                <code>{v.action}</code>
              </td>
              <td>
                <Badge
                  tone={
                    v.decision === "succeeded"
                      ? "green"
                      : v.decision === "denied" || v.decision === "failed"
                        ? "red"
                        : "neutral"
                  }
                >
                  {statusNames[v.decision] ?? v.decision}
                </Badge>
              </td>
              <td>{v.duration_ms} ms</td>
              <td>
                <Button
                  variant="text"
                  onClick={(e) => {
                    e.stopPropagation();
                    setSelected(v);
                  }}
                >
                  查看详情
                </Button>
              </td>
            </tr>
          ))}
        </DataTable>
      )}
      <div className="form-actions">
        {before !== 0 && <Button onClick={() => setBefore(0)}>返回最新</Button>}
        <Button
          disabled={!q.data?.length || q.data.length < 200}
          onClick={() => setBefore(q.data?.at(-1)?.id ?? 0)}
        >
          更早记录
        </Button>
      </div>
      {selected && (
        <RequestDetails
          event={selected}
          records={q.data ?? []}
          onSelect={setSelected}
          onClose={() => setSelected(undefined)}
        />
      )}
    </Page>
  );
}
