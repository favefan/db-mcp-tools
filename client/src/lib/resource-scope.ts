import type { Instance } from "./api";

export const defaultDatabases: Record<string, string> = {
  postgres: "postgres",
  sqlserver: "master",
  mysql: "",
  dm: "",
};
export const resourceName = (name: string) =>
  name.replace(/[%.*?[\]\\/]/g, (char) => `%${char.charCodeAt(0).toString(16).toUpperCase()}`);
export const databaseResource = (database: string, ...parts: string[]) =>
  `db/${resourceName(database)}${parts.length ? `/${parts.map(resourceName).join(".")}` : ""}`;

export function qualifyResources(resources: string[], instance?: Instance): string[] {
  const c = instance?.connection;
  const names =
    c && instance?.driver !== "dm"
      ? c.database_mode === "selected"
        ? c.databases
        : (c.database_mode ?? "single") === "single"
          ? [c.database]
          : undefined
      : undefined;
  return resources.flatMap((key) => {
    if (key.startsWith("db/")) return [key];
    if (names?.length) return names.map((name) => `${databaseResource(name)}/${key}`);
    return [key === "*" ? "*" : `db/*/${key}`];
  });
}
function parents(key: string): string[] {
  const parts = key.split("/");
  if (parts.length !== 3 || parts[0] !== "db") return [];
  const base = `${parts[0]}/${parts[1]}/`,
    path = parts[2].split(".");
  const result = [base + "*"];
  if (path.length >= 2) result.push(base + path[0] + ".*");
  if (path.length === 3) result.push(base + path.slice(0, 2).join("."));
  return result.filter((parent) => parent !== key);
}
export const includesObject = (grants: string[], key: string) =>
  grants.includes("*") ||
  grants.includes(key) ||
  parents(key).some((parent) => grants.includes(parent));
export function compactResources(resources: string[]): string[] {
  const keys = new Set(resources);
  if (keys.has("*")) return ["*"];
  return [...keys].filter((key) => !parents(key).some((parent) => keys.has(parent)));
}
export function scopeConnection(
  connection: Instance["connection"],
  resources: string[],
): Instance["connection"] {
  const databases = new Set<string>();
  for (const key of resources) {
    const parts = key.split("/");
    if (parts.length !== 3 || parts[0] !== "db" || /[?*[\]\\]/.test(parts[1]))
      return { ...connection, database_mode: "all", databases: [] };
    databases.add(decodeURIComponent(parts[1]));
  }
  return {
    ...connection,
    database_mode: databases.size ? "selected" : "all",
    databases: [...databases],
  };
}
export function resourceLabel(key: string, driver: string): string {
  if (key === "*") return "全部可访问对象（含今后新增）";
  if (!key.startsWith("db/")) return key;
  const [, encoded, suffix] = key.split("/");
  const database = encoded === "*" ? "各数据库" : decodeURIComponent(encoded);
  if (suffix === "*") return `${database} · 整个数据库`;
  const parts = (suffix ?? "")
    .split(".")
    .map((part) => (part === "*" ? "全部对象" : decodeURIComponent(part)));
  if (driver === "mysql" && parts[0] === database) parts.shift();
  return [database, ...parts].join(" / ");
}
