import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql, subscribe } from '../api'
import { callOf, fakeSession, memberOf } from '../calls/chatCallFixtures'
import { ChatCallProvider } from '../calls/chatCallProvider'
import type { ChatCall } from '../calls/chatCallApi'
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

function member(userId: string, name: string) {
  return {
    userId,
    username: `${userId}-login`,
    name,
    avatarUrl: null,
    avatarColorIndex: 4,
    memberRole: 'member',
    isDeletedAccount: false,
    isRemoved: false,
    readThroughMessageId: null,
    isAgentReading: false,
    agentId: '',
    agentName: '',
    isAgentActive: false,
    isAgentAskableByOthers: false,
  }
}

const chatConversation = {
  id: 'chat-conversation',
  kind: 'chat',
  title: '',
  summary: '',
  lastAt: '2030-01-01T10:00:00Z',
  archivedAt: null,
  chat: {
    chatId: 'chat-one',
    chatKind: 'direct',
    circleId: null,
    title: '',
    isCircleChat: false,
    unreadCount: 0,
    notificationLevel: 'all',
    isRemoved: false,
    members: [member('person-a', 'Mira'), member('person-b', 'Tobias')],
  },
}
// The same chat as a group of three.
const groupConversation = {
  ...chatConversation,
  chat: {
    ...chatConversation.chat,
    chatKind: 'group',
    members: [member('person-a', 'Mira'), member('person-b', 'Tobias'), member('person-c', 'Priya')],
  },
}
const mainConversation = {
  id: 'main-conversation',
  kind: 'main',
  title: '',
  summary: '',
  lastAt: '2030-01-01T09:00:00Z',
  chat: null,
}

// The line of a call in the chat: no words, the call it names, and the call
// as the message came with it.
function callLine(overrides: Record<string, unknown> = {}) {
  return {
    id: '01J0000000000000000000002',
    createdAt: '2030-01-01T10:00:00Z',
    role: 'assistant',
    content: '',
    name: null,
    toolCallId: null,
    toolCalls: null,
    usage: null,
    attachments: null,
    references: null,
    reactions: null,
    authorKind: 'person',
    authorUserId: 'person-b',
    authorName: 'Tobias',
    authorAvatarUrl: null,
    authorAvatarColorIndex: 4,
    authorAgentId: null,
    isMine: false,
    editedAt: null,
    isDeleted: false,
    callId: 'call-one',
    call: {
      id: 'call-one',
      startedAt: '2030-01-01T10:00:00Z',
      answeredAt: null,
      endedAt: '2030-01-01T10:01:00Z',
      endReason: 'missed',
      transcriptState: '',
      members: [],
    },
    ...overrides,
  }
}

interface Shown {
  messages?: unknown[]
  going?: ChatCall | null
  isGroup?: boolean
}

function answerWith(shown: Shown = {}) {
  const messages = shown.messages ?? []
  const chatShown = shown.isGroup ? groupConversation : chatConversation
  vi.mocked(graphql).mockImplementation((async (document: string, variables: Record<string, unknown> = {}) => {
    if (document.includes('allowed {')) {
      return {
        ReadAgent: { agent: { id: 'agent-one', enabled: true, name: 'Robin' }, allowed: { enabled: true, ask: true } },
      }
    }
    if (document.includes('ListAgentConversations')) return { ListAgentConversations: [mainConversation, chatShown] }
    if (document.includes('ReadAgentConversation(')) {
      if (variables.conversationId === 'chat-conversation') {
        return {
          ReadAgentConversation: {
            conversation: chatShown,
            actingAs: null,
            messages,
            total: messages.length,
            todos: [],
          },
          ListAgentInteractions: [],
        }
      }
      return {
        ReadAgentConversation: { conversation: mainConversation, actingAs: null, messages: [], total: 0, todos: [] },
        ListAgentInteractions: [],
      }
    }
    if (document.includes('ReadGoingChatCall(')) return { ReadGoingChatCall: shown.going ?? null }
    if (document.includes('ListGoingChatCalls')) return { ListGoingChatCalls: [] }
    if (document.includes('StartChatCall('))
      return {
        StartChatCall: callOf({
          askerRingState: 'none',
          isAskerOn: true,
          isGoing: true,
          members: [memberOf('person-a', 'Mira', { isOn: true })],
        }),
      }
    if (document.includes('ListAgentIdeas')) return { ListAgentIdeas: { ideas: [] } }
    if (document.includes('ReadAgentWaiting')) return { ReadAgentWaiting: { waitingCount: 0, interactions: [] } }
    if (document.includes('ReadAgentTab')) {
      return {
        ReadAgentTab: { attached: false },
        ReadAgentComputers: { computers: [] },
        ReadAgent: { budget: null, timezone: 'UTC' },
      }
    }
    return {}
  }) as never)
}

// The handlers of the subscriptions the page opened.
const feeds: Record<string, (data: unknown) => void> = {}
let callEvents: (data: unknown) => void = () => {}

function renderDrawer(hasCalls: boolean) {
  const drawer = (
    <MemoryRouter>
      <AgentDrawer />
    </MemoryRouter>
  )
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
          {hasCalls ? (
            <ChatCallProvider isSupported createSession={() => fakeSession()}>
              {drawer}
            </ChatCallProvider>
          ) : (
            drawer
          )}
        </SessionProvider>
      </ToastProvider>
    </TranslationProvider>,
  )
}

beforeEach(() => {
  localStorage.clear()
  for (const key of Object.keys(feeds)) delete feeds[key]
  vi.mocked(subscribe).mockImplementation(((
    query: string,
    variables: { conversationId: string },
    onData: (data: unknown) => void,
  ) => {
    if (query.includes('ChatCallEvents')) callEvents = onData
    else feeds[variables.conversationId] = onData
    return () => undefined
  }) as never)
})
afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})

