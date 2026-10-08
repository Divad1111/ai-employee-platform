/**
 * 轻量 Markdown 展示。文本制品默认按 Markdown 渲染。
 */
import type { ReactNode } from 'react'

function inline(text: string): ReactNode[] {
  const parts = text.split(/(`[^`]+`|\*\*[^*]+\*\*|\[[^\]]+\]\([^)]+\))/g)
  return parts.map((part, i) => {
    if (part.startsWith('`') && part.endsWith('`') && part.length >= 2) {
      return <code key={i}>{part.slice(1, -1)}</code>
    }
    if (part.startsWith('**') && part.endsWith('**') && part.length >= 4) {
      return <strong key={i}>{part.slice(2, -2)}</strong>
    }
    const link = part.match(/^\[([^\]]+)\]\(([^)]+)\)$/)
    if (link) {
      const href = link[2]
      if (/^https?:\/\//i.test(href)) {
        return (
          <a key={i} href={href} target="_blank" rel="noreferrer">
            {link[1]}
          </a>
        )
      }
      return <span key={i}>{link[1]}</span>
    }
    return <span key={i}>{part}</span>
  })
}

export function MarkdownView({ text }: { text: string }) {
  const lines = text.replace(/\r\n/g, '\n').split('\n')
  const blocks: ReactNode[] = []
  let i = 0
  let key = 0
  while (i < lines.length) {
    const line = lines[i]
    if (line.startsWith('```')) {
      const buf: string[] = []
      i++
      while (i < lines.length && !lines[i].startsWith('```')) {
        buf.push(lines[i])
        i++
      }
      if (i < lines.length) i++
      blocks.push(
        <pre key={key++} className="md-code">
          <code>{buf.join('\n')}</code>
        </pre>,
      )
      continue
    }
    if (/^#{1,3}\s+/.test(line)) {
      const level = line.match(/^#+/)?.[0].length ?? 1
      const content = line.replace(/^#{1,3}\s+/, '')
      if (level === 1) blocks.push(<h3 key={key++}>{inline(content)}</h3>)
      else if (level === 2) blocks.push(<h4 key={key++}>{inline(content)}</h4>)
      else blocks.push(<h5 key={key++}>{inline(content)}</h5>)
      i++
      continue
    }
    if (/^\s*[-*]\s+/.test(line)) {
      const items: string[] = []
      while (i < lines.length && /^\s*[-*]\s+/.test(lines[i])) {
        items.push(lines[i].replace(/^\s*[-*]\s+/, ''))
        i++
      }
      blocks.push(
        <ul key={key++}>
          {items.map((item, idx) => (
            <li key={idx}>{inline(item)}</li>
          ))}
        </ul>,
      )
      continue
    }
    if (line.trim() === '') {
      i++
      continue
    }
    const para: string[] = [line]
    i++
    while (i < lines.length && lines[i].trim() !== '' && !lines[i].startsWith('```') && !/^#{1,3}\s+/.test(lines[i]) && !/^\s*[-*]\s+/.test(lines[i])) {
      para.push(lines[i])
      i++
    }
    blocks.push(<p key={key++}>{inline(para.join('\n'))}</p>)
  }
  return <div className="md-view">{blocks}</div>
}
