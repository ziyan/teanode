import { useEffect, useState } from 'react'

import { graphql } from '../api'
import { useQuery } from './useQuery'

// Whether any of the person's computers reports herdr, asked by the rail
// to decide whether to show the Coding row. On its own, apart from
// herdrSessions, because the rail is in the first download and must not bring
// the Coding page into it. Asks only for the computers' names, and again when
// the page comes back into view after a while, so a computer attached later
// shows the row without a reload.

// The least time between two asks caused by the page coming back into view.
const RECHECK_MS = 5 * 60_000

const HERDR_PRESENCE = `query { ListAgentHerdrSessions { computerNames } }`

// useHerdrPresence says whether a computer reports herdr: null while it is
// being asked, and false when it is not asked or the server cannot say.
export function useHerdrPresence(isAsked: boolean): boolean | null {
  const [askCount, setAskCount] = useState(0)
  useEffect(() => {
    if (!isAsked) return
    let askedAt = Date.now()
    const onVisible = () => {
      if (document.visibilityState !== 'visible' || Date.now() - askedAt < RECHECK_MS) return
      askedAt = Date.now()
      setAskCount((count) => count + 1)
    }
    document.addEventListener('visibilitychange', onVisible)
    window.addEventListener('focus', onVisible)
    return () => {
      document.removeEventListener('visibilitychange', onVisible)
      window.removeEventListener('focus', onVisible)
    }
  }, [isAsked])
  const { data, error } = useQuery(
    async () =>
      isAsked
        ? { ...(await graphql<{ ListAgentHerdrSessions: { computerNames: string[] } }>(HERDR_PRESENCE)), isAsked: true }
        : { ListAgentHerdrSessions: { computerNames: [] as string[] }, isAsked: false },
    [isAsked, askCount],
    { refresh: false },
  )
  if (!isAsked || error) return false
  if (!data?.isAsked) return null
  return data.ListAgentHerdrSessions.computerNames.length > 0
}
