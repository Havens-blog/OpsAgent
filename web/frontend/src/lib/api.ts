/**
 * API client for the OpsAgent backend.
 *
 * In development, Next.js rewrites /api/v1/* to the Go backend
 * (see next.config.ts), so the frontend can call same-origin URLs.
 * In production, set NEXT_PUBLIC_API_URL to the backend address.
 */

// Use empty string in dev to leverage Next.js rewrites (relative paths)
// In production, set NEXT_PUBLIC_API_URL to the backend address
const API_BASE =
  (process.env.NODE_ENV === "production" ? process.env.NEXT_PUBLIC_API_URL?.replace(/\/$/, "") : "") || ""

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
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
  })

  if (!res.ok) {
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

export interface AgentInfo {
  agents: string[]
  count: number
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

/* ---------- Endpoints ---------- */

export const api = {
  health: () => request<HealthResponse>("/api/v1/health"),

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

  getTopology: () =>
    request<{ status: string; topology: ServiceGraph }>("/api/v1/topology"),
}
