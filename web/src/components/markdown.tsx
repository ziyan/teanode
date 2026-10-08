import { createContext, useContext, useState } from 'react'
import { Link } from 'react-router-dom'
import { framedDrawer } from '../api'

import { CodeBlock } from './codeBlock'
import { mailPath, memoryPath } from './dashboardPath'
import { Formula, readsAsTex } from './math'

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
// webAddress is a written link as it may be followed: parsed, and put
// together again from its parts when it is http or https, so what the
// href carries is what the browser itself made of it — and '' for
// anything else, a javascript: scheme above all, which is then drawn as
// the words it was written as.
function webAddress(written: string): string {
  let parsed: URL
  try {
    parsed = new URL(written)
  } catch {
    return ''
  }
  if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') return ''
  // Put together from the parts the browser parsed, which leaves out
  // anything a name and password were written into the address as.
  return `${parsed.protocol}//${parsed.host}${parsed.pathname}${parsed.search}${parsed.hash}`
}

// Leaving is what whoever draws this markdown wants done when a link in
// it takes the reader somewhere else in the dashboard. The drawer on a
// phone is the whole screen, so it puts itself away rather than leaving
// the page it went to behind it.
const Leaving = createContext<() => void>(() => {})

// DashboardLink is a page of the dashboard the agent linked -- a message
// it cited, a page of its memory -- opened where it is.
function DashboardLink({ path, children }: { path: string; children: React.ReactNode }) {
  const leaving = useContext(Leaving)
  return (
    <Link to={path} onClick={leaving}>
      {children}
    </Link>
  )
}

// dashboardPathOf is where a link in the agent's own schemes goes, or null
// for one that is not: [subject](mail:ITEM_ID) and [name](memory:PATH),
// PATH#N for one fact of a page.
function dashboardPathOf(address: string): string | null {
  const mail = /^mail:(.*)$/.exec(address)
  const memory = /^memory:(.*)$/.exec(address)
  const path = mail ? mailPath(mail[1]) : memory ? memoryPath(memory[1]) : null
  // Each segment escaped as well as checked: the checks above already allow
  // only the characters a path of the graph or an item id is made of, so
  // this changes nothing a valid link says, and it is the escaping that
  // keeps a link's text from ever being read as markup where it lands.
  return path === null ? null : path.split('/').map(encodeURIComponent).join('/')
}

// PictureSource is where whoever draws this markdown has a picture fetched
// from, given the address the text wrote; null draws it as a link. A picture
// is never fetched by the browser from where the text says: the text may be
// a model's, and an address it put together can carry what it read to
// whoever runs that server. The drawer has the server fetch it, and only an
// address a tool showed the agent (see the picture endpoint).
const PictureSource = createContext<(address: string) => string | null>(() => null)

// Picture is an image the text showed, fetched from where the reader of
// this markdown allows, and a link when it cannot be.
function Picture({ address, alt, href }: { address: string; alt: string; href: string }) {
  // Checked here as well as where the text was read, so no caller can hand
  // a picture an address that is not http or https.
  const picture = webAddress(address)
  const source = useContext(PictureSource)(picture)
  const [failed, setFailed] = useState(false)
  const target = webAddress(href) || picture
  if (!picture) return <>{alt}</>
  if (!source || failed) {
    return (
      <a href={target} target="_blank" rel="noopener noreferrer nofollow">
        {alt || target}
      </a>
    )
  }
  return (
    <a className="markdown-picture" href={target} target="_blank" rel="noopener noreferrer nofollow">
      <img src={source} alt={alt} loading="lazy" onError={() => setFailed(true)} />
    </a>
  )
}

