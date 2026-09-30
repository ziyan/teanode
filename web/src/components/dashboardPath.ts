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
