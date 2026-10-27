// The search across everything: which areas it covers and in what order, and
// where each area's page takes the words. The order is the server's
// (internal/search), so a group never jumps when a result arrives.
export const SEARCH_AREAS = [
  'chats',
  'mail',
  'contacts',
  'photos',
  'memory',
  'documents',
  'recordings',
  'places',
  'finance',
] as const

export type SearchArea = (typeof SEARCH_AREAS)[number]

export function isSearchArea(value: string): value is SearchArea {
  return (SEARCH_AREAS as readonly string[]).includes(value)
}

// Asked by anything that wants the search box at the top of the rail focused:
// the keys, in NavigationShortcuts.
export const FOCUS_SEARCH_EVENT = 'teanode:focus-search'

export function focusSearch() {
  window.dispatchEvent(new Event(FOCUS_SEARCH_EVENT))
}

// The results page for some words. The words are in the address, so a
// reload, Back and a copied link all land on the same results.
export function searchPath(words: string): string {
  return `/search?${new URLSearchParams({ q: words }).toString()}`
}

function pathWithParameters(path: string, parameters: Record<string, string>): string {
  return `${path}?${new URLSearchParams(parameters).toString()}`
}

// showAllPath is the area's own page, searching for the words in the
// parameter that page reads them from.
export function showAllPath(area: SearchArea, words: string): string {
  switch (area) {
    case 'chats':
      return pathWithParameters('/chat', { search: words })
    // Without a folder: the mailbox opens on its inbox and keeps the words,
    // and everywhere searches every folder, as the results did.
    case 'mail':
      return pathWithParameters('/mailbox', { q: words, everywhere: 'true' })
    case 'contacts':
      return pathWithParameters('/contacts', { search: words })
    case 'photos':
      return pathWithParameters('/photos', { search: words })
    case 'memory':
      return pathWithParameters('/knowledge', { search: words })
    case 'documents':
      return pathWithParameters('/knowledge', { documents: words })
    case 'recordings':
      return pathWithParameters('/recordings', { search: words })
    case 'places':
      return pathWithParameters('/location', { tab: 'places', placesQuery: words })
    case 'finance':
      // The Transactions section's own words parameter (financeFilters.ts),
      // written here rather than imported: the rail reads this file, and the
      // finance modules are not part of the first download.
      return pathWithParameters('/finance/transactions', { text: words })
  }
}

function encodedSegments(path: string): string {
  return path
    .split('/')
    .filter((segment) => segment !== '')
    .map(encodeURIComponent)
    .join('/')
}

// resultPath is where one result opens, or null when the result needs a
// lookup first (a mail thread is opened in its folder, which the result does
// not say). A document has no page of its own: it is found in the
// documents search of Knowledge, so it opens that with the words.
export function resultPath(area: SearchArea, targetId: string, words: string): string | null {
  switch (area) {
    case 'chats':
      return pathWithParameters('/chat', { conversation: targetId })
    case 'mail':
      return null
    case 'contacts':
      return pathWithParameters('/contacts', { contact: targetId })
    case 'photos':
      return `/photos/${encodeURIComponent(targetId)}`
    case 'memory':
      return `/knowledge/${encodedSegments(targetId)}`
    case 'documents':
      return showAllPath('documents', words)
    case 'recordings':
      return `/recordings/${encodeURIComponent(targetId)}`
    case 'places':
      return pathWithParameters('/location', { tab: 'places', place: targetId })
    case 'finance':
      return pathWithParameters('/finance/transactions', { transaction: targetId })
  }
}

export type SearchResult = {
  searchArea: string
  targetId: string
  titleText: string
  detailText: string
  happenedAt?: string | null
  // The person's main conversation, which a client names in the person's
  // language, as it does in its list of conversations.
  isMain?: boolean
}

export type SearchAreaResults = {
  searchArea: string
  results: SearchResult[]
  hasMore: boolean
}

export type SearchEverythingAnswer = {
  words: string
  areas: SearchAreaResults[]
  failedAreas: string[]
}

// How many results an area shows before "Show all".
export const SEARCH_PER_AREA_LIMIT = 5

export const SEARCH_EVERYTHING = `
  query ($words: String!, $perAreaLimit: Int) {
    SearchEverything(words: $words, perAreaLimit: $perAreaLimit) {
      words
      areas {
        searchArea
        results { searchArea targetId titleText detailText happenedAt isMain }
        hasMore
      }
      failedAreas
    }
  }`

export const FIND_MAIL_FOLDER = `query ($itemId: String!) { GetMailboxItem(itemId: $itemId) { id folderId } }`
