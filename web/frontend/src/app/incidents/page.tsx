"use client"

import { useEffect, useState } from "react"
import Link from "next/link"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Badge, severityVariant } from "@/components/ui/badge"
import { api, ApiError, type Incident } from "@/lib/api"
import { formatTime } from "@/lib/utils"

export default function IncidentsPage() {
  const [submitting, setSubmitting] = useState(false)
  const [result, setResult] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const [incidents, setIncidents] = useState<Incident[]>([])
  const [loadingIncidents, setLoadingIncidents] = useState(true)
  const [listError, setListError] = useState<string | null>(null)

  async function loadIncidents() {
    setLoadingIncidents(true)
    setListError(null)
    try {
      const res = await api.getIncidents()
      setIncidents(res.incidents ?? [])
    } catch (err) {
      setListError(err instanceof Error ? err.message : "加载事件失败")
    } finally {
      setLoadingIncidents(false)
    }
  }

  useEffect(() => {
    void loadIncidents()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const [form, setForm] = useState({
    incident_id: "",
    severity: "P2",
    start_time: "",
    end_time: "",
    affected_services: "",
    description: "",
  })

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setSubmitting(true)
    setError(null)
    setResult(null)
    try {
      const now = new Date()
      const start = form.start_time
        ? new Date(form.start_time).toISOString()
        : new Date(now.getTime() - 60 * 60 * 1000).toISOString()
      const end = form.end_time
        ? new Date(form.end_time).toISOString()
        : now.toISOString()

      const res = await api.triggerRCA({
        incident_id: form.incident_id,
        severity: form.severity,
        start_time: start,
        end_time: end,
        affected_services: form.affected_services
          .split(",")
          .map((s) => s.trim())
          .filter(Boolean),
        description: form.description,
      })
      setResult(`已提交 RCA 分析 (incident: ${res.incident_id}, 状态: ${res.status})`)
      // Refresh the list so the newly submitted incident appears.
      void loadIncidents()
    } catch (err) {
      if (err instanceof ApiError) {
        setError(`API ${err.status}: ${err.message}`)
      } else {
        setError(err instanceof Error ? err.message : "提交失败")
      }
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="space-y-8">
      <header>
        <h1 className="text-3xl font-bold tracking-tight">Incidents</h1>
        <p className="text-muted-foreground">
          事件管理 — 触发根因分析并追踪处理进展
        </p>
      </header>

      <Card>
        <CardHeader>
          <CardTitle>触发 RCA 分析</CardTitle>
          <CardDescription>
            提交事件信息，OpsAgent 将多源关联日志、指标、Trace、变更与拓扑
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSubmit} className="grid gap-4 md:grid-cols-2">
            <Field label="Incident ID *" required>
              <input
                required
                value={form.incident_id}
                onChange={(e) =>
                  setForm({ ...form, incident_id: e.target.value })
                }
                placeholder="INC-20260619-001"
                className={inputCls}
              />
            </Field>
            <Field label="严重级别">
              <select
                value={form.severity}
                onChange={(e) =>
                  setForm({ ...form, severity: e.target.value })
                }
                className={inputCls}
              >
                {["P0", "P1", "P2", "P3", "P4"].map((s) => (
                  <option key={s} value={s}>
                    {s}
                  </option>
                ))}
              </select>
            </Field>
            <Field label="开始时间">
              <input
                type="datetime-local"
                value={form.start_time}
                onChange={(e) =>
                  setForm({ ...form, start_time: e.target.value })
                }
                className={inputCls}
              />
            </Field>
            <Field label="结束时间">
              <input
                type="datetime-local"
                value={form.end_time}
                onChange={(e) => setForm({ ...form, end_time: e.target.value })}
                className={inputCls}
              />
            </Field>
            <Field label="受影响服务 (逗号分隔)">
              <input
                value={form.affected_services}
                onChange={(e) =>
                  setForm({ ...form, affected_services: e.target.value })
                }
                placeholder="order-service, payment-service"
                className={inputCls}
              />
            </Field>
            <Field label="事件描述">
              <input
                value={form.description}
                onChange={(e) =>
                  setForm({ ...form, description: e.target.value })
                }
                placeholder="错误率激增，下游超时"
                className={inputCls}
              />
            </Field>
            <div className="md:col-span-2 flex items-center gap-4">
              <button
                type="submit"
                disabled={submitting}
                className="rounded-md bg-primary px-5 py-2 text-sm font-semibold text-primary-foreground transition-colors hover:bg-primary/90 disabled:opacity-50"
              >
                {submitting ? "提交中..." : "开始 RCA 分析"}
              </button>
              {result && (
                <span className="text-sm text-status-healthy">{result}</span>
              )}
              {error && (
                <span className="text-sm text-destructive">{error}</span>
              )}
            </div>
          </form>
        </CardContent>
      </Card>

      <div className="space-y-3">
        <h2 className="text-xl font-semibold">事件列表</h2>
        {loadingIncidents && (
          <p className="text-sm text-muted-foreground">加载中…</p>
        )}
        {listError && (
          <p className="text-sm text-destructive">加载事件失败：{listError}</p>
        )}
        {!loadingIncidents && !listError && incidents.length === 0 && (
          <div className="rounded-md border border-dashed bg-muted/30 p-6 text-center text-sm text-muted-foreground">
            暂无事件。触发一次 RCA 分析，或等待 watch-alert 告警接入后自动产生。
          </div>
        )}
        {incidents.map((inc) => (
          <Link key={inc.id} href={`/incidents/${inc.id}`}>
            <Card className="transition-colors hover:border-primary/50 hover:bg-accent/30">
              <CardContent className="flex items-center justify-between py-4">
                <div className="flex items-center gap-4">
                  <Badge variant={severityVariant(inc.severity)}>
                    {inc.severity}
                  </Badge>
                  <div>
                    <div className="font-semibold">{inc.title}</div>
                    <div className="text-xs text-muted-foreground">
                      {inc.id} · {formatTime(inc.start_time)}
                    </div>
                  </div>
                </div>
                <div className="flex items-center gap-4">
                  {(inc.affected_services || []).map((s) => (
                    <Badge key={s} variant="outline" className="font-mono">
                      {s}
                    </Badge>
                  ))}
                  <Badge
                    variant={
                      inc.status === "active" ? "danger" : "secondary"
                    }
                  >
                    {inc.status}
                  </Badge>
                </div>
              </CardContent>
            </Card>
          </Link>
        ))}
      </div>
    </div>
  )
}

function Field({
  label,
  required,
  children,
}: {
  label: string
  required?: boolean
  children: React.ReactNode
}) {
  return (
    <label className="space-y-1.5">
      <span className="text-xs font-medium text-muted-foreground">
        {label}
      </span>
      {children}
    </label>
  )
}

const inputCls =
  "w-full rounded-md border bg-background px-3 py-2 text-sm outline-none focus:border-primary focus:ring-1 focus:ring-primary"
