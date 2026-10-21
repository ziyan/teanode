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
import { PasswordsError, failureText } from './passwordsErrors'
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
    if (document.includes('ListWaitingPasswordShares')) return { ListWaitingPasswordShares: server.waiting } as never
    if (document.includes('AddPasswordShareKeys')) {
      // The server refuses a key that names an item key other than the item's
      // current one (contract 4).
      for (const key of asked.keys as { itemId: string; itemKey?: string }[]) {
        const current = server.records.find((record) => record.id === key.itemId)
        if (!current || key.itemKey !== current.itemKey) throw new APIError(STALE_MESSAGE)
      }
      return { AddPasswordShareKeys: asked.keys.length } as never
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
    if (document.includes('ListPasswordRecipients')) return { ListPasswordRecipients: server.recipients } as never
    if (document.includes('SetPasswordItemShares')) {
      const input = asked.input
      const existing = server.records.find((record) => record.id === input.itemId)!
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
async function recordFor(
  id: string,
  itemKind: ItemKind,
  fields: object,
  overrides: Partial<PasswordItemRecord> = {},
): Promise<PasswordItemRecord> {
  const sealed = await sealPasswordItem(fixture.accountKey, id, itemKind, fields as never, null)
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

// accessOf is the access record an owner's device seals inside the fields.
function accessOf(grantees: ItemGrantee[], isUsableByAgent = false): ItemAccess {
  return { isUsableByAgent, grantees }
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

// confirmKeyOnDevice writes what a confirm leaves in localStorage: the key is
// both the confirmed key and the seen one.
function confirmKeyOnDevice(otherUserId: string, publicKey: string) {
  for (const kind of ['pins', 'confirmedKeys']) {
    const name = `teanode.passwords.${kind}:${USER_ID}`
    const kept = JSON.parse(window.localStorage.getItem(name) ?? '{}')
    window.localStorage.setItem(name, JSON.stringify({ ...kept, [otherUserId]: publicKey }))
  }
}

function storedKeys(kind: 'pins' | 'confirmedKeys'): Record<string, string> | null {
  const raw = window.localStorage.getItem(`teanode.passwords.${kind}:${USER_ID}`)
  return raw === null ? null : JSON.parse(raw)
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

it("keeps the owner's key the first time a shared item opens with it", async () => {
  serve({ records: await recordsToList() })
  await openSession()
  expect(JSON.parse(window.localStorage.getItem(`teanode.passwords.pins:${USER_ID}`) ?? '{}')).toEqual({
    'person-b': encodeBase64(personB.publicKey),
  })
})

it('does not keep the key of an owner whose share did not open', async () => {
  const stranger = await generatePersonKeyPair()
  // The server names a key for Person B that did not seal the share.
  serve({
    records: [await sharedRecordFor('login-forged', { ...emptyLoginFields(), name: 'Forged' }, stranger, personB)],
  })
  await openSession()
  expect(window.localStorage.getItem(`teanode.passwords.pins:${USER_ID}`)).toBeNull()
  expect(passwordsSession.getSnapshot().items[0].isUnreadable).toBe(true)
})

it('shows a shared item whose owner key changed as closed, with both safety codes, until it is confirmed', async () => {
  const oldKeyPair = await generatePersonKeyPair()
  window.localStorage.setItem(
    `teanode.passwords.pins:${USER_ID}`,
    JSON.stringify({ 'person-b': encodeBase64(oldKeyPair.publicKey) }),
  )
  serve({ records: await recordsToList() })
  await openSession()
  renderAt('/passwords?item=login-shared')

  expect(await screen.findByText("Its owner's key changed")).toBeTruthy()
  expect(await screen.findByText(await safetyCode('person-b', oldKeyPair.publicKey))).toBeTruthy()
  expect(await screen.findByText(await safetyCode('person-b', personB.publicKey))).toBeTruthy()
  expect(screen.queryByRole('button', { name: 'Show Password' })).toBeNull()
  expect(passwordsSession.getSnapshot().items.find((item) => item.id === 'login-shared')?.itemKey).toBeNull()
  // The words say how to compare, and that confirming also lets the person share with them.
  expect(
    screen.getByText(
      'Ask Person B to open Passwords settings on their own device and read you their safety code. Confirm only if it matches.',
    ),
  ).toBeTruthy()
  expect(screen.getByText(/Confirming it also lets you share with them/)).toBeTruthy()
  expect(screen.getByText(/kept on this browser only/)).toBeTruthy()

  fireEvent.click(screen.getByRole('button', { name: 'The new safety code matches' }))
  fireEvent.click(await screen.findByRole('button', { name: 'Show Password' }))
  expect(screen.getByText('invented-password-4')).toBeTruthy()
  expect(JSON.parse(window.localStorage.getItem(`teanode.passwords.pins:${USER_ID}`) ?? '{}')).toEqual({
    'person-b': encodeBase64(personB.publicKey),
  })
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
    { userId: 'person-b', name: 'Person B', username: 'person-b-login', publicKey: personBPublicKey, hasKey: false },
    { userId: 'person-c', name: 'Person C', username: 'person-c-login', publicKey: null, hasKey: false },
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
  expect(await within(dialog).findByText(await safetyCode('person-b', personB.publicKey))).toBeTruthy()
  expect(within(dialog).getByText('Waiting until they set up Passwords')).toBeTruthy()
  expect(within(dialog).getByText('Person C (person-c-login)')).toBeTruthy()

  // The words say how to compare, and a code kept for this browser only.
  expect(
    within(dialog).getByText(
      'Ask Person B (person-b-login) to open Passwords settings on their own device and read you their safety code. Confirm only if it matches.',
    ),
  ).toBeTruthy()

  // Person B's key is new to this device: it is sealed for only after the
  // owner confirms the safety code here.
  fireEvent.click(within(dialog).getByRole('button', { name: 'The safety code matches' }))
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save sharing' }))
  await waitFor(() => expect(calls('SetPasswordItemShares')).toHaveLength(1))
  // Widening: the fields are saved with the wider record first, then the
  // server is asked for the share.
  expect(orderOf({ SavePasswordItem: 'save', SetPasswordItemShares: 'shares' })).toEqual(['save', 'shares'])
  const savedFields = await fieldsSentIn((calls('SavePasswordItem')[0][1] as { input: any }).input, 'login-one')
  expect(savedFields.access).toEqual({ isUsableByAgent: false, grantees: [{ circleId: 'circle-one' }] })
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
            { userId: 'person-b', name: 'Person B', username: 'person-b-login', publicKey: null, hasKey: false },
          ],
        },
      ],
    },
  )
}

function waitingFor(itemId: string, publicKey: string) {
  return {
    itemId,
    recipient: { userId: 'person-b', name: 'Person B', username: 'person-b-login', publicKey, hasKey: false },
  }
}

it('gives the key to a person who set up Passwords after the item was shared, and says so', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  // Person B's key is one the owner confirmed on this device when they shared
  // with them before.
  confirmKeyOnDevice('person-b', personBPublicKey)
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
  await openSession()
  renderAt('/passwords')

  expect(
    await screen.findByText('Gave 1 person access to the items you shared, now that they set up Passwords.'),
  ).toBeTruthy()
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

it('seals nothing for a person whose key is not the kept one until the owner confirms the new key', async () => {
  const oldKeyPair = await generatePersonKeyPair()
  const newPublicKey = encodeBase64(personB.publicKey)
  // Person B was shared with before, and this device kept the key it saw then.
  window.localStorage.setItem(
    `teanode.passwords.pins:${USER_ID}`,
    JSON.stringify({ 'person-b': encodeBase64(oldKeyPair.publicKey) }),
  )
  serve({
    records: [await sharedWithPersonB('login-given', 'Given login')],
    waiting: [waitingFor('login-given', newPublicKey)],
  })
  await openSession()
  renderAt('/passwords')

  const dialog = await screen.findByRole('alertdialog')
  expect(within(dialog).getByText("Person B's key changed")).toBeTruthy()
  expect(await within(dialog).findByText(await safetyCode('person-b', oldKeyPair.publicKey))).toBeTruthy()
  expect(await within(dialog).findByText(await safetyCode('person-b', personB.publicKey))).toBeTruthy()
  expect(calls('AddPasswordShareKeys')).toHaveLength(0)

  fireEvent.click(within(dialog).getByRole('button', { name: 'The new safety code matches' }))
  await waitFor(() => expect(calls('AddPasswordShareKeys')).toHaveLength(1))
  expect((calls('AddPasswordShareKeys')[0][1] as { keys: Record<string, string>[] }).keys[0]).toMatchObject({
    recipientUserId: 'person-b',
    recipientPublicKey: newPublicKey,
  })
  // The new key is the kept one now.
  expect(JSON.parse(window.localStorage.getItem(`teanode.passwords.pins:${USER_ID}`)!)).toEqual({
    'person-b': newPublicKey,
  })
  await waitFor(() => expect(screen.queryByRole('alertdialog')).toBeNull())
})

it('does not ask again about a changed key the owner said no to', async () => {
  const oldKeyPair = await generatePersonKeyPair()
  window.localStorage.setItem(
    `teanode.passwords.pins:${USER_ID}`,
    JSON.stringify({ 'person-b': encodeBase64(oldKeyPair.publicKey) }),
  )
  serve({
    records: [await sharedWithPersonB('login-given', 'Given login')],
    waiting: [waitingFor('login-given', encodeBase64(personB.publicKey))],
  })
  await openSession()
  renderAt('/passwords')

  const dialog = await screen.findByRole('alertdialog')
  fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }))
  await waitFor(() => expect(screen.queryByRole('alertdialog')).toBeNull())
  expect(calls('AddPasswordShareKeys')).toHaveLength(0)
  expect(JSON.parse(window.localStorage.getItem(`teanode.passwords.pins:${USER_ID}`)!)).toEqual({
    'person-b': encodeBase64(oldKeyPair.publicKey),
  })
})

