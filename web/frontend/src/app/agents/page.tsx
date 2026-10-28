"use client"

import { useEffect, useRef, useState } from "react"
import {
  Bot,
  Play,
  Loader2,
  AlertCircle,
  CheckCircle2,
  ShieldAlert,
  ThumbsUp,
  ThumbsDown,
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
  type AgentTask,
  type AgentTaskStatus,
} from "@/lib/api"

// Human-friendly role labels keyed by backend agent id.
const ROLE_LABELS: Record<string, string> = {
  coordinator: "任务调度 · 委派与结论汇总",
  log_analyst: "日志模式识别 · 错误关联分析",
  monitor: "APM 指标分析 · 根因分析",
  inspector: "云资源巡检 · 合规检查",
}

// Coordinator does static routing (no LLM); the specialists call the LLM.
const LLM_BACKED = new Set(["log_analyst", "monitor", "inspector"])

const STATUS_LABELS: Record<AgentTaskStatus, string> = {
  pending: "排队中",
  running: "执行中",
  awaiting_approval: "待审批",
  completed: "已完成",
  failed: "失败",
  rejected: "已拒绝",
}

function statusVariant(status: AgentTaskStatus) {
  switch (status) {
    case "completed":
      return "success" as const
    case "awaiting_approval":
      return "warning" as const
    case "failed":
    case "rejected":
      return "danger" as const
    default:
      return "secondary" as const
  }
}

function isSettled(status: AgentTaskStatus) {
  return (
    status === "completed" ||
    status === "failed" ||
    status === "rejected" ||
    status === "awaiting_approval"
  )
}

