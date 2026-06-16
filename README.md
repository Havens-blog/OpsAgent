<div align="center">

<p align="center">
  <img src="docs/logo.svg" alt="OpsAgent" width="240" />
</p>

<h1>OpsAgent: Multi-Cloud Operations Intelligence</h1>

<p>基于 Claude Agent SDK 的多云运维 Multi-Agent 智能体系统</p>

<p align="center">
  <a href="https://github.com/Havens-blog/OpsAgent/actions/workflows/ci.yml">
    <img src="https://img.shields.io/github/actions/workflow_status/Havens-blog/OpsAgent/ci.yml?branch=main&style=for-the-badge" alt="CI Status">
  </a>
  <img src="https://img.shields.io/badge/Go-1.24+-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go 1.24">
  <img src="https://img.shields.io/badge/License-Apache%202.0-blue.svg?style=for-the-badge" alt="Apache 2.0">
</p>

<p align="center">
  <strong>
    <a href="#快速开始">Quick Start</a> ·
    <a href="#核心功能">Features</a> ·
    <a href="#架构设计">Architecture</a> ·
    <a href="#开发指南">Development</a>
  </strong>
</p>

</div>

---

## Overview

OpsAgent 是一个面向多云环境的运维智能体系统，旨在通过 AI 自动化故障根因分析（RCA）、告警响应和变更关联分析，帮助 SRE 团队快速定位和解决生产问题。

### 为什么需要 OpsAgent？

当生产环境出现故障时，证据分散在：
- 日志服务（SLS、ELK）
- 监控指标（Prometheus、CloudWatch）
- 链路追踪（ARMS、Jaeger）
- 变更记录（CI/CD、K8s Events）
- 运维知识库（Runbook、Wiki）

传统工具要求人工跨平台查询、关联和分析，**MTTR（平均恢复时间）往往超过 1 小时**。

OpsAgent 通过以下方式缩短 MTTR：

1. **告警驱动** - 自动响应 Prometheus Alertmanager、钉钉、企业微信告警
2. **多源关联** - 时序对齐日志、指标、Trace、拓扑、变更事件
3. **智能推理** - 基于 LLM 的假设生成与置信度评分
4. **安全防护** - 执行分层、审批机制、副作用控制

---

## 核心功能

### 🔍 根因分析引擎 (RCA)

| 模块 | 功能 |
|------|------|
| **假设生成** | 基于异常数据生成可能的根因假设 |
| **关联分析** | 跨日志、指标、Trace 做时序对齐与趋势相关性分析 |
| **拓扑感知** | 基于服务依赖图进行向上/下游传播分析 |
| **变更关联** | 自动关联 CI/CD 部署、配置变更、证书过期等变更事件 |
| **置信度评分** | 多维度加权（时间窗口 50%、拓扑 30%、周期性 10%、人工 Hint 10%） |

### 📊 数据源集成

| 类型 | 支持平台 |
|------|---------|
| **日志** | 阿里云 SLS、Elasticsearch |
| **指标** | Prometheus、阿里云 CloudWatch |
| **链路** | 阿里云 ARMS、Jaeger |
| **变更** | Kubernetes Events、GitLab CI |
| **拓扑** | 阿里云 ARMS Service Graph |

### 🛡️ 安全防护

- **执行分层** - ReadOnly、ReadValidate、WriteExecute 三级权限
- **审批机制** - 高风险操作需要人工确认
- **副作用控制** - 白名单限制可执行命令

### 📡 告警接入

- Prometheus Alertmanager Webhook
- 钉钉机器人回调
- 企业微信应用推送
- 通用 HTTP Webhook

---

## 快速开始

### 前置要求

- Go 1.24+
- Docker（可选，用于本地测试）

### 安装

```bash
# 克隆仓库
git clone https://github.com/Havens-blog/OpsAgent.git
cd OpsAgent

# 安装依赖
make install

# 配置数据源
cp config/datasources.yaml.example config/datasources.yaml
# 编辑 config/datasources.yaml 填入你的凭证

# 运行
make run
```

### Docker 运行

```bash
docker-compose up -d
```

---

## 架构设计

