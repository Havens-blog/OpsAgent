"use client"

import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
} from "react"
import {
  Activity,
  AlertTriangle,
  Boxes,
  ChevronRight,
  Database,
  GitBranch,
  Layers3,
  Maximize2,
  MessageSquareMore,
  Minimize2,
  Minus,
  Plus,
  Scan,
  Server,
} from "lucide-react"
import { Badge, healthVariant } from "@/components/ui/badge"
import type { ServiceNode } from "@/lib/api"
import { cn } from "@/lib/utils"

export type GroupDirection = "upstream" | "downstream"
export type GroupTone =
  | "critical"
  | "abnormal"
  | "application"
  | "database"
  | "cache"
  | "queue"
  | "default"

export interface TopologyGroup {
  id: string
  direction: GroupDirection
  title: string
  description: string
  tone: GroupTone
  nodes: ServiceNode[]
}

interface TopologyGraphProps {
  focusNode: ServiceNode
  upstreamGroups: TopologyGroup[]
  downstreamGroups: TopologyGroup[]
  selectedGroupId: string | null
  onSelectGroup: (group: TopologyGroup) => void
}

interface Connection {
  id: string
  path: string
  color: string
  tone: GroupTone
  count: number
  labelX: number
  labelY: number
  selected: boolean
}

const TONE_COLOR: Record<GroupTone, string> = {
  critical: "hsl(var(--primary))",
  abnormal: "hsl(var(--warning))",
  application: "hsl(var(--cat-application))",
  database: "hsl(var(--cat-database))",
  cache: "hsl(var(--cat-cache))",
  queue: "hsl(var(--cat-queue))",
  default: "hsl(var(--muted-foreground))",
}

export function formatThroughput(value: number): string {
  if (value >= 1000) return `${(value / 1000).toFixed(1)}k`
  if (value >= 10) return value.toFixed(0)
  return value.toFixed(1)
}

export function formatErrorRate(value: number): string {
  return `${(value * 100).toFixed(2)}%`
}

export function healthLabel(health: string): string {
  const labels: Record<string, string> = {
    healthy: "健康",
    degraded: "降级",
    unhealthy: "异常",
    unknown: "无数据",
  }
  return labels[health] ?? health ?? "无数据"
}

export function healthContrastClass(health: string): string {
  if (health === "healthy") return "text-success"
  if (health === "degraded") return "text-warning"
  if (health === "unhealthy") return "text-danger"
  return ""
}

export function serviceTypeLabel(type: string): string {
  const labels: Record<string, string> = {
    application: "应用服务",
    database: "数据库",
    cache: "缓存",
    queue: "消息队列",
  }
  return labels[type] ?? type ?? "未知类型"
}

function GroupIcon({ tone, className }: { tone: GroupTone; className?: string }) {
  switch (tone) {
    case "critical":
      return <Activity className={className} />
    case "abnormal":
      return <AlertTriangle className={className} />
    case "database":
      return <Database className={className} />
    case "cache":
      return <Layers3 className={className} />
    case "queue":
      return <MessageSquareMore className={className} />
    case "application":
      return <Boxes className={className} />
    default:
      return <GitBranch className={className} />
  }
}

