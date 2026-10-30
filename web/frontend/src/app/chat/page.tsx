"use client"

import Link from "next/link"
import { useEffect, useRef, useState } from "react"
import {
  AlertCircle,
  Bot,
  Check,
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  Copy,
  Database,
  ExternalLink,
  Loader2,
  MessageSquare,
  Paperclip,
  Pencil,
  Plus,
  RefreshCw,
  Search,
  Send,
  ShieldAlert,
  Square,
  Trash2,
  User,
  X,
  XCircle,
} from "lucide-react"
import { cn } from "@/lib/utils"
import {
  api,
  ApiError,
  type Approval,
  type Conversation,
  type ConversationMessage,
  type ToolEvidence,
} from "@/lib/api"
import { Markdown } from "@/components/markdown"

interface ToolCall {
  callId: string
  name: string
  params: Record<string, unknown>
  status: "running" | "success" | "failed"
  result?: string
  error?: string
}

interface ApprovalState {
  id: string
  toolName: string
  params: Record<string, unknown>
  reason: string
  content: string
  clientMessageId?: string
  decision?: "approved" | "rejected"
  // 批准/拒绝请求进行中
  submitting?: boolean
  // 后端返回 409/410，审批已不可操作
  invalid?: boolean
  error?: string
}

interface UiMessage {
  id: string
  role: "user" | "assistant"
  content: string
  clientMessageId?: string
  streaming?: boolean
  error?: string
  toolCalls?: ToolCall[]
  approval?: ApprovalState
  evidence?: ToolEvidence[]
  grounding?: string
}

const SUGGESTED_PROMPTS = [
  "分析最近的 P1 告警",
  "查询 order-service 错误日志",
  "展示当前服务拓扑",
]

function toMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

function isAbortError(err: unknown): boolean {
  return (
    typeof err === "object" &&
    err !== null &&
    "name" in err &&
    (err as { name?: string }).name === "AbortError"
  )
}

/**
 * 把历史消息和未决审批合并成 UI 消息列表：
 * 审批卡片插在 client_message_id 匹配的用户消息之后，匹配不到时追加到末尾。
 */
function buildUiMessages(
  history: ConversationMessage[],
  pendingApprovals: Approval[]
): UiMessage[] {
  const result: UiMessage[] = history.map((m) => ({
    id: m.id,
    role: m.role,
    content: m.content,
    clientMessageId: m.client_message_id,
    evidence: m.evidence,
    grounding: m.grounding,
  }))

  for (const a of pendingApprovals) {
    const userIdx = a.client_message_id
      ? result.findIndex(
          (m) => m.role === "user" && m.clientMessageId === a.client_message_id
        )
      : -1
    const card: UiMessage = {
      id: `approval-${a.approval_id}`,
      role: "assistant",
      content: "",
      approval: {
        id: a.approval_id,
        toolName: a.tool_name,
        params: a.params ?? {},
        reason: a.reason,
        content: userIdx >= 0 ? result[userIdx].content : "",
        clientMessageId: a.client_message_id,
      },
    }
    if (userIdx < 0) {
      result.push(card)
      continue
    }
    // 同一条用户消息触发多条审批时，按 created_at 顺序排在已插入的卡片之后
    let insertAt = userIdx + 1
    while (
      insertAt < result.length &&
      result[insertAt].approval?.clientMessageId === a.client_message_id &&
      result[insertAt].id.startsWith("approval-")
    ) {
      insertAt++
    }
    result.splice(insertAt, 0, card)
  }
  return result
}

