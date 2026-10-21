import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, expect, it, vi } from 'vitest'

import { APIError, graphql } from '../../api'
import { BreadcrumbProvider } from '../../components/breadcrumb'
import { ToastProvider } from '../../components/toast'
import { TranslationProvider } from '../../i18n/i18n'
import {
  AGENT_RECIPIENT_ID,
  decodeBase64,
  encodeBase64,
  generatePersonKeyPair,
  hashProof,
  openForKeyPair,
  openItemKey,
  parseSealedForKeyPair,
  parseSealedValue,
  parseRecoveryKey,
  PersonKeyPair,
  safetyCode,
  verifyAccess,
  withSignedAccess,
} from '../../passwords/crypto'
import { openItemFields } from '../../passwords/crypto'
import {
  emptyCardFields,
  emptyLoginFields,
  emptyNoteFields,
  ItemAccess,
  ItemGrantee,
  ItemKind,
} from '../../passwords/fields'
import { SessionProvider } from '../../session'
import { PasswordItemRecord, PasswordRecipient, PasswordsSetup } from './passwordsApi'
import { PasswordsPage } from './passwordsPage'
import {
  makeSetUp,
  passphraseSecret,
  passphraseStrength,
  isAgentAllowed,
  PendingSetUp,
  proofOf,
  sealItemKeyFor,
  sealPasswordItem,
} from './passwordsProtocol'
import { filterItems, ItemFilter, NO_FOLDER } from './passwordsList'
import { sitesToShow } from './passwordsItemView'
import { exportText } from './passwordsSettings'
import { passwordsSession } from './passwordsSession'
import { lockPasswords } from './passwordsLock'
import { PasswordsAccessError, PasswordsError, failureText } from './passwordsErrors'
import { clearClipboardLater, RecoveryKeyPanel, SafetyCode, SecretValue } from './passwordsParts'
import { oneTimeCodeUriFromText, rebasedFields, sitesFromText } from './passwordsEditor'

vi.mock('../../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api')>()),
  graphql: vi.fn(),
}))

const USER_ID = 'person-a'
const PASSPHRASE = 'correct horse battery staple'
const STALE_MESSAGE = 'This item changed on another device. Open it again.'

// Made once: the passphrase key takes a moment to derive.
let fixture: PendingSetUp
let secret: Uint8Array
let setupView: PasswordsSetup
let personB: PersonKeyPair
let agentKeyPair: PersonKeyPair
let personC: PersonKeyPair

beforeAll(async () => {
  fixture = await makeSetUp(PASSPHRASE)
  setupView = {
    isSetUp: true,
    passphraseSalt: fixture.input.passphraseSalt,
    passphraseIterationCount: fixture.input.passphraseIterationCount,
    publicKey: fixture.input.publicKey,
    lockAfterMinutes: 15,
    itemCount: 0,
  }
  secret = await passphraseSecret(PASSPHRASE, setupView)
  personB = await generatePersonKeyPair()
  agentKeyPair = await generatePersonKeyPair()
  personC = await generatePersonKeyPair()
}, 60_000)

beforeEach(() => {
  // The tab title reads whether the window is wide.
  window.matchMedia = (() => ({
    matches: true,
    addEventListener() {},
    removeEventListener() {},
    addListener() {},
    removeListener() {},
  })) as unknown as typeof window.matchMedia
  window.localStorage.clear()
})
afterEach(() => {
  cleanup()
  passwordsSession.lock()
  passwordsSession.setLockAfterMinutes(15)
  vi.resetAllMocks()
})

type Server = {
  setup: PasswordsSetup
  records: PasswordItemRecord[]
  recipients: PasswordRecipient[]
  waiting: { itemId: string; recipient: PasswordRecipient }[]
  // isStale makes the next save be refused as one based on an old revision.
  isStale: boolean
  // unlockPublicKey is the public key the server sends on unlock, when it is
  // not the one the person's private key makes.
  unlockPublicKey: string | null
  // isRekeyFailing makes the next re-keys fail with a server error.
  isRekeyFailing: boolean
  // hasMoreWaiting is set on every waiting entry, as the server does when more
  // wait than it lists.
  hasMoreWaiting: boolean
  // fullUserIds are the people the server does not store a key for (too much
  // is already shared with them): it stores the others of a call.
  fullUserIds: string[]
}

function serve(overrides: Partial<Server> = {}): Server {
  const server: Server = {
    setup: setupView,
    records: [],
    recipients: [],
    waiting: [],
    isStale: false,
    unlockPublicKey: null,
    isRekeyFailing: false,
    hasMoreWaiting: false,
    fullUserIds: [],
    ...overrides,
  }
  vi.mocked(graphql).mockImplementation(async (document, variables) => {
    const asked = (variables ?? {}) as Record<string, any>
    if (document.includes('ReadPasswordsSetup')) return { ReadPasswordsSetup: server.setup } as never
    if (document.includes('SetUpPasswords')) return { SetUpPasswords: { ...setupView } } as never
    if (document.includes('UnlockPasswords')) {
      const proofHash = await hashProof(decodeBase64(asked.proof))
      if (proofHash !== fixture.input.passphraseProofHash) throw new APIError('That passphrase is not right.')
      return {
        UnlockPasswords: {
          accountKeyWrapped: fixture.input.accountKeyByPassphrase,
          privateKeyByAccountKey: fixture.input.privateKeyByAccountKey,
          publicKey: server.unlockPublicKey ?? fixture.input.publicKey,
        },
      } as never
    }
    if (document.includes('ListPasswordItems')) {
      const since = (asked.sinceRevision as number | null) ?? 0
      return {
        ListPasswordItems: {
          items: server.records.filter((record) => record.revision > since),
          itemIds: server.records.map((record) => record.id),
          revision: Math.max(0, ...server.records.map((record) => record.revision)),
        },
      } as never
    }
    if (document.includes('ListWaitingPasswordShares')) {
      return {
        ListWaitingPasswordShares: server.waiting.map((waiting) => ({
          hasMoreWaiting: server.hasMoreWaiting,
          ...waiting,
        })),
      } as never
    }
    if (document.includes('AddPasswordShareKeys')) {
      // The server refuses a key that names an item key other than the item's
      // current one (contract 4).
      for (const key of asked.keys as { itemId: string; itemKey?: string }[]) {
        const current = server.records.find((record) => record.id === key.itemId)
        if (!current || key.itemKey !== current.itemKey) throw new APIError(STALE_MESSAGE)
      }
      const storedKeys = (asked.keys as { recipientUserId: string }[]).filter(
        (key) => !server.fullUserIds.includes(key.recipientUserId),
      )
      return { AddPasswordShareKeys: storedKeys.length } as never
    }
    if (document.includes('SavePasswordItem')) {
      const input = asked.input
      const existing = server.records.find((record) => record.id === input.id)
      if (server.isStale) throw new APIError(STALE_MESSAGE)
      const saved: PasswordItemRecord = {
        ownerUserId: USER_ID,
        ownerName: 'Person A',
        ownerPublicKey: fixture.input.publicKey,
        isSharedWithMe: false,
        isUsableByAgent: false,
        needsNewKey: false,
        lastUsedByAgentAt: null,
        deletedAt: null,
        createdAt: '2026-10-01T10:00:00Z',
        updatedAt: '2026-10-02T10:00:00Z',
        shares: [],
        ...existing,
        id: input.id,
        itemKind: input.itemKind,
        encryptedFields: input.encryptedFields,
        itemKey: input.itemKey,
        revision: (existing?.revision ?? 0) + 10,
      }
      server.records = [...server.records.filter((record) => record.id !== input.id), saved]
      return { SavePasswordItem: saved } as never
    }
    if (document.includes('DeletePasswordItem') || document.includes('RestorePasswordItem')) {
      const isDelete = document.includes('DeletePasswordItem')
      const changed = {
        ...server.records.find((record) => record.id === asked.id)!,
        deletedAt: isDelete ? '2026-10-03T10:00:00Z' : null,
      }
      changed.revision += 10
      server.records = server.records.map((record) => (record.id === changed.id ? changed : record))
      return (isDelete ? { DeletePasswordItem: changed } : { RestorePasswordItem: changed }) as never
    }
    if (document.includes('PurgePasswordItem')) {
      server.records = server.records.filter((record) => record.id !== asked.id)
      return { PurgePasswordItem: true } as never
    }
    if (document.includes('ListPasswordRecipients')) return { ListPasswordRecipients: server.recipients } as never
    if (document.includes('SetPasswordItemShares')) {
      const input = asked.input
      const existing = server.records.find((record) => record.id === input.itemId)!
      // The server refuses a key that names an item key other than the item's
      // current one (contract 4).
      for (const key of input.keys as { itemKey?: string }[]) {
        if (key.itemKey !== existing.itemKey) throw new APIError(STALE_MESSAGE)
      }
      // The server marks an item whose shares got narrower.
      const isNarrowed = existing.shares.some(
        (share) =>
          !input.grantees.some(
            (grantee: { granteeUserId: string | null; granteeCircleId: string | null }) =>
              (grantee.granteeUserId ?? null) === (share.granteeUserId ?? null) &&
              (grantee.granteeCircleId ?? null) === (share.granteeCircleId ?? null),
          ),
      )
      const shared = {
        ...existing,
        revision: existing.revision + 10,
        needsNewKey: existing.needsNewKey || isNarrowed,
        shares: input.grantees.map((grantee: { granteeUserId: string | null; granteeCircleId: string | null }) => ({
          id: 'share-one',
          ...grantee,
          granteeName: 'Fixture Family',
          recipients: server.recipients,
        })),
      }
      server.records = server.records.map((record) => (record.id === shared.id ? shared : record))
      return { SetPasswordItemShares: shared } as never
    }
    if (document.includes('ReadPasswordsAgentKey')) {
      return { ReadPasswordsAgentKey: encodeBase64(agentKeyPair.publicKey) } as never
    }
    if (document.includes('SetPasswordItemUsableByAgent')) {
      const input = asked.input
      const existing = server.records.find((record) => record.id === input.itemId)!
      const changed = {
        ...existing,
        revision: existing.revision + 10,
        isUsableByAgent: input.isUsableByAgent,
        needsNewKey: existing.needsNewKey || (existing.isUsableByAgent && !input.isUsableByAgent),
      }
      server.records = server.records.map((record) => (record.id === changed.id ? changed : record))
      return { SetPasswordItemUsableByAgent: changed } as never
    }
    if (document.includes('RekeyPasswordItem')) {
      const input = asked.input
      const existing = server.records.find((record) => record.id === input.id)!
      if (server.isRekeyFailing) throw new APIError('The server could not re-key the item.')
      if (input.baseRevision !== existing.revision) throw new APIError(STALE_MESSAGE)
      // The server refuses an agent copy for an item it does not list as used.
      if (input.itemKeyByAgentKey !== null && !existing.isUsableByAgent) {
        throw new APIError('The agent does not use this item.')
      }
      const changed: PasswordItemRecord = {
        ...existing,
        encryptedFields: input.encryptedFields,
        itemKey: input.itemKey,
        revision: existing.revision + 10,
        isUsableByAgent: existing.isUsableByAgent && input.itemKeyByAgentKey !== null,
        needsNewKey: false,
        // Old keys are dropped: only the people sealed for in this call have one.
        shares: existing.shares.map((share) => ({
          ...share,
          recipients: share.recipients.map((recipient) => ({
            ...recipient,
            hasKey: input.shareKeys.some(
              (key: { recipientUserId: string }) => key.recipientUserId === recipient.userId,
            ),
          })),
        })),
      }
      // Whoever has a public key and no key now is waiting.
      for (const share of changed.shares) {
        for (const recipient of share.recipients) {
          const isListed = server.waiting.some(
            (waiting) => waiting.itemId === changed.id && waiting.recipient.userId === recipient.userId,
          )
          if (recipient.publicKey !== null && !recipient.hasKey && !isListed) {
            server.waiting = [...server.waiting, { itemId: changed.id, recipient }]
          }
        }
      }
      server.records = server.records.map((record) => (record.id === changed.id ? changed : record))
      return { RekeyPasswordItem: changed } as never
    }
    if (document.includes('ListCirclePeople')) return { ListCirclePeople: [] } as never
    if (document.includes('ListCircles')) {
      return { ListCircles: [{ id: 'circle-one', name: 'Fixture Family', members: [{ userId: 'person-b' }] }] } as never
    }
    if (document.includes('ErasePasswords')) return { ErasePasswords: true } as never
    return {} as never
  })
  return server
}

// recordFor seals an item the way the owner's device would.
//
// The access record in the fields (a record that grants nobody when there is
// none) is signed the way the owner's devices sign it (version 1) unless it
// already carries a signature, or isAccessSigned is false, which makes an item
// whose record nobody signed.
async function recordFor(
  id: string,
  itemKind: ItemKind,
  fields: object,
  overrides: Partial<PasswordItemRecord> = {},
  { isAccessSigned = true }: { isAccessSigned?: boolean } = {},
): Promise<PasswordItemRecord> {
  const isObject = typeof fields === 'object' && fields !== null && !Array.isArray(fields)
  const access = (fields as { access?: ItemAccess } | null)?.access ?? { isUsableByAgent: false, grantees: [] }
  const signedFields =
    isObject && isAccessSigned && access.recordMac === undefined
      ? await withSignedAccess(fixture.accountKey, id, itemKind, fields as never, access, 0)
      : fields
  const sealed = await sealPasswordItem(fixture.accountKey, id, itemKind, signedFields as never, null)
  return {
    id,
    ownerUserId: USER_ID,
    ownerName: 'Person A',
    ownerPublicKey: fixture.input.publicKey,
    itemKind,
    encryptedFields: sealed.encryptedFields,
    itemKey: sealed.itemKey,
    isSharedWithMe: false,
    revision: 3,
    isUsableByAgent: false,
    needsNewKey: false,
    lastUsedByAgentAt: null,
    deletedAt: null,
    createdAt: '2026-10-01T10:00:00Z',
    updatedAt: '2026-10-02T10:00:00Z',
    shares: [],
    ...overrides,
  }
}

// accessOf is the access record an owner's device seals inside the fields
// (recordFor signs it).
function accessOf(grantees: ItemGrantee[], isUsableByAgent = false): ItemAccess {
  return { isUsableByAgent, grantees }
}

// expectSignedAccess checks that the fields carry this access record, at this
// version when one is given, with a signature only the owner's devices make.
async function expectSignedAccess(
  fields: Record<string, any>,
  itemId: string,
  expected: { isUsableByAgent: boolean; grantees: ItemGrantee[] },
  recordVersion?: number,
  itemKind: ItemKind = 'login',
) {
  expect(fields.access).toMatchObject(expected)
  expect(fields.access.grantees).toHaveLength(expected.grantees.length)
  if (recordVersion !== undefined) expect(fields.access.recordVersion).toBe(recordVersion)
  expect(await verifyAccess(fixture.accountKey, itemId, itemKind, fields as never)).not.toBeNull()
}

// pickSignature is the version and signature of a record.
function pickSignature(access: ItemAccess | null): Pick<ItemAccess, 'recordVersion' | 'recordMac'> {
  return { recordVersion: access?.recordVersion, recordMac: access?.recordMac }
}

// MEMORY_NAME is where this device keeps what it remembers of its items: by
// item id only, whoever the server says is signed in.
const MEMORY_NAME = 'teanode.passwords.itemMemory'

type DeviceMemory = {
  highestRecordVersion: number
  highestRecordMac: string
  sealedForUserIds: string[]
  hasMadeAgentCopy: boolean
}

// rememberOnDevice writes what an earlier session left in localStorage: the
// highest record version seen (1 unless said, which is what the fixtures
// sign), who this device sealed the item for, and whether it made the agent a
// copy. An item with such a memory is one this device may re-key by itself.
function rememberOnDevice(itemId: string, memory: Partial<DeviceMemory> = {}) {
  const name = MEMORY_NAME
  const kept = JSON.parse(window.localStorage.getItem(name) ?? '{}')
  window.localStorage.setItem(
    name,
    JSON.stringify({
      ...kept,
      [itemId]: {
        highestRecordVersion: 1,
        highestRecordMac: '',
        sealedForUserIds: [],
        hasMadeAgentCopy: false,
        ...kept[itemId],
        ...memory,
      },
    }),
  )
}

function deviceMemoryOf(itemId: string): DeviceMemory | undefined {
  const raw = window.localStorage.getItem(MEMORY_NAME)
  return raw === null ? undefined : JSON.parse(raw)[itemId]
}

// sharedRecordFor is an item Person B owns, with its key sealed by Person B's
// key pair for this person's key pair. ownerKeyPair is the key pair the share is
// sealed with, and ownerPublicKey the key the server names as the owner's; they
// are the same unless a test says otherwise.
async function sharedRecordFor(
  id: string,
  fields: object,
  ownerKeyPair: PersonKeyPair = personB,
  ownerPublicKey: PersonKeyPair = ownerKeyPair,
): Promise<PasswordItemRecord> {
  const ownerAccountKey = new Uint8Array(32).fill(7)
  const sealed = await sealPasswordItem(ownerAccountKey, id, 'login', fields as never, null)
  const itemKey = await openItemKey(ownerAccountKey, parseSealedValue(sealed.itemKey), id)
  return {
    ...(await recordFor(id, 'login', fields)),
    ownerUserId: 'person-b',
    ownerName: 'Person B',
    ownerPublicKey: encodeBase64(ownerPublicKey.publicKey),
    isSharedWithMe: true,
    encryptedFields: sealed.encryptedFields,
    itemKey: await sealItemKeyFor(itemKey, ownerKeyPair, fixture.input.publicKey, id, 'person-b', USER_ID),
  }
}

async function recordsToList(): Promise<PasswordItemRecord[]> {
  return [
    await recordFor('login-one', 'login', {
      ...emptyLoginFields(),
      name: 'Example login',
      userName: 'ada@example.test',
      password: 'invented-password-1',
      sites: ['https://login.example.test/start'],
    }),
    await recordFor('card-one', 'card', {
      ...emptyCardFields(),
      name: 'Travel card',
      cardholderName: 'Ada Example',
      number: '4111 1111 1111 1111',
      brand: 'Visa',
      securityCode: '123',
    }),
    await recordFor('note-one', 'note', {
      ...emptyNoteFields(),
      name: 'Router note',
      text: 'The router is in the hall.',
    }),
    await recordFor('login-two', 'login', {
      ...emptyLoginFields(),
      name: 'Bank',
      userName: 'grace',
      password: 'invented-password-2',
      sites: ['https://bank.example.test'],
      folder: 'Money',
    }),
    await recordFor(
      'login-agent',
      'login',
      {
        ...emptyLoginFields(),
        name: 'Agent login',
        userName: 'agent-user',
        password: 'invented-password-3',
        access: accessOf([], true),
      },
      { isUsableByAgent: true },
    ),
    await sharedRecordFor('login-shared', {
      ...emptyLoginFields(),
      name: 'Shared login',
      userName: 'shared-user',
      password: 'invented-password-4',
    }),
    await recordFor(
      'login-old',
      'login',
      { ...emptyLoginFields(), name: 'Old login', userName: 'old-user' },
      { deletedAt: '2026-10-03T10:00:00Z' },
    ),
  ]
}

// knowKeyOnDevice writes what this device keeps once it knows a person's key.
function knowKeyOnDevice(otherUserId: string, publicKey: string, extra: Record<string, unknown> = {}) {
  const name = `teanode.passwords.knownKeys:${USER_ID}`
  const kept = JSON.parse(window.localStorage.getItem(name) ?? '{}')
  window.localStorage.setItem(
    name,
    JSON.stringify({
      ...kept,
      [otherUserId]: {
        publicKey,
        name: '',
        isCompared: false,
        previousPublicKey: null,
        hasChangeNotice: false,
        ...extra,
      },
    }),
  )
}

// storedKeys is the known key of each person this device keeps, or null when
// it keeps none.
function storedKeys(): Record<string, string> | null {
  const raw = window.localStorage.getItem(`teanode.passwords.knownKeys:${USER_ID}`)
  if (raw === null) return null
  return Object.fromEntries(
    Object.entries(JSON.parse(raw)).map(([key, value]) => [key, (value as { publicKey: string }).publicKey]),
  )
}

function storedKnownKey(otherUserId: string): Record<string, unknown> | null {
  const raw = window.localStorage.getItem(`teanode.passwords.knownKeys:${USER_ID}`)
  return raw === null ? null : (JSON.parse(raw)[otherUserId] ?? null)
}

function Address() {
  const location = useLocation()
  return <output data-testid="address">{location.search}</output>
}

function renderAt(address: string, permissions = ['passwords:use', 'circle:use']) {
  return render(
    <TranslationProvider>
      <ToastProvider>
        <SessionProvider
          value={{
            userId: USER_ID,
            authenticated: true,
            authenticationRequired: true,
            username: 'person-a-login',
            passkeysEnabled: false,
            permissions: { everywhere: permissions, byDomain: [] },
          }}
        >
          <MemoryRouter initialEntries={[address]}>
            <BreadcrumbProvider>
              <Routes>
                <Route
                  path="/passwords"
                  element={
                    <>
                      <PasswordsPage />
                      <Address />
                    </>
                  }
                />
                <Route path="/circles" element={<p>Circles page</p>} />
              </Routes>
            </BreadcrumbProvider>
          </MemoryRouter>
        </SessionProvider>
      </ToastProvider>
    </TranslationProvider>,
  )
}

// openSession unlocks the in-memory session the way the Unlock screen does,
// before the page is drawn.
async function openSession() {
  await passwordsSession.unlock(USER_ID, { secret, isRecoveryKey: false })
}

function calls(operation: string) {
  return vi.mocked(graphql).mock.calls.filter(([document]) => document.includes(operation))
}

// orderOf lists the operations the page called, in order, by the short names
// given (an operation not named is left out).
function orderOf(names: Record<string, string>): string[] {
  return vi
    .mocked(graphql)
    .mock.calls.map(
      ([document]) => Object.entries(names).find(([operation]) => document.includes(operation))?.[1] ?? '',
    )
    .filter(Boolean)
}

// fieldsSentIn opens the fields a save or a re-key sent, the way the owner's
// device would read them back.
async function fieldsSentIn(
  input: Record<string, any>,
  itemId: string,
  itemKind: ItemKind = 'login',
): Promise<Record<string, any>> {
  const itemKey = await openItemKey(fixture.accountKey, parseSealedValue(input.itemKey as string), itemId)
  return (await openItemFields(itemKey, parseSealedValue(input.encryptedFields as string), itemId, itemKind)) as Record<
    string,
    any
  >
}

function rowNames(): string[] {
  return screen
    .getAllByRole('row')
    .slice(1)
    .map((row) => within(row).getAllByRole('cell')[0].textContent ?? '')
}

// --- setting up -----------------------------------------------------------

it('explains Passwords, asks for the passphrase twice, and needs the box ticked before it finishes', async () => {
  serve({
    setup: { ...setupView, isSetUp: false, passphraseSalt: null, passphraseIterationCount: null, publicKey: null },
  })
  renderAt('/passwords')

  expect(
    await screen.findByText(/If you lose both your passphrase and your recovery key, they are gone for good/),
  ).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'Set up' }))

  const passphraseDialog = await screen.findByRole('dialog', { name: 'Choose a passphrase' })
  fireEvent.change(within(passphraseDialog).getByLabelText('Passphrase'), { target: { value: 'short' } })
  fireEvent.change(within(passphraseDialog).getByLabelText('Type it again'), { target: { value: 'short' } })
  expect(within(passphraseDialog).getByText('Too short.')).toBeTruthy()
  fireEvent.click(within(passphraseDialog).getByRole('button', { name: 'Continue' }))
  expect(await screen.findByText('Use at least 12 characters.')).toBeTruthy()

  fireEvent.change(within(passphraseDialog).getByLabelText('Passphrase'), { target: { value: PASSPHRASE } })
  fireEvent.click(within(passphraseDialog).getByRole('button', { name: 'Continue' }))
  expect(await screen.findByText('The two passphrases are not the same.')).toBeTruthy()

  fireEvent.change(within(passphraseDialog).getByLabelText('Type it again'), { target: { value: PASSPHRASE } })
  fireEvent.click(within(passphraseDialog).getByRole('button', { name: 'Continue' }))

  const recoveryDialog = await screen.findByRole('dialog', { name: 'Save your recovery key' }, { timeout: 15_000 })
  const recoveryKeyText = within(recoveryDialog).getByLabelText('Recovery key').textContent ?? ''
  expect(recoveryKeyText).toMatch(/^([A-Z2-7]{4} ){12}[A-Z2-7]{4}$/)
  expect(within(recoveryDialog).getByRole('button', { name: 'Download' })).toBeTruthy()
  expect(within(recoveryDialog).getByText(/cannot be recovered/)).toBeTruthy()

  // Nothing has been sent, and Finish waits for the box.
  const finish = within(recoveryDialog).getByRole('button', { name: 'Finish setting up' })
  expect(finish.hasAttribute('disabled')).toBe(true)
  expect(calls('SetUpPasswords')).toHaveLength(0)

  fireEvent.click(within(recoveryDialog).getByRole('checkbox', { name: 'I saved my recovery key' }))
  expect(finish.hasAttribute('disabled')).toBe(false)
  fireEvent.click(finish)

  expect(await screen.findByText('Passwords is set up and open.')).toBeTruthy()
  expect(calls('SetUpPasswords')).toHaveLength(1)
  const sent = JSON.stringify(calls('SetUpPasswords')[0][1])
  // The server gets sealed keys and hashes: not the passphrase, not the
  // recovery key in either spelling.
  expect(sent).not.toContain(PASSPHRASE)
  expect(sent).not.toContain(recoveryKeyText)
  expect(sent).not.toContain(recoveryKeyText.replace(/ /g, ''))
  expect(calls('SetUpPasswords')[0][1]).toMatchObject({
    input: {
      passphraseIterationCount: 600000,
      publicKey: expect.any(String),
      privateKeyByAccountKey: expect.any(String),
    },
  })
  expect(passwordsSession.isUnlocked).toBe(true)
}, 60_000)

