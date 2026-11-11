import { invoke as tauriInvoke, isTauri } from "@tauri-apps/api/core";

export const isPreview = () =>
  import.meta.env.DEV &&
  (import.meta.env.MODE === "ui-preview" || import.meta.env.VITE_UI_PREVIEW === "1") &&
  !isTauri();
export const hasDesktop = () => isTauri() || isPreview();

export interface SessionStamp {
  profile_id: string;
  generation: number;
}
let session: SessionStamp | undefined;
let acceptedGeneration = -1;
let epoch = 0;
export function acceptSession(current: { current: { id: string } | null; generation: number }) {
  if (current.generation < acceptedGeneration) return;
  acceptedGeneration = current.generation;
  if (session?.profile_id !== current.current?.id || session?.generation !== current.generation)
    epoch++;
  session = current.current
    ? { profile_id: current.current.id, generation: current.generation }
    : undefined;
}
export function suspendSession() {
  epoch++;
}
const guarded = new Set([
  "confirm_close",
  "admin_request",
  "local_command",
  "local_restore",
  "connect_profile",
  "set_desktop_preferences",
  "sync_notifications",
  "integration_list",
  "integration_preview",
  "integration_install",
  "integration_remove_preview",
  "integration_remove",
  "integration_export",
  "disconnect_session",
]);
export class SessionChangedError extends Error {
  constructor() {
    super("配置已切换，请在当前配置重新操作");
  }
}
export async function invoke<T>(command: string, args?: Record<string, unknown>): Promise<T> {
  const generation = epoch;
  const protectedCall = guarded.has(command);
  const payload = protectedCall ? { ...args, session } : args;
  if (isTauri()) {
    try {
      const result = await tauriInvoke<T>(command, payload);
      if (
        protectedCall &&
        !["connect_profile", "disconnect_session", "confirm_close"].includes(command) &&
        generation !== epoch
      )
        throw new SessionChangedError();
      return result;
    } catch (error) {
      if (command !== "frontend_error")
        void tauriInvoke("frontend_error", {
          kind: "mutation",
          operation: command,
          message: error instanceof Error ? error.message : String(error),
          stack: error instanceof Error ? error.stack : null,
        }).catch(() => {});
      throw error;
    }
  }
  if (import.meta.env.DEV && isPreview()) {
    const { previewInvoke } = await import("./preview");
    const result = await previewInvoke(command, payload);
    if (
      protectedCall &&
      !["connect_profile", "disconnect_session", "confirm_close"].includes(command) &&
      generation !== epoch
    )
      throw new SessionChangedError();
    return result as T;
  }
  throw new Error("请打开桌面客户端连接 MCP Server");
}
