import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
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

// Whether the chat's members have agents: the signed-in person's own, which
// is on, and Person B's, which Person B let the others ask. Set by answerWith.
let hasAgents = false

function person(userId: string, name: string, readThroughMessageId: string | null = null) {
  const agent =
    hasAgents && userId === 'person-a'
      ? { agentId: 'agent-robin', agentName: 'Robin', isAgentActive: true, isAgentAskableByOthers: false }
      : hasAgents && userId === 'person-b'
        ? { agentId: 'agent-benny', agentName: 'Benny', isAgentActive: true, isAgentAskableByOthers: true }
        : { agentId: '', agentName: '', isAgentActive: false, isAgentAskableByOthers: false }
  return {
    userId,
    username: `${userId}-login`,
    name,
    avatarUrl: null,
    avatarColorIndex: 4,
    memberRole: 'member',
    isDeletedAccount: false,
    isRemoved: false,
    readThroughMessageId,
    isAgentReading: false,
    ...agent,
  }
}

function chatOf(unreadCount: number, readByOther: string | null = null) {
  return {
    chatId: 'chat-one',
    chatKind: 'direct',
    circleId: null,
    title: '',
    isCircleChat: false,
    unreadCount,
    isMuted: false,
    mutedUntil: null,
    isRemoved: false,
    members: [person('person-a', 'Person A'), person('person-b', 'Person B', readByOther)],
  }
}

function chatMessage(id: string, content: string, isMine: boolean, overrides: Record<string, unknown> = {}) {
  return {
    id,
    createdAt: '2030-01-01T10:00:00Z',
    role: isMine ? 'user' : 'assistant',
    content,
    name: null,
    toolCallId: null,
    toolCalls: null,
    usage: null,
    attachments: null,
    references: null,
    reactions: null,
    authorKind: 'person',
    authorUserId: isMine ? 'person-a' : 'person-b',
    authorName: isMine ? 'Person A' : 'Person B',
    authorAvatarUrl: null,
    authorAvatarColorIndex: 4,
    authorAgentId: null,
    isMine,
    editedAt: null,
    isDeleted: false,
    ...overrides,
  }
}

// Two messages from Person B in a row, then one of the signed-in person's.
const MESSAGES = [
  chatMessage('01J0000000000000000000001', 'Dinner at seven?', false),
  chatMessage('01J0000000000000000000002', 'Bring the pie', false),
  chatMessage('01J0000000000000000000003', 'Sounds good', true),
]

interface Shown {
  isAgentOn?: boolean
  unreadCount?: number
  readByOther?: string | null
  hasChat?: boolean
  // Whether the person shares a circle with anybody.
  hasPeople?: boolean
  // A second chat, spoken in earlier than the first.
  hasOlderChat?: boolean
  // Whether that second chat is a circle's own.
  isOlderChatCircle?: boolean
  // Whether Person B has said one more thing since the chat was read.
  hasNewMessage?: boolean
  // Whether the members have agents the composer's @ can offer.
  hasAgents?: boolean
  // Whether the person’s own agent did something to answer: a call and its
  // result, as rows of their own conversation.
  hasWorkRows?: boolean
}

