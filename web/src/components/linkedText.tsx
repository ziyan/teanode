import { cleanLink, linkDisplayText } from '../lib/linkClean'
import { linksIn } from '../lib/linkText'
import { markMentions, type MentionWord } from './mentionMarks'

// LinkedText is words a person typed, with the web links in them made
// followable: shown as their short text, opened clean in a new tab, and the
// mentions in them marked when the message names any. The rest is as typed,
// line breaks and all (the bubble keeps them). Not for Markdown, which reads
// its own links.
export function LinkedText({ text, mentions = [] }: { text: string; mentions?: MentionWord[] }) {
  const spans = linksIn(text)
  if (spans.length === 0) return <>{markMentions(text, mentions)}</>
  const parts: React.ReactNode[] = []
  let at = 0
  for (const span of spans) {
    if (span.start > at) parts.push(...markMentions(text.slice(at, span.start), mentions, `mention-${at}`))
    const cleaned = cleanLink(span.link)
    parts.push(
      <a key={span.start} href={cleaned} title={cleaned} target="_blank" rel="noopener noreferrer">
        {linkDisplayText(span.link)}
      </a>,
    )
    at = span.end
  }
  if (at < text.length) parts.push(...markMentions(text.slice(at), mentions, `mention-${at}`))
  return <>{parts}</>
}
