import { invoke, acceptSession } from "./desktop";
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
export type BackupInfo = components["schemas"]["BackupInfo"];
export interface Profile {
  id: string;
  name: string;
  mode: "local" | "existing";
  preferences: DesktopPreferences;
  import_legacy_ui?: boolean;
  endpoint: string;
  data_dir: string;
  executable: string;
  server_id: string;
}
export interface DesktopPreferences {
  theme: "system" | "light" | "dark";
  notifications: boolean;
  close_to_tray: boolean;
  close_behavior: "ask" | "tray" | "exit";
  lightweight_mode: boolean;
}
export const getPreferences = () => invoke<DesktopPreferences>("get_preferences");
export const setDesktopPreferences = (
  closeBehavior: DesktopPreferences["close_behavior"],
  lightweightMode: boolean,
  theme?: string,
  notifications?: boolean,
) =>
  invoke<DesktopPreferences>("set_desktop_preferences", {
    closeBehavior,
    lightweightMode,
    theme,
    notifications,
  });
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
  deployed_version?: string;
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
  const active = await getSession();
  if (active.current?.id !== profile.id || !active.connected) throw new Error("连接期间配置已切换");
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
  if (typeof status.running !== "boolean") throw new Error("本机 MCP Server 状态响应无效");
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

export interface SessionView {
  current: Profile | null;
  profiles: Profile[];
  generation: number;
  connected: boolean;
  owned_server?: boolean;
  busy: boolean;
  error?: string;
  preferences: DesktopPreferences;
}
let healthGeneration = -1;
function rememberSession(view: SessionView) {
  acceptSession(view);
  if (view.generation > healthGeneration) {
    serverHealth = undefined;
    healthGeneration = view.generation;
  }
  return view;
}
export async function getSession() {
  return rememberSession(await invoke<SessionView>("get_session"));
}
export async function saveProfile(profile: Profile, token: string) {
  const view = await invoke<SessionView>("save_profile", { profile, token: token || null });
  return rememberSession(view);
}
export async function switchProfile(id: string, connectNow: boolean) {
  const view = await invoke<SessionView>("switch_profile", { id, connectNow });
  return rememberSession(view);
}
export async function disconnectSession() {
  const view = await invoke<SessionView>("disconnect_session");
  return rememberSession(view);
}
export async function deleteProfile(id: string) {
  const view = await invoke<SessionView>("delete_profile", { id });
  return rememberSession(view);
}
