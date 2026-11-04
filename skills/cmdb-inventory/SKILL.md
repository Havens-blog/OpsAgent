---
name: cmdb-inventory
description: 云资源 / CMDB 清单（CDN、ECS、RDS、主机等）查询口径
---

查询云资源 / CMDB 清单时：

1. 先调用 `list_cmdb_instances` 或 `resource_summary`。
2. 实例数量必须用工具返回的 `total`（总数）与 `count`（本次返回条数），严禁把查询参数 `limit`（默认 50）当作实例数量。
3. 只报告每个实例的 `asset_name` 及其 `attributes` 中实际出现的字段值；工具未返回的字段（如 status、cname、权重、https、service_area、business_type、origin_type）一律不得自行补全或编造。
4. 工具未配置或返回空时，如实说明"未配置 / 无数据"，不得编造实例。
