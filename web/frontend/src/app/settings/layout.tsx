"use client"

import { usePathname } from "next/navigation"
import Link from "next/link"
import { Brain, MessageSquare, Database, Shield } from "lucide-react"

const navItems = [
  { href: "/settings/model", label: "模型配置", icon: Brain },
  { href: "/settings/im", label: "IM 通知", icon: MessageSquare },
  { href: "/settings/datasource", label: "数据源", icon: Database },
  { href: "/settings/security", label: "安全审计", icon: Shield },
]

export default function SettingsLayout({
  children,
}: {
  children: React.ReactNode
}) {
  const pathname = usePathname()

  return (
    <div className="space-y-6">
      <header>
        <h1 className="text-3xl font-bold tracking-tight">系统配置</h1>
        <p className="text-muted-foreground">
          模型、IM、数据源与安全配置
        </p>
      </header>

      <div className="flex gap-8">
        <nav className="w-56 shrink-0 space-y-1">
          {navItems.map((item) => {
            const isActive = pathname.startsWith(item.href)
            const Icon = item.icon
            return (
              <Link
                key={item.href}
                href={item.href}
                className={`flex items-center gap-2 rounded-md px-3 py-2 text-sm font-medium transition-colors ${
                  isActive
                    ? "bg-primary text-primary-foreground"
                    : "text-muted-foreground hover:text-foreground"
                }`}
              >
                <Icon className="h-4 w-4" />
                {item.label}
              </Link>
            )
          })}
        </nav>

        <main className="flex-1">{children}</main>
      </div>
    </div>
  )
}
