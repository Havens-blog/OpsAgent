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
import { api, type HealthResponse, type AgentInfo } from "@/lib/api"
import { timeAgo } from "@/lib/utils"

export default function DashboardPage() {
  const [health, setHealth] = useState<HealthResponse | null>(null)
  const [agents, setAgents] = useState<AgentInfo | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    async function load() {
      try {
        const [h, a] = await Promise.all([
          api.health().catch(() => null),
          api.listAgents().catch(() => null),
        ])
        if (cancelled) return
        setHealth(h)
        setAgents(a)
        setError(null)
      } catch (e) {
        setError(e instanceof Error ? e.message : "Failed to load")
      }
    }
    load()
    const id = setInterval(load, 15000)
    return () => {
      cancelled = true
      clearInterval(id)
    }
  }, [])

  return (
    <div className="space-y-8">
      <header className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Dashboard</h1>
          <p className="text-muted-foreground">
            实时运维智能概览
          </p>
        </div>
        <Badge
          variant={health?.status === "healthy" ? "success" : "secondary"}
          className="px-4 py-2"
        >
          <span className="mr-2 inline-block h-2 w-2 animate-pulse rounded-full bg-current" />
          {health?.status ?? "connecting"}
        </Badge>
      </header>

      {error && (
        <Card className="border-destructive/50">
          <CardContent className="py-4 text-sm text-destructive">
            后端连接失败: {error}。请确认 OpsAgent API 服务已启动 (默认 :8080)。
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
          value={String(agents?.count ?? health?.data?.agents_registered ?? 0)}
          hint="coordinator + 分析智能体"
        />
        <KpiCard
          label="数据源"
          value="5"
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
            <CardDescription>多维度根因分析</CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            {[
              ["时间相关性", "50%", "基于时序对齐与趋势相关性"],
              ["拓扑传播", "25%", "服务依赖上下游分析"],
              ["变更临近度", "15%", "CI/CD 与配置变更关联"],
              ["周期性 + Hint", "10%", "周期模式与人工提示"],
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
              {[
                ["Coordinator", "任务调度 · 结论汇总"],
                ["LogAnalyst", "日志模式识别"],
                ["Monitor", "指标异常 · RCA"],
                ["Inspector", "变更关联 · 巡检"],
              ].map(([name, role]) => (
                <div
                  key={name}
                  className="rounded-md border bg-accent/30 p-3"
                >
                  <div className="text-sm font-semibold">{name}</div>
                  <div className="text-xs text-muted-foreground">{role}</div>
                </div>
              ))}
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
