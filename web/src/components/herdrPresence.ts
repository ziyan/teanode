import { useEffect, useRef, useState } from 'react'

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
  // A count the Coding page announced, with the answer it came after: it
  // stands until a newer answer comes.
  const [announced, setAnnounced] = useState<{ askingCount: number; after: number } | null>(null)
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
  // What the rail's own ask last answered, and how many answers it has had,
  // so an announced count can say which answer it came after. Kept through
  // a failed recheck, so a row the person already had does not vanish for
  // one lost request.
  const [known, setKnown] = useState<{ presence: HerdrPresence; answerCount: number } | null>(null)
  const knownRef = useRef(known)
  useEffect(() => {
    knownRef.current = known
  }, [known])
  useEffect(() => {
    const onAnnounced = (event: Event) => {
      const askingCount = (event as CustomEvent<number>).detail
      if (typeof askingCount === 'number') setAnnounced({ askingCount, after: knownRef.current?.answerCount ?? 0 })
    }
    window.addEventListener(ASKING_EVENT, onAnnounced)
    return () => window.removeEventListener(ASKING_EVENT, onAnnounced)
  }, [])
  const { data, error } = useQuery(
    async () =>
      isAsked
        ? { ...(await graphql<HerdrPresenceData>(HERDR_PRESENCE)), isAsked: true }
        : { ListAgentHerdrSessions: { computerNames: [], sessions: [] }, isAsked: false },
    [isAsked, recheckCount],
    { refresh: false },
  )
  useEffect(() => {
    if (!data?.isAsked || error) return
    const list = data.ListAgentHerdrSessions
    const presence = {
      isPresent: list.computerNames.length > 0,
      askingCount: list.sessions.filter((session) => session.herdrSessionState === 'asking').length,
    }
    setKnown((previous) => ({ presence, answerCount: (previous?.answerCount ?? 0) + 1 }))
  }, [data, error])
  if (!isAsked) return { isPresent: false, askingCount: 0 }
  if (known === null) return { isPresent: error ? false : null, askingCount: 0 }
  // A count the Coding page announced since the last answer is the newer one.
  const askingCount =
    announced && announced.after === known.answerCount ? announced.askingCount : known.presence.askingCount
  return { isPresent: known.presence.isPresent, askingCount }
}
