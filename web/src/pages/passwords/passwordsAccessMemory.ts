// What this device remembers about each of the person's items: the highest
// version of the sealed access record it has seen with a valid signature, who
// held a key for the item the last time it looked, and whether the agent had a
// copy. The first catches an old record the server brings back; the other two
// keep an automatic new key from giving anybody access they did not have.
//
// It is kept per signed-in person in localStorage, like the other things this
// device remembers (passwordsPins.ts). None of it is secret. Every access is
// guarded: a browser that refuses storage remembers nothing, which means a
// replay cannot be noticed and an automatic new key seals for nobody. Another
// browser, the iPhone app and the command line each keep their own.

const MEMORY_PREFIX = 'teanode.passwords.access:'

export type ItemMemory = {
  // highestRecordVersion is the highest recordVersion seen on a record whose
  // signature was valid (0 when none was).
  highestRecordVersion: number
  // keyHolderUserIds are the people the server listed with a key for the item
  // the last time the item was seen without the server's new-key mark, or that
  // this device sealed a key for.
  keyHolderUserIds: string[]
  // hadAgentCopy says the server listed the item as used by the agent the last
  // time the item was seen without the new-key mark, or that this device made
  // the agent a copy.
  hadAgentCopy: boolean
}

function readAll(userId: string): Record<string, ItemMemory> {
  try {
    const raw = window.localStorage.getItem(MEMORY_PREFIX + userId)
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
        keyHolderUserIds: Array.isArray(candidate.keyHolderUserIds)
          ? candidate.keyHolderUserIds.filter((other): other is string => typeof other === 'string')
          : [],
        hadAgentCopy: candidate.hadAgentCopy === true,
      }
    }
    return memories
  } catch {
    return {}
  }
}

function writeMemory(userId: string, itemId: string, memory: ItemMemory) {
  try {
    window.localStorage.setItem(MEMORY_PREFIX + userId, JSON.stringify({ ...readAll(userId), [itemId]: memory }))
  } catch {
    // Nothing is remembered; see the note at the top.
  }
}

// memoryOf is what this device remembers for the item, or nothing.
export function memoryOf(userId: string, itemId: string): ItemMemory {
  if (userId === '') return { highestRecordVersion: 0, keyHolderUserIds: [], hadAgentCopy: false }
  return readAll(userId)[itemId] ?? { highestRecordVersion: 0, keyHolderUserIds: [], hadAgentCopy: false }
}

// rememberRecordVersion keeps a record version that was seen with a valid
// signature, when it is higher than the one kept.
export function rememberRecordVersion(userId: string, itemId: string, recordVersion: number) {
  if (userId === '') return
  const memory = memoryOf(userId, itemId)
  if (recordVersion > memory.highestRecordVersion) {
    writeMemory(userId, itemId, { ...memory, highestRecordVersion: recordVersion })
  }
}

// rememberKeyHolders replaces who held a key and whether the agent had a copy,
// as last seen.
export function rememberKeyHolders(userId: string, itemId: string, keyHolderUserIds: string[], hadAgentCopy: boolean) {
  if (userId === '') return
  const memory = memoryOf(userId, itemId)
  writeMemory(userId, itemId, { ...memory, keyHolderUserIds: [...new Set(keyHolderUserIds)], hadAgentCopy })
}

// addKeyHolders adds people this device has just sealed a key for.
export function addKeyHolders(userId: string, itemId: string, keyHolderUserIds: string[]) {
  if (userId === '' || keyHolderUserIds.length === 0) return
  const memory = memoryOf(userId, itemId)
  writeMemory(userId, itemId, {
    ...memory,
    keyHolderUserIds: [...new Set([...memory.keyHolderUserIds, ...keyHolderUserIds])],
  })
}
