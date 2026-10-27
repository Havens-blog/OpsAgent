import type { Metadata } from "next"
import { headers } from "next/headers"
import { AppShell } from "@/components/app-shell"
import "./globals.css"

// Per-request CSP nonce requires request-time rendering: a nonce changes every
// request and cannot be baked into a build-time static HTML. Forcing dynamic
// rendering app-wide lets the middleware's nonce propagate into Next's inline
// bootstrap scripts. Acceptable for a data-driven console (pages are not
// cacheable static content anyway). Applies to all nested routes.
export const dynamic = "force-dynamic"

export const metadata: Metadata = {
  title: "息壤 — Operations Intelligence",
  description:
    "Multi-cloud operations intelligence console for root cause analysis, incident management, and topology-aware monitoring.",
}

// Pre-paint theme resolution — mirrors watch-alert index.js initTheme: read the
// shared `hc_theme` preference (JSON {"theme"} or legacy plain string) and toggle
// html.light/.dark BEFORE first paint so a light-theme user never flashes dark.
const themeBootstrap = `(function(){try{var raw=localStorage.getItem('hc_theme');var t=null;if(raw==='light'||raw==='dark')t=raw;else if(raw){try{var o=JSON.parse(raw);if(o&&o.theme==='light'||o&&o.theme==='dark')t=o.theme}catch(e){}}if(!t){t=window.matchMedia&&window.matchMedia('(prefers-color-scheme: light)').matches?'light':'dark'}var el=document.documentElement;el.classList.toggle('light',t==='light');el.classList.toggle('dark',t!=='light')}catch(e){}})()`

export default async function RootLayout({
  children,
}: {
  children: React.ReactNode
}) {
  // The middleware stamps this nonce on the inbound request headers; Next uses
  // it for its own bootstrap scripts — our inline theme script must carry the
  // same nonce or CSP (nonce + strict-dynamic) silently drops it.
  const nonce = (await headers()).get("x-nonce") ?? undefined
  return (
    <html lang="zh-CN" className="dark" suppressHydrationWarning>
      <head>
        <script
          nonce={nonce}
          dangerouslySetInnerHTML={{ __html: themeBootstrap }}
        />
      </head>
      <body className="min-h-screen bg-background text-foreground antialiased">
        <AppShell>{children}</AppShell>
      </body>
    </html>
  )
}