it('rates a passphrase by its length and variety', () => {
  expect(passphraseStrength('')).toBe('empty')
  expect(passphraseStrength('abc')).toBe('short')
  expect(passphraseStrength('abcdefgh')).toBe('short')
  expect(passphraseStrength('Abcdefg1hijk')).toBe('weak')
  expect(passphraseStrength('correct horse battery')).toBe('strong')
  expect(passphraseStrength('Abcdefg1hijkmnop')).toBe('fair')
  expect(passphraseStrength('Abcdefg1hijkmnopqrst')).toBe('strong')
})

// --- unlocking --------------------------------------------------------------

it('says so in a toast when the passphrase is wrong, and opens with the right one', async () => {
  serve({ records: await recordsToList() })
  renderAt('/passwords')

  await screen.findByRole('heading', { name: 'Passwords is locked' })
  fireEvent.change(screen.getByLabelText('Passphrase'), { target: { value: 'not the passphrase' } })
  fireEvent.click(screen.getByRole('button', { name: 'Unlock' }))
  expect(await screen.findByText('That passphrase is not right.')).toBeTruthy()
  expect(passwordsSession.isUnlocked).toBe(false)
  // What was typed stays, so a typo can be mended.
  expect((screen.getByLabelText('Passphrase') as HTMLInputElement).value).toBe('not the passphrase')

  fireEvent.change(screen.getByLabelText('Passphrase'), { target: { value: PASSPHRASE } })
  fireEvent.click(screen.getByRole('button', { name: 'Unlock' }))
  expect(await screen.findByRole('link', { name: 'Example login' }, { timeout: 15_000 })).toBeTruthy()
  expect(passwordsSession.isUnlocked).toBe(true)
}, 60_000)

it('opens with the recovery key and refuses one that is not a recovery key', async () => {
  serve()
  renderAt('/passwords')

  await screen.findByRole('heading', { name: 'Passwords is locked' })
  fireEvent.click(screen.getByRole('button', { name: 'Use the recovery key' }))
  fireEvent.change(screen.getByLabelText('Recovery key'), { target: { value: 'ABCD EFGH' } })
  fireEvent.click(screen.getByRole('button', { name: 'Unlock' }))
  expect(await screen.findByText(/That is not a recovery key/)).toBeTruthy()
  expect(calls('UnlockPasswords')).toHaveLength(0)
})

it('locks after the lock time without use', async () => {
  serve({ records: await recordsToList() })
  await openSession()
  passwordsSession.setLockAfterMinutes(1)
  passwordsSession.touch(1_000)
  expect(passwordsSession.lockIfIdle(1_000 + 59_000)).toBe(false)
  expect(passwordsSession.isUnlocked).toBe(true)
  expect(passwordsSession.lockIfIdle(1_000 + 60_000)).toBe(true)
  expect(passwordsSession.isUnlocked).toBe(false)
  expect(passwordsSession.getSnapshot().items).toEqual([])
})

// --- the list ----------------------------------------------------------------

it('lists the items in a table with their sites, a shared marker and an agent marker', async () => {
  serve({ records: await recordsToList() })
  await openSession()
  renderAt('/passwords')

  await screen.findByRole('link', { name: 'Example login' })
  // Alphabetical, without what was deleted.
  expect(rowNames()).toEqual(['Agent login', 'Bank', 'Example login', 'Router note', 'Shared login', 'Travel card'])
  const exampleRow = screen.getByRole('link', { name: 'Example login' }).closest('tr')!
  expect(within(exampleRow).getByText('ada@example.test')).toBeTruthy()
  expect(within(exampleRow).getByText('login.example.test')).toBeTruthy()
  const sharedRow = screen.getByRole('link', { name: 'Shared login' }).closest('tr')!
  expect(within(sharedRow).getByText('Shared by Person B')).toBeTruthy()
  const agentRow = screen.getByRole('link', { name: 'Agent login' }).closest('tr')!
  expect(within(agentRow).getByText('Agent')).toBeTruthy()
  const cardRow = screen.getByRole('link', { name: 'Travel card' }).closest('tr')!
  expect(within(cardRow).getByText('Visa •••• 1111')).toBeTruthy()
})

it('narrows the list by kind from the address, and by search and folder without putting them there', async () => {
  serve({ records: await recordsToList() })
  await openSession()
  renderAt('/passwords?kind=card')

  await screen.findByRole('link', { name: 'Travel card' })
  expect(rowNames()).toEqual(['Travel card'])

  cleanup()
  renderAt('/passwords')
  await screen.findByRole('link', { name: 'Bank' })
  fireEvent.click(screen.getByRole('combobox', { name: 'Folder' }))
  fireEvent.click(await screen.findByRole('option', { name: 'Money' }))
  expect(rowNames()).toEqual(['Bank'])
  // The folder name is a decrypted value: it stays out of the address.
  expect(screen.getByTestId('address').textContent).toBe('')

  fireEvent.click(screen.getByRole('combobox', { name: 'Folder' }))
  fireEvent.click(await screen.findByRole('option', { name: 'All folders' }))
  await screen.findByRole('link', { name: 'Bank' })
  fireEvent.change(screen.getByRole('searchbox', { name: 'Search items' }), { target: { value: 'ada@example' } })
  await waitFor(() => expect(rowNames()).toEqual(['Example login']))
  // Neither is the search text.
  expect(screen.getByTestId('address').textContent).toBe('')
  // A password is never searched.
  fireEvent.change(screen.getByRole('searchbox', { name: 'Search items' }), { target: { value: 'invented-password' } })
  expect(await screen.findByText('No items match.')).toBeTruthy()
})

it('filters in plain code', async () => {
  serve({ records: await recordsToList() })
  await openSession()
  const items = passwordsSession.getSnapshot().items
  const base: ItemFilter = { search: '', itemKind: '', folder: '', isDeleted: false }
  expect(filterItems(items, base)).toHaveLength(6)
  expect(filterItems(items, { ...base, isDeleted: true }).map((item) => item.fields.name)).toEqual(['Old login'])
  expect(filterItems(items, { ...base, itemKind: 'note' }).map((item) => item.fields.name)).toEqual(['Router note'])
  expect(filterItems(items, { ...base, folder: NO_FOLDER })).toHaveLength(5)
  expect(filterItems(items, { ...base, search: 'EXAMPLE.test bank' }).map((item) => item.fields.name)).toEqual(['Bank'])
})

it('lists what was deleted lately with a way to restore it', async () => {
  const server = serve({ records: await recordsToList() })
  await openSession()
  renderAt('/passwords?deleted=1')

  await screen.findByText('Old login')
  expect(rowNames()).toEqual(['Old login'])
  fireEvent.click(screen.getByRole('button', { name: 'Old login: Restore' }))
  await waitFor(() => expect(calls('RestorePasswordItem')).toHaveLength(1))
  expect(calls('RestorePasswordItem')[0][1]).toEqual({ id: 'login-old' })
  expect(await screen.findByText('Old login was restored.')).toBeTruthy()
  expect(server.records.find((record) => record.id === 'login-old')?.deletedAt).toBeNull()
})

it('deletes an item in Recently deleted for good, after asking', async () => {
  const server = serve({ records: await recordsToList() })
  await openSession()
  renderAt('/passwords?deleted=1')

  await screen.findByText('Old login')
  fireEvent.click(screen.getByRole('button', { name: 'Old login: Delete forever' }))
  const dialog = await screen.findByRole('alertdialog', { name: 'Delete Old login forever?' })
  expect(within(dialog).getByText(/cannot be brought back/)).toBeTruthy()
  // Leaving the question does nothing.
  fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }))
  expect(calls('PurgePasswordItem')).toHaveLength(0)

  fireEvent.click(screen.getByRole('button', { name: 'Old login: Delete forever' }))
  fireEvent.click(within(await screen.findByRole('alertdialog')).getByRole('button', { name: 'Delete forever' }))
  await waitFor(() => expect(calls('PurgePasswordItem')).toHaveLength(1))
  expect(calls('PurgePasswordItem')[0][1]).toEqual({ id: 'login-old' })
  expect(await screen.findByText('Old login was deleted forever.')).toBeTruthy()
  expect(server.records.some((record) => record.id === 'login-old')).toBe(false)
  expect(passwordsSession.getSnapshot().items.some((item) => item.id === 'login-old')).toBe(false)
})

// --- one item -----------------------------------------------------------------

it('keeps a password hidden until Show, and lists the sites as links', async () => {
  serve({ records: await recordsToList() })
  await openSession()
  renderAt('/passwords?item=login-one')

  expect(await screen.findByRole('heading', { name: 'Example login' })).toBeTruthy()
  expect(screen.queryByText('invented-password-1')).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'Show Password' }))
  expect(screen.getByText('invented-password-1')).toBeTruthy()
  const link = screen.getByRole('link', { name: 'login.example.test' })
  expect(link.getAttribute('href')).toBe('https://login.example.test/start')
  expect(link.getAttribute('rel')).toContain('noopener')
})

it('shows a one-time code with the seconds left, and the passkeys of a login', async () => {
  const record = await recordFor('login-code', 'login', {
    ...emptyLoginFields(),
    name: 'Code login',
    oneTimeCodeUri: 'otpauth://totp/Example:ada?secret=JBSWY3DPEHPK3PXP&digits=6&period=30',
    passkeys: [
      {
        credentialId: 'AAAA',
        relyingPartyId: 'example.test',
        userHandle: 'AAAA',
        userName: 'ada',
        userDisplayName: 'Ada',
        privateKey: 'AAAA',
        createdAt: '2026-09-01T10:00:00Z',
      },
    ],
  })
  serve({ records: [record] })
  await openSession()
  renderAt('/passwords?item=login-code')

  const code = await screen.findByText(/^\d{3} \d{3}$/)
  expect(code).toBeTruthy()
  expect(screen.getByText(/^Changes in \d+s$/)).toBeTruthy()
  expect(screen.getByText('example.test')).toBeTruthy()
  expect(screen.getByText(/created/)).toBeTruthy()
})

it('shows the owner of a shared item and gives it no Edit, Share or Delete', async () => {
  serve({ records: await recordsToList() })
  await openSession()
  renderAt('/passwords?item=login-shared')

  expect(await screen.findByText('Person B shared this with you. Only Person B can change it.')).toBeTruthy()
  expect(screen.queryByRole('button', { name: /Edit/ })).toBeNull()
  expect(screen.queryByRole('button', { name: /Share/ })).toBeNull()
  expect(screen.queryByRole('button', { name: /Delete/ })).toBeNull()
  // It was opened with this person's own key pair and the owner's public key,
  // naming the owner and their own user id.
  fireEvent.click(screen.getByRole('button', { name: 'Show Password' }))
  expect(screen.getByText('invented-password-4')).toBeTruthy()
})

it("keeps the owner's key the first time a shared item opens with it, with no notice", async () => {
  serve({ records: await recordsToList() })
  await openSession()
  expect(storedKeys()).toEqual({ 'person-b': encodeBase64(personB.publicKey) })
  expect(storedKnownKey('person-b')).toMatchObject({ isCompared: false, hasChangeNotice: false })
  expect(passwordsSession.getSnapshot().keyNotices).toEqual([])
})

it('does not keep the key of an owner whose share did not open', async () => {
  const stranger = await generatePersonKeyPair()
  // The server names a key for Person B that did not seal the share.
  serve({
    records: [await sharedRecordFor('login-forged', { ...emptyLoginFields(), name: 'Forged' }, stranger, personB)],
  })
  await openSession()
  expect(window.localStorage.getItem(`teanode.passwords.knownKeys:${USER_ID}`)).toBeNull()
  expect(passwordsSession.getSnapshot().items[0].isUnreadable).toBe(true)
})

it('opens a shared item whose owner key changed with the new key, and shows a notice until it is dismissed', async () => {
  const oldKeyPair = await generatePersonKeyPair()
  knowKeyOnDevice('person-b', encodeBase64(oldKeyPair.publicKey), { name: 'Person B', isCompared: true })
  serve({ records: await recordsToList() })
  await openSession()
  renderAt('/passwords?item=login-shared')

  // The item is open, and nothing waits on the notice.
  fireEvent.click(await screen.findByRole('button', { name: 'Show Password' }))
  expect(screen.getByText('invented-password-4')).toBeTruthy()
  expect(
    screen.getByText(
      "Person B's key changed. This happens when they erase Passwords and set it up again. If you did not expect it, compare safety codes with them.",
    ),
  ).toBeTruthy()
  expect(storedKeys()).toEqual({ 'person-b': encodeBase64(personB.publicKey) })
  expect(storedKnownKey('person-b')).toMatchObject({ isCompared: false, hasChangeNotice: true })

  // Compare codes shows the code before, the code now and the person's own.
  fireEvent.click(screen.getByRole('button', { name: 'Compare codes' }))
  const dialog = await screen.findByRole('alertdialog')
  expect(await within(dialog).findByText(await safetyCode(oldKeyPair.publicKey))).toBeTruthy()
  expect(await within(dialog).findByText(await safetyCode(personB.publicKey))).toBeTruthy()
  expect(await within(dialog).findByText(await safetyCode(fixture.keyPair.publicKey))).toBeTruthy()

  // Comparing marks the key as compared and ends the notice.
  fireEvent.click(within(dialog).getByRole('button', { name: 'The codes match' }))
  await waitFor(() => expect(screen.queryByRole('button', { name: 'Compare codes' })).toBeNull())
  expect(storedKnownKey('person-b')).toMatchObject({ isCompared: true, hasChangeNotice: false })
})

it('keeps the notice about a changed owner key until it is dismissed', async () => {
  const oldKeyPair = await generatePersonKeyPair()
  knowKeyOnDevice('person-b', encodeBase64(oldKeyPair.publicKey), { name: 'Person B' })
  serve({ records: await recordsToList() })
  await openSession()
  renderAt('/passwords')

  expect(await screen.findByRole('button', { name: 'Dismiss' })).toBeTruthy()
  // Another read of the same key does not make a second notice.
  await passwordsSession.refresh()
  expect(passwordsSession.getSnapshot().keyNotices).toHaveLength(1)
  fireEvent.click(screen.getByRole('button', { name: 'Dismiss' }))
  await waitFor(() => expect(screen.queryByRole('button', { name: 'Dismiss' })).toBeNull())
  expect(passwordsSession.getSnapshot().keyNotices).toEqual([])
  expect(storedKeys()).toEqual({ 'person-b': encodeBase64(personB.publicKey) })
})

it('refuses to unlock when the public key the server sends is not the one the private key makes', async () => {
  serve({ records: await recordsToList(), unlockPublicKey: encodeBase64(personB.publicKey) })
  renderAt('/passwords')

  await screen.findByRole('heading', { name: 'Passwords is locked' })
  fireEvent.change(screen.getByLabelText('Passphrase'), { target: { value: PASSPHRASE } })
  fireEvent.click(screen.getByRole('button', { name: 'Unlock' }))
  expect(
    await screen.findByText(
      'The server sent a public key that is not the one your private key makes, so Passwords stays locked.',
      undefined,
      { timeout: 15_000 },
    ),
  ).toBeTruthy()
  expect(passwordsSession.isUnlocked).toBe(false)
  expect(calls('ListPasswordItems')).toHaveLength(0)
}, 60_000)

// --- the editor ----------------------------------------------------------------

it('saves a changed item with the revision it was based on and its own key', async () => {
  const records = await recordsToList()
  const server = serve({ records })
  await openSession()
  renderAt('/passwords?item=login-one&edit=1')

  const dialog = await screen.findByRole('dialog', { name: 'Edit login' })
  fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'Renamed login' } })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))

  await waitFor(() => expect(calls('SavePasswordItem')).toHaveLength(1))
  const input = (calls('SavePasswordItem')[0][1] as { input: Record<string, unknown> }).input
  expect(input).toMatchObject({ id: 'login-one', itemKind: 'login', baseRevision: 3 })
  expect(await screen.findByText('Renamed login was saved.')).toBeTruthy()

  // The fields went sealed under the item's key, which did not change.
  const itemKey = await openItemKey(fixture.accountKey, parseSealedValue(input.itemKey as string), 'login-one')
  const fields = await openItemFields(itemKey, parseSealedValue(input.encryptedFields as string), 'login-one', 'login')
  expect(fields).toMatchObject({ name: 'Renamed login', userName: 'ada@example.test', password: 'invented-password-1' })
  expect(JSON.stringify(input)).not.toContain('invented-password-1')
  expect(server.records.find((record) => record.id === 'login-one')?.revision).toBe(13)
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
})

it('makes the id on the device and sends no revision for a new item', async () => {
  serve()
  await openSession()
  renderAt('/passwords?new=note')

  const dialog = await screen.findByRole('dialog', { name: 'New note' })
  const save = within(dialog).getByRole('button', { name: 'Save' })
  expect(save.hasAttribute('disabled')).toBe(true)
  fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'Spare key place' } })
  fireEvent.change(within(dialog).getByLabelText('Note'), { target: { value: 'Under the blue pot.' } })
  fireEvent.click(save)

  await waitFor(() => expect(calls('SavePasswordItem')).toHaveLength(1))
  const input = (calls('SavePasswordItem')[0][1] as { input: Record<string, unknown> }).input
  expect(input.id).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/)
  expect(input.baseRevision).toBeNull()
  expect(input.itemKind).toBe('note')
  expect(JSON.stringify(input)).not.toContain('Under the blue pot')
  // The address now names the new item.
  await waitFor(() => expect(screen.getByTestId('address').textContent).toContain(`item=${input.id}`))
  expect(await screen.findByText('Spare key place was saved.')).toBeTruthy()
})

it('keeps what was typed when another device changed the item first, and takes the other fields from their change', async () => {
  const server = serve({ records: await recordsToList() })
  await openSession()
  renderAt('/passwords?item=login-one&edit=1')

  const dialog = await screen.findByRole('dialog', { name: 'Edit login' })
  // The other device saved a new user name and password at a later revision,
  // while this person changed the name.
  server.isStale = true
  server.records[server.records.findIndex((record) => record.id === 'login-one')] = await recordFor(
    'login-one',
    'login',
    {
      ...emptyLoginFields(),
      name: 'Example login',
      userName: 'other@example.test',
      password: 'password-from-the-other-device',
      sites: ['https://login.example.test/start'],
    },
    { revision: 9 },
  )
  fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'Late change' } })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))

  expect(await screen.findByText(/Your changes are kept/)).toBeTruthy()
  expect(calls('SavePasswordItem')).toHaveLength(1)
  // Still open, with the typed name and the other device's values elsewhere.
  expect((within(dialog).getByLabelText('Name') as HTMLInputElement).value).toBe('Late change')
  expect((within(dialog).getByLabelText('User name') as HTMLInputElement).value).toBe('other@example.test')
  expect((within(dialog).getByLabelText('Password') as HTMLInputElement).value).toBe('password-from-the-other-device')
  expect(within(dialog).queryByRole('button', { name: 'Reload' })).toBeNull()

  // The next save is based on the revision just read, and carries both.
  server.isStale = false
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(calls('SavePasswordItem')).toHaveLength(2))
  const input = (calls('SavePasswordItem')[1][1] as { input: any }).input
  expect(input.baseRevision).toBe(9)
  expect(await fieldsSentIn(input, 'login-one')).toMatchObject({
    name: 'Late change',
    userName: 'other@example.test',
    password: 'password-from-the-other-device',
  })
})

it('moves typed fields onto a newer copy: an edited field stays, every other field is the new one', () => {
  const original: Record<string, unknown> = { name: 'A', userName: 'ada', passkeys: [{ id: 'one' }] }
  const fresh: Record<string, unknown> = {
    name: 'B',
    userName: 'grace',
    passkeys: [{ id: 'one' }, { id: 'two' }],
    access: { grantees: [] },
  }
  // Only the name was typed.
  expect(rebasedFields({ ...original, name: 'Typed' }, original, fresh)).toEqual({ ...fresh, name: 'Typed' })
  // Nothing typed: the new copy as it is.
  expect(rebasedFields(original, original, fresh)).toEqual(fresh)
})

it('refuses text that is not a one-time code, and makes an address from a bare secret', async () => {
  serve({ records: await recordsToList() })
  await openSession()
  renderAt('/passwords?item=login-one&edit=1')

  const dialog = await screen.findByRole('dialog', { name: 'Edit login' })
  fireEvent.change(within(dialog).getByLabelText(/One-time code/), { target: { value: '!!! not a secret' } })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
  expect(await screen.findByText(/That is not a one-time code/)).toBeTruthy()
  expect(calls('SavePasswordItem')).toHaveLength(0)

  expect(oneTimeCodeUriFromText('jbsw y3dp ehpk 3pxp', 'Example')).toBe(
    'otpauth://totp/Example?secret=jbswy3dpehpk3pxp',
  )
  expect(oneTimeCodeUriFromText('', 'Example')).toBe('')
  expect(oneTimeCodeUriFromText('otpauth://totp/Example?secret=JBSWY3DPEHPK3PXP', 'Example')).toBe(
    'otpauth://totp/Example?secret=JBSWY3DPEHPK3PXP',
  )
  expect(sitesFromText('example.test\n\nhttps://other.example.test/login\nexample.test')).toEqual([
    'https://example.test',
    'https://other.example.test/login',
  ])
})

it('fills the password as soon as the generator opens and on every change, and Done closes the panel', async () => {
  serve()
  await openSession()
  renderAt('/passwords?new=login')

  const dialog = await screen.findByRole('dialog', { name: 'New login' })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Generate' }))
  const password = () => (within(dialog).getByLabelText('Password') as HTMLInputElement).value
  const generated = () => within(dialog).getByLabelText('Generated password').textContent ?? ''
  expect(generated()).toHaveLength(20)
  expect(password()).toBe(generated())

  const first = generated()
  fireEvent.click(within(dialog).getByRole('button', { name: 'Another password' }))
  expect(generated()).not.toBe(first)
  expect(password()).toBe(generated())

  fireEvent.change(within(dialog).getByLabelText('Length'), { target: { value: '30' } })
  expect(generated()).toHaveLength(30)
  expect(password()).toBe(generated())

  // The panel stays open until Done, and Done keeps the password.
  expect(within(dialog).queryByRole('button', { name: 'Use this password' })).toBeNull()
  const kept = password()
  fireEvent.click(within(dialog).getByRole('button', { name: 'Done' }))
  expect(within(dialog).queryByLabelText('Generated password')).toBeNull()
  expect(password()).toBe(kept)
})

// --- sharing --------------------------------------------------------------------

it('shows each recipient with a safety code, or waiting if they have not set up Passwords, and seals the key for the ones who have', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  const recipients: PasswordRecipient[] = [
    {
      userId: 'person-b',
      name: 'Person B',
      username: 'person-b-login',
      publicKey: personBPublicKey,
      hasKey: false,
      hasTooMuchShared: false,
    },
    {
      userId: 'person-c',
      name: 'Person C',
      username: 'person-c-login',
      publicKey: null,
      hasKey: false,
      hasTooMuchShared: false,
    },
  ]
  const server = serve({ records: await onlyExampleLogin(), recipients })
  await openSession()
  renderAt('/passwords?item=login-one')

  fireEvent.click(await screen.findByRole('button', { name: 'Example login: Share' }))
  const dialog = await screen.findByRole('dialog', { name: 'Share Example login' })
  fireEvent.click(await within(dialog).findByRole('combobox', { name: 'Share with' }))
  fireEvent.click(await screen.findByRole('option', { name: 'Fixture Family' }))
  fireEvent.click(within(dialog).getByRole('button', { name: 'Add' }))

  expect(await within(dialog).findByText('Everyone in Fixture Family')).toBeTruthy()
  expect(calls('ListPasswordRecipients')[0][1]).toEqual({ granteeUserId: null, granteeCircleId: 'circle-one' })
  expect(await within(dialog).findByText(await safetyCode(personB.publicKey))).toBeTruthy()
  expect(within(dialog).getByText('Waiting until they set up Passwords')).toBeTruthy()
  expect(within(dialog).getByText('Person C (person-c-login)')).toBeTruthy()

  // Nobody is asked to confirm the key: Compare codes is there to use or not.
  expect(within(dialog).getByRole('button', { name: 'Compare codes' })).toBeTruthy()
  expect(within(dialog).queryByRole('button', { name: 'The safety code matches' })).toBeNull()

  // Person B's key is new to this device and is sealed for at once.
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save sharing' }))
  await waitFor(() => expect(calls('SetPasswordItemShares')).toHaveLength(1))
  // Widening: the fields are saved with the wider record first, then the
  // server is asked for the share.
  expect(orderOf({ SavePasswordItem: 'save', SetPasswordItemShares: 'shares' })).toEqual(['save', 'shares'])
  const savedFields = await fieldsSentIn((calls('SavePasswordItem')[0][1] as { input: any }).input, 'login-one')
  await expectSignedAccess(
    savedFields,
    'login-one',
    { isUsableByAgent: false, grantees: [{ circleId: 'circle-one' }] },
    2,
  )
  const input = (calls('SetPasswordItemShares')[0][1] as { input: Record<string, any> }).input
  expect(input.itemId).toBe('login-one')
  expect(input.grantees).toEqual([{ granteeUserId: null, granteeCircleId: 'circle-one' }])
  // A key only for the person who has set up Passwords, and it opens with
  // their key pair to the item's own key.
  expect(input.keys).toHaveLength(1)
  expect(input.keys[0]).toMatchObject({ recipientUserId: 'person-b', recipientPublicKey: personBPublicKey })
  const sharedKey = await openForKeyPair(
    parseSealedForKeyPair(input.keys[0].itemKeyByRecipientKey),
    personB,
    decodeBase64(fixture.input.publicKey),
    'login-one',
    USER_ID,
    'person-b',
  )
  const ownKey = await openItemKey(
    fixture.accountKey,
    parseSealedValue(server.records.find((record) => record.id === 'login-one')!.itemKey),
    'login-one',
  )
  expect(encodeBase64(sharedKey)).toBe(encodeBase64(ownKey))
  expect(await screen.findByText('Sharing saved. 1 person will get access when they set up Passwords.')).toBeTruthy()
  // The key sealed for is the key known for Person B from now on.
  expect(storedKeys()).toEqual({ 'person-b': personBPublicKey })
  expect(passwordsSession.getSnapshot().keyNotices).toEqual([])
})

