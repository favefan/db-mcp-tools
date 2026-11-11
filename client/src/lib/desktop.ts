import { invoke as tauriInvoke, isTauri } from "@tauri-apps/api/core";

export const isPreview = () =>
  import.meta.env.DEV &&
  (import.meta.env.MODE === "ui-preview" || import.meta.env.VITE_UI_PREVIEW === "1") &&
  !isTauri();
export const hasDesktop = () => isTauri() || isPreview();

export async function invoke<T>(command: string, args?: Record<string, unknown>): Promise<T> {
  if (isTauri()) {
    try {
      return await tauriInvoke<T>(command, args);
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
    return previewInvoke(command, args) as Promise<T>;
  }
  throw new Error("请打开桌面客户端连接 Server");
}
