import type { NextConfig } from 'next'

const nextConfig: NextConfig = {
  // Mounted under /opsagent so the whole app sits behind nginx at
  // https://<platform>:8888/opsagent/ alongside /console (haven-console),
  // /cam, /cmdb — same origin, shared EIAM cookie, no re-login. basePath
  // auto-prefixes pages, _next static assets, the server-side API route
  // handler (/opsagent/api/v1/*), and <Link>/router navigation. It does NOT
  // prefix raw fetch() URLs — src/lib/api.ts sets its base to /opsagent to
  // match. Overridable so a bare (root-served) build is still possible.
  basePath: process.env.NEXT_BASE_PATH ?? '/opsagent',

  // Trailing slash is LEFT AT DEFAULT (false). It must not be enabled: with
  // trailingSlash:true Next 308-redirects every /opsagent/api/v1/* call to a
  // slashed variant, and POST bodies are lost across that redirect (verified:
  // chat/rca POSTs returned 400 "invalid character" = empty body). The page
  // canonicalization (/opsagent/ -> /opsagent) is instead reconciled at the
  // nginx layer: nginx proxies the /opsagent prefix WITHOUT its usual
  // `location = /x { return 301 /x/; }` slash-forcing block, so there is no
  // 301<->308 loop — Next settles the slash itself in a single hop while API
  // routes (called slashless by src/lib/api.ts) never redirect.

  // Build output dir. Defaults to .next (production unchanged); overridable so
  // an isolated build (e.g. verification) can target a separate directory
  // without disturbing a running `next start` serving the live .next.
  distDir: process.env.NEXT_DIST_DIR || '.next',

  // NOTE: do NOT set `output: 'standalone'`. The frontend is served in
  // production via `next start` (see deployments/windows/start-opsagent.ps1),
  // and Next.js warns that `next start` is incompatible with standalone
  // output — under standalone, `next start` fails to serve the App Router
  // route handlers, which 404s the /api/v1/* proxy and breaks the whole
  // browser -> proxy -> backend login chain after any rebuild. The Docker
  // image (Dockerfile) only builds the Go backend and never serves this
  // frontend, so standalone output served no real deployment anyway.

  // Environment variables exposed to browser
  env: {
    NEXT_PUBLIC_API_URL: process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080',
  },

  // Headers for API proxying if needed
  async headers() {
    return [
      {
        source: '/api/:path*',
        headers: [
          { key: 'Access-Control-Allow-Origin', value: '*' },
          { key: 'Access-Control-Allow-Methods', value: 'GET, POST, PUT, DELETE, OPTIONS' },
          { key: 'Access-Control-Allow-Headers', value: 'Content-Type, Authorization' },
        ],
      },
    ]
  },

  // NOTE: no rewrites. /api/v1/* is served by the server-side route handler
  // at src/app/api/v1/[...path]/route.ts, which injects the server-only
  // bearer token (API_AUTH_TOKEN) before forwarding to the Go backend. A dumb
  // rewrite could not add that conditional, server-only header.
}

export default nextConfig
