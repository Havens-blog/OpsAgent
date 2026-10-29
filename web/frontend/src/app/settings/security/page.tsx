"use client"

// 安全审计配置 — loads persisted config from the backend, saves via
// PUT /api/v1/config/security, and renders real config-change audit records
// returned by the backend (recorded when audit logging is enabled).

import { useEffect, useState } from "react"
import { Check } from "lucide-react"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { api, type AuditLogEntry } from "@/lib/api"

const inputCls =
  "w-full rounded-md border bg-background px-3 py-2 text-sm outline-none focus:border-primary focus:ring-1 focus:ring-primary"

export default function SecuritySettingsPage() {
  const [auditEnabled, setAuditEnabled] = useState(true)
  const [auditLogPath, setAuditLogPath] = useState("")
  const [retentionDays, setRetentionDays] = useState("30")
  const [ipWhitelist, setIpWhitelist] = useState("192.168.0.0/16\n10.0.0.0/8")
  const [rateLimit, setRateLimit] = useState("100")

  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [saveMsg, setSaveMsg] = useState<string | null>(null)
  const [auditLogs, setAuditLogs] = useState<AuditLogEntry[]>([])

  async function load() {
    setLoading(true)
    setLoadError(null)
    try {
      const cfg = await api.getSecurityConfig()
      setAuditEnabled(cfg.enable_audit_log)
      setAuditLogPath(cfg.audit_log_path || "")
      setRetentionDays(String(cfg.audit_retention_days || 30))
      setIpWhitelist(cfg.ip_whitelist || "192.168.0.0/16\n10.0.0.0/8")
      setRateLimit(String(cfg.rate_limit || 100))
      setAuditLogs(cfg.audit_logs || [])
    } catch (e) {
      setLoadError(e instanceof Error ? e.message : "加载失败")
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void load()
  }, [])

  async function handleSave() {
    setSaving(true)
    setSaveMsg(null)
    try {
      const res = await api.saveSecurityConfig({
        enable_audit_log: auditEnabled,
        audit_log_path: auditLogPath,
        audit_retention_days: parseInt(retentionDays, 10) || 30,
        ip_whitelist: ipWhitelist,
        rate_limit: parseInt(rateLimit, 10) || 100,
      })
      if (res.persisted) {
        setSaveMsg("✓ 配置已保存")
      } else {
        setSaveMsg(`⚠ 内存已更新但未落盘：${res.error || "未知错误"}`)
      }
      if (res.config?.audit_logs) {
        setAuditLogs(res.config.audit_logs)
      }
    } catch (e) {
      setSaveMsg(`✗ 保存失败：${e instanceof Error ? e.message : String(e)}`)
    } finally {
      setSaving(false)
      setTimeout(() => setSaveMsg(null), 4000)
    }
  }

  return (
    <div className="space-y-8">
      <header>
        <h1 className="text-3xl font-bold tracking-tight">安全审计</h1>
        <p className="text-muted-foreground">
          配置审计日志、数据保留与访问控制
        </p>
      </header>

      {loadError && (
        <div className="rounded-md border border-destructive/40 bg-destructive/10 p-3 text-sm text-destructive">
          加载配置失败：{loadError}
        </div>
      )}

      <Card>
        <CardHeader>
          <CardTitle>审计日志配置</CardTitle>
          <CardDescription>启用审计日志并设置数据保留策略</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex items-center gap-3">
            <button
              type="button"
              onClick={() => setAuditEnabled(!auditEnabled)}
              className={`h-6 w-11 rounded-full transition-colors ${
                auditEnabled ? "bg-primary" : "bg-muted"
              }`}
              aria-pressed={auditEnabled}
            >
              <span
                className={`block h-5 w-5 rounded-full bg-white shadow transition-transform ${
                  auditEnabled ? "translate-x-5" : "translate-x-0.5"
                }`}
              />
            </button>
            <span className="text-sm font-medium">
              {auditEnabled ? "审计日志记录已启用" : "审计日志记录已禁用"}
            </span>
          </div>

          <div className="grid gap-4 md:grid-cols-2">
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-muted-foreground">
                审计日志路径
              </label>
              <input
                value={auditLogPath}
                onChange={(e) => setAuditLogPath(e.target.value)}
                placeholder="/var/log/opsagent/audit.log"
                className={`${inputCls} font-mono text-xs`}
              />
            </div>
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-muted-foreground">
                数据保留天数
              </label>
              <div className="flex items-center gap-2">
                <input
                  type="number"
                  min="1"
                  value={retentionDays}
                  onChange={(e) => setRetentionDays(e.target.value)}
                  className={inputCls}
                />
                <span className="text-sm text-muted-foreground">天</span>
              </div>
            </div>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>IP 白名单</CardTitle>
          <CardDescription>限制仅允许特定 CIDR 网段访问（每行一个）</CardDescription>
        </CardHeader>
        <CardContent>
          <textarea
            value={ipWhitelist}
            onChange={(e) => setIpWhitelist(e.target.value)}
            placeholder="192.168.0.0/16&#10;10.0.0.0/8"
            rows={4}
            className={`${inputCls} font-mono text-xs resize-none`}
          />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>API 速率限制</CardTitle>
          <CardDescription>限制每个用户每秒的最大请求数</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="flex items-center gap-2 md:w-1/3">
            <input
              type="number"
              min="1"
              value={rateLimit}
              onChange={(e) => setRateLimit(e.target.value)}
              className={inputCls}
            />
            <span className="text-sm text-muted-foreground">req/s</span>
          </div>
        </CardContent>
        <div className="px-6 pb-6">
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
                "ml-3 text-sm " +
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
        </div>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>操作审计</CardTitle>
          <CardDescription>最近系统操作记录</CardDescription>
        </CardHeader>
        <CardContent>
          {auditLogs.length === 0 ? (
            <div className="rounded-md border border-dashed bg-muted/30 p-6 text-center text-sm text-muted-foreground">
              暂无审计记录。启用审计日志后，配置变更会在此列出。
            </div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b">
                    <th className="py-2 text-left font-medium text-muted-foreground">时间</th>
                    <th className="py-2 text-left font-medium text-muted-foreground">用户</th>
                    <th className="py-2 text-left font-medium text-muted-foreground">操作</th>
                    <th className="py-2 text-left font-medium text-muted-foreground">IP</th>
                    <th className="py-2 text-left font-medium text-muted-foreground">结果</th>
                  </tr>
                </thead>
                <tbody>
                  {auditLogs.map((log, idx) => (
                    <tr key={idx} className="border-b last:border-0">
                      <td className="py-2 font-mono text-xs text-muted-foreground">
                        {new Date(log.time).toLocaleString("zh-CN", { hour12: false })}
                      </td>
                      <td className="py-2">{log.user}</td>
                      <td className="py-2">{log.operation}</td>
                      <td className="py-2 font-mono text-xs text-muted-foreground">{log.ip}</td>
                      <td className="py-2">
                        <Badge variant={log.result === "success" ? "success" : "danger"}>
                          {log.result === "success" ? "成功" : "失败"}
                        </Badge>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