it('seals nothing for a waiting person this device has no key for until the owner confirms the safety code', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  serve({
    records: [await sharedWithPersonB('login-given', 'Given login')],
    waiting: [waitingFor('login-given', personBPublicKey)],
  })
  await openSession()
  renderAt('/passwords')

  const dialog = await screen.findByRole('alertdialog')
  expect(within(dialog).getByText("Confirm Person B's safety code")).toBeTruthy()
  expect(
    within(dialog).getByText(
      'Ask Person B to open Passwords settings on their own device and read you their safety code. Confirm only if it matches.',
    ),
  ).toBeTruthy()
  expect(within(dialog).getByText(/kept on this browser only/)).toBeTruthy()
  expect(await within(dialog).findByText(await safetyCode('person-b', personB.publicKey))).toBeTruthy()
  // The background read has run, and nothing was sealed or kept.
  expect(calls('AddPasswordShareKeys')).toHaveLength(0)
  expect(window.localStorage.getItem(`teanode.passwords.pins:${USER_ID}`)).toBeNull()

  fireEvent.click(within(dialog).getByRole('button', { name: 'The safety code matches' }))
  await waitFor(() => expect(calls('AddPasswordShareKeys')).toHaveLength(1))
  expect((calls('AddPasswordShareKeys')[0][1] as { keys: Record<string, string>[] }).keys[0]).toMatchObject({
    itemId: 'login-given',
    recipientUserId: 'person-b',
    recipientPublicKey: personBPublicKey,
  })
  expect(JSON.parse(window.localStorage.getItem(`teanode.passwords.pins:${USER_ID}`)!)).toEqual({
    'person-b': personBPublicKey,
  })
  await waitFor(() => expect(screen.queryByRole('alertdialog')).toBeNull())
})

