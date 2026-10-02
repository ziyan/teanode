import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { endImpersonation } from '../api'
import { ImpersonationBanner } from './impersonationBanner'

const notices = vi.hoisted(() => ({ done: vi.fn(), failed: vi.fn(), failure: vi.fn() }))
vi.mock('../api', () => ({ endImpersonation: vi.fn() }))
vi.mock('../i18n/i18n', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, string>) => `${key}${values ? ' ' + Object.values(values).join(' ') : ''}`,
    language: 'en',
  }),
}))
vi.mock('./toast', () => ({ useToast: () => notices }))
const end = vi.mocked(endImpersonation)

const assign = vi.fn()
beforeEach(() => {
  end.mockReset()
  notices.failure.mockReset()
  assign.mockReset()
  Object.defineProperty(window, 'location', { value: { assign }, writable: true })
})
afterEach(cleanup)

// The strip says whose account this is and who is really looking, and its
// button returns the operator and starts the dashboard again as them.
it('names both people and returns the operator', async () => {
  end.mockResolvedValue({ authenticated: true, authenticationRequired: true, username: 'operator', passkeysEnabled: false })
  render(<ImpersonationBanner username="river" impersonatorUsername="operator" endsAt="2030-01-01T10:00:00Z" />)
  expect(screen.getByText(/impersonation.title river/)).toBeTruthy()
  expect(screen.getByText(/impersonation.bodyUntil operator/)).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: /impersonation.return/ }))
  await waitFor(() => expect(assign).toHaveBeenCalledWith('/'))
  expect(end).toHaveBeenCalledTimes(1)
})

// A return the server refuses says so and leaves the button usable.
it('says when the return fails', async () => {
  end.mockRejectedValue(new Error('the server is restarting'))
  render(<ImpersonationBanner username="river" impersonatorUsername="operator" />)
  fireEvent.click(screen.getByRole('button', { name: /impersonation.return/ }))
  await waitFor(() => expect(notices.failure).toHaveBeenCalledTimes(1))
  expect(assign).not.toHaveBeenCalled()
  expect((screen.getByRole('button', { name: /impersonation.return/ }) as HTMLButtonElement).disabled).toBe(false)
})
