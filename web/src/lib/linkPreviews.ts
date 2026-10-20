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

export const READ_LINK_PREVIEWS = `query ($urls: [String!]!) {
  ReadLinkPreviews(urls: $urls) {
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

type Waiting = { url: string; settle: (preview: LinkPreview | null) => void }

const remembered = new Map<string, Promise<LinkPreview | null>>()
let waiting: Waiting[] = []
let timer: ReturnType<typeof setTimeout> | null = null

// requestLinkPreview is the preview of a link, or null when there is none to
// show. The links asked for within a moment of each other go to the server in
// one question, ten at a time, and a link asked for again is not asked again.
export function requestLinkPreview(url: string): Promise<LinkPreview | null> {
  const known = remembered.get(url)
  if (known) return known
  if (remembered.size >= MOST_REMEMBERED) remembered.clear()
  const asked = new Promise<LinkPreview | null>((settle) => {
    waiting.push({ url, settle })
  })
  remembered.set(url, asked)
  if (timer === null) timer = setTimeout(sendWaiting, COLLECT_FOR)
  return asked
}

function sendWaiting() {
  timer = null
  const batch = waiting.slice(0, MOST_LINK_PREVIEW_URLS)
  waiting = waiting.slice(MOST_LINK_PREVIEW_URLS)
  if (waiting.length > 0) timer = setTimeout(sendWaiting, COLLECT_FOR)
  if (batch.length === 0) return
  graphql<{ ReadLinkPreviews: LinkPreview[] }>(READ_LINK_PREVIEWS, { urls: batch.map((each) => each.url) })
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
        setTimeout(() => remembered.delete(each.url), FAILED_FOR)
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
