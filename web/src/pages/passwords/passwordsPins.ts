// What this device remembers about other people's public keys. A public key is
// not secret, so localStorage is fine; everything is kept per signed-in
// person, so two people on one browser do not share it. Every access is
// guarded: a browser that refuses storage remembers nothing, and every key is
// then a first key. What a device knows stays on that device: another
// browser, the iPhone app and the command line each keep their own.
//
// There is one known key per person (by user id): the first key this device
// saw for them, from any source (sharing with them, an item they shared, a
// share waiting for them). It is sealed for without asking anybody. When the
// server later gives another key for the same person, the new key becomes the
// known one at once and a notice about the change is kept until the person
// dismisses it. The notice never gates anything: it only offers to compare
// safety codes, and comparing is optional.
//
// The records written before this one kept two lists, the keys the person had
// confirmed and the keys that were only seen. They are moved into the one
// list the first time it is read: a confirmed key is a compared key.

const KNOWN_PREFIX = 'teanode.passwords.knownKeys:'
const LEGACY_SEEN_PREFIX = 'teanode.passwords.pins:'
const LEGACY_CONFIRMED_PREFIX = 'teanode.passwords.confirmedKeys:'

export type KnownKey = {
  publicKey: string
  // name is how the person was called when the key was recorded, for a notice
  // shown when the person is not on screen.
  name: string
  // isCompared is true once the owner compared the safety code with the
  // person. It is shown beside the person and gates nothing.
  isCompared: boolean
  // previousPublicKey is the key known before the latest change, or null when
  // the key never changed.
  previousPublicKey: string | null
  // hasChangeNotice is true from a change until the person dismisses it.
  hasChangeNotice: boolean
}

export type KeyChangeNotice = {
  otherUserId: string
  name: string
  oldPublicKey: string
  newPublicKey: string
}

export type KeyRecordOutcome = 'first' | 'same' | 'changed'

function readStrings(prefix: string, userId: string): Record<string, string> {
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

function parseKnownKey(value: unknown): KnownKey | null {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return null
  const record = value as Record<string, unknown>
  if (typeof record.publicKey !== 'string') return null
  return {
    publicKey: record.publicKey,
    name: typeof record.name === 'string' ? record.name : '',
    isCompared: record.isCompared === true,
    previousPublicKey: typeof record.previousPublicKey === 'string' ? record.previousPublicKey : null,
    hasChangeNotice: record.hasChangeNotice === true && typeof record.previousPublicKey === 'string',
  }
}

function writeKnownKeys(userId: string, keys: Record<string, KnownKey>) {
  try {
    window.localStorage.setItem(KNOWN_PREFIX + userId, JSON.stringify(keys))
  } catch {
    // Nothing is remembered, and the next key is a first key again.
  }
}

// readKnownKeys reads the one list. When it has never been written, the two
// lists of the earlier version are moved into it (and removed): the seen keys
// first, then the confirmed ones over them, marked as compared.
function readKnownKeys(userId: string): Record<string, KnownKey> {
  try {
    const raw = window.localStorage.getItem(KNOWN_PREFIX + userId)
    if (raw === null) return migrateLegacyKeys(userId)
    const parsed: unknown = JSON.parse(raw)
    if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) return {}
    const keys: Record<string, KnownKey> = {}
    for (const [otherUserId, value] of Object.entries(parsed)) {
      const key = parseKnownKey(value)
      if (key !== null) keys[otherUserId] = key
    }
    return keys
  } catch {
    return {}
  }
}

function migrateLegacyKeys(userId: string): Record<string, KnownKey> {
  const seen = readStrings(LEGACY_SEEN_PREFIX, userId)
  const confirmed = readStrings(LEGACY_CONFIRMED_PREFIX, userId)
  const keys: Record<string, KnownKey> = {}
  for (const [otherUserId, publicKey] of Object.entries(seen)) {
    keys[otherUserId] = { publicKey, name: '', isCompared: false, previousPublicKey: null, hasChangeNotice: false }
  }
  for (const [otherUserId, publicKey] of Object.entries(confirmed)) {
    keys[otherUserId] = { publicKey, name: '', isCompared: true, previousPublicKey: null, hasChangeNotice: false }
  }
  if (Object.keys(keys).length === 0) return keys
  writeKnownKeys(userId, keys)
  try {
    window.localStorage.removeItem(LEGACY_SEEN_PREFIX + userId)
    window.localStorage.removeItem(LEGACY_CONFIRMED_PREFIX + userId)
  } catch {
    // The earlier lists stay; the new one is read first from now on.
  }
  return keys
}

