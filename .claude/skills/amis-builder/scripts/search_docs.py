#!/usr/bin/env python3
"""
搜索 amis 文档（从 GitHub Raw 获取）
Author: Bamzc

说明：
- 本脚本作为 Context7 查询的兜底方案
- 直接从 amis 官方 GitHub 仓库获取 Markdown 文档
- 优先使用 Context7 MCP 服务器查询（在 Claude Code 中自动调用）
"""
import sys
import argparse
from urllib import request, error

# 在线文档 URL
AMIS_GITHUB_RAW_BASE = "https://raw.githubusercontent.com/baidu/amis/master/docs/zh-CN"
AMIS_ONLINE_DOCS_BASE = "https://aisuda.bce.baidu.com/amis/zh-CN"

def fetch_online_doc(component_name):
    """
    从 GitHub Raw 获取组件文档（Markdown 原文）
    :param component_name: 组件名称，如 'form', 'crud'
    :return: 文档内容（Markdown 格式）
    """
    # 尝试多个可能的路径
    possible_paths = [
        f"{AMIS_GITHUB_RAW_BASE}/components/{component_name}/index.md",
        f"{AMIS_GITHUB_RAW_BASE}/components/{component_name}.md",
    ]

    for url in possible_paths:
        try:
            print(f"📥 正在从 GitHub 获取: {url}")

            req = request.Request(url, headers={
                'User-Agent': 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36'
            })

            with request.urlopen(req, timeout=10) as response:
                markdown_content = response.read().decode('utf-8')

            print(f"✅ 成功获取文档（{len(markdown_content)} 字符）")
            return markdown_content

        except error.HTTPError as e:
            if e.code == 404:
                continue  # 尝试下一个路径
            else:
                print(f"❌ HTTP 错误: {e.code}")
                return None
        except error.URLError as e:
            print(f"❌ 网络请求失败: {str(e)}")
            return None
        except Exception as e:
            print(f"❌ 获取文档失败: {str(e)}")
            return None

    print(f"❌ 未找到组件文档: {component_name}")
    return None

def search_docs(keyword):
    """
    搜索 amis 文档（从 GitHub Raw 获取）
    :param keyword: 搜索关键词
    :return: 搜索结果
    """
    # 常用组件列表
    common_components = [
        'form', 'crud', 'dialog', 'drawer', 'page', 'service',
        'input-text', 'input-number', 'input-date', 'input-datetime',
        'select', 'checkboxes', 'radios', 'switch', 'input-image', 'input-file',
        'table', 'cards', 'list', 'chart', 'tabs', 'wizard'
    ]

    results = []

    # 如果关键词匹配组件名，直接获取该组件文档
    if keyword.lower() in common_components:
        content = fetch_online_doc(keyword.lower())
        if content:
            results.append({
                "component": keyword.lower(),
                "source": "github",
                "content": content
            })
    else:
        # 否则搜索常用组件中包含关键词的
        print(f"🔍 在常用组件中搜索关键词: {keyword}")
        for comp in common_components:
            if keyword.lower() in comp:
                content = fetch_online_doc(comp)
                if content and keyword.lower() in content.lower():
                    results.append({
                        "component": comp,
                        "source": "github",
                        "content": content[:5000]  # 返回前 5000 字符
                    })

    return results

if __name__ == "__main__":
    parser = argparse.ArgumentParser(
        description="搜索 amis 文档（从 GitHub 获取）",
        epilog="提示：优先使用 Context7 MCP 服务器查询，本脚本作为兜底方案"
    )
    parser.add_argument("keyword", help="搜索关键词（组件名称）")
    parser.add_argument("--type", default="component", help="文档类型（仅支持 component）")

    args = parser.parse_args()

    print("\n💡 提示：优先使用 Context7 MCP 服务器查询 amis 文档")
    print("   本脚本作为兜底方案，从 GitHub 获取文档\n")

    results = search_docs(args.keyword)

    if results:
        print(f"\n找到 {len(results)} 个相关文档：\n")
        for r in results:
            print(f"## {r['component']} (GitHub Raw)")
            print(f"在线文档: {AMIS_ONLINE_DOCS_BASE}/components/{r['component']}")
            print(f"\n{r['content'][:2000]}...")  # 只显示前 2000 字符
            print("\n" + "=" * 80 + "\n")
    else:
        print("\n❌ 未找到相关文档")
        print("\n建议：")
        print("  1. 检查组件名称是否正确")
        print("  2. 使用 Context7 MCP 服务器查询（更全面）")
        print("  3. 访问官方文档: https://aisuda.bce.baidu.com/amis/zh-CN/components/index")

