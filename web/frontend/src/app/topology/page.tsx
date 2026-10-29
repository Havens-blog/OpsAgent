"use client"

import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import {
  AlertTriangle,
  ArrowDownUp,
  Boxes,
  CheckCircle2,
  ChevronLeft,
  ChevronRight,
  CircleGauge,
  Database,
  Inbox,
  Layers3,
  List,
  Loader2,
  Network,
  RefreshCw,
  Search,
  SearchX,
  Server,
  X,
} from "lucide-react"
import { Badge, healthVariant } from "@/components/ui/badge"
import { Card } from "@/components/ui/card"
import {
  formatErrorRate,
  formatThroughput,
  healthLabel,
  healthContrastClass,
  serviceTypeLabel,
  TopologyGraph,
  type GroupDirection,
  type GroupTone,
  type TopologyGroup,
} from "@/components/topology-graph"
import { api, type ServiceGraph, type ServiceNode } from "@/lib/api"
import { cn } from "@/lib/utils"

const KNOWN_ENVS = [
  "prod",
  "uat",
  "fat",
  "dev",
  "test",
  "pre",
  "gray",
  "sim",
  "daily",
  "stable",
]
const DIRECTORY_BATCH_SIZE = 100
const DEPENDENCY_PAGE_SIZE = 8
const TABLE_PAGE_SIZE = 8

const CONTROL_CLASS =
  "h-9 min-w-0 rounded-md border border-border bg-background px-2.5 text-xs text-foreground outline-none transition focus:border-primary focus:ring-2 focus:ring-primary/15"

type LoadStatus = "loading" | "error" | "ready"
type DirectorySort = "throughput" | "error" | "name"
type DependencySort = "anomaly" | "throughput" | "name"
type ViewMode = "aggregate" | "table"

function envOf(name: string): string {
  const index = name.indexOf("_")
  if (index > 0) {
    const prefix = name.slice(0, index).toLowerCase()
    if (KNOWN_ENVS.includes(prefix)) return prefix
  }
  return "other"
}

function isAbnormalService(node: ServiceNode): boolean {
  return (
    node.health === "degraded" ||
    node.health === "unhealthy" ||
    node.error_rate > 0.01
  )
}

function sortDependencies(nodes: ServiceNode[], sort: DependencySort): ServiceNode[] {
  return [...nodes].sort((a, b) => {
    if (sort === "name") return a.name.localeCompare(b.name)
    if (sort === "throughput") return b.throughput - a.throughput
    return (
      Number(isAbnormalService(b)) - Number(isAbnormalService(a)) ||
      b.error_rate - a.error_rate ||
      b.throughput - a.throughput
    )
  })
}

function healthDotClass(health: string): string {
  if (health === "healthy") return "bg-success"
  if (health === "degraded") return "bg-warning"
  if (health === "unhealthy") return "bg-danger"
  return "bg-tertiary"
}

function downstreamTone(type: string): GroupTone {
  if (["database", "cache", "queue", "application"].includes(type)) {
    return type as GroupTone
  }
  return "default"
}

