"use client"

import { useEffect, useState } from "react"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  api,
  type ProviderInfo,
  type LLMConfig,
  type LLMConfigInput,
} from "@/lib/api"

const inputCls =
  "w-full rounded-md border bg-background px-3 py-2 text-sm outline-none focus:border-primary focus:ring-1 focus:ring-primary"

export default function ModelSettingsPage() {
  const [providers, setProviders] = useState<ProviderInfo[]>([])
  const [provider, setProvider] = useState("")
  const [model, setModel] = useState("")
  const [baseURL, setBaseURL] = useState("")
  const [apiKey, setApiKey] = useState("")
  const [temperature, setTemperature] = useState("0.7")
  const [maxTokens, setMaxTokens] = useState("4096")
  const [timeout, setTimeoutVal] = useState("120")

  // Current persisted state (masked) loaded from backend.
  const [current, setCurrent] = useState<LLMConfig | null>(null)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState<string | null>(null)

  const [testing, setTesting] = useState(false)
  const [testResult, setTestResult] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [saveMsg, setSaveMsg] = useState<string | null>(null)

  // Live model catalog fetched from the provider.
  const [availableModels, setAvailableModels] = useState<string[]>([])
  const [modelsSource, setModelsSource] = useState<"provider" | "fallback" | null>(
    null
  )
  const [modelsLoading, setModelsLoading] = useState(false)
  const [modelsNote, setModelsNote] = useState<string | null>(null)

  // Fetch the model list for a provider. An empty key tells the backend to use
  // the stored key; the backend falls back to the curated list on any failure.
  async function fetchModels(prov: string, base: string, key: string) {
    if (!prov) return
    setModelsLoading(true)
    setModelsNote(null)
    try {
      const input: LLMConfigInput = { provider: prov, model: "" }
      if (base) input.base_url = base
      if (key.trim()) input.api_key = key.trim()
      const res = await api.listModels(input)
      setAvailableModels(res.models || [])
      setModelsSource(res.source)
      if (res.source === "fallback") {
        setModelsNote(res.error || "显示建议模型列表")
      }
    } catch (e) {
      setAvailableModels([])
      setModelsSource(null)
      setModelsNote(e instanceof Error ? e.message : "获取模型失败")
    } finally {
      setModelsLoading(false)
    }
  }

  // Load providers + current config on mount, then fetch the live model list.
  useEffect(() => {
    let cancelled = false
    async function load() {
      try {
        const [pRes, cfg] = await Promise.all([
          api.listProviders(),
          api.getLLMConfig(),
        ])
        if (cancelled) return
        setProviders(pRes.providers)
        setCurrent(cfg)
        // Pre-fill the form with the saved config.
        const prov = cfg.provider || pRes.providers[0]?.id || ""
        setProvider(prov)
        setModel(cfg.model || "")
        setBaseURL(cfg.base_url || "")
        setTemperature(String(cfg.temperature ?? 0.7))
        setMaxTokens(String(cfg.max_tokens || 4096))
        setTimeoutVal(String(cfg.timeout || 120))
        setLoadError(null)
        // Fetch live models using the stored key (empty key => backend uses stored).
        void fetchModels(prov, cfg.base_url || "", "")
      } catch (e) {
        if (cancelled) return
        setLoadError(e instanceof Error ? e.message : "加载失败")
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    load()
    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const currentProvider = providers.find((p) => p.id === provider)

  function handleProviderChange(value: string) {
    setProvider(value)
    const p = providers.find((pp) => pp.id === value)
    // Switching provider resets model + base_url to that provider's defaults.
    setModel(p?.default_model || "")
    setBaseURL("")
    setTestResult(null)
    setSaveMsg(null)
    // Refresh the model list for the newly selected provider.
    void fetchModels(value, "", apiKey)
  }

  function buildInput(): LLMConfigInput {
    const input: LLMConfigInput = {
      provider,
      model,
      base_url: baseURL || undefined,
      max_tokens: Number(maxTokens) || undefined,
      temperature: Number(temperature),
      timeout: Number(timeout) || undefined,
    }
    // Only send the key when the user typed a new one; blank keeps the stored key.
    if (apiKey.trim()) input.api_key = apiKey.trim()
    return input
  }

  async function handleTest() {
    setTesting(true)
    setTestResult(null)
    setSaveMsg(null)
    try {
      const res = await api.testLLMConfig(buildInput())
      if (res.success) {
        setTestResult(
          `✓ 连接正常 · ${res.model ?? ""}${
            res.latency_ms != null ? ` · ${res.latency_ms}ms` : ""
          }`
        )
      } else {
        setTestResult(`✗ ${res.error ?? "连接失败"}`)
      }
    } catch (e) {
      setTestResult(`✗ ${e instanceof Error ? e.message : "连接失败"}`)
    } finally {
      setTesting(false)
    }
  }

  async function handleSave() {
    setSaving(true)
    setSaveMsg(null)
    setTestResult(null)
    try {
      const res = await api.saveLLMConfig(buildInput())
      setCurrent(res.config)
      setApiKey("") // clear the plaintext input; masked value now reflects saved key
      if (res.persisted) {
        setSaveMsg("✓ 已保存并写入配置文件")
      } else {
        setSaveMsg(
          `⚠ 已在当前进程生效，但写入文件失败：${res.persist_error ?? "未知错误"}`
        )
      }
    } catch (e) {
      setSaveMsg(`✗ ${e instanceof Error ? e.message : "保存失败"}`)
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="space-y-8">
      <header>
        <h1 className="text-3xl font-bold tracking-tight">模型配置</h1>
        <p className="text-muted-foreground">
          配置 LLM 提供商、模型与推理参数
        </p>
      </header>

      {loadError && (
        <p className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          加载配置失败：{loadError}。请确认 API 服务已启动。
        </p>
      )}

      {/* Current saved config summary */}
      <Card>
        <CardHeader>
          <CardTitle>当前配置</CardTitle>
          <CardDescription>后端持久化的 LLM 配置状态</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-3 text-sm md:grid-cols-4">
          <SummaryItem label="提供商" value={current?.provider || "—"} />
          <SummaryItem label="模型" value={current?.model || "—"} />
          <SummaryItem
            label="API Key"
            value={
              current?.has_api_key
                ? current.api_key_masked || "已配置"
                : "未配置"
            }
            tone={current?.has_api_key ? "ok" : "warn"}
          />
          <SummaryItem
            label="助手可用"
            value={current?.available ? "是" : "否"}
            tone={current?.available ? "ok" : "warn"}
          />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>LLM 提供商配置</CardTitle>
          <CardDescription>
            选择模型提供商并配置认证信息（留空 API Key 则保留已保存的密钥）
          </CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4 md:grid-cols-2">
          <div className="space-y-1.5">
            <label className="text-xs font-medium text-muted-foreground">
              LLM 提供商
            </label>
            <select
              value={provider}
              onChange={(e) => handleProviderChange(e.target.value)}
              className={inputCls}
              disabled={loading}
            >
              {providers.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </select>
          </div>

          <div className="space-y-1.5">
            <div className="flex items-center justify-between">
              <label className="text-xs font-medium text-muted-foreground">
                模型
              </label>
              <button
                type="button"
                onClick={() => void fetchModels(provider, baseURL, apiKey)}
                disabled={modelsLoading || loading}
                className="text-xs text-primary hover:underline disabled:opacity-50"
              >
                {modelsLoading ? "刷新中..." : "↻ 刷新列表"}
              </button>
            </div>
            {/* Real dropdown populated from the live/provider model list. */}
            <select
              value={availableModels.includes(model) ? model : "__custom__"}
              onChange={(e) => {
                if (e.target.value !== "__custom__") setModel(e.target.value)
              }}
              className={inputCls}
              disabled={loading || modelsLoading}
            >
              {availableModels.map((m) => (
                <option key={m} value={m}>
                  {m}
                </option>
              ))}
              <option value="__custom__">自定义（在下方输入）…</option>
            </select>
            {/* Free-text input as the ultimate fallback / override. */}
            <input
              value={model}
              onChange={(e) => setModel(e.target.value)}
              list="model-suggestions"
              placeholder="或手动输入模型 ID"
              className={inputCls}
              disabled={loading}
            />
            <datalist id="model-suggestions">
              {availableModels.map((m) => (
                <option key={m} value={m} />
              ))}
            </datalist>
            <p className="text-xs text-muted-foreground">
              {modelsLoading
                ? "正在从提供商获取模型列表…"
                : modelsSource === "provider"
                  ? `共 ${availableModels.length} 个模型（来自提供商实时列表）`
                  : modelsNote
                    ? `建议列表：${modelsNote}`
                    : `共 ${availableModels.length} 个建议模型`}
            </p>
          </div>

          <div className="space-y-1.5 md:col-span-2">
            <label className="text-xs font-medium text-muted-foreground">
              API 端点 Base URL（留空使用提供商默认）
            </label>
            <input
              value={baseURL}
              onChange={(e) => setBaseURL(e.target.value)}
              placeholder={currentProvider?.base_url || "https://..."}
              className={inputCls}
              disabled={loading}
            />
          </div>

          <div className="space-y-1.5 md:col-span-2">
            <label className="text-xs font-medium text-muted-foreground">
              API Key
            </label>
            <input
              type="password"
              value={apiKey}
              onChange={(e) => setApiKey(e.target.value)}
              placeholder={
                current?.has_api_key
                  ? `已配置 (${current.api_key_masked}) — 留空则保留`
                  : "输入 API Key"
              }
              className={inputCls}
              disabled={loading}
            />
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>推理参数</CardTitle>
          <CardDescription>调整模型生成的随机性与输出长度</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4 md:grid-cols-3">
          <div className="space-y-1.5">
            <label className="text-xs font-medium text-muted-foreground">
              Temperature (0-2)
            </label>
            <input
              type="number"
              min="0"
              max="2"
              step="0.1"
              value={temperature}
              onChange={(e) => setTemperature(e.target.value)}
              className={inputCls}
            />
          </div>

          <div className="space-y-1.5">
            <label className="text-xs font-medium text-muted-foreground">
              Max Tokens
            </label>
            <input
              type="number"
              min="1"
              value={maxTokens}
              onChange={(e) => setMaxTokens(e.target.value)}
              className={inputCls}
            />
          </div>

          <div className="space-y-1.5">
            <label className="text-xs font-medium text-muted-foreground">
              超时 (秒)
            </label>
            <input
              type="number"
              min="1"
              value={timeout}
              onChange={(e) => setTimeoutVal(e.target.value)}
              className={inputCls}
            />
          </div>
        </CardContent>
      </Card>

      <div className="flex flex-wrap items-center gap-4">
        <button
          onClick={handleTest}
          disabled={testing || loading}
          className="rounded-md border px-5 py-2 text-sm font-medium hover:bg-accent disabled:opacity-50"
        >
          {testing ? "测试中..." : "测试连接"}
        </button>
        <button
          onClick={handleSave}
          disabled={saving || loading}
          className="rounded-md bg-primary px-5 py-2 text-sm font-semibold text-primary-foreground transition-colors hover:bg-primary/90 disabled:opacity-50"
        >
          {saving ? "保存中..." : "保存配置"}
        </button>

        {testResult && (
          <span
            className={
              "text-sm " +
              (testResult.startsWith("✓")
                ? "text-status-healthy"
                : "text-destructive")
            }
          >
            {testResult}
          </span>
        )}
        {saveMsg && (
          <span
            className={
              "text-sm " +
              (saveMsg.startsWith("✓")
                ? "text-status-healthy"
                : saveMsg.startsWith("⚠")
                  ? "text-amber-500"
                  : "text-destructive")
            }
          >
            {saveMsg}
          </span>
        )}
      </div>
    </div>
  )
}

function SummaryItem({
  label,
  value,
  tone = "neutral",
}: {
  label: string
  value: string
  tone?: "neutral" | "ok" | "warn"
}) {
  const toneCls =
    tone === "ok"
      ? "text-status-healthy"
      : tone === "warn"
        ? "text-amber-500"
        : "text-foreground"
  return (
    <div>
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className={"mt-0.5 font-medium " + toneCls}>{value}</div>
    </div>
  )
}
