"use client"

import { useEffect, useMemo, useRef, useState } from "react"
import type { Core, ElementDefinition } from "cytoscape"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Badge, healthVariant } from "@/components/ui/badge"
import type { ServiceGraph, ServiceNode, ServiceEdge } from "@/lib/api"

// Health colors for application nodes.
const HEALTH_COLOR: Record<string, string> = {
  healthy: "#22c55e",
  degraded: "#eab308",
  unhealthy: "#ef4444",
  unknown: "#64748b",
}

// Type colors for infrastructure nodes (db/cache/queue have no health signal).
const TYPE_COLOR: Record<string, string> = {
  database: "#8b5cf6",
  cache: "#06b6d4",
  queue: "#f97316",
}

// Edge colors by real call type (actual data from ARMS APM labels).
const EDGE_COLOR: Record<string, string> = {
  http: "#3b82f6",
  rpc: "#3b82f6",
  database: "#8b5cf6",
  cache: "#06b6d4",
  queue: "#f97316",
}

const TYPE_SHAPE: Record<string, string> = {
  application: "round-rectangle",
  database: "cylinder",
  cache: "diamond",
  queue: "hexagon",
}

const KNOWN_ENVS = ["prod", "uat", "fat", "dev", "test", "pre", "gray", "sim", "daily", "stable"]

// Per-side neighbor cap in the focus graph: showing the busiest N keeps the
// graph readable instead of cramming dozens of nodes into one column.
const NEIGHBOR_LIMIT = 12

type SortKey = "name" | "throughput" | "error"
type Status = "loading" | "error" | "ready"

// envOf extracts an environment prefix (prod_/dev_/...) from a service name.
function envOf(name: string): string {
  const i = name.indexOf("_")
  if (i > 0) {
    const p = name.slice(0, i).toLowerCase()
    if (KNOWN_ENVS.includes(p)) return p
  }
  return "other"
}

function shortLabel(name: string): string {
  return name.length > 26 ? name.slice(0, 24) + "…" : name
}

function fmtRps(v: number): string {
  if (v >= 1000) return `${(v / 1000).toFixed(1)}k`
  if (v >= 10) return v.toFixed(0)
  return v.toFixed(1)
}

