# OpsAgent Multi-Agent 系统实施计划

## Context

本计划旨在构建一个基于 Claude Agent SDK (Go版本) 的多云运维 multi-agent 智能体系统。

**核心需求：**
- 支持多云环境的日志采集（阿里云 SLS、火山云 TLS、华为云 LTS）
- APM 数据接入（阿里云 ARMS）
- 多云时序数据查询（各云时序库 + IDC 自建）
- 运维操作能力（云资源巡检、ES 巡检等）
- **核心功能优先：RCA 根因分析**

**确认的技术决策：**
- 开发语言：Go
- SDK：Claude Agent SDK (Go 版本)
- 云优先级：阿里云优先（SLS + ARMS），其他云厂商后续
- IM 平台：钉钉优先
- 部署环境：云端部署
- 交互方式：Web 控制台、IM 机器人、REST API、CLI 全支持

**安全和工程化借鉴（OpenSRE）：**
- Guardrails 安全护栏
- Sandbox 沙箱执行
- Synthetic RCA 测试
- 工具注册机制
- 审计日志

---

## 推荐架构方案

### 目录结构

```
f:\OpsAgent\
├── cmd\                         # 应用入口
│   ├── agent\                   # 主 Agent 服务
│   ├── api\                     # REST API 服务
│   ├── bot\                     # IM 机器人服务
│   └── cli\                     # 命令行工具
│
├── internal\                    # 私有代码
│   ├── agent\                   # Agent 实现
│   │   ├── base.go              # BaseAgent 基础封装
│   │   ├── coordinator.go       # 协调/规划 Agent
│   │   ├── log_analyst.go       # 日志分析 Agent
│   │   ├── monitor.go           # 监控与 APM Agent
│   │   └── inspector.go         # 运维巡检 Agent
│   │
│   ├── tool\                    # Tool 实现
│   │   ├── registry.go          # Tool 注册中心
│   │   ├── metadata.go          # Tool 元数据
│   │   ├── base.go              # Tool 基类
│   │   ├── common\              # 通用工具
│   │   ├── log\                 # 日志工具
│   │   ├── monitoring\          # 监控工具
│   │   ├── inspection\          # 巡检工具
│   │   ├── operations\          # 运维操作工具
│   │   └── rca\                 # RCA 工具
│   │
│   ├── rca\                      # RCA 引擎 ⭐ NEW (Phase 2 完成)
│   │   ├── engine\              # RCA 核心引擎
│   │   │   └── root_cause_engine.go  # 多维度评分引擎
│   │   ├── models\              # RCA 数据模型
│   │   │   ├── incident.go     # 事件、信号、拓扑、变更模型
│   │   │   └── hypothesis.go   # 假设、评分、报告模型
│   │   ├── scoring\             # 多维度评分
│   │   │   ├── weights.go      # OpenSRE 风格权重配置
│   │   │   └── scorer.go       # MultiDimensionalScorer
│   │   ├── sources\             # RCA 数据源接口
│   │   │   ├── log_source.go   # 日志数据源接口
│   │   │   ├── metric_source.go # 指标数据源接口 (Prometheus)
│   │   │   ├── trace_source.go # Trace 数据源接口 (ARMS)
│   │   │   ├── topology_source.go # 拓扑数据源接口 (ARMS)
│   │   │   └── change_source.go # 变更数据源接口 (K8s)
│   │   └── integrations\        # 数据源集成实现
│   │       ├── prometheus_metrics.go # Prometheus 集成
│   │       ├── arms_topology.go # ARMS 拓扑集成
│   │       └── k8s_change.go    # K8s 变更检测
│   │
│   ├── datasource\              # 数据源层
│   │   ├── core\                # 核心抽象
│   │   │   ├── datasource.go    # DataSource 接口
│   │   │   ├── config.go        # 配置结构
│   │   │   ├── errors.go        # 错误类型
│   │   │   └── retry.go         # 重试逻辑
│   │   ├── registry\            # 数据源注册
│   │   ├── credentials\          # 凭证管理
│   │   ├── log\                 # 日志数据源
│   │   │   ├── base.go
│   │   │   ├── aliyun_sls.go    # 阿里云 SLS (P0)
│   │   │   ├── volcengine_tls.go # 火山云 TLS (P1)
│   │   │   └── huawei_lts.go    # 华为云 LTS (P1)
│   │   ├── metrics\             # 时序数据源
│   │   │   ├── base.go
│   │   │   ├── prometheus.go    # Prometheus (P1)
│   │   │   ├── influxdb.go      # InfluxDB (P1)
│   │   │   └── cloud_tsdb.go    # 云厂商时序 (P1)
│   │   ├── apm\                 # APM 数据源
│   │   │   ├── base.go
│   │   │   └── aliyun_arms.go   # 阿里云 ARMS (P0)
│   │   └── mock\                # Mock 实现（测试）
│   │
│   ├── guardrails\              # 安全护栏模块 ⭐ NEW
│   │   ├── execution_tier.go    # 执行层级定义
│   │   ├── approval.go          # 审批机制
│   │   ├── side_effect.go       # 副作用分级
│   │   └── capability.go        # 功能开关
│   │
│   ├── sandbox\                 # 沙箱执行模块 ⭐ NEW
│   │   ├── executor.go          # 沙箱执行器
│   │   ├── config.go            # 沙箱配置
│   │   └── isolation.go         # 隔离实现
│   │
│   ├── core\                    # 核心组件
│   │   ├── conversation.go      # ConversationManager
│   │   ├── planner.go           # Planner 组件
│   │   └── sdk_adapter.go       # SDK 适配层
│   │
│   ├── config\                  # 配置管理
│   │   └── settings.go          # 配置结构
│   │
│   ├── observability\           # 可观测性
│   │   ├── logger.go            # 结构化日志
│   │   ├── metrics.go           # 指标收集
│   │   ├── tracer.go            # 分布式追踪
│   │   └── audit.go             # 审计日志 ⭐ NEW
│   │
│   └── models\                  # 数据模型
│       ├── message.go           # 消息类型
│       ├── task.go              # 任务模型
│       └── result.go            # 结果模型
│
├── pkg\                         # 公共库
│   └── sdk\                     # Claude Agent SDK 封装
│
├── api\                         # API 定义
│   ├── openapi\                 # OpenAPI 规范
│   └── proto\                   # Protobuf 定义
│
├── web\                         # Web 控制台
│   └── frontend\               # 前端代码
│
├── config\                      # 配置文件
│   ├── settings.yaml            # 环境配置
│   ├── agents.yaml              # Agent 定义
│   └── datasources.yaml         # 数据源配置
│
├── deployments\                 # 部署配置
│   ├── docker\
│   │   └── Dockerfile
│   └── kubernetes\
│       └── manifests\
│
├── tests\                       # 测试
│   ├── unit\                    # 单元测试
│   ├── integration\             # 集成测试
│   ├── synthetic\               # 合成测试场景 ⭐ NEW
│   │   └── rca_scenarios.go
│   └── e2e\                     # 端到端测试
│
├── go.mod
├── go.sum
├── Makefile
├── README.md
└── CLAUDE.md
```