```
┌─────────────────────────────────────────────────────────────┐
│                        OpsAgent                             │
├─────────────────────────────────────────────────────────────┤
│                                                               │
│  ┌───────────────┐    ┌─────────────────────────────────┐  │
│  │ Alert Gateway │───▶│    Coordinator Agent             │  │
│  │  (Webhook)    │    │  - 任务调度                       │  │
│  └───────────────┘    │  - 证据汇总                       │  │
│                       │  - 结论输出                       │  │
│  ┌───────────────┐    └─────────────────────────────────┘  │
│  │ CLI / Web     │                    │                     │
│  │  Console      │                    ▼                     │
│  └───────────────┘    ┌─────────────────────────────────┐  │
│                       │         RCA Engine               │  │
│  ┌───────────────┐    │  - Hypothesis Generator          │  │
│  │  LLM          │    │  - Correlation Analyzer            │  │
│  │  Provider     │    │  - Timeline Analyzer              │  │
│  │  (Claude)     │    │  - Confidence Scorer              │  │
│  └───────────────┘    └─────────────────────────────────┘  │
│                               │                             │
│                               ▼                             │
│  ┌──────────────────────────────────────────────────────┐  │
│  │              Data Sources Layer                       │  │
│  │  ┌──────┐ ┌──────┐ ┌──────┐ ┌──────┐ ┌──────┐      │  │
│  │  │ Logs │ │Metrics│ │Trace │ │Topology│ │Change│      │  │
│  │  └──────┘ └──────┘ └──────┘ └──────┘ └──────┘      │  │
│  └──────────────────────────────────────────────────────┘  │
│                                                               │
│  ┌───────────────────────────────────────────────────────┐  │
│  │            Guardrails & Observability                 │  │
│  │  - Execution Tier  - Approval  - Audit Log            │  │
│  └───────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
```

### Multi-Agent 协作

| Agent | 职责 |
|-------|------|
| **Coordinator** | 总控，任务分发，结论汇总 |
| **LogAnalyst** | 日志模式识别，异常根因分析 |
| **Monitor** | 指标异常检测，时序关联分析 |
| **Inspector** | 变更事件关联，拓扑传播分析 |

---

## 开发指南

### 运行测试

```bash
# 单元测试
make test

# 覆盖率报告
make test-coverage

# 集成测试
make test-integration

# 合成测试（RCA 准确率）
make test-synthetic
```

### 代码规范

```bash
# 格式化
make fmt

# Lint
make lint

# 类型检查
make typecheck
```

### 添加新数据源

1. 在 `internal/datasource/` 下实现接口
2. 在 `config/datasources.yaml` 添加配置模板
3. 在 `internal/rca/sources/` 注册为 RCA 数据源
4. 编写测试

---

## 路线图

### Phase 1 — 核心 RCA 引擎 ✅
- [x] 数据源抽象层
- [x] 假设生成与评分
- [x] 单元测试框架

### Phase 2 — 告警驱动工作流
- [ ] Alertmanager Webhook 接入
- [ ] Incident Lifecycle 模型
- [ ] Seed Call 自动证据获取

### Phase 3 — 变更关联增强
- [ ] CI/CD 部署记录集成
- [ ] 配置变更日志采集
- [ ] 变更-故障时间窗口分析

### Phase 4 — 拓扑感知 RCA
- [ ] ARMS Service Graph 集成
- [ ] 上下游传播分析
- [ ] 拓扑路径评分

### Phase 5 — Multi-Agent 协作
- [ ] Agent 通信协议
- [ ] 证据共享机制
- [ ] 冲突裁决策略

### Phase 6 — 生产就绪
- [ ] Web 控制台
- [ ] REST API
- [ ] 钉钉/企业微信集成
- [ ] Docker/K8s 部署

---

## 参考

本项目参考了 [OpenSRE](https://github.com/Tracer-Cloud/opensre) 的设计思想，特别是：
- 合成测试框架
- RCA 评估方法
- 工具注册中心模式

---

## 许可证

Apache License 2.0

---

## 贡献

欢迎贡献！请查看 [CONTRIBUTING.md](CONTRIBUTING.md) 了解详情。

---

<div align="center">

**[⬆ 回到顶部](#opsagent-multi-cloud-operations-intelligence)**

Made with ❤️ by [Havens-blog](https://github.com/Havens-blog)

</div>
