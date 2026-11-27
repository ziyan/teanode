import { cleanup, render, screen } from '@testing-library/react'
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

let isObserved = true

function answer() {
  vi.mocked(graphql).mockImplementation(async (document) => {
    if (document.includes('allowed {')) {
      return {
        ReadAgent: { agent: { id: 'agent-one', enabled: true, name: 'Robin' }, allowed: { enabled: true, ask: true } },
      } as never
    }
    if (document.includes('ListAgentConversations')) return { ListAgentConversations: [mainConversation] } as never
    if (document.includes('ReadAgentConversation(')) {
      return {
        ReadAgentConversation: {
          conversation: mainConversation,
          actingAs: isObserved ? 'sample-owner' : null,
          isObserved,
          messages: [storedMessage('01J0000000000000000000005', 'a line the owner wrote')],
          total: 1,
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
  localStorage.setItem('teanode.agent.drawer', '1')
  localStorage.setItem('teanode.agent.conversation', 'main-conversation')
  vi.mocked(subscribe).mockImplementation((() => () => undefined) as never)
  isObserved = true
  answer()
})
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  vi.resetAllMocks()
})

it("puts the observed note where the composer would be in somebody else's conversation", async () => {
  renderDrawer()
  expect(
    await screen.findByText("This is sample-owner's conversation. You are observing it and cannot write here."),
  ).toBeTruthy()
  expect(await screen.findByText('a line the owner wrote')).toBeTruthy()
  expect(screen.queryByRole('textbox')).toBeNull()
})

it("keeps the composer in the reader's own conversation", async () => {
  isObserved = false
  renderDrawer()
  expect(await screen.findByRole('textbox')).toBeTruthy()
  expect(screen.queryByText(/You are observing it/)).toBeNull()
})
