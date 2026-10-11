// newRandomId names something this browser made, so it can be told from
// its siblings. crypto.randomUUID exists only in a secure context, and a
// dashboard reached over plain HTTP at an address other than localhost has
// none, so there the name comes from the clock and Math.random instead,
// beginning with fallbackPrefix.
export function newRandomId(fallbackPrefix: string): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') return crypto.randomUUID()
  return `${fallbackPrefix}-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`
}
