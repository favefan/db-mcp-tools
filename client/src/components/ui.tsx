import * as Dialog from "@radix-ui/react-dialog";
import * as Tooltip from "@radix-ui/react-tooltip";
import { Slot } from "@radix-ui/react-slot";
import { X, CircleAlert } from "lucide-react";
import { Children, useRef, type ComponentProps, type ReactNode } from "react";

export function Button({
  variant = "secondary",
  danger = false,
  tone,
  asChild = false,
  className = "",
  type = "button",
  ...props
}: ComponentProps<"button"> & {
  variant?: "primary" | "secondary" | "text" | "icon";
  danger?: boolean;
  tone?: "success" | "danger" | "warning";
  asChild?: boolean;
}) {
  const Component = asChild ? Slot : "button";
  const style =
    variant === "text" ? "text-button" : variant === "icon" ? "icon-button" : `button ${variant}`;
  return (
    <Component
      {...props}
      type={asChild ? undefined : type}
      className={`${style}${danger ? " danger" : ""}${tone ? ` tone-${tone}` : ""}${className ? ` ${className}` : ""}`}
    />
  );
}

export function Stack({ className = "", ...props }: ComponentProps<"div">) {
  return <div {...props} className={`stack${className ? ` ${className}` : ""}`} />;
}

export function DataTable({
  label,
  columns,
  children,
}: {
  label: string;
  columns: string[];
  children: ReactNode;
}) {
  const rows = Children.toArray(children);
  return (
    <div className="table-wrap">
      <table className="data-table" aria-label={label}>
        <thead>
          <tr>
            {columns.map((column) => (
              <th key={column} scope="col">
                {column}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.length ? (
            rows
          ) : (
            <tr>
              <td className="table-empty" colSpan={columns.length}>
                暂无记录
              </td>
            </tr>
          )}
        </tbody>
      </table>
    </div>
  );
}

export function DataList(props: ComponentProps<"ul">) {
  return <ul {...props} className="data-list" />;
}

export function DataListItem(props: ComponentProps<"li">) {
  return <li {...props} className="compact-row" />;
}

export function ViewTabs<T extends string>({
  label,
  value,
  onValueChange,
  items,
  hidden = false,
}: {
  label: string;
  value: T;
  onValueChange: (value: T) => void;
  items: { value: T; label: ReactNode }[];
  hidden?: boolean;
}) {
  return (
    <div className="view-tabs" role="group" aria-label={label} hidden={hidden}>
      {items.map((item) => (
        <button
          key={item.value}
          type="button"
          aria-pressed={value === item.value}
          onClick={() => onValueChange(item.value)}
        >
          {item.label}
        </button>
      ))}
    </div>
  );
}

export function Card({
  kind = "resource",
  className = "",
  ...props
}: ComponentProps<"article"> & { kind?: "resource" | "approval" }) {
  return (
    <article
      {...props}
      className={`${kind === "approval" ? "approval-card" : "instance-card"}${className ? ` ${className}` : ""}`}
    />
  );
}

export function Notice({
  title,
  children,
  action,
  tone = "warning",
}: {
  title?: string;
  children: ReactNode;
  action?: ReactNode;
  tone?: "warning" | "error";
}) {
  return (
    <div className={`connection-banner ${tone}`} role={tone === "error" ? "alert" : "status"}>
      <div className="notice-message">
        {title && <strong>{title}</strong>}
        <div>{children}</div>
      </div>
      {action}
    </div>
  );
}

export function Modal({
  title,
  children,
  onClose,
  wide = false,
  stable = false,
  description = "管理数据库访问配置。保存后应用到 MCP Server。",
}: {
  title: string;
  children: ReactNode;
  onClose: () => void;
  wide?: boolean;
  stable?: boolean;
  description?: string;
}) {
  const content = useRef<HTMLDivElement>(null);
  return (
    <Dialog.Root open onOpenChange={(open) => !open && onClose()}>
      <Dialog.Portal>
        <Dialog.Overlay className="overlay" />
        <Dialog.Content
          ref={content}
          onOpenAutoFocus={(e) => {
            e.preventDefault();
            content.current?.focus({ preventScroll: true });
          }}
          className={`modal ${wide ? "wide" : ""} ${stable ? "stable" : ""}`}
        >
          <div className="modal-heading">
            <Dialog.Title>{title}</Dialog.Title>
            <Dialog.Close asChild>
              <Button variant="icon" aria-label="关闭">
                <X size={18} />
              </Button>
            </Dialog.Close>
          </div>
          <Dialog.Description className="sr-only">{description}</Dialog.Description>
          <div className="modal-body">{children}</div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
export function Drawer({
  title,
  description,
  actions,
  children,
  onClose,
}: {
  title: string;
  description: string;
  actions?: ReactNode;
  children: ReactNode;
  onClose: () => void;
}) {
  const content = useRef<HTMLDivElement>(null);
  return (
    <Dialog.Root open modal={false} onOpenChange={(open) => !open && onClose()}>
      <Dialog.Portal>
        <Dialog.Content
          ref={content}
          onOpenAutoFocus={(e) => {
            e.preventDefault();
            content.current?.focus({ preventScroll: true });
          }}
          className="drawer"
          onInteractOutside={(e) => e.preventDefault()}
        >
          <div className="drawer-heading">
            <Dialog.Title>{title}</Dialog.Title>
            <Dialog.Close asChild>
              <Button variant="icon" aria-label="关闭详情">
                <X size={18} />
              </Button>
            </Dialog.Close>
          </div>
          <Dialog.Description className="sr-only">{description}</Dialog.Description>
          <div className="drawer-actions">{actions}</div>
          <div className="drawer-body">{children}</div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

export function Hint({ text }: { text: string }) {
  return (
    <Tooltip.Provider delayDuration={250}>
      <Tooltip.Root>
        <Tooltip.Trigger asChild>
          <span
            className="hint-trigger"
            tabIndex={0}
            aria-label={text}
            onClick={(e) => e.preventDefault()}
          >
            <CircleAlert size={14} />
          </span>
        </Tooltip.Trigger>
        <Tooltip.Portal>
          <Tooltip.Content className="hint-content" sideOffset={6}>
            {text}
            <Tooltip.Arrow className="hint-arrow" />
          </Tooltip.Content>
        </Tooltip.Portal>
      </Tooltip.Root>
    </Tooltip.Provider>
  );
}

export function Field({
  label,
  children,
  hint,
}: {
  label: string;
  children: ReactNode;
  hint?: string;
}) {
  return (
    <label className="field">
      <span className="field-label">
        {label}
        {hint && <Hint text={hint} />}
      </span>
      {children}
    </label>
  );
}
export function Badge({ children, tone = "neutral" }: { children: ReactNode; tone?: string }) {
  return <span className={`badge ${tone}`}>{children}</span>;
}
export function Empty({ title, children }: { title: string; children?: ReactNode }) {
  return (
    <div className="empty">
      <strong>{title}</strong>
      <p>{children}</p>
    </div>
  );
}
