import { useEffect, useId, useState, type CSSProperties } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  asyncDataLoaderFeature,
  hotkeysCoreFeature,
  selectionFeature,
  syncDataLoaderFeature,
  type TreeInstance,
} from "@headless-tree/core";
import { useTree } from "@headless-tree/react";
import {
  ArrowLeft,
  ArrowRight,
  Check,
  ChevronRight,
  Columns3,
  Database,
  Folder,
  LoaderCircle,
  RefreshCw,
  Table2,
} from "lucide-react";
import { api, errorMessage, type Instance, type Column } from "../lib/api";
import type { components } from "../lib/contract";
import {
  compactResources,
  databaseResource,
  includesObject,
  qualifyResources,
  resourceLabel,
} from "../lib/resource-scope";
import { Empty, Button } from "../components/ui";

type Catalog = components["schemas"]["Catalog"];
type Node = {
  id: string;
  name: string;
  kind: "all" | "database" | "schema" | "table" | "column" | "loading";
  database?: string;
  schema?: string;
  table?: string;
  resource: string;
};
const icons = {
  all: Folder,
  database: Database,
  schema: Folder,
  table: Table2,
  column: Columns3,
  loading: LoaderCircle,
};

export function ScopePicker({
  instanceId,
  instance,
  resources,
  onChange,
}: {
  instanceId?: string;
  instance?: Instance;
  resources: string[];
  onChange: (resources: string[]) => void;
}) {
  const [revision, setRevision] = useState(0);
  const saved = useQuery({
    queryKey: ["instance", instanceId],
    queryFn: () => api<Instance>(`instances/${instanceId}`),
    enabled: !instance && !!instanceId,
  });
  const source = instance ?? saved.data;
  if (!source)
    return (
      <Empty title={saved.error ? "实例读取失败" : "正在读取实例…"}>
        {saved.error ? errorMessage(saved.error) : undefined}
      </Empty>
    );
  if (!source.connection.host || !source.connection.username)
    return (
      <Empty title="先填写连接信息">
        在连接页填写主机、端口和账号后，在这里读取并选择远端对象。
      </Empty>
    );
  return (
    <ScopeTransfer
      key={revision}
      source={source}
      draft={!!instance}
      resources={resources}
      onChange={onChange}
      onRefresh={() => {
        if (!instance) void saved.refetch();
        setRevision((v) => v + 1);
      }}
    />
  );
}

