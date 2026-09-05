// The small part of Markdown a changelog is written in.
//
// No library. Release notes here are Keep a Changelog: a "### Fixed" heading,
// a bullet per change with its continuation lines indented, and `code` and
// **bold** inside them. That is four rules, and a Markdown package is a
// dependency, a supply chain and a sanitiser to think about for a string this
// server fetched from its own release list.
//
// Deliberately not general. It does not do tables, images, block quotes or
// raw HTML, and nothing here interprets HTML: every value below ends up as a
// text node, so a release note containing a <script> tag is a release note
// that displays a <script> tag.

// inline renders `code`, **bold** and [text](url) inside one line.
function inline(text: string, keyPrefix: string): React.ReactNode[] {
  const nodes: React.ReactNode[] = []
  const pattern = /`([^`]+)`|\*\*([^*]+)\*\*|\[([^\]]+)\]\(([^)]+)\)/g
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
    } else {
      // Only http and https. A link in a release note is a link somebody
      // else wrote, and javascript: is a scheme nothing here should follow.
      const href = match[4]
      nodes.push(
        /^https?:\/\//i.test(href) ? (
          <a key={key} href={href} target="_blank" rel="noopener noreferrer nofollow">
            {match[3]}
          </a>
        ) : (
          match[3]
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
  | { kind: 'heading'; text: string }
  | { kind: 'list'; items: string[] }
  | { kind: 'paragraph'; text: string }

// parse groups the lines into blocks. A bullet continues across the indented
// lines under it, which is how a changelog wraps a long entry, and how the
// entries this repository writes are all shaped.
function parse(source: string): Block[] {
  const blocks: Block[] = []
  let items: string[] | null = null
  let paragraph: string[] | null = null

  const endList = () => {
    if (items) {
      blocks.push({ kind: 'list', items })
      items = null
    }
  }
  const endParagraph = () => {
    if (paragraph) {
      blocks.push({ kind: 'paragraph', text: paragraph.join(' ') })
      paragraph = null
    }
  }

  for (const raw of source.split('\n')) {
    const line = raw.trimEnd()
    const heading = /^#{1,6}\s+(.*)$/.exec(line.trim())
    const bullet = /^\s*[-*]\s+(.*)$/.exec(line)

    if (line.trim() === '') {
      endList()
      endParagraph()
      continue
    }
    if (heading) {
      endList()
      endParagraph()
      blocks.push({ kind: 'heading', text: heading[1] })
      continue
    }
    if (bullet) {
      endParagraph()
      items = items ?? []
      items.push(bullet[1])
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
  endList()
  endParagraph()
  return blocks
}

export function Markdown({ text }: { text: string }) {
  return (
    <div className="markdown">
      {parse(text).map((block, index) => {
        if (block.kind === 'heading') {
          return <h5 key={index}>{inline(block.text, `h${index}`)}</h5>
        }
        if (block.kind === 'list') {
          return (
            <ul key={index}>
              {block.items.map((item, position) => (
                <li key={position}>{inline(item, `l${index}-${position}`)}</li>
              ))}
            </ul>
          )
        }
        return <p key={index}>{inline(block.text, `p${index}`)}</p>
      })}
    </div>
  )
}
