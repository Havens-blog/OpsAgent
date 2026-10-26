"use client"

import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { GitBranch, Settings, AlertCircle } from "lucide-react"

type ChangeStatus = "success" | "failed" | "rolled_back"

interface ChangeItem {
  id: string
  kind: "deployment" | "config"
  system: string
  service: string
  message: string
  author: string
  time: string
  status: ChangeStatus
  correlation?: number
}

const CHANGES: ChangeItem[] = [
  {
    id: "gitlab-deploy-001",
    kind: "deployment",
    system: "gitlab_ci",
    service: "order-service",
    message: "Fix: Database connection pool exhaustion",
    author: "John Doe",
    time: new Date(Date.now() - 12 * 60 * 1000).toISOString(),
    status: "success",
    correlation: 0.62,
  },
  {
    id: "config-change-001",
    kind: "config",
    system: "kubernetes",
    service: "order-service",
    message: "ConfigMap app-config 更新: DATABASE_URL, REDIS_HOST",
    author: "ops-bot",
    time: new Date(Date.now() - 18 * 60 * 1000).toISOString(),
    status: "success",
    correlation: 0.71,
  },
  {
    id: "github-deploy-002",
    kind: "deployment",
    system: "github_actions",
    service: "payment-service",
    message: "feat: Add payment gateway integration",
    author: "Jane Smith",
    time: new Date(Date.now() - 45 * 60 * 1000).toISOString(),
    status: "rolled_back",
    correlation: 0.88,
  },
  {
    id: "config-change-002",
    kind: "config",
    system: "kubernetes",
    service: "api-gateway",
    message: "Secret api-keys 更新: API_KEY",
    author: "security",
    time: new Date(Date.now() - 2 * 60 * 60 * 1000).toISOString(),
    status: "success",
    correlation: 0.15,
  },
  {
    id: "gitlab-deploy-003",
    kind: "deployment",
    system: "gitlab_ci",
    service: "user-service",
    message: "chore: bump dependencies",
    author: "CI",
    time: new Date(Date.now() - 5 * 60 * 60 * 1000).toISOString(),
    status: "failed",
    correlation: 0.09,
  },
]

function statusBadge(s: ChangeStatus) {
  if (s === "failed") return <Badge variant="danger">{s}</Badge>
  if (s === "rolled_back") return <Badge variant="warning">{s}</Badge>
  return <Badge variant="success">{s}</Badge>
}

function correlationColor(c?: number) {
  if (c == null) return "bg-muted"
  if (c >= 0.7) return "bg-destructive"
  if (c >= 0.4) return "bg-status-degraded"
  return "bg-status-healthy"
}

export default function ChangesPage() {
  return (
    <div className="space-y-8">
      <header>
        <h1 className="text-3xl font-bold tracking-tight">Changes</h1>
        <p className="text-muted-foreground">
          变更关联分析 — CI/CD 部署与配置变更的时间窗口关联
        </p>
      </header>

      <div className="grid gap-4 md:grid-cols-3">
        <Card>
          <CardContent className="py-4">
            <div className="text-xs uppercase tracking-wide text-muted-foreground">
              近 24h 变更
            </div>
            <div className="mt-1 text-2xl font-bold">{CHANGES.length}</div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="py-4">
            <div className="text-xs uppercase tracking-wide text-muted-foreground">
              高关联变更
            </div>
            <div className="mt-1 text-2xl font-bold text-destructive">
              {CHANGES.filter((c) => (c.correlation ?? 0) >= 0.7).length}
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="py-4">
            <div className="text-xs uppercase tracking-wide text-muted-foreground">
              失败 / 回滚
            </div>
            <div className="mt-1 text-2xl font-bold text-status-degraded">
              {CHANGES.filter((c) => c.status !== "success").length}
            </div>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>变更时间线</CardTitle>
          <CardDescription>
            按时间倒序 — 关联分数 &ge; 0.7 的变更最可能是故障根因
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className="space-y-4">
            {CHANGES.sort(
              (a, b) =>
                new Date(b.time).getTime() - new Date(a.time).getTime()
            ).map((c) => {
              const Icon = c.kind === "deployment" ? GitBranch : Settings
              return (
                <div
                  key={c.id}
                  className="flex gap-4 rounded-md border bg-accent/20 p-4"
                >
                  <div className="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-md bg-secondary">
                    <Icon className="h-5 w-5" />
                  </div>
                  <div className="flex-1 space-y-1">
                    <div className="flex items-center gap-2">
                      <span className="font-semibold">{c.service}</span>
                      <Badge variant="outline" className="font-mono text-xs">
                        {c.system}
                      </Badge>
                      {statusBadge(c.status)}
                    </div>
                    <div className="text-sm">{c.message}</div>
                    <div className="flex items-center gap-3 text-xs text-muted-foreground">
                      <span>{c.author}</span>
                      <span>·</span>
                      <span>{new Date(c.time).toLocaleString("zh-CN", { hour12: false })}</span>
                    </div>
                  </div>
                  <div className="flex w-32 flex-col items-end justify-center">
                    {(c.correlation ?? 0) >= 0.4 && (
                      <AlertCircle className="mb-1 h-4 w-4 text-status-degraded" />
                    )}
                    <div className="text-xs text-muted-foreground">
                      关联分数
                    </div>
                    <div className="font-mono text-sm font-bold">
                      {c.correlation?.toFixed(2) ?? "—"}
                    </div>
                    <div className="mt-1 h-1.5 w-20 overflow-hidden rounded-full bg-muted">
                      <div
                        className={`h-full rounded-full ${correlationColor(
                          c.correlation
                        )}`}
                        style={{
                          width: `${(c.correlation ?? 0) * 100}%`,
                        }}
                      />
                    </div>
                  </div>
                </div>
              )
            })}
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
