import { connect, localCommand, localDefaults, type Profile } from "./api";
import { newerServer } from "./server-version";

// Only the app-owned stable launcher is eligible. Remote/custom programs remain
// under explicit user control. Go coordinates backup, deployment and restart.
export async function upgradeBundledRuntime(profile: Profile, onStart: (version: string) => void) {
  if (!profile.data_dir || !profile.executable) return profile;
  const info = await localDefaults(profile.executable, profile.data_dir);
  if (!info.managed_runtime || !info.bundled_executable || !info.bundled_build || !info.build)
    return profile;
  const status = await localCommand(profile, "status");
  if (status.running && profile.server_id && status.server_id !== profile.server_id)
    throw new Error("本机 Server 身份与连接档案不一致，请在设置中确认后升级。");
  const currentVersion = typeof status.version === "string" ? status.version : info.build.version;
  if (!newerServer(info.bundled_build.version, currentVersion)) return profile;
  onStart(info.bundled_build.version);
  const result = await localCommand({ ...profile, executable: info.bundled_executable }, "upgrade");
  const next = {
    ...profile,
    executable: typeof result.launcher === "string" ? result.launcher : profile.executable,
  };
  const running = result.status as { running?: boolean } | undefined;
  if (running?.running) {
    const health = await connect(next, "");
    next.server_id = health.server_id;
  }
  return next;
}
