import { useState } from "react";
import { open } from "@tauri-apps/plugin-dialog";
import { isTauri } from "@tauri-apps/api/core";
import { toast } from "sonner";
import {
  Button,
  Field,
  Modal,
  Notice,
  Stack,
  DataList,
  DataListItem,
  Badge,
} from "../components/ui";
import {
  getSession,
  saveProfile,
  deleteProfile,
  switchProfile,
  disconnectSession,
  localDefaults,
  errorMessage,
  type Profile,
  type SessionView,
} from "../lib/api";
import { suspendSession } from "../lib/desktop";

export function Profiles({
  session,
  onChange,
  onClose,
}: {
  session: SessionView;
  onChange: (s: SessionView) => void;
  onClose: () => void;
}) {
  const [editing, setEditing] = useState<Profile>();
  const [token, setToken] = useState("");
  const [busy, setBusy] = useState(false);
  const act = async (f: () => Promise<SessionView>, close = false) => {
    if (busy) return false;
    setBusy(true);
    suspendSession();
    try {
      onChange(await f());
      if (close) onClose();
      return true;
    } catch (e) {
      toast.error(errorMessage(e));
      try {
        onChange(await getSession());
      } catch (refreshError) {
        toast.error(errorMessage(refreshError));
      }
      return false;
    } finally {
      setBusy(false);
    }
  };
  const create = async () => {
    setToken("");
    setEditing({
      id: "",
      name: "新环境",
      mode: "existing",
      endpoint: "http://127.0.0.1:19876",
      data_dir: "",
      executable: "",
      server_id: "",
      follow_client: false,
      preferences: {
        theme: "system",
        notifications: false,
        close_behavior: "ask",
        close_to_tray: false,
        lightweight_mode: false,
      },
    });
  };
  const mode = async (value: "local" | "existing") => {
    if (!editing) return;
    const p = { ...editing, mode: value, server_id: "", follow_client: false };
    if (value === "existing") {
      p.data_dir = "";
      p.executable = "";
    }
    setEditing(p);
    if (value === "local" && !p.executable) {
      try {
        const defaults = await localDefaults();
        setEditing((current) =>
          current?.id === p.id && current.mode === "local" && !current.executable
            ? { ...current, executable: defaults.executable ?? "" }
            : current,
        );
      } catch (e) {
        toast.error(errorMessage(e));
      }
    }
  };
  const selected = editing?.id === session.current?.id;
  const locked = selected && (session.connected || session.busy || session.owned_server);
  return (
    <Modal
      title="环境与当前会话"
      description="同一时间只连接一个环境，切换会一起更换配置、凭据和界面偏好。"
      onClose={() => {
        if (!busy && (!editing || window.confirm("放弃未保存的环境编辑？"))) onClose();
      }}
    >
      <Stack>
        <Notice>
          当前环境：{session.current?.name ?? "尚未选择"} ·{" "}
          {session.connected ? "已连接" : "未连接"}。环境不是软件登录账号；连接仍使用对应 Server
          的管理员凭据。
        </Notice>
        {session.owned_server && !session.connected && (
          <Notice>本次启动的 Server 尚未释放，请先断开重试，或到 Server 页检查运行状态。</Notice>
        )}
        {!editing ? (
          <>
            <Button disabled={busy || session.busy} onClick={() => void create()}>
              新建环境
            </Button>
            <DataList>
              {session.profiles.map((p) => (
                <DataListItem key={p.id}>
                  <div>
                    <strong>{p.name}</strong>
                    <p className="muted">
                      {p.mode === "local" ? "本机托管" : "已有 Server"} ·{" "}
                      {p.mode === "local" ? p.data_dir : p.endpoint}
                    </p>
                  </div>
                  {p.id === session.current?.id && <Badge>当前环境</Badge>}
                  <Button
                    disabled={busy || session.busy}
                    onClick={() => {
                      setToken("");
                      setEditing(structuredClone(p));
                    }}
                  >
                    编辑
                  </Button>
                  {p.id !== session.current?.id && (
                    <>
                      <Button
                        disabled={busy || session.busy}
                        onClick={() => {
                          if (
                            window.confirm(
                              "切换会关闭当前页面和未保存的表单，并按设置停止当前跟随实例。继续？",
                            )
                          )
                            void act(
                              () => switchProfile(p.id, p.mode === "existing" || p.follow_client),
                              true,
                            );
                        }}
                      >
                        切换环境
                      </Button>
                    </>
                  )}
                </DataListItem>
              ))}
            </DataList>
            <p className="muted">
              切到未启用随客户端启停的本机环境时先保持断开，由你在 Server 页启动。独立 Server
              和系统服务不会随环境切换停止。
            </p>
          </>
        ) : (
          <form
            onSubmit={(e) => {
              e.preventDefault();
              void act(async () => {
                const next = await saveProfile(editing, token);
                const saved = next.profiles.find(
                  (p) => p.id === editing.id || !session.profiles.some((old) => old.id === p.id),
                );
                if (
                  saved &&
                  next.current?.id === saved.id &&
                  saved.mode === "local" &&
                  saved.follow_client
                )
                  return switchProfile(saved.id, true);
                return next;
              }).then((ok) => {
                if (ok) setEditing(undefined);
              });
            }}
          >
            <Stack>
              {locked && (
                <Notice
                  action={
                    <Button disabled={busy} onClick={() => void act(disconnectSession)}>
                      断开后编辑
                    </Button>
                  }
                >
                  编辑当前环境前须断开；若为本次客户端启动的跟随实例，也会停止。
                </Notice>
              )}
              <Field label="环境名称">
                <input
                  value={editing.name}
                  disabled={busy || locked}
                  onChange={(e) => setEditing({ ...editing, name: e.target.value })}
                  required
                />
              </Field>
              <fieldset className="profile-mode" disabled={busy || locked}>
                <legend>这个环境如何连接 Server？</legend>
                <label className={editing.mode === "local" ? "selected" : ""}>
                  <input
                    type="radio"
                    name="profile-mode"
                    checked={editing.mode === "local"}
                    onChange={() => void mode("local")}
                  />
                  <strong>本机托管</strong>
                  <small>由本机程序管理独立数据目录。</small>
                </label>
                <label className={editing.mode === "existing" ? "selected" : ""}>
                  <input
                    type="radio"
                    name="profile-mode"
                    checked={editing.mode === "existing"}
                    onChange={() => void mode("existing")}
                  />
                  <strong>已有 Server</strong>
                  <small>只连接，不控制其进程与系统服务。</small>
                </label>
              </fieldset>
              {editing.mode === "existing" ? (
                <>
                  <Field label="Admin 地址" hint="远程部署通过 SSH 隧道连接。">
                    <input
                      type="url"
                      value={editing.endpoint}
                      disabled={busy || locked}
                      onChange={(e) => setEditing({ ...editing, endpoint: e.target.value })}
                      required
                    />
                  </Field>
                  <Field
                    label="管理员凭据"
                    hint="独立保存到此环境的系统 keyring；留空保留已有凭据。"
                  >
                    <input
                      type="password"
                      autoComplete="off"
                      value={token}
                      disabled={busy || locked}
                      onChange={(e) => setToken(e.target.value)}
                    />
                  </Field>
                </>
              ) : (
                <>
                  <Field label="本机 dbmcp 程序">
                    <div className="path-input">
                      <input
                        value={editing.executable}
                        disabled={busy || locked}
                        placeholder="选择 dbmcp.exe / dbmcp 的绝对路径"
                        onChange={(e) => setEditing({ ...editing, executable: e.target.value })}
                      />
                      <Button
                        disabled={busy || locked || !isTauri()}
                        onClick={async () => {
                          const p = await open({ multiple: false });
                          if (typeof p === "string") setEditing({ ...editing, executable: p });
                        }}
                      >
                        浏览…
                      </Button>
                    </div>
                  </Field>
                  <Field label="数据目录" hint="新环境留空则创建独立目录，已有数据填写原绝对路径。">
                    <div className="path-input">
                      <input
                        value={editing.data_dir}
                        disabled={busy || locked}
                        onChange={(e) => setEditing({ ...editing, data_dir: e.target.value })}
                      />
                      <Button
                        disabled={busy || locked || !isTauri()}
                        onClick={async () => {
                          const p = await open({ directory: true, multiple: false });
                          if (typeof p === "string") setEditing({ ...editing, data_dir: p });
                        }}
                      >
                        浏览…
                      </Button>
                    </div>
                  </Field>
                  <label className="check">
                    <input
                      type="checkbox"
                      checked={editing.follow_client}
                      disabled={busy || locked}
                      onChange={(e) => setEditing({ ...editing, follow_client: e.target.checked })}
                    />
                    Server 随客户端启停
                  </label>
                  <p className="muted">
                    进入此环境时启动，切走或真正退出时停止本次客户端启动的后台实例；托盘与轻量模式不停止。系统服务不能启用此选项，外部已运行实例需先手动停止。
                  </p>
                </>
              )}
              <div className="form-actions">
                <Button disabled={busy} onClick={() => setEditing(undefined)}>
                  返回列表
                </Button>
                <Button type="submit" variant="primary" disabled={busy || locked}>
                  保存环境
                </Button>
                {editing.id && (
                  <Button
                    danger
                    disabled={busy || locked}
                    onClick={() => {
                      if (
                        window.confirm(
                          "只删除环境配置和保存的管理员凭据，不删除 Server 数据。继续？",
                        )
                      )
                        void act(() => deleteProfile(editing.id)).then((ok) => {
                          if (ok) setEditing(undefined);
                        });
                    }}
                  >
                    删除环境
                  </Button>
                )}
              </div>
            </Stack>
          </form>
        )}
      </Stack>
    </Modal>
  );
}
