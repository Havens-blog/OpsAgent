"use client"

import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import {
  Activity,
  AlertTriangle,
  ChevronLeft,
  ChevronRight,
  GitBranch,
  Inbox,
  Loader2,
  RefreshCw,
  Settings,
  TrendingUp,
} from "lucide-react"
import { Badge } from "@/components/ui/badge"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { api, type ChangeItem, type ChangesResponse } from "@/lib/api"

type LoadStatus = "loading" | "error" | "ready"

/** 页面固定查询近 24 小时的真实变更。 */
const WINDOW_HOURS = 24

/** 时间线每页条数，避免一次性渲染上千条真实事件。 */
const PAGE_SIZE = 50

const KIND_LABELS: Record<string, string> = {
  deployment: "部署",
  config: "配置变更",
  scale: "扩缩容",
  k8s_event: "K8s 事件",
}

function kindLabel(kind: string): string {
  return KIND_LABELS[kind] ?? kind
}

function kindIcon(kind: string) {
  if (kind === "deployment") return GitBranch
  if (kind === "config") return Settings
  if (kind === "scale") return TrendingUp
  return Activity
}

function fmtTime(iso: string): string {
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return iso
  return date.toLocaleString("zh-CN", { hour12: false })
}

export default function ChangesPage() {
  const [data, setData] = useState<ChangesResponse | null>(null)
  const [status, setStatus] = useState<LoadStatus>("loading")
  const [loadError, setLoadError] = useState("")
  const requestIdRef = useRef(0)

  const loadChanges = useCallback(async () => {
    const requestId = ++requestIdRef.current
    setStatus("loading")
    setLoadError("")
    try {
      const response = await api.getChanges(WINDOW_HOURS)
      if (requestId !== requestIdRef.current) return
      setData(response)
      setStatus("ready")
    } catch (error) {
      if (requestId !== requestIdRef.current) return
      setData(null)
      setLoadError(error instanceof Error ? error.message : "变更记录加载失败")
      setStatus("error")
    }
  }, [])

  useEffect(() => {
    void loadChanges()
    return () => {
      requestIdRef.current++
    }
  }, [loadChanges])

  const changes: ChangeItem[] = useMemo(() => data?.changes ?? [], [data])
  const [page, setPage] = useState(1)

  useEffect(() => {
    setPage(1)
  }, [data])

  // Prefer server-side stats: they cover the FULL window even when the
  // payload is truncated; fall back to the returned items otherwise.
  const stats = useMemo(() => {
    let byKind: Record<string, number> = {}
    let serviceCount = 0
    let serviceNames: string[] = []
    let total = changes.length
    if (data?.stats) {
      byKind = data.stats.by_kind ?? {}
      serviceCount = data.stats.services
      serviceNames = data.stats.top_services ?? []
      total = data.stats.total
    } else {
      const services = new Set<string>()
      for (const change of changes) {
        const kind = change.kind || "unknown"
        byKind[kind] = (byKind[kind] ?? 0) + 1
        if (change.service) services.add(change.service)
      }
      serviceCount = services.size
      serviceNames = [...services].sort()
    }
    const kindCounts = Object.entries(byKind)
      .filter(([, count]) => count > 0)
      .sort((a, b) => b[1] - a[1])
    return {
      total,
      kindCounts,
      services: serviceCount,
      serviceNames,
    }
  }, [changes, data])

  const pageCount = Math.max(1, Math.ceil(changes.length / PAGE_SIZE))
  const safePage = Math.min(page, pageCount)
  const pagedChanges = changes.slice((safePage - 1) * PAGE_SIZE, safePage * PAGE_SIZE)

  const sourceReal = data?.source.real ?? false

  return (
    <div className="space-y-8">
      <header>
        <h1 className="text-3xl font-bold tracking-tight">Changes</h1>
        <p className="text-muted-foreground">
          变更记录 — 来自真实变更源的部署 / 配置 / 扩缩容与集群事件（近 {WINDOW_HOURS} 小时）
        </p>
      </header>

      {status === "loading" && (
        <Card className="flex h-[320px] flex-col items-center justify-center gap-3 text-center">
          <span className="flex h-12 w-12 items-center justify-center rounded-2xl bg-indigo-500/10 text-indigo-500">
            <Loader2 className="h-6 w-6 animate-spin" />
          </span>
          <div>
            <p className="text-sm font-medium">正在加载变更记录</p>
            <p className="mt-1 text-xs text-muted-foreground">
              正在查询真实变更源…
            </p>
          </div>
        </Card>
      )}

      {status === "error" && (
        <Card className="flex h-[320px] flex-col items-center justify-center gap-4 px-6 text-center">
          <span className="flex h-12 w-12 items-center justify-center rounded-2xl bg-red-500/10 text-red-500">
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
            className="inline-flex h-9 items-center gap-2 rounded-md bg-indigo-600 px-4 text-xs font-medium text-white hover:bg-indigo-500"
          >
            <RefreshCw className="h-3.5 w-3.5" />
            重试
          </button>
        </Card>
      )}

      {status === "ready" && !sourceReal && (
        <Card className="flex h-[320px] flex-col items-center justify-center gap-3 px-6 text-center">
          <span className="flex h-12 w-12 items-center justify-center rounded-2xl bg-amber-500/10 text-amber-600 dark:text-amber-300">
            <AlertTriangle className="h-6 w-6" />
          </span>
          <div>
            <p className="text-sm font-semibold">变更源未配置</p>
            <p className="mt-1 max-w-lg text-xs leading-5 text-muted-foreground">
              {data?.source.hint ||
                "未配置真实变更源（K8S_ENDPOINT/K8S_TOKEN/K8S_NAMESPACE 或平台事件源）"}
            </p>
          </div>
        </Card>
      )}

      {status === "ready" && sourceReal && changes.length === 0 && (
        <Card className="flex h-[320px] flex-col items-center justify-center gap-3 px-6 text-center">
          <span className="flex h-12 w-12 items-center justify-center rounded-2xl bg-muted text-muted-foreground">
            <Inbox className="h-6 w-6" />
          </span>
          <div>
            <p className="text-sm font-semibold">
              近 {WINDOW_HOURS} 小时无真实变更记录
            </p>
            <p className="mt-1 text-xs text-muted-foreground">
              变更源已连接
              {data?.source.type ? `（${data.source.type}）` : ""}
              ，该时间窗口内未查询到变更事件
            </p>
          </div>
        </Card>
      )}

      {status === "ready" && sourceReal && changes.length > 0 && (
        <>
          <div className="grid gap-4 md:grid-cols-3">
            <Card>
              <CardContent className="py-4">
                <div className="text-xs uppercase tracking-wide text-muted-foreground">
                  近 {WINDOW_HOURS}h 变更
                </div>
                <div className="mt-1 text-2xl font-bold">{stats.total}</div>
              </CardContent>
            </Card>
            <Card>
              <CardContent className="py-4">
                <div className="text-xs uppercase tracking-wide text-muted-foreground">
                  类型分布
                </div>
                <div className="mt-1 text-2xl font-bold">
                  {stats.kindCounts.length}
                  <span className="ml-1 text-xs font-normal text-muted-foreground">
                    种类型
                  </span>
                </div>
                <div className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
                  {stats.kindCounts.map(([kind, count]) => (
                    <span key={kind}>
                      {kindLabel(kind)} {count}
                    </span>
                  ))}
                </div>
              </CardContent>
            </Card>
            <Card>
              <CardContent className="py-4">
                <div className="text-xs uppercase tracking-wide text-muted-foreground">
                  涉及服务
                </div>
                <div className="mt-1 text-2xl font-bold">{stats.services}</div>
                {stats.serviceNames.length > 0 && (
                  <div
                    className="mt-1 truncate text-xs text-muted-foreground"
                    title={stats.serviceNames.join("、")}
                  >
                    {stats.serviceNames.slice(0, 3).join("、")}
                    {stats.serviceNames.length > 3 ? " 等" : ""}
                  </div>
                )}
              </CardContent>
            </Card>
          </div>

          <Card>
            <CardHeader>
              <CardTitle>变更时间线</CardTitle>
              <CardDescription>
                按时间倒序 · 数据来自真实变更源
                {data?.window
                  ? `（查询窗口 ${fmtTime(data.window.start)} ~ ${fmtTime(data.window.end)}）`
                  : ""}
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              {data?.truncated && (
                <div className="flex items-start gap-2 rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-xs text-amber-800 dark:text-amber-200">
                  <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
                  <span>
                    该窗口内真实变更源共返回 {data.total ?? stats.total} 条，为保证页面流畅仅加载最新 {changes.length} 条；
                    统计卡片按全部 {stats.total} 条计算。
                  </span>
                </div>
              )}
              <div className="space-y-4">
                {pagedChanges.map((change, index) => {
                  const Icon = kindIcon(change.kind)
                  return (
                    <div
                      key={`${change.id}-${(safePage - 1) * PAGE_SIZE + index}`}
                      className="flex gap-4 rounded-md border bg-accent/20 p-4"
                    >
                      <div className="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-md bg-secondary">
                        <Icon className="h-5 w-5" />
                      </div>
                      <div className="min-w-0 flex-1 space-y-1">
                        <div className="flex flex-wrap items-center gap-2">
                          <span className="font-semibold">
                            {change.service || "—"}
                          </span>
                          <Badge variant="secondary" className="text-xs">
                            {kindLabel(change.kind)}
                          </Badge>
                          <Badge variant="outline" className="font-mono text-xs">
                            {change.system}
                          </Badge>
                        </div>
                        <div className="break-words text-sm">
                          {change.message}
                        </div>
                        <div className="text-xs text-muted-foreground">
                          {fmtTime(change.time)}
                        </div>
                      </div>
                    </div>
                  )
                })}
              </div>
              {pageCount > 1 && (
                <div className="flex flex-wrap items-center justify-between gap-2 border-t pt-4">
                  <span className="text-xs text-muted-foreground">
                    第 {(safePage - 1) * PAGE_SIZE + 1}-{Math.min(safePage * PAGE_SIZE, changes.length)} 条 / 已加载 {changes.length} 条
                  </span>
                  <div className="flex items-center gap-2">
                    <button
                      type="button"
                      aria-label="上一页"
                      disabled={safePage <= 1}
                      onClick={() => setPage(safePage - 1)}
                      className="flex h-8 w-8 items-center justify-center rounded-md border hover:bg-accent disabled:cursor-not-allowed disabled:opacity-40"
                    >
                      <ChevronLeft className="h-4 w-4" />
                    </button>
                    <span className="min-w-16 text-center font-mono text-xs">
                      {safePage} / {pageCount}
                    </span>
                    <button
                      type="button"
                      aria-label="下一页"
                      disabled={safePage >= pageCount}
                      onClick={() => setPage(safePage + 1)}
                      className="flex h-8 w-8 items-center justify-center rounded-md border hover:bg-accent disabled:cursor-not-allowed disabled:opacity-40"
                    >
                      <ChevronRight className="h-4 w-4" />
                    </button>
                  </div>
                </div>
              )}
            </CardContent>
          </Card>
        </>
      )}
    </div>
  )
}
