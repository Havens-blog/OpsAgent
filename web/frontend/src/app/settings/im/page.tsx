"use client"

// IM 通知配置 — aligned to haven-console style. Loads persisted config from
// the backend, saves via PUT /api/v1/config/notifications, and delivers a
// real test message via POST /api/v1/config/notifications/test.

import { useEffect, useState } from "react"
import { Check } from "lucide-react"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  CardFooter,
} from "@/components/ui/card"
import { Badge, severityVariant } from "@/components/ui/badge"
import { api, type NotificationConfig } from "@/lib/api"

const inputCls =
  "w-full rounded-md border bg-background px-3 py-2 text-sm outline-none focus:border-primary focus:ring-1 focus:ring-primary"

const platforms = [
  { value: "dingtalk", label: "钉钉" },
  { value: "wecom", label: "企业微信" },
  { value: "feishu", label: "飞书" },
]

const severities: Array<"P0" | "P1" | "P2" | "P3" | "P4"> = ["P0", "P1", "P2", "P3", "P4"]

function PlatformHeader({ platform }: { platform: string }) {
  const colors: Record<string, string> = {
    dingtalk: "bg-border",
    wecom: "bg-border",
    feishu: "bg-border",
  }
  return <div className={`h-1 w-full rounded-t-lg ${colors[platform] || "bg-border"}`} />
}

type Severity = "P0" | "P1" | "P2" | "P3" | "P4"