it('returns an unpinned waiting person for confirmation, seals only the key that was confirmed, and asks again if the key is swapped', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  const swappedPublicKey = encodeBase64((await generatePersonKeyPair()).publicKey)
  const server = serve({
    records: [await sharedWithPersonB('login-given', 'Given login')],
    waiting: [waitingFor('login-given', personBPublicKey)],
  })
  await openSession()

  const first = await passwordsSession.completeWaitingShares()
  expect(first.sharedCount).toBe(0)
  expect(first.keysToConfirm).toEqual([
    expect.objectContaining({ recipientUserId: 'person-b', oldPublicKey: null, newPublicKey: personBPublicKey }),
  ])
  expect(calls('AddPasswordShareKeys')).toHaveLength(0)

  // The server lists another key after the owner looked at the first one.
  server.waiting = [waitingFor('login-given', swappedPublicKey)]
  const swapped = await passwordsSession.confirmChangedKey(first.keysToConfirm[0])
  expect(swapped.sharedCount).toBe(0)
  expect(swapped.keysToConfirm).toEqual([expect.objectContaining({ newPublicKey: swappedPublicKey })])
  expect(calls('AddPasswordShareKeys')).toHaveLength(0)

  server.waiting = [waitingFor('login-given', personBPublicKey)]
  const confirmed = await passwordsSession.confirmChangedKey(first.keysToConfirm[0])
  expect(confirmed.sharedCount).toBe(1)
  expect(calls('AddPasswordShareKeys')).toHaveLength(1)
})

