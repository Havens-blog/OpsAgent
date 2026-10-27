import { NextRequest } from "next/server"

/**
 * Server-side proxy for EIAM identity routes: /api/iam/* → EIAM /api/*.
 *
 * Tenant switching must work no matter which entry point the browser uses:
 *   - via nginx :8888, bare /api/iam/* is proxied to EIAM directly by nginx;
 *   - via the Next dev server (:3100) there is no nginx — without this handler
 *     GET /api/iam/user/profile 404s and the header tenant selector silently
 *     degrades to "无所属租户".
 *
 * The frontend therefore calls `${basePath}/api/iam/*` (Next applies the
 * basePath itself), and this handler performs the same rewrite nginx does
 * (/api/iam/X → EIAM /api/X) with FULL cookie forwarding in BOTH directions:
 * the EIAM session cookie rides in on the request, and the re-signed
 * Set-Cookie (tenant/switch destroys + re-issues the session JWT) rides back
 * out — dropping it would 401 the very next request.
 */

const EIAM = (
  process.env.EIAM_INTERNAL_URL || "http://127.0.0.1:9000"
).replace(/\/$/, "")

// Never cache identity/session data.
export const dynamic = "force-dynamic"

async function proxy(request: NextRequest, path: string[]): Promise<Response> {
  const search = request.nextUrl.search
  const target = `${EIAM}/api/${path.join("/")}${search}`

  const headers = new Headers()
  const ct = request.headers.get("content-type")
  if (ct) headers.set("content-type", ct)
  const cookie = request.headers.get("cookie")
  if (cookie) headers.set("cookie", cookie)
  const auth = request.headers.get("authorization")
  if (auth) headers.set("authorization", auth)

  const method = request.method
  const hasBody = method !== "GET" && method !== "HEAD"
  const body = hasBody ? await request.arrayBuffer() : undefined

  let res: Response
  try {
    res = await fetch(target, {
      method,
      headers,
      body,
      cache: "no-store",
      redirect: "manual",
    })
  } catch (err) {
    return new Response(
      JSON.stringify({
        code: 502,
        msg: `无法连接 EIAM (${EIAM})`,
        details: err instanceof Error ? err.message : String(err),
      }),
      { status: 502, headers: { "content-type": "application/json" } }
    )
  }

  const resBody = await res.arrayBuffer()
  const resHeaders = new Headers()
  const resCt = res.headers.get("content-type")
  if (resCt) resHeaders.set("content-type", resCt)
  // Forward EVERY Set-Cookie (not just the first) — ginx session re-issuance
  // may emit several, and they carry the new tenant identity.
  const setCookies =
    typeof res.headers.getSetCookie === "function"
      ? res.headers.getSetCookie()
      : []
  for (const sc of setCookies) resHeaders.append("set-cookie", sc)

  return new Response(resBody, { status: res.status, headers: resHeaders })
}

type Ctx = { params: Promise<{ path: string[] }> }

export async function GET(request: NextRequest, ctx: Ctx) {
  return proxy(request, (await ctx.params).path)
}
export async function POST(request: NextRequest, ctx: Ctx) {
  return proxy(request, (await ctx.params).path)
}
export async function PUT(request: NextRequest, ctx: Ctx) {
  return proxy(request, (await ctx.params).path)
}
export async function PATCH(request: NextRequest, ctx: Ctx) {
  return proxy(request, (await ctx.params).path)
}
export async function DELETE(request: NextRequest, ctx: Ctx) {
  return proxy(request, (await ctx.params).path)
}