// sharedWithPersonB is a record of an item the person owns that is shared with
// Person B, who had not set up Passwords then. The sealed access record names
// Person B, as the owner's device wrote it when it shared.
async function sharedWithPersonB(id: string, name: string): Promise<PasswordItemRecord> {
  return recordFor(
    id,
    'login',
    { ...emptyLoginFields(), name, access: accessOf([{ userId: 'person-b' }]) },
    {
      shares: [
        {
          id: `share-${id}`,
          granteeUserId: 'person-b',
          granteeCircleId: null,
          granteeName: 'Person B',
          recipients: [
            {
              userId: 'person-b',
              name: 'Person B',
              username: 'person-b-login',
              publicKey: null,
              hasKey: false,
              hasTooMuchShared: false,
            },
          ],
        },
      ],
    },
  )
}

function waitingFor(itemId: string, publicKey: string) {
  return {
    itemId,
    recipient: {
      userId: 'person-b',
      name: 'Person B',
      username: 'person-b-login',
      publicKey,
      hasKey: false,
      hasTooMuchShared: false,
    },
  }
}

it('gives the key to a person who set up Passwords after the item was shared, and says so', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  // Person B's key is one the owner confirmed on this device when they shared
  // with them before.
  knowKeyOnDevice('person-b', personBPublicKey)
  const server = serve({
    records: [...(await recordsToList()), await sharedWithPersonB('login-given', 'Given login')],
    waiting: [
      waitingFor('login-given', personBPublicKey),
      // An item somebody else owns is not this person's to give.
      waitingFor('login-shared', personBPublicKey),
      // An item that is not shared with this person at all gets no key, even
      // though the server lists the person as waiting for it.
      waitingFor('login-one', personBPublicKey),
    ],
  })
  rememberOnDevice('login-given', { sealedForUserIds: ['person-b'] })
  await openSession()
  renderAt('/passwords')

  expect(await screen.findByText('Sent 1 key for the items you shared, now that people set up Passwords.')).toBeTruthy()
  expect(calls('AddPasswordShareKeys')).toHaveLength(1)
  const keys = (calls('AddPasswordShareKeys')[0][1] as { keys: Record<string, string>[] }).keys
  expect(keys).toHaveLength(1)
  expect(keys[0]).toMatchObject({
    itemId: 'login-given',
    recipientUserId: 'person-b',
    recipientPublicKey: personBPublicKey,
  })
  // The key names the item key it was sealed from, which the server checks.
  expect(keys[0].itemKey).toBe(server.records.find((record) => record.id === 'login-given')!.itemKey)
  const opened = await openForKeyPair(
    parseSealedForKeyPair(keys[0].itemKeyByRecipientKey),
    personB,
    decodeBase64(fixture.input.publicKey),
    'login-given',
    USER_ID,
    'person-b',
  )
  expect(opened).toHaveLength(32)
})

it('gives a waiting person the key at once for a changed key, keeps it as the known key and shows the notice', async () => {
  const oldKeyPair = await generatePersonKeyPair()
  const newPublicKey = encodeBase64(personB.publicKey)
  // Person B was shared with before, and this device kept the key it saw then.
  knowKeyOnDevice('person-b', encodeBase64(oldKeyPair.publicKey), { name: 'Person B', isCompared: true })
  serve({
    records: [await sharedWithPersonB('login-given', 'Given login')],
    waiting: [waitingFor('login-given', newPublicKey)],
  })
  rememberOnDevice('login-given', { sealedForUserIds: ['person-b'] })
  await openSession()
  renderAt('/passwords')

  // Nothing is asked: the key is sealed for the new key.
  await waitFor(() => expect(calls('AddPasswordShareKeys')).toHaveLength(1))
  expect((calls('AddPasswordShareKeys')[0][1] as { keys: Record<string, string>[] }).keys[0]).toMatchObject({
    recipientUserId: 'person-b',
    recipientPublicKey: newPublicKey,
  })
  expect(screen.queryByRole('alertdialog')).toBeNull()
  // The new key is the known one now, not compared, with a notice to dismiss.
  expect(storedKeys()).toEqual({ 'person-b': newPublicKey })
  expect(storedKnownKey('person-b')).toMatchObject({
    previousPublicKey: encodeBase64(oldKeyPair.publicKey),
    isCompared: false,
    hasChangeNotice: true,
  })
  expect(
    await screen.findByText(
      "Person B's key changed. This happens when they erase Passwords and set it up again. If you did not expect it, compare safety codes with them.",
    ),
  ).toBeTruthy()
})

it('gives a waiting person this device has no key for the key at once, with no question and no notice', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  serve({
    records: [await sharedWithPersonB('login-given', 'Given login')],
    waiting: [waitingFor('login-given', personBPublicKey)],
  })
  rememberOnDevice('login-given', { sealedForUserIds: ['person-b'] })
  await openSession()
  renderAt('/passwords')

  await waitFor(() => expect(calls('AddPasswordShareKeys')).toHaveLength(1))
  expect((calls('AddPasswordShareKeys')[0][1] as { keys: Record<string, string>[] }).keys[0]).toMatchObject({
    itemId: 'login-given',
    recipientUserId: 'person-b',
    recipientPublicKey: personBPublicKey,
  })
  expect(screen.queryByRole('alertdialog')).toBeNull()
  expect(await screen.findByText('Sent 1 key for the items you shared, now that people set up Passwords.')).toBeTruthy()
  expect(storedKeys()).toEqual({ 'person-b': personBPublicKey })
  expect(passwordsSession.getSnapshot().keyNotices).toEqual([])
})

it('seals for the key the server lists for a waiting person, and a swapped key is a changed key', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  const swappedPublicKey = encodeBase64((await generatePersonKeyPair()).publicKey)
  const server = serve({
    records: [await sharedWithPersonB('login-given', 'Given login')],
    waiting: [waitingFor('login-given', personBPublicKey)],
  })
  rememberOnDevice('login-given', { sealedForUserIds: ['person-b'] })
  await openSession()

  const first = await passwordsSession.completeWaitingShares()
  expect(first.storedKeyCount).toBe(1)
  expect(passwordsSession.getSnapshot().keyNotices).toEqual([])

  // The server lists another key later: it is sealed for and noticed.
  server.waiting = [waitingFor('login-given', swappedPublicKey)]
  const swapped = await passwordsSession.completeWaitingShares()
  expect(swapped.storedKeyCount).toBe(1)
  expect(calls('AddPasswordShareKeys')).toHaveLength(2)
  expect((calls('AddPasswordShareKeys')[1][1] as { keys: Record<string, string>[] }).keys[0].recipientPublicKey).toBe(
    swappedPublicKey,
  )
  expect(passwordsSession.getSnapshot().keyNotices).toEqual([
    expect.objectContaining({
      otherUserId: 'person-b',
      oldPublicKey: personBPublicKey,
      newPublicKey: swappedPublicKey,
    }),
  ])
})

it('gives nothing to a forged waiting share for a person the sealed record does not reach', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  knowKeyOnDevice('person-b', personBPublicKey)
  // The server lists a share with Person B, and lists Person B as waiting for
  // it, but the owner's sealed record has no such grantee.
  const forged = await recordFor(
    'login-forged-share',
    'login',
    { ...emptyLoginFields(), name: 'Forged share', access: accessOf([{ userId: 'person-c' }]) },
    { shares: sharedWith([{ userId: 'person-b', name: 'Person B', publicKey: personBPublicKey, hasKey: false }]) },
  )
  serve({ records: [forged], waiting: [waitingFor('login-forged-share', personBPublicKey)] })
  await openSession()

  const result = await passwordsSession.completeWaitingShares()
  expect(result.storedKeyCount).toBe(0)
  expect(calls('AddPasswordShareKeys')).toHaveLength(0)
})

it('reads the items in full once when the waiting list names a person or an item the cached copies do not reach', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  knowKeyOnDevice('person-b', personBPublicKey)
  // The record shares with the circle, but the copy read at unlock does not
  // list Person B as a member: they joined afterwards, which changes no item.
  const withoutMember = await recordFor(
    'login-circle',
    'login',
    { ...emptyLoginFields(), name: 'Circle login', access: accessOf([{ circleId: 'circle-one' }]) },
    {
      shares: [
        {
          id: 'share-circle',
          granteeUserId: null,
          granteeCircleId: 'circle-one',
          granteeName: 'Fixture Family',
          recipients: [],
        },
      ],
    },
  )
  const server = serve({ records: [withoutMember], waiting: [waitingFor('login-circle', personBPublicKey)] })
  rememberOnDevice('login-circle', { sealedForUserIds: ['person-b'] })
  await openSession()
  server.records = [
    {
      ...withoutMember,
      shares: [
        {
          id: 'share-circle',
          granteeUserId: null,
          granteeCircleId: 'circle-one',
          granteeName: 'Fixture Family',
          recipients: [personBRecipient(personBPublicKey)],
        },
      ],
    },
  ]
  const readsBefore = calls('ListPasswordItems').length

  const result = await passwordsSession.completeWaitingShares()
  expect(result.storedKeyCount).toBe(1)
  const reads = calls('ListPasswordItems').slice(readsBefore)
  expect(reads.map((call) => (call[1] as { sinceRevision: number | null }).sinceRevision)).toEqual([null])
  const keys = (calls('AddPasswordShareKeys')[0][1] as { keys: Record<string, string>[] }).keys
  expect(keys).toHaveLength(1)
  expect(keys[0]).toMatchObject({ itemId: 'login-circle', recipientUserId: 'person-b' })
})

it('reads in full once, not at every check, for a waiting person nobody reaches', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  knowKeyOnDevice('person-b', personBPublicKey)
  serve({
    records: [await recordFor('login-one', 'login', { ...emptyLoginFields(), name: 'Example login' })],
    waiting: [waitingFor('login-one', personBPublicKey), waitingFor('item-nobody-has', personBPublicKey)],
  })
  await openSession()
  const readsBefore = calls('ListPasswordItems').length

  await passwordsSession.completeWaitingShares()
  await passwordsSession.completeWaitingShares()
  await passwordsSession.completeWaitingShares()
  expect(calls('ListPasswordItems').length - readsBefore).toBe(1)
  expect(calls('AddPasswordShareKeys')).toHaveLength(0)
})

// --- the agent --------------------------------------------------------------------

it('offers "Let the agent use this" on logins and notes, and not on cards', async () => {
  serve({ records: await recordsToList() })
  await openSession()

  renderAt('/passwords?item=card-one')
  expect(await screen.findByRole('heading', { name: 'Travel card' })).toBeTruthy()
  expect(screen.queryByRole('checkbox', { name: /Let the agent use this/ })).toBeNull()

  cleanup()
  renderAt('/passwords?item=note-one')
  const noteSwitch = await screen.findByRole('checkbox', { name: /Let the agent use this/ })
  expect(noteSwitch).toBeTruthy()
  expect(screen.getByText('The agent can read this note, and so can the model behind it.')).toBeTruthy()

  cleanup()
  renderAt('/passwords?item=login-one')
  expect(await screen.findByRole('checkbox', { name: /Let the agent use this/ })).toBeTruthy()
  expect(screen.getByText('The agent signs in with this login on its sites and never sees the password.')).toBeTruthy()

  // Not on an item somebody else shared.
  cleanup()
  renderAt('/passwords?item=login-shared')
  await screen.findByRole('heading', { name: 'Shared login' })
  expect(screen.queryByRole('checkbox', { name: /Let the agent use this/ })).toBeNull()
})

it('seals the item key for the agent key only when the switch is turned on', async () => {
  serve({ records: await recordsToList() })
  await openSession()
  renderAt('/passwords?item=login-one')

  fireEvent.click(await screen.findByRole('checkbox', { name: /Let the agent use this/ }))
  await waitFor(() => expect(calls('SetPasswordItemUsableByAgent')).toHaveLength(1))
  const input = (calls('SetPasswordItemUsableByAgent')[0][1] as { input: Record<string, any> }).input
  expect(input).toMatchObject({ itemId: 'login-one', isUsableByAgent: true })
  const itemKey = await openForKeyPair(
    parseSealedForKeyPair(input.itemKeyByAgentKey),
    agentKeyPair,
    decodeBase64(fixture.input.publicKey),
    'login-one',
    USER_ID,
    AGENT_RECIPIENT_ID,
  )
  expect(itemKey).toHaveLength(32)
  expect(await screen.findByText('The agent can use this item now.')).toBeTruthy()
  // Widening: the fields are saved with the record that lets the agent in
  // first, then the server is asked.
  expect(orderOf({ SavePasswordItem: 'save', SetPasswordItemUsableByAgent: 'agent' })).toEqual(['save', 'agent'])
  const savedFields = await fieldsSentIn((calls('SavePasswordItem')[0][1] as { input: any }).input, 'login-one')
  await expectSignedAccess(savedFields, 'login-one', { isUsableByAgent: true, grantees: [] }, 2)

  // Turning it off re-keys the item instead (see the re-keying tests).
  fireEvent.click(screen.getByRole('checkbox', { name: /Let the agent use this/ }))
  await waitFor(() => expect(calls('RekeyPasswordItem')).toHaveLength(1))
  expect(calls('SetPasswordItemUsableByAgent')).toHaveLength(1)
  const rekey = (calls('RekeyPasswordItem')[0][1] as { input: any }).input
  expect(rekey.itemKeyByAgentKey).toBeNull()
  // The new fields carry the record with the agent off.
  await expectSignedAccess(
    await fieldsSentIn(rekey, 'login-one'),
    'login-one',
    { isUsableByAgent: false, grantees: [] },
    3,
  )
})

// --- settings -----------------------------------------------------------------------

it('asks, in words that it cannot be undone, before erasing', async () => {
  serve({ records: await recordsToList() })
  await openSession()
  renderAt('/passwords?tab=settings')

  fireEvent.click(await screen.findByRole('button', { name: 'Erase Passwords' }))
  const dialog = await screen.findByRole('alertdialog')
  expect(within(dialog).getByText(/This cannot be undone/)).toBeTruthy()
  expect(calls('ErasePasswords')).toHaveLength(0)
  fireEvent.click(within(dialog).getByRole('button', { name: 'Erase everything' }))
  await waitFor(() => expect(calls('ErasePasswords')).toHaveLength(1))
  expect(await screen.findByText('Passwords was erased.')).toBeTruthy()
  expect(passwordsSession.isUnlocked).toBe(false)
})

it('keeps the key changes for when it is unlocked, and lets erasing stay', async () => {
  serve()
  renderAt('/passwords?tab=settings')

  expect(await screen.findByText(/Unlock Passwords to change the passphrase/)).toBeTruthy()
  expect(screen.queryByRole('button', { name: 'Change passphrase' })).toBeNull()
  expect(screen.getByRole('button', { name: 'Erase Passwords' })).toBeTruthy()
})

it('imports a CSV file after asking, and skips what is already there', async () => {
  serve({ records: await recordsToList() })
  await openSession()
  renderAt('/passwords?tab=settings')

  const csv =
    'name,url,username,password,note\n' +
    'Example login,https://login.example.test/start,ada@example.test,already-here,\n' +
    'Fresh login,https://fresh.example.test,ada,new-password,\n'
  const file = new File([csv], 'passwords.csv', { type: 'text/csv' })
  fireEvent.change(await screen.findByLabelText('CSV file'), { target: { files: [file] } })

  const dialog = await screen.findByRole('alertdialog')
  expect(within(dialog).getByText('Import 1 item from Chrome?')).toBeTruthy()
  expect(within(dialog).getByText('1 item is already here and will be skipped.')).toBeTruthy()
  expect(calls('SavePasswordItem')).toHaveLength(0)
  fireEvent.click(within(dialog).getByRole('button', { name: 'Import' }))

  expect(await screen.findByText('Imported 1 item.')).toBeTruthy()
  expect(calls('SavePasswordItem')).toHaveLength(1)
  expect((calls('SavePasswordItem')[0][1] as { input: { baseRevision: unknown } }).input.baseRevision).toBeNull()
})

it("writes an export of the person's own items only", async () => {
  serve({ records: await recordsToList() })
  await openSession()
  const written = JSON.parse(exportText(passwordsSession.getSnapshot().items, new Date('2026-10-20T10:00:00Z')))
  expect(written.version).toBe(1)
  expect(written.exportedAt).toBe('2026-10-20T10:00:00.000Z')
  type Written = { id: string; itemKind: string; fields: { name: string; password?: string } }
  const names = written.items.map((item: Written) => item.fields.name).sort()
  // Not the shared item, and not the deleted one.
  expect(names).toEqual(['Agent login', 'Bank', 'Example login', 'Router note', 'Travel card'])
  // The same shape the command line writes: id, itemKind and fields.
  const bank = written.items.find((item: Written) => item.fields.name === 'Bank')
  expect(Object.keys(bank).sort()).toEqual(['fields', 'id', 'itemKind'])
  expect(bank).toMatchObject({ id: 'login-two', itemKind: 'login', fields: { password: 'invented-password-2' } })
  expect(Object.keys(written).sort()).toEqual(['exportedAt', 'items', 'version'])
})

it('changes the passphrase after asking for the current one', async () => {
  serve({ records: await recordsToList() })
  vi.mocked(graphql).mockImplementation(
    ((implementation) => async (document: string, variables?: Record<string, unknown>) => {
      if (document.includes('ChangePasswordsPassphrase')) {
        return { ChangePasswordsPassphrase: { ...setupView } } as never
      }
      return implementation(document, variables)
    })(vi.mocked(graphql).getMockImplementation()!),
  )
  await openSession()
  renderAt('/passwords?tab=settings')

  fireEvent.click(await screen.findByRole('button', { name: 'Change passphrase' }))
  const dialog = await screen.findByRole('dialog', { name: 'Change passphrase' })
  fireEvent.change(within(dialog).getByLabelText('Current passphrase'), { target: { value: PASSPHRASE } })
  fireEvent.change(within(dialog).getByLabelText('New passphrase'), { target: { value: 'a different long phrase' } })
  fireEvent.change(within(dialog).getByLabelText('Type it again'), { target: { value: 'a different long phrase' } })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Change passphrase' }))

  expect(await screen.findByText('Your passphrase was changed.', {}, { timeout: 15_000 })).toBeTruthy()
  const input = (calls('ChangePasswordsPassphrase')[0][1] as { input: Record<string, unknown> }).input
  // The proof of the old passphrase, a new salt, and the account key sealed
  // again; never a passphrase.
  expect(input.isRecoveryKey).toBe(false)
  expect(input.passphraseSalt).not.toBe(setupView.passphraseSalt)
  expect(JSON.stringify(input)).not.toContain('a different long phrase')
  expect(JSON.stringify(input)).not.toContain(PASSPHRASE)
}, 60_000)

it('says Passwords is off for an account without the permission', async () => {
  serve()
  renderAt('/passwords', [])
  expect(await screen.findByText(/Your account cannot use Passwords/)).toBeTruthy()
  expect(calls('ReadPasswordsSetup')).toHaveLength(0)
})

it("asks for revisions as 64-bit numbers and reads the server's needsNewKey mark", async () => {
  serve({ records: [await recordFor('login-one', 'login', { ...emptyLoginFields(), name: 'Example login' })] })
  await openSession()
  const document = calls('ListPasswordItems')[0][0]
  expect(document).toContain('$sinceRevision: Int64')
  expect(document).toContain('needsNewKey')
})

it('does not take a background read for use, so a page left open still locks', async () => {
  serve({ records: await recordsToList() })
  await openSession()
  passwordsSession.setLockAfterMinutes(1)
  passwordsSession.touch(1_000)
  await passwordsSession.refresh()
  await passwordsSession.completeWaitingShares()
  expect(passwordsSession.lockIfIdle(1_000 + 60_000)).toBe(true)
})

it('reads the recipients again and tries once more with the new key when a key changed while the share was open', async () => {
  const firstKey = encodeBase64(personB.publicKey)
  const newKeyPair = await generatePersonKeyPair()
  const secondKey = encodeBase64(newKeyPair.publicKey)
  const server = serve({
    records: [
      await recordFor(
        'login-one',
        'login',
        { ...emptyLoginFields(), name: 'Example login', access: accessOf([{ userId: 'person-b' }]) },
        {
          shares: [
            {
              id: 'share-one',
              granteeUserId: 'person-b',
              granteeCircleId: null,
              granteeName: 'Person B',
              recipients: [
                {
                  userId: 'person-b',
                  name: 'Person B',
                  username: 'person-b-login',
                  publicKey: firstKey,
                  hasKey: true,
                  hasTooMuchShared: false,
                },
              ],
            },
          ],
        },
      ),
    ],
    recipients: [
      {
        userId: 'person-b',
        name: 'Person B',
        username: 'person-b-login',
        publicKey: secondKey,
        hasKey: false,
        hasTooMuchShared: false,
      },
    ],
  })
  const answer = vi.mocked(graphql).getMockImplementation()!
  let hasRefused = false
  vi.mocked(graphql).mockImplementation(async (document, variables) => {
    if (document.includes('SetPasswordItemShares') && !hasRefused) {
      hasRefused = true
      throw new APIError("That person's key changed since it was read. Read their key again and seal the item for it.")
    }
    return answer(document, variables)
  })
  await openSession()
  renderAt('/passwords?item=login-one')

  fireEvent.click(await screen.findByRole('button', { name: 'Example login: Share' }))
  const dialog = await screen.findByRole('dialog', { name: 'Share Example login' })
  expect(await within(dialog).findByText(await safetyCode(personB.publicKey))).toBeTruthy()
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save sharing' }))

  // The save is tried again at once with the new key; the person presses
  // nothing and does not read the server's sentence.
  await waitFor(() => expect(calls('SetPasswordItemShares')).toHaveLength(2))
  const sent = (calls('SetPasswordItemShares')[1][1] as { input: { keys: { recipientPublicKey: string }[] } }).input
    .keys
  expect(sent.map((key) => key.recipientPublicKey)).toEqual([secondKey])
  await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Share Example login' })).toBeNull())
  expect(screen.queryByText(/key changed since it was read/)).toBeNull()
  // The key before was the first key this device saw, so the new one is a
  // change, and the notice stays until it is dismissed.
  expect(
    await screen.findByText(
      "Person B's key changed. This happens when they erase Passwords and set it up again. If you did not expect it, compare safety codes with them.",
    ),
  ).toBeTruthy()
  expect(storedKeys()).toEqual({ 'person-b': secondKey })
  expect(server.records).toHaveLength(1)
})

it('makes a new recovery key only after the passphrase is right, and sends it once it is saved', async () => {
  serve({ records: await recordsToList() })
  vi.mocked(graphql).mockImplementation(
    ((implementation) => async (document: string, variables?: Record<string, unknown>) => {
      if (document.includes('ReplacePasswordsRecoveryKey')) {
        return { ReplacePasswordsRecoveryKey: { ...setupView } } as never
      }
      return implementation(document, variables)
    })(vi.mocked(graphql).getMockImplementation()!),
  )
  await openSession()
  renderAt('/passwords?tab=settings')

  fireEvent.click(await screen.findByRole('button', { name: 'Replace recovery key' }))
  const askDialog = await screen.findByRole('dialog', { name: 'Replace recovery key' })
  fireEvent.change(within(askDialog).getByLabelText('Passphrase'), { target: { value: 'not the passphrase' } })
  fireEvent.click(within(askDialog).getByRole('button', { name: 'Continue' }))
  expect(await screen.findByText('That passphrase is not right.', {}, { timeout: 15_000 })).toBeTruthy()
  expect(screen.queryByRole('dialog', { name: 'Save your recovery key' })).toBeNull()

  fireEvent.change(within(askDialog).getByLabelText('Passphrase'), { target: { value: PASSPHRASE } })
  fireEvent.click(within(askDialog).getByRole('button', { name: 'Continue' }))
  const keyDialog = await screen.findByRole('dialog', { name: 'Save your recovery key' }, { timeout: 15_000 })
  const confirm = within(keyDialog).getByRole('button', { name: 'Replace recovery key' })
  expect(confirm.hasAttribute('disabled')).toBe(true)
  expect(calls('ReplacePasswordsRecoveryKey')).toHaveLength(0)
  fireEvent.click(within(keyDialog).getByRole('checkbox', { name: 'I saved my recovery key' }))
  fireEvent.click(confirm)

  expect(await screen.findByText('Your recovery key was replaced.')).toBeTruthy()
  const input = (calls('ReplacePasswordsRecoveryKey')[0][1] as { input: Record<string, string> }).input
  expect(Object.keys(input).sort()).toEqual(['accountKeyByRecoveryKey', 'proof', 'recoveryKeyProofHash'])
}, 60_000)

