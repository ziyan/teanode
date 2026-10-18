import { cleanup, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'
import { graphql } from '../api'
import { TranslationProvider } from '../i18n/i18n'
import { SessionProvider } from '../session'
import type { Session } from '../api'
import { ServiceGate } from './serviceGate'
import { ToastProvider } from './toast'

vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  graphql: vi.fn(),
}))
afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})

const services = {
  isMailRoutingEnabled: false,
  isMailboxEnabled: false,
  isCalendarEnabled: false,
  isRemindersEnabled: false,
  isContactsEnabled: false,
  isPhotosEnabled: false,
  isPhotosChosen: false,
  isAssistantEnabled: false,
  hasPendingServiceRestart: false,
  hasCompletedSetup: false,
}

function renderGate(session: Partial<Session>, isChosen: boolean) {
  vi.mocked(graphql).mockResolvedValue({
    GetServiceSetup: { selectedServices: { ...services, isPhotosEnabled: isChosen } },
  } as never)
  return render(
    <TranslationProvider>
      <ToastProvider>
        <SessionProvider
          value={{ authenticated: true, username: 'fixture', passkeysEnabled: false, ...session } as Session}
        >
          <MemoryRouter initialEntries={['/photos']}>
            <ServiceGate>
              <p>the page</p>
            </ServiceGate>
          </MemoryRouter>
        </SessionProvider>
      </ToastProvider>
    </TranslationProvider>,
  )
}

const operator = { userId: 'user-1', permissions: { everywhere: ['server:manage'], byDomain: [] } }

// The page of a service that is chosen and not running says that a restart
// starts it, to the operator who can restart; everybody else is told it is off.
it('tells an operator the service is chosen and starts after a restart', async () => {
  renderGate({ ...operator, services: { ...services, hasPendingServiceRestart: true } }, true)
  expect(await screen.findByText('Photos will be available after a restart')).toBeTruthy()
  expect(screen.getByRole('button', { name: 'Restart now' })).toBeTruthy()
  expect(screen.getByRole('link', { name: 'Service setup' })).toBeTruthy()
})

it('tells an operator a service that is not chosen is off, and where to choose it', async () => {
  renderGate({ ...operator, services }, false)
  expect(await screen.findByText('This service is off')).toBeTruthy()
  expect(screen.getByRole('link', { name: 'Service setup' })).toBeTruthy()
})

it('tells anybody else the service is off, with nowhere to restart', async () => {
  renderGate({ userId: 'user-2', permissions: { everywhere: ['photos:use'], byDomain: [] }, services }, true)
  expect(await screen.findByText('This service is off')).toBeTruthy()
  expect(screen.queryByRole('button', { name: 'Restart now' })).toBeNull()
  expect(screen.queryByRole('link', { name: 'Service setup' })).toBeNull()
})
