import { useEffect, useRef } from 'react'
import { useNavigate } from 'react-router-dom'

// Where in the dashboard the agent may send somebody, checked here as well
// as on the server: the text a link or an event carries is a model's, so it
// is read as a path of this dashboard or not at all.

// A page of the agent's memory as the graph names it: lowercase letters and
// digits in runs joined by one dash, at most eight segments deep (see
// models.ValidPath and models.Slug).
const MEMORY_SEGMENT = /^[\p{L}\p{N}]+(?:-[\p{L}\p{N}]+)*$/u

// A username a link may name as the owner of a shared page: letters, digits
// and -._~ alone, the characters that need no escaping in an address (the
// server's ownerPattern in models/dashboard_link.go).
const OWNER_USERNAME = /^[A-Za-z0-9._~-]{1,64}$/

// SHARED_CITATION is a fact of another person's shared page cited in prose,
// "people/a-grandparent#3 (shared by Ada)": the path and number, and the name
// the page belongs to. The agent writes it that way when it cites what it read
// in a page somebody shared, and a link to the page is [name](memory:PATH#N@username).
export const SHARED_CITATION = /([a-z0-9][a-z0-9-]*(?:\/[a-z0-9][a-z0-9-]*)*)#(\d{1,6}) \(shared by ([^)\n]+)\)/g

// hasSharedCitation is whether the words cite a fact of a shared page.
export function hasSharedCitation(text: string): boolean {
  return new RegExp(SHARED_CITATION.source).test(text)
}

// linkSharedCitations turns each "people/a-grandparent#3 (shared by Ada)" in
// prose into a link to the shared page, [that](memory:people/a-grandparent#3@ada).
// The words name the owner by display name, so usernameOf finds the username
// from the pages shared with the person; a name it cannot place, or places on
// more than one person, is left as the words it was. A citation the agent
// already made a link of is left alone.
export function linkSharedCitations(text: string, usernameOf: (ownerName: string) => string | undefined): string {
  return text.replace(
    new RegExp(SHARED_CITATION.source, 'g'),
    (written: string, path: string, number: string, ownerName: string, offset: number, whole: string) => {
      if (whole[offset - 1] === '[' || whole[offset + written.length] === ']') return written
      const username = usernameOf(ownerName.trim())
      if (!username || !OWNER_USERNAME.test(username) || /[[\]]/.test(written)) return written
      return `[${written}](memory:${path}#${number}@${username})`
    },
  )
}

// memoryPath is the Knowledge page a memory:PATH link opens, PATH#N for one
// fact of it, or null for anything that is not a page the graph could hold.
// The fact's number is accepted and not carried: the page opens as a whole.
//
// PATH#N@username, the username last, is a page another person shared with
// the person, and opens it read only with ?owner=username.
export function memoryPath(written: string): string | null {
  const [target, owner, ...others] = written.split('@')
  if (others.length > 0 || (owner !== undefined && !OWNER_USERNAME.test(owner))) return null
  const [path, fact, ...rest] = target.split('#')
  if (rest.length > 0 || (fact !== undefined && !/^[0-9]{1,6}$/.test(fact))) return null
  if (!path || path.length > 500) return null
  const segments = path.split('/')
  if (segments.length > 8) return null
  for (const segment of segments) {
    if (!MEMORY_SEGMENT.test(segment) || segment !== segment.toLowerCase() || [...segment].length > 60) return null
  }
  return `/knowledge/${path}${owner === undefined ? '' : `?owner=${encodeURIComponent(owner)}`}`
}

// mailPath is the message a mail:ITEM_ID link opens, or null. Starred opens
// any item by id whichever folder it is in.
export function mailPath(itemId: string): string | null {
  return /^[A-Za-z0-9]+$/.test(itemId) ? `/mailbox/starred/${itemId}` : null
}

// CONNECT_PATH is the page that shows the code a phone scans to connect.
export const CONNECT_PATH = '/connect'

// FINANCE_LINK_PATH is the page Plaid's window runs on. The server gives
// this path alone a policy that lets Plaid in, and a policy comes with the
// document, so the page is only ever loaded at it (in a window of its own),
// never reached by moving within the dashboard. The server, the command line
// and the finance tool hold the same path.
export const FINANCE_LINK_PATH = '/finance/link'

// The parts of the dashboard the agent may take the person to: their own
// pages, never an operator's (internal/agent/tools/openpage holds the same
// list).
const HEALTH_SETTINGS_PATH = '/health?tab=settings'

const SHOWN_PREFIXES = ['/knowledge', '/mailbox', '/settings/agent', '/settings', '/finance', '/location', '/health']

// shownPath is a path the agent asked the dashboard to show, or null: under
// one of the prefixes above, in segments of letters, digits and -._~, so no
// scheme, host, query, '..' or '//' reaches the router.
export function shownPath(written: string): string | null {
  if (!written.startsWith('/') || written.length > 600) return null
  // The Health page's settings, where the person lets the agent read their
  // Health data: the agent's health tool opens it while that is off.
  if (written === HEALTH_SETTINGS_PATH) return written
  // The one query a path of ours carries: whose shared page a Knowledge
  // address opens (memoryPath). Checked here, apart from the segments.
  const [withoutQuery, owner, ...others] = written.split('?owner=')
  if (
    others.length > 0 ||
    (owner !== undefined && (!OWNER_USERNAME.test(owner) || !withoutQuery.startsWith('/knowledge/')))
  ) {
    return null
  }
  const shown = shownSegments(withoutQuery)
  return shown === null || owner === undefined ? shown : `${shown}?owner=${owner}`
}

function shownSegments(written: string): string | null {
  const path = written.length > 1 && written.endsWith('/') ? written.slice(0, -1) : written
  const segments = path.slice(1).split('/')
  for (const segment of segments) {
    if (segment === '' || segment === '.' || segment === '..' || !/^[\p{L}\p{N}\-._~]+$/u.test(segment)) return null
  }
  if (path === FINANCE_LINK_PATH) return null
  // The page with a code to scan that connects the phone app: the agent
  // takes the person there when asked to connect a phone, and never holds
  // the code itself (internal/agent/tools/phone).
  if (path === CONNECT_PATH) return path
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
