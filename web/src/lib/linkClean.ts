// Shortening a link for people: the tracking parameters taken out of its query
// string, and the short text shown for a link that has no words of its own.
//
// This is the same work as internal/util/linkclean on the server and
// LinkClean.swift in the iPhone app, done on the link's text rather than on a
// parsed URL, because each language's URL parser normalizes differently. The
// cases in internal/util/linkclean/testdata/links.json are read by the tests of
// all three; a change here is a change there.

export const LINK_DISPLAY_LENGTH = 48
export const LINK_DISPLAY_QUERY_LENGTH = 32

// Query parameters that say where a visitor came from and nothing about what
// the page is. utm_ is a family, matched by prefix.
const TRACKING_PARAMETERS = new Set([
  'gclid',
  'gclsrc',
  'gbraid',
  'wbraid',
  'dclid',
  'fbclid',
  'msclkid',
  'yclid',
  'twclid',
  'ttclid',
  'li_fat_id',
  'igshid',
  'mc_cid',
  'mc_eid',
  '_gl',
  '_ga',
  '_hsenc',
  '_hsmi',
  'mkt_tok',
  'oly_anon_id',
  'oly_enc_id',
  'vero_id',
  'rb_clickid',
  's_cid',
  'ref_src',
])

// The scheme, authority, path, query (question mark included) and fragment
// (number sign included) of a web link.
const LINK_PATTERN = /^(https?):\/\/([^/?#]*)([^?#]*)(\?[^#]*)?(#[\s\S]*)?$/i

function isTrackingParameter(parameter: string): boolean {
  const name = parameter.split('=', 1)[0].toLowerCase()
  return TRACKING_PARAMETERS.has(name) || name.startsWith('utm_')
}

// cleanLink is raw without its tracking parameters. Every other parameter
// stays, as written and in its order. Anything that is not an http or https
// link, and a link with nothing to remove, comes back exactly as given.
export function cleanLink(raw: string): string {
  const parts = LINK_PATTERN.exec(raw)
  if (!parts || !parts[4]) return raw
  const parameters = parts[4].slice(1).split('&')
  if (!parameters.some(isTrackingParameter)) return raw
  // Dropping a tracking parameter rewrites the query, and the empty
  // parameters it leaves (a doubled ampersand) go with it.
  const kept = parameters.filter((parameter) => parameter !== '' && !isTrackingParameter(parameter))
  return `${parts[1]}://${parts[2]}${parts[3]}${kept.length > 0 ? `?${kept.join('&')}` : ''}${parts[5] ?? ''}`
}

// linkDisplayText is the short text for a link that has no words of its own:
// the host without "www.", the path, and the cleaned query only when it is
// short. No scheme, no credentials, no fragment; past LINK_DISPLAY_LENGTH
// characters the middle is cut and replaced with an ellipsis.
export function linkDisplayText(raw: string): string {
  const parts = LINK_PATTERN.exec(cleanLink(raw))
  if (!parts) return shorten(raw)
  let host = parts[2].toLowerCase()
  const at = host.lastIndexOf('@')
  if (at >= 0) host = host.slice(at + 1)
  if (host.startsWith('www.')) host = host.slice(4)
  let path = parts[3]
  try {
    path = decodeURIComponent(path)
  } catch {
    // Not valid percent-encoding, or not UTF-8: shown as written.
  }
  let query = parts[4] ?? ''
  if (Array.from(query).length > LINK_DISPLAY_QUERY_LENGTH) query = ''
  if (path === '/' && query === '') path = ''
  return shorten(host + path + query)
}

// shorten cuts the middle out of text longer than LINK_DISPLAY_LENGTH
// characters, so that both the start, which says where the link goes, and the
// end, which says what it is, stay. Characters are code points, as on the
// server.
function shorten(text: string): string {
  const characters = Array.from(text)
  if (characters.length <= LINK_DISPLAY_LENGTH) return text
  const headLength = Math.floor(LINK_DISPLAY_LENGTH / 2)
  const tailLength = LINK_DISPLAY_LENGTH - headLength - 1
  return characters.slice(0, headLength).join('') + '…' + characters.slice(characters.length - tailLength).join('')
}
