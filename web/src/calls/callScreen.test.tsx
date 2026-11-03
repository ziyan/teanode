import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'

import { graphql } from '../api'
import {
  answerCallsWith,
  callOf,
  deviceOf,
  fakeSession,
  memberOf,
  quietSnapshot,
  renderWithCalls,
} from './chatCallFixtures'
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
  devices?: Parameters<typeof fakeSession>[1],
) {
  const server = answerCallsWith({ StartChatCall: () => call, ...extra })
  const session = fakeSession(snapshot, devices)
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

it('marks whoever has their microphone off beside their name, this person from the moment they mute', async () => {
  const { screenDialog } = await startCall(
    callOf({
      ...onTheCall,
      members: members.map((member) => (member.userId === 'person-b' ? { ...member, isMuted: true } : member)),
    }),
    { ...quietSnapshot, isMuted: true },
  )

  const tiles = within(screenDialog).getAllByRole('listitem')
  expect(tiles.map((tile) => within(tile).queryByRole('img', { name: 'Muted' }) !== null)).toEqual([true, true, false])
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

it('tells everybody once transcription is on, in a notice they close, and asks nobody', async () => {
  const asked = callOf({
    askerRingState: 'answered',
    isAskerOn: true,
    answeredAt: '2030-01-01T10:00:05Z',
    transcriptionAskedByUserId: 'person-b',
    members: [
      memberOf('person-a', 'Mira', { isOn: true }),
      memberOf('person-b', 'Tobias', { ringState: 'none', isOn: true }),
    ],
  })
  const { screenDialog } = await startCall(asked)

  const notice = within(screenDialog).getByRole('status', { name: 'Tobias turned on transcription' })
  expect(within(notice).getByText(/Everyone's voice on this call is transcribed/)).toBeTruthy()
  expect(within(screenDialog).getByText('Transcribing')).toBeTruthy()
  // It is on already: no second ask, and nothing to answer.
  expect(within(screenDialog).queryByRole('button', { name: 'Transcribe this call' })).toBeNull()
  expect(within(screenDialog).queryByRole('button', { name: 'Allow' })).toBeNull()

  fireEvent.click(within(notice).getByRole('button', { name: 'Dismiss' }))

  expect(within(screenDialog).queryByRole('status', { name: 'Tobias turned on transcription' })).toBeNull()
  // The mark in the head stays while the call is transcribed.
  expect(within(screenDialog).getByText('Transcribing')).toBeTruthy()
})

it('tells whoever turned transcription on that they did', async () => {
  const asked = callOf({
    askerRingState: 'answered',
    isAskerOn: true,
    transcriptionAskedByUserId: 'person-a',
    members: [memberOf('person-a', 'Mira', { isOn: true })],
  })
  const { screenDialog } = await startCall(asked)

  expect(within(screenDialog).getByRole('status', { name: 'You turned on transcription' })).toBeTruthy()
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

// A browser that can play an audio element on a device of its choice.
function allowSoundOutputChoice() {
  const setSinkId = vi.fn(async () => {})
  Object.defineProperty(HTMLMediaElement.prototype, 'setSinkId', { value: setSinkId, configurable: true })
  return setSinkId
}

const devices = {
  microphones: [
    deviceOf('audioinput', 'default', 'Default - Built-in microphone'),
    deviceOf('audioinput', 'microphone-built-in', 'Built-in microphone'),
    deviceOf('audioinput', 'microphone-headset', 'Headset microphone'),
  ],
  speakers: [
    deviceOf('audiooutput', 'default', ''),
    deviceOf('audiooutput', 'speaker-built-in', 'Built-in speakers'),
    deviceOf('audiooutput', 'speaker-headset', 'Headset'),
  ],
}

afterEach(() => {
  Reflect.deleteProperty(HTMLMediaElement.prototype, 'setSinkId')
})

it('chooses the microphone from the audio menu, marking the one in use', async () => {
  const { screenDialog, session } = await startCall(
    onTheCall,
    { ...quietSnapshot, microphoneDeviceId: 'microphone-built-in' },
    {},
    devices,
  )

  fireEvent.click(within(screenDialog).getByRole('button', { name: 'Sound and microphone' }))
  const menu = await screen.findByRole('dialog', { name: 'Sound and microphone' })
  const builtIn = await within(menu).findByRole('button', { name: 'Built-in microphone' })
  expect(builtIn.getAttribute('aria-pressed')).toBe('true')
  expect(within(menu).getByRole('button', { name: 'Headset microphone' }).getAttribute('aria-pressed')).toBe('false')

  fireEvent.click(within(menu).getByRole('button', { name: 'Headset microphone' }))
  expect(session.setMicrophone).toHaveBeenCalledWith('microphone-headset')
  // Choosing closes the menu, and the call stays on screen.
  expect(screen.queryByRole('dialog', { name: 'Sound and microphone' })).toBeNull()
  expect(screen.getByRole('dialog', { name: 'Call in Trip planning' })).toBeTruthy()
})

it('plays the others on the chosen device, new voices included, and names the default one', async () => {
  const setSinkId = allowSoundOutputChoice()
  const { screenDialog, session } = await startCall(
    onTheCall,
    {
      ...quietSnapshot,
      participants: { 'person-b': { audioStream: {} as MediaStream, videoStream: null } },
    },
    {},
    devices,
  )
  // Nothing is asked of a call that never chose a device.
  expect(setSinkId).not.toHaveBeenCalled()

  fireEvent.click(within(screenDialog).getByRole('button', { name: 'Sound and microphone' }))
  const menu = await screen.findByRole('dialog', { name: 'Sound and microphone' })
  // The default device has no label until the browser names it.
  const systemDefault = await within(menu).findByRole('button', { name: 'System default' })
  expect(systemDefault.getAttribute('aria-pressed')).toBe('true')

  fireEvent.click(within(menu).getByRole('button', { name: 'Headset' }))
  expect(session.setSoundOutput).toHaveBeenCalledWith('speaker-headset')
  await waitFor(() => expect(setSinkId).toHaveBeenCalledWith('speaker-headset'))
  expect(document.querySelectorAll('audio')).toHaveLength(1)
})

it('leaves out the sound section where the browser cannot choose where sound plays', async () => {
  const { screenDialog } = await startCall(onTheCall, quietSnapshot, {}, devices)

  fireEvent.click(within(screenDialog).getByRole('button', { name: 'Sound and microphone' }))
  const menu = await screen.findByRole('dialog', { name: 'Sound and microphone' })
  await within(menu).findByRole('button', { name: 'Headset microphone' })
  expect(within(menu).queryByText('Play sound on')).toBeNull()
  expect(within(menu).queryByRole('button', { name: 'Built-in speakers' })).toBeNull()
})

it('closes the audio menu on Escape without putting the call away, and on a click outside it', async () => {
  const { screenDialog } = await startCall(onTheCall, quietSnapshot, {}, devices)
  const open = () => fireEvent.click(within(screenDialog).getByRole('button', { name: 'Sound and microphone' }))

  open()
  await screen.findByRole('dialog', { name: 'Sound and microphone' })
  fireEvent.keyDown(document, { key: 'Escape' })
  expect(screen.queryByRole('dialog', { name: 'Sound and microphone' })).toBeNull()
  expect(screen.getByRole('dialog', { name: 'Call in Trip planning' })).toBeTruthy()

  open()
  await screen.findByRole('dialog', { name: 'Sound and microphone' })
  fireEvent.mouseDown(document.body)
  expect(screen.queryByRole('dialog', { name: 'Sound and microphone' })).toBeNull()

  // With no menu open, Escape puts the call away as it did.
  fireEvent.keyDown(document, { key: 'Escape' })
  expect(await screen.findByRole('group', { name: 'Call in Trip planning' })).toBeTruthy()
})