it('gives nothing to a forged waiting share for a person the sealed record does not reach', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  confirmKeyOnDevice('person-b', personBPublicKey)
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
  expect(result.sharedCount).toBe(0)
  expect(result.keysToConfirm).toEqual([])
  expect(calls('AddPasswordShareKeys')).toHaveLength(0)
})

it('reads the items in full once when the waiting list names a person or an item the cached copies do not reach', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  confirmKeyOnDevice('person-b', personBPublicKey)
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
  expect(result.sharedCount).toBe(1)
  const reads = calls('ListPasswordItems').slice(readsBefore)
  expect(reads.map((call) => (call[1] as { sinceRevision: number | null }).sinceRevision)).toEqual([null])
  const keys = (calls('AddPasswordShareKeys')[0][1] as { keys: Record<string, string>[] }).keys
  expect(keys).toHaveLength(1)
  expect(keys[0]).toMatchObject({ itemId: 'login-circle', recipientUserId: 'person-b' })
})

it('reads in full once, not at every check, for a waiting person nobody reaches', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  confirmKeyOnDevice('person-b', personBPublicKey)
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
  expect(savedFields.access).toEqual({ isUsableByAgent: true, grantees: [] })

  // Turning it off re-keys the item instead (see the re-keying tests).
  fireEvent.click(screen.getByRole('checkbox', { name: /Let the agent use this/ }))
  await waitFor(() => expect(calls('RekeyPasswordItem')).toHaveLength(1))
  expect(calls('SetPasswordItemUsableByAgent')).toHaveLength(1)
  const rekey = (calls('RekeyPasswordItem')[0][1] as { input: any }).input
  expect(rekey.itemKeyByAgentKey).toBeNull()
  // The new fields carry the record with the agent off.
  expect((await fieldsSentIn(rekey, 'login-one')).access).toEqual({ isUsableByAgent: false, grantees: [] })
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

it('reads the recipients again, with their new safety codes, when a key changed while the share was open', async () => {
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
                { userId: 'person-b', name: 'Person B', username: 'person-b-login', publicKey: firstKey, hasKey: true },
              ],
            },
          ],
        },
      ),
    ],
    recipients: [
      { userId: 'person-b', name: 'Person B', username: 'person-b-login', publicKey: secondKey, hasKey: false },
    ],
  })
  const answer = vi.mocked(graphql).getMockImplementation()!
  vi.mocked(graphql).mockImplementation(async (document, variables) => {
    if (document.includes('SetPasswordItemShares')) {
      throw new APIError("That person's key changed since it was read. Read their key again and seal the item for it.")
    }
    return answer(document, variables)
  })
  await openSession()
  renderAt('/passwords?item=login-one')

  fireEvent.click(await screen.findByRole('button', { name: 'Example login: Share' }))
  const dialog = await screen.findByRole('dialog', { name: 'Share Example login' })
  expect(await within(dialog).findByText(await safetyCode('person-b', personB.publicKey))).toBeTruthy()
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save sharing' }))

  expect(await screen.findByText(/key changed since it was read/)).toBeTruthy()
  expect(await within(dialog).findByText(await safetyCode('person-b', newKeyPair.publicKey))).toBeTruthy()
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
  return { userId: 'person-b', name: 'Person B', username: 'person-b-login', publicKey, hasKey }
}

