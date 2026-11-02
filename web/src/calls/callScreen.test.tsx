import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'

import { graphql } from '../api'
import { answerCallsWith, callOf, fakeSession, memberOf, quietSnapshot, renderWithCalls } from './chatCallFixtures'
import type { ChatCallSessionSnapshot } from './chatCallSession'
import { useChatCalls } from './chatCallContext'

vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  graphql: vi.fn(),
  subscribe: vi.fn(),
}))

afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})

// Mira, signed in, is on a call with Tobias and Priya. Priya's camera is on.
const members = [
  memberOf('person-a', 'Mira', { ringState: 'answered', isOn: true }),
  memberOf('person-b', 'Tobias', { ringState: 'none', isOn: true }),
  memberOf('person-c', 'Priya', { ringState: 'answered', isOn: true, isVideoOn: true }),
]
const onTheCall = callOf({
  askerRingState: 'answered',
  isAskerOn: true,
  answeredAt: '2030-01-01T10:00:05Z',
  members,
})

// A button that starts the call in the group, and shows what the calls
// provider says.
function StartHere() {
  const { startCall } = useChatCalls()
  return (
    <button type="button" onClick={() => void startCall('chat-conversation')}>
      Start here
    </button>
  )
}

async function startCall(
  call = onTheCall,
  snapshot: ChatCallSessionSnapshot = quietSnapshot,
  extra: Record<string, (variables: Record<string, unknown>) => unknown> = {},
) {
  const server = answerCallsWith({ StartChatCall: () => call, ...extra })
  const session = fakeSession(snapshot)
  renderWithCalls(<StartHere />, () => session)
  fireEvent.click(screen.getByRole('button', { name: 'Start here' }))
  const screenDialog = await screen.findByRole('dialog', { name: 'Call in Trip planning' })
  return { server, session, screenDialog }
}

it('draws everybody on the call, with their picture only while their camera is on and it has arrived', async () => {
  const { screenDialog } = await startCall(onTheCall, {
    ...quietSnapshot,
    participants: {
      'person-c': { audioStream: null, videoStream: {} as MediaStream },
      'person-b': { audioStream: null, videoStream: null },
    },
  })

  const tiles = within(screenDialog).getAllByRole('listitem')
  expect(tiles.map((tile) => tile.querySelector('.call-tile-name')?.textContent)).toEqual([
    'Mira (you)',
    'Tobias',
    'Priya',
  ])
  // Priya's camera is on and her picture is here; the others show their avatar.
  expect(tiles[2].querySelector('video')).toBeTruthy()
  expect(tiles[1].querySelector('video')).toBeNull()
  expect(tiles[1].querySelector('.avatar')).toBeTruthy()
})

it('has a bar of mute, camera, transcribe and leave, and the switch only when there are several cameras', async () => {
  const { screenDialog, session } = await startCall()

  const bar = within(screenDialog)
  expect(bar.getByRole('button', { name: 'Mute' })).toBeTruthy()
  expect(bar.getByRole('button', { name: 'Turn the camera on' })).toBeTruthy()
  expect(bar.getByRole('button', { name: 'Transcribe this call' })).toBeTruthy()
  expect(bar.getByRole('button', { name: 'Leave the call' })).toBeTruthy()
  expect(bar.queryByRole('button', { name: 'Switch camera' })).toBeNull()

  fireEvent.click(bar.getByRole('button', { name: 'Mute' }))
  expect(session.setMuted).toHaveBeenCalledWith(true)
  fireEvent.click(bar.getByRole('button', { name: 'Turn the camera on' }))
  expect(session.setVideo).toHaveBeenCalledWith(true)
})

it('offers the camera switch when the device has several', async () => {
  const { screenDialog, session } = await startCall(onTheCall, {
    ...quietSnapshot,
    isVideoOn: true,
    canSwitchCamera: true,
  })

  fireEvent.click(within(screenDialog).getByRole('button', { name: 'Switch camera' }))
  expect(session.switchCamera).toHaveBeenCalled()
  expect(within(screenDialog).getByRole('button', { name: 'Turn the camera off' })).toBeTruthy()
})

it('leaves the call and closes the screen', async () => {
  const { screenDialog, session } = await startCall(onTheCall, quietSnapshot, { LeaveChatCall: () => onTheCall })

  fireEvent.click(within(screenDialog).getByRole('button', { name: 'Leave the call' }))

  await waitFor(() => expect(session.leave).toHaveBeenCalled())
  await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Call in Trip planning' })).toBeNull())
})

