import { useCallback, useEffect, useRef, useState } from 'react'

// A minimal replacement for a data fetching library: run a promise, expose
// what it returned, and refresh it in the background. Six screens do not need
// caching, normalisation or subscriptions.

// How often a list re-reads itself. Long enough not to be chatty, short enough
// that a message which has arrived is on screen before somebody reaches for a
// reload that is not there.
const REFRESH_INTERVAL = 10_000

export function useQuery<T>(run: () => Promise<T>, dependencies: unknown[] = [], options: { refresh?: boolean } = {}) {
  const { refresh = true } = options
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState<unknown>(null)
  const [loading, setLoading] = useState(true)

  // Whether anything has arrived yet. A background refresh must not put the
  // page back into its loading state — the reader is looking at the list, and
  // replacing it with "loading…" every ten seconds would be worse than a
  // stale row.
  const loaded = useRef(false)

  const load = useCallback(
    async (quiet = false) => {
      if (!quiet) {
        setLoading(true)
      }
      try {
        setData(await run())
        setError(null)
        loaded.current = true
      } catch (caught) {
        // A failed background refresh keeps whatever is on screen. The
        // connection dropping for one poll is not a reason to throw away a
        // list somebody is reading.
        if (!quiet || !loaded.current) {
          setError(caught)
        }
      } finally {
        if (!quiet) {
          setLoading(false)
        }
      }
    },
    // The caller controls when this changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    dependencies,
  )

  useEffect(() => {
    loaded.current = false
    void load()
  }, [load])

  useEffect(() => {
    if (!refresh) {
      return
    }

    const timer = window.setInterval(() => {
      // Nothing is read while the tab is in the background, so nothing needs
      // fetching. A dashboard left open in a forgotten tab should not keep
      // asking the server questions nobody is listening to.
      if (document.visibilityState === 'visible') {
        void load(true)
      }
    }, REFRESH_INTERVAL)

    // Coming back to the tab is exactly when the data is most likely stale,
    // and waiting out the rest of the interval is the wrong answer.
    const onVisible = () => {
      if (document.visibilityState === 'visible') {
        void load(true)
      }
    }
    document.addEventListener('visibilitychange', onVisible)

    return () => {
      window.clearInterval(timer)
      document.removeEventListener('visibilitychange', onVisible)
    }
  }, [load, refresh])

  return { data, error, loading, reload: load }
}
