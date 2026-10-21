// Finding the web links in a message's words.

// A link runs to the next white space, an angle bracket, a quote, or a
// character of the CJK and full-width punctuation blocks, so that a link
// written straight against Chinese or Japanese words does not carry them off.
export const BARE_LINK_SOURCE = 'https?:\\/\\/[^\\s<>"\'`\\u3000-\\u303f\\uff00-\\uffef]+'

const CLOSERS: Record<string, string> = { ')': '(', ']': '[', '}': '{' }

// trimLinkEnd takes the punctuation that ends a sentence off the end of a
// link, and a closing bracket that has no opening one in the link: the full
// stop after "see https://example.com/a." and the bracket in "(see
// https://example.com/a)" are the writer's, a bracket in
// "https://example.com/wiki/Thing_(film)" is the link's.
export function trimLinkEnd(link: string): string {
  let end = link.length
  while (end > 0) {
    const last = link[end - 1]
    if ('.,;:!?*_~'.includes(last)) {
      end--
      continue
    }
    const opener = CLOSERS[last]
    if (opener) {
      const inside = link.slice(0, end)
      if (inside.split(last).length > inside.split(opener).length) {
        end--
        continue
      }
    }
    break
  }
  return link.slice(0, end)
}

export type LinkSpan = { start: number; end: number; link: string }

// linksIn is where each web link is in text, in order.
export function linksIn(text: string): LinkSpan[] {
  const spans: LinkSpan[] = []
  for (const match of text.matchAll(new RegExp(BARE_LINK_SOURCE, 'gi'))) {
    const link = trimLinkEnd(match[0])
    // A link needs a host: "https://" and nothing after it is not one.
    if (/^https?:\/\/[^/?#]/i.test(link)) {
      spans.push({ start: match.index, end: match.index + link.length, link })
    }
  }
  return spans
}

// firstLinkOf is the first web link in the words of a message, or null. In
// words written as Markdown, code and pictures are not links to show a card
// for, so they are left out first.
export function firstLinkOf(text: string, isMarkdown: boolean): string | null {
  const words = isMarkdown
    ? text
        .replace(/```[\s\S]*?(```|$)/g, ' ')
        .replace(/`[^`]*`/g, ' ')
        .replace(/!\[[^\]]*\]\([^)]*\)/g, ' ')
    : text
  return linksIn(words)[0]?.link ?? null
}
