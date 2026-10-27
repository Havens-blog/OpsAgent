/**
 * API client for the OpsAgent backend.
 *
 * The browser ALWAYS calls same-origin {basePath}/api/v1/* (relative paths).
 * Those requests are handled by the server-side proxy at
 * src/app/api/v1/[...path]/route.ts, which injects the EIAM/bearer credential
 * and forwards to the Go backend. The browser never holds the token and never
 * talks to the backend directly — so a direct backend address must NOT be used
 * here (that would bypass the proxy and fail auth).
 *
 * API_BASE must equal next.config basePath (/opsagent): Next.js auto-prefixes
 * the route handler to /opsagent/api/v1/*, but fetch() does NOT inherit
 * basePath, so the base is prepended explicitly here. Kept env-overridable and
 * in lock-step with next.config's NEXT_BASE_PATH default.
 */

// Same-origin, basePath-prefixed: the Next.js server proxies to the backend.
const API_BASE = process.env.NEXT_PUBLIC_BASE_PATH ?? "/opsagent"

// Where to send an unauthenticated browser to start EIAM login. Browser-
// visible (NEXT_PUBLIC_), not a secret. Defaults to the console login route,
// matching the legacy ops console. The current URL is appended as `redirect`
// so EIAM can return the user after login.
const EIAM_LOGIN_URL =
  process.env.NEXT_PUBLIC_EIAM_LOGIN_URL || "/console/login"

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

// redirectToLogin sends the browser to EIAM login. It NEVER clears any cookie
// (the EIAM session cookie is owned by EIAM) and guards against redirect loops
// by not re-navigating when already on the login route.
function redirectToLogin() {
  if (typeof window === "undefined") return
  const current = window.location.href
  if (window.location.pathname.startsWith(EIAM_LOGIN_URL)) return
  const sep = EIAM_LOGIN_URL.includes("?") ? "&" : "?"
  window.location.href = `${EIAM_LOGIN_URL}${sep}redirect=${encodeURIComponent(current)}`
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const url = `${API_BASE}${path}`
  const res = await fetch(url, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      ...(init?.headers || {}),
    },
    // Always fetch fresh data — this is a live operations console.
    cache: "no-store",
    // Send the same-origin EIAM session cookie so the proxy can forward it.
    credentials: "same-origin",
  })

  if (!res.ok) {
    // 401 = unauthenticated → bounce to EIAM login (contract: never clear the
    // session cookie). 403 (tenant not selected) is deliberately NOT a login
    // redirect — the user is authenticated and selects a tenant via EIAM.
    if (res.status === 401) {
      redirectToLogin()
    }
    let detail = res.statusText
    try {
      const body = await res.json()
      detail = body?.error?.message || body?.message || detail
    } catch {
      /* keep statusText */
    }
    throw new ApiError(res.status, detail)
  }

  // Some endpoints return 202/204 with empty body
  const text = await res.text()
  if (!text) return undefined as unknown as T
  return JSON.parse(text) as T
}

export interface StreamCallbacks {
  onDelta: (content: string) => void
  onDone: (messageId: string, conversationId: string) => void
  onError: (message: string) => void
  onToolCall?: (event: ToolEvent) => void
  onToolResult?: (event: ToolEvent) => void
  onApprovalRequired?: (event: ApprovalRequiredEvent) => void
  signal?: AbortSignal
}

function processSSEEvent(raw: string, callbacks: StreamCallbacks) {
  const line = raw.trim()
  if (!line.startsWith("data:")) return
  const data = line.slice(5).trim()
  if (!data) return

  let event: StreamEvent
  try {
    event = JSON.parse(data) as StreamEvent
  } catch {
    return
  }

  if (event.type === "delta") callbacks.onDelta(event.content)
  else if (event.type === "done") callbacks.onDone(event.message_id, event.conversation_id)
  else if (event.type === "error") callbacks.onError(event.message)
  else if (event.type === "tool_call")
    callbacks.onToolCall?.({
      call_id: event.call_id,
      tool_name: event.tool_name,
      params: event.params,
      status: event.status as ToolEvent["status"],
    })
  else if (event.type === "tool_result")
    callbacks.onToolResult?.({
      call_id: event.call_id,
      tool_name: event.tool_name,
      status: event.status as ToolEvent["status"],
      result: event.result,
      error: event.error,
    })
  else if (event.type === "approval_required")
    callbacks.onApprovalRequired?.({
      approval_id: event.approval_id,
      tool_name: event.tool_name,
      params: event.params,
      reason: event.reason,
    })
}

