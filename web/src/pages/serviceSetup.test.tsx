import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
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

it('has one Restart now, in the steps, once the choices are saved and a restart is waiting', async () => {
  answer({})
  renderSetup('/manage/server/setup?choose=photos')
  await waitFor(() => expect((screen.getByLabelText(/Photos/) as HTMLInputElement).checked).toBe(true))
  fireEvent.click(screen.getByRole('button', { name: 'Review your choices' }))
  // Once applied, the server says Photos is chosen and waits for a restart.
  answer({ selected: { isPhotosEnabled: true }, pendingRestart: ['services'] })
  const after = vi.mocked(graphql).getMockImplementation()!
  vi.mocked(graphql).mockImplementation(async (document, variables) =>
    document.includes('UpdateServices') ? ({ UpdateServices: none } as never) : after(document, variables),
  )
  fireEvent.click(screen.getByRole('button', { name: 'Apply choices' }))
  const saved = (await screen.findByText('Choices saved')).closest('[role="status"]') as HTMLElement
  expect(
    within(saved).getByText(/Photos will be available after a restart\. Restart the server in the steps below\./),
  ).toBeTruthy()
  expect(within(saved).queryByRole('button')).toBeNull()
  expect(screen.getAllByRole('button', { name: 'Restart now' })).toHaveLength(1)
  expect(within(step('Restart the server')).getByRole('button', { name: 'Restart now' })).toBeTruthy()
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

// Sent from the welcome page's photos choice, the person said they did not come
// for an assistant: its three steps are one optional one for as long as the
// page is open, even though the address forgets the choice at once.
it('makes the assistant optional for somebody sent here for their photos', async () => {
  answer({ running: { isPhotosEnabled: true }, selected: { isPhotosEnabled: true } })
  renderSetup('/manage/server/setup?choose=photos')
  await screen.findByText(/Photos is running/)
  expect(screen.queryByText('Connect a model provider and test it')).toBeNull()
  expect(screen.queryByText('Enable the assistant')).toBeNull()
  expect(screen.queryByText('Turn on your own agent')).toBeNull()
  expect(within(step('Add the personal assistant')).getByText('Optional')).toBeTruthy()
  expect(within(step('Restart the server')).getByText('Done')).toBeTruthy()
})

it('still asks for the assistant when somebody arrives without saying they came for photos', async () => {
  answer({ running: { isPhotosEnabled: true }, selected: { isPhotosEnabled: true } })
  renderSetup('/manage/server/setup')
  await screen.findByText(/Photos is running/)
  expect(within(step('Connect a model provider and test it')).getByText('To do')).toBeTruthy()
})

it('points the Photos step at the iPhone backup once Photos runs, and not before', async () => {
  answer({ running: { isPhotosEnabled: true }, selected: { isPhotosEnabled: true } })
  renderSetup('/manage/server/setup?choose=photos')
  await screen.findByText(/Photos is running/)
  const photosStep = step('Open Photos')
  expect(within(photosStep).getByRole('link', { name: 'Back up an iPhone' }).getAttribute('href')).toBe(
    '/settings/devices',
  )
  expect(within(photosStep).getByRole('link', { name: 'Open Photos' }).getAttribute('href')).toBe('/photos')
  cleanup()
  answer({ selected: { isPhotosEnabled: true }, pendingRestart: ['services'] })
  renderSetup('/manage/server/setup?choose=photos')
  await screen.findByText(/Photos can be opened once the server has restarted/)
  expect(screen.queryByRole('link', { name: 'Back up an iPhone' })).toBeNull()
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

// The server stores whatever it is sent as a choice, and Photos is ticked on a
// new server only because it is on by default. A save that sent it anyway would
// mark Photos chosen, and the sign-in would then skip the welcome page.
function updatesSent() {
  return vi
    .mocked(graphql)
    .mock.calls.filter(([document]) => document.includes('UpdateServices'))
    .map(([, variables]) => variables as Record<string, unknown>)
}

async function saveChoices() {
  const original = vi.mocked(graphql).getMockImplementation()!
  vi.mocked(graphql).mockImplementation(async (document, variables) =>
    document.includes('UpdateServices') ? ({ UpdateServices: none } as never) : original(document, variables),
  )
  fireEvent.click(screen.getByRole('button', { name: 'Review your choices' }))
  fireEvent.click(screen.getByRole('button', { name: 'Apply choices' }))
  await screen.findByText('Choices saved')
}

it('does not send Photos when saving other services on a new server', async () => {
  answer({ selected: { isPhotosEnabled: true }, running: { isPhotosEnabled: true } })
  renderSetup()
  await screen.findByText(/Photos is running/)
  const checkbox = (name: string) => screen.getByText(name, { selector: 'strong' }).closest('label')!.querySelector('input')!
  fireEvent.click(checkbox('Calendar'))
  fireEvent.click(checkbox('Contacts'))
  await saveChoices()
  const [variables] = updatesSent()
  expect(variables).toMatchObject({ isCalendarEnabled: true, isContactsEnabled: true })
  expect(variables).not.toHaveProperty('isPhotosEnabled')
})

it('sends Photos when the operator unticks or ticks it on this page', async () => {
  answer({ selected: { isPhotosEnabled: true }, running: { isPhotosEnabled: true } })
  renderSetup()
  const photos = (await screen.findByText('Photos', { selector: 'strong' })).closest('label')!.querySelector('input')!
  fireEvent.click(photos)
  await saveChoices()
  expect(updatesSent()[0]).toMatchObject({ isPhotosEnabled: false })
})

it('sends Photos from a preset that turns it off', async () => {
  answer({ selected: { isPhotosEnabled: true, isCalendarEnabled: true }, running: { isPhotosEnabled: true } })
  renderSetup()
  await screen.findByText(/Photos is running/)
  fireEvent.click(screen.getByRole('button', { name: 'Personal assistant' }))
  await saveChoices()
  expect(updatesSent()[0]).toMatchObject({ isPhotosEnabled: false })
})

it('sends Photos as chosen when sent here for them, even though it is already on', async () => {
  answer({ selected: { isPhotosEnabled: true }, running: { isPhotosEnabled: true } })
  renderSetup('/manage/server/setup?choose=photos')
  await screen.findByText(/Photos is running/)
  await saveChoices()
  expect(updatesSent()[0]).toMatchObject({ isPhotosEnabled: true })
})
