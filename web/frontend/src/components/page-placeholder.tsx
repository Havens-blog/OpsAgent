import { Card, CardContent } from "@/components/ui/card"

interface PagePlaceholderProps {
  title: string
  description: string
  icon: React.ComponentType<{ className?: string }>
}

/**
 * Lightweight placeholder for routes whose full implementation
 * is scheduled in a later phase. Keeps navigation reachable
 * while the page is under construction.
 */
export function PagePlaceholder({
  title,
  description,
  icon: Icon,
}: PagePlaceholderProps) {
  return (
    <div className="space-y-8">
      <header>
        <h1 className="text-3xl font-bold tracking-tight">{title}</h1>
        <p className="text-muted-foreground">{description}</p>
      </header>
      <Card>
        <CardContent className="flex flex-col items-center justify-center py-24 text-center">
          <Icon className="h-12 w-12 text-muted-foreground/40" />
          <p className="mt-4 text-lg font-medium">建设中</p>
          <p className="mt-1 text-sm text-muted-foreground">
            此页面即将上线，敬请期待
          </p>
        </CardContent>
      </Card>
    </div>
  )
}
