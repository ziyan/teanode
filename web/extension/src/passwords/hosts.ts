// Which saved sites a page belongs to, and the facts about a page that a
// passkey needs (relyingParty.ts decides which relying party ids a page may
// sign for). The site rule is the one internal/passwords/sitematch.go follows
// (IsOriginOfSites), so that the dashboard, the iPhone, the agent and this
// extension agree on what a site is: the same host with "www." ignored, the
// same port, and nothing looser. The shared cases in
// internal/passwords/testdata/sitematch.json are run against both. The rules
// here are the whole of the extension's host checking: the content script, the
// service worker and the agent's fill all call them.

// parseSite reads a saved site or a page's address into the scheme, host and
// port that decide whether they are the same place. It is null for anything
// that is not an http or https address with a host and a usable port.
//
// A site that names a scheme is read by the browser's own address parser
// (new URL), so that "https://example.com\@evil.test/" and the like mean to
// this code what they mean to the browser, never something else. A site typed
// without a scheme is read as if it began with https://. A backslash or a
// control character anywhere is refused rather than interpreted: those are
// where parsers differ, and no saved site needs one.
interface ParsedSite {
  scheme: 'http' | 'https'
  // host is lower case, without a trailing dot and with its leading "www."
  // kept, so that a relying party id can be compared to it exactly.
  host: string
  // port is the port the address names, or the default of its scheme.
  port: number
}

// hasBackslashOrControl is true for a backslash and for an ASCII control
// character, including a delete.
function hasBackslashOrControl(text: string): boolean {
  for (let index = 0; index < text.length; index++) {
    const code = text.charCodeAt(index)
    if (code === 0x5c || code < 0x20 || code === 0x7f) return true
  }
  return false
}
const SCHEME_WITH_SLASHES_PATTERN = /^[a-z][a-z0-9+.-]*:\/\//i
// A scheme that is not followed by slashes ("javascript:", "about:blank",
// "mailto:"), which is not a host and a port ("localhost:8080").
const SCHEME_WITHOUT_SLASHES_PATTERN = /^[a-z][a-z0-9+.-]*:(?!\d+(?:[/?#]|$))/i

// withoutTrailingDots drops the dots at the end of a host. A loop from the
// end, because a pattern such as /\.+$/ tries every start in a long run of
// dots and takes time that grows with the square of the run in some engines.
function withoutTrailingDots(host: string): string {
  let end = host.length
  while (end > 0 && host.charCodeAt(end - 1) === 0x2e) end--
  return host.slice(0, end)
}

function parseSite(site: string): ParsedSite | null {
  const text = site.trim()
  if (text === '' || hasBackslashOrControl(text)) return null
  let address: string
  if (SCHEME_WITH_SLASHES_PATTERN.test(text)) address = text
  else if (text.startsWith('//')) address = 'https:' + text
  else if (SCHEME_WITHOUT_SLASHES_PATTERN.test(text)) return null
  else address = 'https://' + text
  let url: URL
  try {
    url = new URL(address)
  } catch {
    return null
  }
  if (url.protocol !== 'http:' && url.protocol !== 'https:') return null
  const scheme = url.protocol === 'https:' ? 'https' : 'http'
  const host = withoutTrailingDots(url.hostname.toLowerCase())
  if (host === '') return null
  const port = url.port === '' ? (scheme === 'https' ? 443 : 80) : Number(url.port)
  if (!Number.isInteger(port) || port < 1 || port > 65535) return null
  return { scheme, host, port }
}

// withoutWWW drops a leading "www.", unless nothing but a top level name
// would be left.
function withoutWWW(host: string): string {
  return host.startsWith('www.') && host.slice(4).includes('.') ? host.slice(4) : host
}

// hostOf is the host of a site or a page, with its leading "www." dropped
// when shouldDropWWW says so. It is empty when the address is not one
// parseSite reads.
export function hostOf(site: string, shouldDropWWW: boolean): string {
  const parsed = parseSite(site)
  if (parsed === null) return ''
  return shouldDropWWW ? withoutWWW(parsed.host) : parsed.host
}

// normalizeHost is the host of a site or a page for matching a saved site.
export function normalizeHost(site: string): string {
  return hostOf(site, true)
}

// isWebPage says whether the address is a page the extension may fill or
// offer on: http or https, with a host.
export function isWebPage(pageUrl: string): boolean {
  return /^https?:\/\//i.test(pageUrl.trim()) && parseSite(pageUrl) !== null
}

// doesSiteMatchPage says whether a saved site is the page's origin, the way
// IsOriginOfSites in internal/passwords/sitematch.go does, and the shared
// cases in internal/passwords/testdata/sitematch.json check it: the same host
// ("www." ignored), and the same port. A site that names a port matches only
// that port; one that names none matches only the default port of its own
// scheme (443 for a site saved without a scheme or as https). A page on http
// matches only a site saved as http, so a password saved for a secure site is
// never typed into its plain-text twin.
export function doesSiteMatchPage(site: string, pageUrl: string): boolean {
  if (!isWebPage(pageUrl)) return false
  const page = parseSite(pageUrl)
  const saved = parseSite(site)
  if (page === null || saved === null) return false
  if (withoutWWW(page.host) !== withoutWWW(saved.host)) return false
  if (page.scheme === 'http' && saved.scheme !== 'http') return false
  return page.port === saved.port
}

export function doSitesMatchPage(sites: readonly string[], pageUrl: string): boolean {
  return sites.some((site) => doesSiteMatchPage(site, pageUrl))
}

// originOf is the origin (scheme, host and port) of a web page, or an empty
// text when the address is not one. Two addresses with the same origin are
// the same place for filling.
export function originOf(pageUrl: string): string {
  if (!isWebPage(pageUrl)) return ''
  try {
    return new URL(pageUrl.trim()).origin
  } catch {
    return ''
  }
}

// originOfSite is the origin a saved site stands for, written the way a page's
// origin is (originOf): the scheme it names, or https where it names none, the
// host as saved, and the port only where it is not the scheme's own. It is
// empty for a site that is not an http or https address.
export function originOfSite(site: string): string {
  const parsed = parseSite(site)
  if (parsed === null) return ''
  const isDefaultPort = parsed.port === (parsed.scheme === 'https' ? 443 : 80)
  return `${parsed.scheme}://${parsed.host}${isDefaultPort ? '' : `:${parsed.port}`}`
}

// doesHostMatch is the check an agent fill makes: the tab's address against
// the host the server named, both read the same way.
export function doesHostMatch(pageUrl: string, host: string): boolean {
  const pageHost = normalizeHost(pageUrl)
  return isWebPage(pageUrl) && pageHost !== '' && pageHost === normalizeHost(host)
}

export function isInternetAddress(host: string): boolean {
  if (host.startsWith('[')) return true
  const parts = host.split('.')
  return parts.length === 4 && parts.every((part) => /^\d+$/.test(part))
}

// isSecureContextPage says whether WebAuthn works on the page: https, or a
// local development address on http.
export function isSecureContextPage(pageUrl: string): boolean {
  if (/^\s*https:\/\//i.test(pageUrl)) return normalizeHost(pageUrl) !== ''
  const host = hostOf(pageUrl, false)
  return /^\s*http:\/\//i.test(pageUrl) && (host === 'localhost' || host.endsWith('.localhost') || host === '127.0.0.1')
}

// effectiveDomain is the host a relying party id defaults to when a page does
// not name one: the page's own host, lower case and without a port ("www."
// is part of it here; only matching a saved site ignores it).
export function effectiveDomain(pageUrl: string): string {
  return hostOf(pageUrl, false)
}
