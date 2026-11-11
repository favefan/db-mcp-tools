import { useState } from "react";
import { Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage, type Policy, type MaskRule } from "../lib/api";
import { Field, Button } from "../components/ui";

export function PolicyEditor({
  value,
  onChange,
}: {
  value: Policy;
  onChange: (v: Policy) => void;
}) {
  const update = <K extends keyof Policy>(key: K, v: Policy[K]) => onChange({ ...value, [key]: v });
  return (
    <div className="form-stack">
      <Field
        label="可访问的表与字段"
        hint="建议在范围页选择。对象键包含数据库，整库、模式、表和字段可分别许可；* 允许范围内全部对象。实例与实体范围取交集。旧的 schema.table[.column] 规则仍适用于各数据库；名称含特殊字符时使用选择器。"
      >
        <textarea
          rows={4}
          value={(value.resources ?? []).join("\n")}
          onChange={(e) => update("resources", e.target.value.split("\n").filter(Boolean))}
        />
      </Field>
      <div className="field">
        <span>允许的操作</span>
        <div className="checks">
          {["select", "insert", "update", "delete"].map((op) => (
            <label key={op}>
              <input
                type="checkbox"
                checked={(value.operations ?? []).includes(op)}
                onChange={(e) =>
                  update(
                    "operations",
                    e.target.checked
                      ? [...(value.operations ?? []), op]
                      : (value.operations ?? []).filter((x) => x !== op),
                  )
                }
              />
              {op.toUpperCase()}
            </label>
          ))}
        </div>
      </div>
      <div className="form-grid">
        <Field label="人工审批">
          <select value={value.approval} onChange={(e) => update("approval", e.target.value)}>
            <option value="writes">写入时询问</option>
            <option value="always">每次询问</option>
            <option value="never">无需逐次审批</option>
          </select>
        </Field>
        <Field label="允许的来源 CIDR" hint="每行一个网段，两层规则均需满足。">
          <textarea
            rows={2}
            value={(value.cidrs ?? []).join("\n")}
            onChange={(e) => update("cidrs", e.target.value.split("\n").filter(Boolean))}
          />
        </Field>
        {(
          [
            ["max_rows", "最多返回行数"],
            ["max_bytes", "结果上限（字节）"],
            ["timeout_seconds", "执行超时（秒）"],
            ["concurrency", "实例并发上限"],
            ["approval_seconds", "审批有效期（秒）"],
          ] as const
        ).map(([key, title]) => (
          <Field label={title} key={key}>
            <input
              type="number"
              min={1}
              value={value[key]}
              onChange={(e) => update(key, Number(e.target.value))}
            />
          </Field>
        ))}
      </div>
    </div>
  );
}

export function MaskEditor({
  rules,
  onChange,
}: {
  rules: MaskRule[];
  onChange: (rules: MaskRule[]) => void;
}) {
  const [sample, setSample] = useState("");
  const [source, setSource] = useState({ schema: "", table: "", column: "" });
  const [rulesOpen, setRulesOpen] = useState(false);
  const [preview, setPreview] = useState<unknown>();
  const update = (i: number, v: Partial<MaskRule>) =>
    onChange(rules.map((r, n) => (n === i ? { ...r, ...v } : r)));
  return (
    <div className="form-stack">
      <details
        className="mask-rules"
        open={rulesOpen}
        onToggle={(e) => setRulesOpen(e.currentTarget.open)}
      >
        <summary>字段脱敏规则（{rules.length}）</summary>
        <div className="form-stack">
          {rules.map((r, i) => (
            <div className="mask-row" key={r.id}>
              <Field label="字段或模式">
                <input
                  value={r.target}
                  onChange={(e) => update(i, { target: e.target.value })}
                  placeholder="public.users.email 或 *password*"
                />
              </Field>
              <Field label="处理方式">
                <select value={r.mode} onChange={(e) => update(i, { mode: e.target.value })}>
                  <option value="" disabled>
                    请选择处理方式
                  </option>
                  <option value="fixed">固定替换</option>
                  <option value="partial">部分保留</option>
                  <option value="null">设为 NULL</option>
                  <option value="regex">正则替换</option>
                  <option value="deny">禁止访问</option>
                </select>
              </Field>
              {(r.mode === "fixed" || r.mode === "regex") && (
                <Field label="替换值">
                  <input
                    value={r.value ?? ""}
                    placeholder="例如：[已脱敏]"
                    onChange={(e) => update(i, { value: e.target.value })}
                  />
                </Field>
              )}
              {r.mode === "regex" && (
                <Field label="正则表达式">
                  <input
                    value={r.pattern ?? ""}
                    placeholder="填写正则表达式"
                    onChange={(e) => update(i, { pattern: e.target.value })}
                  />
                </Field>
              )}
              {r.mode === "partial" && (
                <>
                  <Field label="保留开头">
                    <input
                      type="number"
                      min={0}
                      value={r.keep_start ?? 0}
                      onChange={(e) => update(i, { keep_start: Number(e.target.value) })}
                    />
                  </Field>
                  <Field label="保留结尾">
                    <input
                      type="number"
                      min={0}
                      value={r.keep_end ?? 0}
                      onChange={(e) => update(i, { keep_end: Number(e.target.value) })}
                    />
                  </Field>
                </>
              )}
              <Button
                variant="icon"
                danger
                onClick={() => onChange(rules.filter((_, n) => n !== i))}
                aria-label="删除规则"
              >
                <Trash2 size={16} />
              </Button>
            </div>
          ))}
          {!rules.length && <p className="muted">尚未配置字段规则。</p>}
        </div>
      </details>
      <Button
        onClick={() => {
          setRulesOpen(true);
          onChange([...rules, { id: crypto.randomUUID(), target: "", mode: "", value: "" }]);
        }}
      >
        <Plus size={16} />
        添加规则
      </Button>
      <div className="panel rule-preview">
        <h3>规则预览</h3>
        <div className="form-grid preview-source">
          <Field label="Schema">
            <input
              value={source.schema}
              placeholder="例如：public"
              onChange={(e) => setSource({ ...source, schema: e.target.value })}
            />
          </Field>
          <Field label="表">
            <input
              value={source.table}
              placeholder="例如：users"
              onChange={(e) => setSource({ ...source, table: e.target.value })}
            />
          </Field>
          <Field label="字段">
            <input
              value={source.column}
              placeholder="例如：email"
              onChange={(e) => setSource({ ...source, column: e.target.value })}
            />
          </Field>
        </div>
        <Field label="示例值">
          <input
            value={sample}
            placeholder="输入用于预览的示例值"
            onChange={(e) => setSample(e.target.value)}
          />
        </Field>
        <Button
          disabled={!source.schema || !source.table || !source.column}
          onClick={async () => {
            try {
              const v = await api<{ value: unknown }>("mask-preview", "POST", {
                rules,
                source,
                value: sample,
              });
              setPreview(v.value);
            } catch (e) {
              toast.error(errorMessage(e));
            }
          }}
        >
          使用 MCP Server 规则预览
        </Button>
        {preview !== undefined && <pre>{JSON.stringify(preview)}</pre>}
      </div>
    </div>
  );
}
