import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql, subscribe } from '../api'
import { TranslationProvider } from '../i18n/i18n'
import { SessionProvider } from '../session'
import { AgentDrawer } from './agentDrawer'
import { CHAT_COMPOSER_MODE_KEY } from './chatVoiceComposer'
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
const mainConversation = {
  id: 'main-conversation',
  kind: 'main',
  title: '',
  summary: '',
  lastAt: '2030-01-01T09:00:00Z',
  chat: null,
}

// 48 levels, as the server sends them.
const waveform = btoa(String.fromCharCode(...Array.from({ length: 48 }, (_, index) => index * 5)))

function voiceMessage(overrides: Record<string, unknown> = {}) {
  return {
    id: '01J0000000000000000000001',
    createdAt: '2030-01-01T10:00:00Z',
    role: 'assistant',
    content: '',
    name: null,
    toolCallId: null,
    toolCalls: null,
    usage: null,
    attachments: [
      {
        id: 'attachment-1',
        name: 'Voice message.m4a',
        contentType: 'audio/mp4',
        size: 4000,
        isRemoved: false,
        audioDurationMS: 4000,
        audioWaveform: waveform,
      },
    ],
    references: null,
    reactions: null,
    isVoiceMessage: true,
    transcriptText: '',
    transcriptState: 'waiting',
    authorKind: 'person',
    authorUserId: 'person-b',
    authorName: 'Tobias',
    authorAvatarUrl: null,
    authorAvatarColorIndex: 4,
    authorAgentId: null,
    isMine: false,
    editedAt: null,
    isDeleted: false,
    ...overrides,
  }
}

interface Offered {
  isVoiceAvailable: boolean
  isVoiceMessageAvailable: boolean
}

function answerWith(offered: Offered, messages: unknown[] = [voiceMessage()]) {
  vi.mocked(graphql).mockImplementation(async (document, variables) => {
    if (document.includes('allowed {')) {
      return {
        ReadAgent: { agent: { id: 'agent-one', enabled: true, name: 'Robin' }, allowed: { enabled: true, ask: true } },
      } as never
    }
    if (document.includes('ListAgentConversations'))
      return { ListAgentConversations: [mainConversation, chatConversation] } as never
    if (document.includes('ReadAgentConversation(')) {
      const id = (variables as { conversationId?: string } | undefined)?.conversationId
      if (id === 'chat-conversation') {
        return {
          ReadAgentConversation: {
            conversation: chatConversation,
            actingAs: null,
            messages,
            total: messages.length,
            todos: [],
          },
          ListAgentInteractions: [],
        } as never
      }
      return {
        ReadAgentConversation: { conversation: mainConversation, actingAs: null, messages: [], total: 0, todos: [] },
        ListAgentInteractions: [],
      } as never
    }
    if (document.includes('ReadAgentVoice')) return { ReadAgentVoice: { ...offered, sampleRate: 24000 } } as never
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
  for (const key of Object.keys(feeds)) delete feeds[key]
  vi.mocked(subscribe).mockImplementation(((
    _query: string,
    variables: { conversationId: string },
    onData: (data: unknown) => void,
  ) => {
    feeds[variables.conversationId] = onData
    return () => undefined
  }) as never)
  // The browser can record.
  Object.defineProperty(navigator, 'mediaDevices', { configurable: true, value: { getUserMedia: vi.fn() } })
  vi.stubGlobal('MediaRecorder', class {})
})
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  vi.resetAllMocks()
})

function openOn(conversationId: string) {
  localStorage.setItem(OPEN_KEY, '1')
  localStorage.setItem(CONVERSATION_KEY, conversationId)
}

it('draws a voice message as a bubble, and its words appear when the message comes back as edited', async () => {
  answerWith({ isVoiceAvailable: true, isVoiceMessageAvailable: true })
  openOn('chat-conversation')
  const { container } = renderDrawer()

  expect(await screen.findByText('Transcribing…')).toBeTruthy()
  expect(screen.getByRole('button', { name: 'Play the voice message' })).toBeTruthy()
  expect(container.querySelector('audio')?.getAttribute('src')).toBe('/api/v1/agent/attachments/attachment-1')
  // No chip with the file's name and size: the bubble is the file.
  expect(screen.queryByText('Voice message.m4a')).toBeNull()
  await waitFor(() => expect(feeds['chat-conversation']).toBeTruthy())

  act(() => {
    feeds['chat-conversation']({
      AgentConversationEvents: {
        kind: 'edited',
        runId: '',
        sequence: 0,
        message: voiceMessage({ transcriptText: 'Running ten minutes late', transcriptState: 'done' }),
      },
    })
  })
  expect(await screen.findByText('Running ten minutes late')).toBeTruthy()
  expect(screen.queryByText('Transcribing…')).toBeNull()
  // Nobody edited it: the line does not say so.
  expect(screen.queryByText('edited')).toBeNull()
})