---

## 安全护栏设计 ⭐ (借鉴 OpenSRE)

### 1. 执行层级分级

```go
// internal/guardrails/execution_tier.go
package guardrails

type ExecutionTier string

const (
    TierExempt   ExecutionTier = "exempt"   // 元命令，无需确认（如 /help, /exit）
    TierSafe     ExecutionTier = "safe"     // 只读操作，始终允许（如查询日志、指标）
    TierElevated ExecutionTier = "elevated" // 变更性操作，需要确认（如重启服务、修改配置）
)
```

### 2. 审批机制

```go
// internal/guardrails/approval.go
package guardrails

type ApprovalScope string

const (
    ScopeOneShot ApprovalScope = "one_shot" // 单次调用有效
    ScopeSession ApprovalScope = "session"  // 会话期间有效
)

type ApprovalRequest struct {
    ToolName       string
    Operation      string
    Params         map[string]interface{}
    Reason         string
    ExpirySeconds  int           // 审批过期时间（默认 5 分钟）
    Scope          ApprovalScope
}

// ApprovalManager 管理审批状态
type ApprovalManager interface {
    RequireApproval(req ApprovalRequest) error
    GrantApproval(toolName string, scope ApprovalScope) error
    IsValid(toolName string, scope ApprovalScope) bool
}
```