async function sendMessageStream(
  conversationId: string,
  content: string,
  callbacks: StreamCallbacks,
  clientMessageId?: string
): Promise<void> {
  const res = await fetch(
    `${API_BASE}/api/v1/conversations/${conversationId}/messages`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        content,
        ...(clientMessageId ? { client_message_id: clientMessageId } : {}),
      }),
      cache: "no-store",
      credentials: "same-origin",
      signal: callbacks.signal,
    }
  )

  if (!res.ok || !res.body) {
    if (res.status === 401) redirectToLogin()
    let detail = res.statusText
    try {
      const body = await res.json()
      detail = body?.error?.message || detail
    } catch {
      /* keep statusText */
    }
    throw new ApiError(res.status, detail)
  }

  const reader = res.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ""

  try {
    for (;;) {
      const { value, done } = await reader.read()
      if (done) break
      buffer += decoder.decode(value, { stream: true })

      let idx: number
      while ((idx = buffer.indexOf("\n\n")) !== -1) {
        const raw = buffer.slice(0, idx)
        buffer = buffer.slice(idx + 2)
        processSSEEvent(raw, callbacks)
      }
    }
    if (buffer.trim()) processSSEEvent(buffer, callbacks)
  } finally {
    reader.releaseLock()
  }
}

/* ---------- Types ---------- */

export interface HealthResponse {
  status: string
  timestamp: string
  version: string
  data?: { agents_registered: number }
}

export interface Incident {
  id: string
  severity: string
  title: string
  description: string
  start_time: string
  end_time?: string
  status: string
  affected_services?: string[]
  evidence?: Evidence[]
  root_cause?: string
  confidence?: number
}

export interface Evidence {
  type: string
  source: string
  start_time: string
  end_time: string
  description: string
}

export interface IncidentTimeline {
  time: string
  event: string
}

/** A single incident with root-cause + evidence, as returned by
 * GET /api/v1/incidents/:id. */
export interface IncidentDetail extends Incident {
  evidence: Evidence[]
  timeline: IncidentTimeline[]
}

export interface AgentDetail {
  id: string
  name: string
  type: string
  description: string
  capabilities: string[]
}

export interface AgentInfo {
  agents: string[]
  count: number
  details?: AgentDetail[]
}

export interface DataSourceDimension {
  dimension: string
  source: string
  real: boolean
  env: string[]
}

export interface DataSourceStatus {
  dimensions: DataSourceDimension[]
  real_count: number
  total: number
  all_mock: boolean
}

export interface AgentExecResult {
  agent_id: string
  task_id: string
  success: boolean
  response: string
  data?: Record<string, unknown>
  tools_used?: string[]
  duration_ms: number
  error?: string
}

export interface ServiceNode {
  id: string
  name: string
  type: string
  health: string
  error_rate: number
  latency: number
  throughput: number
}

export interface ServiceEdge {
  id: string
  from_node_id: string
  to_node_id: string
  type: string
  error_rate: number
  latency: number
  throughput: number
}

export interface ServiceGraph {
  nodes: ServiceNode[]
  edges: ServiceEdge[]
}

export interface DeploymentEvent {
  id: string
  cicd_system: string
  project: string
  environment: string
  service: string
  commit_sha: string
  commit_message: string
  started_at: string
  status: string
}

/* ---------- RCA types ---------- */

/** Multi-dimensional scoring breakdown (mirrors models.ScoreBreakdown). */
export interface ScoreBreakdown {
  time_correlation: number
  topology_score: number
  change_proximity: number
  metric_severity: number
  error_frequency: number
  trace_evidence: number
}

/** A single root-cause candidate hypothesis. */
export interface Hypothesis {
  type: string // error_spike | metric_anomaly | change | dependency | latency | resource
  service: string
  description: string
  metric?: string
  change_id?: string
  evidence: string[]
  confidence: number
}

/** A hypothesis with its computed score and breakdown. */
export interface ScoredHypothesis extends Hypothesis {
  score: number
  breakdown: ScoreBreakdown
}

