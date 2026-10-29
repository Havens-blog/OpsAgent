"use client"

// Sidebar — design-aligned to watch-alert src/components/sider/index.jsx so both
// consoles read as one product family:
//   top     : Xirang logo + tenant selector (glass button, accent-blue icon)
//   divider : gradient hairline
//   middle  : grouped nav — uppercase 11px group titles; 38px items; the selected
//             item = solid bg-primary + text-primary-foreground, semibold, shadow-sm;
//             hover = bg-subtle wash + 6px slide + text-foreground
//   bottom  : user (primary avatar + username → popover: 个人信息 / 退出登录)
//             + theme toggle (Sun in dark / Moon in light, hc_theme contract)
//
// There is deliberately NO top header — watch-alert has none; breadcrumb lives
// inside the content card (AppShell). The sidebar owns the single profile fetch
// and passes tenant context down to TenantSelector.

import Link from "next/link"
import { usePathname } from "next/navigation"
import { useEffect, useRef, useState } from "react"
import {
  AlertTriangle,
  BookOpen,
  Bot,
  GitBranch,
  LayoutDashboard,
  LogOut,
  MessageSquare,
  Moon,
  Network,
  Search,
  Settings,
  Sun,
  User,
} from "lucide-react"
import { cn } from "@/lib/utils"
import {
  logoutEiam,
  useEiamProfile,
  type EiamProfile,
} from "@/lib/eiam"
import { initTheme, resolveTheme, toggleTheme, watchTheme } from "@/lib/theme"
import { TenantSelector } from "@/components/tenant-selector"

type NavItem = {
  href: string
  label: string
  icon: React.ComponentType<{ className?: string }>
}

type NavGroup = {
  label: string
  items: NavItem[]
}

// Exported for AppShell breadcrumb resolution (single source of truth).
export const navGroups: NavGroup[] = [
  {
    label: "核心",
    items: [
      { href: "/", label: "仪表盘", icon: LayoutDashboard },
      { href: "/rca", label: "RCA 分析", icon: Search },
      { href: "/chat", label: "对话助手", icon: MessageSquare },
    ],
  },
  {
    label: "数据视图",
    items: [
      { href: "/topology", label: "拓扑视图", icon: Network },
      { href: "/changes", label: "变更记录", icon: GitBranch },
      { href: "/incidents", label: "事件列表", icon: AlertTriangle },
    ],
  },
  {
    label: "管理",
    items: [
      { href: "/agents", label: "Agent 管理", icon: Bot },
      { href: "/settings", label: "系统配置", icon: Settings },
      { href: "/knowledge", label: "知识库", icon: BookOpen },
    ],
  },
]

// 息壤 Xirang — horizontal wordmark (260x64), shown in the sidebar top.
function XirangWordmark() {
  return (
    <svg viewBox="0 0 260 64" xmlns="http://www.w3.org/2000/svg" className="h-9 w-[150px]">
      <title>息壤 Xirang</title>
      <defs>
        <linearGradient id="hsh-top" x1="8" y1="22" x2="56" y2="46" gradientUnits="userSpaceOnUse">
          <stop offset="0" stopColor="#7170ff" />
          <stop offset="1" stopColor="#5e6ad2" />
        </linearGradient>
        <linearGradient id="hsh-sprout" x1="32" y1="34" x2="32" y2="6" gradientUnits="userSpaceOnUse">
          <stop offset="0" stopColor="#7170ff" />
          <stop offset="1" stopColor="#a5b4fc" />
        </linearGradient>
      </defs>
      <g transform="translate(0,2) scale(0.92)">
        <path d="M8 46 L32 58 L56 46" fill="none" stroke="#5e6ad2" strokeOpacity="0.45" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" />
        <path d="M8 40 L32 52 L56 40" fill="none" stroke="#5e6ad2" strokeOpacity="0.75" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" />
        <path d="M32 22 L56 34 L32 46 L8 34 Z" fill="url(#hsh-top)" />
        <g stroke="url(#hsh-sprout)" strokeWidth="3" strokeLinecap="round" fill="none">
          <path d="M32 34 V14" />
          <path d="M32 24 L22 17" />
          <path d="M32 24 L42 17" />
        </g>
        <circle cx="32" cy="34" r="3.2" fill="#ffffff" />
        <circle cx="32" cy="11" r="3.6" fill="#a5b4fc" />
        <circle cx="20" cy="15.5" r="3" fill="#8b93ff" />
        <circle cx="44" cy="15.5" r="3" fill="#8b93ff" />
      </g>
      <text x="72" y="42" fontSize="26" fontWeight="700" fill="currentColor" fontFamily="Inter, system-ui, sans-serif">息壤</text>
      <text x="130" y="42" fontSize="15" fontWeight="500" fill="#8a8f98" fontFamily="Inter, system-ui, sans-serif" letterSpacing="2">XIRANG</text>
    </svg>
  )
}

function useThemeState() {
  const [isDark, setIsDark] = useState(true)
  useEffect(() => {
    const sync = () => setIsDark(resolveTheme() === "dark")
    initTheme()
    sync()
    return watchTheme(sync)
  }, [])
  return { isDark, toggle: toggleTheme }
}

