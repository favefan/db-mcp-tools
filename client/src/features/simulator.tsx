import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { api, errorMessage, type Binding, type Credential, type Instance } from "../lib/api";
import { Field, Badge, Button } from "../components/ui";

export function Simulator({
  entity,
  bindings,
  credentials,
  instances,
}: {
  entity: string;
  bindings: Binding[];
  credentials: Credential[];
  instances: Instance[];
}) {
  const [instance, setInstance] = useState(bindings[0]?.instance_id ?? "");
  const [selectedDatabase, setDatabase] = useState("");
  const databases = useQuery({
    queryKey: ["databases", instance],
    queryFn: () => api<string[]>(`instances/${instance}/databases`),
    enabled: !!instance,
  });
  const database = databases.data?.includes(selectedDatabase)
    ? selectedDatabase
    : (databases.data?.[0] ?? "");
  const [credential, setCredential] = useState(credentials.find((c) => !c.revoked)?.id ?? "");
  const [source, setSource] = useState("127.0.0.1");
  const [sql, setSQL] = useState("");
  const [tool, setTool] = useState("db_query");
  const [result, setResult] = useState<{ decision: string; statement: unknown }>();
  const [busy, setBusy] = useState(false);
  return (
    <section className="panel form-stack">
      <h3>规则模拟器</h3>
      <div className="form-grid">
        <Field label="实例">
          <select value={instance} onChange={(e) => setInstance(e.target.value)}>
            <option value="">选择已绑定实例</option>
            {bindings.map((b) => (
              <option key={b.instance_id} value={b.instance_id}>
                {instances.find((i) => i.id === b.instance_id)?.name ?? b.instance_id}
              </option>
            ))}
          </select>
        </Field>
        <Field label="目标数据库">
          <select value={database} onChange={(e) => setDatabase(e.target.value)}>
            <option value="">选择数据库</option>
            {databases.data?.map((name) => (
              <option key={name}>{name}</option>
            ))}
          </select>
          {databases.error && <small>{errorMessage(databases.error)}</small>}
        </Field>
        <Field label="有效凭据">
          <select value={credential} onChange={(e) => setCredential(e.target.value)}>
            <option value="">选择凭据</option>
            {credentials
              .filter((c) => !c.revoked)
              .map((c) => (
                <option value={c.id} key={c.id}>
                  {c.prefix}…
                </option>
              ))}
          </select>
        </Field>
        <Field label="请求来源 IP">
          <input value={source} onChange={(e) => setSource(e.target.value)} />
        </Field>
        <Field label="操作工具">
          <select value={tool} onChange={(e) => setTool(e.target.value)}>
            <option value="db_query">查询</option>
            <option value="db_execute">写入</option>
            <option value="db_explain">非执行计划</option>
          </select>
        </Field>
      </div>
      <Field label="SQL">
        <textarea
          rows={5}
          value={sql}
          onChange={(e) => setSQL(e.target.value)}
          placeholder="SELECT id FROM public.users WHERE id = $1"
        />
      </Field>
      <Button
        disabled={busy || !instance || !database || !credential || !sql}
        onClick={async () => {
          setBusy(true);
          try {
            setResult(
              await api("simulate", "POST", {
                entity_id: entity,
                credential_id: credential,
                source,
                query: {
                  instance_id: instance,
                  database,
                  sql,
                  tool,
                  client_request_id: "simulation",
                },
              }),
            );
          } catch (e) {
            setResult({ decision: "denied", statement: { reason: errorMessage(e) } });
            toast.error(errorMessage(e));
          } finally {
            setBusy(false);
          }
        }}
      >
        {busy ? "正在分析…" : "分析规则"}
      </Button>
      {result && (
        <div>
          <Badge
            tone={
              result.decision === "allowed"
                ? "green"
                : result.decision === "denied"
                  ? "red"
                  : "orange"
            }
          >
            {result.decision === "allowed"
              ? "允许"
              : result.decision === "denied"
                ? "拒绝"
                : "需要审批"}
          </Badge>
          <pre>{JSON.stringify(result.statement, null, 2)}</pre>
        </div>
      )}
    </section>
  );
}
