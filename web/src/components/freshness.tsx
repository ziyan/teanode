import { useEffect, useState } from 'react'

import { graphql } from '../api'

// Two things this dashboard should notice about the server it is talking to,
// and neither of them arrives on its own: that the server has been upgraded
// under it, so the page in the browser is older than the one the server now
// serves; and that a release exists that nobody has installed yet.
//
// Both are polled, quietly, on a schedule that costs nothing: the first is one
// small query, and the second answers from what the server already knows
// rather than asking the release list.

const STATUS = `{ GetServerStatus { version commit } }`
const UPGRADE = `{ GetUpgrade(check: false) { available } }`

// How often to look. A minute: the page is left open for hours and neither of
// these is urgent to the second.
const INTERVAL = 60_000

// useStaleBundle reports whether the server is running a different build than
// the one that served this page.
//
// The dashboard is compiled into the binary, so the server's commit and the
// bundle in the browser are the same thing: when the commit changes, the page
// is the old one. Which matters because the old page talks to the new server —
// usually fine, and occasionally a field that no longer exists.
export function useStaleBundle(): boolean {
  const [stale, setStale] = useState(false)

  useEffect(() => {
    let loaded = ''
    let cancelled = false

    async function look() {
      try {
        const status = (await graphql<{ GetServerStatus: { commit: string } }>(STATUS)).GetServerStatus
        if (cancelled || !status?.commit) {
          return
        }
        if (loaded === '') {
          loaded = status.commit
          return
        }
        if (status.commit !== loaded) {
          setStale(true)
        }
      } catch (caught) {
        // A server that is restarting answers nothing, which is exactly when
        // this is likely to change. It is asked again in a minute.
        void caught
      }
    }

    return poll(look, () => {
      cancelled = true
    })
  }, [])

  return stale
}

// poll runs look now, then on the interval, and only while somebody is looking
// at the page.
//
// The same rule useQuery follows, and for the same reason: a dashboard left
// open in a forgotten tab should not keep asking the server questions nobody
// is listening to, and each of these is a database transaction. Coming back to
// the tab asks immediately, because that is when the answer is most likely to
// have changed.
function poll(look: () => void, cancel: () => void): () => void {
  void look()

  const timer = window.setInterval(() => {
    if (document.visibilityState === 'visible') {
      look()
    }
  }, INTERVAL)

  const onVisible = () => {
    if (document.visibilityState === 'visible') {
      look()
    }
  }
  document.addEventListener('visibilitychange', onVisible)

  return () => {
    cancel()
    window.clearInterval(timer)
    document.removeEventListener('visibilitychange', onVisible)
  }
}

// useUpgradeAvailable reports whether the server knows of a release newer than
// the one it is running.
//
// Read from what the server already found on its own schedule; this never asks
// it to go and look. A dot in the rail is not worth a request to somebody
// else's endpoint.
export function useUpgradeAvailable(): boolean {
  const [available, setAvailable] = useState(false)

  useEffect(() => {
    let cancelled = false

    async function look() {
      try {
        const upgrade = (await graphql<{ GetUpgrade: { available: boolean } }>(UPGRADE)).GetUpgrade
        if (!cancelled) {
          setAvailable(Boolean(upgrade?.available))
        }
      } catch (caught) {
        // Not every server has this — an older one, or one that was built
        // without it — and a dot is not worth an error.
        void caught
      }
    }

    return poll(look, () => {
      cancelled = true
    })
  }, [])

  return available
}
