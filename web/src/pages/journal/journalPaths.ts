// isDashboardPath says a path the server sent is a page of this dashboard: a
// slash followed by a letter, a digit or one of ._~-, with no backslash. A
// browser reads "//host" and "/\\host" as another site, so those are never
// links.
export function isDashboardPath(path: string): boolean {
  return /^\/[A-Za-z0-9._~-]/.test(path) && !path.includes('\\')
}
