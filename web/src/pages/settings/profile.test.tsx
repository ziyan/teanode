import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'

import { graphql } from '../../api'
import { ToastProvider } from '../../components/toast'
import { TranslationProvider } from '../../i18n/i18n'
import { SessionProvider } from '../../session'
import { ProfilePage } from './profile'

vi.mock('../../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api')>()),
  graphql: vi.fn(),
}))
afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})

function answerWith(pronoun: string) {
  vi.mocked(graphql).mockImplementation(async (document) => {
    if (document.includes('GetCurrentUser')) {
      return {
        GetCurrentUser: {
          id: 'person-a',
          username: 'person-a-login',
          name: 'Person A',
          email: '',
          avatarUrl: null,
          avatarColorIndex: 2,
          timezone: '',
          timezoneMode: 'auto',
          pronoun,
        },
      } as never
    }
    return {} as never
  })
}

function renderPage() {
  return render(
    <TranslationProvider>
      <ToastProvider>
        <SessionProvider
          value={{
            userId: 'person-a',
            authenticated: true,
            authenticationRequired: true,
            username: 'person-a-login',
            passkeysEnabled: false,
            permissions: { everywhere: [], byDomain: [] },
          }}
        >
          <MemoryRouter>
            <ProfilePage onSaved={() => {}} />
          </MemoryRouter>
        </SessionProvider>
      </ToastProvider>
    </TranslationProvider>,
  )
}

function updateCalls() {
  return vi.mocked(graphql).mock.calls.filter(([document]) => document.includes('UpdateUser'))
}

it('offers a pronoun, not set until the person says, with what it is for', async () => {
  answerWith('')
  renderPage()

  const field = await screen.findByRole('combobox', { name: 'Pronoun' })
  expect(field.textContent).toContain('Not set')
  expect(screen.getByText(/such as son or daughter, and how your agent speaks of you/)).toBeTruthy()
  // Nothing has changed, so there is nothing to save.
  expect((screen.getByRole('button', { name: 'Save' }) as HTMLButtonElement).disabled).toBe(true)
})

it('saves the chosen pronoun with the rest of the profile', async () => {
  answerWith('')
  renderPage()

  fireEvent.click(await screen.findByRole('combobox', { name: 'Pronoun' }))
  fireEvent.click(screen.getByRole('option', { name: 'She' }))
  fireEvent.click(screen.getByRole('button', { name: 'Save' }))

  await waitFor(() => expect(updateCalls()).toHaveLength(1))
  expect(updateCalls()[0][1]).toMatchObject({ userId: 'person-a', pronoun: 'she' })
})

it('shows the pronoun the person has, and clears it when they choose Not set', async () => {
  answerWith('they')
  renderPage()

  const field = await screen.findByRole('combobox', { name: 'Pronoun' })
  expect(field.textContent).toContain('They')

  fireEvent.click(field)
  fireEvent.click(screen.getByRole('option', { name: 'Not set' }))
  fireEvent.click(screen.getByRole('button', { name: 'Save' }))

  await waitFor(() => expect(updateCalls()).toHaveLength(1))
  expect(updateCalls()[0][1]).toMatchObject({ pronoun: '' })
})
