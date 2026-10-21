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

export interface LoginFields {
  name: string
  userName: string
  password: string
  sites: string[]
  oneTimeCodeUri: string
  notes: string
  passkeys: PasskeyFields[]
  folder: string
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
}

export interface NoteFields {
  name: string
  text: string
  folder: string
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

// parseItemFields checks what came out of decrypting an item against the
// kind's shape and returns the fields with every key the kind defines, or null
// when the value is not an object or any field has the wrong type. A person
// the item is shared by can seal anything with the item's key, so what opens
// is not trusted to be an item: the screens and the sort read these fields
// as the types above and would fail on a number where a name goes. Keys the
// kind does not define are kept, so a newer client's fields survive an edit.
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
  return { ...value, ...checked } as unknown as ItemFieldsByKind[Kind]
}