export default function TopologyPage() {
  const containerRef = useRef<HTMLDivElement>(null)
  const [graph, setGraph] = useState<ServiceGraph | null>(null)
  const [status, setStatus] = useState<Status>("loading")
  const [query, setQuery] = useState("")
  const [envFilter, setEnvFilter] = useState<string>("all")
  const [healthFilter, setHealthFilter] = useState<string>("all")
  const [sortKey, setSortKey] = useState<SortKey>("throughput")
  const [focusId, setFocusId] = useState<string | null>(null)
  const [tip, setTip] = useState<{ node: ServiceNode; x: number; y: number } | null>(null)

  // Fetch the full graph once. No mock fallback: topology is real data now, so
  // a failure is shown honestly instead of masked by a fake demo graph.
  useEffect(() => {
    let disposed = false
    import("@/lib/api").then(({ api }) => {
      api
        .getTopology()
        .then((res) => {
          if (disposed) return
          setGraph(res.topology)
          setStatus("ready")
        })
        .catch(() => {
          if (disposed) return
          setStatus("error")
        })
    })
    return () => {
      disposed = true
    }
  }, [])

  // Index nodes and build adjacency once per graph load.
  const index = useMemo(() => {
    const byId = new Map<string, ServiceNode>()
    const deps = new Map<string, ServiceEdge[]>() // outgoing: node -> things it calls
    const dependents = new Map<string, ServiceEdge[]>() // incoming: callers of node
    if (graph) {
      for (const n of graph.nodes) byId.set(n.id, n)
      for (const e of graph.edges) {
        if (!deps.has(e.from_node_id)) deps.set(e.from_node_id, [])
        if (!dependents.has(e.to_node_id)) dependents.set(e.to_node_id, [])
        deps.get(e.from_node_id)!.push(e)
        dependents.get(e.to_node_id)!.push(e)
      }
    }
    return { byId, deps, dependents }
  }, [graph])

  const stats = useMemo(() => {
    const out = {
      total: 0,
      app: 0,
      byHealth: {} as Record<string, number>,
      byType: {} as Record<string, number>,
      byEnv: {} as Record<string, number>,
    }
    if (!graph) return out
    out.total = graph.nodes.length
    for (const n of graph.nodes) {
      out.byType[n.type] = (out.byType[n.type] ?? 0) + 1
      if (n.type === "application") {
        out.app++
        out.byHealth[n.health] = (out.byHealth[n.health] ?? 0) + 1
        out.byEnv[envOf(n.name)] = (out.byEnv[envOf(n.name)] ?? 0) + 1
      }
    }
    return out
  }, [graph])

  // Filtered + sorted application services for the searchable list.
  const matches = useMemo(() => {
    if (!graph) return [] as ServiceNode[]
    const q = query.trim().toLowerCase()
    const list = graph.nodes
      .filter((n) => n.type === "application")
      .filter((n) => (envFilter === "all" ? true : envOf(n.name) === envFilter))
      .filter((n) => (healthFilter === "all" ? true : n.health === healthFilter))
      .filter((n) => (q ? n.name.toLowerCase().includes(q) : true))
    list.sort((a, b) => {
      if (sortKey === "name") return a.name.localeCompare(b.name)
      if (sortKey === "throughput") return b.throughput - a.throughput
      return b.error_rate - a.error_rate // "error"
    })
    return list
  }, [graph, query, envFilter, healthFilter, sortKey])

  const focusNode = focusId ? index.byId.get(focusId) ?? null : null
  const focusDeps = focusId ? index.deps.get(focusId) ?? [] : []
  const focusDependents = focusId ? index.dependents.get(focusId) ?? [] : []

  // Build the neighbor split once (also used to size the canvas), sorted by
  // the neighbor's throughput so the busiest — usually most relevant — show.
  const neighborSplit = useMemo(() => {
    if (!focusId || !graph) return { up: [] as string[], down: [] as string[], upTotal: 0, downTotal: 0 }
    const tp = (id: string) => index.byId.get(id)?.throughput ?? 0
    const dSeen = new Set<string>()
    const uSeen = new Set<string>()
    const down: string[] = []
    const up: string[] = []
    for (const e of index.deps.get(focusId) ?? []) {
      if (e.to_node_id !== focusId && !dSeen.has(e.to_node_id)) {
        dSeen.add(e.to_node_id)
        down.push(e.to_node_id)
      }
    }
    for (const e of index.dependents.get(focusId) ?? []) {
      if (e.from_node_id !== focusId && !uSeen.has(e.from_node_id) && !dSeen.has(e.from_node_id)) {
        uSeen.add(e.from_node_id)
        up.push(e.from_node_id)
      }
    }
    up.sort((a, b) => tp(b) - tp(a))
    down.sort((a, b) => tp(b) - tp(a))
    return {
      up: up.slice(0, NEIGHBOR_LIMIT),
      down: down.slice(0, NEIGHBOR_LIMIT),
      upTotal: up.length,
      downTotal: down.length,
    }
  }, [focusId, graph, index])

  // Adaptive canvas height: grow with the busier column so nodes never get
  // crushed, but stay bounded so the card doesn't explode.
  const graphHeight = useMemo(() => {
    const rows = Math.max(neighborSplit.up.length, neighborSplit.down.length, 1)
    return Math.min(820, Math.max(440, rows * 52 + 80))
  }, [neighborSplit])

  // Render the focused service's 1-hop neighborhood as a left→right flow.
  useEffect(() => {
    if (!containerRef.current || !focusId || !graph) return
    let cy: Core | null = null

    import("cytoscape").then((mod) => {
      const cytoscape = mod.default
      const { up: upstreamIds, down: downstreamIds } = neighborSplit

      const COL = 360
      const rowCount = Math.max(upstreamIds.length, downstreamIds.length, 1)
      const ROW = Math.min(64, (graphHeight - 100) / rowCount)
      const colY = (count: number, i: number) => (i - (count - 1) / 2) * ROW
      const elements: ElementDefinition[] = []

      const pushNode = (id: string, x: number, y: number) => {
        const n = index.byId.get(id)
        if (!n) return
        elements.push({
          data: {
            id: n.id,
            label: shortLabel(n.name),
            health: n.health,
            type: n.type,
            isFocus: n.id === focusId ? 1 : 0,
          },
          position: { x, y },
        })
      }

      pushNode(focusId, 0, 0)
      upstreamIds.forEach((id, i) => pushNode(id, -COL, colY(upstreamIds.length, i)))
      downstreamIds.forEach((id, i) => pushNode(id, COL, colY(downstreamIds.length, i)))

      const placed = new Set<string>([focusId, ...upstreamIds, ...downstreamIds])
      for (const e of graph.edges) {
        if (
          (e.from_node_id === focusId || e.to_node_id === focusId) &&
          placed.has(e.from_node_id) &&
          placed.has(e.to_node_id)
        ) {
          elements.push({
            data: {
              id: e.id,
              source: e.from_node_id,
              target: e.to_node_id,
              etype: e.type,
            },
          })
        }
      }

      cy = cytoscape({
        container: containerRef.current,
        elements,
        style: [
          {
            selector: "node",
            style: {
              label: "data(label)",
              "background-color": (ele: any) =>
                ele.data("type") === "application"
                  ? HEALTH_COLOR[ele.data("health")] ?? HEALTH_COLOR.unknown
                  : TYPE_COLOR[ele.data("type")] ?? HEALTH_COLOR.unknown,
              shape: (ele: any) => (TYPE_SHAPE[ele.data("type")] ?? "ellipse") as any,
              color: "#e2e8f0",
              "font-size": 9,
              width: (ele: any) => (ele.data("isFocus") ? 58 : 42),
              height: (ele: any) => (ele.data("isFocus") ? 58 : 42),
              "border-width": (ele: any) => (ele.data("isFocus") ? 3 : 0),
              "border-color": "#f8fafc",
              "text-valign": "bottom",
              "text-margin-y": 5,
              "text-max-width": "100px",
            },
          },
          {
            selector: "edge",
            style: {
              width: 2,
              "line-color": (ele: any) => EDGE_COLOR[ele.data("etype")] ?? "#475569",
              "target-arrow-color": (ele: any) => EDGE_COLOR[ele.data("etype")] ?? "#475569",
              "target-arrow-shape": "triangle",
              "curve-style": "bezier",
              "arrow-scale": 1,
              label: "data(etype)",
              "font-size": 7,
              color: "#94a3b8",
              "text-rotation": "autorotate",
              "text-background-color": "#0f172a",
              "text-background-opacity": 0.7,
              "text-background-padding": "2px",
            },
          },
          {
            selector: "node:active, node.hl",
            style: { "overlay-opacity": 0.1 },
          },
        ],
        layout: { name: "preset", fit: true, padding: 36 } as any,
      })

      cy.on("tap", "node", (evt: any) => {
        const id = evt.target.id()
        if (id && id !== focusId) {
          setTip(null)
          setFocusId(id)
        }
      })

      // Hover tooltip: show the neighbor's REAL per-service RED metrics (edge-
      // level metrics are not available from the backend, so we never fake
      // them onto edges — node metrics are the honest, in-hand data).
      cy.on("mouseover", "node", (evt: any) => {
        const n = index.byId.get(evt.target.id())
        const rp = evt.target.renderedPosition()
        if (n) setTip({ node: n, x: rp.x, y: rp.y })
      })
      cy.on("mouseout", "node", () => setTip(null))
      cy.on("pan zoom", () => setTip(null))
    })

    return () => {
      cy?.destroy()
    }
  }, [focusId, graph, index, neighborSplit, graphHeight])

  return (
    <div className="space-y-8">
      <header>
        <h1 className="text-3xl font-bold tracking-tight">Topology</h1>
        <p className="text-muted-foreground">
          服务依赖拓扑 — 搜索并选择一个服务，查看它的直接依赖与被依赖关系
        </p>
      </header>

      {status === "ready" && graph && (
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-4 lg:grid-cols-6">
          <StatCard label="节点总数" value={stats.total} />
          <StatCard label="应用服务" value={stats.app} />
          <StatCard label="健康" value={stats.byHealth.healthy ?? 0} color="#22c55e" />
          <StatCard label="降级" value={stats.byHealth.degraded ?? 0} color="#eab308" />
          <StatCard label="数据库" value={stats.byType.database ?? 0} color="#8b5cf6" />
          <StatCard label="缓存/队列" value={(stats.byType.cache ?? 0) + (stats.byType.queue ?? 0)} color="#06b6d4" />
        </div>
      )}

      <div className="grid gap-6 lg:grid-cols-3">
        <Card className="lg:col-span-2">
          <CardHeader>
            <CardTitle>{focusNode ? focusNode.name : "服务列表"}</CardTitle>
            <CardDescription>
              {focusNode
                ? `${focusNode.type} · 1 跳邻域（悬停看指标，点击邻居继续探索）`
                : "搜索、排序、筛选，点击一个服务以聚焦其依赖关系"}
            </CardDescription>
          </CardHeader>
          <CardContent>
            <div className="mb-4 flex flex-wrap items-center gap-2">
              <input
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder="搜索服务名…"
                className="h-9 flex-1 min-w-[150px] rounded-md border border-border bg-background px-3 text-sm outline-none focus:ring-1 focus:ring-ring"
              />
              {!focusId && (
                <>
                  <select
                    value={sortKey}
                    onChange={(e) => setSortKey(e.target.value as SortKey)}
                    className="h-9 rounded-md border border-border bg-background px-2 text-sm outline-none focus:ring-1 focus:ring-ring"
                    title="排序"
                  >
                    <option value="throughput">按吞吐</option>
                    <option value="error">按错误率</option>
                    <option value="name">按名称</option>
                  </select>
                  <select
                    value={healthFilter}
                    onChange={(e) => setHealthFilter(e.target.value)}
                    className="h-9 rounded-md border border-border bg-background px-2 text-sm outline-none focus:ring-1 focus:ring-ring"
                    title="健康筛选"
                  >
                    <option value="all">全部健康</option>
                    <option value="healthy">健康</option>
                    <option value="degraded">降级</option>
                    <option value="unknown">无数据</option>
                  </select>
                  <select
                    value={envFilter}
                    onChange={(e) => setEnvFilter(e.target.value)}
                    className="h-9 rounded-md border border-border bg-background px-2 text-sm outline-none focus:ring-1 focus:ring-ring"
                    title="环境筛选"
                  >
                    <option value="all">全部环境</option>
                    {Object.keys(stats.byEnv)
                      .sort()
                      .map((env) => (
                        <option key={env} value={env}>
                          {env} ({stats.byEnv[env]})
                        </option>
                      ))}
                  </select>
                </>
              )}
              {focusId && (
                <button
                  onClick={() => {
                    setTip(null)
                    setFocusId(null)
                  }}
                  className="h-9 rounded-md border border-border bg-background px-3 text-sm hover:bg-accent"
                >
                  返回列表
                </button>
              )}
            </div>

            {status === "loading" && (
              <div className="flex h-[440px] items-center justify-center text-sm text-muted-foreground">
                加载拓扑数据…
              </div>
            )}
            {status === "error" && (
              <div className="flex h-[440px] items-center justify-center text-sm text-red-400">
                拓扑数据加载失败（后端 /api/v1/topology 不可达或返回错误）
              </div>
            )}

            {status === "ready" && focusId && (
              <>
                <div className="mb-2 flex items-center justify-between px-1 text-xs font-medium">
                  <span className="flex items-center gap-1 text-muted-foreground">
                    <span className="text-sky-400">上游调用方</span>
                    {neighborSplit.upTotal > NEIGHBOR_LIMIT && (
                      <span className="text-amber-400">(前 {NEIGHBOR_LIMIT}/{neighborSplit.upTotal})</span>
                    )}
                    <span>→</span>
                  </span>
                  <span className="text-slate-200">焦点服务</span>
                  <span className="flex items-center gap-1 text-muted-foreground">
                    <span>→</span>
                    <span className="text-sky-400">下游依赖</span>
                    {neighborSplit.downTotal > NEIGHBOR_LIMIT && (
                      <span className="text-amber-400">(前 {NEIGHBOR_LIMIT}/{neighborSplit.downTotal})</span>
                    )}
                  </span>
                </div>
                <div className="relative w-full">
                  <div
                    ref={containerRef}
                    className="w-full rounded-md border bg-background"
                    style={{ height: graphHeight }}
                  />
                  {tip && (
                    <div
                      className="pointer-events-none absolute z-10 w-52 rounded-md border border-border bg-popover/95 p-2 text-xs shadow-lg backdrop-blur"
                      style={{
                        left: Math.max(4, Math.min(tip.x + 12, 560)),
                        top: Math.max(4, tip.y + 12),
                      }}
                    >
                      <div className="mb-1 truncate font-medium text-slate-100">{tip.node.name}</div>
                      <div className="mb-1 flex items-center gap-1.5">
                        <span
                          className="inline-block h-2.5 w-2.5 rounded-full"
                          style={{
                            background:
                              tip.node.type === "application"
                                ? HEALTH_COLOR[tip.node.health] ?? HEALTH_COLOR.unknown
                                : TYPE_COLOR[tip.node.type] ?? HEALTH_COLOR.unknown,
                          }}
                        />
                        <span className="text-muted-foreground">
                          {tip.node.type === "application" ? tip.node.health : tip.node.type}
                        </span>
                      </div>
                      {tip.node.type === "application" && tip.node.throughput > 0 ? (
                        <dl className="space-y-0.5 font-mono">
                          <TipRow k="吞吐" v={`${fmtRps(tip.node.throughput)} rps`} />
                          <TipRow
                            k="错误率"
                            v={`${(tip.node.error_rate * 100).toFixed(2)}%`}
                            warn={tip.node.error_rate > 0.01}
                          />
                          <TipRow k="延迟" v={`${tip.node.latency.toFixed(1)} ms`} />
                        </dl>
                      ) : (
                        <div className="text-muted-foreground">
                          {tip.node.type === "application" ? "近 5 分钟无流量" : "基础设施节点"}
                        </div>
                      )}
                    </div>
                  )}
                </div>
                <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-2 text-xs text-muted-foreground">
                  <span className="font-medium text-slate-300">边类型：</span>
                  {Object.entries(EDGE_COLOR)
                    .filter(([k]) => k !== "rpc")
                    .map(([k, v]) => (
                      <span key={k} className="flex items-center gap-1.5">
                        <span className="inline-block h-0.5 w-4" style={{ background: v }} />
                        {k}
                      </span>
                    ))}
                  <span className="ml-2 font-medium text-slate-300">节点健康：</span>
                  {(["healthy", "degraded", "unknown"] as const).map((k) => (
                    <span key={k} className="flex items-center gap-1.5">
                      <span
                        className="inline-block h-3 w-3 rounded-full"
                        style={{ background: HEALTH_COLOR[k] }}
                      />
                      {k}
                    </span>
                  ))}
                </div>
              </>
            )}

            {status === "ready" && !focusId && (
              <ServiceList matches={matches} onPick={setFocusId} index={index} />
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>{focusNode ? "依赖关系" : "节点详情"}</CardTitle>
            <CardDescription>
              {focusNode ? focusNode.name : "选择一个服务查看其上下游"}
            </CardDescription>
          </CardHeader>
          <CardContent>
            {focusNode ? (
              <div className="space-y-5">
                <div className="flex items-center justify-between">
                  <span className="text-sm text-muted-foreground">健康</span>
                  <Badge variant={healthVariant(focusNode.health)}>{focusNode.health}</Badge>
                </div>

                {focusNode.type === "application" &&
                  (focusNode.throughput > 0 ? (
                    <div className="space-y-2 rounded-md border border-border/60 p-3">
                      <MetricRow label="吞吐" value={`${focusNode.throughput.toFixed(1)} rps`} />
                      <MetricRow
                        label="错误率"
                        value={`${(focusNode.error_rate * 100).toFixed(2)}%`}
                        warn={focusNode.error_rate > 0.01}
                      />
                      <MetricRow label="平均延迟" value={`${focusNode.latency.toFixed(1)} ms`} />
                      <p className="pt-1 text-[10px] text-muted-foreground">近 5 分钟 · 来自 ARMS APM</p>
                    </div>
                  ) : (
                    <p className="rounded-md border border-border/60 p-3 text-xs text-muted-foreground">
                      近 5 分钟无流量数据
                    </p>
                  ))}

                <NeighborList
                  title={`调用的下游 (${new Set(focusDeps.map((e) => e.to_node_id)).size})`}
                  edges={focusDeps}
                  pick={(e) => e.to_node_id}
                  index={index}
                  onPick={setFocusId}
                />
                <NeighborList
                  title={`被哪些上游调用 (${new Set(focusDependents.map((e) => e.from_node_id)).size})`}
                  edges={focusDependents}
                  pick={(e) => e.from_node_id}
                  index={index}
                  onPick={setFocusId}
                />
              </div>
            ) : (
              <p className="text-sm text-muted-foreground">
                从左侧列表选择一个服务，这里会显示它调用的下游、以及调用它的上游（均可点击继续探索）。
              </p>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  )
}

function StatCard({ label, value, color }: { label: string; value: number; color?: string }) {
  return (
    <div className="rounded-lg border border-border bg-card p-3">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-1 text-xl font-semibold" style={color ? { color } : undefined}>
        {value}
      </div>
    </div>
  )
}

function TipRow({ k, v, warn }: { k: string; v: string; warn?: boolean }) {
  return (
    <div className="flex items-center justify-between">
      <span className="text-muted-foreground">{k}</span>
      <span className={warn ? "text-red-400" : "text-slate-200"}>{v}</span>
    </div>
  )
}

function ServiceList({
  matches,
  onPick,
  index,
}: {
  matches: ServiceNode[]
  onPick: (id: string) => void
  index: { deps: Map<string, ServiceEdge[]>; dependents: Map<string, ServiceEdge[]> }
}) {
  const LIMIT = 100
  const shown = matches.slice(0, LIMIT)
  return (
    <div className="max-h-[560px] overflow-y-auto rounded-md border border-border">
      {matches.length === 0 ? (
        <div className="p-6 text-center text-sm text-muted-foreground">没有匹配的服务</div>
      ) : (
        <ul className="divide-y divide-border/60">
          {shown.map((n) => {
            const d = new Set((index.deps.get(n.id) ?? []).map((e) => e.to_node_id)).size
            const u = new Set((index.dependents.get(n.id) ?? []).map((e) => e.from_node_id)).size
            return (
              <li key={n.id}>
                <button
                  onClick={() => onPick(n.id)}
                  className="flex w-full items-center justify-between gap-3 px-3 py-2 text-left text-sm hover:bg-accent"
                >
                  <span className="flex min-w-0 items-center gap-2">
                    <span
                      className="inline-block h-2.5 w-2.5 shrink-0 rounded-full"
                      style={{ background: HEALTH_COLOR[n.health] ?? HEALTH_COLOR.unknown }}
                    />
                    <span className="truncate">{n.name}</span>
                  </span>
                  <span className="flex shrink-0 items-center gap-2 font-mono text-xs text-muted-foreground">
                    {n.throughput > 0 && <span className="text-sky-400">{fmtRps(n.throughput)}rps</span>}
                    {n.error_rate > 0.01 && (
                      <span className="text-red-400">{(n.error_rate * 100).toFixed(1)}%</span>
                    )}
                    <span>↓{d} ↑{u}</span>
                  </span>
                </button>
              </li>
            )
          })}
          {matches.length > LIMIT && (
            <li className="px-3 py-2 text-center text-xs text-muted-foreground">
              还有 {matches.length - LIMIT} 个，请用搜索/筛选缩小范围
            </li>
          )}
        </ul>
      )}
    </div>
  )
}

function MetricRow({ label, value, warn }: { label: string; value: string; warn?: boolean }) {
  return (
    <div className="flex items-center justify-between">
      <span className="text-sm text-muted-foreground">{label}</span>
      <span className={`font-mono text-sm font-medium ${warn ? "text-red-400" : ""}`}>{value}</span>
    </div>
  )
}

function NeighborList({
  title,
  edges,
  pick,
  index,
  onPick,
}: {
  title: string
  edges: ServiceEdge[]
  pick: (e: ServiceEdge) => string
  index: { byId: Map<string, ServiceNode> }
  onPick: (id: string) => void
}) {
  const seen = new Set<string>()
  const items = edges.filter((e) => {
    const id = pick(e)
    if (seen.has(id)) return false
    seen.add(id)
    return true
  })
  return (
    <div>
      <div className="mb-2 text-sm font-medium">{title}</div>
      {items.length === 0 ? (
        <p className="text-xs text-muted-foreground">无</p>
      ) : (
        <ul className="max-h-48 space-y-1 overflow-y-auto">
          {items.map((e) => {
            const id = pick(e)
            const n = index.byId.get(id)
            return (
              <li key={e.id}>
                <button
                  onClick={() => onPick(id)}
                  className="flex w-full items-center justify-between gap-2 rounded px-2 py-1 text-left text-xs hover:bg-accent"
                >
                  <span className="truncate">{n ? n.name : id}</span>
                  <span className="shrink-0 rounded bg-muted px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground">
                    {e.type}
                  </span>
                </button>
              </li>
            )
          })}
        </ul>
      )}
    </div>
  )
}
