import { useEffect, useRef, useState, type ReactNode } from "react";
import { isTauri } from "@tauri-apps/api/core";
import { getCurrentWindow } from "@tauri-apps/api/window";
import { Copy, Minus, Square, X, type LucideIcon } from "lucide-react";

export function Page({
  title,
  icon,
  count,
  description,
  actions,
  children,
}: {
  title: string;
  icon: LucideIcon;
  count?: number;
  description?: string;
  actions?: ReactNode;
  children: ReactNode;
}) {
  const page = useRef<HTMLElement>(null);
  useEffect(() => {
    page.current?.focus({ preventScroll: true });
  }, [title]);
  return (
    <section className="page" ref={page} tabIndex={-1}>
      <PageHeader title={title} icon={icon} count={count}>
        {actions}
      </PageHeader>
      <div className="page-content">
        {description && <p className="page-description">{description}</p>}
        {children}
      </div>
    </section>
  );
}

function PageHeader({
  title,
  icon: Icon,
  count,
  children,
}: {
  title: string;
  icon: LucideIcon;
  count?: number;
  children?: ReactNode;
}) {
  return (
    <header className="page-title" data-tauri-drag-region>
      <div className="page-heading" data-tauri-drag-region>
        <Icon size={18} />
        <h1 data-tauri-drag-region>{title}</h1>
        {count !== undefined && <span className="page-count">{count}</span>}
      </div>
      <div className="page-actions">{children}</div>
      {isTauri() && <WindowControls />}
    </header>
  );
}

function WindowControls() {
  const [maximized, setMaximized] = useState(false);
  useEffect(() => {
    const appWindow = getCurrentWindow();
    const update = () => void appWindow.isMaximized().then(setMaximized);
    update();
    const listener = appWindow.onResized(update);
    return () => {
      void listener.then((unlisten) => unlisten());
    };
  }, []);
  return (
    <div className="window-controls">
      <button aria-label="最小化" title="最小化" onClick={() => void getCurrentWindow().minimize()}>
        <Minus size={15} />
      </button>
      <button
        aria-label={maximized ? "还原窗口" : "最大化"}
        title={maximized ? "还原窗口" : "最大化"}
        onClick={() => void getCurrentWindow().toggleMaximize()}
      >
        {maximized ? <Copy size={13} /> : <Square size={13} />}
      </button>
      <button
        className="window-close"
        aria-label="关闭窗口"
        title="关闭窗口"
        onClick={() => void getCurrentWindow().close()}
      >
        <X size={16} />
      </button>
    </div>
  );
}
