"use client"

import { useState, useMemo } from "react"
import { Search } from "lucide-react"
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  CardDescription,
} from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { cn, timeAgo } from "@/lib/utils"

type Category = "Runbook" | "案例" | "FAQ" | "全部"

interface KnowledgeEntry {
  id: string
  category: "Runbook" | "案例" | "FAQ"
  title: string
  summary: string
  content: string
  tags: string[]
  updated_at: string
  author: string
}

const MOCK_ENTRIES: KnowledgeEntry[] = [
  {
    id: "kb-001",
    category: "Runbook",
    title: "PostgreSQL 连接池耗尽处置流程",
    summary: "当数据库连接池耗尽导致服务不可用时，如何快速定位并恢复服务的标准处置流程。",
    content: "当 PostgreSQL 连接池耗尽时，首先检查应用侧连接池配置是否合理。典型症状包括：应用层大量 'connection pool exhausted' 错误、数据库侧活跃连接数达到 max_connections、新请求排队超时。\n\n处置步骤：\n1. 立即确认连接池大小设置（通常建议 CPU 核心数 × 2）\n2. 检查是否存在慢查询堆积（pg_stat_activity）\n3. 如有慢查询，优先终止长时间运行的会话\n4. 若连接池配置偏小，临时扩容应用副本数\n5. 优化高频慢查询，降低连接持有时间\n\n预防措施：开启连接池监控告警，定期审查慢查询日志。",
    tags: ["PostgreSQL", "连接池", "应急"],
    updated_at: new Date(Date.now() - 2 * 24 * 60 * 60 * 1000).toISOString(),
    author: "张运维",
  },
  {
    id: "kb-002",
    category: "案例",
    title: "Redis 命中率突降排查案例",
    summary: "生产环境 Redis 缓存命中率从 92% 跌至 65% 的根因分析与处置过程复盘。",
    content: "现象：订单服务 Redis 缓存命中率从正常 92% 突降至 65%，数据库查询量激增 3 倍，导致整体响应时间 P99 从 200ms 升至 800ms。\n\n排查过程：\n1. 检查 Redis 慢查询：发现大量 KEY * 操作\n2. 排查审计日志：某开发人员为调试直接在生产 Redis 执行 KEYS *\n3. KEYS * 是阻塞操作，导致后续命令排队等待\n\n处置方案：\n1. 立即禁止生产环境直接执行 KEYS 命令\n2. 使用 SCAN 替代 KEYS 进行键遍历\n3. 在 Redis 配置中重命名危险命令（rename-command KEYS \"\"）\n4. 命中率逐步恢复至 90%\n\n经验总结：生产环境必须通过 ACL 限制危险命令访问。",
    tags: ["Redis", "缓存", "性能"],
    updated_at: new Date(Date.now() - 5 * 24 * 60 * 60 * 1000).toISOString(),
    author: "李DBA",
  },
  {
    id: "kb-003",
    category: "FAQ",
    title: "如何配置告警静默?",
    summary: "在变更维护期间临时静默告警的标准操作步骤。",
    content: "变更期间临时静默告警可避免误报，但需确保在变更结束后及时恢复。\n\n操作步骤：\n1. 进入告警管理 → 静默规则\n2. 点击「新建静默」，选择静默范围：\n   - 全局静默（谨慎使用）\n   - 按服务静默（推荐）\n   - 按告警规则静默（推荐）\n3. 设置静默时长（建议与变更窗口一致，最长不超过 4 小时）\n4. 填写静默原因与变更单号关联\n5. 提交并通知相关值班人员\n\n注意事项：\n- 静默到期自动恢复，无需手动关闭\n- 静默期间真实故障不会消失，仍需关注监控趋势\n- 变更完成后主动验证告警是否恢复正常",
    tags: ["告警", "静默", "运维操作"],
    updated_at: new Date(Date.now() - 12 * 60 * 1000).toISOString(),
    author: "王SRE",
  },
  {
    id: "kb-004",
    category: "案例",
    title: "K8s 滚动更新导致 5xx 应急",
    summary: "Deployment 滚动更新期间因就绪检查配置不当引发大量 5xx 错误的案例分析。",
    content: "事故描述：订单服务滚动更新期间，新 Pod 启动后立即标记 Ready 并接收流量，但应用 JVM 预热需 30 秒，导致前 30 秒请求全部超时返回 5xx，错误率峰值达 8%。\n\n根因分析：\n1. 就绪探针只检查 HTTP 200 响应，未判断应用是否真正就绪\n2. minReadySeconds 配置为 0，Pod Ready 后立即加入 Service\n3. maxSurge=1 maxUnavailable=0，滚动更新速度过快\n\n解决方案：\n1. 就绪探针增加业务健康检查端点（/health/ready），验证依赖连接可用\n2. 设置 minReadySeconds=30，确保 Pod 真正稳定后再承接流量\n3. 调整滚动策略为 maxSurge=1 maxUnavailable=1\n4. 引入灰度发布金丝雀策略，逐步切换流量\n\n效果验证：后续滚动更新错误率保持在 0.1% 以下。",
    tags: ["Kubernetes", "滚动更新", "5xx"],
    updated_at: new Date(Date.now() - 14 * 24 * 60 * 60 * 1000).toISOString(),
    author: "陈架构",
  },
  {
    id: "kb-005",
    category: "Runbook",
    title: "ARMS 拓扑断连自愈指引",
    summary: "应用实时监控服务 ARMS 拓扑数据中断时的自愈检查清单。",
    content: "当 ARMS 拓扑页面显示服务断连或数据中断时，按以下顺序检查：\n\n1. 检查 Agent 状态\n   - 登录目标服务器，检查 ARMS Agent 进程是否运行\n   - 查看日志：/var/log/arms-agent/arms-agent.log\n\n2. 网络连通性验证\n   - Agent 到 ARMS 后端端口（通常是 443）连通性\n   - 检查防火墙/安全组规则是否变更\n\n3. 认证配置\n   - LicenseKey 是否过期（控制台→应用管理→凭证管理）\n   - 确认 AccessKey 未被停用\n\n4. 资源限制\n   - Agent 进程 CPU/内存使用是否异常\n   - 检查容器 cgroup 限制（如果 Agent 运行在容器内）\n\n5. 常见自愈操作\n   - 重启 Agent：systemctl restart arms-agent\n   - 清理缓存：rm -rf /var/log/arms-agent/cache/*\n   - 升级 Agent 版本（控制台一键升级）\n\n若以上步骤仍无法恢复，联系阿里云技术支持并提供 Agent 日志。",
    tags: ["ARMS", "监控", "自愈"],
    updated_at: new Date(Date.now() - 1 * 24 * 60 * 60 * 1000).toISOString(),
    author: "刘监控",
  },
  {
    id: "kb-006",
    category: "FAQ",
    title: "Prometheus PromQL 常用查询",
    summary: "运维高频使用的 Prometheus 查询语句速查手册。",
    content: "以下查询可直接用于 Grafana 面板或告警规则：\n\nCPU 使用率（按 Pod）：\n```\nrate(container_cpu_usage_seconds_total{pod=\"order-service-xxx\"}[5m]) * 100\n```\n\n内存使用率（按节点）：\n```\n(1 - node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes) * 100\n```\n\nP99 延迟（按服务）：\n```\nhistogram_quantile(0.99, sum(rate(http_request_duration_seconds_bucket{service=\"order-service\"}[5m])) by (le))\n```\n\nQPS（按接口）：\n```\nsum(rate(http_requests_total{endpoint=\"/api/orders\"}[1m]))\n```\n\n错误率（按状态码）：\n```\nsum(rate(http_requests_total{status=~\"5..\"}[5m])) / sum(rate(http_requests_total[5m])) * 100\n```\n\n磁盘使用率：\n```\n(1 - node_filesystem_avail_bytes / node_filesystem_size_bytes) * 100\n```\n\n提示：使用 Prometheus 控台自动补全功能，输入 metric 名后按 Tab。",
    tags: ["Prometheus", "PromQL", "监控"],
    updated_at: new Date(Date.now() - 3 * 24 * 60 * 60 * 1000).toISOString(),
    author: "赵SRE",
  },
  {
    id: "kb-007",
    category: "案例",
    title: "支付网关全链路不可用复盘",
    summary: "支付网关因第三方渠道证书过期导致的全链路故障复盘与改进措施。",
    content: "故障时间：2025-03-15 14:20 ~ 15:05（持续 45 分钟）\n影响范围：所有支付请求失败，用户无法完成订单支付\n\n故障经过：\n14:20 支付网关告警触发，错误率 100%\n14:22 排查发现第三方支付渠道 TLS 证书已过期\n14:25 尝试热加载证书失败（缓存未刷新）\n14:30 紧急重启网关服务，部分恢复\n14:45 证书全部更新完成，服务恢复\n15:05 全链路验证通过，解除故障状态\n\n根因分析：\n1. 第三方渠道证书过期未收到提前通知\n2. 网关代码缓存了 TLS 连接，未实现证书自动重载\n3. 监控告警未覆盖证书过期检查\n\n改进措施：\n1. 增加证书有效期监控（提前 30 天告警）\n2. 支持证书热加载，无需重启服务\n3. 与第三方渠道建立变更通知机制\n4. 定期演练证书过期应急预案\n\n经验教训：外部依赖的任何配置都可能成为故障点，需要主动监控而非被动响应。",
    tags: ["支付", "故障复盘", "TLS"],
    updated_at: new Date(Date.now() - 7 * 24 * 60 * 60 * 1000).toISOString(),
    author: "孙架构",
  },
]

