"use client"

import { useEffect, useState } from "react"
import Link from "next/link"
import { Plus, Search } from "lucide-react"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Badge, severityVariant } from "@/components/ui/badge"
import { api, type RCAReport } from "@/lib/api"
import { confidencePct, formatTime } from "@/lib/utils"

function statusVariant(s: string) {
  if (s === "completed") return "success" as const
  if (s === "running") return "default" as const
  return "danger" as const
}

function statusLabel(s: string) {
  return s === "completed" ? "已完成" : s === "running" ? "进行中" : "失败"
}

export default function RCAListPage() {
  const [reports, setReports] = useState<RCAReport[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [filter, setFilter] = useState<"all" | "running" | "completed" | "failed">(
    "all"
  )
  const [query, setQuery] = useState("")

  useEffect(() => {
    let cancelled = false
    api
      .listRCA()
      .then((res) => {
        if (cancelled) return
        setReports(res?.reports ?? [])
        setError(null)
      })
      .catch(() => {
        if (cancelled) return
        setError("无法加载 RCA 列表（后端 /api/v1/rca 不可达或返回错误）")
      })
      .finally(() => !cancelled && setLoading(false))
    return () => {
      cancelled = true
    }
  }, [])

  const filtered = reports.filter((r) => {
    if (filter !== "all" && r.status !== filter) return false
    if (query) {
      const q = query.toLowerCase()
      return (
        r.incident_id.toLowerCase().includes(q) ||
        r.description.toLowerCase().includes(q) ||
        r.affected_services.some((s) => s.toLowerCase().includes(q))
      )
    }
    return true
  })

  const counts = {
    total: reports.length,
    running: reports.filter((r) => r.status === "running").length,
    completed: reports.filter((r) => r.status === "completed").length,
    failed: reports.filter((r) => r.status === "failed").length,
  }

  return (
    <div className="space-y-8">
      <header className="flex items-start justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">RCA 分析</h1>
          <p className="text-muted-foreground">
            查看和管理所有根因分析任务
          </p>
        </div>
        <Link
          href="/rca/new"
          className="flex items-center gap-2 rounded-md bg-primary px-4 py-2 text-sm font-semibold text-primary-foreground transition-colors hover:bg-primary/90"
        >
          <Plus className="h-4 w-4" />
          新建分析
        </Link>
      </header>

      {error && (
        <p className="rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-sm text-red-600 dark:text-red-400">
          {error}
        </p>
      )}

      <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
        <StatCard label="总计" value={counts.total} />
        <StatCard label="进行中" value={counts.running} tone="default" />
        <StatCard label="已完成" value={counts.completed} tone="success" />
        <StatCard label="失败" value={counts.failed} tone="danger" />
      </div>

      <div className="flex flex-wrap items-center gap-3">
        <div className="relative flex-1 min-w-[240px]">
          <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="搜索 ID、服务、描述..."
            className="w-full rounded-md border bg-background py-2 pl-9 pr-3 text-sm outline-none focus:border-primary focus:ring-1 focus:ring-primary"
          />
        </div>
        <div className="flex gap-1 rounded-md border p-1">
          {(["all", "running", "completed", "failed"] as const).map((f) => (
            <button
              key={f}
              onClick={() => setFilter(f)}
              className={
                "rounded px-3 py-1 text-xs font-medium transition-colors " +
                (filter === f
                  ? "bg-primary text-primary-foreground"
                  : "text-muted-foreground hover:text-foreground")
              }
            >
              {f === "all" ? "全部" : statusLabel(f)}
            </button>
          ))}
        </div>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>分析记录</CardTitle>
          <CardDescription>
            {loading
              ? "加载中..."
              : `显示 ${filtered.length} 条，共 ${reports.length} 条`}
          </CardDescription>
        </CardHeader>
        <CardContent className="p-0">
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead className="border-b text-left text-xs text-muted-foreground">
                <tr>
                  <th className="px-4 py-3 font-medium">RCA ID</th>
                  <th className="px-4 py-3 font-medium">描述</th>
                  <th className="px-4 py-3 font-medium">状态</th>
                  <th className="px-4 py-3 font-medium">严重度</th>
                  <th className="px-4 py-3 font-medium">创建时间</th>
                  <th className="px-4 py-3 font-medium">置信度</th>
                  <th className="px-4 py-3 font-medium"></th>
                </tr>
              </thead>
              <tbody>
                {filtered.map((r) => (
                  <tr
                    key={r.incident_id}
                    className="border-b transition-colors last:border-0 hover:bg-accent/30"
                  >
                    <td className="px-4 py-3 font-mono text-xs text-muted-foreground">
                      {r.incident_id}
                    </td>
                    <td className="px-4 py-3">
                      <div className="font-medium">{r.description}</div>
                      <div className="text-xs text-muted-foreground">
                        {(r.affected_services || []).join(", ")}
                      </div>
                    </td>
                    <td className="px-4 py-3">
                      <Badge variant={statusVariant(r.status)}>
                        {statusLabel(r.status)}
                      </Badge>
                    </td>
                    <td className="px-4 py-3">
                      <Badge variant={severityVariant(r.severity)}>
                        {r.severity}
                      </Badge>
                    </td>
                    <td className="px-4 py-3 text-xs text-muted-foreground">
                      {formatTime(r.start_time)}
                    </td>
                    <td className="px-4 py-3">
                      {r.status === "completed" ? (
                        <div className="flex items-center gap-2">
                          <div className="h-1.5 w-16 overflow-hidden rounded-full bg-muted">
                            <div
                              className="h-full rounded-full bg-primary"
                              style={{
                                width: `${(r.confidence || 0) * 100}%`,
                              }}
                            />
                          </div>
                          <span className="text-xs text-muted-foreground">
                            {confidencePct(r.confidence)}
                          </span>
                        </div>
                      ) : (
                        <span className="text-xs text-muted-foreground">—</span>
                      )}
                    </td>
                    <td className="px-4 py-3 text-right">
                      <Link
                        href={`/rca/${r.incident_id}`}
                        className="text-xs font-medium text-primary hover:underline"
                      >
                        查看
                      </Link>
                    </td>
                  </tr>
                ))}
                {filtered.length === 0 && (
                  <tr>
                    <td
                      colSpan={7}
                      className="px-4 py-12 text-center text-muted-foreground"
                    >
                      没有匹配的 RCA 记录
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}

function StatCard({
  label,
  value,
  tone = "neutral",
}: {
  label: string
  value: number
  tone?: "neutral" | "success" | "danger" | "default"
}) {
  const toneCls =
    tone === "success"
      ? "text-emerald-500"
      : tone === "danger"
        ? "text-red-500"
        : tone === "default"
          ? "text-blue-500"
          : "text-foreground"
  return (
    <Card>
      <CardContent className="py-4">
        <div className="text-xs text-muted-foreground">{label}</div>
        <div className={"mt-1 text-2xl font-bold " + toneCls}>{value}</div>
      </CardContent>
    </Card>
  )
}