/** A complete RCA report — the payload of GET /api/v1/rca/:id. */
export interface RCAReport {
  incident_id: string
  severity: string
  description: string
  start_time: string
  end_time?: string
  affected_services: string[]
  status: "running" | "completed" | "failed"
  candidates: ScoredHypothesis[]
  top_candidate?: ScoredHypothesis
  confidence: number
  duration_ms?: number
  generated_at?: string
}

/* ---------- Chat types ---------- */

export interface ChatMessage {
  role: "user" | "assistant"
  content: string
}

export interface ChatResponse {
  reply: string
  agent: string
  timestamp: string
  model?: string
  thinking?: { step: string; detail: string }[]
  tools?: { tool: string; params: Record<string, unknown>; result: string }[]
}

/* ---------- Conversation types ---------- */

export interface Conversation {
  id: string
  tenant_id: number
  user_id: number
  title: string
  created_at: string
  updated_at: string
}

export interface ConversationMessage {
  id: string
  conversation_id: string
  role: "user" | "assistant"
  content: string
  created_at: string
}

export interface ToolEvent {
  call_id: string
  tool_name: string
  params?: Record<string, unknown>
  status: "running" | "success" | "failed"
  result?: string
  error?: string
}

export interface ApprovalRequiredEvent {
  approval_id: string
  tool_name: string
  params: Record<string, unknown>
  reason: string
}

export interface Approval {
  approval_id: string
  tool_name: string
  params: Record<string, unknown>
  reason: string
  conversation_id: string
  tenant_id: number
  user_id: number
  param_hash: string
  scope: string
  expiry_seconds: number
  expires_at: string
  status: string
  created_at: string
}

export type StreamEvent =
  | { type: "delta"; content: string }
  | { type: "done"; message_id: string; conversation_id: string }
  | { type: "error"; message: string }
  | {
      type: "tool_call"
      call_id: string
      tool_name: string
      params: Record<string, unknown>
      status: string
    }
  | {
      type: "tool_result"
      call_id: string
      tool_name: string
      status: string
      result?: string
      error?: string
    }
  | {
      type: "approval_required"
      approval_id: string
      tool_name: string
      params: Record<string, unknown>
      reason: string
    }

/* ---------- LLM config types ---------- */

export interface ProviderInfo {
  id: string
  name: string
  base_url: string
  default_model: string
  models: string[]
}

export interface LLMConfig {
  provider: string
  base_url: string
  model: string
  has_api_key: boolean
  api_key_masked: string
  max_tokens: number
  temperature: number
  timeout: number
  available: boolean
}

export interface LLMConfigInput {
  provider: string
  base_url?: string
  model: string
  api_key?: string
  max_tokens?: number
  temperature?: number
  timeout?: number
}

export interface LLMTestResult {
  success: boolean
  error?: string
  model?: string
  latency_ms?: number
  reply?: string
}

export interface SaveLLMResult {
  saved: boolean
  persisted: boolean
  persist_error?: string
  config: LLMConfig
}

export interface ModelsResult {
  models: string[]
  source: "provider" | "fallback"
  error?: string
}

export interface NotificationConfig {
  platform: string
  webhook_url: string
  has_secret: boolean
  secret_masked: string
  severities: string[]
  start_time: string
  end_time: string
}

export interface NotificationConfigInput {
  platform: string
  webhook_url: string
  secret?: string
  severities: string[]
  start_time: string
  end_time: string
}

export interface AuditLogEntry {
  time: string
  user: string
  operation: string
  ip: string
  result: "success" | "failed"
  detail?: string
}

export interface SecurityConfig {
  enable_audit_log: boolean
  audit_log_path: string
  audit_retention_days: number
  ip_whitelist: string
  rate_limit: number
  audit_logs: AuditLogEntry[]
}

export interface SecurityConfigInput {
  enable_audit_log: boolean
  audit_log_path: string
  audit_retention_days: number
  ip_whitelist: string
  rate_limit: number
}

export interface NotificationTestInput {
  platform: string
  webhook_url: string
  secret?: string
}

export interface NotificationTestResult {
  success: boolean
  platform: string
  http_status?: number
  response?: string
  error?: string
  latency_ms?: number
}

interface SaveResult {
  status: string
  persisted: boolean
  error?: string
}

/* ---------- Endpoints ---------- */

