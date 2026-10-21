import { graphql } from '../api'

// What the page behind a link says about itself, for the card under a message.
// The names are the server's (internal/api/v1api/apigraph/link_preview.go).
// The server fetches the page and its picture, never this browser, and keeps
// both, so asking again is cheap.

export type LinkPreview = {
  url: string
  /** The link without its tracking parameters: what a click opens. */
  cleanedUrl: string
  displayText: string
  /** False when the page could not be read or had nothing to say. */
  isAvailable: boolean
  title: string
  description: string
  siteName: string
  /** The picture, served by this server. */
  imageUrl: string | null
  imageWidthPixels: number | null
  imageHeightPixels: number | null
}

export const READ_LINK_PREVIEWS = `query ($conversationId: String!, $urls: [String!]!) {
  ReadLinkPreviews(conversationId: $conversationId, urls: $urls) {
    url cleanedUrl displayText isAvailable title description siteName imageUrl imageWidthPixels imageHeightPixels
  }
}`

/** How many links one question may be about. */
export const MOST_LINK_PREVIEW_URLS = 10

/** How long the cards of a screenful are collected before they are asked for. */
const COLLECT_FOR = 60

/** How long a question that failed is not asked again. */
const FAILED_FOR = 60_000

const MOST_REMEMBERED = 500

type Waiting = { conversationId: string; url: string; settle: (preview: LinkPreview | null) => void }

const remembered = new Map<string, Promise<LinkPreview | null>>()
let waiting: Waiting[] = []
let timer: ReturnType<typeof setTimeout> | null = null

// requestLinkPreview is the preview of a link written in a conversation, or
// null when there is none to show. The server previews only a link whose exact
// text is in a message of that conversation that the agent did not write, so
// the link is sent as it stands in the message, never cleaned. The links asked
// for within a moment of each other go to the server in one question per
// conversation, ten at a time, and a link asked for again is not asked again.
export function requestLinkPreview(conversationId: string, url: string): Promise<LinkPreview | null> {
  const remembering = `${conversationId}\n${url}`
  const known = remembered.get(remembering)
  if (known) return known
  if (remembered.size >= MOST_REMEMBERED) remembered.clear()
  const asked = new Promise<LinkPreview | null>((settle) => {
    waiting.push({ conversationId, url, settle })
  })
  remembered.set(remembering, asked)
  if (timer === null) timer = setTimeout(sendWaiting, COLLECT_FOR)
  return asked
}

function sendWaiting() {
  timer = null
  // The oldest question's conversation goes first, with as many of its links
  // as one question takes; the rest wait for the next.
  const conversationId = waiting[0]?.conversationId
  const batch = waiting.filter((each) => each.conversationId === conversationId).slice(0, MOST_LINK_PREVIEW_URLS)
  waiting = waiting.filter((each) => !batch.includes(each))
  if (waiting.length > 0) timer = setTimeout(sendWaiting, COLLECT_FOR)
  if (batch.length === 0 || conversationId === undefined) return
  graphql<{ ReadLinkPreviews: LinkPreview[] }>(READ_LINK_PREVIEWS, {
    conversationId,
    urls: batch.map((each) => each.url),
  })
    .then((response) => {
      const previews = response?.ReadLinkPreviews ?? []
      batch.forEach((each, index) => {
        // Answered in the order asked.
        each.settle(previews[index] ?? null)
      })
    })
    .catch(() => {
      // Not shown, and asked about again after a while.
      for (const each of batch) {
        each.settle(null)
        setTimeout(() => remembered.delete(`${each.conversationId}\n${each.url}`), FAILED_FOR)
      }
    })
}

// forgetLinkPreviews is for a test that wants a clean start.
export function forgetLinkPreviews() {
  remembered.clear()
  waiting = []
  if (timer !== null) clearTimeout(timer)
  timer = null
}
