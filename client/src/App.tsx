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
  ChevronRight,
  PanelLeftClose,
  PanelLeftOpen,
  Circle,
  ArrowLeft,
  HardDrive,
  Cable,
  LayoutTemplate,
  Archive,
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
import { Button, Notice } from "./components/ui";
import { serverCompatibility } from "./lib/server-version";
import { Profiles } from "./features/profiles";

const navigation = [
  { name: "数据库实例", icon: Database },
  { name: "访问实体", icon: Users },
  { name: "待审批", icon: ClipboardCheck },
  { name: "访问日志", icon: ScrollText },
];
const settingsNavigation: { id: SettingsSection; name: string; icon: typeof Settings2 }[] = [
  { id: "general", name: "通用", icon: Settings2 },
  { id: "server", name: "Server", icon: HardDrive },
  { id: "agents", name: "Agent 接入", icon: Cable },
  { id: "templates", name: "实例模板", icon: LayoutTemplate },
  { id: "data", name: "数据管理", icon: Archive },
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
  const [notifications, setNotifications] = useState(false);
  const [session, setSession] = useState<SessionView>();
  const [managingProfiles, setManagingProfiles] = useState(false);
  const generation = useRef(-1);
  const wasConnected = useRef(false);
  const importedLegacy = useRef(false);
  const seen = useRef(new Set<string>());
  const bootstrapped = useRef(false);
  const [upgrading, setUpgrading] = useState("");
  const [upgradeError, setUpgradeError] = useState("");
  const client = useQueryClient();
  const applySession = useCallback(
    (next: SessionView) => {
      if (next.generation < generation.current) return;
      if (next.generation !== generation.current || next.connected !== wasConnected.current) {
        setUpgradeError("");
        client.clear();
        seen.current.clear();
        setApprovalTarget(undefined);
        if (!next.busy) setPage(next.connected ? 0 : 4);
      }
      wasConnected.current = next.connected;
      generation.current = next.generation;
      setSession(next);
      setProfile(next.current);
      setTheme(next.preferences.theme);
      setNotifications(next.preferences.notifications);
      setUpgrading(next.busy ? "当前环境" : "");
      client.setQueryData(["desktop-preferences"], next.preferences);
    },
    [client],
  );
  const refreshSession = useCallback(() => getSession().then(applySession), [applySession]);
  const saveAppearance = async (nextTheme: string, nextNotifications: boolean) => {
    if (!session?.current) return;
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
      sendNotification({
        title: "DB MCP Tools · 操作待审批",
        body: `${added.length} 条新请求等待处理，请打开待审批页面。`,
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
            <button
              className="server-profile"
              onClick={() => setManagingProfiles(true)}
              title="当前会话 · 切换环境"
            >
              <Circle size={9} fill="currentColor" className={connected ? "online" : "offline"} />
              {!collapsed && (
                <>
                  <div>
                    <strong>{profile ? `当前环境 · ${profile.name}` : "选择当前环境"}</strong>
                    <small>
                      {!profile
                        ? "尚未选择连接"
                        : profile.mode === "local"
                          ? "本机托管"
                          : "已有 Server"}{" "}
                      ·{" "}
                      {session?.busy
                        ? "切换中"
                        : session?.connected && health.isPending
                          ? "连接中"
                          : connected
                            ? "已连接"
                            : "未连接"}
                    </small>
                  </div>
                  <ChevronRight size={14} />
                </>
              )}
            </button>
          </div>
        </aside>
        <main ref={main}>
          {upgrading && page !== 4 && (
            <Page title="准备当前环境" icon={HardDrive}>
              <Notice>正在处理当前环境的连接与本机运行状态…</Notice>
            </Page>
          )}
          {page !== 4 && !ready && !upgrading && (
            <Page title={navigation[page].name} icon={navigation[page].icon}>
              <Notice
                action={
                  <Button variant="text" onClick={() => openSettings("server")}>
                    前往设置 <ChevronRight size={14} />
                  </Button>
                }
              >
                {compatibility ||
                  session?.error ||
                  (session?.connected && health.isPending
                    ? "正在读取当前 Server 状态…"
                    : profile
                      ? "Server 连接已断开，请检查运行状态。"
                      : "先连接 Server，开始管理数据库访问。")}
              </Notice>
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
              upgradeError={upgradeError}
              connectionError={session?.error}
              onUpgradeErrorChange={setUpgradeError}
              onConnect={() => void refreshSession()}
              onManageProfiles={() => setManagingProfiles(true)}
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
      {managingProfiles && session && (
        <Profiles
          session={session}
          onChange={applySession}
          onClose={() => setManagingProfiles(false)}
        />
      )}
      <CloseDialog
        followClient={profile?.follow_client ?? false}
        generation={session?.generation}
      />
      <Toaster
        richColors
        position="bottom-right"
        theme={theme === "dark" ? "dark" : theme === "light" ? "light" : "system"}
      />
    </>
  );
}
