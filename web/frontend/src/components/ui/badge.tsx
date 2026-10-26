import * as React from "react"
import { cva, type VariantProps } from "class-variance-authority"
import { cn } from "@/lib/utils"

const badgeVariants = cva(
  "inline-flex items-center rounded-full border px-2.5 py-0.5 text-xs font-semibold transition-colors focus:outline-none",
  {
    variants: {
      variant: {
        default:
          "border-transparent bg-primary text-primary-foreground",
        secondary:
          "border-transparent bg-secondary text-secondary-foreground",
        destructive:
          "border-transparent bg-destructive text-destructive-foreground",
        outline: "text-foreground",
        success:
          "border-transparent bg-status-healthy/20 text-status-healthy",
        warning:
          "border-transparent bg-status-degraded/20 text-status-degraded",
        danger:
          "border-transparent bg-status-unhealthy/20 text-status-unhealthy",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  }
)

export interface BadgeProps
  extends React.HTMLAttributes<HTMLDivElement>,
    VariantProps<typeof badgeVariants> {}

function Badge({ className, variant, ...props }: BadgeProps) {
  return (
    <div className={cn(badgeVariants({ variant }), className)} {...props} />
  )
}

/** Maps an incident severity (P0..P4) to a badge variant. */
export function severityVariant(sev: string) {
  const s = sev.toUpperCase()
  if (s === "P0" || s === "P1") return "danger" as const
  if (s === "P2") return "warning" as const
  return "secondary" as const
}

/** Maps a health/status string to a badge variant. */
export function healthVariant(health?: string) {
  switch ((health || "").toLowerCase()) {
    case "healthy":
      return "success" as const
    case "degraded":
      return "warning" as const
    case "unhealthy":
      return "danger" as const
    default:
      return "secondary" as const
  }
}

export { Badge, badgeVariants }
