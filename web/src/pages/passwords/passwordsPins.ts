// The public key each person has been shared with, remembered on this device
// so a key that changes later is noticed (trust on first use). A public key is
// not secret, so localStorage is fine; it is kept per signed-in person, so two
// people on one browser do not share pins. Every access is guarded: a browser
// that refuses storage just has no pins, and every key is then a first one.

const PINS_PREFIX = 'teanode.passwords.pins:'

function readPins(userId: string): Record<string, string> {
  try {
    const raw = window.localStorage.getItem(PINS_PREFIX + userId)
    const parsed: unknown = raw ? JSON.parse(raw) : {}
    if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) return {}
    const pins: Record<string, string> = {}
    for (const [recipientUserId, publicKey] of Object.entries(parsed)) {
      if (typeof publicKey === 'string') pins[recipientUserId] = publicKey
    }
    return pins
  } catch {
    return {}
  }
}

// pinnedKeyOf is the key kept for this recipient, or null if none was.
export function pinnedKeyOf(userId: string, recipientUserId: string): string | null {
  if (userId === '') return null
  return readPins(userId)[recipientUserId] ?? null
}

// pinKey keeps this key for the recipient, replacing an earlier one.
export function pinKey(userId: string, recipientUserId: string, publicKey: string) {
  if (userId === '') return
  try {
    window.localStorage.setItem(
      PINS_PREFIX + userId,
      JSON.stringify({ ...readPins(userId), [recipientUserId]: publicKey }),
    )
  } catch {
    // Nothing is remembered, and the next key is a first one again.
  }
}

// hasKeyChanged says whether a key differs from the one kept. No pin kept is
// not a change.
export function hasKeyChanged(userId: string, recipientUserId: string, publicKey: string): boolean {
  const pinned = pinnedKeyOf(userId, recipientUserId)
  return pinned !== null && pinned !== publicKey
}
