"use client"

import { useEffect, useRef, useState } from "react"
import { ChevronDown, Wrench, Bot, User, AlertCircle, Send } from "lucide-react"
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  CardDescription,
} from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { api, type ChatMessage as ApiChatMessage } from "@/lib/api"

type AgentRole = "Coordinator" | "LogAnalyst" | "Inspector" | "Monitor"

interface ToolCall {
  tool: string
  params: Record<string, unknown>
  result: string
}

interface ThinkingStep {
  step: string
  detail: string
}

interface Message {
  id: string
  role: "user" | "assistant"
  content: string
  agent?: AgentRole
  thinking?: ThinkingStep[]
  tools?: ToolCall[]
  timestamp: Date
  error?: string // For error messages
}

const agentBadgeVariant: Record<AgentRole, "default" | "warning" | "secondary" | "success"> = {
  Coordinator: "default",
  LogAnalyst: "warning",
  Inspector: "secondary",
  Monitor: "success",
}

const agentLabels: Record<AgentRole, string> = {
  Coordinator: "协调器",
  LogAnalyst: "日志分析师",
  Inspector: "巡检员",
  Monitor: "监控员",
}

const SUGGESTED_PROMPTS = [
  "分析最近的 P1 告警",
  "查询 order-service 错误日志",
  "展示当前服务拓扑",
  "Redis 命中率为何下降?",
]

const WELCOME_MESSAGE: Message = {
  id: "welcome",
  role: "assistant",
  agent: "Coordinator",
  content:
    "你好！我是 OpsAgent 运维助手。我可以帮你分析告警、梳理排查思路、理解服务拓扑等。请问有什么可以帮你？",
  timestamp: new Date(),
}

