"use client"

import { use, useState, useEffect } from "react"
import Link from "next/link"
import { ArrowLeft } from "lucide-react"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Badge, severityVariant } from "@/components/ui/badge"
import { api, ApiError, type RCAReport, type ScoredHypothesis } from "@/lib/api"
import { confidencePct, formatTime } from "@/lib/utils"

function statusVariant(s: string) {
  if (s === "completed") return "success" as const
  if (s === "running") return "default" as const
  return "danger" as const
}

function statusLabel(s: string) {
  return s === "completed" ? "已完成" : s === "running" ? "进行中" : "失败"
}

function formatDuration(ms?: number): string {
  if (!ms) return "—"
  const minutes = Math.floor(ms / 60000)
  const seconds = Math.floor((ms % 60000) / 1000)
  return `${minutes}m ${seconds}s`
}

function hypothesisTypeVariant(type: string) {
  if (type === "resource" || type === "metric_anomaly") return "warning" as const
  if (type === "change" || type === "error_spike") return "danger" as const
  return "default" as const
}

interface ScoreBreakdownRow {
  label: string
  weight: string
  key: keyof ScoredHypothesis["breakdown"]
}

const BREAKDOWN_ROWS: ScoreBreakdownRow[] = [
  { label: "时间相关性", weight: "50%", key: "time_correlation" },
  { label: "拓扑关系", weight: "25%", key: "topology_score" },
  { label: "变更检测", weight: "15%", key: "change_proximity" },
  { label: "指标严重度", weight: "5%", key: "metric_severity" },
  { label: "日志模式", weight: "3%", key: "error_frequency" },
  { label: "链路追踪", weight: "2%", key: "trace_evidence" },
]