export default function AgentsPage() {
  const [details, setDetails] = useState<AgentDetail[]>([])
  const [loadErr, setLoadErr] = useState<string | null>(null)

  // Execution panel state.
  const [selected, setSelected] = useState<string>("")
  const [task, setTask] = useState<string>("")
  const [clientTaskId, setClientTaskId] = useState<string>("")
  const [busy, setBusy] = useState(false)
  const [taskRecord, setTaskRecord] = useState<AgentTask | null>(null)
  const [execErr, setExecErr] = useState<string | null>(null)
  const runSeq = useRef(0)

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
            const first =
              i.details.find((d) => LLM_BACKED.has(d.id)) || i.details[0]
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

  const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms))

  const pollTask = async (taskId: string, seq: number) => {
    for (let i = 0; i < 60; i++) {
      if (seq !== runSeq.current) return
      try {
        const { task: t } = await api.getAgentTask(taskId)
        if (seq !== runSeq.current) return
        setTaskRecord(t)
        if (isSettled(t.status)) return
      } catch (e) {
        if (seq !== runSeq.current) return
        setExecErr(e instanceof Error ? e.message : String(e))
        return
      }
      await sleep(1500)
    }
    if (seq === runSeq.current) setExecErr("任务轮询超时")
  }

  const runTask = async () => {
    if (!selected || !task.trim() || busy) return
    const seq = ++runSeq.current
    setBusy(true)
    setExecErr(null)
    setTaskRecord(null)

    // Reuse the client_task_id across retries/duplicate clicks so the backend
    // deduplicates the submission; a new description resets it.
    const cid = clientTaskId || crypto.randomUUID()
    setClientTaskId(cid)

    try {
      const res = await api.submitAgentTask(selected, {
        description: task.trim(),
        client_task_id: cid,
      })
      if (seq !== runSeq.current) return
      await pollTask(res.task.id, seq)
    } catch (e) {
      if (seq !== runSeq.current) return
      setExecErr(e instanceof Error ? e.message : String(e))
    } finally {
      if (seq === runSeq.current) setBusy(false)
    }
  }

  const handleApproval = async (action: "approve" | "reject") => {
    if (!taskRecord?.approval_id || busy) return
    const seq = ++runSeq.current
    setBusy(true)
    setExecErr(null)

    try {
      if (action === "approve") {
        await api.approveApproval(taskRecord.approval_id)
      } else {
        await api.rejectApproval(taskRecord.approval_id)
      }
      if (seq !== runSeq.current) return
      await pollTask(taskRecord.id, seq)
    } catch (e) {
      if (seq !== runSeq.current) return
      setExecErr(e instanceof Error ? e.message : String(e))
    } finally {
      if (seq === runSeq.current) setBusy(false)
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
        <div className="flex items-center gap-2 rounded-md border border-status-unhealthy/40 bg-status-unhealthy/10 p-3 text-sm text-status-unhealthy">
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
            选择一个智能体并下发任务 — 任务异步执行，可实时跟踪状态
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
            onChange={(e) => {
              setTask(e.target.value)
              if (clientTaskId) setClientTaskId("")
            }}
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
              disabled={!selected || !task.trim() || busy}
              className="inline-flex items-center gap-2 rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground transition-opacity disabled:opacity-50"
            >
              {busy ? (
                <>
                  <Loader2 className="h-4 w-4 animate-spin" />
                  处理中…
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
            <div className="flex items-center gap-2 rounded-md border border-status-unhealthy/40 bg-status-unhealthy/10 p-3 text-sm text-status-unhealthy">
              <AlertCircle className="h-4 w-4" />
              执行失败：{execErr}
            </div>
          )}

          {taskRecord && (
            <div className="space-y-3 rounded-md border bg-accent/20 p-4">
              <div className="flex flex-wrap items-center gap-3 text-xs">
                <Badge variant={statusVariant(taskRecord.status)}>
                  {STATUS_LABELS[taskRecord.status]}
                </Badge>
                {taskRecord.client_task_id && (
                  <span className="text-muted-foreground">
                    client_task_id{" "}
                    <span className="font-mono">{taskRecord.client_task_id}</span>
                  </span>
                )}
                {typeof taskRecord.result?.data?.model === "string" && (
                  <span className="text-muted-foreground">
                    模型{" "}
                    <span className="font-mono">
                      {String(taskRecord.result?.data?.model)}
                    </span>
                  </span>
                )}
                {typeof taskRecord.result?.data?.provider === "string" && (
                  <span className="text-muted-foreground">
                    provider{" "}
                    <span className="font-mono">
                      {String(taskRecord.result?.data?.provider)}
                    </span>
                  </span>
                )}
                {taskRecord.result?.duration_ms != null && (
                  <span className="text-muted-foreground">
                    耗时{" "}
                    <span className="font-mono">
                      {taskRecord.result.duration_ms}ms
                    </span>
                  </span>
                )}
              </div>

              {(taskRecord.status === "pending" ||
                taskRecord.status === "running") && (
                <div className="flex items-center gap-2 text-sm text-muted-foreground">
                  <Loader2 className="h-4 w-4 animate-spin" />
                  {STATUS_LABELS[taskRecord.status]}
                </div>
              )}

              {taskRecord.status === "awaiting_approval" &&
                taskRecord.approval_id && (
                  <div className="space-y-3">
                    <div className="flex items-center gap-2 text-sm text-status-degraded">
                      <ShieldAlert className="h-4 w-4" />
                      该任务需要审批，请选择批准或拒绝
                    </div>
                    <div className="flex gap-2">
                      <button
                        onClick={() => handleApproval("approve")}
                        disabled={busy}
                        className="inline-flex items-center gap-2 rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground transition-opacity disabled:opacity-50"
                      >
                        <ThumbsUp className="h-4 w-4" />
                        批准
                      </button>
                      <button
                        onClick={() => handleApproval("reject")}
                        disabled={busy}
                        className="inline-flex items-center gap-2 rounded-md border border-status-unhealthy/40 bg-status-unhealthy/10 px-4 py-2 text-sm font-medium text-status-unhealthy transition-opacity disabled:opacity-50"
                      >
                        <ThumbsDown className="h-4 w-4" />
                        拒绝
                      </button>
                    </div>
                  </div>
                )}

              {taskRecord.status === "completed" && taskRecord.result && (
                <div className="space-y-2">
                  <div className="flex items-center gap-2 text-sm text-status-healthy">
                    <CheckCircle2 className="h-4 w-4" />
                    执行完成
                  </div>
                  <div className="whitespace-pre-wrap text-sm leading-relaxed">
                    {taskRecord.result.response}
                  </div>
                  {taskRecord.result.error && (
                    <div className="text-xs text-status-unhealthy">
                      {taskRecord.result.error}
                    </div>
                  )}
                </div>
              )}

              {(taskRecord.status === "failed" ||
                taskRecord.status === "rejected") && (
                <div className="flex items-start gap-2 text-sm text-status-unhealthy">
                  <AlertCircle className="h-4 w-4 shrink-0" />
                  <span>
                    {taskRecord.status === "rejected" ? "任务已被拒绝" : "任务失败"}
                    {taskRecord.error ? `：${taskRecord.error}` : ""}
                  </span>
                </div>
              )}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
