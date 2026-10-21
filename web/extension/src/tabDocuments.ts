// Which document each of the agent's tabs shows, and which of them the agent
// filled a password or a one-time code into (background.js uses this).
//
// The server locks a tab it filled until that tab commits a new document, so
// the extension tells it every time one of the agent's tabs commits a main
// frame document (chrome.webNavigation.onCommitted, main frame only: a
// history.pushState or a change of the fragment is not a commit). Each report
// carries the document's id, its address, whether it was a move back or
// forward through the history, and the tab's running count of commits, which
// orders reports that arrive late. A fill answers with the same count and the
// id of the document it filled.
//
// Here the same record also decides what the extension keeps for cdp_events: a
// tab filled in this document keeps nothing the page does until the tab shows
// another document, and no request body is ever kept.

export interface CommitReport {
  tab: number
  documentId: string
  url: string
  isBackForward: boolean
  count: number
}

export interface CommitDetails {
  tabId: number
  frameId: number
  documentId?: string
  url: string
  transitionQualifiers?: string[]
}

// reportsKept is the most reports held while the server cannot be told.
const reportsKept = 100

// commitOriginsKept is how many of a tab's latest commits are remembered by
// the site they were on, to name the sites a script may have run on while the
// tab moved (see TabDocuments.originsSince).
const commitOriginsKept = 20

// SCRIPTED_ORIGINS_KEY is where the list of scripted sites is kept, in
// chrome.storage.local: an array of origin strings, nothing else. The options
// page reads the same key to show the list.
export const SCRIPTED_ORIGINS_KEY = 'scriptedOrigins'

// originOf is the site an address is on, written the one way sites are compared
// here and in the server (internal/browser/filled.go, OriginOf): scheme, lower
// case host without a leading "www.", and the port when it is not the scheme's
// own. It is empty for an address that is not an http or https one.
export function originOf(address: unknown): string {
  if (typeof address !== 'string') return ''
  let parsed: URL
  try {
    parsed = new URL(address.trim())
  } catch {
    return ''
  }
  const scheme = parsed.protocol.replace(/:$/, '').toLowerCase()
  if (scheme !== 'http' && scheme !== 'https') return ''
  let host = parsed.hostname.toLowerCase().replace(/\.$/, '')
  if (host === '') return ''
  if (host.startsWith('www.') && host.slice(4).includes('.')) host = host.slice(4)
  const isDefaultPort =
    parsed.port === '' || (scheme === 'https' && parsed.port === '443') || (scheme === 'http' && parsed.port === '80')
  return `${scheme}://${host}${isDefaultPort ? '' : ':' + parsed.port}`
}

// ScriptedOrigins are the sites the agent ran a script on in the person's
// browser. A script leaves things behind that outlive its page (a window it
// opened that reaches back into the site's pages, a worker, a handler on
// another page of the site), so a site on this list is not filled by the agent
// until the person allows it again. Only origins are kept, never an address or
// a value. The list is the extension's record: the server is told it, but the
// extension decides.
export class ScriptedOrigins {
  private origins = new Set<string>()

  // add notes the site an address is on. It says whether that was news.
  add(address: unknown): boolean {
    const origin = originOf(address)
    if (origin === '' || this.origins.has(origin)) return false
    this.origins.add(origin)
    return true
  }

  // has says the agent ran a script on the site the address is on.
  has(address: unknown): boolean {
    const origin = originOf(address)
    return origin !== '' && this.origins.has(origin)
  }

  list(): string[] {
    return [...this.origins].sort()
  }

  clear(): void {
    this.origins.clear()
  }

  // replace puts a stored list in place of this one, ignoring anything that is
  // not an origin string.
  replace(kept: unknown): void {
    this.origins.clear()
    for (const entry of Array.isArray(kept) ? kept : []) this.add(entry)
  }
}

// KeptDocuments is what the class keeps of itself between one run of the
// service worker and the next (chrome.storage.session): ids and counts, never
// an address or a value.
export interface KeptDocuments {
  counts: [number, number][]
  current: [number, string][]
  filled: [number, string[]][]
}

export class TabDocuments {
  private counts = new Map<number, number>()
  private current = new Map<number, string>()
  private filled = new Map<number, Set<string>>()
  private pending: CommitReport[] = []
  private commitOrigins = new Map<number, { count: number; origin: string }[]>()