export default function ChatPage() {
  const [messages, setMessages] = useState<Message[]>([WELCOME_MESSAGE])
  const [input, setInput] = useState("")
  const [expandedThinking, setExpandedThinking] = useState<Set<string>>(new Set())
  const [isLoading, setIsLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const messagesEndRef = useRef<HTMLDivElement>(null)
  const textareaRef = useRef<HTMLTextAreaElement>(null)

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: "smooth" })
  }, [messages])

  function toggleThinking(msgId: string) {
    setExpandedThinking((prev) => {
      const next = new Set(prev)
      if (next.has(msgId)) next.delete(msgId)
      else next.add(msgId)
      return next
    })
  }

  async function handleSend() {
    const trimmed = input.trim()
    if (!trimmed || isLoading) return

    setError(null)

    // Add user message
    const userMsg: Message = {
      id: String(Date.now()),
      role: "user",
      content: trimmed,
      timestamp: new Date(),
    }
    setMessages((prev) => [...prev, userMsg])
    setInput("")
    setIsLoading(true)

    try {
      // Convert frontend messages to API format
      const apiMessages: ApiChatMessage[] = messages
        .filter((m) => m.role === "user" || m.role === "assistant")
        .map((m) => ({
          role: m.role,
          content: m.content,
        }))

      // Add the new user message
      apiMessages.push({
        role: "user",
        content: trimmed,
      })

      const response = await api.chat(apiMessages)

      const assistantMsg: Message = {
        id: String(Date.now() + 1),
        role: "assistant",
        agent: "Coordinator", // MVP: always Coordinator
        content: response.reply,
        timestamp: new Date(response.timestamp),
        // Map thinking and tools if present
        thinking: response.thinking?.map((t) => ({ step: t.step, detail: t.detail })),
        tools: response.tools?.map((t) => ({
          tool: t.tool,
          params: t.params as Record<string, unknown>,
          result: t.result,
        })),
      }

      setMessages((prev) => [...prev, assistantMsg])
    } catch (err) {
      console.error("Chat error:", err)

      const errorMsg = err instanceof Error ? err.message : "Failed to get response"

      // Add error message as a system message
      const errorMessage: Message = {
        id: String(Date.now() + 1),
        role: "assistant",
        agent: "Coordinator",
        content: "",
        error: errorMsg,
        timestamp: new Date(),
      }

      setMessages((prev) => [...prev, errorMessage])
      setError(errorMsg)
    } finally {
      setIsLoading(false)
    }
  }

  function handleKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault()
      handleSend()
    }
  }

  function setPrompt(prompt: string) {
    setInput(prompt)
    textareaRef.current?.focus()
  }

  return (
    <div className="space-y-8">
      <header>
        <h1 className="text-3xl font-bold tracking-tight">对话助手</h1>
        <p className="text-muted-foreground">
          多智能体协同诊断 (Coordinator / LogAnalyst / Inspector / Monitor)
        </p>
      </header>

      <Card className="flex flex-col" style={{ minHeight: "calc(100vh - 280px)" }}>
        <CardHeader className="border-b">
          <CardTitle>对话</CardTitle>
          <CardDescription>与多智能体系统交互，执行诊断、查询与分析任务</CardDescription>
        </CardHeader>

        <CardContent className="flex-1 overflow-y-auto p-4" style={{ maxHeight: "calc(100vh - 340px)" }}>
          <div className="space-y-4">
            {messages.map((msg) => (
              <div key={msg.id} className="flex flex-col gap-1">
                {msg.role === "user" ? (
                  <div className="flex items-start justify-end gap-2">
                    <div className="flex max-w-[80%] flex-col items-end gap-1">
                      <div className="flex items-center gap-1 text-xs text-muted-foreground">
                        <User className="h-3 w-3" />
                        <span>我</span>
                        <span className="font-mono opacity-60">
                          {msg.timestamp.toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" })}
                        </span>
                      </div>
                      <div className="rounded-lg bg-primary px-4 py-2 text-sm text-primary-foreground">
                        {msg.content}
                      </div>
                    </div>
                  </div>
                ) : (
                  <div className="flex items-start gap-3">
                    <div className="flex max-w-[80%] flex-col gap-2">
                      {msg.error ? (
                        // Error message display
                        <div className="rounded-md border border-destructive/50 bg-destructive/10 p-3">
                          <div className="flex items-start gap-2 text-sm">
                            <AlertCircle className="h-4 w-4 text-destructive mt-0.5" />
                            <div>
                              <div className="font-medium text-destructive">请求失败</div>
                              <div className="mt-1 text-muted-foreground">{msg.error}</div>
                              <div className="mt-2 text-xs text-muted-foreground">
                                请确保后端服务已启动，并检查 config/settings.yaml 中的 LLM 配置是否正确。
                              </div>
                            </div>
                          </div>
                        </div>
                      ) : (
                        <>
                          <div className="flex items-center gap-2">
                            <Bot className="h-4 w-4 text-muted-foreground" />
                            <Badge variant={agentBadgeVariant[msg.agent!]}>
                              {agentLabels[msg.agent!]}
                            </Badge>
                            <span className="font-mono text-xs text-muted-foreground">
                              {msg.timestamp.toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" })}
                            </span>
                          </div>

                          {msg.thinking && msg.thinking.length > 0 && (
                            <div className="rounded-md border bg-muted/30">
                              <button
                                onClick={() => toggleThinking(msg.id)}
                                className="flex w-full items-center justify-between px-3 py-2 text-xs font-medium text-muted-foreground transition-colors hover:text-foreground"
                              >
                                思考过程
                                <ChevronDown
                                  className={`h-3 w-3 transition-transform ${
                                    expandedThinking.has(msg.id) ? "rotate-180" : ""
                                  }`}
                                />
                              </button>
                              {expandedThinking.has(msg.id) && (
                                <ul className="mx-3 mb-2 space-y-1 text-xs text-muted-foreground">
                                  {msg.thinking.map((t, i) => (
                                    <li key={i}>
                                      <span className="font-medium">{t.step}：</span>
                                      {t.detail}
                                    </li>
                                  ))}
                                </ul>
                              )}
                            </div>
                          )}

                          {msg.tools && msg.tools.length > 0 && (
                            <div className="space-y-2">
                              {msg.tools.map((tool, i) => (
                                <div key={i} className="rounded-md border bg-background p-3">
                                  <div className="flex items-center gap-2 text-xs">
                                    <Wrench className="h-3 w-3 text-muted-foreground" />
                                    <span className="font-mono font-medium text-primary">{tool.tool}</span>
                                  </div>
                                  <div className="mt-1 font-mono text-xs text-muted-foreground">
                                    {Object.entries(tool.params)
                                      .map(([k, v]) => `${k}=${JSON.stringify(v)}`)
                                      .join(" ")}
                                  </div>
                                  <div className="mt-1 text-xs text-muted-foreground">{tool.result}</div>
                                </div>
                              ))}
                            </div>
                          )}

                          {msg.content && (
                            <div className="rounded-lg bg-accent/40 px-4 py-3 text-sm whitespace-pre-wrap">
                              {msg.content}
                            </div>
                          )}
                        </>
                      )}
                    </div>
                  </div>
                )}
              </div>
            ))}

            {isLoading && (
              <div className="flex items-center gap-2 text-sm text-muted-foreground">
                <Bot className="h-4 w-4 animate-pulse" />
                <span>正在思考...</span>
              </div>
            )}

            <div ref={messagesEndRef} />
          </div>
        </CardContent>

        <div className="border-t bg-accent/10 p-4">
          <div className="mb-3 flex flex-wrap gap-2">
            {SUGGESTED_PROMPTS.map((prompt) => (
              <button
                key={prompt}
                onClick={() => setPrompt(prompt)}
                disabled={isLoading}
                className="rounded-full border px-3 py-1 text-xs text-muted-foreground transition-colors hover:border-primary hover:text-foreground disabled:opacity-50"
              >
                {prompt}
              </button>
            ))}
          </div>
          <div className="flex items-end gap-2">
            <textarea
              ref={textareaRef}
              value={input}
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={handleKeyDown}
              placeholder="输入问题，按 Enter 发送，Shift+Enter 换行..."
              rows={1}
              disabled={isLoading}
              className="w-full resize-none rounded-md border bg-background px-3 py-2 text-sm outline-none focus:border-primary focus:ring-1 focus:ring-primary disabled:opacity-50"
              style={{ minHeight: "40px", maxHeight: "120px" }}
            />
            <button
              onClick={handleSend}
              disabled={!input.trim() || isLoading}
              className="rounded-md bg-primary p-2 text-primary-foreground transition-colors hover:bg-primary/90 disabled:opacity-50"
              aria-label="发送"
            >
              <Send className="h-4 w-4" />
            </button>
          </div>
        </div>
      </Card>
    </div>
  )
}