function openOnChat() {
  localStorage.setItem(OPEN_KEY, '1')
  localStorage.setItem(CONVERSATION_KEY, 'chat-conversation')
}

it('starts a voice call from the menu of the call button in the chat header, and opens the call screen', async () => {
  answerWith()
  openOnChat()
  renderDrawer(true)

  fireEvent.click(await screen.findByRole('button', { name: 'Start a call' }))
  // A direct chat has nobody to choose.
  expect(screen.queryByRole('menuitem', { name: 'Choose who to call' })).toBeNull()
  fireEvent.click(screen.getByRole('menuitem', { name: 'Voice call' }))

  expect(await screen.findByRole('dialog', { name: 'Call in Trip planning' })).toBeTruthy()
  expect(vi.mocked(graphql)).toHaveBeenCalledWith(expect.stringContaining('StartChatCall('), {
    conversationId: 'chat-conversation',
    isVideoOn: false,
    userIds: null,
  })
  // Already on a call in this chat: no second button.
  expect(screen.queryByRole('button', { name: 'Start a call' })).toBeNull()
})

it('starts a video call from the same menu', async () => {
  answerWith()
  openOnChat()
  renderDrawer(true)

  fireEvent.click(await screen.findByRole('button', { name: 'Start a call' }))
  fireEvent.click(screen.getByRole('menuitem', { name: 'Video call' }))

  await screen.findByRole('dialog', { name: 'Call in Trip planning' })
  expect(vi.mocked(graphql)).toHaveBeenCalledWith(expect.stringContaining('StartChatCall('), {
    conversationId: 'chat-conversation',
    isVideoOn: true,
    userIds: null,
  })
})

it('rings only the people chosen in a group, everybody else chosen to begin with', async () => {
  answerWith({ isGroup: true })
  openOnChat()
  renderDrawer(true)

  fireEvent.click(await screen.findByRole('button', { name: 'Start a call' }))
  fireEvent.click(screen.getByRole('menuitem', { name: 'Choose who to call' }))
  const chooser = await screen.findByRole('alertdialog', { name: 'Who to call' })
  // Mira is the person signed in, so is not among those to call.
  expect(within(chooser).queryByRole('checkbox', { name: /Mira/ })).toBeNull()
  fireEvent.click(within(chooser).getByRole('checkbox', { name: /Tobias/ }))
  fireEvent.click(within(chooser).getByRole('button', { name: 'Voice call' }))

  await screen.findByRole('dialog', { name: 'Call in Trip planning' })
  expect(vi.mocked(graphql)).toHaveBeenCalledWith(expect.stringContaining('StartChatCall('), {
    conversationId: 'chat-conversation',
    isVideoOn: false,
    userIds: ['person-c'],
  })
})

it('offers no call button where nothing provides calls', async () => {
  answerWith()
  openOnChat()
  renderDrawer(false)

  await screen.findByRole('button', { name: 'Chat menu' })
  expect(screen.queryByRole('button', { name: 'Start a call' })).toBeNull()
})

it('offers no call button in the conversation with the agent', async () => {
  answerWith()
  localStorage.setItem(OPEN_KEY, '1')
  localStorage.setItem(CONVERSATION_KEY, 'main-conversation')
  renderDrawer(true)

  await screen.findByLabelText('Robin')
  expect(screen.queryByRole('button', { name: 'Start a call' })).toBeNull()
})

it('shows a banner to join when the chat has a call going, and takes it away when it ends', async () => {
  const going = callOf({
    askerRingState: '',
    members: [memberOf('person-b', 'Tobias', { ringState: 'none', isOn: true })],
  })
  answerWith({ going })
  openOnChat()
  renderDrawer(true)

  expect(await screen.findByText('Call going, 1 on it')).toBeTruthy()
  expect(screen.getByRole('button', { name: 'Join' })).toBeTruthy()

  act(() =>
    callEvents({ ChatCallEvents: { ...going, isGoing: false, endReason: 'ended', endedAt: '2030-01-01T10:05:00Z' } }),
  )
  await waitFor(() => expect(screen.queryByText('Call going, 1 on it')).toBeNull())
})

it('draws the line of a call in the transcript, and changes it as the call ends', async () => {
  answerWith({
    messages: [
      callLine({
        call: {
          id: 'call-one',
          startedAt: '2030-01-01T10:00:00Z',
          answeredAt: null,
          endedAt: null,
          endReason: '',
          transcriptState: '',
          members: [],
        },
      }),
    ],
  })
  openOnChat()
  const { container } = renderDrawer(true)

  const line = await waitFor(() => {
    const found = container.querySelector('.chat-call-line')
    expect(found).toBeTruthy()
    return found as HTMLElement
  })
  expect(within(line).getByText('Call going')).toBeTruthy()
  expect(within(line).getByRole('button', { name: 'Join' })).toBeTruthy()
  // It is the call's line and not a message: no author name over it, and no
  // bubble.
  expect(container.querySelector('.agent-chat-author')).toBeNull()

  act(() =>
    callEvents({
      ChatCallEvents: callOf({
        askerRingState: '',
        answeredAt: '2030-01-01T10:00:10Z',
        isGoing: false,
        endReason: 'ended',
        endedAt: '2030-01-01T10:03:22Z',
      }),
    }),
  )
  expect(await screen.findByText('Call, 3:12')).toBeTruthy()
})

it('draws a missed call from the message alone', async () => {
  answerWith({ messages: [callLine()] })
  openOnChat()
  renderDrawer(false)

  expect(await screen.findByText('Missed call')).toBeTruthy()
})
