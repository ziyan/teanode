import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'
import { graphql } from '../api'
import { ToastProvider } from '../components/toast'
import { TranslationProvider } from '../i18n/i18n'
import { SetupPage } from './serviceSetup'

vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  graphql: vi.fn(),
}))
afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})

const none = {
  isMailRoutingEnabled: false,
  isMailboxEnabled: false,
  isCalendarEnabled: false,
  isRemindersEnabled: false,
  isContactsEnabled: false,
  isPhotosEnabled: false,
  isAssistantEnabled: false,
  hasPendingServiceRestart: false,
  hasCompletedSetup: false,
}

// answer fakes the server: which services are chosen, which of them run, and
// the settings that wait for a restart.
function answer(options: {
  selected?: Partial<typeof none>
  running?: Partial<typeof none>
  pendingRestart?: string[]
  hasModelProvider?: boolean
}) {
  const selected = { ...none, ...options.selected }
  const running = {
    ...none,
    ...options.running,
    hasPendingServiceRestart: (options.pendingRestart ?? []).length > 0,
  }
  vi.mocked(graphql).mockImplementation(async (document) => {
    if (document.includes('GetServiceSetup'))
      return {
        GetServiceSetup: {
          selectedServices: selected,
          runningServices: running,
          hasModelProvider: options.hasModelProvider ?? false,
          hasMailDomain: false,
          mailServerName: '',
        },
        GetServerStatus: { pendingRestart: options.pendingRestart ?? [] },
      } as never
    if (document.includes('ReadAgent')) return { ReadAgent: { agent: null, allowed: { enabled: false } } } as never
    throw new Error('Unexpected fixture query')
  })
}

function renderSetup(entry = '/manage/server/setup') {
  return render(
    <TranslationProvider>
      <ToastProvider>
        <MemoryRouter initialEntries={[entry]}>
          <SetupPage />
        </MemoryRouter>
      </ToastProvider>
    </TranslationProvider>,
  )
}

const step = (name: string) => screen.getAllByText(name)[0].closest('.settings-row') as HTMLElement

// Photos runs on a new server without being chosen, so it alone does not make
// the assistant optional; once setup is finished without one, it is.
it('keeps the assistant steps to do on a new server, where Photos runs by default', async () => {
  answer({ selected: { isPhotosEnabled: true }, running: { isPhotosEnabled: true } })
  renderSetup()
  await screen.findByText(/Photos is running/)
  expect(within(step('Connect a model provider and test it')).getByText('To do')).toBeTruthy()
  expect(within(step('Open Photos')).getByText('To do')).toBeTruthy()
})

it('does not ask a finished server with only Photos to connect a model', async () => {
  answer({ selected: { isPhotosEnabled: true, hasCompletedSetup: true }, running: { isPhotosEnabled: true } })
  renderSetup()
  await screen.findByText(/Photos is running/)
  expect(screen.queryByText('Connect a model provider and test it')).toBeNull()
  expect(screen.queryByText('Enable the assistant')).toBeNull()
  expect(within(step('Add the personal assistant')).getByText('Optional')).toBeTruthy()
  expect(within(step('Open Photos')).getByText('To do')).toBeTruthy()
  expect(within(step('Restart the server')).getByText('Done')).toBeTruthy()
})

it('does not ask a server with calendars and no assistant to connect a model', async () => {
  answer({ selected: { isCalendarEnabled: true }, running: { isCalendarEnabled: true } })
  renderSetup()
  await screen.findByText('Add the personal assistant')
  expect(screen.queryByText('Connect a model provider and test it')).toBeNull()
  expect(within(step('Restart the server')).getByText('Done')).toBeTruthy()
})

it('says which services start after a restart, with the button for it', async () => {
  answer({ selected: { isPhotosEnabled: true }, pendingRestart: ['services'] })
  renderSetup()
  await screen.findByText('Open Photos')
  expect(within(step('Restart the server')).getByText('Photos will be available after a restart.')).toBeTruthy()
  expect(within(step('Restart the server')).getByRole('button', { name: 'Restart now' })).toBeTruthy()
  expect(within(step('Open Photos')).getByText('After the steps above')).toBeTruthy()
  expect(screen.queryByText(/Waiting for a restart/)).toBeNull()
})

it('keeps the assistant steps to do on a server that has only the assistant', async () => {
  answer({})
  renderSetup()
  await screen.findByText('Connect a model provider and test it')
  expect(within(step('Connect a model provider and test it')).getByText('To do')).toBeTruthy()
  expect(screen.queryByText('Open Photos')).toBeNull()
})

it('ticks Photos once when sent here from the welcome page', async () => {
  answer({})
  renderSetup('/manage/server/setup?choose=photos')
  const photos = (await screen.findByText('Photos')).closest('label')!.querySelector('input')!
  expect(photos.checked).toBe(true)
  expect(screen.getByText('Review your choices')).toBeTruthy()
})

it('shows the note about keeping data only when something is turned off', async () => {
  answer({ selected: { isCalendarEnabled: true, isPhotosEnabled: true }, running: { isCalendarEnabled: true } })
  renderSetup()
  const photos = (await screen.findByText('Photos')).closest('label')!.querySelector('input')!
  fireEvent.click(screen.getByRole('button', { name: 'Review your choices' }))
  expect(screen.queryByText(/Turning a service off keeps its data/)).toBeNull()
  fireEvent.click(photos)
  fireEvent.click(screen.getByRole('button', { name: 'Review your choices' }))
  expect(screen.getByText(/Turning a service off keeps its data/)).toBeTruthy()
})
