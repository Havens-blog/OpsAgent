"use client"

import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { useRouter } from "next/navigation"
import {
  AlertTriangle,
  Activity,
  Check,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  ChevronUp,
  Circle,
  CircleAlert,
  Copy,
  Download,
  GitBranch,
  Info,
  Inbox,
  Loader2,
  RefreshCw,
  RotateCcw,
  Search,
  Settings,
  TrendingUp,
  X,
} from "lucide-react"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  api,
  type ChangeItem,
  type ChangesQuery,
  type ChangesResponse,
  type ChangesStats,
} from "@/lib/api"
import { cn } from "@/lib/utils"

type LoadStatus = "loading" | "error" | "ready"
type Order = "desc" | "asc"

const WINDOWS = [
  { hours: 1, label: "最近1小时", short: "1小时" },
  { hours: 6, label: "最近6小时", short: "6小时" },
  { hours: 24, label: "最近24小时", short: "24小时" },
  { hours: 72, label: "最近3天", short: "3天" },
  { hours: 168, label: "最近7天", short: "7天" },
]

const PAGE_SIZES = [20, 50, 100]

/** Export caps: the backend page size is 200, so a full export loops offsets. */
const EXPORT_PAGE_SIZE = 200
const EXPORT_MAX_ROWS = 2000

const KIND_LABELS: Record<string, string> = {
  deployment: "部署",
  config: "配置",
  scale: "扩缩容",
  k8s_event: "K8s 事件",
}

/** Type-tab ordering; unknown kinds are appended alphabetically. */
const KIND_ORDER = ["deployment", "config", "scale", "k8s_event"]

const KIND_BADGE: Record<string, string> = {
  deployment:
    "border-indigo-200 bg-indigo-50 text-indigo-700 dark:border-indigo-500/30 dark:bg-indigo-500/15 dark:text-indigo-300",
  config:
    "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-500/30 dark:bg-emerald-500/15 dark:text-emerald-300",
  scale:
    "border-orange-200 bg-orange-50 text-orange-700 dark:border-orange-500/30 dark:bg-orange-500/15 dark:text-orange-300",
  k8s_event:
    "border-sky-200 bg-sky-50 text-sky-700 dark:border-sky-500/30 dark:bg-sky-500/15 dark:text-sky-300",
}

// Curated Chinese labels for well-known Kubernetes event reasons. Display-only
// convenience: the raw reason is always shown next to it, and unknown reasons
// fall back to the raw text — nothing is inferred about root cause.
const REASON_LABELS: Record<string, string> = {
  // 探针 / 容器状态
  Unhealthy: "就绪探针检查失败",
  BackOff: "容器反复重启",
  CrashLoopBackOff: "容器崩溃循环",
  Killing: "容器正在终止",
  Started: "容器已启动",
  Created: "资源已创建",
  Failed: "执行失败",
  FailedCreate: "创建失败",
  Preempting: "发生抢占",
  Evicted: "Pod 被驱逐",
  OOMKilling: "内存超限被终止",
  // 镜像
  FailedToRetrieveImagePullSecret: "镜像拉取凭据获取失败",
  Pulling: "正在拉取镜像",
  Pulled: "镜像拉取完成",
  ErrImagePull: "镜像拉取失败",
  ImagePullBackOff: "镜像拉取重试中",
  ContainerCreating: "容器创建中",
  // 调度 / 节点
  Scheduled: "调度成功",
  FailedScheduling: "调度失败",
  NodeNotReady: "节点未就绪",
  NodeReady: "节点已就绪",
  RegisteredNode: "节点注册成功",
  NodeAllocatableEnforced: "节点可分配资源已更新",
  PolicyViolation: "策略校验失败",
  Info: "一般事件",
  // 弹性伸缩
  SuccessfulRescale: "扩缩容完成",
  ScalingReplicaSet: "扩缩副本集",
  FailedGetScale: "未找到扩缩容目标",
  FailedGetResourceMetric: "资源指标获取失败",
  FailedComputeMetricsReplicas: "副本指标计算失败",
  // 存储
  FailedMount: "存储卷挂载失败",
  FailedAttachVolume: "存储卷挂载失败",
  SuccessfulAttachVolume: "存储卷挂载成功",
  Provisioning: "存储卷供应中",
  ExternalProvisioning: "外部存储卷供应中",
  VolumeResizeSuccessful: "存储卷扩容成功",
  // 负载均衡 / 其他
  SyncLoadBalancerFailed: "负载均衡器同步失败",
  StartReconcileALBIngress: "开始调和 ALB Ingress",
  ReconcileALBIngressSuccessfully: "ALB Ingress 调和成功",
  StartReconcileALBInstance: "开始调和 ALB 实例",
  ReconcileALBInstanceFailed: "ALB 实例调和失败",
  AllocIPSucceed: "IP 分配成功",
  RequestSuccessfully: "请求成功",
  SuccessfulCreate: "创建成功",
  SuccessfulDelete: "删除成功",
  Upgrade: "组件升级",
}

function reasonLabel(reason?: string): string {
  if (!reason) return ""
  return REASON_LABELS[reason] ?? reason
}

function kindLabel(kind: string): string {
  return KIND_LABELS[kind] ?? kind ?? "—"
}

function kindIcon(kind: string) {
  if (kind === "deployment") return GitBranch
  if (kind === "config") return Settings
  if (kind === "scale") return TrendingUp
  return Activity
}

function orderedKinds(counts: Record<string, number> | undefined): [string, number][] {
  const entries = Object.entries(counts ?? {}).filter(([, count]) => count > 0)
  return entries.sort((a, b) => {
    const ia = KIND_ORDER.indexOf(a[0])
    const ib = KIND_ORDER.indexOf(b[0])
    const ra = ia === -1 ? KIND_ORDER.length : ia
    const rb = ib === -1 ? KIND_ORDER.length : ib
    if (ra !== rb) return ra - rb
    return a[0].localeCompare(b[0])
  })
}

/** Object-kind chips: most frequent first, ties broken alphabetically. */
function countsByDescending(counts: Record<string, number> | undefined): [string, number][] {
  return Object.entries(counts ?? {})
    .filter(([, count]) => count > 0)
    .sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
}

/** Preview size before the object-kind chips collapse behind 「展开更多」. */
const OBJECT_KIND_PREVIEW = 8