it('keeps the key it sealed for as the known key of that person', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  serve({ records: await onlyExampleLogin(), recipients: [personBRecipient(personBPublicKey)] })
  await openSession()
  renderAt('/passwords?item=login-one')

  const dialog = await openShareWithCircle()
  fireEvent.click(within(dialog).getByRole('button', { name: 'The safety code matches' }))
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save sharing' }))
  await waitFor(() => expect(calls('SetPasswordItemShares')).toHaveLength(1))
  await screen.findByText('Sharing saved.')
  expect(JSON.parse(window.localStorage.getItem(`teanode.passwords.pins:${USER_ID}`)!)).toEqual({
    'person-b': personBPublicKey,
  })
})

it('seals nothing for a person whose key this device has never kept until the owner confirms it in the share dialog', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  serve({ records: await onlyExampleLogin(), recipients: [personBRecipient(personBPublicKey)] })
  await openSession()
  renderAt('/passwords?item=login-one')

  const dialog = await openShareWithCircle()
  expect(within(dialog).getByText('This device has not confirmed their key yet.')).toBeTruthy()
  // Saved without confirming: the sharing is saved, with no key for Person B,
  // and nothing is kept as their known key.
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save sharing' }))
  await waitFor(() => expect(calls('SetPasswordItemShares')).toHaveLength(1))
  expect((calls('SetPasswordItemShares')[0][1] as { input: { keys: unknown[] } }).input.keys).toEqual([])
  expect(
    await screen.findByText('Sharing saved. 1 person was left out until you confirm their safety code.'),
  ).toBeTruthy()
  expect(window.localStorage.getItem(`teanode.passwords.pins:${USER_ID}`)).toBeNull()
})

it('refuses to seal in shareItem for an unconfirmed key unless that very key was confirmed', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  const otherPublicKey = encodeBase64((await generatePersonKeyPair()).publicKey)
  serve({ records: await onlyExampleLogin() })
  await openSession()
  const item = passwordsSession.getSnapshot().items.find((candidate) => candidate.id === 'login-one')!
  const targets = [
    {
      grantee: { granteeUserId: 'person-b', granteeCircleId: null },
      recipients: [personBRecipient(personBPublicKey)],
    },
  ]

  // A different key was confirmed than the one the server lists now.
  let shared = await passwordsSession.shareItem(item, targets, new Map([['person-b', otherPublicKey]]))
  expect(shared.unconfirmedUserIds).toEqual(['person-b'])
  expect((calls('SetPasswordItemShares')[0][1] as { input: { keys: unknown[] } }).input.keys).toEqual([])

  shared = await passwordsSession.shareItem(shared.item, targets, new Map([['person-b', personBPublicKey]]))
  expect(shared.unconfirmedUserIds).toEqual([])
  expect((calls('SetPasswordItemShares')[1][1] as { input: { keys: unknown[] } }).input.keys).toHaveLength(1)
  expect(passwordsSession.isKeyConfirmed('person-b', personBPublicKey)).toBe(true)
})

it('flags a key that is not the known one and seals nothing for it until the owner confirms it', async () => {
  const oldKeyPair = await generatePersonKeyPair()
  const newPublicKey = encodeBase64(personB.publicKey)
  window.localStorage.setItem(
    `teanode.passwords.pins:${USER_ID}`,
    JSON.stringify({ 'person-b': encodeBase64(oldKeyPair.publicKey) }),
  )
  serve({ records: await recordsToList(), recipients: [personBRecipient(newPublicKey)] })
  await openSession()
  renderAt('/passwords?item=login-one')

  const dialog = await openShareWithCircle()
  expect(within(dialog).getByText('Their key changed.')).toBeTruthy()
  expect(await within(dialog).findByText(await safetyCode('person-b', oldKeyPair.publicKey))).toBeTruthy()

  // Saved without confirming: the sharing is saved, with no key for Person B.
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save sharing' }))
  await waitFor(() => expect(calls('SetPasswordItemShares')).toHaveLength(1))
  expect((calls('SetPasswordItemShares')[0][1] as { input: { keys: unknown[] } }).input.keys).toEqual([])
  expect(
    await screen.findByText('Sharing saved. 1 person was left out until you confirm their safety code.'),
  ).toBeTruthy()
  expect(JSON.parse(window.localStorage.getItem(`teanode.passwords.pins:${USER_ID}`)!)).toEqual({
    'person-b': encodeBase64(oldKeyPair.publicKey),
  })
})