// answerWith makes the server: the agent on or off, the chat in the list,
// its messages. The main conversation exists only with the agent on, as it
// does on the real server.
function answerWith(shown: Shown = {}) {
  const {
    isAgentOn = true,
    unreadCount = 0,
    readByOther = null,
    hasChat = true,
    hasPeople = true,
    hasOlderChat = false,
    isOlderChatCircle = false,
    hasNewMessage = false,
  } = shown
  hasAgents = shown.hasAgents ?? false
  const work = shown.hasWorkRows
    ? [
        chatMessage('01J0000000000000000000005', '', true, {
          role: 'assistant',
          toolCalls: [{ id: 'call-1', name: 'calendar', arguments: '{}' }],
          authorKind: 'agent',
          authorAgentId: 'agent-robin',
          isAgentWork: true,
        }),
        chatMessage('01J0000000000000000000006', '{"events":[]}', true, {
          role: 'tool',
          toolCallId: 'call-1',
          name: 'calendar',
          authorKind: 'agent',
          authorAgentId: 'agent-robin',
          isAgentWork: true,
        }),
      ]
    : []
  const messages = hasNewMessage
    ? [...MESSAGES, chatMessage('01J0000000000000000000004', 'Running late', false), ...work]
    : [...MESSAGES, ...work]
  const chat = {
    id: 'chat-conversation',
    kind: 'chat',
    title: '',
    summary: '',
    lastAt: hasNewMessage ? '2030-01-01T11:00:00Z' : '2030-01-01T10:00:00Z',
    archivedAt: null,
    chat: chatOf(unreadCount, readByOther),
  }
  const main = {
    id: 'main-conversation',
    kind: 'main',
    title: '',
    summary: '',
    lastAt: '2030-01-01T09:00:00Z',
    chat: null,
  }
  const olderChat = {
    id: 'older-conversation',
    kind: 'chat',
    title: 'Weekend plans',
    summary: '',
    lastAt: '2029-12-31T10:00:00Z',
    archivedAt: null,
    chat: {
      ...chatOf(0),
      chatId: 'chat-older',
      chatKind: 'group',
      title: 'Weekend plans',
      isCircleChat: isOlderChatCircle,
      members: [person('person-a', 'Person A'), person('person-b', 'Person B'), person('person-c', 'Person C')],
    },
  }
  const listed = [...(isAgentOn ? [main] : []), ...(hasOlderChat ? [olderChat] : []), ...(hasChat ? [chat] : [])]
  vi.mocked(graphql).mockImplementation(async (document, variables) => {
    if (document.includes('allowed {')) {
      return {
        ReadAgent: {
          agent: isAgentOn ? { id: 'agent-one', enabled: true, name: 'Robin' } : null,
          allowed: { enabled: isAgentOn, ask: isAgentOn },
        },
      } as never
    }
    if (document.includes('ListAgentConversations')) return { ListAgentConversations: listed } as never
    if (document.includes('ListCirclePeople')) {
      return {
        ListCirclePeople: hasPeople
          ? [{ userId: 'person-b', username: 'person-b-login', name: 'Person B', avatarUrl: null, avatarColorIndex: 4 }]
          : [],
      } as never
    }
    if (document.includes('ReadAgentConversation(')) {
      const id = (variables as { conversationId?: string } | undefined)?.conversationId
      if (id === 'chat-conversation') {
        return {
          ReadAgentConversation: {
            conversation: { ...chat, chat: chatOf(unreadCount, readByOther) },
            actingAs: null,
            messages,
            total: messages.length,
            todos: [],
          },
          ListAgentInteractions: [],
        } as never
      }
      if (id === 'older-conversation') {
        return {
          ReadAgentConversation: { conversation: olderChat, actingAs: null, messages: [], total: 0, todos: [] },
          ListAgentInteractions: [],
        } as never
      }
      return {
        ReadAgentConversation: { conversation: main, actingAs: null, messages: [], total: 0, todos: [] },
        ListAgentInteractions: [],
      } as never
    }
    if (document.includes('AskAgent')) return { AskAgent: { runId: '', conversationId: 'chat-conversation' } } as never
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
    if (document.includes('ReadAgentVoice'))
      return { ReadAgentVoice: { isVoiceAvailable: false, sampleRate: 0 } } as never
    return {} as never
  })
}

// The handlers of the subscriptions the drawer opened, by conversation.
const feeds: Record<string, (data: unknown) => void> = {}

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
            name: 'Person A',
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

function tell(conversationId: string, event: Record<string, unknown>) {
  act(() => {
    feeds[conversationId]({
      AgentConversationEvents: { runId: '', sequence: 0, ...event },
    })
  })
}

beforeEach(() => {
  localStorage.clear()
  for (const key of Object.keys(feeds)) delete feeds[key]
  vi.mocked(subscribe).mockImplementation(((
    _query: string,
    variables: { conversationId: string },
    onData: (data: unknown) => void,
  ) => {
    feeds[variables.conversationId] = onData
    return () => undefined
  }) as never)
})
afterEach(() => {
  cleanup()
  vi.resetAllMocks()
  hasAgents = false
})

function openOnChat() {
  localStorage.setItem(OPEN_KEY, '1')
  localStorage.setItem(CONVERSATION_KEY, 'chat-conversation')
}