/** Cap on the reason dropdown entries (the long tail stays reachable via search). */
const REASON_OPTION_LIMIT = 20

/** Level dropdown labels; every level the backend did not name reads as 未知. */
function levelFilterLabel(level: string): string {
  if (level === "warning") return "警告"
  if (level === "info") return "信息"
  if (level === "error") return "错误"
  return "未知"
}

/** Sort key (YYYY-MM-DD in local time) for date grouping. */
function dayKey(date: Date): string {
  return `${date.getFullYear()}-${date.getMonth() + 1}-${date.getDate()}`
}

/** 「今天 · 10月28日」/「昨天 · 10月27日」/「10月26日」 */
function dayLabel(date: Date, today: Date): string {
  const monthDay = `${date.getMonth() + 1}月${date.getDate()}日`
  const stamp = dayKey(date)
  if (stamp === dayKey(today)) return `今天 · ${monthDay}`
  const yesterday = new Date(today.getTime() - 24 * 60 * 60 * 1000)
  if (stamp === dayKey(yesterday)) return `昨天 · ${monthDay}`
  if (date.getFullYear() !== today.getFullYear()) {
    return `${date.getFullYear()}年${monthDay}`
  }
  return monthDay
}

interface DayGroup {
  key: string
  label: string
  items: ChangeItem[]
}

/** Groups the current page by local calendar day (server order is preserved). */
function groupByDay(items: ChangeItem[]): DayGroup[] {
  const today = new Date()
  const groups: DayGroup[] = []
  const byKey = new Map<string, DayGroup>()
  for (const item of items) {
    const date = new Date(item.time)
    if (Number.isNaN(date.getTime())) {
      const fallback = byKey.get("unknown") ?? { key: "unknown", label: "时间未知", items: [] }
      if (!byKey.has("unknown")) {
        byKey.set("unknown", fallback)
        groups.push(fallback)
      }
      fallback.items.push(item)
      continue
    }
    const key = dayKey(date)
    let group = byKey.get(key)
    if (!group) {
      group = { key, label: dayLabel(date, today), items: [] }
      byKey.set(key, group)
      groups.push(group)
    }
    group.items.push(item)
  }
  return groups
}

function fmtClock(iso?: string): string {
  if (!iso) return "--:--:--"
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return "--:--:--"
  return date.toLocaleTimeString("zh-CN", { hour12: false })
}

function fmtDateTime(iso?: string): string {
  if (!iso) return "—"
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return iso
  return date.toLocaleString("zh-CN", { hour12: false })
}

/** Severity icon: warning = orange, info = blue, anything else = neutral. */
function LevelIcon({ level, className }: { level?: string; className?: string }) {
  const normalized = (level ?? "").toLowerCase()
  if (normalized === "warning") {
    return (
      <CircleAlert className={cn("h-4 w-4 text-orange-500", className)} />
    )
  }
  if (normalized === "info") {
    return <Info className={cn("h-4 w-4 text-sky-500", className)} />
  }
  return <Circle className={cn("h-4 w-4 text-muted-foreground", className)} />
}

function levelLabel(level?: string): string {
  const normalized = (level ?? "").toLowerCase()
  if (normalized === "warning") return "警告"
  if (normalized === "info") return "信息"
  if (!normalized) return "级别未知"
  return level as string
}

function resourceName(item: ChangeItem): string {
  return item.object || item.service || "—"
}

function paginationItems(current: number, count: number): (number | "gap")[] {
  if (count <= 7) return Array.from({ length: count }, (_, i) => i + 1)
  const items: (number | "gap")[] = [1]
  const start = Math.max(2, current - 1)
  const end = Math.min(count - 1, current + 1)
  if (start > 2) items.push("gap")
  for (let page = start; page <= end; page++) items.push(page)
  if (end < count - 1) items.push("gap")
  items.push(count)
  return items
}

/** JSON summary of one record — real fields only, no derived conclusions. */
function recordSummary(item: ChangeItem): string {
  return JSON.stringify(
    {
      id: item.id,
      time: item.time,
      kind: item.kind,
      system: item.system,
      namespace: item.namespace ?? "",
      resource: item.object || item.service || "",
      object_kind: item.object_kind ?? "",
      reason: item.reason ?? "",
      level: item.level ?? "",
      cluster: item.cluster ?? "",
      service: item.service ?? "",
      occurrences: item.occurrences ?? 1,
      first_seen: item.first_seen ?? item.time,
      last_seen: item.last_seen ?? item.time,
      message: item.message,
      details: item.details ?? {},
    },
    null,
    2
  )
}

function csvCell(value: string): string {
  const text = value.replace(/\r?\n/g, " ")
  return `"${text.replace(/"/g, '""')}"`
}

function Switch({
  checked,
  onToggle,
  label,
}: {
  checked: boolean
  onToggle: (next: boolean) => void
  label: string
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      onClick={() => onToggle(!checked)}
      className={cn(
        "inline-flex items-center gap-2 rounded-md px-1.5 py-1 text-xs transition-colors",
        checked ? "text-foreground" : "text-muted-foreground",
        "hover:bg-accent"
      )}
    >
      <span
        className={cn(
          "relative inline-flex h-4 w-7 shrink-0 items-center rounded-full transition-colors",
          checked ? "bg-primary" : "bg-muted-foreground/30"
        )}
      >
        <span
          className={cn(
            "inline-block h-3 w-3 rounded-full bg-white shadow transition-transform",
            checked ? "translate-x-3.5" : "translate-x-0.5"
          )}
        />
      </span>
      {label}
    </button>
  )
}