// --- locking ----------------------------------------------------------------

it('is locked by lockPasswords, which the shell calls when the person signs out or changes', async () => {
  serve({ records: await recordsToList() })
  await openSession()
  expect(passwordsSession.isUnlocked).toBe(true)
  lockPasswords()
  expect(passwordsSession.isUnlocked).toBe(false)
  expect(passwordsSession.getSnapshot().items).toEqual([])
})

it('looks at the lock time again when the page is shown after being hidden', async () => {
  serve({ records: await recordsToList() })
  await openSession()
  passwordsSession.setLockAfterMinutes(1)
  passwordsSession.touch(Date.now() - 120_000)
  document.dispatchEvent(new Event('visibilitychange'))
  expect(passwordsSession.isUnlocked).toBe(false)
})

it('locks again and says why when the first read of the items fails, instead of staying on Loading', async () => {
  serve({ records: await recordsToList() })
  const answer = vi.mocked(graphql).getMockImplementation()!
  vi.mocked(graphql).mockImplementation(async (document, variables) => {
    if (document.includes('ListPasswordItems')) throw new APIError('The server could not list the items.')
    return answer(document, variables)
  })
  await expect(openSession()).rejects.toThrow('The server could not list the items.')
  expect(passwordsSession.isUnlocked).toBe(false)

  renderAt('/passwords')
  await screen.findByRole('heading', { name: 'Passwords is locked' })
  fireEvent.change(screen.getByLabelText('Passphrase'), { target: { value: PASSPHRASE } })
  fireEvent.click(screen.getByRole('button', { name: 'Unlock' }))
  expect(await screen.findByText('The server could not list the items.', {}, { timeout: 15_000 })).toBeTruthy()
  expect(screen.getByRole('heading', { name: 'Passwords is locked' })).toBeTruthy()
  expect(screen.queryByText('Loading')).toBeNull()
}, 60_000)

// --- reading what other devices changed ---------------------------------------

it('still reads an item another device saved at a lower revision after this device saved one', async () => {
  const server = serve({ records: await recordsToList() })
  await openSession()
  // Revisions are one sequence for the server: this save gets 10.
  await passwordsSession.saveItem({ itemKind: 'note', fields: { ...emptyNoteFields(), name: 'Saved here' } }, null)
  // The other device saved before this one, at 5.
  server.records.push(
    await recordFor('login-other', 'login', { ...emptyLoginFields(), name: 'From the other device' }, { revision: 5 }),
  )
  await passwordsSession.refresh()
  expect(passwordsSession.getSnapshot().items.map((item) => item.fields.name)).toContain('From the other device')
  const reads = calls('ListPasswordItems')
  const lastRead = reads[reads.length - 1][1] as { sinceRevision: number | null }
  expect(lastRead.sinceRevision).toBe(3)
})

it('reads everything once more when the server lists an item an incremental read did not bring', async () => {
  const server = serve({ records: await recordsToList() })
  await openSession()
  // Added at a revision below the one already read, so reading from there misses it.
  server.records.push(
    await recordFor('login-late', 'login', { ...emptyLoginFields(), name: 'Late item' }, { revision: 1 }),
  )
  await passwordsSession.refresh()
  expect(passwordsSession.getSnapshot().items.map((item) => item.fields.name)).toContain('Late item')
  expect(calls('ListPasswordItems').map((call) => (call[1] as { sinceRevision: number | null }).sinceRevision)).toEqual(
    [null, 3, null],
  )
})

it('does not read everything again for an item of a kind this version cannot show', async () => {
  serve({
    records: [
      await recordFor('login-one', 'login', { ...emptyLoginFields(), name: 'Example login' }),
      await recordFor('thing-one', 'login', { ...emptyLoginFields(), name: 'Other' }, { itemKind: 'hologram' }),
    ],
  })
  await openSession()
  await passwordsSession.refresh()
  await passwordsSession.refresh()
  expect(
    calls('ListPasswordItems').filter((call) => (call[1] as { sinceRevision: number | null }).sinceRevision === null),
  ).toHaveLength(1)
})

// --- keys that were seen before ------------------------------------------------

async function openShareWithCircle(item = 'Example login') {
  fireEvent.click(await screen.findByRole('button', { name: `${item}: Share` }))
  const dialog = await screen.findByRole('dialog', { name: `Share ${item}` })
  fireEvent.click(await within(dialog).findByRole('combobox', { name: 'Share with' }))
  fireEvent.click(await screen.findByRole('option', { name: 'Fixture Family' }))
  fireEvent.click(within(dialog).getByRole('button', { name: 'Add' }))
  await within(dialog).findByText('Everyone in Fixture Family')
  return dialog
}

// onlyExampleLogin is a list with one item the person owns and nothing shared
// by Person B: an item Person B shared would already make this device keep
// Person B's key, and these tests are about a key it has not kept.
async function onlyExampleLogin(): Promise<PasswordItemRecord[]> {
  return [await recordFor('login-one', 'login', { ...emptyLoginFields(), name: 'Example login' })]
}

function personBRecipient(publicKey: string | null, hasKey = false): PasswordRecipient {
  return {
    userId: 'person-b',
    name: 'Person B',
    username: 'person-b-login',
    publicKey,
    hasKey,
    hasTooMuchShared: false,
  }
}

it('seals at once for a person whose key this device has never seen, and keeps it as their known key', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  serve({ records: await onlyExampleLogin(), recipients: [personBRecipient(personBPublicKey)] })
  await openSession()
  renderAt('/passwords?item=login-one')

  const dialog = await openShareWithCircle()
  // Nothing to confirm: only an optional way to compare codes.
  expect(within(dialog).queryByText('This device has not confirmed their key yet.')).toBeNull()
  expect(within(dialog).queryByRole('button', { name: 'The safety code matches' })).toBeNull()
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save sharing' }))
  await waitFor(() => expect(calls('SetPasswordItemShares')).toHaveLength(1))
  const keys = (calls('SetPasswordItemShares')[0][1] as { input: { keys: Record<string, string>[] } }).input.keys
  expect(keys).toHaveLength(1)
  expect(keys[0]).toMatchObject({ recipientUserId: 'person-b', recipientPublicKey: personBPublicKey })
  await screen.findByText('Sharing saved.')
  expect(storedKeys()).toEqual({ 'person-b': personBPublicKey })
  expect(storedKnownKey('person-b')).toMatchObject({ isCompared: false, hasChangeNotice: false })
  expect(passwordsSession.getSnapshot().keyNotices).toEqual([])
})

it('seals in shareItem for the key the server lists, and a different key later is a changed key', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  const otherPublicKey = encodeBase64((await generatePersonKeyPair()).publicKey)
  serve({ records: await onlyExampleLogin() })
  await openSession()
  const item = passwordsSession.getSnapshot().items.find((candidate) => candidate.id === 'login-one')!
  const targetsFor = (publicKey: string) => [
    {
      grantee: { granteeUserId: 'person-b', granteeCircleId: null },
      recipients: [personBRecipient(publicKey)],
    },
  ]

  const shared = await passwordsSession.shareItem(item, targetsFor(personBPublicKey))
  const firstKeys = (calls('SetPasswordItemShares')[0][1] as { input: { keys: Record<string, string>[] } }).input.keys
  expect(firstKeys).toHaveLength(1)
  expect(firstKeys[0].recipientPublicKey).toBe(personBPublicKey)
  expect(passwordsSession.getSnapshot().keyNotices).toEqual([])

  // The server now lists another key for the person who has no key for the
  // item: it is sealed for, recorded, and noticed.
  await passwordsSession.shareItem(shared.item, targetsFor(otherPublicKey))
  const secondKeys = (calls('SetPasswordItemShares')[1][1] as { input: { keys: Record<string, string>[] } }).input.keys
  expect(secondKeys).toHaveLength(1)
  expect(secondKeys[0].recipientPublicKey).toBe(otherPublicKey)
  expect(passwordsSession.getSnapshot().keyNotices).toEqual([
    expect.objectContaining({ otherUserId: 'person-b', oldPublicKey: personBPublicKey, newPublicKey: otherPublicKey }),
  ])
})

it('seals at once for a key that is not the known one, and keeps it as the known key with a notice', async () => {
  const oldKeyPair = await generatePersonKeyPair()
  const newPublicKey = encodeBase64(personB.publicKey)
  knowKeyOnDevice('person-b', encodeBase64(oldKeyPair.publicKey), { name: 'Person B' })
  serve({ records: await onlyExampleLogin(), recipients: [personBRecipient(newPublicKey)] })
  await openSession()
  renderAt('/passwords?item=login-one')

  const dialog = await openShareWithCircle()
  expect(within(dialog).getByText('Their key changed.')).toBeTruthy()
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save sharing' }))
  await waitFor(() => expect(calls('SetPasswordItemShares')).toHaveLength(1))
  const keys = (calls('SetPasswordItemShares')[0][1] as { input: { keys: Record<string, string>[] } }).input.keys
  expect(keys).toHaveLength(1)
  expect(keys[0]).toMatchObject({ recipientUserId: 'person-b', recipientPublicKey: newPublicKey })
  await screen.findByText('Sharing saved.')
  expect(storedKeys()).toEqual({ 'person-b': newPublicKey })
  expect(
    await screen.findByText(
      "Person B's key changed. This happens when they erase Passwords and set it up again. If you did not expect it, compare safety codes with them.",
    ),
  ).toBeTruthy()
})

it('compares codes from the share dialog, shows both codes and marks Codes compared', async () => {
  const oldKeyPair = await generatePersonKeyPair()
  const personBPublicKey = encodeBase64(personB.publicKey)
  knowKeyOnDevice('person-b', encodeBase64(oldKeyPair.publicKey), { name: 'Person B' })
  serve({ records: await onlyExampleLogin(), recipients: [personBRecipient(personBPublicKey)] })
  await openSession()
  renderAt('/passwords?item=login-one')

  const dialog = await openShareWithCircle()
  expect(within(dialog).queryByText('Codes compared')).toBeNull()
  fireEvent.click(within(dialog).getByRole('button', { name: 'Compare codes' }))
  const compare = await screen.findByRole('alertdialog')
  expect(await within(compare).findByText(await safetyCode(oldKeyPair.publicKey))).toBeTruthy()
  expect(await within(compare).findByText(await safetyCode(personB.publicKey))).toBeTruthy()
  expect(await within(compare).findByText(await safetyCode(fixture.keyPair.publicKey))).toBeTruthy()

  fireEvent.click(within(compare).getByRole('button', { name: 'The codes match' }))
  expect(await within(dialog).findByText('Codes compared')).toBeTruthy()
  expect(storedKnownKey('person-b')).toMatchObject({ publicKey: personBPublicKey, isCompared: true })
})

it('closing Compare codes changes nothing', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  serve({ records: await onlyExampleLogin(), recipients: [personBRecipient(personBPublicKey)] })
  await openSession()
  renderAt('/passwords?item=login-one')

  const dialog = await openShareWithCircle()
  fireEvent.click(within(dialog).getByRole('button', { name: 'Compare codes' }))
  const compare = await screen.findByRole('alertdialog')
  fireEvent.click(within(compare).getByRole('button', { name: 'Cancel' }))
  await waitFor(() => expect(screen.queryByRole('alertdialog')).toBeNull())
  expect(within(dialog).queryByText('Codes compared')).toBeNull()
  expect(window.localStorage.getItem(`teanode.passwords.knownKeys:${USER_ID}`)).toBeNull()
})

it('says whom an item is shared with and names a person once in the share dialog', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  const circleMember = (userId: string, publicKey: string | null): PasswordRecipient => ({
    userId,
    name: `Member ${userId}`,
    username: userId,
    publicKey,
    hasKey: publicKey !== null,
    hasTooMuchShared: false,
  })
  serve({
    records: [
      await recordFor(
        'login-one',
        'login',
        {
          ...emptyLoginFields(),
          name: 'Example login',
          access: accessOf([{ userId: 'person-b' }, { circleId: 'circle-one' }]),
        },
        {
          shares: [
            {
              id: 'share-person',
              granteeUserId: 'person-b',
              granteeCircleId: null,
              granteeName: 'Person B (person-b-login)',
              recipients: [personBRecipient(personBPublicKey, true)],
            },
            {
              id: 'share-circle',
              granteeUserId: null,
              granteeCircleId: 'circle-one',
              granteeName: 'Fixture Family',
              recipients: [
                circleMember('member-one', personBPublicKey),
                circleMember('member-two', personBPublicKey),
                circleMember('member-three', null),
              ],
            },
          ],
        },
      ),
    ],
  })
  await openSession()
  renderAt('/passwords?item=login-one')

  expect(
    await screen.findByText('Shared with Person B (person-b-login) · Fixture Family: 3 people, 1 waiting'),
  ).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'Share' }))
  const dialog = await screen.findByRole('dialog', { name: 'Share Example login' })
  // The person is the title of their card and is not repeated in a row; the
  // circle names each of its people.
  expect(within(dialog).getAllByText('Person B (person-b-login)')).toHaveLength(1)
  expect(within(dialog).getByText('Everyone in Fixture Family')).toBeTruthy()
  expect(within(dialog).getByText('Member member-one (member-one)')).toBeTruthy()
  // The code is made from the key alone, so three people who hold the same key
  // show the same code, and no user id the server supplies goes into it.
  expect(within(dialog).getAllByText(await safetyCode(personB.publicKey))).toHaveLength(3)
})

it('says Codes compared beside a recipient whose key was compared, with no button to compare again', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  knowKeyOnDevice('person-b', personBPublicKey, { isCompared: true })
  serve({ records: await onlyExampleLogin(), recipients: [personBRecipient(personBPublicKey)] })
  await openSession()
  renderAt('/passwords?item=login-one')

  const dialog = await openShareWithCircle()
  expect(within(dialog).getByText('Codes compared')).toBeTruthy()
  expect(within(dialog).queryByRole('button', { name: 'Compare codes' })).toBeNull()
})

it('does not seal again for a person who already has a key for the item', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  serve({
    records: [
      await recordFor(
        'login-one',
        'login',
        { ...emptyLoginFields(), name: 'Example login', access: accessOf([{ userId: 'person-b' }]) },
        {
          shares: [
            {
              id: 'share-one',
              granteeUserId: 'person-b',
              granteeCircleId: null,
              granteeName: 'Person B',
              recipients: [personBRecipient(personBPublicKey, true)],
            },
          ],
        },
      ),
    ],
  })
  await openSession()
  renderAt('/passwords?item=login-one')

  fireEvent.click(await screen.findByRole('button', { name: 'Example login: Share' }))
  const dialog = await screen.findByRole('dialog', { name: 'Share Example login' })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save sharing' }))
  await waitFor(() => expect(calls('SetPasswordItemShares')).toHaveLength(1))
  expect((calls('SetPasswordItemShares')[0][1] as { input: { keys: unknown[] } }).input.keys).toEqual([])
})

it('names a share the server lists that the sealed record does not have, and removes it when sharing is saved', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  knowKeyOnDevice('person-b', personBPublicKey)
  serve({
    records: [
      await recordFor(
        'login-one',
        'login',
        { ...emptyLoginFields(), name: 'Example login' },
        { shares: sharedWith([{ userId: 'person-b', name: 'Person B', publicKey: personBPublicKey, hasKey: true }]) },
      ),
    ],
  })
  await openSession()
  renderAt('/passwords?item=login-one')

  // The view does not say it is shared with Person B; it says the server lists them.
  expect(
    await screen.findByText(/The server also lists Person B, but you did not share this item with them/),
  ).toBeTruthy()
  expect(screen.queryByText(/^Shared with/)).toBeNull()

  fireEvent.click(screen.getByRole('button', { name: 'Example login: Share' }))
  const dialog = await screen.findByRole('dialog', { name: 'Share Example login' })
  expect(within(dialog).getByText('Not shared with anyone.')).toBeTruthy()
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save sharing' }))
  // The share is taken off, and the item gets a new key without Person B.
  await waitFor(() => expect(calls('RekeyPasswordItem')).toHaveLength(1))
  expect(orderOf({ SetPasswordItemShares: 'shares', RekeyPasswordItem: 'rekey' })).toEqual(['shares', 'rekey'])
  const rekey = (calls('RekeyPasswordItem')[0][1] as { input: any }).input
  expect(rekey.shareKeys).toEqual([])
})

it('shows a key that is not valid base64 as unreadable instead of throwing', async () => {
  render(
    <TranslationProvider>
      <SafetyCode publicKey="not base64 !!!" />
    </TranslationProvider>,
  )
  expect(await screen.findByText('could not be read')).toBeTruthy()
})

// --- the passphrase -----------------------------------------------------------

it('changes the passphrase with the recovery key when the current passphrase is forgotten', async () => {
  serve({ records: await recordsToList() })
  vi.mocked(graphql).mockImplementation(
    ((implementation) => async (document: string, variables?: Record<string, unknown>) => {
      if (document.includes('ChangePasswordsPassphrase')) {
        return { ChangePasswordsPassphrase: { ...setupView } } as never
      }
      return implementation(document, variables)
    })(vi.mocked(graphql).getMockImplementation()!),
  )
  await openSession()
  renderAt('/passwords?tab=settings')

  fireEvent.click(await screen.findByRole('button', { name: 'Change passphrase' }))
  const dialog = await screen.findByRole('dialog', { name: 'Change passphrase' })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Use the recovery key' }))
  fireEvent.change(within(dialog).getByLabelText('Recovery key'), { target: { value: fixture.recoveryKeyText } })
  fireEvent.change(within(dialog).getByLabelText('New passphrase'), { target: { value: 'a different long phrase' } })
  fireEvent.change(within(dialog).getByLabelText('Type it again'), { target: { value: 'a different long phrase' } })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Change passphrase' }))

  expect(await screen.findByText('Your passphrase was changed.', {}, { timeout: 15_000 })).toBeTruthy()
  const input = (calls('ChangePasswordsPassphrase')[0][1] as { input: Record<string, unknown> }).input
  expect(input.isRecoveryKey).toBe(true)
  expect(input.proof).toBe(await proofOf(parseRecoveryKey(fixture.recoveryKeyText)))
  expect(JSON.stringify(input)).not.toContain(fixture.recoveryKeyText)
}, 60_000)

it('refuses an iteration count from the server outside 600000 to 10000000', async () => {
  for (const passphraseIterationCount of [1000, 599_999, 10_000_001, 600_000.5]) {
    expect(() => passphraseSecret(PASSPHRASE, { ...setupView, passphraseIterationCount })).toThrow(PasswordsError)
  }
  expect(() => passphraseSecret(PASSPHRASE, { ...setupView, passphraseSalt: null })).toThrow(PasswordsError)
  serve({ setup: { ...setupView, passphraseIterationCount: 1000 } })
  renderAt('/passwords')
  await screen.findByRole('heading', { name: 'Passwords is locked' })
  fireEvent.change(screen.getByLabelText('Passphrase'), { target: { value: PASSPHRASE } })
  fireEvent.click(screen.getByRole('button', { name: 'Unlock' }))
  expect(await screen.findByText(/passphrase setting that is not safe/)).toBeTruthy()
  expect(calls('UnlockPasswords')).toHaveLength(0)
})

it("says what went wrong from a code in the reader's own words, never from thrown English", () => {
  const t = ((key: string) => `words:${key}`) as never
  expect(failureText(new PasswordsError('locked'), t, 'fallback')).toBe('words:passwords.errorLocked')
  expect(failureText(new PasswordsError('publicKeyMismatch'), t, 'fallback')).toBe(
    'words:passwords.errorPublicKeyMismatch',
  )
  expect(failureText(new APIError('The server says no.'), t, 'fallback')).toBe('The server says no.')
  expect(failureText(new Error('English from a library'), t, 'fallback')).toBe('fallback')
  expect(failureText('text', t, 'fallback')).toBe('fallback')
})

// --- the page ------------------------------------------------------------------

it('says Passwords needs HTTPS where the browser has no secure context', async () => {
  serve()
  Object.defineProperty(window, 'isSecureContext', { value: false, configurable: true })
  try {
    renderAt('/passwords')
    expect(await screen.findByText(/Passwords needs a secure connection/)).toBeTruthy()
    expect(calls('ReadPasswordsSetup')).toHaveLength(0)
  } finally {
    Object.defineProperty(window, 'isSecureContext', { value: undefined, configurable: true })
  }
})

it('opens an item from anywhere on its row', async () => {
  serve({ records: await recordsToList() })
  await openSession()
  renderAt('/passwords')

  const row = (await screen.findByRole('link', { name: 'Bank' })).closest('tr')!
  expect(row.className).toContain('linked')
  fireEvent.click(within(row).getByText('grace'))
  await waitFor(() => expect(screen.getByTestId('address').textContent).toBe('?item=login-two'))
  expect(await screen.findByRole('heading', { name: 'Bank' })).toBeTruthy()
})

it('gives the locked Settings card a title and an Unlock button that goes to the unlock screen', async () => {
  serve()
  renderAt('/passwords?tab=settings')
  expect(await screen.findByRole('heading', { name: 'Passwords is locked' })).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'Unlock' }))
  await waitFor(() => expect(screen.getByTestId('address').textContent).toBe(''))
  expect(await screen.findByLabelText('Passphrase')).toBeTruthy()
})

it('lists at most 50 sites, counts the rest, and does not slow down for thousands written twice', async () => {
  const many = Array.from({ length: 60 }, (_, index) => `site-${index}.example.test`)
  expect(sitesToShow(many).shown).toHaveLength(50)
  expect(sitesToShow(many).hiddenCount).toBe(10)
  // A site written twice counts once, bare or with its scheme.
  expect(sitesToShow(['example.test', 'https://example.test', ' ', 'other.example.test'])).toEqual({
    shown: ['https://example.test', 'https://other.example.test'],
    hiddenCount: 0,
  })
  const started = performance.now()
  const repeated = Array.from({ length: 50_000 }, (_, index) => `site-${index % 70}.example.test`)
  expect(sitesToShow(repeated)).toMatchObject({ hiddenCount: 20 })
  expect(performance.now() - started).toBeLessThan(2000)

  serve({
    records: [await recordFor('login-sites', 'login', { ...emptyLoginFields(), name: 'Many sites', sites: many })],
  })
  await openSession()
  renderAt('/passwords?item=login-sites')
  expect(await screen.findByText('and 10 more')).toBeTruthy()
  expect(screen.getAllByRole('link', { name: /^site-\d+\.example\.test$/ })).toHaveLength(50)
})

it('lists a site written bare as a link, once, and a site that is not a web address as text', async () => {
  serve({
    records: [
      await recordFor('login-sites', 'login', {
        ...emptyLoginFields(),
        name: 'Many sites',
        sites: ['example.test', 'https://example.test', 'javascript:alert(1)'],
      }),
    ],
  })
  await openSession()
  renderAt('/passwords?item=login-sites')

  const links = await screen.findAllByRole('link', { name: 'example.test' })
  expect(links).toHaveLength(1)
  expect(links[0].getAttribute('href')).toBe('https://example.test')
  expect(screen.getByText('javascript:alert(1)').closest('a')).toBeNull()
})

it('reads the passphrase setting again before each unlock, so a passphrase changed on another device still opens', async () => {
  const server = serve({
    records: await recordsToList(),
    setup: { ...setupView, passphraseSalt: encodeBase64(new Uint8Array(16).fill(9)) },
  })
  renderAt('/passwords')
  await screen.findByRole('heading', { name: 'Passwords is locked' })
  // The passphrase was changed elsewhere after this page read the setting.
  server.setup = setupView
  fireEvent.change(screen.getByLabelText('Passphrase'), { target: { value: PASSPHRASE } })
  fireEvent.click(screen.getByRole('button', { name: 'Unlock' }))
  expect(await screen.findByRole('link', { name: 'Example login' }, { timeout: 15_000 })).toBeTruthy()
  // One attempt on the server, with the new setting.
  expect(calls('UnlockPasswords')).toHaveLength(1)
}, 60_000)

