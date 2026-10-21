// What is inside an item once it is decrypted. The names are the ones the
// plan fixes (docs/planning/passwords-execplan.md, "Item fields"); the Go,
// Swift and command line code spell them the same way, because the fields
// travel as one JSON text sealed with the item key.

export type ItemKind = 'login' | 'card' | 'note'

export const ITEM_KINDS: readonly ItemKind[] = ['login', 'card', 'note']

// A passkey kept inside a login. credentialId, userHandle and privateKey (PKCS#8
// DER, ES256) are bytes written as standard base64 with padding, not the
// base64url a website's JSON uses. createdAt is an RFC 3339 time in UTC.
export interface PasskeyFields {
  credentialId: string
  relyingPartyId: string
  userHandle: string
  userName: string
  userDisplayName: string
  privateKey: string
  createdAt: string
}

// One person or one circle that may have an item. Exactly one of the two is
// set, as a non-empty id.
export type ItemGrantee = { userId: string; circleId?: undefined } | { circleId: string; userId?: undefined }

// Who may have an item and whether the agent may use it. It is sealed inside
// the fields, written only by the owner's device, so the server cannot widen
// it. An item with no record grants nobody and is not usable by the agent. A
// recipient's copy may show the record but never acts on it.
//
// recordVersion and recordMac are its signature: the version counts the owner's
// changes, and the mac is the base64 of an HMAC-SHA256 under a key made from
// the account key, which only the owner's devices hold (see macOfAccess in
// crypto.ts). A record may lack both, as one written before they existed;
// verifyAccess does not trust such a record.
export interface ItemAccess {
  isUsableByAgent: boolean
  grantees: ItemGrantee[]
  recordVersion?: number
  recordMac?: string
}

export interface LoginFields {
  name: string
  userName: string
  password: string
  sites: string[]
  oneTimeCodeUri: string
  notes: string
  passkeys: PasskeyFields[]
  folder: string
  access?: ItemAccess
}

export interface CardFields {
  name: string
  cardholderName: string
  number: string
  expiryMonth: string
  expiryYear: string
  securityCode: string
  brand: string
  notes: string
  folder: string
  access?: ItemAccess
}

export interface NoteFields {
  name: string
  text: string
  folder: string
  access?: ItemAccess
}

export interface ItemFieldsByKind {
  login: LoginFields
  card: CardFields
  note: NoteFields
}

export type ItemFields = ItemFieldsByKind[ItemKind]

export function emptyLoginFields(): LoginFields {
  return {
    name: '',
    userName: '',
    password: '',
    sites: [],
    oneTimeCodeUri: '',
    notes: '',
    passkeys: [],
    folder: '',
  }
}

export function emptyCardFields(): CardFields {
  return {
    name: '',
    cardholderName: '',
    number: '',
    expiryMonth: '',
    expiryYear: '',
    securityCode: '',
    brand: '',
    notes: '',
    folder: '',
  }
}

export function emptyNoteFields(): NoteFields {
  return { name: '', text: '', folder: '' }
}

// fillMissingFields returns the fields with every key the kind defines. Another
// client may leave out an empty one, and the screens read all of them.
export function fillMissingFields<Kind extends ItemKind>(
  itemKind: Kind,
  fields: Partial<ItemFieldsByKind[Kind]>,
): ItemFieldsByKind[Kind] {
  const empty: ItemFields =
    itemKind === 'login' ? emptyLoginFields() : itemKind === 'card' ? emptyCardFields() : emptyNoteFields()
  return { ...empty, ...fields } as ItemFieldsByKind[Kind]
}

// isReachedByAccess reports whether the record grants an item to a recipient:
// a person grantee by user id, or a circle grantee when the server lists the
// recipient as a member of that circle (circleIdsOfRecipient). The recipient
// still needs a key confirmed on this device before anything is sealed for
// them. No record reaches nobody.
export function isReachedByAccess(
  access: ItemAccess | undefined,
  recipientUserId: string,
  circleIdsOfRecipient: readonly string[],
): boolean {
  if (!access || recipientUserId === '') return false
  return access.grantees.some((grantee) =>
    grantee.userId !== undefined
      ? grantee.userId === recipientUserId
      : grantee.circleId !== undefined && circleIdsOfRecipient.includes(grantee.circleId),
  )
}

// accessWithGrantees returns a new record with these grantees and the same
// agent use, without a version or signature: sign it again (withSignedAccess)
// before it is sealed. No record counts as one that grants nobody and is not
// usable by the agent. The given record is not changed.
export function accessWithGrantees(access: ItemAccess | undefined, grantees: readonly ItemGrantee[]): ItemAccess {
  return { isUsableByAgent: access?.isUsableByAgent ?? false, grantees: grantees.map((grantee) => ({ ...grantee })) }
}

// accessWithAgentUse returns a new record with agent use turned on or off and
// the same grantees, without a version or signature: sign it again
// (withSignedAccess) before it is sealed. The given record is not changed.
export function accessWithAgentUse(access: ItemAccess | undefined, isUsableByAgent: boolean): ItemAccess {
  return { isUsableByAgent, grantees: (access?.grantees ?? []).map((grantee) => ({ ...grantee })) }
}

// How each field of a kind is typed. A list of text is a list whose every
// element is text; a list of passkeys is a list of objects whose every field
// is text.
type FieldType = 'text' | 'textList' | 'passkeyList'

