import { useState } from "react";
import * as Popover from "@radix-ui/react-popover";
import { Check, ChevronRight, Circle, Settings2 } from "lucide-react";
import { toast } from "sonner";
import { getSession, switchProfile, errorMessage, type SessionView } from "../lib/api";
import { suspendSession } from "../lib/desktop";

export function ProfileMenu({
  session,
  connected,
  collapsed,
  onChange,
  onSettings,
}: {
  session?: SessionView;
  connected: boolean;
  collapsed: boolean;
  onChange: (s: SessionView) => void;
  onSettings: () => void;
}) {
  const [open, setOpen] = useState(false),
    [busy, setBusy] = useState(false);
  const activate = async (id: string) => {
    if (busy || !session || id === session.current?.id) return;
    if (!window.confirm("切换配置将关闭未保存的表单，继续？")) return;
    setBusy(true);
    suspendSession();
    try {
      onChange(await switchProfile(id, true));
      setOpen(false);
    } catch (e) {
      toast.error(errorMessage(e), { id: "session-error" });
      try {
        onChange(await getSession());
      } catch (error) {
        toast.error(errorMessage(error));
      }
    } finally {
      setBusy(false);
    }
  };
  return (
    <Popover.Root open={open} onOpenChange={setOpen}>
      <Popover.Trigger asChild>
        <button className="server-profile" title="切换配置" aria-label="切换配置">
          <Circle size={9} fill="currentColor" className={connected ? "online" : "offline"} />
          {!collapsed && (
            <>
              <div>
                <strong>{session?.current?.name ?? "选择配置"}</strong>
                <small>{session?.busy ? "切换中" : connected ? "已连接" : "未连接"}</small>
              </div>
              <ChevronRight size={14} />
            </>
          )}
        </button>
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content
          className="profile-menu"
          side="right"
          align="end"
          sideOffset={8}
          collisionPadding={8}
          aria-label="配置快捷切换"
        >
          <div className="profile-menu-list">
            {session?.profiles.map((p) => (
              <button
                key={p.id}
                className="profile-menu-item"
                data-active={p.id === session.current?.id}
                disabled={busy || session.busy}
                onClick={() => void activate(p.id)}
              >
                <span>{p.name}</span>
                {p.id === session.current?.id && <Check size={15} />}
              </button>
            ))}
          </div>
          <button
            className="profile-menu-settings"
            onClick={() => {
              setOpen(false);
              onSettings();
            }}
          >
            <Settings2 size={15} />
            配置与 MCP Server
          </button>
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}