### 3. 参数隔离

敏感参数（凭证、端点）不暴露给 LLM，运行时注入：

```go
// internal/tool/metadata.go
package tool

type ToolMetadata struct {
    Name           string
    Description    string
    InputSchema    json.RawMessage
    InjectedParams []string // 运行时注入的参数名（不暴露给 LLM）
    // ...
}

// PublicInputSchema 移除 injected_params，只暴露安全的参数给 LLM
func (m *ToolMetadata) PublicInputSchema() map[string]interface{} {
    // 移除敏感参数的逻辑
}
```

### 4. 副作用分级

```go
// internal/guardrails/side_effect.go
package guardrails

type SideEffectLevel string

const (
    SideEffectNone     SideEffectLevel = "none"      // 无副作用
    SideEffectReadOnly SideEffectLevel = "read_only" // 只读
    SideEffectMutating SideEffectLevel = "mutating"  // 修改状态
    SideEffectExternal SideEffectLevel = "external"  // 外部调用
)
```

### 5. 功能开关

```go
// internal/guardrails/capability.go
package guardrails

// CapabilityNotDisabled 检查功能是否被显式禁用
func CapabilityNotDisabled(session *Session, capabilityName string) bool {
    availableCapabilities := session.AvailableCapabilities
    capabilityValues := availableCapabilities[capabilityName]
    return !isExplicitlyDisabled(capabilityValues)
}

// 当 session 的某个 capability 被显式设为空时，对应工具不可用
```

---

## 沙箱执行设计 ⭐ (借鉴 OpenSRE)

### Go 沙箱实现

```go
// internal/sandbox/executor.go
package sandbox

import (
    "context"
    "os/exec"
    "time"
)

type SandboxConfig struct {
    Timeout       time.Duration // 默认 30s，最大 60s
    AllowNetwork  bool          // 默认 false
    AllowWrite    []string      // 允许写入的路径
    MaxMemoryMB   int           // 内存限制
}

type SandboxResult struct {
    ExitCode   int
    TimedOut   bool
    Output     string
    Error      string
}

type SandboxExecutor interface {
    Execute(ctx context.Context, cmd *exec.Cmd) (*SandboxResult, error)
}

// 实现隔离机制：
// - 超时控制：通过 context.WithTimeout
// - 资源限制：通过 cgroup 或 ulimit
// - 网络隔离：通过 network namespace
// - 文件系统隔离：通过 chroot 或 mount namespace
```

---

## RCA 引擎设计 ⭐ (Phase 2 完成)

### 核心算法：多维度评分 (OpenSRE 风格)

基于真实运维流程 "告警响 → 面板确认 → 错误日志 → 指标趋势 → Trace → 变更 → 可疑服务 → 确认根因" 设计的多维度评分引擎：

| 评分维度 | 权重 | 算法说明 |
|---------|------|----------|
| **Time Correlation** | 50% | Trend-based 时间对齐 - 将时间窗口分为 5 个子窗口，对比 incident 窗口与 baseline 的趋势方向（up/down/stable），而非原始值 |
| **Topology Score** | 25% | ARMS 服务拓扑图 - 直接受影响服务=1.0，直接依赖=0.9，二级依赖=0.5 |
| **Change Proximity** | 15% | K8s 部署变更检测 - 5分钟内变更=1.0，15分钟内=0.7，30分钟内=0.4 |
| **Metric Severity** | 5% | 指标异常一致性 - 3+ 窗口持续异常=1.0，2个窗口=0.7，1个窗口=0.4 |
| **Error Frequency** | 3% | 错误率比值 - incident 窗口错误率 / baseline 错误率 |
| **Trace Evidence** | 2% | Trace 错误率 - 服务错误 trace 占比 |

