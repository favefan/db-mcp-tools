import { useEffect, useState } from "react";
import { isTauri } from "@tauri-apps/api/core";
import { listen } from "@tauri-apps/api/event";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { invoke } from "../lib/desktop";
import { errorMessage } from "../lib/api";
import { Modal, Button } from "./ui";

export function CloseDialog({ generation }: { generation?: number }) {
  const [open, setOpen] = useState(false);
  const [remember, setRemember] = useState(false);
  const [busy, setBusy] = useState(false);
  const [exitError, setExitError] = useState("");
  const client = useQueryClient();
  useEffect(() => {
    setOpen(false);
    setRemember(false);
  }, [generation]);
  useEffect(() => {
    if (!isTauri()) return;
    const pending = listen("close-requested", () => {
      setRemember(false);
      setOpen(true);
    });
    const failed = listen<string>("exit-failed", (event) => {
      setExitError(event.payload);
      setOpen(true);
    });
    return () => {
      void pending.then((unlisten) => unlisten());
      void failed.then((unlisten) => unlisten());
    };
  }, []);
  const close = async (action: "tray" | "exit") => {
    setBusy(true);
    try {
      await invoke("confirm_close", { action, remember });
      setOpen(false);
      await client.invalidateQueries({ queryKey: ["desktop-preferences"] });
    } catch (error) {
      setExitError(errorMessage(error));
      toast.error(errorMessage(error));
    } finally {
      setBusy(false);
    }
  };
  return open ? (
    <Modal
      description="选择最小化到托盘或直接退出客户端。"
      title="关闭 DB MCP Tools"
      onClose={() => {
        if (!busy) setOpen(false);
      }}
    >
      {exitError && (
        <>
          <p className="error">{exitError}</p>
          <Button
            disabled={busy}
            onClick={async () => {
              setBusy(true);
              try {
                await invoke("exit_session", { leaveRunning: true });
              } catch (e) {
                setExitError(errorMessage(e));
              } finally {
                setBusy(false);
              }
            }}
          >
            保留 MCP Server 并退出
          </Button>
        </>
      )}
      <label className="check">
        <input type="checkbox" checked={remember} onChange={(e) => setRemember(e.target.checked)} />
        不再提醒，记住本次选择
      </label>
      <div className="form-actions">
        <Button disabled={busy} onClick={() => setOpen(false)}>
          取消
        </Button>
        <Button disabled={busy} onClick={() => void close("exit")}>
          直接退出
        </Button>
        <Button variant="primary" disabled={busy} onClick={() => void close("tray")}>
          最小化到托盘
        </Button>
      </div>
    </Modal>
  ) : null;
}
