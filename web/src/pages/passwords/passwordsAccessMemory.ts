// What this device remembers about each of the person's items: the highest
// version of the sealed access record it has seen with a valid signature and
// that record's signature, the people this device itself sealed the item key
// for, and whether this device itself made the agent a copy. The first catches
// an old record the server brings back (a lower version, or the same version
// with another signature); the other two keep an automatic new key from giving
// anybody access this device did not give.
//
// Who held a key and whether the agent had a copy come only from this device's
// own seals, never from the server's list: the server could have said anything
// the last time the item was looked at.
//
// It is kept in localStorage, by item id only, like the other things this
// device remembers (passwordsPins.ts). Item ids are random and made by the
// owner's device, so they name an item whoever the server says is signed in:
// a server that names another user id for the same sign-in does not get an
// empty memory. It stays when the person signs out or the server ends the
// sign-in: it is what notices a replay after the next sign-in. None of it is
// secret. Every access is guarded: a browser that refuses storage remembers
// nothing, which means a replay cannot be noticed and an automatic new key
// seals for nobody. Another browser, the iPhone app and the command line each
// keep their own.
//
// Earlier versions kept one memory for each signed-in person under
// "teanode.passwords.access:<userId>". Those are merged into the single store
// the first time it is read, and then removed.
//
// Known limit: a device that sees an item for the first time cannot tell an
// old signed record from the newest one, so it takes the newest signed record
// the server shows it when the person acts.

const MEMORY_NAME = 'teanode.passwords.itemMemory'
const LEGACY_MEMORY_PREFIX = 'teanode.passwords.access:'

export type ItemMemory = {
  // highestRecordVersion is the highest recordVersion seen on a record whose
  // signature was valid (0 when none was).
  highestRecordVersion: number
  // highestRecordMac is the signature of the record at that version, or an
  // empty text when it was not kept.
  highestRecordMac: string
  // sealedForUserIds are the people this device sealed the item key for.
  sealedForUserIds: string[]
  // hasMadeAgentCopy says this device made the agent a copy of the item key.
  hasMadeAgentCopy: boolean
}

function emptyMemory(): ItemMemory {
  return { highestRecordVersion: 0, highestRecordMac: '', sealedForUserIds: [], hasMadeAgentCopy: false }
}

// memoriesOf reads the stored text as a memory for each item id, dropping
// whatever is not in the shape.
function memoriesOf(raw: string | null): Record<string, ItemMemory> {
  const parsed: unknown = raw ? JSON.parse(raw) : {}
  if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) return {}
  const memories: Record<string, ItemMemory> = {}
  for (const [itemId, value] of Object.entries(parsed)) {
    if (typeof value !== 'object' || value === null) continue
    const candidate = value as Partial<ItemMemory>
    memories[itemId] = {
      highestRecordVersion:
        typeof candidate.highestRecordVersion === 'number' && Number.isSafeInteger(candidate.highestRecordVersion)
          ? candidate.highestRecordVersion
          : 0,
      highestRecordMac: typeof candidate.highestRecordMac === 'string' ? candidate.highestRecordMac : '',
      sealedForUserIds: Array.isArray(candidate.sealedForUserIds)
        ? candidate.sealedForUserIds.filter((other): other is string => typeof other === 'string')
        : [],
      hasMadeAgentCopy: candidate.hasMadeAgentCopy === true,
    }
  }
  return memories
}

// mergedMemory is what two memories of one item say together: the higher
// record version with its signature, everybody either sealed for, and the
// agent copy if either made it.
function mergedMemory(first: ItemMemory, second: ItemMemory): ItemMemory {
  const [higher, lower] =
    second.highestRecordVersion > first.highestRecordVersion ||
    (second.highestRecordVersion === first.highestRecordVersion && first.highestRecordMac === '')
      ? [second, first]
      : [first, second]
  return {
    highestRecordVersion: higher.highestRecordVersion,
    highestRecordMac: higher.highestRecordMac,
    sealedForUserIds: [...new Set([...lower.sealedForUserIds, ...higher.sealedForUserIds])],
    hasMadeAgentCopy: first.hasMadeAgentCopy || second.hasMadeAgentCopy,
  }
}