### 趋势对齐算法 (Trend-Based Correlation)

```go
// 将时间窗口分为 5 个子窗口
windows := generateTimeWindows(incident, windowSize)
subWindowSize := windowSize / 5

// 计算每个子窗口的平均值
avg1 := getAverageMetricValue(metric, windows[i])
avg2 := getAverageMetricValue(metric, windows[i+1])

// 计算相对变化率，而非绝对值
change := (avg2 - avg1) / avg1

// 分类趋势：>10% 增长=up, <-10% 下降=down, 其他=stable
switch {
case change > 0.1: trend = TrendUp
case change < -0.1: trend = TrendDown
default: trend = TrendStable
}
```

**为什么用趋势而非绝对值？**
- 不同服务的指标量级差异巨大（QPS 从 10 到 10000+）
- 趋势方向（异常增长/下降）在跨服务关联时更可靠
- 符合 OpenSRE 的最佳实践

### 置信度量化

```go
// Sigmoid 归一化：将任意分数映射到 0-1
confidence = 1 / (1 + exp(-5 * (score - 0.5)))

// 置信度阈值
minConfidence = 0.3    // 最低报告阈值
highConfidence = 0.7   // 高置信度阈值
```

### 数据源集成

| 数据源 | 集成方式 | 接口 | 用途 |
|-------|---------|------|------|
| **Prometheus** | `internal/rca/integrations/prometheus_metrics.go` | MetricSource | 告警指标、异常检测 |
| **ARMS Topology** | `internal/rca/integrations/arms_topology.go` | TopologySource | 服务依赖图、邻居查询 |
| **K8s API** | `internal/rca/integrations/k8s_change.go` | ChangeSource | 部署变更检测 (镜像版本) |
| **SLS** | `internal/datasource/log/` | LogSource | 错误日志查询 (复用) |
| **ARMS Trace** | `internal/datasource/apm/` | TraceSource | 分布式追踪 (复用) |

### 分析流程

```
1. gatherSignals() - 并行收集数据
   ├── 错误日志 (SLS)
   ├── 异常指标 (Prometheus)
   ├── 错误 Trace (ARMS)
   ├── 服务拓扑 (ARMS)
   └── 最近变更 (K8s)

2. normalizeTimeWindows() - 趋势对齐
   ├── 生成 5 个时间子窗口
   ├── 分桶统计 (日志/Trace)
   └── 计算趋势方向 (指标)

3. generateHypotheses() - 生成候选假设
   ├── Error Spike 假设 (错误日志突增)
   ├── Metric Anomaly 假设 (指标异常趋势)
   ├── Change 假设 (最近部署变更)
   └── Dependency 假设 (上游依赖故障)

4. scoreCandidates() - 多维度评分
   └── 每个假设计算 6 个维度的分数

5. rankAndFilter() - 排序过滤
   ├── 按总分降序排列
   └── 过滤低置信度候选

6. RCAReport - 输出报告
   ├── TopCandidate (最高分假设)
   ├── Confidence (整体置信度)
   ├── Breakdown (各维度得分)
   └── Evidence (支持证据)
```

### 已实现文件

```
internal/rca/
├── models/
│   ├── incident.go          # Incident, Signals, LogEntry, MetricSeries,
│                           # TraceSummary, Topology, Change, Trend
│   └── hypothesis.go        # Hypothesis, ScoredHypothesis, RCAReport
├── scoring/
│   ├── weights.go           # DefaultWeights: 50/25/15/5/3/2
│   └── scorer.go           # MultiDimensionalScorer 实现
├── sources/
│   ├── log_source.go        # LogSource 接口
│   ├── metric_source.go     # MetricSource 接口 (含 Alert)
│   ├── trace_source.go      # TraceSource 接口
│   ├── topology_source.go   # TopologySource 接口
│   └── change_source.go     # ChangeSource 接口
├── engine/
│   └── root_cause_engine.go # Engine + Analyze() 方法
└── integrations/
    ├── prometheus_metrics.go  # Prometheus HTTP API 集成
    ├── arms_topology.go       # ARMS 拓扑 API 集成
    └── k8s_change.go          # K8s Deployment API 集成
```