it('draws the other person once for a run of their messages, and the signed-in person on the right', async () => {
  answerWith()
  openOnChat()
  const { container } = renderDrawer()

  await screen.findByText('Dinner at seven?')
  expect(screen.getByText('Bring the pie')).toBeTruthy()
  // One name for the two messages in a row, with a face beside it.
  const names = container.querySelectorAll('.agent-chat-author')
  expect(names).toHaveLength(1)
  expect(names[0].textContent).toBe('Person B')
  expect(container.querySelectorAll('.agent-chat-turn:not(.mine) .avatar')).toHaveLength(1)
  // The person's own message is where it always was: on the right, with no
  // name or face of its own.
  const own = screen.getByText('Sounds good')
  expect(own.closest('.agent-chat-turn')?.classList.contains('mine')).toBe(true)
  expect(own.closest('.agent-line')?.classList.contains('user')).toBe(true)
  // No thinking dots and no suggested replies in a conversation of people.
  expect(screen.queryByRole('group', { name: 'Suggested replies' })).toBeNull()
})

it('shows the other person’s read mark under the signed-in person’s own newest message they have read', async () => {
  answerWith({ readByOther: '01J0000000000000000000003' })
  openOnChat()
  renderDrawer()

  await screen.findByText('Dinner at seven?')
  const marks = await screen.findByRole('img', { name: 'Read by Person B' })
  expect(marks.closest('.agent-chat-turn')?.classList.contains('mine')).toBe(true)
  expect(marks.closest('.agent-chat-turn')?.textContent).toContain('Sounds good')
})

it('never shows a person’s read mark under their own message', async () => {
  // Person B has read through their own second message and nothing of the
  // signed-in person's: there is no message of anybody else's to put it under.
  answerWith({ readByOther: '01J0000000000000000000002' })
  openOnChat()
  renderDrawer()

  await screen.findByText('Dinner at seven?')
  await waitFor(() =>
    expect(vi.mocked(graphql).mock.calls.some(([document]) => document.includes('MarkChatRead'))).toBe(true),
  )
  expect(screen.queryByRole('img', { name: /Read by/ })).toBeNull()
})

it('puts the number of unread messages on the button while the drawer is shut', async () => {
  answerWith({ unreadCount: 2 })
  renderDrawer()

  const button = await screen.findByRole('button', { name: /2 unread messages/ })
  await waitFor(() => expect(within(button).getByText('2')).toBeTruthy())
})

it('adds a message the chat says was said, and clears the typing line it ends', async () => {
  answerWith()
  openOnChat()
  renderDrawer()
  await screen.findByText('Dinner at seven?')
  await waitFor(() => expect(feeds['chat-conversation']).toBeTruthy())

  tell('chat-conversation', { kind: 'typing', userId: 'person-b', name: 'Person B' })
  expect(await screen.findByText('Person B is typing')).toBeTruthy()

  tell('chat-conversation', {
    kind: 'said',
    message: chatMessage('01J0000000000000000000004', 'See you there', false),
  })
  expect(await screen.findByText('See you there')).toBeTruthy()
  expect(screen.queryByText('Person B is typing')).toBeNull()
  // It follows the person's own reply, so it begins a run of Person B's
  // again and is named: two runs, two names.
  expect(document.querySelectorAll('.agent-chat-author')).toHaveLength(2)
})

it('takes the words of a turn that was stopped off the screen', async () => {
  answerWith()
  openOnChat()
  renderDrawer()
  await screen.findByText('Dinner at seven?')
  await waitFor(() => expect(feeds['chat-conversation']).toBeTruthy())

  tell('chat-conversation', {
    kind: 'text',
    runId: 'run-1',
    text: 'Thursday at',
    name: 'Robin',
    authorAgentId: 'agent-robin',
  })
  expect(await screen.findByText('Thursday at')).toBeTruthy()
  tell('chat-conversation', { kind: 'unsaid', messageId: 'streaming-run-1' })
  await waitFor(() => expect(screen.queryByText('Thursday at')).toBeNull())
  // Nothing was marked deleted: there was no message.
  expect(screen.queryByText('Message deleted')).toBeNull()
})

