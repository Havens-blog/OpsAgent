"use client"

// Tenant selector — parity with watch-alert src/components/sider/index.jsx top
// area: Team icon (accent-blue) + tenant name (13px, ellipsis) + chevron, on a
// glass token background with border that strengthens on hover. Profile data is
// OWNED BY THE SIDEBAR (single fetch) and passed down; this component only owns
// open/switching/error state.
//
// Switch contract (haven-console Hard Rule): POST /api/iam/tenant/switch with
// body { tenant_id } then a FULL-PAGE reload (location.replace) — EIAM destroys
// and re-signs the session, so an SPA store swap would leave stale state.

import { useEffect, useRef, useState } from "react"
import { Check, ChevronsUpDown, Users } from "lucide-react"
import { switchEiamTenant, type EiamTenant } from "@/lib/eiam"

interface TenantSelectorProps {
  tenants: EiamTenant[]
  currentTenantId: number
}

export function TenantSelector({
  tenants,
  currentTenantId,
}: TenantSelectorProps) {
  const [switching, setSwitching] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [open, setOpen] = useState(false)
  const rootRef = useRef<HTMLDivElement>(null)

  // Close on outside click.
  useEffect(() => {
    if (!open) return
    function onDown(e: MouseEvent) {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    document.addEventListener("mousedown", onDown)
    return () => document.removeEventListener("mousedown", onDown)
  }, [open])

  const currentName =
    tenants.find((t) => t.id === currentTenantId)?.name ||
    (currentTenantId ? `租户 #${currentTenantId}` : "")

  async function onSwitch(id: number) {
    if (!id || id === currentTenantId || switching) return
    setSwitching(true)
    setError(null)
    setOpen(false)
    try {
      await switchEiamTenant(id)
      // On success switchEiamTenant does location.replace (full reload), so we
      // never reach the line below in practice.
    } catch (err) {
      setError(err instanceof Error ? err.message : "租户切换失败")
      setSwitching(false)
    }
  }

  return (
    <div ref={rootRef} className="relative">
      <button
        type="button"
        data-testid="tenant-selector-trigger"
        onClick={() => setOpen((v) => !v)}
        disabled={switching}
        title={currentName}
        className="flex w-full items-center gap-2 rounded-lg border border-border bg-[var(--glass-bg)] px-3 py-2 text-left transition-colors duration-200 hover:border-[var(--border-strong)] hover:bg-[var(--glass-bg-hover)] disabled:opacity-60"
      >
        <Users className="h-3.5 w-3.5 shrink-0 text-accent-blue" />
        <span className="min-w-0 flex-1 truncate text-[13px] text-foreground">
          {switching ? "切换中…" : currentName || "选择租户"}
        </span>
        <ChevronsUpDown className="h-3 w-3 shrink-0 text-text-tertiary" />
      </button>

      {open && (
        <div className="absolute left-0 top-full z-50 mt-1 max-h-72 w-full min-w-48 overflow-auto rounded-lg border border-border bg-popover py-1 shadow-lg">
          {tenants.map((t) => (
            <button
              key={t.id}
              type="button"
              data-testid={`tenant-option-${t.id}`}
              onClick={() => void onSwitch(t.id)}
              className="flex w-full items-center justify-between gap-2 px-3 py-2 text-left text-[13px] text-text-regular hover:bg-bg-hover"
            >
              <span className="truncate">{t.name || `租户 #${t.id}`}</span>
              {t.id === currentTenantId && (
                <Check className="h-4 w-4 shrink-0 text-accent-blue" />
              )}
            </button>
          ))}
        </div>
      )}

      {error && (
        <div className="absolute left-0 top-full z-50 mt-1 w-full rounded-lg border border-destructive/40 bg-destructive/10 px-3 py-2 text-xs text-destructive">
          {error}
        </div>
      )}
    </div>
  )
}