### 使用示例

```go
// 创建 RCA 引擎
engine := rca.NewEngine(
    rca.WithLogSource(slsSource),
    rca.WithMetricSource(prometheusSource),
    rca.WithTraceSource(armsSource),
    rca.WithTopologySource(armsTopology),
    rca.WithChangeSource(k8sSource),
    rca.WithWindowSize(15 * time.Minute),
    rca.WithMinConfidence(0.3),
)

// 分析事件
incident := &models.Incident{
    ID:              "incident-20240615-001",
    StartTime:        time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC),
    EndTime:         time.Date(2024, 6, 15, 11, 0, 0, 0, time.UTC),
    AffectedServices: []string{"order-service", "payment-service"},
    Severity:         "P0",
}

report, err := engine.Analyze(ctx, incident)
if err != nil {
    log.Fatal(err)
}

// 输出报告
fmt.Println(report.GetSummary())
fmt.Printf("Confidence: %.0f%%\n", report.Confidence * 100)
```

### 设计决策记录

1. **为什么用 5 个时间子窗口？**
   - 提供足够粒度用于趋势分析 (至少 4 个变化点)
   - 平衡计算开销与准确性
   - 符合业界通用做法

2. **为什么变更检测走 K8s API 而非 CI/CD？**
   - 用户反馈：变更数据源在内网不好获取
   - K8s Deployment 镜像版本变化可间接反映变更
   - 无需集成内网 CI/CD 系统

3. **为什么拓扑分只有 25%？**
   - 多云环境下拓扑可能不完整 (CDN/WAF/LB 日志在前置链路)
   - 上游关系可能不知道，需要动态发现
   - 时间相关性更可靠 (50%)

---

## 核心模块设计

### 1. Agent 层

#### BaseAgent 基础封装
- 文件：`internal/agent/base.go`
- 职责：封装 Claude Agent SDK，提供统一的 Agent 接口
- 核心：
  - `Initialize()` - Agent 初始化
  - `Execute()` - 执行任务
  - `RegisterTools()` - 注册工具
  - `GetSessionID()` - 获取会话 ID

#### CoordinatorAgent (协调/规划 Agent)
- 文件：`internal/agent/coordinator.go`
- 职责：任务分解、Agent 调度、结果汇总

#### LogAnalystAgent (日志分析 Agent)
- 文件：`internal/agent/log_analyst.go`
- 职责：多云日志查询、异常检测、模式分析

#### MonitorAgent (监控与 APM Agent)
- 文件：`internal/agent/monitor.go`
- 职责：APM 数据整合、指标异常检测、RCA 分析

#### InspectorAgent (运维巡检 Agent)
- 文件：`internal/agent/inspector.go`
- 职责：云资源巡检、ES 巡检、配置合规检查

### 2. Tool 层（增强安全护栏）

#### Tool 注册表（带验证）
- 文件：`internal/tool/registry.go`
- 职责：工具注册、验证、发现

#### Tool 元数据
- 文件：`internal/tool/metadata.go`
- 职责：工具元数据定义、参数隔离

#### RCA 工具（核心优先）
- 文件：`internal/tool/rca/`
- 工具：
  - `timeline_analyzer.go` - 时间线分析
  - `correlation_analyzer.go` - 关联分析
  - `root_cause_engine.go` - 根因定位引擎

### 3. Datasource 层

#### 核心抽象
- 文件：`internal/datasource/core/datasource.go`
- 接口：`DataSource`

#### 阿里云 SLS（P0 优先）
- 文件：`internal/datasource/log/aliyun_sls.go`

#### 阿里云 ARMS（P0 优先）
- 文件：`internal/datasource/apm/aliyun_arms.go`

### 4. 可观测性（增加审计）

#### 审计日志 ⭐ NEW
- 文件：`internal/observability/audit.go`
- 记录：所有 Agent/Tool 执行、审批、参数、结果

---

## 分阶段实施计划

