import { useEffect, useState } from 'react'

import { graphql, subscribe } from '../api'

// How many cards of the feed the person has not seen, asked by the rail to
// count them on the Feed row. On its own, apart from the Feed page, because
// the rail is in the first download and must not bring the page into it. The
// count is read once, kept up to date by the feed's own event, and set by the
// page itself when it marks cards read, so the row falls as the cards are
// looked at.

const UNREAD_EVENT = 'teanode:feed-unread'

const FEED_UNREAD = `query { ListAgentFeed(first: 1) { unreadCount } }`

// The same subscription the Feed page follows. The rail only needs the count
// that comes with each event.
export const AGENT_FEED_CHANGED = `subscription { AgentFeedChanged { feedItemId feedChange unreadCount } }`

// announceFeedUnread tells the rail how many cards are unread, from a list
// read or changed elsewhere.
export function announceFeedUnread(unreadCount: number) {
  window.dispatchEvent(new CustomEvent(UNREAD_EVENT, { detail: unreadCount }))
}

// useFeedUnread is the number of unread cards, 0 until it is known and while
// the person is not asked about (no agent to post to the feed).
export function useFeedUnread(isAsked: boolean): number {
  const [unreadCount, setUnreadCount] = useState(0)
  useEffect(() => {
    if (!isAsked) return
    const onAnnounced = (event: Event) => {
      const announced = (event as CustomEvent<number>).detail
      if (typeof announced === 'number') setUnreadCount(announced)
    }
    window.addEventListener(UNREAD_EVENT, onAnnounced)
    return () => window.removeEventListener(UNREAD_EVENT, onAnnounced)
  }, [isAsked])
  useEffect(() => {
    if (!isAsked) return
    let isStopped = false
    const read = async () => {
      try {
        const answer = await graphql<{ ListAgentFeed: { unreadCount: number } }>(FEED_UNREAD)
        if (!isStopped) setUnreadCount(answer.ListAgentFeed.unreadCount)
      } catch {
        // The row simply shows no count; the page says what failed when it is opened.
      }
    }
    void read()
    const stop = subscribe<{ AgentFeedChanged: { unreadCount: number } }>(
      AGENT_FEED_CHANGED,
      {},
      (data) => setUnreadCount(data.AgentFeedChanged.unreadCount),
      undefined,
      // What was posted while the socket was down is counted again before the
      // subscription resumes.
      { beforeStart: (isReconnecting) => (isReconnecting ? read() : undefined) },
    )
    return () => {
      isStopped = true
      stop()
    }
  }, [isAsked])
  return isAsked ? unreadCount : 0
}
