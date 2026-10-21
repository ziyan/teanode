// What this device remembers about other people's public keys, and which items
// still need a new key. A public key is not secret, so localStorage is fine;
// everything is kept per signed-in person, so two people on one browser do not
// share it. Every access is guarded: a browser that refuses storage remembers
// nothing, and every key is then a new one.
//
// There are two kinds of record about a person's key, and they mean different
// things:
//
// - A confirmed key is one the owner compared the safety code of with that
//   person (the share dialog's confirm button, trusting a changed key, the
//   first-time confirm). Only a confirmed key lets this device seal an item
//   key for the person without asking.
// - A seen key is the key a person's own items came with the first time one
//   of them opened here. The server chooses what it sends, so seeing a key
//   proves nothing about whose it is. It is used only to warn when that
//   person's key later changes. The records written before this split are all
//   seen keys, so the person is asked once about each.

const SEEN_PREFIX = 'teanode.passwords.pins:'
const CONFIRMED_PREFIX = 'teanode.passwords.confirmedKeys:'
const REKEY_PENDING_PREFIX = 'teanode.passwords.rekeyPending:'

function readKeys(prefix: string, userId: string): Record<string, string> {
  try {
    const raw = window.localStorage.getItem(prefix + userId)
    const parsed: unknown = raw ? JSON.parse(raw) : {}
    if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) return {}
    const keys: Record<string, string> = {}
    for (const [otherUserId, publicKey] of Object.entries(parsed)) {
      if (typeof publicKey === 'string') keys[otherUserId] = publicKey
    }
    return keys
  } catch {
    return {}
  }
}

function writeKey(prefix: string, userId: string, otherUserId: string, publicKey: string) {
  try {
    window.localStorage.setItem(
      prefix + userId,
      JSON.stringify({ ...readKeys(prefix, userId), [otherUserId]: publicKey }),
    )
  } catch {
    // Nothing is remembered, and the next key is a new one again.
  }
}

// seenKeyOf is the key first seen for this person, or null if none was.
export function seenKeyOf(userId: string, otherUserId: string): string | null {
  if (userId === '') return null
  return readKeys(SEEN_PREFIX, userId)[otherUserId] ?? null
}

// markKeySeen keeps the key an item of this person's came with, replacing an
// earlier one. It does not make the key trusted for sealing.
export function markKeySeen(userId: string, otherUserId: string, publicKey: string) {
  if (userId === '') return
  writeKey(SEEN_PREFIX, userId, otherUserId, publicKey)
}

// confirmedKeyOf is the key the owner confirmed for this person, or null.
export function confirmedKeyOf(userId: string, otherUserId: string): string | null {
  if (userId === '') return null
  return readKeys(CONFIRMED_PREFIX, userId)[otherUserId] ?? null
}

// confirmKey keeps this key as confirmed, after the owner compared the safety
// code with the person. It is also the seen key from then on, so a later
// change of it is noticed on that person's own items too.
export function confirmKey(userId: string, otherUserId: string, publicKey: string) {
  if (userId === '') return
  writeKey(CONFIRMED_PREFIX, userId, otherUserId, publicKey)
  writeKey(SEEN_PREFIX, userId, otherUserId, publicKey)
}

// isKeyConfirmed says whether this very key is the confirmed one for the person.
export function isKeyConfirmed(userId: string, otherUserId: string, publicKey: string): boolean {
  return confirmedKeyOf(userId, otherUserId) === publicKey
}

// knownKeyOf is the key an owner's items are checked against: the confirmed
// one if there is one, else the one first seen.
export function knownKeyOf(userId: string, otherUserId: string): string | null {
  return confirmedKeyOf(userId, otherUserId) ?? seenKeyOf(userId, otherUserId)
}

// earlierKeyOf is a key this device knew for the person that is not the one
// given, or null when it knew none (or only this one). A confirmed key counts
// before a seen one.
export function earlierKeyOf(userId: string, otherUserId: string, publicKey: string): string | null {
  const known = knownKeyOf(userId, otherUserId)
  return known !== null && known !== publicKey ? known : null
}

// The items whose access narrowed but whose new key could not be made then.
// Item ids are not secret. The next save or open of such an item tries again.
function readIdList(userId: string): string[] {
  try {
    const raw = window.localStorage.getItem(REKEY_PENDING_PREFIX + userId)
    const parsed: unknown = raw ? JSON.parse(raw) : []
    if (!Array.isArray(parsed)) return []
    return parsed.filter((entry): entry is string => typeof entry === 'string')
  } catch {
    return []
  }
}

export function isRekeyPending(userId: string, itemId: string): boolean {
  if (userId === '') return false
  return readIdList(userId).includes(itemId)
}

export function setRekeyPending(userId: string, itemId: string, isPending: boolean) {
  if (userId === '') return
  const current = readIdList(userId)
  if (current.includes(itemId) === isPending) return
  const changed = isPending ? [...current, itemId] : current.filter((other) => other !== itemId)
  try {
    window.localStorage.setItem(REKEY_PENDING_PREFIX + userId, JSON.stringify(changed))
  } catch {
    // The mark is lost with the page; the person was told at the time.
  }
}