it('does not put an older copy of an item over a newer one that is held', async () => {
  const newer = await recordFor('login-one', 'login', { ...emptyLoginFields(), name: 'Newer name' }, { revision: 7 })
  const older = await recordFor('login-one', 'login', { ...emptyLoginFields(), name: 'Older name' }, { revision: 3 })
  const server = serve({ records: [newer] })
  await openSession()
  const answer = vi.mocked(graphql).getMockImplementation()!
  vi.mocked(graphql).mockImplementation(async (document, variables) => {
    // A read that started earlier ends last and lists the old copy.
    if (document.includes('ListPasswordItems')) {
      return { ListPasswordItems: { items: [older], itemIds: ['login-one'], revision: 7 } } as never
    }
    if (document.includes('SavePasswordItem')) return { SavePasswordItem: older } as never
    return answer(document, variables)
  })

  await passwordsSession.refresh()
  const names = () => passwordsSession.getSnapshot().items.map((item) => item.fields.name)
  expect(names()).toEqual(['Newer name'])

  // The same goes for an answer to a save that arrives after a newer copy.
  const held = passwordsSession.getSnapshot().items[0]
  const saved = await passwordsSession.saveItem({ itemKind: 'login', fields: emptyLoginFields() }, held)
  expect(saved.fields.name).toBe('Newer name')
  expect(names()).toEqual(['Newer name'])

  // A newer copy replaces the held one.
  server.records = [
    await recordFor('login-one', 'login', { ...emptyLoginFields(), name: 'Newest name' }, { revision: 9 }),
  ]
  vi.mocked(graphql).mockImplementation(answer)
  await passwordsSession.refresh()
  expect(names()).toEqual(['Newest name'])
})

// --- the clipboard and files -----------------------------------------------------

it('clears a copied secret from the clipboard after 30 seconds if it is still there and the window has focus', async () => {
  vi.useFakeTimers()
  try {
    let clipboardText = ''
    const writeText = vi.fn(async (text: string) => {
      clipboardText = text
    })
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText, readText: vi.fn(async () => clipboardText) },
      configurable: true,
    })
    const hasFocus = vi.spyOn(document, 'hasFocus').mockReturnValue(true)
    render(
      <TranslationProvider>
        <ToastProvider>
          <SecretValue label="Password" value="invented-secret" />
        </ToastProvider>
      </TranslationProvider>,
    )
    fireEvent.click(screen.getByRole('button', { name: 'Copy Password' }))
    await vi.advanceTimersByTimeAsync(0)
    expect(clipboardText).toBe('invented-secret')
    await vi.advanceTimersByTimeAsync(29_000)
    expect(clipboardText).toBe('invented-secret')
    await vi.advanceTimersByTimeAsync(1_500)
    expect(clipboardText).toBe('')

    // Something else on the clipboard is left alone.
    clearClipboardLater('invented-secret')
    clipboardText = 'something the person copied after'
    await vi.advanceTimersByTimeAsync(30_000)
    expect(clipboardText).toBe('something the person copied after')

    // So is a clipboard when the window has no focus.
    hasFocus.mockReturnValue(false)
    clipboardText = 'invented-secret'
    clearClipboardLater('invented-secret')
    await vi.advanceTimersByTimeAsync(30_000)
    expect(clipboardText).toBe('invented-secret')
    hasFocus.mockRestore()
  } finally {
    Object.defineProperty(navigator, 'clipboard', { value: undefined, configurable: true })
    vi.useRealTimers()
  }
})

it('clears the recovery key from the clipboard after 30 seconds, like any secret', async () => {
  vi.useFakeTimers()
  try {
    let clipboardText = ''
    Object.defineProperty(navigator, 'clipboard', {
      value: {
        writeText: vi.fn(async (text: string) => {
          clipboardText = text
        }),
        readText: vi.fn(async () => clipboardText),
      },
      configurable: true,
    })
    vi.spyOn(document, 'hasFocus').mockReturnValue(true)
    render(
      <TranslationProvider>
        <ToastProvider>
          <RecoveryKeyPanel recoveryKeyText="INVENTED-RECOVERY-KEY" isSaved={false} onSavedChange={() => undefined} />
        </ToastProvider>
      </TranslationProvider>,
    )
    fireEvent.click(screen.getByRole('button', { name: 'Copy recovery key' }))
    await vi.advanceTimersByTimeAsync(0)
    expect(clipboardText).toBe('INVENTED-RECOVERY-KEY')
    await vi.advanceTimersByTimeAsync(30_500)
    expect(clipboardText).toBe('')
  } finally {
    vi.mocked(document.hasFocus).mockRestore?.()
    Object.defineProperty(navigator, 'clipboard', { value: undefined, configurable: true })
    vi.useRealTimers()
  }
})

it('asks for the passphrase again before exporting, and releases the file a second after the download starts', async () => {
  serve({ records: await recordsToList() })
  await openSession()
  const createObjectURL = vi.fn(() => 'blob:export')
  const revokeObjectURL = vi.fn()
  Object.assign(URL, { createObjectURL, revokeObjectURL })
  const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined)
  try {
    renderAt('/passwords?tab=settings')
    fireEvent.click(await screen.findByRole('button', { name: 'Export items' }))
    const dialog = await screen.findByRole('dialog', { name: 'Export your items?' })
    fireEvent.change(within(dialog).getByLabelText('Passphrase'), { target: { value: 'not the passphrase' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Download' }))
    expect(await screen.findByText('That passphrase is not right.', {}, { timeout: 15_000 })).toBeTruthy()
    expect(createObjectURL).not.toHaveBeenCalled()

    fireEvent.change(within(dialog).getByLabelText('Passphrase'), { target: { value: PASSPHRASE } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Download' }))
    await waitFor(() => expect(click).toHaveBeenCalledTimes(1), { timeout: 15_000 })
    expect(createObjectURL).toHaveBeenCalledTimes(1)
    // Not released as the click returns.
    expect(revokeObjectURL).not.toHaveBeenCalled()
    await waitFor(() => expect(revokeObjectURL).toHaveBeenCalledWith('blob:export'), { timeout: 3000 })
  } finally {
    click.mockRestore()
  }
}, 60_000)

it('tells the screens once when it imports many items, and counts them as it goes', async () => {
  serve()
  await openSession()
  let notified = 0
  const unsubscribe = passwordsSession.subscribe(() => (notified += 1))
  const progress: number[] = []
  const saved = await passwordsSession.importItems(
    ['One', 'Two', 'Three'].map((name) => ({ itemKind: 'note' as const, fields: { ...emptyNoteFields(), name } })),
    (count) => progress.push(count),
  )
  unsubscribe()
  expect(saved).toBe(3)
  expect(progress).toEqual([1, 2, 3])
  expect(notified).toBe(1)
  expect(passwordsSession.getSnapshot().items.map((item) => item.fields.name)).toEqual(['One', 'Three', 'Two'])
})

// --- the one list of known keys ------------------------------------------------------

it('moves the confirmed and seen keys an earlier version kept into the one list, once', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  const seenOnlyPublicKey = encodeBase64((await generatePersonKeyPair()).publicKey)
  const olderSeenPublicKey = encodeBase64((await generatePersonKeyPair()).publicKey)
  window.localStorage.setItem(
    `teanode.passwords.pins:${USER_ID}`,
    JSON.stringify({ 'person-b': olderSeenPublicKey, 'person-seen': seenOnlyPublicKey }),
  )
  window.localStorage.setItem(
    `teanode.passwords.confirmedKeys:${USER_ID}`,
    JSON.stringify({ 'person-b': personBPublicKey }),
  )
  serve({ records: [await sharedWithPersonB('login-given', 'Given login')], waiting: [] })
  await openSession()

  // A confirmed key wins over a seen one and counts as compared.
  expect(storedKeys()).toEqual({ 'person-b': personBPublicKey, 'person-seen': seenOnlyPublicKey })
  expect(storedKnownKey('person-b')).toMatchObject({ isCompared: true, hasChangeNotice: false })
  expect(storedKnownKey('person-seen')).toMatchObject({ isCompared: false, hasChangeNotice: false })
  expect(window.localStorage.getItem(`teanode.passwords.pins:${USER_ID}`)).toBeNull()
  expect(window.localStorage.getItem(`teanode.passwords.confirmedKeys:${USER_ID}`)).toBeNull()
  expect(passwordsSession.getSnapshot().keyNotices).toEqual([])
})

it('seals for a key a seen-only earlier record kept, without asking', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  // What the version before kept: a seen key, which used to ask once.
  window.localStorage.setItem(`teanode.passwords.pins:${USER_ID}`, JSON.stringify({ 'person-b': personBPublicKey }))
  serve({
    records: [await sharedWithPersonB('login-given', 'Given login')],
    waiting: [waitingFor('login-given', personBPublicKey)],
  })
  rememberOnDevice('login-given', { sealedForUserIds: ['person-b'] })
  await openSession()

  const result = await passwordsSession.completeWaitingShares()
  expect(result.storedKeyCount).toBe(1)
  expect(calls('AddPasswordShareKeys')).toHaveLength(1)
  expect(storedKeys()).toEqual({ 'person-b': personBPublicKey })
})

it('keeps the key an incoming item came with as the known key, not compared', async () => {
  serve({ records: await recordsToList() })
  await openSession()
  expect(storedKeys()).toEqual({ 'person-b': encodeBase64(personB.publicKey) })
  expect(storedKnownKey('person-b')).toMatchObject({ isCompared: false })
})

it('still gives a waiting share nothing when the key came with a planted item but the record does not reach the person', async () => {
  // A hostile server plants one item "shared by" Person B and lists Person B
  // as waiting for an item the owner's sealed record never named them for.
  const personBPublicKey = encodeBase64(personB.publicKey)
  const notShared = await recordFor(
    'login-not-shared',
    'login',
    { ...emptyLoginFields(), name: 'Not shared', access: accessOf([{ userId: 'person-c' }]) },
    { shares: sharedWith([{ userId: 'person-b', name: 'Person B', publicKey: personBPublicKey, hasKey: false }]) },
  )
  serve({
    records: [notShared, await sharedRecordFor('login-planted', { ...emptyLoginFields(), name: 'Planted' })],
    waiting: [waitingFor('login-not-shared', personBPublicKey)],
  })
  await openSession()
  expect(storedKeys()).toEqual({ 'person-b': personBPublicKey })

  const result = await passwordsSession.completeWaitingShares()
  expect(result.storedKeyCount).toBe(0)
  expect(calls('AddPasswordShareKeys')).toHaveLength(0)
})

// --- an item whose fields are not an item -------------------------------------------

const VALID_PASSKEY = {
  credentialId: 'AAAA',
  relyingPartyId: 'example.test',
  userHandle: 'AAAA',
  userName: 'ada',
  userDisplayName: 'Ada',
  privateKey: 'AAAA',
  createdAt: '2026-10-01T10:00:00Z',
}

const HOSTILE_FIELDS: [string, unknown][] = [
  ['the name is a number', { ...emptyLoginFields(), name: 5 }],
  ['the name is an object', { ...emptyLoginFields(), name: { a: 1 } }],
  ['the passkeys are text', { ...emptyLoginFields(), name: 'x', passkeys: 'nope' }],
  ['the sites are text', { ...emptyLoginFields(), name: 'x', sites: 'https://x' }],
  ['a site is a number', { ...emptyLoginFields(), name: 'x', sites: [5] }],
  ['a passkey is text', { ...emptyLoginFields(), name: 'x', passkeys: ['nope'] }],
  [
    'a passkey field is a number',
    { ...emptyLoginFields(), name: 'x', passkeys: [{ ...VALID_PASSKEY, credentialId: 5 }] },
  ],
  ['the fields are null', null],
  ['the fields are text', 'hello'],
  ['the fields are a list', []],
]

for (const [label, hostileFields] of HOSTILE_FIELDS) {
  it(`lists a shared item whose fields are wrong (${label}) as unreadable, and unlocks`, async () => {
    serve({
      records: [
        await sharedRecordFor('good-one', { ...emptyLoginFields(), name: 'Zed' }),
        await sharedRecordFor('good-two', { ...emptyLoginFields(), name: 'Alpha' }),
        await sharedRecordFor('hostile', hostileFields as object),
      ],
    })
    await openSession()

    const snapshot = passwordsSession.getSnapshot()
    expect(snapshot.isUnlocked).toBe(true)
    expect(snapshot.items).toHaveLength(3)
    const hostile = snapshot.items.find((item) => item.id === 'hostile')!
    expect(hostile.isUnreadable).toBe(true)
    expect(hostile.itemKey).toBeNull()
    expect(snapshot.items.filter((item) => !item.isUnreadable).map((item) => item.fields.name)).toEqual([
      'Alpha',
      'Zed',
    ])
  })
}

it('shows an item with wrong fields as one that could not be opened, in the list and open', async () => {
  serve({
    records: [
      await sharedRecordFor('hostile', { ...emptyLoginFields(), name: { a: 1 }, passkeys: 'nope', sites: 'https://x' }),
    ],
  })
  await openSession()
  const errors = vi.spyOn(console, 'error').mockImplementation(() => undefined)
  try {
    renderAt('/passwords')
    expect(await screen.findByText('Item that could not be opened')).toBeTruthy()
    cleanup()
    renderAt('/passwords?item=hostile')
    expect(await screen.findByText(/could not be opened on this device/)).toBeTruthy()
    expect(errors).not.toHaveBeenCalled()
  } finally {
    errors.mockRestore()
  }
})

it('treats a missing field or a null list as empty, as a program that writes null does', async () => {
  serve({
    records: [await sharedRecordFor('lenient', { name: 'Lenient', sites: null, passkeys: null } as unknown as object)],
  })
  await openSession()
  const item = passwordsSession.getSnapshot().items[0]
  expect(item.isUnreadable).toBe(false)
  expect(item.fields).toMatchObject({ name: 'Lenient', sites: [], passkeys: [], password: '' })
})

// --- re-keying when access narrows ---------------------------------------------------

const personCPublicKey = () => encodeBase64(personC.publicKey)

function sharedWith(grantees: { userId: string; name: string; publicKey: string | null; hasKey: boolean }[]) {
  return grantees.map((grantee) => ({
    id: `share-${grantee.userId}`,
    granteeUserId: grantee.userId,
    granteeCircleId: null,
    granteeName: grantee.name,
    recipients: [
      {
        userId: grantee.userId,
        name: grantee.name,
        username: `${grantee.userId}-login`,
        publicKey: grantee.publicKey,
        hasKey: grantee.hasKey,
        hasTooMuchShared: false,
      },
    ] as PasswordRecipient[],
  }))
}

async function openedKeyOf(sealed: string, itemId: string): Promise<Uint8Array> {
  return openItemKey(fixture.accountKey, parseSealedValue(sealed), itemId)
}

it('gives the item a new key when "Let the agent use this" is turned off, and the old key opens nothing after', async () => {
  const [oldRecord] = [
    await recordFor(
      'login-one',
      'login',
      { ...emptyLoginFields(), name: 'Example login', password: 'invented-password-1', access: accessOf([], true) },
      { isUsableByAgent: true },
    ),
  ]
  const server = serve({ records: [oldRecord] })
  await openSession()
  renderAt('/passwords?item=login-one')

  fireEvent.click(await screen.findByRole('checkbox', { name: /Let the agent use this/ }))
  await waitFor(() => expect(calls('RekeyPasswordItem')).toHaveLength(1))
  expect(calls('SetPasswordItemUsableByAgent')).toHaveLength(0)
  const input = (calls('RekeyPasswordItem')[0][1] as { input: Record<string, any> }).input
  expect(input).toMatchObject({ id: 'login-one', baseRevision: 3, itemKeyByAgentKey: null, shareKeys: [] })

  const oldKey = await openedKeyOf(oldRecord.itemKey, 'login-one')
  const newKey = await openedKeyOf(input.itemKey, 'login-one')
  expect(encodeBase64(newKey)).not.toBe(encodeBase64(oldKey))
  expect(await openItemFields(newKey, parseSealedValue(input.encryptedFields), 'login-one', 'login')).toMatchObject({
    name: 'Example login',
    password: 'invented-password-1',
    // The record the new fields carry says the agent is off.
    access: { isUsableByAgent: false, grantees: [] },
  })
  await expect(openItemFields(oldKey, parseSealedValue(input.encryptedFields), 'login-one', 'login')).rejects.toThrow()
  expect(await screen.findByText('The agent can no longer use this item.')).toBeTruthy()
  expect(server.records[0].isUsableByAgent).toBe(false)
  // The session holds the new key, so a later save seals with it.
  expect(encodeBase64(passwordsSession.getSnapshot().items[0].itemKey!)).toBe(encodeBase64(newKey))
})

it('seals the new key only for people the record reaches, whatever keys the device knew, when the agent is turned off', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  knowKeyOnDevice('person-b', personBPublicKey)
  serve({
    records: [
      await recordFor(
        'login-one',
        'login',
        {
          ...emptyLoginFields(),
          name: 'Example login',
          access: accessOf([{ userId: 'person-b' }, { userId: 'person-c' }, { userId: 'person-d' }], true),
        },
        {
          isUsableByAgent: true,
          shares: sharedWith([
            { userId: 'person-b', name: 'Person B', publicKey: personBPublicKey, hasKey: true },
            // Person C has a key for the item, and this device never saw theirs: a first key.
            { userId: 'person-c', name: 'Person C', publicKey: personCPublicKey(), hasKey: true },
            { userId: 'person-d', name: 'Person D', publicKey: null, hasKey: false },
            // A share the owner's record does not have, whose key is known here.
            { userId: 'person-e', name: 'Person E', publicKey: personBPublicKey, hasKey: true },
          ]),
        },
      ),
    ],
  })
  knowKeyOnDevice('person-e', personBPublicKey)
  await openSession()
  await passwordsSession.setUsableByAgent(passwordsSession.getSnapshot().items[0], false)

  expect(calls('RekeyPasswordItem')).toHaveLength(1)
  const input = (calls('RekeyPasswordItem')[0][1] as { input: Record<string, any> }).input
  expect(input.shareKeys).toHaveLength(2)
  expect(input.shareKeys[0]).toMatchObject({
    itemId: 'login-one',
    recipientUserId: 'person-b',
    recipientPublicKey: personBPublicKey,
  })
  expect(input.shareKeys[1]).toMatchObject({ recipientUserId: 'person-c', recipientPublicKey: personCPublicKey() })
  expect(storedKeys()).toMatchObject({ 'person-c': personCPublicKey() })
  const newKey = await openedKeyOf(input.itemKey, 'login-one')
  const sharedKey = await openForKeyPair(
    parseSealedForKeyPair(input.shareKeys[0].itemKeyByRecipientKey),
    personB,
    decodeBase64(fixture.input.publicKey),
    'login-one',
    USER_ID,
    'person-b',
  )
  expect(encodeBase64(sharedKey)).toBe(encodeBase64(newKey))
  // Person E, whom the record does not reach, is not in the record the new
  // fields carry either: only the grantees the server still lists stay.
  expect((await fieldsSentIn(input, 'login-one')).access.grantees).toEqual([
    { userId: 'person-b' },
    { userId: 'person-c' },
    { userId: 'person-d' },
  ])
})

it('gives the agent no copy on a re-key when only the server says it may use the item', async () => {
  // The server's flag is on, but the owner's sealed record never let the agent
  // in: nothing the server says can widen it.
  serve({
    records: [
      await recordFor(
        'login-one',
        'login',
        { ...emptyLoginFields(), name: 'Example login' },
        { isUsableByAgent: true, needsNewKey: true },
      ),
    ],
  })
  rememberOnDevice('login-one')
  await openSession()
  await passwordsSession.rekeyIfNeeded('login-one')

  expect(calls('RekeyPasswordItem')).toHaveLength(1)
  expect(calls('ReadPasswordsAgentKey')).toHaveLength(0)
  const input = (calls('RekeyPasswordItem')[0][1] as { input: Record<string, any> }).input
  expect(input.itemKeyByAgentKey).toBeNull()
})

it('does not show the agent as allowed when only the server says so', async () => {
  serve({
    records: [
      await recordFor(
        'login-one',
        'login',
        { ...emptyLoginFields(), name: 'Example login' },
        { isUsableByAgent: true },
      ),
    ],
  })
  await openSession()
  renderAt('/passwords?item=login-one')
  const agentSwitch = await screen.findByRole('checkbox', { name: /Let the agent use this/ })
  expect((agentSwitch as HTMLInputElement).checked).toBe(false)
  expect(screen.queryByText('Agent', { selector: '.tag' })).toBeNull()
})

it('seals the agent copy on a re-key when the opened record lets the agent in', async () => {
  serve({
    records: [
      await recordFor(
        'login-one',
        'login',
        { ...emptyLoginFields(), name: 'Example login', access: accessOf([], true) },
        { isUsableByAgent: true, needsNewKey: true },
      ),
    ],
  })
  // The item had a copy for the agent when this device last saw it.
  rememberOnDevice('login-one', { hasMadeAgentCopy: true })
  await openSession()
  await passwordsSession.rekeyIfNeeded('login-one')

  const input = (calls('RekeyPasswordItem')[0][1] as { input: Record<string, any> }).input
  const newKey = await openedKeyOf(input.itemKey, 'login-one')
  const agentKey = await openForKeyPair(
    parseSealedForKeyPair(input.itemKeyByAgentKey),
    agentKeyPair,
    decodeBase64(fixture.input.publicKey),
    'login-one',
    USER_ID,
    AGENT_RECIPIENT_ID,
  )
  expect(encodeBase64(agentKey)).toBe(encodeBase64(newKey))
})

it('gives the item a new key after somebody is taken off its shares, for the people who keep it and for the agent', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  knowKeyOnDevice('person-b', personBPublicKey)
  const oldRecord = await recordFor(
    'login-one',
    'login',
    {
      ...emptyLoginFields(),
      name: 'Example login',
      access: accessOf([{ userId: 'person-b' }, { userId: 'person-c' }], true),
    },
    {
      isUsableByAgent: true,
      shares: sharedWith([
        { userId: 'person-b', name: 'Person B', publicKey: personBPublicKey, hasKey: true },
        { userId: 'person-c', name: 'Person C', publicKey: personCPublicKey(), hasKey: true },
      ]),
    },
  )
  serve({ records: [oldRecord], recipients: [personBRecipient(personBPublicKey, true)] })
  await openSession()
  renderAt('/passwords?item=login-one')

  fireEvent.click(await screen.findByRole('button', { name: 'Example login: Share' }))
  const dialog = await screen.findByRole('dialog', { name: 'Share Example login' })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Stop sharing with Person C' }))
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save sharing' }))

  await waitFor(() => expect(calls('RekeyPasswordItem')).toHaveLength(1))
  // Narrowing: the narrower record is saved first by the person's tap, then
  // the server is asked for the narrower list, then the item is re-keyed with
  // the same record.
  expect(orderOf({ SetPasswordItemShares: 'shares', RekeyPasswordItem: 'rekey', SavePasswordItem: 'save' })).toEqual([
    'save',
    'shares',
    'rekey',
  ])
  await expectSignedAccess(
    await fieldsSentIn((calls('SavePasswordItem')[0][1] as { input: any }).input, 'login-one'),
    'login-one',
    { isUsableByAgent: true, grantees: [{ userId: 'person-b' }] },
    2,
  )
  const input = (calls('RekeyPasswordItem')[0][1] as { input: Record<string, any> }).input
  expect(input.shareKeys.map((key: { recipientUserId: string }) => key.recipientUserId)).toEqual(['person-b'])
  // The fields the re-key carries have the narrower record.
  await expectSignedAccess(
    await fieldsSentIn(input, 'login-one'),
    'login-one',
    { isUsableByAgent: true, grantees: [{ userId: 'person-b' }] },
    3,
  )
  const newKey = await openedKeyOf(input.itemKey, 'login-one')
  const agentKey = await openForKeyPair(
    parseSealedForKeyPair(input.itemKeyByAgentKey),
    agentKeyPair,
    decodeBase64(fixture.input.publicKey),
    'login-one',
    USER_ID,
    AGENT_RECIPIENT_ID,
  )
  expect(encodeBase64(agentKey)).toBe(encodeBase64(newKey))
  expect(await screen.findByText('Sharing saved.')).toBeTruthy()
})

it('says the item could not be given a new key after a removal, offers to try again, and the server keeps its mark', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  knowKeyOnDevice('person-b', personBPublicKey)
  const server = serve({
    records: [
      await recordFor(
        'login-one',
        'login',
        { ...emptyLoginFields(), name: 'Example login', access: accessOf([{ userId: 'person-b' }]) },
        { shares: sharedWith([{ userId: 'person-b', name: 'Person B', publicKey: personBPublicKey, hasKey: true }]) },
      ),
    ],
    isRekeyFailing: true,
  })
  await openSession()
  renderAt('/passwords?item=login-one')

  fireEvent.click(await screen.findByRole('button', { name: 'Example login: Share' }))
  const dialog = await screen.findByRole('dialog', { name: 'Share Example login' })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Stop sharing with Person B' }))
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save sharing' }))

  expect(await screen.findByText('The item still needs a new key.')).toBeTruthy()
  // The removal itself went through, and the server marked the item. Nothing
  // is remembered on this device: the mark is the server's.
  expect(calls('SetPasswordItemShares')).toHaveLength(1)
  expect(server.records[0].needsNewKey).toBe(true)
  expect(passwordsSession.getSnapshot().items[0].needsNewKey).toBe(true)
  expect(window.localStorage.getItem(`teanode.passwords.rekeyPending:${USER_ID}`)).toBeNull()

  server.isRekeyFailing = false
  fireEvent.click(screen.getByRole('button', { name: 'Try again' }))
  expect(await screen.findByText('The item has a new key.')).toBeTruthy()
  expect(calls('RekeyPasswordItem')).toHaveLength(2)
  expect(server.records[0].needsNewKey).toBe(false)
  expect(passwordsSession.getSnapshot().items[0].needsNewKey).toBe(false)
})

