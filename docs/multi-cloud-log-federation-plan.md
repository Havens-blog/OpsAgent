# 多云日志联邦 —— 实施计划与 AI 提示词

> 面向两个仓库的跨仓计划：先在 **e-cam-service / e-cam-web** 丰富多云统一日志查询，
> 再让 **OpsAgent RCA** 作为消费方接入。两个提示词各自自包含，可直接贴给对应仓库的 AI agent。
>
> 作者：Bamzc ｜ 创建：2026-10-26

---

## 0. 一句话结论

日志能力**在 e-cam 做一处**（多云联邦 + 补"应用日志"类型），**OpsAgent 回头当消费方接一次**。
严禁两边各自去连五朵云——那是重复造轮子。**有先后依赖，不能并行硬接**。

---

## 1. 已查实的事实（grounding，勿推翻重查）

### 1.1 云账号家底（e-cam MongoDB `ecam_cloud_account`，只读探针实测）
5 个账号**全部**在 `tenant 3（深圳市嘉立创）`，均 `active`：

| 账号 | provider | 可写 |
|---|---|---|
| 集团-阿里云 | aliyun | 非只读 |
| 集团-华为云 | huawei | 只读 |
| 集团-AWS | aws | 只读 |
| 集团-腾讯云 | tencent | 非只读 |
| 集团-火山云 | volcengine | 非只读 |

- AccessKeySecret **加密存储**，解密密钥 = `security.encryption_key`（prod.yaml）/ 环境变量 `CAM_ENCRYPTION_KEY`，解密在 account 仓储读取路径完成，仅内存传递。

### 1.2 e-cam 现有日志查询架构（已实核）
- 功能域：`internal/logquery/`，装配入口 `internal/logquery/module.go`（`InitLogQueryModule` / `RegisterRoutes`）。
- 只读域，Phase A **不落库**（ADR D1）；路由前缀 `/api/v1/cam/logs`，组级 `RequireTenant` 租户边界。
- 真实云 Provider 来自**外部 SDK**：`import _ "github.com/Havens-blog/e-cloudx-sdk/providers"`（blank import 触发注册）；
  联邦编排 `internal/logquery/service/federation.go`（`FederationService`，按租户+云+账号 fan-out 再 merge）。
- HTTP 接口（`internal/logquery/web/handler.go` 实核）：
  - `GET  /types`         字段字典（动态列，逐类型探测可聚合字段）
  - `GET  /sources`       日志源清单（`?log_type=cdn&clouds=aliyun,aws`）
  - `POST /search`        联邦查询
  - `POST /aggregate`     窗口聚合（count/sum_bytes/avg_latency/p99_latency）
  - `POST /diagnose`      WAF 流量诊断
  - `POST /cache-analyze` CDN 缓存分析（feature flag 默认关）
- `POST /search` 请求体：`{log_type, start_time(epoch 秒), end_time, query, clouds[], account_ids[], resources[], filters[], limit}`
  响应：`{code:0, msg, data:{entries:[...]}}`
- **当前支持的 LogType**：`CDN / WAF / SLB / Access（源站访问）` —— **全是"云资源流量日志"**。
- 已有功能文档：`docs/features/log-query-optimization/`（6 个 pending 任务：sources 延迟、loading/重试、图表栅格、字段快捷值、按云折叠、TopN 下钻）。

### 1.3 OpsAgent RCA 日志维度现状（已实核 + 已修）
- RCA `LogSource` 接口（`internal/rca/sources/log_source.go`）：
  `QueryErrors(start,end)` / `QueryLogsByService(service,start,end)` / `GetLogCount(service,start,end)`。
- 已把"会编造假数据的 mock 兜底"替换为**诚实空源**（`internal/rca/sources/empty_sources.go`）：
  没接真实源的维度**不编造、保持沉默**。
- 当前真实维度：指标（ARMS/Prometheus，深圳）、拓扑（ARMS APM）；变更、日志为空源。

### 1.4 关键正确性陷阱（最容易踩）
**"云资源流量日志（CDN/WAF/SLB）" ≠ "应用服务错误日志（SLS logstore，含 service/level 字段）"。**
RCA 要的是后者（按服务抓 error 尖峰做根因）。拿 WAF/SLB 流量日志喂 RCA，就是换个姿势的假数据，根因照样不准。

---

## 2. 依赖顺序（硬约束，不可并行）

