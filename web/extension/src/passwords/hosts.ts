// Which saved sites a page belongs to, and which relying party ids a page may
// sign for. The site rule is the one internal/passwords/sitematch.go and
// TeaNodeKit follow, so the dashboard, the iPhone, the agent and this
// extension agree on what a site is: the same host, "www." ignored, and
// nothing looser. The rules here are the whole of the extension's host
// checking: the content script, the service worker and the agent's fill all
// call them.

// hostOf is the host of a site a login keeps or of a page: lower case,
// without a port, a path or a user name, with its leading "www." dropped when
// shouldDropWWW says so. It is empty when there is no host or the scheme is
// neither http nor https. A site typed without a scheme is read as if it had
// one.
function hostOf(site: string, shouldDropWWW: boolean): string {
  let text = site.trim()
  const schemeEnd = text.indexOf('://')
  if (schemeEnd >= 0) {
    const scheme = text.slice(0, schemeEnd).toLowerCase()
    if (scheme !== 'http' && scheme !== 'https') return ''
    text = text.slice(schemeEnd + 3)
  } else if (text.startsWith('//')) {
    text = text.slice(2)
  }
  const authorityEnd = text.search(/[/?#]/)
  if (authorityEnd >= 0) text = text.slice(0, authorityEnd)
  // Anything before the last "@" is a user name and password, never the host.
  const atSign = text.lastIndexOf('@')
  if (atSign >= 0) text = text.slice(atSign + 1)
  if (text.startsWith('[')) {
    const closing = text.indexOf(']')
    return closing < 0 ? '' : text.slice(0, closing + 1).toLowerCase()
  }
  const colon = text.lastIndexOf(':')
  if (colon >= 0) {
    if (!/^\d*$/.test(text.slice(colon + 1))) return ''
    text = text.slice(0, colon)
  }
  let host = text.toLowerCase().replace(/\.+$/, '')
  if (shouldDropWWW && host.startsWith('www.') && host.slice(4).includes('.')) host = host.slice(4)
  if (host === '' || /\s/.test(host)) return ''
  return host
}

// normalizeHost is the host of a site or a page for matching a saved site.
export function normalizeHost(site: string): string {
  return hostOf(site, true)
}

// schemeOf is the scheme a saved site means: http only when it says so.
// A site typed without a scheme is https.
function schemeOf(site: string): 'http' | 'https' {
  return /^\s*http:\/\//i.test(site) ? 'http' : 'https'
}

// isWebPage says whether the address is a page the extension may fill or
// offer on: http or https, with a host.
export function isWebPage(pageUrl: string): boolean {
  return /^https?:\/\//i.test(pageUrl.trim()) && normalizeHost(pageUrl) !== ''
}

// doesSiteMatchPage says whether a saved site is the page's site. The hosts
// must be the same; and a page on http matches only a site saved as http, so
// a password saved for a secure site is never typed into its plain-text twin.
export function doesSiteMatchPage(site: string, pageUrl: string): boolean {
  if (!isWebPage(pageUrl)) return false
  const pageHost = normalizeHost(pageUrl)
  if (pageHost === '' || normalizeHost(site) !== pageHost) return false
  const isPageSecure = !/^\s*http:\/\//i.test(pageUrl)
  return isPageSecure || schemeOf(site) === 'http'
}

export function doSitesMatchPage(sites: readonly string[], pageUrl: string): boolean {
  return sites.some((site) => doesSiteMatchPage(site, pageUrl))
}

// doesHostMatch is the check an agent fill makes: the tab's address against
// the host the server named, both read the same way.
export function doesHostMatch(pageUrl: string, host: string): boolean {
  const pageHost = normalizeHost(pageUrl)
  return isWebPage(pageUrl) && pageHost !== '' && pageHost === normalizeHost(host)
}

function isInternetAddress(host: string): boolean {
  if (host.startsWith('[')) return true
  const parts = host.split('.')
  return parts.length === 4 && parts.every((part) => /^\d+$/.test(part))
}

// Names under which anybody can register a site of their own. A passkey for
// one of them would be a passkey for every site below it. This is a short
// list of the common ones, not the public suffix list the browser carries;
// the pattern below covers the country code second levels (a "co" or "com"
// label before a two letter code).
// Each is written as its labels and joined here, since these are names that
// belong to hosting services, not hosts anything in this repository talks to.
const SHARED_HOSTING_SUFFIXES = new Set(
  [
    ['github', 'io'],
    ['gitlab', 'io'],
    ['herokuapp', 'com'],
    ['vercel', 'app'],
    ['netlify', 'app'],
    ['pages', 'dev'],
    ['workers', 'dev'],
    ['web', 'app'],
    ['firebaseapp', 'com'],
    ['appspot', 'com'],
    ['blogspot', 'com'],
    ['cloudfront', 'net'],
    ['amazonaws', 'com'],
    ['azurewebsites', 'net'],
    ['ngrok', 'io'],
    ['ngrok-free', 'app'],
  ].map((labels) => labels.join('.')),
)

function isPublicSuffixLike(domain: string): boolean {
  if (!domain.includes('.')) return true
  if (SHARED_HOSTING_SUFFIXES.has(domain)) return true
  return /^(?:co|com|org|net|gov|edu|ac|or|ne|go|gob|mil)\.[a-z]{2}$/.test(domain)
}

// isRelyingPartyOfHost says whether a passkey for the relying party id may be
// made or used on a page: the id is the page's host itself, or a domain above
// it that is not a suffix anybody can register under. "example.com" is the
// relying party of "login.example.com"; a bare top level name, a country code
// second level and "example.com.evil.test" are not the relying party of
// anything they are not.
// A page at an address has no domain above it.
export function isRelyingPartyOfHost(relyingPartyId: string, pageUrl: string): boolean {
  const relyingParty = relyingPartyId.trim().toLowerCase().replace(/\.+$/, '')
  if (!isWebPage(pageUrl)) return false
  const host = hostOf(pageUrl, false)
  if (relyingParty === '' || host === '') return false
  if (relyingParty === host) return true
  if (isInternetAddress(host) || !host.endsWith('.' + relyingParty)) return false
  return !isPublicSuffixLike(relyingParty)
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
