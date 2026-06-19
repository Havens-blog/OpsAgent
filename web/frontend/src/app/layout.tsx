import type { Metadata } from "next"
import { Sidebar } from "@/components/sidebar"
import "./globals.css"

export const metadata: Metadata = {
  title: "OpsAgent — Operations Intelligence",
  description:
    "Multi-cloud operations intelligence console for root cause analysis, incident management, and topology-aware monitoring.",
}

export default function RootLayout({
  children,
}: {
  children: React.ReactNode
}) {
  return (
    <html lang="zh-CN" className="dark">
      <body className="min-h-screen bg-background antialiased">
        <div className="flex">
          <Sidebar />
          <main className="flex-1 overflow-x-hidden">
            <div className="mx-auto max-w-7xl p-8">{children}</div>
          </main>
        </div>
      </body>
    </html>
  )
}