### Phase 1: 基础架构 + 安全护栏（Week 1-2）

**目标：搭建项目框架、核心抽象和安全护栏**

**文件列表：**
1. `go.mod`, `go.sum` - 依赖管理
2. `internal/config/settings.go` - 配置管理
3. `internal/guardrails/execution_tier.go` - 执行层级 ⭐ NEW
4. `internal/guardrails/approval.go` - 审批机制 ⭐ NEW
5. `internal/guardrails/side_effect.go` - 副作用分级 ⭐ NEW
6. `internal/tool/registry.go` - 工具注册表（带验证）⭐ NEW
7. `internal/tool/metadata.go` - 工具元数据（参数隔离）⭐ NEW
8. `internal/agent/base.go` - BaseAgent 封装
9. `internal/core/conversation.go` - ConversationManager
10. `internal/core/planner.go` - Planner
11. `internal/observability/logger.go` - 日志系统
12. `internal/observability/audit.go` - 审计日志 ⭐ NEW
13. `Makefile` - 构建脚本

**验证：**
- 能够初始化 BaseAgent
- 安全护栏能够检查工具权限
- 审计日志能够记录操作

### Phase 2: 数据源层 + RCA 引擎 ⭐ (已完成)

**目标：实现阿里云 SLS、ARMS 数据源 + RCA 多维度评分引擎**

**已完成文件列表：**

**数据源层：**
1. `internal/datasource/core/datasource.go` - DataSource 接口
2. `internal/datasource/core/config.go` - 配置结构
3. `internal/datasource/core/errors.go` - 错误类型
4. `internal/datasource/core/retry.go` - 重试逻辑
5. `internal/datasource/credentials/provider.go` - 凭证接口
6. `internal/datasource/credentials/env_provider.go` - 环境变量实现
7. `internal/datasource/log/base.go` - 日志数据源基类
8. `internal/datasource/log/aliyun_sls.go` - 阿里云 SLS 框架
9. `internal/datasource/apm/base.go` - APM 数据源基类
10. `internal/datasource/apm/aliyun_arms.go` - 阿里云 ARMS 框架
11. `internal/datasource/registry/registry.go` - 数据源注册管理
12. `config/datasources.yaml` - 数据源配置示例

**RCA 引擎层 ⭐ NEW:**
13. `internal/rca/models/incident.go` - Incident, Signals, LogEntry, MetricSeries, TraceSummary, Topology, Change, Trend
14. `internal/rca/models/hypothesis.go` - Hypothesis, ScoredHypothesis, RCAReport
15. `internal/rca/scoring/weights.go` - DefaultWeights: 50/25/15/5/3/2
16. `internal/rca/scoring/scorer.go` - MultiDimensionalScorer 实现
17. `internal/rca/sources/log_source.go` - LogSource 接口
18. `internal/rca/sources/metric_source.go` - MetricSource 接口
19. `internal/rca/sources/trace_source.go` - TraceSource 接口
20. `internal/rca/sources/topology_source.go` - TopologySource 接口
21. `internal/rca/sources/change_source.go` - ChangeSource 接口
22. `internal/rca/engine/root_cause_engine.go` - RCA 核心引擎 + Analyze() 方法
23. `internal/rca/integrations/prometheus_metrics.go` - Prometheus HTTP API 集成
24. `internal/rca/integrations/arms_topology.go` - ARMS 拓扑 API 集成
25. `internal/rca/integrations/k8s_change.go` - K8s Deployment API 集成

**验证：**
- ✅ RCA 包编译通过 (`go vet ./internal/rca/...`)
- ✅ 多维度评分权重配置
- ✅ Trend-based 时间对齐算法
- ✅ 置信度量化 (Sigmoid 归一化)
- ⏳ 数据源连接测试 (需真实环境)

### Phase 3: Agent 实现 + Synthetic 测试 (下一步)

**目标：实现核心 Agent 和 RCA 合成测试**

