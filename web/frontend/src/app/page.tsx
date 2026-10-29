"use client"

import { useEffect, useState } from "react"
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
  type HealthResponse,
  type AgentInfo,
  type DataSourceStatus,
} from "@/lib/api"
import { timeAgo } from "@/lib/utils"

function errorMessage(e: unknown): string {
  return e instanceof Error ? e.message : String(e)
}

export default function DashboardPage() {
  const [health, setHealth] = useState<HealthResponse | null>(null)
  const [agents, setAgents] = useState<AgentInfo | null>(null)
  const [dataSources, setDataSources] = useState<DataSourceStatus | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    async function load() {
      const [h, a, d] = await Promise.allSettled([
        api.health(),
        api.listAgents(),
        api.getDataSources(),
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
    ? ({ variant: "success", label: health.status } as const)
    : hasData && error
      ? ({ variant: "warning", label: "数据可能过期" } as const)
      : error
        ? ({ variant: "danger", label: "连接失败" } as const)
        : ({ variant: "secondary", label: "connecting" } as const)

  const agentCount =
    agents?.count ?? health?.data?.agents_registered ?? "—"

  return (
    <div className="space-y-8">
      <header className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Dashboard</h1>
          <p className="text-muted-foreground">
            实时运维智能概览
          </p>
        </div>
        <Badge variant={badge.variant} className="px-4 py-2">
          <span className="mr-2 inline-block h-2 w-2 animate-pulse rounded-full bg-current" />
          {badge.label}
        </Badge>
      </header>

      {error && (
        <Card className="border-destructive/50">
          <CardContent className="py-4 text-sm text-destructive">
            上次刷新失败，当前展示的数据可能已过期: {error}
          </CardContent>
        </Card>
      )}

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        <KpiCard
          label="系统状态"
          value={health?.status ?? "—"}
          hint={`v${health?.version ?? "—"}`}
        />
        <KpiCard
          label="注册 Agent"
          value={String(agentCount)}
          hint="coordinator + 分析智能体"
        />
        <KpiCard
          label="数据源"
          value={
            dataSources ? `${dataSources.real_count}/${dataSources.total}` : "—"
          }
          hint="Logs · Metrics · Trace · Topology · Changes"
        />
        <KpiCard
          label="最后心跳"
          value={timeAgo(health?.timestamp)}
          hint={health?.timestamp ? "实时刷新" : "—"}
        />
      </div>

      <div className="grid gap-6 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>RCA 引擎能力</CardTitle>
            <CardDescription>多维度根因分析 · 引擎内置评分权重</CardDescription>
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
                <div className="h-2 flex-1 overflow-hidden rounded-full bg-muted">
                  <div
                    className="h-full rounded-full bg-primary"
                    style={{ width: weight }}
                  />
                </div>
                <div className="w-12 text-right text-xs text-muted-foreground">
                  {weight}
                </div>
                <div className="flex-1 text-xs text-muted-foreground">
                  {desc}
                </div>
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
            <div className="grid grid-cols-2 gap-3">
              {agents?.details && agents.details.length > 0 ? (
                agents.details.map((agent) => (
                  <div
                    key={agent.id}
                    className="rounded-md border bg-accent/30 p-3"
                  >
                    <div className="text-sm font-semibold">{agent.name}</div>
                    <div className="text-xs text-muted-foreground">
                      {agent.description || agent.capabilities?.[0] || "—"}
                    </div>
                  </div>
                ))
              ) : (
                <div className="col-span-2 rounded-md border bg-accent/30 p-3 text-xs text-muted-foreground">
                  {agents ? "暂无数据" : "—"}
                </div>
              )}
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  )
}

function KpiCard({
  label,
  value,
  hint,
}: {
  label: string
  value: string
  hint?: string
}) {
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardDescription className="text-xs uppercase tracking-wide">
          {label}
        </CardDescription>
      </CardHeader>
      <CardContent>
        <div className="text-2xl font-bold capitalize">{value}</div>
        {hint && (
          <div className="mt-1 text-xs text-muted-foreground">{hint}</div>
        )}
      </CardContent>
    </Card>
  )
}
