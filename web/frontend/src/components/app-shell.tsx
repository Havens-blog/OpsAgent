"use client"

// AppShell: page canvas (bg-page), a floating sidebar card (bg-surface + border),
// and the content column rendered directly on the canvas so page cards
// (bg-surface + border) stand out from it. There is no top header; tenant,
// user and theme live in the sidebar.

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
    <div className="flex h-screen overflow-hidden gap-3 bg-page p-3 pb-0">
      {/* Sidebar card */}
      <Sidebar />

      {/* Right column: content on the canvas + footer */}
      <div className="flex min-w-0 flex-1 flex-col">
        <main className="flex flex-1 flex-col overflow-hidden">
          {/* Breadcrumb row (watch-alert renders page title inside content) */}
          <nav
            aria-label="面包屑"
            className="flex shrink-0 items-center gap-2 px-3 pb-1 pt-3 text-sm"
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
          <div className="flex-1 overflow-y-auto px-3 pb-4 pt-3">{children}</div>
        </main>

        {/* Footer — parity watch-alert bottom bar. 对话助手自带「AI 生成内容」免责
            提示，不再叠加这条全局宣传语。 */}
        {pathname !== "/chat" && (
          <footer className="shrink-0 border-t border-border py-0.5 text-center text-xs text-text-secondary">
            <span className="text-primary">息壤</span> 提供智能运维根因分析服务!
          </footer>
        )}
      </div>
    </div>
  )
}