export const api = {
  health: () => request<HealthResponse>("/api/v1/health"),

  listProviders: () =>
    request<{ providers: ProviderInfo[] }>("/api/v1/providers"),

  getLLMConfig: () => request<LLMConfig>("/api/v1/config/llm"),

  saveLLMConfig: (body: LLMConfigInput) =>
    request<SaveLLMResult>("/api/v1/config/llm", {
      method: "PUT",
      body: JSON.stringify(body),
    }),

  testLLMConfig: (body: LLMConfigInput) =>
    request<LLMTestResult>("/api/v1/config/llm/test", {
      method: "POST",
      body: JSON.stringify(body),
    }),

  listModels: (body: LLMConfigInput) =>
    request<ModelsResult>("/api/v1/config/llm/models", {
      method: "POST",
      body: JSON.stringify(body),
    }),

  triggerRCA: (body: {
    incident_id: string
    start_time: string
    end_time: string
    severity: string
    affected_services: string[]
    description?: string
  }) =>
    request<{ incident_id: string; status: string }>("/api/v1/rca/analyze", {
      method: "POST",
      body: JSON.stringify(body),
    }),

  listRCA: () =>
    request<{ reports: RCAReport[]; count: number }>("/api/v1/rca"),

  getRCA: (id: string) => request<RCAReport>(`/api/v1/rca/${id}`),

  getIncidents: () =>
    request<{ incidents: Incident[]; count: number }>("/api/v1/incidents"),

  getIncident: (id: string) => request<IncidentDetail>(`/api/v1/incidents/${id}`),

  queryLogs: (body: {
    query: string
    start_time: string
    end_time: string
    service?: string
    level?: string
    limit?: number
  }) =>
    request<{ query: string; status: string; count: number }>("/api/v1/logs/query", {
      method: "POST",
      body: JSON.stringify(body),
    }),

  queryMetrics: (body: { query: string; start_time: string; end_time: string }) =>
    request<{ query: string; status: string }>("/api/v1/metrics/query", {
      method: "POST",
      body: JSON.stringify(body),
    }),

  listAgents: () => request<AgentInfo>("/api/v1/agents"),

  executeAgent: (agentId: string, description: string, params?: Record<string, unknown>) =>
    request<AgentExecResult>(`/api/v1/agents/${agentId}`, {
      method: "POST",
      body: JSON.stringify({ description, params }),
    }),

  getDataSources: () =>
    request<DataSourceStatus>("/api/v1/config/datasources"),

  getNotificationConfig: () =>
    request<NotificationConfig>("/api/v1/config/notifications"),

  saveNotificationConfig: (body: NotificationConfigInput) =>
    request<SaveResult & { config: NotificationConfig }>("/api/v1/config/notifications", {
      method: "PUT",
      body: JSON.stringify(body),
    }),

  testNotificationConfig: (body: NotificationTestInput) =>
    request<NotificationTestResult>("/api/v1/config/notifications/test", {
      method: "POST",
      body: JSON.stringify(body),
    }),

  getSecurityConfig: () =>
    request<SecurityConfig>("/api/v1/config/security"),

  saveSecurityConfig: (body: SecurityConfigInput) =>
    request<SaveResult & { config: SecurityConfig }>("/api/v1/config/security", {
      method: "PUT",
      body: JSON.stringify(body),
    }),

  getTopology: () =>
    request<{ status: string; topology: ServiceGraph }>("/api/v1/topology"),

  chat: (messages: ChatMessage[]) =>
    request<ChatResponse>("/api/v1/chat", {
      method: "POST",
      body: JSON.stringify({ messages }),
    }),

  listConversations: () =>
    request<{ conversations: Conversation[]; count: number }>(
      "/api/v1/conversations"
    ),

  createConversation: (title?: string) =>
    request<{ conversation: Conversation }>("/api/v1/conversations", {
      method: "POST",
      body: JSON.stringify({ title }),
    }),

  getConversation: (id: string) =>
    request<{ conversation: Conversation; messages: ConversationMessage[] }>(
      `/api/v1/conversations/${id}`
    ),

  deleteConversation: (id: string) =>
    request<void>(`/api/v1/conversations/${id}`, { method: "DELETE" }),

  sendMessageStream,

  listApprovals: () =>
    request<{ approvals: Approval[]; count: number }>("/api/v1/approvals"),

  approveApproval: (id: string) =>
    request<{ status: string }>(`/api/v1/approvals/${id}/approve`, {
      method: "POST",
    }),

  rejectApproval: (id: string) =>
    request<{ status: string }>(`/api/v1/approvals/${id}/reject`, {
      method: "POST",
    }),
}
