// A stand-in for the server's Passwords operations, for the tests: it answers
// the few GraphQL documents the extension sends, with sealed values made by
// the same code the dashboard uses, so a test exercises the real encryption.

import { decodeBase64, hashProof } from '../../../src/passwords/crypto'
import { emptyLoginFields, LoginFields } from '../../../src/passwords/fields'
import { PasswordItemRecord } from '../../../src/pages/passwords/passwordsApi'
import { makeSetUp, PendingSetUp, sealPasswordItem } from '../../../src/pages/passwords/passwordsProtocol'
import { GraphqlCall, StorageArea } from './vault'

export const PASSPHRASE = 'correct horse battery staple'
export const USER_ID = 'person-a'

export function memoryArea(): StorageArea & { contents: Record<string, unknown> } {
  const contents: Record<string, unknown> = {}
  return {
    contents,
    async get(keys) {
      const wanted = Array.isArray(keys) ? keys : [keys]
      return Object.fromEntries(
        wanted.filter((key) => key in contents).map((key) => [key, structuredClone(contents[key])]),
      )
    },
    async set(items) {
      for (const [key, value] of Object.entries(items)) contents[key] = structuredClone(value)
    },
    async remove(keys) {
      for (const key of Array.isArray(keys) ? keys : [keys]) delete contents[key]
    },
  }
}

export function makeLogin(overrides: Partial<LoginFields>): LoginFields {
  return {
    ...emptyLoginFields(),
    name: 'Example',
    userName: 'ada@example.test',
    password: 'hunter2-plaintext',
    sites: ['https://example.com'],
    ...overrides,
  }
}

export async function ownRecord(
  fixture: PendingSetUp,
  id: string,
  fields: LoginFields,
  revision = 1,
): Promise<PasswordItemRecord> {
  const sealed = await sealPasswordItem(fixture.accountKey, id, 'login', fields, null)
  return {
    id,
    ownerUserId: USER_ID,
    ownerName: 'Ada',
    ownerPublicKey: fixture.input.publicKey,
    itemKind: 'login',
    encryptedFields: sealed.encryptedFields,
    itemKey: sealed.itemKey,
    isSharedWithMe: false,
    revision,
    isUsableByAgent: false,
    lastUsedByAgentAt: null,
    deletedAt: null,
    createdAt: '2026-10-20T00:00:00Z',
    updatedAt: '2026-10-20T00:00:00Z',
    shares: [],
  }
}

export function makeFixture(): Promise<PendingSetUp> {
  return makeSetUp(PASSPHRASE)
}

export interface FakeServer {
  records: PasswordItemRecord[]
  calls: string[]
  saved: { input: Record<string, any> }[]
  call: GraphqlCall
}

export function createFakeServer(fixture: PendingSetUp, records: PasswordItemRecord[] = []): FakeServer {
  const server: FakeServer = {
    records,
    calls: [],
    saved: [],
    call: async (query, variables = {}) => {
      const name =
        /(ReadPasswordsSetup|UnlockPasswords|GetSession|ListPasswordItems|SavePasswordItem)/.exec(query)?.[1] ?? ''
      server.calls.push(name)
      if (name === 'ReadPasswordsSetup') {
        return {
          ReadPasswordsSetup: {
            isSetUp: true,
            passphraseSalt: fixture.input.passphraseSalt,
            passphraseIterationCount: fixture.input.passphraseIterationCount,
            publicKey: fixture.input.publicKey,
            lockAfterMinutes: 5,
            itemCount: server.records.length,
          },
        }
      }
      if (name === 'UnlockPasswords') {
        const hash = await hashProof(decodeBase64(variables.proof as string))
        const expected = variables.isRecoveryKey
          ? fixture.input.recoveryKeyProofHash
          : fixture.input.passphraseProofHash
        if (hash !== expected) {
          throw new Error(variables.isRecoveryKey ? 'That recovery key is not right.' : 'That passphrase is not right.')
        }
        return {
          UnlockPasswords: {
            accountKeyWrapped: variables.isRecoveryKey
              ? fixture.input.accountKeyByRecoveryKey
              : fixture.input.accountKeyByPassphrase,
            privateKeyByAccountKey: fixture.input.privateKeyByAccountKey,
            publicKey: fixture.input.publicKey,
          },
        }
      }
      if (name === 'GetSession') return { GetSession: { userId: USER_ID } }
      if (name === 'ListPasswordItems') {
        return {
          ListPasswordItems: { items: server.records, itemIds: server.records.map((record) => record.id), revision: 9 },
        }
      }
      if (name === 'SavePasswordItem') {
        const input = variables.input as Record<string, any>
        server.saved.push({ input })
        const existing = server.records.find((record) => record.id === input.id)
        if (existing && existing.revision !== input.baseRevision) {
          throw new Error('This item changed on another device. Open it again.')
        }
        const record: PasswordItemRecord = {
          ...(existing ?? (await ownRecord(fixture, input.id, emptyLoginFields()))),
          encryptedFields: input.encryptedFields,
          itemKey: input.itemKey,
          revision: (existing?.revision ?? 0) + 1,
        }
        server.records = [...server.records.filter((candidate) => candidate.id !== input.id), record]
        return { SavePasswordItem: record }
      }
      throw new Error('unexpected query ' + query)
    },
  }
  return server
}