it('seals for a changed key once the owner confirms it, and keeps it as the known key', async () => {
  const oldKeyPair = await generatePersonKeyPair()
  const newPublicKey = encodeBase64(personB.publicKey)
  window.localStorage.setItem(
    `teanode.passwords.pins:${USER_ID}`,
    JSON.stringify({ 'person-b': encodeBase64(oldKeyPair.publicKey) }),
  )
  serve({ records: await recordsToList(), recipients: [personBRecipient(newPublicKey)] })
  await openSession()
  renderAt('/passwords?item=login-one')

  const dialog = await openShareWithCircle()
  fireEvent.click(within(dialog).getByRole('button', { name: 'The new safety code matches' }))
  expect(within(dialog).getByText('You confirmed this key.')).toBeTruthy()
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save sharing' }))
  await waitFor(() => expect(calls('SetPasswordItemShares')).toHaveLength(1))
  const keys = (calls('SetPasswordItemShares')[0][1] as { input: { keys: Record<string, string>[] } }).input.keys
  expect(keys).toHaveLength(1)
  expect(keys[0]).toMatchObject({ recipientUserId: 'person-b', recipientPublicKey: newPublicKey })
  await screen.findByText('Sharing saved.')
  expect(JSON.parse(window.localStorage.getItem(`teanode.passwords.pins:${USER_ID}`)!)).toEqual({
    'person-b': newPublicKey,
  })
})