```
Phase 1 (e-cam)：多云联邦查询丰富 + 新增"应用日志(SLS logstore)"类型
        │  产出：GET /api/v1/cam/logs/types 里出现"应用日志"类型，字段含 service/level
        ▼
Phase 2 (OpsAgent)：RCA 日志维度接 e-cam /search 联邦接口，取代空源
```

Phase 2 的前置校验：`GET /api/v1/cam/logs/types` 必须已返回应用日志类型。
若只有 CDN/WAF/SLB/Access，**停下来报告**，不要拿流量日志冒充应用错误日志。

---

## 3. 提示词 ① —— 丰富 e-cam-service + e-cam-web 多云日志查询

```
# 任务：丰富 e-cam-service + e-cam-web 的多云统一日志查询能力

## 角色
你是 e-cam-service（Go 后端）+ e-cam-web（Vue3 前端）的资深工程师。
遵循第一性原理：先把问题拆到最底层事实，验证而非假设，最小必要实现，拒绝过度设计和货物崇拜。
动机不清先停下来问，不要猜着乱改。回答用中文，简洁，给文件路径链接。

## 必读的既有架构事实（先看代码坐实，别推翻重造）
1. 日志查询功能域在 internal/logquery/，是"多云统一日志查询 Phase 1"：
   - 装配入口 internal/logquery/module.go（InitLogQueryModule / RegisterRoutes）
   - 只读域，Phase A 不落库（ADR D1），凭证经 account 仓储读取路径解密、仅内存传递
   - 路由挂 /api/v1/cam/logs，租户边界由组级 RequireTenant 承接
2. 真实云厂商 Provider 来自外部 SDK：import "github.com/Havens-blog/e-cloudx-sdk/providers"（blank import 触发注册）。
   先去读 e-cloudx-sdk/providers，搞清楚：每朵云（aliyun/huawei/aws/tencent/volcengine）各实现了哪些 LogType，
   Provider/Aggregator 接口长什么样，RegisterProvider 注册机制。
3. 联邦编排在 internal/logquery/service/federation.go（FederationService）：按租户+云+账号 fan-out 查询再 merge；
   有 result_cache、diagnose、cache_analyze、llm 等子能力；diagwatch 是定时诊断看护。
4. 云账号存在 MongoDB 集合 ecam_cloud_account（当前 5 个账号全在 tenant 3，均 active）；
   AccessKeySecret 加密存储，security.encryption_key / CAM_ENCRYPTION_KEY 解密。
5. 前端在 e-cam-web：api/logs.ts、api/types/logs.ts 是契约层；日志查询视图需定位（grep logs/diagnose 相关 .vue）。
6. 已有功能文档 docs/features/log-query-optimization/（manifest + 6 个 pending 任务：sources 延迟 20s、
   loading/重试、图表栅格自适应、字段筛选快捷值、按云·账号折叠分组、TopN 下钻）——先读完，别和它冲突或重复。

## 真实缺口（先调查确认，再动手）
- 日志类型覆盖：现有 CDN/WAF/SLB/Access 是"云资源流量日志"。缺"应用服务日志"（阿里云 SLS logstore，
  带 service/level 字段）——这是做服务级根因分析的关键，和 WAF/CDN 流量日志完全不是一回事，不要混为一谈。
  调查 e-cloudx-sdk 是否已支持 SLS 应用日志 Provider，没有就补。
- Provider 覆盖矩阵：列出"云 × LogType"的真实支持矩阵，找出空洞（如华为/AWS 的日志 Provider 是否缺失）。
- 联邦能力：跨云字段 schema 是否统一？时间轴是否对齐？跨云关联/聚合是否可用？
- 前端：多云来源的展示、分组、筛选、下钻是否完整（参考那 6 个 pending 任务）。

## 硬约束
- 严格保持既有架构：只读 Phase、ADR D1 不落库、RequireTenant 租户边界、Provider 经 SDK 注册——不要绕开。
- 新增云/日志类型走"在 providers 包补一行注册"的既有扩展点，不要另起炉灶。
- 不编造数据：没接通的云/类型就如实返回空或报错，绝不返回假数据冒充。
- 密钥只按 key 名引用，绝不回显/落盘/外传。
- 每行代码能回答"解决了什么具体问题"，不做未来特性预留。
- 改完必须编译 + vet + 相关测试全绿；前端 type-check 绿。涉及真实云 API 的，用真实凭证做一次最小连通性验证（只读）。

## 第一步要求
先只做调查，产出一份《现状与缺口报告》：真实 Provider 支持矩阵、FederationService 能力清单、
前端契约现状、与已有 6 任务的关系。报告给我确认后再动手实现。
```

