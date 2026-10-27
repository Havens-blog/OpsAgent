import Link from "next/link"
import { Brain, MessageSquare, Database, Shield, ArrowRight } from "lucide-react"
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
} from "@/components/ui/card"

const configCards = [
  {
    href: "/settings/model",
    title: "模型配置",
    description: "LLM 提供商与参数",
    icon: Brain,
  },
  {
    href: "/settings/im",
    title: "IM 通知",
    description: "钉钉/企微/飞书接入",
    icon: MessageSquare,
  },
  {
    href: "/settings/datasource",
    title: "数据源",
    description: "日志/指标/拓扑/变更",
    icon: Database,
  },
  {
    href: "/settings/security",
    title: "安全审计",
    description: "审计与访问控制",
    icon: Shield,
  },
]

export default function SettingsIndexPage() {
  return (
    <div className="space-y-8">
      <header>
        <h1 className="text-3xl font-bold tracking-tight">系统配置</h1>
        <p className="text-muted-foreground">
          配置模型、IM 通知、数据源与安全策略
        </p>
      </header>

      <div className="grid gap-6 md:grid-cols-2">
        {configCards.map((card) => {
          const Icon = card.icon
          return (
            <Link
              key={card.href}
              href={card.href}
              className="transition-transform hover:scale-[1.02]"
            >
              <Card className="h-full hover:border-primary/50">
                <CardHeader>
                  <div className="flex items-center justify-between">
                    <Icon className="h-6 w-6 text-muted-foreground" />
                    <ArrowRight className="h-4 w-4 text-muted-foreground" />
                  </div>
                  <CardTitle className="mt-2">{card.title}</CardTitle>
                  <CardDescription>{card.description}</CardDescription>
                </CardHeader>
              </Card>
            </Link>
          )
        })}
      </div>
    </div>
  )
}