it('says so again when trying again fails too', async () => {
  knowKeyOnDevice('person-b', encodeBase64(personB.publicKey))
  serve({
    records: [
      await recordFor(
        'login-one',
        'login',
        { ...emptyLoginFields(), name: 'Example login', access: accessOf([{ userId: 'person-b' }]) },
        {
          shares: sharedWith([
            { userId: 'person-b', name: 'Person B', publicKey: encodeBase64(personB.publicKey), hasKey: true },
          ]),
        },
      ),
    ],
    isRekeyFailing: true,
  })
  await openSession()
  renderAt('/passwords?item=login-one')
  fireEvent.click(await screen.findByRole('button', { name: 'Example login: Share' }))
  const dialog = await screen.findByRole('dialog', { name: 'Share Example login' })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Stop sharing with Person B' }))
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save sharing' }))
  await screen.findByText('The item still needs a new key.')

  fireEvent.click(screen.getByRole('button', { name: 'Try again' }))
  await waitFor(() => expect(calls('RekeyPasswordItem')).toHaveLength(2))
  expect(await screen.findByRole('button', { name: 'Try again' })).toBeTruthy()
  expect(passwordsSession.getSnapshot().items[0].needsNewKey).toBe(true)
})

it('leaves the agent switch on and says why when the re-key is refused', async () => {
  serve({
    records: [
      await recordFor(
        'login-one',
        'login',
        { ...emptyLoginFields(), name: 'Example login', access: accessOf([], true) },
        { isUsableByAgent: true },
      ),
    ],
    isRekeyFailing: true,
  })
  await openSession()
  renderAt('/passwords?item=login-one')
  fireEvent.click(await screen.findByRole('checkbox', { name: /Let the agent use this/ }))
  expect(await screen.findByText('The server could not re-key the item.')).toBeTruthy()
  expect(passwordsSession.getSnapshot().items[0].isUsableByAgent).toBe(true)
})

it('reads the item again and tries once more when another device changed it before the re-key', async () => {
  const server = serve({
    records: [
      await recordFor(
        'login-one',
        'login',
        { ...emptyLoginFields(), name: 'Example login', access: accessOf([], true) },
        { isUsableByAgent: true },
      ),
    ],
  })
  await openSession()
  // Another device saved the item after this one read it.
  server.records = [{ ...server.records[0], revision: 40 }]
  await passwordsSession.setUsableByAgent(passwordsSession.getSnapshot().items[0], false)

  expect(calls('RekeyPasswordItem')).toHaveLength(2)
  expect((calls('RekeyPasswordItem')[0][1] as { input: { baseRevision: number } }).input.baseRevision).toBe(3)
  expect((calls('RekeyPasswordItem')[1][1] as { input: { baseRevision: number } }).input.baseRevision).toBe(40)
})

it('gives an item the server marked a new key with the next save, on a device that remembers it', async () => {
  rememberOnDevice('login-one')
  serve({
    records: [
      await recordFor('login-one', 'login', { ...emptyLoginFields(), name: 'Example login' }, { needsNewKey: true }),
    ],
  })
  await openSession()
  const held = passwordsSession.getSnapshot().items[0]
  expect(held.needsNewKey).toBe(true)
  await passwordsSession.saveItem(
    { itemKind: 'login', fields: { ...held.fields, password: 'invented-new-password' } as never },
    held,
  )

  expect(calls('SavePasswordItem')).toHaveLength(0)
  expect(calls('RekeyPasswordItem')).toHaveLength(1)
  const input = (calls('RekeyPasswordItem')[0][1] as { input: Record<string, any> }).input
  expect(input.baseRevision).toBe(held.revision)
  const newKey = await openedKeyOf(input.itemKey, 'login-one')
  expect(await openItemFields(newKey, parseSealedValue(input.encryptedFields), 'login-one', 'login')).toMatchObject({
    password: 'invented-new-password',
  })
  expect(passwordsSession.getSnapshot().items[0].needsNewKey).toBe(false)
})

it('gives every item the server marked a new key as Passwords opens, once', async () => {
  const server = serve({
    records: [
      await recordFor('login-one', 'login', { ...emptyLoginFields(), name: 'Example login' }, { needsNewKey: true }),
      await recordFor('login-two', 'login', { ...emptyLoginFields(), name: 'Bank' }),
    ],
  })
  rememberOnDevice('login-one')
  rememberOnDevice('login-two')
  await openSession()
  renderAt('/passwords')
  await screen.findByRole('link', { name: 'Example login' })
  await waitFor(() => expect(calls('RekeyPasswordItem')).toHaveLength(1))
  expect((calls('RekeyPasswordItem')[0][1] as { input: { id: string } }).input.id).toBe('login-one')
  await waitFor(() => expect(server.records.find((record) => record.id === 'login-one')?.needsNewKey).toBe(false))
})

it('does not make a second new key when the item is opened while the page gives it one', async () => {
  serve({
    records: [
      await recordFor('login-one', 'login', { ...emptyLoginFields(), name: 'Example login' }, { needsNewKey: true }),
    ],
  })
  rememberOnDevice('login-one')
  rememberOnDevice('login-two')
  await openSession()
  renderAt('/passwords?item=login-one')
  await waitFor(() => expect(passwordsSession.getSnapshot().items[0].needsNewKey).toBe(false))
  expect(calls('RekeyPasswordItem')).toHaveLength(1)
})

it('gives a person who held a key the new key at once in a re-key, with no question', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  // This device saw Person B hold a key for the item before the server marked it.
  rememberOnDevice('login-one', { sealedForUserIds: ['person-b'] })
  serve({
    records: [
      await recordFor(
        'login-one',
        'login',
        { ...emptyLoginFields(), name: 'Example login', access: accessOf([{ userId: 'person-b' }]) },
        {
          needsNewKey: true,
          shares: sharedWith([{ userId: 'person-b', name: 'Person B', publicKey: personBPublicKey, hasKey: true }]),
        },
      ),
    ],
  })
  await openSession()
  renderAt('/passwords')

  await waitFor(() => expect(calls('RekeyPasswordItem')).toHaveLength(1))
  const rekey = (calls('RekeyPasswordItem')[0][1] as { input: Record<string, any> }).input
  expect(rekey.shareKeys).toHaveLength(1)
  expect(rekey.shareKeys[0]).toMatchObject({ recipientUserId: 'person-b', recipientPublicKey: personBPublicKey })
  expect(screen.queryByRole('alertdialog')).toBeNull()
  expect(storedKeys()).toEqual({ 'person-b': personBPublicKey })
})

it('re-keys for the people who stay at once when somebody is taken off', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  serve({
    records: [
      await recordFor(
        'login-one',
        'login',
        {
          ...emptyLoginFields(),
          name: 'Example login',
          access: accessOf([{ userId: 'person-b' }, { userId: 'person-c' }]),
        },
        {
          shares: sharedWith([
            { userId: 'person-b', name: 'Person B', publicKey: personBPublicKey, hasKey: true },
            { userId: 'person-c', name: 'Person C', publicKey: personCPublicKey(), hasKey: true },
          ]),
        },
      ),
    ],
    recipients: [personBRecipient(personBPublicKey, true)],
  })
  await openSession()
  renderAt('/passwords?item=login-one')

  fireEvent.click(await screen.findByRole('button', { name: 'Example login: Share' }))
  const dialog = await screen.findByRole('dialog', { name: 'Share Example login' })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Stop sharing with Person C' }))
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save sharing' }))

  await waitFor(() => expect(calls('RekeyPasswordItem')).toHaveLength(1))
  const rekey = (calls('RekeyPasswordItem')[0][1] as { input: Record<string, any> }).input
  expect(rekey.shareKeys).toHaveLength(1)
  expect(rekey.shareKeys[0]).toMatchObject({ recipientUserId: 'person-b', recipientPublicKey: personBPublicKey })
  expect(screen.queryByRole('alertdialog')).toBeNull()
})

// --- the title, the fields' attributes and the wording ------------------------------

it('names the kind in the tab title and never the open item', async () => {
  serve({ records: await recordsToList() })
  await openSession()
  renderAt('/passwords?item=login-two')
  await screen.findByRole('heading', { name: 'Bank' })
  await waitFor(() => expect(document.title).toContain('Login'))
  expect(document.title).not.toContain('Bank')
  expect(document.title).toContain('Passwords')
})

it('tells browsers and password managers to leave the passphrase and the editor alone', async () => {
  serve({ records: [] })
  renderAt('/passwords')
  const passphrase = await screen.findByLabelText('Passphrase')
  // The passphrase already exists, so it is a current password and not a new one.
  expect(passphrase.getAttribute('autocomplete')).toBe('current-password')
  expect(passphrase.getAttribute('data-1p-ignore')).toBe('true')
  expect(passphrase.getAttribute('data-lpignore')).toBe('true')
  expect(passphrase.getAttribute('data-bwignore')).toBe('true')

  cleanup()
  await openSession()
  renderAt('/passwords?new=login')
  const dialog = await screen.findByRole('dialog')
  const password = within(dialog).getByLabelText('Password')
  expect(password.getAttribute('autocomplete')).toBe('new-password')
  expect(password.getAttribute('data-bwignore')).toBe('true')
  const userName = within(dialog).getByLabelText('User name')
  expect(userName.getAttribute('autocomplete')).toBe('off')
  for (const attribute of ['data-1p-ignore', 'data-lpignore', 'data-bwignore']) {
    expect(userName.getAttribute(attribute)).toBe('true')
  }
})

it('masks a card number and security code with CSS in text fields where the browser can, and keeps password fields where it cannot', async () => {
  serve({ records: [] })
  await openSession()
  try {
    vi.stubGlobal('CSS', { supports: () => true })
    renderAt('/passwords?new=card')
    let dialog = await screen.findByRole('dialog')
    for (const label of ['Card number', 'Security code']) {
      const field = within(dialog).getByLabelText(label)
      expect(field.getAttribute('type')).toBe('text')
      expect(field.classList.contains('passwords-masked')).toBe(true)
      expect(field.getAttribute('inputmode')).toBe('numeric')
      expect(field.getAttribute('autocomplete')).toBe('off')
      expect(field.getAttribute('data-bwignore')).toBe('true')
    }
    cleanup()

    vi.stubGlobal('CSS', { supports: () => false })
    renderAt('/passwords?new=card')
    dialog = await screen.findByRole('dialog')
    for (const label of ['Card number', 'Security code']) {
      expect(within(dialog).getByLabelText(label).getAttribute('type')).toBe('password')
    }
  } finally {
    vi.unstubAllGlobals()
  }
})

it('shows the masked card number and security code with one Show button, and each field has the attributes once', async () => {
  serve({ records: [] })
  await openSession()
  try {
    vi.stubGlobal('CSS', { supports: () => true })
    renderAt('/passwords?new=card')
    const dialog = await screen.findByRole('dialog')
    const number = within(dialog).getByLabelText('Card number')
    const code = within(dialog).getByLabelText('Security code')
    expect(number.classList.contains('passwords-masked')).toBe(true)
    fireEvent.click(within(dialog).getByRole('button', { name: 'Show card number and security code' }))
    for (const field of [number, code]) {
      expect(field.getAttribute('type')).toBe('text')
      expect(field.classList.contains('passwords-masked')).toBe(false)
      // Still kept away from the browser's and the extensions' managers.
      expect(field.getAttribute('autocomplete')).toBe('off')
      expect(field.getAttribute('data-bwignore')).toBe('true')
    }
    fireEvent.click(within(dialog).getByRole('button', { name: 'Hide card number and security code' }))
    expect(number.classList.contains('passwords-masked')).toBe(true)
  } finally {
    vi.unstubAllGlobals()
  }
})

it('asks for the current passphrase as a current password and the new one as a new password', async () => {
  serve({ records: [] })
  await openSession()
  renderAt('/passwords?tab=settings')
  fireEvent.click(await screen.findByRole('button', { name: 'Change passphrase' }))
  const dialog = await screen.findByRole('dialog', { name: 'Change passphrase' })
  expect(within(dialog).getByLabelText('Current passphrase').getAttribute('autocomplete')).toBe('current-password')
  expect(within(dialog).getByLabelText('New passphrase').getAttribute('autocomplete')).toBe('new-password')
  expect(within(dialog).getByLabelText('Type it again').getAttribute('autocomplete')).toBe('new-password')
})

it('shows the own safety code in Settings from the opened key pair', async () => {
  serve({ records: [], unlockPublicKey: null })
  await openSession()
  renderAt('/passwords?tab=settings')
  expect(await screen.findByText('Your safety code')).toBeTruthy()
  const code = await safetyCode(fixture.keyPair.publicKey)
  expect(code).toMatch(/^(\d{4} ){7}\d{4}$/)
  expect(await screen.findByText(code)).toBeTruthy()
  expect(passwordsSession.getSnapshot().publicKey).toBe(encodeBase64(fixture.keyPair.publicKey))
})

it('says the old recovery key no longer unlocks Passwords, and nothing stronger', async () => {
  serve({ records: [] })
  await openSession()
  renderAt('/passwords?tab=settings')
  fireEvent.click(await screen.findByRole('button', { name: 'Replace recovery key' }))
  const dialog = await screen.findByRole('dialog', { name: 'Replace recovery key' })
  expect(within(dialog).getByText(/the old recovery key no longer unlocks Passwords/)).toBeTruthy()
  expect(within(dialog).queryByText(/stops working/)).toBeNull()
})

// --- the clipboard waits for focus -----------------------------------------------------

it('clears a copied secret when the window is focused again, if the window had no focus at 30 seconds', async () => {
  vi.useFakeTimers()
  try {
    let clipboardText = 'invented-secret'
    Object.defineProperty(navigator, 'clipboard', {
      value: {
        readText: vi.fn(async () => clipboardText),
        writeText: vi.fn(async (text: string) => {
          clipboardText = text
        }),
      },
      configurable: true,
    })
    let isFocused = true
    const hasFocus = vi.spyOn(document, 'hasFocus').mockImplementation(() => isFocused)
    clearClipboardLater('invented-secret')
    // The person switched to the site's tab to paste.
    isFocused = false
    await vi.advanceTimersByTimeAsync(30_001)
    expect(clipboardText).toBe('invented-secret')
    await vi.advanceTimersByTimeAsync(10 * 60_000)
    expect(clipboardText).toBe('invented-secret')

    // A focus event while the window still has none changes nothing.
    window.dispatchEvent(new Event('focus'))
    await vi.advanceTimersByTimeAsync(0)
    expect(clipboardText).toBe('invented-secret')

    isFocused = true
    window.dispatchEvent(new Event('focus'))
    await vi.advanceTimersByTimeAsync(0)
    expect(clipboardText).toBe('')

    // Something the person copied meanwhile is left alone, and once is enough.
    clipboardText = 'invented-secret'
    window.dispatchEvent(new Event('focus'))
    await vi.advanceTimersByTimeAsync(0)
    expect(clipboardText).toBe('invented-secret')

    // The same when the page is shown again rather than focused.
    isFocused = false
    clearClipboardLater('invented-secret')
    await vi.advanceTimersByTimeAsync(30_001)
    clipboardText = 'something the person copied after'
    isFocused = true
    document.dispatchEvent(new Event('visibilitychange'))
    await vi.advanceTimersByTimeAsync(0)
    expect(clipboardText).toBe('something the person copied after')

    isFocused = false
    clearClipboardLater('another-secret')
    clipboardText = 'another-secret'
    await vi.advanceTimersByTimeAsync(30_001)
    isFocused = true
    document.dispatchEvent(new Event('visibilitychange'))
    await vi.advanceTimersByTimeAsync(0)
    expect(clipboardText).toBe('')
    hasFocus.mockRestore()
  } finally {
    Object.defineProperty(navigator, 'clipboard', { value: undefined, configurable: true })
    vi.useRealTimers()
  }
})

// --- the access record is signed, versioned and remembered (round 5) ------------------

it('signs a new item with a record that grants nobody, and keeps the record as it is on every edit of the fields', async () => {
  const server = serve({ records: await onlyExampleLogin() })
  await openSession()
  const held = passwordsSession.getSnapshot().items[0]
  expect(held.verifiedAccess).toMatchObject({ recordVersion: 1 })
  expect(held.accessProblem).toBeNull()

  await passwordsSession.saveItem({ itemKind: 'note', fields: { ...emptyNoteFields(), name: 'Fresh note' } }, null)
  const created = (calls('SavePasswordItem')[0][1] as { input: any }).input
  await expectSignedAccess(
    await fieldsSentIn(created, created.id, 'note'),
    created.id,
    { isUsableByAgent: false, grantees: [] },
    1,
    'note',
  )

  await passwordsSession.saveItem(
    { itemKind: 'login', fields: { ...held.fields, password: 'invented-new-password' } as never },
    held,
  )
  const edited = (calls('SavePasswordItem')[1][1] as { input: any }).input
  const editedFields = await fieldsSentIn(edited, 'login-one')
  expect(editedFields.password).toBe('invented-new-password')
  // A save of the fields signs nothing: the record is the one the item had,
  // with its version and signature, so an old record is never written forward.
  await expectSignedAccess(editedFields, 'login-one', { isUsableByAgent: false, grantees: [] }, 1)
  expect(pickSignature(editedFields.access)).toEqual(pickSignature(held.verifiedAccess))
  // The highest version seen is kept on this device, per item.
  expect(deviceMemoryOf('login-one')?.highestRecordVersion).toBe(1)
  expect(server.records.length).toBe(2)
})

it('treats a record written by whoever holds the item key as unchecked: it reaches nobody and gives the agent nothing', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  knowKeyOnDevice('person-b', personBPublicKey)
  const server = serve({ records: await onlyExampleLogin(), waiting: [] })
  await openSession()
  const item = passwordsSession.getSnapshot().items[0]
  // Whoever holds the item key (a person it was shared with, or the server
  // through the agent's copy) seals new fields with a record of their own.
  const forged = await sealPasswordItem(
    fixture.accountKey,
    'login-one',
    'login',
    {
      ...item.fields,
      // The signature and version the owner's record had, on words it did not say.
      access: { ...accessOf([{ userId: 'person-b' }], true), ...pickSignature(item.verifiedAccess) },
    } as never,
    item.itemKey,
  )
  server.records = [
    {
      ...server.records[0],
      encryptedFields: forged.encryptedFields,
      revision: 50,
      isUsableByAgent: true,
      needsNewKey: true,
      shares: sharedWith([{ userId: 'person-b', name: 'Person B', publicKey: personBPublicKey, hasKey: false }]),
    },
  ]
  server.waiting = [{ itemId: 'login-one', recipient: personBRecipient(personBPublicKey, false) }]
  await passwordsSession.refresh()

  const forgedItem = passwordsSession.getSnapshot().items[0]
  expect(forgedItem.accessProblem).toBe('notVerified')
  expect(forgedItem.verifiedAccess).toBeNull()
  // Not the server's flag, not the record's own words, make the agent allowed.
  expect(isAgentAllowed(forgedItem)).toBe(false)
  expect(await passwordsSession.rekeyIfNeeded('login-one')).toBeNull()
  expect(await passwordsSession.rekeyMarkedItems()).toEqual([])
  const result = await passwordsSession.completeWaitingShares()
  expect(result.storedKeyCount).toBe(0)
  expect(calls('RekeyPasswordItem')).toHaveLength(0)
  expect(calls('AddPasswordShareKeys')).toHaveLength(0)
  expect(calls('ReadPasswordsAgentKey')).toHaveLength(0)
})

it('shows "Sharing could not be checked" for an item with no signature, and sets sharing again with a button', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  knowKeyOnDevice('person-b', personBPublicKey)
  const server = serve({
    records: [
      // A record that names Person B and the agent but carries no signature.
      await recordFor(
        'login-one',
        'login',
        { ...emptyLoginFields(), name: 'Example login', access: accessOf([{ userId: 'person-b' }], true) },
        {
          isUsableByAgent: true,
          shares: sharedWith([{ userId: 'person-b', name: 'Person B', publicKey: personBPublicKey, hasKey: true }]),
        },
        { isAccessSigned: false },
      ),
    ],
    recipients: [personBRecipient(personBPublicKey, true)],
  })
  await openSession()
  renderAt('/passwords?item=login-one')

  expect(await screen.findByText('Sharing could not be checked')).toBeTruthy()
  expect(screen.queryByText('Agent', { selector: '.tag' })).toBeNull()
  expect(screen.queryByText(/Shared with/)).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'Set sharing again' }))
  const dialog = await screen.findByRole('dialog', { name: 'Share Example login' })
  // Nothing is carried over from the record nobody signed.
  expect(within(dialog).getByText('Not shared with anyone.')).toBeTruthy()
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save sharing' }))

  await waitFor(() => expect(calls('SetPasswordItemShares')).toHaveLength(1))
  await waitFor(() => expect(screen.queryByText('Sharing could not be checked')).toBeNull())
  // The record the tap wrote is signed, grants nobody and has the agent off.
  const saved = await fieldsSentIn((calls('SavePasswordItem')[0][1] as { input: any }).input, 'login-one')
  await expectSignedAccess(saved, 'login-one', { isUsableByAgent: false, grantees: [] }, 1)
  expect(passwordsSession.getSnapshot().items[0].accessProblem).toBeNull()
  expect(server.records.length).toBe(1)
})

it('does nothing automatic with an item whose older record the server brought back, and says so', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  knowKeyOnDevice('person-b', personBPublicKey)
  const benRecipient = personBRecipient(personBPublicKey, true)
  const base = await recordFor(
    'login-one',
    'login',
    {
      ...emptyLoginFields(),
      name: 'Example login',
      password: 'invented-password-1',
      access: accessOf([{ userId: 'person-b' }], true),
    },
    {
      isUsableByAgent: true,
      shares: sharedWith([{ userId: 'person-b', name: 'Person B', publicKey: personBPublicKey, hasKey: true }]),
    },
  )
  const server = serve({ records: [base], recipients: [benRecipient] })
  await openSession()
  const olderRecord = JSON.parse(JSON.stringify(server.records[0])) as PasswordItemRecord
  // The owner takes Person B off and turns the agent off: two signed records.
  const unshared = await passwordsSession.shareItem(passwordsSession.getSnapshot().items[0], [])
  await passwordsSession.setUsableByAgent(unshared.item, false)
  const narrowed = passwordsSession.getSnapshot().items[0]
  expect(narrowed.verifiedAccess).toMatchObject({ isUsableByAgent: false, grantees: [] })
  expect(deviceMemoryOf('login-one')?.highestRecordVersion).toBeGreaterThanOrEqual(3)
  passwordsSession.lock()

  // The server answers with the older item, whose record is right but lower
  // than the version seen, and marks it as needing a new key.
  server.records = [{ ...olderRecord, revision: 1000, needsNewKey: true }]
  server.waiting = [{ itemId: 'login-one', recipient: personBRecipient(personBPublicKey, false) }]
  vi.mocked(graphql).mock.calls.length = 0
  await openSession()
  expect(passwordsSession.getSnapshot().items[0].accessProblem).toBe('replayed')
  expect(await passwordsSession.rekeyMarkedItems()).toEqual([])
  expect((await passwordsSession.completeWaitingShares()).storedKeyCount).toBe(0)
  expect(calls('RekeyPasswordItem')).toHaveLength(0)
  expect(calls('AddPasswordShareKeys')).toHaveLength(0)
  expect(calls('ReadPasswordsAgentKey')).toHaveLength(0)

  renderAt('/passwords?item=login-one')
  expect(await screen.findByText("This item's sharing was changed back by the server")).toBeTruthy()
  expect(screen.getByRole('button', { name: 'Set sharing again' })).toBeTruthy()
  expect(screen.queryByText('Agent', { selector: '.tag' })).toBeNull()
  expect(calls('RekeyPasswordItem')).toHaveLength(0)
})

it("keeps the record when the server marks an item and hides the share: no device cuts it on the server's word", async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  knowKeyOnDevice('person-b', personBPublicKey)
  const base = await recordFor(
    'login-one',
    'login',
    { ...emptyLoginFields(), name: 'Example login', access: accessOf([{ userId: 'person-b' }], false) },
    { shares: sharedWith([{ userId: 'person-b', name: 'Person B', publicKey: personBPublicKey, hasKey: true }]) },
  )
  const server = serve({ records: [base] })
  // The server hides the share and marks the item.
  server.records = [{ ...base, shares: [], needsNewKey: true, revision: 5 }]
  rememberOnDevice('login-one')
  await openSession()
  await passwordsSession.rekeyMarkedItems()

  const input = (calls('RekeyPasswordItem')[0][1] as { input: any }).input
  expect(input.shareKeys).toEqual([])
  // The record is the one the owner signed, written again with the next version.
  await expectSignedAccess(
    await fieldsSentIn(input, 'login-one'),
    'login-one',
    { isUsableByAgent: false, grantees: [{ userId: 'person-b' }] },
    2,
  )
})

