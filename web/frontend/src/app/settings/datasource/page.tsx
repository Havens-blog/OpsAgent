"use client"

import { useEffect, useState } from "react"
import { Database, AlertCircle, RefreshCw, CheckCircle2 } from "lucide-react"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { api, type DataSourceStatus } from "@/lib/api"

// Human-friendly labels keyed by backend dimension id.
const DIM_LABELS: Record<string, string> = {
  metrics: "指标",
  topology: "拓扑",
  changes: "变更",
  logs: "日志",
}

export default function DatasourceSettingsPage() {
  const [status, setStatus] = useState<DataSourceStatus | null>(null)
  const [err, setErr] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  const load = () => {
    setLoading(true)
    api
      .getDataSources()
      .then((s) => {
        setStatus(s)
        setErr(null)
      })
      .catch((e) => setErr(e instanceof Error ? e.message : String(e)))
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return (
    <div className="space-y-8">
      <header className="flex items-start justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">数据源</h1>
          <p className="text-muted-foreground">
            RCA 四个维度的数据来源 — 真实集成或 Mock 回退
          </p>
        </div>
        <button
          onClick={load}
          disabled={loading}
          className="inline-flex items-center gap-2 rounded-md border px-3 py-2 text-sm hover:bg-accent disabled:opacity-50"
        >
          <RefreshCw className={`h-4 w-4 ${loading ? "animate-spin" : ""}`} />
          刷新
        </button>
      </header>

      {err && (
        <div className="flex items-center gap-2 rounded-md border border-status-critical/40 bg-status-critical/10 p-3 text-sm text-status-critical">
          <AlertCircle className="h-4 w-4" />
          无法读取数据源状态：{err}
        </div>
      )}

      {status && (
        <>
          <div className="rounded-md border bg-accent/20 p-4 text-sm">
            {status.all_mock ? (
              <p className="text-muted-foreground">
                当前所有维度均使用 <span className="font-mono">Mock</span> 数据。
                系统开箱即用，按下方说明配置环境变量后，对应维度会自动切换为真实数据源（无需改代码）。
              </p>
            ) : (
              <p>
                <span className="font-semibold text-status-healthy">
                  {status.real_count}/{status.total}
                </span>{" "}
                个维度已接入真实数据源，其余回退 Mock。
              </p>
            )}
          </div>

          <div className="grid gap-4 md:grid-cols-2">
            {status.dimensions.map((d) => (
              <Card key={d.dimension}>
                <CardHeader>
                  <div className="flex items-center justify-between">
                    <CardTitle className="flex items-center gap-2 text-base">
                      <Database className="h-4 w-4" />
                      {DIM_LABELS[d.dimension] || d.dimension}
                    </CardTitle>
                    <Badge variant={d.real ? "success" : "secondary"}>
                      {d.real ? (
                        <span className="flex items-center gap-1">
                          <CheckCircle2 className="h-3 w-3" /> 真实
                        </span>
                      ) : (
                        "Mock"
                      )}
                    </Badge>
                  </div>
                  <CardDescription>{d.source}</CardDescription>
                </CardHeader>
                <CardContent>
                  <div className="text-xs text-muted-foreground">
                    激活所需环境变量
                  </div>
                  <div className="mt-2 flex flex-wrap gap-1.5">
                    {d.env.map((e) => (
                      <code
                        key={e}
                        className="rounded bg-muted px-1.5 py-0.5 font-mono text-[11px]"
                      >
                        {e}
                      </code>
                    ))}
                  </div>
                </CardContent>
              </Card>
            ))}
          </div>

          <Card>
            <CardHeader>
              <CardTitle className="text-base">如何接入真实数据源</CardTitle>
              <CardDescription>
                凭证通过后端环境变量注入，不在前端表单保存
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-2 text-sm text-muted-foreground">
              <p>
                1. 在后端 <code className="font-mono">.env</code> 中填入对应维度的环境变量（见上方卡片）。
              </p>
              <p>
                2. 重启后端服务，带相应维度配置的数据源会自动替换 Mock。
              </p>
              <p>
                3. 回到本页点击「刷新」，确认对应维度徽标变为
                <Badge variant="success" className="mx-1">
                  真实
                </Badge>
                。
              </p>
            </CardContent>
          </Card>
        </>
      )}
    </div>
  )
}