export function TopologyGraph({
  focusNode,
  upstreamGroups,
  downstreamGroups,
  selectedGroupId,
  onSelectGroup,
}: TopologyGraphProps) {
  const panelRef = useRef<HTMLDivElement>(null)
  const viewportRef = useRef<HTMLDivElement>(null)
  const sceneRef = useRef<HTMLDivElement>(null)
  const focusRef = useRef<HTMLDivElement>(null)
  const groupRefs = useRef(new Map<string, HTMLButtonElement>())
  const [zoom, setZoom] = useState(1)
  const [viewportSize, setViewportSize] = useState({ width: 0, height: 500 })
  const [connections, setConnections] = useState<Connection[]>([])
  const [canFullscreen, setCanFullscreen] = useState(false)
  const [isFullscreen, setIsFullscreen] = useState(false)
  const [fullscreenError, setFullscreenError] = useState("")

  const groupRows = Math.max(upstreamGroups.length, downstreamGroups.length, 1)
  const sceneHeight = Math.max(430, groupRows * 94 + 24)
  const compact = viewportSize.width > 0 && viewportSize.width < 620

  const centerCanvas = useCallback(() => {
    const viewport = viewportRef.current
    if (!viewport) return
    viewport.scrollTo({
      left: Math.max(0, (viewport.scrollWidth - viewport.clientWidth) / 2),
      top: Math.max(0, (viewport.scrollHeight - viewport.clientHeight) / 2),
    })
  }, [])

  useLayoutEffect(() => {
    const viewport = viewportRef.current
    if (!viewport) return
    const updateSize = () => {
      setViewportSize({ width: viewport.clientWidth, height: viewport.clientHeight })
    }
    updateSize()
    const observer = new ResizeObserver(updateSize)
    observer.observe(viewport)
    return () => observer.disconnect()
  }, [])

  useLayoutEffect(() => {
    const scene = sceneRef.current
    const focus = focusRef.current
    if (!scene || !focus || viewportSize.width === 0) return

    const sceneRect = scene.getBoundingClientRect()
    const focusRect = focus.getBoundingClientRect()
    const point = (rect: DOMRect) => ({
      left: (rect.left - sceneRect.left) / zoom,
      right: (rect.right - sceneRect.left) / zoom,
      centerY: (rect.top - sceneRect.top + rect.height / 2) / zoom,
    })
    const focusPoint = point(focusRect)
    const next: Connection[] = []

    for (const group of [...upstreamGroups, ...downstreamGroups]) {
      const element = groupRefs.current.get(group.id)
      if (!element) continue
      const groupPoint = point(element.getBoundingClientRect())
      const upstream = group.direction === "upstream"
      const x1 = upstream ? groupPoint.right : focusPoint.right
      const x2 = upstream ? focusPoint.left : groupPoint.left
      const y1 = upstream ? groupPoint.centerY : focusPoint.centerY
      const y2 = upstream ? focusPoint.centerY : groupPoint.centerY
      const bend = Math.max(18, Math.abs(x2 - x1) * 0.45)
      next.push({
        id: group.id,
        path: `M ${x1} ${y1} C ${x1 + bend} ${y1}, ${x2 - bend} ${y2}, ${x2} ${y2}`,
        color: TONE_COLOR[group.tone],
        tone: group.tone,
        count: group.nodes.length,
        labelX: (x1 + x2) / 2,
        labelY: (y1 + y2) / 2 - 8,
        selected: selectedGroupId === group.id,
      })
    }
    setConnections(next)
  }, [
    focusNode.id,
    selectedGroupId,
    upstreamGroups,
    downstreamGroups,
    viewportSize.width,
    sceneHeight,
    zoom,
  ])

  useEffect(() => {
    const frame = requestAnimationFrame(centerCanvas)
    return () => cancelAnimationFrame(frame)
  }, [zoom, centerCanvas, viewportSize.width, viewportSize.height])

  useEffect(() => {
    const panel = panelRef.current
    setCanFullscreen(
      Boolean(panel && document.fullscreenEnabled && panel.requestFullscreen),
    )
    const onFullscreenChange = () => {
      setIsFullscreen(document.fullscreenElement === panel)
      requestAnimationFrame(centerCanvas)
    }
    document.addEventListener("fullscreenchange", onFullscreenChange)
    return () => document.removeEventListener("fullscreenchange", onFullscreenChange)
  }, [centerCanvas])

  function fitCanvas() {
    setZoom(1)
    requestAnimationFrame(centerCanvas)
  }

  async function toggleFullscreen() {
    const panel = panelRef.current
    if (!panel) return
    setFullscreenError("")
    try {
      if (document.fullscreenElement === panel) {
        await document.exitFullscreen()
      } else {
        await panel.requestFullscreen()
      }
    } catch {
      setFullscreenError("浏览器未允许全屏，请检查站点权限")
    }
  }

  const scaledWidth = viewportSize.width * zoom
  const scaledHeight = sceneHeight * zoom
  const stageWidth = Math.max(viewportSize.width, scaledWidth)
  const stageHeight = Math.max(viewportSize.height, scaledHeight)
  const tones = [...new Set(connections.map((connection) => connection.tone))]

  return (
    <div
      ref={panelRef}
      className={cn(
        "overflow-hidden rounded-xl border border-border bg-card text-card-foreground",
        isFullscreen && "flex h-screen w-screen flex-col rounded-none bg-background p-4",
      )}
    >
      <div className="flex shrink-0 flex-wrap items-center justify-between gap-3 border-b border-border px-4 py-3">
        <div>
          <h2 className="text-sm font-semibold text-foreground">关系聚合</h2>
          <p className="mt-0.5 text-xs text-muted-foreground">
            真实依赖按组分线，点击卡片筛选完整清单
          </p>
        </div>
        <div className="flex items-center gap-1.5">
          <button
            type="button"
            onClick={fitCanvas}
            className="inline-flex h-8 items-center gap-1.5 rounded-md border border-border bg-background px-2.5 text-xs text-foreground hover:bg-accent"
            title="恢复 100% 并居中"
          >
            <Scan className="h-3.5 w-3.5" />
            适应画布
          </button>
          {canFullscreen && (
            <button
              type="button"
              onClick={() => void toggleFullscreen()}
              className="inline-flex h-8 items-center gap-1.5 rounded-md border border-border bg-background px-2.5 text-xs text-foreground hover:bg-accent"
              title={isFullscreen ? "退出全屏" : "全屏查看"}
            >
              {isFullscreen ? (
                <Minimize2 className="h-3.5 w-3.5" />
              ) : (
                <Maximize2 className="h-3.5 w-3.5" />
              )}
              {isFullscreen ? "退出全屏" : "全屏"}
            </button>
          )}
        </div>
      </div>

      {fullscreenError && (
        <p className="shrink-0 border-b border-warning-border bg-warning-soft px-4 py-2 text-xs text-warning">
          {fullscreenError}
        </p>
      )}

      <div
        ref={viewportRef}
        className={cn(
          "relative min-w-0 overflow-auto bg-background/60",
          isFullscreen ? "min-h-0 flex-1" : "h-[500px]",
        )}
        style={{
          backgroundImage: "radial-gradient(circle, hsl(var(--border)) 1px, transparent 1px)",
          backgroundSize: "20px 20px",
        }}
      >
        <div className="relative" style={{ width: stageWidth, height: stageHeight }}>
          <div
            ref={sceneRef}
            className="absolute"
            style={{
              left: (stageWidth - scaledWidth) / 2,
              top: (stageHeight - scaledHeight) / 2,
              width: viewportSize.width,
              height: sceneHeight,
              transform: `scale(${zoom})`,
              transformOrigin: "top left",
            }}
          >
            <svg
              className="pointer-events-none absolute inset-0 z-0 h-full w-full overflow-visible"
              width={viewportSize.width}
              height={sceneHeight}
              aria-hidden="true"
            >
              <defs>
                {tones.map((tone) => (
                  <marker
                    key={tone}
                    id={`topology-arrow-${tone}`}
                    viewBox="0 0 10 10"
                    refX="9"
                    refY="5"
                    markerWidth="5"
                    markerHeight="5"
                    orient="auto-start-reverse"
                  >
                    <path d="M 0 0 L 10 5 L 0 10 z" style={{ fill: TONE_COLOR[tone] }} />
                  </marker>
                ))}
              </defs>
              {connections.map((connection) => (
                <g key={connection.id} opacity={selectedGroupId && !connection.selected ? 0.35 : 0.9}>
                  <path
                    d={connection.path}
                    fill="none"
                    style={{ stroke: connection.color }}
                    strokeWidth={connection.selected ? 2.6 : 1.7}
                    markerEnd={`url(#topology-arrow-${connection.tone})`}
                  />
                  <g transform={`translate(${connection.labelX} ${connection.labelY})`}>
                    <rect
                      x="-10"
                      y="-8"
                      width="20"
                      height="16"
                      rx="8"
                      fill="hsl(var(--card))"
                      style={{ stroke: connection.color }}
                      strokeOpacity="0.45"
                    />
                    <text
                      textAnchor="middle"
                      dominantBaseline="central"
                      fontSize="9"
                      fontWeight="600"
                      fill="hsl(var(--foreground))"
                    >
                      {connection.count}
                    </text>
                  </g>
                </g>
              ))}
            </svg>

            <div
              className="relative z-10 grid h-full items-center"
              style={{
                gridTemplateColumns: compact
                  ? "minmax(0,1fr) minmax(112px,1.25fr) minmax(0,1fr)"
                  : "minmax(0,1fr) minmax(170px,1.05fr) minmax(0,1fr)",
                columnGap: compact ? 12 : 40,
              }}
            >
              <GroupColumn
                groups={upstreamGroups}
                emptyLabel="无直接上游"
                selectedGroupId={selectedGroupId}
                compact={compact}
                groupRefs={groupRefs}
                onSelectGroup={onSelectGroup}
              />

              <div className="flex justify-center">
                <div
                  ref={focusRef}
                  className={cn(
                    "w-full max-w-[250px] rounded-2xl border-2 border-primary bg-card shadow-md",
                    compact ? "p-2.5" : "p-4",
                  )}
                >
                  <div className="flex items-center justify-between gap-2">
                    <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-primary-soft text-primary">
                      <Server className="h-4 w-4" />
                    </span>
                    <Badge
                      variant={healthVariant(focusNode.health)}
                      className={cn(healthContrastClass(focusNode.health), "px-2 text-[10px]")}
                    >
                      {healthLabel(focusNode.health)}
                    </Badge>
                  </div>
                  <p className="mt-3 text-[10px] font-medium uppercase tracking-wider text-primary">
                    核心服务
                  </p>
                  <h3
                    className={cn("mt-1 truncate font-semibold text-foreground", compact ? "text-xs" : "text-sm")}
                    title={focusNode.name}
                  >
                    {focusNode.name}
                  </h3>
                  <div className="mt-3 grid grid-cols-2 gap-2 border-t border-border pt-3">
                    <div>
                      <p className="text-[10px] text-muted-foreground">吞吐</p>
                      <p className={cn("mt-0.5 font-mono font-semibold text-foreground", compact ? "text-[10px]" : "text-xs")}>
                        {formatThroughput(focusNode.throughput)} rps
                      </p>
                    </div>
                    <div>
                      <p className="text-[10px] text-muted-foreground">错误率</p>
                      <p
                        className={cn(
                          "mt-0.5 font-mono font-semibold",
                          compact ? "text-[10px]" : "text-xs",
                          focusNode.error_rate > 0.01
                            ? "text-danger"
                            : "text-foreground",
                        )}
                      >
                        {formatErrorRate(focusNode.error_rate)}
                      </p>
                    </div>
                  </div>
                </div>
              </div>

              <GroupColumn
                groups={downstreamGroups}
                emptyLabel="无直接下游"
                selectedGroupId={selectedGroupId}
                compact={compact}
                groupRefs={groupRefs}
                onSelectGroup={onSelectGroup}
              />
            </div>
          </div>
        </div>
      </div>

      <div className="flex shrink-0 flex-wrap items-center justify-between gap-3 border-t border-border px-4 py-2.5">
        <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-xs text-muted-foreground">
          <span className="flex items-center gap-1.5">
            <span className="h-2 w-2 rounded-full bg-success" />健康
          </span>
          <span className="flex items-center gap-1.5">
            <span className="h-2 w-2 rounded-full bg-warning" />降级
          </span>
          <span className="flex items-center gap-1.5">
            <span className="h-2 w-2 rounded-full bg-tertiary" />无流量
          </span>
        </div>
        <div className="flex items-center gap-1">
          <button
            type="button"
            aria-label="缩小"
            title="缩小"
            disabled={zoom <= 0.6}
            onClick={() => setZoom((value) => Math.max(0.6, Number((value - 0.1).toFixed(1))))}
            className="flex h-7 w-7 items-center justify-center rounded-md border border-border text-foreground hover:bg-accent disabled:cursor-not-allowed disabled:opacity-40"
          >
            <Minus className="h-3.5 w-3.5" />
          </button>
          <span className="w-12 text-center font-mono text-xs text-muted-foreground">
            {Math.round(zoom * 100)}%
          </span>
          <button
            type="button"
            aria-label="放大"
            title="放大"
            disabled={zoom >= 1.6}
            onClick={() => setZoom((value) => Math.min(1.6, Number((value + 0.1).toFixed(1))))}
            className="flex h-7 w-7 items-center justify-center rounded-md border border-border text-foreground hover:bg-accent disabled:cursor-not-allowed disabled:opacity-40"
          >
            <Plus className="h-3.5 w-3.5" />
          </button>
        </div>
      </div>
    </div>
  )
}

