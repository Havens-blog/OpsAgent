"use client"

import { Suspense, useState } from "react"
import { useRouter, useSearchParams } from "next/navigation"
import Link from "next/link"
import { ArrowLeft } from "lucide-react"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { api, ApiError } from "@/lib/api"

const inputCls =
  "w-full rounded-md border bg-background px-3 py-2 text-sm outline-none focus:border-primary focus:ring-1 focus:ring-primary"

const DIMENSIONS = [
  { key: "time_correlation", label: "时间相关性分析", weight: "50%", desc: "时间序列相关性" },
  { key: "topology_score", label: "拓扑关系分析", weight: "25%", desc: "服务依赖拓扑" },
  { key: "change_proximity", label: "变更检测分析", weight: "15%", desc: "K8s 变更关联" },
  { key: "metric_severity", label: "指标严重度分析", weight: "5%", desc: "指标异常评分" },
  { key: "error_frequency", label: "日志模式分析", weight: "3%", desc: "错误日志模式识别" },
  { key: "trace_evidence", label: "链路追踪分析", weight: "2%", desc: "Trace 异常检测" },
]

/**
 * useSearchParams needs a Suspense boundary so the shell can be rendered while
 * the query string (nullable during prerender) resolves; without it the page
 * would have to be fully dynamic.
 */
export default function NewRCAPage() {
  return (
    <Suspense fallback={<NewRCAFallback />}>
      <NewRCAForm />
    </Suspense>
  )
}

function NewRCAFallback() {
  return (
    <div className="space-y-8">
      <header>
        <h1 className="text-3xl font-bold tracking-tight">新建 RCA 分析</h1>
        <p className="text-muted-foreground">配置分析参数并启动根因分析任务</p>
      </header>
      <Card className="flex h-40 items-center justify-center text-sm text-muted-foreground">
        正在加载分析参数…
      </Card>
    </div>
  )
}

function NewRCAForm() {
  const router = useRouter()
  // Query prefill from the change timeline ("发起 RCA 分析"): the resource's
  // service (omitted when the record has none) and a reason/message summary.
  // Without query params this page behaves exactly as before.
  const searchParams = useSearchParams()
  const prefillService = (searchParams.get("service") ?? "").trim()
  const prefillSymptom = (searchParams.get("symptom") ?? "").trim()
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const [form, setForm] = useState(() => ({
    incident_id: "",
    severity: "P2",
    start_time: "",
    end_time: "",
    affected_services: prefillService,
    description: prefillSymptom,
  }))


  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setSubmitting(true)
    setError(null)
    try {
      const now = new Date()
      const start = form.start_time
        ? new Date(form.start_time).toISOString()
        : new Date(now.getTime() - 60 * 60 * 1000).toISOString()
      const end = form.end_time
        ? new Date(form.end_time).toISOString()
        : now.toISOString()

      await api.triggerRCA({
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
      router.push("/rca")
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
      <div className="flex items-center gap-3 text-sm text-muted-foreground">
        <Link href="/rca" className="flex items-center gap-1 hover:text-foreground">
          <ArrowLeft className="h-4 w-4" />
          RCA 分析
        </Link>
      </div>

      <header>
        <h1 className="text-3xl font-bold tracking-tight">新建 RCA 分析</h1>
        <p className="text-muted-foreground">
          配置分析参数并启动根因分析任务
        </p>
      </header>

      <form onSubmit={handleSubmit} className="space-y-6">
        <Card>
          <CardHeader>
            <CardTitle>基本信息</CardTitle>
            <CardDescription>标识本次分析的事件与服务范围</CardDescription>
          </CardHeader>
          <CardContent className="grid gap-4 md:grid-cols-2">
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
                onChange={(e) => setForm({ ...form, severity: e.target.value })}
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
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>评分维度</CardTitle>
            <CardDescription>
              RCA 引擎按以下固定权重做多维度加权评分（所有维度始终参与，不可单独关闭）
            </CardDescription>
          </CardHeader>
          <CardContent className="grid gap-3 md:grid-cols-2">
            {DIMENSIONS.map((d) => (
              <div
                key={d.key}
                className="flex items-start gap-3 rounded-md border bg-accent/20 p-3"
              >
                <div className="flex-1">
                  <div className="flex items-center justify-between">
                    <span className="text-sm font-medium">{d.label}</span>
                    <span className="font-mono text-xs text-muted-foreground">
                      {d.weight}
                    </span>
                  </div>
                  <div className="text-xs text-muted-foreground">{d.desc}</div>
                </div>
              </div>
            ))}
          </CardContent>
        </Card>

        <div className="flex items-center gap-4">
          <button
            type="submit"
            disabled={submitting}
            className="rounded-md bg-primary px-5 py-2 text-sm font-semibold text-primary-foreground transition-colors hover:bg-primary/90 disabled:opacity-50"
          >
            {submitting ? "提交中..." : "启动 RCA 分析"}
          </button>
          <Link
            href="/rca"
            className="rounded-md border px-5 py-2 text-sm font-medium hover:bg-accent"
          >
            取消
          </Link>
          {error && (
            <span className="text-sm text-destructive">{error}</span>
          )}
        </div>
      </form>
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
        {required && <span className="text-destructive"> *</span>}
      </span>
      {children}
    </label>
  )
}
