import { useRef } from 'react'
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
  return `/settings/knowledge/${path}`
}

// mailPath is the message a mail:ITEM_ID link opens, or null. Starred opens
// any item by id whichever folder it is in.
export function mailPath(itemId: string): string | null {
  return /^[A-Za-z0-9]+$/.test(itemId) ? `/mailbox/starred/${itemId}` : null
}

// The parts of the dashboard the agent may take the person to: their own
// pages, never an operator's (internal/agent/tools/openpage holds the same
// list).
const SHOWN_PREFIXES = ['/settings/knowledge', '/mailbox', '/settings/agent', '/settings']

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
// must not take the person back to a page they have since left. A tab in
// the background is not where the person is looking, so it stays put.
export function useShowPage(leaving: () => void): (event: ShowPageEvent) => void {
  const navigate = useNavigate()
  const shown = useRef(new Set<string>())
  return (event) => {
    if (event.kind !== 'navigate') return
    const key = `${event.runId}-${event.sequence}`
    if (shown.current.has(key)) return
    shown.current.add(key)
    if (typeof document !== 'undefined' && document.visibilityState === 'hidden') return
    const path = shownPath(event.text ?? '')
    if (!path) return
    navigate(path)
    leaving()
  }
}
