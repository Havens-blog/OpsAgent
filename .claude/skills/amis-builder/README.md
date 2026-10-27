# amis-builder Skill

amis 低代码框架专家和知识库，用于快速构建 amis 页面和回答 amis 使用问题。

## 核心定位

这是一个 **amis 框架专家 + 知识库**，提供：
1. **📚 知识库**：回答 amis 使用问题（组件、属性、表达式、事件等）
2. **🛠️ 代码生成**：根据需求生成 amis JSON schema
3. **🔍 文档查询**：通过 Context7 + GitHub 实时获取最新 amis 官方文档
4. **💡 最佳实践**：参考常用的页面模板和设计模式

## 触发条件

**只有当你明确提到以下关键词时才会触发：**
- amis、百度 amis、aisuda
- 低代码、low-code
- schema、JSON schema
- amis 组件、amis 表达式

**示例：**
- ✅ "amis 的 CRUD 组件怎么用？"
- ✅ "帮我生成一个 amis 表单"
- ✅ "用低代码生成一个列表页"
- ❌ "帮我生成一个表单"（没提 amis，不会触发）
- ❌ "用 React 生成表单"（明确要求 React，不会触发）

## 功能特性

- ✅ 根据字段数据自动生成 amis 页面（CRUD、表单、卡片等）
- ✅ 智能字段类型映射（文本、日期、图片、文件、下拉选择等）
- ✅ **通过 Context7 MCP 服务器实时获取 amis 官方文档**（5833+ 代码片段）
- ✅ **GitHub Raw 兜底查询**（直接从官方仓库获取 Markdown）
- ✅ 参考常用页面模板（新增、编辑、删除等常用模式）
- ✅ 支持必填字段验证
- ✅ 支持多列布局、水平布局等

## 使用场景

### 场景 1：询问 amis 使用问题（知识库模式）

```
你: amis 的 CRUD 组件怎么配置筛选条件？
你: amis 表达式怎么写？
你: 如何在 amis 中绑定点击事件？
你: amis 的 Dialog 和 Drawer 有什么区别？
```

Skill 会：
1. **优先使用 Context7 MCP 服务器**查询 amis 文档（最新最全，5833+ 代码片段）
2. **兜底使用 GitHub Raw 查询**（直接从官方仓库获取）
3. 参考项目模板和最佳实践
4. 提供清晰的解释和完整的示例代码
5. 给出相关文档链接

### 场景 2：根据字段数据生成页面（快速生成模式）

```
用户: 根据这些字段生成表单：
[
  {"name": "username", "displayName": "用户名", "fieldType": 1, "isRequired": 1},
  {"name": "email", "displayName": "邮箱", "fieldType": 1, "isRequired": 0}
]
```

### 场景 3：查询 amis 组件文档（文档查询模式）

```
用户: CRUD 组件怎么用？
用户: Form 表单的属性有哪些？
```

**文档查询优先级（三层保障）：**

1. **Context7 MCP 服务器**（主力）
   - 库 ID: `/baidu/amis` 或 `/websites/baidu_github_io_amis_zh-cn`
   - 5833+ 代码片段，完整示例
   - 在 Claude Code 中自动调用

2. **GitHub Raw 查询**（兜底）
   - 使用 `search_docs.py` 脚本
   - 直接从官方仓库获取 Markdown

3. **项目模板参考**（补充）
   - `references/amis-templates.md`
   - `references/field-types.md`

## 目录结构

```
amis-builder/
├── SKILL.md                    # 核心指令
├── README.md                   # 使用说明
├── scripts/
│   └── search_docs.py          # 从 GitHub 搜索 amis 文档（兜底方案）
├── references/
│   ├── field-types.md          # 字段类型映射配置
│   └── amis-templates.md       # 常用页面模板
└── config/
    └── docs-path.json          # 配置文件
```

## 脚本使用

### 搜索 amis 文档

