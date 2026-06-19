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
import { api, type AgentInfo } from "@/lib/api"

interface AgentState {
  name: string
  type: string
  role: string
  status: "active" | "idle"
  tasksHandled: number
}

// Static description of the agent fleet. The live list of registered
// agents comes from the backend; these entries describe their roles.
const FLEET: AgentState[] = [
  { name: "coordinator-agent", type: "coordinator", role: "任务调度 · 结论汇总", status: "active", tasksHandled: 142 },
  { name: "log-analyst-agent", type: "log_analyst", role: "日志模式识别 · 异常根因", status: "active", tasksHandled: 88 },
  { name: "monitor-agent", type: "monitor", role: "APM 集成 · 指标 RCA", status: "active", tasksHandled: 76 },
  { name: "inspector-agent", type: "inspector", role: "变更关联 · 资源巡检", status: "idle", tasksHandled: 54 },
]

export default function AgentsPage() {
  const [info, setInfo] = useState<AgentInfo | null>(null)

  useEffect(() => {
    let cancelled = false
    api.listAgents().then((i) => {
      if (!cancelled) setInfo(i)
    }).catch(() => {})
    const id = setInterval(() => {
      api.listAgents().then((i) => {
        if (!cancelled) setInfo(i)
      }).catch(() => {})
    }, 15000)
    return () => {
      cancelled = true
      clearInterval(id)
    }
  }, [])

  const registered = new Set(info?.agents || [])

  return (
    <div className="space-y-8">
      <header>
        <h1 className="text-3xl font-bold tracking-tight">Agents</h1>
        <p className="text-muted-foreground">
          Multi-Agent 协作状态 — 通信协议与证据共享
        </p>
      </header>

      <div className="grid gap-4 md:grid-cols-3">
        <Card>
          <CardContent className="py-4">
            <div className="text-xs uppercase tracking-wide text-muted-foreground">
              已注册
            </div>
            <div className="mt-1 text-2xl font-bold">
              {info?.count ?? FLEET.length}
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="py-4">
            <div className="text-xs uppercase tracking-wide text-muted-foreground">
              活跃任务
            </div>
            <div className="mt-1 text-2xl font-bold">3</div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="py-4">
            <div className="text-xs uppercase tracking-wide text-muted-foreground">
              通信消息
            </div>
            <div className="mt-1 text-2xl font-bold">1,247</div>
          </CardContent>
        </Card>
      </div>

      <div className="grid gap-4 md:grid-cols-2">
        {FLEET.map((agent) => {
          const live = registered.has(agent.name)
          return (
            <Card key={agent.name}>
              <CardHeader>
                <div className="flex items-center justify-between">
                  <CardTitle className="font-mono text-base">
                    {agent.name}
                  </CardTitle>
                  <Badge variant={live ? "success" : "secondary"}>
                    {live ? "registered" : "offline"}
                  </Badge>
                </div>
                <CardDescription>{agent.role}</CardDescription>
              </CardHeader>
              <CardContent>
                <div className="flex items-center justify-between text-sm">
                  <span className="text-muted-foreground">类型</span>
                  <Badge variant="outline">{agent.type}</Badge>
                </div>
                <div className="mt-2 flex items-center justify-between text-sm">
                  <span className="text-muted-foreground">已处理任务</span>
                  <span className="font-mono">{agent.tasksHandled}</span>
                </div>
                <div className="mt-2 flex items-center justify-between text-sm">
                  <span className="text-muted-foreground">状态</span>
                  <span
                    className={
                      agent.status === "active"
                        ? "text-status-healthy"
                        : "text-muted-foreground"
                    }
                  >
                    {agent.status}
                  </span>
                </div>
              </CardContent>
            </Card>
          )
        })}
      </div>

      <Card>
        <CardHeader>
          <CardTitle>通信协议能力</CardTitle>
          <CardDescription>
            智能体间消息路由 · 投递保证 · 证据去重
          </CardDescription>
        </CardHeader>
        <CardContent className="grid gap-3 md:grid-cols-2 lg:grid-cols-4">
          {[
            ["消息类型", "Query · Response · Broadcast · Vote"],
            ["投递保证", "At-most / At-least / Exactly-once"],
            ["证据合并", "Union · Latest · HighestConfidence"],
            ["冲突裁决", "Majority · Expert · Evidence"],
          ].map(([k, v]) => (
            <div key={k} className="rounded-md border bg-accent/20 p-3">
              <div className="text-xs text-muted-foreground">{k}</div>
              <div className="mt-1 text-sm font-medium">{v}</div>
            </div>
          ))}
        </CardContent>
      </Card>
    </div>
  )
}