function formatTime(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ""
  return d.toLocaleString("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  })
}

// 会话列表右侧的时间：今天只显示时刻，更早显示月/日。
function formatConversationTime(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ""
  const startOfToday = new Date()
  startOfToday.setHours(0, 0, 0, 0)
  if (d.getTime() >= startOfToday.getTime()) {
    return d.toLocaleTimeString("zh-CN", {
      hour: "2-digit",
      minute: "2-digit",
      hour12: false,
    })
  }
  return d.toLocaleDateString("zh-CN", { month: "2-digit", day: "2-digit" })
}

// 会话列表的「今天 / 更早」分组（按 updated_at 是否落在今天）。
function groupByDay(convs: Conversation[]): { label: string; items: Conversation[] }[] {
  const startOfToday = new Date()
  startOfToday.setHours(0, 0, 0, 0)
  const today: Conversation[] = []
  const earlier: Conversation[] = []
  for (const c of convs) {
    const t = new Date(c.updated_at).getTime()
    if (!Number.isNaN(t) && t >= startOfToday.getTime()) today.push(c)
    else earlier.push(c)
  }
  const groups: { label: string; items: Conversation[] }[] = []
  if (today.length) groups.push({ label: "今天", items: today })
  if (earlier.length) groups.push({ label: "更早", items: earlier })
  return groups
}

const PROVIDER_LABELS: Record<string, string> = {
  aliyun: "阿里云",
  tencent: "腾讯云",
  huawei: "华为云",
  aws: "AWS",
  azure: "Azure",
  volcano: "火山云",
  volcengine: "火山云",
}

const ASSET_LABELS: Record<string, string> = {
  cdn: "CDN",
  ecs: "云服务器",
  oss: "对象存储",
  s3: "对象存储",
  rds: "云数据库",
  redis: "Redis",
  mongodb: "MongoDB",
  vpc: "专有网络",
  eip: "弹性公网IP",
  lb: "负载均衡",
  slb: "负载均衡",
  waf: "WAF",
  host: "主机",
  disk: "云盘",
  snapshot: "快照",
  eni: "弹性网卡",
  image: "镜像",
  subnet: "子网",
  k8s_cluster: "K8s 集群",
  k8s_deployment: "K8s Deployment",
  k8s_service: "K8s Service",
  k8s_pod: "K8s Pod",
  k8s_node: "K8s 节点",
}

const TOOL_LABELS: Record<string, string> = {
  list_cmdb_models: "CMDB 资产模型",
  list_cmdb_instances: "资源实例",
  resource_summary: "云资源概况",
  lookup_cmdb_domain: "域名源站",
  lookup_domain_backend: "域名后端",
  analyze_incident: "根因分析",
  query_metrics: "指标查询",
  query_logs: "日志查询",
  query_logs_advanced: "高级日志查询",
  query_traces: "调用链查询",
  get_topology: "服务拓扑",
  analyze_logs: "日志分析",
  correlate_logs: "日志关联",
  query_waf_logs: "WAF 拦截日志",
  run_inspection: "资源巡检",
  check_health: "健康检查",
  validate_compliance: "合规校验",
  restart_service: "重启服务",
}

function strParam(params: Record<string, unknown>, key: string): string {
  const v = params[key]
  return typeof v === "string" ? v : ""
}

function normalizeProvider(p: string): string {
  return p === "volcengine" ? "volcano" : p
}

function providerName(p: string): string {
  return PROVIDER_LABELS[normalizeProvider(p)] ?? p
}

function assetName(uid: string): string {
  return ASSET_LABELS[uid] ?? uid.toUpperCase()
}

// 结果卡片标题，如「火山云 CDN 实例」。
function resultTitle(toolName: string, params: Record<string, unknown>): string {
  if (toolName === "list_cmdb_instances") {
    const provider = providerName(strParam(params, "provider"))
    const asset = strParam(params, "model_uid") ? assetName(strParam(params, "model_uid")) : ""
    return [provider, asset].filter(Boolean).join(" ") + "实例"
  }
  if (toolName === "resource_summary") return "云资源概况"
  return TOOL_LABELS[toolName] ?? toolName
}

// 步骤行上的一行说明，如「查询火山云 CDN 实例」。
function toolSummary(toolName: string, params: Record<string, unknown>): string {
  switch (toolName) {
    case "list_cmdb_instances":
      return `查询${resultTitle(toolName, params)}`
    case "list_cmdb_models":
      return "查询 CMDB 资产模型"
    case "resource_summary":
      return "查询云资源概况"
    case "lookup_cmdb_domain":
    case "lookup_domain_backend": {
      const domain = strParam(params, "domain")
      return domain ? `解析域名 ${domain}` : "解析域名后端"
    }
    default:
      return TOOL_LABELS[toolName] ?? "工具调用"
  }
}

function parseResultObject(raw?: string): Record<string, unknown> | null {
  if (!raw) return null
  try {
    const v: unknown = JSON.parse(raw)
    if (v && typeof v === "object" && !Array.isArray(v)) {
      return v as Record<string, unknown>
    }
  } catch {
    // 非 JSON 结果（错误文本等）不渲染卡片
  }
  return null
}

// prettyJSON 格式化工具结果；非 JSON 原样返回并按长度截断。
function prettyJSON(raw: string): string {
  try {
    return JSON.stringify(JSON.parse(raw), null, 2)
  } catch {
    return raw.length > 2000 ? `${raw.slice(0, 2000)}…` : raw
  }
}

/**
 * 工具调用的结构化结果卡片：突出总数与首条示例，原始 JSON 收进「查看原始结果」，
 * 并提供跳去 CMDB 的入口。未识别的工具不渲染（避免退化成一大段 JSON）。
 */
function ToolResultCard({
  toolName,
  params,
  result,
}: {
  toolName: string
  params: Record<string, unknown>
  result?: string
}) {
  const [rawOpen, setRawOpen] = useState(false)
  const data = parseResultObject(result)
  if (!data) return null

  let count: number | null = null
  let countLabel = ""
  let returned: number | null = null
  let sample = ""
  let sampleKey = ""

  if (toolName === "list_cmdb_instances") {
    if (typeof data.total === "number") count = data.total
    countLabel = "条实例"
    const instances = Array.isArray(data.instances) ? data.instances : []
    if (typeof data.count === "number") returned = data.count
    else returned = instances.length
    const first = instances[0]
    if (first && typeof first === "object") {
      const name = (first as Record<string, unknown>).asset_name
      if (typeof name === "string" && name) {
        sample = name
        sampleKey = "asset_name"
      }
    }
  } else if (toolName === "resource_summary") {
    if (typeof data.total === "number") count = data.total
    countLabel = "个资源"
  } else {
    return null
  }

  return (
    <div className="mt-2 rounded-lg border border-border bg-background/60 px-3.5 py-3">
      <div className="flex items-start justify-between gap-4">
        <div className="min-w-0">
          <div className="flex items-center gap-1.5">
            <Database className="h-3.5 w-3.5 shrink-0 text-text-secondary" />
            <span className="text-[13px] font-medium text-foreground">
              {resultTitle(toolName, params)}
            </span>
          </div>
          {sample && (
            <div className="mt-1.5 flex min-w-0 items-center gap-2 text-xs">
              <span className="shrink-0 text-text-tertiary">首条示例</span>
              <code className="truncate font-mono text-text-regular">
                {sampleKey}: {sample}
              </code>
            </div>
          )}
        </div>
        {count != null && (
          <div className="flex shrink-0 flex-col items-end">
            <div className="flex items-baseline gap-1">
              <span className="text-2xl font-semibold tabular-nums text-foreground">
                {count}
              </span>
              <span className="text-xs text-text-secondary">{countLabel}</span>
            </div>
            {returned != null && returned !== count && (
              <span className="mt-0.5 text-[11px] text-text-tertiary">
                本次返回 {returned} 条
              </span>
            )}
          </div>
        )}
      </div>

      <div className="mt-3 flex items-center gap-3 border-t border-border pt-2.5 text-xs">
        <button
          type="button"
          onClick={() => setRawOpen((v) => !v)}
          className="text-primary hover:underline"
        >
          查看原始结果
        </button>
        <Link
          href="/topology"
          className="flex items-center gap-1 text-primary hover:underline"
        >
          在 CMDB 中查看
          <ExternalLink className="h-3 w-3" />
        </Link>
      </div>

      {rawOpen && (
        <pre className="mt-2 max-h-60 overflow-auto rounded-md bg-subtle px-2.5 py-2 font-mono text-[11px] leading-relaxed text-text-regular">
          {prettyJSON(result ?? "")}
        </pre>
      )}
    </div>
  )
}

/**
 * 工具调用组：头部只报「已执行 N 个工具」+ 总体状态，每一步一行；
 * 参数/结果 JSON 默认折叠进「展开详情」，需要追溯时再展开。
 */
function ToolCallGroup({ calls }: { calls: ToolCall[] }) {
  const [expanded, setExpanded] = useState<Record<string, boolean>>({})
  const running = calls.some((c) => c.status === "running")
  const failed = calls.some((c) => c.status === "failed")
  const badge = running ? "运行中" : failed ? "失败" : "成功"

  return (
    <div className="overflow-hidden rounded-lg border border-border bg-surface">
      <div className="flex items-center gap-2 border-b border-border px-3 py-2">
        <span className="text-[13px] font-medium text-foreground">
          已执行 {calls.length} 个工具
        </span>
        <span
          className={cn(
            "rounded-full px-2 py-0.5 text-[10px] font-medium",
            running && "bg-subtle text-text-secondary",
            !running && failed && "bg-danger-soft text-danger",
            !running && !failed && "bg-success-soft text-success"
          )}
        >
          {badge}
        </span>
      </div>

      <div className="divide-y divide-border">
        {calls.map((tc) => {
          const open = !!expanded[tc.callId]
          return (
            <div key={tc.callId} className="px-3 py-2.5">
              <div className="flex items-center gap-2">
                {tc.status === "running" ? (
                  <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin text-text-secondary" />
                ) : tc.status === "failed" ? (
                  <XCircle className="h-3.5 w-3.5 shrink-0 text-danger" />
                ) : (
                  <CheckCircle2 className="h-3.5 w-3.5 shrink-0 text-success" />
                )}
                <code className="shrink-0 font-mono text-[13px] text-foreground">{tc.name}</code>
                <span className="truncate text-xs text-text-secondary">
                  {toolSummary(tc.name, tc.params)}
                </span>
                <button
                  type="button"
                  onClick={() =>
                    setExpanded((p) => ({ ...p, [tc.callId]: !p[tc.callId] }))
                  }
                  aria-expanded={open}
                  className="ml-auto flex shrink-0 items-center gap-1 rounded-md px-1.5 py-0.5 text-[11px] text-text-secondary transition-colors hover:bg-subtle hover:text-foreground"
                >
                  展开详情
                  {open ? (
                    <ChevronDown className="h-3 w-3" />
                  ) : (
                    <ChevronRight className="h-3 w-3" />
                  )}
                </button>
              </div>

              {open && (
                <div className="mt-2 space-y-2">
                  <div>
                    <div className="mb-1 text-[11px] text-text-tertiary">参数</div>
                    <pre className="max-h-40 overflow-auto rounded-md bg-subtle px-2.5 py-2 font-mono text-[11px] leading-relaxed text-text-regular">
                      {JSON.stringify(tc.params ?? {}, null, 2)}
                    </pre>
                  </div>
                  {tc.status === "success" && tc.result && (
                    <div>
                      <div className="mb-1 text-[11px] text-text-tertiary">结果</div>
                      <pre className="max-h-40 overflow-auto rounded-md bg-subtle px-2.5 py-2 font-mono text-[11px] leading-relaxed text-text-regular">
                        {prettyJSON(tc.result)}
                      </pre>
                    </div>
                  )}
                  {tc.status === "failed" && tc.error && (
                    <div className="rounded-md bg-danger-soft px-2.5 py-2 text-[11px] text-danger">
                      {tc.error}
                    </div>
                  )}
                </div>
              )}

              {tc.status === "success" && (
                <ToolResultCard toolName={tc.name} params={tc.params} result={tc.result} />
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}

export default function ChatPage() {
  const [conversations, setConversations] = useState<Conversation[]>([])
  const [activeId, setActiveId] = useState<string | null>(null)
  const [messages, setMessages] = useState<UiMessage[]>([])
  const [input, setInput] = useState("")
  const [sending, setSending] = useState(false)
  const [creating, setCreating] = useState(false)
  const [loadingList, setLoadingList] = useState(true)
  const [loadingMessages, setLoadingMessages] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [editingTitle, setEditingTitle] = useState("")
  const [search, setSearch] = useState("")
  const [copiedId, setCopiedId] = useState<string | null>(null)

  const abortRef = useRef<AbortController | null>(null)
  const renamingRef = useRef(false)
  const activeIdRef = useRef<string | null>(null)
  // 正在提交批准/拒绝的审批 id
  const approvalInFlightRef = useRef<Set<string>>(new Set())
  const streamIdRef = useRef<string>("")
  const messagesEndRef = useRef<HTMLDivElement>(null)
  const scrollRef = useRef<HTMLDivElement>(null)
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  // High-frequency stream deltas are accumulated here and flushed once per
  // animation frame, so the UI does not re-render the whole message list for
  // every tiny LLM delta (which caused visible jank).
  const deltaBufRef = useRef<{ streamId: string; parts: string[]; rafId: number | null }>({
    streamId: "",
    parts: [],
    rafId: null,
  })

  function flushDeltaBuffer() {
    const buf = deltaBufRef.current
    if (buf.rafId != null) {
      cancelAnimationFrame(buf.rafId)
      buf.rafId = null
    }
    const chunk = buf.parts.join("")
    buf.parts = []
    if (!chunk) return
    const sid = buf.streamId
    setMessages((prev) =>
      prev.map((m) => (m.id === sid ? { ...m, content: m.content + chunk } : m))
    )
  }

  function stopStream() {
    abortRef.current?.abort()
    abortRef.current = null
    const sid = streamIdRef.current
    if (sid) {
      setMessages((prev) =>
        prev.map((m) => (m.id === sid ? { ...m, streaming: false } : m))
      )
      streamIdRef.current = ""
    }
    setSending(false)
  }

  async function loadMessages(id: string) {
    setLoadingMessages(true)
    setError(null)
    try {
      const { messages, pending_approvals } = await api.getConversation(id)
      setMessages(buildUiMessages(messages, pending_approvals ?? []))
    } catch (err) {
      setError(toMessage(err))
      setMessages([])
    } finally {
      setLoadingMessages(false)
    }
  }

  async function refreshConversations() {
    try {
      const { conversations } = await api.listConversations()
      setConversations(conversations)
    } catch {
      // Keep the current list on refresh failure.
    }
  }

  async function selectConversation(id: string) {
    if (id === activeId) return
    stopStream()
    setActiveId(id)
    await loadMessages(id)
  }

  async function newConversation() {
    stopStream()
    setCreating(true)
    try {
      const { conversation } = await api.createConversation()
      setConversations((prev) => [conversation, ...prev])
      setActiveId(conversation.id)
      setMessages([])
      setError(null)
      textareaRef.current?.focus()
    } catch (err) {
      setError(toMessage(err))
    } finally {
      setCreating(false)
    }
  }

  async function removeConversation(id: string) {
    if (!window.confirm("确认删除该会话？此操作不可撤销。")) return
    try {
      await api.deleteConversation(id)
      setConversations((prev) => prev.filter((c) => c.id !== id))
      if (activeId === id) {
        setActiveId(null)
        setMessages([])
        setError(null)
      }
    } catch (err) {
      setError(toMessage(err))
    }
  }

  function startRename(conversation: Conversation) {
    setEditingId(conversation.id)
    setEditingTitle(conversation.title)
  }

  function cancelRename() {
    setEditingId(null)
    setEditingTitle("")
  }

  async function saveRename(id: string) {
    if (renamingRef.current) return
    const title = editingTitle.trim()
    if (!title) {
      cancelRename()
      return
    }
    if (title === conversations.find((c) => c.id === id)?.title) {
      cancelRename()
      return
    }

    renamingRef.current = true
    try {
      const { conversation } = await api.renameConversation(id, title)
      setConversations((prev) =>
        prev.map((c) => (c.id === id ? conversation : c))
      )
      cancelRename()
    } catch (err) {
      setError(toMessage(err))
    } finally {
      renamingRef.current = false
    }
  }

  async function runStream(
    content: string,
    opts: { skipUserEcho?: boolean; clientMessageId?: string } = {}
  ) {
    const clientMessageId = opts.clientMessageId ?? crypto.randomUUID()
    let conversationId = activeId
    if (!conversationId) {
      try {
        const { conversation } = await api.createConversation()
        conversationId = conversation.id
        setActiveId(conversationId)
        setConversations((prev) => [conversation, ...prev])
      } catch (err) {
        setError(toMessage(err))
        return
      }
    }

    setError(null)
    const streamId = `assistant-${Date.now()}-${Math.random().toString(36).slice(2)}`
    const assistantMsg: UiMessage = {
      id: streamId,
      role: "assistant",
      content: "",
      streaming: true,
    }

    if (opts.skipUserEcho) {
      setMessages((prev) => [...prev, assistantMsg])
    } else {
      const userMsg: UiMessage = { id: `user-${Date.now()}`, role: "user", content }
      setMessages((prev) => [...prev, userMsg, assistantMsg])
      setInput("")
    }

    setSending(true)
    streamIdRef.current = streamId

    const controller = new AbortController()
    abortRef.current = controller

    let approvalRequired = false

    try {
      await api.sendMessageStream(conversationId, content, {
        signal: controller.signal,
        onDelta: (delta) => {
          const buf = deltaBufRef.current
          if (buf.streamId !== streamId) {
            if (buf.rafId != null) {
              cancelAnimationFrame(buf.rafId)
              buf.rafId = null
            }
            buf.streamId = streamId
            buf.parts = []
          }
          buf.parts.push(delta)
          if (buf.rafId == null) {
            buf.rafId = requestAnimationFrame(() => {
              buf.rafId = null
              flushDeltaBuffer()
            })
          }
        },
        onDone: (_messageId, _conversationId, evidence, grounding) => {
          flushDeltaBuffer()
          setMessages((prev) =>
            prev.map((m) =>
              m.id === streamId
                ? { ...m, streaming: false, evidence, grounding }
                : m
            )
          )
        },
        onError: (msg) => {
          flushDeltaBuffer()
          if (approvalRequired) {
            setMessages((prev) =>
              prev.map((m) => (m.id === streamId ? { ...m, streaming: false } : m))
            )
            return
          }
          setMessages((prev) =>
            prev.map((m) =>
              m.id === streamId ? { ...m, streaming: false, error: msg } : m
            )
          )
          setError(msg)
        },
        onToolCall: (ev) => {
          setMessages((prev) =>
            prev.map((m) => {
              if (m.id !== streamId) return m
              const toolCalls = [...(m.toolCalls ?? [])]
              toolCalls.push({
                callId: ev.call_id,
                name: ev.tool_name,
                params: ev.params ?? {},
                status: ev.status,
              })
              return { ...m, toolCalls }
            })
          )
        },
        onToolResult: (ev) => {
          setMessages((prev) =>
            prev.map((m) => {
              if (m.id !== streamId) return m
              const toolCalls = (m.toolCalls ?? []).map((tc) =>
                tc.callId === ev.call_id
                  ? { ...tc, status: ev.status, result: ev.result, error: ev.error }
                  : tc
              )
              return { ...m, toolCalls }
            })
          )
        },
        onApprovalRequired: (ev) => {
          approvalRequired = true
          setMessages((prev) =>
            prev.map((m) =>
              m.id === streamId
                ? {
                    ...m,
                    streaming: false,
                    approval: {
                      id: ev.approval_id,
                      toolName: ev.tool_name,
                      params: ev.params,
                      reason: ev.reason,
                      content,
                      clientMessageId,
                    },
                  }
                : m
            )
          )
        },
      }, clientMessageId)
    } catch (err) {
      if (isAbortError(err)) {
        setMessages((prev) =>
          prev.map((m) => (m.id === streamId ? { ...m, streaming: false } : m))
        )
      } else {
        const msg = toMessage(err)
        setMessages((prev) =>
          prev.map((m) =>
            m.id === streamId ? { ...m, streaming: false, error: msg } : m
          )
        )
        setError(msg)
      }
    } finally {
      streamIdRef.current = ""
      abortRef.current = null
      setSending(false)
      void refreshConversations()
    }
  }

  async function send() {
    const content = input.trim()
    if (!content || sending || creating) return
    await runStream(content)
  }

  async function copyText(text: string) {
    try {
      await navigator.clipboard.writeText(text)
    } catch {
      // 剪贴板不可用（非安全上下文等）时静默忽略
    }
  }

  // 重新生成：重发最近一条用户提问，不重复回显提问气泡。
  function regenerate() {
    if (sending) return
    const lastUser = [...messages].reverse().find((m) => m.role === "user")
    if (!lastUser) return
    void runStream(lastUser.content, { skipUserEcho: true })
  }

  function patchApproval(messageId: string, patch: Partial<ApprovalState>) {
    setMessages((prev) =>
      prev.map((m) =>
        m.id === messageId && m.approval
          ? { ...m, approval: { ...m.approval, ...patch } }
          : m
      )
    )
  }

  /**
   * 批准或拒绝审批。同一审批的请求进行中时忽略重复点击；
   * 失败时不标记决策，409/410 标为失效，其余错误恢复按钮可用。
   */
  async function handleApproval(messageId: string, action: "approve" | "reject") {
    const target = messages.find((m) => m.id === messageId)
    const approval = target?.approval
    if (!approval || approval.decision || approval.invalid) return
    // 状态更新是异步的，用 ref 同步拦截连续点击
    if (approvalInFlightRef.current.has(approval.id)) return
    approvalInFlightRef.current.add(approval.id)

    const conversationId = activeId
    patchApproval(messageId, { submitting: true, error: undefined })

    try {
      if (action === "approve") await api.approveApproval(approval.id)
      else await api.rejectApproval(approval.id)
    } catch (err) {
      const msg = toMessage(err)
      const gone = err instanceof ApiError && (err.status === 409 || err.status === 410)
      patchApproval(messageId, { submitting: false, invalid: gone, error: msg })
      setError(msg)
      return
    } finally {
      approvalInFlightRef.current.delete(approval.id)
    }

    patchApproval(messageId, {
      submitting: false,
      decision: action === "approve" ? "approved" : "rejected",
    })

    // 请求期间已切换会话时，不在新会话里重试
    if (action !== "approve" || activeIdRef.current !== conversationId) return
    if (!approval.content) return
    await runStream(approval.content, {
      skipUserEcho: true,
      clientMessageId: approval.clientMessageId,
    })
  }

  useEffect(() => {
    activeIdRef.current = activeId
  }, [activeId])

  useEffect(() => {
    const el = scrollRef.current
    if (!el) return
    // Only follow the stream when the user is already near the bottom; a user
    // reading earlier messages must not have their scroll position stolen.
    const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 80
    if (nearBottom) messagesEndRef.current?.scrollIntoView({ behavior: "smooth" })
  }, [messages, sending])

  useEffect(() => {
    let cancelled = false
    async function boot() {
      setLoadingList(true)
      try {
        const { conversations } = await api.listConversations()
        if (cancelled) return
        setConversations(conversations)
        if (conversations.length > 0) {
          const first = conversations[0]
          setActiveId(first.id)
          await loadMessages(first.id)
        }
      } catch (err) {
        if (!cancelled) setError(toMessage(err))
      } finally {
        if (!cancelled) setLoadingList(false)
      }
    }
    void boot()
    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  function handleKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault()
      void send()
    }
  }

  const query = search.trim().toLowerCase()
  const filteredConversations = query
    ? conversations.filter((c) => c.title.toLowerCase().includes(query))
    : conversations

  const activeTitle =
    conversations.find((c) => c.id === activeId)?.title ?? "新对话"

  function copyMessage(content: string, id: string) {
    void navigator.clipboard.writeText(content).then(() => {
      setCopiedId(id)
      window.setTimeout(() => setCopiedId((c) => (c === id ? null : c)), 1500)
    })
  }

  function renderMessage(msg: UiMessage) {
    if (msg.role === "user") {
      return (
        <div key={msg.id} className="flex justify-end">
          <div className="flex max-w-[80%] flex-col items-end gap-1">
            <div className="flex items-center gap-1 text-[11px] text-text-tertiary">
              <User className="h-3 w-3" />
              <span>我</span>
              <button
                type="button"
                aria-label="复制消息"
                onClick={() => void copyMessage(msg.content, msg.id)}
                className="rounded p-0.5 transition-colors hover:text-foreground"
              >
                {copiedId === msg.id ? (
                  <Check className="h-3 w-3" />
                ) : (
                  <Copy className="h-3 w-3" />
                )}
              </button>
            </div>
            <div className="rounded-xl bg-primary px-4 py-2.5 text-sm text-primary-foreground">
              {msg.content}
            </div>
          </div>
        </div>
      )
    }

    const evidenceCount = (msg.evidence ?? []).filter(
      (e) => e.status === "success"
    ).length

    return (
      <div key={msg.id} className="flex items-start gap-3">
        <div className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-primary">
          <Bot className="h-4 w-4 text-primary-foreground" />
        </div>
        <div className="flex min-w-0 max-w-[85%] flex-col gap-1.5">
          <div className="flex items-center gap-1.5">
            <span className="text-[13px] font-medium text-foreground">OpsAgent</span>
            {msg.content && !msg.streaming && (
              <button
                type="button"
                aria-label="复制回答"
                onClick={() => void copyMessage(msg.content, msg.id)}
                className="rounded p-0.5 text-text-tertiary transition-colors hover:text-foreground"
              >
                {copiedId === msg.id ? (
                  <Check className="h-3 w-3" />
                ) : (
                  <Copy className="h-3 w-3" />
                )}
              </button>
            )}
          </div>

          {msg.toolCalls && msg.toolCalls.length > 0 && (
            <ToolCallGroup calls={msg.toolCalls} />
          )}

          {msg.grounding === "unsupported" && (
            <div
              role="alert"
              className="flex items-start gap-2 rounded-md border border-warning-border bg-warning-soft px-3 py-2 text-xs leading-5 text-warning"
            >
              <ShieldAlert className="mt-0.5 h-4 w-4 shrink-0" />
              <span>
                本次回答未取得相关数据源的工具结果，可信度低，请以工具/控制台实际数据为准。
              </span>
            </div>
          )}

          {msg.grounding === "grounded" && (
            <div className="flex items-center gap-1.5">
              <span className="rounded-full bg-accent px-2.5 py-1 text-[11px] font-medium text-accent-foreground">
                基于 {evidenceCount} 条工具证据
              </span>
            </div>
          )}

          {msg.approval && (
            <div
              role="group"
              aria-label={`待审批操作：${msg.approval.toolName}`}
              className="rounded-md border border-warning-border bg-warning-soft px-3 py-2 text-xs"
            >
              <div className="flex items-center gap-2">
                <ShieldAlert className="h-3.5 w-3.5 text-warning" />
                <span className="font-medium">需要审批：{msg.approval.toolName}</span>
              </div>
              {msg.approval.reason && (
                <div className="mt-1 text-text-secondary">{msg.approval.reason}</div>
              )}
              <pre className="mt-1 max-h-24 overflow-auto whitespace-pre-wrap break-all text-text-secondary">
                {JSON.stringify(msg.approval.params, null, 2)}
              </pre>
              {msg.approval.decision ? (
                <div className="mt-2 text-xs text-text-secondary" role="status">
                  {msg.approval.decision === "rejected"
                    ? "已拒绝"
                    : msg.approval.content
                      ? "已批准"
                      : "已批准，请重新发送指令以执行"}
                </div>
              ) : msg.approval.invalid ? (
                <div className="mt-2 text-xs text-danger" role="status">
                  审批已失效：{msg.approval.error}
                </div>
              ) : (
                <>
                  <div className="mt-2 flex gap-2">
                    <button
                      type="button"
                      onClick={() => void handleApproval(msg.id, "approve")}
                      disabled={msg.approval.submitting}
                      aria-disabled={msg.approval.submitting}
                      className="flex items-center gap-1 rounded-md bg-primary px-2.5 py-1 text-xs font-medium text-primary-foreground transition-colors hover:bg-primary-hover disabled:cursor-not-allowed disabled:opacity-50 disabled:hover:bg-primary"
                    >
                      {msg.approval.submitting ? (
                        <Loader2 className="h-3.5 w-3.5 animate-spin" />
                      ) : (
                        <Check className="h-3.5 w-3.5" />
                      )}
                      批准
                    </button>
                    <button
                      type="button"
                      onClick={() => void handleApproval(msg.id, "reject")}
                      disabled={msg.approval.submitting}
                      aria-disabled={msg.approval.submitting}
                      className="flex items-center gap-1 rounded-md border border-border bg-surface px-2.5 py-1 text-xs text-foreground transition-colors hover:bg-subtle disabled:cursor-not-allowed disabled:opacity-50 disabled:hover:bg-surface"
                    >
                      <X className="h-3.5 w-3.5" />
                      拒绝
                    </button>
                  </div>
                  {msg.approval.error && (
                    <div className="mt-1 text-xs text-danger" role="alert">
                      操作失败：{msg.approval.error}
                    </div>
                  )}
                </>
              )}
            </div>
          )}

          {msg.content && (
            <div className="whitespace-pre-wrap text-sm leading-relaxed text-text-regular">
              <Markdown content={msg.content} />
              {msg.streaming && (
                <span className="ml-0.5 inline-block h-4 w-2 animate-pulse bg-current align-middle" />
              )}
            </div>
          )}

          {!msg.streaming && msg.content && (
            <div className="flex items-center gap-3 text-[11px] text-text-tertiary">
              <button
                type="button"
                onClick={() => void copyText(msg.content)}
                className="flex items-center gap-1 transition-colors hover:text-foreground"
              >
                <Copy className="h-3 w-3" />
                复制
              </button>
              <button
                type="button"
                onClick={() => regenerate()}
                disabled={sending}
                className="flex items-center gap-1 transition-colors hover:text-foreground disabled:opacity-50"
              >
                <RefreshCw className="h-3 w-3" />
                重新生成
              </button>
            </div>
          )}

          {msg.streaming && !msg.content && !msg.toolCalls?.length && !msg.approval && (
            <div className="flex items-center gap-2 text-[13px] text-text-secondary">
              <Loader2 className="h-4 w-4 animate-spin" />
              正在思考…
            </div>
          )}

          {msg.error && (
            <div className="flex items-start gap-2 rounded-lg border border-danger/40 bg-danger-soft p-3 text-[13px]">
              <AlertCircle className="mt-0.5 h-4 w-4 shrink-0 text-danger" />
              <span className="text-danger">{msg.error}</span>
            </div>
          )}
        </div>
      </div>
    )
  }

  return (
    <div className="flex h-full min-h-0 gap-4">
        <aside className="flex w-[280px] shrink-0 flex-col overflow-hidden rounded-lg border border-border bg-surface shadow-sm">
          <div className="space-y-2.5 border-b border-border p-3">
            <div className="relative">
              <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-text-tertiary" />
              <input
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder="搜索会话"
                aria-label="搜索会话"
                className="w-full rounded-lg border border-border bg-background py-2 pl-8 pr-2.5 text-[13px] text-foreground outline-none transition-colors placeholder:text-text-tertiary focus:border-primary focus:ring-1 focus:ring-primary"
              />
            </div>
            <button
              onClick={() => void newConversation()}
              className="flex w-full items-center justify-center gap-2 rounded-lg bg-primary px-3 py-2 text-[13px] font-semibold text-primary-foreground transition-colors hover:bg-primary-hover"
            >
              <Plus className="h-4 w-4" />
              新建对话
            </button>
          </div>

          <div className="flex-1 overflow-y-auto px-2 py-2">
            {loadingList ? (
              <div className="flex items-center justify-center gap-2 p-4 text-[13px] text-text-secondary">
                <Loader2 className="h-4 w-4 animate-spin" />
                加载中…
              </div>
            ) : filteredConversations.length === 0 ? (
              <div className="p-4 text-center text-[13px] text-text-secondary">
                {conversations.length === 0 ? "暂无会话" : "没有匹配的会话"}
              </div>
            ) : (
              groupByDay(filteredConversations).map((group) => (
                <div key={group.label} className="mb-1">
                  <div className="px-2 py-1.5 text-[11px] text-text-tertiary">
                    {group.label}
                  </div>
                  <ul className="space-y-0.5">
                    {group.items.map((c) => {
                  const active = c.id === activeId
                  return (
                    <li key={c.id}>
                      {editingId === c.id ? (
                        <div className="flex items-center gap-1 rounded-md bg-subtle">
                          <input
                            autoFocus
                            value={editingTitle}
                            onChange={(e) => setEditingTitle(e.target.value)}
                            onFocus={(e) => e.currentTarget.select()}
                            onBlur={() => void saveRename(c.id)}
                            onKeyDown={(e) => {
                              if (e.key === "Enter") {
                                e.preventDefault()
                                void saveRename(c.id)
                              } else if (e.key === "Escape") {
                                cancelRename()
                              }
                            }}
                            aria-label="重命名会话"
                            className="min-w-0 flex-1 rounded-md border bg-background px-3 py-2 text-sm text-foreground outline-none focus:border-primary focus:ring-1 focus:ring-primary"
                          />
                        </div>
                      ) : (
                        <div
                          className={cn(
                            "group flex items-center gap-1 rounded-md transition-colors",
                            active
                              ? "border border-primary-border bg-primary-soft text-foreground"
                              : "hover:bg-subtle"
                          )}
                        >
                          <button
                            onClick={() => void selectConversation(c.id)}
                            className="flex min-w-0 flex-1 items-center justify-between gap-2 px-2.5 py-2 text-left"
                          >
                            <span className="min-w-0 truncate text-[13px] font-medium">
                              {c.title}
                            </span>
                            <span className="shrink-0 text-[11px] text-text-tertiary">
                              {formatConversationTime(c.updated_at)}
                            </span>
                          </button>
                          <button
                            onClick={() => startRename(c)}
                            aria-label="重命名会话"
                            className={cn(
                              "rounded-md p-1.5 opacity-0 transition-opacity group-hover:opacity-100",
                              active
                                ? "text-text-secondary hover:bg-subtle hover:text-foreground"
                                : "text-text-secondary hover:bg-subtle hover:text-foreground"
                            )}
                          >
                            <Pencil className="h-4 w-4" />
                          </button>
                          <button
                            onClick={() => void removeConversation(c.id)}
                            aria-label="删除会话"
                            className={cn(
                              "mr-1 rounded-md p-1.5 opacity-0 transition-opacity group-hover:opacity-100",
                              active
                                ? "text-text-secondary hover:bg-danger-soft hover:text-danger"
                                : "text-text-secondary hover:bg-danger-soft hover:text-danger"
                            )}
                          >
                            <Trash2 className="h-4 w-4" />
                          </button>
                        </div>
                      )}
                    </li>
                  )
                    })}
                  </ul>
                </div>
              ))
            )}
          </div>
        </aside>

        <section className="flex min-w-0 flex-1 flex-col overflow-hidden rounded-lg border border-border bg-surface shadow-sm">
          <div className="flex shrink-0 items-center justify-between gap-3 border-b border-border px-4 py-3">
            <h2 className="truncate text-[15px] font-semibold text-foreground">
              {activeTitle}
            </h2>
            <div className="flex shrink-0 items-center gap-2">
              <span className="rounded-md border border-border bg-subtle px-2 py-0.5 text-[11px] text-text-secondary">
                OpsAgent
              </span>
              {sending && (
                <button
                  onClick={stopStream}
                  className="flex items-center gap-1.5 rounded-md border border-danger/40 px-3 py-1.5 text-[13px] text-danger transition-colors hover:bg-danger-soft"
                >
                  <Square className="h-3.5 w-3.5" />
                  停止
                </button>
              )}
            </div>
          </div>

          <div ref={scrollRef} className="flex-1 overflow-y-auto p-4">
            {loadingMessages ? (
              <div className="flex h-full items-center justify-center gap-2 text-[13px] text-text-secondary">
                <Loader2 className="h-4 w-4 animate-spin" />
                加载中…
              </div>
            ) : error && messages.length === 0 ? (
              <div className="flex h-full flex-col items-center justify-center gap-3 text-center">
                <AlertCircle className="h-8 w-8 text-danger" />
                <p className="max-w-sm text-[13px] text-text-secondary">{error}</p>
              </div>
            ) : messages.length === 0 ? (
              <div className="flex h-full flex-col items-center justify-center gap-4">
                <MessageSquare className="h-10 w-10 text-text-tertiary" />
                <p className="text-[13px] text-text-secondary">
                  选择或新建一个会话开始对话
                </p>
                <div className="flex flex-wrap justify-center gap-2">
                  {SUGGESTED_PROMPTS.map((prompt) => (
                    <button
                      key={prompt}
                      onClick={() => {
                        setInput(prompt)
                        textareaRef.current?.focus()
                      }}
                      className="rounded-full border border-border px-3 py-1 text-xs text-text-secondary transition-colors hover:border-primary hover:text-foreground"
                    >
                      {prompt}
                    </button>
                  ))}
                </div>
              </div>
            ) : (
              <div className="space-y-4">{messages.map(renderMessage)}</div>
            )}
            <div ref={messagesEndRef} />
          </div>

          <div className="shrink-0 border-t border-border p-3">
            <div className="rounded-xl border border-border bg-background px-3 py-2 transition-colors focus-within:border-primary">
              <textarea
                ref={textareaRef}
                value={input}
                onChange={(e) => setInput(e.target.value)}
                onKeyDown={handleKeyDown}
                placeholder="输入问题，按 Enter 发送，Shift+Enter 换行"
                rows={1}
                className="w-full resize-none bg-transparent text-sm text-foreground outline-none placeholder:text-text-tertiary disabled:opacity-50"
                style={{ minHeight: "40px", maxHeight: "140px" }}
              />
              <div className="flex items-center justify-between gap-2">
                <span className="flex items-center gap-1 text-[11px] text-text-tertiary">
                  <Paperclip className="h-3.5 w-3.5" />
                  引用资源
                </span>
                {sending ? (
                  <button
                    onClick={stopStream}
                    aria-label="停止"
                    className="flex h-8 w-8 items-center justify-center rounded-full border border-danger/40 text-danger transition-colors hover:bg-danger-soft"
                  >
                    <Square className="h-4 w-4" />
                  </button>
                ) : (
                  <button
                    onClick={() => void send()}
                    disabled={!input.trim() || creating}
                    aria-label="发送"
                    className="flex h-8 w-8 items-center justify-center rounded-full bg-primary text-primary-foreground transition-colors hover:bg-primary-hover disabled:opacity-50"
                  >
                    <Send className="h-4 w-4" />
                  </button>
                )}
              </div>
            </div>
            <p className="mt-2 text-center text-[11px] text-text-tertiary">
              AI 生成内容仅供参考，请核实后操作
            </p>
          </div>
        </section>
    </div>
  )
}
