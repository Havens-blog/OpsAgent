"use client"

import Link from "next/link"
import { useEffect, useState } from "react"
import {
  Activity,
  AlertTriangle,
  Bot,
  Database,
  GitBranch,
  type LucideIcon,
} from "lucide-react"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import {
  api,
  type AgentDetail,
  type HealthResponse,
  type AgentInfo,
  type DataSourceStatus,
} from "@/lib/api"
import { timeAgo } from "@/lib/utils"

function errorMessage(e: unknown): string {
  return e instanceof Error ? e.message : String(e)
}

// 智能体的一句话职责（与 Agent 管理页保持同一套说法）。
const AGENT_CAPTIONS: Record<string, string> = {
  log_analyst: "日志模式识别",
  monitor: "APM 指标分析",
  inspector: "云资源巡检",
  resource_topology: "CMDB 拓扑查询",
}

// 结构图的固定展示顺序：/agents 的 details 返回次序不稳定，排序后不再跳位。
const SPECIALIST_ORDER = ["log_analyst", "monitor", "inspector", "resource_topology"]

function agentRank(type: string): number {
  const i = SPECIALIST_ORDER.indexOf(type)
  return i === -1 ? SPECIALIST_ORDER.length : i
}

function agentCaption(agent: AgentDetail): string {
  return AGENT_CAPTIONS[agent.type] ?? agent.description ?? "—"
}

