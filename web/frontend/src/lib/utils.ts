import { clsx, type ClassValue } from "clsx"
import { twMerge } from "tailwind-merge"

/**
 * cn merges Tailwind class names, resolving conflicts.
 * Standard shadcn/ui helper.
 */
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

/** Format an ISO timestamp for display. */
export function formatTime(iso?: string): string {
  if (!iso) return "—"
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString("zh-CN", { hour12: false })
}

/** Format a relative "time ago" string. */
export function timeAgo(iso?: string): string {
  if (!iso) return "—"
  const d = new Date(iso).getTime()
  if (Number.isNaN(d)) return iso
  const sec = Math.round((Date.now() - d) / 1000)
  if (sec < 60) return `${sec}s ago`
  if (sec < 3600) return `${Math.round(sec / 60)}m ago`
  if (sec < 86400) return `${Math.round(sec / 3600)}h ago`
  return `${Math.round(sec / 86400)}d ago`
}

/** Format a 0-1 confidence as a percentage. */
export function confidencePct(v?: number): string {
  if (v == null || Number.isNaN(v)) return "—"
  return `${Math.round(v * 100)}%`
}
