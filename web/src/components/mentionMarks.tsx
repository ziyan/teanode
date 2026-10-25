import { createContext, useContext } from 'react'

// MentionWord is a word a message mentions somebody by, after its @: a
// member's name, or the word for everybody. isReader says it is the person
// reading, whose mention is drawn stronger.
export interface MentionWord {
  word: string
  isReader: boolean
}

// MentionWords are the mentions of the message being drawn, for the plain
// text inside its Markdown to mark.
export const MentionWords = createContext<MentionWord[]>([])

// markMentions is the words with each mention in them marked: the @ and the
// name, as the message's mentions name them. Words that mention nobody are
// as they are.
export function markMentions(text: string, mentions: MentionWord[], keyPrefix = 'mention'): React.ReactNode[] {
  if (mentions.length === 0 || !text.includes('@')) return [text]
  const spans = mentionSpansOf(
    text,
    mentions.map((mention) => mention.word),
  )
  if (spans.length === 0) return [text]
  const nodes: React.ReactNode[] = []
  let at = 0
  for (const span of spans) {
    if (span.start > at) nodes.push(text.slice(at, span.start))
    const isReader = mentions.some(
      (mention) => mention.isReader && mention.word.toLowerCase() === span.word.toLowerCase(),
    )
    nodes.push(
      <mark
        key={`${keyPrefix}-${span.start}`}
        className={isReader ? 'chat-mention chat-mention-reader' : 'chat-mention'}
      >
        {text.slice(span.start, span.end)}
      </mark>,
    )
    at = span.end
  }
  if (at < text.length) nodes.push(text.slice(at))
  return nodes
}

// MentionedText is plain text inside Markdown with the mentions of the
// message around it marked.
export function MentionedText({ text }: { text: string }) {
  const mentions = useContext(MentionWords)
  return <>{markMentions(text, mentions)}</>
}

// mentionSpansOf is where the words hold an @ and one of the given words,
// earliest first, each at most once at a place, the longer of two that fit.
export function mentionSpansOf(text: string, words: string[]): { start: number; end: number; word: string }[] {
  const lowered = text.toLowerCase()
  const spans: { start: number; end: number; word: string }[] = []
  const candidates = words
    .map((word) => word.trim())
    .filter(Boolean)
    .sort((left, right) => right.length - left.length)
  for (let at = lowered.indexOf('@'); at >= 0; at = lowered.indexOf('@', at + 1)) {
    if (at > 0 && /[A-Za-z0-9_.+\-@]/.test(text[at - 1])) continue
    for (const word of candidates) {
      if (!lowered.startsWith(word.toLowerCase(), at + 1)) continue
      const after = text[at + 1 + word.length]
      if (after !== undefined && /[\p{L}\p{N}_]/u.test(after)) continue
      spans.push({ start: at, end: at + 1 + word.length, word })
      break
    }
  }
  return spans
}
