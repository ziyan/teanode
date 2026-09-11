import { Link } from 'react-router-dom'
import { framedDrawer } from '../api'

import { CodeBlock } from './codeBlock'

// The part of Markdown a changelog and an agent's answer are written in.
//
// No library. It began as the four rules a release note needs — a heading,
// a bullet per change with its continuation lines indented, `code` and
// **bold** — and grew what an assistant's answer needs beside them: numbered
// lists, fenced code with a copy mark, a table, a quote, a rule, italics.
// A Markdown package is a dependency, a supply chain and a sanitizer to
// think about for text that came from a release list or a model.
//
// Nothing here interprets HTML: every value below ends up as a text node,
// so a note containing a <script> tag is a note that displays a <script>
// tag.

// inline renders `code`, **bold**, *italic* and [text](url) inside one line.
function inline(text: string, keyPrefix: string): React.ReactNode[] {
  const nodes: React.ReactNode[] = []
  // No lookbehind: what precedes an italic run is captured and put back,
  // since a lookbehind is a syntax error for a browser that predates it and
  // takes the whole bundle down with it.
  const pattern = /`([^`]+)`|\*\*([^*]+)\*\*|(^|[\s(])[*_]([^*_\n]+)[*_](?=[\s.,;:!?)]|$)|\[([^\]]+)\]\(([^)]+)\)/g
  let index = 0
  let match: RegExpExecArray | null
  let count = 0

  while ((match = pattern.exec(text)) !== null) {
    if (match.index > index) {
      nodes.push(text.slice(index, match.index))
    }
    const key = `${keyPrefix}-${count++}`
    if (match[1] !== undefined) {
      nodes.push(<code key={key}>{match[1]}</code>)
    } else if (match[2] !== undefined) {
      nodes.push(<strong key={key}>{match[2]}</strong>)
    } else if (match[4] !== undefined) {
      if (match[3]) nodes.push(match[3])
      nodes.push(<em key={key}>{match[4]}</em>)
    } else {
      // Only http and https. A link in a release note is a link somebody
      // else wrote, and javascript: is a scheme nothing here should follow.
      const href = match[6]
      const mail = /^mail:([A-Za-z0-9]+)$/.exec(href)
      nodes.push(
        mail && framedDrawer ? (
          // Framed into another site, the drawer sends the person to the
          // dashboard itself: nothing of it but the drawer is drawn here.
          <a key={key} href={`${window.location.origin}/mailbox/starred/${encodeURIComponent(mail[1])}`} target="_blank" rel="noopener noreferrer">
            {match[5]}
          </a>
        ) : mail ? (
          // The agent cites a message as mail:ITEM_ID; Starred opens any
          // item by id whichever folder it is in.
          <Link key={key} to={`/mailbox/starred/${mail[1]}`}>
            {match[5]}
          </Link>
        ) : /^https?:\/\//i.test(href) ? (
          <a key={key} href={href} target="_blank" rel="noopener noreferrer nofollow">
            {match[5]}
          </a>
        ) : (
          match[5]
        ),
      )
    }
    index = match.index + match[0].length
  }
  if (index < text.length) {
    nodes.push(text.slice(index))
  }
  return nodes
}

type Block =
  | { kind: 'heading'; level: number; text: string }
  | { kind: 'list'; ordered: boolean; items: string[] }
  | { kind: 'paragraph'; text: string }
  | { kind: 'code'; language: string; text: string }
  | { kind: 'quote'; text: string }
  | { kind: 'rule' }
  | { kind: 'table'; header: string[]; rows: string[][] }

function cells(line: string): string[] {
  return line
    .trim()
    .replace(/^\|/, '')
    .replace(/\|$/, '')
    .split('|')
    .map((cell) => cell.trim())
}

// parse groups the lines into blocks. A bullet continues across the indented
// lines under it, which is how a changelog wraps a long entry, and how the
// entries this repository writes are all shaped.
function parse(source: string): Block[] {
  const blocks: Block[] = []
  let items: string[] | null = null
  let ordered = false
  let paragraph: string[] | null = null
  let quote: string[] | null = null
  let code: { language: string; lines: string[] } | null = null
  let table: { header: string[]; rows: string[][] } | null = null

  const endList = () => {
    if (items) {
      blocks.push({ kind: 'list', ordered, items })
      items = null
    }
  }
  const endParagraph = () => {
    if (paragraph) {
      blocks.push({ kind: 'paragraph', text: paragraph.join(' ') })
      paragraph = null
    }
  }
  const endQuote = () => {
    if (quote) {
      blocks.push({ kind: 'quote', text: quote.join(' ') })
      quote = null
    }
  }
  const endTable = () => {
    if (table) {
      blocks.push({ kind: 'table', header: table.header, rows: table.rows })
      table = null
    }
  }
  const endAll = () => {
    endList()
    endParagraph()
    endQuote()
    endTable()
  }

  const lines = source.split('\n')
  for (let at = 0; at < lines.length; at++) {
    const raw = lines[at]
    const line = raw.trimEnd()

    if (code) {
      if (/^\s*```/.test(line)) {
        blocks.push({ kind: 'code', language: code.language, text: code.lines.join('\n') })
        code = null
      } else {
        code.lines.push(raw)
      }
      continue
    }
    const fence = /^\s*```\s*([\w+-]*)\s*$/.exec(line)
    if (fence) {
      endAll()
      code = { language: fence[1].toLowerCase(), lines: [] }
      continue
    }

    const heading = /^(#{1,6})\s+(.*)$/.exec(line.trim())
    const bullet = /^\s*[-*+]\s+(.*)$/.exec(line)
    const numbered = /^\s*\d+[.)]\s+(.*)$/.exec(line)
    const quoted = /^\s*>\s?(.*)$/.exec(line)
    const rule = /^\s*([-*_])(\s*\1){2,}\s*$/.test(line)

    if (line.trim() === '') {
      endAll()
      continue
    }
    if (rule) {
      endAll()
      blocks.push({ kind: 'rule' })
      continue
    }
    if (heading) {
      endAll()
      blocks.push({ kind: 'heading', level: heading[1].length, text: heading[2] })
      continue
    }
    // A table is a header row, a separator of dashes, and rows.
    if (!table && line.includes('|') && at + 1 < lines.length && /^\s*\|?\s*:?-{2,}/.test(lines[at + 1])) {
      endAll()
      table = { header: cells(line), rows: [] }
      at++
      continue
    }
    if (table) {
      if (line.includes('|')) {
        table.rows.push(cells(line))
        continue
      }
      endTable()
    }
    if (quoted) {
      endList()
      endParagraph()
      quote = quote ?? []
      quote.push(quoted[1])
      continue
    }
    endQuote()
    if (bullet || numbered) {
      endParagraph()
      const isOrdered = !bullet
      if (items && ordered !== isOrdered) endList()
      items = items ?? []
      ordered = isOrdered
      items.push((bullet ?? numbered)![1])
      continue
    }
    if (items && /^\s+/.test(raw)) {
      // An indented line under a bullet belongs to it.
      items[items.length - 1] += ' ' + line.trim()
      continue
    }
    endList()
    paragraph = paragraph ?? []
    paragraph.push(line.trim())
  }
  if (code) {
    blocks.push({ kind: 'code', language: code.language, text: code.lines.join('\n') })
  }
  endAll()
  return blocks
}

export function Markdown({ text }: { text: string }) {
  return (
    <div className="markdown">
      {parse(text).map((block, index) => {
        switch (block.kind) {
          case 'heading': {
            const Tag = block.level <= 2 ? 'h4' : 'h5'
            return <Tag key={index}>{inline(block.text, `h${index}`)}</Tag>
          }
          case 'list': {
            const List = block.ordered ? 'ol' : 'ul'
            return (
              <List key={index}>
                {block.items.map((item, position) => (
                  <li key={position}>{inline(item, `l${index}-${position}`)}</li>
                ))}
              </List>
            )
          }
          case 'code':
            return <CodeBlock key={index} text={block.text} language={block.language || undefined} />
          case 'quote':
            return <blockquote key={index}>{inline(block.text, `q${index}`)}</blockquote>
          case 'rule':
            return <hr key={index} />
          case 'table':
            return (
              <div key={index} className="table-wrap">
                <table>
                  <thead>
                    <tr>
                      {block.header.map((cell, position) => (
                        <th key={position}>{inline(cell, `th${index}-${position}`)}</th>
                      ))}
                    </tr>
                  </thead>
                  <tbody>
                    {block.rows.map((row, rowIndex) => (
                      <tr key={rowIndex}>
                        {row.map((cell, position) => (
                          <td key={position}>{inline(cell, `td${index}-${rowIndex}-${position}`)}</td>
                        ))}
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )
          default:
            return <p key={index}>{inline(block.text, `p${index}`)}</p>
        }
      })}
    </div>
  )
}
