import { afterEach, describe, expect, it, vi } from 'vitest'
import { newRandomId } from './randomId'

describe('newRandomId', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('uses the browser when it can make one', () => {
    vi.stubGlobal('crypto', { randomUUID: () => '00000000-0000-4000-8000-000000000000' })
    expect(newRandomId('tab')).toBe('00000000-0000-4000-8000-000000000000')
  })

  it('makes one of its own outside a secure context, where the browser has none', () => {
    vi.stubGlobal('crypto', {})
    const first = newRandomId('tab')
    expect(first).toMatch(/^tab-[0-9a-z]+-[0-9a-z]+$/)
    expect(newRandomId('tab')).not.toBe(first)
  })
})