function inline(text: string, keyPrefix: string): React.ReactNode[] {
  const nodes: React.ReactNode[] = []
  // No lookbehind: what precedes an italic run is captured and put back,
  // since a lookbehind is a syntax error for a browser that predates it and
  // takes the whole bundle down with it. A code span comes first, so the
  // dollars in `$HOME/$USER` stay code. A picture comes next, linked or
  // not, so its brackets are not read as a link's. A formula is \(inline\),
  // \[displayed\] or $$displayed$$, or $inline$ where it reads as TeX and
  // not as two prices: no space inside either dollar, no digit after the
  // first or the last.
  const pattern =
    /`([^`]+)`|\[!\[([^\]]*)\]\(([^)\s]+)\)\]\(([^)\s]+)\)|!\[([^\]]*)\]\(([^)\s]+)\)|\\\((.+?)\\\)|\\\[(.+?)\\\]|\$\$(.+?)\$\$|(^|[^\\$\w`])\$(?=[^\s$\d])([^$\n]*?[^\s$\\])\$(?!\d)|\*\*([^*]+)\*\*|(^|[\s(])[*_]([^*_\n]+)[*_](?=[\s.,;:!?)]|$)|\[([^\]]+)\]\(([^)]+)\)/g
  let index = 0
  let match: RegExpExecArray | null
  let count = 0

  while ((match = pattern.exec(text)) !== null) {
    if (match.index > index) {
      nodes.push(text.slice(index, match.index))
    }
    const key = `${keyPrefix}-${count++}`
    const [
      ,
      codeSpan,
      linkedAlt,
      linkedPicture,
      pictureHref,
      alt,
      picture,
      inlineFormula,
      displayedFormula,
      dollarsFormula,
      beforeDollarFormula,
      dollarFormula,
      bold,
      beforeItalic,
      italic,
      linkText,
      linkAddress,
    ] = match
    if (codeSpan !== undefined) {
      nodes.push(<code key={key}>{codeSpan}</code>)
    } else if (linkedPicture !== undefined || picture !== undefined) {
      // A picture whose address is not http or https is its words.
      const address = webAddress(linkedPicture ?? picture)
      const href = pictureHref !== undefined ? webAddress(pictureHref) : ''
      nodes.push(
        address ? <Picture key={key} address={address} alt={linkedAlt ?? alt} href={href} /> : (linkedAlt ?? alt),
      )
    } else if (inlineFormula !== undefined) {
      nodes.push(<Formula key={key} tex={inlineFormula} isDisplay={false} />)
    } else if (displayedFormula !== undefined || dollarsFormula !== undefined) {
      nodes.push(<Formula key={key} tex={displayedFormula ?? dollarsFormula} isDisplay={true} />)
    } else if (dollarFormula !== undefined) {
      if (beforeDollarFormula) nodes.push(beforeDollarFormula)
      nodes.push(
        readsAsTex(dollarFormula) ? <Formula key={key} tex={dollarFormula} isDisplay={false} /> : `$${dollarFormula}$`,
      )
    } else if (bold !== undefined) {
      // What is inside is read the same way: a link or code in bold is a
      // link or code, not its brackets.
      nodes.push(<strong key={key}>{inline(bold, key)}</strong>)
    } else if (italic !== undefined) {
      if (beforeItalic) nodes.push(beforeItalic)
      nodes.push(<em key={key}>{inline(italic, key)}</em>)
    } else {
      // Only http and https, and the dashboard's own pages the agent links
      // as mail: and memory:. A link in a release note is a link somebody
      // else wrote, and javascript: is a scheme nothing here should follow.
      const href = webAddress(linkAddress)
      const dashboardPath = dashboardPathOf(linkAddress)
      nodes.push(
        dashboardPath && framedDrawer ? (
          // Framed into another site, the drawer sends the person to the
          // dashboard itself: nothing of it but the drawer is drawn here.
          <a key={key} href={`${window.location.origin}${dashboardPath}`} target="_blank" rel="noopener noreferrer">
            {linkText}
          </a>
        ) : dashboardPath ? (
          <DashboardLink key={key} path={dashboardPath}>
            {linkText}
          </DashboardLink>
        ) : href ? (
          <a key={key} href={href} target="_blank" rel="noopener noreferrer nofollow">
            {linkText}
          </a>
        ) : (
          linkText
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
  | { kind: 'formula'; tex: string }
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

    // A displayed formula: \[ or $$ to \] or $$, on one line or several.
    // A [ and a ] on lines of their own around TeX are one too: that is
    // how a formula reads once something has eaten its backslashes. The
    // closing is looked for up to a blank line or a code fence, neither of
    // which TeX has in it; with none, the opening line is a paragraph, and
    // an answer that writes "$$$" does not lose everything after it.
    const formulaOpening = /^\s*(\\\[|\$\$|\[\s*$)(.*)$/.exec(line)
    if (formulaOpening) {
      const closing = formulaOpening[1] === '$$' ? '$$' : formulaOpening[1] === '\\[' ? '\\]' : ']'
      const isBare = closing === ']'
      let end = -1
      let closedAt = -1
      for (let following = at; following < lines.length; following++) {
        if (following > at && (lines[following].trim() === '' || /^\s*```/.test(lines[following]))) break
        const searched = following === at ? formulaOpening[2] : lines[following]
        const found = isBare ? (following > at && searched.trim() === ']' ? 0 : -1) : searched.indexOf(closing)
        if (found >= 0) {
          end = following
          closedAt = found
          break
        }
      }
      const texLines =
        end < 0
          ? []
          : end === at
            ? [formulaOpening[2].slice(0, closedAt)]
            : [formulaOpening[2], ...lines.slice(at + 1, end), isBare ? '' : lines[end].slice(0, closedAt)]
      const tex = texLines.join('\n').trim()
      const after = end < 0 ? '' : (end === at ? formulaOpening[2] : lines[end]).slice(closedAt + closing.length).trim()
      // A bare bracket is a formula only around TeX, and a formula opened
      // and closed on one line with words after it is part of a paragraph.
      const isFormula = end >= 0 && (isBare ? end > at && /\\[a-zA-Z]/.test(tex) : end !== at || after === '')
      if (isFormula) {
        endAll()
        blocks.push({ kind: 'formula', tex })
        at = end
        if (after) paragraph = [after]
        continue
      }
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

export function Markdown({
  text,
  onLeaving,
  pictureSource,
}: {
  text: string
  onLeaving?: () => void
  pictureSource?: (address: string) => string | null
}) {
  return (
    <Leaving.Provider value={onLeaving ?? noLeaving}>
      <PictureSource.Provider value={pictureSource ?? noPictures}>
        <MarkdownBlocks text={text} />
      </PictureSource.Provider>
    </Leaving.Provider>
  )
}

// noPictures draws every picture as a link: what anything but the drawer
// gets, having no server to fetch through.
const noPictures = () => null

// noLeaving is the default: a link goes where it goes and nothing else
// happens. Kept out of the render so the provider's value is stable.
const noLeaving = () => {}

function MarkdownBlocks({ text }: { text: string }) {
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
          case 'formula':
            return <Formula key={index} tex={block.tex} isDisplay={true} />
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
