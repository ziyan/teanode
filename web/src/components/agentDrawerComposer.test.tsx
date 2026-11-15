import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql, subscribe } from '../api'
import { TranslationProvider } from '../i18n/i18n'
import { SessionProvider } from '../session'
import { AgentDrawer } from './agentDrawer'
import { ToastProvider } from './toast'

// The drawer asks the window about the screen when it loads, which jsdom
// does not answer.
vi.hoisted(() => {
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: () => ({
      matches: false,
      addEventListener() {},
      removeEventListener() {},
      addListener() {},
      removeListener() {},
    }),
  })
})
vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  graphql: vi.fn(),
  subscribe: vi.fn(),
}))

const mainConversation = {
  id: 'main-conversation',
  kind: 'main',
  title: '',
  summary: '',
  lastAt: '2030-01-01T09:00:00Z',
  chat: null,
}

function storedMessage(id: string, content: string) {
  return {
    id,
    createdAt: '2030-01-01T10:00:00Z',
    role: 'user',
    content,
    name: null,
    toolCallId: null,
    toolCalls: null,
    usage: null,
    attachments: null,
    references: null,
    reactions: null,
    isVoiceMessage: false,
    transcriptText: '',
    transcriptState: '',
    authorKind: 'person',
    authorUserId: 'person-a',
    authorName: 'Mira',
    authorAvatarUrl: null,
    authorAvatarColorIndex: 4,
    authorAgentId: null,
    isMine: true,
    editedAt: null,
    isDeleted: false,
  }
}

// What the server answers for a conversation holding `total` messages, of
// which the newest two are the first page.
let earlierAnswer: () => Promise<unknown>
const total = 5

function answer() {
  vi.mocked(graphql).mockImplementation(async (document, variables) => {
    if (document.includes('allowed {')) {
      return {
        ReadAgent: { agent: { id: 'agent-one', enabled: true, name: 'Robin' }, allowed: { enabled: true, ask: true } },
      } as never
    }
    if (document.includes('ListAgentConversations')) return { ListAgentConversations: [mainConversation] } as never
    if (document.includes('ReadAgentConversation(')) {
      const offset = (variables as { offset?: number } | undefined)?.offset
      if (offset) return (await earlierAnswer()) as never
      return {
        ReadAgentConversation: {
          conversation: mainConversation,
          actingAs: null,
          messages: [
            storedMessage('01J0000000000000000000004', 'fourth'),
            storedMessage('01J0000000000000000000005', 'fifth'),
          ],
          total,
          todos: [],
        },
        ListAgentInteractions: [],
      } as never
    }
    if (document.includes('ReadAgentVoice')) return { ReadAgentVoice: { isVoiceAvailable: false } } as never
    if (document.includes('ListAgentIdeas')) return { ListAgentIdeas: { ideas: [] } } as never
    if (document.includes('ReadAgentWaiting'))
      return { ReadAgentWaiting: { waitingCount: 0, interactions: [] } } as never
    if (document.includes('ReadAgentTab')) {
      return {
        ReadAgentTab: { attached: false },
        ReadAgentComputers: { computers: [] },
        ReadAgent: { budget: null, timezone: 'UTC' },
      } as never
    }
    return {} as never
  })
}

function earlierPage(count: number) {
  return {
    ReadAgentConversation: {
      messages: Array.from({ length: count }, (_, index) =>
        storedMessage(`01J000000000000000000000${index + 1}`, `older ${index + 1}`),
      ),
      total,
    },
  }
}

function renderDrawer() {
  return render(
    <TranslationProvider>
      <ToastProvider>
        <SessionProvider
          value={{
            userId: 'person-a',
            authenticated: true,
            authenticationRequired: true,
            username: 'person-a-login',
            name: 'Mira',
            passkeysEnabled: false,
          }}
        >
          <MemoryRouter>
            <AgentDrawer />
          </MemoryRouter>
        </SessionProvider>
      </ToastProvider>
    </TranslationProvider>,
  )
}

const earlierCalls = () =>
  vi.mocked(graphql).mock.calls.filter(([document, variables]) => {
    return document.includes('ReadAgentConversation(') && Boolean((variables as { offset?: number })?.offset)
  })

beforeEach(() => {
  localStorage.clear()
  localStorage.setItem('teanode.agent.drawer', '1')
  localStorage.setItem('teanode.agent.conversation', 'main-conversation')
  vi.mocked(subscribe).mockImplementation((() => () => undefined) as never)
  earlierAnswer = async () => earlierPage(3)
  answer()
})
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  vi.resetAllMocks()
})

// The composer measures its field and the draft on one line. jsdom lays
// nothing out, so the widths are given: a field 200 pixels across and 8
// pixels for each character of the draft.
function measureComposer() {
  vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockImplementation(function (this: HTMLElement) {
    return this instanceof HTMLTextAreaElement ? 200 : 0
  })
  vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockImplementation(function (this: HTMLElement) {
    return this.classList.contains('agent-composer-mirror') ? (this.textContent ?? '').length * 8 : 0
  })
}

