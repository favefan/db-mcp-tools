import { Button } from "./ui";
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Download } from "lucide-react";
import { toast } from "sonner";
import { isTauri } from "@tauri-apps/api/core";
import { invoke } from "../lib/desktop";
import { errorMessage } from "../lib/api";

export function DiagnosticExport({ details = false }: { details?: boolean }) {
  const [busy, setBusy] = useState(false);
  const info = useQuery({
    queryKey: ["diagnostics-info"],
    queryFn: () => invoke<{ desktop_dir: string; process_dir: string }>("diagnostics_info"),
    enabled: isTauri(),
    staleTime: Infinity,
  });
  return (
    <div className={details ? "settings-panel" : ""}>
      {details && (
        <>
          <h2>诊断日志</h2>
          <p className="muted">
            导出客户端、Bridge 和 Server
            的运行记录、版本与审计摘要。连接不可用时仍可导出本地日志，包内会注明缺失项。
          </p>
          <p className="muted">自动轮转，单文件 2 MiB；不记录凭据、原始 SQL、参数或查询结果。</p>
          {info.data && (
            <p className="muted">
              本机日志：<code>{info.data.desktop_dir}</code>
              <br />
              CLI / Bridge：<code>{info.data.process_dir}</code>
            </p>
          )}
        </>
      )}
      <Button
        disabled={busy || !isTauri()}
        onClick={async () => {
          setBusy(true);
          try {
            const result = await invoke<{ path: string; server_status: string } | null>(
              "diagnostics_export",
            );
            if (result)
              toast.success(
                result.server_status === "collected"
                  ? "诊断包已导出，包含 Server 日志"
                  : "本地诊断包已导出；Server 未连接或不支持采集，缺失项已注明",
                { description: result.path },
              );
          } catch (error) {
            toast.error(errorMessage(error));
          } finally {
            setBusy(false);
          }
        }}
      >
        <Download size={15} />
        {busy ? "正在采集…" : "导出诊断包"}
      </Button>
    </div>
  );
}
