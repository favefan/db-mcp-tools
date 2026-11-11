import { useEffect, useRef, useState } from "react";
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
  getProfile,
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
import { upgradeBundledRuntime } from "./lib/server-upgrade";

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
  const [theme, setTheme] = useState(() => localStorage.getItem("theme") ?? "system");
  const [notifications, setNotifications] = useState(
    () => localStorage.getItem("notifications") === "true",
  );
  const seen = useRef(new Set<string>());
  const bootstrapped = useRef(false);
  const [upgrading, setUpgrading] = useState("");
  const [upgradeError, setUpgradeError] = useState("");
  const client = useQueryClient();
  const health = useQuery({
    queryKey: ["health", profile?.server_id],
    queryFn: () => api<Health>("health"),
    enabled: !!profile,
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
    if (hasDesktop() && !bootstrapped.current) {
      bootstrapped.current = true;
      void getProfile()
        .then(async (p) => {
          setProfile(p);
          if (!p) setPage(4);
          else {
            try {
              const next = await upgradeBundledRuntime(p, setUpgrading);
              setProfile(next);
              await client.invalidateQueries();
            } catch (error) {
              setUpgradeError(errorMessage(error));
              setSettingsSection("server");
              setPage(4);
            } finally {
              setUpgrading("");
            }
          }
        })
        .catch((error) => {
          toast.error(`读取连接档案失败：${errorMessage(error)}`);
          setSettingsSection("server");
          setPage(4);
        });
    }
  }, [client]);
  useEffect(() => {
    localStorage.setItem("theme", theme);
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
    const unlisten = listen(
      "server-changed",
      () =>
        void client.invalidateQueries({
          predicate: (query) =>
            !["integrations", "local-defaults", "autostart"].includes(String(query.queryKey[0])),
        }),
    );
    return () => {
      void unlisten.then((f) => f());
    };
  }, [client]);
  const compatibility = serverCompatibility(health.data);
  const connected = !!health.data && !health.error;
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
              onClick={() => openSettings("server")}
              title={connected ? "Server 已连接" : "Server 未连接"}
            >
              <Circle size={9} fill="currentColor" className={connected ? "online" : "offline"} />
              {!collapsed && (
                <>
                  <div>
                    <strong>{profile?.data_dir ? "本机 Server" : "Server 连接"}</strong>
                    <small>{connected ? "已连接" : "未连接"}</small>
                  </div>
                  <ChevronRight size={14} />
                </>
              )}
            </button>
          </div>
        </aside>
        <main ref={main}>
          {upgrading && page !== 4 && (
            <Page title="升级本机 Server" icon={HardDrive}>
              <Notice>正在备份并升级本机 Server 至 {upgrading}，完成后自动重新连接…</Notice>
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
                  (profile
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
              key={profile?.server_id ?? "new"}
              section={settingsSection}
              title={settingsNavigation.find((item) => item.id === settingsSection)!.name}
              profile={profile}
              health={connected ? health.data : undefined}
              upgrading={!!upgrading}
              upgradeError={upgradeError}
              onUpgradeErrorChange={setUpgradeError}
              onConnect={(p) => {
                setUpgradeError("");
                client.clear();
                setProfile(p);
              }}
              theme={theme}
              setTheme={setTheme}
              notifications={notifications}
              setNotifications={(v) => {
                localStorage.setItem("notifications", String(v));
                setNotifications(v);
              }}
            />
          )}
        </main>
      </div>
      <CloseDialog />
      <Toaster
        richColors
        position="bottom-right"
        theme={theme === "dark" ? "dark" : theme === "light" ? "light" : "system"}
      />
    </>
  );
}
