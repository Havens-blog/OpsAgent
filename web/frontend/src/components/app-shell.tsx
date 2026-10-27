"use client"

// AppShell — layout aligned to watch-alert ComponentsContent (src/components/index.jsx):
// page background (--bg-page), a floating rounded sidebar card (210px, 12px
// radius, soft shadow), a rounded content card with the breadcrumb row inside,
// and a slim footer. There is NO top header — tenant/user/theme live in the
// sidebar bottom/top, exactly like watch-alert.

import Link from "next/link"
import { usePathname } from "next/navigation"
import { Sidebar, navGroups } from "@/components/sidebar"

// Breadcrumb "当前页" label: resolve the active nav item by path prefix
// (moved here from the removed AppHeader; same longest-prefix logic).
function useCurrentPageLabel(pathname: string): string {
  for (const group of navGroups) {
    let best = ""
    for (const item of group.items) {
      const href = item.href
      const match = href === "/" ? pathname === "/" : pathname.startsWith(href)
      if (match && href.length > best.length) best = href
    }
    const found = group.items.find((i) => i.href === best)
    if (found) return found.label
  }
  return "当前页"
}

export function AppShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname()
  const pageLabel = useCurrentPageLabel(pathname)

  return (
    <div className="flex h-screen overflow-hidden gap-4 bg-background p-4 pb-0">
      {/* Sidebar card */}
      <Sidebar />

      {/* Right column: content card + footer */}
      <div className="flex min-w-0 flex-1 flex-col">
        <main className="flex flex-1 flex-col overflow-hidden rounded-xl bg-elevated shadow-[0_2px_8px_rgba(0,0,0,0.06)]">
          {/* Breadcrumb row (watch-alert renders page title inside content) */}
          <nav
            aria-label="面包屑"
            className="flex shrink-0 items-center gap-2 px-5 pb-1 pt-4 text-sm"
          >
            <Link href="/" className="text-text-secondary hover:text-foreground">
              首页
            </Link>
            <span className="text-text-tertiary" aria-hidden="true">
              /
            </span>
            <span className="text-foreground" aria-current="page">
              {pageLabel}
            </span>
          </nav>
          <div className="flex-1 overflow-y-auto p-5 pt-3">{children}</div>
        </main>

        {/* Footer — parity watch-alert bottom bar */}
        <footer className="shrink-0 border-t border-border bg-elevated py-0.5 text-center text-xs text-text-secondary">
          <span className="text-accent-blue">息壤</span> 提供智能运维根因分析服务!
        </footer>
      </div>
    </div>
  )
}
