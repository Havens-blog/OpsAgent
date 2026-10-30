import { Fragment, type ReactNode } from "react"

interface MarkdownProps {
  content: string
}

type InlineNode =
  | { type: "text"; content: string }
  | { type: "code"; content: string }
  | { type: "strong"; children: InlineNode[] }
  | { type: "link"; href: string; children: InlineNode[] }

type Block =
  | { type: "paragraph"; lines: string[] }
  | { type: "code"; code: string }
  | { type: "list"; ordered: boolean; items: string[] }

/**
 * 仅放行 http/https 链接，阻止 javascript:、data: 等危险协议。
 */
function isSafeUrl(url: string): boolean {
  return /^https?:\/\//i.test(url.trim())
}

/**
 * 解析行内标记（粗体、行内代码、链接），畸形输入回退为纯文本。
 */
function parseInline(text: string): InlineNode[] {
  const nodes: InlineNode[] = []
  let i = 0

  const pushText = (value: string) => {
    if (value) nodes.push({ type: "text", content: value })
  }

  while (i < text.length) {
    const ch = text[i]

    if (ch === "`") {
      const close = text.indexOf("`", i + 1)
      if (close === -1) {
        pushText(text.slice(i))
        break
      }
      nodes.push({ type: "code", content: text.slice(i + 1, close) })
      i = close + 1
      continue
    }

    if (ch === "*" && text[i + 1] === "*") {
      const close = text.indexOf("**", i + 2)
      if (close === -1) {
        pushText(ch)
        i += 1
        continue
      }
      const inner = text.slice(i + 2, close)
      if (!inner) {
        pushText("**")
        i += 2
        continue
      }
      nodes.push({ type: "strong", children: parseInline(inner) })
      i = close + 2
      continue
    }

    if (ch === "_" && text[i + 1] === "_") {
      const close = text.indexOf("__", i + 2)
      if (close === -1) {
        pushText(ch)
        i += 1
        continue
      }
      const inner = text.slice(i + 2, close)
      if (!inner) {
        pushText("__")
        i += 2
        continue
      }
      nodes.push({ type: "strong", children: parseInline(inner) })
      i = close + 2
      continue
    }

    if (ch === "[") {
      const closeBracket = text.indexOf("]", i + 1)
      const hasParen =
        closeBracket !== -1 && text[closeBracket + 1] === "("
      const closeParen = hasParen
        ? text.indexOf(")", closeBracket + 2)
        : -1

      if (closeBracket === -1 || !hasParen || closeParen === -1) {
        pushText(ch)
        i += 1
        continue
      }

      const label = text.slice(i + 1, closeBracket)
      const href = text.slice(closeBracket + 2, closeParen)
      if (!label) {
        pushText(ch)
        i += 1
        continue
      }

      nodes.push({ type: "link", href, children: parseInline(label) })
      i = closeParen + 1
      continue
    }

    pushText(ch)
    i += 1
  }

  return nodes
}

function renderInline(nodes: InlineNode[], keyPrefix: string): ReactNode[] {
  return nodes.map((node, index) => {
    const key = `${keyPrefix}-${index}`
    switch (node.type) {
      case "text":
        return node.content
      case "code":
        return (
          <code
            key={key}
            className="rounded bg-surface px-1 py-0.5 font-mono text-[0.9em] text-text-regular"
          >
            {node.content}
          </code>
        )
      case "strong":
        return (
          <strong key={key} className="font-semibold text-text-regular">
            {renderInline(node.children, key)}
          </strong>
        )
      case "link":
        if (!isSafeUrl(node.href)) {
          return <Fragment key={key}>{renderInline(node.children, key)}</Fragment>
        }
        return (
          <a
            key={key}
            href={node.href.trim()}
            target="_blank"
            rel="noopener noreferrer"
            className="break-all text-primary underline"
          >
            {renderInline(node.children, key)}
          </a>
        )
    }
  })
}

function parseBlocks(content: string): Block[] {
  const lines = content.split(/\r?\n/)
  const blocks: Block[] = []
  let i = 0

  while (i < lines.length) {
    const line = lines[i]

    if (line.trim() === "") {
      i += 1
      continue
    }

    if (/^\s*```/.test(line)) {
      const codeLines: string[] = []
      i += 1
      while (i < lines.length && !/^\s*```/.test(lines[i])) {
        codeLines.push(lines[i])
        i += 1
      }
      if (i < lines.length) i += 1
      blocks.push({ type: "code", code: codeLines.join("\n") })
      continue
    }

    const ul = line.match(/^(\s*)[-*]\s+(.*)$/)
    if (ul) {
      const items = [ul[2]]
      i += 1
      while (i < lines.length) {
        const m = lines[i].match(/^(\s*)[-*]\s+(.*)$/)
        if (!m) break
        items.push(m[2])
        i += 1
      }
      blocks.push({ type: "list", ordered: false, items })
      continue
    }

    const ol = line.match(/^(\s*)\d+\.\s+(.*)$/)
    if (ol) {
      const items = [ol[2]]
      i += 1
      while (i < lines.length) {
        const m = lines[i].match(/^(\s*)\d+\.\s+(.*)$/)
        if (!m) break
        items.push(m[2])
        i += 1
      }
      blocks.push({ type: "list", ordered: true, items })
      continue
    }

    const paragraph: string[] = [line]
    i += 1
    while (i < lines.length) {
      const l = lines[i]
      if (l.trim() === "") break
      if (/^\s*```/.test(l)) break
      if (/^(\s*)[-*]\s+/.test(l)) break
      if (/^(\s*)\d+\.\s+/.test(l)) break
      paragraph.push(l)
      i += 1
    }
    blocks.push({ type: "paragraph", lines: paragraph })
  }

  return blocks
}

function renderBlock(block: Block, key: number): ReactNode {
  switch (block.type) {
    case "code":
      return (
        <pre
          key={key}
          className="my-2 overflow-auto rounded-md bg-surface p-3"
        >
          <code className="font-mono text-[13px] leading-relaxed text-text-regular">
            {block.code}
          </code>
        </pre>
      )
    case "list": {
      const items = block.items.map((item, index) => (
        <li key={index} className="my-0.5">
          {renderInline(parseInline(item), `${key}-${index}`)}
        </li>
      ))
      return block.ordered ? (
        <ol key={key} className="my-2 list-decimal pl-5">
          {items}
        </ol>
      ) : (
        <ul key={key} className="my-2 list-disc pl-5">
          {items}
        </ul>
      )
    }
    case "paragraph":
      return (
        <p key={key} className="my-1">
          {block.lines.map((line, index) => (
            <Fragment key={index}>
              {index > 0 && <br />}
              {renderInline(parseInline(line), `${key}-${index}`)}
            </Fragment>
          ))}
        </p>
      )
  }
}

/**
 * 零依赖的轻量 Markdown 渲染组件，全程使用 React 元素，杜绝 XSS。
 */
export function Markdown({ content }: MarkdownProps) {
  return <>{parseBlocks(content).map((block, index) => renderBlock(block, index))}</>
}