const CATEGORY_VARIANT: Record<
  KnowledgeEntry["category"],
  "default" | "warning" | "secondary"
> = {
  Runbook: "default",
  案例: "warning",
  FAQ: "secondary",
}

export default function KnowledgePage() {
  const [searchQuery, setSearchQuery] = useState("")
  const [selectedCategory, setSelectedCategory] = useState<Category>("全部")
  const [selectedEntry, setSelectedEntry] = useState<KnowledgeEntry | null>(null)

  const stats = useMemo(() => {
    const total = MOCK_ENTRIES.length
    const runbook = MOCK_ENTRIES.filter((e) => e.category === "Runbook").length
    const cases = MOCK_ENTRIES.filter((e) => e.category === "案例").length
    const faq = MOCK_ENTRIES.filter((e) => e.category === "FAQ").length
    return { total, runbook, cases, faq }
  }, [])

  const filteredEntries = useMemo(() => {
    return MOCK_ENTRIES.filter((entry) => {
      if (selectedCategory !== "全部" && entry.category !== selectedCategory) {
        return false
      }

      if (searchQuery) {
        const q = searchQuery.toLowerCase()
        return (
          entry.title.toLowerCase().includes(q) ||
          entry.summary.toLowerCase().includes(q) ||
          entry.tags.some((t) => t.toLowerCase().includes(q))
        )
      }

      return true
    })
  }, [searchQuery, selectedCategory])

  const categories: Category[] = ["全部", "Runbook", "案例", "FAQ"]

  return (
    <div className="space-y-8">
      <div className="space-y-2">
        <h1 className="text-3xl font-bold tracking-tight">知识库</h1>
        <p className="text-muted-foreground">运维知识、处置案例与常见问题</p>
      </div>

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard label="总计" value={stats.total} />
        <StatCard label="Runbook" value={stats.runbook} />
        <StatCard label="案例" value={stats.cases} />
        <StatCard label="FAQ" value={stats.faq} />
      </div>

      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex flex-wrap gap-2">
          {categories.map((cat) => (
            <button
              key={cat}
              onClick={() => setSelectedCategory(cat)}
              className={cn(
                "rounded-full px-4 py-1.5 text-sm font-medium transition-colors",
                selectedCategory === cat
                  ? "bg-primary text-primary-foreground"
                  : "text-muted-foreground hover:text-foreground"
              )}
            >
              {cat}
            </button>
          ))}
        </div>

        <div className="relative">
          <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
          <input
            type="text"
            placeholder="搜索标题、摘要或标签..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="w-full rounded-md border bg-background pl-10 pr-3 py-2 text-sm outline-none focus:border-primary focus:ring-1 focus:ring-primary sm:w-80"
          />
        </div>
      </div>

      {filteredEntries.length === 0 ? (
        <div className="rounded-lg border border-dashed p-12 text-center text-muted-foreground">
          没有找到匹配的知识条目
        </div>
      ) : (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {filteredEntries.map((entry) => (
            <button
              key={entry.id}
              onClick={() => setSelectedEntry(entry)}
              className="rounded-lg border bg-card p-5 text-left transition-shadow hover:shadow-md"
            >
              <div className="mb-3 flex items-center gap-2">
                <Badge variant={CATEGORY_VARIANT[entry.category]}>
                  {entry.category}
                </Badge>
                <span className="font-mono text-xs text-muted-foreground">
                  {entry.id}
                </span>
              </div>
              <h3 className="mb-2 font-medium leading-snug">{entry.title}</h3>
              <p className="mb-4 line-clamp-2 text-sm text-muted-foreground">
                {entry.summary}
              </p>
              <div className="mb-3 flex flex-wrap gap-1.5">
                {entry.tags.map((tag) => (
                  <Badge key={tag} variant="outline" className="text-xs">
                    {tag}
                  </Badge>
                ))}
              </div>
              <div className="flex items-center justify-between text-xs text-muted-foreground">
                <span>{timeAgo(entry.updated_at)}</span>
                <span>{entry.author}</span>
              </div>
            </button>
          ))}
        </div>
      )}

      {selectedEntry && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4"
          onClick={() => setSelectedEntry(null)}
        >
          <Card
            className="w-full max-w-2xl max-h-[80vh] overflow-y-auto"
            onClick={(e: React.MouseEvent) => e.stopPropagation()}
          >
            <CardHeader>
              <div className="flex items-center gap-2">
                <Badge variant={CATEGORY_VARIANT[selectedEntry.category]}>
                  {selectedEntry.category}
                </Badge>
                <span className="font-mono text-xs text-muted-foreground">
                  {selectedEntry.id}
                </span>
              </div>
              <CardTitle className="text-xl">{selectedEntry.title}</CardTitle>
              <CardDescription className="flex items-center gap-3">
                <span>作者: {selectedEntry.author}</span>
                <span>更新于: {timeAgo(selectedEntry.updated_at)}</span>
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div>
                <h4 className="mb-2 text-sm font-medium">摘要</h4>
                <p className="text-sm text-muted-foreground">
                  {selectedEntry.summary}
                </p>
              </div>
              <div>
                <h4 className="mb-2 text-sm font-medium">内容</h4>
                <div className="whitespace-pre-wrap text-sm leading-relaxed">
                  {selectedEntry.content}
                </div>
              </div>
              <div className="flex flex-wrap gap-2">
                {selectedEntry.tags.map((tag) => (
                  <Badge key={tag} variant="outline">
                    {tag}
                  </Badge>
                ))}
              </div>
              <div className="pt-4">
                <button
                  onClick={() => setSelectedEntry(null)}
                  className="rounded-md bg-secondary px-5 py-2 text-sm font-medium transition-colors hover:bg-secondary/80"
                >
                  关闭
                </button>
              </div>
            </CardContent>
          </Card>
        </div>
      )}
    </div>
  )
}

function StatCard({ label, value }: { label: string; value: number }) {
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-2xl">{value}</CardTitle>
        <CardDescription>{label}</CardDescription>
      </CardHeader>
    </Card>
  )
}
