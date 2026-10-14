import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { TranslationProvider } from '../i18n/i18n'
import { PasswordUsage, isMaybeSafeToDelete, sortNewestFirst } from './passwordUsage'

afterEach(cleanup)

const NOW = Date.parse('2030-06-01T12:00:00Z')
const MINUTE_MS = 60 * 1000
const DAY_MS = 24 * 60 * MINUTE_MS

function isoBefore(elapsedMS: number): string {
  return new Date(Date.now() - elapsedMS).toISOString()
}

function renderUsage(password: { createdAt: string; lastUsedAt?: string | null }) {
  return render(
    <TranslationProvider>
      <div data-testid="usage">
        <PasswordUsage password={password} />
      </div>
    </TranslationProvider>,
  )
}

describe('PasswordUsage', () => {
  it('shows when it was created and how long ago it was last used', () => {
    renderUsage({ createdAt: isoBefore(10 * DAY_MS), lastUsedAt: isoBefore(5 * MINUTE_MS) })
    const text = screen.getByTestId('usage').textContent ?? ''
    expect(text).toContain('Created ')
    expect(text).toContain('Last used 5 minutes ago')
    expect(text).not.toContain('Never used')
    expect(text).not.toContain('safe to delete')
  })

  it('says never used, without a hint, within the first hour', () => {
    renderUsage({ createdAt: isoBefore(10 * MINUTE_MS), lastUsedAt: null })
    const text = screen.getByTestId('usage').textContent ?? ''
    expect(text).toContain('Never used')
    expect(text).not.toContain('safe to delete')
  })

  it('hints that a password never used after an hour may be safe to delete', () => {
    renderUsage({ createdAt: isoBefore(2 * DAY_MS) })
    const text = screen.getByTestId('usage').textContent ?? ''
    expect(text).toContain('Never used')
    expect(text).toContain('May be safe to delete')
  })

  it('hints on a password last used a long time ago', () => {
    renderUsage({ createdAt: isoBefore(400 * DAY_MS), lastUsedAt: isoBefore(120 * DAY_MS) })
    expect(screen.getByTestId('usage').textContent).toContain('May be safe to delete')
  })
})

describe('isMaybeSafeToDelete', () => {
  it('uses one hour and ninety days as the limits', () => {
    const created = new Date(NOW - 61 * MINUTE_MS).toISOString()
    expect(isMaybeSafeToDelete({ createdAt: created }, NOW)).toBe(true)
    expect(isMaybeSafeToDelete({ createdAt: created }, NOW - 2 * MINUTE_MS)).toBe(false)
    const used = new Date(NOW - 89 * DAY_MS).toISOString()
    expect(isMaybeSafeToDelete({ createdAt: used, lastUsedAt: used }, NOW)).toBe(false)
    const longAgo = new Date(NOW - 91 * DAY_MS).toISOString()
    expect(isMaybeSafeToDelete({ createdAt: longAgo, lastUsedAt: longAgo }, NOW)).toBe(true)
  })
})

describe('sortNewestFirst', () => {
  it('puts the newest password first without changing the input', () => {
    const passwords = [
      { id: 'old', createdAt: '2030-01-01T00:00:00Z' },
      { id: 'new', createdAt: '2030-03-01T00:00:00Z' },
      { id: 'middle', createdAt: '2030-02-01T00:00:00Z' },
    ]
    expect(sortNewestFirst(passwords).map((password) => password.id)).toEqual(['new', 'middle', 'old'])
    expect(passwords[0].id).toBe('old')
  })
})