it('shows a deleted message as such and an edited one with its mark', async () => {
  answerWith()
  openOnChat()
  renderDrawer()
  await screen.findByText('Dinner at seven?')
  await waitFor(() => expect(feeds['chat-conversation']).toBeTruthy())

  tell('chat-conversation', { kind: 'unsaid', messageId: '01J0000000000000000000002' })
  expect(await screen.findByText('Message deleted')).toBeTruthy()
  expect(screen.queryByText('Bring the pie')).toBeNull()

  tell('chat-conversation', {
    kind: 'edited',
    message: chatMessage('01J0000000000000000000001', 'Dinner at eight?', false, {
      editedAt: '2030-01-01T10:05:00Z',
    }),
  })
  const edited = await screen.findByText('Dinner at eight?')
  expect(within(edited.closest('.agent-line') as HTMLElement).getByText('edited')).toBeTruthy()
})

it('sends a message into the chat without waiting on a run, and the chat’s own copy replaces it', async () => {
  answerWith()
  openOnChat()
  renderDrawer()
  await screen.findByText('Dinner at seven?')
  await waitFor(() => expect(feeds['chat-conversation']).toBeTruthy())

  const box = screen.getByRole('textbox', { name: 'Message Person B…' })
  fireEvent.change(box, { target: { value: 'On my way' } })
  fireEvent.submit(box.closest('form') as HTMLFormElement)

  await waitFor(() =>
    expect(vi.mocked(graphql).mock.calls.some(([document]) => document.includes('AskAgent'))).toBe(true),
  )
  const ask = vi.mocked(graphql).mock.calls.find(([document]) => document.includes('AskAgent'))
  expect(ask?.[1]).toMatchObject({ conversationId: 'chat-conversation', message: 'On my way' })
  expect(screen.getByText('On my way').closest('.agent-line')?.classList.contains('pending')).toBe(true)

  tell('chat-conversation', {
    kind: 'said',
    message: chatMessage('01J0000000000000000000005', 'On my way', true),
  })
  await waitFor(() => expect(screen.getAllByText('On my way')).toHaveLength(1))
  expect(screen.getByText('On my way').closest('.agent-line')?.classList.contains('pending')).toBe(false)
})

it('tells the server a chat on screen has been read through its newest message', async () => {
  answerWith({ unreadCount: 2 })
  openOnChat()
  renderDrawer()
  await screen.findByText('Dinner at seven?')

  await waitFor(() =>
    expect(vi.mocked(graphql).mock.calls.some(([document]) => document.includes('MarkChatRead'))).toBe(true),
  )
  const mark = vi.mocked(graphql).mock.calls.find(([document]) => document.includes('MarkChatRead'))
  expect(mark?.[1]).toEqual({ conversationId: 'chat-conversation', throughMessageId: '01J0000000000000000000003' })
})

it('opens on the chats when the agent is off, with the offer to turn it on in the main chat’s place', async () => {
  answerWith({ isAgentOn: false })
  localStorage.setItem(OPEN_KEY, '1')
  renderDrawer()

  // The newest chat is open, and the box writes to it.
  expect(await screen.findByText('Dinner at seven?')).toBeTruthy()
  expect(screen.getByRole('textbox', { name: 'Message Person B…' })).toBeTruthy()

  fireEvent.click(screen.getByRole('button', { name: 'Person B' }))
  fireEvent.click(await screen.findByRole('menuitem', { name: /Main chat/ }))
  expect(await screen.findByText(/Your agent is not on yet/)).toBeTruthy()
  expect(screen.getByRole('button', { name: 'Turn it on…' })).toBeTruthy()
  // Nothing to write to, so no box.
  expect(screen.queryByRole('textbox')).toBeNull()
})

it('offers the chats to a person with no chat yet and no agent, who shares a circle with someone', async () => {
  answerWith({ isAgentOn: false, hasChat: false })
  localStorage.setItem(OPEN_KEY, '1')
  renderDrawer()

  expect(await screen.findByText(/Your agent is not on yet/)).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'Main chat' }))
  expect(await screen.findByRole('menuitem', { name: 'Message someone' })).toBeTruthy()
  expect(screen.getByRole('menuitem', { name: 'New group' })).toBeTruthy()
  // A side chat is the agent's, and the agent is off.
  expect(screen.queryByRole('menuitem', { name: 'New side chat' })).toBeNull()
})