export default function TopologyPage() {
  const [graph, setGraph] = useState<ServiceGraph | null>(null)
  const [status, setStatus] = useState<LoadStatus>("loading")
  const [loadError, setLoadError] = useState("")
  const requestIdRef = useRef(0)

  const [query, setQuery] = useState("")
  const [envFilter, setEnvFilter] = useState("all")
  const [healthFilter, setHealthFilter] = useState("all")
  const [directorySort, setDirectorySort] = useState<DirectorySort>("throughput")
  const [visibleCount, setVisibleCount] = useState(DIRECTORY_BATCH_SIZE)
  const directoryRef = useRef<HTMLDivElement>(null)

  const [focusId, setFocusId] = useState<string | null>(null)
  const [viewMode, setViewMode] = useState<ViewMode>("aggregate")
  const [selectedGroupId, setSelectedGroupId] = useState<string | null>(null)

  const [detailDirection, setDetailDirection] = useState<GroupDirection>("upstream")
  const [detailQuery, setDetailQuery] = useState("")
  const [detailSort, setDetailSort] = useState<DependencySort>("anomaly")
  const [detailType, setDetailType] = useState("all")
  const [detailHealth, setDetailHealth] = useState("all")
  const [detailPage, setDetailPage] = useState(1)

  const [tableDirection, setTableDirection] = useState<GroupDirection>("upstream")
  const [tableQuery, setTableQuery] = useState("")
  const [tableSort, setTableSort] = useState<DependencySort>("anomaly")
  const [tablePage, setTablePage] = useState(1)

  const loadTopology = useCallback(async () => {
    const requestId = ++requestIdRef.current
    setStatus("loading")
    setLoadError("")
    try {
      const response = await api.getTopology()
      if (requestId !== requestIdRef.current) return
      setGraph(response.topology)
      setFocusId(null)
      setSelectedGroupId(null)
      setStatus("ready")
    } catch (error) {
      if (requestId !== requestIdRef.current) return
      setGraph(null)
      setLoadError(error instanceof Error ? error.message : "拓扑数据加载失败")
      setStatus("error")
    }
  }, [])

  useEffect(() => {
    void loadTopology()
    return () => {
      requestIdRef.current++
    }
  }, [loadTopology])

  const index = useMemo(() => {
    const byId = new Map<string, ServiceNode>()
    const downstreamSets = new Map<string, Set<string>>()
    const upstreamSets = new Map<string, Set<string>>()
    if (graph) {
      for (const node of graph.nodes) byId.set(node.id, node)
      for (const edge of graph.edges) {
        if (!downstreamSets.has(edge.from_node_id)) {
          downstreamSets.set(edge.from_node_id, new Set())
        }
        if (!upstreamSets.has(edge.to_node_id)) {
          upstreamSets.set(edge.to_node_id, new Set())
        }
        downstreamSets.get(edge.from_node_id)!.add(edge.to_node_id)
        upstreamSets.get(edge.to_node_id)!.add(edge.from_node_id)
      }
    }
    const downstream = new Map<string, string[]>()
    const upstream = new Map<string, string[]>()
    for (const [id, ids] of downstreamSets) downstream.set(id, [...ids])
    for (const [id, ids] of upstreamSets) upstream.set(id, [...ids])
    return { byId, downstream, upstream }
  }, [graph])

  const stats = useMemo(() => {
    const result = {
      total: 0,
      applications: 0,
      healthy: 0,
      degraded: 0,
      database: 0,
      cacheAndQueue: 0,
      environments: {} as Record<string, number>,
    }
    if (!graph) return result
    result.total = graph.nodes.length
    for (const node of graph.nodes) {
      if (node.type === "application") {
        result.applications++
        if (node.health === "healthy") result.healthy++
        if (node.health === "degraded") result.degraded++
        const environment = envOf(node.name)
        result.environments[environment] = (result.environments[environment] ?? 0) + 1
      }
      if (node.type === "database") result.database++
      if (node.type === "cache" || node.type === "queue") result.cacheAndQueue++
    }
    return result
  }, [graph])

  const directoryMatches = useMemo(() => {
    if (!graph) return []
    const normalizedQuery = query.trim().toLowerCase()
    const nodes = graph.nodes.filter(
      (node) =>
        node.type === "application" &&
        (envFilter === "all" || envOf(node.name) === envFilter) &&
        (healthFilter === "all" || node.health === healthFilter) &&
        (!normalizedQuery || node.name.toLowerCase().includes(normalizedQuery)),
    )
    return nodes.sort((a, b) => {
      if (directorySort === "name") return a.name.localeCompare(b.name)
      if (directorySort === "error") return b.error_rate - a.error_rate
      return b.throughput - a.throughput
    })
  }, [graph, query, envFilter, healthFilter, directorySort])

  useEffect(() => {
    setVisibleCount(DIRECTORY_BATCH_SIZE)
    if (directoryRef.current) directoryRef.current.scrollTop = 0
  }, [query, envFilter, healthFilter, directorySort, graph])

  const focusNode = focusId ? index.byId.get(focusId) ?? null : null
  const getNeighbors = useCallback(
    (id: string | null, direction: GroupDirection): ServiceNode[] => {
      if (!id) return []
      const adjacency = direction === "upstream" ? index.upstream : index.downstream
      return (adjacency.get(id) ?? [])
        .filter((neighborId) => neighborId !== id)
        .flatMap((neighborId) => {
          const node = index.byId.get(neighborId)
          return node ? [node] : []
        })
    },
    [index],
  )

  const upstreamNodes = useMemo(
    () => getNeighbors(focusId, "upstream"),
    [focusId, getNeighbors],
  )
  const downstreamNodes = useMemo(
    () => getNeighbors(focusId, "downstream"),
    [focusId, getNeighbors],
  )

  const upstreamGroups = useMemo<TopologyGroup[]>(() => {
    const sorted = [...upstreamNodes].sort((a, b) => b.throughput - a.throughput)
    const critical = sorted.slice(0, 3)
    const remaining = sorted.slice(3)
    const abnormal = remaining.filter(isAbnormalService)
    const other = remaining.filter((node) => !isAbnormalService(node))
    const groups: TopologyGroup[] = []
    if (critical.length > 0) {
      groups.push({
        id: "upstream:critical",
        direction: "upstream",
        title: "关键上游",
        description: "按吞吐排序的核心调用方",
        tone: "critical",
        nodes: critical,
      })
    }
    if (abnormal.length > 0) {
      groups.push({
        id: "upstream:abnormal",
        direction: "upstream",
        title: "异常上游",
        description: "健康异常或错误率超过 1%",
        tone: "abnormal",
        nodes: abnormal,
      })
    }
    if (other.length > 0) {
      groups.push({
        id: "upstream:other",
        direction: "upstream",
        title: "其他应用服务",
        description: "其余全部直接调用方",
        tone: "application",
        nodes: other,
      })
    }
    return groups
  }, [upstreamNodes])

  const downstreamGroups = useMemo<TopologyGroup[]>(() => {
    const grouped = new Map<string, ServiceNode[]>()
    for (const node of downstreamNodes) {
      const nodes = grouped.get(node.type) ?? []
      nodes.push(node)
      grouped.set(node.type, nodes)
    }
    const order = ["database", "cache", "queue", "application"]
    const descriptions: Record<string, string> = {
      database: "持久化与数据存储依赖",
      cache: "缓存与高速数据访问",
      queue: "异步消息与任务分发",
      application: "直接调用的应用服务",
    }
    return [...grouped.entries()]
      .sort(([typeA], [typeB]) => {
        const indexA = order.indexOf(typeA)
        const indexB = order.indexOf(typeB)
        return (indexA < 0 ? order.length : indexA) - (indexB < 0 ? order.length : indexB) ||
          typeA.localeCompare(typeB)
      })
      .map(([type, nodes]) => ({
        id: `downstream:${type}`,
        direction: "downstream" as const,
        title: serviceTypeLabel(type),
        description: descriptions[type] ?? `${type || "未知"} 类型依赖`,
        tone: downstreamTone(type),
        nodes: [...nodes].sort((a, b) => b.throughput - a.throughput),
      }))
  }, [downstreamNodes])

  const selectedGroup = useMemo(
    () =>
      [...upstreamGroups, ...downstreamGroups].find(
        (group) => group.id === selectedGroupId,
      ) ?? null,
    [upstreamGroups, downstreamGroups, selectedGroupId],
  )

  const abnormalUpstreamCount = useMemo(
    () => upstreamNodes.filter(isAbnormalService).length,
    [upstreamNodes],
  )

  function focusService(id: string) {
    if (!index.byId.has(id)) return
    setFocusId(id)
    setSelectedGroupId(null)
    setDetailDirection("upstream")
    setDetailQuery("")
    setDetailSort("anomaly")
    setDetailType("all")
    setDetailHealth("all")
    setDetailPage(1)
    setTableDirection("upstream")
    setTableQuery("")
    setTableSort("anomaly")
    setTablePage(1)
  }

  function selectGroup(group: TopologyGroup) {
    setSelectedGroupId(group.id)
    setDetailDirection(group.direction)
    setDetailQuery("")
    setDetailType("all")
    setDetailHealth("all")
    setDetailPage(1)
  }

  function changeDetailDirection(direction: GroupDirection) {
    setDetailDirection(direction)
    setSelectedGroupId(null)
    setDetailType("all")
    setDetailHealth("all")
    setDetailPage(1)
  }

  const detailBaseNodes = detailDirection === "upstream" ? upstreamNodes : downstreamNodes
  const detailSourceNodes =
    selectedGroup?.direction === detailDirection ? selectedGroup.nodes : detailBaseNodes
  const detailTypes = useMemo(
    () => [...new Set(detailBaseNodes.map((node) => node.type))].sort(),
    [detailBaseNodes],
  )
  const detailHealthOptions = useMemo(
    () => [...new Set(detailBaseNodes.map((node) => node.health))].sort(),
    [detailBaseNodes],
  )

  const filteredDetailNodes = useMemo(() => {
    const normalizedQuery = detailQuery.trim().toLowerCase()
    const filtered = detailSourceNodes.filter(
      (node) =>
        (!normalizedQuery || node.name.toLowerCase().includes(normalizedQuery)) &&
        (detailType === "all" || node.type === detailType) &&
        (detailHealth === "all" || node.health === detailHealth),
    )
    return sortDependencies(filtered, detailSort)
  }, [detailSourceNodes, detailQuery, detailType, detailHealth, detailSort])

  const detailPageCount = Math.max(1, Math.ceil(filteredDetailNodes.length / DEPENDENCY_PAGE_SIZE))
  const safeDetailPage = Math.min(detailPage, detailPageCount)
  const pagedDetailNodes = filteredDetailNodes.slice(
    (safeDetailPage - 1) * DEPENDENCY_PAGE_SIZE,
    safeDetailPage * DEPENDENCY_PAGE_SIZE,
  )

  const tableBaseNodes = tableDirection === "upstream" ? upstreamNodes : downstreamNodes
  const filteredTableNodes = useMemo(() => {
    const normalizedQuery = tableQuery.trim().toLowerCase()
    return sortDependencies(
      tableBaseNodes.filter(
        (node) => !normalizedQuery || node.name.toLowerCase().includes(normalizedQuery),
      ),
      tableSort,
    )
  }, [tableBaseNodes, tableQuery, tableSort])
  const tablePageCount = Math.max(1, Math.ceil(filteredTableNodes.length / TABLE_PAGE_SIZE))
  const safeTablePage = Math.min(tablePage, tablePageCount)
  const pagedTableNodes = filteredTableNodes.slice(
    (safeTablePage - 1) * TABLE_PAGE_SIZE,
    safeTablePage * TABLE_PAGE_SIZE,
  )

  function onDirectoryScroll() {
    const element = directoryRef.current
    if (!element || visibleCount >= directoryMatches.length) return
    if (element.scrollHeight - element.scrollTop - element.clientHeight < 72) {
      setVisibleCount((count) => Math.min(count + DIRECTORY_BATCH_SIZE, directoryMatches.length))
    }
  }

  const remainingDirectory = Math.max(0, directoryMatches.length - visibleCount)

  return (
    <div className="min-w-0 space-y-5">
      <header>
        <h1 className="text-2xl font-bold tracking-tight text-foreground">服务拓扑</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          以核心服务为中心，快速定位异常依赖
        </p>
      </header>

      {status === "ready" && graph && (
        <section aria-label="拓扑统计" className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6">
          <StatCard label="节点总数" value={stats.total} icon={Network} iconClass="bg-primary-soft text-primary" />
          <StatCard label="应用服务" value={stats.applications} icon={Boxes} iconClass="bg-cat-application/10 text-cat-application" />
          <StatCard label="健康" value={stats.healthy} icon={CheckCircle2} iconClass="bg-success-soft text-success" />
          <StatCard label="降级" value={stats.degraded} icon={CircleGauge} iconClass="bg-warning-soft text-warning" />
          <StatCard label="数据库" value={stats.database} icon={Database} iconClass="bg-cat-database/10 text-cat-database" />
          <StatCard label="缓存队列" value={stats.cacheAndQueue} icon={Layers3} iconClass="bg-cat-cache/10 text-cat-cache" />
        </section>
      )}

      {status === "loading" && (
        <Card className="flex h-[430px] flex-col items-center justify-center gap-3 text-center">
          <span className="flex h-12 w-12 items-center justify-center rounded-2xl bg-primary-soft text-primary">
            <Loader2 className="h-6 w-6 animate-spin" />
          </span>
          <div>
            <p className="text-sm font-medium text-foreground">正在加载服务拓扑</p>
            <p className="mt-1 text-xs text-muted-foreground">正在获取 ARMS APM 真实依赖与指标…</p>
          </div>
        </Card>
      )}

      {status === "error" && (
        <Card className="flex h-[430px] flex-col items-center justify-center gap-4 px-6 text-center">
          <span className="flex h-12 w-12 items-center justify-center rounded-2xl bg-danger-soft text-danger">
            <AlertTriangle className="h-6 w-6" />
          </span>
          <div>
            <p className="text-sm font-semibold text-foreground">拓扑数据加载失败</p>
            <p className="mt-1 max-w-lg break-words text-xs text-muted-foreground">
              {loadError || "后端 /api/v1/topology 暂不可用"}
            </p>
          </div>
          <button
            type="button"
            onClick={() => void loadTopology()}
            className="inline-flex h-9 items-center gap-2 rounded-md bg-primary px-4 text-xs font-medium text-primary-foreground hover:bg-primary-hover"
          >
            <RefreshCw className="h-3.5 w-3.5" />
            重新加载
          </button>
        </Card>
      )}

      {status === "ready" && graph && (
        <div className="grid min-w-0 items-start gap-4 min-[1200px]:grid-cols-[200px_minmax(0,1fr)_280px] xl:grid-cols-[240px_minmax(0,1fr)_330px] 2xl:grid-cols-[260px_minmax(0,1fr)_350px]">
          <Card className="min-w-0 overflow-hidden">
            <div className="flex items-center justify-between gap-2 border-b border-border px-4 py-3.5">
              <div>
                <h2 className="text-sm font-semibold text-foreground">服务目录</h2>
                <p className="mt-0.5 text-[11px] text-muted-foreground">选择服务探索一跳依赖</p>
              </div>
              <span className="shrink-0 rounded-full bg-primary-soft px-2 py-1 text-[10px] font-medium text-primary">
                {directoryMatches.length} 个结果
              </span>
            </div>

            <div className="space-y-2 border-b border-border p-3">
              <label className="relative block">
                <span className="sr-only">搜索服务</span>
                <Search className="pointer-events-none absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
                <input
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
                  placeholder="搜索服务名…"
                  className={cn(CONTROL_CLASS, "w-full pl-8")}
                />
              </label>
              <div className="grid grid-cols-2 gap-2">
                <select
                  aria-label="环境筛选"
                  value={envFilter}
                  onChange={(event) => setEnvFilter(event.target.value)}
                  className={CONTROL_CLASS}
                >
                  <option value="all">全部环境</option>
                  {Object.keys(stats.environments)
                    .sort()
                    .map((environment) => (
                      <option key={environment} value={environment}>
                        {environment} ({stats.environments[environment]})
                      </option>
                    ))}
                </select>
                <select
                  aria-label="健康筛选"
                  value={healthFilter}
                  onChange={(event) => setHealthFilter(event.target.value)}
                  className={CONTROL_CLASS}
                >
                  <option value="all">全部健康</option>
                  <option value="healthy">健康</option>
                  <option value="degraded">降级</option>
                  <option value="unhealthy">异常</option>
                  <option value="unknown">无数据</option>
                </select>
              </div>
              <label className="relative block">
                <span className="sr-only">服务排序</span>
                <ArrowDownUp className="pointer-events-none absolute left-2.5 top-2.5 h-3.5 w-3.5 text-muted-foreground" />
                <select
                  value={directorySort}
                  onChange={(event) => setDirectorySort(event.target.value as DirectorySort)}
                  className={cn(CONTROL_CLASS, "w-full pl-8")}
                >
                  <option value="throughput">按吞吐排序</option>
                  <option value="error">按错误率排序</option>
                  <option value="name">按名称排序</option>
                </select>
              </label>
            </div>

            <div
              ref={directoryRef}
              onScroll={onDirectoryScroll}
              className="h-[540px] overflow-y-auto overscroll-contain"
            >
              {directoryMatches.length === 0 ? (
                <div className="flex h-full flex-col items-center justify-center px-5 text-center">
                  <SearchX className="h-7 w-7 text-muted-foreground" />
                  <p className="mt-3 text-sm font-medium text-foreground">没有匹配的服务</p>
                  <p className="mt-1 text-xs text-muted-foreground">请调整关键词或筛选条件</p>
                  <button
                    type="button"
                    onClick={() => {
                      setQuery("")
                      setEnvFilter("all")
                      setHealthFilter("all")
                    }}
                    className="mt-3 text-xs font-medium text-primary hover:underline"
                  >
                    清除搜索与筛选
                  </button>
                </div>
              ) : (
                <ul>
                  {directoryMatches.slice(0, visibleCount).map((node) => {
                    const active = node.id === focusId
                    return (
                      <li key={node.id}>
                        <button
                          type="button"
                          onClick={() => focusService(node.id)}
                          aria-current={active ? "true" : undefined}
                          className={cn(
                            "flex w-full items-center gap-2 border-b border-border/60 px-3 py-3 text-left transition-colors",
                            active
                              ? "bg-primary-soft text-foreground"
                              : "text-foreground hover:bg-accent",
                          )}
                        >
                          <span
                            className={cn("h-2 w-2 shrink-0 rounded-full", healthDotClass(node.health))}
                            title={healthLabel(node.health)}
                          />
                          <span className="min-w-0 flex-1">
                            <span className="block truncate text-xs font-medium" title={node.name}>
                              {node.name}
                            </span>
                            {node.throughput > 0 && (
                              <span className="mt-1 block font-mono text-[10px] text-muted-foreground">
                                {formatThroughput(node.throughput)} rps
                              </span>
                            )}
                          </span>
                          <ChevronRight
                            className={cn(
                              "h-3.5 w-3.5 shrink-0",
                              active ? "text-primary" : "text-muted-foreground",
                            )}
                          />
                        </button>
                      </li>
                    )
                  })}
                </ul>
              )}
            </div>
            <div className="border-t border-border px-3 py-2.5 text-center text-[10px] text-muted-foreground">
              {remainingDirectory > 0
                ? `剩余 ${remainingDirectory} 个 · 滑动加载或搜索查看更多`
                : directoryMatches.length > DIRECTORY_BATCH_SIZE
                  ? `已加载全部 ${directoryMatches.length} 个服务`
                  : `共 ${directoryMatches.length} 个服务`}
            </div>
          </Card>

          <section className="min-w-0 space-y-3" aria-label="依赖工作台">
            <div className="grid grid-cols-2 rounded-xl border border-border bg-card p-1.5" role="tablist" aria-label="拓扑视图模式">
              <button
                type="button"
                role="tab"
                aria-selected={viewMode === "aggregate"}
                onClick={() => setViewMode("aggregate")}
                className={cn(
                  "flex h-9 items-center justify-center gap-2 rounded-lg text-xs font-medium transition",
                  viewMode === "aggregate"
                    ? "bg-primary text-primary-foreground shadow-sm"
                    : "text-muted-foreground hover:bg-accent hover:text-foreground",
                )}
              >
                <Network className="h-4 w-4" />聚合视图
              </button>
              <button
                type="button"
                role="tab"
                aria-selected={viewMode === "table"}
                onClick={() => setViewMode("table")}
                className={cn(
                  "flex h-9 items-center justify-center gap-2 rounded-lg text-xs font-medium transition",
                  viewMode === "table"
                    ? "bg-primary text-primary-foreground shadow-sm"
                    : "text-muted-foreground hover:bg-accent hover:text-foreground",
                )}
              >
                <List className="h-4 w-4" />明细列表
              </button>
            </div>

            {!focusNode ? (
              <FocusEmptyState />
            ) : viewMode === "aggregate" ? (
              <TopologyGraph
                focusNode={focusNode}
                upstreamGroups={upstreamGroups}
                downstreamGroups={downstreamGroups}
                selectedGroupId={selectedGroupId}
                onSelectGroup={selectGroup}
              />
            ) : (
              <Card className="min-w-0 overflow-hidden">
                <div className="border-b border-border px-4 py-3.5">
                  <h2 className="text-sm font-semibold text-foreground">依赖明细</h2>
                  <p className="mt-0.5 truncate text-xs text-muted-foreground" title={focusNode.name}>
                    {focusNode.name} · 完整一跳邻域
                  </p>
                </div>
                <div className="space-y-3 p-3">
                  <DirectionTabs
                    direction={tableDirection}
                    upstreamCount={upstreamNodes.length}
                    downstreamCount={downstreamNodes.length}
                    onChange={(direction) => {
                      setTableDirection(direction)
                      setTablePage(1)
                    }}
                  />
                  <div className="grid grid-cols-[minmax(0,1fr)_112px] gap-2">
                    <label className="relative block">
                      <span className="sr-only">搜索依赖明细</span>
                      <Search className="pointer-events-none absolute left-2.5 top-2.5 h-3.5 w-3.5 text-muted-foreground" />
                      <input
                        value={tableQuery}
                        onChange={(event) => {
                          setTableQuery(event.target.value)
                          setTablePage(1)
                        }}
                        placeholder="搜索当前依赖…"
                        className={cn(CONTROL_CLASS, "w-full pl-8")}
                      />
                    </label>
                    <select
                      aria-label="依赖明细排序"
                      value={tableSort}
                      onChange={(event) => {
                        setTableSort(event.target.value as DependencySort)
                        setTablePage(1)
                      }}
                      className={CONTROL_CLASS}
                    >
                      <option value="anomaly">异常优先</option>
                      <option value="throughput">吞吐优先</option>
                      <option value="name">名称排序</option>
                    </select>
                  </div>

                  <div className="overflow-hidden rounded-lg border border-border">
                    <table className="w-full table-fixed text-left text-xs">
                      <thead className="bg-muted/70 text-muted-foreground">
                        <tr>
                          <th className="w-[34%] px-2.5 py-2.5 font-medium">服务名</th>
                          <th className="w-[16%] px-2 py-2.5 font-medium">类型</th>
                          <th className="w-[14%] px-2 py-2.5 font-medium">健康</th>
                          <th className="w-[18%] px-2 py-2.5 text-right font-medium">吞吐</th>
                          <th className="w-[18%] px-2 py-2.5 text-right font-medium">错误率</th>
                        </tr>
                      </thead>
                      <tbody className="divide-y divide-border bg-card">
                        {pagedTableNodes.map((node) => (
                          <tr
                            key={node.id}
                            onClick={() => focusService(node.id)}
                            className="cursor-pointer transition-colors hover:bg-accent"
                          >
                            <td className="px-2.5 py-3">
                              <button
                                type="button"
                                onClick={(event) => {
                                  event.stopPropagation()
                                  focusService(node.id)
                                }}
                                className="flex w-full min-w-0 items-center gap-1.5 text-left"
                                title={node.name}
                              >
                                <span className={cn("h-1.5 w-1.5 shrink-0 rounded-full", healthDotClass(node.health))} />
                                <span className="truncate font-medium text-foreground">{node.name}</span>
                              </button>
                            </td>
                            <td className="truncate px-2 py-3 text-muted-foreground" title={serviceTypeLabel(node.type)}>
                              {serviceTypeLabel(node.type)}
                            </td>
                            <td className="px-2 py-3 text-muted-foreground">{healthLabel(node.health)}</td>
                            <td className="px-2 py-3 text-right font-mono text-[10px] text-foreground">
                              {formatThroughput(node.throughput)}
                              <span className="ml-0.5 text-muted-foreground">rps</span>
                            </td>
                            <td
                              className={cn(
                                "px-2 py-3 text-right font-mono text-[10px]",
                                node.error_rate > 0.01 ? "text-danger" : "text-foreground",
                              )}
                            >
                              {formatErrorRate(node.error_rate)}
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                    {pagedTableNodes.length === 0 && (
                      <EmptyDependencyState
                        hasDependencies={tableBaseNodes.length > 0}
                        direction={tableDirection}
                      />
                    )}
                  </div>

                  <Pagination
                    page={safeTablePage}
                    pageCount={tablePageCount}
                    total={filteredTableNodes.length}
                    pageSize={TABLE_PAGE_SIZE}
                    onChange={setTablePage}
                  />
                </div>
              </Card>
            )}
          </section>

          <Card className="min-w-0 overflow-hidden">
            {!focusNode ? (
              <div className="flex min-h-[360px] flex-col items-center justify-center px-7 text-center">
                <span className="flex h-12 w-12 items-center justify-center rounded-2xl bg-primary-soft text-primary">
                  <Server className="h-6 w-6" />
                </span>
                <h2 className="mt-4 text-sm font-semibold text-foreground">服务详情</h2>
                <p className="mt-2 text-xs leading-5 text-muted-foreground">
                  聚焦服务后，这里会展示 RED 指标与完整上下游清单
                </p>
              </div>
            ) : (
              <>
                <div className="border-b border-border p-4">
                  <h2 className="text-sm font-semibold text-foreground">服务详情</h2>
                  <p className="mt-2 break-words text-sm font-medium leading-5 text-foreground [overflow-wrap:anywhere]">
                    {focusNode.name}
                  </p>
                  <div className="mt-2.5 flex flex-wrap gap-1.5">
                    <Badge
                      variant={healthVariant(focusNode.health)}
                      className={healthContrastClass(focusNode.health)}
                    >
                      {healthLabel(focusNode.health)}
                    </Badge>
                    <Badge variant="secondary">
                      {focusNode.type === "application"
                        ? envOf(focusNode.name)
                        : serviceTypeLabel(focusNode.type)}
                    </Badge>
                  </div>
                </div>

                <div className="border-b border-border p-4">
                  <div className="grid grid-cols-3 gap-2">
                    <MetricBlock
                      label="吞吐"
                      value={focusNode.throughput > 0 ? `${formatThroughput(focusNode.throughput)} rps` : "—"}
                    />
                    <MetricBlock
                      label="错误率"
                      value={focusNode.throughput > 0 ? formatErrorRate(focusNode.error_rate) : "—"}
                      danger={focusNode.error_rate > 0.01}
                    />
                    <MetricBlock
                      label="平均延迟"
                      value={focusNode.throughput > 0 ? `${focusNode.latency.toFixed(1)} ms` : "—"}
                    />
                  </div>
                  <p className="mt-2.5 text-[10px] text-muted-foreground">近5分钟 · 来自 ARMS APM</p>
                  {focusNode.throughput <= 0 && (
                    <p className="mt-1.5 text-[10px] text-muted-foreground">近5分钟无流量，指标暂无采样</p>
                  )}
                </div>

                {abnormalUpstreamCount > 0 && (
                  <div className="m-3 mb-0 flex items-start gap-2 rounded-lg border border-warning-border bg-warning-soft p-3 text-xs leading-5 text-warning">
                    <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" />
                    发现 {abnormalUpstreamCount} 个异常上游，建议优先排查
                  </div>
                )}

                <div className="p-3">
                  <DirectionTabs
                    direction={detailDirection}
                    upstreamCount={upstreamNodes.length}
                    downstreamCount={downstreamNodes.length}
                    onChange={changeDetailDirection}
                  />

                  {selectedGroup?.direction === detailDirection && (
                    <div className="mt-3 flex items-center justify-between gap-2 rounded-md border border-primary-border bg-primary-soft px-2.5 py-2 text-[11px] text-foreground">
                      <span className="truncate">分组：{selectedGroup.title} · {selectedGroup.nodes.length}</span>
                      <button
                        type="button"
                        aria-label="清除分组筛选"
                        title="清除分组筛选"
                        onClick={() => setSelectedGroupId(null)}
                        className="shrink-0 rounded p-0.5 hover:bg-primary/15"
                      >
                        <X className="h-3.5 w-3.5" />
                      </button>
                    </div>
                  )}

                  <div className="mt-3 grid grid-cols-[minmax(0,1fr)_108px] gap-2">
                    <label className="relative block">
                      <span className="sr-only">搜索完整依赖清单</span>
                      <Search className="pointer-events-none absolute left-2.5 top-2.5 h-3.5 w-3.5 text-muted-foreground" />
                      <input
                        value={detailQuery}
                        onChange={(event) => {
                          setDetailQuery(event.target.value)
                          setDetailPage(1)
                        }}
                        placeholder="搜索依赖…"
                        className={cn(CONTROL_CLASS, "w-full pl-8")}
                      />
                    </label>
                    <select
                      aria-label="完整依赖排序"
                      value={detailSort}
                      onChange={(event) => {
                        setDetailSort(event.target.value as DependencySort)
                        setDetailPage(1)
                      }}
                      className={CONTROL_CLASS}
                    >
                      <option value="anomaly">异常优先</option>
                      <option value="throughput">吞吐优先</option>
                      <option value="name">名称排序</option>
                    </select>
                  </div>
                  <div className="mt-2 grid grid-cols-2 gap-2">
                    <select
                      aria-label="依赖类型筛选"
                      value={detailType}
                      onChange={(event) => {
                        setDetailType(event.target.value)
                        setDetailPage(1)
                      }}
                      className={CONTROL_CLASS}
                    >
                      <option value="all">全部类型</option>
                      {detailTypes.map((type) => (
                        <option key={type} value={type}>{serviceTypeLabel(type)}</option>
                      ))}
                    </select>
                    <select
                      aria-label="依赖健康筛选"
                      value={detailHealth}
                      onChange={(event) => {
                        setDetailHealth(event.target.value)
                        setDetailPage(1)
                      }}
                      className={CONTROL_CLASS}
                    >
                      <option value="all">全部健康</option>
                      {detailHealthOptions.map((health) => (
                        <option key={health} value={health}>{healthLabel(health)}</option>
                      ))}
                    </select>
                  </div>
                </div>

                <div className="border-t border-border">
                  {pagedDetailNodes.length > 0 ? (
                    <ul className="divide-y divide-border">
                      {pagedDetailNodes.map((node) => (
                        <li key={node.id}>
                          <button
                            type="button"
                            onClick={() => focusService(node.id)}
                            className="flex w-full items-center gap-2.5 px-3 py-3 text-left transition-colors hover:bg-accent"
                          >
                            <span className={cn("mt-1 h-2 w-2 shrink-0 self-start rounded-full", healthDotClass(node.health))} />
                            <span className="min-w-0 flex-1">
                              <span className="block truncate text-xs font-medium text-foreground" title={node.name}>
                                {node.name}
                              </span>
                              <span className="mt-1 block text-[10px] text-muted-foreground">
                                {serviceTypeLabel(node.type)}
                              </span>
                            </span>
                            <span className="shrink-0 text-right">
                              <span className="block font-mono text-[10px] text-foreground">
                                {formatThroughput(node.throughput)} rps
                              </span>
                              <span
                                className={cn(
                                  "mt-1 block font-mono text-[10px]",
                                  node.error_rate > 0.01 ? "text-danger" : "text-muted-foreground",
                                )}
                              >
                                {formatErrorRate(node.error_rate)}
                              </span>
                            </span>
                          </button>
                        </li>
                      ))}
                    </ul>
                  ) : (
                    <EmptyDependencyState
                      hasDependencies={detailSourceNodes.length > 0}
                      direction={detailDirection}
                    />
                  )}
                </div>

                <div className="border-t border-border px-3 py-3">
                  <Pagination
                    page={safeDetailPage}
                    pageCount={detailPageCount}
                    total={filteredDetailNodes.length}
                    pageSize={DEPENDENCY_PAGE_SIZE}
                    onChange={setDetailPage}
                  />
                </div>
              </>
            )}
          </Card>
        </div>
      )}
    </div>
  )
}

function StatCard({
  label,
  value,
  icon: Icon,
  iconClass,
}: {
  label: string
  value: number
  icon: React.ComponentType<{ className?: string }>
  iconClass: string
}) {
  return (
    <div className="flex min-w-0 items-center gap-3 rounded-xl border border-border bg-card p-3 shadow-sm">
      <span className={cn("flex h-9 w-9 shrink-0 items-center justify-center rounded-lg", iconClass)}>
        <Icon className="h-4 w-4" />
      </span>
      <div className="min-w-0">
        <p className="truncate text-[11px] text-muted-foreground">{label}</p>
        <p className="mt-0.5 text-xl font-semibold leading-none text-foreground">{value}</p>
      </div>
    </div>
  )
}

function FocusEmptyState() {
  return (
    <Card className="relative flex h-[560px] min-w-0 flex-col items-center justify-center overflow-hidden px-6 text-center">
      <div className="absolute h-64 w-64 rounded-full border border-primary/10" />
      <div className="absolute h-44 w-44 rounded-full border border-dashed border-primary/20" />
      <span className="relative flex h-16 w-16 items-center justify-center rounded-2xl border border-primary-border bg-primary-soft text-primary shadow-md">
        <Network className="h-8 w-8" />
      </span>
      <h2 className="relative mt-5 text-base font-semibold text-foreground">选择一个核心服务</h2>
      <p className="relative mt-2 max-w-sm text-xs leading-5 text-muted-foreground">
        从服务目录聚焦任意应用，即可查看聚合依赖、完整明细与 ARMS RED 指标
      </p>
    </Card>
  )
}

function DirectionTabs({
  direction,
  upstreamCount,
  downstreamCount,
  onChange,
}: {
  direction: GroupDirection
  upstreamCount: number
  downstreamCount: number
  onChange: (direction: GroupDirection) => void
}) {
  return (
    <div className="grid grid-cols-2 rounded-lg bg-muted p-1" role="tablist" aria-label="依赖方向">
      {(["upstream", "downstream"] as const).map((item) => (
        <button
          key={item}
          type="button"
          role="tab"
          aria-selected={direction === item}
          onClick={() => onChange(item)}
          className={cn(
            "h-8 rounded-md text-xs font-medium transition",
            direction === item
              ? "bg-card text-foreground shadow-sm"
              : "text-muted-foreground hover:text-foreground",
          )}
        >
          {item === "upstream" ? `上游 ${upstreamCount}` : `下游 ${downstreamCount}`}
        </button>
      ))}
    </div>
  )
}

function MetricBlock({ label, value, danger = false }: { label: string; value: string; danger?: boolean }) {
  return (
    <div className="min-w-0 rounded-lg border border-border/70 bg-background p-2.5">
      <p className="text-[10px] text-muted-foreground">{label}</p>
      <p
        className={cn(
          "mt-1.5 truncate font-mono text-[11px] font-semibold",
          danger ? "text-danger" : "text-foreground",
        )}
        title={value}
      >
        {value}
      </p>
    </div>
  )
}

function EmptyDependencyState({
  hasDependencies,
  direction,
}: {
  hasDependencies: boolean
  direction: GroupDirection
}) {
  return (
    <div className="flex min-h-36 flex-col items-center justify-center px-4 py-7 text-center">
      <Inbox className="h-6 w-6 text-muted-foreground" />
      <p className="mt-2 text-xs font-medium text-foreground">
        {hasDependencies ? "没有匹配的依赖" : `无直接${direction === "upstream" ? "上游" : "下游"}`}
      </p>
      <p className="mt-1 text-[10px] text-muted-foreground">
        {hasDependencies ? "请调整搜索或筛选条件" : "当前服务在这一方向没有一跳依赖"}
      </p>
    </div>
  )
}

function Pagination({
  page,
  pageCount,
  total,
  pageSize,
  onChange,
}: {
  page: number
  pageCount: number
  total: number
  pageSize: number
  onChange: (page: number) => void
}) {
  if (total === 0) return null
  let start = Math.max(1, Math.min(page - 2, pageCount - 4))
  if (pageCount <= 5) start = 1
  const end = Math.min(pageCount, start + 4)
  const pages = Array.from({ length: end - start + 1 }, (_, index) => start + index)
  const first = (page - 1) * pageSize + 1
  const last = Math.min(total, page * pageSize)

  return (
    <div className="flex flex-wrap items-center justify-between gap-2">
      <span className="text-[10px] text-muted-foreground">{first}-{last} / 共 {total} 个</span>
      <div className="flex items-center gap-1">
        <button
          type="button"
          aria-label="上一页"
          title="上一页"
          disabled={page <= 1}
          onClick={() => onChange(page - 1)}
          className="flex h-7 w-7 items-center justify-center rounded-md border border-border text-foreground hover:bg-accent disabled:cursor-not-allowed disabled:opacity-40"
        >
          <ChevronLeft className="h-3.5 w-3.5" />
        </button>
        {pages.map((pageNumber) => (
          <button
            key={pageNumber}
            type="button"
            aria-label={`第 ${pageNumber} 页`}
            aria-current={pageNumber === page ? "page" : undefined}
            onClick={() => onChange(pageNumber)}
            className={cn(
              "h-7 min-w-7 rounded-md border px-1 font-mono text-[10px]",
              pageNumber === page
                ? "border-primary bg-primary text-primary-foreground"
                : "border-border text-foreground hover:bg-accent",
            )}
          >
            {pageNumber}
          </button>
        ))}
        <button
          type="button"
          aria-label="下一页"
          title="下一页"
          disabled={page >= pageCount}
          onClick={() => onChange(page + 1)}
          className="flex h-7 w-7 items-center justify-center rounded-md border border-border text-foreground hover:bg-accent disabled:cursor-not-allowed disabled:opacity-40"
        >
          <ChevronRight className="h-3.5 w-3.5" />
        </button>
      </div>
    </div>
  )
}