// moveLegacyMemories merges the memories earlier versions kept for each
// signed-in person into the single store, and removes them.
function moveLegacyMemories(memories: Record<string, ItemMemory>): Record<string, ItemMemory> {
  const legacyNames: string[] = []
  for (let index = 0; index < window.localStorage.length; index += 1) {
    const name = window.localStorage.key(index)
    if (name !== null && name.startsWith(LEGACY_MEMORY_PREFIX)) legacyNames.push(name)
  }
  if (legacyNames.length === 0) return memories
  const merged = { ...memories }
  for (const name of legacyNames) {
    try {
      for (const [itemId, legacy] of Object.entries(memoriesOf(window.localStorage.getItem(name)))) {
        merged[itemId] = merged[itemId] === undefined ? legacy : mergedMemory(merged[itemId], legacy)
      }
    } catch {
      // A text that is not JSON holds nothing to carry over.
    }
  }
  window.localStorage.setItem(MEMORY_NAME, JSON.stringify(merged))
  for (const name of legacyNames) window.localStorage.removeItem(name)
  return merged
}

function readAll(): Record<string, ItemMemory> {
  try {
    return moveLegacyMemories(memoriesOf(window.localStorage.getItem(MEMORY_NAME)))
  } catch {
    return {}
  }
}

function writeMemory(itemId: string, memory: ItemMemory) {
  try {
    window.localStorage.setItem(MEMORY_NAME, JSON.stringify({ ...readAll(), [itemId]: memory }))
  } catch {
    // Nothing is remembered; see the note at the top.
  }
}

// memoryOf is what this device remembers for the item, or nothing.
export function memoryOf(itemId: string): ItemMemory {
  return readAll()[itemId] ?? emptyMemory()
}

// hasMemoryOf says whether this device has seen a signed record of the item
// before (in an earlier session, or by signing one itself).
export function hasMemoryOf(itemId: string): boolean {
  return memoryOf(itemId).highestRecordVersion > 0
}

// isReplayedRecord says whether a record with a valid signature is an old one:
// its version is lower than the highest this device remembers, or equal to it
// with another signature.
export function isReplayedRecord(itemId: string, recordVersion: number, recordMac: string): boolean {
  const memory = memoryOf(itemId)
  if (recordVersion < memory.highestRecordVersion) return true
  return (
    recordVersion === memory.highestRecordVersion &&
    memory.highestRecordMac !== '' &&
    recordMac !== memory.highestRecordMac
  )
}

// rememberRecord keeps a record that was seen with a valid signature, when its
// version is higher than the one kept (or the same version, kept without its
// signature).
export function rememberRecord(itemId: string, recordVersion: number, recordMac: string) {
  const memory = memoryOf(itemId)
  if (
    recordVersion > memory.highestRecordVersion ||
    (recordVersion === memory.highestRecordVersion && memory.highestRecordMac === '' && recordMac !== '')
  ) {
    writeMemory(itemId, { ...memory, highestRecordVersion: recordVersion, highestRecordMac: recordMac })
  }
}

// rememberSealedFor replaces the people this device sealed the item key for
// and whether it made the agent a copy: a new item key has just been made, and
// only the copies made with it exist.
export function rememberSealedFor(itemId: string, sealedForUserIds: string[], hasMadeAgentCopy: boolean) {
  const memory = memoryOf(itemId)
  writeMemory(itemId, { ...memory, sealedForUserIds: [...new Set(sealedForUserIds)], hasMadeAgentCopy })
}

// addSealedFor adds people this device has just sealed a key for.
export function addSealedFor(itemId: string, sealedForUserIds: string[]) {
  if (sealedForUserIds.length === 0) return
  const memory = memoryOf(itemId)
  writeMemory(itemId, {
    ...memory,
    sealedForUserIds: [...new Set([...memory.sealedForUserIds, ...sealedForUserIds])],
  })
}

// rememberAgentCopy keeps that this device made the agent a copy of the key.
export function rememberAgentCopy(itemId: string) {
  const memory = memoryOf(itemId)
  if (!memory.hasMadeAgentCopy) writeMemory(itemId, { ...memory, hasMadeAgentCopy: true })
}