**推荐方式：直接在 Claude Code 中查询**

在对话中直接询问 amis 相关问题，Claude Code 会自动调用 Context7 MCP 服务器查询。

**兜底方式：使用 search_docs.py 脚本**

如果 Context7 查询失败，可以使用脚本从 GitHub 获取：

```bash
# 搜索组件文档
python3 scripts/search_docs.py "crud"

# 搜索表单组件
python3 scripts/search_docs.py "form"

# 搜索其他组件
python3 scripts/search_docs.py "dialog"
```

脚本会从 GitHub Raw 实时获取官方文档。

## 字段类型映射

| fieldType | 组件类型 | 说明 |
|-----------|---------|------|
| 1 | input-text | 文本输入 |
| 2 | input-number | 数字输入 |
| 3 | textarea | 多行文本 |
| 4 | select | 下拉选择 |
| 5 | input-date | 日期选择 |
| 6 | input-image | 图片上传 |
| 7 | input-file | 文件上传 |
| 8 | switch | 开关 |
| 9 | input-city | 城市选择 |

详细配置请查看 `references/field-types.md`。

## 输出格式

Skill 输出的 JSON schema 格式：

```
<<<<<<< START_TITLE index.json >>>>>>> END_TITLE
\`\`\`json
{
  "type": "form",
  "api": {
    "method": "post",
    "url": "/api/create"
  },
  "body": [
    {
      "type": "input-text",
      "name": "username",
      "label": "用户名",
      "required": true
    }
  ]
}
\`\`\`
```

## 配置说明

### config/docs-path.json

```json
{
  "amis_docs_root": "",
  "components_dir": "components",
  "concepts_dir": "concepts",
  "extend_dir": "extend",
  "_comment": "如果你有本地 amis 文档，请配置 amis_docs_root 路径"
}
```

**说明：**
- `amis_docs_root`: 本地 amis 文档路径（可选）
- 文档查询已改用 Context7 + GitHub Raw，无需配置本地文档路径

## 常见问题

### Q: 如何添加新的字段类型？

A: 编辑 `references/field-types.md`，添加新的字段类型配置。

### Q: 如何查看完整的 amis 文档？

A:
1. **推荐**：在 Claude Code 中直接询问，自动调用 Context7 查询
2. **兜底**：使用 `search_docs.py` 脚本从 GitHub 获取
3. **官方**：访问官方文档 https://aisuda.bce.baidu.com/amis/zh-CN/docs/index

### Q: Context7 查询失败怎么办？

A: 脚本会自动降级到 GitHub Raw 查询，直接从官方仓库获取文档。

## 示例

### 生成表单页面

```
用户: 帮我生成一个用户表单，包含用户名和邮箱字段
```

输出：
```json
{
  "type": "form",
  "mode": "horizontal",
  "labelWidth": 112,
  "columnCount": 2,
  "api": {
    "method": "post",
    "url": "/api/user/create"
  },
  "body": [
    {
      "type": "input-text",
      "name": "username",
      "label": "用户名",
      "required": true
    },
    {
      "type": "input-email",
      "name": "email",
      "label": "邮箱",
      "required": false
    }
  ]
}
```

### 生成 CRUD 页面

```
用户: 帮我生成一个用户列表页面
```

输出：
```json
{
  "type": "crud",
  "api": "/api/user/list",
  "headerToolbar": [
    {
      "type": "button",
      "label": "新增",
      "icon": "fa fa-plus",
      "actionType": "dialog",
      "dialog": {
        "title": "新增用户",
        "size": "lg",
        "body": {
          "type": "form",
          "api": {
            "method": "post",
            "url": "/api/user/create"
          },
          "body": [...]
        }
      }
    }
  ],
  "columns": [
    {"name": "id", "label": "ID"},
    {"name": "username", "label": "用户名"},
    {"name": "email", "label": "邮箱"}
  ]
}
```

## 作者

Bamzc

## 许可证

MIT
