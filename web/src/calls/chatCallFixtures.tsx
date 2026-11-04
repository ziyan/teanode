import { act, render } from '@testing-library/react'
import { vi } from 'vitest'
import { MemoryRouter } from 'react-router-dom'

import { graphql, subscribe } from '../api'
import { ToastProvider } from '../components/toast'
import { TranslationProvider } from '../i18n/i18n'
import { SessionProvider } from '../session'
import type { ChatCall, ChatCallMember } from './chatCallApi'
import { ChatCallProvider } from './chatCallProvider'
import type { ChatCallSession, ChatCallSessionSnapshot } from './chatCallSession'

// What the tests of the calls share: a call, a person on it, a connection
// that connects to nothing, and a server that answers by operation name.
// A test file mocks '../api' itself; these use the mocked functions.

export function memberOf(userId: string, name: string, overrides: Partial<ChatCallMember> = {}): ChatCallMember {
  return {
    userId,
    name,
    avatarUrl: null,
    avatarColorIndex: 3,
    ringState: 'answered',
    isOn: false,
    isVideoOn: false,
    isMuted: false,
    joinedAt: null,
    leftAt: null,
    ...overrides,
  }
}

// callOf is a call in a group chat that Tobias started and is ringing Mira,
// who is signed in. Say what differs.
export function callOf(overrides: Partial<ChatCall> = {}): ChatCall {
  return {
    id: 'call-one',
    conversationId: 'chat-conversation',
    chatTitle: 'Trip planning',
    isGroup: true,
    startedByUserId: 'person-b',
    startedByName: 'Tobias',
    startedAt: '2030-01-01T10:00:00Z',
    answeredAt: null,
    endedAt: null,
    endReason: '',
    isGoing: true,
    durationMs: 0,
    askerRingState: 'ringing',
    isAskerOn: false,
    transcriptionAskedByUserId: null,
    transcriptState: '',
    summaryMessageId: null,
    members: [
      memberOf('person-b', 'Tobias', { ringState: 'none', isOn: true }),
      memberOf('person-a', 'Mira', { ringState: 'ringing' }),
    ],
    ...overrides,
  }
}

export const quietSnapshot: ChatCallSessionSnapshot = {
  phase: 'connected',
  isMuted: false,
  isVideoOn: false,
  canSwitchCamera: false,
  isConnectionLost: false,
  microphoneDeviceId: '',
  soundOutputDeviceId: '',
  localAudioStream: null,
  localVideoStream: null,
  participants: {},
  speakingUserId: '',
  featuredUserId: '',
}

// fakeSession is a connection that connects to nothing and says so. What the
// person chooses of the sound is kept and told of, as the real one does, and
// the devices are the ones given.
export function fakeSession(
  snapshot: ChatCallSessionSnapshot = quietSnapshot,
  devices: { microphones: MediaDeviceInfo[]; speakers: MediaDeviceInfo[] } = { microphones: [], speakers: [] },
) {
  let current = snapshot
  const listeners = new Set<() => void>()
  const session = {
    subscribe: (listener: () => void) => {
      listeners.add(listener)
      return () => {
        listeners.delete(listener)
      }
    },
    getSnapshot: () => current,
    start: vi.fn(async () => {}),
    leave: vi.fn(async () => {}),
    close: vi.fn(),
    setMuted: vi.fn(),
    setFeatured: vi.fn((featuredUserId: string) => {
      current = { ...current, featuredUserId }
      listeners.forEach((listener) => listener())
    }),
    setVideo: vi.fn(async () => {}),
    switchCamera: vi.fn(async () => {}),
    setMicrophone: vi.fn(async () => {}),
    setSoundOutput: vi.fn((soundOutputDeviceId: string) => {
      current = { ...current, soundOutputDeviceId }
      listeners.forEach((listener) => listener())
    }),
    listAudioDevices: vi.fn(async () => devices),
    watchDeviceChanges: vi.fn(() => () => {}),
  }
  return session as unknown as ChatCallSession & typeof session
}

// deviceOf is a microphone or a loudspeaker as the browser lists it.
export function deviceOf(kind: 'audioinput' | 'audiooutput', deviceId: string, label: string): MediaDeviceInfo {
  return { kind, deviceId, label, groupId: '' } as MediaDeviceInfo
}

// answerCallsWith makes the server: each operation named answers with what the
// function given returns for its variables, and the others answer nothing. It
// also opens ChatCallEvents, and returns the way to tell the page of a change.
type Answers = Record<string, (variables: Record<string, unknown>) => unknown>

export function answerCallsWith(given: Answers) {
  // No call going unless the test says so.
  const answers: Answers = { ListGoingChatCalls: () => [], ReadGoingChatCall: () => null, ...given }
  vi.mocked(graphql).mockImplementation((async (document: string, variables: Record<string, unknown> = {}) => {
    for (const [name, answer] of Object.entries(answers)) {
      if (document.includes(`${name}(`) || document.includes(`${name} `)) return { [name]: answer(variables) }
    }
    return {}
  }) as never)
  let onEvent: (data: unknown) => void = () => {}
  vi.mocked(subscribe).mockImplementation(((
    query: string,
    _variables: unknown,
    onData: (data: unknown) => void,
    _onEnd: unknown,
    options?: { beforeStart?: () => Promise<void> },
  ) => {
    if (query.includes('ChatCallEvents')) {
      onEvent = onData
      // The page's subscription reads what it missed first, and goes on if
      // that fails.
      void Promise.resolve(options?.beforeStart?.()).catch(() => {})
    }
    return () => {}
  }) as never)
  return {
    tell: (call: ChatCall) => act(() => onEvent({ ChatCallEvents: call })),
  }
}

export function renderWithCalls(ui: React.ReactNode, createSession: () => ChatCallSession = () => fakeSession()) {
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
            <ChatCallProvider isSupported createSession={createSession}>
              {ui}
            </ChatCallProvider>
          </MemoryRouter>
        </SessionProvider>
      </ToastProvider>
    </TranslationProvider>,
  )
}
