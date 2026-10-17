import { useState } from 'react'

// How many colors a person without a picture can have: --avatar-0 to
// --avatar-11 in style.css. The server and the iPhone app use the same number.
export const AVATAR_COLOR_COUNT = 12

// avatarColorIndexOf is the color for text the server did not pick one for: an
// FNV-1a hash of the UTF-8 bytes modulo the color count, the same function the
// server runs over a user id and the iPhone app carries as AvatarColor.
// Everything that returns a person says which color they are, so this is only
// the fallback for a row about something that has no such field (an agent
// listed by its name), and it is deliberately the same function so a name that
// is also an id lands on the same color everywhere.
export function avatarColorIndexOf(text: string): number {
  let hash = 0x811c9dc5
  for (const byte of new TextEncoder().encode(text)) {
    hash = Math.imul(hash ^ byte, 0x01000193) >>> 0
  }
  return hash % AVATAR_COLOR_COUNT
}

// initialOf is the first character of what is displayed, which is enough to
// tell one account from another and does not pretend to be a picture nobody
// uploaded.
//
// Array.from rather than charAt, so a name beginning with an emoji or a
// character outside the basic plane is not cut in half.
export function initialOf(name: string): string {
  return (Array.from(name.trim())[0] ?? '?').toUpperCase()
}

// The server renders a picture at 64 or 512 pixels square. A rendering is
// asked for at twice the size it is drawn so it stays sharp on a dense screen,
// and the small one covers everything up to 32 pixels.
function sizedPictureUrl(avatarUrl: string, size: number): string {
  const pictureSize = size <= 32 ? 64 : 512
  return `${avatarUrl}${avatarUrl.includes('?') ? '&' : '?'}size=${pictureSize}`
}

// Avatar is a person: their picture when they have one, otherwise the first
// letter of their name on the color the server chose for them.
//
// Drawn as an element of its own rather than a style on whatever held the
// letter before, so the rail, a row in a list and the settings page show the
// same picture. Children are drawn on top, which is where the mark for an operator
// signed in as this account goes.
export function Avatar({
  name,
  avatarUrl,
  colorIndex,
  size = 28,
  className,
  children,
}: {
  name: string
  avatarUrl?: string | null
  colorIndex?: number | null
  size?: number
  className?: string
  children?: React.ReactNode
}) {
  // The address that failed, rather than a flag: a new picture has a new
  // address, and should get its chance without anything resetting the flag.
  const [failedUrl, setFailedUrl] = useState<string | null>(null)
  const index =
    typeof colorIndex === 'number' && colorIndex >= 0 && colorIndex < AVATAR_COLOR_COUNT
      ? Math.floor(colorIndex)
      : avatarColorIndexOf(name)
  const showsPicture = Boolean(avatarUrl) && failedUrl !== avatarUrl
  return (
    <span
      className={['avatar', `avatar-color-${index}`, className].filter(Boolean).join(' ')}
      style={{ width: size, height: size, fontSize: Math.round(size * 0.46) }}
      aria-hidden="true"
    >
      {showsPicture ? (
        <img
          className="avatar-picture"
          src={sizedPictureUrl(avatarUrl as string, size)}
          alt=""
          loading="lazy"
          onError={() => setFailedUrl(avatarUrl ?? null)}
        />
      ) : (
        initialOf(name)
      )}
      {children}
    </span>
  )
}