export default function RCADetailPage({
  params,
}: {
  params: Promise<{ id: string }>
}) {
  const { id } = use(params)
  const [report, setReport] = useState<RCAReport | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [selectedEvidence, setSelectedEvidence] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    api
      .getRCA(id)
      .then((res) => {
        if (cancelled) return
        setReport(res)
        setError(null)
      })
      .catch((err) => {
        if (cancelled) return
        setError(
          err instanceof ApiError && err.status === 404
            ? "未找到此 RCA 记录"
            : "加载 RCA 详情失败（后端不可达或返回错误）"
        )
      })
      .finally(() => !cancelled && setLoading(false))
    return () => {
      cancelled = true
    }
  }, [id])

  if (loading) {
    return (
      <div className="space-y-8">
        <div className="flex items-center gap-3 text-sm text-muted-foreground">
          <Link href="/rca" className="flex items-center gap-1 hover:text-foreground">
            <ArrowLeft className="h-4 w-4" />
            RCA 分析
          </Link>
        </div>
        <p className="text-muted-foreground">加载中...</p>
      </div>
    )
  }

  if (!report) {
    return (
      <div className="space-y-8">
        <div className="flex items-center gap-3 text-sm text-muted-foreground">
          <Link href="/rca" className="flex items-center gap-1 hover:text-foreground">
            <ArrowLeft className="h-4 w-4" />
            RCA 分析
          </Link>
        </div>
        <p className="text-muted-foreground">{error ?? "未找到 RCA 记录"}</p>
      </div>
    )
  }

  const sortedCandidates = [...report.candidates].sort((a, b) => b.score - a.score)
  const evidenceToShow = report.top_candidate?.evidence?.length
    ? report.top_candidate.evidence
    : sortedCandidates.flatMap((c) => c.evidence)

  return (
    <div className="space-y-8">
      <div className="flex items-center gap-3 text-sm text-muted-foreground">
        <Link href="/rca" className="flex items-center gap-1 hover:text-foreground">
          <ArrowLeft className="h-4 w-4" />
          RCA 分析
        </Link>
      </div>

      <header className="flex items-start justify-between">
        <div className="space-y-2">
          <div className="flex items-center gap-3">
            <Badge variant={severityVariant(report.severity)}>
              {report.severity}
            </Badge>
            <Badge variant={statusVariant(report.status)}>
              {statusLabel(report.status)}
            </Badge>
            <span className="font-mono text-xs text-muted-foreground">
              {report.incident_id}
            </span>
          </div>
          <h1 className="text-3xl font-bold tracking-tight">
            {report.description}
          </h1>
          <div className="flex gap-4 text-xs text-muted-foreground">
            <span>开始: {formatTime(report.start_time)}</span>
            {report.end_time && <span>结束: {formatTime(report.end_time)}</span>}
            <span>耗时: {formatDuration(report.duration_ms)}</span>
          </div>
        </div>
      </header>

      <div className="grid gap-6 lg:grid-cols-3">
        <Card className="lg:col-span-2 border-primary/40">
          <CardHeader>
            <CardTitle>根因分析结论</CardTitle>
            <CardDescription>由 Coordinator Agent 汇总多智能体证据</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            {report.top_candidate ? (
              <>
                <div className="rounded-md bg-accent/40 p-4">
                  <div className="text-sm text-muted-foreground">推断根因</div>
                  <div className="mt-1 text-base font-medium">
                    {report.top_candidate.description}
                  </div>
                </div>
                <div className="flex items-center gap-4">
                  <div className="flex-1">
                    <div className="mb-1 flex justify-between text-xs">
                      <span className="text-muted-foreground">置信度</span>
                      <span className="font-mono">
                        {confidencePct(report.confidence)}
                      </span>
                    </div>
                    <div className="h-2 overflow-hidden rounded-full bg-muted">
                      <div
                        className="h-full rounded-full bg-primary"
                        style={{ width: `${(report.confidence || 0) * 100}%` }}
                      />
                    </div>
                  </div>
                </div>
                <div className="flex flex-wrap gap-2">
                  {(report.affected_services || []).map((s) => (
                    <Badge key={s} variant="outline" className="font-mono">
                      {s}
                    </Badge>
                  ))}
                </div>
              </>
            ) : (
              <p className="text-sm text-muted-foreground">
                {report.status === "running"
                  ? "RCA 分析进行中，尚未生成根因结论..."
                  : "未能生成根因结论"}
              </p>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>事件时间线</CardTitle>
            <CardDescription>基于本次分析的真实时间戳</CardDescription>
          </CardHeader>
          <CardContent>
            <ol className="space-y-3">
              <li className="flex gap-3">
                <span className="w-28 shrink-0 font-mono text-xs text-primary">
                  {formatTime(report.start_time)}
                </span>
                <span className="text-sm">事件开始</span>
              </li>
              {report.end_time && (
                <li className="flex gap-3">
                  <span className="w-28 shrink-0 font-mono text-xs text-primary">
                    {formatTime(report.end_time)}
                  </span>
                  <span className="text-sm">事件结束</span>
                </li>
              )}
              {report.generated_at && (
                <li className="flex gap-3">
                  <span className="w-28 shrink-0 font-mono text-xs text-primary">
                    {formatTime(report.generated_at)}
                  </span>
                  <span className="text-sm">
                    RCA 分析完成
                    {report.duration_ms
                      ? `（耗时 ${formatDuration(report.duration_ms)}）`
                      : ""}
                  </span>
                </li>
              )}
            </ol>
          </CardContent>
        </Card>
      </div>

      {report.top_candidate?.breakdown && (
        <Card>
          <CardHeader>
            <CardTitle>评分维度</CardTitle>
            <CardDescription>
              根因候选的多维度评分分解
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            {BREAKDOWN_ROWS.map((row) => {
              const value = report.top_candidate?.breakdown?.[row.key] ?? 0
              return (
                <div key={row.key}>
                  <div className="mb-1 flex justify-between text-xs">
                    <span className="text-muted-foreground">
                      {row.label} <span className="font-mono">({row.weight})</span>
                    </span>
                    <span className="font-mono text-muted-foreground">
                      {value.toFixed(2)}
                    </span>
                  </div>
                  <div className="h-2 overflow-hidden rounded-full bg-muted">
                    <div
                      className="h-full rounded-full bg-primary"
                      style={{ width: `${value * 100}%` }}
                    />
                  </div>
                </div>
              )
            })}
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader>
          <CardTitle>
            候选根因 ({sortedCandidates.length})
          </CardTitle>
          <CardDescription>
            按综合评分降序排列的根因假设
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          {sortedCandidates.length > 0 ? (
            sortedCandidates.map((candidate, index) => {
              const isTopCandidate = report.top_candidate?.service === candidate.service && report.top_candidate?.type === candidate.type
              return (
                <div
                  key={`${candidate.type}-${candidate.service}-${index}`}
                  className={
                    "rounded-md border p-3 " +
                    (isTopCandidate
                      ? "border-primary/40 bg-accent/30"
                      : "border-border")
                  }
                >
                  <div className="flex items-start justify-between gap-3">
                    <div className="flex-1 space-y-1">
                      <div className="flex items-center gap-2">
                        <Badge variant={hypothesisTypeVariant(candidate.type)}>
                          {candidate.type}
                        </Badge>
                        <span className="font-mono text-xs text-muted-foreground">
                          {candidate.service}
                        </span>
                      </div>
                      <div className="text-sm">{candidate.description}</div>
                    </div>
                    <div className="flex flex-col items-end gap-1 min-w-[100px]">
                      <div className="flex items-center gap-2">
                        <div className="h-1.5 w-16 overflow-hidden rounded-full bg-muted">
                          <div
                            className="h-full rounded-full bg-primary"
                            style={{ width: `${candidate.score * 100}%` }}
                          />
                        </div>
                        <span className="font-mono text-xs text-muted-foreground">
                          {candidate.score.toFixed(2)}
                        </span>
                      </div>
                    </div>
                  </div>
                </div>
              )
            })
          ) : (
            <p className="text-sm text-muted-foreground">
              {report.status === "running"
                ? "RCA 分析进行中，尚未生成候选根因..."
                : "未能生成候选根因"}
            </p>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>证据 ({evidenceToShow.length})</CardTitle>
          <CardDescription>
            多源关联证据 — 点击查看详情
          </CardDescription>
        </CardHeader>
        <CardContent>
          {evidenceToShow.length > 0 ? (
            <div className="grid gap-3 md:grid-cols-2">
              {evidenceToShow.map((ev, i) => (
                <button
                  key={i}
                  onClick={() => setSelectedEvidence(ev)}
                  className="rounded-md border bg-accent/20 p-3 text-left transition-colors hover:border-primary/50 hover:bg-accent/40"
                >
                  <div className="text-sm">{ev.slice(0, 80)}{ev.length > 80 ? "..." : ""}</div>
                </button>
              ))}
            </div>
          ) : (
            <p className="text-sm text-muted-foreground">暂无证据</p>
          )}
        </CardContent>
      </Card>

      {selectedEvidence && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4"
          onClick={() => setSelectedEvidence(null)}
        >
          <Card
            className="w-full max-w-lg"
            onClick={(e: React.MouseEvent) => e.stopPropagation()}
          >
            <CardHeader>
              <CardTitle>证据详情</CardTitle>
            </CardHeader>
            <CardContent className="space-y-3 text-sm">
              <p className="whitespace-pre-wrap">{selectedEvidence}</p>
              <button
                onClick={() => setSelectedEvidence(null)}
                className="rounded-md border px-4 py-2 text-sm font-medium hover:bg-accent"
              >
                关闭
              </button>
            </CardContent>
          </Card>
        </div>
      )}
    </div>
  )
}
