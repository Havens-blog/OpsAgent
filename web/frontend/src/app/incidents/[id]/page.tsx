"use client"

import { use, useState } from "react"
import Link from "next/link"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Badge, severityVariant } from "@/components/ui/badge"
import { confidencePct, formatTime } from "@/lib/utils"
import type { Evidence } from "@/lib/api"

interface IncidentDetail {
  id: string
  severity: string
  title: string
  description: string
  start_time: string
  end_time?: string
  status: string
  affected_services: string[]
  root_cause: string
  confidence: number
  evidence: Evidence[]
  timeline: { time: string; event: string }[]
}

// Demo detail data. In production this would be fetched from
// GET /api/v1/incidents/:id once the backend exposes it.
const DEMO: Record<string, IncidentDetail> = {
  "INC-20260619-order": {
    id: "INC-20260619-order",
    severity: "P1",
    title: "order-service 错误率激增至 2.5%",
    description: "上游 api-gateway 报告订单接口 5xx 增加",
    start_time: new Date(Date.now() - 8 * 60 * 1000).toISOString(),
    status: "active",
    affected_services: ["order-service", "postgres-order"],
    root_cause: "postgres-order 连接池耗尽导致 order-service 请求超时级联失败",
    confidence: 0.82,
    evidence: [
      {
        type: "metric",
        source: "order-service",
        start_time: new Date(Date.now() - 8 * 60 * 1000).toISOString(),
        end_time: new Date().toISOString(),
        description: "错误率从 0.1% 升至 2.5%，延迟 P99 从 120ms 升至 480ms",
      },
      {
        type: "log",
        source: "order-service",
        start_time: new Date(Date.now() - 7 * 60 * 1000).toISOString(),
        end_time: new Date().toISOString(),
        description: "出现大量 connection pool exhausted 错误日志",
      },
      {
        type: "trace",
        source: "postgres-order",
        start_time: new Date(Date.now() - 7 * 60 * 1000).toISOString(),
        end_time: new Date().toISOString(),
        description: "数据库查询延迟升高，活跃连接数达上限",
      },
      {
        type: "change",
        source: "kubernetes",
        start_time: new Date(Date.now() - 12 * 60 * 1000).toISOString(),
        end_time: new Date(Date.now() - 11 * 60 * 1000).toISOString(),
        description: "deployment order-service 副本数从 5 扩至 8（12 分钟前）",
      },
      {
        type: "topology",
        source: "arms",
        start_time: new Date(Date.now() - 8 * 60 * 1000).toISOString(),
        end_time: new Date().toISOString(),
        description: "order-service → postgres-order 边错误率 1.5%，健康降级",
      },
    ],
    timeline: [
      { time: "-12m", event: "order-service 扩容（变更）" },
      { time: "-8m", event: "告警触发：错误率 > 1%" },
      { time: "-7m", event: "RCA 引擎启动证据采集" },
      { time: "-5m", event: "LogAnalyst 识别连接池错误模式" },
      { time: "-3m", event: "Inspector 关联扩容变更" },
      { time: "-1m", event: "Coordinator 汇总根因假设" },
    ],
  },
}

const EVIDENCE_STYLE: Record<string, string> = {
  metric: "success",
  log: "warning",
  trace: "default",
  change: "danger",
  topology: "secondary",
}

export default function IncidentDetailPage({
  params,
}: {
  params: Promise<{ id: string }>
}) {
  const { id } = use(params)
  const [selectedEvidence, setSelectedEvidence] = useState<Evidence | null>(null)
  const incident = DEMO[id] ?? DEMO["INC-20260619-order"]

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