export default function DashboardPage() {
  const [health, setHealth] = useState<HealthResponse | null>(null)
  const [agents, setAgents] = useState<AgentInfo | null>(null)
  const [dataSources, setDataSources] = useState<DataSourceStatus | null>(null)
  const [incidentCount, setIncidentCount] = useState<number | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    async function load() {
      const [h, a, d, i] = await Promise.allSettled([
        api.health(),
        api.listAgents(),
        api.getDataSources(),
        api.getIncidents(),
      ])
      if (cancelled) return

      const failures: string[] = []
      if (h.status === "fulfilled") {
        setHealth(h.value)
      } else {
        failures.push(errorMessage(h.reason))
      }
      if (a.status === "fulfilled") {
        setAgents(a.value)
      } else {
        failures.push(errorMessage(a.reason))
      }
      if (d.status === "fulfilled") {
        setDataSources(d.value)
      } else {
        failures.push(errorMessage(d.reason))
      }
      // 事件列表只是概览指标，取不到时留「—」，不把看板顶成错误态。
      if (i.status === "fulfilled") {
        setIncidentCount(i.value.count)
      }
      setError(failures.length ? failures.join("; ") : null)
    }
    load()
    const id = setInterval(load, 15000)
    return () => {
      cancelled = true
      clearInterval(id)
    }
  }, [])

  const hasData = !!(health || agents || dataSources)
  const badge = health && !error
    ? ({ variant: "success", label: "系统运行正常" } as const)
    : hasData && error
      ? ({ variant: "warning", label: "数据可能过期" } as const)
      : error
        ? ({ variant: "danger", label: "连接失败" } as const)
        : ({ variant: "secondary", label: "connecting" } as const)

  const agentCount =
    agents?.count ?? health?.data?.agents_registered ?? "—"

  const details = agents?.details ?? []
  const coordinator = details.find((a) => a.type === "coordinator")
  const specialists = details
    .filter((a) => a.type !== "coordinator")
    .sort((a, b) => agentRank(a.type) - agentRank(b.type))

  return (
    <div className="space-y-6">
      <header className="flex items-center justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">仪表盘</h1>
          <p className="text-[13px] text-text-secondary">实时运维智能概览</p>
        </div>
        <div className="flex items-center gap-3">
          <Badge variant={badge.variant} className="px-3 py-1.5">
            <span className="mr-2 inline-block h-2 w-2 animate-pulse rounded-full bg-current" />
            {badge.label}
          </Badge>
          <div className="hidden text-right sm:block">
            <div className="text-[11px] text-text-tertiary">最后心跳</div>
            <div className="text-[13px] font-medium text-foreground">
              {timeAgo(health?.timestamp)}
            </div>
          </div>
        </div>
      </header>

      {error && (
        <Card className="border-danger/40">
          <CardContent className="py-3 text-[13px] text-danger">
            上次刷新失败，当前展示的数据可能已过期: {error}
          </CardContent>
        </Card>
      )}

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <KpiCard
          icon={Activity}
          label="系统状态"
          value={health?.status ?? "—"}
          hint={`v${health?.version ?? "—"}`}
        />
        <KpiCard
          icon={Bot}
          label="注册 Agent"
          value={String(agentCount)}
          hint="1 个调度 + 4 个分析智能体"
        />
        <KpiCard
          icon={Database}
          label="数据源"
          value={
            dataSources ? `${dataSources.real_count}/${dataSources.total} 在线` : "—"
          }
          hint="覆盖日志、指标、链路、拓扑、变更"
        />
        <KpiCard
          icon={AlertTriangle}
          label="事件"
          value={incidentCount == null ? "—" : String(incidentCount)}
          hint="告警与事件记录"
        />
      </div>

      <div className="grid gap-6 lg:grid-cols-2">
        <Card>
          <CardHeader className="flex flex-row items-start justify-between gap-3 space-y-0">
            <div className="space-y-1.5">
              <CardTitle>RCA 引擎评分权重</CardTitle>
              <CardDescription>多维度根因分析 · 引擎内置权重</CardDescription>
            </div>
            <span className="shrink-0 rounded-md bg-primary-soft px-2.5 py-1 text-[11px] font-medium text-primary">
              权重合计 100%
            </span>
          </CardHeader>
          <CardContent className="space-y-3">
            {[
              ["时间相关性", "50%", "时序对齐与趋势相关性"],
              ["拓扑传播", "25%", "服务依赖上下游分析"],
              ["变更临近度", "15%", "CI/CD 与配置变更关联"],
              ["指标严重度", "5%", "指标异常严重程度"],
              ["错误频率", "3%", "错误发生频率"],
              ["追踪证据", "2%", "调用链追踪证据"],
            ].map(([name, weight, desc]) => (
              <div key={name} className="flex items-center gap-4">
                <div className="w-28 text-sm font-medium">{name}</div>
                <div className="h-2 flex-1 overflow-hidden rounded-full bg-subtle">
                  <div
                    className="h-full rounded-full bg-primary"
                    style={{ width: weight }}
                  />
                </div>
                <div className="w-12 text-right text-xs text-text-secondary">
                  {weight}
                </div>
                <div className="flex-1 text-xs text-text-tertiary">{desc}</div>
              </div>
            ))}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Multi-Agent 协作</CardTitle>
            <CardDescription>智能体分工与证据共享</CardDescription>
          </CardHeader>
          <CardContent>
            {details.length === 0 ? (
              <div className="rounded-md border border-border bg-subtle p-3 text-xs text-text-secondary">
                {agents ? "暂无智能体数据" : "—"}
              </div>
            ) : (
              <>
                {coordinator && (
                  <div className="mx-auto w-full max-w-[260px] rounded-lg border border-primary-border bg-primary-soft px-4 py-3 text-center shadow-sm">
                    <div className="flex items-center justify-center gap-2">
                      <span className="text-sm font-semibold text-foreground">
                        {coordinator.name}
                      </span>
                      <span className="h-1.5 w-1.5 rounded-full bg-success" />
                    </div>
                    <div className="mt-0.5 text-[11px] text-text-secondary">
                      {coordinator.description || "任务调度、委派与结论汇总"}
                    </div>
                  </div>
                )}

                {coordinator && specialists.length > 0 && (
                  <div className="mx-auto h-5 w-px bg-border" aria-hidden="true" />
                )}

                {specialists.length > 0 && (
                  <div className="relative">
                    <div
                      className="absolute left-[12.5%] right-[12.5%] top-0 h-px bg-border"
                      aria-hidden="true"
                    />
                    <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
                      {specialists.map((agent) => (
                        <div key={agent.id} className="relative pt-5">
                          <div
                            className="absolute left-1/2 top-0 h-5 w-px -translate-x-1/2 bg-border"
                            aria-hidden="true"
                          />
                          <div className="rounded-lg border border-border bg-surface px-3 py-2.5">
                            <div className="flex items-center justify-between gap-2">
                              <span className="truncate text-[13px] font-medium text-foreground">
                                {agent.name}
                              </span>
                              <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-success" />
                            </div>
                            <div className="mt-0.5 truncate text-[11px] text-text-tertiary">
                              {agentCaption(agent)}
                            </div>
                          </div>
                        </div>
                      ))}
                    </div>
                  </div>
                )}

                <div className="mt-4 flex items-center justify-between border-t border-border pt-3 text-xs">
                  <span className="flex items-center gap-1.5 text-text-secondary">
                    <span className="h-1.5 w-1.5 rounded-full bg-success" />
                    {details.length} 个智能体在线
                  </span>
                  <Link
                    href="/agents"
                    className="font-medium text-primary hover:underline"
                  >
                    管理 Agent
                  </Link>
                </div>
              </>
            )}
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardContent className="flex flex-wrap items-center gap-3 py-4">
          <span className="mr-2 text-sm font-semibold text-foreground">快速开始</span>
          <Link
            href="/rca/new"
            className="flex items-center gap-1.5 rounded-md bg-primary px-4 py-2 text-[13px] font-medium text-primary-foreground transition-colors hover:bg-primary-hover"
          >
            <Activity className="h-3.5 w-3.5" />
            发起 RCA 分析
          </Link>
          <Link
            href="/chat"
            className="flex items-center gap-1.5 rounded-md border border-border px-4 py-2 text-[13px] font-medium text-foreground transition-colors hover:bg-subtle"
          >
            <Bot className="h-3.5 w-3.5" />
            打开对话助手
          </Link>
          <Link
            href="/incidents"
            className="flex items-center gap-1.5 rounded-md border border-border px-4 py-2 text-[13px] font-medium text-foreground transition-colors hover:bg-subtle"
          >
            <GitBranch className="h-3.5 w-3.5" />
            查看事件列表
          </Link>
        </CardContent>
      </Card>
    </div>
  )
}

function KpiCard({
  icon: Icon,
  label,
  value,
  hint,
}: {
  icon: LucideIcon
  label: string
  value: string
  hint?: string
}) {
  return (
    <Card>
      <CardContent className="flex items-start gap-3 py-4">
        <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-primary-soft text-primary">
          <Icon className="h-4 w-4" />
        </span>
        <div className="min-w-0">
          <div className="text-xs text-text-secondary">{label}</div>
          <div className="mt-0.5 truncate text-2xl font-bold capitalize text-foreground">
            {value}
          </div>
          {hint && (
            <div className="mt-0.5 truncate text-[11px] text-text-tertiary">
              {hint}
            </div>
          )}
        </div>
      </CardContent>
    </Card>
  )
}