it('seals an automatic new key only for people this device sealed for, and the agent only if this device made a copy', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  knowKeyOnDevice('person-b', personBPublicKey)
  knowKeyOnDevice('person-c', personCPublicKey())
  const records = (isMarked: boolean) =>
    recordFor(
      'login-one',
      'login',
      {
        ...emptyLoginFields(),
        name: 'Example login',
        access: accessOf([{ userId: 'person-b' }, { userId: 'person-c' }], true),
      },
      {
        isUsableByAgent: true,
        needsNewKey: isMarked,
        shares: sharedWith([
          { userId: 'person-b', name: 'Person B', publicKey: personBPublicKey, hasKey: true },
          // Person C is in the record and confirmed here, but has never had a key.
          { userId: 'person-c', name: 'Person C', publicKey: personCPublicKey(), hasKey: false },
        ]),
      },
    )
  const unmarked = await records(false)
  const server = serve({ records: [unmarked] })
  // This device sealed the item for Person B and made the agent a copy in an
  // earlier session, but never sealed it for Person C.
  rememberOnDevice('login-one', { sealedForUserIds: ['person-b'], hasMadeAgentCopy: true })
  await openSession()
  expect(deviceMemoryOf('login-one')).toMatchObject({ sealedForUserIds: ['person-b'], hasMadeAgentCopy: true })
  // The server marks it, and the 30 second read brings the mark.
  server.records = [{ ...unmarked, needsNewKey: true, revision: 40 }]
  await passwordsSession.refresh()
  await passwordsSession.rekeyIfNeeded('login-one')

  const input = (calls('RekeyPasswordItem')[0][1] as { input: any }).input
  expect(input.shareKeys.map((key: { recipientUserId: string }) => key.recipientUserId)).toEqual(['person-b'])
  expect(input.itemKeyByAgentKey).not.toBeNull()
})

it('gives the agent no copy on an automatic new key when the item had none when last seen', async () => {
  serve({
    records: [
      await recordFor(
        'login-one',
        'login',
        { ...emptyLoginFields(), name: 'Example login', access: accessOf([], true) },
        { isUsableByAgent: true, needsNewKey: true },
      ),
    ],
  })
  rememberOnDevice('login-one')
  await openSession()
  await passwordsSession.rekeyIfNeeded('login-one')

  const input = (calls('RekeyPasswordItem')[0][1] as { input: any }).input
  expect(input.itemKeyByAgentKey).toBeNull()
  expect(calls('ReadPasswordsAgentKey')).toHaveLength(0)
  // The record still says the agent may, since only the person narrows it.
  await expectSignedAccess(
    await fieldsSentIn(input, 'login-one'),
    'login-one',
    { isUsableByAgent: true, grantees: [] },
    2,
  )
})

it('seals nothing new when the server replays an older record with the agent and a person in it after a narrowing on this device', async () => {
  // The audit's rollback case with no memory of the versions: Person B is in the
  // older record and confirmed here, but never held a key as far as this device
  // saw, and the agent had no copy.
  const personBPublicKey = encodeBase64(personB.publicKey)
  knowKeyOnDevice('person-b', personBPublicKey)
  serve({
    records: [
      await recordFor(
        'login-one',
        'login',
        { ...emptyLoginFields(), name: 'Example login', access: accessOf([{ userId: 'person-b' }], true) },
        {
          isUsableByAgent: true,
          needsNewKey: true,
          revision: 1000,
          shares: sharedWith([{ userId: 'person-b', name: 'Person B', publicKey: personBPublicKey, hasKey: false }]),
        },
      ),
    ],
  })
  rememberOnDevice('login-one')
  await openSession()
  await passwordsSession.rekeyMarkedItems()

  const input = (calls('RekeyPasswordItem')[0][1] as { input: any }).input
  expect(input.shareKeys).toEqual([])
  expect(input.itemKeyByAgentKey).toBeNull()
})

it('tries an automatic new key for an item once in ten minutes, and refuses the rest until the person taps Try again', async () => {
  const marked = await recordFor(
    'login-one',
    'login',
    { ...emptyLoginFields(), name: 'Example login' },
    { needsNewKey: true },
  )
  const server = serve({ records: [marked] })
  const now = vi.spyOn(Date, 'now')
  try {
    now.mockReturnValue(1_000_000)
    rememberOnDevice('login-one')
    await openSession()
    await passwordsSession.rekeyIfNeeded('login-one')
    expect(calls('RekeyPasswordItem')).toHaveLength(1)

    // The server marks it again straight away.
    server.records = [{ ...server.records[0], needsNewKey: true, revision: server.records[0].revision + 10 }]
    await passwordsSession.refresh()
    now.mockReturnValue(1_000_000 + 9 * 60_000)
    await expect(passwordsSession.rekeyIfNeeded('login-one')).rejects.toThrow()
    expect(calls('RekeyPasswordItem')).toHaveLength(1)
    expect(await passwordsSession.rekeyMarkedItems()).toEqual(['login-one'])

    // After ten minutes the next automatic one goes through.
    now.mockReturnValue(1_000_000 + 10 * 60_000 + 1)
    await passwordsSession.rekeyIfNeeded('login-one')
    expect(calls('RekeyPasswordItem')).toHaveLength(2)

    // The person's own tap is not held back.
    server.records = [{ ...server.records[0], needsNewKey: true, revision: server.records[0].revision + 10 }]
    await passwordsSession.refresh()
    await passwordsSession.giveNewKey('login-one')
    expect(calls('RekeyPasswordItem')).toHaveLength(3)
  } finally {
    now.mockRestore()
  }
})

it('makes at most 20 automatic new keys in one unlocked session', async () => {
  const records: PasswordItemRecord[] = []
  for (let index = 0; index < 22; index += 1) {
    records.push(
      await recordFor(
        `login-${index}`,
        'login',
        { ...emptyLoginFields(), name: `Login ${index}` },
        { needsNewKey: true },
      ),
    )
  }
  serve({ records })
  for (const record of records) rememberOnDevice(record.id)
  await openSession()
  const failedItemIds = await passwordsSession.rekeyMarkedItems()

  expect(calls('RekeyPasswordItem')).toHaveLength(20)
  expect(failedItemIds).toHaveLength(2)
  // The tap on Try again still works for those.
  await passwordsSession.giveNewKey(failedItemIds[0])
  expect(calls('RekeyPasswordItem')).toHaveLength(21)
})

it('names the item key a share key was sealed from, and refuses a key sealed from a key another device replaced', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  const server = serve({ records: await onlyExampleLogin(), recipients: [personBRecipient(personBPublicKey)] })
  await openSession()
  const first = passwordsSession.getSnapshot().items[0]
  const targets = [
    { grantee: { granteeUserId: 'person-b', granteeCircleId: null }, recipients: [personBRecipient(personBPublicKey)] },
  ]
  const shared = await passwordsSession.shareItem(first, targets)

  // Another device gives the item a new key. This one has not read it yet.
  const current = server.records[0]
  const sealedNew = await sealPasswordItem(fixture.accountKey, 'login-one', 'login', shared.item.fields as never, null)
  server.records = [
    {
      ...current,
      encryptedFields: sealedNew.encryptedFields,
      itemKey: sealedNew.itemKey,
      revision: current.revision + 10,
    },
  ]
  // Saving again: the key sealed from the old item key is refused, and
  // nothing stale is stored.
  await expect(passwordsSession.shareItem(shared.item, targets)).rejects.toThrow(STALE_MESSAGE)
  const sent = (calls('SetPasswordItemShares')[1][1] as { input: { keys: { itemKey: string }[] } }).input.keys
  expect(sent).toHaveLength(1)
  expect(sent[0].itemKey).toBe(shared.item.sealedItemKey)
  expect(sent[0].itemKey).not.toBe(server.records[0].itemKey)
  // The item was read again, so the next try starts from the current key.
  expect(passwordsSession.getSnapshot().items[0].sealedItemKey).toBe(server.records[0].itemKey)
  await passwordsSession.shareItem(passwordsSession.getSnapshot().items[0], targets)
  const retriedKeys = (calls('SetPasswordItemShares')[2][1] as { input: { keys: { itemKey: string }[] } }).input.keys
  expect(retriedKeys[0].itemKey).toBe(server.records[0].itemKey)
})

it('leaves the access record out of an export, as the command line does', async () => {
  serve({
    records: [
      await recordFor('login-one', 'login', {
        ...emptyLoginFields(),
        name: 'Example login',
        access: accessOf([{ userId: 'person-b' }], true),
      }),
    ],
  })
  await openSession()
  const exported = JSON.parse(exportText(passwordsSession.getSnapshot().items, new Date('2026-10-05T10:00:00Z')))
  expect(exported.items).toHaveLength(1)
  expect(exported.items[0].fields.name).toBe('Example login')
  expect('access' in exported.items[0].fields).toBe(false)
  expect(JSON.stringify(exported)).not.toContain('recordMac')
  // The held item keeps its record.
  expect(passwordsSession.getSnapshot().items[0].verifiedAccess).not.toBeNull()
})

// --- round 7: nothing automatic on first sight, memory only from what this device did ---

it('does not re-key by itself on a device with no memory of the item, so a replayed older record turns nothing back on', async () => {
  const base = await recordFor(
    'login-one',
    'login',
    { ...emptyLoginFields(), name: 'Example login', password: 'invented-password-1', access: accessOf([], true) },
    { isUsableByAgent: true },
  )
  const server = serve({ records: [base] })

  // The first device turns the agent off: version 2, agent off.
  await openSession()
  expect(passwordsSession.getSnapshot().items[0].verifiedAccess).toMatchObject({
    recordVersion: 1,
    isUsableByAgent: true,
  })
  await passwordsSession.setUsableByAgent(passwordsSession.getSnapshot().items[0], false)
  expect(passwordsSession.getSnapshot().items[0].verifiedAccess).toMatchObject({
    recordVersion: 2,
    isUsableByAgent: false,
  })
  const firstDeviceMemory = window.localStorage.getItem(MEMORY_NAME)
  expect(JSON.parse(firstDeviceMemory!)['login-one']).toMatchObject({ highestRecordVersion: 2 })
  expect(JSON.parse(firstDeviceMemory!)['login-one'].highestRecordMac).not.toBe('')
  passwordsSession.lock()

  // A browser with nothing stored is shown the item as it was before, then
  // marked. It has no way to know the record is old, so it does nothing by
  // itself and leaves the item for the person.
  window.localStorage.clear()
  server.records = [{ ...base, revision: 500 }]
  await openSession()
  expect(passwordsSession.getSnapshot().items[0].verifiedAccess).toMatchObject({ recordVersion: 1 })
  server.records = [{ ...base, revision: 501, needsNewKey: true }]
  await passwordsSession.refresh()
  vi.mocked(graphql).mock.calls.length = 0
  expect(await passwordsSession.rekeyMarkedItems()).toEqual([])
  expect(await passwordsSession.rekeyIfNeeded('login-one')).toBeNull()
  expect(calls('RekeyPasswordItem')).toHaveLength(0)
  expect(calls('ReadPasswordsAgentKey')).toHaveLength(0)
  expect(passwordsSession.getSnapshot().itemIdsToFinish).toEqual(['login-one'])

  // The person opens it and presses the button: that is their action, and the
  // record is signed one above the highest version this device was shown.
  await passwordsSession.finishSharing('login-one')
  const rekey = (calls('RekeyPasswordItem')[0][1] as { input: any }).input
  expect(await fieldsSentIn(rekey, 'login-one')).toMatchObject({ access: { recordVersion: 2, isUsableByAgent: true } })
  expect(passwordsSession.getSnapshot().itemIdsToFinish).toEqual([])
  passwordsSession.lock()

  // The first device comes back with its memory: the other device's version 2
  // has the same version and another signature, which is a replay.
  window.localStorage.clear()
  window.localStorage.setItem(MEMORY_NAME, firstDeviceMemory!)
  await openSession()
  expect(passwordsSession.getSnapshot().items[0].accessProblem).toBe('replayed')
})

it('notices a record with the remembered version and another signature as a replay', async () => {
  serve({ records: await onlyExampleLogin() })
  await openSession()
  expect(passwordsSession.getSnapshot().items[0].accessProblem).toBeNull()
  const remembered = deviceMemoryOf('login-one')!
  expect(remembered.highestRecordVersion).toBe(1)
  expect(remembered.highestRecordMac).not.toBe('')
  passwordsSession.lock()

  // Version 1 again, signed by the same account key for other people.
  const other = await recordFor('login-one', 'login', {
    ...emptyLoginFields(),
    name: 'Example login',
    access: await (
      await withSignedAccess(
        fixture.accountKey,
        'login-one',
        'login',
        emptyLoginFields(),
        accessOf([{ userId: 'person-b' }]),
        0,
      )
    ).access,
  })
  serve({ records: [other] })
  await openSession()
  expect(passwordsSession.getSnapshot().items[0].accessProblem).toBe('replayed')
})

it('gives a device that never saw an item before no key for people the owner removed', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  knowKeyOnDevice('person-b', personBPublicKey)
  const base = await recordFor(
    'login-one',
    'login',
    { ...emptyLoginFields(), name: 'Example login', access: accessOf([{ userId: 'person-b' }], false) },
    { shares: sharedWith([{ userId: 'person-b', name: 'Person B', publicKey: personBPublicKey, hasKey: true }]) },
  )
  const server = serve({ records: [base], recipients: [personBRecipient(personBPublicKey, true)] })
  await openSession()
  // The owner takes Person B off on this device.
  await passwordsSession.shareItem(passwordsSession.getSnapshot().items[0], [])
  expect(passwordsSession.getSnapshot().items[0].verifiedAccess).toMatchObject({ grantees: [] })
  passwordsSession.lock()

  // A browser with nothing stored is shown the record from before the removal,
  // then the mark: it re-keys nothing by itself.
  window.localStorage.clear()
  server.records = [{ ...base, revision: 900 }]
  await openSession()
  expect(passwordsSession.getSnapshot().items[0].verifiedAccess?.grantees).toEqual([{ userId: 'person-b' }])
  server.records = [{ ...base, revision: 901, needsNewKey: true }]
  await passwordsSession.refresh()
  vi.mocked(graphql).mock.calls.length = 0
  await passwordsSession.rekeyMarkedItems()
  expect(calls('RekeyPasswordItem')).toHaveLength(0)
  expect(passwordsSession.getSnapshot().itemIdsToFinish).toEqual(['login-one'])
})

it('seals nobody and no agent copy in an automatic new key with no seal of its own, and the person sealing gives both', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  knowKeyOnDevice('person-b', personBPublicKey)
  const record = await recordFor(
    'login-one',
    'login',
    { ...emptyLoginFields(), name: 'Example login', access: accessOf([{ userId: 'person-b' }], true) },
    {
      isUsableByAgent: true,
      needsNewKey: true,
      shares: sharedWith([{ userId: 'person-b', name: 'Person B', publicKey: personBPublicKey, hasKey: false }]),
    },
  )
  serve({ records: [record], recipients: [personBRecipient(personBPublicKey, false)] })
  // An earlier session saw the item, but never sealed it for anybody.
  rememberOnDevice('login-one')
  await openSession()
  await passwordsSession.rekeyMarkedItems()
  const automatic = (calls('RekeyPasswordItem')[0][1] as { input: any }).input
  expect(automatic.shareKeys).toEqual([])
  expect(automatic.itemKeyByAgentKey).toBeNull()
  expect(await fieldsSentIn(automatic, 'login-one')).toMatchObject({ access: { recordVersion: 2 } })
  // Person B waits, and the record lets the agent in but it has no copy.
  expect(passwordsSession.getSnapshot().itemIdsToFinish).toEqual(['login-one'])

  // The person saves sharing on a marked item. Their own action seals for
  // the people they chose, but the new key is the automatic one: this device
  // sealed for nobody, so the re-key seals for nobody and the keys are sent
  // with the sharing instead.
  const server = serve({
    records: [{ ...record, revision: 700 }],
    recipients: [personBRecipient(personBPublicKey, false)],
  })
  passwordsSession.lock()
  window.localStorage.clear()
  knowKeyOnDevice('person-b', personBPublicKey)
  rememberOnDevice('login-one')
  await openSession()
  vi.mocked(graphql).mock.calls.length = 0
  const target = {
    grantee: { granteeUserId: 'person-b', granteeCircleId: null },
    recipients: [personBRecipient(personBPublicKey, false)],
  }
  await passwordsSession.shareItem(passwordsSession.getSnapshot().items[0], [target])
  const viaShare = (calls('RekeyPasswordItem')[0][1] as { input: any }).input
  expect(viaShare.shareKeys).toEqual([])
  expect(viaShare.itemKeyByAgentKey).toBeNull()
  const sharesInput = (calls('SetPasswordItemShares')[0][1] as { input: any }).input
  expect(sharesInput.keys.map((key: { recipientUserId: string }) => key.recipientUserId)).toEqual(['person-b'])
  expect(server.records[0].needsNewKey).toBe(false)
})

it('shares a marked item with the key it has on a device with no memory of it, and leaves the new key to Finish sharing', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  knowKeyOnDevice('person-b', personBPublicKey)
  const record = await recordFor(
    'login-one',
    'login',
    { ...emptyLoginFields(), name: 'Example login', access: accessOf([{ userId: 'person-b' }], true) },
    {
      isUsableByAgent: true,
      needsNewKey: true,
      shares: sharedWith([{ userId: 'person-b', name: 'Person B', publicKey: personBPublicKey, hasKey: true }]),
    },
  )
  const server = serve({ records: [record], recipients: [personBRecipient(personBPublicKey, true)] })
  await openSession()
  vi.mocked(graphql).mock.calls.length = 0
  const keyBefore = encodeBase64(passwordsSession.getSnapshot().items[0].itemKey as Uint8Array)
  // The person saves a field, and then the sharing as it is.
  await passwordsSession.saveItem(
    {
      itemKind: 'login',
      fields: { ...passwordsSession.getSnapshot().items[0].fields, password: 'invented-new-password' } as never,
    },
    passwordsSession.getSnapshot().items[0],
  )
  const target = {
    grantee: { granteeUserId: 'person-b', granteeCircleId: null },
    recipients: [personBRecipient(personBPublicKey, true)],
  }
  await passwordsSession.shareItem(passwordsSession.getSnapshot().items[0], [target])
  // No new key was made by itself, and nobody was sealed for from the old record.
  expect(calls('RekeyPasswordItem')).toHaveLength(0)
  expect(encodeBase64(passwordsSession.getSnapshot().items[0].itemKey as Uint8Array)).toBe(keyBefore)
  expect(server.records[0].needsNewKey).toBe(true)
  expect(passwordsSession.getSnapshot().items[0].fields).toMatchObject({ password: 'invented-new-password' })
})

it('turns the agent on for a marked item with the verified record, one version above the highest remembered', async () => {
  const record = await recordFor(
    'login-one',
    'login',
    { ...emptyLoginFields(), name: 'Example login', access: accessOf([], false) },
    { needsNewKey: true },
  )
  serve({ records: [record] })
  rememberOnDevice('login-one')
  await openSession()
  await passwordsSession.setUsableByAgent(passwordsSession.getSnapshot().items[0], true)
  const rekey = (calls('RekeyPasswordItem')[0][1] as { input: any }).input
  expect(await fieldsSentIn(rekey, 'login-one')).toMatchObject({ access: { recordVersion: 2, isUsableByAgent: false } })
  const saves = calls('SavePasswordItem')
  const lastSave = (saves[saves.length - 1][1] as { input: any }).input
  expect(await fieldsSentIn(lastSave, 'login-one')).toMatchObject({
    access: { recordVersion: 3, isUsableByAgent: true },
  })
  expect(deviceMemoryOf('login-one')).toMatchObject({ highestRecordVersion: 3, hasMadeAgentCopy: true })
})

it('turns the agent on for a marked item with the key it has on a device with no memory of it', async () => {
  const record = await recordFor(
    'login-one',
    'login',
    { ...emptyLoginFields(), name: 'Example login', access: accessOf([], false) },
    { needsNewKey: true },
  )
  serve({ records: [record] })
  await openSession()
  await passwordsSession.setUsableByAgent(passwordsSession.getSnapshot().items[0], true)
  // No automatic new key on first sight: the agent is given the key the item
  // has, and the new key waits for Finish sharing.
  expect(calls('RekeyPasswordItem')).toHaveLength(0)
  const saves = calls('SavePasswordItem')
  expect(await fieldsSentIn((saves[saves.length - 1][1] as { input: any }).input, 'login-one')).toMatchObject({
    access: { recordVersion: 2, isUsableByAgent: true },
  })
  expect(deviceMemoryOf('login-one')).toMatchObject({ highestRecordVersion: 2, hasMadeAgentCopy: true })
  expect(passwordsSession.getSnapshot().items[0].needsNewKey).toBe(true)
})

it('signs a new record one above the highest version remembered when the record on the server could not be checked', async () => {
  rememberOnDevice('login-one', { highestRecordVersion: 5 })
  serve({
    records: [
      await recordFor(
        'login-one',
        'login',
        { ...emptyLoginFields(), name: 'Example login' },
        {},
        { isAccessSigned: false },
      ),
    ],
  })
  await openSession()
  const item = passwordsSession.getSnapshot().items[0]
  expect(item.accessProblem).toBe('notVerified')
  await passwordsSession.shareItem(item, [])
  const saves = calls('SavePasswordItem')
  expect(await fieldsSentIn((saves[0][1] as { input: any }).input, 'login-one')).toMatchObject({
    access: { recordVersion: 6 },
  })
})

it('gives the item a new key when the agent is turned on for a record that could not be checked', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  knowKeyOnDevice('person-b', personBPublicKey)
  const server = serve({
    records: [
      await recordFor(
        'login-one',
        'login',
        { ...emptyLoginFields(), name: 'Example login', access: accessOf([{ userId: 'person-b' }], false) },
        { shares: sharedWith([{ userId: 'person-b', name: 'Person B', publicKey: personBPublicKey, hasKey: true }]) },
        { isAccessSigned: false },
      ),
    ],
  })
  await openSession()
  const item = passwordsSession.getSnapshot().items[0]
  expect(item.accessProblem).toBe('notVerified')
  const keyBefore = encodeBase64(item.itemKey as Uint8Array)
  await passwordsSession.setUsableByAgent(item, true)
  expect(calls('RekeyPasswordItem')).toHaveLength(1)
  expect(encodeBase64(passwordsSession.getSnapshot().items[0].itemKey as Uint8Array)).not.toBe(keyBefore)
  // The fresh record grants nobody, so Person B has no key for the new item key.
  expect(server.records[0].shares[0].recipients[0].hasKey).toBe(false)
  expect(passwordsSession.getSnapshot().items[0].verifiedAccess).toMatchObject({ isUsableByAgent: true, grantees: [] })
})

it('gives the item a new key after setting sharing again when the server lists shares or agent use', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  knowKeyOnDevice('person-b', personBPublicKey)
  const target = {
    grantee: { granteeUserId: 'person-b', granteeCircleId: null },
    recipients: [personBRecipient(personBPublicKey, true)],
  }
  // The server lists a share, and sharing is set again with the same person.
  serve({
    recipients: [personBRecipient(personBPublicKey, true)],
    records: [
      await recordFor(
        'login-one',
        'login',
        { ...emptyLoginFields(), name: 'Example login', access: accessOf([{ userId: 'person-b' }], false) },
        { shares: sharedWith([{ userId: 'person-b', name: 'Person B', publicKey: personBPublicKey, hasKey: true }]) },
        { isAccessSigned: false },
      ),
    ],
  })
  await openSession()
  const result = await passwordsSession.shareItem(passwordsSession.getSnapshot().items[0], [target])
  expect(result.hasRekeyFailed).toBe(false)
  expect(calls('RekeyPasswordItem')).toHaveLength(1)
  const rekey = (calls('RekeyPasswordItem')[0][1] as { input: any }).input
  expect(rekey.shareKeys.map((key: { recipientUserId: string }) => key.recipientUserId)).toEqual(['person-b'])
  passwordsSession.lock()

  // Only agent use is listed.
  serve({
    records: [
      await recordFor(
        'login-one',
        'login',
        { ...emptyLoginFields(), name: 'Example login', access: accessOf([], true) },
        { isUsableByAgent: true },
        { isAccessSigned: false },
      ),
    ],
  })
  window.localStorage.clear()
  await openSession()
  vi.mocked(graphql).mock.calls.length = 0
  await passwordsSession.shareItem(passwordsSession.getSnapshot().items[0], [])
  expect(calls('RekeyPasswordItem')).toHaveLength(1)
  passwordsSession.lock()

  // Nothing listed: nothing to replace.
  serve({
    records: [
      await recordFor(
        'login-one',
        'login',
        { ...emptyLoginFields(), name: 'Example login' },
        {},
        { isAccessSigned: false },
      ),
    ],
  })
  window.localStorage.clear()
  await openSession()
  vi.mocked(graphql).mock.calls.length = 0
  await passwordsSession.shareItem(passwordsSession.getSnapshot().items[0], [])
  expect(calls('RekeyPasswordItem')).toHaveLength(0)
})