function SidebarBottom({ profile, failed }: { profile: EiamProfile | null; failed: boolean }) {
  const { isDark, toggle } = useThemeState()
  const [menuOpen, setMenuOpen] = useState(false)
  const rootRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!menuOpen) return
    function onDown(e: MouseEvent) {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) {
        setMenuOpen(false)
      }
    }
    document.addEventListener("mousedown", onDown)
    return () => document.removeEventListener("mousedown", onDown)
  }, [menuOpen])

  const username = profile?.username || ""

  return (
    <div
      ref={rootRef}
      className="relative flex items-center justify-between gap-2 border-t border-border bg-elevated px-3 py-3"
    >
      {/* User: primary avatar (first letter) + username → popover menu */}
      <button
        type="button"
        data-testid="user-trigger"
        onClick={() => setMenuOpen((v) => !v)}
        className="flex min-w-0 flex-1 items-center gap-2.5 rounded-lg p-1.5 text-left hover:bg-bg-hover"
      >
        <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-primary text-sm font-bold text-primary-foreground">
          {username ? username.charAt(0).toUpperCase() : "·"}
        </span>
        <span className="min-w-0 truncate text-[13px] text-foreground">
          {failed ? "未获取用户" : username || "…"}
        </span>
      </button>

      {/* Theme toggle — Sun in dark (switch to light) / Moon in light */}
      <button
        type="button"
        data-testid="theme-toggle"
        onClick={() => toggle()}
        aria-label={isDark ? "切换到浅色主题" : "切换到深色主题"}
        title={isDark ? "切换到浅色主题" : "切换到深色主题"}
        className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg text-text-secondary hover:bg-bg-hover hover:text-foreground"
      >
        {isDark ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
      </button>

      {menuOpen && (
        <div className="absolute bottom-full left-0 z-50 mb-2 w-full min-w-44 rounded-lg border border-border bg-popover py-1 shadow-lg">
          <Link
            href="/settings"
            data-testid="user-profile-item"
            onClick={() => setMenuOpen(false)}
            className="flex items-center gap-2 px-3 py-2 text-[13px] text-text-regular hover:bg-bg-hover"
          >
            <User className="h-4 w-4" />
            个人信息
          </Link>
          <div className="my-1 h-px bg-border" />
          <button
            type="button"
            data-testid="user-logout-item"
            onClick={() => void logoutEiam()}
            className="flex w-full items-center gap-2 px-3 py-2 text-left text-[13px] text-destructive hover:bg-bg-hover"
          >
            <LogOut className="h-4 w-4" />
            退出登录
          </button>
        </div>
      )}
    </div>
  )
}

export function Sidebar() {
  const pathname = usePathname()
  const { profile, failed } = useEiamProfile()

  return (
    <aside className="flex h-full w-[210px] shrink-0 flex-col overflow-hidden rounded-xl bg-elevated shadow-sm">
      {/* Top: logo + tenant selector (sticky) */}
      <div className="sticky top-0 z-10 bg-elevated px-3 pt-4">
        <div className="mb-4 flex justify-center text-foreground">
          <XirangWordmark />
        </div>
        {failed ? (
          <div className="rounded-lg border border-dashed border-destructive/40 px-3 py-2 text-xs text-destructive opacity-80">
            租户不可用
          </div>
        ) : (
          <TenantSelector
            tenants={profile?.tenants ?? []}
            currentTenantId={profile?.currentTenantId ?? 0}
          />
        )}
      </div>

      {/* Gradient hairline divider */}
      <div
        className="mx-4 mt-3 h-px shrink-0"
        style={{
          background:
            "linear-gradient(90deg, transparent 0%, var(--border-strong) 50%, transparent 100%)",
        }}
      />

      {/* Grouped nav */}
      <nav className="flex-1 overflow-y-auto pb-2 pt-1">
        {navGroups.map((group) => (
          <div key={group.label}>
            <div className="px-4 pb-1 pt-3 text-[11px] tracking-[1px] text-text-secondary">
              {group.label}
            </div>
            <div className="space-y-0.5 px-0">
              {group.items.map((item) => {
                const active =
                  item.href === "/"
                    ? pathname === "/"
                    : pathname.startsWith(item.href)
                const Icon = item.icon
                return (
                  <Link
                    key={item.href}
                    href={item.href}
                    data-testid={`nav-${item.href.replace(/\//g, "") || "home"}`}
                    className={cn(
                      "mx-2 flex h-[38px] items-center gap-3 rounded-lg px-4 text-[13px] transition-all duration-300",
                      active
                        ? "bg-primary font-semibold text-primary-foreground shadow-sm"
                        : "text-text-secondary hover:translate-x-1.5 hover:bg-subtle hover:text-foreground",
                    )}
                  >
                    <Icon className="h-4 w-4 shrink-0" />
                    <span className="leading-none">{item.label}</span>
                  </Link>
                )
              })}
            </div>
          </div>
        ))}
      </nav>

      {/* Bottom: user + theme (parity watch-alert sider bottom bar) */}
      <SidebarBottom profile={profile} failed={failed} />
    </aside>
  )
}
