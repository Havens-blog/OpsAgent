import { NextRequest, NextResponse } from 'next/server'

/**
 * Per-request nonce CSP for the Next.js console.
 *
 * Why middleware and not nginx: Next.js App Router emits INLINE bootstrap/
 * hydration <script> tags in production that cannot be externalized, so a
 * host-only `script-src 'self'` white-screens the app. The secure fix is a
 * per-request nonce stamped onto both the CSP header AND every script Next
 * renders. Only Next can coordinate that, so the CSP header is owned here, not
 * in nginx. Static headers (X-Frame-Options, HSTS) stay in nginx.
 *
 * Path filtering is done INSIDE the function on request.nextUrl.pathname
 * (which includes basePath) instead of a matcher regex: with basePath the
 * matcher-vs-path semantics are version-sensitive, so an explicit prefix check
 * is unambiguous and robust.
 */
export function middleware(request: NextRequest) {
  const { pathname } = request.nextUrl

  // Skip the server-side API proxy (JSON, no CSP needed) and immutable static
  // assets (nonce-irrelevant). pathname includes basePath (/opsagent).
  if (
    pathname.startsWith('/opsagent/api/') ||
    pathname.startsWith('/opsagent/_next/static') ||
    pathname.startsWith('/opsagent/_next/image') ||
    pathname === '/opsagent/favicon.ico'
  ) {
    return NextResponse.next()
  }

  const nonce = btoa(crypto.randomUUID())

  // Dev needs 'unsafe-eval': react-refresh and webpack eval-source-map both
  // evaluate strings, and without it EVERY client script dies (hydration never
  // runs, useEffects never fire) — the page renders as SSR-only dead HTML.
  // Production stays strict.
  const isDev = process.env.NODE_ENV !== "production"

  const csp = [
    `default-src 'self'`,
    `script-src 'self' 'nonce-${nonce}' 'strict-dynamic'${isDev ? " 'unsafe-eval'" : ""}`,
    `style-src 'self' 'unsafe-inline'`,
    `img-src 'self' data:`,
    `font-src 'self'`,
    `connect-src 'self'`,
    `frame-ancestors 'none'`,
    `object-src 'none'`,
    `base-uri 'self'`,
    `form-action 'self'`,
  ].join('; ')

  // Next auto-stamps this nonce onto the scripts it renders when it sees the
  // CSP on the INBOUND request header.
  const requestHeaders = new Headers(request.headers)
  requestHeaders.set('x-nonce', nonce)
  requestHeaders.set('content-security-policy', csp)

  const response = NextResponse.next({ request: { headers: requestHeaders } })
  response.headers.set('content-security-policy', csp)
  response.headers.set('x-mw-ran', '1') // diagnostic: did middleware execute?
  return response
}
