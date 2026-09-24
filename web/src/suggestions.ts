// The replies an answer offers to send with a click.
//
// An answer on the dashboard may end with a hidden line naming two to six
// replies the person would likely send next,
// <!--suggestions:["Yes, go ahead","Not now"]-->, which the chat draws as
// buttons above the box. The line is taken off what is shown, whole or
// half-streamed, and off everything that leaves the dashboard on the
// server (models.StripSuggestedReplies).

const MARKER_PATTERN = /\n?<!--suggestions:(\[[^\]]*\])-->\s*/
const MARKER_OPENING = '<!--suggestions:'

export interface SuggestedReplies {
  // displayText is the answer without the line.
  displayText: string
  // suggestions are the replies, or none when the line is missing or
  // unreadable.
  suggestions: string[]
}

// suggestedRepliesOf reads the replies off an answer. A line that is there
// but unreadable is still taken off, so the person never sees it.
export function suggestedRepliesOf(text: string): SuggestedReplies {
  const match = text.match(MARKER_PATTERN)
  if (!match) return { displayText: text, suggestions: [] }
  const displayText = text.replace(MARKER_PATTERN, '').trimEnd()
  try {
    const parsed: unknown = JSON.parse(match[1])
    if (
      !Array.isArray(parsed) ||
      parsed.length < 2 ||
      parsed.length > 6 ||
      parsed.some((item) => typeof item !== 'string' || item.trim() === '')
    ) {
      return { displayText, suggestions: [] }
    }
    return { displayText, suggestions: (parsed as string[]).map((item) => item.trim()) }
  } catch {
    return { displayText, suggestions: [] }
  }
}

// withoutPartialMarker is an answer still streaming, without a line that
// has begun and not ended: the characters of the marker arrive one by one,
// and none of them are for the person.
export function withoutPartialMarker(text: string): string {
  const start = text.lastIndexOf('<!--')
  if (start < 0) return text
  const tail = text.slice(start)
  if (tail.includes('-->')) return suggestedRepliesOf(text).displayText
  const isMarker =
    tail.length <= MARKER_OPENING.length ? MARKER_OPENING.startsWith(tail) : tail.startsWith(MARKER_OPENING)
  return isMarker ? text.slice(0, start).trimEnd() : text
}