it('saves a marked item with the key it has when automatic new keys are limited, and Finish sharing gives the new key', async () => {
  const marked = await recordFor(
    'login-one',
    'login',
    { ...emptyLoginFields(), name: 'Example login' },
    { needsNewKey: true },
  )
  const server = serve({ records: [marked] })
  rememberOnDevice('login-one')
  const now = vi.spyOn(Date, 'now')
  try {
    now.mockReturnValue(1_000_000)
    await openSession()
    await passwordsSession.rekeyIfNeeded('login-one')
    expect(calls('RekeyPasswordItem')).toHaveLength(1)

    // Marked again straight away: the automatic one is refused for ten minutes.
    server.records = [{ ...server.records[0], needsNewKey: true, revision: server.records[0].revision + 10 }]
    await passwordsSession.refresh()
    now.mockReturnValue(1_000_000 + 60_000)
    await expect(passwordsSession.rekeyIfNeeded('login-one')).rejects.toThrow()
    expect(calls('RekeyPasswordItem')).toHaveLength(1)

    // The save is tried as an automatic new key too: it is refused for now,
    // so the words are saved with the key the item has.
    const held = passwordsSession.getSnapshot().items[0]
    await passwordsSession.saveItem(
      { itemKind: 'login', fields: { ...held.fields, name: 'Renamed login' } as never },
      held,
    )
    expect(calls('RekeyPasswordItem')).toHaveLength(1)
    expect(passwordsSession.getSnapshot().items[0].fields.name).toBe('Renamed login')
    expect(server.records[0].needsNewKey).toBe(true)

    // The person's Finish sharing gives the new key and keeps their words.
    await passwordsSession.finishSharing('login-one')
    expect(calls('RekeyPasswordItem')).toHaveLength(2)
    const rekey = (calls('RekeyPasswordItem')[1][1] as { input: any }).input
    expect(await fieldsSentIn(rekey, 'login-one')).toMatchObject({ name: 'Renamed login' })
    expect(server.records[0].needsNewKey).toBe(false)
  } finally {
    now.mockRestore()
  }
})

it('leaves the people this device never sealed for to the person, who finishes the sharing from the item', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  const record = await recordFor(
    'login-one',
    'login',
    { ...emptyLoginFields(), name: 'Example login', access: accessOf([{ userId: 'person-b' }]) },
    { shares: sharedWith([{ userId: 'person-b', name: 'Person B', publicKey: personBPublicKey, hasKey: false }]) },
  )
  serve({ records: [record], waiting: [waitingFor('login-one', personBPublicKey)] })
  // An earlier session saw the item, but never sealed it for Person B.
  rememberOnDevice('login-one')
  await openSession()

  // Nothing is sealed by itself.
  expect((await passwordsSession.completeWaitingShares()).storedKeyCount).toBe(0)
  expect(calls('AddPasswordShareKeys')).toHaveLength(0)
  expect(passwordsSession.getSnapshot().itemIdsToFinish).toEqual(['login-one'])

  // The list says so on the item, and opening it shows the button.
  const view = renderAt('/passwords')
  expect(await screen.findByText('Open this item to finish sharing it')).toBeTruthy()
  view.unmount()
  renderAt('/passwords?item=login-one')
  fireEvent.click(await screen.findByRole('button', { name: 'Finish sharing' }))
  await waitFor(() => expect(calls('AddPasswordShareKeys')).toHaveLength(1))
  const keys = (calls('AddPasswordShareKeys')[0][1] as { keys: Record<string, string>[] }).keys
  expect(keys[0]).toMatchObject({
    itemId: 'login-one',
    recipientUserId: 'person-b',
    recipientPublicKey: personBPublicKey,
  })
  await waitFor(() => expect(screen.queryByRole('button', { name: 'Finish sharing' })).toBeNull())
  expect(deviceMemoryOf('login-one')?.sealedForUserIds).toEqual(['person-b'])
  expect(passwordsSession.getSnapshot().itemIdsToFinish).toEqual([])
})

it('tells the owner who got an item through a circle when the server adds a member this device never sealed for', async () => {
  const evilPublicKey = encodeBase64(personC.publicKey)
  const record = await recordFor(
    'login-one',
    'login',
    { ...emptyLoginFields(), name: 'Example login', access: accessOf([{ circleId: 'circle-one' }], false) },
    {
      shares: [
        {
          id: 'share-circle',
          granteeUserId: null,
          granteeCircleId: 'circle-one',
          granteeName: 'Fixture Family',
          recipients: [
            // Somebody the server put in the circle, with a key of its choosing.
            {
              userId: 'person-added',
              name: 'Person D',
              username: 'person-d-login',
              publicKey: evilPublicKey,
              hasKey: false,
              hasTooMuchShared: false,
            },
          ],
        },
      ],
    },
  )
  serve({
    records: [record],
    waiting: [
      {
        itemId: 'login-one',
        recipient: {
          userId: 'person-added',
          name: 'Person D',
          username: 'person-d-login',
          publicKey: evilPublicKey,
          hasKey: false,
          hasTooMuchShared: false,
        },
      },
    ],
  })
  rememberOnDevice('login-one')
  await openSession()
  // By itself the device seals nothing for a member it never sealed for.
  expect((await passwordsSession.completeWaitingShares()).storedKeyCount).toBe(0)
  expect(passwordsSession.getSnapshot().circleShareNotices).toEqual([])

  renderAt('/passwords?item=login-one')
  fireEvent.click(await screen.findByRole('button', { name: 'Finish sharing' }))
  expect(await screen.findByText('Person D got Example login through Fixture Family.')).toBeTruthy()
  expect(passwordsSession.getSnapshot().circleShareNotices).toHaveLength(1)
  fireEvent.click(screen.getByRole('button', { name: 'Dismiss' }))
  await waitFor(() => expect(screen.queryByText('Person D got Example login through Fixture Family.')).toBeNull())
  expect(passwordsSession.getSnapshot().circleShareNotices).toEqual([])
})

it('names a circle member in a notice when the owner shares with the circle, and not a person chosen by name', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  serve({ records: await onlyExampleLogin(), recipients: [personBRecipient(personBPublicKey)] })
  await openSession()
  renderAt('/passwords?item=login-one')

  const dialog = await openShareWithCircle()
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save sharing' }))
  expect(await screen.findByText('Person B got Example login through Fixture Family.')).toBeTruthy()
})

it('keeps the known keys and the item memories when Passwords is locked as the person signs out', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  serve({ records: [...(await recordsToList())] })
  await openSession()
  expect(storedKeys()).toEqual({ 'person-b': personBPublicKey })
  const memoryBefore = window.localStorage.getItem(MEMORY_NAME)
  expect(memoryBefore).not.toBeNull()

  lockPasswords()
  expect(passwordsSession.isUnlocked).toBe(false)
  expect(storedKeys()).toEqual({ 'person-b': personBPublicKey })
  expect(window.localStorage.getItem(MEMORY_NAME)).toBe(memoryBefore)
})

it('does not read who held a key or whether the agent had a copy from what the server lists', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  serve({
    records: [
      await recordFor(
        'login-one',
        'login',
        { ...emptyLoginFields(), name: 'Example login', access: accessOf([{ userId: 'person-b' }], true) },
        {
          isUsableByAgent: true,
          shares: sharedWith([{ userId: 'person-b', name: 'Person B', publicKey: personBPublicKey, hasKey: true }]),
        },
      ),
    ],
  })
  await openSession()
  expect(deviceMemoryOf('login-one')).toMatchObject({ sealedForUserIds: [], hasMadeAgentCopy: false })
})

it('remembers a person the owner named here, so their waiting share completes by itself, and not a circle member', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  // Person B has not set up Passwords yet: no key can be sealed now.
  const server = serve({ records: await onlyExampleLogin(), recipients: [personBRecipient(null)] })
  await openSession()
  await passwordsSession.shareItem(passwordsSession.getSnapshot().items[0], [
    { grantee: { granteeUserId: 'person-b', granteeCircleId: null }, recipients: [personBRecipient(null)] },
  ])
  expect(calls('AddPasswordShareKeys')).toHaveLength(0)
  expect(deviceMemoryOf('login-one')?.sealedForUserIds).toEqual(['person-b'])
  passwordsSession.lock()

  // Later Person B sets up Passwords: the share completes with no one pressing anything.
  server.recipients = [personBRecipient(personBPublicKey)]
  server.records = server.records.map((record) => ({
    ...record,
    shares: record.shares.map((share) => ({ ...share, recipients: [personBRecipient(personBPublicKey)] })),
  }))
  server.waiting = [waitingFor('login-one', personBPublicKey)]
  await openSession()
  expect(passwordsSession.getSnapshot().itemIdsToFinish).toEqual([])
  expect((await passwordsSession.completeWaitingShares()).storedKeyCount).toBe(1)
  expect(calls('AddPasswordShareKeys')).toHaveLength(1)
})

it('keeps a person the owner named here remembered through a new key, and leaves circle members to the person', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  serve({ records: await onlyExampleLogin(), recipients: [personBRecipient(null)] })
  await openSession()
  const target = { grantee: { granteeUserId: 'person-b', granteeCircleId: null }, recipients: [personBRecipient(null)] }
  await passwordsSession.shareItem(passwordsSession.getSnapshot().items[0], [target])
  await passwordsSession.giveNewKey('login-one')
  expect(deviceMemoryOf('login-one')?.sealedForUserIds).toEqual(['person-b'])

  // A circle: nobody is remembered, even one who has a key sealed now.
  passwordsSession.lock()
  window.localStorage.clear()
  serve({ records: await onlyExampleLogin(), recipients: [personBRecipient(personBPublicKey)] })
  await openSession()
  await passwordsSession.shareItem(passwordsSession.getSnapshot().items[0], [
    { grantee: { granteeUserId: null, granteeCircleId: 'circle-one' }, recipients: [personBRecipient(null)] },
  ])
  expect(deviceMemoryOf('login-one')?.sealedForUserIds).toEqual([])
})

// --- the eighth audit: a device with no memory, the chosen record, memory by item ---

// oldRecordOnFreshBrowser opens an item as it was before the owner took
// Person B off and turned the agent off on this device, on a browser with
// nothing stored, with Person B listed holding a key.
async function oldRecordOnFreshBrowser() {
  const personBPublicKey = encodeBase64(personB.publicKey)
  knowKeyOnDevice('person-b', personBPublicKey)
  const base = await recordFor(
    'login-one',
    'login',
    {
      ...emptyLoginFields(),
      name: 'Example login',
      password: 'invented-password-1',
      access: accessOf([{ userId: 'person-b' }], true),
    },
    {
      isUsableByAgent: true,
      shares: sharedWith([{ userId: 'person-b', name: 'Person B', publicKey: personBPublicKey, hasKey: true }]),
    },
  )
  const server = serve({ records: [base], recipients: [personBRecipient(personBPublicKey, true)] })
  await openSession()
  await passwordsSession.shareItem(passwordsSession.getSnapshot().items[0], [])
  await passwordsSession.setUsableByAgent(passwordsSession.getSnapshot().items[0], false)
  passwordsSession.lock()
  window.localStorage.clear()
  return { base, server, personBPublicKey }
}

it('saves an edit of a marked item with the key it has when an old record is shown to a device with no memory', async () => {
  const { base, server } = await oldRecordOnFreshBrowser()
  server.records = [{ ...base, revision: 900, needsNewKey: true }]
  await openSession()
  const seen = passwordsSession.getSnapshot().items[0]
  expect(seen.accessProblem).toBeNull()
  expect(seen.verifiedAccess?.grantees).toEqual([{ userId: 'person-b' }])
  const keyBefore = encodeBase64(seen.itemKey as Uint8Array)
  vi.mocked(graphql).mock.calls.length = 0
  await passwordsSession.saveItem(
    { itemKind: 'login', fields: { ...(seen.fields as object), password: 'invented-password-2' } as never },
    seen,
  )
  // Nothing was sealed for the person the owner removed, and no new key was made.
  expect(calls('RekeyPasswordItem')).toHaveLength(0)
  const saved = (calls('SavePasswordItem')[0][1] as { input: any }).input
  const written = await fieldsSentIn(saved, 'login-one')
  expect(written.password).toBe('invented-password-2')
  // The record was written as it was, not signed again as a newer one.
  expect(pickSignature(written.access)).toEqual(pickSignature(seen.verifiedAccess))
  expect(written.access.recordVersion).toBe(1)
  expect(encodeBase64(passwordsSession.getSnapshot().items[0].itemKey as Uint8Array)).toBe(keyBefore)
  // The new key waits for the person's Finish sharing.
  expect(passwordsSession.getSnapshot().items[0].needsNewKey).toBe(true)
  expect(passwordsSession.getSnapshot().itemIdsToFinish).toEqual(['login-one'])
})

it('does not write an old record forward as a newer version when only the fields are saved', async () => {
  const { base, server } = await oldRecordOnFreshBrowser()
  server.records = [{ ...base, revision: 900 }]
  await openSession()
  const seen = passwordsSession.getSnapshot().items[0]
  expect(seen.verifiedAccess).toMatchObject({ recordVersion: 1, isUsableByAgent: true })
  vi.mocked(graphql).mock.calls.length = 0
  await passwordsSession.saveItem(
    { itemKind: 'login', fields: { ...(seen.fields as object), password: 'invented-password-2' } as never },
    seen,
  )
  const written = await fieldsSentIn((calls('SavePasswordItem')[0][1] as { input: any }).input, 'login-one')
  expect(written.access).toMatchObject({ isUsableByAgent: true, recordVersion: 1 })
  expect(written.access.recordMac).toBe(seen.verifiedAccess?.recordMac)
  expect(written.access.grantees).toEqual([{ userId: 'person-b' }])
})

it('refuses a sharing change the server did not keep, and seals nothing for the people it removed', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  knowKeyOnDevice('person-b', personBPublicKey)
  const base = await recordFor(
    'login-one',
    'login',
    {
      ...emptyLoginFields(),
      name: 'Example login',
      password: 'invented-password-1',
      access: accessOf([{ userId: 'person-b' }], false),
    },
    { shares: sharedWith([{ userId: 'person-b', name: 'Person B', publicKey: personBPublicKey, hasKey: true }]) },
  )
  const server = serve({ records: [base], recipients: [personBRecipient(personBPublicKey, true)] })
  await openSession()
  const baseImplementation = vi.mocked(graphql).getMockImplementation()!
  vi.mocked(graphql).mockImplementation(async (document, variables) => {
    // The server saves nothing and answers with the record it already had.
    if (document.includes('SavePasswordItem')) return { SavePasswordItem: server.records[0] } as never
    return baseImplementation(document, variables)
  })
  vi.mocked(graphql).mock.calls.length = 0
  await expect(passwordsSession.shareItem(passwordsSession.getSnapshot().items[0], [])).rejects.toMatchObject({
    code: 'sharingNotKept',
  })
  expect(calls('SetPasswordItemShares')).toHaveLength(0)
  expect(calls('RekeyPasswordItem')).toHaveLength(0)
  expect(failureText(new PasswordsAccessError('sharingNotKept'), (key) => key, 'fallback')).toBe(
    'passwords.errorSharingNotKept',
  )
})

it('re-keys a removal from the people the person chose, not from the shares the server answers with', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  knowKeyOnDevice('person-b', personBPublicKey)
  const base = await recordFor(
    'login-one',
    'login',
    {
      ...emptyLoginFields(),
      name: 'Example login',
      password: 'invented-password-1',
      access: accessOf([{ userId: 'person-b' }], false),
    },
    { shares: sharedWith([{ userId: 'person-b', name: 'Person B', publicKey: personBPublicKey, hasKey: true }]) },
  )
  const server = serve({ records: [base], recipients: [personBRecipient(personBPublicKey, true)] })
  await openSession()
  const baseImplementation = vi.mocked(graphql).getMockImplementation()!
  vi.mocked(graphql).mockImplementation(async (document, variables) => {
    if (document.includes('SetPasswordItemShares')) {
      // The server marks the item, as an honest one does, but still lists
      // Person B with a key.
      server.records = [{ ...server.records[0], needsNewKey: true, revision: server.records[0].revision + 10 }]
      return { SetPasswordItemShares: server.records[0] } as never
    }
    return baseImplementation(document, variables)
  })
  vi.mocked(graphql).mock.calls.length = 0
  const result = await passwordsSession.shareItem(passwordsSession.getSnapshot().items[0], [])
  expect(result.hasRekeyFailed).toBe(false)
  const rekeys = calls('RekeyPasswordItem')
  expect(rekeys).toHaveLength(1)
  const input = (rekeys[0][1] as { input: any }).input
  expect(input.shareKeys).toEqual([])
  const written = await fieldsSentIn(input, 'login-one')
  expect(written.access.grantees).toEqual([])
})

it('keeps the memory of an item under its id, whoever the server says is signed in', async () => {
  const base = await recordFor('login-one', 'login', { ...emptyLoginFields(), name: 'Example login' })
  const server = serve({ records: [base] })
  await openSession()
  const item = passwordsSession.getSnapshot().items[0]
  // A change of sharing signs version 2.
  await passwordsSession.shareItem(item, [
    { grantee: { granteeUserId: 'person-b', granteeCircleId: null }, recipients: [personBRecipient(null)] },
  ])
  const versionAfter = passwordsSession.getSnapshot().items[0].verifiedAccess?.recordVersion
  expect(versionAfter).toBeGreaterThan(1)
  passwordsSession.lock()
  // The server shows the record from before, and names another user id.
  server.records = [{ ...base, revision: 500 }]
  await passwordsSession.unlock('person-a-renamed', { secret, isRecoveryKey: false })
  expect(passwordsSession.getSnapshot().items[0].accessProblem).toBe('replayed')
  passwordsSession.lock()
  await passwordsSession.unlock(USER_ID, { secret, isRecoveryKey: false })
  expect(passwordsSession.getSnapshot().items[0].accessProblem).toBe('replayed')
  // One store, by item id only.
  expect(Object.keys(JSON.parse(window.localStorage.getItem(MEMORY_NAME)!))).toEqual(['login-one'])
})

it('moves the memories earlier versions kept for each signed-in person into the one store', async () => {
  const base = await recordFor('login-one', 'login', { ...emptyLoginFields(), name: 'Example login' })
  const olderMac = 'older-mac'
  window.localStorage.setItem(
    `teanode.passwords.access:${USER_ID}`,
    JSON.stringify({
      'login-one': {
        highestRecordVersion: 3,
        highestRecordMac: 'mac-three',
        sealedForUserIds: ['person-b'],
        hasMadeAgentCopy: false,
      },
    }),
  )
  window.localStorage.setItem(
    'teanode.passwords.access:person-a-earlier',
    JSON.stringify({
      'login-one': {
        highestRecordVersion: 2,
        highestRecordMac: olderMac,
        sealedForUserIds: ['person-c'],
        hasMadeAgentCopy: true,
      },
      'login-two': { highestRecordVersion: 1, highestRecordMac: '', sealedForUserIds: [], hasMadeAgentCopy: false },
    }),
  )
  serve({ records: [base] })
  await openSession()
  // The record on the server is version 1, below the remembered 3.
  expect(passwordsSession.getSnapshot().items[0].accessProblem).toBe('replayed')
  expect(window.localStorage.getItem(`teanode.passwords.access:${USER_ID}`)).toBeNull()
  expect(window.localStorage.getItem('teanode.passwords.access:person-a-earlier')).toBeNull()
  const kept = JSON.parse(window.localStorage.getItem(MEMORY_NAME)!)
  expect(kept['login-one']).toEqual({
    highestRecordVersion: 3,
    highestRecordMac: 'mac-three',
    sealedForUserIds: ['person-c', 'person-b'],
    hasMadeAgentCopy: true,
  })
  expect(kept['login-two'].highestRecordVersion).toBe(1)
})

it('finishes the sharing of every waiting item with one tap, and says how many', async () => {
  const markedFields = (name: string) => ({ ...emptyLoginFields(), name })
  const server = serve({
    records: [
      await recordFor('login-one', 'login', markedFields('First login'), { needsNewKey: true }),
      await recordFor('login-two', 'login', markedFields('Second login'), { needsNewKey: true }),
      await recordFor('login-three', 'login', markedFields('Third login')),
    ],
  })
  await openSession()
  expect(passwordsSession.getSnapshot().itemIdsToFinish.sort()).toEqual(['login-one', 'login-two'])
  renderAt('/passwords')
  fireEvent.click(await screen.findByRole('button', { name: 'Finish sharing 2 items' }))
  expect(await screen.findByText('Finished sharing 2 items.')).toBeTruthy()
  expect(calls('RekeyPasswordItem')).toHaveLength(2)
  expect(server.records.every((record) => !record.needsNewKey)).toBe(true)
  expect(screen.queryByRole('button', { name: /^Finish sharing/ })).toBeNull()
})

it('shows that a person has too much shared with them, and does not seal for them', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  const full: PasswordRecipient = { ...personBRecipient(personBPublicKey), hasTooMuchShared: true }
  serve({ records: await onlyExampleLogin(), recipients: [full] })
  await openSession()
  renderAt('/passwords?item=login-one')
  const dialog = await openShareWithCircle()
  expect(within(dialog).getByText('Person B has too much shared with them.')).toBeTruthy()
  await passwordsSession.shareItem(passwordsSession.getSnapshot().items[0], [
    { grantee: { granteeUserId: null, granteeCircleId: 'circle-one' }, recipients: [full] },
  ])
  const sharesInput = (calls('SetPasswordItemShares')[0][1] as { input: any }).input
  expect(sharesInput.keys).toEqual([])
})

it('skips a waiting person who has too much shared with them, and counts the keys the server stored', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  const personCPublicKey = encodeBase64(personC.publicKey)
  knowKeyOnDevice('person-b', personBPublicKey)
  knowKeyOnDevice('person-c', personCPublicKey)
  const waitingPerson = (userId: string, publicKey: string, hasTooMuchShared: boolean) => ({
    itemId: 'login-one',
    recipient: {
      userId,
      name: `Person ${userId.slice(-1).toUpperCase()}`,
      username: `${userId}-login`,
      publicKey,
      hasKey: false,
      hasTooMuchShared,
    },
  })
  const record = await recordFor(
    'login-one',
    'login',
    {
      ...emptyLoginFields(),
      name: 'Example login',
      access: accessOf([{ userId: 'person-b' }, { userId: 'person-c' }]),
    },
    {
      shares: [
        {
          id: 'share-b',
          granteeUserId: 'person-b',
          granteeCircleId: null,
          granteeName: 'Person B',
          recipients: [waitingPerson('person-b', personBPublicKey, true).recipient],
        },
        {
          id: 'share-c',
          granteeUserId: 'person-c',
          granteeCircleId: null,
          granteeName: 'Person C',
          recipients: [waitingPerson('person-c', personCPublicKey, false).recipient],
        },
      ],
    },
  )
  serve({
    records: [record],
    waiting: [waitingPerson('person-b', personBPublicKey, true), waitingPerson('person-c', personCPublicKey, false)],
    hasMoreWaiting: true,
  })
  rememberOnDevice('login-one', { sealedForUserIds: ['person-b', 'person-c'] })
  await openSession()
  expect((await passwordsSession.completeWaitingShares()).storedKeyCount).toBe(1)
  const keys = (calls('AddPasswordShareKeys')[0][1] as { keys: { recipientUserId: string }[] }).keys
  expect(keys.map((key) => key.recipientUserId)).toEqual(['person-c'])
  expect(passwordsSession.getSnapshot().hasMoreWaiting).toBe(true)
  // Nothing is left for the person to finish for the full recipient.
  expect(passwordsSession.getSnapshot().itemIdsToFinish).toEqual([])
})

it('uses the number of keys the server stored, not the number asked, for the toast', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  knowKeyOnDevice('person-b', personBPublicKey)
  const record = await recordFor(
    'login-one',
    'login',
    { ...emptyLoginFields(), name: 'Example login', access: accessOf([{ userId: 'person-b' }]) },
    { shares: sharedWith([{ userId: 'person-b', name: 'Person B', publicKey: personBPublicKey, hasKey: false }]) },
  )
  serve({ records: [record], waiting: [waitingFor('login-one', personBPublicKey)], fullUserIds: ['person-b'] })
  rememberOnDevice('login-one', { sealedForUserIds: ['person-b'] })
  await openSession()
  // Asked for one key, the server stored none.
  expect((await passwordsSession.completeWaitingShares()).storedKeyCount).toBe(0)
  expect(calls('AddPasswordShareKeys')).toHaveLength(1)
})

it('says more are waiting when the server lists only some of the waiting shares', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  const record = await recordFor(
    'login-one',
    'login',
    { ...emptyLoginFields(), name: 'Example login', access: accessOf([{ userId: 'person-b' }]) },
    { shares: sharedWith([{ userId: 'person-b', name: 'Person B', publicKey: personBPublicKey, hasKey: false }]) },
  )
  serve({ records: [record], waiting: [waitingFor('login-one', personBPublicKey)], hasMoreWaiting: true })
  await openSession()
  renderAt('/passwords')
  expect(await screen.findByText('More are waiting: finish these first.')).toBeTruthy()
})

it('calls a second record of the same version with another signature a replay', async () => {
  const first = await recordFor('login-one', 'login', {
    ...emptyLoginFields(),
    name: 'Example login',
    access: accessOf([{ userId: 'person-b' }]),
  })
  const secondFields = await withSignedAccess(
    fixture.accountKey,
    'login-one',
    'login',
    { ...emptyLoginFields(), name: 'Example login' },
    accessOf([]),
    0,
  )
  const second = await recordFor('login-one', 'login', secondFields as never)
  expect(pickSignature((secondFields as { access: ItemAccess }).access).recordVersion).toBe(1)
  const server = serve({ records: [first] })
  await openSession()
  expect(passwordsSession.getSnapshot().items[0].accessProblem).toBeNull()
  server.records = [{ ...second, revision: 99 }]
  await passwordsSession.refresh()
  expect(passwordsSession.getSnapshot().items[0].accessProblem).toBe('replayed')
})
