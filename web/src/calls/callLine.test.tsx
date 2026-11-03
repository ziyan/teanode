import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'

import { graphql } from '../api'
import { ChatCallLine } from './callLine'
import type { ChatCallSummary } from './chatCallApi'
import { answerCallsWith, callOf, fakeSession, renderWithCalls } from './chatCallFixtures'

vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  graphql: vi.fn(),
  subscribe: vi.fn(),
}))

afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})

// The call as a message of the chat carries it.
function summaryOf(overrides: Partial<ChatCallSummary> = {}): ChatCallSummary {
  return {
    id: 'call-one',
    startedAt: '2030-01-01T10:00:00Z',
    answeredAt: null,
    endedAt: null,
    endReason: '',
    transcriptState: '',
    members: [
      { userId: 'person-b', ringState: 'none' },
      { userId: 'person-a', ringState: 'ringing' },
    ],
    ...overrides,
  }
}

it('says how long a call lasted, from the first answer to the end', () => {
  answerCallsWith({})
  renderWithCalls(
    <ChatCallLine
      callId="call-one"
      call={summaryOf({ answeredAt: '2030-01-01T10:00:10Z', endedAt: '2030-01-01T10:04:22Z', endReason: 'ended' })}
    />,
  )

  expect(screen.getByText('Call, 4:12')).toBeTruthy()
  expect(screen.queryByRole('button', { name: 'Join' })).toBeNull()
  expect(screen.queryByRole('button', { name: 'Transcript' })).toBeNull()
})

it('says a call nobody answered was missed', () => {
  answerCallsWith({})
  renderWithCalls(
    <ChatCallLine callId="call-one" call={summaryOf({ endedAt: '2030-01-01T10:01:00Z', endReason: 'missed' })} />,
  )

  expect(screen.getByText('Missed call')).toBeTruthy()
})

it('says a call everybody said no to was declined', () => {
  answerCallsWith({})
  renderWithCalls(
    <ChatCallLine callId="call-one" call={summaryOf({ endedAt: '2030-01-01T10:00:20Z', endReason: 'declined' })} />,
  )

  expect(screen.getByText('Declined call')).toBeTruthy()
})

it('says a call the server lost failed', () => {
  answerCallsWith({})
  renderWithCalls(
    <ChatCallLine callId="call-one" call={summaryOf({ endedAt: '2030-01-01T10:00:20Z', endReason: 'failed' })} />,
  )

  expect(screen.getByText('Call failed')).toBeTruthy()
})

it('offers to join a call that is going, and connects', async () => {
  answerCallsWith({ JoinChatCall: () => callOf({ askerRingState: 'answered', isAskerOn: true }) })
  const session = fakeSession()
  renderWithCalls(<ChatCallLine callId="call-one" call={summaryOf()} />, () => session)

  expect(screen.getByText('Call going')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'Join' }))

  await waitFor(() => expect(session.start).toHaveBeenCalled())
  expect(vi.mocked(graphql)).toHaveBeenCalledWith(expect.stringContaining('JoinChatCall('), { callId: 'call-one' })
  // On the call, there is nothing left to join.
  await waitFor(() => expect(screen.queryByRole('button', { name: 'Join' })).toBeNull())
})

it('follows the call from going to over as the events tell of it', async () => {
  const server = answerCallsWith({})
  renderWithCalls(<ChatCallLine callId="call-one" call={summaryOf()} />)
  expect(screen.getByText('Call going')).toBeTruthy()

  server.tell(
    callOf({
      answeredAt: '2030-01-01T10:00:05Z',
      isGoing: false,
      endedAt: '2030-01-01T10:01:10Z',
      endReason: 'ended',
    }),
  )

  expect(await screen.findByText('Call, 1:05')).toBeTruthy()
  expect(screen.queryByText('Call going')).toBeNull()
  expect(screen.queryByRole('button', { name: 'Join' })).toBeNull()
})

it('keeps a call that is over over, whatever the events last said', () => {
  answerCallsWith({})
  renderWithCalls(
    <ChatCallLine callId="call-one" call={summaryOf({ endedAt: '2030-01-01T10:01:00Z', endReason: 'missed' })} />,
  )
  expect(screen.getByText('Missed call')).toBeTruthy()
})

it('opens the transcript of a transcribed call, each line under its speaker', async () => {
  answerCallsWith({
    ReadChatCall: () => ({
      call: callOf({ isGoing: false }),
      transcript: [
        {
          id: 'line-1',
          callId: 'call-one',
          userId: 'person-b',
          speakerName: 'Tobias',
          startMs: 2000,
          endMs: 5000,
          text: 'Shall we book the train?',
        },
        {
          id: 'line-2',
          callId: 'call-one',
          userId: 'person-a',
          speakerName: 'Mira',
          startMs: 6000,
          endMs: 8000,
          text: 'Yes, the early one.',
        },
      ],
      recordingId: 'recording-one',
      isRecordingOwned: true,
    }),
  })
  renderWithCalls(
    <ChatCallLine
      callId="call-one"
      call={summaryOf({
        answeredAt: '2030-01-01T10:00:10Z',
        endedAt: '2030-01-01T10:04:22Z',
        endReason: 'ended',
        transcriptState: 'done',
      })}
    />,
  )

  fireEvent.click(screen.getByRole('button', { name: 'Transcript' }))

  const dialog = await screen.findByRole('alertdialog', { name: 'Call transcript' })
  expect(dialog.textContent).toContain('Tobias')
  expect(dialog.textContent).toContain('Shall we book the train?')
  expect(dialog.textContent).toContain('Mira')
  expect(dialog.textContent).toContain('Yes, the early one.')
  expect(vi.mocked(graphql)).toHaveBeenCalledWith(expect.stringContaining('ReadChatCall('), { callId: 'call-one' })
  // The call is a recording, which the dialog opens.
  expect(within(dialog).getByRole('button', { name: 'Open the recording' })).toBeTruthy()
})

it('offers no transcript until it is done', () => {
  answerCallsWith({})
  renderWithCalls(
    <ChatCallLine
      callId="call-one"
      call={summaryOf({
        answeredAt: '2030-01-01T10:00:10Z',
        endedAt: '2030-01-01T10:04:22Z',
        endReason: 'ended',
        transcriptState: 'waiting',
      })}
    />,
  )

  expect(screen.queryByRole('button', { name: 'Transcript' })).toBeNull()
})