**文件列表：**
1. `internal/agent/coordinator.go` - 协调 Agent
2. `internal/agent/log_analyst.go` - 日志分析 Agent
3. `internal/agent/monitor.go` - 监控与 APM Agent (集成 RCA)
4. `internal/agent/inspector.go` - 运维巡检 Agent
5. `config/agents.yaml` - Agent 配置
6. `tests/synthetic/rca_scenarios.go` - 合成 RCA 测试 ⭐ NEW
7. `tests/synthetic/scoring.go` - RCA 评分标准 ⭐ NEW

**验证：**
- MonitorAgent 能够执行 RCA
- 合成测试验证根因定位准确性
- Agent 间协作正常

### Phase 4: Agent 实现（Week 7-8）

**目标：实现四个核心 Agent**

**文件列表：**
1. `internal/agent/coordinator.go` - 协调 Agent
2. `internal/agent/log_analyst.go` - 日志分析 Agent
3. `internal/agent/monitor.go` - 监控与 APM Agent
4. `internal/agent/inspector.go` - 运维巡检 Agent
5. `config/agents.yaml` - Agent 配置

**验证：**
- CoordinatorAgent 能够分解任务
- LogAnalystAgent 能够查询和分析日志
- MonitorAgent 能够执行 RCA
- InspectorAgent 能够执行巡检

### Phase 5: API 和交互层（Week 9-10）

**目标：实现 REST API、钉钉机器人、CLI**

**文件列表：**
1. `cmd/api/main.go` - API 服务入口
2. `cmd/bot/main.go` - 机器��服务入口
3. `cmd/cli/main.go` - CLI 工具入口
4. `internal/interfaces/api.go` - API 处理器
5. `internal/interfaces/bot.go` - 钉钉机器人集成
6. `internal/interfaces/cli.go` - CLI 命令

**验证：**
- REST API 可用
- 钉钉机器人可交互
- CLI 可执行任务

### Phase 6: 部署和测试（Week 11-12）

**目标：容器化部署、集成测试**

**文件列表：**
1. `deployments/docker/Dockerfile` - Docker 镜像
2. `deployments/kubernetes/deployment.yaml` - K8s 部署
3. `tests/integration/rca_test.go` - RCA 集成测试
4. `tests/e2e/agent_test.go` - 端到端测试

**验证：**
- 能够在 Docker 中运行
- 集成测试通过
- 端到端场景验证

---

## 关键文件列表（按优先级）

### P0 - 核心基础和安全护栏
1. `internal/config/settings.go` - 配置管理
2. `internal/guardrails/execution_tier.go` - 执行层级 ⭐ NEW
3. `internal/guardrails/approval.go` - 审批机制 ⭐ NEW
4. `internal/guardrails/side_effect.go` - 副作用分级 ⭐ NEW
5. `internal/tool/registry.go` - 工具注册表（带验证）⭐ NEW
6. `internal/tool/metadata.go` - 工具元数据（参数隔离）⭐ NEW
7. `internal/agent/base.go` - Agent 基类
8. `internal/core/conversation.go` - 会话管理
9. `internal/core/planner.go` - 任务规划
10. `internal/observability/logger.go` - 日志系统
11. `internal/observability/audit.go` - 审计日志 ⭐ NEW

### P0 - 数据源（阿里云优先）
12. `internal/datasource/core/datasource.go` - 数据源接口
13. `internal/datasource/core/errors.go` - 错误处理
14. `internal/datasource/log/aliyun_sls.go` - 阿里云 SLS
15. `internal/datasource/apm/aliyun_arms.go` - 阿里云 ARMS

### P0 - RCA 能力 ⭐ (已完成)
16. `internal/rca/models/incident.go` - 事件、信号、拓扑模型 ✅
17. `internal/rca/models/hypothesis.go` - 假设、评分、报告模型 ✅
18. `internal/rca/scoring/weights.go` - OpenSRE 风格权重 ✅
19. `internal/rca/scoring/scorer.go` - 多维度评分实现 ✅
20. `internal/rca/sources/*.go` - RCA 数据源接口 ✅
21. `internal/rca/engine/root_cause_engine.go` - RCA 核心引擎 ✅
22. `internal/rca/integrations/prometheus_metrics.go` - Prometheus 集成 ✅
23. `internal/rca/integrations/arms_topology.go` - ARMS 拓扑集成 ✅
24. `internal/rca/integrations/k8s_change.go` - K8s 变更检测 ✅
25. `internal/agent/monitor.go` - 监控与 APM Agent (待实现)

