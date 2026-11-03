import { act, cleanup, render, screen } from '@testing-library/react'
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

const OPEN_KEY = 'teanode.agent.drawer'
const CONVERSATION_KEY = 'teanode.agent.conversation'

const mainConversation = {
  id: 'main-conversation',
  kind: 'main',
  title: '',
  summary: '',
  lastAt: '2030-01-01T09:00:00Z',
  chat: null,
}

const idea = {
  id: 'idea-one',
  ideaKind: 'suggestion',
  ideaCategory: 'mail',
  emoji: null,
  headline: 'Sort the mail from this week',
  body: null,
  openingRequest: 'Sort my mail from this week.',
  suggestionReason: '',
  evidence: [],
  ideaStatus: 'open',
  expiredReason: '',
  startedConversationId: null,
  createdAt: '2030-01-01T08:00:00Z',
  shownAt: null,
  startedAt: null,
  closedAt: null,
}

const sentMessage = {
  id: 'message-one',
  createdAt: '2030-01-01T09:00:00Z',
  role: 'user',
  content: 'What is on my calendar tomorrow?',
  runId: null,
  attachments: [],
  references: [],
  reactions: [],
}

// The conversation's read waits for the test to finish it.
function answerWith(readConversation: () => Promise<unknown>) {
  vi.mocked(graphql).mockImplementation(async (document) => {
    if (document.includes('allowed {')) {
      return {
        ReadAgent: { agent: { id: 'agent-one', enabled: true, name: 'Robin' }, allowed: { enabled: true, ask: true } },
      } as never
    }
    if (document.includes('ListAgentConversations')) return { ListAgentConversations: [mainConversation] } as never
    if (document.includes('ReadAgentConversation(')) return (await readConversation()) as never
    if (document.includes('ListAgentIdeas')) return { ListAgentIdeas: { ideas: [idea], ideaCategories: [] } } as never
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

function conversationRead(messages: unknown[]) {
  return {
    ReadAgentConversation: {
      conversation: mainConversation,
      actingAs: null,
      messages,
      total: messages.length,
      todos: [],
    },
    ListAgentInteractions: [],
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

beforeEach(() => {
  localStorage.clear()
  localStorage.setItem(OPEN_KEY, '1')
  localStorage.setItem(CONVERSATION_KEY, 'main-conversation')
  vi.mocked(subscribe).mockImplementation((() => () => undefined) as never)
})
afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})

it('offers no ideas while the conversation is still being read, and none once it has messages', async () => {
  let finishRead: (read: unknown) => void = () => {}
  answerWith(() => new Promise((resolve) => (finishRead = resolve)))
  renderDrawer()

  // Everything else has answered; the conversation has not.
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 50))
  })
  expect(screen.queryByText('Sort the mail from this week')).toBeNull()
  expect(screen.queryByText(/Ask about anything, or ask for something to be done/)).toBeNull()

  await act(async () => finishRead(conversationRead([sentMessage])))

  expect(await screen.findByText('What is on my calendar tomorrow?')).toBeTruthy()
  expect(screen.queryByText('Sort the mail from this week')).toBeNull()
})

it('offers the ideas once the conversation has been read and is empty', async () => {
  answerWith(async () => conversationRead([]))
  renderDrawer()

  expect(await screen.findByText('Sort the mail from this week')).toBeTruthy()
  expect(screen.getByText(/Ask about anything, or ask for something to be done/)).toBeTruthy()
})
