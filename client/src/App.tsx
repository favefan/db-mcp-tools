import { useEffect, useRef, useState, useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { isTauri } from "@tauri-apps/api/core";
import { listen } from "@tauri-apps/api/event";
import { getCurrentWindow } from "@tauri-apps/api/window";
import { onAction, sendNotification } from "@tauri-apps/plugin-notification";
import {
  Database,
  Users,
  ClipboardCheck,
  ScrollText,
  Settings2,
  PanelLeftClose,
  PanelLeftOpen,
  ArrowLeft,
  HardDrive,
  Cable,
  LayoutTemplate,
  Archive,
  Info,
  MonitorCog,
  Sun,
  Moon,
} from "lucide-react";
import { Toaster, toast } from "sonner";
import {
  api,
  getSession,
  setDesktopPreferences,
  type SessionView,
  errorMessage,
  type Profile,
  type Health,
  type Approval,
  type Registration,
} from "./lib/api";
import { Instances } from "./features/instances";
import { Entities } from "./features/entities";
import { Approvals } from "./features/approvals";
import { Logs } from "./features/logs";
import { Settings, type SettingsSection } from "./features/settings";
import { Page } from "./components/page-header";
import { hasDesktop, isPreview, invoke } from "./lib/desktop";
import { CloseDialog } from "./components/close-dialog";
import { Button } from "./components/ui";
import { serverCompatibility } from "./lib/server-version";
import { ProfileMenu } from "./components/profile-menu";

const navigation = [
  { name: "数据库实例", icon: Database },
  { name: "访问实体", icon: Users },
  { name: "待审批", icon: ClipboardCheck },
  { name: "访问日志", icon: ScrollText },
];
const settingsNavigation: { id: SettingsSection; name: string; icon: typeof Settings2 }[] = [
  { id: "general", name: "通用", icon: Settings2 },
  { id: "server", name: "配置与 MCP Server", icon: HardDrive },
  { id: "agents", name: "Agent 接入", icon: Cable },
  { id: "templates", name: "实例模板", icon: LayoutTemplate },
  { id: "data", name: "数据管理", icon: Archive },
  { id: "about", name: "关于", icon: Info },
];
export default function App() {
  const [page, setPage] = useState(() => (hasDesktop() ? 0 : 4));
  const [collapsed, setCollapsed] = useState(false);
  const [settingsSection, setSettingsSection] = useState<SettingsSection>("server");
  const [previousPage, setPreviousPage] = useState(0);
  const [approvalTarget, setApprovalTarget] = useState<string>();
  const main = useRef<HTMLElement>(null);
  const [profile, setProfile] = useState<Profile | null>(null);
  const [theme, setTheme] = useState("system");
  const [savingAppearance, setSavingAppearance] = useState(false);
  const [notifications, setNotifications] = useState(false);
  const [session, setSession] = useState<SessionView>();
  const generation = useRef(-1);
  const wasConnected = useRef(false);
  const settledProfile = useRef<string | null>(null);
  const importedLegacy = useRef(false);
  const seen = useRef(new Set<string>());
  const bootstrapped = useRef(false);
  const [upgrading, setUpgrading] = useState("");
  const client = useQueryClient();
  const applySession = useCallback(
    (next: SessionView) => {
      if (next.generation < generation.current) return;
      if (next.generation !== generation.current || next.connected !== wasConnected.current) {
        client.clear();
        seen.current.clear();
        setApprovalTarget(undefined);
        if (!next.busy) {
          if (!next.connected) setPage(4);
          else if (settledProfile.current !== (next.current?.id ?? null)) setPage(0);
        }
      }
      if (!next.busy) settledProfile.current = next.current?.id ?? null;
      wasConnected.current = next.connected;
      generation.current = next.generation;
      setSession(next);
      setProfile(next.current);
      setTheme(next.preferences.theme);
      setNotifications(next.preferences.notifications);
      setUpgrading(next.busy ? "当前配置" : "");
      client.setQueryData(["desktop-preferences"], next.preferences);
    },
    [client],
  );
  const refreshSession = useCallback(() => getSession().then(applySession), [applySession]);
  const saveAppearance = async (nextTheme: string, nextNotifications: boolean) => {
    if (!session?.current || savingAppearance) return;
    setSavingAppearance(true);
    try {
      await setDesktopPreferences(
        session.preferences.close_behavior,
        session.preferences.lightweight_mode,
        nextTheme,
        nextNotifications,
      );
      await refreshSession();
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setSavingAppearance(false);
    }
  };
  useEffect(() => {
    if (!session?.current?.import_legacy_ui || session.busy || importedLegacy.current) return;
    importedLegacy.current = true;
    const previousTheme = localStorage.getItem("theme");
    const nextTheme = ["system", "light", "dark"].includes(previousTheme ?? "")
      ? previousTheme!
      : session.preferences.theme;
    const nextNotifications = localStorage.getItem("notifications") === "true";
    void setDesktopPreferences(
      session.preferences.close_behavior,
      session.preferences.lightweight_mode,
      nextTheme,
      nextNotifications,
    )
      .then(refreshSession)
      .catch((e) => {
        importedLegacy.current = false;
        toast.error(errorMessage(e));
      });
  }, [session, refreshSession]);
  const health = useQuery({
    queryKey: ["health", profile?.id, session?.generation],
    queryFn: () => api<Health>("health"),
    enabled: !!profile && !!session?.connected && !session.busy,
    refetchInterval: 15000,
    retry: false,
  });
  const approvals = useQuery({
    queryKey: ["approvals"],
    queryFn: () => api<Approval[]>("approvals"),
    enabled: !!health.data && !health.error && !serverCompatibility(health.data) && !upgrading,
    refetchInterval: 30000,
  });
  const registrations = useQuery({
    queryKey: ["registrations"],
    queryFn: () => api<Registration[]>("registrations"),
    enabled: !!health.data && !health.error && !serverCompatibility(health.data) && !upgrading,
    refetchInterval: 30000,
  });
  const pending =
    (approvals.data?.filter((a) => a.status === "pending").length ?? 0) +
    (registrations.data?.filter((r) => r.status === "pending").length ?? 0);
  useEffect(() => {
    const waiting = [
      ...(approvals.data?.filter((a) => a.status === "pending") ?? []),
      ...(registrations.data?.filter((r) => r.status === "pending") ?? []),
    ];
    const added = waiting.filter((a) => !seen.current.has(a.id));
    seen.current = new Set(waiting.map((a) => a.id));
    if (isTauri())
      void invoke("sync_notifications", {
        enabled: notifications,
        pendingIds: waiting.map((a) => a.id),
      }).catch(() => {});
    if (notifications && added.length && isTauri()) {
      for (const item of added)
        sendNotification({
          title: "DB MCP Tools · 操作待审批",
          body:
            "agent_reason" in item && item.agent_reason
              ? `请求说明（Agent 提供）：${item.agent_reason}`
              : "新请求等待处理，请打开待审批页面。",
        });
    }
  }, [approvals.data, registrations.data, notifications]);
  useEffect(() => {
    if (!isTauri()) return;
    const listener = onAction(() => {
      setPage(2);
      void getCurrentWindow().show();
      void getCurrentWindow().setFocus();
    });
    return () => {
      void listener.then((l) => l.unregister());
    };
  }, []);
  useEffect(() => {
    if (!hasDesktop()) return;
    const refresh = () => void refreshSession().catch((e) => toast.error(errorMessage(e)));
    if (!bootstrapped.current) {
      bootstrapped.current = true;
      refresh();
    }
    if (!isTauri()) return;
    const listener = listen("session-changed", refresh);
    return () => {
      void listener.then((f) => f());
    };
  }, [refreshSession]);
  useEffect(() => {
    if (!session?.busy) return;
    const timer = setInterval(() => void refreshSession(), 1000);
    return () => clearInterval(timer);
  }, [session?.busy, refreshSession]);
  useEffect(() => {
    const media = matchMedia("(prefers-color-scheme: dark)");
    const update = () =>
      (document.documentElement.dataset.theme =
        theme === "system" ? (media.matches ? "dark" : "light") : theme);
    update();
    media.addEventListener("change", update);
    return () => media.removeEventListener("change", update);
  }, [theme]);
  useEffect(() => {
    if (!isTauri()) return;
    const unlisten = listen("server-changed", (event) => {
      const stamp = event.payload as { profile_id?: string; generation?: number };
      if (stamp.profile_id !== profile?.id || stamp.generation !== session?.generation) return;
      void client.invalidateQueries({
        predicate: (query) =>
          !["integrations", "local-defaults", "autostart"].includes(String(query.queryKey[0])),
      });
    });
    return () => {
      void unlisten.then((f) => f());
    };
  }, [client, profile?.id, session?.generation]);
  const sessionReady = session !== undefined;
  useEffect(() => {
    if (!isTauri() || !sessionReady) return;
    const listener = listen("open-about", () => {
      void invoke("take_about_request");
      setSettingsSection("about");
      setPage(4);
    });
    void invoke<boolean>("take_about_request").then((requested) => {
      if (requested) {
        setSettingsSection("about");
        setPage(4);
      }
    });
    return () => {
      void listener.then((unlisten) => unlisten());
    };
  }, [sessionReady]);
  const themes = [
    { value: "system", label: "自动", icon: MonitorCog },
    { value: "light", label: "浅色", icon: Sun },
    { value: "dark", label: "深色", icon: Moon },
  ];
  const activeTheme = themes.find((item) => item.value === theme) ?? themes[0];
  const compatibility = serverCompatibility(health.data);
  const connected = !!session?.connected && !!health.data && !health.error && !session.busy;
  const ready = connected && !compatibility && !upgrading;
  const openSettings = (section: SettingsSection) => {
    if (page !== 4) setPreviousPage(page);
    setSettingsSection(section);
    setPage(4);
  };
  useEffect(() => {
    main.current?.querySelector<HTMLElement>(".page-content")?.scrollTo({ top: 0 });
  }, [page, settingsSection]);
  return (
    <>
      <div className={`app ${collapsed ? "collapsed" : ""}`}>
        <aside>
          <div className="brand" data-tauri-drag-region>
            <div className="brand-mark">
              <Database size={19} />
            </div>
            {!collapsed && <strong data-tauri-drag-region>DB MCP Tools</strong>}
            <button
              className="icon-button collapse-button"
              onClick={() => setCollapsed(!collapsed)}
              aria-label={collapsed ? "展开侧栏" : "收起侧栏"}
              title={collapsed ? "展开侧栏" : "收起侧栏"}
            >
              {collapsed ? <PanelLeftOpen size={16} /> : <PanelLeftClose size={16} />}
            </button>
          </div>
          {page === 4 ? (
            <nav aria-label="设置分类">
              <button className="back-button" onClick={() => setPage(previousPage)} title="返回">
                <ArrowLeft size={17} />
                {!collapsed && <span>返回</span>}
              </button>
              {!collapsed && <div className="nav-caption">设置</div>}
              {settingsNavigation.map((item) => (
                <button
                  key={item.id}
                  className={settingsSection === item.id ? "active" : ""}
                  onClick={() => setSettingsSection(item.id)}
                  aria-current={settingsSection === item.id ? "page" : undefined}
                  title={collapsed ? item.name : undefined}
                >
                  <item.icon size={17} />
                  {!collapsed && <span>{item.name}</span>}
                </button>
              ))}
            </nav>
          ) : (
            <nav aria-label="主导航">
              {navigation.map((item, i) => (
                <button
                  key={item.name}
                  className={page === i ? "active" : ""}
                  onClick={() => {
                    if (i === 2) setApprovalTarget(undefined);
                    setPage(i);
                  }}
                  aria-current={page === i ? "page" : undefined}
                  title={collapsed ? item.name : undefined}
                >
                  <item.icon size={17} />
                  {!collapsed && <span>{item.name}</span>}
                  {i === 2 && pending > 0 && <b className="count">{pending}</b>}
                </button>
              ))}
            </nav>
          )}
          <div className="sidebar-bottom">
            {isPreview() && !collapsed && (
              <small className="preview-note">界面预览 · 演示数据</small>
            )}
            {page !== 4 && (
              <button
                className="settings-button"
                onClick={() => openSettings("general")}
                title="设置"
              >
                <Settings2 size={17} />
                {!collapsed && <span>设置</span>}
              </button>
            )}
            {collapsed ? (
              <button
                className="settings-button theme-cycle"
                disabled={!profile || savingAppearance || session?.busy}
                title={`主题：${activeTheme.label}，点击切换为${themes[(themes.indexOf(activeTheme) + 1) % themes.length].label}`}
                aria-label={`主题：${activeTheme.label}`}
                onClick={() =>
                  void saveAppearance(
                    themes[(themes.indexOf(activeTheme) + 1) % themes.length].value,
                    notifications,
                  )
                }
              >
                <activeTheme.icon size={17} />
              </button>
            ) : (
              <div className="theme-switch" role="group" aria-label="主题">
                <span
                  className="theme-pill"
                  style={{ transform: `translateX(${themes.indexOf(activeTheme) * 100}%)` }}
                />
                {themes.map((item) => (
                  <button
                    key={item.value}
                    aria-pressed={theme === item.value}
                    disabled={!profile || savingAppearance || session?.busy}
                    onClick={() => void saveAppearance(item.value, notifications)}
                  >
                    <item.icon size={14} />
                    <span>{item.label}</span>
                  </button>
                ))}
              </div>
            )}
            <ProfileMenu
              session={session}
              connected={connected}
              collapsed={collapsed}
              onChange={applySession}
              onSettings={() => openSettings("server")}
            />
          </div>
        </aside>
        <main ref={main}>
          {upgrading && page !== 4 && (
            <Page title="准备当前配置" icon={HardDrive}>
              <p className="muted">正在连接…</p>
            </Page>
          )}
          {page !== 4 && !ready && !upgrading && (
            <Page title={navigation[page].name} icon={navigation[page].icon}>
              <div className="connection-state">
                <p>
                  {" "}
                  {compatibility ||
                    session?.error ||
                    (session?.connected && health.isPending
                      ? "正在读取当前 MCP Server 状态…"
                      : profile
                        ? "MCP Server 连接已断开，请检查运行状态。"
                        : "先连接 MCP Server，开始管理数据库访问。")}
                </p>
                <Button onClick={() => openSettings("server")}>配置与 MCP Server</Button>
              </div>
            </Page>
          )}
          {page === 0 && ready && <Instances drivers={health.data?.drivers ?? {}} />}
          {page === 1 && ready && (
            <Entities
              onAuthorize={(entity) => {
                setApprovalTarget(entity);
                setPage(2);
              }}
            />
          )}
          {page === 2 && ready && (
            <Approvals
              initialTab={approvalTarget ? "registrations" : "operations"}
              focusEntity={approvalTarget}
            />
          )}
          {page === 3 && ready && <Logs />}
          {page === 4 && (
            <Settings
              key={(profile?.id ?? "new") + ":" + (session?.generation ?? 0)}
              section={settingsSection}
              title={settingsNavigation.find((item) => item.id === settingsSection)!.name}
              profile={profile}
              health={connected ? health.data : undefined}
              upgrading={!!upgrading}
              connectionError={session?.error}
              onConnect={() => void refreshSession()}
              session={session}
              onSessionChange={applySession}
              theme={theme}
              setTheme={(value) => void saveAppearance(value, notifications)}
              notifications={notifications}
              setNotifications={(v) => {
                void saveAppearance(theme, v);
              }}
            />
          )}
        </main>
      </div>
      <CloseDialog generation={session?.generation} />
      <Toaster
        richColors
        position="bottom-right"
        theme={theme === "dark" ? "dark" : theme === "light" ? "light" : "system"}
      />
    </>
  );
}
