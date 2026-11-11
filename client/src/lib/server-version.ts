import { gte, gt, valid } from "semver";
import type { Health } from "./api";

export const minimumServerVersion = "1.0.2";
export function serverCompatibility(health?: Health): string | undefined {
  if (!health) return;
  if (health.api_version !== 1)
    return `Server API ${health.api_version} 不兼容，请升级客户端或 Server。`;
  if (!valid(health.version) || !gte(health.version, minimumServerVersion))
    return `当前 Server ${health.version} 版本过旧，需要 ${minimumServerVersion} 或更新版本。请升级 Server；保留数据不会阻止程序升级。`;
}
export const newerServer = (candidate: string, current: string) =>
  !!valid(candidate) && !!valid(current) && gt(candidate, current);
