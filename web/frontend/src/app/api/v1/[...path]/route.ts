import { NextRequest } from "next/server"

/**
 * Server-side proxy for all /api/v1/* calls.
 *
 * The browser calls same-origin /api/v1/* with NO credentials. This handler
 * runs on the Next.js server, injects the bearer token from a server-only env
 * var (API_AUTH_TOKEN — never NEXT_PUBLIC_, so it is never shipped to the
 * browser), and forwards to the Go backend. This is why enabling backend auth
 * does not break the console: the secret lives only on the server side.
 *
 * Replaces the dumb next.config rewrite, which could not inject a conditional
 * Authorization header from server-only config.
 */

const BACKEND = (
  process.env.BACKEND_INTERNAL_URL ||
  process.env.NEXT_PUBLIC_API_URL ||
  "http://127.0.0.1:8080"
).replace(/\/$/, "")

const API_TOKEN = process.env.API_AUTH_TOKEN || ""

// EIAM session cookie name. When a request carries this cookie (set by EIAM
// after the user logs in on the same domain), the proxy forwards it to the
// backend as `Authorization: Bearer <token>` — the EIAM session JWT the Go
// backend verifies. Must match the backend's OPSAGENT_SESSION_COOKIE_NAME.
const EIAM_COOKIE = process.env.OPSAGENT_SESSION_COOKIE_NAME || "ecmdb-token-key"

// Never cache operations-console data.
export const dynamic = "force-dynamic"

async function proxy(request: NextRequest, path: string[]): Promise<Response> {
  const search = request.nextUrl.search
  const target = `${BACKEND}/api/v1/${path.join("/")}${search}`

  const headers = new Headers()
  const ct = request.headers.get("content-type")
  if (ct) headers.set("content-type", ct)
  const accept = request.headers.get("accept")
  if (accept) headers.set("accept", accept)

  // Auth precedence (first match wins):
  //   1. an inbound Authorization header (explicit caller-provided token);
  //   2. the EIAM session cookie — the real user session, forwarded as a
  //      Bearer token for the backend to verify (OpsAgent's EIAM contract);
  //   3. the static server-only API_AUTH_TOKEN (no-EIAM / dev fallback).
  const inboundAuth = request.headers.get("authorization")
  const eiamToken = request.cookies.get(EIAM_COOKIE)?.value
  if (inboundAuth) {
    headers.set("authorization", inboundAuth)
  } else if (eiamToken) {
    headers.set("authorization", `Bearer ${eiamToken}`)
  } else if (API_TOKEN) {
    headers.set("authorization", `Bearer ${API_TOKEN}`)
  }

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
      // Propagate client disconnects (e.g. the chat "stop" button) upstream so
      // the Go backend cancels the LLM stream instead of running it to the end.
      signal: request.signal,
    })
  } catch (err) {
    return new Response(
      JSON.stringify({
        error: {
          code: "backend_unreachable",
          message: `无法连接后端 (${BACKEND})`,
          details: err instanceof Error ? err.message : String(err),
        },
      }),
      { status: 502, headers: { "content-type": "application/json" } }
    )
  }

  const resHeaders = new Headers()
  const resCt = res.headers.get("content-type") || ""
  if (resCt) resHeaders.set("content-type", resCt)

  // Stream SSE responses byte-for-byte instead of buffering them, so deltas
  // reach the browser as the backend emits them.
  if (resCt.includes("text/event-stream") && res.body) {
    const cacheControl = res.headers.get("cache-control")
    if (cacheControl) resHeaders.set("cache-control", cacheControl)
    return new Response(res.body, { status: res.status, headers: resHeaders })
  }

  const resBody = await res.arrayBuffer()
  // 204/205/304 are bodiless by spec: the Response constructor rejects ANY body
  // argument for them (even a zero-length ArrayBuffer). Passing one turned every
  // successful DELETE (backend replies 204) into a proxied 500.
  const bodiless = res.status === 204 || res.status === 205 || res.status === 304
  return new Response(bodiless ? null : resBody, {
    status: res.status,
    headers: resHeaders,
  })
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
