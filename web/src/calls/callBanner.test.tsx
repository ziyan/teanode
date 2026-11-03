import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'

import { graphql } from '../api'
import { ChatCallBanner } from './callBanner'
import { answerCallsWith, callOf, fakeSession, memberOf, renderWithCalls } from './chatCallFixtures'

vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  graphql: vi.fn(),
  subscribe: vi.fn(),
}))

afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})

// A call going in the chat that did not ring the signed-in Mira (she set the
// chat to quiet), with Tobias and Priya on it.
const going = callOf({
  askerRingState: '',
  members: [
    memberOf('person-b', 'Tobias', { ringState: 'none', isOn: true }),
    memberOf('person-c', 'Priya', { ringState: 'answered', isOn: true }),
  ],
})

it('reads the call going when the chat opens, and says how many are on it', async () => {
  answerCallsWith({ ReadGoingChatCall: () => going })
  renderWithCalls(<ChatCallBanner conversationId="chat-conversation" />)

  expect(await screen.findByText('Call going, 2 on it')).toBeTruthy()
  expect(vi.mocked(graphql)).toHaveBeenCalledWith(expect.stringContaining('ReadGoingChatCall('), {
    conversationId: 'chat-conversation',
  })
  expect(screen.getByRole('button', { name: 'Join' })).toBeTruthy()
})

it('draws nothing when no call is going in the chat', async () => {
  answerCallsWith({ ReadGoingChatCall: () => null })
  const { container } = renderWithCalls(<ChatCallBanner conversationId="chat-conversation" />)

  await waitFor(() =>
    expect(vi.mocked(graphql).mock.calls.some(([document]) => String(document).includes('ReadGoingChatCall'))).toBe(
      true,
    ),
  )
  expect(container.querySelector('.call-banner')).toBeNull()
})

it('joins the call and connects, and the banner goes while the person is on it', async () => {
  answerCallsWith({
    ReadGoingChatCall: () => going,
    JoinChatCall: () => callOf({ askerRingState: 'answered', isAskerOn: true, members: going.members }),
  })
  const session = fakeSession()
  renderWithCalls(<ChatCallBanner conversationId="chat-conversation" />, () => session)

  fireEvent.click(await screen.findByRole('button', { name: 'Join' }))

  await waitFor(() => expect(session.start).toHaveBeenCalled())
  expect(vi.mocked(graphql)).toHaveBeenCalledWith(expect.stringContaining('JoinChatCall('), { callId: 'call-one' })
  await waitFor(() => expect(screen.queryByText('Call going, 2 on it')).toBeNull())
})

it('follows the call as the events tell of it', async () => {
  const server = answerCallsWith({ ReadGoingChatCall: () => null })
  renderWithCalls(<ChatCallBanner conversationId="chat-conversation" />)
  await waitFor(() =>
    expect(vi.mocked(graphql).mock.calls.some(([document]) => String(document).includes('ReadGoingChatCall'))).toBe(
      true,
    ),
  )

  // A call starts in the chat, then a third person joins, then it ends.
  server.tell(going)
  expect(await screen.findByText('Call going, 2 on it')).toBeTruthy()

  server.tell(
    callOf({
      askerRingState: '',
      members: [...going.members, memberOf('person-d', 'Anika', { ringState: 'answered', isOn: true })],
    }),
  )
  expect(await screen.findByText('Call going, 3 on it')).toBeTruthy()

  server.tell(callOf({ isGoing: false, endReason: 'ended', endedAt: '2030-01-01T10:09:00Z', askerRingState: '' }))
  await waitFor(() => expect(screen.queryByText(/Call going/)).toBeNull())
})

it('says one person on a call in the singular', async () => {
  answerCallsWith({
    ReadGoingChatCall: () =>
      callOf({ askerRingState: '', members: [memberOf('person-b', 'Tobias', { ringState: 'none', isOn: true })] }),
  })
  renderWithCalls(<ChatCallBanner conversationId="chat-conversation" />)

  expect(await screen.findByText('Call going, 1 on it')).toBeTruthy()
})

it('is not shown for another chat', async () => {
  answerCallsWith({ ReadGoingChatCall: () => going })
  const { container } = renderWithCalls(<ChatCallBanner conversationId="another-conversation" />)

  await waitFor(() =>
    expect(vi.mocked(graphql).mock.calls.some(([document]) => String(document).includes('ReadGoingChatCall'))).toBe(
      true,
    ),
  )
  expect(container.querySelector('.call-banner')).toBeNull()
})
