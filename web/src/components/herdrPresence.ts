import { useEffect, useMemo, useState } from 'react'

import { graphql } from '../api'
import { useQuery } from './useQuery'

// Whether any of the person's computers reports herdr, asked by the rail
// to decide whether to show the Coding row, and how many of their coding
// sessions ask something, which the row counts. On its own, apart from
// herdrSessions, because the rail is in the first download and must not bring
// the Coding page into it. Asked again every half minute while the page is
// in view, so a question shows on the row soon after it comes, and a
// computer attached later shows the row without a reload.

// How often it is asked again while the page is in view.
const RECHECK_MS = 30_000

// The Coding page reads the sessions more often than the rail does; it says
// how many ask with this event, so the count changes as soon as a question is
// answered there.
const ASKING_EVENT = 'teanode:herdr-asking'

const HERDR_PRESENCE = `query { ListAgentHerdrSessions { computerNames sessions { herdrSessionState } } }`

interface HerdrPresenceData {
  ListAgentHerdrSessions: { computerNames: string[]; sessions: { herdrSessionState: string }[] }
}

export interface HerdrPresence {
  // Null while it is being asked, and false when it is not asked or the
  // server cannot say.
  isPresent: boolean | null
  askingCount: number
}

// announceHerdrAsking tells the rail how many coding sessions ask, from a
// list read elsewhere.
export function announceHerdrAsking(askingCount: number) {
  window.dispatchEvent(new CustomEvent(ASKING_EVENT, { detail: askingCount }))
}

// useHerdrPresence says whether a computer reports herdr, and how many of
// the person's coding sessions ask something.
export function useHerdrPresence(isAsked: boolean): HerdrPresence {
  const [recheckCount, setRecheckCount] = useState(0)
  // A count the Coding page announced, and when: it stands until an answer
  // to an ask the rail started after it arrives.
  const [announced, setAnnounced] = useState<{ askingCount: number; announcedAt: number } | null>(null)
  useEffect(() => {
    if (!isAsked) return
    let askedAt = Date.now()
    const recheck = () => {
      if (document.visibilityState !== 'visible' || Date.now() - askedAt < RECHECK_MS) return
      askedAt = Date.now()
      setRecheckCount((count) => count + 1)
    }
    const timer = window.setInterval(recheck, RECHECK_MS)
    document.addEventListener('visibilitychange', recheck)
    window.addEventListener('focus', recheck)
    return () => {
      window.clearInterval(timer)
      document.removeEventListener('visibilitychange', recheck)
      window.removeEventListener('focus', recheck)
    }
  }, [isAsked])
  useEffect(() => {
    const onAnnounced = (event: Event) => {
      const askingCount = (event as CustomEvent<number>).detail
      if (typeof askingCount === 'number') setAnnounced({ askingCount, announcedAt: Date.now() })
    }
    window.addEventListener(ASKING_EVENT, onAnnounced)
    return () => window.removeEventListener(ASKING_EVENT, onAnnounced)
  }, [])
  const { data, error } = useQuery(
    async () => {
      if (!isAsked) return { ListAgentHerdrSessions: { computerNames: [], sessions: [] }, isAsked: false }
      const askedAt = Date.now()
      const answer = await graphql<HerdrPresenceData>(HERDR_PRESENCE)
      // Every answer replaces a count announced before it was asked, even
      // one the same as the last answer.
      setAnnounced((previous) => (previous && previous.announcedAt < askedAt ? null : previous))
      return { ...answer, isAsked: true }
    },
    [isAsked, recheckCount],
    { refresh: false },
  )
  // What the rail's own ask last answered; a failed recheck leaves the last
  // answer in data, so a row the person already had does not vanish for one
  // lost request.
  const known = useMemo<HerdrPresence | null>(() => {
    if (!data?.isAsked) return null
    const list = data.ListAgentHerdrSessions
    return {
      isPresent: list.computerNames.length > 0,
      askingCount: list.sessions.filter((session) => session.herdrSessionState === 'asking').length,
    }
  }, [data])
  if (!isAsked) return { isPresent: false, askingCount: 0 }
  if (known === null) return { isPresent: error ? false : null, askingCount: 0 }
  return { isPresent: known.isPresent, askingCount: announced ? announced.askingCount : known.askingCount }
}
