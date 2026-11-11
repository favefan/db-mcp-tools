import { isTauri, invoke } from "@tauri-apps/api/core";

export function reportFrontend(kind: string, error: unknown, stack?: string) {
  if (!isTauri()) return;
  void invoke("frontend_error", {
    kind,
    operation: null,
    message: error instanceof Error ? error.message : String(error),
    stack: stack ?? (error instanceof Error ? error.stack : null),
  }).catch(() => {});
}
export function installErrorReporting() {
  window.addEventListener("error", (event) =>
    reportFrontend("error", event.error ?? event.message),
  );
  window.addEventListener("unhandledrejection", (event) =>
    reportFrontend("rejection", event.reason),
  );
}