it('draws nothing for a person with no agent and nobody to write to', async () => {
  answerWith({ isAgentOn: false, hasChat: false })
  vi.mocked(graphql).mockImplementation(async (document) => {
    if (document.includes('allowed {'))
      return { ReadAgent: { agent: null, allowed: { enabled: false, ask: false } } } as never
    if (document.includes('ListCirclePeople')) return { ListCirclePeople: [] } as never
    return { ListAgentConversations: [] } as never
  })
  const { container } = renderDrawer()
  await waitFor(() =>
    expect(vi.mocked(graphql).mock.calls.some(([document]) => document.includes('ListCirclePeople'))).toBe(true),
  )
  expect(container.querySelector('.agent-drawer-toggle')).toBeNull()
})

it('says who can be messaged, and links to the circles, when choosing Message someone with nobody to message', async () => {
  // A chat that is there and nobody else to start one with, so the drawer
  // opens on the chat and the picker still offers the entry.
  answerWith({ hasPeople: false })
  openOnChat()
  renderDrawer()

  fireEvent.click(await screen.findByRole('button', { name: 'Person B' }))
  fireEvent.click(await screen.findByRole('menuitem', { name: 'Message someone' }))

  const dialog = await screen.findByRole('alertdialog', { name: 'Message someone' })
  expect(within(dialog).getByText(/people who are in a circle with you/)).toBeTruthy()
  const link = within(dialog).getByRole('link', { name: 'Open Circles' }) as HTMLAnchorElement
  expect(link.getAttribute('href')).toBe('/circles')
  // Nothing to choose, so nothing to send.
  expect(within(dialog).queryByRole('radio')).toBeNull()
  expect(within(dialog).queryByRole('button', { name: 'Message' })).toBeNull()

  fireEvent.click(link)
  await waitFor(() => expect(screen.queryByRole('alertdialog', { name: 'Message someone' })).toBeNull())
})

it('lists the people in a circle with the person when choosing Message someone', async () => {
  answerWith()
  openOnChat()
  renderDrawer()

  fireEvent.click(await screen.findByRole('button', { name: 'Person B' }))
  fireEvent.click(await screen.findByRole('menuitem', { name: 'Message someone' }))

  const dialog = await screen.findByRole('dialog', { name: 'Message someone' })
  expect(await within(dialog).findByRole('radio')).toBeTruthy()
  expect(within(dialog).queryByRole('link', { name: 'Open Circles' })).toBeNull()
})

it('follows only the chat when the agent is off, as it does with the agent on', async () => {
  answerWith({ isAgentOn: false })
  localStorage.setItem(OPEN_KEY, '1')
  renderDrawer()

  expect(await screen.findByText('Dinner at seven?')).toBeTruthy()
  await waitFor(() => expect(Object.keys(feeds)).toEqual(['chat-conversation']))
  // Only the chat was followed: not the main conversation, whose name is ''.
  const followed = vi
    .mocked(subscribe)
    .mock.calls.map(([, variables]) => (variables as { conversationId: string }).conversationId)
  expect(followed.every((conversationId) => conversationId === 'chat-conversation')).toBe(true)

  // What the other person says arrives on the feed. The server has one
  // whether or not it runs a model, so the chat is never read on a timer.
  tell('chat-conversation', {
    kind: 'said',
    message: chatMessage('01J0000000000000000000006', 'Running late', false),
  })
  expect(await screen.findByText('Running late')).toBeTruthy()
})

it('tells the server the person is present when the agent is off, for the chats', async () => {
  answerWith({ isAgentOn: false })
  localStorage.setItem(OPEN_KEY, '1')
  renderDrawer()
  await screen.findByText('Dinner at seven?')
  await waitFor(() =>
    expect(vi.mocked(graphql).mock.calls.some(([document]) => document.includes('ReportAgentPresence'))).toBe(true),
  )
})

it('opens on the chat spoken in last when the agent is off, whichever the list holds first', async () => {
  answerWith({ isAgentOn: false, hasOlderChat: true })
  localStorage.setItem(OPEN_KEY, '1')
  renderDrawer()

  expect(await screen.findByText('Dinner at seven?')).toBeTruthy()
  expect(screen.queryByText(/Your agent is not on yet/)).toBeNull()
  expect(screen.getByRole('textbox', { name: 'Message Person B…' })).toBeTruthy()
})