it('says whom an item is shared with and names a person once in the share dialog', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  const circleMember = (userId: string, publicKey: string | null): PasswordRecipient => ({
    userId,
    name: `Member ${userId}`,
    username: userId,
    publicKey,
    hasKey: publicKey !== null,
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
  // The code is made from the person's user id as well as the key, so two
  // people who happen to hold the same key still have different codes.
  expect(within(dialog).getAllByText(await safetyCode('person-b', personB.publicKey))).toHaveLength(1)
  expect(within(dialog).getAllByText(await safetyCode('member-one', personB.publicKey))).toHaveLength(1)
  expect(within(dialog).getAllByText(await safetyCode('member-two', personB.publicKey))).toHaveLength(1)
})

it('says a recipient was confirmed on this browser, with no button to confirm again', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  confirmKeyOnDevice('person-b', personBPublicKey)
  serve({ records: await onlyExampleLogin(), recipients: [personBRecipient(personBPublicKey)] })
  await openSession()
  renderAt('/passwords?item=login-one')

  const dialog = await openShareWithCircle()
  expect(within(dialog).getByText('You confirmed this person on this browser.')).toBeTruthy()
  expect(within(dialog).queryByRole('button', { name: 'The safety code matches' })).toBeNull()
  // The known limit is said where it matters: the confirmation stays on this browser.
  expect(within(dialog).getByText(/kept on this browser only/)).toBeTruthy()
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
  confirmKeyOnDevice('person-b', personBPublicKey)
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
      <SafetyCode userId="person-b" publicKey="not base64 !!!" />
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

// --- keys that were only seen -------------------------------------------------------

it('keeps the key an incoming item came with as a seen key, not a confirmed one', async () => {
  serve({ records: await recordsToList() })
  await openSession()
  expect(storedKeys('pins')).toEqual({ 'person-b': encodeBase64(personB.publicKey) })
  expect(storedKeys('confirmedKeys')).toBeNull()
})

it('asks before sealing for a key that only a planted item came with', async () => {
  // A hostile server sets Passwords up for Person B with a key pair of its own
  // (personB here), plants one item "shared by" Person B, and lists Person B as
  // waiting for the owner's item. Opening the planted item must not make the
  // key confirmed.
  const personBPublicKey = encodeBase64(personB.publicKey)
  serve({
    records: [
      await sharedWithPersonB('login-given', 'Given login'),
      await sharedRecordFor('login-planted', { ...emptyLoginFields(), name: 'Planted' }),
    ],
    waiting: [waitingFor('login-given', personBPublicKey)],
  })
  await openSession()
  expect(storedKeys('pins')).toEqual({ 'person-b': personBPublicKey })

  const result = await passwordsSession.completeWaitingShares()
  expect(result.sharedCount).toBe(0)
  expect(result.keysToConfirm).toEqual([
    expect.objectContaining({ recipientUserId: 'person-b', oldPublicKey: null, newPublicKey: personBPublicKey }),
  ])
  expect(calls('AddPasswordShareKeys')).toHaveLength(0)

  // The page asks the owner the same way, and seals nothing meanwhile.
  renderAt('/passwords')
  const dialog = await screen.findByRole('alertdialog')
  expect(within(dialog).getByText("Confirm Person B's safety code")).toBeTruthy()
  expect(calls('AddPasswordShareKeys')).toHaveLength(0)
})

it('asks once about a key an earlier version kept, and does not ask again after the owner confirms it', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  // What the version before the split left: a kept key, now only a seen one.
  window.localStorage.setItem(`teanode.passwords.pins:${USER_ID}`, JSON.stringify({ 'person-b': personBPublicKey }))
  serve({
    records: [await sharedWithPersonB('login-given', 'Given login')],
    waiting: [waitingFor('login-given', personBPublicKey)],
  })
  await openSession()

  const first = await passwordsSession.completeWaitingShares()
  expect(first.sharedCount).toBe(0)
  expect(first.keysToConfirm).toHaveLength(1)
  expect(calls('AddPasswordShareKeys')).toHaveLength(0)

  const confirmed = await passwordsSession.confirmChangedKey(first.keysToConfirm[0])
  expect(confirmed.sharedCount).toBe(1)
  expect(storedKeys('confirmedKeys')).toEqual({ 'person-b': personBPublicKey })

  const later = await passwordsSession.completeWaitingShares()
  expect(later.keysToConfirm).toEqual([])
  expect(later.sharedCount).toBe(1)
})

it('asks in the share dialog about a key that was only seen, and seals nothing until it is confirmed', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  serve({
    records: [
      ...(await onlyExampleLogin()),
      await sharedRecordFor('login-planted', { ...emptyLoginFields(), name: 'Planted' }),
    ],
    recipients: [personBRecipient(personBPublicKey)],
  })
  await openSession()
  expect(storedKeys('pins')).toEqual({ 'person-b': personBPublicKey })
  renderAt('/passwords?item=login-one')

  const dialog = await openShareWithCircle()
  expect(within(dialog).getByRole('button', { name: 'The safety code matches' })).toBeTruthy()
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save sharing' }))
  await waitFor(() => expect(calls('SetPasswordItemShares')).toHaveLength(1))
  expect((calls('SetPasswordItemShares')[0][1] as { input: { keys: unknown[] } }).input.keys).toEqual([])
  expect(storedKeys('confirmedKeys')).toBeNull()
})

