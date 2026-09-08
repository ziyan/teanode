import { useState } from 'react'

// Who a message is from, as a picture.
//
// A sending domain can publish a logo in DNS for exactly this — the standard
// is BIMI, and what it publishes is an SVG on the sender's own server. It is
// fetched by this server rather than by the browser, so that showing one does
// not tell the sender which address opened which message at what moment; the
// page only ever asks this server, and only for mail that proved it came from
// that domain.
//
// Most senders publish nothing, so the ordinary answer is the monogram: the
// first letter of the name, on a colour derived from it. That is stable — the
// same sender is the same colour on every visit — without storing anything.

export function SenderLogo({
  name,
  logoDomain,
  src,
  size = 32,
}: {
  name: string
  // The sending domain whose published mark this server has fetched and
  // cached, for mail that arrived.
  logoDomain?: string
  // Or an address to draw directly, for a mark this server publishes itself
  // and therefore has no cache entry for.
  src?: string
  size?: number
}) {
  // A logo that fails to load falls back to the monogram rather than leaving
  // a broken image: what is cached may have been removed since.
  const [broken, setBroken] = useState(false)
  const letter = firstLetter(name)
  const style = { width: `${size}px`, height: `${size}px` }
  const address = src ?? (logoDomain ? `/api/v1/logo/${encodeURIComponent(logoDomain)}` : '')

  if (address && !broken) {
    return (
      <span className="sender-logo" style={style}>
        <img src={address} alt="" width={size} height={size} onError={() => setBroken(true)} />
      </span>
    )
  }
  return (
    <span
      className="sender-logo monogram"
      style={{ ...style, background: colorOf(name), fontSize: `${Math.round(size * 0.44)}px` }}
      aria-hidden="true"
    >
      {letter}
    </span>
  )
}

// The first letter worth showing: a name written in any script has one, and a
// name that is only punctuation has none, in which case the mark is blank
// rather than a stray bracket.
function firstLetter(name: string): string {
  for (const letter of name.trim()) {
    if (/\p{L}|\p{N}/u.test(letter)) {
      return letter.toUpperCase()
    }
  }
  return ''
}

// A colour from the name, so that the same sender is the same colour every
// time without anything being stored. Fixed saturation and lightness, so
// every one of them sits at the same weight beside the others and none of
// them shouts.
function colorOf(name: string): string {
  let hash = 0
  for (const letter of name) {
    hash = (hash * 31 + letter.codePointAt(0)!) % 360
  }
  return `hsl(${hash} 45% 42%)`
}