it('shows the usage ring in the main conversation only, and not in a chat or with the agent off', async () => {
  // The agent is on and the main conversation is open: the ring is there.
  answerWith()
  localStorage.setItem(OPEN_KEY, '1')
  localStorage.setItem(CONVERSATION_KEY, 'main-conversation')
  const { container, unmount } = renderDrawer()
  await waitFor(() => expect(container.querySelector('.agent-drawer-head')).toBeTruthy())
  await waitFor(() => expect(container.querySelector('.agent-drawer-budget')).toBeTruthy())
  unmount()
  cleanup()

  // The same person in a chat: the ring is the agent's budget.
  answerWith()
  openOnChat()
  const inChat = renderDrawer()
  await screen.findByText('Dinner at seven?')
  expect(inChat.container.querySelector('.agent-drawer-budget')).toBeNull()
  inChat.unmount()
  cleanup()

  // With the agent off there is no budget at all.
  answerWith({ isAgentOn: false })
  localStorage.setItem(OPEN_KEY, '1')
  localStorage.removeItem(CONVERSATION_KEY)
  const agentOff = renderDrawer()
  await screen.findByText('Dinner at seven?')
  expect(agentOff.container.querySelector('.agent-drawer-budget')).toBeNull()
})

it('has no entry to leave or rename in the menu of a circle’s chat, which follows the circle', async () => {
  answerWith({ hasOlderChat: true, isOlderChatCircle: true })
  localStorage.setItem(OPEN_KEY, '1')
  localStorage.setItem(CONVERSATION_KEY, 'older-conversation')
  renderDrawer()
  fireEvent.click(await screen.findByRole('button', { name: 'Chat menu' }))
  expect(await screen.findByRole('menuitem', { name: /Members/ })).toBeTruthy()
  expect(screen.queryByRole('menuitem', { name: /Leave group/ })).toBeNull()
  expect(screen.queryByRole('menuitem', { name: /Rename group/ })).toBeNull()
})

it('has the entries to leave and rename in the menu of a group chat of its own', async () => {
  answerWith({ hasOlderChat: true })
  localStorage.setItem(OPEN_KEY, '1')
  localStorage.setItem(CONVERSATION_KEY, 'older-conversation')
  renderDrawer()
  fireEvent.click(await screen.findByRole('button', { name: 'Chat menu' }))
  expect(await screen.findByRole('menuitem', { name: /Members/ })).toBeTruthy()
  expect(screen.getByRole('menuitem', { name: /Leave group/ })).toBeTruthy()
  expect(screen.getByRole('menuitem', { name: /Rename group/ })).toBeTruthy()
})

it('asks again which chats have news the moment the window regains focus', async () => {
  answerWith({ unreadCount: 1 })
  renderDrawer()
  const button = await screen.findByRole('button', { name: /1 unread message/ })
  expect(within(button).getByText('1')).toBeTruthy()

  answerWith({ unreadCount: 3 })
  act(() => {
    window.dispatchEvent(new Event('focus'))
  })
  await waitFor(() =>
    expect(within(screen.getByRole('button', { name: /3 unread messages/ })).getByText('3')).toBeTruthy(),
  )
})

it('offers the agents an @ can ask, and sends the one chosen with the message', async () => {
  answerWith({ hasAgents: true })
  openOnChat()
  renderDrawer()
  await screen.findByText('Dinner at seven?')
  const box = await screen.findByRole('textbox', { name: /Message/ })

  // A bare @ offers the person's own agent first, then the one that may be asked.
  fireEvent.change(box, { target: { value: '@', selectionStart: 1, selectionEnd: 1 } })
  const options = await screen.findAllByRole('option')
  expect(options.map((option) => option.textContent)).toEqual(['RobinYours', 'BennyPerson B'])

  // Letters narrow it; Enter chooses it and puts its name in the box.
  fireEvent.change(box, { target: { value: '@Be', selectionStart: 3, selectionEnd: 3 } })
  expect(await screen.findAllByRole('option')).toHaveLength(1)
  fireEvent.keyDown(box, { key: 'Enter' })
  expect((box as HTMLTextAreaElement).value).toBe('@Benny ')
  expect(screen.queryByRole('option')).toBeNull()

  fireEvent.change(box, { target: { value: '@Benny what is the weather?', selectionStart: 27, selectionEnd: 27 } })
  fireEvent.keyDown(box, { key: 'Enter' })
  await waitFor(() => {
    const asked = vi.mocked(graphql).mock.calls.find(([document]) => document.includes('mutation') && document.includes('AskAgent'))
    expect(asked).toBeTruthy()
    expect(asked?.[1]).toMatchObject({ message: '@Benny what is the weather?', addressedAgentIds: ['agent-benny'] })
  })
})