const composerForm = (container: HTMLElement) => container.querySelector('form.agent-composer') as HTMLFormElement

it('is a pill at rest, becomes a card when the draft would wrap, and keeps the same field', async () => {
  measureComposer()
  const { container } = renderDrawer()
  const field = (await screen.findByRole('textbox')) as HTMLTextAreaElement
  expect(composerForm(container).className).not.toContain('is-card')

  fireEvent.change(field, { target: { value: 'short' } })
  expect(composerForm(container).className).not.toContain('is-card')

  fireEvent.change(field, { target: { value: 'a draft that is far too long to fit on one line' } })
  await waitFor(() => expect(composerForm(container).className).toContain('is-card'))
  // Not remounted: focus and the caret live in this element.
  expect(screen.getByRole('textbox')).toBe(field)

  fireEvent.change(field, { target: { value: 'short' } })
  await waitFor(() => expect(composerForm(container).className).not.toContain('is-card'))
  expect(screen.getByRole('textbox')).toBe(field)
})

it('is a card while files wait to be sent, and a pill again when they are removed', async () => {
  const { container } = renderDrawer()
  const field = await screen.findByRole('textbox')
  const picker = container.querySelector('input[type="file"]') as HTMLInputElement
  fireEvent.change(picker, { target: { files: [new File(['x'], 'notes.txt', { type: 'text/plain' })] } })
  await waitFor(() => expect(composerForm(container).className).toContain('is-card'))
  expect(screen.getByText('notes.txt')).toBeTruthy()
  expect(screen.getByRole('textbox')).toBe(field)

  fireEvent.click(screen.getByRole('button', { name: 'notes.txt: Remove' }))
  await waitFor(() => expect(composerForm(container).className).not.toContain('is-card'))
})

it('says what is above, and loads nothing when the top is reached', async () => {
  const { container } = renderDrawer()
  expect(await screen.findByText('Pull down for earlier messages')).toBeTruthy()
  const transcript = container.querySelector('.agent-drawer-transcript') as HTMLElement
  transcript.scrollTop = 0
  fireEvent.scroll(transcript)
  await new Promise((resolve) => setTimeout(resolve, 50))
  expect(earlierCalls()).toHaveLength(0)
})

it('arms a pull at 72 pixels, disarms below 56, and loads a page of 30 on release', async () => {
  const { container } = renderDrawer()
  await screen.findByText('Pull down for earlier messages')
  const transcript = container.querySelector('.agent-drawer-transcript') as HTMLElement

  fireEvent.touchStart(transcript, { touches: [{ clientY: 100 }] })
  fireEvent.touchMove(transcript, { touches: [{ clientY: 150 }] })
  expect(screen.getByText('Pull down for earlier messages')).toBeTruthy()
  fireEvent.touchMove(transcript, { touches: [{ clientY: 175 }] })
  expect(screen.getByText('Release to load earlier messages')).toBeTruthy()
  // Still armed between the two distances.
  fireEvent.touchMove(transcript, { touches: [{ clientY: 160 }] })
  expect(screen.getByText('Release to load earlier messages')).toBeTruthy()
  fireEvent.touchMove(transcript, { touches: [{ clientY: 150 }] })
  expect(screen.getByText('Pull down for earlier messages')).toBeTruthy()

  // A pull that is let go before it armed loads nothing.
  fireEvent.touchEnd(transcript)
  expect(earlierCalls()).toHaveLength(0)

  fireEvent.touchStart(transcript, { touches: [{ clientY: 100 }] })
  fireEvent.touchMove(transcript, { touches: [{ clientY: 180 }] })
  fireEvent.touchEnd(transcript)
  await waitFor(() => expect(earlierCalls()).toHaveLength(1))
  expect(earlierCalls()[0][1]).toMatchObject({ first: 30, offset: 2 })
  expect(await screen.findByText('Start of the conversation')).toBeTruthy()
})

it('shows a spinner while loading, then a reason with Try again when it fails, with no toast', async () => {
  let fail = true
  earlierAnswer = async () => {
    if (fail) throw new Error('The server is away')
    return earlierPage(3)
  }
  renderDrawer()
  fireEvent.click(await screen.findByRole('button', { name: 'Load earlier messages' }))
  expect(await screen.findByText('The server is away')).toBeTruthy()
  expect(screen.queryByRole('status')?.textContent ?? '').not.toContain('The server is away')

  fail = false
  fireEvent.click(screen.getByRole('button', { name: 'Try again' }))
  expect(await screen.findByText('Start of the conversation')).toBeTruthy()
  expect(screen.queryByText('The server is away')).toBeNull()
})

it('says loading while a page is on its way', async () => {
  let finish: (page: unknown) => void = () => undefined
  earlierAnswer = () => new Promise((resolve) => (finish = resolve))
  renderDrawer()
  fireEvent.click(await screen.findByRole('button', { name: 'Load earlier messages' }))
  expect(await screen.findByText('Loading earlier messages…')).toBeTruthy()
  finish(earlierPage(3))
  expect(await screen.findByText('Start of the conversation')).toBeTruthy()
})
