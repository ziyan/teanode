import { useEffect, useRef } from 'react'
import { useNavigate } from 'react-router-dom'

// Where in the dashboard the agent may send somebody, checked here as well
// as on the server: the text a link or an event carries is a model's, so it
// is read as a path of this dashboard or not at all.

// A page of the agent's memory as the graph names it: lowercase letters and
// digits in runs joined by one dash, at most eight segments deep (see
// models.ValidPath and models.Slug).
const MEMORY_SEGMENT = /^[\p{L}\p{N}]+(?:-[\p{L}\p{N}]+)*$/u

// memoryPath is the Knowledge page a memory:PATH link opens, PATH#N for one
// fact of it, or null for anything that is not a page the graph could hold.
// The fact's number is accepted and not carried: the page opens as a whole.
export function memoryPath(target: string): string | null {
  const [path, fact, ...rest] = target.split('#')
  if (rest.length > 0 || (fact !== undefined && !/^[0-9]{1,6}$/.test(fact))) return null
  if (!path || path.length > 500) return null
  const segments = path.split('/')
  if (segments.length > 8) return null
  for (const segment of segments) {
    if (!MEMORY_SEGMENT.test(segment) || segment !== segment.toLowerCase() || [...segment].length > 60) return null
  }
  return `/knowledge/${path}`
}

// mailPath is the message a mail:ITEM_ID link opens, or null. Starred opens
// any item by id whichever folder it is in.
export function mailPath(itemId: string): string | null {
  return /^[A-Za-z0-9]+$/.test(itemId) ? `/mailbox/starred/${itemId}` : null
}

// FINANCE_LINK_PATH is the page Plaid's window runs on. The server gives
// this path alone a policy that lets Plaid in, and a policy comes with the
// document, so the page is only ever loaded at it (in a window of its own),
// never reached by moving within the dashboard. The server, the command line
// and the finance tool hold the same path.
export const FINANCE_LINK_PATH = '/finance/link'

// The parts of the dashboard the agent may take the person to: their own
// pages, never an operator's (internal/agent/tools/openpage holds the same
// list).
const SHOWN_PREFIXES = ['/knowledge', '/mailbox', '/settings/agent', '/settings', '/finance']

// shownPath is a path the agent asked the dashboard to show, or null: under
// one of the prefixes above, in segments of letters, digits and -._~, so no
// scheme, host, query, '..' or '//' reaches the router.
export function shownPath(written: string): string | null {
  if (!written.startsWith('/') || written.length > 600) return null
  const path = written.length > 1 && written.endsWith('/') ? written.slice(0, -1) : written
  const segments = path.slice(1).split('/')
  for (const segment of segments) {
    if (segment === '' || segment === '.' || segment === '..' || !/^[\p{L}\p{N}\-._~]+$/u.test(segment)) return null
  }
  if (path === FINANCE_LINK_PATH) return null
  return SHOWN_PREFIXES.some((prefix) => path === prefix || path.startsWith(`${prefix}/`)) ? path : null
}

// What useShowPage reads of a run's event.
interface ShowPageEvent {
  kind: string
  runId: string
  sequence: number
  text?: string
}

// useShowPage is what the drawer does with a navigate event: the dashboard
// goes to the page, and whoever drew the drawer is told it is leaving (on a
// phone the drawer covers the page, so it is put away, as a link out of it
// does). Each event moves the dashboard once: the events of a turn in flight
// are replayed when the drawer opens or its socket comes back, and a replay
// must not take the person back to a page they have since left.
//
// A tab in the background is not where the person is looking, so it does
// not move then; but they may have asked here and looked away while the
// agent answered, so the page is kept and shown when they come back to the
// tab, if they come back within showPageWaitMS. Later than that, what they
// are doing now matters more than what they asked a while ago.
const showPageWaitMS = 2 * 60 * 1000

export function useShowPage(leaving: () => void): (event: ShowPageEvent) => void {
  const navigate = useNavigate()
  const shown = useRef(new Set<string>())
  const waiting = useRef<{ path: string; askedAt: number } | null>(null)
  const leavingRef = useRef(leaving)
  leavingRef.current = leaving

  useEffect(() => {
    if (typeof document === 'undefined') return
    const onVisible = () => {
      const pending = waiting.current
      if (document.visibilityState !== 'visible' || !pending) return
      waiting.current = null
      if (Date.now() - pending.askedAt > showPageWaitMS) return
      navigate(pending.path)
      leavingRef.current()
    }
    document.addEventListener('visibilitychange', onVisible)
    return () => document.removeEventListener('visibilitychange', onVisible)
  }, [navigate])

  return (event) => {
    if (event.kind !== 'navigate') return
    const key = `${event.runId}-${event.sequence}`
    if (shown.current.has(key)) return
    shown.current.add(key)
    const path = shownPath(event.text ?? '')
    if (!path) return
    if (typeof document !== 'undefined' && document.visibilityState === 'hidden') {
      waiting.current = { path, askedAt: Date.now() }
      return
    }
    navigate(path)
    leaving()
  }
}