it('addresses nothing when an agent’s name is only typed, or taken out again', async () => {
  answerWith({ hasAgents: true })
  openOnChat()
  renderDrawer()
  await screen.findByText('Dinner at seven?')
  const box = await screen.findByRole('textbox', { name: /Message/ })

  fireEvent.change(box, { target: { value: 'ask Benny about it', selectionStart: 18, selectionEnd: 18 } })
  fireEvent.keyDown(box, { key: 'Enter' })
  await waitFor(() => expect(vi.mocked(graphql).mock.calls.some(([document]) => document.includes('AskAgent'))).toBe(true))
  const asked = vi.mocked(graphql).mock.calls.find(([document]) => document.includes('mutation') && document.includes('AskAgent'))
  expect((asked?.[1] as { addressedAgentIds?: string[] }).addressedAgentIds).toBeUndefined()
})

it('offers no @ where no agent can be asked', async () => {
  answerWith({ hasAgents: false })
  openOnChat()
  renderDrawer()
  await screen.findByText('Dinner at seven?')
  const box = await screen.findByRole('textbox', { name: /Message/ })
  fireEvent.change(box, { target: { value: '@', selectionStart: 1, selectionEnd: 1 } })
  expect(screen.queryByRole('listbox')).toBeNull()
})

it('shows an agent as thinking by name, draws its words as they come and replaces them with the stored answer', async () => {
  answerWith({ hasAgents: true })
  openOnChat()
  renderDrawer()
  await screen.findByText('Dinner at seven?')
  await waitFor(() => expect(feeds['chat-conversation']).toBeTruthy())

  tell('chat-conversation', { kind: 'typing', name: 'Robin', authorAgentId: 'agent-robin' })
  expect(await screen.findByText('Robin is thinking')).toBeTruthy()
  // A person typing is worded another way, and both can show.
  tell('chat-conversation', { kind: 'typing', userId: 'person-b', name: 'Person B' })
  expect(await screen.findByText('Person B is typing')).toBeTruthy()

  tell('chat-conversation', { kind: 'text', runId: 'run-1', text: 'Thurs', name: 'Robin', authorAgentId: 'agent-robin' })
  tell('chat-conversation', { kind: 'text', runId: 'run-1', text: 'day at 3', name: 'Robin', authorAgentId: 'agent-robin' })
  expect(await screen.findByText('Thursday at 3')).toBeTruthy()
  // It is drawn as the agent’s message, under its name.
  expect(document.querySelectorAll('.agent-chat-author')).toHaveLength(2)

  tell('chat-conversation', {
    kind: 'said',
    message: chatMessage('01J0000000000000000000009', 'Thursday at 3.', false, {
      role: 'assistant',
      authorKind: 'agent',
      authorUserId: 'person-a',
      authorName: 'Robin',
      authorAgentId: 'agent-robin',
      agentRunId: 'run-1',
    }),
  })
  expect(await screen.findByText('Thursday at 3.')).toBeTruthy()
  expect(screen.queryByText('Thursday at 3')).toBeNull()
  // One message, not the words and the message; and it has finished thinking.
  expect(screen.queryByText('Robin is thinking')).toBeNull()
  expect(screen.getAllByText(/Thursday at 3/)).toHaveLength(1)
})

