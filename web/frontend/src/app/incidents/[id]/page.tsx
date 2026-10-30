"use client"

import { use, useEffect, useState } from "react"
import Link from "next/link"
import { AlertTriangle } from "lucide-react"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Badge, severityVariant } from "@/components/ui/badge"
import { confidencePct, formatTime } from "@/lib/utils"
import { api, type Evidence, type IncidentDetail } from "@/lib/api"

const EVIDENCE_STYLE: Record<string, string> = {
  metric: "success",
  log: "warning",
  trace: "default",
  change: "danger",
  topology: "secondary",
  resource: "danger",
  waf: "danger",
}

export default function IncidentDetailPage({
  params,
}: {
  params: Promise<{ id: string }>
}) {
  const { id } = use(params)
  const [incident, setIncident] = useState<IncidentDetail | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [selectedEvidence, setSelectedEvidence] = useState<Evidence | null>(null)

  useEffect(() => {
    let cancelled = false
    async function load() {
      setLoading(true)
      setError(null)
      try {
        const detail = await api.getIncident(id)
        if (!cancelled) setIncident(detail)
      } catch (err) {
        if (!cancelled)
          setError(err instanceof Error ? err.message : "加载事件失败")
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    void load()
    return () => {
      cancelled = true
    }
  }, [id])

  if (loading) {
    return (
      <div className="space-y-8">
        <Link href="/incidents" className="inline-flex items-center gap-3 text-sm text-muted-foreground hover:text-foreground">
          ← Incidents
        </Link>
        <p className="text-sm text-muted-foreground">加载中…</p>
      </div>
    )
  }

  if (error || !incident) {
    return (
      <div className="space-y-8">
        <Link href="/incidents" className="inline-flex items-center gap-3 text-sm text-muted-foreground hover:text-foreground">
          ← Incidents
        </Link>
        <div className="rounded-md border border-destructive/40 bg-destructive/10 p-4 text-sm text-destructive">
          加载事件失败：{error || "未找到该事件"}
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-8">
      <div className="flex items-center gap-3 text-sm text-muted-foreground">
        <Link href="/incidents" className="hover:text-foreground">
          ← Incidents
        </Link>
      </div>

      <header className="flex items-start justify-between">
        <div className="space-y-2">
          <div className="flex items-center gap-3">
            <Badge variant={severityVariant(incident.severity)}>
              {incident.severity}
            </Badge>
            <Badge variant={incident.status === "active" ? "danger" : "secondary"}>
              {incident.status}
            </Badge>
            <span className="font-mono text-xs text-muted-foreground">
              {incident.id}
            </span>
          </div>
          <h1 className="text-2xl font-bold tracking-tight">
            {incident.title}
          </h1>
          <p className="text-muted-foreground">{incident.description}</p>
          <div className="flex gap-4 text-xs text-muted-foreground">
            <span>开始: {formatTime(incident.start_time)}</span>
            {incident.end_time && (
              <span>结束: {formatTime(incident.end_time)}</span>
            )}
          </div>
        </div>
      </header>

      {(incident.warnings?.length ?? 0) > 0 && (
        <div
          role="alert"
          className="rounded-md border border-warning-border bg-warning-soft px-4 py-3 text-sm"
        >
          <div className="flex items-center gap-2 font-medium text-warning">
            <AlertTriangle className="h-4 w-4" />
            数据源告警：以下数据源采集失败，分析结论可能不完整
          </div>
          <ul className="mt-2 list-inside list-disc space-y-1 text-muted-foreground">
            {incident.warnings!.map((w) => (
              <li key={w}>{w}</li>
            ))}
          </ul>
        </div>
      )}

      <div className="grid gap-6 lg:grid-cols-3">
        <Card className="lg:col-span-2 border-primary/40">
          <CardHeader>
            <CardTitle>根因分析结论</CardTitle>
            <CardDescription>由 Coordinator Agent 汇总多智能体证据</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="rounded-md bg-accent/40 p-4">
              <div className="text-sm text-muted-foreground">推断根因</div>
              <div className="mt-1 text-base font-medium">
                {incident.root_cause}
              </div>
            </div>
            <div className="flex items-center gap-4">
              <div className="flex-1">
                <div className="mb-1 flex justify-between text-xs">
                  <span className="text-muted-foreground">置信度</span>
                  <span className="font-mono">
                    {confidencePct(incident.confidence)}
                  </span>
                </div>
                <div className="h-2 overflow-hidden rounded-full bg-muted">
                  <div
                    className="h-full rounded-full bg-primary"
                    style={{ width: `${(incident.confidence || 0) * 100}%` }}
                  />
                </div>
              </div>
            </div>
            <div className="flex flex-wrap gap-2">
              {(incident.affected_services || []).map((s) => (
                <Badge key={s} variant="outline" className="font-mono">
                  {s}
                </Badge>
              ))}
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>事件时间线</CardTitle>
            <CardDescription>关键事件序列</CardDescription>
          </CardHeader>
          <CardContent>
            <ol className="space-y-3">
              {incident.timeline.map((t, i) => (
                <li key={i} className="flex gap-3">
                  <span className="font-mono text-xs text-primary">
                    {t.time}
                  </span>
                  <span className="text-sm">{t.event}</span>
                </li>
              ))}
            </ol>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>证据 ({incident.evidence.length})</CardTitle>
          <CardDescription>
            多源关联证据 — 点击查看详情
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className="grid gap-3 md:grid-cols-2">
            {incident.evidence.map((ev, i) => (
              <button
                key={i}
                onClick={() => setSelectedEvidence(ev)}
                className="rounded-md border bg-accent/20 p-3 text-left transition-colors hover:border-primary/50 hover:bg-accent/40"
              >
                <div className="flex items-center justify-between">
                  <Badge
                    variant={
                      (EVIDENCE_STYLE[ev.type] as
                        | "success"
                        | "warning"
                        | "default"
                        | "danger"
                        | "secondary") ?? "secondary"
                    }
                  >
                    {ev.type}
                  </Badge>
                  <span className="font-mono text-xs text-muted-foreground">
                    {ev.source}
                  </span>
                </div>
                <div className="mt-2 text-sm">{ev.description}</div>
              </button>
            ))}
          </div>
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
              <CardTitle className="capitalize">
                {selectedEvidence.type} 证据
              </CardTitle>
              <CardDescription>{selectedEvidence.source}</CardDescription>
            </CardHeader>
            <CardContent className="space-y-3 text-sm">
              <p>{selectedEvidence.description}</p>
              <div className="text-xs text-muted-foreground">
                窗口: {formatTime(selectedEvidence.start_time)} →{" "}
                {formatTime(selectedEvidence.end_time)}
              </div>
              <button
                onClick={() => setSelectedEvidence(null)}
                className="rounded-md bg-secondary px-4 py-2 text-sm font-medium"
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
