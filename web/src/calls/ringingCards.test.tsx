import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'

import { graphql } from '../api'
import { useChatCalls } from './chatCallContext'
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

// A call that Tobias started in the group, ringing the signed-in Mira.
const ringing = callOf()
const answered = callOf({
  askerRingState: 'answered',
  isAskerOn: true,
  answeredAt: '2030-01-01T10:00:05Z',
})

function operations() {
  return vi.mocked(graphql).mock.calls.map(([document]) => String(document))
}

it('rings on the page with who is calling, the chat, and what can be done', async () => {
  answerCallsWith({ ListGoingChatCalls: () => [ringing] })
  renderWithCalls(<p>The page</p>)

  expect(await screen.findByText('Tobias is calling')).toBeTruthy()
  expect(screen.getByText('Trip planning')).toBeTruthy()
  expect(screen.getByRole('button', { name: 'Answer' })).toBeTruthy()
  expect(screen.getByRole('button', { name: 'Answer with video' })).toBeTruthy()
  expect(screen.getByRole('button', { name: 'Decline' })).toBeTruthy()
})

it('does not ring for a call that is not ringing the person', async () => {
  answerCallsWith({ ListGoingChatCalls: () => [callOf({ askerRingState: 'declined' })] })
  renderWithCalls(<p>The page</p>)

  await waitFor(() => expect(operations().some((document) => document.includes('ListGoingChatCalls'))).toBe(true))
  expect(screen.queryByText('Tobias is calling')).toBeNull()
})

it('answers with audio: joins the call, then connects with the camera off', async () => {
  answerCallsWith({ ListGoingChatCalls: () => [ringing], JoinChatCall: () => answered })
  const session = fakeSession()
  const createSession = vi.fn(() => session)
  renderWithCalls(<p>The page</p>, createSession)

  fireEvent.click(await screen.findByRole('button', { name: 'Answer' }))

  await waitFor(() => expect(session.start).toHaveBeenCalled())
  expect(vi.mocked(graphql)).toHaveBeenCalledWith(expect.stringContaining('JoinChatCall('), { callId: 'call-one' })
  expect(createSession).toHaveBeenCalledWith(expect.objectContaining({ callId: 'call-one', isVideoOn: false }))
  // The call screen is up, over the page, and the ringing has stopped.
  expect(await screen.findByRole('dialog', { name: 'Call in Trip planning' })).toBeTruthy()
  expect(screen.queryByText('Tobias is calling')).toBeNull()
})

it('answers with video: connects with the camera on', async () => {
  answerCallsWith({ ListGoingChatCalls: () => [ringing], JoinChatCall: () => answered })
  const createSession = vi.fn(() => fakeSession())
  renderWithCalls(<p>The page</p>, createSession)

  fireEvent.click(await screen.findByRole('button', { name: 'Answer with video' }))

  await waitFor(() => expect(createSession).toHaveBeenCalled())
  expect(createSession).toHaveBeenCalledWith(expect.objectContaining({ callId: 'call-one', isVideoOn: true }))
})

it('declines, and the card goes', async () => {
  answerCallsWith({
    ListGoingChatCalls: () => [ringing],
    DeclineChatCall: () => callOf({ askerRingState: 'declined' }),
  })
  const createSession = vi.fn(() => fakeSession())
  renderWithCalls(<p>The page</p>, createSession)

  fireEvent.click(await screen.findByRole('button', { name: 'Decline' }))

  await waitFor(() => expect(screen.queryByText('Tobias is calling')).toBeNull())
  expect(vi.mocked(graphql)).toHaveBeenCalledWith(expect.stringContaining('DeclineChatCall('), { callId: 'call-one' })
  expect(createSession).not.toHaveBeenCalled()
})

it('rings for a call an event tells of, and stops when it is answered somewhere else', async () => {
  const server = answerCallsWith({ ListGoingChatCalls: () => [] })
  renderWithCalls(<p>The page</p>)
  await waitFor(() => expect(operations().some((document) => document.includes('ListGoingChatCalls'))).toBe(true))
  expect(screen.queryByText('Tobias is calling')).toBeNull()

  server.tell(ringing)
  expect(await screen.findByText('Tobias is calling')).toBeTruthy()

  // The same person answered on their phone.
  server.tell(callOf({ askerRingState: 'answered', isAskerOn: true }))
  await waitFor(() => expect(screen.queryByText('Tobias is calling')).toBeNull())
})

it('stops ringing when the caller gives up', async () => {
  const server = answerCallsWith({ ListGoingChatCalls: () => [ringing] })
  renderWithCalls(<p>The page</p>)
  expect(await screen.findByText('Tobias is calling')).toBeTruthy()

  server.tell(
    callOf({ isGoing: false, endReason: 'missed', endedAt: '2030-01-01T10:01:00Z', askerRingState: 'missed' }),
  )
  await waitFor(() => expect(screen.queryByText('Tobias is calling')).toBeNull())
})

// A button that starts a call in another chat, as the drawer's does.
function StartInOtherChat() {
  const { startCall } = useChatCalls()
  return (
    <button type="button" onClick={() => void startCall('other-conversation')}>
      Start elsewhere
    </button>
  )
}

it('says that answering leaves the call the person is on, and does', async () => {
  const server = answerCallsWith({
    ListGoingChatCalls: () => [],
    StartChatCall: () =>
      callOf({
        id: 'call-two',
        conversationId: 'other-conversation',
        chatTitle: 'Garden club',
        askerRingState: 'none',
        isAskerOn: true,
      }),
    JoinChatCall: () => answered,
  })
  const sessions = [fakeSession(), fakeSession()]
  const createSession = vi.fn(() => sessions[createSession.mock.calls.length - 1])
  renderWithCalls(<StartInOtherChat />, createSession)

  fireEvent.click(screen.getByRole('button', { name: 'Start elsewhere' }))
  expect(await screen.findByRole('dialog', { name: 'Call in Garden club' })).toBeTruthy()

  server.tell(ringing)
  expect(await screen.findByText('Answering leaves your current call.')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'Answer' }))

  await waitFor(() => expect(sessions[1].start).toHaveBeenCalled())
  expect(sessions[0].leave).toHaveBeenCalled()
  expect(await screen.findByRole('dialog', { name: 'Call in Trip planning' })).toBeTruthy()
})