it('says whose agent spoke or is thinking when two of them are both called Agent', async () => {
  answerWith({ hasAgents: true })
  openOnChat()
  renderDrawer()
  await screen.findByText('Dinner at seven?')
  await waitFor(() => expect(feeds['chat-conversation']).toBeTruthy())

  // Neither owner is the reader; both agents keep the default name.
  tell('chat-conversation', { kind: 'typing', name: 'Agent', authorOwnerName: 'Person B', authorAgentId: 'agent-b' })
  expect(await screen.findByText("Person B's agent is thinking")).toBeTruthy()
  tell('chat-conversation', { kind: 'typing', name: 'Bertie', authorOwnerName: 'Person C', authorAgentId: 'agent-c' })
  expect(await screen.findByText("Person B's agent and Bertie · Person C's agent are thinking")).toBeTruthy()

  const said = (id: string, content: string, overrides: Record<string, unknown>) =>
    chatMessage(id, content, false, { role: 'assistant', authorKind: 'agent', ...overrides })
  tell('chat-conversation', {
    kind: 'said',
    message: said('01J0000000000000000000011', 'Free on Friday.', {
      authorUserId: 'person-b',
      authorName: 'Agent',
      authorOwnerName: 'Person B',
      authorAgentId: 'agent-b',
    }),
  })
  tell('chat-conversation', {
    kind: 'said',
    message: said('01J0000000000000000000012', 'Busy on Friday.', {
      authorUserId: 'person-c',
      authorName: 'Bertie',
      authorOwnerName: 'Person C',
      authorAgentId: 'agent-c',
    }),
  })
  expect(await screen.findByText('Busy on Friday.')).toBeTruthy()
  const authors = Array.from(document.querySelectorAll('.agent-chat-author')).map((author) => author.textContent)
  expect(authors).toContain("Person B's agent")
  expect(authors).toContain("Bertie · Person C's agent")
  // The default name alone is never what an agent is shown as.
  expect(authors).not.toContain('Agent')
})

it('draws the run’s own answer event as nothing in a chat, since the chat says it', async () => {
  answerWith({ hasAgents: true })
  openOnChat()
  renderDrawer()
  await screen.findByText('Dinner at seven?')
  await waitFor(() => expect(feeds['chat-conversation']).toBeTruthy())
  tell('chat-conversation', { kind: 'asked', runId: 'run-2', text: 'what time?', note: 'chat' })
  tell('chat-conversation', { kind: 'message', runId: 'run-2', sequence: 3, text: 'It is three.' })
  expect(screen.queryByText('It is three.')).toBeNull()
  expect(screen.queryByText('what time?')).toBeNull()
})

it('puts a note only the asker sees where the agent could not be asked', async () => {
  answerWith({ hasAgents: true })
  openOnChat()
  renderDrawer()
  await screen.findByText('Dinner at seven?')
  await waitFor(() => expect(feeds['chat-conversation']).toBeTruthy())
  tell('chat-conversation', {
    kind: 'note',
    runId: '',
    note: 'Only Benny’s owner can ask it in this chat.',
    noteKind: 'chat_agent_owner_only',
    noteDetail: 'Benny',
  })
  expect(await screen.findByText('Only the owner of Benny can ask it in this chat.')).toBeTruthy()
})

it('has the two switches for the person’s own agent in the members, and turns each on with its own call', async () => {
  answerWith({ hasAgents: true })
  openOnChat()
  renderDrawer()
  fireEvent.click(await screen.findByRole('button', { name: 'Chat menu' }))
  fireEvent.click(await screen.findByRole('menuitem', { name: /Members/ }))

  const remember = await screen.findByRole('checkbox', { name: 'My agent remembers this chat' })
  const askable = await screen.findByRole('checkbox', { name: 'Others may ask my agent here' })
  // Each says what it means beside it.
  expect(screen.getByText(/reads what is said here/)).toBeTruthy()
  expect(screen.getByText(/nothing of yours/)).toBeTruthy()
  // Person B's agent may be asked, and is shown to be.
  expect(screen.getByText('Others may ask their agent')).toBeTruthy()

  fireEvent.click(remember)
  await waitFor(() => {
    const call = vi.mocked(graphql).mock.calls.find(([document]) => document.includes('SetChatAgentReading'))
    expect(call?.[1]).toMatchObject({ conversationId: 'chat-conversation', isAgentReading: true })
  })
  fireEvent.click(askable)
  await waitFor(() => {
    const call = vi.mocked(graphql).mock.calls.find(([document]) => document.includes('SetChatAgentAskableByOthers'))
    expect(call?.[1]).toMatchObject({ conversationId: 'chat-conversation', isAgentAskableByOthers: true })
  })
})

it('does not draw the work of the person’s own agent as something said in the chat', async () => {
  answerWith({ hasWorkRows: true })
  openOnChat()
  const { container } = renderDrawer()
  await screen.findByText('Dinner at seven?')
  // The call a round made and its result are rows of the person’s own
  // conversation: neither is a message in the chat, so there are still three.
  expect(container.querySelectorAll('.agent-chat-turn')).toHaveLength(3)
  expect(container.querySelectorAll('.agent-chat-author')).toHaveLength(1)
})
