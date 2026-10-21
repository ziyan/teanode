// Which relying party ids a page may sign a passkey for. It is apart from
// hosts.ts because it carries the public suffix list (tldts, about a hundred
// kilobytes), which the content script that runs in every page, and which
// imports hosts.ts, has no use for.

import { parse } from 'tldts'
import { hostOf, isInternetAddress, isWebPage } from './hosts'

// isPublicSuffix says whether a domain is a name under which anybody can
// register a site of their own, so that a passkey for it would be a passkey
// for every site below it: a top level name, a country code second level, and
// a hosting service's own name. It reads the public suffix list the browser
// carries, the private section included (tldts, with allowPrivateDomains).
function isPublicSuffix(domain: string): boolean {
  if (!domain.includes('.')) return true
  return parse(domain, { allowPrivateDomains: true }).publicSuffix === domain
}

// isRelyingPartyOfHost says whether a passkey for the relying party id may be
// made or used on a page: the id is the page's host itself, or a domain above
// it that is not a suffix anybody can register under. "example.com" is the
// relying party of "login.example.com"; a bare top level name, a country code
// second level and "example.com.evil.test" are not the relying party of
// anything they are not. An address is not a relying party at all, and has no
// domain above it: the one exception is the loopback address a local test page
// uses ("localhost" is a name, and passes as the host itself).
export function isRelyingPartyOfHost(relyingPartyId: string, pageUrl: string): boolean {
  const relyingParty = relyingPartyId.trim().toLowerCase().replace(/\.+$/, '')
  if (!isWebPage(pageUrl)) return false
  const host = hostOf(pageUrl, false)
  if (relyingParty === '' || host === '') return false
  if (isInternetAddress(relyingParty) || isInternetAddress(host)) {
    return relyingParty === host && host === '127.0.0.1'
  }
  if (relyingParty === host) return true
  if (!host.endsWith('.' + relyingParty)) return false
  return !isPublicSuffix(relyingParty)
}
