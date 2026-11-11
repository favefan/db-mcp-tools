import { useEffect, useRef, useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { useQueryClient, useQuery } from "@tanstack/react-query";
import { Play, Square, RefreshCw, Settings2 } from "lucide-react";
import { toast } from "sonner";
import {
  api,
  connect,
  localCommand,
  localRestore,
  localDefaults,
  getLocalStatus,
  getPreferences,
  setDesktopPreferences,
  errorMessage,
  type Profile,
  type Health,
  type Template,
  type Policy,
  type Configuration,
  type LocalDefaults,
  type LocalStatus,
  type SessionView,
} from "../lib/api";
import {
  Field,
  Badge,
  Modal,
  Button,
  DataList,
  DataListItem,
  DataTable,
  Hint,
  Stack,
} from "../components/ui";
import { Profiles } from "./profiles";
import { Page } from "../components/page-header";
import { MaskEditor, PolicyEditor } from "./policy";
import { Integrations } from "./integrations";
import { isPermissionGranted, requestPermission } from "@tauri-apps/plugin-notification";
import { open } from "@tauri-apps/plugin-dialog";
import { enable, disable, isEnabled } from "@tauri-apps/plugin-autostart";
import { isTauri } from "@tauri-apps/api/core";
import { hasDesktop } from "../lib/desktop";
import { DiagnosticExport } from "../components/diagnostic-export";
import { newerServer, serverCompatibility } from "../lib/server-version";

const schema = z.object({
  endpoint: z.url(),
  data_dir: z.string(),
  executable: z.string(),
  token: z.string(),
});
type FormValues = z.infer<typeof schema>;
export type SettingsSection = "general" | "server" | "agents" | "templates" | "data";
export function Settings({
  section,
  title,
  profile,
  health,
  onConnect,
  session,
  onSessionChange,
  theme,
  setTheme,
  notifications,
  setNotifications,
  upgrading = false,
  connectionError,
}: {
  section: SettingsSection;
  title: string;
  profile: Profile | null;
  health?: Health;
  onConnect: (p: Profile) => void;
  session?: SessionView;
  onSessionChange: (s: SessionView) => void;
  theme: string;
  setTheme: (s: string) => void;
  notifications: boolean;
  setNotifications: (v: boolean) => void;
  upgrading?: boolean;
  connectionError?: string;
}) {
  const form = useForm<FormValues>({
    resolver: zodResolver(schema),
    defaultValues: {
      endpoint: profile?.endpoint ?? "http://127.0.0.1:19876",
      data_dir: profile?.data_dir ?? "",
      executable: profile?.executable ?? "",
      token: "",
    },
  });
  const [busy, setBusy] = useState(false);
  const serverMode = profile?.mode ?? "existing";
  const [template, setTemplate] = useState<Template>();
  const [incoming, setIncoming] = useState<{
    configuration: Configuration;
    digest: string;
    instances: number;
    entities: number;
    bindings: number;
    templates: number;
    file: string;
  }>();
  const [restorePath, setRestorePath] = useState("");
  const [selectedBuild, setSelectedBuild] = useState<LocalDefaults>();
  const client = useQueryClient();
  const compatibility = serverCompatibility(health);
  const preferences = useQuery({
    queryKey: ["desktop-preferences"],
    queryFn: getPreferences,
    enabled: hasDesktop(),
  });
  const defaults = useQuery({
    queryKey: ["local-defaults", profile?.executable, profile?.data_dir],
    queryFn: () => localDefaults(profile?.executable, profile?.data_dir),
    enabled: hasDesktop(),
    staleTime: Infinity,
  });
  const autostart = useQuery({
    queryKey: ["autostart"],
    queryFn: isEnabled,
    enabled: isTauri(),
    staleTime: Infinity,
  });
  const localInfo = selectedBuild ?? defaults.data;
  const executable = form.watch("executable").trim();
  const dataDir = form.watch("data_dir").trim();
  const hasLocalTarget = !!executable && !!dataDir;
  const localStatus = useQuery({
    queryKey: ["local-status", executable, dataDir],
    queryFn: () =>
      getLocalStatus({
        ...profile!,
        ...form.getValues(),
        executable,
        data_dir: dataDir,
        server_id: "",
      }),
    enabled:
      hasDesktop() &&
      section === "server" &&
      serverMode === "local" &&
      hasLocalTarget &&
      !busy &&
      !upgrading,
    retry: false,
    refetchInterval: 15000,
  });
  const localControlDisabled =
    busy || upgrading || !hasLocalTarget || localStatus.isFetching || localStatus.isError;
  const running = localStatus.data?.running;
  const currentVersion =
    health?.version ?? localStatus.data?.version ?? localInfo?.deployed_version;
  const bundledUpgrade =
    defaults.data?.bundled_executable &&
    defaults.data.bundled_build &&
    newerServer(
      defaults.data.bundled_build.version,
      currentVersion ?? localInfo?.build?.version ?? "0.0.0",
    );
  const selectedUpgrade =
    currentVersion && localInfo?.build && newerServer(localInfo.build.version, currentVersion);

  const templates = useQuery({
    queryKey: ["templates"],
    queryFn: () => api<Template[]>("templates"),
    enabled: !!health && !compatibility,
  });
  const backups = useQuery({
    queryKey: ["backups"],
    queryFn: () => api<{ path: string; created_at: string }[]>("backups"),
    enabled: !!health && !compatibility,
  });
  const previousError = useRef("");
  useEffect(() => {
    const message =
      connectionError ||
      compatibility ||
      (localStatus.error ? errorMessage(localStatus.error) : "") ||
      (defaults.error ? errorMessage(defaults.error) : "");
    if (message && message !== previousError.current)
      toast.error(message, { id: connectionError ? "session-error" : "desktop-status" });
    previousError.current = message;
  }, [connectionError, compatibility, localStatus.error, defaults.error]);
  const current = (): Profile => ({
    ...profile!,
    ...form.getValues(),
    server_id: form.getValues("endpoint") === profile?.endpoint ? profile.server_id : "",
  });
  const perform = async (f: () => Promise<unknown>, onError?: (message: string) => void) => {
    if (upgrading) return false;
    setBusy(true);
    try {
      const value = await f();
      if (value !== undefined) toast.success("操作完成");
      await client.invalidateQueries();
      return true;
    } catch (e) {
      if (onError) onError(errorMessage(e));
      else toast.error(errorMessage(e));
      return false;
    } finally {
      setBusy(false);
    }
  };
  const submit = form.handleSubmit(async (data) => {
    await perform(async () => {
      const p = current();
      if (serverMode === "existing") {
        p.data_dir = "";
        p.executable = "";
      }
      const h = await connect(p, data.token);
      p.server_id = h.server_id;
      onConnect(p);
      form.setValue("token", "");
      await client.invalidateQueries();
      const problem = serverCompatibility(h);
      if (problem) toast.error(problem);
      return h;
    });
  });
  const runLocal = async (action: string, source?: string) => {
    if (upgrading) return false;
    const isUpgrade = action === "upgrade";
    try {
      return await perform(
        async () => {
          const p = current();
          if (source) p.executable = source;
          const result = await localCommand(p, action);
          if (typeof result.launcher === "string") {
            p.executable = result.launcher;
            form.setValue("executable", p.executable);
          }
          if (typeof result.data_dir === "string") {
            p.data_dir = result.data_dir;
            form.setValue("data_dir", p.data_dir);
          }
          if (typeof result.admin === "string") {
            p.endpoint = `http://${result.admin}`;
            form.setValue("endpoint", p.endpoint);
          }
          const nested = result.status as LocalStatus | undefined;
          const status =
            typeof result.running === "boolean"
              ? { ...result, running: result.running }
              : typeof nested?.running === "boolean"
                ? nested
                : await getLocalStatus(p);
          client.setQueryData(["local-status", p.executable.trim(), p.data_dir.trim()], status);
          setSelectedBuild(await localDefaults(p.executable, p.data_dir));
          if (status.running && ["start", "restart", "upgrade"].includes(action)) {
            p.server_id = status.server_id ?? p.server_id;
            const connected = await connect(p, "");
            p.server_id = connected.server_id;
            onConnect(p);
          }
          return result;
        },
        isUpgrade
          ? (message) => {
              toast.error(message);
            }
          : undefined,
      );
    } finally {
      await client.invalidateQueries({ queryKey: ["local-status"] });
    }
  };
  return (
    <Page
      title={title}
      icon={Settings2}
      actions={<>{health && <Badge tone="green">MCP Server {health.version}</Badge>}</>}
    >
      <div className="settings-content">
        {section === "server" && session && (
          <Profiles session={session} onChange={onSessionChange} />
        )}
        <section
          className="panel settings-panel"
          hidden={section !== "server" || serverMode !== "existing" || !profile}
        >
          <h2>连接地址与凭据</h2>
          <p className="muted">{profile?.endpoint}</p>
          <Button variant="primary" disabled={busy || !profile} onClick={() => void submit()}>
            连接当前配置
          </Button>
          <Button
            disabled={busy}
            onClick={() =>
              document
                .querySelector<HTMLElement>(".settings-content")
                ?.scrollIntoView({ block: "start" })
            }
          >
            编辑配置
          </Button>
          {health && (
            <p className="muted">
              MCP Server ID：{health.server_id} · 运行方式：{health.mode}
            </p>
          )}
        </section>
        <div hidden={section !== "agents"}>
          <Integrations profile={profile} health={compatibility ? undefined : health} />
        </div>
        <section
          className="panel settings-panel local-settings"
          hidden={section !== "server" || serverMode !== "local" || !profile}
        >
          <h2>程序与数据</h2>
          {(bundledUpgrade || selectedUpgrade) && (
            <Button
              disabled={busy || upgrading}
              onClick={() =>
                void runLocal(
                  "upgrade",
                  bundledUpgrade ? defaults.data?.bundled_executable : undefined,
                )
              }
            >
              升级 MCP Server
            </Button>
          )}
          <Stack>
            <Field label="dbmcp 程序路径">
              <input value={profile?.executable ?? ""} readOnly />
            </Field>
            <Field label="数据目录">
              <input value={profile?.data_dir ?? ""} readOnly />
            </Field>
          </Stack>
          <div className="actions settings-actions">
            <Button
              disabled={busy || upgrading || !hasLocalTarget}
              onClick={() => void runLocal("init")}
            >
              初始化目录
            </Button>
            <Button
              tone="success"
              disabled={localControlDisabled || running !== false}
              onClick={() => void runLocal("start")}
            >
              <Play size={15} />
              启动
            </Button>
            <Button
              tone="danger"
              disabled={localControlDisabled || running !== true}
              onClick={() => void runLocal("stop")}
            >
              <Square size={15} />
              停止
            </Button>
            <Button
              tone="warning"
              disabled={localControlDisabled || running !== true}
              onClick={() => void runLocal("restart")}
            >
              <RefreshCw size={15} />
              重启
            </Button>
            <Button
              variant="text"
              disabled={busy || upgrading || !hasLocalTarget}
              onClick={() => void runLocal("status")}
            >
              检查状态
            </Button>
            <Button
              variant="text"
              disabled={busy || upgrading || !hasLocalTarget}
              onClick={() => void submit()}
            >
              重新连接
            </Button>
          </div>
          {localInfo?.build && (
            <p className="muted">
              所选 MCP Server {localInfo.build.version} · 可用驱动：
              {Object.entries(localInfo.build.drivers)
                .filter(([, available]) => available)
                .map(([name]) => name)
                .join(" / ")}
            </p>
          )}
        </section>
        <section className="panel settings-panel general-settings" hidden={section !== "general"}>
          <h2>外观与行为</h2>
          <div className="settings-fields">
            <Field label="主题">
              <select
                value={theme}
                disabled={!profile || busy}
                onChange={(e) => setTheme(e.target.value)}
              >
                <option value="system">跟随系统</option>
                <option value="light">浅色</option>
                <option value="dark">深色</option>
              </select>
            </Field>
            <label className="setting-toggle">
              <span>
                <strong>
                  默认关闭行为{" "}
                  <Hint text="退出客户端会停止本机托管的 MCP Server；最小化到托盘会继续运行。" />
                </strong>
              </span>
              <select
                aria-label="默认关闭行为"
                value={preferences.data?.close_behavior ?? "ask"}
                disabled={busy || !profile || !preferences.data}
                onChange={async (e) => {
                  setBusy(true);
                  try {
                    client.setQueryData(
                      ["desktop-preferences"],
                      await setDesktopPreferences(
                        e.target.value as "ask" | "tray" | "exit",
                        preferences.data?.lightweight_mode ?? false,
                      ),
                    );
                  } catch (error) {
                    toast.error(errorMessage(error));
                  } finally {
                    setBusy(false);
                  }
                }}
              >
                <option value="ask">每次询问</option>
                <option value="tray">最小化到托盘</option>
                <option value="exit">直接退出</option>
              </select>
            </label>
            <label className="setting-toggle">
              <span>
                <strong>
                  轻量模式{" "}
                  <Hint text="关闭主窗口到托盘 3 分钟后释放窗口资源，重新打开会重建窗口。" />
                </strong>
              </span>
              <input
                type="checkbox"
                role="switch"
                checked={preferences.data?.lightweight_mode ?? false}
                disabled={busy || !profile || !preferences.data}
                onChange={async (e) => {
                  setBusy(true);
                  try {
                    client.setQueryData(
                      ["desktop-preferences"],
                      await setDesktopPreferences(
                        preferences.data?.close_behavior ?? "ask",
                        e.target.checked,
                      ),
                    );
                  } catch (error) {
                    toast.error(errorMessage(error));
                  } finally {
                    setBusy(false);
                  }
                }}
              />
            </label>
            <label className="setting-toggle">
              <span>
                <strong>登录后启动</strong>
              </span>
              <input
                type="checkbox"
                role="switch"
                checked={autostart.data ?? false}
                disabled={busy || !isTauri()}
                onChange={async (e) => {
                  const checked = e.target.checked;
                  setBusy(true);
                  try {
                    if (checked) await enable();
                    else await disable();
                    client.setQueryData(["autostart"], checked);
                  } catch (error) {
                    toast.error(errorMessage(error));
                  } finally {
                    setBusy(false);
                  }
                }}
              />
            </label>
            <label className="setting-toggle">
              <span>
                <strong>审批通知</strong>
              </span>
              <input
                type="checkbox"
                role="switch"
                checked={notifications}
                disabled={!profile || busy}
                onChange={async (e) => {
                  if (!e.target.checked) {
                    setNotifications(false);
                    return;
                  }
                  try {
                    const granted =
                      (await isPermissionGranted()) || (await requestPermission()) === "granted";
                    setNotifications(granted);
                    if (!granted) toast.error("系统未授予通知权限");
                  } catch (error) {
                    toast.error(errorMessage(error));
                  }
                }}
              />
            </label>
          </div>
        </section>
        <section className="panel settings-panel" hidden={section !== "general"}>
          <DiagnosticExport details />
        </section>
        <section className="panel" hidden={section !== "templates"}>
          <div className="section-title">
            <h2>
              策略快照 <Hint text="创建实例时复制模板；修改模板仅影响新建实例。" />
            </h2>
            <Button
              disabled={!health || busy}
              onClick={() =>
                void perform(async () => {
                  const policy = await api<Policy>("default-policy");
                  setTemplate({ id: "", name: "", policy, revision: 0 });
                  return undefined;
                })
              }
            >
              新建模板
            </Button>
          </div>
          {health && !templates.data?.length && <p className="muted">暂无模板</p>}
          {!health && <p className="muted">连接 MCP Server 后管理模板。</p>}
          <DataList>
            {templates.data?.map((t) => (
              <DataListItem key={t.id}>
                <strong>{t.name}</strong>
                <Button variant="text" onClick={() => setTemplate(structuredClone(t))}>
                  编辑
                </Button>
              </DataListItem>
            ))}
          </DataList>
        </section>
        <section className="panel" hidden={section !== "data"}>
          <h2>
            备份与恢复 <Hint text="手动备份包含状态与配套密钥；恢复前需停止 MCP Server。" />
          </h2>
          <Stack>
            <Button
              disabled={busy || !health}
              onClick={() => void perform(() => api("backups", "POST"))}
            >
              创建完整备份
            </Button>
            <DataTable label="备份记录" columns={["创建时间", "备份路径"]}>
              {backups.data?.map((b) => (
                <tr key={b.path}>
                  <td>
                    <time>{new Date(b.created_at).toLocaleString()}</time>
                  </td>
                  <td>
                    <code>{b.path}</code>
                  </td>
                </tr>
              ))}
            </DataTable>
            <details>
              <summary>恢复本机快照</summary>
              <Stack>
                <p className="muted">
                  先停止 MCP Server，再选择备份目录。恢复前会保存当前状态快照。
                </p>
                <Field label="备份目录">
                  <input value={restorePath} onChange={(e) => setRestorePath(e.target.value)} />
                  <Button
                    variant="text"
                    onClick={async () => {
                      try {
                        const path = await open({
                          directory: true,
                          multiple: false,
                          title: "选择完整备份目录",
                        });
                        if (typeof path === "string") setRestorePath(path);
                      } catch (e) {
                        toast.error(errorMessage(e));
                      }
                    }}
                  >
                    选择备份…
                  </Button>
                </Field>
                <Button
                  disabled={busy || !restorePath || !hasLocalTarget}
                  onClick={() => void perform(() => localRestore(current(), restorePath))}
                >
                  恢复已停止的本机 MCP Server
                </Button>
              </Stack>
            </details>
          </Stack>
        </section>
        <section className="panel" hidden={section !== "data"}>
          <h2>配置导入导出</h2>
          <p className="muted">
            配置文件只包含实例、实体、模板和策略。导入创建新的停用对象，填写密码并签发凭据后再启用。
          </p>
          <div className="actions">
            <Button
              disabled={busy || !health}
              onClick={() =>
                void perform(async () => {
                  const configuration = await api<Configuration>("configuration");
                  const url = URL.createObjectURL(
                    new Blob([JSON.stringify(configuration, null, 2)], {
                      type: "application/json",
                    }),
                  );
                  const link = document.createElement("a");
                  link.href = url;
                  link.download = "dbmcp-configuration.json";
                  link.click();
                  URL.revokeObjectURL(url);
                  return undefined;
                })
              }
            >
              导出配置
            </Button>
            <Button asChild>
              <label>
                选择配置文件
                <input
                  className="sr-only"
                  type="file"
                  accept=".json,application/json"
                  disabled={busy || !health}
                  onChange={(e) => {
                    const file = e.target.files?.[0];
                    if (file)
                      void perform(async () => {
                        const configuration = JSON.parse(await file.text()) as Configuration;
                        const plan = await api<{
                          digest: string;
                          instances: number;
                          entities: number;
                          bindings: number;
                          templates: number;
                        }>("configuration/preview", "POST", configuration);
                        setIncoming({ ...plan, configuration, file: file.name });
                        return undefined;
                      });
                    e.target.value = "";
                  }}
                />
              </label>
            </Button>
          </div>
        </section>
        {incoming && (
          <Modal title="确认配置导入" onClose={() => setIncoming(undefined)}>
            <p>{incoming.file}</p>
            <p>
              将新增 {incoming.instances} 个停用实例、{incoming.entities} 个停用实体、
              {incoming.bindings} 项绑定和 {incoming.templates} 个模板。现有配置继续保留。
            </p>
            <div className="form-actions">
              <Button onClick={() => setIncoming(undefined)}>取消</Button>
              <Button
                variant="primary"
                disabled={busy}
                onClick={() =>
                  void perform(async () => {
                    await api("configuration/import", "POST", {
                      configuration: incoming.configuration,
                      digest: incoming.digest,
                    });
                    setIncoming(undefined);
                    toast.success("配置已导入，请重新填写密码与签发凭据");
                    return undefined;
                  })
                }
              >
                应用导入
              </Button>
            </div>
          </Modal>
        )}
        {template && (
          <Modal title="实例模板" onClose={() => setTemplate(undefined)} wide>
            <Field label="模板名称">
              <input
                value={template.name}
                onChange={(e) => setTemplate({ ...template, name: e.target.value })}
              />
            </Field>
            <PolicyEditor
              value={template.policy}
              onChange={(policy) => setTemplate({ ...template, policy })}
            />
            <MaskEditor
              rules={template.policy.masks ?? []}
              onChange={(masks) =>
                setTemplate({ ...template, policy: { ...template.policy, masks } })
              }
            />
            <div className="form-actions">
              <Button
                variant="primary"
                disabled={busy}
                onClick={() =>
                  void perform(async () => {
                    await api("templates", "POST", template);
                    await client.invalidateQueries();
                    setTemplate(undefined);
                    toast.success("模板已保存");
                    return undefined;
                  })
                }
              >
                保存模板
              </Button>
            </div>
          </Modal>
        )}
      </div>
    </Page>
  );
}