export default function IMSettingsPage() {
  const [platform, setPlatform] = useState("dingtalk")
  const [webhookUrl, setWebhookUrl] = useState("")
  const [secret, setSecret] = useState("")
  const [hasSecret, setHasSecret] = useState(false)
  const [selectedSeverities, setSelectedSeverities] = useState<Severity[]>(["P0", "P1"])
  const [startTime, setStartTime] = useState("00:00")
  const [endTime, setEndTime] = useState("23:59")

  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [saveMsg, setSaveMsg] = useState<string | null>(null)
  const [testing, setTesting] = useState(false)
  const [testMsg, setTestMsg] = useState<string | null>(null)

  async function load() {
    setLoading(true)
    setLoadError(null)
    try {
      const cfg: NotificationConfig = await api.getNotificationConfig()
      setPlatform(cfg.platform || "dingtalk")
      setWebhookUrl(cfg.webhook_url || "")
      setHasSecret(cfg.has_secret)
      setSecret("")
      setSelectedSeverities((cfg.severities?.length ? cfg.severities : ["P0", "P1"]) as Severity[])
      setStartTime(cfg.start_time || "00:00")
      setEndTime(cfg.end_time || "23:59")
    } catch (e) {
      setLoadError(e instanceof Error ? e.message : "加载失败")
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void load()
  }, [])

  function toggleSeverity(sev: Severity) {
    setSelectedSeverities((prev) =>
      prev.includes(sev) ? prev.filter((s) => s !== sev) : [...prev, sev],
    )
  }

  async function handleSave() {
    setSaving(true)
    setSaveMsg(null)
    try {
      const res = await api.saveNotificationConfig({
        platform,
        webhook_url: webhookUrl,
        secret: secret || undefined,
        severities: selectedSeverities,
        start_time: startTime,
        end_time: endTime,
      })
      if (res.persisted) {
        setSaveMsg("✓ 配置已保存")
      } else {
        setSaveMsg(`⚠ 内存已更新但未落盘：${res.error || "未知错误"}`)
      }
      setHasSecret(!!res.config?.has_secret)
      setSecret("")
    } catch (e) {
      setSaveMsg(`✗ 保存失败：${e instanceof Error ? e.message : String(e)}`)
    } finally {
      setSaving(false)
      setTimeout(() => setSaveMsg(null), 4000)
    }
  }

  async function handleTest() {
    setTesting(true)
    setTestMsg(null)
    try {
      const res = await api.testNotificationConfig({
        platform,
        webhook_url: webhookUrl,
        secret: secret || undefined,
      })
      if (res.success) {
        setTestMsg(`✓ 测试消息已送达（${res.platform}，${res.latency_ms ?? "?"}ms）`)
      } else {
        setTestMsg(
          `✗ 平台返回失败：${res.response || res.error || "未知错误"}（HTTP ${res.http_status ?? "?"}）`,
        )
      }
    } catch (e) {
      setTestMsg(`✗ 下发失败：${e instanceof Error ? e.message : String(e)}`)
    } finally {
      setTesting(false)
      setTimeout(() => setTestMsg(null), 6000)
    }
  }

  return (
    <div className="space-y-8">
      <header>
        <h1 className="text-3xl font-bold tracking-tight">IM 通知</h1>
        <p className="text-muted-foreground">
          配置钉钉、企业微信或飞书告警通知
        </p>
      </header>

      {loadError && (
        <div className="rounded-md border border-destructive/40 bg-destructive/10 p-3 text-sm text-destructive">
          加载配置失败：{loadError}
        </div>
      )}

      <Card>
        <CardHeader>
          <CardTitle>通知平台配置</CardTitle>
          <CardDescription>选择平台并配置 Webhook URL</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4 md:grid-cols-2">
          <div className="space-y-1.5">
            <label className="text-xs font-medium text-muted-foreground">平台</label>
            <select
              value={platform}
              onChange={(e) => setPlatform(e.target.value)}
              className={inputCls}
            >
              {platforms.map((p) => (
                <option key={p.value} value={p.value}>
                  {p.label}
                </option>
              ))}
            </select>
          </div>
          <div className="space-y-1.5">
            <label className="text-xs font-medium text-muted-foreground">
              Webhook URL
            </label>
            <input
              value={webhookUrl}
              onChange={(e) => setWebhookUrl(e.target.value)}
              placeholder="https://oapi.dingtalk.com/robot/send?access_token=..."
              className={inputCls}
            />
          </div>
          <div className="space-y-1.5 md:col-span-2">
            <label className="text-xs font-medium text-muted-foreground">
              签名密钥 Secret {hasSecret && <span className="text-status-healthy">（已配置 {`***`}，留空保持不变）</span>}
            </label>
            <input
              type="password"
              value={secret}
              onChange={(e) => setSecret(e.target.value)}
              placeholder={hasSecret ? "留空保持现有密钥不变" : "可选，用于签名校验"}
              className={inputCls}
            />
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>通知规则</CardTitle>
          <CardDescription>配置告警级别过滤与通知时段</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="space-y-1.5">
            <label className="text-xs font-medium text-muted-foreground">
              通知严重级别（多选）
            </label>
            <div className="flex flex-wrap gap-2">
              {severities.map((sev) => {
                const selected = selectedSeverities.includes(sev)
                return (
                  <button
                    key={sev}
                    type="button"
                    onClick={() => toggleSeverity(sev)}
                    className={`rounded-full border px-3 py-1 text-xs font-semibold transition-colors ${
                      selected
                        ? "bg-primary text-primary-foreground"
                        : "text-muted-foreground hover:text-foreground"
                    }`}
                  >
                    {sev}
                  </button>
                )
              })}
            </div>
          </div>

          <div className="grid gap-4 md:grid-cols-2">
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-muted-foreground">
                通知开始时间
              </label>
              <input
                type="time"
                value={startTime}
                onChange={(e) => setStartTime(e.target.value)}
                className={inputCls}
              />
            </div>
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-muted-foreground">
                通知结束时间
              </label>
              <input
                type="time"
                value={endTime}
                onChange={(e) => setEndTime(e.target.value)}
                className={inputCls}
              />
            </div>
          </div>
        </CardContent>
        <CardFooter className="flex items-center gap-3">
          <button
            onClick={handleSave}
            disabled={saving || loading}
            className="inline-flex items-center gap-2 rounded-md bg-primary px-5 py-2 text-sm font-semibold text-primary-foreground transition-colors hover:bg-primary/90 disabled:opacity-50"
          >
            {saving ? "保存中…" : (<><Check className="h-4 w-4" /> 保存配置</>)}
          </button>
          {saveMsg && (
            <span
              className={
                "text-sm " +
                (saveMsg.startsWith("✓")
                  ? "text-status-healthy"
                  : saveMsg.startsWith("⚠")
                    ? "text-warning"
                    : "text-destructive")
              }
            >
              {saveMsg}
            </span>
          )}
        </CardFooter>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>消息模板预览</CardTitle>
          <CardDescription>告警消息在实际平台中的渲染效果</CardDescription>
        </CardHeader>
        <CardContent className="p-0">
          <div className="mx-6 overflow-hidden rounded-lg border">
            <PlatformHeader platform={platform} />
            <div className="space-y-3 p-4">
              <div className="flex items-center gap-2">
                <Badge variant={severityVariant("P1")}>P1</Badge>
                <span className="font-semibold">告警：订单服务错误率异常</span>
              </div>
              <div className="space-y-1 text-sm text-muted-foreground">
                <p>告警时间：2026-10-15 17:00:00</p>
                <p>影响服务：order-service, payment-service</p>
              </div>
              <div className="rounded-md bg-muted p-3 text-xs">
                <p className="font-medium">指标摘要</p>
                <p>错误率从 0.1% 上升至 3.2%（阈值 1%）</p>
                <p>持续时长：15 分钟</p>
              </div>
              <div className="rounded-md bg-muted p-3 text-xs">
                <p className="font-medium">根因分析</p>
                <p>检测到数据库连接池耗尽，下游服务响应超时。</p>
              </div>
            </div>
          </div>
        </CardContent>
        <CardFooter>
          <button
            onClick={handleTest}
            disabled={testing || !webhookUrl}
            className="rounded-md border px-5 py-2 text-sm font-medium hover:bg-accent disabled:opacity-50"
          >
            {testing ? "发送中…" : "发送测试消息"}
          </button>
          {testMsg && (
            <span
              className={
                "text-sm " +
                (testMsg.startsWith("✓")
                  ? "text-status-healthy"
                  : "text-destructive")
              }
            >
              {testMsg}
            </span>
          )}
        </CardFooter>
      </Card>
    </div>
  )
}
