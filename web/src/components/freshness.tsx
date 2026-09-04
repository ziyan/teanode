import { useEffect, useState } from 'react'

import { graphql } from '../api'

// Two things this dashboard should notice about the server it is talking to,
// and neither of them arrives on its own: that the server has been upgraded
// under it, so the page in the browser is older than the one the server now
// serves; and that a release exists that nobody has installed yet.
//
// One query and one timer for both. They were a hook each, which meant two
// requests a minute from every open tab on every page — the rail renders
// everywhere — and each of them is a database transaction on the other end,
// for one dot and one button.

const FRESHNESS = `{
  GetServerStatus { commit }
  GetUpgrade(check: false) { available }
}`

// How often to look. A minute: the page is left open for hours and neither of
// these is urgent to the second.
const INTERVAL = 60_000

export type Freshness = {
  // Whether the server is running a different build than the one that served
  // this page.
  //
  // The dashboard is compiled into the binary, so the server's commit and the
  // bundle in the browser are the same thing: when the commit changes, the
  // page is the old one. Which matters because the old page talks to the new
  // server — usually fine, and occasionally a field that no longer exists.
  staleBundle: boolean

  // Whether the server knows of a release newer than the one it is running.
  // Read from what it already found on its own schedule; this never asks it
  // to go and look. A dot in the rail is not worth a request to somebody
  // else's endpoint.
  upgradeAvailable: boolean
}

export function useFreshness(): Freshness {
  const [freshness, setFreshness] = useState<Freshness>({
    staleBundle: false,
    upgradeAvailable: false,
  })

  useEffect(() => {
    let loaded = ''
    let cancelled = false

    async function look() {
      let answer: { GetServerStatus?: { commit: string }; GetUpgrade?: { available: boolean } }
      try {
        answer = await graphql(FRESHNESS)
      } catch (caught) {
        // A server that is restarting answers nothing, which is exactly when
        // this is likely to change. Not every server has the upgrade query
        // either — an older one, or one built without it — and a dot is not
        // worth an error. Asked again in a minute.
        void caught
        return
      }
      if (cancelled) {
        return
      }

      const commit = answer.GetServerStatus?.commit
      // The first answer is the baseline: this page was served by whatever
      // was running then.
      const stale = Boolean(commit && loaded && commit !== loaded)
      if (commit && !loaded) {
        loaded = commit
      }
      setFreshness({
        staleBundle: stale,
        upgradeAvailable: Boolean(answer.GetUpgrade?.available),
      })
    }

    void look()

    // Only while somebody is looking at the page. The same rule useQuery
    // follows, and for the same reason: a dashboard left open in a forgotten
    // tab should not keep asking the server questions nobody is listening to.
    const timer = window.setInterval(() => {
      if (document.visibilityState === 'visible') {
        void look()
      }
    }, INTERVAL)

    // Coming back to the tab is when the answer is most likely to have
    // changed, and waiting out the rest of the interval is the wrong answer.
    const onVisible = () => {
      if (document.visibilityState === 'visible') {
        void look()
      }
    }
    document.addEventListener('visibilitychange', onVisible)

    return () => {
      cancelled = true
      window.clearInterval(timer)
      document.removeEventListener('visibilitychange', onVisible)
    }
  }, [])

  return freshness
}