const FIELD_TYPES: Record<ItemKind, Record<string, FieldType>> = {
  login: {
    name: 'text',
    userName: 'text',
    password: 'text',
    sites: 'textList',
    oneTimeCodeUri: 'text',
    notes: 'text',
    passkeys: 'passkeyList',
    folder: 'text',
  },
  card: {
    name: 'text',
    cardholderName: 'text',
    number: 'text',
    expiryMonth: 'text',
    expiryYear: 'text',
    securityCode: 'text',
    brand: 'text',
    notes: 'text',
    folder: 'text',
  },
  note: { name: 'text', text: 'text', folder: 'text' },
}

const PASSKEY_FIELD_NAMES: readonly (keyof PasskeyFields)[] = [
  'credentialId',
  'relyingPartyId',
  'userHandle',
  'userName',
  'userDisplayName',
  'privateKey',
  'createdAt',
]

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

// A field another client left out, or wrote as null (a Go program writes an
// empty list as null), is an empty one. Anything else must have the field's
// type, or the whole item is refused.
function readText(value: unknown): string | null {
  if (value === undefined || value === null) return ''
  return typeof value === 'string' ? value : null
}

function readTextList(value: unknown): string[] | null {
  if (value === undefined || value === null) return []
  if (!Array.isArray(value) || !value.every((element) => typeof element === 'string')) return null
  return [...value] as string[]
}

function readPasskeyList(value: unknown): PasskeyFields[] | null {
  if (value === undefined || value === null) return []
  if (!Array.isArray(value)) return null
  const passkeys: PasskeyFields[] = []
  for (const element of value) {
    if (!isPlainObject(element)) return null
    const passkey = {} as Record<string, string>
    for (const fieldName of PASSKEY_FIELD_NAMES) {
      const text = readText(element[fieldName])
      if (text === null) return null
      passkey[fieldName] = text
    }
    passkeys.push(passkey as unknown as PasskeyFields)
  }
  return passkeys
}

// A signature is 32 bytes: the output of HMAC-SHA256.
const ACCESS_MAC_BYTE_COUNT = 32

// readAccess reads the access record. Nothing or null is no record
// (undefined). Anything that is not exactly the shape of ItemAccess is refused
// (null): an object with isUsableByAgent, a boolean, and grantees, a list of
// objects each with exactly one of userId and circleId as a non-empty text,
// and optionally recordVersion, a positive integer, and recordMac, canonical
// base64 of 32 bytes. The item is then unreadable instead of half understood.
function readAccess(value: unknown): ItemAccess | undefined | null {
  if (value === undefined || value === null) return undefined
  if (!isPlainObject(value)) return null
  const keys = Object.keys(value)
  if (!keys.includes('isUsableByAgent') || !keys.includes('grantees')) return null
  if (keys.some((key) => !['isUsableByAgent', 'grantees', 'recordVersion', 'recordMac'].includes(key))) return null
  if (typeof value.isUsableByAgent !== 'boolean' || !Array.isArray(value.grantees)) return null
  const grantees: ItemGrantee[] = []
  for (const element of value.grantees) {
    if (!isPlainObject(element)) return null
    const grantee = readGrantee(element)
    if (grantee === null) return null
    grantees.push(grantee)
  }
  const access: ItemAccess = { isUsableByAgent: value.isUsableByAgent, grantees }
  if (keys.includes('recordVersion')) {
    const version = value.recordVersion
    if (typeof version !== 'number' || !Number.isSafeInteger(version) || version < 1) return null
    access.recordVersion = version
  }
  if (keys.includes('recordMac')) {
    const mac = value.recordMac
    if (typeof mac !== 'string' || !isCanonicalMac(mac)) return null
    access.recordMac = mac
  }
  return access
}

// isCanonicalMac reports whether the text is standard base64 with padding of
// exactly 32 bytes and no other spelling of them.
function isCanonicalMac(text: string): boolean {
  try {
    const bytes = Uint8Array.from(atob(text), (character) => character.charCodeAt(0))
    return bytes.length === ACCESS_MAC_BYTE_COUNT && btoa(String.fromCharCode(...bytes)) === text
  } catch {
    return false
  }
}

function readGrantee(element: Record<string, unknown>): ItemGrantee | null {
  const keys = Object.keys(element)
  if (keys.length !== 1) return null
  const [key] = keys
  const id = element[key]
  if (typeof id !== 'string' || id === '') return null
  if (key === 'userId') return { userId: id }
  if (key === 'circleId') return { circleId: id }
  return null
}

// parseItemFields checks what came out of decrypting an item against the
// kind's shape and returns the fields with every key the kind defines, or null
// when the value is not an object or any field has the wrong type. A person
// the item is shared by can seal anything with the item's key, so what opens
// is not trusted to be an item: the screens and the sort read these fields
// as the types above and would fail on a number where a name goes. Keys the
// kind does not define are kept, so a newer client's fields survive an edit. The access
// record, if there is one, must have the shape of ItemAccess.
export function parseItemFields<Kind extends ItemKind>(itemKind: Kind, value: unknown): ItemFieldsByKind[Kind] | null {
  if (!isPlainObject(value)) return null
  const checked: Record<string, unknown> = {}
  for (const [fieldName, fieldType] of Object.entries(FIELD_TYPES[itemKind])) {
    const read =
      fieldType === 'text'
        ? readText(value[fieldName])
        : fieldType === 'textList'
          ? readTextList(value[fieldName])
          : readPasskeyList(value[fieldName])
    if (read === null) return null
    checked[fieldName] = read
  }
  const access = readAccess(value.access)
  if (access === null) return null
  const parsed: Record<string, unknown> = { ...value, ...checked }
  if (access === undefined) {
    delete parsed.access
  } else {
    parsed.access = access
  }
  return parsed as unknown as ItemFieldsByKind[Kind]
}
