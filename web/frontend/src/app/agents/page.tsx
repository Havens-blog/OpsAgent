"use client"

import { useEffect, useState } from "react"
import { Bot, Play, Loader2, AlertCircle, CheckCircle2 } from "lucide-react"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { api, type AgentDetail, type AgentExecResult } from "@/lib/api"

// Human-friendly role labels keyed by backend agent id.
const ROLE_LABELS: Record<string, string> = {
  coordinator: "任务调度 · 委派与结论汇总",
  log_analyst: "日志模式识别 · 错误关联分析",
  monitor: "APM 指标分析 · 根因分析",
  inspector: "云资源巡检 · 合规检查",
}

// Coordinator does static routing (no LLM); the specialists call the LLM.
const LLM_BACKED = new Set(["log_analyst", "monitor", "inspector"])

export default function AgentsPage() {
  const [details, setDetails] = useState<AgentDetail[]>([])
  const [loadErr, setLoadErr] = useState<string | null>(null)

  // Execution panel state.
  const [selected, setSelected] = useState<string>("")
  const [task, setTask] = useState<string>("")
  const [running, setRunning] = useState(false)
  const [result, setResult] = useState<AgentExecResult | null>(null)
  const [execErr, setExecErr] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    const load = () => {
      api
        .listAgents()
        .then((i) => {
          if (cancelled) return
          setDetails(i.details || [])
          setLoadErr(null)
          if (!selected && i.details && i.details.length > 0) {
            // Default to the first LLM-backed specialist.
            const first = i.details.find((d) => LLM_BACKED.has(d.id)) || i.details[0]
            setSelected(first.id)
          }
        })
        .catch((e) => {
          if (!cancelled) setLoadErr(e?.message || "无法连接后端")
        })
    }
    load()
    const id = setInterval(load, 15000)
    return () => {
      cancelled = true
      clearInterval(id)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const runTask = async () => {
    if (!selected || !task.trim() || running) return
    setRunning(true)
    setResult(null)
    setExecErr(null)
    try {
      const r = await api.executeAgent(selected, task.trim())
      setResult(r)
    } catch (e) {
      setExecErr(e instanceof Error ? e.message : String(e))
    } finally {
      setRunning(false)
    }
  }

  return (
    <div className="space-y-8">
      <header>
        <h1 className="text-3xl font-bold tracking-tight">Agents</h1>
        <p className="text-muted-foreground">
          多智能体协作 — 实时注册状态与任务执行
        </p>
      </header>

      {loadErr && (
        <div className="flex items-center gap-2 rounded-md border border-status-critical/40 bg-status-critical/10 p-3 text-sm text-status-critical">
          <AlertCircle className="h-4 w-4" />
          后端连接失败：{loadErr}
        </div>
      )}

      <div className="grid gap-4 md:grid-cols-3">
        <Card>
          <CardContent className="py-4">
            <div className="text-xs uppercase tracking-wide text-muted-foreground">
              已注册智能体
            </div>
            <div className="mt-1 text-2xl font-bold">{details.length}</div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="py-4">
            <div className="text-xs uppercase tracking-wide text-muted-foreground">
              LLM 驱动
            </div>
            <div className="mt-1 text-2xl font-bold">
              {details.filter((d) => LLM_BACKED.has(d.id)).length}
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="py-4">
            <div className="text-xs uppercase tracking-wide text-muted-foreground">
              调度器
            </div>
            <div className="mt-1 text-2xl font-bold">
              {details.some((d) => d.id === "coordinator") ? "就绪" : "—"}
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Agent fleet — live from backend */}
      <div className="grid gap-4 md:grid-cols-2">
        {details.map((agent) => (
          <Card
            key={agent.id}
            className={
              selected === agent.id
                ? "ring-2 ring-primary cursor-pointer"
                : "cursor-pointer hover:border-primary/50"
            }
            onClick={() => setSelected(agent.id)}
          >
            <CardHeader>
              <div className="flex items-center justify-between">
                <CardTitle className="flex items-center gap-2 text-base">
                  <Bot className="h-4 w-4" />
                  {agent.name}
                </CardTitle>
                <Badge variant="success">registered</Badge>
              </div>
              <CardDescription>
                {ROLE_LABELS[agent.id] || agent.description}
              </CardDescription>
            </CardHeader>
            <CardContent>
              <div className="flex items-center justify-between text-sm">
                <span className="text-muted-foreground">类型</span>
                <Badge variant="outline">{agent.type}</Badge>
              </div>
              <div className="mt-2 flex items-center justify-between text-sm">
                <span className="text-muted-foreground">执行方式</span>
                <span className="font-mono text-xs">
                  {LLM_BACKED.has(agent.id) ? "LLM 实时调用" : "静态路由"}
                </span>
              </div>
              <div className="mt-3 flex flex-wrap gap-1">
                {agent.capabilities.map((c) => (
                  <Badge key={c} variant="secondary" className="text-[10px]">
                    {c}
                  </Badge>
                ))}
              </div>
            </CardContent>
          </Card>
        ))}
      </div>

      {/* Execution panel */}
      <Card>
        <CardHeader>
          <CardTitle>任务执行</CardTitle>
          <CardDescription>
            选择一个智能体并下发任务 — 专家智能体会实时调用大模型
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex flex-wrap gap-2">
            {details.map((d) => (
              <button
                key={d.id}
                onClick={() => setSelected(d.id)}
                className={`rounded-md border px-3 py-1.5 text-sm transition-colors ${
                  selected === d.id
                    ? "border-primary bg-primary/10 text-primary"
                    : "border-border hover:border-primary/50"
                }`}
              >
                {d.name}
              </button>
            ))}
          </div>

          <textarea
            value={task}
            onChange={(e) => setTask(e.target.value)}
            placeholder="描述要执行的任务，例如：分析 order-service 最近出现的 connection pool exhausted 错误，给出排查方向。"
            rows={3}
            className="w-full resize-y rounded-md border border-border bg-background p-3 text-sm outline-none focus:border-primary"
            onKeyDown={(e) => {
              if ((e.metaKey || e.ctrlKey) && e.key === "Enter") runTask()
            }}
          />

          <div className="flex items-center justify-between">
            <span className="text-xs text-muted-foreground">
              {selected ? `目标：${selected}` : "请选择一个智能体"}
              {LLM_BACKED.has(selected) && " · Ctrl/⌘+Enter 执行"}
            </span>
            <button
              onClick={runTask}
              disabled={!selected || !task.trim() || running}
              className="inline-flex items-center gap-2 rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground transition-opacity disabled:opacity-50"
            >
              {running ? (
                <>
                  <Loader2 className="h-4 w-4 animate-spin" />
                  执行中…
                </>
              ) : (
                <>
                  <Play className="h-4 w-4" />
                  执行任务
                </>
              )}
            </button>
          </div>

          {execErr && (
            <div className="flex items-center gap-2 rounded-md border border-status-critical/40 bg-status-critical/10 p-3 text-sm text-status-critical">
              <AlertCircle className="h-4 w-4" />
              执行失败：{execErr}
            </div>
          )}

          {result && (
            <div className="space-y-3 rounded-md border bg-accent/20 p-4">
              <div className="flex flex-wrap items-center gap-3 text-xs">
                <Badge variant={result.success ? "success" : "secondary"}>
                  {result.success ? (
                    <span className="flex items-center gap-1">
                      <CheckCircle2 className="h-3 w-3" /> success
                    </span>
                  ) : (
                    "failed"
                  )}
                </Badge>
                {typeof result.data?.model === "string" && result.data.model && (
                  <span className="text-muted-foreground">
                    模型 <span className="font-mono">{String(result.data.model)}</span>
                  </span>
                )}
                {typeof result.data?.provider === "string" && result.data.provider && (
                  <span className="text-muted-foreground">
                    provider <span className="font-mono">{String(result.data.provider)}</span>
                  </span>
                )}
                <span className="text-muted-foreground">
                  耗时 <span className="font-mono">{result.duration_ms}ms</span>
                </span>
              </div>
              <div className="whitespace-pre-wrap text-sm leading-relaxed">
                {result.response}
              </div>
              {result.error && (
                <div className="text-xs text-status-critical">{result.error}</div>
              )}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
