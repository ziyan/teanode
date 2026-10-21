import { afterEach, describe, expect, it, vi } from 'vitest'
import { restrictStorageToTrustedContexts } from './storageAccess'

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('restrictStorageToTrustedContexts', () => {
  it('closes the local and the sync storage to content scripts', async () => {
    const local = { setAccessLevel: vi.fn(async () => undefined) }
    const sync = { setAccessLevel: vi.fn(async () => undefined) }
    const session = { setAccessLevel: vi.fn(async () => undefined) }
    vi.stubGlobal('chrome', { storage: { local, sync, session } })
    await restrictStorageToTrustedContexts()
    expect(local.setAccessLevel).toHaveBeenCalledWith({ accessLevel: 'TRUSTED_CONTEXTS' })
    expect(sync.setAccessLevel).toHaveBeenCalledWith({ accessLevel: 'TRUSTED_CONTEXTS' })
    // The session storage is closed to them already.
    expect(session.setAccessLevel).not.toHaveBeenCalled()
  })

  it('carries on when one is refused or the browser has no such setting', async () => {
    const sync = { setAccessLevel: vi.fn(async () => undefined) }
    vi.stubGlobal('chrome', {
      storage: {
        local: {
          setAccessLevel: async () => {
            throw new Error('not supported')
          },
        },
        sync,
      },
    })
    await expect(restrictStorageToTrustedContexts()).resolves.toBeUndefined()
    expect(sync.setAccessLevel).toHaveBeenCalled()
    vi.stubGlobal('chrome', { storage: { local: {}, sync: {} } })
    await expect(restrictStorageToTrustedContexts()).resolves.toBeUndefined()
  })
})