export default function ChangesPage() {
  const router = useRouter()

  const [hours, setHours] = useState(24)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(PAGE_SIZES[0])
  const [kind, setKind] = useState("")
  const [system, setSystem] = useState("")
  const [namespace, setNamespace] = useState("")
  const [objectKind, setObjectKind] = useState("")
  const [reason, setReason] = useState("")
  const [level, setLevel] = useState("")
  const [objectKindExpanded, setObjectKindExpanded] = useState(false)
  const [searchText, setSearchText] = useState("")
  const [searchQuery, setSearchQuery] = useState("")
  const [order, setOrder] = useState<Order>("desc")
  const [dedupe, setDedupe] = useState(false)

  const [data, setData] = useState<ChangesResponse | null>(null)
  const [status, setStatus] = useState<LoadStatus>("loading")
  const [loadError, setLoadError] = useState("")
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [copiedField, setCopiedField] = useState("")
  const [exportState, setExportState] = useState<{
    status: "idle" | "busy" | "done" | "error"
    message: string
  }>({ status: "idle", message: "" })

  const requestIdRef = useRef(0)
  const copyTimerRef = useRef<number | null>(null)

  // Debounce the search box so typing does not fire a request per keystroke.
  useEffect(() => {
    const trimmed = searchText.trim()
    if (trimmed === searchQuery) return
    const timer = window.setTimeout(() => {
      setSearchQuery(trimmed)
      setPage(1)
    }, 300)
    return () => window.clearTimeout(timer)
  }, [searchText, searchQuery])

  useEffect(() => {
    return () => {
      if (copyTimerRef.current !== null) window.clearTimeout(copyTimerRef.current)
    }
  }, [])

  const loadChanges = useCallback(async () => {
    const requestId = ++requestIdRef.current
    setStatus("loading")
    setLoadError("")
    try {
      const response = await api.getChanges({
        hours,
        limit: pageSize,
        offset: (page - 1) * pageSize,
        kind: kind || undefined,
        system: system || undefined,
        namespace: namespace || undefined,
        object_kind: objectKind || undefined,
        reason: reason || undefined,
        level: level || undefined,
        q: searchQuery || undefined,
        order,
        dedupe,
      })
      if (requestId !== requestIdRef.current) return
      setData(response)
      setStatus("ready")
    } catch (error) {
      if (requestId !== requestIdRef.current) return
      setData(null)
      setLoadError(error instanceof Error ? error.message : "变更记录加载失败")
      setStatus("error")
    }
  }, [
    hours,
    page,
    pageSize,
    kind,
    system,
    namespace,
    objectKind,
    reason,
    level,
    searchQuery,
    order,
    dedupe,
  ])

  useEffect(() => {
    void loadChanges()
    return () => {
      requestIdRef.current++
    }
  }, [loadChanges])

  const changes = useMemo(() => data?.changes ?? [], [data])
  const stats: ChangesStats | null = data?.stats ?? null
  const sourceReal = data?.source.real ?? false
  const total = data?.total ?? 0
  const hasMore = data?.has_more ?? false
  const pageCount = Math.max(1, Math.ceil(total / pageSize))

  // Keep the pager inside the valid range when a filter shrinks the result set.
  useEffect(() => {
    if (page > pageCount) setPage(pageCount)
  }, [page, pageCount])

  useEffect(() => {
    if (selectedId && !changes.some((item) => item.id === selectedId)) {
      setSelectedId(null)
    }
  }, [changes, selectedId])

  const selected = useMemo(
    () => changes.find((item) => item.id === selectedId) ?? null,
    [changes, selectedId]
  )

  const windowMeta = WINDOWS.find((w) => w.hours === hours) ?? WINDOWS[2]
  const days = useMemo(() => groupByDay(changes), [changes])
  const kindRows = useMemo(() => orderedKinds(stats?.by_kind), [stats])
  const objectKindRows = useMemo(
    () => countsByDescending(stats?.by_object_kind),
    [stats]
  )
  const reasonRows = useMemo(
    () => countsByDescending(stats?.by_reason).slice(0, REASON_OPTION_LIMIT),
    [stats]
  )
  const levelRows = useMemo(
    () => countsByDescending(stats?.level_counts),
    [stats]
  )

  // Second-level tabs only make sense for the event kinds that actually carry
  // an object type; deployment/config/scale rows would be an empty line.
  const showObjectKinds =
    (kind === "" || kind === "k8s_event") &&
    (objectKindRows.length > 0 || objectKind !== "")

  const visibleObjectKinds = useMemo(() => {
    if (objectKindExpanded || objectKindRows.length <= OBJECT_KIND_PREVIEW) {
      return objectKindRows
    }
    return objectKindRows.slice(0, OBJECT_KIND_PREVIEW)
  }, [objectKindExpanded, objectKindRows])

  // A selection beyond the collapsed preview has no chip on screen, so it is
  // called out in text — the row never hides an active filter silently.
  const hiddenObjectKind =
    objectKind !== "" &&
    !visibleObjectKinds.some(([name]) => name === objectKind)

  // A selected reason may sit outside the top-N list (e.g. after a state
  // restore); it stays selectable instead of rendering a blank control.
  const reasonOptions = useMemo(() => {
    const options = [...reasonRows]
    if (reason && !options.some(([name]) => name === reason)) {
      options.push([reason, stats?.by_reason?.[reason] ?? 0])
    }
    return options
  }, [reasonRows, reason, stats])

  const levelOptions = useMemo(() => {
    const options = [...levelRows]
    if (level && !options.some(([name]) => name === level)) {
      options.push([level, stats?.level_counts?.[level] ?? 0])
    }
    return options
  }, [levelRows, level, stats])

  const dominantKind = kindRows[0]
  const dominantShare =
    dominantKind && stats && stats.total > 0
      ? Math.round((dominantKind[1] / stats.total) * 100)
      : 0

  const activeFilterCount =
    (kind ? 1 : 0) +
    (system ? 1 : 0) +
    (namespace ? 1 : 0) +
    (objectKind ? 1 : 0) +
    (reason ? 1 : 0) +
    (level ? 1 : 0) +
    (searchQuery ? 1 : 0)

  const clearFilters = useCallback(() => {
    setKind("")
    setSystem("")
    setNamespace("")
    setObjectKind("")
    setReason("")
    setLevel("")
    setSearchText("")
    setSearchQuery("")
    setPage(1)
  }, [])

  const exportQuery: ChangesQuery = useMemo(
    () => ({
      hours,
      kind: kind || undefined,
      system: system || undefined,
      namespace: namespace || undefined,
      object_kind: objectKind || undefined,
      reason: reason || undefined,
      level: level || undefined,
      q: searchQuery || undefined,
      order,
      dedupe,
    }),
    [
      hours,
      kind,
      system,
      namespace,
      objectKind,
      reason,
      level,
      searchQuery,
      order,
      dedupe,
    ]
  )

  const copyText = useCallback(async (text: string, field: string) => {
    try {
      await navigator.clipboard.writeText(text)
      setCopiedField(field)
    } catch {
      setCopiedField("复制失败，请手动选择文本")
    }
    if (copyTimerRef.current !== null) window.clearTimeout(copyTimerRef.current)
    copyTimerRef.current = window.setTimeout(() => setCopiedField(""), 2500)
  }, [])

  const startRca = useCallback(
    (item: ChangeItem) => {
      const params = new URLSearchParams()
      if (item.service) params.set("service", item.service)
      const symptom = [item.reason, (item.message || "").split("\n")[0]]
        .filter(Boolean)
        .join(" · ")
      if (symptom) params.set("symptom", symptom.slice(0, 200))
      const query = params.toString()
      router.push(query ? `/rca/new?${query}` : "/rca/new")
    },
    [router]
  )

  async function handleExport() {
    if (!sourceReal) {
      setExportState({
        status: "error",
        message: "变更源未配置，无真实数据可导出",
      })
      return
    }
    setExportState({ status: "busy", message: "正在导出当前筛选结果…" })
    try {
      const rows: ChangeItem[] = []
      let offset = 0
      let reportedTotal = 0
      for (;;) {
        const response = await api.getChanges({
          ...exportQuery,
          limit: EXPORT_PAGE_SIZE,
          offset,
        })
        if (!response.source.real) {
          throw new Error("变更源未配置，无真实数据可导出")
        }
        reportedTotal = response.total
        rows.push(...response.changes)
        offset += response.changes.length
        if (!response.has_more || response.changes.length === 0) break
        if (rows.length >= EXPORT_MAX_ROWS) break
      }

      const items = rows.slice(0, EXPORT_MAX_ROWS)
      if (items.length === 0) {
        setExportState({ status: "error", message: "当前筛选条件下没有可导出的记录" })
        return
      }

      const header = [
        "发生时间",
        "类型",
        "来源",
        "命名空间",
        "资源名",
        "对象类型",
        "原因",
        "级别",
        "服务",
        "描述",
        "合并条数",
        "首次出现",
        "最近出现",
      ]
      const body = items.map((item) => [
        fmtDateTime(item.time),
        kindLabel(item.kind),
        item.system ?? "",
        item.namespace ?? "",
        item.object || item.service || "",
        item.object_kind ?? "",
        item.reason ?? "",
        item.level ?? "",
        item.service ?? "",
        item.message ?? "",
        String(item.occurrences ?? 1),
        item.first_seen ? fmtDateTime(item.first_seen) : "",
        item.last_seen ? fmtDateTime(item.last_seen) : "",
      ])
      const csv = [header, ...body]
        .map((row) => row.map(csvCell).join(","))
        .join("\r\n")

      const stamp = new Date()
        .toISOString()
        .replace(/[:.]/g, "-")
        .slice(0, 19)
      const blob = new Blob([`\uFEFF${csv}`], {
        type: "text/csv;charset=utf-8;",
      })
      const url = URL.createObjectURL(blob)
      const anchor = document.createElement("a")
      anchor.href = url
      anchor.download = `变更记录_${windowMeta.short}_${stamp}.csv`
      document.body.appendChild(anchor)
      anchor.click()
      document.body.removeChild(anchor)
      URL.revokeObjectURL(url)

      const truncated = reportedTotal > items.length
      setExportState({
        status: "done",
        message: truncated
          ? `已导出前 ${items.length} 条（当前筛选共 ${reportedTotal} 条，导出上限 ${EXPORT_MAX_ROWS} 条）`
          : `已导出 ${items.length} 条真实记录`,
      })
    } catch (error) {
      setExportState({
        status: "error",
        message: error instanceof Error ? error.message : "导出失败",
      })
    }
  }

  const showingSkeleton = status === "loading" && data === null
  const refreshing = status === "loading" && data !== null

  return (
    <div className="space-y-6">
      <header className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
        <div className="min-w-0">
          <h1 className="text-3xl font-bold tracking-tight">变更记录</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            聚合部署、配置、扩缩容与集群事件，追踪服务变化。
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <label className="sr-only" htmlFor="changes-window">
            时间范围
          </label>
          <select
            id="changes-window"
            value={hours}
            onChange={(e) => {
              setHours(Number(e.target.value))
              setPage(1)
            }}
            className="h-9 rounded-md border bg-background px-3 text-sm outline-none focus:border-primary"
          >
            {WINDOWS.map((w) => (
              <option key={w.hours} value={w.hours}>
                {w.label}
              </option>
            ))}
          </select>
          <button
            type="button"
            onClick={() => void loadChanges()}
            className="inline-flex h-9 items-center gap-2 rounded-md border px-3 text-sm font-medium hover:bg-accent"
          >
            <RefreshCw className={cn("h-4 w-4", refreshing && "animate-spin")} />
            刷新
          </button>
          <button
            type="button"
            onClick={() => void handleExport()}
            disabled={exportState.status === "busy"}
            className="inline-flex h-9 items-center gap-2 rounded-md border px-3 text-sm font-medium hover:bg-accent disabled:opacity-50"
          >
            {exportState.status === "busy" ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : (
              <Download className="h-4 w-4" />
            )}
            导出
          </button>
        </div>
      </header>

      {exportState.message && (
        <div
          className={cn(
            "flex items-start gap-2 rounded-md border px-3 py-2 text-xs",
            exportState.status === "error"
              ? "border-destructive/30 bg-destructive/10 text-destructive"
              : "border-border bg-accent/40 text-muted-foreground"
          )}
        >
          {exportState.status === "done" && (
            <Check className="mt-0.5 h-3.5 w-3.5 shrink-0" />
          )}
          <span className="break-words">{exportState.message}</span>
        </div>
      )}

      <div className="grid gap-4 md:grid-cols-3">
        <Card>
          <CardContent className="py-4">
            <div className="text-xs uppercase tracking-wide text-muted-foreground">
              近{windowMeta.short}记录
            </div>
            <div className="mt-1 text-2xl font-bold">
              {stats ? stats.total : "—"}
            </div>
            <div className="mt-1 text-xs text-muted-foreground">
              {stats
                ? `部署 ${stats.by_kind?.deployment ?? 0} | 配置 ${
                    stats.by_kind?.config ?? 0
                  } | 扩缩容 ${stats.by_kind?.scale ?? 0}`
                : "暂无统计数据"}
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="py-4">
            <div className="text-xs uppercase tracking-wide text-muted-foreground">
              涉及服务
            </div>
            <div className="mt-1 text-2xl font-bold">
              {stats ? stats.services : "—"}
            </div>
            <div className="mt-1 truncate text-xs text-muted-foreground">
              {stats?.top_services && stats.top_services.length > 0
                ? `主要服务 ${stats.top_services.slice(0, 3).join("、")}`
                : "无服务字段的真实记录"}
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="py-4">
            <div className="text-xs uppercase tracking-wide text-muted-foreground">
              记录类型
            </div>
            <div className="mt-1 text-2xl font-bold">
              {dominantKind ? kindLabel(dominantKind[0]) : "—"}
            </div>
            <div className="mt-1 text-xs text-muted-foreground">
              {kindRows.length === 0
                ? "当前窗口内暂无变更事件。"
                : kindRows.length === 1
                  ? "当前数据均为集群事件。"
                  : `${kindRows
                      .slice(0, 2)
                      .map(([k, count]) => `${kindLabel(k)} ${count} 条`)
                      .join(" · ")}（占 ${dominantShare}%）`}
            </div>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardContent className="py-4">
          <div className="grid gap-3 md:grid-cols-2 lg:grid-cols-3">
            <label className="relative md:col-span-2 lg:col-span-1">
              <span className="sr-only">搜索变更</span>
              <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
              <input
                value={searchText}
                onChange={(e) => setSearchText(e.target.value)}
                placeholder="搜索服务、资源名称或事件内容"
                className="h-9 w-full rounded-md border bg-background pl-9 pr-3 text-sm outline-none focus:border-primary"
              />
            </label>

            <label>
              <span className="sr-only">类型</span>
              <select
                value={kind}
                onChange={(e) => {
                  setKind(e.target.value)
                  setPage(1)
                }}
                className="h-9 w-full rounded-md border bg-background px-3 text-sm outline-none focus:border-primary"
              >
                <option value="">全部类型</option>
                {kindRows.map(([k]) => (
                  <option key={k} value={k}>
                    {kindLabel(k)}
                  </option>
                ))}
              </select>
            </label>

            <label>
              <span className="sr-only">来源</span>
              <select
                value={system}
                onChange={(e) => {
                  setSystem(e.target.value)
                  setPage(1)
                }}
                className="h-9 w-full rounded-md border bg-background px-3 text-sm outline-none focus:border-primary"
              >
                <option value="">全部来源</option>
                {(stats?.systems ?? []).map((s) => (
                  <option key={s} value={s}>
                    {s}
                  </option>
                ))}
              </select>
            </label>

            <label>
              <span className="sr-only">命名空间</span>
              <select
                value={namespace}
                onChange={(e) => {
                  setNamespace(e.target.value)
                  setPage(1)
                }}
                className="h-9 w-full rounded-md border bg-background px-3 text-sm outline-none focus:border-primary"
              >
                <option value="">全部命名空间</option>
                {(stats?.namespaces ?? []).map((ns) => (
                  <option key={ns} value={ns}>
                    {ns}
                  </option>
                ))}
              </select>
            </label>

            <label>
              <span className="sr-only">事件原因</span>
              <select
                value={reason}
                onChange={(e) => {
                  setReason(e.target.value)
                  setPage(1)
                }}
                className="h-9 w-full rounded-md border bg-background px-3 text-sm outline-none focus:border-primary"
              >
                <option value="">全部原因</option>
                {reasonOptions.map(([name, count]) => (
                  <option key={name} value={name}>
                    {count > 0 ? `${name} (${count})` : name}
                  </option>
                ))}
              </select>
            </label>

            <label>
              <span className="sr-only">级别</span>
              <select
                value={level}
                onChange={(e) => {
                  setLevel(e.target.value)
                  setPage(1)
                }}
                className="h-9 w-full rounded-md border bg-background px-3 text-sm outline-none focus:border-primary"
              >
                <option value="">全部级别</option>
                {levelOptions.map(([name, count]) => (
                  <option key={name} value={name}>
                    {count > 0
                      ? `${levelFilterLabel(name)} (${count})`
                      : levelFilterLabel(name)}
                  </option>
                ))}
              </select>
            </label>
          </div>

          {activeFilterCount > 0 && (
            <div className="mt-3 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
              <span>已启用 {activeFilterCount} 个筛选条件</span>
              <button
                type="button"
                onClick={clearFilters}
                className="inline-flex items-center gap-1 rounded-md border px-2 py-1 font-medium text-foreground hover:bg-accent"
              >
                <RotateCcw className="h-3 w-3" />
                重置
              </button>
            </div>
          )}
        </CardContent>
      </Card>

      <div className="grid min-w-0 gap-6 lg:grid-cols-[minmax(0,1fr)_360px] xl:grid-cols-[minmax(0,1fr)_400px]">
        <div className="min-w-0 space-y-6">
          <div className="flex flex-wrap gap-1 border-b">
            <button
              type="button"
              onClick={() => {
                setKind("")
                setPage(1)
              }}
              className={cn(
                "-mb-px border-b-2 px-3 py-2 text-sm font-medium transition-colors",
                kind === ""
                  ? "border-primary text-foreground"
                  : "border-transparent text-muted-foreground hover:text-foreground"
              )}
            >
              全部 {stats?.total ?? 0}
            </button>
            {kindRows.map(([k, count]) => (
              <button
                key={k}
                type="button"
                onClick={() => {
                  setKind(k)
                  setPage(1)
                }}
                className={cn(
                  "-mb-px border-b-2 px-3 py-2 text-sm font-medium transition-colors",
                  kind === k
                    ? "border-primary text-foreground"
                    : "border-transparent text-muted-foreground hover:text-foreground"
                )}
              >
                {kindLabel(k)} {count}
              </button>
            ))}
          </div>

          {showObjectKinds && (
            <div className="rounded-lg border bg-accent/20 px-3 py-2.5">
              <div className="flex flex-wrap items-baseline gap-x-2 gap-y-1">
                <span className="text-xs font-semibold text-foreground">
                  对象类型
                </span>
                <span className="text-[11px] text-muted-foreground">
                  按事件的 object kind 再分一层
                </span>
              </div>

              <div className="mt-2 flex flex-wrap items-center gap-1.5">
                <button
                  type="button"
                  onClick={() => {
                    setObjectKind("")
                    setPage(1)
                  }}
                  className={cn(
                    "inline-flex max-w-full items-center gap-1 rounded-full border px-2.5 py-1 text-[11px] font-medium transition-colors",
                    objectKind === ""
                      ? "border-transparent bg-primary text-primary-foreground"
                      : "border-border bg-background text-muted-foreground hover:bg-accent hover:text-foreground"
                  )}
                >
                  <span className="truncate">全部对象</span>
                  <span className="tabular-nums opacity-80">
                    {stats?.total ?? 0}
                  </span>
                </button>

                {visibleObjectKinds.map(([name, count]) => (
                  <button
                    key={name}
                    type="button"
                    onClick={() => {
                      setObjectKind((prev) => (prev === name ? "" : name))
                      setPage(1)
                    }}
                    className={cn(
                      "inline-flex max-w-full items-center gap-1 rounded-full border px-2.5 py-1 text-[11px] font-medium transition-colors",
                      objectKind === name
                        ? "border-transparent bg-primary text-primary-foreground"
                        : "border-border bg-background text-muted-foreground hover:bg-accent hover:text-foreground"
                    )}
                  >
                    <span className="truncate">{name}</span>
                    <span className="tabular-nums opacity-80">{count}</span>
                  </button>
                ))}

                {objectKindRows.length > OBJECT_KIND_PREVIEW && (
                  <button
                    type="button"
                    onClick={() => setObjectKindExpanded((prev) => !prev)}
                    className="inline-flex items-center gap-1 rounded-full px-2 py-1 text-[11px] font-medium text-muted-foreground underline-offset-2 hover:text-foreground hover:underline"
                  >
                    {objectKindExpanded ? (
                      <>
                        收起 <ChevronUp className="h-3 w-3" />
                      </>
                    ) : (
                      <>
                        展开更多（{objectKindRows.length - OBJECT_KIND_PREVIEW}）{" "}
                        <ChevronDown className="h-3 w-3" />
                      </>
                    )}
                  </button>
                )}

                {hiddenObjectKind && (
                  <span className="text-[11px] text-muted-foreground">
                    已选 {objectKind}
                  </span>
                )}
              </div>
            </div>
          )}

          <Card>
            <CardHeader className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
              <div className="min-w-0">
                <CardTitle>变更时间线</CardTitle>
                <CardDescription>
                  {data?.window
                    ? `查询窗口 ${fmtDateTime(data.window.start)} ~ ${fmtDateTime(data.window.end)}`
                    : `近${windowMeta.short}的真实变更记录`}
                </CardDescription>
              </div>
              <div className="flex flex-wrap items-center gap-2">
                <Switch
                  checked={order === "desc"}
                  onToggle={(next) => {
                    setOrder(next ? "desc" : "asc")
                    setPage(1)
                  }}
                  label="最新优先"
                />
                <Switch
                  checked={dedupe}
                  onToggle={(next) => {
                    setDedupe(next)
                    setPage(1)
                  }}
                  label="合并重复事件"
                />
              </div>
            </CardHeader>

            <CardContent className="space-y-5">
              {showingSkeleton && (
                <div className="space-y-3">
                  {Array.from({ length: 6 }).map((_, index) => (
                    <div
                      key={index}
                      className="flex animate-pulse items-start gap-4 rounded-md border p-4"
                    >
                      <div className="h-3 w-16 rounded bg-muted" />
                      <div className="h-8 w-8 rounded-full bg-muted" />
                      <div className="flex-1 space-y-2">
                        <div className="h-3 w-40 rounded bg-muted" />
                        <div className="h-3 w-3/4 rounded bg-muted" />
                      </div>
                    </div>
                  ))}
                </div>
              )}

              {status === "error" && (
                <div className="flex flex-col items-center justify-center gap-3 px-6 py-12 text-center">
                  <span className="flex h-12 w-12 items-center justify-center rounded-2xl bg-destructive/10 text-destructive">
                    <AlertTriangle className="h-6 w-6" />
                  </span>
                  <div>
                    <p className="text-sm font-semibold">变更记录加载失败</p>
                    <p className="mt-1 max-w-lg break-words text-xs text-muted-foreground">
                      {loadError || "后端 /api/v1/changes 暂不可用"}
                    </p>
                  </div>
                  <button
                    type="button"
                    onClick={() => void loadChanges()}
                    className="inline-flex h-9 items-center gap-2 rounded-md bg-primary px-4 text-xs font-medium text-primary-foreground hover:bg-primary/90"
                  >
                    <RefreshCw className="h-3.5 w-3.5" />
                    重试
                  </button>
                </div>
              )}

              {status !== "error" && !showingSkeleton && !sourceReal && (
                <div className="flex flex-col items-center justify-center gap-3 px-6 py-12 text-center">
                  <span className="flex h-12 w-12 items-center justify-center rounded-2xl bg-amber-500/10 text-amber-600 dark:text-amber-300">
                    <AlertTriangle className="h-6 w-6" />
                  </span>
                  <div>
                    <p className="text-sm font-semibold">变更源未配置</p>
                    <p className="mt-1 max-w-lg break-words text-xs leading-5 text-muted-foreground">
                      {data?.source.hint ||
                        "未配置真实变更源（K8S_ENDPOINT/K8S_TOKEN/K8S_NAMESPACE 或平台事件源）"}
                    </p>
                  </div>
                </div>
              )}

              {status !== "error" && !showingSkeleton && sourceReal && total === 0 && (
                <div className="flex flex-col items-center justify-center gap-3 px-6 py-12 text-center">
                  <span className="flex h-12 w-12 items-center justify-center rounded-2xl bg-muted text-muted-foreground">
                    <Inbox className="h-6 w-6" />
                  </span>
                  <div>
                    <p className="text-sm font-semibold">
                      近{windowMeta.short}无真实变更记录
                    </p>
                    <p className="mt-1 text-xs text-muted-foreground">
                      变更源已连接
                      {data?.source.type ? `（${data.source.type}）` : ""}
                      ，当前筛选条件下未查询到变更事件
                    </p>
                  </div>
                </div>
              )}

              {status !== "error" && !showingSkeleton && sourceReal && total > 0 && (
                <div className="space-y-6">
                  {days.map((day) => (
                    <div key={day.key} className="space-y-2">
                      <div className="flex items-center gap-2">
                        <span className="text-xs font-semibold text-muted-foreground">
                          {day.label}
                        </span>
                        <span className="h-px flex-1 bg-border" />
                        <span className="text-xs text-muted-foreground">
                          {day.items.length} 条
                        </span>
                      </div>

                      <div className="relative">
                        {day.items.map((item, index) => {
                          const Icon = kindIcon(item.kind)
                          const isSelected = item.id === selectedId
                          return (
                            <button
                              key={`${item.id}-${index}`}
                              type="button"
                              onClick={() => setSelectedId(item.id)}
                              className={cn(
                                "flex w-full items-start gap-3 rounded-md border px-3 py-3 text-left transition-colors",
                                isSelected
                                  ? "border-sky-500 bg-sky-50 dark:border-sky-500/70 dark:bg-sky-500/10"
                                  : "border-transparent hover:bg-accent/40"
                              )}
                            >
                              <span className="w-16 shrink-0 pt-0.5 font-mono text-xs text-muted-foreground">
                                {fmtClock(item.time)}
                              </span>

                              <span className="relative flex w-4 shrink-0 justify-center self-stretch">
                                <span className="absolute inset-y-0 w-px bg-border" />
                                <span
                                  className={cn(
                                    "relative mt-1.5 h-2.5 w-2.5 rounded-full ring-4 ring-background",
                                    isSelected ? "bg-sky-500" : "bg-muted-foreground/50"
                                  )}
                                />
                              </span>

                              <LevelIcon level={item.level} className="mt-0.5 shrink-0" />

                              <span className="min-w-0 flex-1 space-y-1">
                                <span className="flex flex-wrap items-center gap-2">
                                  <span className="truncate text-sm font-semibold">
                                    {resourceName(item)}
                                  </span>
                                  {item.namespace && (
                                    <span className="rounded border px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground">
                                      {item.namespace}
                                    </span>
                                  )}
                                  {item.object_kind && (
                                    <span className="rounded border px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground">
                                      {item.object_kind}
                                    </span>
                                  )}
                                  <span
                                    className={cn(
                                      "inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[10px] font-semibold",
                                      KIND_BADGE[item.kind] ??
                                        "border-transparent bg-secondary text-secondary-foreground"
                                    )}
                                  >
                                    <Icon className="h-3 w-3" />
                                    {kindLabel(item.kind)}
                                  </span>
                                  {item.reason && (
                                    <span className="inline-flex items-center rounded border border-orange-200 bg-orange-50 px-1.5 py-0.5 font-mono text-[10px] font-medium text-orange-700 dark:border-orange-500/30 dark:bg-orange-500/15 dark:text-orange-300">
                                      {item.reason}
                                    </span>
                                  )}
                                  {dedupe && (item.occurrences ?? 1) > 1 && (
                                    <span className="inline-flex items-center rounded-full bg-secondary px-2 py-0.5 text-[10px] font-semibold text-secondary-foreground">
                                      ×{item.occurrences}
                                    </span>
                                  )}
                                </span>
                                <span className="line-clamp-2 block break-words text-xs text-muted-foreground">
                                  {item.message || "（源事件无描述）"}
                                </span>
                              </span>

                              <ChevronRight
                                className={cn(
                                  "mt-1 h-4 w-4 shrink-0 text-muted-foreground",
                                  isSelected && "text-sky-500"
                                )}
                              />
                            </button>
                          )
                        })}
                      </div>
                    </div>
                  ))}
                </div>
              )}

              {status !== "error" && !showingSkeleton && sourceReal && total > 0 && (
                <div className="flex flex-col gap-3 border-t pt-4 sm:flex-row sm:items-center sm:justify-between">
                  <span className="text-xs text-muted-foreground">
                    共 {total} 条
                    {pageCount > 1 ? ` · 第 ${page}/${pageCount} 页` : ""}
                  </span>

                  <div className="flex flex-wrap items-center gap-3">
                    <div className="flex items-center gap-1">
                      <button
                        type="button"
                        aria-label="上一页"
                        disabled={page <= 1}
                        onClick={() => setPage((prev) => Math.max(1, prev - 1))}
                        className="flex h-8 w-8 items-center justify-center rounded-md border hover:bg-accent disabled:cursor-not-allowed disabled:opacity-40"
                      >
                        <ChevronLeft className="h-4 w-4" />
                      </button>
                      {paginationItems(page, pageCount).map((item, index) =>
                        item === "gap" ? (
                          <span
                            key={`gap-${index}`}
                            className="px-1 text-xs text-muted-foreground"
                          >
                            …
                          </span>
                        ) : (
                          <button
                            key={item}
                            type="button"
                            onClick={() => setPage(item)}
                            className={cn(
                              "h-8 min-w-8 rounded-md border px-2 text-xs font-medium",
                              item === page
                                ? "border-primary bg-primary/10 text-foreground"
                                : "hover:bg-accent"
                            )}
                          >
                            {item}
                          </button>
                        )
                      )}
                      <button
                        type="button"
                        aria-label="下一页"
                        disabled={!hasMore}
                        onClick={() => setPage((prev) => prev + 1)}
                        className="flex h-8 w-8 items-center justify-center rounded-md border hover:bg-accent disabled:cursor-not-allowed disabled:opacity-40"
                      >
                        <ChevronRight className="h-4 w-4" />
                      </button>
                    </div>

                    <label className="flex items-center gap-2 text-xs text-muted-foreground">
                      每页
                      <select
                        value={pageSize}
                        onChange={(e) => {
                          setPageSize(Number(e.target.value))
                          setPage(1)
                        }}
                        className="h-8 rounded-md border bg-background px-2 text-xs outline-none focus:border-primary"
                      >
                        {PAGE_SIZES.map((size) => (
                          <option key={size} value={size}>
                            {size}
                          </option>
                        ))}
                      </select>
                      条
                    </label>
                  </div>
                </div>
              )}
            </CardContent>
          </Card>
        </div>

        <aside className="min-w-0 lg:sticky lg:top-6 lg:self-start">
          {selected ? (
            <Card className="min-w-0">
              <CardHeader className="flex flex-row items-start justify-between gap-3 space-y-0">
                <CardTitle className="text-base">记录详情</CardTitle>
                <button
                  type="button"
                  aria-label="关闭详情"
                  onClick={() => setSelectedId(null)}
                  className="rounded-md p-1 text-muted-foreground hover:bg-accent hover:text-foreground"
                >
                  <X className="h-4 w-4" />
                </button>
              </CardHeader>
              <CardContent className="space-y-4">
                <div className="flex items-start gap-2 rounded-md border border-orange-200 bg-orange-50 px-3 py-2.5 dark:border-orange-500/30 dark:bg-orange-500/10">
                  <CircleAlert className="mt-0.5 h-4 w-4 shrink-0 text-orange-500" />
                  <div className="min-w-0 space-y-1.5">
                    <p className="break-words text-xs font-semibold text-orange-800 dark:text-orange-200">
                      {reasonLabel(selected.reason) || levelLabel(selected.level)}
                    </p>
                    {selected.reason && (
                      <span className="inline-flex max-w-full items-center truncate rounded border border-orange-300 bg-white/70 px-1.5 py-0.5 font-mono text-[10px] font-medium text-orange-700 dark:border-orange-500/40 dark:bg-orange-500/10 dark:text-orange-300">
                        {selected.reason}
                      </span>
                    )}
                    <p className="break-words text-[11px] text-orange-700/90 dark:text-orange-200/80">
                      {kindLabel(selected.kind)} · {levelLabel(selected.level)}
                    </p>
                  </div>
                </div>

                <dl className="grid grid-cols-2 gap-x-3 gap-y-3 text-xs">
                  <div className="min-w-0">
                    <dt className="text-muted-foreground">事件时间</dt>
                    <dd className="mt-0.5 break-words font-medium">
                      {fmtDateTime(selected.time)}
                    </dd>
                  </div>
                  <div className="min-w-0">
                    <dt className="text-muted-foreground">来源</dt>
                    <dd className="mt-0.5 break-words font-medium">
                      {selected.system || "—"}
                    </dd>
                  </div>
                  <div className="min-w-0">
                    <dt className="text-muted-foreground">类型</dt>
                    <dd className="mt-0.5 break-words font-medium">
                      {kindLabel(selected.kind)}
                    </dd>
                  </div>
                  <div className="min-w-0">
                    <dt className="text-muted-foreground">命名空间</dt>
                    <dd className="mt-0.5 break-words font-medium">
                      {selected.namespace || "—"}
                    </dd>
                  </div>
                  <div className="col-span-2 min-w-0">
                    <dt className="text-muted-foreground">资源名</dt>
                    <dd className="mt-0.5 flex flex-wrap items-center gap-2 font-medium">
                      <span className="break-all">{resourceName(selected)}</span>
                      {selected.object_kind && (
                        <span className="rounded border px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground">
                          {selected.object_kind}
                        </span>
                      )}
                    </dd>
                  </div>
                  {dedupe && (
                    <div className="col-span-2 min-w-0">
                      <dt className="text-muted-foreground">合并统计</dt>
                      <dd className="mt-0.5 break-words font-medium">
                        {selected.occurrences ?? 1} 条 · 首次{" "}
                        {fmtDateTime(selected.first_seen ?? selected.time)} · 最近{" "}
                        {fmtDateTime(selected.last_seen ?? selected.time)}
                      </dd>
                    </div>
                  )}
                </dl>

                <div className="space-y-2">
                  <div className="flex items-center justify-between gap-2">
                    <span className="text-xs font-semibold">原始事件</span>
                    <button
                      type="button"
                      onClick={() =>
                        void copyText(
                          selected.message || "",
                          "已复制原始事件内容"
                        )
                      }
                      className="inline-flex items-center gap-1 rounded-md border px-2 py-1 text-[11px] hover:bg-accent"
                    >
                      <Copy className="h-3 w-3" />
                      复制
                    </button>
                  </div>
                  <pre className="max-h-48 overflow-auto whitespace-pre-wrap break-words rounded-md border bg-muted/40 p-3 font-mono text-[11px] leading-5">
                    {selected.message || "（源事件未提供描述）"}
                  </pre>
                  <p className="text-[11px] text-muted-foreground">
                    保留源事件内容，不代表已确认根因。
                  </p>
                </div>

                <div className="space-y-1 rounded-md border bg-accent/20 p-3">
                  <div className="text-xs font-semibold">关联服务</div>
                  <p className="break-words text-xs font-medium">
                    {selected.service || "—"}
                  </p>
                  <p className="text-[11px] text-muted-foreground">
                    {selected.service
                      ? "来源于变更记录自身的 service 字段。"
                      : "尚未关联服务，不要凭 Pod 名称推断服务关系。"}
                  </p>
                </div>

                {copiedField && (
                  <p className="text-[11px] text-muted-foreground">{copiedField}</p>
                )}

                <div className="flex flex-col gap-2 sm:flex-row">
                  <button
                    type="button"
                    onClick={() =>
                      void copyText(recordSummary(selected), "已复制记录 JSON 摘要")
                    }
                    className="inline-flex h-9 flex-1 items-center justify-center gap-2 rounded-md border px-3 text-xs font-medium hover:bg-accent"
                  >
                    <Copy className="h-3.5 w-3.5" />
                    复制记录
                  </button>
                  <button
                    type="button"
                    onClick={() => startRca(selected)}
                    className="inline-flex h-9 flex-1 items-center justify-center gap-2 rounded-md bg-primary px-3 text-xs font-semibold text-primary-foreground hover:bg-primary/90"
                  >
                    发起 RCA 分析
                  </button>
                </div>
              </CardContent>
            </Card>
          ) : (
            <Card>
              <CardHeader>
                <CardTitle className="text-base">记录详情</CardTitle>
                <CardDescription>选择时间线中的一条记录查看详情</CardDescription>
              </CardHeader>
              <CardContent className="space-y-2 py-6 text-center text-xs text-muted-foreground">
                <span className="mx-auto flex h-12 w-12 items-center justify-center rounded-2xl bg-muted">
                  <Activity className="h-5 w-5" />
                </span>
                <p>点击任意变更事件，可查看原始事件内容、命名空间与资源信息。</p>
                <p>详情仅展示源事件字段，不做根因推断。</p>
              </CardContent>
            </Card>
          )}
        </aside>
      </div>
    </div>
  )
}