  // committed notes a navigation that committed. It answers the report to send
  // the server, or null for a frame that is not a tab's main frame and for a
  // tab the agent cannot act in (its addresses are not told to the server).
  committed(details: CommitDetails, isListed: boolean): CommitReport | null {
    if (details.frameId !== 0) return null
    const count = (this.counts.get(details.tabId) ?? 0) + 1
    this.counts.set(details.tabId, count)
    if (details.documentId) this.current.set(details.tabId, details.documentId)
    if (!isListed) return null
    const origin = originOf(details.url)
    if (origin !== '') {
      const recent = this.commitOrigins.get(details.tabId) ?? []
      recent.push({ count, origin })
      if (recent.length > commitOriginsKept) recent.splice(0, recent.length - commitOriginsKept)
      this.commitOrigins.set(details.tabId, recent)
    }
    const report: CommitReport = {
      tab: details.tabId,
      documentId: details.documentId ?? '',
      url: details.url,
      isBackForward: (details.transitionQualifiers ?? []).includes('forward_back'),
      count,
    }
    this.pending.push(report)
    if (this.pending.length > reportsKept) this.pending.splice(0, this.pending.length - reportsKept)
    return report
  }

  // keep is what to save so that a worker started again still knows which
  // document each tab shows and which of them were filled.
  keep(): KeptDocuments {
    return {
      counts: [...this.counts],
      current: [...this.current],
      filled: [...this.filled].map(([tabId, documentIds]) => [tabId, [...documentIds]]),
    }
  }

  // restore puts back what keep saved. A tab already noted in this run keeps
  // what this run knows of it (the saved record is older), and anything that
  // is not in the shape keep makes is left out.
  restore(kept: unknown): void {
    if (typeof kept !== 'object' || kept === null) return
    const record = kept as Partial<KeptDocuments>
    for (const entry of Array.isArray(record.counts) ? record.counts : []) {
      if (Array.isArray(entry) && typeof entry[0] === 'number' && typeof entry[1] === 'number') {
        if (!this.counts.has(entry[0])) this.counts.set(entry[0], entry[1])
      }
    }
    for (const entry of Array.isArray(record.current) ? record.current : []) {
      if (Array.isArray(entry) && typeof entry[0] === 'number' && typeof entry[1] === 'string') {
        if (!this.current.has(entry[0])) this.current.set(entry[0], entry[1])
      }
    }
    for (const entry of Array.isArray(record.filled) ? record.filled : []) {
      if (!Array.isArray(entry) || typeof entry[0] !== 'number' || !Array.isArray(entry[1])) continue
      const documents = this.filled.get(entry[0]) ?? new Set<string>()
      for (const documentId of entry[1]) if (typeof documentId === 'string') documents.add(documentId)
      this.filled.set(entry[0], documents)
    }
  }

  // takeReports hands over the reports not yet sent, oldest first.
  takeReports(): CommitReport[] {
    return this.pending.splice(0)
  }

  // countOf is how many commits the tab has had since the extension started
  // counting, which a fill reports to the server.
  countOf(tabId: number): number {
    return this.counts.get(tabId) ?? 0
  }

  // originsSince are the sites the tab committed documents on after its
  // count of commits was the one given: a script that waits can run in several.
  originsSince(tabId: number, count: number): string[] {
    return (this.commitOrigins.get(tabId) ?? []).filter((entry) => entry.count > count).map((entry) => entry.origin)
  }

  // markFilled notes that a value was put into the tab's document.
  markFilled(tabId: number, documentId: string | undefined): void {
    if (!documentId) return
    this.current.set(tabId, documentId)
    const documents = this.filled.get(tabId) ?? new Set<string>()
    documents.add(documentId)
    this.filled.set(tabId, documents)
  }

  // isShowingFilledDocument says the tab now shows a document a value was put
  // into (a page restored from the history's cache is the same document).
  isShowingFilledDocument(tabId: number): boolean {
    const documentId = this.current.get(tabId)
    return documentId !== undefined && (this.filled.get(tabId)?.has(documentId) ?? false)
  }

  forget(tabId: number): void {
    this.commitOrigins.delete(tabId)
    this.counts.delete(tabId)
    this.current.delete(tabId)
    this.filled.delete(tabId)
  }
}

// withoutRequestBodies is a DevTools event's parameters without the body of
// the request they describe (and without the marker that there is one): a
// form's request is where a typed password travels, and nothing the agent does
// needs a request's body.
export function withoutRequestBodies<T>(params: T): T {
  if (typeof params !== 'object' || params === null) return params
  const record = params as Record<string, unknown>
  const request = record.request
  if (typeof request !== 'object' || request === null) return params
  const kept: Record<string, unknown> = { ...(request as Record<string, unknown>) }
  delete kept.postData
  delete kept.hasPostData
  delete kept.postDataEntries
  return { ...record, request: kept } as T
}
