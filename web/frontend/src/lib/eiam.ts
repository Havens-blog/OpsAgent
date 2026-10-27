/**
 * EIAM identity client — tenant context for the console header.
 *
 * Tenant routes go through the Next.js server proxy at
 * src/app/api/iam/[...path]/route.ts (same pattern as src/lib/api.ts): the
 * browser calls same-origin `${basePath}/api/iam/*`, the handler rewrites to
 * EIAM /api/* and forwards cookies BOTH ways — the EIAM session cookie rides
 * in, and tenant/switch's re-signed Set-Cookie rides back out. This works on
 * every entry point (nginx :8888 and direct Next :3100); a bare /api/iam/*
 * fetch would 404 anywhere nginx's /api/iam location is not in the path.
 *
 * Contract (verified against haven-console stores/tenant.ts):
 *   GET  /api/iam/user/profile      -> { code: 0, msg, data: { user, tenants,
 *                                       current_tenant_id, is_admin, ... } }
 *   POST /api/iam/tenant/switch     body { tenant_id: <id> }  -> { code: 0 }
 *       On success EIAM destroyed the old session + re-signed the JWT, so the
 *       caller MUST do a full-page reload (location.replace) — not an SPA
 *       store swap — to pick up the new cookie + re-run the profile fetch.
 */

"use client"

import { useEffect, useState } from "react"

// Same-origin, basePath-prefixed — identical convention to src/lib/api.ts.
const API_BASE = process.env.NEXT_PUBLIC_BASE_PATH ?? "/opsagent"

// Re-declared here (rather than importing from api.ts, where it is private) to
// keep the EIAM client self-contained. Matches api.ts EIAM_LOGIN_URL semantics.
const EIAM_LOGIN_URL =
  process.env.NEXT_PUBLIC_EIAM_LOGIN_URL || "/console/login"

function redirectToLogin() {
  if (typeof window === "undefined") return
  const current = window.location.href
  if (window.location.pathname.startsWith(EIAM_LOGIN_URL)) return
  const sep = EIAM_LOGIN_URL.includes("?") ? "&" : "?"
  window.location.href = `${EIAM_LOGIN_URL}${sep}redirect=${encodeURIComponent(current)}`
}

/** A tenant the current user belongs to (EIAM TenantVO subset). */
export interface EiamTenant {
  id: number
  name: string
  code: string
}

/** Normalized subset of EIAM GET /api/user/profile data. */
export interface EiamProfile {
  tenants: EiamTenant[]
  currentTenantId: number
  isAdmin: boolean
  username?: string
}

/** Envelope shape common to all ginx endpoints: { code, msg, data }. */
interface EiamEnvelope<T> {
  code: number
  msg?: string
  data?: T
}

async function eiamRequest<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      ...(init?.headers || {}),
    },
    cache: "no-store",
    credentials: "same-origin",
  })

  if (res.status === 401) {
    // Unauthenticated → bounce to EIAM login (same contract as api.ts).
    redirectToLogin()
  }

  let body: EiamEnvelope<T>
  try {
    body = (await res.json()) as EiamEnvelope<T>
  } catch {
    throw new Error(`EIAM ${res.status} ${res.statusText}`)
  }

  if (!res.ok || body.code !== 0) {
    throw new Error(body.msg || `EIAM ${res.status}`)
  }

  return body.data as T
}

/**
 * Fetch the current user's tenants + active tenant id from EIAM.
 * Returns null when the user is unauthenticated (caller shows nothing).
 */
export async function getEiamProfile(): Promise<EiamProfile | null> {
  const data = await eiamRequest<{
    tenants?: EiamTenant[]
    current_tenant_id?: number
    is_admin?: boolean
    user?: { username?: string }
  }>("/api/iam/user/profile")

  return {
    tenants: (data.tenants || []).map((t) => ({
      id: Number(t.id),
      name: t.name,
      code: t.code,
    })),
    currentTenantId: Number(data.current_tenant_id || 0),
    isAdmin: Boolean(data.is_admin),
    username: data.user?.username,
  }
}

/**
 * Switch the active tenant. On success the EIAM session cookie has been
 * re-signed, so reload the whole page (location.replace) to re-run the profile
 * fetch and reload tenant-scoped data. Returns true on success.
 */
export async function switchEiamTenant(id: number): Promise<boolean> {
  await eiamRequest<void>("/api/iam/tenant/switch", {
    method: "POST",
    body: JSON.stringify({ tenant_id: id }),
  })
  // Hard rule (parity haven-console): full-page reload, not an SPA swap.
  window.location.replace(window.location.href)
  return true
}

/**
 * Destroy the shared EIAM session (POST /api/iam/user/logout → EIAM
 * /api/user/logout). Non-credential preferences (hc_theme) are kept — parity
 * with haven-console/watch-alert logout. Then bounce to the console login page.
 */
export async function logoutEiam(): Promise<void> {
  try {
    await eiamRequest<void>("/api/iam/user/logout", { method: "POST" })
  } catch {
    // Logout best-effort: still redirect so the user is never stuck.
  }
  redirectToLogin()
}

/**
 * One-shot EIAM profile loader for the shell (username + tenant context).
 * `failed` distinguishes "request failed" from "still loading" so the sidebar
 * can show a real error state instead of a misleading placeholder.
 */
export function useEiamProfile(): {
  profile: EiamProfile | null
  failed: boolean
} {
  const [profile, setProfile] = useState<EiamProfile | null>(null)
  const [failed, setFailed] = useState(false)

  useEffect(() => {
    let cancelled = false
    getEiamProfile()
      .then((p) => {
        if (!cancelled) setProfile(p)
      })
      .catch(() => {
        // 401 already redirected inside eiamRequest; everything else → failed.
        if (!cancelled) setFailed(true)
      })
    return () => {
      cancelled = true
    }
  }, [])

  return { profile, failed }
}