it('confirms the owner key once the person confirms it', async () => {
  const oldKeyPair = await generatePersonKeyPair()
  window.localStorage.setItem(
    `teanode.passwords.pins:${USER_ID}`,
    JSON.stringify({ 'person-b': encodeBase64(oldKeyPair.publicKey) }),
  )
  serve({ records: await recordsToList() })
  await openSession()
  renderAt('/passwords?item=login-shared')
  fireEvent.click(await screen.findByRole('button', { name: 'The new safety code matches' }))
  await screen.findByRole('button', { name: 'Show Password' })
  expect(storedKeys('confirmedKeys')).toEqual({ 'person-b': encodeBase64(personB.publicKey) })
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

it('seals the new key only for people the record reaches and whose key the owner confirmed, when the agent is turned off', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  confirmKeyOnDevice('person-b', personBPublicKey)
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
            // Person C has a key for the item, but this device never confirmed theirs.
            { userId: 'person-c', name: 'Person C', publicKey: personCPublicKey(), hasKey: true },
            { userId: 'person-d', name: 'Person D', publicKey: null, hasKey: false },
            // A share the owner's record does not have, whose key is confirmed here.
            { userId: 'person-e', name: 'Person E', publicKey: personBPublicKey, hasKey: true },
          ]),
        },
      ),
    ],
  })
  confirmKeyOnDevice('person-e', personBPublicKey)
  await openSession()
  await passwordsSession.setUsableByAgent(passwordsSession.getSnapshot().items[0], false)

  expect(calls('RekeyPasswordItem')).toHaveLength(1)
  const input = (calls('RekeyPasswordItem')[0][1] as { input: Record<string, any> }).input
  expect(input.shareKeys).toHaveLength(1)
  expect(input.shareKeys[0]).toMatchObject({
    itemId: 'login-one',
    recipientUserId: 'person-b',
    recipientPublicKey: personBPublicKey,
  })
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
  confirmKeyOnDevice('person-b', personBPublicKey)
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
  // Narrowing: the narrower list is saved first, then the item is re-keyed.
  expect(orderOf({ SetPasswordItemShares: 'shares', RekeyPasswordItem: 'rekey', SavePasswordItem: 'save' })).toEqual([
    'shares',
    'rekey',
  ])
  const input = (calls('RekeyPasswordItem')[0][1] as { input: Record<string, any> }).input
  expect(input.shareKeys.map((key: { recipientUserId: string }) => key.recipientUserId)).toEqual(['person-b'])
  // The fields the re-key carries have the narrower record.
  expect((await fieldsSentIn(input, 'login-one')).access).toEqual({
    isUsableByAgent: true,
    grantees: [{ userId: 'person-b' }],
  })
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
  confirmKeyOnDevice('person-b', personBPublicKey)
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
  confirmKeyOnDevice('person-b', encodeBase64(personB.publicKey))
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

it('gives an item the server marked a new key with the next save', async () => {
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
  await openSession()
  renderAt('/passwords?item=login-one')
  await waitFor(() => expect(passwordsSession.getSnapshot().items[0].needsNewKey).toBe(false))
  expect(calls('RekeyPasswordItem')).toHaveLength(1)
})

it('says how many people wait for their safety codes after a re-key that could not seal for them, and asks at once', async () => {
  const personBPublicKey = encodeBase64(personB.publicKey)
  // Nobody here has confirmed Person B's safety code.
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

  expect(await screen.findByText('1 person waits until you confirm their safety code.')).toBeTruthy()
  const dialog = await screen.findByRole('alertdialog')
  expect(within(dialog).getByText("Confirm Person B's safety code")).toBeTruthy()
  const rekey = (calls('RekeyPasswordItem')[0][1] as { input: Record<string, any> }).input
  expect(rekey.shareKeys).toEqual([])
  expect(calls('AddPasswordShareKeys')).toHaveLength(0)

  // Confirming gives them the key to the new item key at once.
  fireEvent.click(within(dialog).getByRole('button', { name: 'The safety code matches' }))
  await waitFor(() => expect(calls('AddPasswordShareKeys')).toHaveLength(1))
  const keys = (calls('AddPasswordShareKeys')[0][1] as { keys: Record<string, string>[] }).keys
  expect(keys[0]).toMatchObject({ itemId: 'login-one', recipientUserId: 'person-b' })
  expect(keys[0].itemKey).toBe(rekey.itemKey)
})

it('says how many wait when taking somebody off leaves another person unconfirmed', async () => {
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

  expect(await screen.findByText('1 person waits until you confirm their safety code.')).toBeTruthy()
  expect(await screen.findByRole('alertdialog')).toBeTruthy()
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

it('shows the own safety code in Settings from the opened key pair and the own user id', async () => {
  serve({ records: [], unlockPublicKey: null })
  await openSession()
  renderAt('/passwords?tab=settings')
  expect(await screen.findByText('Your safety code')).toBeTruthy()
  const code = await safetyCode(USER_ID, fixture.keyPair.publicKey)
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