function GroupColumn({
  groups,
  emptyLabel,
  selectedGroupId,
  compact,
  groupRefs,
  onSelectGroup,
}: {
  groups: TopologyGroup[]
  emptyLabel: string
  selectedGroupId: string | null
  compact: boolean
  groupRefs: React.MutableRefObject<Map<string, HTMLButtonElement>>
  onSelectGroup: (group: TopologyGroup) => void
}) {
  if (groups.length === 0) {
    return (
      <div className="flex justify-center">
        <div className="flex h-16 w-full items-center justify-center rounded-xl border border-dashed border-border bg-card/50 px-2 text-center text-xs text-muted-foreground">
          {emptyLabel}
        </div>
      </div>
    )
  }

  return (
    <div className="flex flex-col justify-center gap-3">
      {groups.map((group) => {
        const selected = selectedGroupId === group.id
        const color = TONE_COLOR[group.tone]
        return (
          <button
            key={group.id}
            ref={(element) => {
              if (element) groupRefs.current.set(group.id, element)
              else groupRefs.current.delete(group.id)
            }}
            type="button"
            aria-pressed={selected}
            onClick={() => onSelectGroup(group)}
            className={cn(
              "flex w-full shrink-0 items-center gap-2 rounded-xl border bg-card text-left shadow-sm transition-all hover:-translate-y-0.5 hover:border-primary/70 hover:shadow-md",
              compact ? "h-[78px] p-2" : "h-[82px] p-3",
              selected
                ? "border-primary ring-2 ring-primary/20"
                : "border-border",
            )}
          >
            <span
              className={cn(
                "flex shrink-0 items-center justify-center rounded-lg",
                compact ? "h-7 w-7" : "h-9 w-9",
              )}
              style={{ color, backgroundColor: `color-mix(in srgb, ${color} 9%, transparent)` }}
            >
              <GroupIcon tone={group.tone} className="h-4 w-4" />
            </span>
            <span className="min-w-0 flex-1">
              <span className={cn("block truncate font-semibold text-foreground", compact ? "text-[11px]" : "text-xs")}>
                {group.title} · {group.nodes.length}
              </span>
              <span className="mt-1 block truncate text-[10px] text-muted-foreground" title={group.description}>
                {group.description}
              </span>
            </span>
            <ChevronRight
              className={cn(
                "h-3.5 w-3.5 shrink-0",
                selected ? "text-primary" : "text-muted-foreground",
              )}
            />
          </button>
        )
      })}
    </div>
  )
}
