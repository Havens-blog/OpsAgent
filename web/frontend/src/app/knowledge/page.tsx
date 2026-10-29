import { BookOpen } from "lucide-react"

export default function KnowledgePage() {
  return (
    <div className="space-y-8">
      <div className="space-y-2">
        <h1 className="text-3xl font-bold tracking-tight">知识库</h1>
        <p className="text-muted-foreground">运维知识、处置案例与常见问题</p>
      </div>

      <div className="flex flex-col items-center justify-center gap-3 rounded-lg border border-dashed px-6 py-20 text-center">
        <BookOpen className="h-10 w-10 text-muted-foreground" />
        <div className="text-lg font-medium text-foreground">知识库未接入</div>
        <p className="max-w-md text-sm text-muted-foreground">
          知识库后端尚未接入，暂无运维知识、处置案例或常见问题数据。接入后此处将展示真实内容。
        </p>
      </div>
    </div>
  )
}