// knownKeyRecordOf is what this device knows of the person's key, or null.
export function knownKeyRecordOf(userId: string, otherUserId: string): KnownKey | null {
  if (userId === '') return null
  return readKnownKeys(userId)[otherUserId] ?? null
}

// knownKeyOf is the key this device knows for the person, or null.
export function knownKeyOf(userId: string, otherUserId: string): string | null {
  return knownKeyRecordOf(userId, otherUserId)?.publicKey ?? null
}

// recordKey keeps the key the server gives for a person. A person never seen
// before is a 'first' key; the known key again is 'same' and changes nothing;
// any other key replaces the known one at once, is 'changed', and leaves a
// notice (with the key before) until it is dismissed. A changed key is not a
// compared one.
export function recordKey(userId: string, otherUserId: string, publicKey: string, name: string): KeyRecordOutcome {
  if (userId === '') return 'same'
  const keys = readKnownKeys(userId)
  const known = keys[otherUserId]
  if (known?.publicKey === publicKey) {
    if (known.name === '' && name !== '') writeKnownKeys(userId, { ...keys, [otherUserId]: { ...known, name } })
    return 'same'
  }
  keys[otherUserId] = {
    publicKey,
    name: name || known?.name || '',
    isCompared: false,
    previousPublicKey: known?.publicKey ?? null,
    hasChangeNotice: known !== undefined,
  }
  writeKnownKeys(userId, keys)
  return known === undefined ? 'first' : 'changed'
}

// markKeyCompared records that the owner compared the safety code of this very
// key with the person. A key that is not the known one becomes the known one
// without a notice, since the owner is looking at it now. Its notice, if there
// was one, is done with.
export function markKeyCompared(userId: string, otherUserId: string, publicKey: string, name: string) {
  if (userId === '') return
  const keys = readKnownKeys(userId)
  const known = keys[otherUserId]
  keys[otherUserId] = {
    publicKey,
    name: name || known?.name || '',
    isCompared: true,
    previousPublicKey:
      known !== undefined && known.publicKey !== publicKey ? known.publicKey : (known?.previousPublicKey ?? null),
    hasChangeNotice: false,
  }
  writeKnownKeys(userId, keys)
}

// isKeyCompared says whether the owner compared this very key with the person.
export function isKeyCompared(userId: string, otherUserId: string, publicKey: string): boolean {
  const known = knownKeyRecordOf(userId, otherUserId)
  return known !== null && known.publicKey === publicKey && known.isCompared
}

// dismissKeyChangeNotice stops showing the notice for a person's changed key.
export function dismissKeyChangeNotice(userId: string, otherUserId: string) {
  if (userId === '') return
  const keys = readKnownKeys(userId)
  const known = keys[otherUserId]
  if (known === undefined || !known.hasChangeNotice) return
  writeKnownKeys(userId, { ...keys, [otherUserId]: { ...known, hasChangeNotice: false } })
}

// keyChangeNotices are the changed keys the person has not dismissed.
export function keyChangeNotices(userId: string): KeyChangeNotice[] {
  if (userId === '') return []
  return Object.entries(readKnownKeys(userId)).flatMap(([otherUserId, known]) =>
    known.hasChangeNotice && known.previousPublicKey !== null
      ? [{ otherUserId, name: known.name, oldPublicKey: known.previousPublicKey, newPublicKey: known.publicKey }]
      : [],
  )
}

// earlierKeyOf is a key this device knew for the person that is not the one
// given, or null when it knew none (or only this one).
export function earlierKeyOf(userId: string, otherUserId: string, publicKey: string): string | null {
  const known = knownKeyOf(userId, otherUserId)
  return known !== null && known !== publicKey ? known : null
}
