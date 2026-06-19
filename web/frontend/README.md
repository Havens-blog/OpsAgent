# OpsAgent Web Console

基于 Next.js 15 (App Router) + React 19 + Tailwind CSS 的运维智能控制台。

## 功能

| 页面 | 路径 | 说明 |
|------|------|------|
| **Dashboard** | `/` | 系统健康、Agent 概览、RCA 引擎能力 |
| **Incidents** | `/incidents` | 事件列表、触发 RCA 分析表单 |
| **Incident Detail** | `/incidents/[id]` | 根因结论、置信度、证据列表、时间线 |
| **Topology** | `/topology` | 服务依赖图（cytoscape），错误边高亮 |
| **Changes** | `/changes` | CI/CD 部���与配置变更的关联分析 |
| **Agents** | `/agents` | Multi-Agent 协作状态、通信协议能力 |

## 快速开始

### 前置要求

- Node.js 18.18+
- 后端 OpsAgent API 服务运行在 `:8080`

### 安装与运行

```bash
cd web/frontend
npm install
npm run dev
```

控制台启动在 `http://localhost:3000`。

开发模式下，`/api/v1/*` 请求会被代理到后端 `http://localhost:8080`（见 `next.config.ts` 的 rewrites）。

### 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `NEXT_PUBLIC_API_URL` | `http://localhost:8080` | 后端 API 地址 |

### 生产构建

```bash
npm run build
npm run start
```

构建采用 `output: 'standalone'`，适合 Docker 部署。

## 技术栈

- **Next.js 15** — App Router, RSC
- **React 19** — `use()` API for params
- **Tailwind CSS** — 暗色主题, shadcn/ui 设计令牌
- **cytoscape** — 服务拓扑图可视化
- **lucide-react** — 图标
- **TypeScript** — strict mode

## 目录结构

```
src/
├── app/
│   ├── layout.tsx           # 根布局 + 侧边栏
│   ├── page.tsx             # Dashboard
│   ├── globals.css          # 主题 CSS 变量
│   ├── incidents/
│   │   ├── page.tsx         # 事件列表 + 触发 RCA
│   │   └── [id]/page.tsx    # 事件详情
│   ├── topology/page.tsx    # 拓扑可视化
│   ├── changes/page.tsx     # 变更关联
│   └── agents/page.tsx      # Agent 监控
├── components/
│   ├── sidebar.tsx          # 导航
│   └── ui/                  # 基础组件 (card, badge)
└── lib/
    ├── api.ts               # API 客户端
    └── utils.ts             # 工具函数
```

## 后端 API 依赖

控制台调用以下后端端点（在 `internal/interfaces/api.go` 注册）：

- `GET /api/v1/health`
- `GET /api/v1/agents`
- `GET /api/v1/topology`
- `POST /api/v1/rca/analyze`
- `POST /api/v1/logs/query`
- `POST /api/v1/metrics/query`
