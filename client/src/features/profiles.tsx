import { useState } from "react";
import { open } from "@tauri-apps/plugin-dialog";
import { isTauri } from "@tauri-apps/api/core";
import { toast } from "sonner";
import { Button, Field, Modal, Stack, DataList, DataListItem, Hint } from "../components/ui";
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
}: {
  session: SessionView;
  onChange: (s: SessionView) => void;
}) {
  const [editing, setEditing] = useState<Profile>();
  const [token, setToken] = useState("");
  const [busy, setBusy] = useState(false);
  const act = async (f: () => Promise<SessionView>) => {
    if (busy) return false;
    setBusy(true);
    suspendSession();
    try {
      onChange(await f());
      return true;
    } catch (e) {
      toast.error(errorMessage(e), { id: "session-error" });
      try {
        onChange(await getSession());
      } catch (error) {
        toast.error(errorMessage(error));
      }
      return false;
    } finally {
      setBusy(false);
    }
  };
  const create = () => {
    setToken("");
    setEditing({
      id: "",
      name: "新配置",
      mode: "existing",
      endpoint: "http://127.0.0.1:19876",
      data_dir: "",
      executable: "",
      server_id: "",
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
    const p = { ...editing, mode: value, server_id: "" };
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
  const locked =
    editing?.id === session.current?.id &&
    (session.connected || session.busy || session.owned_server);
  const cancel = () => {
    if (!busy) setEditing(undefined);
  };
  return (
    <>
      <section className="panel settings-panel">
        <div className="section-title">
          <h2>配置</h2>
          <Button disabled={busy || session.busy} onClick={create}>
            新建配置
          </Button>
        </div>
        <DataList>
          {session.profiles.map((p) => (
            <DataListItem key={p.id} data-active={p.id === session.current?.id}>
              <div className="profile-summary">
                <strong>{p.name}</strong>
                <small>
                  {p.mode === "local" ? "本机托管" : "已有 MCP Server"} ·{" "}
                  {p.mode === "local" ? p.data_dir : p.endpoint}
                </small>
              </div>
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
                <Button
                  disabled={busy || session.busy}
                  onClick={() => {
                    if (window.confirm("切换配置将关闭未保存的表单，继续？"))
                      void act(() => switchProfile(p.id, true));
                  }}
                >
                  切换
                </Button>
              )}
            </DataListItem>
          ))}
        </DataList>
      </section>
      {editing && (
        <Modal
          title={editing.id ? "编辑配置" : "新建配置"}
          description="设置配置名称与 MCP Server 连接方式。"
          onClose={() => {
            if (!busy && window.confirm("放弃未保存的修改？")) cancel();
          }}
        >
          <form
            onSubmit={(e) => {
              e.preventDefault();
              void act(async () => {
                const next = await saveProfile(editing, token);
                const saved = next.profiles.find(
                  (p) => p.id === editing.id || !session.profiles.some((old) => old.id === p.id),
                );
                if (saved && next.current?.id === saved.id && saved.mode === "local")
                  return switchProfile(saved.id, true);
                return next;
              }).then((ok) => {
                if (ok) cancel();
              });
            }}
          >
            <Stack>
              {locked && (
                <div className="actions">
                  <Button disabled={busy} onClick={() => void act(disconnectSession)}>
                    断开后编辑
                  </Button>
                </div>
              )}
              <Field label="配置名称">
                <input
                  required
                  value={editing.name}
                  disabled={busy || locked}
                  onChange={(e) => setEditing({ ...editing, name: e.target.value })}
                />
              </Field>
              <fieldset className="connection-options" disabled={busy || locked}>
                <legend>
                  连接方式 <Hint text="本机托管由客户端启停；已有 MCP Server 仅通过 HTTP 连接。" />
                </legend>
                <label data-selected={editing.mode === "local"}>
                  <input
                    type="radio"
                    name="profile-mode"
                    checked={editing.mode === "local"}
                    onChange={() => void mode("local")}
                  />
                  本机托管
                </label>
                <label data-selected={editing.mode === "existing"}>
                  <input
                    type="radio"
                    name="profile-mode"
                    checked={editing.mode === "existing"}
                    onChange={() => void mode("existing")}
                  />
                  已有 MCP Server
                </label>
              </fieldset>
              {editing.mode === "existing" ? (
                <>
                  <Field label="Admin 地址" hint="远程管理请使用 SSH 隧道。">
                    <input
                      type="url"
                      required
                      value={editing.endpoint}
                      disabled={busy || locked}
                      onChange={(e) => setEditing({ ...editing, endpoint: e.target.value })}
                    />
                  </Field>
                  <Field label="管理员凭据" hint="留空保留此配置已有的凭据。">
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
                  <Field label="MCP Server 程序">
                    <div className="path-input">
                      <input
                        value={editing.executable}
                        disabled={busy || locked}
                        placeholder="dbmcp.exe / dbmcp"
                        onChange={(e) => setEditing({ ...editing, executable: e.target.value })}
                      />
                      <Button
                        disabled={busy || locked || !isTauri()}
                        onClick={async () => {
                          try {
                            const p = await open({ multiple: false });
                            if (typeof p === "string") setEditing({ ...editing, executable: p });
                          } catch (e) {
                            toast.error(errorMessage(e));
                          }
                        }}
                      >
                        浏览…
                      </Button>
                    </div>
                  </Field>
                  <Field
                    label="数据目录"
                    hint="留空创建独立目录；使用已有数据时填写原目录的绝对路径。"
                  >
                    <div className="path-input">
                      <input
                        value={editing.data_dir}
                        disabled={busy || locked}
                        onChange={(e) => setEditing({ ...editing, data_dir: e.target.value })}
                      />
                      <Button
                        disabled={busy || locked || !isTauri()}
                        onClick={async () => {
                          try {
                            const p = await open({ directory: true, multiple: false });
                            if (typeof p === "string") setEditing({ ...editing, data_dir: p });
                          } catch (e) {
                            toast.error(errorMessage(e));
                          }
                        }}
                      >
                        浏览…
                      </Button>
                    </div>
                  </Field>
                </>
              )}
              <div className="form-actions profile-editor-actions">
                {editing.id && (
                  <Button
                    danger
                    disabled={busy || locked}
                    onClick={() => {
                      if (window.confirm("删除此配置及保存的凭据？MCP Server 数据会保留。"))
                        void act(() => deleteProfile(editing.id)).then((ok) => {
                          if (ok) cancel();
                        });
                    }}
                  >
                    删除配置
                  </Button>
                )}
                <Button disabled={busy} onClick={cancel}>
                  取消
                </Button>
                <Button type="submit" variant="primary" disabled={busy || locked}>
                  保存
                </Button>
              </div>
            </Stack>
          </form>
        </Modal>
      )}
    </>
  );
}
