import { cleanup, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'
import { graphql } from '../api'
import { ToastProvider } from '../components/toast'
import { TranslationProvider } from '../i18n/i18n'
import { WelcomePage } from './welcome'

// The agent drawer, which the page imports, reads the color scheme when it loads.
vi.hoisted(() => {
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    addEventListener: () => {},
    removeEventListener: () => {},
  })) as unknown as typeof window.matchMedia
})

vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  graphql: vi.fn(),
}))
afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})

const none = {
  isMailboxEnabled: false,
  isCalendarEnabled: false,
  isRemindersEnabled: false,
  isContactsEnabled: false,
  isPhotosEnabled: false,
  hasPendingServiceRestart: false,
}

function renderWelcome(running: Partial<typeof none>) {
  vi.mocked(graphql).mockResolvedValue({ GetServiceCapabilities: { ...none, ...running } } as never)
  return render(
    <TranslationProvider>
      <ToastProvider>
        <MemoryRouter>
          <WelcomePage />
        </MemoryRouter>
      </ToastProvider>
    </TranslationProvider>,
  )
}

// Somebody who came for their photos should find them on the page that asks
// for a model key, not only the key.
it('offers the services that already run beside the model key', async () => {
  renderWelcome({ isPhotosEnabled: true, isCalendarEnabled: true })
  expect(await screen.findByRole('link', { name: 'Open Photos' })).toBeTruthy()
  expect(screen.getByRole('link', { name: 'Open Calendar' })).toBeTruthy()
  expect(screen.getByRole('button', { name: 'Start my assistant' })).toBeTruthy()
})

it('leads to service setup, with Photos ticked, when nothing runs', async () => {
  renderWelcome({})
  const link = await screen.findByRole('link', { name: 'Set up Photos and other services' })
  expect(link.getAttribute('href')).toBe('/manage/server/setup?choose=photos')
})

it('says chosen services wait for a restart', async () => {
  renderWelcome({ hasPendingServiceRestart: true })
  expect(await screen.findByRole('link', { name: 'Continue with service setup' })).toBeTruthy()
})
