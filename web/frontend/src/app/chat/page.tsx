"use client"

import { useEffect, useRef, useState } from "react"
import {
  AlertCircle,
  Bot,
  Check,
  Loader2,
  MessageSquare,
  Pencil,
  Plus,
  Send,
  ShieldAlert,
  Square,
  Trash2,
  User,
  Wrench,
  X,
} from "lucide-react"
import { cn } from "@/lib/utils"
import {
  api,
  type Conversation,
  type ConversationMessage,
} from "@/lib/api"

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
}

interface UiMessage {
  id: string
  role: "user" | "assistant"
  content: string
  streaming?: boolean
  error?: string
  toolCalls?: ToolCall[]
  approval?: ApprovalState
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

export default function ChatPage() {
  const [conversations, setConversations] = useState<Conversation[]>([])
  const [activeId, setActiveId] = useState<string | null>(null)
  const [messages, setMessages] = useState<UiMessage[]>([])
  const [input, setInput] = useState("")
  const [sending, setSending] = useState(false)
  const [loadingList, setLoadingList] = useState(true)
  const [loadingMessages, setLoadingMessages] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [editingTitle, setEditingTitle] = useState("")

  const abortRef = useRef<AbortController | null>(null)
  const renamingRef = useRef(false)
  const streamIdRef = useRef<string>("")
  const messagesEndRef = useRef<HTMLDivElement>(null)
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
      const { messages } = await api.getConversation(id)
      setMessages(
        messages.map((m: ConversationMessage) => ({
          id: m.id,
          role: m.role,
          content: m.content,
        }))
      )
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
    try {
      const { conversation } = await api.createConversation()
      setConversations((prev) => [conversation, ...prev])
      setActiveId(conversation.id)
      setMessages([])
      setError(null)
      textareaRef.current?.focus()
    } catch (err) {
      setError(toMessage(err))
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
        onDone: () => {
          flushDeltaBuffer()
          setMessages((prev) =>
            prev.map((m) => (m.id === streamId ? { ...m, streaming: false } : m))
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
    if (!content || sending) return
    await runStream(content)
  }

  async function handleApproval(messageId: string, action: "approve" | "reject") {
    const target = messages.find((m) => m.id === messageId)
    const approval = target?.approval
    if (!approval) return

    try {
      if (action === "approve") await api.approveApproval(approval.id)
      else await api.rejectApproval(approval.id)
    } catch (err) {
      setError(toMessage(err))
      return
    }

    setMessages((prev) =>
      prev.map((m) =>
        m.id === messageId && m.approval
          ? {
              ...m,
              approval: {
                ...m.approval,
                decision: action === "approve" ? "approved" : "rejected",
              },
            }
          : m
      )
    )

    if (action === "approve") {
      await runStream(approval.content, {
        skipUserEcho: true,
        clientMessageId: approval.clientMessageId,
      })
    }
  }

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: "smooth" })
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

  const activeTitle =
    conversations.find((c) => c.id === activeId)?.title ?? "对话助手"

  function renderMessage(msg: UiMessage) {
    if (msg.role === "user") {
      return (
        <div key={msg.id} className="flex justify-end">
          <div className="flex max-w-[80%] flex-col items-end gap-1">
            <div className="flex items-center gap-1 text-xs text-muted-foreground">
              <User className="h-3 w-3" />
              <span>我</span>
            </div>
            <div className="rounded-lg bg-gradient-to-br from-[#7170ff] to-[#5e6ad2] px-4 py-2 text-sm text-white">
              {msg.content}
            </div>
          </div>
        </div>
      )
    }

    return (
      <div key={msg.id} className="flex items-start gap-3">
        <div className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-accent">
          <Bot className="h-4 w-4 text-muted-foreground" />
        </div>
        <div className="flex max-w-[80%] flex-col gap-1">
          <span className="text-xs text-muted-foreground">OpsAgent</span>

          {msg.toolCalls?.map((tc) => (
            <div key={tc.callId} className="rounded-md border bg-background/60 px-3 py-2 text-xs">
              <div className="flex items-center gap-2">
                <Wrench className="h-3.5 w-3.5 text-muted-foreground" />
                <span className="font-medium">{tc.name}</span>
                <span
                  className={cn(
                    "rounded-full px-2 py-0.5 text-[10px]",
                    tc.status === "running" && "bg-accent text-muted-foreground",
                    tc.status === "success" && "bg-emerald-500/10 text-emerald-600",
                    tc.status === "failed" && "bg-destructive/10 text-destructive"
                  )}
                >
                  {tc.status === "running" ? "运行中" : tc.status === "success" ? "成功" : "失败"}
                </span>
              </div>
              <pre className="mt-1 max-h-24 overflow-auto whitespace-pre-wrap break-all text-muted-foreground">
                {JSON.stringify(tc.params, null, 2)}
              </pre>
              {tc.status === "success" && tc.result && (
                <div className="mt-1 max-h-24 overflow-auto whitespace-pre-wrap break-all text-muted-foreground">
                  {tc.result}
                </div>
              )}
              {tc.status === "failed" && tc.error && (
                <div className="mt-1 text-destructive">{tc.error}</div>
              )}
            </div>
          ))}

          {msg.approval && (
            <div className="rounded-md border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-xs">
              <div className="flex items-center gap-2">
                <ShieldAlert className="h-3.5 w-3.5 text-amber-600" />
                <span className="font-medium">需要审批：{msg.approval.toolName}</span>
              </div>
              {msg.approval.reason && (
                <div className="mt-1 text-muted-foreground">{msg.approval.reason}</div>
              )}
              <pre className="mt-1 max-h-24 overflow-auto whitespace-pre-wrap break-all text-muted-foreground">
                {JSON.stringify(msg.approval.params, null, 2)}
              </pre>
              {msg.approval.decision ? (
                <div className="mt-2 text-xs text-muted-foreground">
                  {msg.approval.decision === "approved" ? "已批准，正在执行…" : "已拒绝"}
                </div>
              ) : (
                <div className="mt-2 flex gap-2">
                  <button
                    onClick={() => void handleApproval(msg.id, "approve")}
                    className="flex items-center gap-1 rounded-md bg-emerald-600 px-2.5 py-1 text-xs font-medium text-white transition-opacity hover:opacity-90"
                  >
                    <Check className="h-3.5 w-3.5" />
                    批准
                  </button>
                  <button
                    onClick={() => void handleApproval(msg.id, "reject")}
                    className="flex items-center gap-1 rounded-md border border-destructive/40 px-2.5 py-1 text-xs text-destructive transition-colors hover:bg-destructive/10"
                  >
                    <X className="h-3.5 w-3.5" />
                    拒绝
                  </button>
                </div>
              )}
            </div>
          )}

          {msg.content && (
            <div className="whitespace-pre-wrap rounded-lg bg-accent/40 px-4 py-3 text-sm">
              {msg.content}
              {msg.streaming && (
                <span className="ml-0.5 inline-block h-4 w-2 animate-pulse bg-current align-middle" />
              )}
            </div>
          )}

          {msg.streaming && !msg.content && !msg.toolCalls?.length && !msg.approval && (
            <div className="flex items-center gap-2 text-sm text-muted-foreground">
              <Loader2 className="h-4 w-4 animate-spin" />
              正在思考…
            </div>
          )}

          {msg.error && (
            <div className="flex items-start gap-2 rounded-md border border-destructive/50 bg-destructive/10 p-3 text-sm">
              <AlertCircle className="mt-0.5 h-4 w-4 shrink-0 text-destructive" />
              <span className="text-destructive">{msg.error}</span>
            </div>
          )}
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <header>
        <h1 className="text-3xl font-bold tracking-tight">对话助手</h1>
        <p className="text-muted-foreground">会话管理 · SSE 流式输出</p>
      </header>

      <div className="flex gap-4" style={{ height: "calc(100vh - 180px)" }}>
        <aside className="flex w-64 shrink-0 flex-col overflow-hidden rounded-lg border bg-card shadow-sm">
          <div className="border-b p-3">
            <button
              onClick={() => void newConversation()}
              className="flex w-full items-center justify-center gap-2 rounded-md bg-gradient-to-br from-[#7170ff] to-[#5e6ad2] px-3 py-2 text-sm font-semibold text-white transition-opacity hover:opacity-90"
            >
              <Plus className="h-4 w-4" />
              新建对话
            </button>
          </div>

          <div className="flex-1 overflow-y-auto p-2">
            {loadingList ? (
              <div className="flex items-center justify-center gap-2 p-4 text-sm text-muted-foreground">
                <Loader2 className="h-4 w-4 animate-spin" />
                加载中…
              </div>
            ) : conversations.length === 0 ? (
              <div className="p-4 text-center text-sm text-muted-foreground">
                暂无会话
              </div>
            ) : (
              <ul className="space-y-1">
                {conversations.map((c) => {
                  const active = c.id === activeId
                  return (
                    <li key={c.id}>
                      {editingId === c.id ? (
                        <div className="flex items-center gap-1 rounded-md bg-accent">
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
                            className="min-w-0 flex-1 rounded-md border bg-background px-3 py-2 text-sm text-foreground outline-none focus:border-[#7170ff] focus:ring-1 focus:ring-[#7170ff]"
                          />
                        </div>
                      ) : (
                        <div
                          className={cn(
                            "group flex items-center gap-1 rounded-md transition-colors",
                            active
                              ? "bg-gradient-to-br from-[#7170ff] to-[#5e6ad2] text-white"
                              : "hover:bg-accent"
                          )}
                        >
                          <button
                            onClick={() => void selectConversation(c.id)}
                            className="min-w-0 flex-1 px-3 py-2 text-left"
                          >
                            <span className="block truncate text-sm font-medium">
                              {c.title}
                            </span>
                            <span
                              className={cn(
                                "block truncate text-xs",
                                active ? "text-white/70" : "text-muted-foreground"
                              )}
                            >
                              {formatTime(c.updated_at)}
                            </span>
                          </button>
                          <button
                            onClick={() => startRename(c)}
                            aria-label="重命名会话"
                            className={cn(
                              "rounded-md p-1.5 opacity-0 transition-opacity group-hover:opacity-100",
                              active
                                ? "text-white hover:bg-white/20"
                                : "text-muted-foreground hover:bg-accent hover:text-foreground"
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
                                ? "text-white hover:bg-white/20"
                                : "text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
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
            )}
          </div>
        </aside>

        <section className="flex flex-1 flex-col overflow-hidden rounded-lg border bg-card shadow-sm">
          <div className="flex items-center justify-between border-b px-4 py-3">
            <div className="flex min-w-0 items-center gap-2">
              <MessageSquare className="h-4 w-4 shrink-0 text-muted-foreground" />
              <span className="truncate text-sm font-medium">{activeTitle}</span>
            </div>
            {sending && (
              <button
                onClick={stopStream}
                className="flex items-center gap-1.5 rounded-md border border-destructive/40 px-3 py-1.5 text-sm text-destructive transition-colors hover:bg-destructive/10"
              >
                <Square className="h-3.5 w-3.5" />
                停止
              </button>
            )}
          </div>

          <div className="flex-1 overflow-y-auto p-4">
            {loadingMessages ? (
              <div className="flex h-full items-center justify-center gap-2 text-sm text-muted-foreground">
                <Loader2 className="h-4 w-4 animate-spin" />
                加载中…
              </div>
            ) : error && messages.length === 0 ? (
              <div className="flex h-full flex-col items-center justify-center gap-3 text-center">
                <AlertCircle className="h-8 w-8 text-destructive" />
                <p className="max-w-sm text-sm text-muted-foreground">{error}</p>
              </div>
            ) : messages.length === 0 ? (
              <div className="flex h-full flex-col items-center justify-center gap-4">
                <MessageSquare className="h-10 w-10 text-muted-foreground/40" />
                <p className="text-sm text-muted-foreground">
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
                      className="rounded-full border px-3 py-1 text-xs text-muted-foreground transition-colors hover:border-[#7170ff] hover:text-foreground"
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

          <div className="border-t bg-accent/10 p-4">
            <div className="flex items-end gap-2">
              <textarea
                ref={textareaRef}
                value={input}
                onChange={(e) => setInput(e.target.value)}
                onKeyDown={handleKeyDown}
                placeholder="输入问题，按 Enter 发送，Shift+Enter 换行…"
                rows={2}
                className="w-full resize-none rounded-md border bg-background px-3 py-2 text-sm outline-none focus:border-[#7170ff] focus:ring-1 focus:ring-[#7170ff] disabled:opacity-50"
                style={{ minHeight: "44px", maxHeight: "140px" }}
              />
              {sending ? (
                <button
                  onClick={stopStream}
                  aria-label="停止"
                  className="rounded-md border border-destructive/40 p-2 text-destructive transition-colors hover:bg-destructive/10"
                >
                  <Square className="h-4 w-4" />
                </button>
              ) : (
                <button
                  onClick={() => void send()}
                  disabled={!input.trim()}
                  aria-label="发送"
                  className="rounded-md bg-gradient-to-br from-[#7170ff] to-[#5e6ad2] p-2 text-white transition-opacity hover:opacity-90 disabled:opacity-50"
                >
                  <Send className="h-4 w-4" />
                </button>
              )}
            </div>
          </div>
        </section>
      </div>
    </div>
  )
}