### P1 - 沙箱执行
26. `internal/sandbox/executor.go` - 沙箱执行器 ⭐ NEW
27. `internal/sandbox/config.go` - 沙箱配置 ⭐ NEW

### P1 - 扩展功能
21. `internal/agent/coordinator.go` - 协调 Agent
22. `internal/agent/log_analyst.go` - 日志分析 Agent
23. `internal/interfaces/bot.go` - 钉钉机器人
24. `cmd/api/main.go` - API 服务

### P1 - 其他云厂商
25. `internal/datasource/log/volcengine_tls.go` - 火山云 TLS
26. `internal/datasource/log/huawei_lts.go` - 华为云 LTS

### P1 - 测试
27. `tests/synthetic/rca_scenarios.go` - 合成 RCA 测试 ⭐ NEW

---

## 验证方案

### 单元测试
- Mock 云 SDK
- 测试 Agent 逻辑
- 测试 RCA 算法
- 测试安全护栏 ⭐ NEW

### 集成测试
- 使用阿里云 sandbox 环境
- 测试数据源连接
- 测试 Agent 协作

### 端到端测试
**RCA 场景：**
```
用户：分析 2024-06-15 10:00-11:00 的服务异常
↓
CoordinatorAgent 分解任务
↓
LogAnalystAgent 查询异常日志
↓
MonitorAgent 查询异常指标
↓
MonitorAgent 执行 RCA
↓
返回根因报告
```

### Synthetic 测试 ⭐ NEW
- 预定义的 RCA 场景
- Mock 各数据源响应
- 验证根因定位准确性

---

## 外部依赖

### Go 依赖
- `github.com/anthropics/claude-agent-sdk-go` - Claude Agent SDK
- `github.com/aliyun/aliyun-log-go-sdk` - 阿里云 SLS
- `github.com/aliyun/alibaba-cloud-sdk-go` - 阿里云 ARMS
- `github.com/go-redis/redis` - Redis（缓存）
- `go.opentelemetry.io/otel` - OpenTelemetry
- `github.com/spf13/cobra` - CLI 框架
- `github.com/gin-gonic/gin` - HTTP 框架

### 基础设施
- Redis（会话缓存、审批状态存储）
- MySQL（配置存储、审计日志）
- Kafka（消息队列，可选）
- Jaeger/Tempo（分布式追踪）

---

## 安全考虑

### 1. 凭证管理
- 不硬编码，支持环境变量和 Vault
- 定期轮换机制
- 参数隔离（不暴露给 LLM）

### 2. 操作确认
- 危险操作需要二次确认
- 审批过期机制
- 审批作用域控制

### 3. 访问控制
- API 认证
- Agent 权限隔离
- 执行层级分级

### 4. 沙箱隔离
- 超时控制
- 网络隔离
- 文件系统隔离
- 资源限制

### 5. 数据安全
- 传输加密（TLS）
- 敏感数据脱敏
- 审计日志记录

### 6. 运行时安全 ⭐ NEW
- Tool 执行前验证
- 参数类型检查
- 副作用级别检查
- 功能开关控制

---

## 参考资料

- [Claude Agent SDK 文档](https://code.anthropic.com/docs/en/agent-sdk/overview)
- [阿里云 SLS SDK](https://help.aliyun.com/zh/sls/developer-reference/overview-of-log-service-sdk)
- [阿里云 ARMS API](https://api.aliyun.com/document/ARMS)
- [OpenTelemetry Go](https://opentelemetry.io/docs/instrumentation/go/)
- [OpenSRE - GitHub](https://github.com/Tracer-Cloud/opensre) ⭐ NEW
- [SafeAgent - GitHub](https://github.com/parthamehta123/safeagent) ⭐ NEW
