import { cleanLink, linkDisplayText } from '../lib/linkClean'
import { linksIn } from '../lib/linkText'

// LinkedText is words a person typed, with the web links in them made
// followable: shown as their short text, opened clean in a new tab. The rest is
// as typed, line breaks and all (the bubble keeps them). Not for Markdown, which
// reads its own links.
export function LinkedText({ text }: { text: string }) {
  const spans = linksIn(text)
  if (spans.length === 0) return <>{text}</>
  const parts: React.ReactNode[] = []
  let at = 0
  for (const span of spans) {
    if (span.start > at) parts.push(text.slice(at, span.start))
    const cleaned = cleanLink(span.link)
    parts.push(
      <a key={span.start} href={cleaned} title={cleaned} target="_blank" rel="noopener noreferrer">
        {linkDisplayText(span.link)}
      </a>,
    )
    at = span.end
  }
  if (at < text.length) parts.push(text.slice(at))
  return <>{parts}</>
}
