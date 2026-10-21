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