it('asks for the voice fields of a message and of its recording', async () => {
  answerWith({ isVoiceAvailable: true, isVoiceMessageAvailable: true })
  openOn('chat-conversation')
  renderDrawer()
  await screen.findByText('Transcribing…')
  const reading = vi.mocked(graphql).mock.calls.find(([document]) => document.includes('ReadAgentConversation('))
  expect(reading?.[0]).toContain('isVoiceMessage transcriptText transcriptState')
  expect(reading?.[0]).toContain('audioDurationMS audioWaveform')
  const subscription = vi.mocked(subscribe).mock.calls[0]
  expect(String(subscription[0])).toContain('isVoiceMessage transcriptText transcriptState')
  expect(String(subscription[0])).toContain('audioDurationMS audioWaveform')
})

it('switches a chat to a button held to talk in place of the box, and keeps the mode', async () => {
  answerWith({ isVoiceAvailable: true, isVoiceMessageAvailable: true })
  openOn('chat-conversation')
  renderDrawer()
  await screen.findByText('Transcribing…')

  fireEvent.click(await screen.findByRole('button', { name: 'Record a voice message' }))
  expect(screen.getByRole('button', { name: 'Hold to talk' })).toBeTruthy()
  expect(screen.queryByRole('textbox', { name: /Message/ })).toBeNull()
  expect(localStorage.getItem(CHAT_COMPOSER_MODE_KEY)).toBe('voice')

  fireEvent.click(screen.getByRole('button', { name: 'Use the keyboard' }))
  expect(screen.queryByRole('button', { name: 'Hold to talk' })).toBeNull()
  expect(localStorage.getItem(CHAT_COMPOSER_MODE_KEY)).toBe('keyboard')
  expect(screen.getByRole('button', { name: 'Record a voice message' })).toBeTruthy()
})

it('opens a chat in voice mode when that is the mode kept', async () => {
  localStorage.setItem(CHAT_COMPOSER_MODE_KEY, 'voice')
  answerWith({ isVoiceAvailable: true, isVoiceMessageAvailable: true })
  openOn('chat-conversation')
  renderDrawer()
  expect(await screen.findByRole('button', { name: 'Hold to talk' })).toBeTruthy()
})

it('offers no voice mode when the server cannot make voice messages, whatever mode was kept', async () => {
  localStorage.setItem(CHAT_COMPOSER_MODE_KEY, 'voice')
  answerWith({ isVoiceAvailable: true, isVoiceMessageAvailable: false })
  openOn('chat-conversation')
  renderDrawer()
  await screen.findByText('Transcribing…')
  await waitFor(() => expect(screen.getByRole('button', { name: 'Dictate' })).toBeTruthy())
  expect(screen.queryByRole('button', { name: 'Hold to talk' })).toBeNull()
  expect(screen.queryByRole('button', { name: 'Record a voice message' })).toBeNull()
})

it('has no voice mode in a conversation with the agent, only the microphone for dictating', async () => {
  localStorage.setItem(CHAT_COMPOSER_MODE_KEY, 'voice')
  answerWith({ isVoiceAvailable: true, isVoiceMessageAvailable: true })
  openOn('main-conversation')
  renderDrawer()
  expect(await screen.findByRole('button', { name: 'Dictate' })).toBeTruthy()
  expect(screen.queryByRole('button', { name: 'Hold to talk' })).toBeNull()
  expect(screen.queryByRole('button', { name: 'Record a voice message' })).toBeNull()
})

it('shows no microphone to dictate into when the server has no voice', async () => {
  answerWith({ isVoiceAvailable: false, isVoiceMessageAvailable: false })
  openOn('chat-conversation')
  renderDrawer()
  await screen.findByText('Transcribing…')
  expect(screen.queryByRole('button', { name: 'Dictate' })).toBeNull()
})
