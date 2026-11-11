import { invoke } from "./desktop";
import type { components } from "./contract";
import { serverCompatibility } from "./server-version";

export type Instance = components["schemas"]["Instance"];
export type Policy = components["schemas"]["Policy"];
export type MaskRule = components["schemas"]["MaskRule"];
export type Entity = components["schemas"]["Entity"];
export type Binding = components["schemas"]["Binding"];
export type Credential = components["schemas"]["Credential"];
export type Registration = components["schemas"]["Registration"];
export type Approval = components["schemas"]["Request"];
export type Audit = components["schemas"]["Audit"];
export type AuditDetail = components["schemas"]["AuditDetail"];
export type Column = components["schemas"]["Column"];
export type Template = components["schemas"]["Template"];
export type Effective = components["schemas"]["Effective"];
export type Configuration = components["schemas"]["Configuration"];
export interface Profile {
  endpoint: string;
  data_dir: string;
  executable: string;
  server_id: string;
}
export interface DesktopPreferences {
  close_to_tray: boolean;
  close_behavior: "ask" | "tray" | "exit";
  lightweight_mode: boolean;
}
export const getPreferences = () => invoke<DesktopPreferences>("get_preferences");
export const setDesktopPreferences = (
  closeBehavior: DesktopPreferences["close_behavior"],
  lightweightMode: boolean,
) => invoke<DesktopPreferences>("set_desktop_preferences", { closeBehavior, lightweightMode });
export interface Health {
  server_id: string;
  data_dir: string;
  version: string;
  api_version: number;
  mode: string;
  drivers: Record<string, boolean>;
}
export interface LocalStatus extends Partial<Health> {
  running: boolean;
}
export interface LocalDefaults {
  executable?: string;
  bundled: boolean;
  managed_runtime?: boolean;
  bundled_executable?: string;
  bundled_build?: LocalDefaults["build"];
  platform: string;
  build?: {
    default_data_dir: string;
    system_data_dir: string;
    version: string;
    drivers: Record<string, boolean>;
  };
}
export const localDefaults = (executable?: string, dataDir?: string) =>
  invoke<LocalDefaults>("local_defaults", {
    executable: executable || null,
    dataDir: dataDir || null,
  });
let serverHealth: Health | undefined;
export async function api<T>(path: string, method = "GET", body?: unknown): Promise<T> {
  const problem = serverCompatibility(serverHealth);
  if (path !== "health" && path !== "diagnostics" && problem) throw new Error(problem);
  const result = await invoke<T>("admin_request", {
    method,
    path: `/admin/v1/${path}`,
    body: body ?? null,
  });
  if (path === "health") serverHealth = result as Health;
  return result;
}
export const getProfile = () => invoke<Profile | null>("get_profile");
export const connect = async (profile: Profile, token: string) => {
  const health = await invoke<Health>("connect_profile", { profile, token: token || null });
  serverHealth = health;
  return health;
};
export const localCommand = (profile: Profile, action: string) =>
  invoke<Record<string, unknown>>("local_command", {
    executable: profile.executable,
    dataDir: profile.data_dir,
    action,
  });
export async function getLocalStatus(profile: Profile): Promise<LocalStatus> {
  const status = await localCommand(profile, "status");
  if (typeof status.running !== "boolean") throw new Error("本机 Server 状态响应无效");
  return { ...status, running: status.running };
}
export const localRestore = (profile: Profile, backup: string) =>
  invoke<Record<string, string>>("local_restore", {
    executable: profile.executable,
    dataDir: profile.data_dir,
    backup,
  });
export function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : String(error);
}