---

## 4. 提示词 ② —— OpsAgent RCA 消费 e-cam 日志联邦（Phase 1 完成后再用）

```
# 任务：让 OpsAgent RCA 的日志维度消费 e-cam 多云日志联邦，取代本地空源

## 前置依赖（没满足就先停，别硬接）
e-cam-service 必须已支持"应用日志"类型（SLS logstore，字段含 service/level/message/timestamp）。
确认方法：GET http://<e-cam>/api/v1/cam/logs/types 返回里有应用日志类型。
若只有 CDN/WAF/SLB/Access（流量日志），说明依赖未就绪——停下来报告，
不要拿流量日志冒充应用错误日志喂 RCA。

## 背景（已坐实的事实）
- OpsAgent RCA 日志维度现在是诚实空源（internal/rca/sources/empty_sources.go），没接真实源时不编造。
- 要接的目标接口：LogSource（internal/rca/sources/log_source.go）三方法：
  QueryErrors(start,end) / QueryLogsByService(service,start,end) / GetLogCount(service,start,end)
- e-cam 日志联邦契约（已实核 internal/logquery/web/handler.go）：
  - POST /api/v1/cam/logs/search
    body: {log_type, start_time(epoch 秒), end_time, query, clouds[], account_ids[], resources[], filters[], limit}
    resp:  {code:0, msg, data:{entries:[...], ...}}
  - 鉴权：组级 RequireTenant，按 EIAM 会话取租户；日志按云账号隔离，云账号属租户。

## 必须先回答的设计问题（不要擅自假设）
OpsAgent RCA 在后台 goroutine 跑（tenant.IDFrom(ctx) 持有租户 ID），不是用户请求线程。
调 e-cam 的 RequireTenant 接口需要租户身份。要搞清楚 e-cam 对"服务间调用"的鉴权方式：
是复用 EIAM 会话 cookie？还是有服务账号/内部 token？还是显式传 tenant_id？
查 e-cam 中间件（middleware.RequireTenant / GetTenantID）确认，别猜。

## 实现要求
1. 新增 internal/rca/sources 下的 ECamLogSource，实现 LogSource 接口，内部 HTTP 调 e-cam /search。
   - QueryErrors：filters 带 level=error（按 e-cam 应用日志字段真实键名）
   - QueryLogsByService：filters 带 service=<svc>
   - GetLogCount：优先用 /aggregate(metric=count)，拿不到再退化为 search 计数
   - 字段映射：把 e-cam LogEntry 映射到 models.LogEntry（service/level/message/timestamp 对齐，缺字段如实留空，不编造）
2. 接进渐进增强工厂 internal/agent/rca_sources.go：配了 e-cam 端点就用 ECamLogSource，否则保持空源。
   新增 env：ECAM_LOGQUERY_ENDPOINT 等（跟现有 PROMETHEUS_/ARMS_ 一套风格）。
3. 带 pingOK 可达性探测，连不通就回退空源（不阻塞启动，和现有 metrics/topology 一致）。

## 硬约束
- 不编造数据：e-cam 返回空就是空，报错就如实报错，绝不塞假日志。
- 服务名对齐：e-cam 日志里的 service 值必须能和告警/ARMS 的服务名对上，否则 RCA 匹配不到——
  这是日志维度准不准的命门，实现后用真实请求验证能匹配上。
- 改完 go build/vet/test 全绿，用真实 incident 跑一次 /api/v1/rca/analyze 验证日志维度真出了假设、且 service 对得上。
- 密钥/token 只按 key 名引用，不回显不落盘。
```

---

## 5. 验收标准（两阶段都做完后）

1. `GET /api/v1/cam/logs/types` 返回包含"应用日志"类型，字段含 `service` / `level`。
2. e-cam 多云 Provider 支持矩阵有明确文档，空洞被补齐或显式标注"未支持"。
3. OpsAgent `POST /api/v1/rca/analyze` 真实请求：日志维度能产出假设，且 `service` 与告警/ARMS 服务名对得上。
4. 全程不编造：任何没接通的云/维度，如实空或报错，绝不假数据冒充。
5. e-cam 编译/vet/test 绿、前端 type-check 绿；OpsAgent build/vet/test 绿。