it('shrinks to a small window that keeps the call, and brings it back', async () => {
  const { screenDialog } = await startCall()

  fireEvent.click(within(screenDialog).getByRole('button', { name: 'Make the call small' }))

  const mini = await screen.findByRole('group', { name: 'Call in Trip planning' })
  expect(screen.queryByRole('dialog', { name: 'Call in Trip planning' })).toBeNull()
  expect(within(mini).getByRole('button', { name: 'Mute' })).toBeTruthy()
  expect(within(mini).getByRole('button', { name: 'Leave the call' })).toBeTruthy()

  fireEvent.click(within(mini).getAllByRole('button', { name: 'Show the call' })[0])
  expect(await screen.findByRole('dialog', { name: 'Call in Trip planning' })).toBeTruthy()
})

it('asks before transcribing: the request names who asked, and Allow answers for this voice only', async () => {
  const asked = callOf({
    askerRingState: 'answered',
    isAskerOn: true,
    answeredAt: '2030-01-01T10:00:05Z',
    transcriptionAskedByUserId: 'person-b',
    members: [
      memberOf('person-a', 'Mira', { isOn: true, transcriptionConsent: '' }),
      memberOf('person-b', 'Tobias', { ringState: 'none', isOn: true, transcriptionConsent: 'yes' }),
    ],
  })
  const allowed = callOf({
    ...asked,
    members: [{ ...asked.members[0], transcriptionConsent: 'yes' }, asked.members[1]],
  })
  const { screenDialog } = await startCall(asked, quietSnapshot, { AnswerChatCallTranscription: () => allowed })

  expect(within(screenDialog).getByText('Tobias asked to transcribe this call')).toBeTruthy()
  // Somebody has agreed, so it is being kept.
  expect(within(screenDialog).getByText('Transcribing')).toBeTruthy()
  // It was asked for already: no second ask.
  expect(within(screenDialog).queryByRole('button', { name: 'Transcribe this call' })).toBeNull()

  fireEvent.click(within(screenDialog).getByRole('button', { name: 'Allow' }))

  await waitFor(() =>
    expect(vi.mocked(graphql)).toHaveBeenCalledWith(expect.stringContaining('AnswerChatCallTranscription('), {
      callId: 'call-one',
      isConsenting: true,
    }),
  )
  await waitFor(() => expect(within(screenDialog).queryByText('Tobias asked to transcribe this call')).toBeNull())
})

it('lets a person say no to being transcribed', async () => {
  const asked = callOf({
    askerRingState: 'answered',
    isAskerOn: true,
    transcriptionAskedByUserId: 'person-b',
    members: [
      memberOf('person-a', 'Mira', { isOn: true }),
      memberOf('person-b', 'Tobias', { ringState: 'none', isOn: true, transcriptionConsent: 'yes' }),
    ],
  })
  const { screenDialog } = await startCall(asked, quietSnapshot, { AnswerChatCallTranscription: () => asked })

  fireEvent.click(within(screenDialog).getByRole('button', { name: 'Not me' }))

  await waitFor(() =>
    expect(vi.mocked(graphql)).toHaveBeenCalledWith(expect.stringContaining('AnswerChatCallTranscription('), {
      callId: 'call-one',
      isConsenting: false,
    }),
  )
})

it('shows no transcribing mark when nobody has agreed', async () => {
  const asked = callOf({
    askerRingState: 'answered',
    isAskerOn: true,
    transcriptionAskedByUserId: 'person-a',
    members: [memberOf('person-a', 'Mira', { isOn: true, transcriptionConsent: 'no' })],
  })
  const { screenDialog } = await startCall(asked)

  expect(within(screenDialog).queryByText('Transcribing')).toBeNull()
})

it('asks for a transcript through the bar', async () => {
  const { screenDialog } = await startCall(onTheCall, quietSnapshot, {
    AskChatCallTranscription: () => callOf({ ...onTheCall, transcriptionAskedByUserId: 'person-a' }),
  })

  fireEvent.click(within(screenDialog).getByRole('button', { name: 'Transcribe this call' }))

  await waitFor(() =>
    expect(vi.mocked(graphql)).toHaveBeenCalledWith(expect.stringContaining('AskChatCallTranscription('), {
      callId: 'call-one',
    }),
  )
})