function ScopeTransfer({
  source,
  draft,
  resources,
  onChange,
  onRefresh,
}: {
  source: Instance;
  draft: boolean;
  resources: string[];
  onChange: (resources: string[]) => void;
  onRefresh: () => void;
}) {
  const queryClient = useQueryClient();
  const uid = useId();
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [leftSelected, setLeftSelected] = useState<string[]>([]);
  const [rightSelected, setRightSelected] = useState<string[]>([]);
  const grants = compactResources(qualifyResources(resources));
  const [provider] = useState(() => {
    const nodes = new Map<string, Node>([
      ["__root", { id: "__root", name: "远端对象", kind: "all", resource: "" }],
      [
        "*",
        { id: "*", name: draft ? "全部数据库" : "实例允许的全部对象", kind: "all", resource: "*" },
      ],
    ]);
    const catalogs = new Map<string, Catalog>();
    const add = (node: Node) => {
      nodes.set(node.id, node);
      return node.id;
    };
    const loadCatalog = async (database: string) => {
      const catalog = await queryClient.fetchQuery<Catalog>({
        queryKey: ["scope", uid, database],
        queryFn: () =>
          draft
            ? api<Catalog>("instances/schema", "POST", { instance: source, database })
            : api<Catalog>(
                `instances/${source.id}/schema?database=${encodeURIComponent(database)}`,
              ),
        retry: false,
        staleTime: Infinity,
      });
      catalogs.set(database, catalog);
      return catalog;
    };
    const tables = (database: string, columns: Column[]) =>
      [...new Map(columns.map((c) => [c.table_resource, c])).values()].map((c) =>
        add({
          id: c.table_resource,
          name: c.table,
          kind: "table",
          database,
          schema: c.schema,
          table: c.table,
          resource: c.table_resource,
        }),
      );
    return {
      nodes,
      async getChildren(id: string): Promise<string[]> {
        try {
          setErrors((current) => {
            if (!current[id]) return current;
            const next = { ...current };
            delete next[id];
            return next;
          });
          if (id === "__root") return ["*"];
          if (id === "*") {
            const names = await queryClient.fetchQuery<string[]>({
              queryKey: ["scope", uid, "databases"],
              queryFn: () =>
                draft
                  ? api<string[]>("instances/databases", "POST", source)
                  : api<string[]>(`instances/${source.id}/databases`),
              retry: false,
              staleTime: Infinity,
            });
            return names.map((name) => {
              const key = databaseResource(name) + "/*";
              return add({ id: key, name, kind: "database", database: name, resource: key });
            });
          }
          const node = nodes.get(id);
          if (!node?.database) return [];
          const catalog = catalogs.get(node.database) ?? (await loadCatalog(node.database));
          if (node.kind === "database") {
            if (source.driver === "mysql") return tables(node.database, catalog.columns ?? []);
            return (catalog.schemas ?? []).map((schema) => {
              const key = databaseResource(node.database!, schema) + ".*";
              return add({
                id: key,
                name: schema,
                kind: "schema",
                database: node.database,
                schema,
                resource: key,
              });
            });
          }
          if (node.kind === "schema")
            return tables(
              node.database,
              (catalog.columns ?? []).filter((c) => c.schema === node.schema),
            );
          if (node.kind === "table")
            return (catalog.columns ?? [])
              .filter((c) => c.schema === node.schema && c.table === node.table)
              .map((c) =>
                add({ id: c.resource, name: c.name, kind: "column", resource: c.resource }),
              );
          return [];
        } catch (error) {
          setErrors((current) => ({ ...current, [id]: errorMessage(error) }));
          return [];
        }
      },
    };
  });
  const left = useTree<Node>({
    hotkeys: { toggleSelectedItem: { hotkey: "Space" } },
    rootItemId: "__root",
    initialState: { expandedItems: ["*"] },
    state: { selectedItems: leftSelected },
    setSelectedItems: setLeftSelected,
    getItemName: (item) => item.getItemData().name,
    isItemFolder: (item) => !["column", "loading"].includes(item.getItemData().kind),
    createLoadingItemData: () => ({
      id: "loading",
      name: "正在读取…",
      kind: "loading",
      resource: "",
    }),
    dataLoader: {
      getItem: async (id) =>
        provider.nodes.get(id) ?? { id, name: "正在读取…", kind: "loading", resource: "" },
      getChildren: provider.getChildren,
    },
    features: [asyncDataLoaderFeature, selectionFeature, hotkeysCoreFeature],
  });
  const right = useTree<Node>({
    hotkeys: { toggleSelectedItem: { hotkey: "Space" } },
    rootItemId: "__root",
    state: { selectedItems: rightSelected },
    setSelectedItems: setRightSelected,
    getItemName: (item) => item.getItemData().name,
    isItemFolder: () => false,
    dataLoader: {
      getItem: (id) => ({
        id,
        name: resourceLabel(id, source.driver),
        kind: "column",
        resource: id,
      }),
      getChildren: () => grants,
    },
    features: [syncDataLoaderFeature, selectionFeature, hotkeysCoreFeature],
  });
  useEffect(() => {
    right.rebuildTree();
  }, [right, resources]);
  const additions = leftSelected
    .map((id) => provider.nodes.get(id)?.resource)
    .filter((key): key is string => !!key && !includesObject(grants, key));
  const remove = () => {
    onChange(grants.filter((key) => !rightSelected.includes(key)));
    right.setSelectedItems([]);
  };
  const add = () => {
    onChange(compactResources([...grants, ...additions]));
    left.setSelectedItems([]);
  };
  return (
    <div className="form-stack scope-editor">
      <div className="scope-toolbar">
        <span className="muted">
          选择整库、{source.driver === "mysql" ? "表或字段" : "模式、表或字段"}
          ；许可上级包含今后新增的下级对象。
        </span>
        <Button onClick={onRefresh}>
          <RefreshCw size={14} />
          刷新对象
        </Button>
      </div>
      <div className="scope-transfer">
        <section className="scope-pane">
          <div className="scope-pane-header">
            <strong>远端对象</strong>
            <small>
              {source.driver === "mysql"
                ? "数据库 → 表 → 字段"
                : source.driver === "dm"
                  ? "数据库 → 用户／模式 → 表 → 字段"
                  : "数据库 → 模式 → 表 → 字段"}
            </small>
          </div>
          <ScopeTree tree={left} label="远端对象" covered={(key) => includesObject(grants, key)} />
        </section>
        <div className="scope-arrows">
          <Button
            aria-label="许可选中对象"
            title="移入许可对象"
            disabled={!additions.length}
            onClick={add}
          >
            <ArrowRight size={16} />
          </Button>
          <Button
            aria-label="移除选中许可"
            title="移出许可对象"
            disabled={!rightSelected.some((key) => grants.includes(key))}
            onClick={remove}
          >
            <ArrowLeft size={16} />
          </Button>
        </div>
        <section className="scope-pane">
          <div className="scope-pane-header">
            <strong>许可对象</strong>
            <small>{grants.length} 项范围</small>
          </div>
          <ScopeTree tree={right} label="许可对象" />
          {!grants.length && <p className="scope-empty">尚未许可任何对象</p>}
        </section>
      </div>
      {!!Object.keys(errors).length && (
        <p className="error" role="alert">
          {Object.values(errors).join("；")}。请检查连接后刷新对象。
        </p>
      )}
      <small className="muted">
        Ctrl / ⌘ 或 Shift 可多选，方向键导航、空格选择。缩小范围请先移出上级，再许可所需下级。
        {draft ? "实例范围是实体授权的上限。" : "实体授权仍与实例范围取交集。"}
      </small>
    </div>
  );
}

function ScopeTree({
  tree,
  label,
  covered,
}: {
  tree: TreeInstance<Node>;
  label: string;
  covered?: (key: string) => boolean;
}) {
  return (
    <div {...tree.getContainerProps(label)} className="scope-tree">
      {tree.getItems().map((item) => {
        const data = item.getItemData(),
          Icon = icons[data.kind];
        return (
          <div
            {...item.getProps()}
            key={item.getId()}
            className="scope-item"
            data-selected={item.isSelected()}
            style={{ "--tree-level": item.getItemMeta().level } as CSSProperties}
            title={data.name}
          >
            {item.isFolder() ? (
              <Button
                variant="icon"
                className="scope-expander"
                tabIndex={-1}
                aria-label={`${item.isExpanded() ? "收起" : "展开"} ${data.name}`}
                onClick={(event) => {
                  event.stopPropagation();
                  if (item.isExpanded()) item.collapse();
                  else item.expand();
                }}
              >
                <ChevronRight size={13} className={item.isExpanded() ? "expanded" : ""} />
              </Button>
            ) : (
              <span className="scope-expander" />
            )}
            {item.isLoading() ? <LoaderCircle size={14} className="spin" /> : <Icon size={14} />}
            <span className="scope-name">{data.name}</span>
            {covered?.(data.resource) && (
              <Check size={12} className="scope-included" aria-label="已许可" />
            )}
          </div>
        );
      })}
    </div>
  );
}
